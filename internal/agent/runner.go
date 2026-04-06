package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// AnalysisAgent is the runner-facing contract for analysis participants.
type AnalysisAgent interface {
	Name() string
	Role() AgentRole
	Analyze(ctx context.Context, input AnalysisInput) (AnalysisOutput, error)
}

// DebateAgent is the runner-facing contract for debate participants.
type DebateAgent interface {
	Name() string
	Role() AgentRole
	Debate(ctx context.Context, input DebateInput) (DebateOutput, error)
}

// ResearchJudge is the runner-facing contract for the invest judge.
type ResearchJudge interface {
	Name() string
	Role() AgentRole
	JudgeResearch(ctx context.Context, input DebateInput) (ResearchJudgeOutput, error)
}

// TradeAgent is the runner-facing contract for the trader.
type TradeAgent interface {
	Name() string
	Role() AgentRole
	Trade(ctx context.Context, input TradingInput) (TradingOutput, error)
}

// RiskJudge is the runner-facing contract for the risk manager.
type RiskJudge interface {
	Name() string
	Role() AgentRole
	JudgeRisk(ctx context.Context, input RiskJudgeInput) (RiskJudgeOutput, error)
}

// RunPersister is the runner dependency responsible for durable run state.
type RunPersister = DecisionPersister

// Dependencies holds runner infrastructure dependencies.
type Dependencies struct {
	Persister RunPersister
	Events    chan<- PipelineEvent
	Logger    *slog.Logger
	Clock     func() time.Time
}

// Definition describes the concrete participants for each stage.
type Definition struct {
	Analysis []AnalysisAgent
	Research ResearchDebateStage
	Trader   TradeAgent
	Risk     RiskDebateStage
}

// ResearchDebateStage holds debate participants and the final research judge.
type ResearchDebateStage struct {
	Debaters []DebateAgent
	Judge    ResearchJudge
}

// RiskDebateStage holds risk debate participants and the final risk judge.
type RiskDebateStage struct {
	Debaters []DebateAgent
	Judge    RiskJudge
}

// RuntimeConfig contains the per-run execution settings after resolution.
type RuntimeConfig struct {
	PipelineTimeout time.Duration
	AnalysisTimeout time.Duration
	TradingTimeout  time.Duration
	DebateTimeout   time.Duration
	ResearchRounds  int
	RiskRounds      int
	SkipPhases      map[Phase]bool
}

// InitialStateSeed captures pre-fetched pipeline inputs that should be loaded
// into a run before phase execution starts.
type InitialStateSeed struct {
	Market       *MarketData
	News         []data.NewsArticle
	Fundamentals *data.Fundamentals
	Social       *data.SocialSentiment
}

// PreparedRun is the immutable execution plan for one strategy run.
type PreparedRun struct {
	Strategy       domain.Strategy
	Config         ResolvedConfig
	Runtime        RuntimeConfig
	ConfigSnapshot json.RawMessage
	InitialState   InitialStateSeed
}

// RunWarning captures non-fatal execution warnings surfaced to callers.
type RunWarning struct {
	Phase      Phase
	Role       AgentRole
	Message    string
	OccurredAt time.Time
}

// StateView is the runner result snapshot exposed to callers/tests.
type StateView struct {
	PipelineRunID  uuid.UUID
	StrategyID     uuid.UUID
	Ticker         string
	AnalystReports map[AgentRole]string
	ResearchDebate ResearchDebateState
	TradingPlan    TradingPlan
	RiskDebate     RiskDebateState
	FinalSignal    FinalSignal
	LLMCacheStats  llm.CacheStats
}

// RunResult is the runner output for one completed or failed run.
type RunResult struct {
	Run      domain.PipelineRun
	Signal   domain.PipelineSignal
	State    StateView
	Warnings []RunWarning
}

// Runner owns immutable run preparation and execution.
type Runner struct {
	def       Definition
	persister RunPersister
	events    chan<- PipelineEvent
	logger    *slog.Logger
	nowMu     sync.RWMutex
	now       func() time.Time
	helper    PhaseHelper
}

