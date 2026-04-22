package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/analysts"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategyscaffold"
)

// BacktestService encapsulates the multi-step orchestration required to run a
// backtest config: load config, load strategy, fetch historical data, build
// the rules pipeline, run the orchestrator, persist the result, and
// optionally auto-activate the strategy.
type BacktestService struct {
	backtestConfigs repository.BacktestConfigRepository
	backtestRuns    repository.BacktestRunRepository
	strategies      repository.StrategyRepository
	auditLog        repository.AuditLogRepository
	dataService     *data.DataService
	llmProvider     llm.Provider
	logger          *slog.Logger
}

func NewBacktestService(
	backtestConfigs repository.BacktestConfigRepository,
	backtestRuns repository.BacktestRunRepository,
	strategies repository.StrategyRepository,
	auditLog repository.AuditLogRepository,
	dataService *data.DataService,
	llmProvider llm.Provider,
	logger *slog.Logger,
) *BacktestService {
	return &BacktestService{
		backtestConfigs: backtestConfigs,
		backtestRuns:    backtestRuns,
		strategies:      strategies,
		auditLog:        auditLog,
		dataService:     dataService,
		llmProvider:     llmProvider,
		logger:          logger,
	}
}

// RunBacktest executes the 10-step backtest orchestration and persists the
// result. Returns the persisted BacktestRun on success, or a *ServiceError
// for caller-visible errors.
func (svc *BacktestService) RunBacktest(ctx context.Context, configID uuid.UUID, actor string) (*domain.BacktestRun, error) {
	config, err := svc.backtestConfigs.Get(ctx, configID)
	if err != nil {
		if isNotFound(err) {
			return nil, &ServiceError{Status: 404, Message: "backtest config not found"}
		}
		return nil, &ServiceError{Status: 500, Message: "failed to get backtest config"}
	}

	strategy, err := svc.strategies.Get(ctx, config.StrategyID)
	if err != nil {
		if isNotFound(err) {
			return nil, &ServiceError{Status: 404, Message: "strategy not found"}
		}
		return nil, &ServiceError{Status: 500, Message: "failed to get strategy"}
	}

	var stratCfg map[string]json.RawMessage
	if len(strategy.Config) > 0 {
		if err := json.Unmarshal(strategy.Config, &stratCfg); err != nil {
			return nil, &ServiceError{Status: 400, Message: "invalid strategy config JSON"}
		}
	}

	if rulesEngineRaw := stratCfg["rules_engine"]; len(rulesEngineRaw) > 0 {
		return svc.runRulesEngineBacktest(ctx, config, strategy, rulesEngineRaw, actor)
	}

	if optionsRulesRaw := stratCfg["options_rules"]; len(optionsRulesRaw) > 0 {
		return svc.runOptionsRulesBacktest(ctx, config, strategy, optionsRulesRaw, actor)
	}

	return nil, &ServiceError{Status: 400, Message: "strategy config must include either a \"rules_engine\" or \"options_rules\" JSON key for backtesting"}
}

