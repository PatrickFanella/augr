package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DiscoveryRun is a persisted record of a discovery pipeline execution.
type DiscoveryRun struct {
	ID          uuid.UUID       `json:"id"`
	Config      json.RawMessage `json:"config"`
	Result      json.RawMessage `json:"result"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	DurationNS  int64           `json:"duration_ns"`
	Candidates  int             `json:"candidates"`
	Deployed    int             `json:"deployed"`
	CreatedAt   time.Time       `json:"created_at"`
}

// RunRepository persists discovery run records.
type RunRepository interface {
	Create(ctx context.Context, config, result json.RawMessage, startedAt time.Time, duration time.Duration, candidates, deployed int) error
	List(ctx context.Context, limit, offset int) ([]DiscoveryRun, error)
	Count(ctx context.Context) (int, error)
}

// PersistRun stores the auditable configuration and complete terminal result
// for a discovery execution. Runtime-only provider and metrics objects are
// deliberately excluded from the configuration payload.
func PersistRun(ctx context.Context, repo RunRepository, cfg DiscoveryConfig, result *DiscoveryResult, startedAt time.Time) error {
	if repo == nil {
		return fmt.Errorf("discovery: run repository is required")
	}
	if result == nil {
		return fmt.Errorf("discovery: terminal result is required")
	}
	configJSON, err := json.Marshal(struct {
		Version   int            `json:"version"`
		Screener  ScreenerConfig `json:"screener"`
		Generator struct {
			Model      string `json:"model,omitempty"`
			MaxRetries int    `json:"max_retries"`
		} `json:"generator"`
		Sweep struct {
			InitialCash float64 `json:"initial_cash"`
			Variations  int     `json:"variations"`
		} `json:"sweep"`
		Scoring      ScoringConfig    `json:"scoring"`
		Validation   ValidationConfig `json:"validation"`
		MaxWinners   int              `json:"max_winners"`
		DryRun       bool             `json:"dry_run"`
		ScheduleCron string           `json:"schedule_cron"`
	}{
		Version:  1,
		Screener: cfg.Screener,
		Generator: struct {
			Model      string `json:"model,omitempty"`
			MaxRetries int    `json:"max_retries"`
		}{Model: cfg.Generator.Model, MaxRetries: cfg.Generator.MaxRetries},
		Sweep: struct {
			InitialCash float64 `json:"initial_cash"`
			Variations  int     `json:"variations"`
		}{InitialCash: cfg.Sweep.InitialCash, Variations: cfg.Sweep.Variations},
		Scoring:      cfg.Scoring,
		Validation:   cfg.Validation,
		MaxWinners:   cfg.MaxWinners,
		DryRun:       cfg.DryRun,
		ScheduleCron: cfg.ScheduleCron,
	})
	if err != nil {
		return fmt.Errorf("discovery: marshal run config: %w", err)
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("discovery: marshal run result: %w", err)
	}
	if err := repo.Create(ctx, configJSON, resultJSON, startedAt, result.Duration, result.Candidates, result.Deployed); err != nil {
		return fmt.Errorf("discovery: persist run: %w", err)
	}
	return nil
}