// NewRunner constructs a runner with the supplied participants and dependencies.
func NewRunner(def Definition, deps Dependencies) *Runner {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	r := &Runner{
		def:       def,
		persister: deps.Persister,
		events:    deps.Events,
		logger:    logger,
		now:       clock,
	}
	r.helper = newPhaseHelper(deps.Persister, deps.Events, logger, r.currentTime)
	return r
}

// SetNowFunc overrides the runner time source for tests/backtests.
func (r *Runner) SetNowFunc(now func() time.Time) {
	if r == nil || now == nil {
		return
	}
	r.nowMu.Lock()
	defer r.nowMu.Unlock()
	r.now = now
}

// Prepare resolves a strategy config into an immutable prepared run.
func (r *Runner) Prepare(strategy domain.Strategy, globals GlobalSettings) (PreparedRun, error) {
	var stratCfg *StrategyConfig
	if len(strategy.Config) > 0 {
		var parsed StrategyConfig
		if err := json.Unmarshal(strategy.Config, &parsed); err != nil {
			return PreparedRun{}, fmt.Errorf("agent/runner: parse strategy config: %w", err)
		}
		stratCfg = &parsed
	}

	resolved := ResolveConfig(stratCfg, globals)
	configSnapshot, err := json.Marshal(resolved)
	if err != nil {
		return PreparedRun{}, fmt.Errorf("agent/runner: marshal config snapshot: %w", err)
	}

	return PreparedRun{
		Strategy: strategy,
		Config:   resolved,
		Runtime: RuntimeConfig{
			PipelineTimeout: runtimePipelineTimeout(resolved),
			AnalysisTimeout: runtimeAnalysisTimeout(resolved),
			TradingTimeout:  runtimeAnalysisTimeout(resolved),
			DebateTimeout:   runtimeDebateTimeout(resolved),
			ResearchRounds:  resolved.PipelineConfig.DebateRounds,
			RiskRounds:      resolved.PipelineConfig.DebateRounds,
			SkipPhases:      defaultSkipPhases(),
		},
		ConfigSnapshot: configSnapshot,
	}, nil
}

// RunStrategy prepares and executes one strategy run.
func (r *Runner) RunStrategy(ctx context.Context, strategy domain.Strategy, globals GlobalSettings) (*RunResult, error) {
	prepared, err := r.Prepare(strategy, globals)
	if err != nil {
		return nil, err
	}
	return r.Run(ctx, prepared)
}

