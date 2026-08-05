package risk

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

var (
	ErrLadderNotFound = errors.New("capital ladder not found")
	ErrLadderAtMax    = errors.New("capital ladder already at max step")
)

type (
	CapitalLadderConfig struct{ Step, MaxStep, Tolerance float64 }
	CapitalLadder       struct {
		cfg  CapitalLadderConfig
		repo repository.CapitalLadderRepository
	}
)

func NewCapitalLadder(cfg CapitalLadderConfig, repo repository.CapitalLadderRepository) *CapitalLadder {
	if cfg.Step == 0 {
		cfg.Step = domain.DefaultCapitalLadderStep
	}
	if cfg.MaxStep == 0 {
		cfg.MaxStep = domain.DefaultCapitalLadderMaxStep
	}
	if cfg.Tolerance == 0 {
		cfg.Tolerance = domain.DefaultCapitalLadderTolerance
	}
	return &CapitalLadder{cfg: cfg, repo: repo}
}

func (c *CapitalLadder) Status(ctx context.Context, strategyID string) (*domain.CapitalLadderEntry, error) {
	entry, err := c.repo.Get(ctx, strategyID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrLadderNotFound
		}
		return nil, err
	}
	return entry, nil
}

func (c *CapitalLadder) Promote(ctx context.Context, strategyID string, now time.Time) (*domain.CapitalLadderEntry, error) {
	entry, err := c.repo.Get(ctx, strategyID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrLadderNotFound
		}
		return nil, err
	}
	if entry.FillRate < entry.BaselineFillRate*(1-c.cfg.Tolerance) || entry.WinRate < entry.BaselineWinRate*(1-c.cfg.Tolerance) {
		return nil, fmt.Errorf("capital ladder below tolerance")
	}
	newStep := entry.StepPct + c.cfg.Step
	if newStep > c.cfg.MaxStep {
		newStep = c.cfg.MaxStep
	}
	if newStep == entry.StepPct {
		return nil, ErrLadderAtMax
	}
	if err := c.repo.AdvanceStep(ctx, strategyID, newStep, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrLadderNotFound
		}
		return nil, err
	}
	return c.repo.Get(ctx, strategyID)
}

func (c *CapitalLadder) RecordMetrics(ctx context.Context, strategyID string, fillRate, winRate, drawdownPct float64) error {
	return c.repo.UpdateMetrics(ctx, strategyID, fillRate, winRate, drawdownPct)
}
