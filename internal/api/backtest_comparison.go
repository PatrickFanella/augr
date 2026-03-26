package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	defaultBacktestComparisonLimit = 50
	backtestComparisonPageSize     = 100
)

// BacktestComparisonAPI exposes filtered historical backtest queries and
// side-by-side run comparisons built on the repository layer.
type BacktestComparisonAPI struct {
	strategies repository.StrategyRepository
	configs    repository.BacktestConfigRepository
	runs       repository.BacktestRunRepository
}

// HistoricalBacktestRunsQuery defines the supported filters for querying saved
// historical backtest runs.
type HistoricalBacktestRunsQuery struct {
	StrategyIDs       []uuid.UUID
	RunAfter          *time.Time
	RunBefore         *time.Time
	PromptVersion     string
	PromptVersionHash string
	Limit             int
	Offset            int
}

// HistoricalBacktestRun captures a persisted historical run with strategy and
// configuration metadata for API consumers.
type HistoricalBacktestRun struct {
	RunID              uuid.UUID
	BacktestConfigID   uuid.UUID
	BacktestConfigName string
	StrategyID         uuid.UUID
	StrategyName       string
	Ticker             string
	MarketType         domain.MarketType
	StartDate          time.Time
	EndDate            time.Time
	RunTimestamp       time.Time
	Duration           time.Duration
	PromptVersion      string
	PromptVersionHash  string
	Metrics            backtest.Metrics
	ComparisonLabel    string
}

// HistoricalBacktestRunsResponse contains the filtered runs and the total match
// count before pagination.
type HistoricalBacktestRunsResponse struct {
	Runs  []HistoricalBacktestRun
	Total int
}

// CompareHistoricalBacktestRunsRequest identifies the persisted runs to compare
// side-by-side.
type CompareHistoricalBacktestRunsRequest struct {
	RunIDs []uuid.UUID
}

// HistoricalBacktestRunComparison contains the aligned metric table and
// relative metric deltas for the requested historical runs.
type HistoricalBacktestRunComparison struct {
	Runs         []HistoricalBacktestRun
	MetricTable  backtest.MetricComparisonTable
	SummaryDiffs []backtest.MetricComparisonDiff
}

// NewBacktestComparisonAPI constructs a BacktestComparisonAPI.
func NewBacktestComparisonAPI(
	strategies repository.StrategyRepository,
	configs repository.BacktestConfigRepository,
	runs repository.BacktestRunRepository,
) (*BacktestComparisonAPI, error) {
	switch {
	case strategies == nil:
		return nil, fmt.Errorf("api: strategy repository is required")
	case configs == nil:
		return nil, fmt.Errorf("api: backtest config repository is required")
	case runs == nil:
		return nil, fmt.Errorf("api: backtest run repository is required")
	default:
		return &BacktestComparisonAPI{
			strategies: strategies,
			configs:    configs,
			runs:       runs,
		}, nil
	}
}

// QueryHistoricalRuns returns historical backtest results that match the
// provided filters.
func (a *BacktestComparisonAPI) QueryHistoricalRuns(
	ctx context.Context,
	query HistoricalBacktestRunsQuery,
) (*HistoricalBacktestRunsResponse, error) {
	limit, offset, err := normalizeBacktestQuery(query)
	if err != nil {
		return nil, err
	}

	configs, err := a.listMatchingConfigs(ctx, query.StrategyIDs)
	if err != nil {
		return nil, err
	}

	strategyCache := make(map[uuid.UUID]*domain.Strategy)
	summaries := make([]HistoricalBacktestRun, 0)
	for _, cfg := range configs {
		runs, err := a.listAllRuns(ctx, repository.BacktestRunFilter{
			BacktestConfigID:  &cfg.ID,
			PromptVersion:     strings.TrimSpace(query.PromptVersion),
			PromptVersionHash: strings.TrimSpace(query.PromptVersionHash),
			RunAfter:          query.RunAfter,
			RunBefore:         query.RunBefore,
		})
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			summary, err := a.historicalRunSummary(ctx, run, cfg, strategyCache)
			if err != nil {
				return nil, err
			}
			summaries = append(summaries, summary)
		}
	}

	sortHistoricalRuns(summaries)
	total := len(summaries)
	summaries = paginateHistoricalRuns(summaries, limit, offset)

	return &HistoricalBacktestRunsResponse{
		Runs:  summaries,
		Total: total,
	}, nil
}

