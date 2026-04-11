package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type runnerSpyPersister struct {
	mu        sync.Mutex
	runs      map[uuid.UUID]domain.PipelineRun
	decisions map[uuid.UUID][]persistedDecision
}

type persistedDecision struct {
	role  AgentRole
	phase Phase
	round *int
	text  string
}

func newRunnerSpyPersister() *runnerSpyPersister {
	return &runnerSpyPersister{
		runs:      make(map[uuid.UUID]domain.PipelineRun),
		decisions: make(map[uuid.UUID][]persistedDecision),
	}
}

func (p *runnerSpyPersister) RecordRunStart(_ context.Context, run *domain.PipelineRun) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := *run
	p.runs[run.ID] = cp
	return nil
}

func (p *runnerSpyPersister) RecordRunComplete(_ context.Context, runID uuid.UUID, _ time.Time, status domain.PipelineStatus, completedAt time.Time, errMsg string, _ json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := p.runs[runID]
	run.Status = status
	run.CompletedAt = &completedAt
	run.ErrorMessage = errMsg
	p.runs[runID] = run
	return nil
}

func (*runnerSpyPersister) SupportsSnapshots() bool { return false }
func (*runnerSpyPersister) PersistSnapshot(context.Context, *domain.PipelineRunSnapshot) error {
	return nil
}
func (*runnerSpyPersister) PersistEvent(context.Context, *domain.AgentEvent) error { return nil }
func (p *runnerSpyPersister) PersistDecision(_ context.Context, runID uuid.UUID, node Node, roundNumber *int, output string, _ *DecisionLLMResponse) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decisions[runID] = append(p.decisions[runID], persistedDecision{role: node.Role(), phase: node.Phase(), round: cloneRoundNumber(roundNumber), text: output})
	return nil
}

func (p *runnerSpyPersister) decisionCount(runID uuid.UUID) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.decisions[runID])
}

type stubAnalysisAgent struct {
	name string
	role AgentRole
	fn   func(context.Context, AnalysisInput) (AnalysisOutput, error)
}

func (a stubAnalysisAgent) Name() string    { return a.name }
func (a stubAnalysisAgent) Role() AgentRole { return a.role }
func (a stubAnalysisAgent) Analyze(ctx context.Context, input AnalysisInput) (AnalysisOutput, error) {
	return a.fn(ctx, input)
}

type stubDebateAgent struct {
	name string
	role AgentRole
	fn   func(context.Context, DebateInput) (DebateOutput, error)
}

func (a stubDebateAgent) Name() string    { return a.name }
func (a stubDebateAgent) Role() AgentRole { return a.role }
func (a stubDebateAgent) Debate(ctx context.Context, input DebateInput) (DebateOutput, error) {
	return a.fn(ctx, input)
}

type stubResearchJudge struct {
	name string
	role AgentRole
	fn   func(context.Context, DebateInput) (ResearchJudgeOutput, error)
}

func (j stubResearchJudge) Name() string    { return j.name }
func (j stubResearchJudge) Role() AgentRole { return j.role }
func (j stubResearchJudge) JudgeResearch(ctx context.Context, input DebateInput) (ResearchJudgeOutput, error) {
	return j.fn(ctx, input)
}

type stubTradeAgent struct {
	name string
	role AgentRole
	fn   func(context.Context, TradingInput) (TradingOutput, error)
}

func (a stubTradeAgent) Name() string    { return a.name }
func (a stubTradeAgent) Role() AgentRole { return a.role }
func (a stubTradeAgent) Trade(ctx context.Context, input TradingInput) (TradingOutput, error) {
	return a.fn(ctx, input)
}

type stubRiskJudge struct {
	name string
	role AgentRole
	fn   func(context.Context, RiskJudgeInput) (RiskJudgeOutput, error)
}

