package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// BacktestRun stores the persisted results of a single executed backtest.
type BacktestRun struct {
	ID                uuid.UUID       `json:"id"`
	BacktestConfigID  uuid.UUID       `json:"backtest_config_id"`
	Metrics           json.RawMessage `json:"metrics"`
	TradeLog          json.RawMessage `json:"trade_log"`
	EquityCurve       json.RawMessage `json:"equity_curve"`
	RunTimestamp      time.Time       `json:"run_timestamp"`
	Duration          time.Duration   `json:"duration"`
	PromptVersion     string          `json:"prompt_version"`
	PromptVersionHash string          `json:"prompt_version_hash"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// Validate checks that the persisted backtest run has all required metadata and results.
func (r *BacktestRun) Validate() error {
	if r == nil {
		return fmt.Errorf("backtest run is required")
	}
	if r.BacktestConfigID == uuid.Nil {
		return fmt.Errorf("backtest_config_id is required")
	}
	if err := validateRequiredJSON("metrics", r.Metrics); err != nil {
		return err
	}
	if err := validateRequiredJSON("trade_log", r.TradeLog); err != nil {
		return err
	}
	if err := validateRequiredJSON("equity_curve", r.EquityCurve); err != nil {
		return err
	}
	if r.RunTimestamp.IsZero() {
		return fmt.Errorf("run_timestamp is required")
	}
	if r.Duration < 0 {
		return fmt.Errorf("duration must be non-negative")
	}
	if err := requireNonEmpty("prompt_version", r.PromptVersion); err != nil {
		return err
	}
	if err := requireNonEmpty("prompt_version_hash", r.PromptVersionHash); err != nil {
		return err
	}
	return nil
}

func validateRequiredJSON(field string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%s is required", field)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("%s must be valid JSON", field)
	}
	return nil
}
