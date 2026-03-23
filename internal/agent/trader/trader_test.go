package trader

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

type mockProvider struct {
	response *llm.CompletionResponse
	err      error
	lastReq  llm.CompletionRequest
}

func (m *mockProvider) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.lastReq = req
	return m.response, m.err
}

func TestNewTraderNilLogger(t *testing.T) {
	tr := NewTrader(nil, "openai", "model", nil)
	if tr == nil {
		t.Fatal("NewTrader() returned nil")
	}
}

func TestTraderNodeInterface(t *testing.T) {
	tr := NewTrader(nil, "openai", "model", slog.Default())

	if got := tr.Name(); got != "trader" {
		t.Fatalf("Name() = %q, want %q", got, "trader")
	}
	if got := tr.Role(); got != agent.AgentRoleTrader {
		t.Fatalf("Role() = %q, want %q", got, agent.AgentRoleTrader)
	}
	if got := tr.Phase(); got != agent.PhaseTrading {
		t.Fatalf("Phase() = %q, want %q", got, agent.PhaseTrading)
	}
}

func TestTraderExecuteStoresTradingPlanAndDecision(t *testing.T) {
	validJSON := `{
  "action": "buy",
  "ticker": "AAPL",
  "entry_type": "limit",
  "entry_price": 178.50,
  "position_size": 5000.00,
  "stop_loss": 172.00,
  "take_profit": 195.00,
  "time_horizon": "swing",
  "confidence": 0.75,
  "rationale": "Strong revenue momentum supports a long position with a favorable risk/reward profile.",
  "risk_reward": 2.54
}`

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: validJSON,
			Model:   "test-model",
			Usage: llm.CompletionUsage{
				PromptTokens:     300,
				CompletionTokens: 100,
			},
		},
	}

	tr := NewTrader(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "AAPL",
		AnalystReports: map[agent.AgentRole]string{
			agent.AgentRoleMarketAnalyst: "Trend is bullish.",
		},
		ResearchDebate: agent.ResearchDebateState{
			InvestmentPlan: `{"direction":"buy","conviction":7,"key_evidence":["Strong revenue growth"],"acknowledged_risks":["High valuation"],"rationale":"Bull case is stronger."}`,
		},
	}

	if err := tr.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Verify state.TradingPlan fields are populated correctly.
	if state.TradingPlan.Action != agent.PipelineSignalBuy {
		t.Fatalf("Action = %q, want %q", state.TradingPlan.Action, agent.PipelineSignalBuy)
	}
	if state.TradingPlan.Ticker != "AAPL" {
		t.Fatalf("Ticker = %q, want %q", state.TradingPlan.Ticker, "AAPL")
	}
	if state.TradingPlan.EntryType != "limit" {
		t.Fatalf("EntryType = %q, want %q", state.TradingPlan.EntryType, "limit")
	}
	if state.TradingPlan.EntryPrice != 178.50 {
		t.Fatalf("EntryPrice = %v, want 178.50", state.TradingPlan.EntryPrice)
	}
	if state.TradingPlan.PositionSize != 5000.00 {
		t.Fatalf("PositionSize = %v, want 5000.00", state.TradingPlan.PositionSize)
	}
	if state.TradingPlan.StopLoss != 172.00 {
		t.Fatalf("StopLoss = %v, want 172.00", state.TradingPlan.StopLoss)
	}
	if state.TradingPlan.TakeProfit != 195.00 {
		t.Fatalf("TakeProfit = %v, want 195.00", state.TradingPlan.TakeProfit)
	}
	if state.TradingPlan.TimeHorizon != "swing" {
		t.Fatalf("TimeHorizon = %q, want %q", state.TradingPlan.TimeHorizon, "swing")
	}
	if state.TradingPlan.Confidence != 0.75 {
		t.Fatalf("Confidence = %v, want 0.75", state.TradingPlan.Confidence)
	}
	if state.TradingPlan.RiskReward != 2.54 {
		t.Fatalf("RiskReward = %v, want 2.54", state.TradingPlan.RiskReward)
	}
	if state.TradingPlan.Rationale != "Strong revenue momentum supports a long position with a favorable risk/reward profile." {
		t.Fatalf("Rationale = %q, want expected", state.TradingPlan.Rationale)
	}

	// Verify decision was recorded (nil round for trader).
	decision, ok := state.Decision(agent.AgentRoleTrader, agent.PhaseTrading, nil)
	if !ok {
		t.Fatal("Decision() not found for trader")
	}
	if decision.LLMResponse == nil || decision.LLMResponse.Response == nil {
		t.Fatal("decision LLM response is nil")
	}
	if decision.LLMResponse.Response.Usage.PromptTokens != 300 {
		t.Fatalf("prompt tokens = %d, want 300", decision.LLMResponse.Response.Usage.PromptTokens)
	}
	if decision.LLMResponse.Response.Usage.CompletionTokens != 100 {
		t.Fatalf("completion tokens = %d, want 100", decision.LLMResponse.Response.Usage.CompletionTokens)
	}
	if decision.LLMResponse.Provider != "test-provider" {
		t.Fatalf("provider = %q, want %q", decision.LLMResponse.Provider, "test-provider")
	}
	if decision.LLMResponse.Response.Model != "test-model" {
		t.Fatalf("model in response = %q, want %q", decision.LLMResponse.Response.Model, "test-model")
	}

	// Verify the system prompt was the trader prompt.
	if mock.lastReq.Messages[0].Content != TraderSystemPrompt {
		t.Fatalf("system prompt mismatch:\ngot:  %q\nwant: %q", mock.lastReq.Messages[0].Content, TraderSystemPrompt)
	}

	// Verify the model was forwarded.
	if mock.lastReq.Model != "test-model" {
		t.Fatalf("model = %q, want %q", mock.lastReq.Model, "test-model")
	}
}

