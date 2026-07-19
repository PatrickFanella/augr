package debate

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestBuildBudgetedDebateMessagesSmallInputPreservesPrompt(t *testing.T) {
	systemPrompt := "You are the bull researcher."
	rounds := []agent.DebateRound{{Number: 1, Contributions: map[agent.AgentRole]string{agent.AgentRoleBullResearcher: "Opening bull thesis.", agent.AgentRoleBearResearcher: "Initial rebuttal."}}}
	reports := map[agent.AgentRole]string{agent.AgentRoleMarketAnalyst: "Trend remains constructive.", agent.AgentRoleNewsAnalyst: "News flow is mixed."}

	messages, stats, err := buildBudgetedDebateMessages(systemPrompt, rounds, reports)
	if err != nil {
		t.Fatal(err)
	}
	if stats.OriginalBytes != stats.FinalBytes {
		t.Fatalf("stats = %+v, want unchanged", stats)
	}
	want := []llm.Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: "Previous debate rounds:\nRound 1:\n- bear_researcher: Initial rebuttal.\n- bull_researcher: Opening bull thesis.\n\nAnalyst reports:\nmarket_analyst:\nTrend remains constructive.\n\nnews_analyst:\nNews flow is mixed."}}
	if len(messages) != len(want) || messages[0] != want[0] || messages[1] != want[1] {
		t.Fatalf("messages = %+v, want %+v", messages, want)
	}
}

func TestBuildBudgetedDebateMessagesKeepsSingleNineKiBReportUnderBudget(t *testing.T) {
	systemPrompt := "You are the bull researcher."
	reports := map[agent.AgentRole]string{agent.AgentRoleMarketAnalyst: strings.Repeat("r", 9*1024)}

	messages, stats, err := buildBudgetedDebateMessages(systemPrompt, nil, reports)
	if err != nil {
		t.Fatal(err)
	}
	if stats.OriginalBytes != stats.FinalBytes {
		t.Fatalf("stats = %+v, want unchanged", stats)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	if !strings.Contains(messages[1].Content, strings.Repeat("r", 9*1024)) {
		t.Fatal("expected 9KiB report to remain unchanged")
	}
}

func TestBuildBudgetedDebateMessagesCompactsNewestRoundsAndUtf8(t *testing.T) {
	systemPrompt := strings.Repeat("s", 32)
	rounds := make([]agent.DebateRound, 20)
	for i := range rounds {
		rounds[i] = agent.DebateRound{Number: i + 1, Contributions: map[agent.AgentRole]string{agent.AgentRoleBullResearcher: strings.Repeat("b", 8*1024), agent.AgentRoleBearResearcher: strings.Repeat("é", 4096)}}
	}
	reports := map[agent.AgentRole]string{agent.AgentRoleMarketAnalyst: strings.Repeat("m", 20*1024), agent.AgentRoleNewsAnalyst: strings.Repeat("n", 20*1024), agent.AgentRoleFundamentalsAnalyst: strings.Repeat("f", 20*1024)}
	messages, stats, err := buildBudgetedDebateMessages(systemPrompt, rounds, reports)
	if err != nil {
		t.Fatal(err)
	}
	if got := llm.PromptBytes(messages); got > maxDebatePromptBytes {
		t.Fatalf("prompt bytes = %d, max = %d", got, maxDebatePromptBytes)
	}
	if !strings.Contains(messages[1].Content, "Round 20:") || strings.Contains(messages[1].Content, "Round 1:") {
		t.Fatalf("round selection failed: %q", messages[1].Content)
	}
	if !utf8.ValidString(messages[1].Content) {
		t.Fatal("user content is not valid UTF-8")
	}
	if stats.DroppedRounds == 0 {
		t.Fatal("expected compaction to drop rounds")
	}
}

func TestBuildBudgetedDebateMessagesCompactsOverheadHeavyReportsToFit(t *testing.T) {
	systemPrompt := strings.Repeat("s", 8*1024)
	rounds := []agent.DebateRound{{Number: 1, Contributions: map[agent.AgentRole]string{agent.AgentRoleBullResearcher: strings.Repeat("b", 18*1024)}}}
	reports := map[agent.AgentRole]string{
		agent.AgentRoleMarketAnalyst:       strings.Repeat("m", 25*1024),
		agent.AgentRoleNewsAnalyst:         strings.Repeat("n", 25*1024),
		agent.AgentRoleFundamentalsAnalyst: strings.Repeat("f", 25*1024),
		agent.AgentRoleSocialMediaAnalyst:  strings.Repeat("t", 25*1024),
	}

	messages, stats, err := buildBudgetedDebateMessages(systemPrompt, rounds, reports)
	if err != nil {
		t.Fatal(err)
	}
	if got := llm.PromptBytes(messages); got > maxDebatePromptBytes {
		t.Fatalf("prompt bytes = %d, max = %d", got, maxDebatePromptBytes)
	}
	if stats.FinalBytes > maxDebatePromptBytes || stats.FinalBytes >= stats.OriginalBytes {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestBuildBudgetedDebateMessagesNearLimitSystemPromptMinimalContext(t *testing.T) {
	minimalContext := len("Previous debate rounds:\nNo previous debate rounds.\n\nAnalyst reports:\nNo analyst reports available.")
	systemPrompt := strings.Repeat("s", maxDebatePromptBytes-minimalContext)
	messages, stats, err := buildBudgetedDebateMessages(systemPrompt, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.OriginalBytes != maxDebatePromptBytes || stats.FinalBytes != maxDebatePromptBytes {
		t.Fatalf("stats = %+v", stats)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
}
