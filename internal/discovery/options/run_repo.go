package options

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
)

// PersistRun stores the sanitized options-discovery configuration and complete
// terminal result in the shared discovery ledger. Runtime providers and metric
// collectors are deliberately excluded.
func PersistRun(ctx context.Context, repo discovery.RunRepository, cfg OptionsDiscoveryConfig, result *OptionsDiscoveryResult, startedAt time.Time) error {
	if repo == nil {
		return fmt.Errorf("options/discovery: run repository is required")
	}
	if result == nil {
		return fmt.Errorf("options/discovery: terminal result is required")
	}
	configJSON, err := json.Marshal(struct {
		Version  int    `json:"version"`
		Kind     string `json:"kind"`
		Screener struct {
			Tickers       []string `json:"tickers"`
			MinPrice      float64  `json:"min_price"`
			MinADV        float64  `json:"min_adv"`
			MinChainWidth int      `json:"min_chain_width"`
			MinOI         float64  `json:"min_oi"`
			TargetDTE     int      `json:"target_dte"`
		} `json:"screener"`
		Generator struct {
			Model      string `json:"model,omitempty"`
			MaxRetries int    `json:"max_retries"`
		} `json:"generator"`
		Scoring      OptionsScoringConfig       `json:"scoring"`
		Backtest     discovery.ScoringConfig    `json:"backtest"`
		Validation   discovery.ValidationConfig `json:"validation"`
		MaxWinners   int                        `json:"max_winners"`
		DryRun       bool                       `json:"dry_run"`
		ScheduleCron string                     `json:"schedule_cron"`
	}{
		Version: 1,
		Kind:    "options",
		Screener: struct {
			Tickers       []string `json:"tickers"`
			MinPrice      float64  `json:"min_price"`
			MinADV        float64  `json:"min_adv"`
			MinChainWidth int      `json:"min_chain_width"`
			MinOI         float64  `json:"min_oi"`
			TargetDTE     int      `json:"target_dte"`
		}{
			Tickers: append([]string(nil), cfg.Screener.Tickers...), MinPrice: cfg.Screener.MinPrice,
			MinADV: cfg.Screener.MinADV, MinChainWidth: cfg.Screener.MinChainWidth,
			MinOI: cfg.Screener.MinOI, TargetDTE: cfg.Screener.TargetDTE,
		},
		Generator: struct {
			Model      string `json:"model,omitempty"`
			MaxRetries int    `json:"max_retries"`
		}{Model: cfg.Generator.Model, MaxRetries: cfg.Generator.MaxRetries},
		Scoring: cfg.Scoring, Backtest: cfg.BacktestCfg, Validation: cfg.Validation,
		MaxWinners: cfg.MaxWinners, DryRun: cfg.DryRun, ScheduleCron: cfg.ScheduleCron,
	})
	if err != nil {
		return fmt.Errorf("options/discovery: marshal run config: %w", err)
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("options/discovery: marshal run result: %w", err)
	}
	if err := repo.Create(ctx, configJSON, resultJSON, startedAt, result.Duration, result.Candidates, result.Deployed); err != nil {
		return fmt.Errorf("options/discovery: persist run: %w", err)
	}
	return nil
}
