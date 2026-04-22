package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategyscaffold"
)

func TestGetBacktestRunReturnsPersistedOptionsArtifacts(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	configID := uuid.New()
	tradeLog, _ := json.Marshal([]domain.Trade{{
		ID:                 uuid.New(),
		Ticker:             "QQQ240621P00450000",
		Side:               domain.OrderSideSell,
		Quantity:           1,
		Price:              2.15,
		ExecutedAt:         time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:          time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		AssetClass:         domain.AssetClassOption,
		OpenClose:          "open",
		ContractMultiplier: 100,
		Premium:            2.15,
	}})
	equityCurve, _ := json.Marshal([]backtest.EquityPoint{{
		Timestamp: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		Cash:      99500,
		Equity:    100000,
	}, {
		Timestamp: time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC),
		Cash:      100050,
		Equity:    100050,
	}})
	metrics, _ := json.Marshal(map[string]any{"total_bars": 2, "sharpe_ratio": 1.2})

	deps := testDeps()
	deps.BacktestRuns = &stubBacktestRunRepo{items: map[uuid.UUID]*domain.BacktestRun{
		runID: {
			ID:                runID,
			BacktestConfigID:  configID,
			Metrics:           metrics,
			TradeLog:          tradeLog,
			EquityCurve:       equityCurve,
			RunTimestamp:      time.Date(2024, 3, 3, 0, 0, 0, 0, time.UTC),
			PromptVersion:     "options-rules-v1",
			PromptVersionHash: "hash",
		},
	}}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/backtests/runs/"+runID.String(), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var got domain.BacktestRun
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.TradeLog) == 0 {
		t.Fatal("TradeLog empty, want persisted options trades")
	}
	if len(got.EquityCurve) == 0 {
		t.Fatal("EquityCurve empty, want persisted options equity points")
	}

	var trades []domain.Trade
	if err := json.Unmarshal(got.TradeLog, &trades); err != nil {
		t.Fatalf("json.Unmarshal(trade_log) error = %v", err)
	}
	if len(trades) == 0 || trades[0].AssetClass != domain.AssetClassOption {
		t.Fatalf("trades = %#v, want first trade to be option trade", trades)
	}

	var curve []backtest.EquityPoint
	if err := json.Unmarshal(got.EquityCurve, &curve); err != nil {
		t.Fatalf("json.Unmarshal(equity_curve) error = %v", err)
	}
	if len(curve) != 2 {
		t.Fatalf("equity curve len = %d, want 2", len(curve))
	}
}

func TestRunBacktestConfigReturnsAndPersistsOptionsArtifactsWithExitReasons(t *testing.T) {
	optionsStrategy, err := strategyscaffold.OptionsPaperBullPutSpread("QQQ")
	if err != nil {
		t.Fatalf("OptionsPaperBullPutSpread() error = %v", err)
	}
	optionsStrategy.Status = domain.StrategyStatusInactive

	cfg := &domain.BacktestConfig{
		ID:         uuid.New(),
		StrategyID: optionsStrategy.ID,
		Name:       "options-paper-validation",
		StartDate:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		Simulation: domain.BacktestSimulationParameters{InitialCapital: 100_000},
	}

	bars := syntheticOHLCVSeries(cfg.StartDate.AddDate(-1, 0, 0), 520)
	marketDataRepo := &stubMarketDataRepo{bars: bars}
	provider := &stubDataProvider{bars: bars}
	dataSvc := data.NewDataService(config.Config{}, &data.ProviderRegistry{
		Yahoo: func(data.ProviderConfig) data.DataProvider { return provider },
	}, marketDataRepo, slog.Default(), nil)
	dataSvc.SetNowFunc(func() time.Time { return cfg.EndDate })

	runRepo := &stubBacktestRunRepo{items: map[uuid.UUID]*domain.BacktestRun{}}
	auditRepo := &stubAuditLogRepo{}
	deps := testDeps()
	deps.Strategies = &stubStrategyRepo{items: map[uuid.UUID]domain.Strategy{optionsStrategy.ID: optionsStrategy}}
	deps.BacktestConfigs = &stubBacktestConfigRepo{items: map[uuid.UUID]*domain.BacktestConfig{cfg.ID: cfg}}
	deps.BacktestRuns = runRepo
	deps.AuditLog = auditRepo
	deps.DataService = dataSvc

	srv := newTestServerWithDeps(t, deps)
	rr := doRequest(t, srv, http.MethodPost, "/api/v1/backtests/configs/"+cfg.ID.String()+"/run", nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var got domain.BacktestRun
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.BacktestConfigID != cfg.ID {
		t.Fatalf("BacktestConfigID = %s, want %s", got.BacktestConfigID, cfg.ID)
	}
	if len(got.TradeLog) == 0 || len(got.EquityCurve) == 0 || len(got.Metrics) == 0 {
		t.Fatalf("returned artifacts missing: metrics=%d trade_log=%d equity_curve=%d", len(got.Metrics), len(got.TradeLog), len(got.EquityCurve))
	}

	persisted, ok := runRepo.items[got.ID]
	if !ok {
		t.Fatalf("persisted run %s not found in repo", got.ID)
	}
	if string(persisted.TradeLog) != string(got.TradeLog) {
		t.Fatal("persisted trade_log differs from response body")
	}
	if string(persisted.EquityCurve) != string(got.EquityCurve) {
		t.Fatal("persisted equity_curve differs from response body")
	}

	var trades []map[string]any
	if err := json.Unmarshal(got.TradeLog, &trades); err != nil {
		t.Fatalf("json.Unmarshal(trade_log) error = %v", err)
	}
	if len(trades) < 4 {
		t.Fatalf("trade_log len = %d, want >= 4", len(trades))
	}
	foundCloseReason := false
	for _, trade := range trades {
		if trade["open_close"] == "close" {
			reason, _ := trade["exit_reason"].(string)
			if reason != "" {
				foundCloseReason = true
				break
			}
		}
	}
	if !foundCloseReason {
		t.Fatal("expected at least one closing options trade with exit_reason metadata")
	}

	var curve []backtest.EquityPoint
	if err := json.Unmarshal(got.EquityCurve, &curve); err != nil {
		t.Fatalf("json.Unmarshal(equity_curve) error = %v", err)
	}
	if len(curve) < 10 {
		t.Fatalf("equity_curve len = %d, want >= 10", len(curve))
	}
	if len(auditRepo.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(auditRepo.entries))
	}
}