// Run executes one prepared run and returns the canonical result.
func (r *Runner) Run(ctx context.Context, prepared PreparedRun) (*RunResult, error) {
	if r.persister == nil {
		return nil, fmt.Errorf("agent/runner: persister is required")
	}

	if prepared.Runtime.PipelineTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, prepared.Runtime.PipelineTimeout)
		defer cancel()
	}

	cacheStatsCollector := llm.NewCacheStatsCollector()
	ctx = llm.WithCacheStatsCollector(ctx, cacheStatsCollector)

	now := r.currentTime().UTC()
	run := domain.PipelineRun{
		ID:             uuid.New(),
		StrategyID:     prepared.Strategy.ID,
		Ticker:         prepared.Strategy.Ticker,
		TradeDate:      now.Truncate(24 * time.Hour),
		Status:         domain.PipelineStatusRunning,
		StartedAt:      now,
		ConfigSnapshot: prepared.ConfigSnapshot,
	}
	if err := r.persister.RecordRunStart(ctx, &run); err != nil {
		return nil, err
	}

	state := &PipelineState{
		PipelineRunID: run.ID,
		StrategyID:    prepared.Strategy.ID,
		Ticker:        prepared.Strategy.Ticker,
		mu:            &sync.Mutex{},
	}
	applyInitialStateSeed(state, prepared.InitialState)

	r.helper.persistStructuredEvent(ctx, r.helper.newStructuredEvent(
		run.ID,
		prepared.Strategy.ID,
		AgentEventKindPipelineStarted,
		"",
		"Pipeline started",
		"",
		nil,
		[]string{"pipeline"},
	))
	r.helper.emitEvent(PipelineEvent{
		Type:          PipelineStarted,
		PipelineRunID: run.ID,
		StrategyID:    prepared.Strategy.ID,
		Ticker:        prepared.Strategy.Ticker,
		OccurredAt:    r.currentTime().UTC(),
	})

	phaseTimings := map[string]int64{}
	warnings := make([]RunWarning, 0)
	var warningsMu sync.Mutex
	phases := []struct {
		name  string
		phase Phase
		fn    func(context.Context, *PipelineState, PreparedRun, *[]RunWarning, *sync.Mutex) error
	}{
		{"analysis", PhaseAnalysis, r.runAnalysis},
		{"research_debate", PhaseResearchDebate, r.runResearchDebate},
		{"trading", PhaseTrading, r.runTrading},
		{"risk_debate", PhaseRiskDebate, r.runRiskDebate},
	}

	for _, phase := range phases {
		if prepared.Runtime.SkipPhases[phase.phase] {
			continue
		}
		r.helper.persistStructuredEvent(ctx, r.helper.newStructuredEvent(
			run.ID,
			prepared.Strategy.ID,
			AgentEventKindPhaseStarted,
			"",
			"Phase started",
			"",
			map[string]any{"phase": phase.name},
			[]string{"phase", phase.name},
		))
		phaseStart := time.Now()
		if err := phase.fn(ctx, state, prepared, &warnings, &warningsMu); err != nil {
			phaseTimings[phase.name+"_ms"] = time.Since(phaseStart).Milliseconds()
			completedAt := r.currentTime().UTC()
			phaseTimingsJSON, _ := json.Marshal(phaseTimings)
			_ = r.persister.RecordRunComplete(ctx, run.ID, run.TradeDate, domain.PipelineStatusFailed, completedAt, err.Error(), phaseTimingsJSON)
			r.helper.emitCacheStats(state, cacheStatsCollector, run.ID, prepared.Strategy.ID, prepared.Strategy.Ticker)
			r.helper.persistStructuredTerminalEvent(r.helper.newStructuredEvent(
				run.ID,
				prepared.Strategy.ID,
				AgentEventKindPipelineFailed,
				"",
				"Pipeline failed",
				err.Error(),
				map[string]any{"phase": phase.name, "error_message": err.Error()},
				[]string{"pipeline", "failed"},
			))
			r.helper.emitEvent(PipelineEvent{
				Type:          PipelineError,
				PipelineRunID: run.ID,
				StrategyID:    prepared.Strategy.ID,
				Ticker:        prepared.Strategy.Ticker,
				Error:         err.Error(),
				OccurredAt:    r.currentTime().UTC(),
			})
			run.Status = domain.PipelineStatusFailed
			run.CompletedAt = &completedAt
			run.ErrorMessage = err.Error()
			return &RunResult{Run: run, Signal: r.canonicalSignal(state), State: snapshotState(state), Warnings: warnings}, err
		}
		phaseTimings[phase.name+"_ms"] = time.Since(phaseStart).Milliseconds()
		r.helper.persistStructuredEvent(ctx, r.helper.newStructuredEvent(
			run.ID,
			prepared.Strategy.ID,
			AgentEventKindPhaseCompleted,
			"",
			"Phase completed",
			"",
			map[string]any{"phase": phase.name},
			[]string{"phase", phase.name},
		))
	}

	completedAt := r.currentTime().UTC()
	phaseTimingsJSON, _ := json.Marshal(phaseTimings)
	_ = r.persister.RecordRunComplete(ctx, run.ID, run.TradeDate, domain.PipelineStatusCompleted, completedAt, "", phaseTimingsJSON)
	r.helper.emitCacheStats(state, cacheStatsCollector, run.ID, prepared.Strategy.ID, prepared.Strategy.Ticker)
	r.helper.persistStructuredTerminalEvent(r.helper.newStructuredEvent(
		run.ID,
		prepared.Strategy.ID,
		AgentEventKindPipelineCompleted,
		"",
		"Pipeline completed",
		"",
		nil,
		[]string{"pipeline", "completed"},
	))
	r.helper.emitEvent(PipelineEvent{
		Type:          PipelineCompleted,
		PipelineRunID: run.ID,
		StrategyID:    prepared.Strategy.ID,
		Ticker:        prepared.Strategy.Ticker,
		OccurredAt:    r.currentTime().UTC(),
	})
	run.Status = domain.PipelineStatusCompleted
	run.CompletedAt = &completedAt

	return &RunResult{Run: run, Signal: r.canonicalSignal(state), State: snapshotState(state), Warnings: warnings}, nil
}

