package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	agentanalysts "github.com/PatrickFanella/get-rich-quick/internal/agent/analysts"
	agentdebate "github.com/PatrickFanella/get-rich-quick/internal/agent/debate"
	agentrisk "github.com/PatrickFanella/get-rich-quick/internal/agent/risk"
	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	agenttrader "github.com/PatrickFanella/get-rich-quick/internal/agent/trader"
	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	alpacaexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/alpaca"
	binanceexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/binance"
	kalshiexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	polymarketexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	polymarketdata "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	"github.com/PatrickFanella/get-rich-quick/internal/notification"
	"github.com/PatrickFanella/get-rich-quick/internal/portfolio"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

const (
	strategyMarketLookback = 400 * 24 * time.Hour
	strategyNewsLookback   = 7 * 24 * time.Hour
	strategySocialLookback = 7 * 24 * time.Hour
	postCloseDataGrace     = 30 * time.Minute
	requiredNewsMaxAge     = 36 * time.Hour
	requiredNewsMinDirect  = 3
	localPaperBuyingPower  = 100_000.0
	nativeTerminalTimeout  = 5 * time.Second
)

var defaultAnalysisRoles = []agent.AgentRole{
	agent.AgentRoleMarketAnalyst,
	agent.AgentRoleFundamentalsAnalyst,
	agent.AgentRoleNewsAnalyst,
	agent.AgentRoleSocialMediaAnalyst,
}

type marketDataService interface {
	GetOHLCV(ctx context.Context, marketType domain.MarketType, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error)
	GetFundamentals(ctx context.Context, marketType domain.MarketType, ticker string) (data.Fundamentals, error)
	GetNews(ctx context.Context, marketType domain.MarketType, ticker string, from, to time.Time) ([]data.NewsArticle, error)
	GetSocialSentiment(ctx context.Context, marketType domain.MarketType, ticker string, from, to time.Time) ([]data.SocialSentiment, error)
}

type promptOverrideSource interface {
	Overrides() map[agent.AgentRole]string
}

type polymarketMarketDataSource interface {
	GetMarketData(ctx context.Context, slug string) (*agent.PredictionMarketData, error)
}

type kalshiMarketDataSource interface {
	LoadSnapshot(ctx context.Context, ticker string) (kalshiexecution.Snapshot, error)
}

type polymarketTickFeed interface {
	Ticks(slug string) <-chan polymarketdata.Tick
}

type realStrategyRunner struct {
	cfg                    config.Config
	globals                agent.GlobalSettings
	dataService            marketDataService
	optionsProvider        data.OptionsDataProvider
	runRepo                repository.PipelineRunRepository
	snapshotRepo           repository.PipelineRunSnapshotRepository
	decisionRepo           repository.AgentDecisionRepository
	eventRepo              repository.AgentEventRepository
	orderRepo              repository.OrderRepository
	positionRepo           repository.PositionRepository
	tradeRepo              repository.TradeRepository
	opportunityRepo        repository.OpportunityRepository
	auditLogRepo           repository.AuditLogRepository
	financialRepo          repository.FinancialLifecycleRepository
	riskEngine             risk.RiskEngine
	tradeDecisionRecorder  execution.DecisionRecorder
	metrics                *metrics.Metrics
	notificationManager    *notification.Manager
	runRegistry            *agent.RunContextRegistry
	llmBudget              *llm.Budget
	promptOverrides        promptOverrideSource
	logger                 *slog.Logger
	localPaperMu           sync.Mutex
	localPaperBroker       *paper.PaperBroker
	portfolioAllocatorMode portfolio.AllocatorMode
	kalshiLiveClient       kalshiexecution.LiveClient
	kalshiDataProvider     kalshiMarketDataSource
	polymarketClient       *polymarketexecution.Client // nil if not configured
	polymarketMarketData   polymarketMarketDataSource
	kalshiMarketData       kalshiMarketDataSource
	polymarketFeed         polymarketTickFeed
	polymarketStopGuard    *polymarketexecution.StopGuard
	polymarketWorkers      sync.Map
	polymarketWorkerCtx    context.Context
	polymarketWorkerStop   context.CancelFunc
	polymarketWorkerWG     sync.WaitGroup
	hub                    *api.Hub // nil until wired; optional WebSocket broadcast
}

func newRealStrategyRunner(
	cfg config.Config,
	dataService marketDataService,
	runRepo repository.PipelineRunRepository,
	snapshotRepo repository.PipelineRunSnapshotRepository,
	decisionRepo repository.AgentDecisionRepository,
	eventRepo repository.AgentEventRepository,
	orderRepo repository.OrderRepository,
	positionRepo repository.PositionRepository,
	tradeRepo repository.TradeRepository,
	auditLogRepo repository.AuditLogRepository,
	financialRepo repository.FinancialLifecycleRepository,
	riskEngine risk.RiskEngine,
	appMetrics *metrics.Metrics,
	notificationManager *notification.Manager,
	runRegistry *agent.RunContextRegistry,
	llmBudget *llm.Budget,
	promptOverrides promptOverrideSource,
	tradeDecisionRecorder execution.DecisionRecorder,
	kalshiDataProvider kalshiMarketDataSource,
	kalshiLiveClient kalshiexecution.LiveClient,
	polymarketFeed polymarketTickFeed,
	logger *slog.Logger,
) *realStrategyRunner {
	if logger == nil {
		logger = slog.Default()
	}

	workerCtx, workerStop := context.WithCancel(context.Background())
	runner := &realStrategyRunner{
		cfg:                   cfg,
		globals:               globalSettingsFromConfig(cfg),
		dataService:           dataService,
		runRepo:               runRepo,
		snapshotRepo:          snapshotRepo,
		decisionRepo:          decisionRepo,
		eventRepo:             eventRepo,
		orderRepo:             orderRepo,
		positionRepo:          positionRepo,
		tradeRepo:             tradeRepo,
		auditLogRepo:          auditLogRepo,
		financialRepo:         financialRepo,
		riskEngine:            riskEngine,
		tradeDecisionRecorder: tradeDecisionRecorder,
		metrics:               appMetrics,
		notificationManager:   notificationManager,
		runRegistry:           runRegistry,
		llmBudget:             llmBudget,
		promptOverrides:       promptOverrides,
		logger:                logger,
		polymarketFeed:        polymarketFeed,
		kalshiDataProvider:    kalshiDataProvider,
		kalshiLiveClient:      kalshiLiveClient,
		polymarketWorkerCtx:   workerCtx,
		polymarketWorkerStop:  workerStop,
		localPaperBroker:      paper.NewPaperBroker(localPaperBuyingPower, 0, 0),
	}
	runner.setRiskPortfolioSnapshotSource(runner.localPaperBroker)

	// Wire Polymarket client if credentials are configured.
	pm := cfg.Brokers.Polymarket
	if strings.TrimSpace(pm.KeyID) != "" {
		client := polymarketexecution.NewClient(pm.KeyID, pm.SecretKey, logger)
		client.SetL2Auth(pm.Address, pm.KeyID, pm.SecretKey, pm.Passphrase)
		client.SetAPIBaseURL(pm.APIBaseURL)
		client.SetGatewayBaseURL(pm.GatewayBaseURL)
		runner.polymarketClient = client
		runner.polymarketMarketData = client
		if strings.TrimSpace(pm.SecretKey) != "" {
			if guard, err := polymarketexecution.NewStopGuard(polymarketexecution.StopGuardConfig{Broker: polymarketexecution.NewBroker(client), Logger: logger, Metrics: appMetrics}); err == nil {
				runner.polymarketStopGuard = guard
			} else {
				logger.Warn("polymarket stop guard disabled", slog.String("error", err.Error()))
			}
		}
	}
	if runner.polymarketMarketData == nil {
		client := polymarketexecution.NewClient("", "", logger)
		client.SetGatewayBaseURL(pm.GatewayBaseURL)
		runner.polymarketMarketData = client
	}
	if runner.kalshiMarketData == nil {
		runner.kalshiMarketData = runner.kalshiDataProvider
	}

	return runner
}

func (r *realStrategyRunner) RunStrategy(ctx context.Context, strategy domain.Strategy) (*api.StrategyRunResult, error) {
	if strategy.MarketType.Normalize() == domain.MarketTypeKalshi {
		return r.runKalshiNative(ctx, strategy)
	}
	if strategy.MarketType.Normalize() == domain.MarketTypePolymarket {
		return r.runPolymarketNative(ctx, strategy)
	}

	runner, prepared, strategyConfig, eventsCh, err := r.prepareStrategyRun(ctx, strategy)
	if err != nil {
		if persistErr := r.recordStrategyPreparationFailure(ctx, strategy, err); persistErr != nil {
			return nil, errors.Join(err, persistErr)
		}
		return nil, err
	}

	// Drain phase events to the WebSocket hub in a background goroutine.
	// The channel is closed after runner.Run returns so the goroutine exits naturally.
	if eventsCh != nil {
		go r.drainPipelineEvents(eventsCh)
	}

	result, err := runner.Run(ctx, prepared)

	// Close the channel regardless of success/failure — the drainer exits via range.
	if eventsCh != nil {
		close(eventsCh)
	}
	defer r.refreshExecutionMetrics(context.Background())
	if result != nil {
		r.recordPipelineMetrics(result.Run)
	}

	if err != nil {
		return nil, err
	}

	run, err := r.findRun(ctx, result.Run.ID)
	if err != nil {
		return nil, err
	}

	agent.ApplyStrategyRiskOverridesToResult(result, strategyConfig)
	signal := result.Signal
	state := agent.PipelineStateFromView(result.State)
	planTicker := result.State.TradingPlan.Ticker
	if planTicker == "" {
		planTicker = strategy.Ticker
	}

	update := repository.PipelineRunStatusUpdate{
		Status:       run.Status,
		Signal:       &signal,
		CompletedAt:  run.CompletedAt,
		ErrorMessage: run.ErrorMessage,
	}
	if err := r.runRepo.UpdateStatus(ctx, run.ID, run.TradeDate, update); err != nil {
		return nil, err
	}
	run.Signal = signal

	if strategy.MarketType.Normalize() == domain.MarketTypePolymarket {
		normalizedSide, err := normalizePolymarketStrategySide(result.State.TradingPlan.Side)
		if err != nil {
			return nil, fmt.Errorf("polymarket strategy %s: %w", strategy.Name, err)
		}
		result.State.TradingPlan.Side = normalizedSide
		entryPrice := result.State.TradingPlan.EntryPrice
		if entryPrice > 0 && entryPrice > 1 {
			return nil, fmt.Errorf("polymarket strategy %s: entry price %.4f outside valid range [0,1]", strategy.Name, entryPrice)
		}
	}

	decisionMetadata := r.executionDecisionMetadata(ctx, run.ID)

	finalSignal := execution.FinalSignal{Signal: signal, Confidence: result.State.FinalSignal.Confidence}
	tradingPlan := execution.TradingPlan{
		Action:           signal,
		MarketType:       strategy.MarketType.Normalize(),
		Ticker:           planTicker,
		EntryType:        result.State.TradingPlan.EntryType,
		EntryPrice:       result.State.TradingPlan.EntryPrice,
		PositionSize:     result.State.TradingPlan.PositionSize,
		StopLoss:         result.State.TradingPlan.StopLoss,
		TakeProfit:       result.State.TradingPlan.TakeProfit,
		TimeHorizon:      result.State.TradingPlan.TimeHorizon,
		Confidence:       result.State.TradingPlan.Confidence,
		Rationale:        result.State.TradingPlan.Rationale,
		RiskReward:       result.State.TradingPlan.RiskReward,
		Side:             result.State.TradingPlan.Side,
		DecisionMetadata: decisionMetadata,
	}
	r.recordPortfolioOpportunity(ctx, strategy, &run.ID, finalSignal, tradingPlan)

	if strategy.MarketType.Normalize() == domain.MarketTypeOptions {
		if err := r.executeOptionsSignal(ctx, strategy, run.ID, finalSignal); err != nil {
			return nil, err
		}
	} else if !r.portfolioAllocatorOwnsPaperExecution(strategy, signal) {
		orderManager, err := r.newOrderManager(ctx, strategy, prepared.Config, strategyConfig)
		if err != nil {
			return nil, err
		}
		if err := orderManager.ProcessSignal(ctx, finalSignal, tradingPlan, strategy.ID, run.ID); err != nil {
			return nil, err
		}
	}

	// Notification delivery is an optional side effect and must not monopolize a
	// scheduler execution slot when a webhook is slow or rate limited.
	notificationCtx, cancelNotifications := context.WithTimeout(ctx, 30*time.Second)
	defer cancelNotifications()
	if err := r.dispatchNotifications(notificationCtx, strategy, run, state); err != nil {
		r.logger.WarnContext(ctx, "notification dispatch failed (non-fatal)", "error", err, "run_id", run.ID)
	}

	orders, err := r.orderRepo.GetByRun(ctx, run.ID, repository.OrderFilter{}, 10, 0)
	if err != nil {
		return nil, err
	}
	positions, err := r.positionRepo.GetByStrategy(ctx, strategy.ID, repository.PositionFilter{}, 10, 0)
	if err != nil {
		return nil, err
	}

	return &api.StrategyRunResult{
		Run:       *run,
		Signal:    signal,
		Orders:    orders,
		Positions: positions,
	}, nil
}

