package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
)

// metrics.New() uses a private registry per instance, so New() can be called
// multiple times in the same process without duplicate-registration panics.
// Each test that needs an isolated registry should call metrics.New() directly.
var shared = metrics.New()

func TestNew(t *testing.T) {
	t.Parallel()
	m := metrics.New() // each call is now safe
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
	if m.StaleRunsReconciled == nil {
		t.Fatal("StaleRunsReconciled is nil")
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
	m.RecordStaleRunReconciled()
	m.SetPortfolioValue(50000.0)
	m.SetPositionsOpen(3)
	m.SetCircuitBreakerState(true)
	m.SetKillSwitchActive(false)
}

func TestHandler(t *testing.T) {
	t.Parallel()
	// Use a fresh isolated registry so this test does not depend on the
	// execution order of TestConvenienceMethods. Vector metrics (counters,
	// histograms) only appear in the output once at least one label
	// combination has been observed; record seed data here.
	m := metrics.New()
	m.RecordPipelineRun("AAPL", "buy", "success")
	m.ObservePipelineDuration("AAPL", 1.5)
	m.RecordLLMCall("openai", "gpt-4", "analyst")
	m.RecordLLMTokens(100, 200)
	m.ObserveLLMLatency("openai", "gpt-4", 0.8)
	m.RecordOrder("alpaca", "buy", "filled")
	m.RecordStaleRunReconciled()

	h := m.Handler()
	if h == nil {
		t.Fatal("Handler() returned nil")
	}

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
		"tradingagent_stale_runs_reconciled_total",
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
