package risk

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func TestNewRiskManagerNilLogger(t *testing.T) {
	rm := NewRiskManager(nil, "openai", "model", nil)
	if rm == nil {
		t.Fatal("NewRiskManager() returned nil")
	}
}

func TestRiskManagerNodeInterface(t *testing.T) {
	rm := NewRiskManager(nil, "openai", "model", slog.Default())

	if got := rm.Name(); got != "risk_manager" {
		t.Fatalf("Name() = %q, want %q", got, "risk_manager")
	}
	if got := rm.Role(); got != agent.AgentRoleRiskManager {
		t.Fatalf("Role() = %q, want %q", got, agent.AgentRoleRiskManager)
	}
	if got := rm.Phase(); got != agent.PhaseRiskDebate {
		t.Fatalf("Phase() = %q, want %q", got, agent.PhaseRiskDebate)
	}
}

func TestRiskManagerExecuteValidBuySignal(t *testing.T) {
	validJSON := `{
  "action": "BUY",
  "confidence": 8,
  "adjusted_position_size": 75,
  "adjusted_stop_loss": 235.00,
  "reasoning": "Aggressive analyst's upside case is strong, but conservative concerns warrant reduced size."
}`

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: validJSON,
			Usage: llm.CompletionUsage{
				PromptTokens:     300,
				CompletionTokens: 90,
			},
		},
	}

	rm := NewRiskManager(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "TSLA",
		TradingPlan: agent.TradingPlan{
			Action:       agent.PipelineSignalBuy,
			Ticker:       "TSLA",
			EntryPrice:   250.00,
			PositionSize: 100,
			StopLoss:     240.00,
			TakeProfit:   280.00,
			Confidence:   0.8,
			RiskReward:   3.0,
			Rationale:    "Strong momentum with breakout pattern.",
		},
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{
					Number: 1,
					Contributions: map[agent.AgentRole]string{
						agent.AgentRoleAggressiveAnalyst:   "Increase position to capture full upside.",
						agent.AgentRoleConservativeAnalyst: "Reduce position due to volatility risk.",
						agent.AgentRoleNeutralAnalyst:      "Moderate position aligns with expected value.",
					},
				},
			},
		},
	}

	if err := rm.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify FinalSignal was populated.
	if state.FinalSignal.Signal != agent.PipelineSignalBuy {
		t.Fatalf("FinalSignal.Signal = %q, want %q", state.FinalSignal.Signal, agent.PipelineSignalBuy)
	}
	if state.FinalSignal.Confidence != 0.8 {
		t.Fatalf("FinalSignal.Confidence = %v, want 0.8", state.FinalSignal.Confidence)
	}

	// Verify TradingPlan adjustments.
	if state.TradingPlan.PositionSize != 75 {
		t.Fatalf("TradingPlan.PositionSize = %v, want 75", state.TradingPlan.PositionSize)
	}
	if state.TradingPlan.StopLoss != 235.00 {
		t.Fatalf("TradingPlan.StopLoss = %v, want 235", state.TradingPlan.StopLoss)
	}

	// Verify RiskDebate.FinalSignal stores normalized JSON.
	if state.RiskDebate.FinalSignal == "" {
		t.Fatal("RiskDebate.FinalSignal is empty")
	}
	if !strings.Contains(state.RiskDebate.FinalSignal, `"action":"BUY"`) {
		t.Fatalf("RiskDebate.FinalSignal = %q, want it to contain normalized action", state.RiskDebate.FinalSignal)
	}

	// Verify decision was recorded (no round number for manager).
	decision, ok := state.Decision(agent.AgentRoleRiskManager, agent.PhaseRiskDebate, nil)
	if !ok {
		t.Fatal("Decision() not found for risk_manager")
	}
	if decision.LLMResponse == nil || decision.LLMResponse.Response == nil {
		t.Fatal("decision LLM response is nil")
	}
	if decision.LLMResponse.Response.Usage.PromptTokens != 300 {
		t.Fatalf("prompt tokens = %d, want 300", decision.LLMResponse.Response.Usage.PromptTokens)
	}
	if decision.LLMResponse.Response.Usage.CompletionTokens != 90 {
		t.Fatalf("completion tokens = %d, want 90", decision.LLMResponse.Response.Usage.CompletionTokens)
	}
	if decision.LLMResponse.Provider != "test-provider" {
		t.Fatalf("provider = %q, want %q", decision.LLMResponse.Provider, "test-provider")
	}
	if decision.LLMResponse.Response.Model != "test-model" {
		t.Fatalf("model in response = %q, want %q", decision.LLMResponse.Response.Model, "test-model")
	}

	// Verify the system prompt was the risk manager prompt.
	if mock.lastReq.Messages[0].Content != RiskManagerSystemPrompt {
		t.Fatalf("system prompt mismatch:\ngot:  %q\nwant: %q", mock.lastReq.Messages[0].Content, RiskManagerSystemPrompt)
	}

	// Verify the model was forwarded.
	if mock.lastReq.Model != "test-model" {
		t.Fatalf("model = %q, want %q", mock.lastReq.Model, "test-model")
	}

	// Verify the trading plan is included in the user message context.
	userMsg := mock.lastReq.Messages[1].Content
	if len(userMsg) == 0 {
		t.Fatal("user message is empty")
	}
	if !strings.Contains(userMsg, "trader") {
		t.Fatalf("user message should reference trader role, got: %q", userMsg)
	}
}

