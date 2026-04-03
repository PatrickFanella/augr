package debate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/parse"
)

// ResearchManagerSystemPrompt instructs the LLM to act as a senior research
// manager who objectively weighs bull and bear arguments, then produces a
// balanced investment plan in a structured JSON format.
const ResearchManagerSystemPrompt = `You are a senior research manager (investment judge) at a trading firm. Your role is to objectively weigh the bull and bear arguments from the debate and produce a balanced investment plan.

Your responsibilities:
- Evaluate each side's arguments fairly and impartially
- Identify the strongest points from both bull and bear researchers
- Weigh the quality and specificity of the evidence presented
- Acknowledge legitimate risks even when recommending a position
- Produce a clear, actionable investment recommendation

You MUST respond with a JSON object in the following format (no markdown, no code fences, just raw JSON):
{
  "direction": "buy" | "sell" | "hold",
  "conviction": <integer 1-10>,
  "key_evidence": ["evidence point 1", "evidence point 2", ...],
  "acknowledged_risks": ["risk 1", "risk 2", ...],
  "rationale": "Brief explanation of the overall recommendation"
}

Rules:
- "direction" must be exactly one of: "buy", "sell", or "hold"
- "conviction" must be an integer from 1 (very low) to 10 (very high)
- "key_evidence" must list the most compelling data points supporting your recommendation
- "acknowledged_risks" must list the most significant risks that could invalidate your thesis
- "rationale" must be a concise summary tying the evidence and risks together
- Be data-driven: every claim should reference specific evidence from the debate or analyst reports`

// InvestmentPlanOutput represents the structured output parsed from the
// research manager's LLM response. It captures the recommendation direction,
// conviction level, key supporting evidence, and acknowledged risks.
type InvestmentPlanOutput struct {
	Direction         string   `json:"direction"`
	Conviction        int      `json:"conviction"`
	KeyEvidence       []string `json:"key_evidence"`
	AcknowledgedRisks []string `json:"acknowledged_risks"`
	Rationale         string   `json:"rationale"`
}

// ResearchManager is a research-debate-phase Node that acts as the judge,
// synthesizing bull and bear arguments into a balanced investment plan. It
// embeds BaseDebater for shared LLM calling logic and writes its output to
// state.ResearchDebate.InvestmentPlan.
type ResearchManager struct {
	BaseDebater
	providerName string
	systemPrompt string
}

// Compile-time checks: *ResearchManager implements both the legacy node contract and the runner-facing research judge contract.
var (
	_ agent.Node          = (*ResearchManager)(nil)
	_ agent.ResearchJudge = (*ResearchManager)(nil)
)

// NewResearchManager returns a ResearchManager wired to the given LLM provider
// and model. providerName (e.g. "openai") is recorded in decision metadata.
// A nil logger is replaced with the default logger.
func NewResearchManager(provider llm.Provider, providerName, model string, logger *slog.Logger) *ResearchManager {
	return NewResearchManagerWithPrompt(provider, providerName, model, "", logger)
}

// NewResearchManagerWithPrompt returns a ResearchManager wired to the given
// LLM provider and model, using systemPrompt when provided.
func NewResearchManagerWithPrompt(provider llm.Provider, providerName, model, systemPrompt string, logger *slog.Logger) *ResearchManager {
	if systemPrompt == "" {
		systemPrompt = ResearchManagerSystemPrompt
	}

	return &ResearchManager{
		BaseDebater: NewBaseDebater(
			agent.AgentRoleInvestJudge,
			agent.PhaseResearchDebate,
			provider,
			model,
			logger,
		),
		providerName: providerName,
		systemPrompt: systemPrompt,
	}
}

// Name returns the human-readable name for this node.
func (r *ResearchManager) Name() string { return "research_manager" }

// Role returns the agent role constant.
func (r *ResearchManager) Role() agent.AgentRole { return agent.AgentRoleInvestJudge }

// Phase returns the pipeline phase this node belongs to.
func (r *ResearchManager) Phase() agent.Phase { return agent.PhaseResearchDebate }

// Execute preserves the legacy node contract by adapting the runner-facing research judge method onto PipelineState.
func (r *ResearchManager) Execute(ctx context.Context, state *agent.PipelineState) error {
	output, err := r.JudgeResearch(ctx, agent.DebateInput{
		Ticker:         state.Ticker,
		Rounds:         state.ResearchDebate.Rounds,
		ContextReports: state.AnalystReports,
	})
	if err != nil {
		return err
	}
	state.ResearchDebate.InvestmentPlan = output.InvestmentPlan
	state.RecordDecision(agent.AgentRoleInvestJudge, agent.PhaseResearchDebate, nil, output.InvestmentPlan, output.LLMResponse)
	return nil
}

// JudgeResearch implements the runner-facing research judge contract.
func (r *ResearchManager) JudgeResearch(ctx context.Context, input agent.DebateInput) (agent.ResearchJudgeOutput, error) {
	content, promptText, usage, err := r.CallWithContext(
		ctx,
		r.systemPrompt,
		input.Rounds,
		input.ContextReports,
	)
	if err != nil {
		return agent.ResearchJudgeOutput{}, err
	}

	storedPlan := content
	plan, parseErr := ParseInvestmentPlan(content)
	if parseErr != nil {
		r.logger.Warn("research_manager: failed to parse structured output; storing raw content",
			slog.String("error", parseErr.Error()),
		)
	} else {
		r.logger.Info("research_manager: parsed investment plan",
			slog.String("direction", plan.Direction),
			slog.Int("conviction", plan.Conviction),
		)
		if normalized, err := json.Marshal(plan); err == nil {
			storedPlan = string(normalized)
		}
	}

	return agent.ResearchJudgeOutput{
		InvestmentPlan: storedPlan,
		LLMResponse: &agent.DecisionLLMResponse{
			Provider:   r.providerName,
			PromptText: promptText,
			Response: &llm.CompletionResponse{
				Content: content,
				Model:   r.model,
				Usage:   usage,
			},
		},
	}, nil
}

// ParseInvestmentPlan attempts to parse the LLM response content into a
// structured InvestmentPlanOutput. It handles responses that may include
// markdown code fences around the JSON. If parsing fails entirely, it returns
// a descriptive error.
func ParseInvestmentPlan(content string) (*InvestmentPlanOutput, error) {
	return parse.Parse(content, validateInvestmentPlan)
}

// validateInvestmentPlan checks that the parsed plan has valid field values.
func validateInvestmentPlan(plan *InvestmentPlanOutput) error {
	switch plan.Direction {
	case "buy", "sell", "hold":
		// valid
	case "":
		return fmt.Errorf("investment plan missing required field: direction")
	default:
		return fmt.Errorf("investment plan has invalid direction: %q", plan.Direction)
	}

	if plan.Conviction < 1 || plan.Conviction > 10 {
		return fmt.Errorf("investment plan conviction must be 1-10, got %d", plan.Conviction)
	}

	if len(plan.KeyEvidence) == 0 {
		return fmt.Errorf("investment plan missing required field: key_evidence")
	}

	if len(plan.AcknowledgedRisks) == 0 {
		return fmt.Errorf("investment plan missing required field: acknowledged_risks")
	}

	if strings.TrimSpace(plan.Rationale) == "" {
		return fmt.Errorf("investment plan missing required field: rationale")
	}

	return nil
}
