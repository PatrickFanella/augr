package agent

import "time"

// PredictionMarketData holds Polymarket-specific market state threaded through
// the pipeline alongside standard market data. It is fetched via the Polymarket
// CLOB client and cached on the AnalysisInput / PipelineState for the duration
// of a single pipeline run.
type PredictionMarketData struct {
	// Market identity
	Slug               string // market slug (= strategy ticker)
	Question           string // "Will X happen by Y date?"
	Description        string // full market description
	ResolutionCriteria string // how the market resolves

	// Resolution
	EndDate          *time.Time // when the market closes
	ResolutionSource string     // who/what resolves it

	// Current state
	YesPrice     float64 // current YES token price (0–1)
	NoPrice      float64 // current NO token price (0–1)
	Volume24h    float64 // 24h volume in USDC
	Liquidity    float64 // total liquidity in USDC
	OpenInterest float64 // total open interest

	// Token resolution (cached for execution)
	ConditionID string
	YesTokenID  string
	NoTokenID   string

	// Order book snapshot
	BestBidYes float64
	BestAskYes float64
	BestBidNo  float64
	BestAskNo  float64
	SpreadYes  float64 // ask − bid for YES token
}
