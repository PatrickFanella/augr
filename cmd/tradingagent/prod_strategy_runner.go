package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	agentanalysts "github.com/PatrickFanella/get-rich-quick/internal/agent/analysts"
	agentdebate "github.com/PatrickFanella/get-rich-quick/internal/agent/debate"
	agentrisk "github.com/PatrickFanella/get-rich-quick/internal/agent/risk"
	agenttrader "github.com/PatrickFanella/get-rich-quick/internal/agent/trader"
	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	alpacaexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/alpaca"
	binanceexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/binance"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	polymarketexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	"github.com/PatrickFanella/get-rich-quick/internal/notification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

const (
	strategyMarketLookback = 400 * 24 * time.Hour
	strategyNewsLookback   = 7 * 24 * time.Hour
	strategySocialLookback = 7 * 24 * time.Hour
	localPaperBuyingPower  = 100_000.0
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

type realStrategyRunner struct {
	cfg                 config.Config
	globals             agent.GlobalSettings
	dataService         marketDataService
	runRepo             repository.PipelineRunRepository
	snapshotRepo        repository.PipelineRunSnapshotRepository
	decisionRepo        repository.AgentDecisionRepository
	eventRepo           repository.AgentEventRepository
	orderRepo           repository.OrderRepository
	positionRepo        repository.PositionRepository
	tradeRepo           repository.TradeRepository
	auditLogRepo        repository.AuditLogRepository
	riskEngine          risk.RiskEngine
	metrics             *metrics.Metrics
	notificationManager *notification.Manager
	runRegistry         *agent.RunContextRegistry
	logger              *slog.Logger
	localPaperMu        sync.Mutex
	localPaperBroker    *paper.PaperBroker
	polymarketClient    *polymarketexecution.Client // nil if not configured
	hub                 *api.Hub                    // nil until wired; optional WebSocket broadcast
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
	riskEngine risk.RiskEngine,
	appMetrics *metrics.Metrics,
	notificationManager *notification.Manager,
	runRegistry *agent.RunContextRegistry,
	logger *slog.Logger,
) *realStrategyRunner {
	if logger == nil {
		logger = slog.Default()
	}

	runner := &realStrategyRunner{
		cfg:                 cfg,
		globals:             globalSettingsFromConfig(cfg),
		dataService:         dataService,
		runRepo:             runRepo,
		snapshotRepo:        snapshotRepo,
		decisionRepo:        decisionRepo,
		eventRepo:           eventRepo,
		orderRepo:           orderRepo,
		positionRepo:        positionRepo,
		tradeRepo:           tradeRepo,
		auditLogRepo:        auditLogRepo,
		riskEngine:          riskEngine,
		metrics:             appMetrics,
		notificationManager: notificationManager,
		runRegistry:         runRegistry,
		logger:              logger,
		localPaperBroker:    paper.NewPaperBroker(localPaperBuyingPower, 0, 0),
	}

	// Wire Polymarket client if credentials are configured.
	pm := cfg.Brokers.Polymarket
	if strings.TrimSpace(pm.KeyID) != "" {
		client := polymarketexecution.NewClient(pm.KeyID, pm.SecretKey, logger)
		client.SetAPIBaseURL(pm.APIBaseURL)
		client.SetGatewayBaseURL(pm.GatewayBaseURL)
		runner.polymarketClient = client
	}

	return runner
}

func (r *realStrategyRunner) RunStrategy(ctx context.Context, strategy domain.Strategy) (*api.StrategyRunResult, error) {
	runner, prepared, strategyConfig, eventsCh, err := r.prepareStrategyRun(ctx, strategy)
	if err != nil {
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

	state := agent.PipelineStateFromView(result.State)
	if err := r.dispatchNotifications(ctx, strategy, run, state); err != nil {
		return nil, err
	}

	orderManager, err := r.newOrderManager(strategy, prepared.Config)
	if err != nil {
		return nil, err
	}

	planTicker := result.State.TradingPlan.Ticker
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

	if err := orderManager.ProcessSignal(
		ctx,
		execution.FinalSignal{
			Signal:     signal,
			Confidence: result.State.FinalSignal.Confidence,
		},
		execution.TradingPlan{
			Action:       signal,
			Ticker:       planTicker,
			EntryType:    result.State.TradingPlan.EntryType,
			EntryPrice:   result.State.TradingPlan.EntryPrice,
			PositionSize: result.State.TradingPlan.PositionSize,
			StopLoss:     result.State.TradingPlan.StopLoss,
			TakeProfit:   result.State.TradingPlan.TakeProfit,
			TimeHorizon:  result.State.TradingPlan.TimeHorizon,
			Confidence:   result.State.TradingPlan.Confidence,
			Rationale:    result.State.TradingPlan.Rationale,
			RiskReward:   result.State.TradingPlan.RiskReward,
			Side:         result.State.TradingPlan.Side,
		},
		strategy.ID,
		run.ID,
	); err != nil {
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

	return &api.StrategyRunResult{
		Run:       *run,
		Signal:    signal,
		Orders:    orders,
		Positions: positions,
	}, nil
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

func (r *realStrategyRunner) prepareStrategyRun(ctx context.Context, strategy domain.Strategy) (*agent.Runner, agent.PreparedRun, *agent.StrategyConfig, chan agent.PipelineEvent, error) {
	strategyConfig, err := parseStrategyConfig(strategy.Config)
	if err != nil {
		return nil, agent.PreparedRun{}, nil, nil, err
	}

	resolved := agent.ResolveConfig(strategyConfig, r.globals)
	provider, err := newLLMProviderForSelection(r.cfg.LLM, resolved.LLMConfig.Provider, resolved.LLMConfig.QuickThinkModel, r.logger)
	if err != nil {
		return nil, agent.PreparedRun{}, nil, nil, fmt.Errorf("build llm provider for strategy %s: %w", strategy.Name, err)
	}

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

	prepared.InitialState, err = r.loadInitialState(ctx, strategy)
	if err != nil {
		return nil, agent.PreparedRun{}, nil, nil, err
	}

	return runner, prepared, strategyConfig, eventsCh, nil
}

func (r *realStrategyRunner) loadInitialState(ctx context.Context, strategy domain.Strategy) (agent.InitialStateSeed, error) {
	if r.dataService == nil {
		return agent.InitialStateSeed{}, errors.New("market data service is required")
	}

	to := time.Now().UTC()
	from := to.Add(-strategyMarketLookback)
	bars, err := r.dataService.GetOHLCV(ctx, strategy.MarketType, strategy.Ticker, data.Timeframe1d, from, to)
	if err != nil {
		return agent.InitialStateSeed{}, fmt.Errorf("load ohlcv for %s: %w", strategy.Ticker, err)
	}
	if len(bars) == 0 {
		return agent.InitialStateSeed{}, fmt.Errorf("load ohlcv for %s: no bars returned", strategy.Ticker)
	}

	seed := agent.InitialStateSeed{
		Market: &agent.MarketData{
			Bars:       bars,
			Indicators: data.IndicatorSnapshotFromBars(bars),
		},
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

	newsFrom := to.Add(-strategyNewsLookback)
	if articles, err := r.dataService.GetNews(ctx, strategy.MarketType, strategy.Ticker, newsFrom, to); err == nil {
		seed.News = articles
	} else if ctxErr := contextErr(err); ctxErr != nil {
		return agent.InitialStateSeed{}, ctxErr
	} else {
		r.logger.Warn("prod strategy runner: news unavailable",
			slog.String("ticker", strategy.Ticker),
			slog.Any("error", err),
		)
	}

	socialFrom := to.Add(-strategySocialLookback)
	if snapshots, err := r.dataService.GetSocialSentiment(ctx, strategy.MarketType, strategy.Ticker, socialFrom, to); err == nil {
		seed.Social = latestSocialSnapshot(snapshots)
	} else if ctxErr := contextErr(err); ctxErr != nil {
		return agent.InitialStateSeed{}, ctxErr
	} else {
		r.logger.Warn("prod strategy runner: social sentiment unavailable",
			slog.String("ticker", strategy.Ticker),
			slog.Any("error", err),
		)
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

	return seed, nil
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
	var timeout time.Duration
	if llmTimeout > 0 {
		timeout = llmTimeout
	}
	if resolved.PipelineConfig.DebateTimeoutSeconds > 0 {
		debateTimeout := time.Duration(resolved.PipelineConfig.DebateTimeoutSeconds) * time.Second
		if timeout <= 0 || debateTimeout < timeout {
			timeout = debateTimeout
		}
	}
	return timeout
}

func promptOverride(overrides map[agent.AgentRole]string, role agent.AgentRole, fallback string) string {
	prompt := strings.TrimSpace(overrides[role])
	if prompt == "" {
		return fallback
	}
	return prompt
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
			BuildPrompt: func(input agent.AnalysisInput) (string, bool) {
				return agentanalysts.FormatSocialAnalystUserPrompt(input.Ticker, input.Social), true
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

func (r *realStrategyRunner) newOrderManager(strategy domain.Strategy, resolved agent.ResolvedConfig) (*execution.OrderManager, error) {
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
		execution.SizingConfig{
			Method:      execution.PositionSizingMethodFixedFractional,
			FractionPct: resolved.RiskConfig.PositionSizePct / 100.0,
		},
		r.logger,
	).WithMetrics(r.metrics), nil
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

	if !r.cfg.Features.EnableLiveTrading {
		return nil, "", fmt.Errorf("live trading is disabled for strategy %s", strategy.Name)
	}

	switch marketType {
	case domain.MarketTypeStock:
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
		pm := r.cfg.Brokers.Polymarket
		if strings.TrimSpace(pm.KeyID) == "" || strings.TrimSpace(pm.SecretKey) == "" {
			return nil, "", errors.New("polymarket credentials (POLYMARKET_KEY_ID and POLYMARKET_SECRET_KEY) are required for live polymarket trading")
		}
		if r.polymarketClient == nil {
			return nil, "", errors.New("polymarket client not initialised")
		}
		return polymarketexecution.NewBroker(r.polymarketClient), "polymarket", nil
	default:
		return nil, "", fmt.Errorf("live trading is not supported for market type %q", strategy.MarketType)
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
			Data:       map[string]any{"error": e.Error},
			Timestamp:  e.OccurredAt,
		}
	default:
		// LLMCacheStatsReported, PipelineCompleted — no WS mapping needed.
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
	tradeDate := time.Now().UTC().Truncate(24 * time.Hour)
	run, err := r.runRepo.Get(ctx, runID, tradeDate)
	if err == nil {
		return run, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	const pageSize = 100
	for offset := 0; ; offset += pageSize {
		runs, err := r.runRepo.List(ctx, repository.PipelineRunFilter{}, pageSize, offset)
		if err != nil {
			return nil, err
		}
		if len(runs) == 0 {
			break
		}
		for i := range runs {
			if runs[i].ID == runID {
				return &runs[i], nil
			}
		}
		if len(runs) < pageSize {
			break
		}
	}
	return nil, fmt.Errorf("run %s: %w", runID, repository.ErrNotFound)
}
