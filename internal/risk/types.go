package risk

import (
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// EngineStatus describes the current aggregate status of the risk system.
type EngineStatus struct {
	RiskStatus         domain.RiskStatus                      `json:"risk_status"`
	CircuitBreaker     CircuitBreakerStatus                   `json:"circuit_breaker"`
	KillSwitch         KillSwitchStatus                       `json:"kill_switch"`
	MarketKillSwitches map[domain.MarketType]KillSwitchStatus `json:"market_kill_switches,omitempty"`
	PositionLimits     PositionLimits                         `json:"position_limits"`
	UpdatedAt          time.Time                              `json:"updated_at"`
}

// CircuitBreakerPhase defines whether trading is allowed or temporarily halted.
type CircuitBreakerPhase string

const (
	CircuitBreakerPhaseOpen     CircuitBreakerPhase = "open"
	CircuitBreakerPhaseTripped  CircuitBreakerPhase = "tripped"
	CircuitBreakerPhaseCooldown CircuitBreakerPhase = "cooldown"
)

// String returns the string representation of a CircuitBreakerPhase.
func (s CircuitBreakerPhase) String() string {
	return string(s)
}

// CircuitBreakerStatus captures circuit breaker state and latest transition.
type CircuitBreakerStatus struct {
	State       CircuitBreakerPhase `json:"state"`
	Reason      string              `json:"reason,omitempty"`
	TrippedAt   *time.Time          `json:"tripped_at,omitempty"`
	CooldownEnd *time.Time          `json:"cooldown_end,omitempty"`
}

// KillSwitchMechanism identifies the source used to activate the kill switch.
type KillSwitchMechanism string

const (
	KillSwitchMechanismAPI     KillSwitchMechanism = "api_toggle"
	KillSwitchMechanismFile    KillSwitchMechanism = "file_flag"
	KillSwitchMechanismEnvVar  KillSwitchMechanism = "env_var"
	KillSwitchMechanismUnknown KillSwitchMechanism = "unknown"
)

// String returns the string representation of a KillSwitchMechanism.
func (m KillSwitchMechanism) String() string {
	return string(m)
}

// KillSwitchStatus captures whether the kill switch is active and why.
type KillSwitchStatus struct {
	Active      bool                  `json:"active"`
	Reason      string                `json:"reason,omitempty"`
	Mechanisms  []KillSwitchMechanism `json:"mechanisms,omitempty"`
	ActivatedAt *time.Time            `json:"activated_at,omitempty"`
}

// CircuitBreakerConfig holds thresholds and timing for the circuit breaker.
type CircuitBreakerConfig struct {
	MaxDailyLossPct      float64       // Trip when daily loss exceeds this (e.g. 0.03 = 3%).
	MaxDrawdownPct       float64       // Trip when total drawdown exceeds this (e.g. 0.10 = 10%).
	MaxConsecutiveLosses int           // Trip when consecutive losses exceed this count.
	CooldownDuration     time.Duration // After tripping, auto-reset after this duration.
}

// DefaultCircuitBreakerConfig returns the default circuit breaker configuration.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxDailyLossPct:      0.03,
		MaxDrawdownPct:       0.10,
		MaxConsecutiveLosses: 5,
		CooldownDuration:     15 * time.Minute,
	}
}

// PositionLimits defines portfolio-level and per-market exposure constraints.
type PositionLimits struct {
	MaxPerPositionPct       float64  `json:"max_per_position_pct"`
	MaxTotalPct             float64  `json:"max_total_pct"`
	MaxConcurrent           int      `json:"max_concurrent"`
	MaxPerMarketPct         float64  `json:"max_per_market_pct"`
	CurrentOpenPositions    *int     `json:"current_open_positions,omitempty"`
	CurrentTotalExposurePct *float64 `json:"current_total_exposure_pct,omitempty"`
}

// Portfolio carries the current exposure context used for risk checks.
type Portfolio struct {
	TotalExposurePct         float64                       `json:"total_exposure_pct"`
	ConcurrentPositions      int                           `json:"concurrent_positions"`
	PositionExposureBySymbol map[string]float64            `json:"position_exposure_by_symbol,omitempty"`
	MarketExposurePct        map[domain.MarketType]float64 `json:"market_exposure_pct,omitempty"`
}

// PolymarketLimits holds prediction-market-specific risk parameters that
// supplement the standard position limits.
type PolymarketLimits struct {
	MaxSingleMarketExposurePct float64 // fraction of portfolio in one market (default: 0.05)
	MaxTotalExposurePct        float64 // fraction across all polymarket positions (default: 0.30)
	MaxPositionUSDC            float64 // hard USD cap per position; 0 disables
	MinLiquidity               float64 // minimum market liquidity in USDC
	MaxSpreadPct               float64 // max bid-ask spread as fraction of mid price
	MinDaysToResolution        int     // skip markets resolving in fewer days
}

// DefaultPolymarketLimits returns conservative defaults for polymarket risk limits.
func DefaultPolymarketLimits() PolymarketLimits {
	return PolymarketLimits{
		MaxSingleMarketExposurePct: 0.05,
		MaxTotalExposurePct:        0.30,
		MaxPositionUSDC:            0,
		MinLiquidity:               1000,
		MaxSpreadPct:               0.10,
		MinDaysToResolution:        1,
	}
}
