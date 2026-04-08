package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/parse"
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
  "risk_reward": <float>,
  "watch_terms": ["term1", "term2"],
  "thesis_summary": "One-sentence thesis summary for signal monitoring",
  "conviction": <float between 0.0 and 1.0>,
  "invalidation_conditions": ["condition1", "condition2"]
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
- Be data-driven: reference specific evidence from the investment plan and analyst reports
- "watch_terms": 3–8 keywords or phrases that, if seen in news or social media, indicate this thesis is being affected. Include the ticker, company name, key catalysts, and relevant macro terms.
- "thesis_summary": a single sentence summarising why this trade makes sense, suitable for display in a monitoring dashboard.
- "conviction": your confidence in the thesis independent of position sizing, 0.0–1.0.
- "invalidation_conditions": 1–4 natural language conditions that would invalidate this thesis (e.g. "price breaks below $X support", "earnings miss consensus by >10%").`

// TradingPlanOutput represents the structured output parsed from the trader's
// LLM response. It captures all the parameters needed for a concrete trading plan
// plus thesis fields used by the signal intelligence layer.
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
	// Polymarket-only: the token side the plan is acting on.
	Side string `json:"side,omitempty"`
	// Thesis fields — populated alongside the trading plan.
	WatchTerms            []string `json:"watch_terms,omitempty"`
	ThesisSummary         string   `json:"thesis_summary,omitempty"`
	Conviction            float64  `json:"conviction,omitempty"`
	InvalidationConditions []string `json:"invalidation_conditions,omitempty"`
}

// Trader is a trading-phase Node that converts the investment plan from the
// research debate into a concrete, machine-parseable trading plan. It uses
// the LLM provider directly and writes its output to state.TradingPlan.
type Trader struct {
	provider     llm.Provider
	providerName string
	model        string
	systemPrompt string
	logger       *slog.Logger
}

// Compile-time check: *Trader implements agent.TraderNode.
var _ agent.TraderNode = (*Trader)(nil)

// NewTrader returns a Trader wired to the given LLM provider and model.
// providerName (e.g. "openai") is recorded in decision metadata.
// A nil logger is replaced with the default logger.
func NewTrader(provider llm.Provider, providerName, model string, logger *slog.Logger) *Trader {
	return NewTraderWithPrompt(provider, providerName, model, "", logger)
}

// NewTraderWithPrompt returns a Trader wired to the given LLM provider and
// model, using systemPrompt when provided.
func NewTraderWithPrompt(provider llm.Provider, providerName, model, systemPrompt string, logger *slog.Logger) *Trader {
	if logger == nil {
		logger = slog.Default()
	}
	if systemPrompt == "" {
		systemPrompt = TraderSystemPrompt
	}
	return &Trader{
		provider:     provider,
		providerName: providerName,
		model:        model,
		systemPrompt: systemPrompt,
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
	input := agent.TradingInput{
		Ticker:         state.Ticker,
		InvestmentPlan: state.ResearchDebate.InvestmentPlan,
		AnalystReports: state.AnalystReports,
	}
	output, err := t.Trade(ctx, input)
	if err != nil {
		return err
	}
	state.TradingPlan = output.Plan
	if output.Thesis != nil {
		output.Thesis.PipelineRunID = state.PipelineRunID
		state.ActiveThesis = output.Thesis
	}
	state.RecordDecision(agent.AgentRoleTrader, agent.PhaseTrading, nil, output.StoredOutput, output.LLMResponse)
	return nil
}

// Trade implements the TraderNode interface. It calls the LLM with the trader
// system prompt, the investment plan, and analyst reports and returns a typed
// TradingOutput with the parsed trading plan.
func (t *Trader) Trade(ctx context.Context, input agent.TradingInput) (agent.TradingOutput, error) {
	if t.provider == nil {
		return agent.TradingOutput{}, fmt.Errorf("trader (trading): nil llm provider")
	}

	userContent := buildUserPromptFromInput(input)
	messages := []llm.Message{
		{Role: "system", Content: t.systemPrompt},
		{Role: "user", Content: userContent},
	}
	promptText := agent.PromptTextFromMessages(messages)

	resp, err := t.provider.Complete(ctx, llm.CompletionRequest{
		Model:          t.model,
		Messages:       messages,
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	})
	if err != nil {
		return agent.TradingOutput{}, fmt.Errorf("trader (trading): llm completion failed: %w", err)
	}
	if resp == nil {
		return agent.TradingOutput{}, fmt.Errorf("trader (trading): nil llm response")
	}

	content := resp.Content
	usage := resp.Usage

	// Attempt to parse the structured output. When parsing succeeds we
	// populate the TradingPlan from the parsed fields. On failure we
	// store a default hold plan so the pipeline can still proceed.
	storedOutput := content
	var tradingPlan agent.TradingPlan
	plan, parseErr := ParseTradingPlan(content)
	if parseErr != nil {
		t.logger.Warn("trader: failed to parse structured output; storing default hold plan",
			slog.String("error", parseErr.Error()),
		)
		tradingPlan = agent.TradingPlan{
			Action:    agent.PipelineSignalHold,
			Ticker:    input.Ticker,
			Rationale: "Failed to parse trading plan: " + parseErr.Error(),
		}
	} else {
		// Validate that the LLM-returned ticker matches the pipeline ticker.
		// Override with input.Ticker to prevent acting on a hallucinated symbol.
		if !strings.EqualFold(strings.TrimSpace(plan.Ticker), input.Ticker) {
			t.logger.Warn("trader: LLM returned mismatched ticker; overriding with pipeline ticker",
				slog.String("llm_ticker", plan.Ticker),
				slog.String("pipeline_ticker", input.Ticker),
			)
			plan.Ticker = input.Ticker
		}
		t.logger.Info("trader: parsed trading plan",
			slog.String("action", plan.Action),
			slog.String("ticker", plan.Ticker),
			slog.Float64("confidence", plan.Confidence),
		)
		tradingPlan = mapToTradingPlan(plan)
		if normalized, err := json.Marshal(plan); err == nil {
			storedOutput = string(normalized)
		}
	}

	var thesis *agent.Thesis
	if plan != nil {
		thesis = mapToThesis(plan)
	}

	return agent.TradingOutput{
		Plan:         tradingPlan,
		Thesis:       thesis,
		StoredOutput: storedOutput,
		LLMResponse: &agent.DecisionLLMResponse{
			Provider:   t.providerName,
			PromptText: promptText,
			Response: &llm.CompletionResponse{
				Content: content,
				Model:   resp.Model,
				Usage:   usage,
			},
		},
	}, nil
}

// buildUserPromptFromInput constructs the user message from a TradingInput,
// including the investment plan and analyst reports.
func buildUserPromptFromInput(input agent.TradingInput) string {
	var b strings.Builder

	b.WriteString("Ticker: ")
	b.WriteString(input.Ticker)
	b.WriteString("\n\nInvestment Plan:\n")
	if input.InvestmentPlan != "" {
		b.WriteString(input.InvestmentPlan)
	} else {
		b.WriteString("No investment plan available.")
	}

	b.WriteString("\n\nAnalyst Reports:\n")
	if len(input.AnalystReports) == 0 {
		b.WriteString("No analyst reports available.")
	} else {
		roles := make([]agent.AgentRole, 0, len(input.AnalystReports))
		for role := range input.AnalystReports {
			roles = append(roles, role)
		}
		sort.Slice(roles, func(i, j int) bool {
			return roles[i] < roles[j]
		})
		for _, role := range roles {
			_, _ = fmt.Fprintf(&b, "%s:\n%s\n\n", role, input.AnalystReports[role])
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
		Side:         plan.Side,
	}
}

// mapToThesis converts thesis fields from a parsed TradingPlanOutput into an
// agent.Thesis. Returns nil when no meaningful thesis fields were populated by the LLM.
// The caller is responsible for setting PipelineRunID after the fact.
func mapToThesis(plan *TradingPlanOutput) *agent.Thesis {
	if plan.ThesisSummary == "" && len(plan.WatchTerms) == 0 {
		return nil
	}
	direction := plan.Action
	if plan.Side != "" {
		direction = plan.Side
	}
	return &agent.Thesis{
		WatchTerms:   plan.WatchTerms,
		Summary:      plan.ThesisSummary,
		Conviction:   plan.Conviction,
		Direction:    direction,
		TimeHorizon:  plan.TimeHorizon,
		InvalidateIf: plan.InvalidationConditions,
		GeneratedAt:  time.Now(),
	}
}

// ParseTradingPlan attempts to parse the LLM response content into a
// structured TradingPlanOutput. It handles responses that may include
// markdown code fences around the JSON. If parsing fails entirely, it returns
// a descriptive error.
func ParseTradingPlan(content string) (*TradingPlanOutput, error) {
	return parse.Parse(content, validateTradingPlan)
}

// validateTradingPlan checks that the parsed plan has valid field values.
func validateTradingPlan(plan *TradingPlanOutput) error {
	plan.Action = strings.ToLower(strings.TrimSpace(plan.Action))
	switch plan.Action {
	case "buy", "sell", "hold":
		// valid
	case "wait", "none", "pass", "skip":
		plan.Action = "hold"
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

	// Confidence is validated in the 0-1 range; the risk manager uses a different
	// 1-10 scale that is normalized downstream.
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
