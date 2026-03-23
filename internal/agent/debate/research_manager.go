package debate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
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
	Direction        string   `json:"direction"`
	Conviction       int      `json:"conviction"`
	KeyEvidence      []string `json:"key_evidence"`
	AcknowledgedRisks []string `json:"acknowledged_risks"`
	Rationale        string   `json:"rationale"`
}

// ResearchManager is a research-debate-phase Node that acts as the judge,
// synthesizing bull and bear arguments into a balanced investment plan. It
// embeds BaseDebater for shared LLM calling logic and writes its output to
// state.ResearchDebate.InvestmentPlan.
type ResearchManager struct {
	BaseDebater
	providerName string
}

// NewResearchManager returns a ResearchManager wired to the given LLM provider
// and model. providerName (e.g. "openai") is recorded in decision metadata.
// A nil logger is replaced with the default logger.
func NewResearchManager(provider llm.Provider, providerName, model string, logger *slog.Logger) *ResearchManager {
	return &ResearchManager{
		BaseDebater: NewBaseDebater(
			agent.AgentRoleInvestJudge,
			agent.PhaseResearchDebate,
			provider,
			model,
			logger,
		),
		providerName: providerName,
	}
}

// Name returns the human-readable name for this node.
func (r *ResearchManager) Name() string { return "research_manager" }

// Role returns the agent role constant.
func (r *ResearchManager) Role() agent.AgentRole { return agent.AgentRoleInvestJudge }

// Phase returns the pipeline phase this node belongs to.
func (r *ResearchManager) Phase() agent.Phase { return agent.PhaseResearchDebate }

// Execute calls the LLM with the research manager system prompt, all debate
// rounds, and analyst reports. It parses the structured JSON output into an
// InvestmentPlanOutput and stores a normalized JSON string in
// state.ResearchDebate.InvestmentPlan when parsing succeeds. If parsing fails,
// the raw LLM content is stored instead so the pipeline can proceed.
func (r *ResearchManager) Execute(ctx context.Context, state *agent.PipelineState) error {
	rounds := state.ResearchDebate.Rounds

	content, usage, err := r.CallWithContext(
		ctx,
		ResearchManagerSystemPrompt,
		rounds,
		state.AnalystReports,
	)
	if err != nil {
		return err
	}

	// Attempt to parse the structured output. When parsing succeeds we
	// store a clean, re-marshaled JSON string. On failure we fall back to
	// the raw LLM content so the pipeline can still proceed.
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

	// Store the investment plan in the research debate state.
	state.ResearchDebate.InvestmentPlan = storedPlan

	// Record the decision so the pipeline can persist it with LLM metadata.
	// The raw LLM content is kept in the decision for debugging purposes.
	state.RecordDecision(
		agent.AgentRoleInvestJudge,
		agent.PhaseResearchDebate,
		nil,
		storedPlan,
		&agent.DecisionLLMResponse{
			Provider: r.providerName,
			Response: &llm.CompletionResponse{
				Content: content,
				Model:   r.model,
				Usage:   usage,
			},
		},
	)

	return nil
}

// ParseInvestmentPlan attempts to parse the LLM response content into a
// structured InvestmentPlanOutput. It handles responses that may include
// markdown code fences around the JSON. If parsing fails entirely, it returns
// a descriptive error.
func ParseInvestmentPlan(content string) (*InvestmentPlanOutput, error) {
	cleaned := stripCodeFences(content)

	var plan InvestmentPlanOutput
	if err := json.Unmarshal([]byte(cleaned), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse investment plan JSON: %w", err)
	}

	if err := validateInvestmentPlan(&plan); err != nil {
		return nil, err
	}

	return &plan, nil
}

// stripCodeFences removes optional markdown code fences (```json ... ``` or ``` ... ```)
// from the LLM response so the JSON can be parsed cleanly. It handles both
// fences with a newline after the opening tag and inline fences where the JSON
// starts on the same line (e.g. ```json { ... }```).
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
