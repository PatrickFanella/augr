package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus instruments for the trading agent.
type Metrics struct {
	PipelineRunsTotal   *prometheus.CounterVec
	PipelineDuration    *prometheus.HistogramVec
	LLMCallsTotal       *prometheus.CounterVec
	LLMTokensTotal      *prometheus.CounterVec
	LLMLatency          *prometheus.HistogramVec
	OrdersTotal         *prometheus.CounterVec
	PortfolioValue      prometheus.Gauge
	PositionsOpen       prometheus.Gauge
	CircuitBreakerState prometheus.Gauge
	KillSwitchActive    prometheus.Gauge
}

// New registers all Prometheus metrics and returns a ready-to-use Metrics instance.
func New() *Metrics {
	return &Metrics{
		PipelineRunsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_pipeline_runs_total",
			Help: "Total number of pipeline runs by ticker, signal, and status.",
		}, []string{"ticker", "signal", "status"}),

		PipelineDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tradingagent_pipeline_duration_seconds",
			Help:    "Pipeline run duration in seconds by ticker.",
			Buckets: prometheus.DefBuckets,
		}, []string{"ticker"}),

		LLMCallsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_llm_calls_total",
			Help: "Total LLM API calls by provider, model, and agent role.",
		}, []string{"provider", "model", "agent_role"}),

		LLMTokensTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_llm_tokens_total",
			Help: "Total LLM tokens consumed by type (prompt or completion).",
		}, []string{"type"}),

		LLMLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tradingagent_llm_latency_seconds",
			Help:    "LLM call latency in seconds by provider and model.",
			Buckets: prometheus.DefBuckets,
		}, []string{"provider", "model"}),

		OrdersTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "tradingagent_orders_total",
			Help: "Total orders by broker, side, and status.",
		}, []string{"broker", "side", "status"}),

		PortfolioValue: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_portfolio_value",
			Help: "Current portfolio value.",
		}),

		PositionsOpen: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_positions_open",
			Help: "Number of currently open positions.",
		}),

		CircuitBreakerState: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_circuit_breaker_state",
			Help: "Circuit breaker state: 1 = active, 0 = inactive.",
		}),

		KillSwitchActive: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tradingagent_kill_switch_active",
			Help: "Kill switch state: 1 = active, 0 = inactive.",
		}),
	}
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

func (m *Metrics) RecordLLMTokens(promptTokens, completionTokens int) {
	m.LLMTokensTotal.WithLabelValues("prompt").Add(float64(promptTokens))
	m.LLMTokensTotal.WithLabelValues("completion").Add(float64(completionTokens))
}

func (m *Metrics) ObserveLLMLatency(provider, model string, seconds float64) {
	m.LLMLatency.WithLabelValues(provider, model).Observe(seconds)
}

func (m *Metrics) RecordOrder(broker, side, status string) {
	m.OrdersTotal.WithLabelValues(broker, side, status).Inc()
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

// Handler returns an http.Handler that serves Prometheus metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}
