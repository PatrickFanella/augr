package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBacktestComparisonAPIQueryHistoricalRunsFiltersAndPaginates(t *testing.T) {
	t.Parallel()

	api := newTestBacktestComparisonAPI(t)
	runAfter := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	runBefore := time.Date(2024, 1, 5, 23, 59, 59, 0, time.UTC)

	got, err := api.QueryHistoricalRuns(context.Background(), HistoricalBacktestRunsQuery{
		StrategyIDs:   []uuid.UUID{testStrategyA.ID},
		RunAfter:      &runAfter,
		RunBefore:     &runBefore,
		PromptVersion: "prompt-v1",
		Limit:         1,
		Offset:        1,
	})
	if err != nil {
		t.Fatalf("QueryHistoricalRuns() error = %v", err)
	}

	if got.Total != 2 {
		t.Fatalf("got.Total = %d, want 2", got.Total)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("len(got.Runs) = %d, want 1", len(got.Runs))
	}

	run := got.Runs[0]
	if run.RunID != testRunAOlder.ID {
		t.Fatalf("run.RunID = %s, want %s", run.RunID, testRunAOlder.ID)
	}
	if run.StrategyName != testStrategyA.Name {
		t.Fatalf("run.StrategyName = %q, want %q", run.StrategyName, testStrategyA.Name)
	}
	if run.BacktestConfigName != testConfigAOne.Name {
		t.Fatalf("run.BacktestConfigName = %q, want %q", run.BacktestConfigName, testConfigAOne.Name)
	}
	if run.Metrics.TotalReturn != 0.10 {
		t.Fatalf("run.Metrics.TotalReturn = %v, want 0.10", run.Metrics.TotalReturn)
	}
}

func TestBacktestComparisonAPICompareHistoricalRunsReturnsAlignedMetricTableAndDiffs(t *testing.T) {
	t.Parallel()

	api := newTestBacktestComparisonAPI(t)

	got, err := api.CompareHistoricalRuns(context.Background(), CompareHistoricalBacktestRunsRequest{
		RunIDs: []uuid.UUID{testRunAOlder.ID, testRunANewer.ID, testRunB.ID},
	})
	if err != nil {
		t.Fatalf("CompareHistoricalRuns() error = %v", err)
	}

	if len(got.Runs) != 3 {
		t.Fatalf("len(got.Runs) = %d, want 3", len(got.Runs))
	}
	if len(got.MetricTable.Headers) != 4 {
		t.Fatalf("len(got.MetricTable.Headers) = %d, want 4", len(got.MetricTable.Headers))
	}
	for _, want := range []string{testStrategyA.Name, "prompt-v1", testStrategyB.Name, "prompt-v2"} {
		if !containsString(got.MetricTable.Headers, want) {
			t.Fatalf("headers %#v do not include %q", got.MetricTable.Headers, want)
		}
	}

	totalReturnRow := findComparisonRow(t, got.MetricTable, "Total Return")
	if len(totalReturnRow.Values) != 3 {
		t.Fatalf("len(totalReturnRow.Values) = %d, want 3", len(totalReturnRow.Values))
	}
	if totalReturnRow.Values[0] != 0.10 || totalReturnRow.Values[1] != 0.15 || totalReturnRow.Values[2] != 0.08 {
		t.Fatalf("unexpected Total Return values: %#v", totalReturnRow.Values)
	}

	totalReturnDiff := findComparisonDiff(t, got.SummaryDiffs, "Total Return")
	if totalReturnDiff.Baseline != 0.10 {
		t.Fatalf("totalReturnDiff.Baseline = %v, want 0.10", totalReturnDiff.Baseline)
	}
	if len(totalReturnDiff.Deltas) != 3 {
		t.Fatalf("len(totalReturnDiff.Deltas) = %d, want 3", len(totalReturnDiff.Deltas))
	}
	if !closeEnough(totalReturnDiff.Deltas[0], 0) ||
		!closeEnough(totalReturnDiff.Deltas[1], 0.05) ||
		!closeEnough(totalReturnDiff.Deltas[2], -0.02) {
		t.Fatalf("unexpected Total Return deltas: %#v", totalReturnDiff.Deltas)
	}
}

func TestBacktestComparisonAPIRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	api := newTestBacktestComparisonAPI(t)
	runAfter := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	runBefore := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)

	_, err := api.QueryHistoricalRuns(context.Background(), HistoricalBacktestRunsQuery{
		RunAfter:  &runAfter,
		RunBefore: &runBefore,
	})
	if err == nil || !strings.Contains(err.Error(), "run_before") {
		t.Fatalf("expected run_before validation error, got %v", err)
	}

	_, err = api.CompareHistoricalRuns(context.Background(), CompareHistoricalBacktestRunsRequest{
		RunIDs: []uuid.UUID{testRunAOlder.ID},
	})
	if err == nil || !strings.Contains(err.Error(), "at least 2 run IDs") {
		t.Fatalf("expected comparison validation error, got %v", err)
	}
}

