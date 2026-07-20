package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus instruments for the trading agent.
type Metrics struct {
	registry                           *prometheus.Registry
	PipelineRunsTotal                  *prometheus.CounterVec
	PipelineDuration                   *prometheus.HistogramVec
	LLMCallsTotal                      *prometheus.CounterVec
	LLMFallbackTotal                   *prometheus.CounterVec
	LLMTokensTotal                     *prometheus.CounterVec
	LLMLatency                         *prometheus.HistogramVec
	LLMCacheHitsTotal                  prometheus.Counter
	LLMCacheMissesTotal                prometheus.Counter
	OrdersTotal                        *prometheus.CounterVec
	SignalParseFailuresTotal           prometheus.Counter
	GeneratorOutcomesTotal             *prometheus.CounterVec
	DataSourceLastSuccess              *prometheus.GaugeVec
	DataSourceCooldownUntil            *prometheus.GaugeVec
	SchedulerTickTotal                 *prometheus.CounterVec
	AutomationJobErrorsTotal           *prometheus.CounterVec
	AlpacaReconcileRunsTotal           *prometheus.CounterVec
	KalshiReconcileRunsTotal           *prometheus.CounterVec
	KalshiRateLimitTotal               *prometheus.CounterVec
	KalshiRetryAttemptsTotal           *prometheus.CounterVec
	KalshiRetryWaitSeconds             *prometheus.HistogramVec
	KalshiSettlementDryRunTotal        *prometheus.CounterVec
	KalshiSettlementOutcomeTotal       *prometheus.CounterVec
	KalshiSettlementTransitionTotal    *prometheus.CounterVec
	PolymarketReconciliationDriftTotal *prometheus.CounterVec
	PolymarketStopGuardTriggeredTotal  *prometheus.CounterVec
	PolymarketStopGuardSendErrorsTotal *prometheus.CounterVec
	PolymarketStopGuardTickToFire      *prometheus.HistogramVec
	PolymarketStopGuardActive          prometheus.Gauge
	StaleRunsReconciled                prometheus.Counter
	PortfolioValue                     prometheus.Gauge
	PositionsOpen                      prometheus.Gauge
	CircuitBreakerState                prometheus.Gauge
	KillSwitchActive                   prometheus.Gauge
	LLMRetryTotal                      *prometheus.CounterVec
	LLMBudgetExhaustedTotal            prometheus.Counter
	ReportWorkerSuccessTotal           *prometheus.CounterVec
	ReportWorkerErrorTotal             *prometheus.CounterVec
	ReportStaleness                    *prometheus.HistogramVec
}

