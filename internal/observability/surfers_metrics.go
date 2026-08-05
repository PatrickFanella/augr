package observability

import (
	"math"
	"strconv"
	"sync/atomic"

	polymarketmd "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/recorder"
	"github.com/prometheus/client_golang/prometheus"
)

type SurfersMetrics struct {
	wsConnections    *prometheus.GaugeVec
	wsJitterMS       *prometheus.GaugeVec
	wsTicksDropped   *prometheus.CounterVec
	recorderInserted *prometheus.CounterVec
	recorderLag      prometheus.Gauge
	breakerTripped   *prometheus.CounterVec
	fillRate         *prometheus.GaugeVec
	ghostFills       *prometheus.CounterVec
	recorderLagValue atomic.Uint64
}

func NewSurfersMetrics(reg prometheus.Registerer) *SurfersMetrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &SurfersMetrics{
		wsConnections:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "polymarket_ws_connections", Help: "Polymarket websocket connections by state."}, []string{"slug", "state"}),
		wsJitterMS:       prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "polymarket_ws_jitter_ms", Help: "Polymarket websocket jitter in milliseconds."}, []string{"slug"}),
		wsTicksDropped:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "polymarket_ws_ticks_dropped_total", Help: "Dropped Polymarket websocket ticks."}, []string{"slug", "reason"}),
		recorderInserted: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "polymarket_recorder_inserted_total", Help: "Inserted Polymarket records."}, []string{"kind"}),
		recorderLag:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "polymarket_recorder_lag_seconds", Help: "Polymarket recorder lag in seconds."}),
		breakerTripped:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "risk_breaker_tripped_total", Help: "Risk breaker trips."}, []string{"scope", "reason"}),
		fillRate:         prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "polymarket_fill_rate", Help: "Polymarket fill rate by strategy."}, []string{"strategy"}),
		ghostFills:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "polymarket_ghost_fill_total", Help: "Polymarket ghost fills by strategy."}, []string{"strategy"}),
	}
	reg.MustRegister(m.wsConnections, m.wsJitterMS, m.wsTicksDropped, m.recorderInserted, m.recorderLag, m.breakerTripped, m.fillRate, m.ghostFills)
	return m
}

func (m *SurfersMetrics) FeedMetrics() polymarketmd.Metrics { return feedMetricsAdapter{m: m} }
func (m *SurfersMetrics) RecorderMetrics() recorder.RecorderMetrics {
	return recorderMetricsAdapter{m: m}
}

func (m *SurfersMetrics) IncBreakerTripped(scope, reason string) {
	m.breakerTripped.WithLabelValues(scope, reason).Inc()
}

func (m *SurfersMetrics) SetFillRate(strategy string, rate float64) {
	m.fillRate.WithLabelValues(strategy).Set(rate)
}
func (m *SurfersMetrics) IncGhostFill(strategy string) { m.ghostFills.WithLabelValues(strategy).Inc() }
func (m *SurfersMetrics) LastRecorderLag() float64 {
	return math.Float64frombits(m.recorderLagValue.Load())
}

type feedMetricsAdapter struct{ m *SurfersMetrics }

func (a feedMetricsAdapter) IncCounter(name string, labels map[string]string) {
	switch name {
	case "polymarket_ws_connections":
		a.m.wsConnections.WithLabelValues(labels["slug"], labels["state"]).Inc()
	case "polymarket_ws_ticks_dropped_total":
		a.m.wsTicksDropped.WithLabelValues(labels["slug"], labels["reason"]).Inc()
	case "polymarket_ws_jitter_ms":
		if v := labels["value"]; v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				a.m.wsJitterMS.WithLabelValues(labels["slug"]).Set(f)
			}
		}
	}
}

type recorderMetricsAdapter struct{ m *SurfersMetrics }

func (a recorderMetricsAdapter) IncInserted(kind string, n int) {
	a.m.recorderInserted.WithLabelValues(kind).Add(float64(n))
}

func (a recorderMetricsAdapter) ObserveLagSeconds(_ string, sec float64) {
	a.m.recorderLag.Set(sec)
	a.m.recorderLagValue.Store(math.Float64bits(sec))
}

func (a recorderMetricsAdapter) IncDropped(kind string, n int) {
	a.m.wsTicksDropped.WithLabelValues(kind, "backpressure").Add(float64(n))
}
