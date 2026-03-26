package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type stubRunRepo struct {
	created *domain.BacktestRun
	err     error
}

func (s *stubRunRepo) Create(ctx context.Context, run *domain.BacktestRun) error {
	s.created = run
	return s.err
}

func (s *stubRunRepo) Get(ctx context.Context, id uuid.UUID) (*domain.BacktestRun, error) {
	return nil, nil
}

func (s *stubRunRepo) List(ctx context.Context, filter repository.BacktestRunFilter, limit, offset int) ([]domain.BacktestRun, error) {
	return nil, nil
}

func TestRepoPersister_PersistRun_Success(t *testing.T) {
	repo := &stubRunRepo{}
	p := NewRepoPersister(repo)

	configID := uuid.New()
	triggeredAt := time.Date(2026, 3, 26, 10, 0, 0, 0, time.UTC)
	duration := 5 * time.Minute

	result := &OrchestratorResult{
		Metrics:           Metrics{TotalReturn: 0.15, SharpeRatio: 1.5},
		Trades:            []domain.Trade{{Quantity: 5}},
		EquityCurve:       []EquityPoint{{Timestamp: triggeredAt, Equity: 10000}},
		PromptVersion:     "v1",
		PromptVersionHash: "abc123",
	}

	if err := p.PersistRun(context.Background(), configID, triggeredAt, duration, result); err != nil {
		t.Fatalf("PersistRun() error = %v", err)
	}

	if repo.created == nil {
		t.Fatal("expected repo.Create to be called")
	}

	if repo.created.BacktestConfigID != configID {
		t.Fatalf("BacktestConfigID = %v, want %v", repo.created.BacktestConfigID, configID)
	}
	if repo.created.Duration != duration {
		t.Fatalf("Duration = %v, want %v", repo.created.Duration, duration)
	}
	if repo.created.PromptVersion != "v1" {
		t.Fatalf("PromptVersion = %q, want %q", repo.created.PromptVersion, "v1")
	}

	var metrics Metrics
	if err := json.Unmarshal(repo.created.Metrics, &metrics); err != nil {
		t.Fatalf("unmarshal metrics: %v", err)
	}
	if metrics.TotalReturn != 0.15 {
		t.Fatalf("metrics.TotalReturn = %f, want 0.15", metrics.TotalReturn)
	}
}

func TestRepoPersister_PersistRun_NilResult(t *testing.T) {
	repo := &stubRunRepo{}
	p := NewRepoPersister(repo)

	err := p.PersistRun(context.Background(), uuid.New(), time.Now(), time.Minute, nil)
	if err == nil {
		t.Fatal("PersistRun(nil) error = nil, want error")
	}
}

func TestRepoPersister_PersistRun_RepoError(t *testing.T) {
	repo := &stubRunRepo{err: fmt.Errorf("db down")}
	p := NewRepoPersister(repo)

	result := &OrchestratorResult{}
	err := p.PersistRun(context.Background(), uuid.New(), time.Now(), time.Minute, result)
	if err == nil {
		t.Fatal("PersistRun() error = nil, want error")
	}
}
