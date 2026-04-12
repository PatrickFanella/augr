package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
	if m.LLMFallbackTotal == nil {
		t.Fatal("LLMFallbackTotal is nil")
	}
	if m.OrdersTotal == nil {
		t.Fatal("OrdersTotal is nil")
	}
	if m.SignalParseFailuresTotal == nil {
		t.Fatal("SignalParseFailuresTotal is nil")
	}
	if m.SchedulerTickTotal == nil {
		t.Fatal("SchedulerTickTotal is nil")
	}
	if m.AutomationJobErrorsTotal == nil {
		t.Fatal("AutomationJobErrorsTotal is nil")
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
	m.RecordLLMFallback("deadline_exceeded")
	m.RecordLLMTokens(100, 200)
	m.ObserveLLMLatency("openai", "gpt-4", 0.8)
	m.RecordOrder("alpaca", "buy", "filled")
	m.RecordSignalParseFailure()
	m.RecordSchedulerTick("strategy")
	m.RecordAutomationJobError("sync_positions")
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
	m.RecordLLMFallback("deadline_exceeded")
	m.RecordLLMTokens(100, 200)
	m.ObserveLLMLatency("openai", "gpt-4", 0.8)
	m.RecordOrder("alpaca", "buy", "filled")
	m.RecordSignalParseFailure()
	m.RecordSchedulerTick("strategy")
	m.RecordAutomationJobError("sync_positions")
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
		"tradingagent_llm_fallback_total",
		"tradingagent_llm_tokens_total",
		"tradingagent_llm_latency_seconds",
		"tradingagent_orders_total",
		"tradingagent_signal_parse_failures_total",
		"tradingagent_scheduler_tick_total",
		"tradingagent_automation_job_errors_total",
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

func TestNewCounters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		collector func(*metrics.Metrics) prometheus.Collector
		add       func(*metrics.Metrics)
		want      string
	}{
		{
			name:      "llm fallback",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.LLMFallbackTotal },
			add: func(m *metrics.Metrics) {
				m.RecordLLMFallback("deadline_exceeded")
				m.RecordLLMFallback("provider_error")
			},
			want: `# HELP tradingagent_llm_fallback_total Total LLM fallback events by reason.
# TYPE tradingagent_llm_fallback_total counter
tradingagent_llm_fallback_total{reason="deadline_exceeded"} 1
tradingagent_llm_fallback_total{reason="provider_error"} 1
`,
		},
		{
			name:      "signal parse failures",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.SignalParseFailuresTotal },
			add: func(m *metrics.Metrics) {
				m.RecordSignalParseFailure()
				m.RecordSignalParseFailure()
			},
			want: `# HELP tradingagent_signal_parse_failures_total Total signal parse failures.
# TYPE tradingagent_signal_parse_failures_total counter
tradingagent_signal_parse_failures_total 2
`,
		},
		{
			name:      "scheduler tick",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.SchedulerTickTotal },
			add: func(m *metrics.Metrics) {
				m.RecordSchedulerTick("strategy")
				m.RecordSchedulerTick("backtest")
				m.RecordSchedulerTick("discovery")
			},
			want: `# HELP tradingagent_scheduler_tick_total Total scheduler ticks by type.
# TYPE tradingagent_scheduler_tick_total counter
tradingagent_scheduler_tick_total{type="backtest"} 1
tradingagent_scheduler_tick_total{type="discovery"} 1
tradingagent_scheduler_tick_total{type="strategy"} 1
`,
		},
		{
			name:      "automation job errors",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.AutomationJobErrorsTotal },
			add: func(m *metrics.Metrics) {
				m.RecordAutomationJobError("sync_positions")
				m.RecordAutomationJobError("sync_positions")
				m.RecordAutomationJobError("reconcile_orders")
			},
			want: `# HELP tradingagent_automation_job_errors_total Total automation job errors by job name.
# TYPE tradingagent_automation_job_errors_total counter
tradingagent_automation_job_errors_total{job_name="reconcile_orders"} 1
tradingagent_automation_job_errors_total{job_name="sync_positions"} 2
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := metrics.New()
			tt.add(m)
			if err := testutil.CollectAndCompare(tt.collector(m), strings.NewReader(tt.want)); err != nil {
				t.Fatalf("collect compare failed: %v", err)
			}
		})
	}
}
