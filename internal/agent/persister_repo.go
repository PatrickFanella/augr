package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const statusUpdateTimeout = 10 * time.Second

// RepoPersister implements DecisionPersister using repository interfaces.
type RepoPersister struct {
	pipelineRunRepo   repository.PipelineRunRepository
	snapshotRepo      repository.PipelineRunSnapshotRepository
	agentDecisionRepo repository.AgentDecisionRepository
	agentEventRepo    repository.AgentEventRepository
	logger            *slog.Logger
}

// NewRepoPersister creates a RepoPersister with the given repositories.
func NewRepoPersister(
	pipelineRunRepo repository.PipelineRunRepository,
	snapshotRepo repository.PipelineRunSnapshotRepository,
	agentDecisionRepo repository.AgentDecisionRepository,
	agentEventRepo repository.AgentEventRepository,
	logger *slog.Logger,
) *RepoPersister {
	if logger == nil {
		logger = slog.Default()
	}
	return &RepoPersister{
		pipelineRunRepo:   pipelineRunRepo,
		snapshotRepo:      snapshotRepo,
		agentDecisionRepo: agentDecisionRepo,
		agentEventRepo:    agentEventRepo,
		logger:            logger,
	}
}

func (p *RepoPersister) RecordRunStart(ctx context.Context, run *domain.PipelineRun) error {
	if p.pipelineRunRepo == nil {
		return nil
	}
	if err := p.pipelineRunRepo.Create(ctx, run); err != nil {
		return fmt.Errorf("agent/pipeline: create pipeline run: %w", err)
	}
	return nil
}

func (p *RepoPersister) RecordRunComplete(_ context.Context, runID uuid.UUID, tradeDate time.Time, status domain.PipelineStatus, completedAt time.Time, errMsg string, phaseTimings json.RawMessage) error {
	if p.pipelineRunRepo == nil {
		return nil
	}
	dbCtx, dbCancel := context.WithTimeout(context.Background(), statusUpdateTimeout)
	defer dbCancel()

	update := repository.PipelineRunStatusUpdate{
		Status:       status,
		CompletedAt:  &completedAt,
		PhaseTimings: phaseTimings,
	}
	if errMsg != "" {
		update.ErrorMessage = errMsg
	}

	if err := p.pipelineRunRepo.UpdateStatus(dbCtx, runID, tradeDate, update); err != nil {
		p.logger.Error("agent/pipeline: failed to update run status",
			slog.Any("error", err),
		)
	}
	return nil
}

func (p *RepoPersister) SupportsSnapshots() bool {
	return p.snapshotRepo != nil
}

func (p *RepoPersister) PersistSnapshot(ctx context.Context, snapshot *domain.PipelineRunSnapshot) error {
	if p.snapshotRepo == nil {
		return nil
	}
	if err := p.snapshotRepo.Create(ctx, snapshot); err != nil {
		return fmt.Errorf("agent/pipeline: persist snapshot %s: %w", snapshot.DataType, err)
	}

	return nil
}

func (p *RepoPersister) PersistDecision(
	ctx context.Context,
	runID uuid.UUID,
	node Node,
	roundNumber *int,
	output string,
	llmResponse *DecisionLLMResponse,
) error {
	if p.agentDecisionRepo == nil {
		return nil
	}

	decision := &domain.AgentDecision{
		PipelineRunID: runID,
		AgentRole:     node.Role(),
		Phase:         node.Phase(),
		RoundNumber:   cloneRoundNumber(roundNumber),
		OutputText:    output,
	}
	if llmResponse != nil {
		decision.LLMProvider = llmResponse.Provider
		decision.PromptText = llmResponse.PromptText
		if llmResponse.Response != nil {
			decision.LLMModel = llmResponse.Response.Model
			decision.PromptTokens = llmResponse.Response.Usage.PromptTokens
			decision.CompletionTokens = llmResponse.Response.Usage.CompletionTokens
			decision.LatencyMS = llmResponse.Response.LatencyMS
			decision.CostUSD = llmResponse.Response.CostUSD
		}
	}

	if err := p.agentDecisionRepo.Create(ctx, decision); err != nil {
		return fmt.Errorf("agent/pipeline: persist decision for %s: %w", node.Name(), err)
	}

	return nil
}

func (p *RepoPersister) PersistEvent(ctx context.Context, event *domain.AgentEvent) error {
	if p.agentEventRepo == nil {
		return nil
	}
	if err := p.agentEventRepo.Create(ctx, event); err != nil {
		return fmt.Errorf("agent/pipeline: persist event %s: %w", event.EventKind, err)
	}

	return nil
}
