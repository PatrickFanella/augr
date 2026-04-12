package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// PipelineConfig holds timeout and debate-round configuration for a Pipeline.
type PipelineConfig struct {
	PipelineTimeout      time.Duration
	PhaseTimeout         time.Duration
	ResearchDebateRounds int
	RiskDebateRounds     int
	SkipPhases           map[Phase]bool
}

// Pipeline holds all dependencies and configuration needed by the executor.
type Pipeline struct {
	nodes          map[Phase][]Node
	persister      DecisionPersister
	events         chan<- PipelineEvent
	logger         *slog.Logger
	config         PipelineConfig
	nowMu          sync.RWMutex
	now            func() time.Time
	configSnapshot json.RawMessage // set by ExecuteStrategy for auditability
	helper         PhaseHelper
}

// NewPipeline constructs a Pipeline with the supplied dependencies. Default
// debate-round counts of 3 are applied when the config fields are zero.
func NewPipeline(
	config PipelineConfig,
	persister DecisionPersister,
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
	p := &Pipeline{
		nodes:     make(map[Phase][]Node),
		persister: persister,
		events:    events,
		logger:    logger,
		config:    config,
		now:       time.Now,
	}
	p.helper = newPhaseHelper(persister, events, logger, p.currentTime)
	return p
}

