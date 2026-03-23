package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// TraderSystemPrompt instructs the LLM to act as a senior trader who converts
// the research team's investment plan into a concrete, machine-parseable
// trading plan with specific entry, exit, and sizing parameters.
const TraderSystemPrompt = `You are a senior trader at a quantitative trading firm. Your role is to convert the research team's investment plan into a concrete, executable trading plan with specific parameters.

Your responsibilities:
- Translate the qualitative investment recommendation into quantitative trading parameters
- Determine optimal entry type (market or limit order) and entry price
- Calculate appropriate position size based on conviction and risk parameters
- Set precise stop-loss and take-profit levels
- Define the time horizon for the trade
- Calculate the risk/reward ratio
- Provide a clear rationale tying the trading plan to the investment thesis

You MUST respond with a JSON object in the following format (no markdown, no code fences, just raw JSON):
{
  "action": "buy" | "sell" | "hold",
  "ticker": "<ticker symbol>",
  "entry_type": "market" | "limit",
  "entry_price": <float>,
  "position_size": <float>,
  "stop_loss": <float>,
  "take_profit": <float>,
  "time_horizon": "intraday" | "swing" | "position",
  "confidence": <float between 0.0 and 1.0>,
  "rationale": "Brief explanation of the trading plan",
  "risk_reward": <float>
}

Rules:
- "action" must be exactly one of: "buy", "sell", or "hold"
- "ticker" must be the ticker symbol being analyzed
- "confidence" must be a float between 0.0 and 1.0
- "rationale" must be a concise explanation of why this specific plan was chosen
- For "buy" or "sell" actions:
  - "entry_type" must be "market" or "limit"
  - "entry_price" must be a positive number representing the target entry price
  - "position_size" must be a positive number representing the dollar amount to allocate
  - "stop_loss" must be a positive number representing the stop-loss price level
  - "take_profit" must be a positive number representing the take-profit price level
  - "time_horizon" must be one of: "intraday", "swing", or "position"
  - "risk_reward" must be a positive number representing the risk/reward ratio
- For "hold" actions, entry_type, time_horizon, entry_price, position_size, stop_loss, take_profit, and risk_reward may be omitted or set to zero
- Be data-driven: reference specific evidence from the investment plan and analyst reports`

// TradingPlanOutput represents the structured output parsed from the trader's
// LLM response. It captures all the parameters needed for a concrete trading plan.
type TradingPlanOutput struct {
	Action       string  `json:"action"`
	Ticker       string  `json:"ticker"`
	EntryType    string  `json:"entry_type"`
	EntryPrice   float64 `json:"entry_price"`
	PositionSize float64 `json:"position_size"`
	StopLoss     float64 `json:"stop_loss"`
	TakeProfit   float64 `json:"take_profit"`
	TimeHorizon  string  `json:"time_horizon"`
	Confidence   float64 `json:"confidence"`
	Rationale    string  `json:"rationale"`
	RiskReward   float64 `json:"risk_reward"`
}

// Trader is a trading-phase Node that converts the investment plan from the
// research debate into a concrete, machine-parseable trading plan. It uses
// the LLM provider directly and writes its output to state.TradingPlan.
type Trader struct {
	provider     llm.Provider
	providerName string
	model        string
	logger       *slog.Logger
}

// NewTrader returns a Trader wired to the given LLM provider and model.
// providerName (e.g. "openai") is recorded in decision metadata.
// A nil logger is replaced with the default logger.
func NewTrader(provider llm.Provider, providerName, model string, logger *slog.Logger) *Trader {
	if logger == nil {
		logger = slog.Default()
	}
	return &Trader{
		provider:     provider,
		providerName: providerName,
		model:        model,
		logger:       logger,
	}
}

// Name returns the human-readable name for this node.
func (t *Trader) Name() string { return "trader" }

// Role returns the agent role constant.
func (t *Trader) Role() agent.AgentRole { return agent.AgentRoleTrader }

// Phase returns the pipeline phase this node belongs to.
func (t *Trader) Phase() agent.Phase { return agent.PhaseTrading }