func TestRiskManagerExecuteHoldSignalNoAdjustments(t *testing.T) {
	holdJSON := `{
  "action": "HOLD",
  "confidence": 4,
  "adjusted_position_size": 0,
  "adjusted_stop_loss": 0,
  "reasoning": "Risk/reward is unfavorable at current levels. Wait for better entry."
}`

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: holdJSON,
			Usage: llm.CompletionUsage{
				PromptTokens:     250,
				CompletionTokens: 50,
			},
		},
	}

	rm := NewRiskManager(mock, "test-provider", "test-model", slog.Default())

	originalPositionSize := 100.0
	originalStopLoss := 240.0

	state := &agent.PipelineState{
		Ticker: "TSLA",
		TradingPlan: agent.TradingPlan{
			Action:       agent.PipelineSignalBuy,
			Ticker:       "TSLA",
			EntryPrice:   250.00,
			PositionSize: originalPositionSize,
			StopLoss:     originalStopLoss,
			TakeProfit:   280.00,
			Confidence:   0.8,
			RiskReward:   3.0,
			Rationale:    "Strong momentum with breakout pattern.",
		},
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{
					Number:        1,
					Contributions: make(map[agent.AgentRole]string),
				},
			},
		},
	}

	if err := rm.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify FinalSignal is HOLD.
	if state.FinalSignal.Signal != agent.PipelineSignalHold {
		t.Fatalf("FinalSignal.Signal = %q, want %q", state.FinalSignal.Signal, agent.PipelineSignalHold)
	}
	if state.FinalSignal.Confidence != 0.4 {
		t.Fatalf("FinalSignal.Confidence = %v, want 0.4", state.FinalSignal.Confidence)
	}

	// HOLD should NOT modify the TradingPlan.
	if state.TradingPlan.PositionSize != originalPositionSize {
		t.Fatalf("TradingPlan.PositionSize = %v, want %v (unchanged)", state.TradingPlan.PositionSize, originalPositionSize)
	}
	if state.TradingPlan.StopLoss != originalStopLoss {
		t.Fatalf("TradingPlan.StopLoss = %v, want %v (unchanged)", state.TradingPlan.StopLoss, originalStopLoss)
	}

	// Verify RiskDebate.FinalSignal was stored.
	if state.RiskDebate.FinalSignal == "" {
		t.Fatal("RiskDebate.FinalSignal is empty")
	}
	if !strings.Contains(state.RiskDebate.FinalSignal, `"action":"HOLD"`) {
		t.Fatalf("RiskDebate.FinalSignal = %q, want it to contain HOLD", state.RiskDebate.FinalSignal)
	}
}

