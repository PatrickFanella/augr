package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// PipelineConfig holds timeout and debate-round configuration for a Pipeline.
type PipelineConfig struct {
	PipelineTimeout      time.Duration
	PhaseTimeout         time.Duration
	ResearchDebateRounds int
	RiskDebateRounds     int
}

// Pipeline holds all dependencies and configuration needed by the executor.
type Pipeline struct {
	nodes             map[Phase][]Node
	pipelineRunRepo   repository.PipelineRunRepository
	agentDecisionRepo repository.AgentDecisionRepository
	events            chan<- PipelineEvent
	logger            *slog.Logger
	config            PipelineConfig
}

// NewPipeline constructs a Pipeline with the supplied dependencies. Default
// debate-round counts of 3 are applied when the config fields are zero.
func NewPipeline(
	config PipelineConfig,
	pipelineRunRepo repository.PipelineRunRepository,
	agentDecisionRepo repository.AgentDecisionRepository,
	events chan<- PipelineEvent,
	logger *slog.Logger,
) *Pipeline {
	if config.ResearchDebateRounds == 0 {
		config.ResearchDebateRounds = 3
	}
	if config.RiskDebateRounds == 0 {
		config.RiskDebateRounds = 3
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Pipeline{
		nodes:             make(map[Phase][]Node),
		pipelineRunRepo:   pipelineRunRepo,
		agentDecisionRepo: agentDecisionRepo,
		events:            events,
		logger:            logger,
		config:            config,
	}
}

// RegisterNode adds a node to the phase group determined by node.Phase().
func (p *Pipeline) RegisterNode(node Node) {
	if p.nodes == nil {
		p.nodes = make(map[Phase][]Node)
	}
	phase := node.Phase()
	p.nodes[phase] = append(p.nodes[phase], node)
}

// Config returns the resolved PipelineConfig (with defaults applied).
func (p *Pipeline) Config() PipelineConfig {
	return p.config
}

// Nodes returns a copy of the phase-to-nodes map for inspection.
func (p *Pipeline) Nodes() map[Phase][]Node {
	out := make(map[Phase][]Node, len(p.nodes))
	for phase, nodes := range p.nodes {
		out[phase] = append([]Node(nil), nodes...)
	}
	return out
}

// nodeByRole returns the first registered Node in the given phase that matches
// the specified role, or nil if none is found.
func (p *Pipeline) nodeByRole(phase Phase, role AgentRole) Node {
	for _, n := range p.nodes[phase] {
		if n.Role() == role {
			return n
		}
	}
	return nil
}

// executeResearchDebatePhase runs the multi-round research debate. For each
// round (up to config.ResearchDebateRounds), the BullResearcher and
// BearResearcher nodes execute sequentially. A DebateRoundCompleted event is
// emitted after each completed round. After all rounds the InvestJudge node
// runs to produce the investment plan.
func (p *Pipeline) executeResearchDebatePhase(ctx context.Context, state *PipelineState) error {
	phaseCtx := ctx
	if p.config.PhaseTimeout > 0 {
		var cancel context.CancelFunc
		phaseCtx, cancel = context.WithTimeout(ctx, p.config.PhaseTimeout)
		defer cancel()
	}

	bullNode := p.nodeByRole(PhaseResearchDebate, AgentRoleBullResearcher)
	bearNode := p.nodeByRole(PhaseResearchDebate, AgentRoleBearResearcher)
	judgeNode := p.nodeByRole(PhaseResearchDebate, AgentRoleInvestJudge)

	// Fail fast when required debate nodes are missing.
	if bullNode == nil {
		return fmt.Errorf("agent/pipeline: research debate phase requires a %s node", AgentRoleBullResearcher)
	}
	if bearNode == nil {
		return fmt.Errorf("agent/pipeline: research debate phase requires a %s node", AgentRoleBearResearcher)
	}
	if judgeNode == nil {
		return fmt.Errorf("agent/pipeline: research debate phase requires a %s node", AgentRoleInvestJudge)
	}

	rounds := p.config.ResearchDebateRounds
	if rounds < 1 {
		p.logger.Warn("agent/pipeline: invalid ResearchDebateRounds; clamping to 1",
			slog.Int("configured_rounds", p.config.ResearchDebateRounds),
		)
		rounds = 1
	}

	for i := 1; i <= rounds; i++ {
		// Check for context cancellation before starting the round.
		if err := phaseCtx.Err(); err != nil {
			return err
		}

		// Prepare the round structure so nodes can write contributions.
		state.ResearchDebate.Rounds = append(state.ResearchDebate.Rounds, DebateRound{
			Number:        i,
			Contributions: make(map[AgentRole]string),
		})

		// Execute bull researcher.
		if err := bullNode.Execute(phaseCtx, state); err != nil {
			return err
		}

		// Execute bear researcher.
		if err := bearNode.Execute(phaseCtx, state); err != nil {
			return err
		}

		// Emit DebateRoundCompleted event.
		if p.events != nil {
			event := PipelineEvent{
				Type:          DebateRoundCompleted,
				PipelineRunID: state.PipelineRunID,
				StrategyID:    state.StrategyID,
				Ticker:        state.Ticker,
				Phase:         PhaseResearchDebate,
				Round:         i,
				OccurredAt:    time.Now().UTC(),
			}
			select {
			case p.events <- event:
			case <-phaseCtx.Done():
				p.logger.Debug("agent/pipeline: DebateRoundCompleted event dropped; phase context cancelled",
					slog.Int("round", i),
				)
			default:
				p.logger.Debug("agent/pipeline: DebateRoundCompleted event dropped; events channel full",
					slog.Int("round", i),
				)
			}
		}
	}

	// Execute the InvestJudge (Research Manager).
	if err := judgeNode.Execute(phaseCtx, state); err != nil {
		return err
	}

	return nil
}

// executeAnalysisPhase runs all registered PhaseAnalysis nodes concurrently using
// errgroup. If any node fails, a warning is logged and the remaining nodes continue
// unaffected (partial failures do not abort the phase). If config.PhaseTimeout is
// positive, it is applied as a deadline for the entire phase, cancelling any nodes
// that have not yet completed. An AgentDecisionMade event is emitted (non-blocking)
// after each node completes successfully.
//
// This method always returns nil; analyst node failures are tolerated and surfaced only
// through log warnings. The error return is reserved for future structural failures
// (e.g., a cancelled parent context passed before any node is launched).
func (p *Pipeline) executeAnalysisPhase(ctx context.Context, state *PipelineState) error {
	// Ensure the analyst-reports mutex is initialised before goroutines start.
	// This single-threaded initialisation is safe because goroutines are not yet running.
	if state.mu == nil {
		state.mu = &sync.Mutex{}
	}

	phaseCtx := ctx
	if p.config.PhaseTimeout > 0 {
		var cancel context.CancelFunc
		phaseCtx, cancel = context.WithTimeout(ctx, p.config.PhaseTimeout)
		defer cancel()
	}

	g, gCtx := errgroup.WithContext(phaseCtx)

	for _, n := range p.nodes[PhaseAnalysis] {
		node := n
		g.Go(func() error {
			if err := node.Execute(gCtx, state); err != nil {
				p.logger.Warn("agent/pipeline: analyst node failed",
					slog.String("node", node.Name()),
					slog.Any("error", err),
				)
				return nil // partial failures are tolerated; do not abort the phase
			}

			if p.events != nil {
				event := PipelineEvent{
					Type:          AgentDecisionMade,
					PipelineRunID: state.PipelineRunID,
					StrategyID:    state.StrategyID,
					Ticker:        state.Ticker,
					AgentRole:     node.Role(),
					Phase:         PhaseAnalysis,
					OccurredAt:    time.Now().UTC(),
				}
				// Non-blocking send: drop the event rather than let the goroutine
				// stall if the channel is full or the phase context is cancelled.
				select {
				case p.events <- event:
				case <-gCtx.Done():
					p.logger.Debug("agent/pipeline: AgentDecisionMade event dropped; phase context cancelled",
						slog.String("node", node.Name()),
					)
				}
			}
			return nil
		})
	}

	return g.Wait()
}

// executeTradingPhase runs the single registered Trader node. If no Trader
// node is registered an error is returned immediately. On success an
// AgentDecisionMade event is emitted (non-blocking). If config.PhaseTimeout
// is positive it is applied as a deadline for the phase.
func (p *Pipeline) executeTradingPhase(ctx context.Context, state *PipelineState) error {
	phaseCtx := ctx
	if p.config.PhaseTimeout > 0 {
		var cancel context.CancelFunc
		phaseCtx, cancel = context.WithTimeout(ctx, p.config.PhaseTimeout)
		defer cancel()
	}

	traderNode := p.nodeByRole(PhaseTrading, AgentRoleTrader)
	if traderNode == nil {
		return fmt.Errorf("agent/pipeline: trading phase requires a %s node", AgentRoleTrader)
	}

	if err := traderNode.Execute(phaseCtx, state); err != nil {
		return err
	}

	if p.events != nil {
		event := PipelineEvent{
			Type:          AgentDecisionMade,
			PipelineRunID: state.PipelineRunID,
			StrategyID:    state.StrategyID,
			Ticker:        state.Ticker,
			AgentRole:     traderNode.Role(),
			Phase:         PhaseTrading,
			OccurredAt:    time.Now().UTC(),
		}
		select {
		case p.events <- event:
		case <-phaseCtx.Done():
			p.logger.Debug("agent/pipeline: AgentDecisionMade event dropped; phase context cancelled",
				slog.String("node", traderNode.Name()),
			)
		default:
			p.logger.Debug("agent/pipeline: AgentDecisionMade event dropped; events channel full",
				slog.String("node", traderNode.Name()),
			)
		}
	}

	return nil
}

// executeRiskDebatePhase runs the multi-round risk debate. For each round (up
// to config.RiskDebateRounds), the Aggressive, Conservative, and Neutral
// analyst nodes execute sequentially. A DebateRoundCompleted event is emitted
// after each completed round. After all rounds the RiskManager node runs to
// produce the final risk signal.
func (p *Pipeline) executeRiskDebatePhase(ctx context.Context, state *PipelineState) error {
	phaseCtx := ctx
	if p.config.PhaseTimeout > 0 {
		var cancel context.CancelFunc
		phaseCtx, cancel = context.WithTimeout(ctx, p.config.PhaseTimeout)
		defer cancel()
	}

	aggressiveNode := p.nodeByRole(PhaseRiskDebate, AgentRoleAggressiveAnalyst)
	conservativeNode := p.nodeByRole(PhaseRiskDebate, AgentRoleConservativeAnalyst)
	neutralNode := p.nodeByRole(PhaseRiskDebate, AgentRoleNeutralAnalyst)
	riskManagerNode := p.nodeByRole(PhaseRiskDebate, AgentRoleRiskManager)

	// Fail fast when required debate nodes are missing.
	if aggressiveNode == nil {
		return fmt.Errorf("agent/pipeline: risk debate phase requires a %s node", AgentRoleAggressiveAnalyst)
	}
	if conservativeNode == nil {
		return fmt.Errorf("agent/pipeline: risk debate phase requires a %s node", AgentRoleConservativeAnalyst)
	}
	if neutralNode == nil {
		return fmt.Errorf("agent/pipeline: risk debate phase requires a %s node", AgentRoleNeutralAnalyst)
	}
	if riskManagerNode == nil {
		return fmt.Errorf("agent/pipeline: risk debate phase requires a %s node", AgentRoleRiskManager)
	}

	rounds := p.config.RiskDebateRounds
	if rounds < 1 {
		p.logger.Warn("agent/pipeline: invalid RiskDebateRounds; clamping to 1",
			slog.Int("configured_rounds", p.config.RiskDebateRounds),
		)
		rounds = 1
	}

	for i := 1; i <= rounds; i++ {
		// Check for context cancellation before starting the round.
		if err := phaseCtx.Err(); err != nil {
			return err
		}

		// Prepare the round structure so nodes can write contributions.
		state.RiskDebate.Rounds = append(state.RiskDebate.Rounds, DebateRound{
			Number:        i,
			Contributions: make(map[AgentRole]string),
		})

		// Execute aggressive analyst.
		if err := aggressiveNode.Execute(phaseCtx, state); err != nil {
			return err
		}

		// Execute conservative analyst.
		if err := conservativeNode.Execute(phaseCtx, state); err != nil {
			return err
		}

		// Execute neutral analyst.
		if err := neutralNode.Execute(phaseCtx, state); err != nil {
			return err
		}

		// Emit DebateRoundCompleted event.
		if p.events != nil {
			event := PipelineEvent{
				Type:          DebateRoundCompleted,
				PipelineRunID: state.PipelineRunID,
				StrategyID:    state.StrategyID,
				Ticker:        state.Ticker,
				Phase:         PhaseRiskDebate,
				Round:         i,
				OccurredAt:    time.Now().UTC(),
			}
			select {
			case p.events <- event:
			case <-phaseCtx.Done():
				p.logger.Debug("agent/pipeline: DebateRoundCompleted event dropped; phase context cancelled",
					slog.Int("round", i),
				)
			default:
				p.logger.Debug("agent/pipeline: DebateRoundCompleted event dropped; events channel full",
					slog.Int("round", i),
				)
			}
		}
	}

	// Execute the RiskManager to produce the final risk signal.
	if err := riskManagerNode.Execute(phaseCtx, state); err != nil {
		return err
	}

	return nil
}

// Execute runs the full pipeline for the given strategy and ticker. It creates
// a PipelineRun record in the database (status=running), applies the
// pipeline-level timeout from config, and executes the four phases in order:
// Analysis → ResearchDebate → Trading → RiskDebate. A PipelineStarted event
// is emitted at the beginning, and either PipelineCompleted or PipelineError
// at the end. The PipelineRun status is updated to completed or failed
// accordingly.
func (p *Pipeline) Execute(ctx context.Context, strategyID uuid.UUID, ticker string) (*PipelineState, error) {
	// Apply pipeline-level timeout when configured.
	if p.config.PipelineTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.PipelineTimeout)
		defer cancel()
	}

	now := time.Now().UTC()
	run := &domain.PipelineRun{
		ID:         uuid.New(),
		StrategyID: strategyID,
		Ticker:     ticker,
		TradeDate:  now.Truncate(24 * time.Hour),
		Status:     domain.PipelineStatusRunning,
		StartedAt:  now,
	}

	if err := p.pipelineRunRepo.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("agent/pipeline: create pipeline run: %w", err)
	}

	state := &PipelineState{
		PipelineRunID: run.ID,
		StrategyID:    strategyID,
		Ticker:        ticker,
		mu:            &sync.Mutex{},
	}

	// Emit PipelineStarted event.
	p.emitEvent(ctx, PipelineEvent{
		Type:          PipelineStarted,
		PipelineRunID: run.ID,
		StrategyID:    strategyID,
		Ticker:        ticker,
		OccurredAt:    time.Now().UTC(),
	})

	// Execute phases in order.
	phases := []struct {
		name string
		fn   func(context.Context, *PipelineState) error
	}{
		{"analysis", p.executeAnalysisPhase},
		{"research_debate", p.executeResearchDebatePhase},
		{"trading", p.executeTradingPhase},
		{"risk_debate", p.executeRiskDebatePhase},
	}

	for _, phase := range phases {
		if err := phase.fn(ctx, state); err != nil {
			p.logger.Error("agent/pipeline: phase failed",
				slog.String("phase", phase.name),
				slog.Any("error", err),
			)

			// Use a background context for the status update so that it
			// succeeds even when the pipeline context has been cancelled.
			completedAt := time.Now().UTC()
			if updateErr := p.pipelineRunRepo.UpdateStatus(
				context.Background(), run.ID, run.TradeDate,
				repository.PipelineRunStatusUpdate{
					Status:       domain.PipelineStatusFailed,
					CompletedAt:  &completedAt,
					ErrorMessage: err.Error(),
				},
			); updateErr != nil {
				p.logger.Error("agent/pipeline: failed to update run status",
					slog.Any("error", updateErr),
				)
			}

			p.emitEvent(ctx, PipelineEvent{
				Type:          PipelineError,
				PipelineRunID: run.ID,
				StrategyID:    strategyID,
				Ticker:        ticker,
				Error:         err.Error(),
				OccurredAt:    time.Now().UTC(),
			})

			return state, err
		}
	}

	// All phases succeeded – mark the run as completed.
	completedAt := time.Now().UTC()
	if updateErr := p.pipelineRunRepo.UpdateStatus(
		ctx, run.ID, run.TradeDate,
		repository.PipelineRunStatusUpdate{
			Status:      domain.PipelineStatusCompleted,
			CompletedAt: &completedAt,
		},
	); updateErr != nil {
		p.logger.Error("agent/pipeline: failed to update run status",
			slog.Any("error", updateErr),
		)
	}

	p.emitEvent(ctx, PipelineEvent{
		Type:          PipelineCompleted,
		PipelineRunID: run.ID,
		StrategyID:    strategyID,
		Ticker:        ticker,
		OccurredAt:    time.Now().UTC(),
	})

	return state, nil
}

// emitEvent sends an event to the events channel in a non-blocking fashion.
// It is a no-op when the events channel is nil.
func (p *Pipeline) emitEvent(ctx context.Context, event PipelineEvent) {
	if p.events == nil {
		return
	}
	select {
	case p.events <- event:
	case <-ctx.Done():
		p.logger.Debug("agent/pipeline: event dropped; context cancelled",
			slog.String("type", string(event.Type)),
		)
	default:
		p.logger.Debug("agent/pipeline: event dropped; events channel full",
			slog.String("type", string(event.Type)),
		)
	}
}
