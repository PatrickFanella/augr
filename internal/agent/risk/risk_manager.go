package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/debate"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/parse"
)

// RiskManagerSystemPrompt instructs the LLM to act as a senior risk manager
// who synthesises all risk perspectives from the debate and produces a final
// trading decision with adjusted position parameters.
const RiskManagerSystemPrompt = `You are a senior risk manager (final decision maker) at a trading firm. Your role is to synthesize all risk perspectives from the aggressive, conservative, and neutral analysts to produce the FINAL trading decision.

Your responsibilities:
- Weigh each risk analyst's arguments based on the quality and specificity of their evidence
- Determine whether the proposed trade should proceed, be modified, or be rejected
- Adjust position size and stop-loss levels based on the consensus risk assessment
- Ensure the final decision balances return potential with capital preservation
- Provide clear reasoning that references specific points from the risk debate

You MUST respond with a JSON object in the following format (no markdown, no code fences, just raw JSON):
{
  "action": "BUY" | "SELL" | "HOLD",
  "confidence": <integer 1-10>,
  "adjusted_position_size": <number>,
  "adjusted_stop_loss": <number>,
  "reasoning": "Brief explanation of the final decision"
}

Rules:
- "action" must be exactly one of: "BUY", "SELL", or "HOLD"
- "confidence" must be an integer from 1 (very low) to 10 (very high)
- "adjusted_position_size" is the final recommended position size after risk adjustment (0 for HOLD)
- "adjusted_stop_loss" is the final recommended stop-loss price after risk adjustment (0 for HOLD)
- "reasoning" must be a concise summary tying the risk debate perspectives together
- Be data-driven: every conclusion should reference specific arguments from the risk debate`

// FinalSignalOutput represents the structured output parsed from the risk
// manager's LLM response. It captures the final action, confidence, adjusted
// position parameters, and reasoning behind the decision.
type FinalSignalOutput struct {
	Action               string  `json:"action"`
	Confidence           int     `json:"confidence"`
	AdjustedPositionSize float64 `json:"adjusted_position_size"`
	AdjustedStopLoss     float64 `json:"adjusted_stop_loss"`
	Reasoning            string  `json:"reasoning"`
}

// RiskManager is a risk-debate-phase Node that acts as the final judge,
// synthesizing all risk perspectives into a FINAL BUY/SELL/HOLD signal. It
// embeds debate.BaseDebater for shared LLM calling logic and writes its output
// to state.RiskDebate.FinalSignal and state.FinalSignal.
type RiskManager struct {
	debate.BaseDebater
	providerName string
	systemPrompt string
	logger       *slog.Logger
}

// Compile-time check: *RiskManager implements agent.RiskJudgeNode.
var _ agent.RiskJudgeNode = (*RiskManager)(nil)

// NewRiskManager returns a RiskManager wired to the given LLM provider and
// model. providerName (e.g. "openai") is recorded in decision metadata.
// A nil logger is replaced with the default logger.
func NewRiskManager(provider llm.Provider, providerName, model string, logger *slog.Logger) *RiskManager {
	return NewRiskManagerWithPrompt(provider, providerName, model, "", logger)
}

// NewRiskManagerWithPrompt returns a RiskManager wired to the given LLM
// provider and model, using systemPrompt when provided.
func NewRiskManagerWithPrompt(provider llm.Provider, providerName, model, systemPrompt string, logger *slog.Logger) *RiskManager {
	if logger == nil {
		logger = slog.Default()
	}
	if systemPrompt == "" {
		systemPrompt = RiskManagerSystemPrompt
	}
	return &RiskManager{
		BaseDebater: debate.NewBaseDebater(
			agent.AgentRoleRiskManager,
			agent.PhaseRiskDebate,
			provider,
			model,
			logger,
		),
		providerName: providerName,
		systemPrompt: systemPrompt,
		logger:       logger,
	}
}

// Name returns the human-readable name for this node.
func (r *RiskManager) Name() string { return "risk_manager" }

// Role returns the agent role constant.
func (r *RiskManager) Role() agent.AgentRole { return agent.AgentRoleRiskManager }

// Phase returns the pipeline phase this node belongs to.
func (r *RiskManager) Phase() agent.Phase { return agent.PhaseRiskDebate }

// Execute calls the LLM with the risk manager system prompt, the current
// trading plan, and all risk debate rounds. It parses the structured JSON
// output into a FinalSignalOutput and writes both state.RiskDebate.FinalSignal
// (normalized JSON or raw content) and state.FinalSignal (signal + confidence).
// For BUY/SELL actions, TradingPlan position size and stop-loss are updated
// with the risk-adjusted values. For HOLD, no position adjustments are made.
func (r *RiskManager) Execute(ctx context.Context, state *agent.PipelineState) error {
	input := agent.RiskJudgeInput{
		Ticker:      state.Ticker,
		Rounds:      state.RiskDebate.Rounds,
		TradingPlan: state.TradingPlan,
	}
	output, err := r.JudgeRisk(ctx, input)
	if err != nil {
		return err
	}
	state.FinalSignal = output.FinalSignal
	state.TradingPlan = output.TradingPlan
	state.RiskDebate.FinalSignal = output.StoredSignal
	state.RecordDecision(agent.AgentRoleRiskManager, agent.PhaseRiskDebate, nil, output.StoredSignal, output.LLMResponse)
	return nil
}