func applyInitialStateSeed(state *PipelineState, seed InitialStateSeed) {
	if state == nil {
		return
	}

	if seed.Market != nil {
		state.Market = &MarketData{
			Bars:       cloneOHLCV(seed.Market.Bars),
			Indicators: cloneIndicators(seed.Market.Indicators),
		}
	}
	if len(seed.News) > 0 {
		state.News = cloneNewsArticles(seed.News)
	}
	if seed.Fundamentals != nil {
		fundamentals := *seed.Fundamentals
		state.Fundamentals = &fundamentals
	}
	if seed.Social != nil {
		social := *seed.Social
		state.Social = &social
	}
}

func cloneOHLCV(src []domain.OHLCV) []domain.OHLCV {
	if len(src) == 0 {
		return nil
	}
	dst := make([]domain.OHLCV, len(src))
	copy(dst, src)
	return dst
}

func cloneIndicators(src []domain.Indicator) []domain.Indicator {
	if len(src) == 0 {
		return nil
	}
	dst := make([]domain.Indicator, len(src))
	copy(dst, src)
	return dst
}

func cloneNewsArticles(src []data.NewsArticle) []data.NewsArticle {
	if len(src) == 0 {
		return nil
	}
	dst := make([]data.NewsArticle, len(src))
	copy(dst, src)
	return dst
}

