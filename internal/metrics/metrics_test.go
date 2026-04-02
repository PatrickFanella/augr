package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
)

// newMetrics creates a Metrics instance for testing. Because promauto registers
// against the default registry and we can only register each name once per
// process, all tests share this single instance.
var shared = metrics.New()

func TestNew(t *testing.T) {
	t.Parallel()
	m := shared
	if m.PipelineRunsTotal == nil {
		t.Fatal("PipelineRunsTotal is nil")
	}
	if m.PipelineDuration == nil {
		t.Fatal("PipelineDuration is nil")
	}
	if m.LLMCallsTotal == nil {
		t.Fatal("LLMCallsTotal is nil")
	}
	if m.LLMTokensTotal == nil {
		t.Fatal("LLMTokensTotal is nil")
	}
	if m.LLMLatency == nil {
		t.Fatal("LLMLatency is nil")
	}
	if m.OrdersTotal == nil {
		t.Fatal("OrdersTotal is nil")
	}
	if m.PortfolioValue == nil {
		t.Fatal("PortfolioValue is nil")
	}
	if m.PositionsOpen == nil {
		t.Fatal("PositionsOpen is nil")
	}
	if m.CircuitBreakerState == nil {
		t.Fatal("CircuitBreakerState is nil")
	}
	if m.KillSwitchActive == nil {
		t.Fatal("KillSwitchActive is nil")
	}
}

func TestConvenienceMethods(t *testing.T) {
	t.Parallel()
	m := shared

	// None of these should panic.
	m.RecordPipelineRun("AAPL", "buy", "success")
	m.ObservePipelineDuration("AAPL", 1.5)
	m.RecordLLMCall("openai", "gpt-4", "analyst")
	m.RecordLLMTokens(100, 200)
	m.ObserveLLMLatency("openai", "gpt-4", 0.8)
	m.RecordOrder("alpaca", "buy", "filled")
	m.SetPortfolioValue(50000.0)
	m.SetPositionsOpen(3)
	m.SetCircuitBreakerState(true)
	m.SetKillSwitchActive(false)
}

func TestHandler(t *testing.T) {
	t.Parallel()
	m := shared

	h := m.Handler()
	if h == nil {
		t.Fatal("Handler() returned nil")
	}

	// Ensure the handler implements http.Handler.
	_ = h

	// Fire a request and check that expected metric names appear in the output.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	expected := []string{
		"tradingagent_pipeline_runs_total",
		"tradingagent_pipeline_duration_seconds",
		"tradingagent_llm_calls_total",
		"tradingagent_llm_tokens_total",
		"tradingagent_llm_latency_seconds",
		"tradingagent_orders_total",
		"tradingagent_portfolio_value",
		"tradingagent_positions_open",
		"tradingagent_circuit_breaker_state",
		"tradingagent_kill_switch_active",
	}
	for _, name := range expected {
		if !strings.Contains(body, name) {
			t.Errorf("handler output missing metric %q", name)
		}
	}
}