func (r *realStrategyRunner) executeOptionsSignal(ctx context.Context, strategy domain.Strategy, runID uuid.UUID, signal execution.FinalSignal) error {
	if signal.Signal == domain.PipelineSignalHold {
		return nil
	}
	if !strategy.IsPaper {
		return errors.New("options runtime: live options execution is disabled")
	}
	if r.optionsProvider == nil {
		return errors.New("options runtime: options data provider is required")
	}
	manager := execution.NewOptionsOrderManager(r.localPaperBroker, r.orderRepo, r.positionRepo, r.tradeRepo, r.riskEngine, r.logger).
		WithBrokerName("paper").WithLiveTrading(false)
	if signal.Signal == domain.PipelineSignalSell {
		positions, err := r.positionRepo.GetByStrategy(ctx, strategy.ID, repository.PositionFilter{}, 100, 0)
		if err != nil {
			return fmt.Errorf("options runtime: load positions for close: %w", err)
		}
		var open []*domain.Position
		for index := range positions {
			if positions[index].AssetClass == domain.AssetClassOption && positions[index].ClosedAt == nil && positions[index].Quantity > 0 {
				open = append(open, &positions[index])
			}
		}
		if len(open) == 0 {
			return nil
		}
		position := open[0]
		if position.OptionType == nil || position.Expiry == nil {
			return errors.New("options runtime: persisted position lacks contract metadata")
		}
		chain, err := r.optionsProvider.GetOptionsChain(ctx, position.UnderlyingTicker, *position.Expiry, *position.OptionType)
		if err != nil {
			return fmt.Errorf("options runtime: load close quote: %w", err)
		}
		if len(open) > 1 {
			spread, quantity, err := buildPaperSpreadClosePlan(open, chain)
			if err != nil {
				return err
			}
			return manager.ProcessSpreadSignal(ctx, spread, quantity, strategy.ID, runID)
		}
		closePrice, err := executableOptionClosePrice(position, chain)
		if err != nil {
			return err
		}
		return manager.CloseOptionPosition(ctx, position, closePrice, runID, "strategy sell signal")
	}
	if signal.Signal != domain.PipelineSignalBuy {
		return fmt.Errorf("options runtime: unsupported signal %q", signal.Signal)
	}
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(strategy.Config, &sections); err != nil {
		return fmt.Errorf("options runtime: parse strategy config: %w", err)
	}
	cfg, err := rules.ParseOptions(sections["options_rules"])
	if err != nil {
		return fmt.Errorf("options runtime: %w", err)
	}
	if cfg == nil {
		return errors.New("options runtime: options_rules config is required")
	}
	chain, err := r.optionsProvider.GetOptionsChain(ctx, cfg.Underlying, time.Time{}, "")
	if err != nil {
		return fmt.Errorf("options runtime: load chain: %w", err)
	}
	if len(cfg.LegSelection) > 1 {
		spread, quantity, err := buildPaperDebitSpreadPlan(cfg, chain, time.Now().UTC())
		if err != nil {
			return err
		}
		legs := make([]risk.OptionLegExposure, 0, len(spread.Legs))
		for _, leg := range spread.Legs {
			legs = append(legs, risk.OptionLegExposure{Greeks: leg.Greeks, Side: leg.Side, Quantity: quantity * float64(leg.Ratio), Multiplier: leg.Contract.Multiplier})
		}
		if err := r.enforceOptionsGreekRisk(ctx, cfg.Underlying, legs); err != nil {
			return err
		}
		return manager.ProcessSpreadSignal(ctx, spread, quantity, strategy.ID, runID)
	}
	plan, err := buildPaperSingleLegPlan(cfg, chain, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := r.enforceOptionsGreekRisk(ctx, cfg.Underlying, []risk.OptionLegExposure{{Greeks: *plan.OptionGreeks, Side: domain.OrderSideBuy, Quantity: plan.PositionSize, Multiplier: 100}}); err != nil {
		return err
	}
	return manager.ProcessOptionSignal(ctx, signal, plan, strategy.ID, runID)
}

func buildPaperSpreadClosePlan(positions []*domain.Position, chain []domain.OptionSnapshot) (*domain.OptionSpread, float64, error) {
	if len(positions) != 2 || positions[0] == nil || positions[0].LegGroupID == nil {
		return nil, 0, errors.New("options runtime: exactly one persisted two-leg group is required for atomic close")
	}
	first, quantity := positions[0], positions[0].Quantity
	if first.OptionType == nil || first.Expiry == nil || quantity <= 0 {
		return nil, 0, errors.New("options runtime: spread position metadata is incomplete")
	}
	strategyType := domain.StrategyBullCallSpread
	if *first.OptionType == domain.OptionTypePut {
		strategyType = domain.StrategyBearPutSpread
	}
	spread := &domain.OptionSpread{StrategyType: strategyType, Underlying: first.UnderlyingTicker}
	for _, position := range positions {
		if position == nil || position.LegGroupID == nil || *position.LegGroupID != *first.LegGroupID || position.Quantity != quantity || position.UnderlyingTicker != first.UnderlyingTicker || position.OptionType == nil || *position.OptionType != *first.OptionType || position.Expiry == nil || !position.Expiry.Equal(*first.Expiry) || position.Strike == nil {
			return nil, 0, errors.New("options runtime: spread legs do not share durable group and contract semantics")
		}
		price, err := executableOptionClosePrice(position, chain)
		if err != nil {
			return nil, 0, err
		}
		contract := domain.OptionContract{OCCSymbol: position.Ticker, Underlying: position.UnderlyingTicker, OptionType: *position.OptionType, Strike: *position.Strike, Expiry: *position.Expiry, Multiplier: position.ContractMultiplier}
		leg := domain.SpreadLeg{Contract: contract, Ratio: 1, Quantity: quantity, ExecutablePrice: price}
		if position.Side == domain.PositionSideLong {
			leg.Side, leg.PositionIntent = domain.OrderSideSell, domain.PositionIntentSellToClose
		} else {
			leg.Side, leg.PositionIntent = domain.OrderSideBuy, domain.PositionIntentBuyToClose
		}
		if position.Delta != nil {
			leg.Greeks.Delta = *position.Delta
		}
		if position.Gamma != nil {
			leg.Greeks.Gamma = *position.Gamma
		}
		if position.Theta != nil {
			leg.Greeks.Theta = *position.Theta
		}
		if position.Vega != nil {
			leg.Greeks.Vega = *position.Vega
		}
		spread.Legs = append(spread.Legs, leg)
	}
	return spread, quantity, nil
}

func (r *realStrategyRunner) enforceOptionsGreekRisk(ctx context.Context, underlying string, proposed []risk.OptionLegExposure) error {
	to := time.Now().UTC()
	bars, err := r.dataService.GetOHLCV(ctx, domain.MarketTypeStock, underlying, data.Timeframe1d, to.Add(-7*24*time.Hour), to)
	if err != nil || len(bars) == 0 || bars[len(bars)-1].Close <= 0 {
		return fmt.Errorf("options runtime: underlying price required for Greek risk: %w", err)
	}
	balance, err := r.localPaperBroker.GetAccountBalance(ctx)
	if err != nil {
		return fmt.Errorf("options runtime: paper balance for Greek risk: %w", err)
	}
	positions, err := r.positionRepo.GetOpen(ctx, repository.PositionFilter{}, 1000, 0)
	if err != nil {
		return fmt.Errorf("options runtime: open positions for Greek risk: %w", err)
	}
	_, allowed, reason := risk.CheckOptionsExposure(risk.DefaultOptionsLimits(), balance.Equity, bars[len(bars)-1].Close, positions, proposed)
	if !allowed {
		return errors.New(reason)
	}
	return nil
}

func buildPaperDebitSpreadPlan(cfg *rules.OptionsRulesConfig, chain []domain.OptionSnapshot, now time.Time) (*domain.OptionSpread, float64, error) {
	if cfg == nil || len(cfg.LegSelection) != 2 {
		return nil, 0, errors.New("options runtime: paper debit vertical requires exactly two legs")
	}
	selected, err := rules.SelectSpreadLegs(chain, cfg.LegSelection, now)
	if err != nil {
		return nil, 0, fmt.Errorf("options runtime: select spread legs: %w", err)
	}
	spread, err := rules.BuildSpread(cfg.StrategyType, cfg.Underlying, selected, cfg.LegSelection)
	if err != nil {
		return nil, 0, fmt.Errorf("options runtime: build spread: %w", err)
	}
	var netDebit, minStrike, maxStrike float64
	minStrike = math.Inf(1)
	for index := range spread.Legs {
		leg := &spread.Legs[index]
		snapshot := selectedLegSnapshot(selected, leg.Contract.OCCSymbol)
		if snapshot == nil || snapshot.Bid <= 0 || snapshot.Ask <= 0 || snapshot.Ask < snapshot.Bid {
			return nil, 0, errors.New("options runtime: every spread leg requires a valid executable bid/ask")
		}
		if leg.Side == domain.OrderSideBuy {
			leg.ExecutablePrice = snapshot.Ask
			netDebit += snapshot.Ask * float64(leg.Ratio)
		} else {
			leg.ExecutablePrice = snapshot.Bid
			netDebit -= snapshot.Bid * float64(leg.Ratio)
		}
		leg.Greeks = snapshot.Greeks
		minStrike = math.Min(minStrike, leg.Contract.Strike)
		maxStrike = math.Max(maxStrike, leg.Contract.Strike)
	}
	if netDebit <= 0 || len(spread.Legs) == 0 {
		return nil, 0, errors.New("options runtime: only net-debit verticals are enabled")
	}
	multiplier := spread.Legs[0].Contract.Multiplier
	spread.MaxRisk = netDebit * multiplier
	spread.MaxReward = ((maxStrike - minStrike) - netDebit) * multiplier
	if spread.MaxReward <= 0 {
		return nil, 0, errors.New("options runtime: debit vertical has no finite positive max reward")
	}
	quantity := 0.0
	switch cfg.PositionSizing.Method {
	case "fixed_contracts":
		quantity = float64(cfg.PositionSizing.FixedContracts)
	case "premium_budget":
		quantity = math.Floor(cfg.PositionSizing.PremiumBudget / spread.MaxRisk)
	case "max_risk":
		quantity = math.Floor(cfg.PositionSizing.MaxRiskUSD / spread.MaxRisk)
	}
	if quantity < 1 {
		return nil, 0, errors.New("options runtime: sizing budget cannot purchase one spread")
	}
	return spread, quantity, nil
}

func selectedLegSnapshot(selected map[string]*domain.OptionSnapshot, symbol string) *domain.OptionSnapshot {
	for _, snapshot := range selected {
		if snapshot != nil && snapshot.Contract.OCCSymbol == symbol {
			return snapshot
		}
	}
	return nil
}

func executableOptionClosePrice(position *domain.Position, chain []domain.OptionSnapshot) (float64, error) {
	if position == nil {
		return 0, errors.New("options runtime: position is required")
	}
	for _, snapshot := range chain {
		if snapshot.Contract.OCCSymbol != position.Ticker {
			continue
		}
		price := snapshot.Bid
		if position.Side == domain.PositionSideShort {
			price = snapshot.Ask
		}
		if price <= 0 || snapshot.Ask <= 0 || snapshot.Bid <= 0 || snapshot.Ask < snapshot.Bid {
			return 0, errors.New("options runtime: valid executable close bid/ask is required")
		}
		return price, nil
	}
	return 0, fmt.Errorf("options runtime: close quote for %s not found", position.Ticker)
}

func buildPaperSingleLegPlan(cfg *rules.OptionsRulesConfig, chain []domain.OptionSnapshot, now time.Time) (execution.TradingPlan, error) {
	if cfg == nil || len(cfg.LegSelection) != 1 {
		return execution.TradingPlan{}, errors.New("options runtime: only atomic single-leg plans are currently executable")
	}
	var selector rules.LegSelector
	for _, value := range cfg.LegSelection {
		selector = value
	}
	if selector.Side != domain.OrderSideBuy || selector.Intent != domain.PositionIntentBuyToOpen {
		return execution.TradingPlan{}, errors.New("options runtime: uncovered or non-opening short options are disabled")
	}
	snapshot, err := rules.SelectLeg(chain, selector, now)
	if err != nil {
		return execution.TradingPlan{}, fmt.Errorf("options runtime: select contract: %w", err)
	}
	if snapshot.Ask <= 0 || snapshot.Bid <= 0 || snapshot.Ask < snapshot.Bid {
		return execution.TradingPlan{}, errors.New("options runtime: valid executable bid/ask is required")
	}
	quantity := 0.0
	switch cfg.PositionSizing.Method {
	case "fixed_contracts":
		quantity = float64(cfg.PositionSizing.FixedContracts)
	case "premium_budget":
		quantity = float64(int(cfg.PositionSizing.PremiumBudget / (snapshot.Ask * snapshot.Contract.Multiplier)))
	case "max_risk":
		quantity = float64(int(cfg.PositionSizing.MaxRiskUSD / (snapshot.Ask * snapshot.Contract.Multiplier)))
	}
	if quantity < 1 {
		return execution.TradingPlan{}, errors.New("options runtime: sizing budget cannot purchase one contract")
	}
	greeks := snapshot.Greeks
	return execution.TradingPlan{Action: domain.PipelineSignalBuy, MarketType: domain.MarketTypeOptions, Ticker: snapshot.Contract.OCCSymbol, EntryType: "limit", EntryPrice: snapshot.Ask, PositionSize: quantity, OptionGreeks: &greeks}, nil
}

func (r *realStrategyRunner) runPolymarketNative(ctx context.Context, strategy domain.Strategy) (*api.StrategyRunResult, error) {
	executionStrategy := r.effectivePolymarketExecutionStrategy(strategy)

	strategyConfig, err := parseStrategyConfig(strategy.Config)
	if err != nil {
		return nil, err
	}
	resolved := agent.ResolveConfig(strategyConfig, r.globals)
	now := time.Now().UTC()
	run := domain.PipelineRun{
		ID:             uuid.New(),
		StrategyID:     strategy.ID,
		Ticker:         strategy.Ticker,
		TradeDate:      now,
		Status:         domain.PipelineStatusRunning,
		StartedAt:      now,
		ConfigSnapshot: strategy.Config,
	}
	if err := r.startNativeRun(ctx, "polymarket", &run); err != nil {
		return nil, err
	}

	completeRun := func(status domain.PipelineStatus, signal domain.PipelineSignal, errMsg string) error {
		return r.completeNativeRun(ctx, "polymarket", &run, status, signal, errMsg)
	}
	failRun := func(err error) (*api.StrategyRunResult, error) {
		if updateErr := completeRun(domain.PipelineStatusFailed, domain.PipelineSignalHold, err.Error()); updateErr != nil {
			err = errors.Join(err, updateErr)
		}
		return nil, err
	}

	if r.polymarketMarketData == nil {
		return failRun(errors.New("polymarket native: market data client is required"))
	}
	marketData, err := r.polymarketMarketData.GetMarketData(ctx, strategy.Ticker)
	if err != nil {
		return failRun(fmt.Errorf("polymarket native: fetch market data for %s: %w", strategy.Ticker, err))
	}
	snapshot := polymarketexecution.SnapshotFromPredictionMarketData(marketData, time.Now().UTC())
	if err := r.persistPolymarketNativeSnapshot(ctx, run.ID, snapshot); err != nil {
		return failRun(err)
	}

	executor := polymarketexecution.NewDeterministicNativeExecutor()
	decision, err := executor.Execute(ctx, strategy, snapshot)
	if err != nil {
		return failRun(fmt.Errorf("polymarket native: execute strategy %s: %w", strategy.Name, err))
	}
	signal := decision.Signal
	if signal == "" {
		signal = domain.PipelineSignalHold
	}
	if signal == domain.PipelineSignalBuy {
		sizingConfig := applyPolymarketSizingCap(strategy.MarketType, sizingConfigForStrategy(ctx, strategy, strategyConfig, resolved, r.positionRepo, r.logger), r.cfg.Risk.Polymarket.MaxPositionUSDC)
		plannedNotional, err := r.plannedPolymarketNotional(ctx, executionStrategy, sizingConfig, decision)
		if err != nil {
			return failRun(err)
		}
		if err := r.checkPolymarketNativePreconditions(snapshot, decision, plannedNotional); err != nil {
			return failRun(err)
		}
	}

	orderManager, err := r.newOrderManager(ctx, executionStrategy, resolved, strategyConfig)
	if err != nil {
		return failRun(err)
	}
	finalSignal := execution.FinalSignal{Signal: signal, Confidence: decision.Confidence}
	tradingPlan := execution.TradingPlan{
		Action:           signal,
		MarketType:       domain.MarketTypePolymarket,
		Ticker:           strategy.Ticker,
		EntryType:        decision.EntryType,
		EntryPrice:       decision.EntryPrice,
		StopLoss:         decision.StopLoss,
		TakeProfit:       decision.TakeProfit,
		TimeHorizon:      decision.TimeHorizon,
		Confidence:       decision.Confidence,
		Rationale:        decision.Rationale,
		RiskReward:       decision.RiskReward,
		Side:             decision.Side,
		ExternalMarketID: snapshot.ConditionID,
		FairValue:        decision.FairProbability, Spread: decision.Spread, Depth: decision.Depth,
		GrossEV: decision.GrossEdge, NetEV: decision.NetEdge,
		Evidence:   predictionNativeEvidence("polymarket", snapshot, decision),
		Features:   predictionNativeFeatures(decision),
		RegimeTags: []string{"event_market", "deterministic_execution", decision.Template},
	}
	r.recordPortfolioOpportunity(ctx, strategy, &run.ID, finalSignal, tradingPlan)
	if !r.portfolioAllocatorOwnsPaperExecution(strategy, signal) {
		if err := orderManager.ProcessSignal(ctx, finalSignal, tradingPlan, strategy.ID, run.ID); err != nil {
			return failRun(err)
		}
	}

	if err := completeRun(domain.PipelineStatusCompleted, signal, ""); err != nil {
		return nil, err
	}
	orders, err := r.orderRepo.GetByRun(ctx, run.ID, repository.OrderFilter{}, 10, 0)
	if err != nil {
		return nil, err
	}
	positions, err := r.positionRepo.GetByStrategy(ctx, strategy.ID, repository.PositionFilter{}, 10, 0)
	if err != nil {
		return nil, err
	}
	if !executionStrategy.IsPaper {
		if err := r.registerPolymarketPositions(positions); err != nil && r.logger != nil {
			r.logger.WarnContext(ctx, "polymarket stop guard registration failed", "error", err, "strategy_id", strategy.ID)
		}
	}
	return &api.StrategyRunResult{Run: run, Signal: signal, Orders: orders, Positions: positions}, nil
}

func (r *realStrategyRunner) runKalshiNative(ctx context.Context, strategy domain.Strategy) (*api.StrategyRunResult, error) {
	now := time.Now().UTC()
	run := domain.PipelineRun{
		ID:             uuid.New(),
		StrategyID:     strategy.ID,
		Ticker:         strategy.Ticker,
		TradeDate:      now,
		Status:         domain.PipelineStatusRunning,
		StartedAt:      now,
		ConfigSnapshot: strategy.Config,
	}
	if err := r.startNativeRun(ctx, "kalshi", &run); err != nil {
		return nil, err
	}

	completeRun := func(status domain.PipelineStatus, signal domain.PipelineSignal, errMsg string) error {
		return r.completeNativeRun(ctx, "kalshi", &run, status, signal, errMsg)
	}
	failRun := func(err error) (*api.StrategyRunResult, error) {
		if updateErr := completeRun(domain.PipelineStatusFailed, domain.PipelineSignalHold, err.Error()); updateErr != nil {
			err = errors.Join(err, updateErr)
		}
		return nil, err
	}

	if r.kalshiMarketData == nil {
		r.kalshiMarketData = r.kalshiDataProvider
	}
	if r.kalshiMarketData == nil {
		return failRun(errors.New("kalshi native: market data client is required"))
	}
	if !strategy.IsPaper {
		gate, err := r.liveGateForStrategy(strategy)
		if err != nil {
			return failRun(err)
		}
		if allowed, denial := gate.Allows(&strategy.ID, brokerNameForStrategy(strategy)); !allowed {
			return failRun(fmt.Errorf("order_manager: live execution denied for kalshi: %s", denial.Message))
		}
		if _, _, err := r.newBrokerForStrategy(strategy); err != nil {
			return failRun(err)
		}
	}
	snapshot, err := r.kalshiMarketData.LoadSnapshot(ctx, strategy.Ticker)
	if err != nil {
		return failRun(fmt.Errorf("kalshi native: fetch snapshot for %s: %w", strategy.Ticker, err))
	}
	if err := r.persistKalshiNativeSnapshot(ctx, run.ID, snapshot); err != nil {
		return failRun(err)
	}

	var openPositions []domain.Position
	if r.cfg.Brokers.Kalshi.AutoExitsEnabled && r.positionRepo != nil {
		openPositions, err = r.positionRepo.GetByStrategy(ctx, strategy.ID, repository.PositionFilter{}, 10_000, 0)
		if err != nil {
			return failRun(fmt.Errorf("kalshi native: load positions for exit evaluation: %w", err))
		}
	}

	decision, exit := kalshiexecution.EvaluateExit(strategy, snapshot, openPositions, now)
	if !exit {
		decision, err = kalshiexecution.DeterministicNativeExecutor{}.Execute(ctx, strategy, snapshot)
	}
	if err != nil {
		return failRun(fmt.Errorf("kalshi native: execute strategy %s: %w", strategy.Name, err))
	}
	signal := decision.Signal
	if signal == "" {
		signal = domain.PipelineSignalHold
	}

	if signal == domain.PipelineSignalHold {
		if err := completeRun(domain.PipelineStatusCompleted, signal, ""); err != nil {
			return nil, err
		}
		return &api.StrategyRunResult{Run: run, Signal: signal}, nil
	}

	strategyConfig, err := parseStrategyConfig(strategy.Config)
	if err != nil {
		return failRun(fmt.Errorf("kalshi native: parse strategy config: %w", err))
	}
	resolved := agent.ResolveConfig(strategyConfig, r.globals)
	orderManager, err := r.newOrderManager(ctx, strategy, resolved, strategyConfig)
	if err != nil {
		return failRun(err)
	}
	finalSignal := execution.FinalSignal{Signal: signal, Confidence: decision.Confidence}
	tradingPlan := kalshiTradingPlan(signal, snapshot, decision, strategy.Ticker)
	r.recordPortfolioOpportunity(ctx, strategy, &run.ID, finalSignal, tradingPlan)
	if !r.portfolioAllocatorOwnsPaperExecution(strategy, signal) {
		if err := orderManager.ProcessSignal(ctx, finalSignal, tradingPlan, strategy.ID, run.ID); err != nil {
			return failRun(err)
		}
	}

	if err := completeRun(domain.PipelineStatusCompleted, signal, ""); err != nil {
		return nil, err
	}

	var orders []domain.Order
	if r.orderRepo != nil {
		orders, err = r.orderRepo.GetByRun(ctx, run.ID, repository.OrderFilter{}, 10, 0)
		if err != nil {
			return nil, err
		}
	}
	var positions []domain.Position
	if r.positionRepo != nil {
		positions, err = r.positionRepo.GetByStrategy(ctx, strategy.ID, repository.PositionFilter{}, 10, 0)
		if err != nil {
			return nil, err
		}
	}

	return &api.StrategyRunResult{Run: run, Signal: signal, Orders: orders, Positions: positions}, nil
}

func (r *realStrategyRunner) startNativeRun(ctx context.Context, source string, run *domain.PipelineRun) error {
	if run == nil {
		return fmt.Errorf("%s native: pipeline run is required", source)
	}
	if r.runRepo == nil {
		return fmt.Errorf("%s native: pipeline run repository is required", source)
	}
	if r.eventRepo == nil {
		return fmt.Errorf("%s native: agent event repository is required", source)
	}
	persistCtx, cancel := context.WithTimeout(ctx, nativeTerminalTimeout)
	if err := r.runRepo.Create(persistCtx, run); err != nil {
		cancel()
		return fmt.Errorf("%s native: create run: %w", source, err)
	}
	cancel()

	metadata, err := json.Marshal(map[string]string{"execution_path": source + "_native"})
	if err != nil {
		return fmt.Errorf("%s native: marshal start event: %w", source, err)
	}
	event := &domain.AgentEvent{
		PipelineRunID: &run.ID,
		StrategyID:    &run.StrategyID,
		EventKind:     agent.AgentEventKindPipelineStarted.String(),
		Title:         "Pipeline started",
		Summary:       "Native deterministic pipeline admitted for evaluation.",
		Tags:          []string{"pipeline", "native", source},
		Metadata:      metadata,
	}
	eventCtx, eventCancel := context.WithTimeout(ctx, nativeTerminalTimeout)
	eventErr := r.eventRepo.Create(eventCtx, event)
	eventCancel()
	if eventErr == nil {
		return nil
	}

	completedAt := time.Now().UTC()
	fallbackSignal := domain.PipelineSignalHold
	fallbackErr := fmt.Errorf("%s native: start audit event persistence failed", source)
	run.Status = domain.PipelineStatusFailed
	run.Signal = fallbackSignal
	run.CompletedAt = &completedAt
	run.ErrorMessage = fallbackErr.Error()
	update := repository.PipelineRunStatusUpdate{
		Status: domain.PipelineStatusFailed, Signal: &fallbackSignal, CompletedAt: &completedAt, ErrorMessage: fallbackErr.Error(),
	}
	updateCtx, updateCancel := context.WithTimeout(context.WithoutCancel(ctx), nativeTerminalTimeout)
	updateErr := r.runRepo.UpdateStatus(updateCtx, run.ID, run.TradeDate, update)
	updateCancel()
	if updateErr != nil {
		return errors.Join(
			fmt.Errorf("%s native: persist start event: %w", source, eventErr),
			fmt.Errorf("%s native: persist start-event failure status: %w", source, updateErr),
		)
	}
	return fmt.Errorf("%s native: persist start event: %w", source, eventErr)
}

func (r *realStrategyRunner) completeNativeRun(
	ctx context.Context,
	source string,
	run *domain.PipelineRun,
	status domain.PipelineStatus,
	signal domain.PipelineSignal,
	errMsg string,
) error {
	if run == nil {
		return fmt.Errorf("%s native: pipeline run is required", source)
	}
	if r.runRepo == nil {
		return fmt.Errorf("%s native: pipeline run repository is required", source)
	}

	completedAt := time.Now().UTC()
	run.Status = status
	run.Signal = signal
	run.CompletedAt = &completedAt
	run.ErrorMessage = errMsg
	update := repository.PipelineRunStatusUpdate{
		Status: status, Signal: &signal, CompletedAt: &completedAt, ErrorMessage: errMsg,
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), nativeTerminalTimeout)
	err := r.runRepo.UpdateStatus(persistCtx, run.ID, run.TradeDate, update)
	cancel()
	if err != nil {
		return fmt.Errorf("%s native: persist terminal run status: %w", source, err)
	}

	eventErr := r.persistNativeTerminalEvent(ctx, source, run, status, signal)
	if eventErr == nil {
		return nil
	}
	if status != domain.PipelineStatusCompleted {
		return eventErr
	}

	// A completed row without its terminal audit event is not auditable. Mark
	// it failed before returning so downstream execution cannot treat it as a
	// successful native pipeline.
	fallbackErr := fmt.Errorf("%s native: terminal audit event persistence failed", source)
	fallbackSignal := domain.PipelineSignalHold
	run.Status = domain.PipelineStatusFailed
	run.Signal = fallbackSignal
	run.ErrorMessage = fallbackErr.Error()
	fallback := repository.PipelineRunStatusUpdate{
		Status: domain.PipelineStatusFailed, Signal: &fallbackSignal, CompletedAt: &completedAt, ErrorMessage: fallbackErr.Error(),
	}
	fallbackCtx, fallbackCancel := context.WithTimeout(context.WithoutCancel(ctx), nativeTerminalTimeout)
	updateErr := r.runRepo.UpdateStatus(fallbackCtx, run.ID, run.TradeDate, fallback)
	fallbackCancel()
	if updateErr != nil {
		return errors.Join(eventErr, fmt.Errorf("%s native: persist terminal-event failure status: %w", source, updateErr))
	}
	return eventErr
}

