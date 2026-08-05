package risk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type fakeCapitalLadderRepo struct{ e *domain.CapitalLadderEntry }

func (f *fakeCapitalLadderRepo) Upsert(_ context.Context, entry domain.CapitalLadderEntry) error {
	f.e = &entry
	return nil
}

func (f *fakeCapitalLadderRepo) Get(_ context.Context, strategyID string) (*domain.CapitalLadderEntry, error) {
	if f.e == nil || f.e.StrategyID != strategyID {
		return nil, repository.ErrNotFound
	}
	cp := *f.e
	return &cp, nil
}

func (f *fakeCapitalLadderRepo) List(context.Context) ([]domain.CapitalLadderEntry, error) {
	if f.e == nil {
		return nil, nil
	}
	return []domain.CapitalLadderEntry{*f.e}, nil
}

func (f *fakeCapitalLadderRepo) UpdateMetrics(_ context.Context, strategyID string, fillRate, winRate, drawdownPct float64) error {
	if f.e == nil || f.e.StrategyID != strategyID {
		return repository.ErrNotFound
	}
	f.e.FillRate, f.e.WinRate, f.e.DrawdownPct = fillRate, winRate, drawdownPct
	return nil
}

func (f *fakeCapitalLadderRepo) AdvanceStep(_ context.Context, strategyID string, newStep float64, advancedAt time.Time) error {
	if f.e == nil || f.e.StrategyID != strategyID {
		return repository.ErrNotFound
	}
	f.e.StepPct = newStep
	f.e.BaselineFillRate = f.e.FillRate
	f.e.BaselineWinRate = f.e.WinRate
	f.e.AdvancedAt = &advancedAt
	return nil
}

func TestCapitalLadderPromoteAdvances(t *testing.T) {
	repo := &fakeCapitalLadderRepo{e: &domain.CapitalLadderEntry{StrategyID: "s1", StepPct: .1, FillRate: .99, WinRate: .98, BaselineFillRate: .95, BaselineWinRate: .96}}
	cl := NewCapitalLadder(CapitalLadderConfig{}, repo)
	got, err := cl.Promote(context.Background(), "s1", time.Now().UTC())
	if err != nil || got.StepPct != .2 {
		t.Fatalf("got %#v err=%v", got, err)
	}
}

func TestCapitalLadderPromoteRespectsMaxStep(t *testing.T) {
	repo := &fakeCapitalLadderRepo{e: &domain.CapitalLadderEntry{StrategyID: "s1", StepPct: 1.0, FillRate: 1, WinRate: 1, BaselineFillRate: 1, BaselineWinRate: 1}}
	cl := NewCapitalLadder(CapitalLadderConfig{Step: .1, MaxStep: 1.0, Tolerance: .03}, repo)
	_, err := cl.Promote(context.Background(), "s1", time.Now().UTC())
	if !errors.Is(err, ErrLadderAtMax) {
		t.Fatalf("err=%v", err)
	}
}

func TestCapitalLadderPromoteRejectsBelowTolerance(t *testing.T) {
	repo := &fakeCapitalLadderRepo{e: &domain.CapitalLadderEntry{StrategyID: "s1", StepPct: .1, FillRate: .9, WinRate: .9, BaselineFillRate: .95, BaselineWinRate: .95}}
	cl := NewCapitalLadder(CapitalLadderConfig{}, repo)
	_, err := cl.Promote(context.Background(), "s1", time.Now().UTC())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCapitalLadderPromoteRejectsMissing(t *testing.T) {
	cl := NewCapitalLadder(CapitalLadderConfig{}, &fakeCapitalLadderRepo{})
	_, err := cl.Promote(context.Background(), "x", time.Now().UTC())
	if !errors.Is(err, ErrLadderNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestCapitalLadderDefaultsApplied(t *testing.T) {
	cl := NewCapitalLadder(CapitalLadderConfig{}, &fakeCapitalLadderRepo{})
	if cl.cfg.Step != domain.DefaultCapitalLadderStep || cl.cfg.MaxStep != domain.DefaultCapitalLadderMaxStep || cl.cfg.Tolerance != domain.DefaultCapitalLadderTolerance {
		t.Fatalf("cfg=%#v", cl.cfg)
	}
}