// New creates a new isolated Prometheus registry, registers all trading-agent
// metrics on it, and returns a ready-to-use Metrics instance. Using a private
// registry means New() can safely be called more than once (e.g., in tests)
// without triggering duplicate-registration panics on the global default
// registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,

		PipelineRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_pipeline_runs_total",
			Help: "Total number of pipeline runs by ticker, signal, and status.",
		}, []string{"ticker", "signal", "status"}),

		PipelineDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tradingagent_pipeline_duration_seconds",
			Help:    "Pipeline run duration in seconds by ticker.",
			Buckets: prometheus.DefBuckets,
		}, []string{"ticker"}),

		LLMCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_llm_calls_total",
			Help: "Total LLM API calls by provider, model, and agent role.",
		}, []string{"provider", "model", "agent_role"}),

		LLMFallbackTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_llm_fallback_total",
			Help: "Total LLM fallback events by reason.",
		}, []string{"reason"}),

		LLMTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_llm_tokens_total",
			Help: "Total LLM tokens consumed by type (prompt or completion).",
		}, []string{"type"}),

		LLMLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tradingagent_llm_latency_seconds",
			Help:    "LLM call latency in seconds by provider and model.",
			Buckets: prometheus.DefBuckets,
		}, []string{"provider", "model"}),

		LLMCacheHitsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tradingagent_llm_cache_hits_total",
			Help: "Total LLM response cache hits.",
		}),

		LLMCacheMissesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tradingagent_llm_cache_misses_total",
			Help: "Total LLM response cache misses.",
		}),

		OrdersTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_orders_total",
			Help: "Total orders by broker, side, and status.",
		}, []string{"broker", "side", "status"}),

		SignalParseFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tradingagent_signal_parse_failures_total",
			Help: "Total signal parse failures.",
		}),

		GeneratorOutcomesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_generator_outcomes_total",
			Help: "Terminal structured strategy generator outcomes by asset class.",
		}, []string{"asset", "outcome"}),

		DataSourceLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tradingagent_data_source_last_success_unixtime",
			Help: "Unix timestamp of the last successful fetch by external data source.",
		}, []string{"source"}),

		DataSourceCooldownUntil: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tradingagent_data_source_cooldown_until_unixtime",
			Help: "Unix timestamp until which an external data source is cooling down; zero when inactive.",
		}, []string{"source"}),

		SchedulerTickTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_scheduler_tick_total",
			Help: "Total scheduler ticks by type.",
		}, []string{"type"}),

		AutomationJobErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_automation_job_errors_total",
			Help: "Total automation job errors by job name.",
		}, []string{"job_name"}),

		AlpacaReconcileRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_alpaca_reconcile_runs_total",
			Help: "Total Alpaca reconciliation runs by outcome.",
		}, []string{"result"}),

		KalshiReconcileRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_kalshi_reconcile_runs_total",
			Help: "Total Kalshi reconciliation runs by outcome.",
		}, []string{"result"}),

		KalshiRateLimitTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_kalshi_rate_limit_total",
			Help: "Total Kalshi 429 responses by provider, client type, and method.",
		}, []string{"provider", "client_type", "method"}),

		KalshiRetryAttemptsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_kalshi_retry_attempts_total",
			Help: "Total Kalshi retry attempts by provider, client type, and method.",
		}, []string{"provider", "client_type", "method"}),

		KalshiRetryWaitSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tradingagent_kalshi_retry_wait_seconds",
			Help:    "Kalshi retry wait seconds by provider, client type, and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"provider", "client_type", "method"}),

		KalshiSettlementDryRunTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_kalshi_settlement_dry_run_total",
			Help: "Kalshi settlement dry-run decisions.",
		}, []string{"result"}),

		KalshiSettlementOutcomeTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_kalshi_settlement_outcome_total",
			Help: "Kalshi settlement outcomes.",
		}, []string{"result"}),

		KalshiSettlementTransitionTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_kalshi_settlement_transition_total",
			Help: "Kalshi settlement transition helpers.",
		}, []string{"from", "to"}),

		PolymarketReconciliationDriftTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_polymarket_reconciliation_drift_total",
			Help: "Total Polymarket reconciliation drifts by drift type.",
		}, []string{"drift_type"}),

		PolymarketStopGuardTriggeredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_polymarket_stop_guard_triggered_total",
			Help: "Total Polymarket stop-guard triggers.",
		}, nil),

		PolymarketStopGuardSendErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_polymarket_stop_guard_send_errors_total",
			Help: "Total Polymarket stop-guard send errors.",
		}, nil),

		PolymarketStopGuardTickToFire: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tradingagent_polymarket_stop_guard_tick_to_fire_seconds",
			Help:    "Seconds from tick receipt to stop-guard fire.",
			Buckets: prometheus.DefBuckets,
		}, nil),

		PolymarketStopGuardActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_polymarket_stop_guard_active",
			Help: "Number of active Polymarket stop-guard registrations.",
		}),

		StaleRunsReconciled: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tradingagent_stale_runs_reconciled_total",
			Help: "Total number of stale pipeline runs force-failed by the reconciler.",
		}),

		PortfolioValue: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_portfolio_value",
			Help: "Current portfolio value.",
		}),

		PositionsOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_positions_open",
			Help: "Number of currently open positions.",
		}),

		CircuitBreakerState: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_circuit_breaker_state",
			Help: "Circuit breaker state: 1 = active, 0 = inactive.",
		}),

		KillSwitchActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_kill_switch_active",
			Help: "Kill switch state: 1 = active, 0 = inactive.",
		}),

		LLMRetryTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_llm_retry_total",
			Help: "Total LLM retry attempts by provider.",
		}, []string{"provider"}),

		LLMBudgetExhaustedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tradingagent_llm_budget_exhausted_total",
			Help: "Total times an LLM call was rejected due to budget exhaustion.",
		}),

		ReportWorkerSuccessTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_report_worker_success_total",
			Help: "Total successful report generations by strategy ID.",
		}, []string{"strategy_id"}),

		ReportWorkerErrorTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_report_worker_error_total",
			Help: "Total failed report generations by strategy ID.",
		}, []string{"strategy_id"}),

		ReportStaleness: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tradingagent_report_staleness_seconds",
			Help:    "Report staleness in seconds at query time.",
			Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 43200, 86400},
		}, []string{"strategy_id"}),
	}

	reg.MustRegister(
		m.PipelineRunsTotal,
		m.PipelineDuration,
		m.LLMCallsTotal,
		m.LLMFallbackTotal,
		m.LLMTokensTotal,
		m.LLMLatency,
		m.LLMCacheHitsTotal,
		m.LLMCacheMissesTotal,
		m.OrdersTotal,
		m.SignalParseFailuresTotal,
		m.GeneratorOutcomesTotal,
		m.DataSourceLastSuccess,
		m.DataSourceCooldownUntil,
		m.SchedulerTickTotal,
		m.AutomationJobErrorsTotal,
		m.AlpacaReconcileRunsTotal,
		m.KalshiReconcileRunsTotal,
		m.KalshiRateLimitTotal,
		m.KalshiRetryAttemptsTotal,
		m.KalshiRetryWaitSeconds,
		m.KalshiSettlementDryRunTotal,
		m.KalshiSettlementOutcomeTotal,
		m.KalshiSettlementTransitionTotal,
		m.PolymarketReconciliationDriftTotal,
		m.PolymarketStopGuardTriggeredTotal,
		m.PolymarketStopGuardSendErrorsTotal,
		m.PolymarketStopGuardTickToFire,
		m.PolymarketStopGuardActive,
		m.StaleRunsReconciled,
		m.PortfolioValue,
		m.PositionsOpen,
		m.CircuitBreakerState,
		m.KillSwitchActive,
		m.LLMRetryTotal,
		m.LLMBudgetExhaustedTotal,
		m.ReportWorkerSuccessTotal,
		m.ReportWorkerErrorTotal,
		m.ReportStaleness,
	)

	return m
}