func (svc *BacktestService) runRulesEngineBacktest(
	ctx context.Context,
	config *domain.BacktestConfig,
	strategy *domain.Strategy,
	rulesEngineRaw json.RawMessage,
	actor string,
) (*domain.BacktestRun, error) {
	rulesConfig, err := rules.Parse(rulesEngineRaw)
	if err != nil {
		return nil, &ServiceError{Status: 400, Message: "invalid rules_engine config: " + err.Error()}
	}
	if rulesConfig == nil {
		return nil, &ServiceError{Status: 400, Message: "strategy must have rules_engine config for backtesting"}
	}

	allBars, svcErr := svc.loadHistoricalBars(ctx, strategy.Ticker, strategy.MarketType, config)
	if svcErr != nil {
		return nil, svcErr
	}

	pipeline := rules.NewRulesPipeline(*rulesConfig, allBars, config.StartDate, config.Simulation.InitialCapital, agent.NoopPersister{}, nil, svc.logger)

	orchConfig := backtest.OrchestratorConfig{
		StrategyID:  strategy.ID,
		Ticker:      strategy.Ticker,
		StartDate:   config.StartDate,
		EndDate:     config.EndDate,
		InitialCash: config.Simulation.InitialCapital,
		FillConfig: backtest.FillConfig{
			Slippage: backtest.ProportionalSlippage{BasisPoints: 5},
		},
	}
	if svc.llmProvider != nil {
		reviewer := rules.NewSignalReviewer(svc.llmProvider, "", svc.logger)
		orchConfig.EntryReviewFunc = func(ctx context.Context, plan *agent.TradingPlan, state *agent.PipelineState, bar domain.OHLCV, cash float64) (bool, string) {
			return reviewer.Review(ctx, plan, state, bar, cash)
		}
		orchConfig.ExitReviewFunc = reviewer.ReviewExit
	}
	orch, err := backtest.NewOrchestrator(orchConfig, allBars, pipeline, svc.logger)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to create backtest orchestrator: " + err.Error()}
	}

	start := time.Now()
	result, err := orch.Run(ctx)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "backtest execution failed: " + err.Error()}
	}
	duration := time.Since(start)

	metricsJSON, err := json.Marshal(result.Metrics)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to serialize metrics"}
	}
	tradeLogJSON, err := json.Marshal(result.Trades)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to serialize trade log"}
	}
	equityCurveJSON, err := json.Marshal(result.EquityCurve)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to serialize equity curve"}
	}

	run, svcErr := svc.persistBacktestRun(ctx, actor, config.ID, strategy.Ticker, metricsJSON, tradeLogJSON, equityCurveJSON, start, duration, result.PromptVersion, result.PromptVersionHash)
	if svcErr != nil {
		return nil, svcErr
	}

	if strategy.Status == domain.StrategyStatusInactive &&
		result.Metrics.SharpeRatio > 0 &&
		len(result.Trades) > 0 {
		strategy.Status = domain.StrategyStatusActive
		if err := svc.strategies.Update(ctx, strategy); err != nil {
			svc.logger.Warn("backtest: failed to auto-activate strategy",
				"strategy_id", strategy.ID, "error", err)
		} else {
			svc.logger.Info("backtest: auto-activated strategy after passing backtest",
				"strategy_id", strategy.ID,
				"sharpe_ratio", result.Metrics.SharpeRatio,
				"total_trades", len(result.Trades),
			)
		}
	}

	return &run, nil
}

func (svc *BacktestService) runOptionsRulesBacktest(
	ctx context.Context,
	config *domain.BacktestConfig,
	strategy *domain.Strategy,
	optionsRulesRaw json.RawMessage,
	actor string,
) (*domain.BacktestRun, error) {
	optionsConfig, err := rules.ParseOptions(optionsRulesRaw)
	if err != nil {
		return nil, &ServiceError{Status: 400, Message: "invalid options_rules config: " + err.Error()}
	}
	if optionsConfig == nil {
		return nil, &ServiceError{Status: 400, Message: "strategy must have options_rules config for backtesting"}
	}
	underlying := strings.ToUpper(strings.TrimSpace(optionsConfig.Underlying))
	if underlying == "" {
		underlying = strategy.Ticker
	}

	allBars, svcErr := svc.loadHistoricalBars(ctx, underlying, domain.MarketTypeStock, config)
	if svcErr != nil {
		return nil, svcErr
	}

	start := time.Now()
	summary, err := strategyscaffold.RunOptionsPaperBacktestWithConfig(
		ctx,
		*optionsConfig,
		allBars,
		config.StartDate,
		config.EndDate,
		config.Simulation.InitialCapital,
		svc.logger,
	)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "backtest execution failed: " + err.Error()}
	}
	duration := time.Since(start)

	metricsJSON, err := json.Marshal(summary.Metrics)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to serialize metrics"}
	}
	tradeLogJSON, err := json.Marshal(summary.Trades)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to serialize trade log"}
	}
	equityCurveJSON, err := json.Marshal(summary.EquityCurve)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to serialize equity curve"}
	}

	run, svcErr := svc.persistBacktestRun(ctx, actor, config.ID, underlying, metricsJSON, tradeLogJSON, equityCurveJSON, start, duration, "options-rules-v1", analysts.CurrentPromptVersionHash())
	if svcErr != nil {
		return nil, svcErr
	}

	if strategy.Status == domain.StrategyStatusInactive &&
		summary.Validation != nil &&
		summary.Validation.Passed &&
		summary.Metrics.SharpeRatio > 0 &&
		len(summary.Trades) > 0 {
		strategy.Status = domain.StrategyStatusActive
		if err := svc.strategies.Update(ctx, strategy); err != nil {
			svc.logger.Warn("backtest: failed to auto-activate strategy",
				"strategy_id", strategy.ID, "error", err)
		} else {
			svc.logger.Info("backtest: auto-activated options strategy after passing backtest",
				"strategy_id", strategy.ID,
				"sharpe_ratio", summary.Metrics.SharpeRatio,
				"oos_ratio", summary.Validation.OOSRatio,
			)
		}
	}

	return &run, nil
}

