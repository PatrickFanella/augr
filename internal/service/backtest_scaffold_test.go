package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/service"
	"github.com/PatrickFanella/get-rich-quick/internal/strategyscaffold"
)

func TestRunBacktestRejectsStrategyWithoutSupportedBacktestConfig(t *testing.T) {
	strategy := domain.Strategy{
		ID:         uuid.New(),
		Name:       "unsupported",
		Ticker:     "QQQ",
		MarketType: domain.MarketTypeOptions,
		Status:     domain.StrategyStatusActive,
		Config:     domain.StrategyConfig(`{"unsupported":true}`),
	}

	cfg := domain.BacktestConfig{
		ID:         uuid.New(),
		StrategyID: strategy.ID,
		Name:       "options-paper-validation",
		StartDate:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 6, 28, 0, 0, 0, 0, time.UTC),
		Simulation: domain.BacktestSimulationParameters{InitialCapital: 100_000},
	}

	svc := service.NewBacktestService(
		stubBacktestConfigRepo{config: &cfg},
		&recordingBacktestRunRepo{},
		&stubStrategyRepo{strategy: &strategy},
		nil,
		nil,
		nil,
		slog.Default(),
	)

	run, err := svc.RunBacktest(context.Background(), cfg.ID, "tester")
	if err == nil {
		t.Fatal("RunBacktest() error = nil, want error")
	}
	if run != nil {
		t.Fatalf("RunBacktest() run = %#v, want nil", run)
	}
	var svcErr *service.ServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type = %T, want *service.ServiceError", err)
	}
	if svcErr.Status != 400 {
		t.Fatalf("ServiceError.Status = %d, want 400", svcErr.Status)
	}
	if svcErr.Message != "strategy config must include either a \"rules_engine\" or \"options_rules\" JSON key for backtesting" {
		t.Fatalf("ServiceError.Message = %q", svcErr.Message)
	}
}

func TestRunBacktestExecutesOptionsRulesAndPersistsRun(t *testing.T) {
	optionsStrategy, err := strategyscaffold.OptionsPaperBullPutSpread("QQQ")
	if err != nil {
		t.Fatalf("OptionsPaperBullPutSpread() error = %v", err)
	}
	optionsStrategy.Status = domain.StrategyStatusInactive

	cfg := domain.BacktestConfig{
		ID:         uuid.New(),
		StrategyID: optionsStrategy.ID,
		Name:       "options-paper-validation",
		StartDate:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		Simulation: domain.BacktestSimulationParameters{InitialCapital: 100_000},
	}

	marketDataRepo := &stubMarketDataRepo{bars: syntheticOHLCVSeries(cfg.StartDate.AddDate(-1, 0, 0), 520)}
	provider := &stubDataProvider{bars: marketDataRepo.bars}
	dataSvc := data.NewDataService(config.Config{}, &data.ProviderRegistry{
		Yahoo: func(data.ProviderConfig) data.DataProvider { return provider },
	}, marketDataRepo, slog.Default(), nil)
	dataSvc.SetNowFunc(func() time.Time { return cfg.EndDate })

	runRepo := &recordingBacktestRunRepo{}
	strategyRepo := &stubStrategyRepo{strategy: &optionsStrategy}
	auditRepo := &recordingAuditLogRepo{}

	svc := service.NewBacktestService(
		stubBacktestConfigRepo{config: &cfg},
		runRepo,
		strategyRepo,
		auditRepo,
		dataSvc,
		nil,
		slog.Default(),
	)

	run, err := svc.RunBacktest(context.Background(), cfg.ID, "tester")
	if err != nil {
		t.Fatalf("RunBacktest() error = %v", err)
	}
	if run == nil {
		t.Fatal("RunBacktest() run = nil")
	}
	if run.BacktestConfigID != cfg.ID {
		t.Fatalf("BacktestConfigID = %s, want %s", run.BacktestConfigID, cfg.ID)
	}
	if len(run.Metrics) == 0 || len(run.TradeLog) == 0 || len(run.EquityCurve) == 0 {
		t.Fatalf("persisted run fields missing: metrics=%d trade_log=%d equity_curve=%d", len(run.Metrics), len(run.TradeLog), len(run.EquityCurve))
	}
	if run.PromptVersion != "options-rules-v1" {
		t.Fatalf("PromptVersion = %q, want %q", run.PromptVersion, "options-rules-v1")
	}
	if run.PromptVersionHash == "" {
		t.Fatal("PromptVersionHash empty, want non-empty")
	}
	if runRepo.created == nil {
		t.Fatal("backtest run was not persisted")
	}
	if strategyRepo.updated != nil {
		t.Fatalf("unexpected strategy auto-activation: %#v", strategyRepo.updated)
	}
	if len(auditRepo.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(auditRepo.entries))
	}

	var metrics map[string]any
	if err := json.Unmarshal(run.Metrics, &metrics); err != nil {
		t.Fatalf("json.Unmarshal(metrics) error = %v", err)
	}
	if _, ok := metrics["total_bars"]; !ok {
		t.Fatalf("metrics JSON missing total_bars: %v", metrics)
	}

	var trades []domain.Trade
	if err := json.Unmarshal(run.TradeLog, &trades); err != nil {
		t.Fatalf("json.Unmarshal(trade_log) error = %v", err)
	}
	if len(trades) == 0 {
		t.Fatal("trade_log empty, want full options trades")
	}
	if trades[0].AssetClass != domain.AssetClassOption {
		t.Fatalf("first trade asset_class = %q, want %q", trades[0].AssetClass, domain.AssetClassOption)
	}
	if trades[0].Premium == 0 {
		t.Fatal("first trade premium = 0, want non-zero")
	}

	var curve []backtest.EquityPoint
	if err := json.Unmarshal(run.EquityCurve, &curve); err != nil {
		t.Fatalf("json.Unmarshal(equity_curve) error = %v", err)
	}
	if len(curve) < 10 {
		t.Fatalf("equity_curve len = %d, want >= 10", len(curve))
	}
	if curve[0].Timestamp.IsZero() || curve[len(curve)-1].Timestamp.IsZero() {
		t.Fatal("equity_curve timestamps must be populated")
	}
}

