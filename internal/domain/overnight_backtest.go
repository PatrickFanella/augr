package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	OvernightBacktestStatusRunning   = "running"
	OvernightBacktestStatusCompleted = "completed"
	OvernightBacktestStatusFailed    = "failed"

	OvernightBacktestPhaseScreen              = "screen"
	OvernightBacktestPhaseGenerate            = "generate"
	OvernightBacktestPhaseSweepValidateDeploy = "sweep_validate_deploy"
	OvernightBacktestPhaseDone                = "done"
)

type OvernightBacktestCandidate struct {
	Ticker     string      `json:"ticker"`
	Bars       []OHLCV     `json:"bars"`
	Indicators []Indicator `json:"indicators"`
	Close      float64     `json:"close"`
	ADV        float64     `json:"adv"`
	ATR        float64     `json:"atr"`
}

type OvernightBacktestGenerated struct {
	Ticker   string          `json:"ticker"`
	Config   json.RawMessage `json:"config"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}

type OvernightBacktestSummary struct {
	Candidates int `json:"candidates,omitempty"`
	Generated  int `json:"generated,omitempty"`
	Swept      int `json:"swept,omitempty"`
	Validated  int `json:"validated,omitempty"`
	Deployed   int `json:"deployed,omitempty"`
	Created    int `json:"created,omitempty"`
	Reused     int `json:"reused,omitempty"`
}

type OvernightBacktestRun struct {
	ID             uuid.UUID                    `json:"id"`
	Status         string                       `json:"status"`
	Phase          string                       `json:"phase"`
	CandidateIndex int                          `json:"candidate_index"`
	Candidates     []OvernightBacktestCandidate `json:"candidates"`
	Generated      []OvernightBacktestGenerated `json:"generated"`
	Errors         []string                     `json:"errors"`
	Summary        OvernightBacktestSummary     `json:"summary"`
	StartedAt      time.Time                    `json:"started_at"`
	UpdatedAt      time.Time                    `json:"updated_at"`
	CompletedAt    *time.Time                   `json:"completed_at,omitempty"`
}

func NewOvernightBacktestRun() OvernightBacktestRun {
	return OvernightBacktestRun{
		ID:     uuid.New(),
		Status: OvernightBacktestStatusRunning,
		Phase:  OvernightBacktestPhaseScreen,
	}
}
