// Package conversation builds token-aware LLM context from pipeline state,
// agent decisions, market snapshots, and agent memories.
package conversation

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const defaultMaxTokens = 8000

// ContextBuilder assembles an LLM-ready context from multiple data sources
// with token-aware truncation.
type ContextBuilder struct {
	decisions repository.AgentDecisionRepository
	snapshots repository.PipelineRunSnapshotRepository
	memories  repository.MemoryRepository
	maxTokens int
}

// NewContextBuilder creates a ContextBuilder. If maxTokens <= 0, it defaults to 8000.
func NewContextBuilder(
	decisions repository.AgentDecisionRepository,
	snapshots repository.PipelineRunSnapshotRepository,
	memories repository.MemoryRepository,
	maxTokens int,
) *ContextBuilder {
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	return &ContextBuilder{
		decisions: decisions,
		snapshots: snapshots,
		memories:  memories,
		maxTokens: maxTokens,
	}
}

// ContextInput provides the data needed to build an LLM context.
type ContextInput struct {
	RunID               uuid.UUID
	AgentRole           domain.AgentRole
	ConversationHistory []domain.ConversationMessage
}

// BuiltContext is the assembled prompt ready for an LLM call.
type BuiltContext struct {
	SystemPrompt string
	Messages     []llm.Message
}

// BuildContext assembles a system prompt and message list from pipeline state,
// market snapshots, and agent memories, applying token-aware truncation when
// the total exceeds maxTokens.
func (b *ContextBuilder) BuildContext(ctx context.Context, input ContextInput) (*BuiltContext, error) {
	if b.decisions == nil || b.snapshots == nil || b.memories == nil {
		return nil, fmt.Errorf("context builder: nil repository dependency")
	}

	// 1. Agent decisions for this run filtered by role.
	decisions, err := b.decisions.GetByRun(ctx, input.RunID, repository.AgentDecisionFilter{
		AgentRole: input.AgentRole,
	}, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("context builder: fetch decisions: %w", err)
	}

	// 2. Pipeline run snapshots (market data).
	snapshots, err := b.snapshots.GetByRun(ctx, input.RunID)
	if err != nil {
		return nil, fmt.Errorf("context builder: fetch snapshots: %w", err)
	}

	// 3. Search memories for this agent role.
	memories, err := b.memories.Search(ctx, string(input.AgentRole), repository.MemorySearchFilter{
		AgentRole: input.AgentRole,
	}, 20, 0)
	if err != nil {
		return nil, fmt.Errorf("context builder: fetch memories: %w", err)
	}

	// Build system prompt sections.
	roleSection := buildRoleSection(input.AgentRole)
	decisionSection := buildDecisionSection(decisions)
	marketSection := buildMarketSection(snapshots)
	memorySection := buildMemorySection(memories)

	// Convert conversation history.
	messages := convertMessages(input.ConversationHistory)

	// Token-aware truncation.
	systemPrompt, messages := b.truncate(roleSection, decisionSection, marketSection, memorySection, messages)

	return &BuiltContext{
		SystemPrompt: systemPrompt,
		Messages:     messages,
	}, nil
}

// estimateTokens returns a rough token count: len(text)/4.
func estimateTokens(text string) int {
	return len(text) / 4
}

// truncate assembles the system prompt and truncates to fit within maxTokens.
// Truncation order: market data first, then older conversation turns.
func (b *ContextBuilder) truncate(
	roleSection, decisionSection, marketSection, memorySection string,
	messages []llm.Message,
) (string, []llm.Message) {
	// Core sections that are never truncated.
	core := joinSections(roleSection, decisionSection)

	// Optional sections ordered by truncation priority (market first, then memory).
	systemPrompt := joinSections(core, marketSection, memorySection)
	total := estimateTokens(systemPrompt) + estimateMessages(messages)

	if total <= b.maxTokens {
		return systemPrompt, messages
	}

	// Drop market data first.
	systemPrompt = joinSections(core, memorySection)
	total = estimateTokens(systemPrompt) + estimateMessages(messages)
	if total <= b.maxTokens {
		return systemPrompt, messages
	}

	// Drop memory section too.
	systemPrompt = core
	total = estimateTokens(systemPrompt) + estimateMessages(messages)
	if total <= b.maxTokens {
		return systemPrompt, messages
	}

	// Truncate older conversation turns (keep most recent).
	for len(messages) > 0 && total > b.maxTokens {
		total -= estimateTokens(messages[0].Content)
		messages = messages[1:]
	}

	return systemPrompt, messages
}

func estimateMessages(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += estimateTokens(m.Content)
	}
	return n
}

func joinSections(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

func buildRoleSection(role domain.AgentRole) string {
	return fmt.Sprintf("You are the %s agent in a multi-agent trading pipeline. Analyze the provided data and deliver insights specific to your role.", role)
}

func buildDecisionSection(decisions []domain.AgentDecision) string {
	if len(decisions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Pipeline State (Prior Decisions)")
	for _, d := range decisions {
		fmt.Fprintf(&sb, "\n- [%s/%s] %s", d.Phase, d.AgentRole, d.OutputText)
	}
	return sb.String()
}

func buildMarketSection(snapshots []domain.PipelineRunSnapshot) string {
	if len(snapshots) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Market Data Snapshots")
	for _, s := range snapshots {
		fmt.Fprintf(&sb, "\n- [%s] %s", s.DataType, string(s.Payload))
	}
	return sb.String()
}

func buildMemorySection(memories []domain.AgentMemory) string {
	if len(memories) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Relevant Past Experience")
	for _, m := range memories {
		fmt.Fprintf(&sb, "\n- Situation: %s, Lesson: %s", m.Situation, m.Recommendation)
	}
	return sb.String()
}

func convertMessages(history []domain.ConversationMessage) []llm.Message {
	if len(history) == 0 {
		return nil
	}
	msgs := make([]llm.Message, len(history))
	for i, m := range history {
		msgs[i] = llm.Message{
			Role:    m.Role.String(),
			Content: m.Content,
		}
	}
	return msgs
}