func (r *realStrategyRunner) persistNativeTerminalEvent(
	ctx context.Context,
	source string,
	run *domain.PipelineRun,
	status domain.PipelineStatus,
	signal domain.PipelineSignal,
) error {
	if r.eventRepo == nil {
		return fmt.Errorf("%s native: agent event repository is required", source)
	}
	eventKind := agent.AgentEventKindPipelineFailed.String()
	title := "Pipeline failed"
	tags := []string{"pipeline", "failed", "native", source}
	if status == domain.PipelineStatusCompleted {
		eventKind = agent.AgentEventKindPipelineCompleted.String()
		title = "Pipeline completed"
		tags = []string{"pipeline", "completed", "native", source}
	}
	metadata, err := json.Marshal(map[string]string{
		"execution_path": source + "_native",
		"signal":         string(signal),
		"status":         string(status),
	})
	if err != nil {
		return fmt.Errorf("%s native: marshal terminal event: %w", source, err)
	}
	event := &domain.AgentEvent{
		PipelineRunID: &run.ID,
		StrategyID:    &run.StrategyID,
		EventKind:     eventKind,
		Title:         title,
		Summary:       "Native deterministic pipeline reached a terminal state.",
		Tags:          tags,
		Metadata:      metadata,
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), nativeTerminalTimeout)
	defer cancel()
	if err := r.eventRepo.Create(persistCtx, event); err != nil {
		return fmt.Errorf("%s native: persist terminal event: %w", source, err)
	}
	return nil
}

