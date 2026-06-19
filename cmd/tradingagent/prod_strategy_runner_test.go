package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
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

		runner := &realStrategyRunner{logger: slogDiscardLogger()}
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

	t.Run("live disabled is denied by gate before broker route", func(t *testing.T) {
		t.Parallel()

		runner := &realStrategyRunner{kalshiMarketData: snapshot, logger: slogDiscardLogger()}
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
			kalshiMarketData: snapshot,
			logger:           slogDiscardLogger(),
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
			kalshiMarketData: snapshot,
			logger:           slogDiscardLogger(),
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
			kalshiMarketData: snapshot,
			logger:           slogDiscardLogger(),
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
	runner := &realStrategyRunner{kalshiMarketData: staticKalshiMarketData{snapshot: kalshiexecution.Snapshot{
		Ticker:     "KXTEST-YESNO",
		Title:      "Will test happen?",
		Status:     "active",
		BestBidYes: 0.45,
		BestAskYes: 0.47,
		Volume:     1500,
		CloseTime:  time.Now().UTC().Add(24 * time.Hour),
		FetchedAt:  time.Now().UTC(),
	}}}
	result, err := runner.RunStrategy(context.Background(), strategy)
	if err != nil {
		t.Fatalf("RunStrategy() error = %v", err)
	}
	if result == nil || result.Signal != domain.PipelineSignalHold {
		t.Fatalf("RunStrategy() result = %+v, want hold", result)
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
