package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	kalshiexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	polymarketexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestNormalizePolymarketStrategySide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "yes", input: "yes", want: "YES"},
		{name: "no", input: "NO", want: "NO"},
		{name: "up", input: "up", want: "Up"},
		{name: "down", input: "Down", want: "Down"},
		{name: "over", input: "OVER", want: "Over"},
		{name: "under", input: "under", want: "Under"},
		{name: "blank", input: "", wantErr: true},
		{name: "invalid", input: "sideways", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizePolymarketStrategySide(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizePolymarketStrategySide(%q) error = nil, want error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePolymarketStrategySide(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("normalizePolymarketStrategySide(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestRunStrategy_PolymarketUsesNativePathBeforeLegacyOHLCV(t *testing.T) {
	t.Parallel()

	runner := &realStrategyRunner{polymarketMarketData: failingPolymarketMarketData{err: fmt.Errorf("native data used")}}
	_, err := runner.RunStrategy(context.Background(), domain.Strategy{
		Name:       "native disabled",
		Ticker:     "will-example-happen",
		MarketType: domain.MarketTypePolymarket,
		Status:     domain.StrategyStatusActive,
	})
	if err == nil || !strings.Contains(err.Error(), "native data used") {
		t.Fatalf("RunStrategy() error = %v, want native market-data error", err)
	}
}

func TestRunStrategy_KalshiUsesNativePathBeforeLegacyOHLCV(t *testing.T) {
	t.Parallel()

	runner := &realStrategyRunner{kalshiMarketData: failingKalshiMarketData{err: fmt.Errorf("kalshi native data used")}}
	_, err := runner.RunStrategy(context.Background(), domain.Strategy{
		Name:       "kalshi native disabled",
		Ticker:     "KXTEST-YESNO",
		MarketType: domain.MarketTypeKalshi,
		Status:     domain.StrategyStatusActive,
		IsPaper:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "kalshi native data used") {
		t.Fatalf("RunStrategy() error = %v, want native market-data error", err)
	}
}

func TestRunStrategy_KalshiLiveRoutingRespectsGatesAndClientInitialization(t *testing.T) {
	t.Parallel()

	strategy := domain.Strategy{
		ID:         uuid.New(),
		Name:       "kalshi live",
		Ticker:     "KXTEST-YESNO",
		MarketType: domain.MarketTypeKalshi,
		Status:     domain.StrategyStatusActive,
		IsPaper:    false,
		Config:     mustKalshiConfig(t, map[string]any{"template": "microstructure", "direction": "YES", "confidence": 0.72, "entry_price_max": 0.60}),
	}
	snapshot := staticKalshiMarketData{snapshot: kalshiexecution.Snapshot{
		Ticker:     "KXTEST-YESNO",
		Title:      "Will test happen?",
		Status:     "active",
		BestBidYes: 0.45,
		BestAskYes: 0.47,
		BestBidNo:  0.53,
		BestAskNo:  0.55,
		Volume:     1500,
		CloseTime:  time.Now().UTC().Add(24 * time.Hour),
		FetchedAt:  time.Now().UTC(),
	}}

	t.Run("paper uses fallback broker", func(t *testing.T) {
		t.Parallel()

		runner := &realStrategyRunner{
			cfg: config.Config{
				Features:                     config.FeatureFlags{EnableLiveTrading: true},
				LiveTradingAllowedStrategies: []string{strategy.ID.String()},
				LiveTradingAllowedBrokers:    []string{"kalshi"},
				Brokers: config.BrokerConfigs{Kalshi: config.KalshiConfig{
					APIKeyID:         "kalshi-key-id",
					PrivateKeyPEMB64: "base64-private-key",
				}},
			},
			kalshiDataProvider: &fakeKalshiMarketData{label: "shared-data"},
			kalshiLiveClient:   &fakeKalshiLiveClient{},
			logger:             slogDiscardLogger(),
		}
		broker, name, err := runner.newBrokerForStrategy(domain.Strategy{Ticker: "KXTEST-YESNO", MarketType: domain.MarketTypeKalshi, IsPaper: true})
		if err != nil {
			t.Fatalf("newBrokerForStrategy() error = %v", err)
		}
		if name != "paper" {
			t.Fatalf("broker name = %q, want paper", name)
		}
		if _, ok := broker.(*paper.PaperBroker); !ok {
			t.Fatalf("broker type = %T, want *paper.PaperBroker", broker)
		}
	})

	t.Run("live routes to kalshi broker when client is wired", func(t *testing.T) {
		t.Parallel()

		runner := &realStrategyRunner{
			cfg: config.Config{
				Features:                     config.FeatureFlags{EnableLiveTrading: true},
				LiveTradingAllowedStrategies: []string{strategy.ID.String()},
				LiveTradingAllowedBrokers:    []string{"kalshi"},
				Brokers: config.BrokerConfigs{Kalshi: config.KalshiConfig{
					APIKeyID:         "kalshi-key-id",
					PrivateKeyPEMB64: "base64-private-key",
				}},
			},
			kalshiDataProvider: &fakeKalshiMarketData{label: "shared-data"},
			kalshiLiveClient:   &fakeKalshiLiveClient{},
			logger:             slogDiscardLogger(),
		}
		broker, name, err := runner.newBrokerForStrategy(strategy)
		if err != nil {
			t.Fatalf("newBrokerForStrategy() error = %v", err)
		}
		if name != "kalshi" {
			t.Fatalf("broker name = %q, want kalshi", name)
		}
		if _, ok := broker.(*kalshiexecution.Broker); !ok {
			t.Fatalf("broker type = %T, want *kalshi.Broker", broker)
		}
	})

	t.Run("live disabled is denied by gate before broker route", func(t *testing.T) {
		t.Parallel()

		runner := &realStrategyRunner{kalshiDataProvider: &fakeKalshiMarketData{label: "shared-data"}, kalshiMarketData: snapshot, logger: slogDiscardLogger()}
		_, err := runner.RunStrategy(context.Background(), strategy)
		if err == nil || !strings.Contains(err.Error(), "live trading disabled") {
			t.Fatalf("RunStrategy() error = %v, want live gate denial", err)
		}
	})

	t.Run("missing broker allowlist is denied by gate", func(t *testing.T) {
		t.Parallel()

		runner := &realStrategyRunner{
			cfg: config.Config{
				Features:                     config.FeatureFlags{EnableLiveTrading: true},
				LiveTradingAllowedStrategies: []string{strategy.ID.String()},
			},
			kalshiDataProvider: &fakeKalshiMarketData{label: "shared-data"},
			kalshiMarketData:   snapshot,
			logger:             slogDiscardLogger(),
		}
		_, err := runner.RunStrategy(context.Background(), strategy)
		if err == nil || !strings.Contains(err.Error(), "broker not live-allowlisted") {
			t.Fatalf("RunStrategy() error = %v, want broker allowlist denial", err)
		}
	})

	t.Run("missing credentials fails clearly", func(t *testing.T) {
		t.Parallel()

		runner := &realStrategyRunner{
			cfg: config.Config{
				Features:                     config.FeatureFlags{EnableLiveTrading: true},
				LiveTradingAllowedStrategies: []string{strategy.ID.String()},
				LiveTradingAllowedBrokers:    []string{"kalshi"},
			},
			kalshiDataProvider: &fakeKalshiMarketData{label: "shared-data"},
			kalshiMarketData:   snapshot,
			logger:             slogDiscardLogger(),
		}
		_, err := runner.RunStrategy(context.Background(), strategy)
		if err == nil || !strings.Contains(err.Error(), "KALSHI_API_KEY_ID and KALSHI_PRIVATE_KEY_PEM_B64") {
			t.Fatalf("RunStrategy() error = %v, want credential error", err)
		}
	})

	t.Run("all gates and credentials reach blocked live client", func(t *testing.T) {
		t.Parallel()

		runner := &realStrategyRunner{
			cfg: config.Config{
				Features:                     config.FeatureFlags{EnableLiveTrading: true},
				LiveTradingAllowedStrategies: []string{strategy.ID.String()},
				LiveTradingAllowedBrokers:    []string{"kalshi"},
				Brokers: config.BrokerConfigs{Kalshi: config.KalshiConfig{
					APIKeyID:         "kalshi-key-id",
					PrivateKeyPEMB64: "base64-private-key",
				}},
			},
			kalshiDataProvider: &fakeKalshiMarketData{label: "shared-data"},
			kalshiMarketData:   snapshot,
			logger:             slogDiscardLogger(),
		}
		_, err := runner.RunStrategy(context.Background(), strategy)
		if err == nil || !strings.Contains(err.Error(), "kalshi live client is not initialised") {
			t.Fatalf("RunStrategy() error = %v, want uninitialised live client error", err)
		}
	})

	t.Run("live hold path is blocked before completion", func(t *testing.T) {
		t.Parallel()

		holdStrategy := strategy
		holdStrategy.ID = uuid.New()
		holdStrategy.Config = mustKalshiConfig(t, map[string]any{"template": "microstructure", "direction": "NO", "confidence": 0.72, "entry_price_max": 0.60})
		runner := &realStrategyRunner{
			cfg: config.Config{
				Features:                     config.FeatureFlags{EnableLiveTrading: true},
				LiveTradingAllowedStrategies: []string{holdStrategy.ID.String()},
				LiveTradingAllowedBrokers:    []string{"kalshi"},
				Brokers: config.BrokerConfigs{Kalshi: config.KalshiConfig{
					APIKeyID:         "kalshi-key-id",
					PrivateKeyPEMB64: "base64-private-key",
				}},
			},
			kalshiMarketData: snapshot,
			logger:           slogDiscardLogger(),
		}
		_, err := runner.RunStrategy(context.Background(), holdStrategy)
		if err == nil || !strings.Contains(err.Error(), "kalshi live client is not initialised") {
			t.Fatalf("RunStrategy() error = %v, want uninitialised live client error", err)
		}
	})
}

type fakeKalshiLiveClient struct{}

func (f *fakeKalshiLiveClient) CreateOrder(context.Context, kalshiexecution.CreateOrderRequest) (kalshiexecution.CreateOrderResponse, error) {
	return kalshiexecution.CreateOrderResponse{}, nil
}

func (f *fakeKalshiLiveClient) CancelOrder(context.Context, string) error { return nil }

func (f *fakeKalshiLiveClient) GetOrder(context.Context, string) (kalshiexecution.OrderResponse, error) {
	return kalshiexecution.OrderResponse{}, nil
}

func (f *fakeKalshiLiveClient) ListPositions(context.Context) ([]kalshiexecution.PositionResponse, error) {
	return nil, nil
}

func (f *fakeKalshiLiveClient) GetBalance(context.Context) (kalshiexecution.BalanceResponse, error) {
	return kalshiexecution.BalanceResponse{}, nil
}

func TestRunStrategy_KalshiSafeHoldPath(t *testing.T) {
	t.Parallel()

	strategy := domain.Strategy{
		Name:       "kalshi hold",
		Ticker:     "KXTEST-YESNO",
		MarketType: domain.MarketTypeKalshi,
		Status:     domain.StrategyStatusActive,
		IsPaper:    true,
		Config:     mustKalshiConfig(t, map[string]any{"template": "microstructure", "direction": "NO", "confidence": 0.72, "entry_price_max": 0.60}),
	}
	snapshotRepo := &recordingNativeSnapshotRepo{}
	runner := &realStrategyRunner{
		runRepo:      &stubPipelineRunRepo{},
		eventRepo:    &recordingStrategyPreparationEventRepo{},
		snapshotRepo: snapshotRepo,
		kalshiMarketData: staticKalshiMarketData{snapshot: kalshiexecution.Snapshot{
			Ticker:     "KXTEST-YESNO",
			Title:      "Will test happen?",
			Status:     "active",
			BestBidYes: 0.45,
			BestAskYes: 0.47,
			Volume:     1500,
			CloseTime:  time.Now().UTC().Add(24 * time.Hour),
			FetchedAt:  time.Now().UTC(),
		}},
	}
	result, err := runner.RunStrategy(context.Background(), strategy)
	if err != nil {
		t.Fatalf("RunStrategy() error = %v", err)
	}
	if result == nil || result.Signal != domain.PipelineSignalHold {
		t.Fatalf("RunStrategy() result = %+v, want hold", result)
	}
	if len(snapshotRepo.snapshots) != 1 || snapshotRepo.snapshots[0].DataType != "kalshi_native_snapshot" {
		t.Fatalf("snapshots = %+v, want one kalshi_native_snapshot", snapshotRepo.snapshots)
	}
}

func TestCompleteNativeRunPersistsTerminalEvent(t *testing.T) {
	t.Parallel()

	runRepo := &stubPipelineRunRepo{}
	eventRepo := &recordingStrategyPreparationEventRepo{}
	runner := &realStrategyRunner{runRepo: runRepo, eventRepo: eventRepo}
	run := domain.PipelineRun{ID: uuid.New(), StrategyID: uuid.New(), TradeDate: time.Now().UTC()}

	if err := runner.completeNativeRun(context.Background(), "kalshi", &run, domain.PipelineStatusFailed, domain.PipelineSignalHold, "secret provider detail"); err != nil {
		t.Fatalf("completeNativeRun() error = %v", err)
	}
	if len(runRepo.updates) != 1 || runRepo.updates[0].Status != domain.PipelineStatusFailed {
		t.Fatalf("run updates = %+v, want one failed update", runRepo.updates)
	}
	if len(eventRepo.events) != 1 || eventRepo.events[0].EventKind != agent.AgentEventKindPipelineFailed.String() {
		t.Fatalf("terminal events = %+v, want one pipeline_failed", eventRepo.events)
	}
	encoded, err := json.Marshal(eventRepo.events[0])
	if err != nil {
		t.Fatalf("marshal terminal event: %v", err)
	}
	if strings.Contains(string(encoded), "secret provider detail") {
		t.Fatalf("terminal event leaked raw run error: %s", encoded)
	}
}

func TestCompleteNativeRunEventFailureReclassifiesCompletion(t *testing.T) {
	t.Parallel()

	runRepo := &stubPipelineRunRepo{}
	runner := &realStrategyRunner{
		runRepo:   runRepo,
		eventRepo: &recordingStrategyPreparationEventRepo{err: errors.New("event store unavailable")},
	}
	run := domain.PipelineRun{ID: uuid.New(), StrategyID: uuid.New(), TradeDate: time.Now().UTC()}

	err := runner.completeNativeRun(context.Background(), "kalshi", &run, domain.PipelineStatusCompleted, domain.PipelineSignalHold, "")
	if err == nil || !strings.Contains(err.Error(), "persist terminal event") {
		t.Fatalf("completeNativeRun() error = %v, want event persistence failure", err)
	}
	if len(runRepo.updates) != 2 || runRepo.updates[0].Status != domain.PipelineStatusCompleted || runRepo.updates[1].Status != domain.PipelineStatusFailed {
		t.Fatalf("run updates = %+v, want completed then failed fallback", runRepo.updates)
	}
	if run.Status != domain.PipelineStatusFailed {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
}

func TestNewOrderManager_UsesFinancialLifecycleRepoForPaperOnly(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	runner := &realStrategyRunner{
		cfg: config.Config{
			Features:                     config.FeatureFlags{EnableLiveTrading: true},
			LiveTradingAllowedStrategies: []string{strategyID.String()},
			LiveTradingAllowedBrokers:    []string{"kalshi"},
			Brokers:                      config.BrokerConfigs{Kalshi: config.KalshiConfig{APIKeyID: "kalshi-key-id", PrivateKeyPEMB64: "base64-private-key"}},
		},
		financialRepo:    &strategyLifecycleRepoStub{},
		kalshiLiveClient: &fakeKalshiLiveClient{},
		kalshiMarketData: staticKalshiMarketData{snapshot: kalshiexecution.Snapshot{Ticker: "KXTEST-YESNO", Status: "active", CloseTime: time.Now().UTC().Add(time.Hour), FetchedAt: time.Now().UTC()}},
		logger:           slogDiscardLogger(),
	}
	paperMgr, err := runner.newOrderManager(context.Background(), domain.Strategy{ID: strategyID, IsPaper: true, MarketType: domain.MarketTypeKalshi, Ticker: "KXTEST-YESNO"}, agent.ResolvedConfig{}, &agent.StrategyConfig{})
	if err != nil {
		t.Fatalf("newOrderManager(paper) error = %v", err)
	}
	liveMgr, err := runner.newOrderManager(context.Background(), domain.Strategy{ID: strategyID, IsPaper: false, MarketType: domain.MarketTypeKalshi, Ticker: "KXTEST-YESNO"}, agent.ResolvedConfig{}, &agent.StrategyConfig{})
	if err != nil {
		t.Fatalf("newOrderManager(live) error = %v", err)
	}
	if reflect.ValueOf(paperMgr).Elem().FieldByName("financialRepo").IsNil() {
		t.Fatal("paper order manager did not receive financial lifecycle repo")
	}
	if !reflect.ValueOf(liveMgr).Elem().FieldByName("financialRepo").IsNil() {
		t.Fatal("live order manager unexpectedly received financial lifecycle repo")
	}
}

type strategyLifecycleRepoStub struct{}

func (strategyLifecycleRepoStub) ApplyOrderFill(context.Context, repository.OrderFillInput) (repository.OrderFillResult, error) {
	return repository.OrderFillResult{}, nil
}

func (strategyLifecycleRepoStub) SettlePredictionDecision(context.Context, repository.PredictionDecisionSettlementInput) (repository.PredictionDecisionSettlementResult, error) {
	return repository.PredictionDecisionSettlementResult{}, nil
}

func TestKalshiTradingPlanCopiesReferencePrice(t *testing.T) {
	t.Parallel()

	decision := kalshiexecution.NativeDecision{Side: "YES", EntryPrice: 0.04}
	plan := kalshiTradingPlan(domain.PipelineSignalBuy, kalshiexecution.Snapshot{}, decision, "KXTEST")
	if plan.ReferencePrice != 0.04 {
		t.Fatalf("ReferencePrice = %v, want 0.04", plan.ReferencePrice)
	}
	if plan.EntryPrice != 0.04 {
		t.Fatalf("EntryPrice = %v, want 0.04", plan.EntryPrice)
	}
	if plan.MarketType != domain.MarketTypeKalshi {
		t.Fatalf("MarketType = %q, want kalshi", plan.MarketType)
	}
}

func TestUsesStockOHLCVAnalysisSkipsEventMarkets(t *testing.T) {
	t.Parallel()

	for _, mt := range []domain.MarketType{domain.MarketTypePolymarket, domain.MarketTypeKalshi} {
		if usesStockOHLCVAnalysis(domain.Strategy{MarketType: mt}) {
			t.Fatalf("usesStockOHLCVAnalysis(%q) = true, want false", mt)
		}
	}
}

type failingPolymarketMarketData struct{ err error }

func (f failingPolymarketMarketData) GetMarketData(context.Context, string) (*agent.PredictionMarketData, error) {
	return nil, f.err
}

type staticPolymarketMarketData struct{ data *agent.PredictionMarketData }

func (s staticPolymarketMarketData) GetMarketData(context.Context, string) (*agent.PredictionMarketData, error) {
	return s.data, nil
}

type failingKalshiMarketData struct{ err error }

func (f failingKalshiMarketData) LoadSnapshot(context.Context, string) (kalshiexecution.Snapshot, error) {
	return kalshiexecution.Snapshot{}, f.err
}

type staticKalshiMarketData struct{ snapshot kalshiexecution.Snapshot }

func (s staticKalshiMarketData) LoadSnapshot(context.Context, string) (kalshiexecution.Snapshot, error) {
	return s.snapshot, nil
}

type recordingNativeSnapshotRepo struct {
	snapshots []domain.PipelineRunSnapshot
	err       error
}

func (r *recordingNativeSnapshotRepo) Create(_ context.Context, snapshot *domain.PipelineRunSnapshot) error {
	if r.err != nil {
		return r.err
	}
	r.snapshots = append(r.snapshots, *snapshot)
	return nil
}

func (r *recordingNativeSnapshotRepo) GetByRun(context.Context, uuid.UUID) ([]domain.PipelineRunSnapshot, error) {
	return append([]domain.PipelineRunSnapshot(nil), r.snapshots...), nil
}

type fakeKalshiMarketData struct{ label string }

func (f *fakeKalshiMarketData) LoadSnapshot(context.Context, string) (kalshiexecution.Snapshot, error) {
	return kalshiexecution.Snapshot{Ticker: f.label}, nil
}

func mustKalshiConfig(t *testing.T, meta map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"discovery_meta": meta})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return raw
}

func nativeMarketDataFixture() *agent.PredictionMarketData {
	end := time.Now().UTC().Add(72 * time.Hour)
	return &agent.PredictionMarketData{
		Slug:       "will-example-happen",
		EndDate:    &end,
		YesPrice:   0.42,
		NoPrice:    0.58,
		BestBidYes: 0.41,
		BestAskYes: 0.43,
		BestBidNo:  0.57,
		BestAskNo:  0.59,
		SpreadYes:  0.02,
		Liquidity:  20_000,
	}
}

func TestEffectivePolymarketExecutionStrategy_DefaultsToPaperUnlessLiveAllowlisted(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	strategy := domain.Strategy{ID: strategyID, MarketType: domain.MarketTypePolymarket, IsPaper: false}

	runner := &realStrategyRunner{}
	if got := runner.effectivePolymarketExecutionStrategy(strategy); !got.IsPaper {
		t.Fatal("expected paper when live trading is globally disabled")
	}

	runner.cfg.Features.EnableLiveTrading = true
	if got := runner.effectivePolymarketExecutionStrategy(strategy); !got.IsPaper {
		t.Fatal("expected paper when strategy/broker are not allowlisted")
	}

	runner.cfg.LiveTradingAllowedStrategies = []string{strategyID.String()}
	runner.cfg.LiveTradingAllowedBrokers = []string{"polymarket"}
	if got := runner.effectivePolymarketExecutionStrategy(strategy); got.IsPaper {
		t.Fatal("expected live only after explicit strategy and broker allowlist")
	}
}

func TestPolymarketExecutionDefaultsToPaperForUnspecifiedStrategyMode(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(map[string]any{"discovery_meta": map[string]any{"direction": "YES", "entry_price_max": 0.5}})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	runner := &realStrategyRunner{cfg: config.Config{}, polymarketMarketData: staticPolymarketMarketData{data: nativeMarketDataFixture()}}
	strategy := runner.effectivePolymarketExecutionStrategy(domain.Strategy{ID: uuid.New(), Ticker: "will-example-happen", MarketType: domain.MarketTypePolymarket, Config: raw})
	if !strategy.IsPaper {
		t.Fatal("polymarket strategy should default to paper when not explicitly live-enabled")
	}
}

func TestCheckPolymarketNativePreconditionsRejectsCapBreaches(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	end := now.Add(48 * time.Hour)
	runner := &realStrategyRunner{cfg: config.Config{Risk: config.RiskConfig{Polymarket: config.PolymarketRiskConfig{MaxPositionUSDC: 500, MinLiquidity: 1000}}}}
	snapshot := polymarketexecution.Snapshot{
		Slug:       "will-example-happen",
		EndDate:    &end,
		BestBidYes: 0.41,
		BestAskYes: 0.43,
		BestBidNo:  0.56,
		BestAskNo:  0.58,
		Liquidity:  20_000,
		FetchedAt:  now,
	}
	decision := polymarketexecution.NativeDecision{Side: "YES", EntryPrice: 0.43}

	err := runner.checkPolymarketNativePreconditions(snapshot, decision, 600)
	if err == nil {
		t.Fatal("expected cap breach to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds cap") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecutionDecisionMetadata_PreservesZeroCostWithLLMProvenance(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	promptTokens := 12
	completionTokens := 3
	latencyMS := 456
	decisionRepo := &stubAgentDecisionRepository{decisions: []domain.AgentDecision{{
		PipelineRunID:    runID,
		AgentRole:        domain.AgentRoleTrader,
		Phase:            domain.PhaseTrading,
		PromptText:       " system: preserve exact prompt \n",
		LLMProvider:      " openai ",
		LLMModel:         " gpt-4.1 ",
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		LatencyMS:        latencyMS,
		CostUSD:          0,
	}}}

	got := executionDecisionMetadata(context.Background(), decisionRepo, slog.Default(), runID)
	if got == nil {
		t.Fatal("executionDecisionMetadata() = nil, want metadata")
	}
	if got.PromptText != " system: preserve exact prompt \n" {
		t.Fatalf("PromptText = %q, want exact prompt", got.PromptText)
	}
	if got.LLMProvider != " openai " || got.LLMModel != " gpt-4.1 " {
		t.Fatalf("LLM strings = %+v, want exact preserved values", got)
	}
	if got.PromptTokens == nil || *got.PromptTokens != promptTokens {
		t.Fatalf("PromptTokens = %v, want %d", got.PromptTokens, promptTokens)
	}
	if got.CompletionTokens == nil || *got.CompletionTokens != completionTokens {
		t.Fatalf("CompletionTokens = %v, want %d", got.CompletionTokens, completionTokens)
	}
	if got.LatencyMS == nil || *got.LatencyMS != latencyMS {
		t.Fatalf("LatencyMS = %v, want %d", got.LatencyMS, latencyMS)
	}
	if got.CostUSD == nil || *got.CostUSD != 0 {
		t.Fatalf("CostUSD = %v, want 0", got.CostUSD)
	}
}

func TestExecutionDecisionMetadata_OmitsDeterministicDecision(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	decisionRepo := &stubAgentDecisionRepository{decisions: []domain.AgentDecision{{
		PipelineRunID: runID,
		AgentRole:     domain.AgentRoleTrader,
		Phase:         domain.PhaseTrading,
		CostUSD:       0.25,
	}}}

	if got := executionDecisionMetadata(context.Background(), decisionRepo, slog.Default(), runID); got != nil {
		t.Fatalf("executionDecisionMetadata() = %+v, want nil", got)
	}
}

type stubAgentDecisionRepository struct {
	decisions []domain.AgentDecision
	err       error
}

func (r *stubAgentDecisionRepository) Create(context.Context, *domain.AgentDecision) error {
	return nil
}

func (r *stubAgentDecisionRepository) GetByRun(context.Context, uuid.UUID, repository.AgentDecisionFilter, int, int) ([]domain.AgentDecision, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.decisions, nil
}

func (r *stubAgentDecisionRepository) CountByRun(context.Context, uuid.UUID, repository.AgentDecisionFilter) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return len(r.decisions), nil
}

var _ repository.AgentDecisionRepository = (*stubAgentDecisionRepository)(nil)

func TestValidateDailyBarFreshnessPostCloseGrace(t *testing.T) {
	prior := []domain.OHLCV{{Timestamp: time.Date(2026, 8, 3, 13, 30, 0, 0, time.UTC), Close: 100}}
	beforeGrace := time.Date(2026, 8, 4, 20, 20, 0, 0, time.UTC) // 4:20 PM ET
	if err := validateDailyBarFreshness(domain.MarketTypeStock, beforeGrace, prior); err != nil {
		t.Fatalf("before grace rejected: %v", err)
	}
	afterGrace := time.Date(2026, 8, 4, 20, 31, 0, 0, time.UTC) // 4:31 PM ET
	if err := validateDailyBarFreshness(domain.MarketTypeStock, afterGrace, prior); err == nil || !strings.Contains(err.Error(), "stale after 4:30 PM ET") {
		t.Fatalf("after grace error = %v, want stale error", err)
	}
	current := []domain.OHLCV{{Timestamp: time.Date(2026, 8, 4, 13, 30, 0, 0, time.UTC), Close: 101}}
	if err := validateDailyBarFreshness(domain.MarketTypeStock, afterGrace, current); err != nil {
		t.Fatalf("current post-close bar rejected: %v", err)
	}
}

func TestValidateRequiredAnalysisInputs(t *testing.T) {
	now := time.Date(2026, 8, 4, 18, 0, 0, 0, time.UTC)
	seed := agent.InitialStateSeed{
		Market:       &agent.MarketData{Bars: []domain.OHLCV{{Timestamp: time.Date(2026, 8, 3, 13, 30, 0, 0, time.UTC), Close: 100}}},
		Fundamentals: &data.Fundamentals{Ticker: "AAPL", MarketCap: 1, PERatio: 20, RevenueGrowthYoY: 0.1, FetchedAt: now},
		News: []data.NewsArticle{
			{Relevance: 1, PublishedAt: now.Add(-time.Hour)},
			{Relevance: 1, PublishedAt: now.Add(-2 * time.Hour)},
			{Relevance: 0.85, PublishedAt: now.Add(-3 * time.Hour)},
		},
	}
	required := []agent.AgentRole{agent.AgentRoleMarketAnalyst, agent.AgentRoleFundamentalsAnalyst, agent.AgentRoleNewsAnalyst}
	strategy := domain.Strategy{Ticker: "AAPL", MarketType: domain.MarketTypeStock}
	if err := validateRequiredAnalysisInputs(strategy, required, seed, now); err != nil {
		t.Fatalf("valid required inputs rejected: %v", err)
	}
	seed.News = seed.News[:2]
	if err := validateRequiredAnalysisInputs(strategy, required, seed, now); err == nil || !strings.Contains(err.Error(), "direct news coverage below threshold") {
		t.Fatalf("news threshold error = %v", err)
	}
}

type recordingStrategyPreparationEventRepo struct {
	events []domain.AgentEvent
	err    error
}

func (r *recordingStrategyPreparationEventRepo) Create(_ context.Context, event *domain.AgentEvent) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, *event)
	return nil
}

func (r *recordingStrategyPreparationEventRepo) List(context.Context, repository.AgentEventFilter, int, int) ([]domain.AgentEvent, error) {
	return append([]domain.AgentEvent(nil), r.events...), nil
}

func (r *recordingStrategyPreparationEventRepo) Count(context.Context, repository.AgentEventFilter) (int, error) {
	return len(r.events), nil
}

func TestRecordStrategyPreparationFailurePersistsBoundedReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		failure    error
		wantReason string
	}{
		{name: "news coverage", failure: fmt.Errorf("required analyst role news: direct news coverage below threshold: secret-provider-body"), wantReason: "news_coverage_insufficient"},
		{name: "stale news", failure: fmt.Errorf("newest direct news article is older than 36h: secret-provider-body"), wantReason: "news_stale"},
		{name: "fundamentals", failure: fmt.Errorf("fundamentals completeness below threshold: secret-provider-body"), wantReason: "fundamentals_incomplete"},
		{name: "generic", failure: fmt.Errorf("provider rejected credential secret-provider-body"), wantReason: "preparation_failed"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &recordingStrategyPreparationEventRepo{}
			runner := &realStrategyRunner{eventRepo: repo}
			strategy := domain.Strategy{ID: uuid.New(), Ticker: "SAFE", MarketType: domain.MarketTypeStock}
			if err := runner.recordStrategyPreparationFailure(context.Background(), strategy, tc.failure); err != nil {
				t.Fatalf("recordStrategyPreparationFailure() error = %v", err)
			}
			if len(repo.events) != 1 {
				t.Fatalf("created events = %d, want 1", len(repo.events))
			}
			event := repo.events[0]
			if event.EventKind != "strategy.preparation_rejected" || event.StrategyID == nil || *event.StrategyID != strategy.ID {
				t.Fatalf("event identity = %+v", event)
			}
			var metadata map[string]string
			if err := json.Unmarshal(event.Metadata, &metadata); err != nil {
				t.Fatalf("unmarshal metadata: %v", err)
			}
			if metadata["reason_code"] != tc.wantReason {
				t.Fatalf("reason_code = %q, want %q", metadata["reason_code"], tc.wantReason)
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			if strings.Contains(string(encoded), "secret-provider-body") {
				t.Fatalf("event leaked raw preparation error: %s", encoded)
			}
		})
	}
}