func predictionNativeEvidence(provider string, snapshot, decision any) json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		"provider": provider, "snapshot": snapshot, "decision": decision,
		"llm_role": "advisory_discovery_only", "execution_authority": "deterministic_native_gates",
	})
	if err != nil {
		return json.RawMessage(`{"error":"prediction evidence serialization failed"}`)
	}
	return raw
}

func kalshiTradingPlan(signal domain.PipelineSignal, snapshot kalshiexecution.Snapshot, decision kalshiexecution.NativeDecision, ticker string) execution.TradingPlan {
	tradingPlan := execution.TradingPlan{
		Action:           signal,
		MarketType:       domain.MarketTypeKalshi,
		Ticker:           ticker,
		EntryType:        decision.EntryType,
		EntryPrice:       decision.EntryPrice,
		PositionSize:     decision.PositionSize,
		Confidence:       decision.Confidence,
		Rationale:        decision.Rationale,
		RiskReward:       decision.RiskReward,
		Side:             decision.Side,
		TimeHorizon:      decision.TimeHorizon,
		ExternalMarketID: ticker,
		FairValue:        decision.FairProbability,
		Spread:           decision.Spread,
		Depth:            decision.Depth,
		GrossEV:          decision.GrossEdge,
		NetEV:            decision.NetEdge,
		Evidence:         predictionNativeEvidence("kalshi", snapshot, decision),
		Features:         predictionNativeFeatures(decision),
		RegimeTags:       []string{"event_market", "deterministic_execution", decision.Template},
	}
	if decision.EntryPrice > 0 {
		tradingPlan.ReferencePrice = decision.EntryPrice
	}
	return tradingPlan
}