func TestRunBacktestOptionsRulesUsesConfiguredUnderlying(t *testing.T) {
	const expectedUnderlying = "QQQ"

	optionsStrategy, err := strategyscaffold.OptionsPaperBullPutSpread(expectedUnderlying)
	if err != nil {
		t.Fatalf("OptionsPaperBullPutSpread() error = %v", err)
	}
	optionsStrategy.Ticker = "SPY"

	cfg := domain.BacktestConfig{
		ID:         uuid.New(),
		StrategyID: optionsStrategy.ID,
		Name:       "options-underlying-validation",
		StartDate:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		Simulation: domain.BacktestSimulationParameters{InitialCapital: 100_000},
	}

	provider := &stubDataProvider{bars: syntheticOHLCVSeries(cfg.StartDate.AddDate(-1, 0, 0), 520)}
	marketDataRepo := &stubMarketDataRepo{bars: provider.bars}
	dataSvc := data.NewDataService(config.Config{}, &data.ProviderRegistry{
		Yahoo: func(data.ProviderConfig) data.DataProvider { return provider },
	}, marketDataRepo, slog.Default(), nil)
	dataSvc.SetNowFunc(func() time.Time { return cfg.EndDate })

	svc := service.NewBacktestService(
		stubBacktestConfigRepo{config: &cfg},
		&recordingBacktestRunRepo{},
		&stubStrategyRepo{strategy: &optionsStrategy},
		&recordingAuditLogRepo{},
		dataSvc,
		nil,
		slog.Default(),
	)

	run, err := svc.RunBacktest(context.Background(), cfg.ID, "tester")
	if err != nil {
		t.Fatalf("RunBacktest() error = %v", err)
	}
	if run == nil {
		t.Fatal("RunBacktest() run = nil")
	}
	if marketDataRepo.requestedTicker != expectedUnderlying {
		t.Fatalf("historical ticker = %q, want %q", marketDataRepo.requestedTicker, expectedUnderlying)
	}
}