func TestRiskManagerExecuteMalformedResponseStoresRawContent(t *testing.T) {
	malformedContent := "I recommend holding the position due to unclear risk factors."

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: malformedContent,
			Usage: llm.CompletionUsage{
				PromptTokens:     200,
				CompletionTokens: 30,
			},
		},
	}

	rm := NewRiskManager(mock, "test-provider", "test-model", slog.Default())

	originalPositionSize := 100.0
	originalStopLoss := 240.0

	state := &agent.PipelineState{
		Ticker: "TSLA",
		TradingPlan: agent.TradingPlan{
			Action:       agent.PipelineSignalBuy,
			Ticker:       "TSLA",
			PositionSize: originalPositionSize,
			StopLoss:     originalStopLoss,
		},
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{
					Number:        1,
					Contributions: make(map[agent.AgentRole]string),
				},
			},
		},
	}

	// Execute should not return an error even with malformed output.
	if err := rm.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Raw content should be stored in RiskDebate.FinalSignal.
	if state.RiskDebate.FinalSignal != malformedContent {
		t.Fatalf("RiskDebate.FinalSignal = %q, want %q", state.RiskDebate.FinalSignal, malformedContent)
	}

	// FinalSignal should not be modified on parse failure.
	if state.FinalSignal.Signal != "" {
		t.Fatalf("FinalSignal.Signal = %q, want empty on parse failure", state.FinalSignal.Signal)
	}

	// TradingPlan should not be modified on parse failure.
	if state.TradingPlan.PositionSize != originalPositionSize {
		t.Fatalf("TradingPlan.PositionSize = %v, want %v (unchanged)", state.TradingPlan.PositionSize, originalPositionSize)
	}
	if state.TradingPlan.StopLoss != originalStopLoss {
		t.Fatalf("TradingPlan.StopLoss = %v, want %v (unchanged)", state.TradingPlan.StopLoss, originalStopLoss)
	}

	// Decision should still be recorded.
	decision, ok := state.Decision(agent.AgentRoleRiskManager, agent.PhaseRiskDebate, nil)
	if !ok {
		t.Fatal("Decision() not found for risk_manager after malformed output")
	}
	if decision.OutputText != malformedContent {
		t.Fatalf("decision output = %q, want %q", decision.OutputText, malformedContent)
	}
}

func TestRiskManagerExecuteNilProvider(t *testing.T) {
	rm := NewRiskManager(nil, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := rm.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "risk_manager (risk_debate): nil llm provider"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestRiskManagerExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("service unavailable"),
	}

	rm := NewRiskManager(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	err := rm.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "risk_manager (risk_debate): llm completion failed: service unavailable"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestRiskManagerExecuteNoRounds(t *testing.T) {
	validJSON := `{"action": "HOLD", "confidence": 3, "adjusted_position_size": 0, "adjusted_stop_loss": 0, "reasoning": "Without debate, defaulting to hold."}`

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: validJSON,
			Usage:   llm.CompletionUsage{PromptTokens: 10, CompletionTokens: 5},
		},
	}

	rm := NewRiskManager(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		RiskDebate: agent.RiskDebateState{},
	}

	if err := rm.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Normalized JSON should be stored.
	if state.RiskDebate.FinalSignal == "" {
		t.Fatal("RiskDebate.FinalSignal is empty")
	}
	if !strings.Contains(state.RiskDebate.FinalSignal, `"action":"HOLD"`) {
		t.Fatalf("RiskDebate.FinalSignal = %q, want it to contain HOLD", state.RiskDebate.FinalSignal)
	}

	// Decision should be recorded (nil round for manager).
	decision, ok := state.Decision(agent.AgentRoleRiskManager, agent.PhaseRiskDebate, nil)
	if !ok {
		t.Fatal("Decision() not found for risk_manager with no rounds")
	}
	if decision.OutputText == "" {
		t.Fatal("decision output is empty")
	}
}

// --- ParseFinalSignal unit tests ---

func TestParseFinalSignalValidJSON(t *testing.T) {
	input := `{
  "action": "BUY",
  "confidence": 8,
  "adjusted_position_size": 75,
  "adjusted_stop_loss": 235.00,
  "reasoning": "Balanced view supports reduced-size long position."
}`

	signal, err := ParseFinalSignal(input)
	if err != nil {
		t.Fatalf("ParseFinalSignal() error = %v, want nil", err)
	}

	if signal.Action != "BUY" {
		t.Fatalf("Action = %q, want %q", signal.Action, "BUY")
	}
	if signal.Confidence != 8 {
		t.Fatalf("Confidence = %d, want 8", signal.Confidence)
	}
	if signal.AdjustedPositionSize != 75 {
		t.Fatalf("AdjustedPositionSize = %v, want 75", signal.AdjustedPositionSize)
	}
	if signal.AdjustedStopLoss != 235.00 {
		t.Fatalf("AdjustedStopLoss = %v, want 235", signal.AdjustedStopLoss)
	}
	if signal.Reasoning != "Balanced view supports reduced-size long position." {
		t.Fatalf("Reasoning = %q, want %q", signal.Reasoning, "Balanced view supports reduced-size long position.")
	}
}

func TestParseFinalSignalWithCodeFences(t *testing.T) {
	input := "```json\n" + `{
  "action": "SELL",
  "confidence": 6,
  "adjusted_position_size": 50,
  "adjusted_stop_loss": 260.00,
  "reasoning": "Bearish momentum warrants exit."
}` + "\n```"

	signal, err := ParseFinalSignal(input)
	if err != nil {
		t.Fatalf("ParseFinalSignal() error = %v, want nil", err)
	}
	if signal.Action != "SELL" {
		t.Fatalf("Action = %q, want %q", signal.Action, "SELL")
	}
	if signal.Confidence != 6 {
		t.Fatalf("Confidence = %d, want 6", signal.Confidence)
	}
}