func predictionNativeFeatures(decision any) json.RawMessage {
	raw, err := json.Marshal(decision)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func (r *realStrategyRunner) persistPolymarketNativeSnapshot(ctx context.Context, runID uuid.UUID, snapshot polymarketexecution.Snapshot) error {
	if r.snapshotRepo == nil {
		return errors.New("polymarket native: snapshot repository is required")
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("polymarket native: marshal snapshot: %w", err)
	}
	persistCtx, cancel := context.WithTimeout(ctx, nativeTerminalTimeout)
	defer cancel()
	if err := r.snapshotRepo.Create(persistCtx, &domain.PipelineRunSnapshot{ID: uuid.New(), PipelineRunID: runID, DataType: "polymarket_native_snapshot", Payload: payload, CreatedAt: time.Now().UTC()}); err != nil {
		return fmt.Errorf("polymarket native: persist snapshot: %w", err)
	}
	return nil
}

func (r *realStrategyRunner) persistKalshiNativeSnapshot(ctx context.Context, runID uuid.UUID, snapshot kalshiexecution.Snapshot) error {
	if r.snapshotRepo == nil {
		return errors.New("kalshi native: snapshot repository is required")
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("kalshi native: marshal snapshot: %w", err)
	}
	persistCtx, cancel := context.WithTimeout(ctx, nativeTerminalTimeout)
	defer cancel()
	if err := r.snapshotRepo.Create(persistCtx, &domain.PipelineRunSnapshot{ID: uuid.New(), PipelineRunID: runID, DataType: "kalshi_native_snapshot", Payload: payload, CreatedAt: time.Now().UTC()}); err != nil {
		return fmt.Errorf("kalshi native: persist snapshot: %w", err)
	}
	return nil
}

func (r *realStrategyRunner) checkPolymarketNativePreconditions(snapshot polymarketexecution.Snapshot, decision polymarketexecution.NativeDecision, plannedNotional float64) error {
	limits := r.cfg.Risk.Polymarket
	minLiquidity := limits.MinLiquidity
	if minLiquidity <= 0 {
		minLiquidity = 1000
	}
	now := time.Now().UTC()
	if err := snapshot.ValidateExecutableSide(decision.Side, minLiquidity, now); err != nil {
		return fmt.Errorf("polymarket native: %w", err)
	}
	bid, ask := snapshot.BidAskForSide(decision.Side)
	spread := ask - bid
	if spread <= 0 {
		return fmt.Errorf("polymarket native: executable %s bid/ask spread is required", decision.Side)
	}
	mid := ask - spread/2
	spreadMid := 0.0
	if mid > 0 {
		spreadMid = spread / mid
	}
	daysToResolution := int(snapshot.EndDate.Sub(now).Hours() / 24)
	if ok, reason := risk.CheckPolymarketPreConditions(risk.PolymarketLimits{
		MinLiquidity:        minLiquidity,
		MaxSpreadPct:        limits.MaxSpreadPct,
		MinDaysToResolution: limits.MinDaysToResolution,
		MaxPositionUSDC:     limits.MaxPositionUSDC,
	}, snapshot.Liquidity, spreadMid, daysToResolution, plannedNotional); !ok {
		return errors.New(reason)
	}
	return nil
}

func (r *realStrategyRunner) plannedPolymarketNotional(ctx context.Context, strategy domain.Strategy, sizingConfig execution.SizingConfig, decision polymarketexecution.NativeDecision) (float64, error) {
	if decision.EntryPrice <= 0 {
		return 0, nil
	}
	broker, _, err := r.newBrokerForStrategy(strategy)
	if err != nil {
		return 0, err
	}
	if broker == nil {
		return 0, errors.New("polymarket native: broker is required to estimate position size")
	}
	balance, err := broker.GetAccountBalance(ctx)
	if err != nil {
		return 0, fmt.Errorf("polymarket native: get account balance: %w", err)
	}
	quantity := execution.PolymarketPositionSize(execution.PolymarketSizingParams{
		AccountValue:    balance.Equity,
		FractionPct:     sizingConfig.FractionPct,
		MaxPositionUSDC: sizingConfig.MaxPositionUSDC,
		EntryPrice:      decision.EntryPrice,
	})
	return quantity * decision.EntryPrice, nil
}

func (r *realStrategyRunner) effectivePolymarketExecutionStrategy(strategy domain.Strategy) domain.Strategy {
	effective := strategy
	if strategy.IsPaper || r == nil || !r.cfg.Features.EnableLiveTrading {
		effective.IsPaper = true
		return effective
	}
	gate, err := r.liveGateForStrategy(strategy)
	if err != nil {
		effective.IsPaper = true
		return effective
	}
	if allowed, _ := gate.Allows(&strategy.ID, "polymarket"); !allowed {
		effective.IsPaper = true
	}
	return effective
}

func (r *realStrategyRunner) executionDecisionMetadata(ctx context.Context, runID uuid.UUID) *execution.DecisionMetadata {
	return executionDecisionMetadata(ctx, r.decisionRepo, r.logger, runID)
}

func executionDecisionMetadata(ctx context.Context, decisionRepo repository.AgentDecisionRepository, logger *slog.Logger, runID uuid.UUID) *execution.DecisionMetadata {
	if decisionRepo == nil || runID == uuid.Nil {
		return nil
	}

	decisions, err := decisionRepo.GetByRun(ctx, runID, repository.AgentDecisionFilter{
		AgentRole: domain.AgentRoleTrader,
		Phase:     domain.PhaseTrading,
	}, 1, 0)
	if err != nil || len(decisions) == 0 {
		if err != nil && logger != nil {
			logger.WarnContext(ctx, "load trader decision metadata", "error", err, "run_id", runID)
		}
		return nil
	}

	decision := decisions[0]
	hasLLMProvenance := strings.TrimSpace(decision.PromptText) != "" ||
		strings.TrimSpace(decision.LLMProvider) != "" ||
		strings.TrimSpace(decision.LLMModel) != "" ||
		decision.PromptTokens > 0 || decision.CompletionTokens > 0 || decision.LatencyMS > 0
	if !hasLLMProvenance {
		return nil
	}
	metadata := &execution.DecisionMetadata{
		PromptText:  decision.PromptText,
		LLMProvider: decision.LLMProvider,
		LLMModel:    decision.LLMModel,
	}
	if decision.PromptTokens > 0 {
		value := decision.PromptTokens
		metadata.PromptTokens = &value
	}
	if decision.CompletionTokens > 0 {
		value := decision.CompletionTokens
		metadata.CompletionTokens = &value
	}
	if decision.LatencyMS > 0 {
		value := decision.LatencyMS
		metadata.LatencyMS = &value
	}
	value := decision.CostUSD
	metadata.CostUSD = &value

	return metadata
}

func (r *realStrategyRunner) registerPolymarketPositions(positions []domain.Position) error {
	if r == nil || r.polymarketStopGuard == nil {
		return nil
	}
	var firstErr error
	for _, position := range positions {
		if err := r.registerPolymarketPosition(position); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *realStrategyRunner) registerPolymarketPosition(position domain.Position) error {
	if r == nil || r.polymarketStopGuard == nil {
		return nil
	}
	if position.ClosedAt != nil || position.Quantity <= 0 {
		return nil
	}
	if position.StopLoss == nil && position.TakeProfit == nil {
		return nil
	}
	if err := r.polymarketStopGuard.RegisterPosition(position); err != nil {
		return err
	}
	slug, err := polymarketPositionSlugFromTicker(position.Ticker)
	if err != nil {
		return err
	}
	r.ensurePolymarketTickWorker(slug)
	return nil
}

func (r *realStrategyRunner) ensurePolymarketTickWorker(slug string) {
	if r == nil || r.polymarketStopGuard == nil || r.polymarketFeed == nil || r.polymarketWorkerCtx == nil {
		return
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return
	}
	if _, loaded := r.polymarketWorkers.LoadOrStore(slug, struct{}{}); loaded {
		return
	}
	r.polymarketWorkerWG.Add(1)
	go func() {
		defer r.polymarketWorkerWG.Done()
		ticks := r.polymarketFeed.Ticks(slug)
		for {
			select {
			case <-r.polymarketWorkerCtx.Done():
				return
			case tick, ok := <-ticks:
				if !ok {
					return
				}
				r.polymarketStopGuard.OnTick(r.polymarketWorkerCtx, tick)
			}
		}
	}()
}

func (r *realStrategyRunner) stopPolymarketTickWorkers() {
	if r == nil || r.polymarketWorkerStop == nil {
		return
	}
	r.polymarketWorkerStop()
	r.polymarketWorkerWG.Wait()
}

func polymarketPositionSlugFromTicker(ticker string) (string, error) {
	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return "", errors.New("polymarket: ticker is required")
	}
	slug, _, found := strings.Cut(ticker, ":")
	if !found || strings.TrimSpace(slug) == "" {
		return "", fmt.Errorf("polymarket: ticker %q is not a polymarket position ticker", ticker)
	}
	return strings.TrimSpace(slug), nil
}

func normalizePolymarketStrategySide(side string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "YES":
		return "YES", nil
	case "NO":
		return "NO", nil
	case "UP":
		return "Up", nil
	case "DOWN":
		return "Down", nil
	case "OVER":
		return "Over", nil
	case "UNDER":
		return "Under", nil
	case "":
		return "", fmt.Errorf("trader did not specify Side (YES/NO/Up/Down/Over/Under)")
	default:
		return "", fmt.Errorf("invalid Side %q (want YES, NO, Up, Down, Over, or Under)", side)
	}
}

func (r *realStrategyRunner) recordStrategyPreparationFailure(ctx context.Context, strategy domain.Strategy, preparationErr error) error {
	if r.eventRepo == nil {
		return errors.New("record strategy preparation failure: agent event repository is required")
	}

	metadata, err := json.Marshal(map[string]string{
		"reason_code": strategyPreparationFailureReason(preparationErr),
	})
	if err != nil {
		return fmt.Errorf("record strategy preparation failure: marshal metadata: %w", err)
	}

	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	event := &domain.AgentEvent{
		StrategyID: &strategy.ID,
		EventKind:  "strategy.preparation_rejected",
		Title:      "Strategy preparation rejected",
		Summary:    "Required strategy inputs or runtime preparation did not pass preflight.",
		Tags:       []string{"strategy", "preflight", "rejected"},
		Metadata:   metadata,
	}
	if err := r.eventRepo.Create(persistCtx, event); err != nil {
		return fmt.Errorf("record strategy preparation failure: persist event: %w", err)
	}
	return nil
}

func strategyPreparationFailureReason(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "direct news coverage below threshold"):
		return "news_coverage_insufficient"
	case strings.Contains(message, "newest direct news article is older"):
		return "news_stale"
	case strings.Contains(message, "fundamentals completeness below threshold"):
		return "fundamentals_incomplete"
	case strings.Contains(message, "fundamentals unavailable"),
		strings.Contains(message, "fundamentals snapshot is older"),
		strings.Contains(message, "fundamentals ticker"):
		return "fundamentals_invalid"
	case strings.Contains(message, "daily market data stale"),
		strings.Contains(message, "latest daily bar has a non-positive close"):
		return "market_data_stale"
	case strings.Contains(message, "market bars unavailable"):
		return "market_data_unavailable"
	case strings.Contains(message, "social sentiment"):
		return "social_data_invalid"
	case strings.Contains(message, "build llm provider"):
		return "llm_provider_unavailable"
	default:
		return "preparation_failed"
	}
}

func (r *realStrategyRunner) prepareStrategyRun(ctx context.Context, strategy domain.Strategy) (*agent.Runner, agent.PreparedRun, *agent.StrategyConfig, chan agent.PipelineEvent, error) {
	strategyConfig, err := parseStrategyConfig(strategy.Config)
	if err != nil {
		return nil, agent.PreparedRun{}, nil, nil, err
	}

	globals := r.globals
	if r.promptOverrides != nil {
		globals.PromptOverrides = mergePromptOverrides(globals.PromptOverrides, r.promptOverrides.Overrides())
	}
	resolved := agent.ResolveConfig(strategyConfig, globals)
	provider, err := runtimeLLMComposer.BuildProviderForSelection(r.cfg.LLM, resolved.LLMConfig.Provider, resolved.LLMConfig.QuickThinkModel, r.logger)
	if err != nil {
		return nil, agent.PreparedRun{}, nil, nil, fmt.Errorf("build llm provider for strategy %s: %w", strategy.Name, err)
	}
	provider = runtimeLLMComposer.WrapProviderChain(provider, r.cfg.LLM, r.metrics, r.logger, r.llmBudget)

	definition, err := buildRunnerDefinition(provider, resolved.LLMConfig.Provider, resolved, r.cfg.LLM.Timeout, r.metrics, r.logger)
	if err != nil {
		return nil, agent.PreparedRun{}, nil, nil, err
	}

	var eventsCh chan agent.PipelineEvent
	if r.hub != nil {
		eventsCh = make(chan agent.PipelineEvent, 64)
	}
	runner := agent.NewRunner(definition, agent.Dependencies{
		Persister:   agent.NewRepoPersister(r.runRepo, r.snapshotRepo, r.decisionRepo, r.eventRepo, r.logger),
		Events:      eventsCh,
		Logger:      r.logger,
		RunRegistry: r.runRegistry,
	})

	prepared, err := runner.Prepare(strategy, r.globals)
	if err != nil {
		return nil, agent.PreparedRun{}, nil, nil, err
	}

	prepared.InitialState, err = r.loadInitialState(ctx, strategy, resolved)
	if err != nil {
		return nil, agent.PreparedRun{}, nil, nil, err
	}

	r.logger.Debug("prepareStrategyRun returning successfully")
	return runner, prepared, strategyConfig, eventsCh, nil
}

func (r *realStrategyRunner) loadInitialState(ctx context.Context, strategy domain.Strategy, resolved agent.ResolvedConfig) (agent.InitialStateSeed, error) {
	if r.dataService == nil {
		return agent.InitialStateSeed{}, errors.New("market data service is required")
	}

	to := time.Now().UTC()

	seed := agent.InitialStateSeed{}
	if usesStockOHLCVAnalysis(strategy) {
		from := to.Add(-strategyMarketLookback)
		bars, err := r.dataService.GetOHLCV(ctx, strategy.MarketType, strategy.Ticker, data.Timeframe1d, from, to)
		if err != nil {
			return agent.InitialStateSeed{}, fmt.Errorf("load ohlcv for %s: %w", strategy.Ticker, err)
		}
		if len(bars) == 0 {
			return agent.InitialStateSeed{}, fmt.Errorf("load ohlcv for %s: no bars returned", strategy.Ticker)
		}
		r.logger.Debug("loadInitialState after OHLCV", slog.Int("bars", len(bars)))
		seed.Market = &agent.MarketData{
			Bars:       bars,
			Indicators: data.IndicatorSnapshotFromBars(bars),
		}
	}

	if fundamentals, err := r.dataService.GetFundamentals(ctx, strategy.MarketType, strategy.Ticker); err == nil {
		seed.Fundamentals = &fundamentals
	} else if ctxErr := contextErr(err); ctxErr != nil {
		return agent.InitialStateSeed{}, ctxErr
	} else {
		r.logger.Warn("prod strategy runner: fundamentals unavailable",
			slog.String("ticker", strategy.Ticker),
			slog.Any("error", err),
		)
	}

	r.logger.Debug("loadInitialState after fundamentals")
	newsFrom := to.Add(-strategyNewsLookback)
	if articles, err := r.dataService.GetNews(ctx, strategy.MarketType, strategy.Ticker, newsFrom, to); err == nil {
		seed.News = data.RankRelevantNews(strategy.Ticker, articles, 10)
	} else if ctxErr := contextErr(err); ctxErr != nil {
		return agent.InitialStateSeed{}, ctxErr
	} else {
		r.logger.Warn("prod strategy runner: news unavailable",
			slog.String("ticker", strategy.Ticker),
			slog.Any("error", err),
		)
	}

	r.logger.Debug("loadInitialState after news")
	socialFrom := to.Add(-strategySocialLookback)
	if snapshots, err := r.dataService.GetSocialSentiment(ctx, strategy.MarketType, strategy.Ticker, socialFrom, to); err == nil {
		seed.Social = latestSocialSnapshot(snapshots)
		if seed.Social == nil {
			r.logger.Info("prod strategy runner: social sentiment empty for ticker",
				slog.String("ticker", strategy.Ticker),
			)
		}
	} else if ctxErr := contextErr(err); ctxErr != nil {
		return agent.InitialStateSeed{}, ctxErr
	} else {
		r.logger.Warn("prod strategy runner: social sentiment unavailable",
			slog.String("ticker", strategy.Ticker),
			slog.Any("error", err),
		)
	}

	r.logger.Debug("loadInitialState after social sentiment")
	if err := validateRequiredAnalysisInputs(strategy, resolved.RequiredAnalystRoles, seed, to); err != nil {
		return agent.InitialStateSeed{}, err
	}
	// Polymarket: load prediction market metadata for the market slug.
	if strategy.MarketType.Normalize() == domain.MarketTypePolymarket && r.polymarketClient != nil {
		pm, err := r.polymarketClient.GetMarketData(ctx, strategy.Ticker)
		if err != nil {
			r.logger.Warn("prod strategy runner: polymarket market data unavailable",
				slog.String("slug", strategy.Ticker),
				slog.Any("error", err),
			)
		} else {
			seed.PredictionMarket = pm
		}
	}

	r.logger.Debug("loadInitialState returning seed")
	return seed, nil
}

