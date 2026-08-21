package simulation

import (
	"math"
	"math/rand"
	"sync"
)

// AdverseModel is part of Phase D backtest realism. It is exposed as a
// standalone primitive; integration into FillEngine.Runner is deferred.
// The model is safe for concurrent use.

// AdverseSelectionConfig controls post-fill adverse motion and ghost fills.
type AdverseSelectionConfig struct {
	// BiasBps is the average adverse mid-move after a passive fill, in bps.
	BiasBps float64
	// Sigma is the stddev around BiasBps, in bps.
	Sigma float64
	// GhostFillRate is the Poisson rate per fill event that the fill is ghosted.
	GhostFillRate float64
}

func DefaultAdverseSelectionConfig() AdverseSelectionConfig {
	return AdverseSelectionConfig{BiasBps: 8.0, Sigma: 4.0, GhostFillRate: 0.01}
}

type AdverseModel struct {
	cfg AdverseSelectionConfig
	mu  sync.Mutex
	rng *rand.Rand
}

func NewAdverseModel(cfg AdverseSelectionConfig, seed int64) *AdverseModel {
	return &AdverseModel{cfg: cfg, rng: rand.New(rand.NewSource(seed))}
}

func (m *AdverseModel) AdverseMove(side FillSide) float64 {
	if m == nil || m.rng == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.cfg
	if cfg.Sigma < 0 {
		cfg.Sigma = -cfg.Sigma
	}
	bps := cfg.BiasBps
	if cfg.Sigma > 0 {
		bps = m.rng.NormFloat64()*cfg.Sigma + cfg.BiasBps
	}
	if side == FillBuy {
		return -math.Abs(bps)
	}
	return math.Abs(bps)
}

func (m *AdverseModel) IsGhost() bool {
	if m == nil || m.rng == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rate := m.cfg.GhostFillRate
	if rate <= 0 {
		return false
	}
	if rate > 20 {
		rate = 20
	}
	return m.rng.Float64() < 1-math.Exp(-rate)
}