func TestParseFinalSignalWithPlainCodeFences(t *testing.T) {
	input := "```\n" + `{
  "action": "HOLD",
  "confidence": 5,
  "adjusted_position_size": 0,
  "adjusted_stop_loss": 0,
  "reasoning": "Wait for clarity."
}` + "\n```"

	signal, err := ParseFinalSignal(input)
	if err != nil {
		t.Fatalf("ParseFinalSignal() error = %v, want nil", err)
	}
	if signal.Action != "HOLD" {
		t.Fatalf("Action = %q, want %q", signal.Action, "HOLD")
	}
}

func TestParseFinalSignalMalformedJSON(t *testing.T) {
	input := "This is not valid JSON at all."

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for malformed JSON")
	}
	if got := err.Error(); !strings.Contains(got, "failed to parse JSON") {
		t.Fatalf("error = %q, want it to contain %q", got, "failed to parse JSON")
	}
}

func TestParseFinalSignalInvalidAction(t *testing.T) {
	input := `{"action": "MAYBE", "confidence": 5, "adjusted_position_size": 0, "adjusted_stop_loss": 0, "reasoning": "test"}`

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for invalid action")
	}
	if got := err.Error(); !strings.Contains(got, "invalid action") {
		t.Fatalf("error = %q, want it to contain %q", got, "invalid action")
	}
}

func TestParseFinalSignalMissingAction(t *testing.T) {
	input := `{"confidence": 5, "adjusted_position_size": 0, "adjusted_stop_loss": 0, "reasoning": "test"}`

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for missing action")
	}
	if got := err.Error(); !strings.Contains(got, "missing required field: action") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: action")
	}
}

func TestParseFinalSignalConfidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "confidence too low",
			input: `{"action": "BUY", "confidence": 0, "adjusted_position_size": 50, "adjusted_stop_loss": 240, "reasoning": "test"}`,
		},
		{
			name:  "confidence too high",
			input: `{"action": "BUY", "confidence": 11, "adjusted_position_size": 50, "adjusted_stop_loss": 240, "reasoning": "test"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFinalSignal(tc.input)
			if err == nil {
				t.Fatal("ParseFinalSignal() error = nil, want non-nil for out-of-range confidence")
			}
			if got := err.Error(); !strings.Contains(got, "confidence must be 1-10") {
				t.Fatalf("error = %q, want it to contain %q", got, "confidence must be 1-10")
			}
		})
	}
}

func TestParseFinalSignalAllActions(t *testing.T) {
	tests := []struct {
		action string
		input  string
	}{
		{
			action: "BUY",
			input:  `{"action": "BUY", "confidence": 5, "adjusted_position_size": 50, "adjusted_stop_loss": 240, "reasoning": "test"}`,
		},
		{
			action: "SELL",
			input:  `{"action": "SELL", "confidence": 5, "adjusted_position_size": 50, "adjusted_stop_loss": 240, "reasoning": "test"}`,
		},
		{
			action: "HOLD",
			input:  `{"action": "HOLD", "confidence": 5, "adjusted_position_size": 0, "adjusted_stop_loss": 0, "reasoning": "test"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.action, func(t *testing.T) {
			signal, err := ParseFinalSignal(tc.input)
			if err != nil {
				t.Fatalf("ParseFinalSignal() error = %v for action %q", err, tc.action)
			}
			if signal.Action != tc.action {
				t.Fatalf("Action = %q, want %q", signal.Action, tc.action)
			}
		})
	}
}

func TestParseFinalSignalLowercaseActionNormalized(t *testing.T) {
	input := `{"action": "buy", "confidence": 7, "adjusted_position_size": 50, "adjusted_stop_loss": 240, "reasoning": "test"}`

	signal, err := ParseFinalSignal(input)
	if err != nil {
		t.Fatalf("ParseFinalSignal() error = %v, want nil", err)
	}
	if signal.Action != "BUY" {
		t.Fatalf("Action = %q, want %q (normalized to uppercase)", signal.Action, "BUY")
	}
}

func TestParseFinalSignalMissingReasoning(t *testing.T) {
	input := `{"action": "BUY", "confidence": 5, "adjusted_position_size": 50, "adjusted_stop_loss": 240, "reasoning": ""}`

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for missing reasoning")
	}
	if got := err.Error(); !strings.Contains(got, "missing required field: reasoning") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: reasoning")
	}
}