// SetNowFunc overrides the pipeline time source, allowing backtests to inject a
// simulated clock instead of wall-clock time.
func (p *Pipeline) SetNowFunc(now func() time.Time) {
	if p == nil || now == nil {
		return
	}

	p.nowMu.Lock()
	defer p.nowMu.Unlock()

	p.now = now
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

// debatePhaseSpec describes the roles and configuration for a single debate
// phase so that executeDebatePhase can handle both research and risk debates.
type debatePhaseSpec struct {
	phase       Phase
	rounds      int
	debaters    []AgentRole
	judge       AgentRole
	appendRound func(*PipelineState, DebateRound)
}

// executeDebatePhase resolves the required nodes for a debate, validates they
// exist, and runs the configured number of rounds via DebateExecutor.
func (p *Pipeline) executeDebatePhase(ctx context.Context, state *PipelineState, spec debatePhaseSpec) error {
	debaters := make([]Node, 0, len(spec.debaters))
	for _, role := range spec.debaters {
		n := p.nodeByRole(spec.phase, role)
		if n == nil {
			return fmt.Errorf("agent/pipeline: %s phase requires a %s node", spec.phase, role)
		}
		debaters = append(debaters, n)
	}

	judgeNode := p.nodeByRole(spec.phase, spec.judge)
	if judgeNode == nil {
		return fmt.Errorf("agent/pipeline: %s phase requires a %s node", spec.phase, spec.judge)
	}

	return NewDebateExecutor(DebateContext{
		Helper:          p.helper,
		Persister:       p.persister,
		Events:          p.events,
		Logger:          p.logger,
		NowFunc:         p.currentTime,
		DecisionPayload: p.decisionPayload,
	}, DebateConfig{
		Phase:       spec.phase,
		Rounds:      spec.rounds,
		Debaters:    debaters,
		Judge:       judgeNode,
		AppendRound: spec.appendRound,
	}).Execute(ctx, state)
}

// executeResearchDebatePhase runs the multi-round research debate. For each
// round (up to config.ResearchDebateRounds), the BullResearcher and
// BearResearcher nodes execute sequentially. A DebateRoundCompleted event is
// emitted after each completed round. After all rounds the InvestJudge node
// runs to produce the investment plan.
func (p *Pipeline) executeResearchDebatePhase(ctx context.Context, state *PipelineState) error {
	return p.executeDebatePhase(ctx, state, debatePhaseSpec{
		phase:    PhaseResearchDebate,
		rounds:   p.config.ResearchDebateRounds,
		debaters: []AgentRole{AgentRoleBullResearcher, AgentRoleBearResearcher},
		judge:    AgentRoleInvestJudge,
		appendRound: func(s *PipelineState, r DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, r)
		},
	})
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
			p.helper.persistStructuredEvent(gCtx, p.helper.newStructuredEvent(
				state.PipelineRunID,
				state.StrategyID,
				AgentEventKindAgentStarted,
				node.Role(),
				"Agent started",
				"",
				map[string]any{
					"phase":      PhaseAnalysis.String(),
					"agent_role": node.Role().String(),
				},
				[]string{"agent", PhaseAnalysis.String()},
			))

			if an, ok := node.(AnalystNode); ok {
				result, err := an.Analyze(gCtx, analysisInputFromState(state))
				if err != nil {
					p.logger.Warn("agent/pipeline: analyst node failed",
						slog.String("node", node.Name()),
						slog.Any("error", err),
					)
					return nil // partial failures are tolerated; do not abort the phase
				}
				applyAnalysisOutput(state, node.Role(), result)
			} else {
				if err := node.Execute(gCtx, state); err != nil {
					p.logger.Warn("agent/pipeline: analyst node failed",
						slog.String("node", node.Name()),
						slog.Any("error", err),
					)
					return nil // partial failures are tolerated; do not abort the phase
				}
			}
			output, llmResponse, err := p.decisionPayload(state, node, nil)
			if err != nil {
				return err
			}
			if err := p.persister.PersistDecision(gCtx, state.PipelineRunID, node, nil, output, llmResponse); err != nil {
				return err
			}
			p.helper.persistStructuredEvent(gCtx, p.helper.newStructuredEvent(
				state.PipelineRunID,
				state.StrategyID,
				AgentEventKindAgentCompleted,
				node.Role(),
				"Agent completed",
				"",
				map[string]any{
					"phase":      PhaseAnalysis.String(),
					"agent_role": node.Role().String(),
				},
				[]string{"agent", PhaseAnalysis.String()},
			))

			if p.events != nil {
				event := PipelineEvent{
					Type:          AgentDecisionMade,
					PipelineRunID: state.PipelineRunID,
					StrategyID:    state.StrategyID,
					Ticker:        state.Ticker,
					AgentRole:     node.Role(),
					Phase:         PhaseAnalysis,
					OccurredAt:    p.currentTime().UTC(),
				}
				// Non-blocking send: drop the event rather than let the goroutine
				// stall if the channel is full or the phase context is cancelled.
				select {
				case p.events <- event:
				case <-gCtx.Done():
					p.logger.Debug("agent/pipeline: AgentDecisionMade event dropped; phase context cancelled",
						slog.String("node", node.Name()),
					)
				default:
					p.logger.Debug("agent/pipeline: AgentDecisionMade event dropped; events channel full",
						slog.String("node", node.Name()),
					)
				}
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	return p.helper.persistAnalysisSnapshots(phaseCtx, state)
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
	p.helper.persistStructuredEvent(phaseCtx, p.helper.newStructuredEvent(
		state.PipelineRunID,
		state.StrategyID,
		AgentEventKindAgentStarted,
		traderNode.Role(),
		"Agent started",
		"",
		map[string]any{
			"phase":      PhaseTrading.String(),
			"agent_role": traderNode.Role().String(),
		},
		[]string{"agent", PhaseTrading.String()},
	))

	if tn, ok := traderNode.(TraderNode); ok {
		input := tradingInputFromState(state)
		result, err := tn.Trade(phaseCtx, input)
		if err != nil {
			return err
		}
		applyTradingOutput(state, result)
	} else {
		if err := traderNode.Execute(phaseCtx, state); err != nil {
			return err
		}
	}
	output, llmResponse, err := p.decisionPayload(state, traderNode, nil)
	if err != nil {
		return err
	}
	if err := p.persister.PersistDecision(phaseCtx, state.PipelineRunID, traderNode, nil, output, llmResponse); err != nil {
		return err
	}
	p.helper.persistStructuredEvent(phaseCtx, p.helper.newStructuredEvent(
		state.PipelineRunID,
		state.StrategyID,
		AgentEventKindAgentCompleted,
		traderNode.Role(),
		"Agent completed",
		"",
		map[string]any{
			"phase":      PhaseTrading.String(),
			"agent_role": traderNode.Role().String(),
		},
		[]string{"agent", PhaseTrading.String()},
	))
	p.helper.persistStructuredEvent(phaseCtx, p.helper.newStructuredEvent(
		state.PipelineRunID,
		state.StrategyID,
		AgentEventKindSignalProduced,
		traderNode.Role(),
		"Signal produced",
		"",
		map[string]any{
			"phase":        PhaseTrading.String(),
			"agent_role":   traderNode.Role().String(),
			"signal_value": state.TradingPlan.Action.String(),
		},
		[]string{"signal", PhaseTrading.String()},
	))

	if p.events != nil {
		event := PipelineEvent{
			Type:          AgentDecisionMade,
			PipelineRunID: state.PipelineRunID,
			StrategyID:    state.StrategyID,
			Ticker:        state.Ticker,
			AgentRole:     traderNode.Role(),
			Phase:         PhaseTrading,
			OccurredAt:    p.currentTime().UTC(),
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
	return p.executeDebatePhase(ctx, state, debatePhaseSpec{
		phase:    PhaseRiskDebate,
		rounds:   p.config.RiskDebateRounds,
		debaters: []AgentRole{AgentRoleAggressiveAnalyst, AgentRoleConservativeAnalyst, AgentRoleNeutralAnalyst},
		judge:    AgentRoleRiskManager,
		appendRound: func(s *PipelineState, r DebateRound) {
			s.RiskDebate.Rounds = append(s.RiskDebate.Rounds, r)
		},
	})
}

// ExecuteStrategy runs the full pipeline for the given strategy, resolving
// the strategy's JSON config against the provided global settings before
// execution begins. The resolved config overrides debate rounds, timeouts,
// and LLM selections. A JSON snapshot of the resolved config is stored in
// the pipeline run for auditability.
//
// Callers that do not need config resolution can continue using Execute directly.
func (p *Pipeline) ExecuteStrategy(ctx context.Context, strategy domain.Strategy, globals GlobalSettings) (*PipelineState, error) {
	// Parse the strategy's JSONB config into a typed StrategyConfig.
	var stratCfg *StrategyConfig
	if len(strategy.Config) > 0 {
		var parsed StrategyConfig
		if err := json.Unmarshal(strategy.Config, &parsed); err != nil {
			return nil, fmt.Errorf("agent/pipeline: parse strategy config: %w", err)
		}
		stratCfg = &parsed
	}

	resolved := ResolveConfig(stratCfg, globals)

	// Save original config values and restore them when done so that resolved
	// strategy config does not leak into subsequent runs.
	origResearchDebateRounds := p.config.ResearchDebateRounds
	origRiskDebateRounds := p.config.RiskDebateRounds
	origPhaseTimeout := p.config.PhaseTimeout
	defer func() {
		p.config.ResearchDebateRounds = origResearchDebateRounds
		p.config.RiskDebateRounds = origRiskDebateRounds
		p.config.PhaseTimeout = origPhaseTimeout
		p.configSnapshot = nil
	}()

	// Apply resolved pipeline config to the pipeline's runtime config.
	p.config.ResearchDebateRounds = resolved.PipelineConfig.DebateRounds
	p.config.RiskDebateRounds = resolved.PipelineConfig.DebateRounds
	// Skip the risk debate phase — the research debate + trader already
	// incorporate risk assessment, and this phase doubles the pipeline time.
	if p.config.SkipPhases == nil {
		p.config.SkipPhases = make(map[Phase]bool)
	}
	p.config.SkipPhases[PhaseRiskDebate] = true
	if resolved.PipelineConfig.AnalysisTimeoutSeconds > 0 {
		p.config.PhaseTimeout = time.Duration(resolved.PipelineConfig.AnalysisTimeoutSeconds) * time.Second
	}

	// Snapshot the resolved config for auditability and store it so Execute
	// can attach it to the PipelineRun.
	configSnapshot, _ := json.Marshal(resolved)
	p.configSnapshot = configSnapshot

	return p.Execute(ctx, strategy.ID, strategy.Ticker)
}

// Execute runs the full pipeline for the given strategy and ticker. It creates
// a PipelineRun record in the database (status=running), applies the
// pipeline-level timeout from config, and executes the four phases in order:
// Analysis → ResearchDebate → Trading → RiskDebate. A PipelineStarted event
// is emitted at the beginning, and either PipelineCompleted or PipelineError
// at the end. The PipelineRun status is updated to completed or failed
// accordingly.
func (p *Pipeline) Execute(ctx context.Context, strategyID uuid.UUID, ticker string) (*PipelineState, error) {
	if p.persister == nil {
		return nil, fmt.Errorf("agent/pipeline: persister is required")
	}

	// Apply pipeline-level timeout when configured.
	if p.config.PipelineTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.PipelineTimeout)
		defer cancel()
	}
	cacheStatsCollector := llm.NewCacheStatsCollector()
	ctx = llm.WithCacheStatsCollector(ctx, cacheStatsCollector)

	now := p.currentTime().UTC()
	run := &domain.PipelineRun{
		ID:             uuid.New(),
		StrategyID:     strategyID,
		Ticker:         ticker,
		TradeDate:      now.Truncate(24 * time.Hour),
		Status:         domain.PipelineStatusRunning,
		StartedAt:      now,
		ConfigSnapshot: p.configSnapshot,
	}

	if err := p.persister.RecordRunStart(ctx, run); err != nil {
		return nil, err
	}

	state := &PipelineState{
		PipelineRunID: run.ID,
		StrategyID:    strategyID,
		Ticker:        ticker,
		mu:            &sync.Mutex{},
	}

	// Emit PipelineStarted event.
	p.helper.persistStructuredEvent(ctx, p.helper.newStructuredEvent(
		run.ID,
		strategyID,
		AgentEventKindPipelineStarted,
		"",
		"Pipeline started",
		"",
		nil,
		[]string{"pipeline"},
	))
	p.helper.emitEvent(PipelineEvent{
		Type:          PipelineStarted,
		PipelineRunID: run.ID,
		StrategyID:    strategyID,
		Ticker:        ticker,
		OccurredAt:    p.currentTime().UTC(),
	})

	// Execute phases in order.
	phases := []struct {
		name  string
		phase Phase
		fn    func(context.Context, *PipelineState) error
	}{
		{"analysis", PhaseAnalysis, p.executeAnalysisPhase},
		{"research_debate", PhaseResearchDebate, p.executeResearchDebatePhase},
		{"trading", PhaseTrading, p.executeTradingPhase},
		{"risk_debate", PhaseRiskDebate, p.executeRiskDebatePhase},
	}

	phaseTimingsMap := make(map[string]int64)

	for _, phase := range phases {
		if p.config.SkipPhases[phase.phase] {
			continue
		}
		p.helper.persistStructuredEvent(ctx, p.helper.newStructuredEvent(
			run.ID,
			strategyID,
			AgentEventKindPhaseStarted,
			"",
			"Phase started",
			"",
			map[string]any{
				"phase": phase.name,
			},
			[]string{"phase", phase.name},
		))
		phaseStart := time.Now()
		if err := phase.fn(ctx, state); err != nil {
			elapsed := time.Since(phaseStart).Milliseconds()
			phaseTimingsMap[phase.name+"_ms"] = elapsed
			p.logger.Error("agent/pipeline: phase failed",
				slog.String("phase", phase.name),
				slog.Any("error", err),
			)

			completedAt := p.currentTime().UTC()
			phaseTimingsJSON, _ := json.Marshal(phaseTimingsMap)
			_ = p.persister.RecordRunComplete(ctx, run.ID, run.TradeDate, domain.PipelineStatusFailed, completedAt, err.Error(), phaseTimingsJSON)
			p.helper.emitCacheStats(state, cacheStatsCollector, run.ID, strategyID, ticker)
			p.helper.persistStructuredTerminalEvent(p.helper.newStructuredEvent(
				run.ID,
				strategyID,
				AgentEventKindPipelineFailed,
				"",
				"Pipeline failed",
				err.Error(),
				map[string]any{
					"phase":         phase.name,
					"error_message": err.Error(),
				},
				[]string{"pipeline", "failed"},
			))

			p.helper.emitEvent(PipelineEvent{
				Type:          PipelineError,
				PipelineRunID: run.ID,
				StrategyID:    strategyID,
				Ticker:        ticker,
				Error:         err.Error(),
				TimedOut:      errors.Is(err, context.DeadlineExceeded),
				OccurredAt:    p.currentTime().UTC(),
			})

			return state, err
		}
		elapsed := time.Since(phaseStart).Milliseconds()
		phaseTimingsMap[phase.name+"_ms"] = elapsed
		p.helper.persistStructuredEvent(ctx, p.helper.newStructuredEvent(
			run.ID,
			strategyID,
			AgentEventKindPhaseCompleted,
			"",
			"Phase completed",
			"",
			map[string]any{
				"phase": phase.name,
			},
			[]string{"phase", phase.name},
		))
	}

	// All phases succeeded – mark the run as completed.
	completedAt := p.currentTime().UTC()
	phaseTimingsJSON, _ := json.Marshal(phaseTimingsMap)
	_ = p.persister.RecordRunComplete(ctx, run.ID, run.TradeDate, domain.PipelineStatusCompleted, completedAt, "", phaseTimingsJSON)
	p.helper.emitCacheStats(state, cacheStatsCollector, run.ID, strategyID, ticker)
	p.helper.persistStructuredTerminalEvent(p.helper.newStructuredEvent(
		run.ID,
		strategyID,
		AgentEventKindPipelineCompleted,
		"",
		"Pipeline completed",
		"",
		nil,
		[]string{"pipeline", "completed"},
	))

	p.helper.emitEvent(PipelineEvent{
		Type:          PipelineCompleted,
		PipelineRunID: run.ID,
		StrategyID:    strategyID,
		Ticker:        ticker,
		UsedFallback:  state.UsedFallback,
		TimedOut:      state.TimedOut,
		OccurredAt:    p.currentTime().UTC(),
	})

	return state, nil
}

// emitEvent sends an event to the events channel in a non-blocking fashion.

func (p *Pipeline) currentTime() time.Time {
	if p == nil {
		return time.Now()
	}

	p.nowMu.RLock()
	defer p.nowMu.RUnlock()

	if p.now == nil {
		return time.Now()
	}

	return p.now()
}

func (p *Pipeline) decisionPayload(state *PipelineState, node Node, roundNumber *int) (string, *DecisionLLMResponse, error) {
	if decision, ok := state.Decision(node.Role(), node.Phase(), roundNumber); ok {
		return decision.OutputText, decision.LLMResponse, nil
	}

	switch node.Phase() {
	case PhaseAnalysis:
		return state.GetAnalystReport(node.Role()), nil, nil
	case PhaseResearchDebate:
		if node.Role() == AgentRoleInvestJudge {
			return state.ResearchDebate.InvestmentPlan, nil, nil
		}
		return debateContribution(state.ResearchDebate.Rounds, node.Role(), roundNumber), nil, nil
	case PhaseTrading:
		tradingPlanJSON, err := json.Marshal(state.TradingPlan)
		if err != nil {
			return "", nil, fmt.Errorf("agent/pipeline: marshal trading plan output: %w", err)
		}
		return string(tradingPlanJSON), nil, nil
	case PhaseRiskDebate:
		if node.Role() == AgentRoleRiskManager {
			return state.RiskDebate.FinalSignal, nil, nil
		}
		return debateContribution(state.RiskDebate.Rounds, node.Role(), roundNumber), nil, nil
	default:
		return "", nil, nil
	}
}

func debateContribution(rounds []DebateRound, role AgentRole, roundNumber *int) string {
	if roundNumber == nil {
		return ""
	}

	roundIndex := *roundNumber - 1
	if roundIndex < 0 || roundIndex >= len(rounds) {
		return ""
	}

	return rounds[roundIndex].Contributions[role]
}

func cloneRoundNumber(roundNumber *int) *int {
	if roundNumber == nil {
		return nil
	}

	value := *roundNumber
	return &value
}
