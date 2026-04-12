package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus instruments for the trading agent.
type Metrics struct {
	registry                 *prometheus.Registry
	PipelineRunsTotal        *prometheus.CounterVec
	PipelineDuration         *prometheus.HistogramVec
	LLMCallsTotal            *prometheus.CounterVec
	LLMFallbackTotal         *prometheus.CounterVec
	LLMTokensTotal           *prometheus.CounterVec
	LLMLatency               *prometheus.HistogramVec
	LLMCacheHitsTotal        prometheus.Counter
	LLMCacheMissesTotal      prometheus.Counter
	OrdersTotal              *prometheus.CounterVec
	SignalParseFailuresTotal prometheus.Counter
	SchedulerTickTotal       *prometheus.CounterVec
	AutomationJobErrorsTotal *prometheus.CounterVec
	StaleRunsReconciled      prometheus.Counter
	PortfolioValue           prometheus.Gauge
	PositionsOpen            prometheus.Gauge
	CircuitBreakerState      prometheus.Gauge
	KillSwitchActive         prometheus.Gauge
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

		SchedulerTickTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_scheduler_tick_total",
			Help: "Total scheduler ticks by type.",
		}, []string{"type"}),

		AutomationJobErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_automation_job_errors_total",
			Help: "Total automation job errors by job name.",
		}, []string{"job_name"}),

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
		m.SchedulerTickTotal,
		m.AutomationJobErrorsTotal,
		m.StaleRunsReconciled,
		m.PortfolioValue,
		m.PositionsOpen,
		m.CircuitBreakerState,
		m.KillSwitchActive,
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

func (m *Metrics) RecordSchedulerTick(tickType string) {
	m.SchedulerTickTotal.WithLabelValues(tickType).Inc()
}

func (m *Metrics) RecordAutomationJobError(jobName string) {
	m.AutomationJobErrorsTotal.WithLabelValues(jobName).Inc()
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

// Handler returns an http.Handler that serves Prometheus metrics from the
// instance's private registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