func (r *Runner) runAnalysis(ctx context.Context, state *PipelineState, prepared PreparedRun, warnings *[]RunWarning, warningsMu *sync.Mutex) error {
	phaseCtx := ctx
	if prepared.Runtime.AnalysisTimeout > 0 {
		var cancel context.CancelFunc
		phaseCtx, cancel = context.WithTimeout(ctx, prepared.Runtime.AnalysisTimeout)
		defer cancel()
	}

	g, gCtx := errgroup.WithContext(phaseCtx)
	for _, participant := range r.def.Analysis {
		agent := participant
		g.Go(func() error {
			r.helper.persistStructuredEvent(gCtx, r.helper.newStructuredEvent(
				state.PipelineRunID,
				state.StrategyID,
				AgentEventKindAgentStarted,
				agent.Role(),
				agent.Name(),
				"",
				map[string]any{"phase": PhaseAnalysis.String(), "agent_role": agent.Role().String()},
				[]string{"agent", PhaseAnalysis.String()},
			))

			output, err := agent.Analyze(gCtx, analysisInputFromState(state))
			if err != nil {
				r.logger.Warn("agent/runner: analysis participant failed",
					slog.String("node", agent.Name()),
					slog.Any("error", err),
				)
				warningsMu.Lock()
				*warnings = append(*warnings, RunWarning{Phase: PhaseAnalysis, Role: agent.Role(), Message: err.Error(), OccurredAt: r.currentTime().UTC()})
				warningsMu.Unlock()
				return nil
			}

			applyAnalysisOutput(state, agent.Role(), output)
			if err := r.persistDecision(gCtx, state.PipelineRunID, agent.Name(), agent.Role(), PhaseAnalysis, nil, output.Report, output.LLMResponse); err != nil {
				return err
			}
			r.helper.persistStructuredEvent(gCtx, r.helper.newStructuredEvent(
				state.PipelineRunID,
				state.StrategyID,
				AgentEventKindAgentCompleted,
				agent.Role(),
				agent.Name(),
				"",
				map[string]any{"phase": PhaseAnalysis.String(), "agent_role": agent.Role().String()},
				[]string{"agent", PhaseAnalysis.String()},
			))
			r.helper.emitEvent(PipelineEvent{Type: AgentDecisionMade, PipelineRunID: state.PipelineRunID, StrategyID: state.StrategyID, Ticker: state.Ticker, AgentRole: agent.Role(), Phase: PhaseAnalysis, OccurredAt: r.currentTime().UTC()})
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return r.helper.persistAnalysisSnapshots(phaseCtx, state)
}

func (r *Runner) runResearchDebate(ctx context.Context, state *PipelineState, prepared PreparedRun, _ *[]RunWarning, _ *sync.Mutex) error {
	return r.runDebate(ctx, state, debateRuntimeSpec{
		phase:    PhaseResearchDebate,
		timeout:  prepared.Runtime.DebateTimeout,
		rounds:   prepared.Runtime.ResearchRounds,
		debaters: r.def.Research.Debaters,
		judge:    r.def.Research.Judge,
		appendRound: func(s *PipelineState, round DebateRound) {
			s.ResearchDebate.Rounds = append(s.ResearchDebate.Rounds, round)
		},
	})
}

func (r *Runner) runTrading(ctx context.Context, state *PipelineState, prepared PreparedRun, _ *[]RunWarning, _ *sync.Mutex) error {
	phaseCtx := ctx
	if prepared.Runtime.TradingTimeout > 0 {
		var cancel context.CancelFunc
		phaseCtx, cancel = context.WithTimeout(ctx, prepared.Runtime.TradingTimeout)
		defer cancel()
	}
	trader := r.def.Trader
	if trader == nil {
		return fmt.Errorf("agent/runner: trading phase requires a trader")
	}
	r.helper.persistStructuredEvent(phaseCtx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindAgentStarted, trader.Role(), trader.Name(), "", map[string]any{"phase": PhaseTrading.String(), "agent_role": trader.Role().String()}, []string{"agent", PhaseTrading.String()}))
	output, err := trader.Trade(phaseCtx, tradingInputFromState(state))
	if err != nil {
		return err
	}
	applyTradingOutput(state, output)
	if err := r.persistDecision(phaseCtx, state.PipelineRunID, trader.Name(), trader.Role(), PhaseTrading, nil, output.StoredOutput, output.LLMResponse); err != nil {
		return err
	}
	r.helper.persistStructuredEvent(phaseCtx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindAgentCompleted, trader.Role(), trader.Name(), "", map[string]any{"phase": PhaseTrading.String(), "agent_role": trader.Role().String()}, []string{"agent", PhaseTrading.String()}))
	r.helper.persistStructuredEvent(phaseCtx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindSignalProduced, trader.Role(), "Signal produced", "", map[string]any{"phase": PhaseTrading.String(), "agent_role": trader.Role().String(), "signal_value": state.TradingPlan.Action.String()}, []string{"signal", PhaseTrading.String()}))
	r.helper.emitEvent(PipelineEvent{Type: AgentDecisionMade, PipelineRunID: state.PipelineRunID, StrategyID: state.StrategyID, Ticker: state.Ticker, AgentRole: trader.Role(), Phase: PhaseTrading, OccurredAt: r.currentTime().UTC()})
	return nil
}

func (r *Runner) runRiskDebate(ctx context.Context, state *PipelineState, prepared PreparedRun, _ *[]RunWarning, _ *sync.Mutex) error {
	return r.runDebate(ctx, state, debateRuntimeSpec{
		phase:    PhaseRiskDebate,
		timeout:  prepared.Runtime.DebateTimeout,
		rounds:   prepared.Runtime.RiskRounds,
		debaters: r.def.Risk.Debaters,
		judge:    r.def.Risk.Judge,
		appendRound: func(s *PipelineState, round DebateRound) {
			s.RiskDebate.Rounds = append(s.RiskDebate.Rounds, round)
		},
	})
}

type debateRuntimeSpec struct {
	phase       Phase
	timeout     time.Duration
	rounds      int
	debaters    []DebateAgent
	judge       any
	appendRound func(*PipelineState, DebateRound)
}

