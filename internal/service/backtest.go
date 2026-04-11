package service

import (
	"context"
	"encoding/json"
	"log/slog"
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
	// 1. Load BacktestConfig
	config, err := svc.backtestConfigs.Get(ctx, configID)
	if err != nil {
		if isNotFound(err) {
			return nil, &ServiceError{Status: 404, Message: "backtest config not found"}
		}
		return nil, &ServiceError{Status: 500, Message: "failed to get backtest config"}
	}

	// 2. Load Strategy
	strategy, err := svc.strategies.Get(ctx, config.StrategyID)
	if err != nil {
		if isNotFound(err) {
			return nil, &ServiceError{Status: 404, Message: "strategy not found"}
		}
		return nil, &ServiceError{Status: 500, Message: "failed to get strategy"}
	}

	// 3. Parse strategy.Config as JSON, extract rules_engine field
	var stratCfg map[string]json.RawMessage
	if len(strategy.Config) > 0 {
		if err := json.Unmarshal(strategy.Config, &stratCfg); err != nil {
			return nil, &ServiceError{Status: 400, Message: "invalid strategy config JSON"}
		}
	}

	rulesEngineRaw, ok := stratCfg["rules_engine"]
	if !ok || len(rulesEngineRaw) == 0 {
		return nil, &ServiceError{Status: 400, Message: "strategy config must include a \"rules_engine\" JSON key with entry/exit conditions, position sizing, and stop/take-profit rules"}
	}

	// 4. Parse rules engine config
	rulesConfig, err := rules.Parse(rulesEngineRaw)
	if err != nil {
		return nil, &ServiceError{Status: 400, Message: "invalid rules_engine config: " + err.Error()}
	}
	if rulesConfig == nil {
		return nil, &ServiceError{Status: 400, Message: "strategy must have rules_engine config for backtesting"}
	}

	// 5. Load historical OHLCV bars — include 400 days before start for indicator warmup
	if svc.dataService == nil {
		return nil, &ServiceError{Status: 500, Message: "data service not configured"}
	}
	warmupStart := config.StartDate.AddDate(-1, -2, 0) // ~400 calendar days for SMA-200 warmup
	barsMap, err := svc.dataService.DownloadHistoricalOHLCV(
		ctx,
		strategy.MarketType,
		[]string{strategy.Ticker},
		data.Timeframe1d,
		warmupStart,
		config.EndDate,
		true,
	)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to load historical data: " + err.Error()}
	}

	allBars := barsMap[strategy.Ticker]
	if len(allBars) == 0 {
		return nil, &ServiceError{Status: 400, Message: "no historical bars available for ticker " + strategy.Ticker}
	}

	// 6. Build pipeline
	pipeline := rules.NewRulesPipeline(*rulesConfig, allBars, config.StartDate, config.Simulation.InitialCapital, agent.NoopPersister{}, nil, svc.logger)

	// 7. Build orchestrator with default fill config + optional LLM reviewer
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

	// 8. Run
	start := time.Now()
	result, err := orch.Run(ctx)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "backtest execution failed: " + err.Error()}
	}
	duration := time.Since(start)

	// 9. Serialize metrics/trades/equity to JSON
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

	// 10. Create BacktestRun and persist
	run := domain.BacktestRun{
		ID:                uuid.New(),
		BacktestConfigID:  config.ID,
		Metrics:           metricsJSON,
		TradeLog:          tradeLogJSON,
		EquityCurve:       equityCurveJSON,
		RunTimestamp:      start.UTC(),
		Duration:          duration,
		PromptVersion:     result.PromptVersion,
		PromptVersionHash: result.PromptVersionHash,
	}

	if run.PromptVersionHash == "" {
		run.PromptVersionHash = analysts.CurrentPromptVersionHash()
	}
	if run.PromptVersion == "" {
		run.PromptVersion = "rules-v1"
	}

	if err := svc.backtestRuns.Create(ctx, &run); err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to persist backtest run: " + err.Error()}
	}

	svc.writeAuditLog(ctx, actor, "backtest.run", "backtest_config", &configID,
		map[string]any{"ticker": strategy.Ticker, "run_id": run.ID})

	// Auto-activate inactive strategies that pass backtesting.
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
