package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type captureRunRepository struct {
	config     json.RawMessage
	result     json.RawMessage
	startedAt  time.Time
	duration   time.Duration
	candidates int
	deployed   int
	err        error
}

func (r *captureRunRepository) Create(_ context.Context, config, result json.RawMessage, startedAt time.Time, duration time.Duration, candidates, deployed int) error {
	r.config, r.result, r.startedAt, r.duration, r.candidates, r.deployed = config, result, startedAt, duration, candidates, deployed
	return r.err
}

func (*captureRunRepository) List(context.Context, int, int) ([]DiscoveryRun, error) { return nil, nil }

func (*captureRunRepository) Count(context.Context) (int, error) { return 0, nil }

func TestPersistRunStoresConfigAndCompleteResult(t *testing.T) {
	repo := &captureRunRepository{}
	startedAt := time.Now().Add(-time.Minute).UTC()
	result := &DiscoveryResult{Candidates: 30, Generated: 29, Swept: 28, Validated: 6, Deployed: 3, Duration: 2 * time.Minute, Errors: []string{"one"}}
	cfg := DiscoveryConfig{
		Screener:   ScreenerConfig{Tickers: []string{"AAPL"}, MinADV: 123},
		Generator:  GeneratorConfig{Model: "test-model", MaxRetries: 2},
		Sweep:      SweepConfig{InitialCash: 50_000, Variations: 7},
		MaxWinners: 3,
		DryRun:     true,
	}

	if err := PersistRun(context.Background(), repo, cfg, result, startedAt); err != nil {
		t.Fatalf("PersistRun() error = %v", err)
	}
	if repo.startedAt != startedAt || repo.duration != result.Duration || repo.candidates != 30 || repo.deployed != 3 {
		t.Fatalf("persisted metadata = %#v", repo)
	}
	if strings.Contains(string(repo.config), "Provider") || strings.Contains(string(repo.config), "Metrics") {
		t.Fatalf("runtime-only dependencies leaked into config: %s", repo.config)
	}
	if !strings.Contains(string(repo.config), `"model":"test-model"`) || !strings.Contains(string(repo.result), `"errors":["one"]`) {
		t.Fatalf("missing config/result evidence: config=%s result=%s", repo.config, repo.result)
	}
}

func TestPersistRunFailsVisible(t *testing.T) {
	if err := PersistRun(context.Background(), nil, DiscoveryConfig{}, &DiscoveryResult{}, time.Now()); err == nil {
		t.Fatal("PersistRun() nil repository error = nil")
	}
	repo := &captureRunRepository{err: errors.New("write failed")}
	if err := PersistRun(context.Background(), repo, DiscoveryConfig{}, &DiscoveryResult{}, time.Now()); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("PersistRun() error = %v, want write failure", err)
	}
}
