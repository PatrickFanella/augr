package agent

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
)

// Pipeline orchestrates the execution of registered agent nodes across phases.
type Pipeline struct {
	nodes        []Node
	events       chan<- PipelineEvent
	phaseTimeout time.Duration
	logger       *slog.Logger
}

// NewPipeline constructs a Pipeline with the given nodes, event channel, phase timeout,
// and logger. If logger is nil, slog.Default() is used. If events is nil, no events are
// emitted. If phaseTimeout is zero, no timeout is applied to individual phases.
func NewPipeline(nodes []Node, events chan<- PipelineEvent, phaseTimeout time.Duration, logger *slog.Logger) *Pipeline {
	"log/slog"
	"time"

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
		nodes:        nodes,
		events:       events,
		phaseTimeout: phaseTimeout,
		logger:       logger,
	}
}

// executeAnalysisPhase runs all registered PhaseAnalysis nodes concurrently using
// errgroup. If any node fails, a warning is logged and the remaining nodes continue
// unaffected (partial failures do not abort the phase). If phaseTimeout is positive, it
// is applied as a deadline for the entire phase, cancelling any nodes that have not yet
// completed. An AgentDecisionMade event is emitted after each node completes successfully.
//
// This method always returns nil; analyst node failures are tolerated and surfaced only
// through log warnings. The error return is reserved for future structural failures
// (e.g., a cancelled parent context passed before any node is launched).
func (p *Pipeline) executeAnalysisPhase(ctx context.Context, state *PipelineState) error {
	phaseCtx := ctx
	if p.phaseTimeout > 0 {
		var cancel context.CancelFunc
		phaseCtx, cancel = context.WithTimeout(ctx, p.phaseTimeout)
		defer cancel()
	}

	g, gCtx := errgroup.WithContext(phaseCtx)

	for _, n := range p.nodes {
		if n.Phase() != PhaseAnalysis {
			continue
		}
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
				p.events <- PipelineEvent{
					Type:          AgentDecisionMade,
					PipelineRunID: state.PipelineRunID,
					StrategyID:    state.StrategyID,
					Ticker:        state.Ticker,
					AgentRole:     AgentRole(node.Name()),
					Phase:         PhaseAnalysis,
					OccurredAt:    time.Now().UTC(),
				}
			}
			return nil
		})
	}

	return g.Wait()
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
