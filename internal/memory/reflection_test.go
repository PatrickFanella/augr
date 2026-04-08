package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// --- mock LLM provider ---

type mockLLMProvider struct {
	response *llm.CompletionResponse
	err      error
	calls    int
}

func (m *mockLLMProvider) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.calls++
	return m.response, m.err
}

// --- mock repositories ---

type mockMemoryRepo struct {
	created []*domain.AgentMemory
	err     error
}

func (m *mockMemoryRepo) Create(_ context.Context, mem *domain.AgentMemory) error {
	if m.err != nil {
		return m.err
	}
	mem.ID = uuid.New()
	mem.CreatedAt = time.Now()
	m.created = append(m.created, mem)
	return nil
}

func (m *mockMemoryRepo) Search(context.Context, string, repository.MemorySearchFilter, int, int) ([]domain.AgentMemory, error) {
	return nil, nil
}

func (m *mockMemoryRepo) Delete(context.Context, uuid.UUID) error { return nil }

type mockPipelineRunRepo struct {
	runs []domain.PipelineRun
	err  error
}

func (m *mockPipelineRunRepo) Create(context.Context, *domain.PipelineRun) error { return nil }

func (m *mockPipelineRunRepo) Get(context.Context, uuid.UUID, time.Time) (*domain.PipelineRun, error) {
	return nil, nil
}

func (m *mockPipelineRunRepo) List(_ context.Context, _ repository.PipelineRunFilter, _, _ int) ([]domain.PipelineRun, error) {
	return m.runs, m.err
}

func (m *mockPipelineRunRepo) Count(_ context.Context, _ repository.PipelineRunFilter) (int, error) {
	return 0, nil
}

func (m *mockPipelineRunRepo) UpdateStatus(context.Context, uuid.UUID, time.Time, repository.PipelineRunStatusUpdate) error {
	return nil
}

type mockDecisionRepo struct {
	decisions []domain.AgentDecision
	err       error
}

func (m *mockDecisionRepo) Create(context.Context, *domain.AgentDecision) error { return nil }

func (m *mockDecisionRepo) GetByRun(_ context.Context, _ uuid.UUID, _ repository.AgentDecisionFilter, _, _ int) ([]domain.AgentDecision, error) {
	return m.decisions, m.err
}

func (m *mockDecisionRepo) CountByRun(_ context.Context, _ uuid.UUID, _ repository.AgentDecisionFilter) (int, error) {
	return 0, nil
}

type mockPositionRepo struct {
	position *domain.Position
	err      error
}

func (m *mockPositionRepo) Create(context.Context, *domain.Position) error { return nil }
func (m *mockPositionRepo) Update(context.Context, *domain.Position) error { return nil }
func (m *mockPositionRepo) Delete(context.Context, uuid.UUID) error        { return nil }

func (m *mockPositionRepo) Get(_ context.Context, _ uuid.UUID) (*domain.Position, error) {
	return m.position, m.err
}

func (m *mockPositionRepo) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (m *mockPositionRepo) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (m *mockPositionRepo) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (m *mockPositionRepo) Count(_ context.Context, _ repository.PositionFilter) (int, error) {
	return 0, nil
}

func (m *mockPositionRepo) CountOpen(_ context.Context, _ repository.PositionFilter) (int, error) {
	return 0, nil
}

// --- helpers ---

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discard{}, nil))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func newTestPosition() *domain.Position {
	stratID := uuid.New()
	closed := time.Now()
	return &domain.Position{
		ID:          uuid.New(),
		StrategyID:  &stratID,
		Ticker:      "AAPL",
		Side:        domain.PositionSideLong,
		Quantity:    10,
		AvgEntry:    150.0,
		RealizedPnL: 75.0,
		OpenedAt:    time.Now().Add(-48 * time.Hour),
		ClosedAt:    &closed,
	}
}

func newTestDecisions(runID uuid.UUID) []domain.AgentDecision {
	roles := reflectionRoles
	out := make([]domain.AgentDecision, len(roles))
	for i, r := range roles {
		out[i] = domain.AgentDecision{
			ID:            uuid.New(),
			PipelineRunID: runID,
			AgentRole:     r,
			Phase:         domain.PhaseTrading,
			OutputText:    fmt.Sprintf("%s: recommends action", r),
			CreatedAt:     time.Now(),
		}
	}
	return out
}

// --- tests ---

func TestReflect_GeneratesFiveMemories(t *testing.T) {
	t.Parallel()

	pos := newTestPosition()
	runID := uuid.New()
	run := domain.PipelineRun{
		ID:         runID,
		StrategyID: *pos.StrategyID,
		Ticker:     pos.Ticker,
		TradeDate:  time.Now(),
		Status:     domain.PipelineStatusCompleted,
		Signal:     domain.PipelineSignalBuy,
		StartedAt:  time.Now().Add(-1 * time.Hour),
	}

	memRepo := &mockMemoryRepo{}
	pipeRepo := &mockPipelineRunRepo{runs: []domain.PipelineRun{run}}
	decRepo := &mockDecisionRepo{decisions: newTestDecisions(runID)}
	posRepo := &mockPositionRepo{position: pos}
	provider := &mockLLMProvider{
		response: &llm.CompletionResponse{Content: "lesson learned"},
	}

	ref := NewReflector(memRepo, pipeRepo, decRepo, posRepo, provider, "test-model", discardLogger())

	if err := ref.Reflect(context.Background(), pos.ID); err != nil {
		t.Fatalf("Reflect() error = %v, want nil", err)
	}

	if got := len(memRepo.created); got != 5 {
		t.Fatalf("memories created = %d, want 5", got)
	}

	if provider.calls != 5 {
		t.Errorf("LLM calls = %d, want 5", provider.calls)
	}

	seenRoles := make(map[domain.AgentRole]bool)
	for _, m := range memRepo.created {
		seenRoles[m.AgentRole] = true

		if m.Recommendation != "lesson learned" {
			t.Errorf("memory recommendation = %q, want %q", m.Recommendation, "lesson learned")
		}
		if m.Outcome == "" {
			t.Error("memory outcome is empty, want non-empty outcome summary")
		}
		if m.PipelineRunID == nil || *m.PipelineRunID != runID {
			t.Errorf("memory pipeline_run_id = %v, want %s", m.PipelineRunID, runID)
		}
		if m.Situation == "" {
			t.Error("memory situation is empty")
		}
	}

	for _, role := range reflectionRoles {
		if !seenRoles[role] {
			t.Errorf("missing memory for role %s", role)
		}
	}
}