func TestRecordStrategyPreparationFailureSurfacesPersistenceFailure(t *testing.T) {
	t.Parallel()

	runner := &realStrategyRunner{eventRepo: &recordingStrategyPreparationEventRepo{err: fmt.Errorf("write unavailable")}}
	err := runner.recordStrategyPreparationFailure(context.Background(), domain.Strategy{ID: uuid.New()}, fmt.Errorf("preparation failed"))
	if err == nil || !strings.Contains(err.Error(), "write unavailable") {
		t.Fatalf("recordStrategyPreparationFailure() error = %v, want persistence failure", err)
	}
}

func TestRunStrategyPersistsPreparationRejection(t *testing.T) {
	t.Parallel()

	repo := &recordingStrategyPreparationEventRepo{}
	runner := &realStrategyRunner{eventRepo: repo}
	strategy := domain.Strategy{
		ID:         uuid.New(),
		Ticker:     "SAFE",
		MarketType: domain.MarketTypeStock,
		Config:     json.RawMessage(`{"agents":`),
	}
	_, err := runner.RunStrategy(context.Background(), strategy)
	if err == nil {
		t.Fatal("RunStrategy() error = nil, want invalid configuration rejection")
	}
	if len(repo.events) != 1 || repo.events[0].EventKind != "strategy.preparation_rejected" {
		t.Fatalf("preparation rejection events = %+v", repo.events)
	}
}

func TestValidateFundamentalsInputUsesProviderMissingFieldMetadata(t *testing.T) {
	now := time.Date(2026, 8, 4, 18, 0, 0, 0, time.UTC)
	fundamentals := &data.Fundamentals{
		Ticker: "LOSS", FetchedAt: now,
		RevenueGrowthYoY: -0.2,
		GrossMargin:      0,
		DebtToEquity:     -1,
		MissingFields: data.MissingFundamentalFields(
			data.FundamentalFieldMarketCap,
			data.FundamentalFieldPERatio,
		),
	}
	if err := validateFundamentalsInput("LOSS", fundamentals, now); err != nil {
		t.Fatalf("valid negative and zero metrics rejected: %v", err)
	}
}