func (j stubRiskJudge) Name() string    { return j.name }
func (j stubRiskJudge) Role() AgentRole { return j.role }
func (j stubRiskJudge) JudgeRisk(ctx context.Context, input RiskJudgeInput) (RiskJudgeOutput, error) {
	return j.fn(ctx, input)
}

func defaultRunnerDefinition() Definition {
	return Definition{
		Analysis: []AnalysisAgent{
			stubAnalysisAgent{name: "market", role: AgentRoleMarketAnalyst, fn: func(context.Context, AnalysisInput) (AnalysisOutput, error) {
				return AnalysisOutput{Report: "market-report"}, nil
			}},
		},
		Research: ResearchDebateStage{
			Debaters: []DebateAgent{
				stubDebateAgent{name: "bull", role: AgentRoleBullResearcher, fn: func(_ context.Context, input DebateInput) (DebateOutput, error) {
					return DebateOutput{Contribution: input.Ticker + "-bull"}, nil
				}},
			},
			Judge: stubResearchJudge{name: "judge", role: AgentRoleInvestJudge, fn: func(_ context.Context, input DebateInput) (ResearchJudgeOutput, error) {
				return ResearchJudgeOutput{InvestmentPlan: input.Ticker + "-plan"}, nil
			}},
		},
		Trader: stubTradeAgent{name: "trader", role: AgentRoleTrader, fn: func(_ context.Context, input TradingInput) (TradingOutput, error) {
			plan := TradingPlan{Action: PipelineSignalBuy, Ticker: input.Ticker, EntryType: "market", EntryPrice: 100, PositionSize: 10, StopLoss: 95, TakeProfit: 110, TimeHorizon: "swing", Confidence: 0.8, Rationale: "test", RiskReward: 2}
			payload, _ := json.Marshal(plan)
			return TradingOutput{Plan: plan, StoredOutput: string(payload)}, nil
		}},
		Risk: RiskDebateStage{
			Debaters: []DebateAgent{
				stubDebateAgent{name: "risk", role: AgentRoleAggressiveAnalyst, fn: func(_ context.Context, input DebateInput) (DebateOutput, error) {
					return DebateOutput{Contribution: input.Ticker + "-risk"}, nil
				}},
			},
			Judge: stubRiskJudge{name: "risk-manager", role: AgentRoleRiskManager, fn: func(_ context.Context, input RiskJudgeInput) (RiskJudgeOutput, error) {
				plan := input.TradingPlan
				plan.PositionSize = 5
				return RiskJudgeOutput{FinalSignal: FinalSignal{Signal: PipelineSignalBuy, Confidence: 0.9}, StoredSignal: `{"action":"buy"}`, TradingPlan: plan}, nil
			}},
		},
	}
}

func strategyWithDebateRounds(t *testing.T, ticker string, rounds int) domain.Strategy {
	t.Helper()
	cfg, err := json.Marshal(StrategyConfig{PipelineConfig: &StrategyPipelineConfig{DebateRounds: &rounds}})
	if err != nil {
		t.Fatalf("marshal strategy config: %v", err)
	}
	return domain.Strategy{ID: uuid.New(), Ticker: ticker, Config: cfg}
}

