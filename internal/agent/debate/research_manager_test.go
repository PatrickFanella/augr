package debate

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestNewResearchManagerNilLogger(t *testing.T) {
	rm := NewResearchManager(nil, "openai", "model", nil)
	if rm == nil {
		t.Fatal("NewResearchManager() returned nil")
	}
}

func TestResearchManagerNodeInterface(t *testing.T) {
	rm := NewResearchManager(nil, "openai", "model", slog.Default())

	if got := rm.Name(); got != "research_manager" {
		t.Fatalf("Name() = %q, want %q", got, "research_manager")
	}
	if got := rm.Role(); got != agent.AgentRoleInvestJudge {
		t.Fatalf("Role() = %q, want %q", got, agent.AgentRoleInvestJudge)
	}
	if got := rm.Phase(); got != agent.PhaseResearchDebate {
		t.Fatalf("Phase() = %q, want %q", got, agent.PhaseResearchDebate)
	}
}

func TestResearchManagerExecuteStoresInvestmentPlanAndDecision(t *testing.T) {
	validJSON := `{
  "direction": "buy",
  "conviction": 7,
  "key_evidence": ["Strong revenue growth of 15% YoY", "Expanding margins"],
  "acknowledged_risks": ["High valuation multiple", "Macro headwinds"],
  "rationale": "Bull case is stronger given revenue momentum, but valuation risk limits conviction."
}`

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: validJSON,
			Usage: llm.CompletionUsage{
				PromptTokens:     250,
				CompletionTokens: 80,
			},
		},
	}

	rm := NewResearchManager(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "AAPL",
		AnalystReports: map[agent.AgentRole]string{
			agent.AgentRoleMarketAnalyst: "Trend is bullish.",
			agent.AgentRoleNewsAnalyst:   "Mixed sentiment.",
		},
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{
					Number: 1,
					Contributions: map[agent.AgentRole]string{
						agent.AgentRoleBullResearcher: "Revenue growth is strong.",
						agent.AgentRoleBearResearcher: "Margins are compressing.",
					},
				},
			},
		},
	}

	if err := rm.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// When parsing succeeds, Execute stores a normalized (re-marshaled) JSON
	// string, not the raw LLM content.
	wantPlan := `{"direction":"buy","conviction":7,"key_evidence":["Strong revenue growth of 15% YoY","Expanding margins"],"acknowledged_risks":["High valuation multiple","Macro headwinds"],"rationale":"Bull case is stronger given revenue momentum, but valuation risk limits conviction."}`
	if state.ResearchDebate.InvestmentPlan != wantPlan {
		t.Fatalf("InvestmentPlan = %q, want %q", state.ResearchDebate.InvestmentPlan, wantPlan)
	}

	// Verify decision was recorded (no round number for judge).
	decision, ok := state.Decision(agent.AgentRoleInvestJudge, agent.PhaseResearchDebate, nil)
	if !ok {
		t.Fatal("Decision() not found for invest_judge")
	}
	if decision.OutputText != wantPlan {
		t.Fatalf("decision output = %q, want %q", decision.OutputText, wantPlan)
	}
	if decision.LLMResponse == nil || decision.LLMResponse.Response == nil {
		t.Fatal("decision LLM response is nil")
	}
	if decision.LLMResponse.Response.Usage.PromptTokens != 250 {
		t.Fatalf("prompt tokens = %d, want 250", decision.LLMResponse.Response.Usage.PromptTokens)
	}
	if decision.LLMResponse.Response.Usage.CompletionTokens != 80 {
		t.Fatalf("completion tokens = %d, want 80", decision.LLMResponse.Response.Usage.CompletionTokens)
	}
	if decision.LLMResponse.Provider != "test-provider" {
		t.Fatalf("provider = %q, want %q", decision.LLMResponse.Provider, "test-provider")
	}
	if decision.LLMResponse.Response.Model != "test-model" {
		t.Fatalf("model in response = %q, want %q", decision.LLMResponse.Response.Model, "test-model")
	}

	// Verify the system prompt was the research manager prompt.
	if mock.lastReq.Messages[0].Content != ResearchManagerSystemPrompt {
		t.Fatalf("system prompt mismatch:\ngot:  %q\nwant: %q", mock.lastReq.Messages[0].Content, ResearchManagerSystemPrompt)
	}

	// Verify the model was forwarded.
	if mock.lastReq.Model != "test-model" {
		t.Fatalf("model = %q, want %q", mock.lastReq.Model, "test-model")
	}
}

