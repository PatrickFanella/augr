package automation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

type fakeOvernightBacktestRunRepo struct {
	run                      *domain.OvernightBacktestRun
	getActive                error
	created                  bool
	updated                  bool
	updateSeen               *domain.OvernightBacktestRun
	failOnCancelledUpdateCtx bool
	latest                   []domain.OvernightBacktestRun
}

func (f *fakeOvernightBacktestRunRepo) Create(_ context.Context, run *domain.OvernightBacktestRun) error {
	f.run = run
	f.created = true
	return nil
}

func (f *fakeOvernightBacktestRunRepo) Get(_ context.Context, _ uuid.UUID) (*domain.OvernightBacktestRun, error) {
	return f.run, nil
}

func (f *fakeOvernightBacktestRunRepo) GetActive(_ context.Context) (*domain.OvernightBacktestRun, error) {
	if f.getActive != nil {
		return nil, f.getActive
	}
	return f.run, nil
}

func (f *fakeOvernightBacktestRunRepo) Update(ctx context.Context, run *domain.OvernightBacktestRun) error {
	if f.failOnCancelledUpdateCtx {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	f.run = run
	f.updated = true
	f.updateSeen = run
	return nil
}

type blockingOvernightBacktestLLMProvider struct{}

func (blockingOvernightBacktestLLMProvider) Complete(ctx context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeOvernightBacktestRunRepo) ListLatest(_ context.Context, _ int) ([]domain.OvernightBacktestRun, error) {
	return append([]domain.OvernightBacktestRun(nil), f.latest...), nil
}

type fakeOvernightBacktestLLMProvider struct {
	responses []*llm.CompletionResponse
	requests  []llm.CompletionRequest
	calls     int
}

func (f *fakeOvernightBacktestLLMProvider) Complete(_ context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	f.requests = append(f.requests, request)
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		return f.responses[len(f.responses)-1], nil
	}
	return f.responses[idx], nil
}

func TestOvernightBacktestChunkerGenerateBudget(t *testing.T) {
	c := overnightBacktestChunker{generatePerChunk: 2}
	if got := c.nextGenerateEnd(0, 5); got != 2 {
		t.Fatalf("got %d want 2", got)
	}
	if got := c.nextGenerateEnd(2, 5); got != 4 {
		t.Fatalf("got %d want 4", got)
	}
	if got := c.nextGenerateEnd(4, 5); got != 5 {
		t.Fatalf("got %d want 5", got)
	}
}

func TestOvernightBacktestChunkerGenerateBudgetDefaultsNonPositive(t *testing.T) {
	c := overnightBacktestChunker{generatePerChunk: 0}
	if got := c.nextGenerateEnd(0, 5); got != 3 {
		t.Fatalf("got %d want 3", got)
	}
}

func TestOvernightBacktestChunkerAdvancesToSweepAfterFinalGenerate(t *testing.T) {
	c := overnightBacktestChunker{}
	run := &domain.OvernightBacktestRun{Candidates: []domain.OvernightBacktestCandidate{{Ticker: "AAA"}}, CandidateIndex: 1, Phase: domain.OvernightBacktestPhaseGenerate}
	c.advanceAfterGenerate(run)
	if run.Phase != domain.OvernightBacktestPhaseSweepValidateDeploy {
		t.Fatalf("got %s", run.Phase)
	}
}

