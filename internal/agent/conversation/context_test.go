package conversation_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/conversation"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// --- stub repositories ---

type stubDecisionRepo struct {
	decisions []domain.AgentDecision
	err       error
}

func (s *stubDecisionRepo) Create(_ context.Context, _ *domain.AgentDecision) error { return nil }
func (s *stubDecisionRepo) GetByRun(_ context.Context, _ uuid.UUID, _ repository.AgentDecisionFilter, _, _ int) ([]domain.AgentDecision, error) {
	return s.decisions, s.err
}

type stubSnapshotRepo struct {
	snapshots []domain.PipelineRunSnapshot
	err       error
}

func (s *stubSnapshotRepo) Create(_ context.Context, _ *domain.PipelineRunSnapshot) error {
	return nil
}

func (s *stubSnapshotRepo) GetByRun(_ context.Context, _ uuid.UUID) ([]domain.PipelineRunSnapshot, error) {
	return s.snapshots, s.err
}

type stubMemoryRepo struct {
	memories []domain.AgentMemory
	err      error
}

func (s *stubMemoryRepo) Create(_ context.Context, _ *domain.AgentMemory) error { return nil }
func (s *stubMemoryRepo) Search(_ context.Context, _ string, _ repository.MemorySearchFilter, _, _ int) ([]domain.AgentMemory, error) {
	return s.memories, s.err
}
func (s *stubMemoryRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }

// --- helpers ---

func makeDecisions(n int) []domain.AgentDecision {
	out := make([]domain.AgentDecision, n)
	for i := range out {
		out[i] = domain.AgentDecision{
			ID:         uuid.New(),
			AgentRole:  domain.AgentRoleMarketAnalyst,
			Phase:      domain.PhaseAnalysis,
			OutputText: "BTC looks bullish based on RSI and volume",
		}
	}
	return out
}

func makeSnapshots(n int) []domain.PipelineRunSnapshot {
	out := make([]domain.PipelineRunSnapshot, n)
	for i := range out {
		out[i] = domain.PipelineRunSnapshot{
			ID:       uuid.New(),
			DataType: "price",
			Payload:  json.RawMessage(`{"ticker":"BTC","price":60000}`),
		}
	}
	return out
}

func makeMemories(n int) []domain.AgentMemory {
	out := make([]domain.AgentMemory, n)
	for i := range out {
		out[i] = domain.AgentMemory{
			ID:             uuid.New(),
			AgentRole:      domain.AgentRoleMarketAnalyst,
			Situation:      "BTC RSI above 70",
			Recommendation: "Consider short-term pullback",
			CreatedAt:      time.Now(),
		}
	}
	return out
}

func makeHistory(n int) []domain.ConversationMessage {
	out := make([]domain.ConversationMessage, n)
	for i := range out {
		role := domain.ConversationMessageRoleUser
		if i%2 == 1 {
			role = domain.ConversationMessageRoleAssistant
		}
		out[i] = domain.ConversationMessage{
			ID:      uuid.New(),
			Role:    role,
			Content: strings.Repeat("word ", 200), // ~1000 chars ~ 250 tokens
		}
	}
	return out
}

// --- tests ---

func TestBuildContext_FullData(t *testing.T) {
	t.Parallel()

	cb := conversation.NewContextBuilder(
		&stubDecisionRepo{decisions: makeDecisions(3)},
		&stubSnapshotRepo{snapshots: makeSnapshots(2)},
		&stubMemoryRepo{memories: makeMemories(2)},
		0, // default
	)

	result, err := cb.BuildContext(context.Background(), conversation.ContextInput{
		RunID:               uuid.New(),
		AgentRole:           domain.AgentRoleMarketAnalyst,
		ConversationHistory: makeHistory(4),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SystemPrompt == "" {
		t.Fatal("expected non-empty system prompt")
	}
	if !strings.Contains(result.SystemPrompt, "market_analyst") {
		t.Error("system prompt should mention agent role")
	}
	if !strings.Contains(result.SystemPrompt, "Pipeline State") {
		t.Error("system prompt should contain decision section")
	}
	if !strings.Contains(result.SystemPrompt, "Market Data") {
		t.Error("system prompt should contain market data section")
	}
	if !strings.Contains(result.SystemPrompt, "Past Experience") {
		t.Error("system prompt should contain memory section")
	}
	if len(result.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result.Messages))
	}
}