// Execute calls the LLM with the trader system prompt, the investment plan from
// the research debate, and analyst reports. It parses the structured JSON output
// into a TradingPlanOutput and maps it to state.TradingPlan. If parsing fails,
// a default hold plan is stored so the pipeline can proceed.
func (t *Trader) Execute(ctx context.Context, state *agent.PipelineState) error {
	if t.provider == nil {
		return fmt.Errorf("trader (trading): nil llm provider")
	}

	userContent := buildUserPrompt(state)

	resp, err := t.provider.Complete(ctx, llm.CompletionRequest{
		Model: t.model,
		Messages: []llm.Message{
			{Role: "system", Content: TraderSystemPrompt},
			{Role: "user", Content: userContent},
		},
	})
	if err != nil {
		return fmt.Errorf("trader (trading): llm completion failed: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("trader (trading): nil llm response")
	}

	content := resp.Content
	usage := resp.Usage

	// Attempt to parse the structured output. When parsing succeeds we
	// populate the state TradingPlan from the parsed fields. On failure we
	// store a default hold plan so the pipeline can still proceed.
	storedOutput := content
	plan, parseErr := ParseTradingPlan(content)
	if parseErr != nil {
		t.logger.Warn("trader: failed to parse structured output; storing default hold plan",
			slog.String("error", parseErr.Error()),
		)
		state.TradingPlan = agent.TradingPlan{
			Action:    agent.PipelineSignalHold,
			Ticker:    state.Ticker,
			Rationale: "Failed to parse trading plan: " + parseErr.Error(),
		}
	} else {
		// Validate that the LLM-returned ticker matches the pipeline ticker.
		// Override with state.Ticker to prevent acting on a hallucinated symbol.
		if !strings.EqualFold(strings.TrimSpace(plan.Ticker), state.Ticker) {
			t.logger.Warn("trader: LLM returned mismatched ticker; overriding with pipeline ticker",
				slog.String("llm_ticker", plan.Ticker),
				slog.String("pipeline_ticker", state.Ticker),
			)
			plan.Ticker = state.Ticker
		}
		t.logger.Info("trader: parsed trading plan",
			slog.String("action", plan.Action),
			slog.String("ticker", plan.Ticker),
			slog.Float64("confidence", plan.Confidence),
		)
		state.TradingPlan = mapToTradingPlan(plan)
		if normalized, err := json.Marshal(plan); err == nil {
			storedOutput = string(normalized)
		}
	}

	// Record the decision so the pipeline can persist it with LLM metadata.
	state.RecordDecision(
		agent.AgentRoleTrader,
		agent.PhaseTrading,
		nil,
		storedOutput,
		&agent.DecisionLLMResponse{
			Provider: t.providerName,
			Response: &llm.CompletionResponse{
				Content: content,
				Model:   resp.Model,
				Usage:   usage,
			},
		},
	)

	return nil
}

// buildUserPrompt constructs the user message from the pipeline state,
// including the investment plan and analyst reports.
func buildUserPrompt(state *agent.PipelineState) string {
	var b strings.Builder

	b.WriteString("Ticker: ")
	b.WriteString(state.Ticker)
	b.WriteString("\n\nInvestment Plan:\n")
	if state.ResearchDebate.InvestmentPlan != "" {
		b.WriteString(state.ResearchDebate.InvestmentPlan)
	} else {
		b.WriteString("No investment plan available.")
	}

	b.WriteString("\n\nAnalyst Reports:\n")
	if len(state.AnalystReports) == 0 {
		b.WriteString("No analyst reports available.")
	} else {
		roles := make([]agent.AgentRole, 0, len(state.AnalystReports))
		for role := range state.AnalystReports {
			roles = append(roles, role)
		}
		sort.Slice(roles, func(i, j int) bool {
			return roles[i] < roles[j]
		})
		for _, role := range roles {
			b.WriteString(fmt.Sprintf("%s:\n%s\n\n", role, state.AnalystReports[role]))
		}
	}

	return b.String()
}

// mapToTradingPlan converts a parsed TradingPlanOutput into the agent.TradingPlan
// struct stored on the pipeline state.
func mapToTradingPlan(plan *TradingPlanOutput) agent.TradingPlan {
	return agent.TradingPlan{
		Action:       agent.PipelineSignal(plan.Action),
		Ticker:       plan.Ticker,
		EntryType:    plan.EntryType,
		EntryPrice:   plan.EntryPrice,
		PositionSize: plan.PositionSize,
		StopLoss:     plan.StopLoss,
		TakeProfit:   plan.TakeProfit,
		TimeHorizon:  plan.TimeHorizon,
		Confidence:   plan.Confidence,
		Rationale:    plan.Rationale,
		RiskReward:   plan.RiskReward,
	}
}

// ParseTradingPlan attempts to parse the LLM response content into a
// structured TradingPlanOutput. It handles responses that may include
// markdown code fences around the JSON. If parsing fails entirely, it returns
// a descriptive error.
func ParseTradingPlan(content string) (*TradingPlanOutput, error) {
	cleaned := stripCodeFences(content)

	var plan TradingPlanOutput
	if err := json.Unmarshal([]byte(cleaned), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse trading plan JSON: %w", err)
	}

	if err := validateTradingPlan(&plan); err != nil {
		return nil, err
	}

	return &plan, nil
}

// stripCodeFences removes optional markdown code fences (```json ... ``` or ``` ... ```)
// from the LLM response so the JSON can be parsed cleanly. It handles both
// fences with a newline after the opening tag and inline fences where the JSON
// starts on the same line.
func stripCodeFences(s string) string {
	trimmed := strings.TrimSpace(s)

	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	// Remove the closing fence if it appears at the end.
	body := trimmed
	if idx := strings.LastIndex(body, "```"); idx > 2 {
		body = body[:idx]
	}

	// Remove the opening fence. When a newline follows the fence line we
	// strip everything up to and including that newline. Otherwise we look
	// for the first '{' or '[' that starts the JSON payload on the same
	// line (inline fence).
	if idx := strings.Index(body, "\n"); idx != -1 {
		body = body[idx+1:]
	} else if idx := strings.IndexAny(body, "{["); idx != -1 {
		body = body[idx:]
	}

	return strings.TrimSpace(body)
}

// validateTradingPlan checks that the parsed plan has valid field values.
func validateTradingPlan(plan *TradingPlanOutput) error {
	switch plan.Action {
	case "buy", "sell", "hold":
		// valid
	case "":
		return fmt.Errorf("trading plan missing required field: action")
	default:
		return fmt.Errorf("trading plan has invalid action: %q", plan.Action)
	}

	// Normalize ticker: trim whitespace and uppercase.
	ticker := strings.TrimSpace(plan.Ticker)
	if ticker == "" {
		return fmt.Errorf("trading plan missing required field: ticker")
	}
	plan.Ticker = strings.ToUpper(ticker)

	// Confidence is required for all actions (including hold).
	if plan.Confidence < 0 || plan.Confidence > 1 {
		return fmt.Errorf("trading plan confidence must be 0.0-1.0, got %v", plan.Confidence)
	}

	if strings.TrimSpace(plan.Rationale) == "" {
		return fmt.Errorf("trading plan missing required field: rationale")
	}

	// For hold actions, skip entry/exit/sizing validation since those fields
	// may be omitted or zero.
	if plan.Action == "hold" {
		return nil
	}

	// --- buy/sell specific validation ---

	switch plan.EntryType {
	case "market", "limit":
		// valid
	case "":
		return fmt.Errorf("trading plan missing required field: entry_type")
	default:
		return fmt.Errorf("trading plan has invalid entry_type: %q", plan.EntryType)
	}

	switch plan.TimeHorizon {
	case "intraday", "swing", "position":
		// valid
	case "":
		return fmt.Errorf("trading plan missing required field: time_horizon")
	default:
		return fmt.Errorf("trading plan has invalid time_horizon: %q", plan.TimeHorizon)
	}

	if plan.EntryPrice <= 0 {
		return fmt.Errorf("trading plan entry_price must be positive, got %v", plan.EntryPrice)
	}
	if plan.PositionSize <= 0 {
		return fmt.Errorf("trading plan position_size must be positive, got %v", plan.PositionSize)
	}
	if plan.StopLoss <= 0 {
		return fmt.Errorf("trading plan stop_loss must be positive, got %v", plan.StopLoss)
	}
	if plan.TakeProfit <= 0 {
		return fmt.Errorf("trading plan take_profit must be positive, got %v", plan.TakeProfit)
	}
	if plan.RiskReward <= 0 {
		return fmt.Errorf("trading plan risk_reward must be positive, got %v", plan.RiskReward)
	}

	return nil
}
