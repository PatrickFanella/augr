package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OpportunityStatus represents the lifecycle state of a portfolio opportunity.
type OpportunityStatus string

const (
	OpportunityStatusQueued   OpportunityStatus = "queued"
	OpportunityStatusSelected OpportunityStatus = "selected"
	OpportunityStatusRejected OpportunityStatus = "rejected"
	OpportunityStatusExpired  OpportunityStatus = "expired"
	OpportunityStatusExecuted OpportunityStatus = "executed"
)

// String returns the string representation of an OpportunityStatus.
func (s OpportunityStatus) String() string { return string(s) }

// Opportunity represents a persisted candidate for portfolio allocation.
type Opportunity struct {
	ID                uuid.UUID         `json:"id"`
	StrategyID        uuid.UUID         `json:"strategy_id"`
	PipelineRunID     *uuid.UUID        `json:"pipeline_run_id,omitempty"`
	MarketType        MarketType        `json:"market_type"`
	Ticker            string            `json:"ticker"`
	Side              OrderSide         `json:"side"`
	Signal            PipelineSignal    `json:"signal"`
	Status            OpportunityStatus `json:"status"`
	Score             *float64          `json:"score,omitempty"`
	Confidence        float64           `json:"confidence"`
	EdgePct           float64           `json:"edge_pct"`
	ExpectedReturnPct float64           `json:"expected_return_pct"`
	MaxLossPct        float64           `json:"max_loss_pct"`
	LiquidityUSD      float64           `json:"liquidity_usd"`
	MarketCapUSD      float64           `json:"market_cap_usd"`
	SpreadPct         float64           `json:"spread_pct"`
	ProposedNotional  float64           `json:"proposed_notional"`
	SelectedNotional  float64           `json:"selected_notional"`
	Reason            string            `json:"reason"`
	RejectReason      string            `json:"reject_reason"`
	Evidence          json.RawMessage   `json:"evidence,omitempty"`
	ExpiresAt         time.Time         `json:"expires_at"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	DedupeKey         string            `json:"dedupe_key"`
}