func TestRunnerPrepare_ResolvesRuntimeFromStrategyConfig(t *testing.T) {
	persister := newRunnerSpyPersister()
	runner := NewRunner(defaultRunnerDefinition(), Dependencies{Persister: persister})
	strategy := strategyWithDebateRounds(t, "AAPL", 4)

	prepared, err := runner.Prepare(strategy, GlobalSettings{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.Runtime.ResearchRounds != 4 || prepared.Runtime.RiskRounds != 4 {
		t.Fatalf("prepared.Runtime rounds = %+v, want 4/4", prepared.Runtime)
	}
	if len(prepared.ConfigSnapshot) == 0 {
		t.Fatal("expected config snapshot to be populated")
	}
}

func TestRunnerRunStrategy_ConcurrentRunsKeepConfigIsolated(t *testing.T) {
	persister := newRunnerSpyPersister()
	runner := NewRunner(defaultRunnerDefinition(), Dependencies{Persister: persister})

	strategyOne := strategyWithDebateRounds(t, "AAPL", 1)
	strategyThree := strategyWithDebateRounds(t, "MSFT", 3)

	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan *RunResult, 2)
	errs := make(chan error, 2)
	for _, strategy := range []domain.Strategy{strategyOne, strategyThree} {
		strategy := strategy
		go func() {
			defer wg.Done()
			prepared, pErr := runner.Prepare(strategy, GlobalSettings{})
			if pErr != nil {
				errs <- pErr
				return
			}
			// Enable risk debate for this test (default skips it).
			prepared.Runtime.SkipPhases = nil
			result, err := runner.Run(context.Background(), prepared)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("RunStrategy() error = %v", err)
	}
	close(results)

	counts := []int{}
	for result := range results {
		counts = append(counts, persister.decisionCount(result.Run.ID))
	}
	sort.Ints(counts)
	want := []int{6, 10}
	if len(counts) != len(want) || counts[0] != want[0] || counts[1] != want[1] {
		t.Fatalf("decision counts = %v, want %v", counts, want)
	}
}

func TestRunnerRunStrategy_AnalysisFailureReturnsWarningButCompletes(t *testing.T) {
	persister := newRunnerSpyPersister()
	def := defaultRunnerDefinition()
	def.Analysis = []AnalysisAgent{
		stubAnalysisAgent{name: "market", role: AgentRoleMarketAnalyst, fn: func(context.Context, AnalysisInput) (AnalysisOutput, error) {
			return AnalysisOutput{Report: "market-report"}, nil
		}},
		stubAnalysisAgent{name: "news", role: AgentRoleNewsAnalyst, fn: func(context.Context, AnalysisInput) (AnalysisOutput, error) {
			return AnalysisOutput{}, errors.New("news provider down")
		}},
	}
	runner := NewRunner(def, Dependencies{Persister: persister})

	result, err := runner.RunStrategy(context.Background(), strategyWithDebateRounds(t, "AAPL", 1), GlobalSettings{})
	if err != nil {
		t.Fatalf("RunStrategy() error = %v, want nil", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(result.Warnings))
	}
	if result.Warnings[0].Role != AgentRoleNewsAnalyst {
		t.Fatalf("warning role = %s, want %s", result.Warnings[0].Role, AgentRoleNewsAnalyst)
	}
	if result.Run.Status != domain.PipelineStatusCompleted {
		t.Fatalf("run status = %s, want completed", result.Run.Status)
	}
	if got := result.State.AnalystReports[AgentRoleMarketAnalyst]; got != "market-report" {
		t.Fatalf("market report = %q, want market-report", got)
	}
}

func TestRunnerRunStrategy_RiskJudgeUpdatesCanonicalSignalAndPlan(t *testing.T) {
	persister := newRunnerSpyPersister()
	runner := NewRunner(defaultRunnerDefinition(), Dependencies{Persister: persister})

	prepared, err := runner.Prepare(strategyWithDebateRounds(t, "AAPL", 1), GlobalSettings{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	// Enable risk debate for this test (default skips it).
	prepared.Runtime.SkipPhases = nil

	result, err := runner.Run(context.Background(), prepared)
	if err != nil {
		t.Fatalf("RunStrategy() error = %v", err)
	}
	if result.Signal != domain.PipelineSignalBuy {
		t.Fatalf("signal = %s, want buy", result.Signal)
	}
	if result.State.FinalSignal.Confidence != 0.9 {
		t.Fatalf("final confidence = %v, want 0.9", result.State.FinalSignal.Confidence)
	}
	if result.State.TradingPlan.PositionSize != 5 {
		t.Fatalf("position size = %v, want 5", result.State.TradingPlan.PositionSize)
	}
	if result.State.RiskDebate.FinalSignal == "" {
		t.Fatal("expected stored risk signal to be populated")
	}
}

func TestRunnerRun_SeedsInitialStateBeforeAnalysis(t *testing.T) {
	persister := newRunnerSpyPersister()
	var captured AnalysisInput
	runner := NewRunner(Definition{
		Analysis: []AnalysisAgent{
			stubAnalysisAgent{name: "market", role: AgentRoleMarketAnalyst, fn: func(_ context.Context, input AnalysisInput) (AnalysisOutput, error) {
				captured = input
				return AnalysisOutput{Report: "seeded-market"}, nil
			}},
		},
		Research: ResearchDebateStage{
			Debaters: []DebateAgent{
				stubDebateAgent{name: "bull", role: AgentRoleBullResearcher, fn: func(_ context.Context, input DebateInput) (DebateOutput, error) {
					return DebateOutput{Contribution: input.Ticker + "-bull"}, nil
				}},
				stubDebateAgent{name: "bear", role: AgentRoleBearResearcher, fn: func(_ context.Context, input DebateInput) (DebateOutput, error) {
					return DebateOutput{Contribution: input.Ticker + "-bear"}, nil
				}},
			},
			Judge: stubResearchJudge{name: "judge", role: AgentRoleInvestJudge, fn: func(_ context.Context, input DebateInput) (ResearchJudgeOutput, error) {
				return ResearchJudgeOutput{InvestmentPlan: input.Ticker + "-plan"}, nil
			}},
		},
		Trader: stubTradeAgent{name: "trader", role: AgentRoleTrader, fn: func(_ context.Context, input TradingInput) (TradingOutput, error) {
			plan := TradingPlan{Action: PipelineSignalHold, Ticker: input.Ticker, Rationale: "seed test"}
			payload, _ := json.Marshal(plan)
			return TradingOutput{Plan: plan, StoredOutput: string(payload)}, nil
		}},
		Risk: RiskDebateStage{
			Debaters: []DebateAgent{
				stubDebateAgent{name: "aggressive", role: AgentRoleAggressiveAnalyst, fn: func(_ context.Context, input DebateInput) (DebateOutput, error) {
					return DebateOutput{Contribution: input.Ticker + "-risk"}, nil
				}},
			},
			Judge: stubRiskJudge{name: "risk-manager", role: AgentRoleRiskManager, fn: func(_ context.Context, input RiskJudgeInput) (RiskJudgeOutput, error) {
				return RiskJudgeOutput{FinalSignal: FinalSignal{Signal: PipelineSignalHold, Confidence: 0.7}, StoredSignal: `{"action":"hold"}`, TradingPlan: input.TradingPlan}, nil
			}},
		},
	}, Dependencies{Persister: persister})

	strategy := strategyWithDebateRounds(t, "AAPL", 1)
	prepped, err := runner.Prepare(strategy, GlobalSettings{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	now := time.Date(2026, 4, 5, 14, 30, 0, 0, time.UTC)
	prepped.InitialState = InitialStateSeed{
		Market: &MarketData{
			Bars:       []domain.OHLCV{{Timestamp: now, Open: 100, High: 110, Low: 95, Close: 108, Volume: 2_500}},
			Indicators: []domain.Indicator{{Name: "rsi_14", Value: 62.5, Timestamp: now}},
		},
		News:         []data.NewsArticle{{Title: "AAPL rallies", Summary: "Revenue beats expectations.", PublishedAt: now, Sentiment: 0.8}},
		Fundamentals: &data.Fundamentals{Ticker: "AAPL", MarketCap: 3_000_000_000_000, FetchedAt: now},
		Social:       &data.SocialSentiment{Ticker: "AAPL", Score: 0.71, MeasuredAt: now},
	}

	if _, err := runner.Run(context.Background(), prepped); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if captured.Market == nil || len(captured.Market.Bars) != 1 {
		t.Fatalf("captured market bars = %+v, want seeded market data", captured.Market)
	}
	if got := captured.Market.Indicators[0].Name; got != "rsi_14" {
		t.Fatalf("captured indicator = %q, want rsi_14", got)
	}
	if len(captured.News) != 1 || captured.News[0].Title != "AAPL rallies" {
		t.Fatalf("captured news = %+v, want seeded news", captured.News)
	}
	if captured.Fundamentals == nil || captured.Fundamentals.Ticker != "AAPL" {
		t.Fatalf("captured fundamentals = %+v, want seeded fundamentals", captured.Fundamentals)
	}
	if captured.Social == nil || captured.Social.Score != 0.71 {
		t.Fatalf("captured social = %+v, want seeded social", captured.Social)
	}
}

func TestRunnerRun_PhaseOrdering(t *testing.T) {
	t.Parallel()

	var order []string
	var mu sync.Mutex
	recordPhase := func(phase string) {
		mu.Lock()
		order = append(order, phase)
		mu.Unlock()
	}

	def := Definition{
		Analysis: []AnalysisAgent{
			stubAnalysisAgent{name: "market", role: AgentRoleMarketAnalyst, fn: func(_ context.Context, _ AnalysisInput) (AnalysisOutput, error) {
				recordPhase("analysis")
				return AnalysisOutput{Report: "ok"}, nil
			}},
		},
		Research: ResearchDebateStage{
			Debaters: []DebateAgent{
				stubDebateAgent{name: "bull", role: AgentRoleBullResearcher, fn: func(_ context.Context, _ DebateInput) (DebateOutput, error) {
					recordPhase("research")
					return DebateOutput{Contribution: "bull-contrib"}, nil
				}},
			},
			Judge: stubResearchJudge{name: "judge", role: AgentRoleInvestJudge, fn: func(_ context.Context, _ DebateInput) (ResearchJudgeOutput, error) {
				return ResearchJudgeOutput{InvestmentPlan: "plan"}, nil
			}},
		},
		Trader: stubTradeAgent{name: "trader", role: AgentRoleTrader, fn: func(_ context.Context, input TradingInput) (TradingOutput, error) {
			recordPhase("trading")
			plan := TradingPlan{Action: PipelineSignalBuy, Ticker: input.Ticker, EntryType: "market", EntryPrice: 100}
			payload, _ := json.Marshal(plan)
			return TradingOutput{Plan: plan, StoredOutput: string(payload)}, nil
		}},
		Risk: RiskDebateStage{
			Debaters: []DebateAgent{
				stubDebateAgent{name: "risk", role: AgentRoleAggressiveAnalyst, fn: func(_ context.Context, _ DebateInput) (DebateOutput, error) {
					recordPhase("risk")
					return DebateOutput{Contribution: "risk-contrib"}, nil
				}},
			},
			Judge: stubRiskJudge{name: "rm", role: AgentRoleRiskManager, fn: func(_ context.Context, input RiskJudgeInput) (RiskJudgeOutput, error) {
				return RiskJudgeOutput{FinalSignal: FinalSignal{Signal: PipelineSignalBuy, Confidence: 0.9}, StoredSignal: `{"action":"buy"}`, TradingPlan: input.TradingPlan}, nil
			}},
		},
	}

	persister := newRunnerSpyPersister()
	runner := NewRunner(def, Dependencies{Persister: persister})

	prepared, err := runner.Prepare(strategyWithDebateRounds(t, "TEST", 1), GlobalSettings{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	prepared.Runtime.SkipPhases = nil // Enable all phases.

	if _, err := runner.Run(context.Background(), prepared); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"analysis", "research", "trading", "risk"}
	if len(order) != len(want) {
		t.Fatalf("phase order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("phase[%d] = %q, want %q (full order: %v)", i, order[i], want[i], order)
		}
	}
}

func TestRunnerRun_PhaseSkip(t *testing.T) {
	t.Parallel()

	analysisRan := false
	def := defaultRunnerDefinition()
	def.Analysis = []AnalysisAgent{
		stubAnalysisAgent{name: "market", role: AgentRoleMarketAnalyst, fn: func(_ context.Context, _ AnalysisInput) (AnalysisOutput, error) {
			analysisRan = true
			return AnalysisOutput{Report: "ok"}, nil
		}},
	}

	persister := newRunnerSpyPersister()
	runner := NewRunner(def, Dependencies{Persister: persister})

	prepared, err := runner.Prepare(strategyWithDebateRounds(t, "TEST", 1), GlobalSettings{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	prepared.Runtime.SkipPhases = map[Phase]bool{
		PhaseAnalysis: true,
	}

	result, err := runner.Run(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if analysisRan {
		t.Error("analysis phase ran despite being skipped")
	}
	if result.Run.Status != domain.PipelineStatusCompleted {
		t.Errorf("run status = %s, want completed", result.Run.Status)
	}
}

func TestRunnerRun_ErrorMidPipeline_HaltsSubsequentPhases(t *testing.T) {
	t.Parallel()

	tradingRan := false
	def := defaultRunnerDefinition()
	def.Research = ResearchDebateStage{
		Debaters: []DebateAgent{
			stubDebateAgent{name: "bull", role: AgentRoleBullResearcher, fn: func(_ context.Context, _ DebateInput) (DebateOutput, error) {
				return DebateOutput{}, errors.New("research failed")
			}},
		},
		Judge: stubResearchJudge{name: "judge", role: AgentRoleInvestJudge, fn: func(_ context.Context, _ DebateInput) (ResearchJudgeOutput, error) {
			return ResearchJudgeOutput{InvestmentPlan: "plan"}, nil
		}},
	}
	def.Trader = stubTradeAgent{name: "trader", role: AgentRoleTrader, fn: func(_ context.Context, input TradingInput) (TradingOutput, error) {
		tradingRan = true
		plan := TradingPlan{Action: PipelineSignalHold, Ticker: input.Ticker}
		payload, _ := json.Marshal(plan)
		return TradingOutput{Plan: plan, StoredOutput: string(payload)}, nil
	}}

	persister := newRunnerSpyPersister()
	runner := NewRunner(def, Dependencies{Persister: persister})

	prepared, err := runner.Prepare(strategyWithDebateRounds(t, "TEST", 1), GlobalSettings{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	prepared.Runtime.SkipPhases = nil // Enable all phases.

	result, runErr := runner.Run(context.Background(), prepared)
	if runErr == nil {
		t.Fatal("expected error from research phase")
	}
	if tradingRan {
		t.Error("trading phase ran after research failure")
	}
	if result.Run.Status != domain.PipelineStatusFailed {
		t.Errorf("run status = %s, want failed", result.Run.Status)
	}
}

func TestRunnerRun_ContextCancellation(t *testing.T) {
	t.Parallel()

	def := defaultRunnerDefinition()
	def.Analysis = []AnalysisAgent{
		stubAnalysisAgent{name: "slow", role: AgentRoleMarketAnalyst, fn: func(ctx context.Context, _ AnalysisInput) (AnalysisOutput, error) {
			<-ctx.Done()
			return AnalysisOutput{}, ctx.Err()
		}},
	}

	persister := newRunnerSpyPersister()
	runner := NewRunner(def, Dependencies{Persister: persister})

	prepared, err := runner.Prepare(strategyWithDebateRounds(t, "TEST", 1), GlobalSettings{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay to let Run start.
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result, runErr := runner.Run(ctx, prepared)
	if runErr == nil {
		t.Fatal("expected error from cancelled context")
	}
	if result.Run.Status != domain.PipelineStatusFailed {
		t.Errorf("run status = %s, want failed", result.Run.Status)
	}
}