var (
	testStrategyA = domain.Strategy{
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name:       "Growth",
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
	}
	testStrategyB = domain.Strategy{
		ID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:       "Value",
		Ticker:     "MSFT",
		MarketType: domain.MarketTypeStock,
	}
	testConfigAOne = domain.BacktestConfig{
		ID:         uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1"),
		StrategyID: testStrategyA.ID,
		Name:       "Growth 2024 H1",
		StartDate:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC),
	}
	testConfigATwo = domain.BacktestConfig{
		ID:         uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2"),
		StrategyID: testStrategyA.ID,
		Name:       "Growth 2024 H2",
		StartDate:  time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
	}
	testConfigB = domain.BacktestConfig{
		ID:         uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		StrategyID: testStrategyB.ID,
		Name:       "Value 2024",
		StartDate:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:    time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
	}
	testRunAOlder = makeHistoricalRun(
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		testConfigAOne.ID,
		time.Date(2024, 1, 3, 21, 0, 0, 0, time.UTC),
		"prompt-v1",
		"hash-v1",
		backtest.Metrics{TotalReturn: 0.10, MaxDrawdown: 0.05, SharpeRatio: 1.1, EndEquity: 110_000, RealizedPnL: 10_000, TotalBars: 20},
	)
	testRunANewer = makeHistoricalRun(
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		testConfigATwo.ID,
		time.Date(2024, 1, 5, 21, 0, 0, 0, time.UTC),
		"prompt-v1",
		"hash-v1",
		backtest.Metrics{TotalReturn: 0.15, MaxDrawdown: 0.04, SharpeRatio: 1.4, EndEquity: 115_000, RealizedPnL: 15_000, TotalBars: 22},
	)
	testRunAOtherPrompt = makeHistoricalRun(
		uuid.MustParse("00000000-0000-0000-0000-000000000003"),
		testConfigAOne.ID,
		time.Date(2024, 1, 6, 21, 0, 0, 0, time.UTC),
		"prompt-v2",
		"hash-v2",
		backtest.Metrics{TotalReturn: 0.12, MaxDrawdown: 0.06, SharpeRatio: 1.2, EndEquity: 112_000, RealizedPnL: 12_000, TotalBars: 20},
	)
	testRunB = makeHistoricalRun(
		uuid.MustParse("00000000-0000-0000-0000-000000000004"),
		testConfigB.ID,
		time.Date(2024, 1, 4, 21, 0, 0, 0, time.UTC),
		"prompt-v2",
		"hash-v2",
		backtest.Metrics{TotalReturn: 0.08, MaxDrawdown: 0.07, SharpeRatio: 0.9, EndEquity: 108_000, RealizedPnL: 8_000, TotalBars: 21},
	)
)

func newTestBacktestComparisonAPI(t *testing.T) *BacktestComparisonAPI {
	t.Helper()

	api, err := NewBacktestComparisonAPI(
		fakeStrategyRepo{
			byID: map[uuid.UUID]domain.Strategy{
				testStrategyA.ID: testStrategyA,
				testStrategyB.ID: testStrategyB,
			},
		},
		fakeBacktestConfigRepo{
			byID: map[uuid.UUID]domain.BacktestConfig{
				testConfigAOne.ID: testConfigAOne,
				testConfigATwo.ID: testConfigATwo,
				testConfigB.ID:    testConfigB,
			},
		},
		fakeBacktestRunRepo{
			byID: map[uuid.UUID]domain.BacktestRun{
				testRunAOlder.ID:       testRunAOlder,
				testRunANewer.ID:       testRunANewer,
				testRunAOtherPrompt.ID: testRunAOtherPrompt,
				testRunB.ID:            testRunB,
			},
		},
	)
	if err != nil {
		t.Fatalf("NewBacktestComparisonAPI() error = %v", err)
	}
	return api
}

func makeHistoricalRun(
	id uuid.UUID,
	configID uuid.UUID,
	runTimestamp time.Time,
	promptVersion string,
	promptVersionHash string,
	metrics backtest.Metrics,
) domain.BacktestRun {
	encoded, err := json.Marshal(metrics)
	if err != nil {
		panic(err)
	}
	return domain.BacktestRun{
		ID:                id,
		BacktestConfigID:  configID,
		Metrics:           encoded,
		TradeLog:          []byte(`[]`),
		EquityCurve:       []byte(`[]`),
		RunTimestamp:      runTimestamp,
		Duration:          5 * time.Minute,
		PromptVersion:     promptVersion,
		PromptVersionHash: promptVersionHash,
	}
}