func TestResearchManagerExecuteMalformedOutputStillStoresContent(t *testing.T) {
	// The LLM returns invalid JSON, but Execute should still succeed and
	// store the raw content in InvestmentPlan.
	malformedContent := "I recommend buying AAPL with high conviction based on the analysis."

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: malformedContent,
			Usage: llm.CompletionUsage{
				PromptTokens:     200,
				CompletionTokens: 30,
			},
		},
	}

	rm := NewResearchManager(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "AAPL",
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{
					Number: 1,
					Contributions: map[agent.AgentRole]string{
						agent.AgentRoleBullResearcher: "Revenue growth is strong.",
						agent.AgentRoleBearResearcher: "Margins are compressing.",
					},
				},
			},
		},
	}

	// Execute should not return an error even with malformed output.
	if err := rm.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Raw content should still be stored.
	if state.ResearchDebate.InvestmentPlan != malformedContent {
		t.Fatalf("InvestmentPlan = %q, want %q", state.ResearchDebate.InvestmentPlan, malformedContent)
	}

	// Decision should still be recorded.
	decision, ok := state.Decision(agent.AgentRoleInvestJudge, agent.PhaseResearchDebate, nil)
	if !ok {
		t.Fatal("Decision() not found for invest_judge after malformed output")
	}
	if decision.OutputText != malformedContent {
		t.Fatalf("decision output = %q, want %q", decision.OutputText, malformedContent)
	}
}

func TestResearchManagerExecuteNilProvider(t *testing.T) {
	rm := NewResearchManager(nil, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := rm.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "invest_judge (research_debate): nil llm provider"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestResearchManagerExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("service unavailable"),
	}

	rm := NewResearchManager(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := rm.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "invest_judge (research_debate): llm completion failed: service unavailable"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestResearchManagerExecuteNoRounds(t *testing.T) {
	validJSON := `{"direction": "hold", "conviction": 3, "key_evidence": ["Insufficient data"], "acknowledged_risks": ["No debate occurred"], "rationale": "Without debate, defaulting to hold."}`

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: validJSON,
			Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
		},
	}

	rm := NewResearchManager(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		ResearchDebate: agent.ResearchDebateState{},
	}

	if err := rm.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Normalized JSON should be stored.
	wantPlan := `{"direction":"hold","conviction":3,"key_evidence":["Insufficient data"],"acknowledged_risks":["No debate occurred"],"rationale":"Without debate, defaulting to hold."}`
	if state.ResearchDebate.InvestmentPlan != wantPlan {
		t.Fatalf("InvestmentPlan = %q, want %q", state.ResearchDebate.InvestmentPlan, wantPlan)
	}

	// Decision should be recorded (nil round for judge).
	decision, ok := state.Decision(agent.AgentRoleInvestJudge, agent.PhaseResearchDebate, nil)
	if !ok {
		t.Fatal("Decision() not found for invest_judge with no rounds")
	}
	if decision.OutputText != wantPlan {
		t.Fatalf("decision output = %q, want %q", decision.OutputText, wantPlan)
	}
}

// --- ParseInvestmentPlan unit tests ---

