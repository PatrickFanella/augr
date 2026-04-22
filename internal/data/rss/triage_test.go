package rss

import (
	"context"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type stubTriageProvider struct {
	lastRequest llm.CompletionRequest
}

func (s *stubTriageProvider) Complete(_ context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	s.lastRequest = request

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