// JudgeRisk implements the RiskJudgeNode interface. It calls the LLM with the
// risk manager system prompt, the current trading plan, and all risk debate
// rounds and returns a typed RiskJudgeOutput with the final signal and
// potentially risk-adjusted trading plan.
func (r *RiskManager) JudgeRisk(ctx context.Context, input agent.RiskJudgeInput) (agent.RiskJudgeOutput, error) {
	// Build a context map that includes the trading plan so the LLM can
	// reference concrete position sizes, stop-losses, and take-profit levels.
	tradingPlanJSON, err := json.Marshal(input.TradingPlan)
	if err != nil {
		prefix := fmt.Sprintf("%s (%s)", agent.AgentRoleRiskManager, agent.PhaseRiskDebate)
		return agent.RiskJudgeOutput{}, fmt.Errorf("%s: marshal trading plan: %w", prefix, err)
	}
	contextReports := map[agent.AgentRole]string{
		agent.AgentRoleTrader: string(tradingPlanJSON),
	}

	content, promptText, usage, err := r.CallWithContext(
		ctx,
		r.systemPrompt,
		input.Rounds,
		contextReports,
	)
	if err != nil {
		return agent.RiskJudgeOutput{}, err
	}

	var finalSignal agent.FinalSignal
	tradingPlan := input.TradingPlan

	// Attempt to parse the structured output. When parsing succeeds we
	// store a clean, re-marshaled JSON string and update the pipeline
	// state. On failure we fall back to the raw LLM content so the
	// pipeline can still proceed.
	storedSignal := content
	signal, parseErr := ParseFinalSignal(content)
	if parseErr != nil {
		r.logger.Warn("risk_manager: failed to parse structured output; storing raw content",
			slog.String("error", parseErr.Error()),
		)
	} else {
		r.logger.Info("risk_manager: parsed final signal",
			slog.String("action", signal.Action),
			slog.Int("confidence", signal.Confidence),
		)
		if normalized, marshalErr := json.Marshal(signal); marshalErr == nil {
			storedSignal = string(normalized)
		}

		// Map the parsed action to the pipeline signal.
		switch strings.ToUpper(signal.Action) {
		case "BUY":
			finalSignal.Signal = agent.PipelineSignalBuy
		case "SELL":
			finalSignal.Signal = agent.PipelineSignalSell
		default:
			finalSignal.Signal = agent.PipelineSignalHold
		}
		finalSignal.Confidence = float64(signal.Confidence) / 10.0

		// Update TradingPlan with risk-adjusted values for actionable signals.
		// HOLD signals intentionally leave the TradingPlan unchanged.
		if strings.ToUpper(signal.Action) != "HOLD" {
			if signal.AdjustedPositionSize > 0 {
				tradingPlan.PositionSize = signal.AdjustedPositionSize
			}
			if signal.AdjustedStopLoss > 0 {
				tradingPlan.StopLoss = signal.AdjustedStopLoss
			}
		}
	}

	return agent.RiskJudgeOutput{
		FinalSignal:  finalSignal,
		StoredSignal: storedSignal,
		TradingPlan:  tradingPlan,
		LLMResponse: &agent.DecisionLLMResponse{
			Provider:   r.providerName,
			PromptText: promptText,
			Response: &llm.CompletionResponse{
				Content: content,
				Model:   r.Model(),
				Usage:   usage,
			},
		},
	}, nil
}

// ParseFinalSignal attempts to parse the LLM response content into a
// structured FinalSignalOutput. It handles responses that may include
// markdown code fences around the JSON. If parsing fails entirely, it returns
// a descriptive error.
func ParseFinalSignal(content string) (*FinalSignalOutput, error) {
	return parse.Parse(content, validateFinalSignal)
}

// validateFinalSignal checks that the parsed signal has valid field values.
func validateFinalSignal(signal *FinalSignalOutput) error {
	// Normalize action to uppercase during validation so callers receive
	// a consistently cased value from parse.Parse.
	signal.Action = strings.ToUpper(signal.Action)
	action := signal.Action
	switch action {
	case "BUY", "SELL", "HOLD":
		// valid
	case "":
		return fmt.Errorf("final signal missing required field: action")
	default:
		return fmt.Errorf("final signal has invalid action: %q", signal.Action)
	}

	if signal.Confidence < 1 || signal.Confidence > 10 {
		return fmt.Errorf("final signal confidence must be 1-10, got %d", signal.Confidence)
	}

	if strings.TrimSpace(signal.Reasoning) == "" {
		return fmt.Errorf("final signal missing required field: reasoning")
	}

	// Validate position parameters based on action.
	// BUY/SELL require positive adjusted_position_size and adjusted_stop_loss.
	// HOLD must have both at zero since no position is taken.
	switch action {
	case "BUY", "SELL":
		if signal.AdjustedPositionSize <= 0 {
			return fmt.Errorf("final signal %s requires adjusted_position_size > 0, got %v", action, signal.AdjustedPositionSize)
		}
		if signal.AdjustedStopLoss <= 0 {
			return fmt.Errorf("final signal %s requires adjusted_stop_loss > 0, got %v", action, signal.AdjustedStopLoss)
		}
	case "HOLD":
		if signal.AdjustedPositionSize != 0 {
			return fmt.Errorf("final signal HOLD requires adjusted_position_size = 0, got %v", signal.AdjustedPositionSize)
		}
		if signal.AdjustedStopLoss != 0 {
			return fmt.Errorf("final signal HOLD requires adjusted_stop_loss = 0, got %v", signal.AdjustedStopLoss)
		}
	}

	return nil
}