func TestParseFinalSignalWhitespaceOnlyReasoning(t *testing.T) {
	input := `{"action": "BUY", "confidence": 5, "adjusted_position_size": 50, "adjusted_stop_loss": 240, "reasoning": "   "}`

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for whitespace-only reasoning")
	}
	if got := err.Error(); !strings.Contains(got, "missing required field: reasoning") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: reasoning")
	}
}

func TestParseFinalSignalWithInlineCodeFence(t *testing.T) {
	input := "```json {\"action\": \"BUY\", \"confidence\": 7, \"adjusted_position_size\": 50, \"adjusted_stop_loss\": 240, \"reasoning\": \"test\"}```"

	signal, err := ParseFinalSignal(input)
	if err != nil {
		t.Fatalf("ParseFinalSignal() error = %v, want nil", err)
	}
	if signal.Action != "BUY" {
		t.Fatalf("Action = %q, want %q", signal.Action, "BUY")
	}
	if signal.Confidence != 7 {
		t.Fatalf("Confidence = %d, want 7", signal.Confidence)
	}
}

func TestRiskManagerExecuteSellSignalAdjustsTradingPlan(t *testing.T) {
	sellJSON := `{
  "action": "SELL",
  "confidence": 7,
  "adjusted_position_size": 60,
  "adjusted_stop_loss": 265.00,
  "reasoning": "Bear case prevails. Exit with adjusted stop."
}`

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: sellJSON,
			Usage:   llm.CompletionUsage{PromptTokens: 200, CompletionTokens: 40},
		},
	}

	rm := NewRiskManager(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "TSLA",
		TradingPlan: agent.TradingPlan{
			Action:       agent.PipelineSignalSell,
			Ticker:       "TSLA",
			EntryPrice:   250.00,
			PositionSize: 100,
			StopLoss:     260.00,
		},
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{Number: 1, Contributions: make(map[agent.AgentRole]string)},
			},
		},
	}

	if err := rm.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	if state.FinalSignal.Signal != agent.PipelineSignalSell {
		t.Fatalf("FinalSignal.Signal = %q, want %q", state.FinalSignal.Signal, agent.PipelineSignalSell)
	}
	if state.TradingPlan.PositionSize != 60 {
		t.Fatalf("TradingPlan.PositionSize = %v, want 60", state.TradingPlan.PositionSize)
	}
	if state.TradingPlan.StopLoss != 265.00 {
		t.Fatalf("TradingPlan.StopLoss = %v, want 265", state.TradingPlan.StopLoss)
	}
}

func TestParseFinalSignalBuyZeroPositionSize(t *testing.T) {
	input := `{"action": "BUY", "confidence": 7, "adjusted_position_size": 0, "adjusted_stop_loss": 240, "reasoning": "test"}`

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for BUY with zero position size")
	}
	if got := err.Error(); !strings.Contains(got, "adjusted_position_size > 0") {
		t.Fatalf("error = %q, want it to contain %q", got, "adjusted_position_size > 0")
	}
}

func TestParseFinalSignalSellZeroStopLoss(t *testing.T) {
	input := `{"action": "SELL", "confidence": 7, "adjusted_position_size": 50, "adjusted_stop_loss": 0, "reasoning": "test"}`

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for SELL with zero stop loss")
	}
	if got := err.Error(); !strings.Contains(got, "adjusted_stop_loss > 0") {
		t.Fatalf("error = %q, want it to contain %q", got, "adjusted_stop_loss > 0")
	}
}

func TestParseFinalSignalHoldNonZeroPositionSize(t *testing.T) {
	input := `{"action": "HOLD", "confidence": 5, "adjusted_position_size": 50, "adjusted_stop_loss": 0, "reasoning": "test"}`

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for HOLD with non-zero position size")
	}
	if got := err.Error(); !strings.Contains(got, "adjusted_position_size = 0") {
		t.Fatalf("error = %q, want it to contain %q", got, "adjusted_position_size = 0")
	}
}

func TestParseFinalSignalHoldNonZeroStopLoss(t *testing.T) {
	input := `{"action": "HOLD", "confidence": 5, "adjusted_position_size": 0, "adjusted_stop_loss": 240, "reasoning": "test"}`

	_, err := ParseFinalSignal(input)
	if err == nil {
		t.Fatal("ParseFinalSignal() error = nil, want non-nil for HOLD with non-zero stop loss")
	}
	if got := err.Error(); !strings.Contains(got, "adjusted_stop_loss = 0") {
		t.Fatalf("error = %q, want it to contain %q", got, "adjusted_stop_loss = 0")
	}
}

// Verify RiskManager satisfies the agent.Node interface at compile time.
var _ agent.Node = (*RiskManager)(nil)