func (r *Runner) runDebate(ctx context.Context, state *PipelineState, spec debateRuntimeSpec) error {
	if spec.rounds < 1 {
		r.logger.Warn("agent/runner: invalid debate rounds; clamping to 1", slog.String("phase", spec.phase.String()), slog.Int("configured_rounds", spec.rounds))
		spec.rounds = 1
	}

	newDebateTimeoutContext := func(parent context.Context) (context.Context, context.CancelFunc) {
		if spec.timeout > 0 {
			return context.WithTimeout(parent, spec.timeout)
		}
		return parent, func() {}
	}

	for i := 1; i <= spec.rounds; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		roundCtx, cancel := newDebateTimeoutContext(ctx)
		err := func() error {
			defer cancel()

			if err := roundCtx.Err(); err != nil {
				return err
			}

			spec.appendRound(state, DebateRound{Number: i, Contributions: make(map[AgentRole]string)})
			for _, debater := range spec.debaters {
				roundNumber := i
				r.helper.persistStructuredEvent(roundCtx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindAgentStarted, debater.Role(), debater.Name(), "", map[string]any{"phase": spec.phase.String(), "agent_role": debater.Role().String(), "round_number": roundNumber}, []string{"agent", spec.phase.String()}))
				output, err := debater.Debate(roundCtx, r.debateInputFromState(state, spec.phase))
				if err != nil {
					return err
				}
				applyDebateOutput(state, debater.Role(), spec.phase, roundNumber, output)
				if err := r.persistDecision(roundCtx, state.PipelineRunID, debater.Name(), debater.Role(), spec.phase, &roundNumber, output.Contribution, output.LLMResponse); err != nil {
					return err
				}
				r.helper.persistStructuredEvent(roundCtx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindAgentCompleted, debater.Role(), debater.Name(), "", map[string]any{"phase": spec.phase.String(), "agent_role": debater.Role().String(), "round_number": roundNumber}, []string{"agent", spec.phase.String()}))
			}
			r.helper.persistStructuredEvent(roundCtx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindDebateRoundCompleted, "", "Debate round completed", "", map[string]any{"phase": spec.phase.String(), "round_number": i}, []string{"debate", spec.phase.String()}))
			r.helper.emitEvent(PipelineEvent{Type: DebateRoundCompleted, PipelineRunID: state.PipelineRunID, StrategyID: state.StrategyID, Ticker: state.Ticker, Phase: spec.phase, Round: i, OccurredAt: r.currentTime().UTC()})
			return nil
		}()
		if err != nil {
			return err
		}
	}

	judgeCtx, cancel := newDebateTimeoutContext(ctx)
	defer cancel()
	return r.runDebateJudge(judgeCtx, state, spec.phase, spec.judge)
}

func (r *Runner) runDebateJudge(ctx context.Context, state *PipelineState, phase Phase, judge any) error {
	switch participant := judge.(type) {
	case ResearchJudge:
		r.helper.persistStructuredEvent(ctx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindAgentStarted, participant.Role(), participant.Name(), "", map[string]any{"phase": phase.String(), "agent_role": participant.Role().String()}, []string{"agent", phase.String()}))
		output, err := participant.JudgeResearch(ctx, researchJudgeInputFromState(state))
		if err != nil {
			return err
		}
		applyResearchJudgeOutput(state, output)
		if err := r.persistDecision(ctx, state.PipelineRunID, participant.Name(), participant.Role(), phase, nil, output.InvestmentPlan, output.LLMResponse); err != nil {
			return err
		}
		r.helper.persistStructuredEvent(ctx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindAgentCompleted, participant.Role(), participant.Name(), "", map[string]any{"phase": phase.String(), "agent_role": participant.Role().String()}, []string{"agent", phase.String()}))
		return nil
	case RiskJudge:
		r.helper.persistStructuredEvent(ctx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindAgentStarted, participant.Role(), participant.Name(), "", map[string]any{"phase": phase.String(), "agent_role": participant.Role().String()}, []string{"agent", phase.String()}))
		output, err := participant.JudgeRisk(ctx, riskJudgeInputFromState(state))
		if err != nil {
			return err
		}
		applyRiskJudgeOutput(state, output)
		if err := r.persistDecision(ctx, state.PipelineRunID, participant.Name(), participant.Role(), phase, nil, output.StoredSignal, output.LLMResponse); err != nil {
			return err
		}
		r.helper.persistStructuredEvent(ctx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindAgentCompleted, participant.Role(), participant.Name(), "", map[string]any{"phase": phase.String(), "agent_role": participant.Role().String()}, []string{"agent", phase.String()}))
		if phase == PhaseRiskDebate && state.RiskDebate.FinalSignal != "" {
			r.helper.persistStructuredEvent(ctx, r.helper.newStructuredEvent(state.PipelineRunID, state.StrategyID, AgentEventKindSignalProduced, participant.Role(), "Signal produced", "", map[string]any{"phase": phase.String(), "agent_role": participant.Role().String(), "signal_value": state.RiskDebate.FinalSignal}, []string{"signal", phase.String()}))
		}
		return nil
	default:
		return fmt.Errorf("agent/runner: missing judge for %s", phase)
	}
}