func TestTraderExecuteMalformedOutputStoresDefaultHoldPlan(t *testing.T) {
	malformedContent := "I recommend buying AAPL based on the investment plan."

	mock := &mockProvider{
		response: &llm.CompletionResponse{
			Content: malformedContent,
			Usage: llm.CompletionUsage{
				PromptTokens:     200,
				CompletionTokens: 30,
			},
		},
	}

	tr := NewTrader(mock, "test-provider", "test-model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "AAPL",
		ResearchDebate: agent.ResearchDebateState{
			InvestmentPlan: `{"direction":"buy","conviction":7}`,
		},
	}

	// Execute should not return an error even with malformed output.
	if err := tr.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Default hold plan should be stored.
	if state.TradingPlan.Action != agent.PipelineSignalHold {
		t.Fatalf("Action = %q, want %q", state.TradingPlan.Action, agent.PipelineSignalHold)
	}
	if state.TradingPlan.Ticker != "AAPL" {
		t.Fatalf("Ticker = %q, want %q", state.TradingPlan.Ticker, "AAPL")
	}
	if state.TradingPlan.Rationale == "" {
		t.Fatal("Rationale should contain error message")
	}

	// Decision should still be recorded with raw content.
	decision, ok := state.Decision(agent.AgentRoleTrader, agent.PhaseTrading, nil)
	if !ok {
		t.Fatal("Decision() not found for trader after malformed output")
	}
	if decision.OutputText != malformedContent {
		t.Fatalf("decision output = %q, want %q", decision.OutputText, malformedContent)
	}
}