func findComparisonRow(t *testing.T, table backtest.MetricComparisonTable, metric string) backtest.MetricComparisonRow {
	t.Helper()
	for _, row := range table.Rows {
		if row.Metric == metric {
			return row
		}
	}
	t.Fatalf("metric row %q not found", metric)
	return backtest.MetricComparisonRow{}
}

func findComparisonDiff(t *testing.T, diffs []backtest.MetricComparisonDiff, metric string) backtest.MetricComparisonDiff {
	t.Helper()
	for _, diff := range diffs {
		if diff.Metric == metric {
			return diff
		}
	}
	t.Fatalf("metric diff %q not found", metric)
	return backtest.MetricComparisonDiff{}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func closeEnough(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}

type fakeStrategyRepo struct {
	byID map[uuid.UUID]domain.Strategy
}

func (f fakeStrategyRepo) Create(context.Context, *domain.Strategy) error {
	return fmt.Errorf("not implemented")
}
func (f fakeStrategyRepo) Get(_ context.Context, id uuid.UUID) (*domain.Strategy, error) {
	strategy, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("strategy %s not found", id)
	}
	return &strategy, nil
}
func (f fakeStrategyRepo) List(context.Context, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f fakeStrategyRepo) Update(context.Context, *domain.Strategy) error {
	return fmt.Errorf("not implemented")
}
func (f fakeStrategyRepo) Delete(context.Context, uuid.UUID) error {
	return fmt.Errorf("not implemented")
}

type fakeBacktestConfigRepo struct {
	byID map[uuid.UUID]domain.BacktestConfig
}

func (f fakeBacktestConfigRepo) Create(context.Context, *domain.BacktestConfig) error {
	return fmt.Errorf("not implemented")
}
func (f fakeBacktestConfigRepo) Get(_ context.Context, id uuid.UUID) (*domain.BacktestConfig, error) {
	config, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("config %s not found", id)
	}
	return &config, nil
}
func (f fakeBacktestConfigRepo) List(_ context.Context, filter repository.BacktestConfigFilter, limit, offset int) ([]domain.BacktestConfig, error) {
	configs := make([]domain.BacktestConfig, 0)
	for _, config := range f.byID {
		if filter.StrategyID != nil && config.StrategyID != *filter.StrategyID {
			continue
		}
		configs = append(configs, config)
	}
	sort.Slice(configs, func(i, j int) bool {
		if configs[i].CreatedAt.Equal(configs[j].CreatedAt) {
			return strings.Compare(configs[i].ID.String(), configs[j].ID.String()) > 0
		}
		return configs[i].CreatedAt.After(configs[j].CreatedAt)
	})
	if offset >= len(configs) {
		return nil, nil
	}
	end := offset + limit
	if end > len(configs) {
		end = len(configs)
	}
	return configs[offset:end], nil
}
func (f fakeBacktestConfigRepo) Update(context.Context, *domain.BacktestConfig) error {
	return fmt.Errorf("not implemented")
}
func (f fakeBacktestConfigRepo) Delete(context.Context, uuid.UUID) error {
	return fmt.Errorf("not implemented")
}

type fakeBacktestRunRepo struct {
	byID map[uuid.UUID]domain.BacktestRun
}

func (f fakeBacktestRunRepo) Create(context.Context, *domain.BacktestRun) error {
	return fmt.Errorf("not implemented")
}
func (f fakeBacktestRunRepo) Get(_ context.Context, id uuid.UUID) (*domain.BacktestRun, error) {
	run, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("run %s not found", id)
	}
	return &run, nil
}
func (f fakeBacktestRunRepo) List(_ context.Context, filter repository.BacktestRunFilter, limit, offset int) ([]domain.BacktestRun, error) {
	runs := make([]domain.BacktestRun, 0)
	for _, run := range f.byID {
		if filter.BacktestConfigID != nil && run.BacktestConfigID != *filter.BacktestConfigID {
			continue
		}
		if filter.PromptVersion != "" && run.PromptVersion != filter.PromptVersion {
			continue
		}
		if filter.PromptVersionHash != "" && run.PromptVersionHash != filter.PromptVersionHash {
			continue
		}
		if filter.RunAfter != nil && run.RunTimestamp.Before(*filter.RunAfter) {
			continue
		}
		if filter.RunBefore != nil && run.RunTimestamp.After(*filter.RunBefore) {
			continue
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].RunTimestamp.Equal(runs[j].RunTimestamp) {
			return strings.Compare(runs[i].ID.String(), runs[j].ID.String()) > 0
		}
		return runs[i].RunTimestamp.After(runs[j].RunTimestamp)
	})
	if offset >= len(runs) {
		return nil, nil
	}
	end := offset + limit
	if end > len(runs) {
		end = len(runs)
	}
	return runs[offset:end], nil
}