func TestOvernightBacktestChunkerMarshalsGeneratedConfig(t *testing.T) {
	raw, err := encodeOvernightGeneratedConfig(rules.RulesEngineConfig{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	run := &domain.OvernightBacktestRun{Generated: []domain.OvernightBacktestGenerated{{Ticker: "AAA", Config: raw}}}
	var wrapped map[string]any
	if err := json.Unmarshal(run.Generated[0].Config, &wrapped); err != nil {
		t.Fatal(err)
	}
	if _, ok := wrapped["rules_engine"]; !ok {
		t.Fatalf("expected rules_engine wrapper: %#v", wrapped)
	}
	decoded, err := decodeOvernightGeneratedConfig(run.Generated[0].Config)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "x" {
		t.Fatalf("decoded name = %q, want x", decoded.Name)
	}
}

func TestOvernightBacktestChunkerRejectsUnwrappedGeneratedConfig(t *testing.T) {
	_, err := decodeOvernightGeneratedConfig(json.RawMessage(`{"name":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "missing rules_engine") {
		t.Fatalf("decode error = %v, want missing rules_engine", err)
	}
}

func TestValidateOvernightScreenResultsRejectsEmptySuccess(t *testing.T) {
	if err := validateOvernightScreenResults(nil); err == nil || !strings.Contains(err.Error(), "no candidates") {
		t.Fatalf("error = %v, want no candidates", err)
	}
	if err := validateOvernightScreenResults([]discovery.ScreenResult{{Ticker: "AAPL"}}); err != nil {
		t.Fatalf("non-empty screen error = %v", err)
	}
}

func TestOvernightBacktestChunkerRunChunkCreatesFailedRunWithoutUniverse(t *testing.T) {
	repo := &fakeOvernightBacktestRunRepo{}
	c := overnightBacktestChunker{progress: repo, generatePerChunk: 2}
	if err := c.RunChunk(context.Background()); err == nil || !strings.Contains(err.Error(), "universe not configured") {
		t.Fatalf("error = %v, want missing universe", err)
	}
	if repo.run == nil {
		t.Fatal("run nil")
	}
	if repo.run.Phase != domain.OvernightBacktestPhaseDone {
		t.Fatalf("got %s", repo.run.Phase)
	}
	if repo.run.Status != domain.OvernightBacktestStatusFailed {
		t.Fatalf("status = %s, want failed", repo.run.Status)
	}
}

func TestOvernightBacktestChunkerRunChunkCreatesRunWhenActiveMissing(t *testing.T) {
	repo := &fakeOvernightBacktestRunRepo{getActive: repository.ErrNotFound}
	c := overnightBacktestChunker{progress: repo, generatePerChunk: 2}
	if err := c.RunChunk(context.Background()); err == nil || !strings.Contains(err.Error(), "universe not configured") {
		t.Fatalf("error = %v, want missing universe", err)
	}
	if !repo.created {
		t.Fatal("expected run creation")
	}
}

func TestOvernightBacktestChunkerDoesNotStartSecondCompletedRunSameEasternDay(t *testing.T) {
	completed := domain.OvernightBacktestRun{
		ID: uuid.New(), Status: domain.OvernightBacktestStatusCompleted,
		StartedAt: time.Now().In(easternTime),
	}
	repo := &fakeOvernightBacktestRunRepo{getActive: repository.ErrNotFound, latest: []domain.OvernightBacktestRun{completed}}
	c := overnightBacktestChunker{progress: repo}
	if err := c.RunChunk(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.created {
		t.Fatal("started a second run after today's run completed")
	}
}

func TestOvernightBacktestChunkerRunGenerateChunkRequiresLLMProvider(t *testing.T) {
	repo := &fakeOvernightBacktestRunRepo{}
	run := &domain.OvernightBacktestRun{}
	c := overnightBacktestChunker{progress: repo}
	err := c.runGenerateChunk(context.Background(), run)
	if err == nil || err.Error() != "overnight_backtest: LLM provider not configured" {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != domain.OvernightBacktestStatusFailed || run.CompletedAt == nil {
		t.Fatalf("run = %+v, want terminal failure", run)
	}
}

const validStrategyJSON = `{"version":1,"name":"retry-safe","description":"minimal valid strategy","entry":{"operator":"AND","conditions":[{"field":"rsi_14","op":"lt","value":30}]},"exit":{"operator":"OR","conditions":[{"field":"rsi_14","op":"gt","value":70}]},"position_sizing":{"method":"fixed_fraction","fraction_pct":5},"stop_loss":{"method":"fixed_pct","pct":2},"take_profit":{"method":"risk_reward","ratio":2.5}}`

func TestOvernightBacktestChunkerRunGenerateChunkProcessesChunkAndPersists(t *testing.T) {
	repo := &fakeOvernightBacktestRunRepo{run: &domain.OvernightBacktestRun{ID: uuid.New()}}
	provider := &fakeOvernightBacktestLLMProvider{responses: []*llm.CompletionResponse{
		{Content: validStrategyJSON},
		{Content: validStrategyJSON},
	}}
	run := &domain.OvernightBacktestRun{
		Candidates: []domain.OvernightBacktestCandidate{{Ticker: "AAA"}, {Ticker: "BBB"}, {Ticker: "CCC"}},
		Phase:      domain.OvernightBacktestPhaseGenerate,
	}
	c := overnightBacktestChunker{progress: repo, deps: OrchestratorDeps{LLMProvider: provider}, generatePerChunk: 2}
	if err := c.runGenerateChunk(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	if len(run.Generated) != 2 {
		t.Fatalf("generated = %d, want 2", len(run.Generated))
	}
	if run.CandidateIndex != 2 {
		t.Fatalf("candidate index = %d, want 2", run.CandidateIndex)
	}
	if run.Phase != domain.OvernightBacktestPhaseGenerate {
		t.Fatalf("phase = %s, want %s", run.Phase, domain.OvernightBacktestPhaseGenerate)
	}
	if !repo.updated {
		t.Fatal("expected repo update")
	}
	if repo.updateSeen != run {
		t.Fatal("expected repo to persist the run pointer")
	}
	for i, gen := range run.Generated {
		var wrapped map[string]any
		if err := json.Unmarshal(gen.Config, &wrapped); err != nil {
			t.Fatalf("generated[%d] unwrap: %v", i, err)
		}
		if _, ok := wrapped["rules_engine"]; !ok {
			t.Fatalf("generated[%d] missing rules_engine wrapper: %#v", i, wrapped)
		}
		decoded, err := decodeOvernightGeneratedConfig(gen.Config)
		if err != nil {
			t.Fatalf("generated[%d] decode: %v", i, err)
		}
		if decoded.Name == "" {
			t.Fatalf("generated[%d] decoded name empty", i)
		}
	}
}

func TestOvernightBacktestChunkerPersistsProgressAfterGenerateTimeout(t *testing.T) {
	repo := &fakeOvernightBacktestRunRepo{run: &domain.OvernightBacktestRun{ID: uuid.New()}, failOnCancelledUpdateCtx: true}
	run := &domain.OvernightBacktestRun{
		Candidates: []domain.OvernightBacktestCandidate{{Ticker: "AAA"}},
		Phase:      domain.OvernightBacktestPhaseGenerate,
	}
	c := overnightBacktestChunker{
		progress:         repo,
		deps:             OrchestratorDeps{LLMProvider: blockingOvernightBacktestLLMProvider{}},
		generatePerChunk: 1,
		generateTimeout:  5 * time.Millisecond,
		progressTimeout:  100 * time.Millisecond,
	}
	if err := c.runGenerateChunk(context.Background(), run); err == nil || !strings.Contains(err.Error(), "candidate generations failed") {
		t.Fatalf("error = %v, want candidate generation failure", err)
	}
	if run.CandidateIndex != 1 {
		t.Fatalf("candidate index = %d, want 1", run.CandidateIndex)
	}
	if run.Phase != domain.OvernightBacktestPhaseDone || run.Status != domain.OvernightBacktestStatusFailed {
		t.Fatalf("run state = %s/%s, want failed/done", run.Status, run.Phase)
	}
	if len(run.Errors) == 0 || !strings.Contains(strings.Join(run.Errors, " "), "deadline") {
		t.Fatalf("errors = %#v, want deadline", run.Errors)
	}
	if !repo.updated {
		t.Fatal("expected progress update")
	}
}

func TestOvernightBacktestChunkerMissingUniverseFailsRun(t *testing.T) {
	now := time.Now()
	repo := &fakeOvernightBacktestRunRepo{run: &domain.OvernightBacktestRun{ID: uuid.New(), Status: domain.OvernightBacktestStatusRunning, Phase: domain.OvernightBacktestPhaseScreen, StartedAt: now, UpdatedAt: now}}
	c := overnightBacktestChunker{progress: repo, deps: OrchestratorDeps{DataService: nil, Universe: nil}}
	if err := c.runScreen(context.Background(), repo.run); err == nil || !strings.Contains(err.Error(), "universe not configured") {
		t.Fatalf("error = %v, want missing universe failure", err)
	}
	if repo.run.Status != domain.OvernightBacktestStatusFailed || repo.run.Phase != domain.OvernightBacktestPhaseDone || repo.run.CompletedAt == nil {
		t.Fatalf("unexpected run state: %+v", repo.run)
	}
}

func TestOvernightBacktestChunkerMaxAgeMarksFailedCompletedAt(t *testing.T) {
	started := time.Now().Add(-overnightBacktestMaxRunAge - time.Hour)
	repo := &fakeOvernightBacktestRunRepo{run: &domain.OvernightBacktestRun{ID: uuid.New(), Status: domain.OvernightBacktestStatusRunning, Phase: domain.OvernightBacktestPhaseScreen, StartedAt: started, UpdatedAt: time.Now()}}
	c := overnightBacktestChunker{progress: repo}
	err := c.RunChunk(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stale run") {
		t.Fatalf("error = %v, want stale run failure", err)
	}
	if repo.run.Status != domain.OvernightBacktestStatusFailed || repo.run.Phase != domain.OvernightBacktestPhaseDone || repo.run.CompletedAt == nil {
		t.Fatalf("unexpected run state: %+v", repo.run)
	}
	if len(repo.run.Errors) != 1 || !strings.Contains(repo.run.Errors[0], "stale run") || !strings.Contains(repo.run.Errors[0], "18h") {
		t.Fatalf("errors = %#v, want durable stale-run cause", repo.run.Errors)
	}
}