func validateRequiredAnalysisInputs(strategy domain.Strategy, required []agent.AgentRole, seed agent.InitialStateSeed, now time.Time) error {
	for _, role := range required {
		switch role {
		case agent.AgentRoleMarketAnalyst:
			if seed.Market == nil || len(seed.Market.Bars) == 0 {
				return fmt.Errorf("required analyst role %s: market bars unavailable", role)
			}
			if err := validateDailyBarFreshness(strategy.MarketType, now, seed.Market.Bars); err != nil {
				return fmt.Errorf("required analyst role %s: %w", role, err)
			}
		case agent.AgentRoleFundamentalsAnalyst:
			if err := validateFundamentalsInput(strategy.Ticker, seed.Fundamentals, now); err != nil {
				return fmt.Errorf("required analyst role %s: %w", role, err)
			}
		case agent.AgentRoleNewsAnalyst:
			if err := validateNewsInput(seed.News, now); err != nil {
				return fmt.Errorf("required analyst role %s: %w", role, err)
			}
		case agent.AgentRoleSocialMediaAnalyst:
			if seed.Social == nil || seed.Social.PostCount+seed.Social.CommentCount == 0 {
				return fmt.Errorf("required analyst role %s: social sentiment unavailable or empty", role)
			}
			if seed.Social.MeasuredAt.IsZero() || now.Sub(seed.Social.MeasuredAt) > 24*time.Hour {
				return fmt.Errorf("required analyst role %s: social sentiment is older than 24h", role)
			}
		}
	}
	return nil
}

func validateDailyBarFreshness(marketType domain.MarketType, now time.Time, bars []domain.OHLCV) error {
	if marketType.Normalize() != domain.MarketTypeStock || len(bars) == 0 {
		return nil
	}
	latest := bars[0]
	for _, bar := range bars[1:] {
		if bar.Timestamp.After(latest.Timestamp) {
			latest = bar
		}
	}
	if latest.Close <= 0 {
		return errors.New("latest daily bar has a non-positive close")
	}
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		return fmt.Errorf("load exchange timezone: %w", err)
	}
	localNow := now.In(et)
	refreshDeadline := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 16, 0, 0, 0, et).Add(postCloseDataGrace)
	if !scheduler.IsNYSETradingDay(now) || localNow.Before(refreshDeadline) {
		return nil
	}
	localBar := latest.Timestamp.In(et)
	if localBar.Year() != localNow.Year() || localBar.YearDay() != localNow.YearDay() {
		return fmt.Errorf("daily market data stale after 4:30 PM ET: latest bar %s", localBar.Format("2006-01-02"))
	}
	return nil
}

func validateFundamentalsInput(ticker string, fundamentals *data.Fundamentals, now time.Time) error {
	if fundamentals == nil {
		return errors.New("fundamentals unavailable")
	}
	if !strings.EqualFold(strings.TrimSpace(fundamentals.Ticker), strings.TrimSpace(ticker)) {
		return fmt.Errorf("fundamentals ticker %q does not match strategy ticker %q", fundamentals.Ticker, ticker)
	}
	if fundamentals.FetchedAt.IsZero() || now.Sub(fundamentals.FetchedAt) > 24*time.Hour {
		return errors.New("fundamentals snapshot is older than 24h")
	}
	coreMetrics := []struct {
		name  string
		value float64
	}{
		{name: data.FundamentalFieldMarketCap, value: fundamentals.MarketCap},
		{name: data.FundamentalFieldPERatio, value: fundamentals.PERatio},
		{name: data.FundamentalFieldRevenueGrowthYoY, value: fundamentals.RevenueGrowthYoY},
		{name: data.FundamentalFieldGrossMargin, value: fundamentals.GrossMargin},
		{name: data.FundamentalFieldDebtToEquity, value: fundamentals.DebtToEquity},
	}
	available := 0
	for _, metric := range coreMetrics {
		if math.IsNaN(metric.value) || math.IsInf(metric.value, 0) {
			continue
		}
		// Current providers explicitly identify missing fields, which lets valid
		// zero and negative observations count. Preserve compatibility with older
		// cached snapshots by treating zero as missing only when no field metadata
		// exists at all.
		if len(fundamentals.MissingFields) > 0 {
			if !data.IsFundamentalFieldMissing(*fundamentals, metric.name) {
				available++
			}
		} else if metric.value != 0 {
			available++
		}
	}
	if available < 3 {
		return fmt.Errorf("fundamentals completeness below threshold: %d of 5 core metrics available", available)
	}
	return nil
}

func validateNewsInput(articles []data.NewsArticle, now time.Time) error {
	direct := 0
	newest := time.Time{}
	for _, article := range articles {
		if article.Relevance < 0.85 {
			continue
		}
		direct++
		if article.PublishedAt.After(newest) {
			newest = article.PublishedAt
		}
	}
	if direct < requiredNewsMinDirect {
		return fmt.Errorf("direct news coverage below threshold: %d articles, require %d", direct, requiredNewsMinDirect)
	}
	if newest.IsZero() || now.Sub(newest) > requiredNewsMaxAge {
		return fmt.Errorf("newest direct news article is older than %s", requiredNewsMaxAge)
	}
	return nil
}

func usesStockOHLCVAnalysis(strategy domain.Strategy) bool {
	return !eventmarkets.IsEventMarket(strategy.MarketType) && strategy.MarketType.Normalize() != domain.MarketTypeOptions
}

func buildRunnerDefinition(provider llm.Provider, providerName string, resolved agent.ResolvedConfig, llmTimeout time.Duration, appMetrics *metrics.Metrics, logger *slog.Logger) (agent.Definition, error) {
	analysisAgents, err := buildAnalysisAgents(provider, providerName, resolved, appMetrics, logger)
	if err != nil {
		return agent.Definition{}, err
	}

	deepModel := strings.TrimSpace(resolved.LLMConfig.DeepThinkModel)
	quickModel := strings.TrimSpace(resolved.LLMConfig.QuickThinkModel)
	debateProvider := newDebateTimeoutFallbackProvider(provider, quickModel, effectiveDebateCallTimeout(llmTimeout, resolved), logger)

	return agent.Definition{
		Analysis: analysisAgents,
		Research: agent.ResearchDebateStage{
			Debaters: []agent.DebateAgent{
				agentdebate.NewBullResearcherWithPrompt(newLLMMetricsProvider(debateProvider, providerName, agent.AgentRoleBullResearcher.String(), appMetrics), providerName, deepModel, promptOverride(resolved.PromptOverrides, agent.AgentRoleBullResearcher, agentdebate.BullResearcherSystemPrompt), logger),
				agentdebate.NewBearResearcherWithPrompt(newLLMMetricsProvider(debateProvider, providerName, agent.AgentRoleBearResearcher.String(), appMetrics), providerName, deepModel, promptOverride(resolved.PromptOverrides, agent.AgentRoleBearResearcher, agentdebate.BearResearcherSystemPrompt), logger),
			},
			Judge: agentdebate.NewResearchManagerWithPrompt(newLLMMetricsProvider(debateProvider, providerName, agent.AgentRoleInvestJudge.String(), appMetrics), providerName, deepModel, promptOverride(resolved.PromptOverrides, agent.AgentRoleInvestJudge, agentdebate.ResearchManagerSystemPrompt), logger),
		},
		Trader: agenttrader.NewTraderWithPrompt(newLLMMetricsProvider(provider, providerName, agent.AgentRoleTrader.String(), appMetrics), providerName, deepModel, promptOverride(resolved.PromptOverrides, agent.AgentRoleTrader, agenttrader.TraderSystemPrompt), logger),
		Risk: agent.RiskDebateStage{
			Debaters: []agent.DebateAgent{
				agentrisk.NewAggressiveRiskWithPrompt(newLLMMetricsProvider(debateProvider, providerName, agent.AgentRoleAggressiveAnalyst.String(), appMetrics), providerName, deepModel, promptOverride(resolved.PromptOverrides, agent.AgentRoleAggressiveAnalyst, agentrisk.AggressiveRiskSystemPrompt), logger),
				agentrisk.NewConservativeRiskWithPrompt(newLLMMetricsProvider(debateProvider, providerName, agent.AgentRoleConservativeAnalyst.String(), appMetrics), providerName, deepModel, promptOverride(resolved.PromptOverrides, agent.AgentRoleConservativeAnalyst, agentrisk.ConservativeRiskSystemPrompt), logger),
				agentrisk.NewNeutralRiskWithPrompt(newLLMMetricsProvider(debateProvider, providerName, agent.AgentRoleNeutralAnalyst.String(), appMetrics), providerName, deepModel, promptOverride(resolved.PromptOverrides, agent.AgentRoleNeutralAnalyst, agentrisk.NeutralRiskSystemPrompt), logger),
			},
			Judge: agentrisk.NewRiskManagerWithPrompt(newLLMMetricsProvider(debateProvider, providerName, agent.AgentRoleRiskManager.String(), appMetrics), providerName, deepModel, promptOverride(resolved.PromptOverrides, agent.AgentRoleRiskManager, agentrisk.RiskManagerSystemPrompt), logger),
		},
	}, nil
}

func effectiveDebateCallTimeout(llmTimeout time.Duration, resolved agent.ResolvedConfig) time.Duration {
	callTimeout := llmTimeout
	if timeout := globalDebateCallTimeout(); timeout > 0 {
		callTimeout = timeout
	}

	roundTimeout := time.Duration(resolved.PipelineConfig.DebateTimeoutSeconds) * time.Second
	if roundTimeout <= 0 {
		return callTimeout
	}

	maximumCallTimeout := roundTimeout / 2
	if maximumCallTimeout <= 0 {
		return callTimeout
	}
	if callTimeout <= 0 || callTimeout > maximumCallTimeout {
		return maximumCallTimeout
	}
	return callTimeout
}

func globalDebateCallTimeout() time.Duration {
	if t := os.Getenv("LLM_DEBATE_TIMEOUT"); t != "" {
		if d, err := time.ParseDuration(t); err == nil {
			return d
		}
	}
	return 0
}

func promptOverride(overrides map[agent.AgentRole]string, role agent.AgentRole, fallback string) string {
	prompt := strings.TrimSpace(overrides[role])
	if prompt == "" {
		return fallback
	}
	return prompt
}

func mergePromptOverrides(base, overrides map[agent.AgentRole]string) map[agent.AgentRole]string {
	if len(base) == 0 && len(overrides) == 0 {
		return nil
	}
	merged := make(map[agent.AgentRole]string, len(base)+len(overrides))
	for role, prompt := range base {
		if strings.TrimSpace(prompt) != "" {
			merged[role] = prompt
		}
	}
	for role, prompt := range overrides {
		if strings.TrimSpace(prompt) != "" {
			merged[role] = prompt
		}
	}
	return merged
}

func buildAnalysisAgents(provider llm.Provider, providerName string, resolved agent.ResolvedConfig, appMetrics *metrics.Metrics, logger *slog.Logger) ([]agent.AnalysisAgent, error) {
	roles, err := selectedAnalysisRoles(resolved.AnalystSelection)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(resolved.LLMConfig.QuickThinkModel)
	agents := make([]agent.AnalysisAgent, 0, len(roles))
	for _, role := range roles {
		agentImpl, err := newAnalysisAgent(provider, providerName, model, role, resolved.PromptOverrides[role], appMetrics, logger)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agentImpl)
	}

	return agents, nil
}