func TestBuildContext_MissingSnapshots(t *testing.T) {
	t.Parallel()

	cb := conversation.NewContextBuilder(
		&stubDecisionRepo{decisions: makeDecisions(1)},
		&stubSnapshotRepo{snapshots: nil},
		&stubMemoryRepo{memories: makeMemories(1)},
		0,
	)

	result, err := cb.BuildContext(context.Background(), conversation.ContextInput{
		RunID:     uuid.New(),
		AgentRole: domain.AgentRoleTrader,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result.SystemPrompt, "Market Data") {
		t.Error("system prompt should not contain market data when snapshots are empty")
	}
	if result.Messages != nil {
		t.Error("expected nil messages for empty conversation history")
	}
}

func TestBuildContext_MissingMemories(t *testing.T) {
	t.Parallel()

	cb := conversation.NewContextBuilder(
		&stubDecisionRepo{decisions: makeDecisions(1)},
		&stubSnapshotRepo{snapshots: makeSnapshots(1)},
		&stubMemoryRepo{memories: nil},
		0,
	)

	result, err := cb.BuildContext(context.Background(), conversation.ContextInput{
		RunID:     uuid.New(),
		AgentRole: domain.AgentRoleMarketAnalyst,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result.SystemPrompt, "Past Experience") {
		t.Error("system prompt should not contain memory section when memories are empty")
	}
}

func TestBuildContext_TruncationDropsMarketFirst(t *testing.T) {
	t.Parallel()

	// Large market snapshots that push over the limit.
	bigSnapshots := make([]domain.PipelineRunSnapshot, 1)
	bigSnapshots[0] = domain.PipelineRunSnapshot{
		ID:       uuid.New(),
		DataType: "price",
		Payload:  json.RawMessage(strings.Repeat(`{"data":"x"}`, 5000)), // ~60k chars ~ 15k tokens
	}

	cb := conversation.NewContextBuilder(
		&stubDecisionRepo{decisions: makeDecisions(1)},
		&stubSnapshotRepo{snapshots: bigSnapshots},
		&stubMemoryRepo{memories: makeMemories(1)},
		2000, // tight limit
	)

	result, err := cb.BuildContext(context.Background(), conversation.ContextInput{
		RunID:               uuid.New(),
		AgentRole:           domain.AgentRoleMarketAnalyst,
		ConversationHistory: makeHistory(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Market data should be dropped first.
	if strings.Contains(result.SystemPrompt, "Market Data") {
		t.Error("market data should be truncated first")
	}
	// Core role section should survive.
	if !strings.Contains(result.SystemPrompt, "market_analyst") {
		t.Error("role section should survive truncation")
	}
}

func TestBuildContext_TruncationDropsOlderMessages(t *testing.T) {
	t.Parallel()

	// Many large conversation messages.
	history := makeHistory(40) // 40 * 250 tokens = ~10k tokens

	cb := conversation.NewContextBuilder(
		&stubDecisionRepo{decisions: makeDecisions(1)},
		&stubSnapshotRepo{snapshots: nil},
		&stubMemoryRepo{memories: nil},
		2000, // tight
	)

	result, err := cb.BuildContext(context.Background(), conversation.ContextInput{
		RunID:               uuid.New(),
		AgentRole:           domain.AgentRoleTrader,
		ConversationHistory: history,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Messages) >= 40 {
		t.Errorf("expected truncated messages, got %d", len(result.Messages))
	}
}

func TestBuildContext_NilRepos(t *testing.T) {
	t.Parallel()

	cb := conversation.NewContextBuilder(nil, nil, nil, 0)
	_, err := cb.BuildContext(context.Background(), conversation.ContextInput{
		RunID:     uuid.New(),
		AgentRole: domain.AgentRoleTrader,
	})
	if err == nil {
		t.Fatal("expected error for nil repos")
	}
}

func TestBuildContext_EmptyConversation(t *testing.T) {
	t.Parallel()

	cb := conversation.NewContextBuilder(
		&stubDecisionRepo{decisions: makeDecisions(1)},
		&stubSnapshotRepo{snapshots: makeSnapshots(1)},
		&stubMemoryRepo{memories: nil},
		0,
	)

	result, err := cb.BuildContext(context.Background(), conversation.ContextInput{
		RunID:     uuid.New(),
		AgentRole: domain.AgentRoleRiskManager,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SystemPrompt == "" {
		t.Fatal("expected system prompt even with empty conversation")
	}
	if result.Messages != nil {
		t.Error("expected nil messages for empty conversation history")
	}
}