type stubBacktestRunRepo struct {
	items map[uuid.UUID]*domain.BacktestRun
}

func (s *stubBacktestRunRepo) Create(_ context.Context, run *domain.BacktestRun) error {
	if s.items == nil {
		s.items = map[uuid.UUID]*domain.BacktestRun{}
	}
	copyRun := *run
	s.items[run.ID] = &copyRun
	return nil
}

func (s *stubBacktestRunRepo) Get(_ context.Context, id uuid.UUID) (*domain.BacktestRun, error) {
	run, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("backtest run %v: %w", id, repository.ErrNotFound)
	}
	copyRun := *run
	return &copyRun, nil
}

func (s *stubBacktestRunRepo) List(_ context.Context, filter repository.BacktestRunFilter, _, _ int) ([]domain.BacktestRun, error) {
	out := make([]domain.BacktestRun, 0, len(s.items))
	for _, run := range s.items {
		if filter.BacktestConfigID != nil && run.BacktestConfigID != *filter.BacktestConfigID {
			continue
		}
		out = append(out, *run)
	}
	return out, nil
}

func (s *stubBacktestRunRepo) Count(ctx context.Context, filter repository.BacktestRunFilter) (int, error) {
	items, err := s.List(ctx, filter, 0, 0)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

type stubDataProvider struct {
	bars []domain.OHLCV
}

func (s *stubDataProvider) GetOHLCV(context.Context, string, data.Timeframe, time.Time, time.Time) ([]domain.OHLCV, error) {
	return append([]domain.OHLCV(nil), s.bars...), nil
}
func (s *stubDataProvider) GetFundamentals(context.Context, string) (data.Fundamentals, error) {
	return data.Fundamentals{}, data.ErrNotImplemented
}
func (s *stubDataProvider) GetNews(context.Context, string, time.Time, time.Time) ([]data.NewsArticle, error) {
	return nil, data.ErrNotImplemented
}
func (s *stubDataProvider) GetSocialSentiment(context.Context, string, time.Time, time.Time) ([]data.SocialSentiment, error) {
	return nil, data.ErrNotImplemented
}

type stubMarketDataRepo struct {
	bars []domain.OHLCV
}

func (s *stubMarketDataRepo) Get(context.Context, repository.MarketDataCacheKey) (*domain.MarketData, error) {
	return nil, nil
}
func (s *stubMarketDataRepo) Set(context.Context, *domain.MarketData) error { return nil }
func (s *stubMarketDataRepo) Expire(context.Context, repository.MarketDataCacheExpireFilter) error {
	return nil
}
func (s *stubMarketDataRepo) UpsertHistoricalOHLCV(context.Context, []domain.HistoricalOHLCV) error {
	return nil
}
func (s *stubMarketDataRepo) ListHistoricalOHLCV(context.Context, repository.HistoricalOHLCVFilter) ([]domain.HistoricalOHLCV, error) {
	result := make([]domain.HistoricalOHLCV, 0, len(s.bars))
	for _, bar := range s.bars {
		result = append(result, domain.HistoricalOHLCV{
			Ticker:    "QQQ",
			Provider:  "stock-chain",
			Timeframe: data.Timeframe1d.String(),
			Timestamp: bar.Timestamp,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
		})
	}
	return result, nil
}
func (s *stubMarketDataRepo) UpsertHistoricalOHLCVCoverage(context.Context, domain.HistoricalOHLCVCoverage) error {
	return nil
}
func (s *stubMarketDataRepo) ListHistoricalOHLCVCoverage(context.Context, repository.HistoricalOHLCVCoverageFilter) ([]domain.HistoricalOHLCVCoverage, error) {
	return nil, nil
}

func syntheticOHLCVSeries(start time.Time, count int) []domain.OHLCV {
	bars := make([]domain.OHLCV, 0, count)
	price := 100.0
	for i := 0; i < count; i++ {
		if i%29 == 0 {
			price -= 1.1
		} else {
			price += 0.38
		}
		bars = append(bars, domain.OHLCV{
			Timestamp: start.AddDate(0, 0, i),
			Open:      price - 0.4,
			High:      price + 0.7,
			Low:       price - 0.9,
			Close:     price,
			Volume:    2_000_000 + float64(i*1000),
		})
	}
	return bars
}
