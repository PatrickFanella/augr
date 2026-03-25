package memory

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// reflectionRoles are the agent roles that receive a reflection memory after
// a trade closes.
var reflectionRoles = []domain.AgentRole{
	domain.AgentRoleBullResearcher,
	domain.AgentRoleBearResearcher,
	domain.AgentRoleTrader,
	domain.AgentRoleInvestJudge,
	domain.AgentRoleRiskManager,
}

const reflectionSystemPrompt = "You are a trading reflection assistant. " +
	"Given the trade context, the agent's recommendation, and the actual outcome, " +
	"extract one concise lesson the agent should remember for future decisions. " +
	"Respond with only the lesson, no preamble."

// Reflector generates agent memories by reflecting on closed trade outcomes.
type Reflector struct {
	memoryRepo    repository.MemoryRepository
	pipelineRepo  repository.PipelineRunRepository
	decisionRepo  repository.AgentDecisionRepository
	positionRepo  repository.PositionRepository
	llmProvider   llm.Provider
	logger        *slog.Logger
}

// NewReflector creates a Reflector with the given dependencies.
// A nil logger is replaced with slog.Default().
func NewReflector(
	memoryRepo repository.MemoryRepository,
	pipelineRepo repository.PipelineRunRepository,
	decisionRepo repository.AgentDecisionRepository,
	positionRepo repository.PositionRepository,
	llmProvider llm.Provider,
	logger *slog.Logger,
) *Reflector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reflector{
		memoryRepo:   memoryRepo,
		pipelineRepo: pipelineRepo,
		decisionRepo: decisionRepo,
		positionRepo: positionRepo,
		llmProvider:  llmProvider,
		logger:       logger,
	}
}

// Reflect loads the closed position identified by positionID, finds its linked
// pipeline run, and generates one reflective memory per agent role.
func (r *Reflector) Reflect(ctx context.Context, positionID uuid.UUID) error {
	// 1. Load position.
	pos, err := r.positionRepo.Get(ctx, positionID)
	if err != nil {
		return fmt.Errorf("load position %s: %w", positionID, err)
	}

	// 2. Find the linked pipeline run via the position's strategy.
	run, err := r.findPipelineRun(ctx, pos)
	if err != nil {
		return fmt.Errorf("find pipeline run for position %s: %w", positionID, err)
	}

	// 3. Load all agent decisions for that run.
	decisions, err := r.decisionRepo.GetByRun(ctx, run.ID, repository.AgentDecisionFilter{}, 100, 0)
	if err != nil {
		return fmt.Errorf("load decisions for run %s: %w", run.ID, err)
	}

	decisionsByRole := make(map[domain.AgentRole]domain.AgentDecision, len(decisions))
	for _, d := range decisions {
		decisionsByRole[d.AgentRole] = d
	}

	// 4. Compute outcome summary.
	outcome := computeOutcome(pos)

	// 5. Generate a memory for each reflection role.
	for _, role := range reflectionRoles {
		decision, ok := decisionsByRole[role]
		recommendation := "no recommendation recorded"
		if ok {
			recommendation = decision.OutputText
		}

		situation := fmt.Sprintf(
			"Ticker: %s, Side: %s, Entry: %.4f, Signal: %s",
			pos.Ticker, pos.Side, pos.AvgEntry, run.Signal,
		)

		userPrompt := fmt.Sprintf(
			"Situation: %s\nThe %s recommended: %s\nOutcome: %s",
			situation, role, recommendation, outcome,
		)

		resp, err := r.llmProvider.Complete(ctx, llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "system", Content: reflectionSystemPrompt},
				{Role: "user", Content: userPrompt},
			},
		})
		if err != nil {
			r.logger.WarnContext(ctx, "reflection LLM call failed",
				"role", role, "position_id", positionID, "error", err)
			continue
		}

		runID := run.ID
		mem := &domain.AgentMemory{
			AgentRole:      role,
			Situation:      situation,
			Recommendation: recommendation,
			Outcome:        resp.Content,
			PipelineRunID:  &runID,
		}

		if err := r.memoryRepo.Create(ctx, mem); err != nil {
			r.logger.WarnContext(ctx, "failed to store reflection memory",
				"role", role, "position_id", positionID, "error", err)
			continue
		}

		r.logger.InfoContext(ctx, "reflection memory stored",
			"role", role, "position_id", positionID, "memory_id", mem.ID)
	}

	return nil
}

// findPipelineRun locates the most recent completed pipeline run linked to the
// position through its strategy and ticker.
func (r *Reflector) findPipelineRun(ctx context.Context, pos *domain.Position) (*domain.PipelineRun, error) {
	if pos.StrategyID == nil {
		return nil, fmt.Errorf("position %s has no strategy", pos.ID)
	}

	runs, err := r.pipelineRepo.List(ctx, repository.PipelineRunFilter{
		StrategyID: pos.StrategyID,
		Ticker:     pos.Ticker,
		Status:     domain.PipelineStatusCompleted,
	}, 1, 0)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, fmt.Errorf("no completed pipeline run found for strategy %s ticker %s",
			*pos.StrategyID, pos.Ticker)
	}

	return &runs[0], nil
}

// computeOutcome builds a human-readable outcome string from position data.
func computeOutcome(pos *domain.Position) string {
	pnl := pos.RealizedPnL
	cost := pos.AvgEntry * pos.Quantity
	pctReturn := 0.0
	if cost > 0 {
		pctReturn = (pnl / cost) * 100
	}

	holdingPeriod := "still open"
	if pos.ClosedAt != nil {
		dur := pos.ClosedAt.Sub(pos.OpenedAt)
		holdingPeriod = formatDuration(dur)
	}

	result := "profit"
	if pnl < 0 {
		result = "loss"
	}

	return fmt.Sprintf("%s of %.2f (%.2f%% return), holding period: %s",
		result, math.Abs(pnl), pctReturn, holdingPeriod)
}

// formatDuration returns a human-friendly representation of a duration.
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, int(d.Hours())%24)
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}