func (m *Metrics) RecordPipelineRun(ticker, signal, status string) {
	m.PipelineRunsTotal.WithLabelValues(ticker, signal, status).Inc()
}

func (m *Metrics) ObservePipelineDuration(ticker string, seconds float64) {
	m.PipelineDuration.WithLabelValues(ticker).Observe(seconds)
}

func (m *Metrics) RecordLLMCall(provider, model, agentRole string) {
	m.LLMCallsTotal.WithLabelValues(provider, model, agentRole).Inc()
}

func (m *Metrics) RecordLLMFallback(reason string) {
	m.LLMFallbackTotal.WithLabelValues(reason).Inc()
}

func (m *Metrics) RecordLLMTokens(promptTokens, completionTokens int) {
	m.LLMTokensTotal.WithLabelValues("prompt").Add(float64(promptTokens))
	m.LLMTokensTotal.WithLabelValues("completion").Add(float64(completionTokens))
}

func (m *Metrics) ObserveLLMLatency(provider, model string, seconds float64) {
	m.LLMLatency.WithLabelValues(provider, model).Observe(seconds)
}

func (m *Metrics) RecordLLMCacheHit() {
	if m == nil {
		return
	}
	m.LLMCacheHitsTotal.Inc()
}

func (m *Metrics) RecordLLMCacheMiss() {
	if m == nil {
		return
	}
	m.LLMCacheMissesTotal.Inc()
}

func (m *Metrics) RecordOrder(broker, side, status string) {
	m.OrdersTotal.WithLabelValues(broker, side, status).Inc()
}

func (m *Metrics) RecordSignalParseFailure() {
	m.SignalParseFailuresTotal.Inc()
}

func (m *Metrics) RecordGeneratorOutcome(asset, outcome string) {
	if m == nil {
		return
	}
	m.GeneratorOutcomesTotal.WithLabelValues(asset, outcome).Inc()
}

func (m *Metrics) RecordDataSourceSuccess(source string, at time.Time) {
	if m == nil || at.IsZero() {
		return
	}
	m.DataSourceLastSuccess.WithLabelValues(source).Set(float64(at.Unix()))
}

func (m *Metrics) SetDataSourceCooldown(source string, until time.Time) {
	if m == nil {
		return
	}
	value := float64(0)
	if !until.IsZero() {
		value = float64(until.Unix())
	}
	m.DataSourceCooldownUntil.WithLabelValues(source).Set(value)
}

func (m *Metrics) RecordSchedulerTick(tickType string) {
	m.SchedulerTickTotal.WithLabelValues(tickType).Inc()
}

func (m *Metrics) RecordAutomationJobError(jobName string) {
	m.AutomationJobErrorsTotal.WithLabelValues(jobName).Inc()
}

func (m *Metrics) RecordAlpacaReconcileRun(result string) {
	if m == nil {
		return
	}
	m.AlpacaReconcileRunsTotal.WithLabelValues(result).Inc()
}

func (m *Metrics) RecordKalshiReconcileRun(result string) {
	if m == nil {
		return
	}
	m.KalshiReconcileRunsTotal.WithLabelValues(result).Inc()
}