func (r *Runner) debateInputFromState(state *PipelineState, phase Phase) DebateInput {
	if phase == PhaseRiskDebate {
		return DebateInput{Ticker: state.Ticker, Rounds: state.RiskDebate.Rounds, ContextReports: map[AgentRole]string{AgentRoleTrader: MarshalTradingPlanSafe(state.TradingPlan)}}
	}
	return debateInputFromState(state)
}

func (r *Runner) persistDecision(ctx context.Context, runID uuid.UUID, name string, role AgentRole, phase Phase, roundNumber *int, output string, llmResponse *DecisionLLMResponse) error {
	return r.persister.PersistDecision(ctx, runID, decisionNode{name: name, role: role, phase: phase}, roundNumber, output, llmResponse)
}

type decisionNode struct {
	name  string
	role  AgentRole
	phase Phase
}

func (n decisionNode) Name() string                                  { return n.name }
func (n decisionNode) Role() AgentRole                               { return n.role }
func (n decisionNode) Phase() Phase                                  { return n.phase }
func (n decisionNode) Execute(context.Context, *PipelineState) error { return nil }

func (r *Runner) currentTime() time.Time {
	if r == nil {
		return time.Now()
	}
	r.nowMu.RLock()
	defer r.nowMu.RUnlock()
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}



func (r *Runner) canonicalSignal(state *PipelineState) domain.PipelineSignal {
	if state == nil {
		return domain.PipelineSignalHold
	}
	signal := state.FinalSignal.Signal
	if signal == "" {
		signal = state.TradingPlan.Action
	}
	if signal == "" {
		signal = domain.PipelineSignalHold
	}
	return signal
}

func snapshotState(state *PipelineState) StateView {
	if state == nil {
		return StateView{}
	}
	analystReports := make(map[AgentRole]string, len(state.AnalystReports))
	for role, report := range state.AnalystReports {
		analystReports[role] = report
	}
	researchRounds := append([]DebateRound(nil), state.ResearchDebate.Rounds...)
	riskRounds := append([]DebateRound(nil), state.RiskDebate.Rounds...)
	return StateView{
		PipelineRunID:  state.PipelineRunID,
		StrategyID:     state.StrategyID,
		Ticker:         state.Ticker,
		AnalystReports: analystReports,
		ResearchDebate: ResearchDebateState{Rounds: researchRounds, InvestmentPlan: state.ResearchDebate.InvestmentPlan},
		TradingPlan:    state.TradingPlan,
		RiskDebate:     RiskDebateState{Rounds: riskRounds, FinalSignal: state.RiskDebate.FinalSignal},
		FinalSignal:    state.FinalSignal,
		LLMCacheStats:  state.LLMCacheStats,
	}
}

// defaultSkipPhases returns the phases to skip by default.
// Risk debate is skipped because the research debate + trader already
// incorporate risk assessment, and it doubles the pipeline time.
func defaultSkipPhases() map[Phase]bool {
	return map[Phase]bool{PhaseRiskDebate: true}
}

func runtimePipelineTimeout(_ ResolvedConfig) time.Duration {
	return 0
}

func runtimeAnalysisTimeout(resolved ResolvedConfig) time.Duration {
	if resolved.PipelineConfig.AnalysisTimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(resolved.PipelineConfig.AnalysisTimeoutSeconds) * time.Second
}

func runtimeDebateTimeout(resolved ResolvedConfig) time.Duration {
	if resolved.PipelineConfig.DebateTimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(resolved.PipelineConfig.DebateTimeoutSeconds) * time.Second
}

