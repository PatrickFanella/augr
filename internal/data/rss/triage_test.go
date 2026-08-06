package rss

import (
	"context"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type stubTriageProvider struct {
	lastRequest llm.CompletionRequest
	responses   []string
	callCount   int
}

func (s *stubTriageProvider) Complete(_ context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	s.lastRequest = request
	s.callCount++

	if len(s.responses) > 0 {
		idx := s.callCount - 1
		if idx >= len(s.responses) {
			idx = len(s.responses) - 1
		}
		return &llm.CompletionResponse{Content: s.responses[idx]}, nil
	}

	prompt := request.Messages[len(request.Messages)-1].Content
	if strings.Contains(prompt, `"results"`) {
		return &llm.CompletionResponse{Content: `{"results":[{"tickers":["AAPL"],"category":"company","sentiment":"bullish","relevance":0.9,"summary":"Apple demand stays strong"},{"tickers":["MSFT"],"category":"company","sentiment":"neutral","relevance":0.6,"summary":"Microsoft updates product plans"}]}`}, nil
	}

	// Simulate the observed Ollama behavior when JSON-object mode is requested
	// but the prompt asks for an array: the model returns a single object, which
	// triage cannot map back to the batch.
	return &llm.CompletionResponse{Content: `{"category":"company"}`}, nil
}

func TestTriageRequestsResultsWrapperAndParsesBatch(t *testing.T) {
	t.Parallel()

	provider := &stubTriageProvider{}
	articles := []Article{
		{
			GUID:        "guid-1",
			Source:      "Reuters",
			Title:       "Apple launches new product",
			Description: "Demand remains strong.",
		},
		{
			GUID:        "guid-2",
			Source:      "Bloomberg",
			Title:       "Microsoft updates product plans",
			Description: "Analysts expect limited near-term impact.",
		},
	}

	results := Triage(context.Background(), provider, "", articles, nil)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	if got := results["guid-1"]; got == nil || got.Summary != "Apple demand stays strong" {
		t.Fatalf("results[guid-1] = %#v, want parsed wrapper result", got)
	}
	if got := results["guid-2"]; got == nil || got.Summary != "Microsoft updates product plans" {
		t.Fatalf("results[guid-2] = %#v, want parsed wrapper result", got)
	}

	if provider.lastRequest.ResponseFormat == nil || provider.lastRequest.ResponseFormat.Type != llm.ResponseFormatJSONObject {
		t.Fatalf("ResponseFormat = %#v, want json_object", provider.lastRequest.ResponseFormat)
	}

	prompt := provider.lastRequest.Messages[len(provider.lastRequest.Messages)-1].Content
	if !strings.Contains(prompt, `"results"`) {
		t.Fatalf("prompt = %q, want results wrapper instructions", prompt)
	}
}

func TestTriageParsesOllamaProsePrefixedJSON(t *testing.T) {
	t.Parallel()

	provider := &stubTriageProvider{responses: []string{
		"**Classifying headlines with ticker identification**\n" +
			`{"results":[{"tickers":[" aapl "],"category":"Company","sentiment":"Bullish","relevance":0.9,"summary":" Apple demand stays strong "}]}`,
	}}
	articles := []Article{{GUID: "guid-1", Source: "Reuters", Title: "Apple (AAPL) demand stays strong"}}

	results := Triage(context.Background(), provider, "", articles, nil)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	got := results["guid-1"]
	if got == nil || len(got.Tickers) != 1 || got.Tickers[0] != "AAPL" || got.Category != "company" || got.Sentiment != "bullish" || got.Summary != "Apple demand stays strong" {
		t.Fatalf("results[guid-1] = %#v, want normalized parsed result", got)
	}
}

func TestTriageDropsTickerInferredFromCompanyName(t *testing.T) {
	t.Parallel()

	provider := &stubTriageProvider{responses: []string{
		`{"results":[{"tickers":["DEI"],"category":"company","sentiment":"neutral","relevance":0.7,"summary":"Diversified Energy declares dividend"}]}`,
	}}
	articles := []Article{{GUID: "guid-1", Source: "Business Wire", Title: "Diversified Energy declares dividend"}}

	results := Triage(context.Background(), provider, "", articles, nil)
	got := results["guid-1"]
	if got == nil {
		t.Fatal("results[guid-1] = nil, want classified result")
	}
	if len(got.Tickers) != 0 {
		t.Fatalf("results[guid-1].Tickers = %#v, want ungrounded ticker removed", got.Tickers)
	}
}

func TestTriageRejectsWrongResultCount(t *testing.T) {
	t.Parallel()

	provider := &stubTriageProvider{responses: []string{
		`{"results":[{"tickers":["AAPL"],"category":"company","sentiment":"bullish","relevance":0.9,"summary":"Apple demand stays strong"}]}`,
	}}
	articles := []Article{
		{GUID: "guid-1", Title: "Apple demand stays strong"},
		{GUID: "guid-2", Title: "Microsoft updates product plans"},
	}

	results := Triage(context.Background(), provider, "", articles, nil)
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0 for incomplete provenance mapping", len(results))
	}
}