// CompareHistoricalRuns returns an aligned side-by-side comparison for two or
// more persisted historical runs.
func (a *BacktestComparisonAPI) CompareHistoricalRuns(
	ctx context.Context,
	req CompareHistoricalBacktestRunsRequest,
) (*HistoricalBacktestRunComparison, error) {
	runIDs, err := validateCompareHistoricalRunsRequest(req)
	if err != nil {
		return nil, err
	}

	configCache := make(map[uuid.UUID]domain.BacktestConfig)
	strategyCache := make(map[uuid.UUID]*domain.Strategy)
	runs := make([]HistoricalBacktestRun, 0, len(runIDs))
	inputs := make([]backtest.MetricComparisonInput, 0, len(runIDs))

	for _, runID := range runIDs {
		run, err := a.runs.Get(ctx, runID)
		if err != nil {
			return nil, fmt.Errorf("api: get backtest run %s: %w", runID, err)
		}

		cfg, err := a.getBacktestConfig(ctx, run.BacktestConfigID, configCache)
		if err != nil {
			return nil, err
		}

		summary, err := a.historicalRunSummary(ctx, *run, cfg, strategyCache)
		if err != nil {
			return nil, err
		}
		runs = append(runs, summary)
		inputs = append(inputs, backtest.MetricComparisonInput{
			Name:    summary.ComparisonLabel,
			Metrics: &summary.Metrics,
		})
	}

	table := backtest.BuildMetricComparisonTable(inputs)
	return &HistoricalBacktestRunComparison{
		Runs:         runs,
		MetricTable:  table,
		SummaryDiffs: backtest.BuildMetricComparisonDiffs(table),
	}, nil
}

func normalizeBacktestQuery(query HistoricalBacktestRunsQuery) (int, int, error) {
	if query.RunAfter != nil && query.RunBefore != nil && query.RunBefore.Before(*query.RunAfter) {
		return 0, 0, fmt.Errorf("api: run_before must be on or after run_after")
	}
	if query.Offset < 0 {
		return 0, 0, fmt.Errorf("api: offset must be non-negative")
	}
	limit := query.Limit
	if limit == 0 {
		limit = defaultBacktestComparisonLimit
	}
	if limit < 0 {
		return 0, 0, fmt.Errorf("api: limit must be positive")
	}
	return limit, query.Offset, nil
}

func validateCompareHistoricalRunsRequest(req CompareHistoricalBacktestRunsRequest) ([]uuid.UUID, error) {
	if len(req.RunIDs) < 2 {
		return nil, fmt.Errorf("api: at least 2 run IDs are required for comparison")
	}

	seen := make(map[uuid.UUID]struct{}, len(req.RunIDs))
	runIDs := make([]uuid.UUID, 0, len(req.RunIDs))
	for _, runID := range req.RunIDs {
		if runID == uuid.Nil {
			return nil, fmt.Errorf("api: run ID is required")
		}
		if _, ok := seen[runID]; ok {
			return nil, fmt.Errorf("api: duplicate run ID %s", runID)
		}
		seen[runID] = struct{}{}
		runIDs = append(runIDs, runID)
	}
	return runIDs, nil
}

func (a *BacktestComparisonAPI) listMatchingConfigs(ctx context.Context, strategyIDs []uuid.UUID) ([]domain.BacktestConfig, error) {
	if len(strategyIDs) == 0 {
		return a.listAllConfigs(ctx, repository.BacktestConfigFilter{})
	}

	seen := make(map[uuid.UUID]struct{})
	configs := make([]domain.BacktestConfig, 0)
	for _, strategyID := range strategyIDs {
		if strategyID == uuid.Nil {
			return nil, fmt.Errorf("api: strategy ID is required")
		}
		matched, err := a.listAllConfigs(ctx, repository.BacktestConfigFilter{StrategyID: &strategyID})
		if err != nil {
			return nil, err
		}
		for _, cfg := range matched {
			if _, ok := seen[cfg.ID]; ok {
				continue
			}
			seen[cfg.ID] = struct{}{}
			configs = append(configs, cfg)
		}
	}
	return configs, nil
}

