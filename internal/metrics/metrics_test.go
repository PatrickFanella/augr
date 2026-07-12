package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	if m.PolymarketReconciliationDriftTotal == nil {
		t.Fatal("PolymarketReconciliationDriftTotal is nil")
	}
	if m.PolymarketStopGuardTriggeredTotal == nil {
		t.Fatal("PolymarketStopGuardTriggeredTotal is nil")
	}
	if m.PolymarketStopGuardSendErrorsTotal == nil {
		t.Fatal("PolymarketStopGuardSendErrorsTotal is nil")
	}
	if m.PolymarketStopGuardTickToFire == nil {
		t.Fatal("PolymarketStopGuardTickToFire is nil")
	}
	if m.PolymarketStopGuardActive == nil {
		t.Fatal("PolymarketStopGuardActive is nil")
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
	if m.LLMRetryTotal == nil {
		t.Fatal("LLMRetryTotal is nil")
	}
	if m.LLMBudgetExhaustedTotal == nil {
		t.Fatal("LLMBudgetExhaustedTotal is nil")
	}
	if m.ReportWorkerSuccessTotal == nil {
		t.Fatal("ReportWorkerSuccessTotal is nil")
	}
	if m.ReportWorkerErrorTotal == nil {
		t.Fatal("ReportWorkerErrorTotal is nil")
	}
	if m.ReportStaleness == nil {
		t.Fatal("ReportStaleness is nil")
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
	m.RecordGeneratorOutcome("stock", "success_first_attempt")
	m.RecordDataSourceSuccess("reddit", time.Unix(1_700_000_000, 0))
	m.SetDataSourceCooldown("reddit", time.Unix(1_700_000_300, 0))
	m.RecordSchedulerTick("strategy")
	m.RecordAutomationJobError("sync_positions")
	m.RecordStaleRunReconciled()
	m.IncDrift("quantity_mismatch")
	m.IncTriggered("market-a")
	m.IncSendError("market-a")
	m.ObserveTickToFireSeconds("market-a", 1.25)
	m.SetActive(3)
	m.SetPortfolioValue(50000.0)
	m.SetPositionsOpen(3)
	m.SetCircuitBreakerState(true)
	m.SetKillSwitchActive(false)
	m.RecordLLMRetry("configured_primary:openai")
	m.RecordLLMBudgetExhausted()
	m.RecordReportWorkerSuccess("strategy-a")
	m.RecordReportWorkerError("strategy-a")
	m.ObserveReportStaleness("strategy-a", 120)
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
	m.RecordGeneratorOutcome("stock", "success_first_attempt")
	m.RecordDataSourceSuccess("reddit", time.Unix(1_700_000_000, 0))
	m.SetDataSourceCooldown("reddit", time.Unix(1_700_000_300, 0))
	m.RecordSchedulerTick("strategy")
	m.RecordAutomationJobError("sync_positions")
	m.RecordStaleRunReconciled()
	m.IncDrift("quantity_mismatch")
	m.IncTriggered("market-a")
	m.IncSendError("market-a")
	m.ObserveTickToFireSeconds("market-a", 1.25)
	m.SetActive(3)
	m.RecordLLMRetry("configured_primary:openai")
	m.RecordLLMBudgetExhausted()
	m.RecordReportWorkerSuccess("strategy-a")
	m.RecordReportWorkerError("strategy-a")
	m.ObserveReportStaleness("strategy-a", 120)

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
		"tradingagent_generator_outcomes_total",
		"tradingagent_data_source_last_success_unixtime",
		"tradingagent_data_source_cooldown_until_unixtime",
		"tradingagent_scheduler_tick_total",
		"tradingagent_automation_job_errors_total",
		"tradingagent_polymarket_reconciliation_drift_total",
		"tradingagent_polymarket_stop_guard_triggered_total",
		"tradingagent_polymarket_stop_guard_send_errors_total",
		"tradingagent_polymarket_stop_guard_tick_to_fire_seconds",
		"tradingagent_polymarket_stop_guard_active",
		"tradingagent_stale_runs_reconciled_total",
		"tradingagent_portfolio_value",
		"tradingagent_positions_open",
		"tradingagent_circuit_breaker_state",
		"tradingagent_kill_switch_active",
		"tradingagent_llm_retry_total",
		"tradingagent_llm_budget_exhausted_total",
		"tradingagent_report_worker_success_total",
		"tradingagent_report_worker_error_total",
		"tradingagent_report_staleness_seconds",
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
			name:      "generator outcomes",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.GeneratorOutcomesTotal },
			add: func(m *metrics.Metrics) {
				m.RecordGeneratorOutcome("stock", "success_first_attempt")
				m.RecordGeneratorOutcome("stock", "success_after_retry")
				m.RecordGeneratorOutcome("options", "validation_exhausted")
			},
			want: `# HELP tradingagent_generator_outcomes_total Terminal structured strategy generator outcomes by asset class.
# TYPE tradingagent_generator_outcomes_total counter
tradingagent_generator_outcomes_total{asset="options",outcome="validation_exhausted"} 1
tradingagent_generator_outcomes_total{asset="stock",outcome="success_after_retry"} 1
tradingagent_generator_outcomes_total{asset="stock",outcome="success_first_attempt"} 1
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
		{
			name:      "alpaca reconcile runs",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.AlpacaReconcileRunsTotal },
			add: func(m *metrics.Metrics) {
				m.RecordAlpacaReconcileRun("success")
				m.RecordAlpacaReconcileRun("success")
				m.RecordAlpacaReconcileRun("error")
			},
			want: `# HELP tradingagent_alpaca_reconcile_runs_total Total Alpaca reconciliation runs by outcome.
# TYPE tradingagent_alpaca_reconcile_runs_total counter
tradingagent_alpaca_reconcile_runs_total{result="error"} 1
tradingagent_alpaca_reconcile_runs_total{result="success"} 2
`,
		},
		{
			name:      "polymarket reconciliation drift",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.PolymarketReconciliationDriftTotal },
			add: func(m *metrics.Metrics) {
				m.IncDrift("local_missing_externally")
				m.IncDrift("local_missing_externally")
				m.IncDrift("quantity_mismatch")
			},
			want: `# HELP tradingagent_polymarket_reconciliation_drift_total Total Polymarket reconciliation drifts by drift type.
# TYPE tradingagent_polymarket_reconciliation_drift_total counter
tradingagent_polymarket_reconciliation_drift_total{drift_type="local_missing_externally"} 2
tradingagent_polymarket_reconciliation_drift_total{drift_type="quantity_mismatch"} 1
`,
		},
		{
			name:      "llm retry",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.LLMRetryTotal },
			add: func(m *metrics.Metrics) {
				m.RecordLLMRetry("configured_primary:openai")
				m.RecordLLMRetry("configured_primary:openai")
				m.RecordLLMRetry("configured_primary:anthropic")
			},
			want: `# HELP tradingagent_llm_retry_total Total LLM retry attempts by provider.
# TYPE tradingagent_llm_retry_total counter
tradingagent_llm_retry_total{provider="configured_primary:anthropic"} 1
tradingagent_llm_retry_total{provider="configured_primary:openai"} 2
`,
		},
		{
			name:      "llm budget exhausted",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.LLMBudgetExhaustedTotal },
			add: func(m *metrics.Metrics) {
				m.RecordLLMBudgetExhausted()
				m.RecordLLMBudgetExhausted()
			},
			want: `# HELP tradingagent_llm_budget_exhausted_total Total times an LLM call was rejected due to budget exhaustion.
# TYPE tradingagent_llm_budget_exhausted_total counter
tradingagent_llm_budget_exhausted_total 2
`,
		},
		{
			name:      "report worker success",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.ReportWorkerSuccessTotal },
			add: func(m *metrics.Metrics) {
				m.RecordReportWorkerSuccess("strategy-a")
				m.RecordReportWorkerSuccess("strategy-a")
				m.RecordReportWorkerSuccess("strategy-b")
			},
			want: `# HELP tradingagent_report_worker_success_total Total successful report generations by strategy ID.
# TYPE tradingagent_report_worker_success_total counter
tradingagent_report_worker_success_total{strategy_id="strategy-a"} 2
tradingagent_report_worker_success_total{strategy_id="strategy-b"} 1
`,
		},
		{
			name:      "report worker error",
			collector: func(m *metrics.Metrics) prometheus.Collector { return m.ReportWorkerErrorTotal },
			add: func(m *metrics.Metrics) {
				m.RecordReportWorkerError("strategy-a")
				m.RecordReportWorkerError("strategy-b")
			},
			want: `# HELP tradingagent_report_worker_error_total Total failed report generations by strategy ID.
# TYPE tradingagent_report_worker_error_total counter
tradingagent_report_worker_error_total{strategy_id="strategy-a"} 1
tradingagent_report_worker_error_total{strategy_id="strategy-b"} 1
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

func TestObserveReportStaleness_EmitsSeries(t *testing.T) {
	t.Parallel()

	m := metrics.New()
	m.ObserveReportStaleness("strategy-a", 120)
	if got := testutil.CollectAndCount(m.ReportStaleness); got != 1 {
		t.Fatalf("CollectAndCount(report staleness) = %d, want 1", got)
	}
}

func TestPolymarketStopGuardMetrics(t *testing.T) {
	t.Parallel()

	m := metrics.New()
	m.IncTriggered("market-a")
	m.IncSendError("market-a")
	m.ObserveTickToFireSeconds("market-a", 1.5)
	m.SetActive(4)

	if got := testutil.ToFloat64(m.PolymarketStopGuardActive); got != 4 {
		t.Fatalf("active gauge = %v, want 4", got)
	}
	if got := testutil.CollectAndCount(m.PolymarketStopGuardTickToFire); got != 1 {
		t.Fatalf("CollectAndCount(stop guard tick-to-fire) = %d, want 1", got)
	}
	if err := testutil.CollectAndCompare(m.PolymarketStopGuardTriggeredTotal, strings.NewReader(`
# HELP tradingagent_polymarket_stop_guard_triggered_total Total Polymarket stop-guard triggers.
# TYPE tradingagent_polymarket_stop_guard_triggered_total counter
tradingagent_polymarket_stop_guard_triggered_total 1
`)); err != nil {
		t.Fatalf("trigger metric compare failed: %v", err)
	}
	if err := testutil.CollectAndCompare(m.PolymarketStopGuardSendErrorsTotal, strings.NewReader(`
# HELP tradingagent_polymarket_stop_guard_send_errors_total Total Polymarket stop-guard send errors.
# TYPE tradingagent_polymarket_stop_guard_send_errors_total counter
tradingagent_polymarket_stop_guard_send_errors_total 1
`)); err != nil {
		t.Fatalf("send-error metric compare failed: %v", err)
	}
}
