package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/papervalidation"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
)

type stubReportStrategyRepo struct {
	strategies []domain.Strategy
	byID       map[uuid.UUID]domain.Strategy
	lastFilter repository.StrategyFilter
}

type stubStrategyRepoForReports = stubReportStrategyRepo

func (s *stubReportStrategyRepo) Create(_ context.Context, _ *domain.Strategy) error { return nil }

func (s *stubReportStrategyRepo) Get(_ context.Context, id uuid.UUID) (*domain.Strategy, error) {
	if s.byID != nil {
		if strat, ok := s.byID[id]; ok {
			cloned := strat
			return &cloned, nil
		}
	}
	for i := range s.strategies {
		if s.strategies[i].ID == id {
			return &s.strategies[i], nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *stubReportStrategyRepo) List(_ context.Context, filter repository.StrategyFilter, limit, offset int) ([]domain.Strategy, error) {
	s.lastFilter = filter
	var out []domain.Strategy
	if filter.Status == domain.StrategyStatusActive {
		out = make([]domain.Strategy, 0, len(s.strategies))
		for _, strat := range s.strategies {
			if strat.Status == domain.StrategyStatusActive {
				out = append(out, strat)
			}
		}
	} else {
		out = append([]domain.Strategy(nil), s.strategies...)
	}
	if offset >= len(out) {
		return nil, nil
	}
	end := len(out)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return out[offset:end], nil
}

func (s *stubReportStrategyRepo) Count(_ context.Context, filter repository.StrategyFilter) (int, error) {
	count := 0
	for _, strategy := range s.strategies {
		if filter.Status == "" || strategy.Status == filter.Status {
			count++
		}
	}
	return count, nil
}
func (s *stubReportStrategyRepo) Update(_ context.Context, _ *domain.Strategy) error { return nil }
func (s *stubReportStrategyRepo) Delete(_ context.Context, _ uuid.UUID) error        { return nil }
func (s *stubReportStrategyRepo) UpdateThesis(_ context.Context, _ uuid.UUID, _ json.RawMessage) error {
	return nil
}

func (s *stubReportStrategyRepo) GetThesisRaw(_ context.Context, _ uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type stubReportBacktestConfigRepo struct {
	byStrategy map[uuid.UUID][]domain.BacktestConfig
	lastFilter repository.BacktestConfigFilter
}

func (s *stubReportBacktestConfigRepo) Create(_ context.Context, _ *domain.BacktestConfig) error {
	return nil
}

func (s *stubReportBacktestConfigRepo) Get(_ context.Context, id uuid.UUID) (*domain.BacktestConfig, error) {
	for _, configs := range s.byStrategy {
		for i := range configs {
			if configs[i].ID == id {
				return &configs[i], nil
			}
		}
	}
	return nil, repository.ErrNotFound
}

func (s *stubReportBacktestConfigRepo) List(_ context.Context, filter repository.BacktestConfigFilter, _, _ int) ([]domain.BacktestConfig, error) {
	s.lastFilter = filter
	if filter.StrategyID != nil {
		return append([]domain.BacktestConfig(nil), s.byStrategy[*filter.StrategyID]...), nil
	}
	var out []domain.BacktestConfig
	for _, configs := range s.byStrategy {
		out = append(out, configs...)
	}
	return out, nil
}

func (s *stubReportBacktestConfigRepo) Count(_ context.Context, _ repository.BacktestConfigFilter) (int, error) {
	count := 0
	for _, configs := range s.byStrategy {
		count += len(configs)
	}
	return count, nil
}

func (s *stubReportBacktestConfigRepo) Update(_ context.Context, _ *domain.BacktestConfig) error {
	return nil
}
func (s *stubReportBacktestConfigRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }

type stubReportBacktestRunRepo struct {
	byConfig   map[uuid.UUID][]domain.BacktestRun
	lastFilter repository.BacktestRunFilter
}

func (s *stubReportBacktestRunRepo) Create(_ context.Context, _ *domain.BacktestRun) error {
	return nil
}

func (s *stubReportBacktestRunRepo) Get(_ context.Context, id uuid.UUID) (*domain.BacktestRun, error) {
	for _, runs := range s.byConfig {
		for i := range runs {
			if runs[i].ID == id {
				return &runs[i], nil
			}
		}
	}
	return nil, repository.ErrNotFound
}

func (s *stubReportBacktestRunRepo) List(_ context.Context, filter repository.BacktestRunFilter, _, _ int) ([]domain.BacktestRun, error) {
	s.lastFilter = filter
	if filter.BacktestConfigID != nil {
		return append([]domain.BacktestRun(nil), s.byConfig[*filter.BacktestConfigID]...), nil
	}
	var out []domain.BacktestRun
	for _, runs := range s.byConfig {
		out = append(out, runs...)
	}
	return out, nil
}

func (s *stubReportBacktestRunRepo) Count(_ context.Context, _ repository.BacktestRunFilter) (int, error) {
	count := 0
	for _, runs := range s.byConfig {
		count += len(runs)
	}
	return count, nil
}

type stubReportArtifactRepo struct {
	artifacts []pgrepo.ReportArtifact
	err       error
}

func (s *stubReportArtifactRepo) Upsert(_ context.Context, a *pgrepo.ReportArtifact) error {
	if s.err != nil {
		return s.err
	}
	cloned := *a
	if a.ReportJSON != nil {
		cloned.ReportJSON = append(json.RawMessage(nil), a.ReportJSON...)
	}
	s.artifacts = append(s.artifacts, cloned)
	return nil
}

type captureReportMetrics struct {
	successes []string
	errors    []string
}

func (m *captureReportMetrics) RecordReportWorkerSuccess(strategyID string) {
	m.successes = append(m.successes, strategyID)
}

func (m *captureReportMetrics) RecordReportWorkerError(strategyID string) {
	m.errors = append(m.errors, strategyID)
}

func newTestReportWorker(t *testing.T, strategies []domain.Strategy, configs map[uuid.UUID][]domain.BacktestConfig, runs map[uuid.UUID][]domain.BacktestRun, repo *stubReportArtifactRepo, metrics *captureReportMetrics) *ReportWorker {
	t.Helper()
	w := NewReportWorker(reportWorkerDeps{
		StrategyRepo:       &stubReportStrategyRepo{strategies: strategies},
		BacktestConfigRepo: &stubReportBacktestConfigRepo{byStrategy: configs},
		BacktestRunRepo:    &stubReportBacktestRunRepo{byConfig: runs},
		ReportArtifactRepo: repo,
	}, nil, metrics)
	return w
}

func TestRunPaperValidationReport_FiltersAndPersistsCompletedArtifacts(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 6, 11, 15, 4, 5, 0, time.UTC)

	activePaperA := uuid.New()
	activePaperB := uuid.New()
	activeLive := uuid.New()
	configA := uuid.New()
	configB := uuid.New()

	metricsJSON := mustMarshal(t, backtest.Metrics{
		TotalReturn: 0.14,
		SharpeRatio: 1.7,
		MaxDrawdown: 0.08,
		WinRate:     0.62,
		StartTime:   fixedNow.Add(-72 * time.Hour),
		EndTime:     fixedNow.Add(-24 * time.Hour),
		StartEquity: 10000,
		EndEquity:   11400,
		TotalBars:   12,
	})
	tradesJSON := mustMarshal(t, []domain.Trade{})

	reportRepo := &stubReportArtifactRepo{}
	metrics := &captureReportMetrics{}
	worker := newTestReportWorker(t,
		[]domain.Strategy{
			{ID: activePaperA, Name: "paper-a", Status: domain.StrategyStatusActive, IsPaper: true, CreatedAt: fixedNow.Add(-90 * 24 * time.Hour)},
			{ID: activeLive, Name: "live", Status: domain.StrategyStatusActive, IsPaper: false, CreatedAt: fixedNow.Add(-90 * 24 * time.Hour)},
			{ID: activePaperB, Name: "paper-b", Status: domain.StrategyStatusActive, IsPaper: true, CreatedAt: fixedNow.Add(-60 * 24 * time.Hour)},
			{ID: uuid.New(), Name: "inactive-paper", Status: domain.StrategyStatusInactive, IsPaper: true, CreatedAt: fixedNow.Add(-45 * 24 * time.Hour)},
		},
		map[uuid.UUID][]domain.BacktestConfig{
			activePaperA: {{ID: configA, StrategyID: activePaperA}},
			activePaperB: {{ID: configB, StrategyID: activePaperB}},
		},
		map[uuid.UUID][]domain.BacktestRun{
			configA: {{ID: uuid.New(), BacktestConfigID: configA, Metrics: metricsJSON, TradeLog: tradesJSON}},
			configB: {{ID: uuid.New(), BacktestConfigID: configB, Metrics: metricsJSON, TradeLog: tradesJSON}},
		},
		reportRepo,
		metrics,
	)
	worker.now = func() time.Time { return fixedNow }

	if err := worker.RunPaperValidationReport(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := worker.deps.StrategyRepo.(*stubReportStrategyRepo).lastFilter.Status; got != domain.StrategyStatusActive {
		t.Fatalf("strategy filter status = %q, want %q", got, domain.StrategyStatusActive)
	}
	if got := len(reportRepo.artifacts); got != 2 {
		t.Fatalf("artifacts persisted = %d, want 2", got)
	}
	if got := len(metrics.successes); got != 2 {
		t.Fatalf("success metrics = %d, want 2", got)
	}
	if got := len(metrics.errors); got != 0 {
		t.Fatalf("error metrics = %d, want 0", got)
	}

	for _, artifact := range reportRepo.artifacts {
		if artifact.ReportType != reportTypePaperValidation {
			t.Fatalf("report type = %q, want %q", artifact.ReportType, reportTypePaperValidation)
		}
		if artifact.Status != "completed" {
			t.Fatalf("artifact status = %q, want completed", artifact.Status)
		}
		if artifact.CompletedAt == nil {
			t.Fatal("completed_at should be set")
		}
		var report papervalidation.ValidationReport
		if err := json.Unmarshal(artifact.ReportJSON, &report); err != nil {
			t.Fatalf("unmarshal report: %v", err)
		}
		if !report.ReportDate.Equal(fixedNow) {
			t.Fatalf("report date = %v, want %v", report.ReportDate, fixedNow)
		}
		if report.ElapsedDays != 90 && report.ElapsedDays != 60 {
			t.Fatalf("elapsed days = %d, want a strategy-specific paper age", report.ElapsedDays)
		}
		if report.Decision == "" {
			t.Fatal("report decision should not be empty")
		}
	}
}

func TestGenerateOneReport_PersistsErrorArtifactWhenBacktestConfigMissing(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	fixedNow := time.Date(2026, 6, 11, 15, 4, 5, 0, time.UTC)
	reportRepo := &stubReportArtifactRepo{}
	worker := newTestReportWorker(t,
		[]domain.Strategy{{ID: strategyID, Name: "paper", Status: domain.StrategyStatusActive, IsPaper: true, CreatedAt: fixedNow.Add(-70 * 24 * time.Hour)}},
		map[uuid.UUID][]domain.BacktestConfig{strategyID: nil},
		map[uuid.UUID][]domain.BacktestRun{},
		reportRepo,
		&captureReportMetrics{},
	)
	worker.now = func() time.Time { return fixedNow }

	err := worker.generateOneReport(context.Background(), strategyID, "paper", fixedNow.Truncate(24*time.Hour), fixedNow)
	if err == nil {
		t.Fatal("expected error when no backtest configs exist")
	}
	if got := len(reportRepo.artifacts); got != 1 {
		t.Fatalf("artifacts persisted = %d, want 1", got)
	}
	artifact := reportRepo.artifacts[0]
	if artifact.Status != "error" {
		t.Fatalf("artifact status = %q, want error", artifact.Status)
	}
	if !strings.Contains(artifact.ErrorMessage, "no backtest configs") {
		t.Fatalf("error message = %q, want backtest config failure", artifact.ErrorMessage)
	}
}

func TestGenerateOneReportRejectsMalformedTradeLog(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	configID := uuid.New()
	fixedNow := time.Date(2026, 6, 11, 15, 4, 5, 0, time.UTC)
	reportRepo := &stubReportArtifactRepo{}
	worker := newTestReportWorker(t,
		[]domain.Strategy{{ID: strategyID, Name: "paper", Status: domain.StrategyStatusActive, IsPaper: true, CreatedAt: fixedNow.Add(-70 * 24 * time.Hour)}},
		map[uuid.UUID][]domain.BacktestConfig{strategyID: {{ID: configID, StrategyID: strategyID}}},
		map[uuid.UUID][]domain.BacktestRun{configID: {{ID: uuid.New(), BacktestConfigID: configID, Metrics: mustMarshal(t, backtest.Metrics{}), TradeLog: json.RawMessage(`{`)}}},
		reportRepo,
		&captureReportMetrics{},
	)

	err := worker.generateOneReport(context.Background(), strategyID, "paper", fixedNow.Truncate(24*time.Hour), fixedNow)
	if err == nil || !strings.Contains(err.Error(), "unmarshal trade log") {
		t.Fatalf("generateOneReport() error = %v, want malformed trade log", err)
	}
	if len(reportRepo.artifacts) != 1 || reportRepo.artifacts[0].Status != "error" {
		t.Fatalf("artifacts = %#v, want one error artifact", reportRepo.artifacts)
	}
}

func TestGenerateOneReportSurfacesErrorArtifactPersistenceFailure(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	fixedNow := time.Date(2026, 6, 11, 15, 4, 5, 0, time.UTC)
	reportRepo := &stubReportArtifactRepo{err: errors.New("artifact store unavailable")}
	worker := newTestReportWorker(t,
		[]domain.Strategy{{ID: strategyID, Name: "paper", Status: domain.StrategyStatusActive, IsPaper: true}},
		map[uuid.UUID][]domain.BacktestConfig{strategyID: nil},
		map[uuid.UUID][]domain.BacktestRun{},
		reportRepo,
		&captureReportMetrics{},
	)

	err := worker.generateOneReport(context.Background(), strategyID, "paper", fixedNow.Truncate(24*time.Hour), fixedNow)
	if err == nil || !strings.Contains(err.Error(), "no backtest configs") || !strings.Contains(err.Error(), "persist error artifact") {
		t.Fatalf("generateOneReport() error = %v, want generation and artifact persistence failures", err)
	}
	if len(reportRepo.artifacts) != 0 {
		t.Fatalf("artifacts = %#v, want no falsely persisted artifact", reportRepo.artifacts)
	}
}

func TestRunPaperValidationReportSkipsEventMarketsAndReportsEligibleFailures(t *testing.T) {
	stockID := uuid.New()
	kalshiID := uuid.New()
	fixedNow := time.Date(2026, 6, 11, 15, 4, 5, 0, time.UTC)
	reportRepo := &stubReportArtifactRepo{}
	worker := newTestReportWorker(t,
		[]domain.Strategy{
			{ID: stockID, Name: "stock", MarketType: domain.MarketTypeStock, Status: domain.StrategyStatusActive, IsPaper: true},
			{ID: kalshiID, Name: "kalshi", MarketType: domain.MarketTypeKalshi, Status: domain.StrategyStatusActive, IsPaper: true},
		},
		map[uuid.UUID][]domain.BacktestConfig{}, map[uuid.UUID][]domain.BacktestRun{}, reportRepo, &captureReportMetrics{},
	)
	worker.now = func() time.Time { return fixedNow }

	err := worker.RunPaperValidationReport(context.Background())
	if err == nil || !strings.Contains(err.Error(), "1 of 1 eligible strategies failed") {
		t.Fatalf("RunPaperValidationReport() error = %v", err)
	}
	if got := worker.LastSummary(); got["eligible"] != 1 || got["skipped"] != 1 || got["failed"] != 1 {
		t.Fatalf("summary = %v, want one eligible failure and one skip", got)
	}
	if len(reportRepo.artifacts) != 1 || reportRepo.artifacts[0].StrategyID != stockID {
		t.Fatalf("artifacts = %#v, event-market strategy must be skipped", reportRepo.artifacts)
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