func (a *BacktestComparisonAPI) listAllConfigs(ctx context.Context, filter repository.BacktestConfigFilter) ([]domain.BacktestConfig, error) {
	offset := 0
	configs := make([]domain.BacktestConfig, 0)
	for {
		page, err := a.configs.List(ctx, filter, backtestComparisonPageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("api: list backtest configs: %w", err)
		}
		configs = append(configs, page...)
		if len(page) < backtestComparisonPageSize {
			return configs, nil
		}
		offset += len(page)
	}
}

func (a *BacktestComparisonAPI) listAllRuns(ctx context.Context, filter repository.BacktestRunFilter) ([]domain.BacktestRun, error) {
	offset := 0
	runs := make([]domain.BacktestRun, 0)
	for {
		page, err := a.runs.List(ctx, filter, backtestComparisonPageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("api: list backtest runs: %w", err)
		}
		runs = append(runs, page...)
		if len(page) < backtestComparisonPageSize {
			return runs, nil
		}
		offset += len(page)
	}
}

func (a *BacktestComparisonAPI) getBacktestConfig(
	ctx context.Context,
	configID uuid.UUID,
	cache map[uuid.UUID]domain.BacktestConfig,
) (domain.BacktestConfig, error) {
	if cfg, ok := cache[configID]; ok {
		return cfg, nil
	}
	cfg, err := a.configs.Get(ctx, configID)
	if err != nil {
		return domain.BacktestConfig{}, fmt.Errorf("api: get backtest config %s: %w", configID, err)
	}
	cache[configID] = *cfg
	return *cfg, nil
}

func (a *BacktestComparisonAPI) historicalRunSummary(
	ctx context.Context,
	run domain.BacktestRun,
	cfg domain.BacktestConfig,
	strategyCache map[uuid.UUID]*domain.Strategy,
) (HistoricalBacktestRun, error) {
	strategy, err := a.getStrategy(ctx, cfg.StrategyID, strategyCache)
	if err != nil {
		return HistoricalBacktestRun{}, err
	}

	var metrics backtest.Metrics
	if err := json.Unmarshal(run.Metrics, &metrics); err != nil {
		return HistoricalBacktestRun{}, fmt.Errorf("api: decode backtest run metrics %s: %w", run.ID, err)
	}

	return HistoricalBacktestRun{
		RunID:              run.ID,
		BacktestConfigID:   cfg.ID,
		BacktestConfigName: cfg.Name,
		StrategyID:         strategy.ID,
		StrategyName:       strategy.Name,
		Ticker:             strategy.Ticker,
		MarketType:         strategy.MarketType,
		StartDate:          cfg.StartDate,
		EndDate:            cfg.EndDate,
		RunTimestamp:       run.RunTimestamp,
		Duration:           run.Duration,
		PromptVersion:      run.PromptVersion,
		PromptVersionHash:  run.PromptVersionHash,
		Metrics:            metrics,
		ComparisonLabel:    formatComparisonLabel(strategy.Name, run.RunTimestamp, run.PromptVersion),
	}, nil
}

func (a *BacktestComparisonAPI) getStrategy(
	ctx context.Context,
	strategyID uuid.UUID,
	cache map[uuid.UUID]*domain.Strategy,
) (*domain.Strategy, error) {
	if strategy, ok := cache[strategyID]; ok {
		return strategy, nil
	}
	strategy, err := a.strategies.Get(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("api: get strategy %s: %w", strategyID, err)
	}
	cache[strategyID] = strategy
	return strategy, nil
}

func formatComparisonLabel(strategyName string, runTimestamp time.Time, promptVersion string) string {
	label := strings.TrimSpace(strategyName)
	if label == "" {
		label = "run"
	}
	if version := strings.TrimSpace(promptVersion); version != "" {
		label += " [" + version + "]"
	}
	return label + " @ " + runTimestamp.UTC().Format(time.RFC3339)
}

func sortHistoricalRuns(runs []HistoricalBacktestRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].RunTimestamp.Equal(runs[j].RunTimestamp) {
			return strings.Compare(runs[i].RunID.String(), runs[j].RunID.String()) > 0
		}
		return runs[i].RunTimestamp.After(runs[j].RunTimestamp)
	})
}

func paginateHistoricalRuns(runs []HistoricalBacktestRun, limit, offset int) []HistoricalBacktestRun {
	if offset >= len(runs) {
		return nil
	}
	end := offset + limit
	if end > len(runs) {
		end = len(runs)
	}
	return runs[offset:end]
}