func (m *Metrics) RecordKalshiRateLimit(provider, clientType, method string) {
	if m == nil {
		return
	}
	m.KalshiRateLimitTotal.WithLabelValues(provider, clientType, method).Inc()
}

func (m *Metrics) RecordKalshiRetryAttempt(provider, clientType, method string) {
	if m == nil {
		return
	}
	m.KalshiRetryAttemptsTotal.WithLabelValues(provider, clientType, method).Inc()
}

func (m *Metrics) ObserveKalshiRetryWaitSeconds(provider, clientType, method string, seconds float64) {
	if m == nil {
		return
	}
	m.KalshiRetryWaitSeconds.WithLabelValues(provider, clientType, method).Observe(seconds)
}

func (m *Metrics) RecordKalshiSettlementDryRun(result string) {
	if m != nil {
		m.KalshiSettlementDryRunTotal.WithLabelValues(result).Inc()
	}
}
func (m *Metrics) RecordKalshiSettlementOutcome(result string) {
	if m != nil {
		m.KalshiSettlementOutcomeTotal.WithLabelValues(result).Inc()
	}
}
func (m *Metrics) RecordKalshiSettlementTransition(from, to string) {
	if m != nil {
		m.KalshiSettlementTransitionTotal.WithLabelValues(from, to).Inc()
	}
}

// IncDrift increments the Polymarket reconciliation drift counter for the given drift type.
func (m *Metrics) IncDrift(driftType string) {
	if m == nil {
		return
	}
	m.PolymarketReconciliationDriftTotal.WithLabelValues(driftType).Inc()
}

// IncTriggered increments the Polymarket stop-guard trigger counter.
func (m *Metrics) IncTriggered(slug string) {
	if m == nil {
		return
	}
	m.PolymarketStopGuardTriggeredTotal.WithLabelValues().Inc()
}

// IncSendError increments the Polymarket stop-guard send-error counter.
func (m *Metrics) IncSendError(slug string) {
	if m == nil {
		return
	}
	m.PolymarketStopGuardSendErrorsTotal.WithLabelValues().Inc()
}

// ObserveTickToFireSeconds records the elapsed seconds between tick receipt and guard fire.
func (m *Metrics) ObserveTickToFireSeconds(slug string, seconds float64) {
	if m == nil {
		return
	}
	m.PolymarketStopGuardTickToFire.WithLabelValues().Observe(seconds)
}

// SetActive updates the active stop-guard gauge.
func (m *Metrics) SetActive(count float64) {
	if m == nil {
		return
	}
	m.PolymarketStopGuardActive.Set(count)
}

func (m *Metrics) RecordStaleRunReconciled() {
	m.StaleRunsReconciled.Inc()
}

func (m *Metrics) SetPortfolioValue(value float64) {
	m.PortfolioValue.Set(value)
}

func (m *Metrics) SetPositionsOpen(count float64) {
	m.PositionsOpen.Set(count)
}

func (m *Metrics) SetCircuitBreakerState(active bool) {
	if active {
		m.CircuitBreakerState.Set(1)
	} else {
		m.CircuitBreakerState.Set(0)
	}
}

func (m *Metrics) SetKillSwitchActive(active bool) {
	if active {
		m.KillSwitchActive.Set(1)
	} else {
		m.KillSwitchActive.Set(0)
	}
}

// RecordLLMRetry increments the retry counter for a given provider.
func (m *Metrics) RecordLLMRetry(provider string) {
	if m == nil {
		return
	}
	m.LLMRetryTotal.WithLabelValues(provider).Inc()
}

// RecordLLMBudgetExhausted increments the budget exhaustion counter.
func (m *Metrics) RecordLLMBudgetExhausted() {
	if m == nil {
		return
	}
	m.LLMBudgetExhaustedTotal.Inc()
}

// RecordReportWorkerSuccess increments the report success counter for a strategy.
func (m *Metrics) RecordReportWorkerSuccess(strategyID string) {
	if m == nil {
		return
	}
	m.ReportWorkerSuccessTotal.WithLabelValues(strategyID).Inc()
}

// RecordReportWorkerError increments the report error counter for a strategy.
func (m *Metrics) RecordReportWorkerError(strategyID string) {
	if m == nil {
		return
	}
	m.ReportWorkerErrorTotal.WithLabelValues(strategyID).Inc()
}

// ObserveReportStaleness records how stale a report is at query time.
func (m *Metrics) ObserveReportStaleness(strategyID string, seconds float64) {
	if m == nil {
		return
	}
	m.ReportStaleness.WithLabelValues(strategyID).Observe(seconds)
}

// Handler returns an http.Handler that serves Prometheus metrics from the
// instance's private registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
