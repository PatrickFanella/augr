package prediction

import (
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ExecutableSnapshot captures the shared executable prediction-market contract
// proven by both Polymarket and Kalshi.
type ExecutableSnapshot interface {
	ValidateExecutableSide(side string, minLiquidity float64, now time.Time) error
	EntryPriceForSide(side string) (float64, bool)
	SpreadForSide(side string) (float64, bool)
}

// NativeDecision is the shared prediction-market execution decision shape.
type NativeDecision struct {
	Signal          domain.PipelineSignal `json:"signal"`
	Action          string                `json:"action,omitempty"`
	Side            string                `json:"side,omitempty"`
	EntryType       string                `json:"entry_type,omitempty"`
	EntryPrice      float64               `json:"entry_price,omitempty"`
	StopLoss        float64               `json:"stop_loss,omitempty"`
	TakeProfit      float64               `json:"take_profit,omitempty"`
	Confidence      float64               `json:"confidence,omitempty"`
	TimeHorizon     string                `json:"time_horizon,omitempty"`
	Reason          string                `json:"reason,omitempty"`
	Rationale       string                `json:"rationale,omitempty"`
	RiskReward      float64               `json:"risk_reward,omitempty"`
	MaxEntryPrice   float64               `json:"max_entry_price,omitempty"`
	FairProbability float64               `json:"fair_probability,omitempty"`
	Spread          float64               `json:"spread,omitempty"`
	Depth           float64               `json:"depth,omitempty"`
	GrossEdge       float64               `json:"gross_edge,omitempty"`
	NetEdge         float64               `json:"net_edge,omitempty"`
	Template        string                `json:"template,omitempty"`
	EvidenceSources []string              `json:"evidence_sources,omitempty"`
	Calibration     string                `json:"calibration,omitempty"`
	GateResults     []string              `json:"gate_results,omitempty"`
	PositionSize    float64               `json:"position_size,omitempty"`
	AverageEntry    float64               `json:"average_entry,omitempty"`
	RealizedPnLPct  float64               `json:"realized_pnl_pct,omitempty"`
}

// NormalizeOutcomeSide maps arbitrary side strings to the canonical YES/NO
// values used by prediction-market providers.
func NormalizeOutcomeSide(side string) string {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "YES":
		return "YES"
	case "NO":
		return "NO"
	default:
		return ""
	}
}
