package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/analysts"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// --- BacktestConfig CRUD handlers ---

func (s *Server) handleListBacktestConfigs(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	filter := repository.BacktestConfigFilter{}
	if v := r.URL.Query().Get("strategy_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid strategy_id", ErrCodeBadRequest)
			return
		}
		filter.StrategyID = &id
	}

	configs, err := s.backtestConfigs.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list backtest configs", ErrCodeInternal)
		return
	}
	respondList(w, configs, limit, offset)
}

func (s *Server) handleCreateBacktestConfig(w http.ResponseWriter, r *http.Request) {
	var config domain.BacktestConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if err := config.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	config.ID = uuid.New()
	if err := s.backtestConfigs.Create(r.Context(), &config); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create backtest config", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusCreated, config)
}

func (s *Server) handleGetBacktestConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	config, err := s.backtestConfigs.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest config not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get backtest config", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, config)
}

func (s *Server) handleUpdateBacktestConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	var config domain.BacktestConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	config.ID = id
	if err := config.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	if err := s.backtestConfigs.Update(r.Context(), &config); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest config not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to update backtest config", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, config)
}

func (s *Server) handleDeleteBacktestConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	if err := s.backtestConfigs.Delete(r.Context(), id); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest config not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete backtest config", ErrCodeInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Run a backtest ---

func (s *Server) handleRunBacktestConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	ctx := r.Context()

	// 1. Load BacktestConfig
	config, err := s.backtestConfigs.Get(ctx, id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest config not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get backtest config", ErrCodeInternal)
		return
	}

	// 2. Load Strategy
	strategy, err := s.strategies.Get(ctx, config.StrategyID)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "strategy not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get strategy", ErrCodeInternal)
		return
	}

	// 3. Parse strategy.Config as JSON, extract rules_engine field
	var stratCfg map[string]json.RawMessage
	if len(strategy.Config) > 0 {
		if err := json.Unmarshal(strategy.Config, &stratCfg); err != nil {
			respondError(w, http.StatusBadRequest, "invalid strategy config JSON", ErrCodeBadRequest)
			return
		}
	}

	rulesEngineRaw, ok := stratCfg["rules_engine"]
	if !ok || len(rulesEngineRaw) == 0 {
		respondError(w, http.StatusBadRequest, "strategy config must include a \"rules_engine\" JSON key with entry/exit conditions, position sizing, and stop/take-profit rules", ErrCodeBadRequest)
		return
	}

	// 4. Parse rules engine config
	rulesConfig, err := rules.Parse(rulesEngineRaw)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid rules_engine config: "+err.Error(), ErrCodeValidation)
		return
	}
	if rulesConfig == nil {
		respondError(w, http.StatusBadRequest, "strategy must have rules_engine config for backtesting", ErrCodeBadRequest)
		return
	}

	// 5. Load historical OHLCV bars — include 400 days before start for indicator warmup
	if s.dataService == nil {
		respondError(w, http.StatusInternalServerError, "data service not configured", ErrCodeInternal)
		return
	}
	warmupStart := config.StartDate.AddDate(-1, -2, 0) // ~400 calendar days for SMA-200 warmup
	barsMap, err := s.dataService.DownloadHistoricalOHLCV(
		ctx,
		strategy.MarketType,
		[]string{strategy.Ticker},
		data.Timeframe1d,
		warmupStart,
		config.EndDate,
		true,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load historical data: "+err.Error(), ErrCodeInternal)
		return
	}

	allBars := barsMap[strategy.Ticker]
	if len(allBars) == 0 {
		respondError(w, http.StatusBadRequest, "no historical bars available for ticker "+strategy.Ticker, ErrCodeBadRequest)
		return
	}

	// 6. Build pipeline — indicator node gets ALL bars (including warmup) for SMA-200 etc.
	pipeline := rules.NewRulesPipeline(*rulesConfig, allBars, config.StartDate, config.Simulation.InitialCapital, agent.NoopPersister{}, nil, s.logger)

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
	if s.llmProvider != nil {
		reviewer := rules.NewSignalReviewer(s.llmProvider, "", s.logger)
		orchConfig.EntryReviewFunc = func(ctx context.Context, plan *agent.TradingPlan, state *agent.PipelineState, bar domain.OHLCV, cash float64) (bool, string) {
			return reviewer.Review(ctx, plan, state, bar, cash)
		}
		orchConfig.ExitReviewFunc = reviewer.ReviewExit
	}
	orch, err := backtest.NewOrchestrator(orchConfig, allBars, pipeline, s.logger)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create backtest orchestrator: "+err.Error(), ErrCodeInternal)
		return
	}

	// 8. Run
	start := time.Now()
	result, err := orch.Run(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "backtest execution failed: "+err.Error(), ErrCodeInternal)
		return
	}
	duration := time.Since(start)

	// 9. Serialize metrics/trades/equity to JSON
	metricsJSON, err := json.Marshal(result.Metrics)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to serialize metrics", ErrCodeInternal)
		return
	}
	tradeLogJSON, err := json.Marshal(result.Trades)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to serialize trade log", ErrCodeInternal)
		return
	}
	equityCurveJSON, err := json.Marshal(result.EquityCurve)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to serialize equity curve", ErrCodeInternal)
		return
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

	// Ensure prompt version fields are populated
	if run.PromptVersionHash == "" {
		run.PromptVersionHash = analysts.CurrentPromptVersionHash()
	}
	if run.PromptVersion == "" {
		run.PromptVersion = "rules-v1"
	}

	if err := s.backtestRuns.Create(ctx, &run); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to persist backtest run: "+err.Error(), ErrCodeInternal)
		return
	}

	// Auto-activate inactive strategies that pass backtesting.
	// Criteria: positive Sharpe ratio and at least one trade executed.
	if strategy.Status == domain.StrategyStatusInactive &&
		result.Metrics.SharpeRatio > 0 &&
		len(result.Trades) > 0 {
		strategy.Status = domain.StrategyStatusActive
		if err := s.strategies.Update(ctx, strategy); err != nil {
			s.logger.Warn("backtest: failed to auto-activate strategy",
				"strategy_id", strategy.ID,
				"error", err,
			)
		} else {
			s.logger.Info("backtest: auto-activated strategy after passing backtest",
				"strategy_id", strategy.ID,
				"sharpe_ratio", result.Metrics.SharpeRatio,
				"total_trades", len(result.Trades),
			)
		}
	}

	respondJSON(w, http.StatusCreated, run)
}

// --- BacktestRun list/get handlers ---

func (s *Server) handleListBacktestRuns(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	filter := repository.BacktestRunFilter{}
	if v := r.URL.Query().Get("backtest_config_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid backtest_config_id", ErrCodeBadRequest)
			return
		}
		filter.BacktestConfigID = &id
	}

	runs, err := s.backtestRuns.List(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list backtest runs", ErrCodeInternal)
		return
	}
	respondList(w, runs, limit, offset)
}

func (s *Server) handleGetBacktestRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	run, err := s.backtestRuns.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "backtest run not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get backtest run", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, run)
}
