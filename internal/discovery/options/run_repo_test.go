package options

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
)

type captureOptionsRunRepository struct {
	config     json.RawMessage
	result     json.RawMessage
	startedAt  time.Time
	duration   time.Duration
	candidates int
	deployed   int
	err        error
}

func (r *captureOptionsRunRepository) Create(_ context.Context, config, result json.RawMessage, startedAt time.Time, duration time.Duration, candidates, deployed int) error {
	r.config, r.result, r.startedAt, r.duration, r.candidates, r.deployed = config, result, startedAt, duration, candidates, deployed
	return r.err
}

func (*captureOptionsRunRepository) List(context.Context, int, int) ([]discovery.DiscoveryRun, error) {
	return nil, nil
}
func (*captureOptionsRunRepository) Count(context.Context) (int, error) { return 0, nil }

func TestPersistOptionsRunStoresSanitizedConfigAndCompleteResult(t *testing.T) {
	repo := &captureOptionsRunRepository{}
	startedAt := time.Now().Add(-time.Minute).UTC()
	result := &OptionsDiscoveryResult{
		Candidates: 12, Generated: 4, Deployed: 2, Duration: 45 * time.Second,
		Errors:             []string{"one"},
		GenerationEvidence: []OptionsGenerationEvidence{{Ticker: "NVDA", SystemPromptSHA256: "abc"}},
	}
	cfg := OptionsDiscoveryConfig{
		Screener:   OptionsScreenerConfig{Tickers: []string{"NVDA"}, MinADV: 123},
		Generator:  discovery.GeneratorConfig{Model: "openai/luna", MaxRetries: 2},
		MaxWinners: 2, DryRun: true, ScheduleCron: "30 6 * * 2-6",
	}

	if err := PersistRun(context.Background(), repo, cfg, result, startedAt); err != nil {
		t.Fatalf("PersistRun() error = %v", err)
	}
	if repo.startedAt != startedAt || repo.duration != result.Duration || repo.candidates != 12 || repo.deployed != 2 {
		t.Fatalf("persisted metadata = %#v", repo)
	}
	if strings.Contains(string(repo.config), "Provider") || strings.Contains(string(repo.config), "Metrics") {
		t.Fatalf("runtime-only dependencies leaked into config: %s", repo.config)
	}
	if !strings.Contains(string(repo.config), `"kind":"options"`) || !strings.Contains(string(repo.config), `"model":"openai/luna"`) || !strings.Contains(string(repo.result), `"generation_evidence":[{"ticker":"NVDA"`) {
		t.Fatalf("missing config/result evidence: config=%s result=%s", repo.config, repo.result)
	}
}

func TestPersistOptionsRunFailsVisible(t *testing.T) {
	if err := PersistRun(context.Background(), nil, OptionsDiscoveryConfig{}, &OptionsDiscoveryResult{}, time.Now()); err == nil {
		t.Fatal("PersistRun() nil repository error = nil")
	}
	if err := PersistRun(context.Background(), &captureOptionsRunRepository{}, OptionsDiscoveryConfig{}, nil, time.Now()); err == nil {
		t.Fatal("PersistRun() nil result error = nil")
	}
	repo := &captureOptionsRunRepository{err: errors.New("write failed")}
	if err := PersistRun(context.Background(), repo, OptionsDiscoveryConfig{}, &OptionsDiscoveryResult{}, time.Now()); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("PersistRun() error = %v, want write failure", err)
	}
}
