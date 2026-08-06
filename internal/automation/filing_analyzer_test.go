package automation

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestAnalyzeFilingRejectsIncompleteInputsAndOutputs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>Material filing text</body></html>"))
	}))
	defer server.Close()

	filing := domain.SECFiling{Symbol: "AAPL", Form: "8-K", FiledDate: time.Now().UTC(), URL: server.URL}
	tests := []struct {
		name     string
		filing   domain.SECFiling
		provider llm.Provider
		want     string
	}{
		{name: "fetch", filing: domain.SECFiling{Symbol: "AAPL"}, provider: filingProvider(`{}`), want: "fetch filing text"},
		{name: "provider", filing: filing, provider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return nil, errors.New("unavailable")
		}), want: "LLM call"},
		{name: "nil response", filing: filing, provider: llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) { return nil, nil }), want: "nil response"},
		{name: "malformed", filing: filing, provider: filingProvider(`not-json`), want: "parse LLM response"},
		{name: "invalid schema", filing: filing, provider: filingProvider(`{"sentiment":"neutral","impact":"low","action":"ignore","confidence":0.5,"summary":"summary","reasoning":"reason"}`), want: "invalid action"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := AnalyzeFiling(context.Background(), test.provider, "", test.filing, "strategy", slog.Default())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("AnalyzeFiling() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAnalyzeFilingReturnsValidatedResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("Material filing text"))
	}))
	defer server.Close()
	filed := time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC)
	filing := domain.SECFiling{Symbol: "AAPL", Form: "8-K", FiledDate: filed, URL: server.URL}
	analysis, err := AnalyzeFiling(context.Background(), filingProvider(`{"sentiment":"bearish","impact":"high","action":"reduce_position","confidence":0.8,"summary":"Material weakness.","reasoning":"Risk increased.","key_items":["weakness"]}`), "", filing, "strategy", slog.Default())
	if err != nil {
		t.Fatalf("AnalyzeFiling() error = %v", err)
	}
	if analysis.Symbol != "AAPL" || analysis.Form != "8-K" || !analysis.FiledDate.Equal(filed) {
		t.Fatalf("analysis metadata = %#v", analysis)
	}
}

func filingProvider(content string) llm.Provider {
	return llm.ProviderFunc(func(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
		return &llm.CompletionResponse{Content: content}, nil
	})
}