func TestReflect_MissingPipelineRun(t *testing.T) {
	t.Parallel()

	pos := newTestPosition()

	memRepo := &mockMemoryRepo{}
	pipeRepo := &mockPipelineRunRepo{runs: []domain.PipelineRun{}} // no runs
	decRepo := &mockDecisionRepo{}
	posRepo := &mockPositionRepo{position: pos}
	provider := &mockLLMProvider{
		response: &llm.CompletionResponse{Content: "lesson"},
	}

	ref := NewReflector(memRepo, pipeRepo, decRepo, posRepo, provider, "test-model", discardLogger())

	err := ref.Reflect(context.Background(), pos.ID)
	if err == nil {
		t.Fatal("Reflect() error = nil, want error for missing pipeline run")
	}

	if provider.calls != 0 {
		t.Errorf("LLM calls = %d, want 0 when pipeline run missing", provider.calls)
	}
	if len(memRepo.created) != 0 {
		t.Errorf("memories created = %d, want 0 when pipeline run missing", len(memRepo.created))
	}
}

func TestReflect_MissingPosition(t *testing.T) {
	t.Parallel()

	memRepo := &mockMemoryRepo{}
	pipeRepo := &mockPipelineRunRepo{}
	decRepo := &mockDecisionRepo{}
	posRepo := &mockPositionRepo{err: fmt.Errorf("position not found")}
	provider := &mockLLMProvider{
		response: &llm.CompletionResponse{Content: "lesson"},
	}

	ref := NewReflector(memRepo, pipeRepo, decRepo, posRepo, provider, "test-model", discardLogger())

	err := ref.Reflect(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("Reflect() error = nil, want error for missing position")
	}
}

func TestReflect_NilLoggerDefaultsToSlogDefault(t *testing.T) {
	t.Parallel()

	ref := NewReflector(
		&mockMemoryRepo{},
		&mockPipelineRunRepo{},
		&mockDecisionRepo{},
		&mockPositionRepo{},
		&mockLLMProvider{},
		"test-model",
		nil,
	)

	if ref.logger == nil {
		t.Fatal("logger is nil, want slog.Default()")
	}
}

func TestComputeOutcome_Profit(t *testing.T) {
	t.Parallel()

	closed := time.Now()
	pos := &domain.Position{
		AvgEntry:    100.0,
		Quantity:    10,
		RealizedPnL: 50.0,
		OpenedAt:    time.Now().Add(-72 * time.Hour),
		ClosedAt:    &closed,
	}

	out := computeOutcome(pos)
	if out == "" {
		t.Fatal("computeOutcome() returned empty string")
	}

	for _, want := range []string{"profit", "50.00", "5.00%"} {
		if !strings.Contains(out, want) {
			t.Errorf("computeOutcome() = %q, want substring %q", out, want)
		}
	}
	if strings.Contains(out, "still open") {
		t.Errorf("computeOutcome() = %q, should not contain 'still open' for closed position", out)
	}
}

func TestComputeOutcome_Loss(t *testing.T) {
	t.Parallel()

	closed := time.Now()
	pos := &domain.Position{
		AvgEntry:    100.0,
		Quantity:    10,
		RealizedPnL: -30.0,
		OpenedAt:    time.Now().Add(-24 * time.Hour),
		ClosedAt:    &closed,
	}

	out := computeOutcome(pos)
	if out == "" {
		t.Fatal("computeOutcome() returned empty string")
	}

	for _, want := range []string{"loss", "30.00", "-3.00%"} {
		if !strings.Contains(out, want) {
			t.Errorf("computeOutcome() = %q, want substring %q", out, want)
		}
	}
	if strings.Contains(out, "still open") {
		t.Errorf("computeOutcome() = %q, should not contain 'still open' for closed position", out)
	}
}

func TestReflect_OpenPositionReturnsError(t *testing.T) {
	t.Parallel()

	pos := newTestPosition()
	pos.ClosedAt = nil // still open

	memRepo := &mockMemoryRepo{}
	pipeRepo := &mockPipelineRunRepo{}
	decRepo := &mockDecisionRepo{}
	posRepo := &mockPositionRepo{position: pos}
	provider := &mockLLMProvider{
		response: &llm.CompletionResponse{Content: "lesson"},
	}

	ref := NewReflector(memRepo, pipeRepo, decRepo, posRepo, provider, "test-model", discardLogger())

	err := ref.Reflect(context.Background(), pos.ID)
	if err == nil {
		t.Fatal("Reflect() error = nil, want error for open position")
	}

	if provider.calls != 0 {
		t.Errorf("LLM calls = %d, want 0 for open position", provider.calls)
	}
}
