package agent

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Thesis is the durable, LLM-generated investment thesis produced by the trader
// phase. It bridges the slow LLM pipeline and fast signal-triggered execution:
// the signal intelligence layer can execute a standing thesis without re-running
// the full LLM pipeline.
//
// One thesis is active per strategy at a time. A new pipeline run supersedes
// the previous thesis.
type Thesis struct {
	// Rules holds a serialised RulesEngineConfig for deterministic execution.
	// Stored as raw JSON to avoid a circular import with the rules subpackage.
	Rules json.RawMessage `json:"rules,omitempty"`

	// WatchTerms is the LLM-generated set of keywords the signal intelligence
	// WatchIndex should monitor to detect events relevant to this thesis.
	WatchTerms []string `json:"watch_terms,omitempty"`

	// Human-readable summary for UI display and LLM context in signal evaluation.
	Summary string `json:"summary,omitempty"`

	// Conviction is the LLM's confidence in this thesis, 0–1.
	Conviction float64 `json:"conviction,omitempty"`

	// Direction is the intended trade direction: "buy", "sell", "YES", or "NO".
	Direction string `json:"direction,omitempty"`

	// TimeHorizon is a natural language label: "hours", "days", or "weeks".
	TimeHorizon string `json:"time_horizon,omitempty"`

	// InvalidAfter is an optional hard expiry. The signal evaluator will not
	// execute a thesis whose InvalidAfter has passed.
	InvalidAfter *time.Time `json:"invalid_after,omitempty"`

	// InvalidateIf lists natural language conditions under which the thesis is
	// no longer valid. Evaluated by the signal evaluator LLM when triggers fire.
	InvalidateIf []string `json:"invalidate_if,omitempty"`

	// Metadata
	GeneratedAt   time.Time `json:"generated_at"`
	PipelineRunID uuid.UUID `json:"pipeline_run_id"`
}

// IsExpired reports whether the thesis has passed its hard expiry.
func (t *Thesis) IsExpired() bool {
	if t == nil || t.InvalidAfter == nil {
		return false
	}
	return time.Now().After(*t.InvalidAfter)
}
