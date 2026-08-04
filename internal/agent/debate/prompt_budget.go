package debate

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

const (
	// 96 KiB preserves the three configured debate rounds in observed
	// production prompts while retaining a firm bound well below the configured
	// deep model's context window. Reports remain individually bounded so one
	// analyst cannot crowd out the debate history.
	maxDebatePromptBytes  = 96 * 1024
	maxAnalystReportBytes = 12 * 1024
)

type promptCompactionStats struct {
	OriginalBytes    int
	FinalBytes       int
	DroppedRounds    int
	TruncatedReports int
}

func buildBudgetedDebateMessages(systemPrompt string, rounds []agent.DebateRound, reports map[agent.AgentRole]string) ([]llm.Message, promptCompactionStats, error) {
	stats := promptCompactionStats{}
	base := []llm.Message{{Role: "system", Content: systemPrompt}}
	if err := llm.ValidatePromptBytes(base, maxDebatePromptBytes); err != nil {
		return nil, stats, err
	}

	fullReports, _ := formatAnalystReports(reports, -1)
	user := formatBudgetedContext(rounds, fullReports)
	messages := append(base, llm.Message{Role: "user", Content: user})
	stats.OriginalBytes = llm.PromptBytes(messages)
	if stats.OriginalBytes <= maxDebatePromptBytes {
		stats.FinalBytes = stats.OriginalBytes
		return messages, stats, nil
	}

	selected, dropped := selectRoundsForBudget(rounds, systemPrompt, reports)
	stats.DroppedRounds = dropped
	formattedReports, truncated := compactAnalystReports(systemPrompt, selected, reports)
	stats.TruncatedReports = truncated
	user = formatBudgetedContext(selected, formattedReports)
	messages = append(base, llm.Message{Role: "user", Content: user})
	if err := llm.ValidatePromptBytes(messages, maxDebatePromptBytes); err != nil {
		return nil, stats, err
	}
	stats.FinalBytes = llm.PromptBytes(messages)
	return messages, stats, nil
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if maxBytes < 0 || len(value) <= maxBytes {
		return value, false
	}
	for maxBytes > 0 && !utf8.ValidString(value[:maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes] + "\n[truncated]", true
}

func formatBudgetedContext(rounds []agent.DebateRound, reports string) string {
	return strings.Join([]string{"Previous debate rounds:", formatRoundsForPrompt(rounds), "", "Analyst reports:", reports}, "\n")
}

func formatBudgetedAnalystReports(reports map[agent.AgentRole]string) (string, int) {
	return formatAnalystReports(reports, maxAnalystReportBytes)
}

func formatAnalystReports(reports map[agent.AgentRole]string, maxBytes int) (string, int) {
	if len(reports) == 0 {
		return "No analyst reports available.", 0
	}
	roles := sortedRoles(reports)
	var b strings.Builder
	truncated := 0
	for i, role := range roles {
		if i > 0 {
			b.WriteString("\n\n")
		}
		body, cut := truncateUTF8(reports[role], maxBytes)
		if cut {
			truncated++
		}
		_, _ = fmt.Fprintf(&b, "%s:\n%s", role, body)
	}
	return b.String(), truncated
}

func compactAnalystReports(systemPrompt string, rounds []agent.DebateRound, reports map[agent.AgentRole]string) (string, int) {
	roles := sortedRoles(reports)
	if len(roles) == 0 {
		return "No analyst reports available.", 0
	}
	fixed := len(systemPrompt) + len("Previous debate rounds:\n") + len(formatRoundsForPrompt(rounds)) + len("\n\nAnalyst reports:\n")
	if fixed > maxDebatePromptBytes {
		return "", 0
	}
	available := maxDebatePromptBytes - fixed
	if available <= 0 {
		return "", 0
	}
	bodies := make(map[agent.AgentRole]string, len(reports))
	for role, report := range reports {
		bodies[role] = report
	}
	for {
		formatted, truncated := formatAnalystReportsWithLimit(roles, bodies, available)
		if len(formatted) <= available || available == 0 {
			return formatted, truncated
		}
		available--
	}
}

func formatAnalystReportsWithLimit(roles []agent.AgentRole, reports map[agent.AgentRole]string, maxBytes int) (string, int) {
	if len(roles) == 0 {
		return "No analyst reports available.", 0
	}
	if maxBytes <= 0 {
		return "", 0
	}
	var b strings.Builder
	truncated := 0
	for i, role := range roles {
		if i > 0 {
			b.WriteString("\n\n")
		}
		prefix := role + ":\n"
		remaining := maxBytes - b.Len() - len(prefix)
		if remaining < 0 {
			remaining = 0
		}
		body, cut := truncateUTF8(reports[role], remaining)
		if cut {
			truncated++
		}
		_, _ = fmt.Fprintf(&b, "%s%s", prefix, body)
	}
	return b.String(), truncated
}

func selectRoundsForBudget(rounds []agent.DebateRound, systemPrompt string, reports map[agent.AgentRole]string) ([]agent.DebateRound, int) {
	selected := make([]agent.DebateRound, 0, len(rounds))
	for i := len(rounds) - 1; i >= 0; i-- {
		candidate := append([]agent.DebateRound{rounds[i]}, selected...)
		formattedReports, _ := compactAnalystReports(systemPrompt, candidate, reports)
		msg := []llm.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: formatBudgetedContext(candidate, formattedReports)}}
		if llm.PromptBytes(msg) <= maxDebatePromptBytes {
			selected = candidate
			continue
		}
		break
	}
	return selected, len(rounds) - len(selected)
}

func sortedRoles(reports map[agent.AgentRole]string) []agent.AgentRole {
	roles := make([]agent.AgentRole, 0, len(reports))
	for role := range reports {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return roles
}
