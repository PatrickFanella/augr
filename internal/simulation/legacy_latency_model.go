package simulation

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// LatencyModel is part of Phase D backtest realism. It is exposed as a
// standalone primitive; integration into FillEngine.Runner is deferred.
// The model is safe for concurrent use.
type LatencyModel struct {
	Mu    float64 // log-space mean of latency in seconds
	Sigma float64 // log-space stddev
	mu    sync.Mutex
	rng   *rand.Rand
}

func NewLatencyModel(mu, sigma float64, seed int64) *LatencyModel {
	return &LatencyModel{Mu: mu, Sigma: sigma, rng: rand.New(rand.NewSource(seed))}
}

func DefaultLatencyModel(seed int64) *LatencyModel {
	return NewLatencyModel(math.Log(0.040), 0.5, seed)
}

// Sample returns one latency draw as a time.Duration.
func (m *LatencyModel) Sample() time.Duration {
	if m == nil || m.rng == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sec := math.Exp(m.Mu + m.Sigma*m.rng.NormFloat64())
	if sec <= 0 {
		return time.Nanosecond
	}
	d := time.Duration(sec * float64(time.Second))
	if d <= 0 {
		return time.Nanosecond
	}
	return d
}

// SampleAt advances signalTime by Sample() and returns the effective arrival time.
func (m *LatencyModel) SampleAt(signalTime time.Time) time.Time {
	return signalTime.Add(m.Sample())
}