func TestTriageRetriesOnceOnEmptyTruncatedResponse(t *testing.T) {
	t.Parallel()

	provider := &stubTriageProvider{responses: []string{
		"",
		`{"results":[{"tickers":["AAPL"],"category":"company","sentiment":"bullish","relevance":0.9,"summary":"Apple demand stays strong"},{"tickers":["MSFT"],"category":"company","sentiment":"neutral","relevance":0.6,"summary":"Microsoft updates product plans"}]}`,
	}}
	articles := []Article{
		{
			GUID:        "guid-1",
			Source:      "Reuters",
			Title:       "Apple launches new product",
			Description: "Demand remains strong.",
		},
		{
			GUID:        "guid-2",
			Source:      "Bloomberg",
			Title:       "Microsoft updates product plans",
			Description: "Analysts expect limited near-term impact.",
		},
	}

	results := Triage(context.Background(), provider, "", articles, nil)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if provider.callCount != 2 {
		t.Fatalf("callCount = %d, want 2", provider.callCount)
	}
	if got := results["guid-1"]; got == nil || got.Summary != "Apple demand stays strong" {
		t.Fatalf("results[guid-1] = %#v, want parsed wrapper result after retry", got)
	}
	if got := results["guid-2"]; got == nil || got.Summary != "Microsoft updates product plans" {
		t.Fatalf("results[guid-2] = %#v, want parsed wrapper result after retry", got)
	}
}

func TestTriageSynthesizesNeutralFallbackAfterPersistentEmptyResponsesForMultiHeadlineBatch(t *testing.T) {
	t.Parallel()

	provider := &stubTriageProvider{responses: []string{"", ""}}
	articles := []Article{
		{
			GUID:        "guid-1",
			Source:      "Reuters",
			Title:       "Apple launches new product",
			Description: "Demand remains strong.",
		},
		{
			GUID:        "guid-2",
			Source:      "Bloomberg",
			Title:       "Microsoft updates product plans",
			Description: "Analysts expect limited near-term impact.",
		},
	}

	results := Triage(context.Background(), provider, "", articles, nil)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2 fallback results", len(results))
	}
	if provider.callCount != 2 {
		t.Fatalf("callCount = %d, want 2", provider.callCount)
	}
	if got := results["guid-1"]; got == nil || got.Category != "other" || got.Sentiment != "neutral" || got.Summary != "Apple launches new product" {
		t.Fatalf("results[guid-1] = %#v, want neutral fallback for first headline", got)
	}
	if got := results["guid-2"]; got == nil || got.Category != "other" || got.Sentiment != "neutral" || got.Summary != "Microsoft updates product plans" {
		t.Fatalf("results[guid-2] = %#v, want neutral fallback for second headline", got)
	}
}