func TestParseInvestmentPlanValidJSON(t *testing.T) {
	input := `{
  "direction": "buy",
  "conviction": 8,
  "key_evidence": ["Revenue up 20%", "Market share expanding"],
  "acknowledged_risks": ["High PE ratio", "Sector rotation risk"],
  "rationale": "Strong fundamentals outweigh valuation concerns."
}`

	plan, err := ParseInvestmentPlan(input)
	if err != nil {
		t.Fatalf("ParseInvestmentPlan() error = %v, want nil", err)
	}

	if plan.Direction != "buy" {
		t.Fatalf("Direction = %q, want %q", plan.Direction, "buy")
	}
	if plan.Conviction != 8 {
		t.Fatalf("Conviction = %d, want 8", plan.Conviction)
	}
	if len(plan.KeyEvidence) != 2 {
		t.Fatalf("KeyEvidence length = %d, want 2", len(plan.KeyEvidence))
	}
	if plan.KeyEvidence[0] != "Revenue up 20%" {
		t.Fatalf("KeyEvidence[0] = %q, want %q", plan.KeyEvidence[0], "Revenue up 20%")
	}
	if len(plan.AcknowledgedRisks) != 2 {
		t.Fatalf("AcknowledgedRisks length = %d, want 2", len(plan.AcknowledgedRisks))
	}
	if plan.AcknowledgedRisks[0] != "High PE ratio" {
		t.Fatalf("AcknowledgedRisks[0] = %q, want %q", plan.AcknowledgedRisks[0], "High PE ratio")
	}
	if plan.Rationale != "Strong fundamentals outweigh valuation concerns." {
		t.Fatalf("Rationale = %q, want %q", plan.Rationale, "Strong fundamentals outweigh valuation concerns.")
	}
}

func TestParseInvestmentPlanWithCodeFences(t *testing.T) {
	input := "```json\n" + `{
  "direction": "sell",
  "conviction": 6,
  "key_evidence": ["Declining margins"],
  "acknowledged_risks": ["Potential turnaround"],
  "rationale": "Bearish momentum dominates."
}` + "\n```"

	plan, err := ParseInvestmentPlan(input)
	if err != nil {
		t.Fatalf("ParseInvestmentPlan() error = %v, want nil", err)
	}
	if plan.Direction != "sell" {
		t.Fatalf("Direction = %q, want %q", plan.Direction, "sell")
	}
	if plan.Conviction != 6 {
		t.Fatalf("Conviction = %d, want 6", plan.Conviction)
	}
}

func TestParseInvestmentPlanWithPlainCodeFences(t *testing.T) {
	input := "```\n" + `{
  "direction": "hold",
  "conviction": 5,
  "key_evidence": ["Mixed signals"],
  "acknowledged_risks": ["Uncertain outlook"],
  "rationale": "Wait for clarity."
}` + "\n```"

	plan, err := ParseInvestmentPlan(input)
	if err != nil {
		t.Fatalf("ParseInvestmentPlan() error = %v, want nil", err)
	}
	if plan.Direction != "hold" {
		t.Fatalf("Direction = %q, want %q", plan.Direction, "hold")
	}
}

func TestParseInvestmentPlanMalformedJSON(t *testing.T) {
	input := "This is not valid JSON at all."

	_, err := ParseInvestmentPlan(input)
	if err == nil {
		t.Fatal("ParseInvestmentPlan() error = nil, want non-nil for malformed JSON")
	}
	if got := err.Error(); !contains(got, "failed to parse investment plan JSON") {
		t.Fatalf("error = %q, want it to contain %q", got, "failed to parse investment plan JSON")
	}
}

func TestParseInvestmentPlanInvalidDirection(t *testing.T) {
	input := `{"direction": "maybe", "conviction": 5, "key_evidence": ["a"], "acknowledged_risks": ["b"], "rationale": "test"}`

	_, err := ParseInvestmentPlan(input)
	if err == nil {
		t.Fatal("ParseInvestmentPlan() error = nil, want non-nil for invalid direction")
	}
	if got := err.Error(); !contains(got, "invalid direction") {
		t.Fatalf("error = %q, want it to contain %q", got, "invalid direction")
	}
}

func TestParseInvestmentPlanMissingDirection(t *testing.T) {
	input := `{"conviction": 5, "key_evidence": ["a"], "acknowledged_risks": ["b"], "rationale": "test"}`

	_, err := ParseInvestmentPlan(input)
	if err == nil {
		t.Fatal("ParseInvestmentPlan() error = nil, want non-nil for missing direction")
	}
	if got := err.Error(); !contains(got, "missing required field: direction") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: direction")
	}
}

func TestParseInvestmentPlanConvictionOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "conviction too low",
			input: `{"direction": "buy", "conviction": 0, "key_evidence": ["a"], "acknowledged_risks": ["b"], "rationale": "test"}`,
		},
		{
			name:  "conviction too high",
			input: `{"direction": "buy", "conviction": 11, "key_evidence": ["a"], "acknowledged_risks": ["b"], "rationale": "test"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseInvestmentPlan(tc.input)
			if err == nil {
				t.Fatal("ParseInvestmentPlan() error = nil, want non-nil for out-of-range conviction")
			}
			if got := err.Error(); !contains(got, "conviction must be 1-10") {
				t.Fatalf("error = %q, want it to contain %q", got, "conviction must be 1-10")
			}
		})
	}
}

func TestParseInvestmentPlanAllDirections(t *testing.T) {
	for _, dir := range []string{"buy", "sell", "hold"} {
		t.Run(dir, func(t *testing.T) {
			input := `{"direction": "` + dir + `", "conviction": 5, "key_evidence": ["test"], "acknowledged_risks": ["test"], "rationale": "test"}`
			plan, err := ParseInvestmentPlan(input)
			if err != nil {
				t.Fatalf("ParseInvestmentPlan() error = %v for direction %q", err, dir)
			}
			if plan.Direction != dir {
				t.Fatalf("Direction = %q, want %q", plan.Direction, dir)
			}
		})
	}
}

func TestParseInvestmentPlanWithInlineCodeFence(t *testing.T) {
	input := "```json {\"direction\": \"buy\", \"conviction\": 7, \"key_evidence\": [\"Growth\"], \"acknowledged_risks\": [\"Risk\"], \"rationale\": \"test\"}```"

	plan, err := ParseInvestmentPlan(input)
	if err != nil {
		t.Fatalf("ParseInvestmentPlan() error = %v, want nil", err)
	}
	if plan.Direction != "buy" {
		t.Fatalf("Direction = %q, want %q", plan.Direction, "buy")
	}
	if plan.Conviction != 7 {
		t.Fatalf("Conviction = %d, want 7", plan.Conviction)
	}
}

func TestParseInvestmentPlanMissingKeyEvidence(t *testing.T) {
	input := `{"direction": "buy", "conviction": 5, "key_evidence": [], "acknowledged_risks": ["risk"], "rationale": "test"}`

	_, err := ParseInvestmentPlan(input)
	if err == nil {
		t.Fatal("ParseInvestmentPlan() error = nil, want non-nil for missing key_evidence")
	}
	if got := err.Error(); !contains(got, "missing required field: key_evidence") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: key_evidence")
	}
}

func TestParseInvestmentPlanMissingAcknowledgedRisks(t *testing.T) {
	input := `{"direction": "buy", "conviction": 5, "key_evidence": ["evidence"], "acknowledged_risks": [], "rationale": "test"}`

	_, err := ParseInvestmentPlan(input)
	if err == nil {
		t.Fatal("ParseInvestmentPlan() error = nil, want non-nil for missing acknowledged_risks")
	}
	if got := err.Error(); !contains(got, "missing required field: acknowledged_risks") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: acknowledged_risks")
	}
}

func TestParseInvestmentPlanMissingRationale(t *testing.T) {
	input := `{"direction": "buy", "conviction": 5, "key_evidence": ["evidence"], "acknowledged_risks": ["risk"], "rationale": ""}`

	_, err := ParseInvestmentPlan(input)
	if err == nil {
		t.Fatal("ParseInvestmentPlan() error = nil, want non-nil for missing rationale")
	}
	if got := err.Error(); !contains(got, "missing required field: rationale") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: rationale")
	}
}

func TestParseInvestmentPlanWhitespaceOnlyRationale(t *testing.T) {
	input := `{"direction": "buy", "conviction": 5, "key_evidence": ["evidence"], "acknowledged_risks": ["risk"], "rationale": "   "}`

	_, err := ParseInvestmentPlan(input)
	if err == nil {
		t.Fatal("ParseInvestmentPlan() error = nil, want non-nil for whitespace-only rationale")
	}
	if got := err.Error(); !contains(got, "missing required field: rationale") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: rationale")
	}
}

// contains is a test helper that checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstr(s, substr)
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Verify ResearchManager satisfies the agent.Node interface at compile time.
var _ agent.Node = (*ResearchManager)(nil)
