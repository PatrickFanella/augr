package debate

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// BaseDebater holds the common dependencies shared by debate agents.
type BaseDebater struct {
	role     agent.AgentRole
	phase    agent.Phase
	provider llm.Provider
	model    string
	logger   *slog.Logger
}

// NewBaseDebater creates a BaseDebater with the given role, phase, LLM
// provider, model, and logger. A nil logger is replaced with the default logger.
func NewBaseDebater(
	role agent.AgentRole,
	phase agent.Phase,
	provider llm.Provider,
	model string,
	logger *slog.Logger,
) BaseDebater {
	if logger == nil {
		logger = slog.Default()
	}

	return BaseDebater{
		role:     role,
		phase:    phase,
		provider: provider,
		model:    model,
		logger:   logger,
	}
}

// Model returns the LLM model name configured for this debater.
func (b BaseDebater) Model() string { return b.model }

// CallWithContext sends a completion request to the LLM provider with the
// given system prompt, previous debate rounds, and analyst reports formatted
// as the user message. It returns the response content, the concatenated prompt
// text sent to the LLM (system message followed by user message), token usage,
// and any error encountered. Errors are prefixed with "role (phase):" for
// easier debugging.
func (b BaseDebater) CallWithContext(
	ctx context.Context,
	systemPrompt string,
	previousRounds []agent.DebateRound,
	analystReports map[agent.AgentRole]string,
) (string, string, llm.CompletionUsage, error) {
	errorPrefix := fmt.Sprintf("%s (%s)", b.role, b.phase)

	if b.provider == nil {
		return "", "", llm.CompletionUsage{}, fmt.Errorf("%s: nil llm provider", errorPrefix)
	}

	messages, stats, err := buildBudgetedDebateMessages(systemPrompt, previousRounds, analystReports)
	if err != nil {
		return "", "", llm.CompletionUsage{}, fmt.Errorf("%s: prompt preflight failed: %w", errorPrefix, err)
	}
	if stats.OriginalBytes != stats.FinalBytes {
		b.logger.WarnContext(ctx, "debate prompt compacted",
			"role", b.role,
			"phase", b.phase,
			"original_bytes", stats.OriginalBytes,
			"final_bytes", stats.FinalBytes,
			"dropped_rounds", stats.DroppedRounds,
			"truncated_reports", stats.TruncatedReports,
		)
	}
	promptText := agent.PromptTextFromMessages(messages)

	resp, err := b.provider.Complete(ctx, llm.CompletionRequest{
		Model:    b.model,
		Messages: messages,
	})
	if err != nil {
		return "", "", llm.CompletionUsage{}, fmt.Errorf("%s: llm completion failed: %w", errorPrefix, err)
	}
	if resp == nil {
		return "", "", llm.CompletionUsage{}, fmt.Errorf("%s: nil llm response", errorPrefix)
	}

	return resp.Content, promptText, resp.Usage, nil
}
func formatRoundsForPrompt(rounds []agent.DebateRound) string {
	if len(rounds) == 0 {
		return "No previous debate rounds."
	}

	var builder strings.Builder
	for i, round := range rounds {
		if i > 0 {
			builder.WriteString("\n\n")
		}

		_, _ = fmt.Fprintf(&builder, "Round %d:", round.Number)
		if len(round.Contributions) == 0 {
			builder.WriteString("\n- No contributions recorded.")
			continue
		}

		roles := make([]agent.AgentRole, 0, len(round.Contributions))
		for role := range round.Contributions {
			roles = append(roles, role)
		}
		sort.Slice(roles, func(i, j int) bool {
			return roles[i] < roles[j]
		})

		for _, role := range roles {
			_, _ = fmt.Fprintf(&builder, "\n- %s: %s", role, round.Contributions[role])
		}
	}

	return builder.String()
}
