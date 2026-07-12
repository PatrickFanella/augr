package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// BacktestModelConfig stores flexible JSON for simulation components.
type BacktestModelConfig = json.RawMessage

// BacktestLatestRunSummary captures the most recent run surfaced alongside
// backtest config list responses.
type BacktestLatestRunSummary struct {
	ID               uuid.UUID       `json:"id"`
	BacktestConfigID uuid.UUID       `json:"backtest_config_id"`
	Metrics          json.RawMessage `json:"metrics"`
	RunTimestamp     time.Time       `json:"run_timestamp"`
}

// BacktestSimulationParameters captures reusable simulation settings for a backtest.
type BacktestSimulationParameters struct {
	InitialCapital   float64             `json:"initial_capital"`
	SlippageModel    BacktestModelConfig `json:"slippage_model,omitempty"`
	TransactionCosts BacktestModelConfig `json:"transaction_costs,omitempty"`
	SpreadModel      BacktestModelConfig `json:"spread_model,omitempty"`
	OptionsFillModel BacktestModelConfig `json:"options_fill_model,omitempty"`
	MaxVolumePct     float64             `json:"max_volume_pct,omitempty"` // 0 means no volume limit
	TrailingStopPct  float64             `json:"trailing_stop_pct,omitempty"`
}

// BacktestConfig represents a reusable backtest definition.
type BacktestConfig struct {
	ID               uuid.UUID                    `json:"id"`
	StrategyID       uuid.UUID                    `json:"strategy_id"`
	Name             string                       `json:"name"`
	Description      string                       `json:"description,omitempty"`
	ScheduleCron     string                       `json:"schedule_cron,omitempty"`
	StartDate        time.Time                    `json:"start_date"`
	EndDate          time.Time                    `json:"end_date"`
	Simulation       BacktestSimulationParameters `json:"simulation"`
	CreatedAt        time.Time                    `json:"created_at"`
	UpdatedAt        time.Time                    `json:"updated_at"`
	LatestRunSummary *BacktestLatestRunSummary    `json:"latest_run_summary,omitempty"`
}

// Validate checks that the backtest configuration has valid required fields.
func (c *BacktestConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("backtest config is required")
	}
	if c.StrategyID == uuid.Nil {
		return fmt.Errorf("strategy_id is required")
	}
	if err := requireNonEmpty("name", c.Name); err != nil {
		return err
	}
	if c.StartDate.IsZero() {
		return fmt.Errorf("start_date is required")
	}
	if c.EndDate.IsZero() {
		return fmt.Errorf("end_date is required")
	}
	if c.EndDate.Before(c.StartDate) {
		return fmt.Errorf("end_date must be on or after start_date")
	}
	if err := requirePositive("initial_capital", c.Simulation.InitialCapital); err != nil {
		return err
	}
	if c.Simulation.MaxVolumePct < 0 || c.Simulation.MaxVolumePct > 1 {
		return fmt.Errorf("max_volume_pct must be between 0 and 1, got %v", c.Simulation.MaxVolumePct)
	}
	if c.Simulation.TrailingStopPct < 0 || c.Simulation.TrailingStopPct >= 100 {
		return fmt.Errorf("trailing_stop_pct must be between 0 (inclusive) and 100 (exclusive), got %v", c.Simulation.TrailingStopPct)
	}
	return nil
}