func TestTraderExecuteNilProvider(t *testing.T) {
	tr := NewTrader(nil, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "AAPL",
		ResearchDebate: agent.ResearchDebateState{
			InvestmentPlan: `{"direction":"buy"}`,
		},
	}

	err := tr.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "trader (trading): nil llm provider"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestTraderExecuteLLMError(t *testing.T) {
	mock := &mockProvider{
		err: errors.New("service unavailable"),
	}

	tr := NewTrader(mock, "openai", "model", slog.Default())

	state := &agent.PipelineState{
		Ticker: "AAPL",
		ResearchDebate: agent.ResearchDebateState{
			InvestmentPlan: `{"direction":"buy"}`,
		},
	}

	err := tr.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("Execute() error = nil, want non-nil")
	}

	want := "trader (trading): llm completion failed: service unavailable"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// --- ParseTradingPlan unit tests ---

func TestParseTradingPlanValidJSON(t *testing.T) {
	input := `{
  "action": "buy",
  "ticker": "AAPL",
  "entry_type": "market",
  "entry_price": 180.00,
  "position_size": 10000.00,
  "stop_loss": 170.00,
  "take_profit": 200.00,
  "time_horizon": "swing",
  "confidence": 0.8,
  "rationale": "Strong momentum supports entry.",
  "risk_reward": 2.0
}`

	plan, err := ParseTradingPlan(input)
	if err != nil {
		t.Fatalf("ParseTradingPlan() error = %v, want nil", err)
	}

	if plan.Action != "buy" {
		t.Fatalf("Action = %q, want %q", plan.Action, "buy")
	}
	if plan.Ticker != "AAPL" {
		t.Fatalf("Ticker = %q, want %q", plan.Ticker, "AAPL")
	}
	if plan.EntryType != "market" {
		t.Fatalf("EntryType = %q, want %q", plan.EntryType, "market")
	}
	if plan.EntryPrice != 180.00 {
		t.Fatalf("EntryPrice = %v, want 180.00", plan.EntryPrice)
	}
	if plan.PositionSize != 10000.00 {
		t.Fatalf("PositionSize = %v, want 10000.00", plan.PositionSize)
	}
	if plan.StopLoss != 170.00 {
		t.Fatalf("StopLoss = %v, want 170.00", plan.StopLoss)
	}
	if plan.TakeProfit != 200.00 {
		t.Fatalf("TakeProfit = %v, want 200.00", plan.TakeProfit)
	}
	if plan.TimeHorizon != "swing" {
		t.Fatalf("TimeHorizon = %q, want %q", plan.TimeHorizon, "swing")
	}
	if plan.Confidence != 0.8 {
		t.Fatalf("Confidence = %v, want 0.8", plan.Confidence)
	}
	if plan.Rationale != "Strong momentum supports entry." {
		t.Fatalf("Rationale = %q, want %q", plan.Rationale, "Strong momentum supports entry.")
	}
	if plan.RiskReward != 2.0 {
		t.Fatalf("RiskReward = %v, want 2.0", plan.RiskReward)
	}
}

func TestParseTradingPlanWithCodeFences(t *testing.T) {
	input := "```json\n" + `{
  "action": "sell",
  "ticker": "TSLA",
  "entry_type": "limit",
  "entry_price": 250.00,
  "position_size": 7500.00,
  "stop_loss": 260.00,
  "take_profit": 220.00,
  "time_horizon": "position",
  "confidence": 0.65,
  "rationale": "Bearish momentum and overvaluation warrant a short.",
  "risk_reward": 3.0
}` + "\n```"

	plan, err := ParseTradingPlan(input)
	if err != nil {
		t.Fatalf("ParseTradingPlan() error = %v, want nil", err)
	}
	if plan.Action != "sell" {
		t.Fatalf("Action = %q, want %q", plan.Action, "sell")
	}
	if plan.Ticker != "TSLA" {
		t.Fatalf("Ticker = %q, want %q", plan.Ticker, "TSLA")
	}
}

func TestParseTradingPlanWithPlainCodeFences(t *testing.T) {
	input := "```\n" + `{
  "action": "hold",
  "ticker": "GOOG",
  "entry_type": "",
  "entry_price": 0,
  "position_size": 0,
  "stop_loss": 0,
  "take_profit": 0,
  "time_horizon": "",
  "confidence": 0.3,
  "rationale": "Insufficient conviction to trade.",
  "risk_reward": 0
}` + "\n```"

	plan, err := ParseTradingPlan(input)
	if err != nil {
		t.Fatalf("ParseTradingPlan() error = %v, want nil", err)
	}
	if plan.Action != "hold" {
		t.Fatalf("Action = %q, want %q", plan.Action, "hold")
	}
}

func TestParseTradingPlanWithInlineCodeFence(t *testing.T) {
	input := "```json {\"action\":\"buy\",\"ticker\":\"MSFT\",\"entry_type\":\"market\",\"entry_price\":400,\"position_size\":3000,\"stop_loss\":390,\"take_profit\":420,\"time_horizon\":\"intraday\",\"confidence\":0.6,\"rationale\":\"Short-term momentum play.\",\"risk_reward\":2.0}```"

	plan, err := ParseTradingPlan(input)
	if err != nil {
		t.Fatalf("ParseTradingPlan() error = %v, want nil", err)
	}
	if plan.Action != "buy" {
		t.Fatalf("Action = %q, want %q", plan.Action, "buy")
	}
	if plan.Ticker != "MSFT" {
		t.Fatalf("Ticker = %q, want %q", plan.Ticker, "MSFT")
	}
}

func TestParseTradingPlanMalformedJSON(t *testing.T) {
	input := "This is not valid JSON at all."

	_, err := ParseTradingPlan(input)
	if err == nil {
		t.Fatal("ParseTradingPlan() error = nil, want non-nil for malformed JSON")
	}
	if got := err.Error(); !strings.Contains(got, "failed to parse trading plan JSON") {
		t.Fatalf("error = %q, want it to contain %q", got, "failed to parse trading plan JSON")
	}
}

func TestParseTradingPlanInvalidAction(t *testing.T) {
	input := `{"action":"maybe","ticker":"AAPL","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":0.7,"rationale":"test","risk_reward":2.0}`

	_, err := ParseTradingPlan(input)
	if err == nil {
		t.Fatal("ParseTradingPlan() error = nil, want non-nil for invalid action")
	}
	if got := err.Error(); !strings.Contains(got, "invalid action") {
		t.Fatalf("error = %q, want it to contain %q", got, "invalid action")
	}
}

func TestParseTradingPlanMissingAction(t *testing.T) {
	input := `{"ticker":"AAPL","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":0.7,"rationale":"test","risk_reward":2.0}`

	_, err := ParseTradingPlan(input)
	if err == nil {
		t.Fatal("ParseTradingPlan() error = nil, want non-nil for missing action")
	}
	if got := err.Error(); !strings.Contains(got, "missing required field: action") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: action")
	}
}