func (svc *BacktestService) loadHistoricalBars(
	ctx context.Context,
	ticker string,
	marketType domain.MarketType,
	config *domain.BacktestConfig,
) ([]domain.OHLCV, *ServiceError) {
	if svc.dataService == nil {
		return nil, &ServiceError{Status: 500, Message: "data service not configured"}
	}
	warmupStart := config.StartDate.AddDate(-1, -2, 0)
	barsMap, err := svc.dataService.DownloadHistoricalOHLCV(
		ctx,
		marketType,
		[]string{ticker},
		data.Timeframe1d,
		warmupStart,
		config.EndDate,
		true,
	)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to load historical data: " + err.Error()}
	}
	allBars := barsMap[ticker]
	if len(allBars) == 0 {
		return nil, &ServiceError{Status: 400, Message: "no historical bars available for ticker " + ticker}
	}
	return allBars, nil
}

func (svc *BacktestService) persistBacktestRun(
	ctx context.Context,
	actor string,
	configID uuid.UUID,
	ticker string,
	metricsJSON json.RawMessage,
	tradeLogJSON json.RawMessage,
	equityCurveJSON json.RawMessage,
	start time.Time,
	duration time.Duration,
	promptVersion string,
	promptVersionHash string,
) (domain.BacktestRun, *ServiceError) {
	run := domain.BacktestRun{
		ID:                uuid.New(),
		BacktestConfigID:  configID,
		Metrics:           metricsJSON,
		TradeLog:          tradeLogJSON,
		EquityCurve:       equityCurveJSON,
		RunTimestamp:      start.UTC(),
		Duration:          duration,
		PromptVersion:     promptVersion,
		PromptVersionHash: promptVersionHash,
	}

	if run.PromptVersionHash == "" {
		run.PromptVersionHash = analysts.CurrentPromptVersionHash()
	}
	if run.PromptVersion == "" {
		run.PromptVersion = "rules-v1"
	}

	if err := svc.backtestRuns.Create(ctx, &run); err != nil {
		return domain.BacktestRun{}, &ServiceError{Status: 500, Message: "failed to persist backtest run: " + err.Error()}
	}

	svc.writeAuditLog(ctx, actor, "backtest.run", "backtest_config", &configID,
		map[string]any{"ticker": ticker, "run_id": run.ID})

	return run, nil
}

func (svc *BacktestService) writeAuditLog(ctx context.Context, actor, eventType, entityType string, entityID *uuid.UUID, details any) {
	if svc.auditLog == nil {
		return
	}
	var raw json.RawMessage
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			raw = b
		}
	}
	entry := &domain.AuditLogEntry{
		ID:         uuid.New(),
		EventType:  eventType,
		EntityType: entityType,
		EntityID:   entityID,
		Actor:      actor,
		Details:    raw,
		CreatedAt:  time.Now().UTC(),
	}
	if err := svc.auditLog.Create(ctx, entry); err != nil {
		svc.logger.Warn("audit log write failed",
			"event_type", eventType, "error", err.Error())
	}
}
