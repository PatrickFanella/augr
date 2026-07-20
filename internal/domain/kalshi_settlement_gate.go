package domain

import "time"

// KalshiSettlementGateState tracks durable dry-run eligibility for Kalshi settlement.
type KalshiSettlementGateState struct {
	JobName               string     `json:"job_name"`
	ConsecutiveSuccesses  int        `json:"consecutive_successes"`
	Threshold             int        `json:"threshold"`
	Eligible              bool       `json:"eligible"`
	ProjectionFingerprint string     `json:"projection_fingerprint,omitempty"`
	LastOutcome           string     `json:"last_outcome,omitempty"`
	LastError             string     `json:"last_error,omitempty"`
	Fetched               int        `json:"fetched"`
	Resolved              int        `json:"resolved"`
	WouldSettleMarkets    int        `json:"would_settle_markets"`
	WouldSettleDecisions  int        `json:"would_settle_decisions"`
	LastRunAt             *time.Time `json:"last_run_at,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
}
