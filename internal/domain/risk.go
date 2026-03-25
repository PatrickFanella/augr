package domain

import "time"

// RiskStatus represents the current risk level of the system.
type RiskStatus string

const (
	RiskStatusNormal   RiskStatus = "normal"
	RiskStatusWarning  RiskStatus = "warning"
	RiskStatusBreached RiskStatus = "breached"
)

// String returns the string representation of a RiskStatus.
func (s RiskStatus) String() string {
	return string(s)
}

// CircuitBreakerState represents the state of the circuit breaker.
type CircuitBreakerState string

const (
	CircuitBreakerClosed   CircuitBreakerState = "closed"
	CircuitBreakerOpen     CircuitBreakerState = "open"
	CircuitBreakerHalfOpen CircuitBreakerState = "half_open"
)

// String returns the string representation of a CircuitBreakerState.
func (s CircuitBreakerState) String() string {
	return string(s)
}

// IsValid returns true if the status is a defined RiskStatus constant.
func (s RiskStatus) IsValid() bool {
	switch s {
	case RiskStatusNormal, RiskStatusWarning, RiskStatusBreached:
		return true
	}
	return false
}

// validCircuitBreakerTransitions defines the legal circuit breaker state transitions.
var validCircuitBreakerTransitions = map[CircuitBreakerState][]CircuitBreakerState{
	CircuitBreakerClosed:   {CircuitBreakerOpen},
	CircuitBreakerOpen:     {CircuitBreakerHalfOpen},
	CircuitBreakerHalfOpen: {CircuitBreakerClosed, CircuitBreakerOpen},
}

// IsValid returns true if the state is a defined CircuitBreakerState constant.
func (s CircuitBreakerState) IsValid() bool {
	_, ok := validCircuitBreakerTransitions[s]
	return ok
}

// CanTransitionTo returns true if transitioning from s to next is valid.
func (s CircuitBreakerState) CanTransitionTo(next CircuitBreakerState) bool {
	allowed, ok := validCircuitBreakerTransitions[s]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == next {
			return true
		}
	}
	return false
}

// RiskLimits defines the thresholds used by the risk management system.
type RiskLimits struct {
	MaxPositionSizePct  float64             `json:"max_position_size_pct"`
	MaxDailyLossPct     float64             `json:"max_daily_loss_pct"`
	MaxDrawdownPct      float64             `json:"max_drawdown_pct"`
	MaxOpenPositions    int                 `json:"max_open_positions"`
	MaxOrderValueUSD    float64             `json:"max_order_value_usd"`
	CircuitBreakerState CircuitBreakerState `json:"circuit_breaker_state"`
	UpdatedAt           time.Time           `json:"updated_at"`
}