func TestStockScaffoldConfigIncludesRulesEngineForBacktestService(t *testing.T) {
	strategy, err := strategyscaffold.StockPaperMovingAverageCrossover("SPY")
	if err != nil {
		t.Fatalf("StockPaperMovingAverageCrossover() error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(strategy.Config, &raw); err != nil {
		t.Fatalf("json.Unmarshal(config) error = %v", err)
	}
	if len(raw["rules_engine"]) == 0 {
		t.Fatal("config.rules_engine is empty, want scaffolded backtest rules")
	}
}

type stubBacktestConfigRepo struct {
	config *domain.BacktestConfig
}

func (s stubBacktestConfigRepo) Create(context.Context, *domain.BacktestConfig) error { return nil }
func (s stubBacktestConfigRepo) Get(context.Context, uuid.UUID) (*domain.BacktestConfig, error) {
	if s.config == nil {
		return nil, repository.ErrNotFound
	}
	return s.config, nil
}
func (s stubBacktestConfigRepo) List(context.Context, repository.BacktestConfigFilter, int, int) ([]domain.BacktestConfig, error) {
	return nil, nil
}
func (s stubBacktestConfigRepo) Count(context.Context, repository.BacktestConfigFilter) (int, error) {
	return 0, nil
}
func (s stubBacktestConfigRepo) Update(context.Context, *domain.BacktestConfig) error { return nil }
func (s stubBacktestConfigRepo) Delete(context.Context, uuid.UUID) error              { return nil }

type recordingBacktestRunRepo struct {
	created *domain.BacktestRun
}

func (r *recordingBacktestRunRepo) Create(_ context.Context, run *domain.BacktestRun) error {
	copyRun := *run
	r.created = &copyRun
	return nil
}
func (recordingBacktestRunRepo) Get(context.Context, uuid.UUID) (*domain.BacktestRun, error) {
	return nil, repository.ErrNotFound
}
func (recordingBacktestRunRepo) List(context.Context, repository.BacktestRunFilter, int, int) ([]domain.BacktestRun, error) {
	return nil, nil
}
func (recordingBacktestRunRepo) Count(context.Context, repository.BacktestRunFilter) (int, error) {
	return 0, nil
}

type stubStrategyRepo struct {
	strategy *domain.Strategy
	updated  *domain.Strategy
}

func (s *stubStrategyRepo) Create(context.Context, *domain.Strategy) error { return nil }
func (s *stubStrategyRepo) Get(context.Context, uuid.UUID) (*domain.Strategy, error) {
	if s.strategy == nil {
		return nil, repository.ErrNotFound
	}
	copyStrategy := *s.strategy
	return &copyStrategy, nil
}
func (s *stubStrategyRepo) List(context.Context, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	return nil, nil
}
func (s *stubStrategyRepo) Count(context.Context, repository.StrategyFilter) (int, error) {
	return 0, nil
}
func (s *stubStrategyRepo) Update(_ context.Context, strategy *domain.Strategy) error {
	copyStrategy := *strategy
	s.updated = &copyStrategy
	if s.strategy != nil {
		*s.strategy = copyStrategy
	}
	return nil
}
func (s *stubStrategyRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (s *stubStrategyRepo) UpdateThesis(context.Context, uuid.UUID, json.RawMessage) error {
	return nil
}
func (s *stubStrategyRepo) GetThesisRaw(context.Context, uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type recordingAuditLogRepo struct {
	entries []domain.AuditLogEntry
}

func (r *recordingAuditLogRepo) Create(_ context.Context, entry *domain.AuditLogEntry) error {
	copyEntry := *entry
	r.entries = append(r.entries, copyEntry)
	return nil
}
func (recordingAuditLogRepo) Query(context.Context, repository.AuditLogFilter, int, int) ([]domain.AuditLogEntry, error) {
	return nil, nil
}
func (recordingAuditLogRepo) Count(context.Context, repository.AuditLogFilter) (int, error) {
	return 0, nil
}

type stubDataProvider struct {
	bars            []domain.OHLCV
	requestedTicker string
}

func (s *stubDataProvider) GetOHLCV(_ context.Context, ticker string, _ data.Timeframe, _, _ time.Time) ([]domain.OHLCV, error) {
	s.requestedTicker = ticker
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
	bars            []domain.OHLCV
	requestedTicker string
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
func (s *stubMarketDataRepo) ListHistoricalOHLCV(_ context.Context, filter repository.HistoricalOHLCVFilter) ([]domain.HistoricalOHLCV, error) {
	s.requestedTicker = filter.Ticker
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