func TestParseTradingPlanMissingTicker(t *testing.T) {
	input := `{"action":"buy","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":0.7,"rationale":"test","risk_reward":2.0}`

	_, err := ParseTradingPlan(input)
	if err == nil {
		t.Fatal("ParseTradingPlan() error = nil, want non-nil for missing ticker")
	}
	if got := err.Error(); !strings.Contains(got, "missing required field: ticker") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: ticker")
	}
}

func TestParseTradingPlanInvalidEntryType(t *testing.T) {
	input := `{"action":"buy","ticker":"AAPL","entry_type":"stop","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":0.7,"rationale":"test","risk_reward":2.0}`

	_, err := ParseTradingPlan(input)
	if err == nil {
		t.Fatal("ParseTradingPlan() error = nil, want non-nil for invalid entry_type")
	}
	if got := err.Error(); !strings.Contains(got, "invalid entry_type") {
		t.Fatalf("error = %q, want it to contain %q", got, "invalid entry_type")
	}
}

func TestParseTradingPlanInvalidTimeHorizon(t *testing.T) {
	input := `{"action":"buy","ticker":"AAPL","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"weekly","confidence":0.7,"rationale":"test","risk_reward":2.0}`

	_, err := ParseTradingPlan(input)
	if err == nil {
		t.Fatal("ParseTradingPlan() error = nil, want non-nil for invalid time_horizon")
	}
	if got := err.Error(); !strings.Contains(got, "invalid time_horizon") {
		t.Fatalf("error = %q, want it to contain %q", got, "invalid time_horizon")
	}
}

func TestParseTradingPlanConfidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "confidence too low",
			input: `{"action":"buy","ticker":"AAPL","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":-0.1,"rationale":"test","risk_reward":2.0}`,
		},
		{
			name:  "confidence too high",
			input: `{"action":"buy","ticker":"AAPL","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":1.5,"rationale":"test","risk_reward":2.0}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTradingPlan(tc.input)
			if err == nil {
				t.Fatal("ParseTradingPlan() error = nil, want non-nil for out-of-range confidence")
			}
			if got := err.Error(); !strings.Contains(got, "confidence must be 0.0-1.0") {
				t.Fatalf("error = %q, want it to contain %q", got, "confidence must be 0.0-1.0")
			}
		})
	}
}

func TestParseTradingPlanMissingRationale(t *testing.T) {
	input := `{"action":"buy","ticker":"AAPL","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":0.7,"rationale":"","risk_reward":2.0}`

	_, err := ParseTradingPlan(input)
	if err == nil {
		t.Fatal("ParseTradingPlan() error = nil, want non-nil for missing rationale")
	}
	if got := err.Error(); !strings.Contains(got, "missing required field: rationale") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: rationale")
	}
}

func TestParseTradingPlanWhitespaceOnlyRationale(t *testing.T) {
	input := `{"action":"buy","ticker":"AAPL","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":0.7,"rationale":"   ","risk_reward":2.0}`

	_, err := ParseTradingPlan(input)
	if err == nil {
		t.Fatal("ParseTradingPlan() error = nil, want non-nil for whitespace-only rationale")
	}
	if got := err.Error(); !strings.Contains(got, "missing required field: rationale") {
		t.Fatalf("error = %q, want it to contain %q", got, "missing required field: rationale")
	}
}

func TestParseTradingPlanHoldActionSkipsNumericValidation(t *testing.T) {
	input := `{"action":"hold","ticker":"AAPL","entry_type":"","entry_price":0,"position_size":0,"stop_loss":0,"take_profit":0,"time_horizon":"","confidence":0.3,"rationale":"No clear signal; waiting for better entry.","risk_reward":0}`

	plan, err := ParseTradingPlan(input)
	if err != nil {
		t.Fatalf("ParseTradingPlan() error = %v, want nil", err)
	}
	if plan.Action != "hold" {
		t.Fatalf("Action = %q, want %q", plan.Action, "hold")
	}
	if plan.Ticker != "AAPL" {
		t.Fatalf("Ticker = %q, want %q", plan.Ticker, "AAPL")
	}
}

func TestParseTradingPlanAllActions(t *testing.T) {
	for _, action := range []string{"buy", "sell"} {
		t.Run(action, func(t *testing.T) {
			input := `{"action":"` + action + `","ticker":"AAPL","entry_type":"market","entry_price":180,"position_size":5000,"stop_loss":170,"take_profit":200,"time_horizon":"swing","confidence":0.7,"rationale":"test","risk_reward":2.0}`
			plan, err := ParseTradingPlan(input)
			if err != nil {
				t.Fatalf("ParseTradingPlan() error = %v for action %q", err, action)
			}
			if plan.Action != action {
				t.Fatalf("Action = %q, want %q", plan.Action, action)
			}
		})
	}

	// Test hold separately since it has different validation.
	t.Run("hold", func(t *testing.T) {
		input := `{"action":"hold","ticker":"AAPL","rationale":"Wait and see."}`
		plan, err := ParseTradingPlan(input)
		if err != nil {
			t.Fatalf("ParseTradingPlan() error = %v for action hold", err)
		}
		if plan.Action != "hold" {
			t.Fatalf("Action = %q, want %q", plan.Action, "hold")
		}
	})
}

// Verify Trader satisfies the agent.Node interface at compile time.
var _ agent.Node = (*Trader)(nil)