func selectedAnalysisRoles(selection []agent.AgentRole) ([]agent.AgentRole, error) {
	if selection == nil {
		roles := make([]agent.AgentRole, len(defaultAnalysisRoles))
		copy(roles, defaultAnalysisRoles)
		return roles, nil
	}

	requested := make(map[agent.AgentRole]struct{}, len(selection))
	for _, role := range selection {
		if !isAnalysisRole(role) {
			return nil, fmt.Errorf("analyst_selection includes non-analysis role %q", role)
		}
		requested[role] = struct{}{}
	}

	roles := make([]agent.AgentRole, 0, len(requested))
	for _, role := range defaultAnalysisRoles {
		if _, ok := requested[role]; ok {
			roles = append(roles, role)
		}
	}
	if len(roles) == 0 {
		return nil, errors.New("analyst_selection must enable at least one analysis role")
	}

	return roles, nil
}

func isAnalysisRole(role agent.AgentRole) bool {
	switch role {
	case agent.AgentRoleMarketAnalyst,
		agent.AgentRoleFundamentalsAnalyst,
		agent.AgentRoleNewsAnalyst,
		agent.AgentRoleSocialMediaAnalyst:
		return true
	default:
		return false
	}
}

func newAnalysisAgent(provider llm.Provider, providerName, model string, role agent.AgentRole, promptOverride string, appMetrics *metrics.Metrics, logger *slog.Logger) (agent.AnalysisAgent, error) {
	if logger == nil {
		logger = slog.Default()
	}

	provider = newLLMMetricsProvider(provider, providerName, role.String(), appMetrics)

	prompt := strings.TrimSpace(promptOverride)
	switch role {
	case agent.AgentRoleMarketAnalyst:
		if prompt == "" {
			prompt = agentanalysts.MarketAnalystSystemPrompt
		}
		base := agentanalysts.NewBaseAnalyst(agentanalysts.BaseAnalystConfig{
			Provider:     provider,
			ProviderName: providerName,
			Model:        model,
			Logger:       logger,
			Role:         role,
			Name:         "market_analyst",
			SystemPrompt: prompt,
			BuildPrompt: func(input agent.AnalysisInput) (string, bool) {
				var bars []domain.OHLCV
				var indicators []domain.Indicator
				if input.Market != nil {
					bars = input.Market.Bars
					indicators = input.Market.Indicators
				}
				return agentanalysts.FormatMarketAnalystUserPrompt(input.Ticker, bars, indicators), true
			},
		})
		return &agentanalysts.MarketAnalyst{BaseAnalyst: base}, nil
	case agent.AgentRoleFundamentalsAnalyst:
		if prompt == "" {
			prompt = agentanalysts.FundamentalsAnalystSystemPrompt
		}
		base := agentanalysts.NewBaseAnalyst(agentanalysts.BaseAnalystConfig{
			Provider:     provider,
			ProviderName: providerName,
			Model:        model,
			Logger:       logger,
			Role:         role,
			Name:         "fundamentals_analyst",
			SystemPrompt: prompt,
			SkipMessage:  "No fundamentals available for this asset type.",
			BuildPrompt: func(input agent.AnalysisInput) (string, bool) {
				if input.Fundamentals == nil {
					return "", false
				}
				return agentanalysts.FormatFundamentalsAnalystUserPrompt(input.Ticker, input.Fundamentals), true
			},
		})
		return &agentanalysts.FundamentalsAnalyst{BaseAnalyst: base}, nil
	case agent.AgentRoleNewsAnalyst:
		if prompt == "" {
			prompt = agentanalysts.NewsAnalystSystemPrompt
		}
		base := agentanalysts.NewBaseAnalyst(agentanalysts.BaseAnalystConfig{
			Provider:     provider,
			ProviderName: providerName,
			Model:        model,
			Logger:       logger,
			Role:         role,
			Name:         "news_analyst",
			SystemPrompt: prompt,
			SkipMessage:  "No news articles available. Unable to perform news analysis.",
			BuildPrompt: func(input agent.AnalysisInput) (string, bool) {
				if len(input.News) == 0 {
					return "", false
				}
				return agentanalysts.FormatNewsAnalystUserPrompt(input.Ticker, input.News), true
			},
		})
		return &agentanalysts.NewsAnalyst{BaseAnalyst: base}, nil
	case agent.AgentRoleSocialMediaAnalyst:
		if prompt == "" {
			prompt = agentanalysts.SocialAnalystSystemPrompt
		}
		base := agentanalysts.NewBaseAnalyst(agentanalysts.BaseAnalystConfig{
			Provider:     provider,
			ProviderName: providerName,
			Model:        model,
			Logger:       logger,
			Role:         role,
			Name:         "social_media_analyst",
			SystemPrompt: prompt,
			SkipMessage:  "Social sentiment data unavailable for this ticker. Analysis skipped to conserve resources.",
			BuildPrompt: func(input agent.AnalysisInput) (string, bool) {
				return agentanalysts.FormatSocialAnalystUserPrompt(input.Ticker, input.Social), input.Social != nil
			},
		})
		return &agentanalysts.SocialMediaAnalyst{BaseAnalyst: base}, nil
	default:
		return nil, fmt.Errorf("unsupported analysis role %q", role)
	}
}

func parseStrategyConfig(raw domain.StrategyConfig) (*agent.StrategyConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var cfg agent.StrategyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse strategy config: %w", err)
	}
	if err := agent.ValidateStrategyConfig(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func globalSettingsFromConfig(cfg config.Config) agent.GlobalSettings {
	var llmConfig *agent.StrategyLLMConfig
	provider := strings.TrimSpace(cfg.LLM.DefaultProvider)
	deep := strings.TrimSpace(cfg.LLM.DeepThinkModel)
	quick := strings.TrimSpace(cfg.LLM.QuickThinkModel)
	if provider != "" || deep != "" || quick != "" {
		llmConfig = &agent.StrategyLLMConfig{}
		if provider != "" {
			llmConfig.Provider = &provider
		}
		if deep != "" {
			llmConfig.DeepThinkModel = &deep
		}
		if quick != "" {
			llmConfig.QuickThinkModel = &quick
		}
	}

	var riskConfig *agent.StrategyRiskConfig
	if cfg.Risk.MaxPositionSizePct > 0 {
		positionSizePct := cfg.Risk.MaxPositionSizePct * 100
		riskConfig = &agent.StrategyRiskConfig{PositionSizePct: &positionSizePct}
	}

	return agent.GlobalSettings{
		LLMConfig:  llmConfig,
		RiskConfig: riskConfig,
	}
}

func (r *realStrategyRunner) newOrderManager(ctx context.Context, strategy domain.Strategy, resolved agent.ResolvedConfig, strategyConfig *agent.StrategyConfig) (*execution.OrderManager, error) {
	gate, err := r.liveGateForStrategy(strategy)
	if err != nil {
		return nil, err
	}
	if !strategy.IsPaper {
		brokerName := brokerNameForStrategy(strategy)
		if allowed, denial := gate.Allows(&strategy.ID, brokerName); !allowed {
			return nil, fmt.Errorf("order_manager: live execution denied for %s: %s", brokerName, denial.Message)
		}
	}

	broker, brokerName, err := r.newBrokerForStrategy(strategy)
	if err != nil {
		return nil, err
	}
	r.setRiskPortfolioSnapshotSource(broker)

	return execution.NewOrderManager(
		broker,
		brokerName,
		r.riskEngine,
		r.positionRepo,
		r.orderRepo,
		r.tradeRepo,
		r.auditLogRepo,
		r.eventRepo,
		applyPolymarketSizingCap(strategy.MarketType, sizingConfigForStrategy(ctx, strategy, strategyConfig, resolved, r.positionRepo, r.logger), r.cfg.Risk.Polymarket.MaxPositionUSDC),
		r.logger,
	).WithMetrics(r.metrics).WithDecisionRecorder(r.tradeDecisionRecorder).WithLiveGate(gate).WithLiveTrading(!strategy.IsPaper).WithFinancialLifecycleRepo(func() repository.FinancialLifecycleRepository {
		if strategy.IsPaper {
			return r.financialRepo
		}
		return nil
	}()), nil
}

func (r *realStrategyRunner) recordPortfolioOpportunity(ctx context.Context, strategy domain.Strategy, runID *uuid.UUID, finalSignal execution.FinalSignal, plan execution.TradingPlan) {
	if r == nil || r.opportunityRepo == nil {
		return
	}
	if finalSignal.Signal != domain.PipelineSignalBuy && finalSignal.Signal != domain.PipelineSignalSell {
		return
	}
	maxLossPct := opportunityMaxLossPct(finalSignal.Signal, plan.EntryPrice, plan.StopLoss)
	proposedNotional := plan.PositionSize * plan.EntryPrice
	opportunity, reason, err := portfolio.BuildOpportunity(portfolio.OpportunityBuildInput{
		Strategy:          strategy,
		Run:               runFromID(runID),
		Signal:            finalSignal.Signal,
		PredictionSide:    plan.Side,
		Confidence:        firstPositive(finalSignal.Confidence, plan.Confidence),
		EdgePct:           positiveEdgeFromRiskReward(plan.RiskReward),
		ExpectedReturnPct: positiveEdgeFromRiskReward(plan.RiskReward),
		MaxLossPct:        maxLossPct,
		EntryPrice:        plan.EntryPrice,
		ProposedNotional:  proposedNotional,
		Reason:            plan.Rationale,
		Evidence:          opportunityEvidence(plan),
	}, portfolio.OpportunityBuilderConfig{})
	if err != nil {
		r.logger.WarnContext(ctx, "portfolio opportunity skipped", "strategy_id", strategy.ID, "ticker", strategy.Ticker, "reason", reason, "error", err)
		return
	}
	if opportunity == nil {
		return
	}
	if err := r.opportunityRepo.UpsertQueuedByDedupeKey(ctx, opportunity); err != nil {
		r.logger.WarnContext(ctx, "portfolio opportunity persist failed", "strategy_id", strategy.ID, "ticker", strategy.Ticker, "error", err)
	}
}

func (r *realStrategyRunner) portfolioAllocatorOwnsPaperExecution(strategy domain.Strategy, signal domain.PipelineSignal) bool {
	if r == nil || r.portfolioAllocatorMode != portfolio.AllocatorModePaper || !strategy.IsPaper {
		return false
	}
	return signal == domain.PipelineSignalBuy || signal == domain.PipelineSignalSell
}

func runFromID(id *uuid.UUID) *domain.PipelineRun {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	return &domain.PipelineRun{ID: *id}
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func positiveEdgeFromRiskReward(riskReward float64) float64 {
	if riskReward <= 0 {
		return 0
	}
	return riskReward / 100
}

func opportunityMaxLossPct(signal domain.PipelineSignal, entryPrice, stopLoss float64) float64 {
	if entryPrice <= 0 || stopLoss <= 0 {
		return 0
	}
	switch signal {
	case domain.PipelineSignalSell:
		if stopLoss <= entryPrice {
			return 0
		}
		return (stopLoss - entryPrice) / entryPrice
	default:
		if stopLoss >= entryPrice {
			return 0
		}
		return (entryPrice - stopLoss) / entryPrice
	}
}

func opportunityEvidence(plan execution.TradingPlan) json.RawMessage {
	payload, err := json.Marshal(map[string]any{
		"entry_type":   plan.EntryType,
		"entry_price":  plan.EntryPrice,
		"stop_loss":    plan.StopLoss,
		"take_profit":  plan.TakeProfit,
		"time_horizon": plan.TimeHorizon,
		"risk_reward":  plan.RiskReward,
		"side":         plan.Side,
	})
	if err != nil {
		return json.RawMessage("{}")
	}
	return payload
}

func (r *realStrategyRunner) liveGateForStrategy(strategy domain.Strategy) (execution.LiveGateConfig, error) {
	if r == nil || strategy.IsPaper || !r.cfg.Features.EnableLiveTrading {
		return execution.LiveGateConfig{}, nil
	}

	allowedStrategies := make(map[uuid.UUID]bool, len(r.cfg.LiveTradingAllowedStrategies))
	for _, raw := range r.cfg.LiveTradingAllowedStrategies {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		strategyID, err := uuid.Parse(raw)
		if err != nil {
			return execution.LiveGateConfig{}, fmt.Errorf("parse LIVE_TRADING_ALLOWED_STRATEGIES value %q: %w", raw, err)
		}
		allowedStrategies[strategyID] = true
	}

	allowedBrokers := make(map[string]bool, len(r.cfg.LiveTradingAllowedBrokers))
	for _, raw := range r.cfg.LiveTradingAllowedBrokers {
		broker := strings.ToLower(strings.TrimSpace(raw))
		if broker == "" {
			continue
		}
		allowedBrokers[broker] = true
	}

	return execution.LiveGateConfig{EnableLiveTrading: true, AllowedStrategies: allowedStrategies, AllowedBrokers: allowedBrokers}, nil
}

func (r *realStrategyRunner) recordPipelineMetrics(run domain.PipelineRun) {
	if r == nil || r.metrics == nil {
		return
	}
	signal := run.Signal.String()
	if signal == "" {
		signal = string(domain.PipelineSignalHold)
	}
	r.metrics.RecordPipelineRun(run.Ticker, signal, run.Status.String())
	if run.CompletedAt != nil {
		r.metrics.ObservePipelineDuration(run.Ticker, run.CompletedAt.Sub(run.StartedAt).Seconds())
	}
}

func (r *realStrategyRunner) refreshExecutionMetrics(ctx context.Context) {
	if r == nil || r.metrics == nil {
		return
	}
	if count, err := r.positionRepo.CountOpen(ctx, repository.PositionFilter{}); err == nil {
		r.metrics.SetPositionsOpen(float64(count))
	}
	if status, err := r.riskEngine.GetStatus(ctx); err == nil {
		r.metrics.SetCircuitBreakerState(status.CircuitBreaker.State == risk.CircuitBreakerPhaseTripped)
		r.metrics.SetKillSwitchActive(status.KillSwitch.Active)
	}
}

func (r *realStrategyRunner) newBrokerForStrategy(strategy domain.Strategy) (execution.Broker, string, error) {
	marketType := strategy.MarketType.Normalize()
	if strategy.IsPaper {
		switch marketType {
		case domain.MarketTypeStock:
			if hasBrokerCredentials(r.cfg.Brokers.Alpaca) && r.cfg.Brokers.Alpaca.PaperMode {
				return alpacaexecution.NewBroker(alpacaexecution.NewClient(
					r.cfg.Brokers.Alpaca.APIKey,
					r.cfg.Brokers.Alpaca.APISecret,
					true,
					r.logger,
				)), "alpaca", nil
			}
		case domain.MarketTypeCrypto:
			if hasBrokerCredentials(r.cfg.Brokers.Binance) && r.cfg.Brokers.Binance.PaperMode {
				return binanceexecution.NewBroker(binanceexecution.NewClient(
					r.cfg.Brokers.Binance.APIKey,
					r.cfg.Brokers.Binance.APISecret,
					true,
					r.logger,
				)), "binance", nil
			}
		case domain.MarketTypePolymarket:
			// Polymarket has no separate paper-trading mode; use local paper broker.
		}

		return r.fallbackPaperBroker(), "paper", nil
	}

	switch marketType {
	case domain.MarketTypeStock:
		if !r.cfg.Features.EnableLiveTrading {
			return nil, "", fmt.Errorf("live trading is disabled for strategy %s", strategy.Name)
		}
		if !hasBrokerCredentials(r.cfg.Brokers.Alpaca) {
			return nil, "", errors.New("alpaca broker credentials are required for live stock trading")
		}
		return alpacaexecution.NewBroker(alpacaexecution.NewClient(
			r.cfg.Brokers.Alpaca.APIKey,
			r.cfg.Brokers.Alpaca.APISecret,
			false,
			r.logger,
		)), "alpaca", nil
	case domain.MarketTypeCrypto:
		if !r.cfg.Features.EnableLiveTrading {
			return nil, "", fmt.Errorf("live trading is disabled for strategy %s", strategy.Name)
		}
		if !hasBrokerCredentials(r.cfg.Brokers.Binance) {
			return nil, "", errors.New("binance broker credentials are required for live crypto trading")
		}
		return binanceexecution.NewBroker(binanceexecution.NewClient(
			r.cfg.Brokers.Binance.APIKey,
			r.cfg.Brokers.Binance.APISecret,
			false,
			r.logger,
		)), "binance", nil
	case domain.MarketTypePolymarket:
		if !r.cfg.Features.EnableLiveTrading {
			return nil, "", fmt.Errorf("live trading is disabled for strategy %s", strategy.Name)
		}
		pm := r.cfg.Brokers.Polymarket
		if strings.TrimSpace(pm.KeyID) == "" || strings.TrimSpace(pm.SecretKey) == "" {
			return nil, "", errors.New("polymarket credentials (POLYMARKET_KEY_ID and POLYMARKET_SECRET_KEY) are required for live polymarket trading")
		}
		if r.polymarketClient == nil {
			return nil, "", errors.New("polymarket client not initialised")
		}
		return polymarketexecution.NewBroker(r.polymarketClient), "polymarket", nil
	case domain.MarketTypeKalshi:
		if !r.cfg.Features.EnableLiveTrading {
			return nil, "", fmt.Errorf("live trading is disabled for strategy %s", strategy.Name)
		}
		kc := r.cfg.Brokers.Kalshi
		if strings.TrimSpace(kc.APIKeyID) == "" || strings.TrimSpace(kc.PrivateKeyPEMB64) == "" {
			return nil, "", errors.New("kalshi credentials (KALSHI_API_KEY_ID and KALSHI_PRIVATE_KEY_PEM_B64) are required for live kalshi trading")
		}
		if r.kalshiLiveClient == nil {
			return nil, "", errors.New("kalshi live client is not initialised")
		}
		return kalshiexecution.NewBroker(r.kalshiLiveClient), "kalshi", nil
	default:
		return nil, "", fmt.Errorf("live trading is not supported for market type %q", strategy.MarketType)
	}
}

func brokerNameForStrategy(strategy domain.Strategy) string {
	switch strategy.MarketType.Normalize() {
	case domain.MarketTypeStock:
		return "alpaca"
	case domain.MarketTypeCrypto:
		return "binance"
	case domain.MarketTypePolymarket:
		return "polymarket"
	case domain.MarketTypeKalshi:
		return "kalshi"
	default:
		return ""
	}
}

func (r *realStrategyRunner) fallbackPaperBroker() *paper.PaperBroker {
	if r == nil {
		return paper.NewPaperBroker(localPaperBuyingPower, 0, 0)
	}

	r.localPaperMu.Lock()
	defer r.localPaperMu.Unlock()

	if r.localPaperBroker == nil {
		r.localPaperBroker = paper.NewPaperBroker(localPaperBuyingPower, 0, 0)
	}

	return r.localPaperBroker
}

func (r *realStrategyRunner) setRiskPortfolioSnapshotSource(broker execution.Broker) {
	if broker == nil || r == nil || r.positionRepo == nil {
		return
	}

	engineImpl, ok := r.riskEngine.(*risk.RiskEngineImpl)
	if !ok {
		return
	}

	engineImpl.SetPortfolioSnapshotFunc(func(ctx context.Context) (risk.Portfolio, error) {
		return execution.BuildRiskPortfolioSnapshot(ctx, broker, r.positionRepo)
	})
}

func hasBrokerCredentials(cfg config.BrokerConfig) bool {
	return strings.TrimSpace(cfg.APIKey) != "" && strings.TrimSpace(cfg.APISecret) != ""
}

func latestSocialSnapshot(snapshots []data.SocialSentiment) *data.SocialSentiment {
	if len(snapshots) == 0 {
		return nil
	}

	latest := snapshots[0]
	for _, snapshot := range snapshots[1:] {
		if snapshot.MeasuredAt.After(latest.MeasuredAt) {
			latest = snapshot
		}
	}

	return &latest
}

// drainPipelineEvents reads phase events emitted by the agent runner and
// broadcasts them to the WebSocket hub. It exits when the channel is closed.
func (r *realStrategyRunner) drainPipelineEvents(events <-chan agent.PipelineEvent) {
	for e := range events {
		msg := pipelineEventToWSMessage(e)
		if msg.Type == "" {
			continue // unmapped event type — skip
		}
		r.hub.Broadcast(msg)
	}
}

// pipelineEventToWSMessage converts an agent.PipelineEvent to an api.WSMessage
// using the event-type vocabulary defined in internal/api/hub.go.
func pipelineEventToWSMessage(e agent.PipelineEvent) api.WSMessage {
	switch e.Type {
	case agent.PipelineStarted:
		return api.WSMessage{
			Type:       api.EventPipelineStart,
			StrategyID: e.StrategyID,
			RunID:      e.PipelineRunID,
			Data:       map[string]any{"phase": e.Phase, "ticker": e.Ticker},
			Timestamp:  e.OccurredAt,
		}
	case agent.AgentDecisionMade:
		return api.WSMessage{
			Type:       api.EventAgentDecision,
			StrategyID: e.StrategyID,
			RunID:      e.PipelineRunID,
			Data:       map[string]any{"agent_role": e.AgentRole, "phase": e.Phase},
			Timestamp:  e.OccurredAt,
		}
	case agent.DebateRoundCompleted:
		return api.WSMessage{
			Type:       api.EventDebateRound,
			StrategyID: e.StrategyID,
			RunID:      e.PipelineRunID,
			Data:       map[string]any{"phase": e.Phase, "round": e.Round},
			Timestamp:  e.OccurredAt,
		}
	case agent.PipelineError:
		return api.WSMessage{
			Type:       api.EventError,
			StrategyID: e.StrategyID,
			RunID:      e.PipelineRunID,
			Data:       map[string]any{"error": e.Error, "timed_out": e.TimedOut, "used_fallback": e.UsedFallback},
			Timestamp:  e.OccurredAt,
		}
	case agent.PipelineCompleted:
		if !e.UsedFallback && !e.TimedOut {
			return api.WSMessage{}
		}
		return api.WSMessage{
			Type:       api.EventPipelineHealth,
			StrategyID: e.StrategyID,
			RunID:      e.PipelineRunID,
			Data:       map[string]any{"timed_out": e.TimedOut, "used_fallback": e.UsedFallback},
			Timestamp:  e.OccurredAt,
		}
	default:
		// LLMCacheStatsReported — no WS mapping needed.
		return api.WSMessage{}
	}
}

func contextErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func (r *realStrategyRunner) dispatchNotifications(ctx context.Context, strategy domain.Strategy, run *domain.PipelineRun, state *agent.PipelineState) error {
	if r.notificationManager == nil || run == nil || state == nil {
		return nil
	}

	signal := state.FinalSignal.Signal
	if signal == "" {
		signal = state.TradingPlan.Action
	}
	if signal == "" {
		signal = domain.PipelineSignalHold
	}

	occurredAt := time.Time{}
	if run.CompletedAt != nil {
		occurredAt = *run.CompletedAt
	}

	reasoning := state.TradingPlan.Rationale
	if reasoning == "" {
		reasoning = state.RiskDebate.FinalSignal
	}

	if err := r.notificationManager.RecordSignal(ctx, notification.SignalEvent{
		StrategyID:   strategy.ID,
		StrategyName: strategy.Name,
		RunID:        run.ID,
		Ticker:       strategy.Ticker,
		Signal:       signal,
		Confidence:   state.FinalSignal.Confidence,
		Reasoning:    reasoning,
		OccurredAt:   occurredAt,
	}); err != nil {
		return fmt.Errorf("dispatch signal notification: %w", err)
	}

	decisions, err := r.decisionRepo.GetByRun(ctx, run.ID, repository.AgentDecisionFilter{}, 100, 0)
	if err != nil {
		return fmt.Errorf("load run decisions: %w", err)
	}
	for _, decision := range decisions {
		if err := r.notificationManager.RecordDecision(ctx, notification.DecisionEvent{
			StrategyID:    strategy.ID,
			RunID:         run.ID,
			AgentRole:     decision.AgentRole,
			Phase:         decision.Phase,
			OutputSummary: decision.OutputText,
			LLMProvider:   decision.LLMProvider,
			LLMModel:      decision.LLMModel,
			LatencyMS:     decision.LatencyMS,
			OccurredAt:    decision.CreatedAt,
		}); err != nil {
			return fmt.Errorf("dispatch decision notification: %w", err)
		}
	}

	return nil
}

func (r *realStrategyRunner) findRun(ctx context.Context, runID uuid.UUID) (*domain.PipelineRun, error) {
	run, err := r.runRepo.GetByID(ctx, runID)
	if err == nil {
		return run, nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("run %s: %w", runID, repository.ErrNotFound)
	}
	return nil, err
}
