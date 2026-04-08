package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	redis "github.com/redis/go-redis/v9"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/cli"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/automation"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	alpacaData "github.com/PatrickFanella/get-rich-quick/internal/data/alpaca"
	"github.com/PatrickFanella/get-rich-quick/internal/data/alphavantage"
	"github.com/PatrickFanella/get-rich-quick/internal/data/binance"
	"github.com/PatrickFanella/get-rich-quick/internal/data/finnhub"
	"github.com/PatrickFanella/get-rich-quick/internal/data/fmp"
	"github.com/PatrickFanella/get-rich-quick/internal/data/newsapi"
	"github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
	polymarketData "github.com/PatrickFanella/get-rich-quick/internal/data/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/data/tradier"
	"github.com/PatrickFanella/get-rich-quick/internal/data/yahoo"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/anthropic"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/google"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/ollama"
	openaiProvider "github.com/PatrickFanella/get-rich-quick/internal/llm/openai"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	"github.com/PatrickFanella/get-rich-quick/internal/notification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

func newAPIServer(ctx context.Context, cfg config.Config, logger *slog.Logger) (*api.Server, cli.SchedulerLifecycle, func(), error) {
	db, err := pgrepo.NewDB(ctx, cfg.Database.URL)
	if err != nil {
		return nil, nil, nil, err
	}

	redisHealth, closeRedis := newRedisHealthCheck(cfg)

	appMetrics := metrics.New()

	strategyRepo := pgrepo.NewStrategyRepo(db.Pool)
	runRepo := pgrepo.NewPipelineRunRepo(db.Pool)
	snapshotRepo := pgrepo.NewPipelineRunSnapshotRepo(db.Pool)
	decisionRepo := pgrepo.NewAgentDecisionRepo(db.Pool)
	eventRepo := pgrepo.NewAgentEventRepo(db.Pool)
	orderRepo := pgrepo.NewOrderRepo(db.Pool)
	positionRepo := pgrepo.NewPositionRepo(db.Pool)
	tradeRepo := pgrepo.NewTradeRepo(db.Pool)
	memoryRepo := pgrepo.NewMemoryRepo(db.Pool)
	apiKeyRepo := pgrepo.NewAPIKeyRepo(db.Pool)
	auditLogRepo := pgrepo.NewAuditLogRepo(db.Pool)
	userRepo := pgrepo.NewUserRepo(db.Pool)
	conversationRepo := pgrepo.NewConversationRepo(db.Pool)
	marketDataCacheRepo := pgrepo.NewMarketDataCacheRepo(db.Pool)
	jobRunRepo := pgrepo.NewJobRunRepo(db.Pool)
	optionsScanRepo := pgrepo.NewOptionsScanRepo(db.Pool)
	newsFeedRepo := pgrepo.NewNewsFeedRepo(db.Pool)
	polymarketAccountRepo := pgrepo.NewPolymarketAccountRepo(db.Pool)

	riskEngine := risk.NewRiskEngine(
		risk.PositionLimits{
			MaxPerPositionPct: cfg.Risk.MaxPositionSizePct,
			MaxTotalPct:       1.0,
			MaxConcurrent:     cfg.Risk.MaxOpenPositions,
			MaxPerMarketPct:   0.50,
		},
		risk.CircuitBreakerConfig{
			MaxDailyLossPct:      cfg.Risk.MaxDailyLossPct,
			MaxDrawdownPct:       cfg.Risk.MaxDrawdownPct,
			MaxConsecutiveLosses: 5,
			CooldownDuration:     cfg.Risk.CircuitBreakerCooldown,
		},
		positionRepo,
		logger,
	).WithPolymarketLimits(risk.PolymarketLimits{
		MaxSingleMarketExposurePct: cfg.Risk.Polymarket.MaxSingleMarketExposurePct,
		MaxTotalExposurePct:        cfg.Risk.Polymarket.MaxTotalExposurePct,
		MaxPositionUSDC:            cfg.Risk.Polymarket.MaxPositionUSDC,
		MinLiquidity:               cfg.Risk.Polymarket.MinLiquidity,
		MaxSpreadPct:               cfg.Risk.Polymarket.MaxSpreadPct,
		MinDaysToResolution:        cfg.Risk.Polymarket.MinDaysToResolution,
	}).WithStatePersister(ctx, pgrepo.NewRiskStatePersister(db.Pool))

	settingsSvc := api.NewMemorySettingsServiceFromConfig(cfg).
		WithPersister(ctx, pgrepo.NewSettingsPersister(db.Pool), logger)

	deps := api.Deps{
		Strategies:      strategyRepo,
		Runs:            runRepo,
		Decisions:       decisionRepo,
		Orders:          orderRepo,
		Positions:       positionRepo,
		Trades:          tradeRepo,
		Memories:        memoryRepo,
		APIKeys:         apiKeyRepo,
		Users:           userRepo,
		Risk:            riskEngine,
		Settings:        settingsSvc,
		DBHealth:        api.HealthCheckFunc(db.Pool.Ping),
		RedisHealth:     redisHealth,
		Conversations:   conversationRepo,
		AuditLog:        auditLogRepo,
		Events:          eventRepo,
		MetricsHandler:  appMetrics.Handler(),
		Snapshots:       snapshotRepo,
		LLMProvider:     throttleLLM(newLLMProviderFromConfig(cfg.LLM, logger)),
		BacktestConfigs:  pgrepo.NewBacktestConfigRepo(db.Pool),
		BacktestRuns:     pgrepo.NewBacktestRunRepo(db.Pool),
		NewsFeedRepo:     newsFeedRepo,
		DiscoveryRunRepo: pgrepo.NewDiscoveryRunRepo(db.Pool),
	}
	notificationManager := newNotificationManager(cfg)

	var sched *scheduler.Scheduler
	// serverRef is populated after api.NewServer returns. The scheduler only
	// starts (via cli.Execute → runServeLifecycle) after newAPIServer returns,
	// so any closure that reads serverRef will see the final non-nil value.
	var serverRef *api.Server

	if strings.EqualFold(cfg.Environment, "smoke") {
		pipeline := newSmokePipeline(runRepo, snapshotRepo, decisionRepo, eventRepo, logger)
		runner := newSmokeRunner(runRepo, snapshotRepo, decisionRepo, eventRepo, logger)
		strategyRunner := newSmokeStrategyRunner(runner, runRepo, decisionRepo, orderRepo, positionRepo, tradeRepo, auditLogRepo, eventRepo, riskEngine, notificationManager, logger)
		deps.Runner = strategyRunner
		sched = scheduler.NewScheduler(
			strategyRepo,
			pipeline,
			riskEngine,
			logger,
			scheduler.WithStrategyExecution(func(ctx context.Context, strategy domain.Strategy) error {
				_, err := strategyRunner.RunStrategy(ctx, strategy)
				return err
			}),
		)
	} else {
		// Global rate limiter: 50 req/min shared across all providers.
		data.SetGlobalLimiter(data.NewRateLimiter(50, time.Minute))

		reg := data.NewProviderRegistry()
		polygon.Register(reg)
		alphavantage.Register(reg)
		finnhub.Register(reg)
		fmp.Register(reg)
		newsapi.Register(reg)
		yahoo.Register(reg)
		binance.Register(reg)
		polymarketData.Register(reg)
		dataService := data.NewDataService(cfg, reg, marketDataCacheRepo, logger)
		deps.DataService = dataService
		// Options data chain: Tradier (full Greeks from ORATS) → Yahoo (free, BS Greeks)
		// → Alpaca (paper account) → Polygon (rate-limited).
		optProviders := []data.OptionsDataProvider{}
		if strings.TrimSpace(cfg.DataProviders.Tradier.APIKey) != "" {
			optProviders = append(optProviders, tradier.NewOptionsProvider(cfg.DataProviders.Tradier.APIKey, cfg.DataProviders.Tradier.Sandbox, logger))
		}
		optProviders = append(optProviders, yahoo.NewOptionsProvider(logger))
		if strings.TrimSpace(cfg.Brokers.Alpaca.APIKey) != "" && strings.TrimSpace(cfg.Brokers.Alpaca.APISecret) != "" {
			optProviders = append(optProviders, alpacaData.NewOptionsDataProvider(cfg.Brokers.Alpaca.APIKey, cfg.Brokers.Alpaca.APISecret, logger))
		}
		if strings.TrimSpace(cfg.DataProviders.Polygon.APIKey) != "" {
			optProviders = append(optProviders, polygon.NewOptionsProvider(polygon.NewClient(cfg.DataProviders.Polygon.APIKey, logger)))
		}
		deps.OptionsProvider = data.NewOptionsProviderChain(logger, optProviders...)
		// Events provider: Finnhub provides earnings, filings, economic, IPO calendars.
		if strings.TrimSpace(cfg.DataProviders.Finnhub.APIKey) != "" {
			eventsClient := finnhub.NewClient(cfg.DataProviders.Finnhub.APIKey, logger)
			deps.EventsProvider = finnhub.NewProvider(eventsClient)
		}
		deps.DiscoveryDeps = &discovery.DiscoveryDeps{
			DataService: dataService,
			LLMProvider: deps.LLMProvider,
			Strategies:  strategyRepo,
			Logger:      logger,
		}
		strategyRunner := newRealStrategyRunner(
			cfg,
			dataService,
			runRepo,
			snapshotRepo,
			decisionRepo,
			eventRepo,
			orderRepo,
			positionRepo,
			tradeRepo,
			auditLogRepo,
			riskEngine,
			notificationManager,
			logger,
		)
		deps.Runner = strategyRunner

		if cfg.Features.EnableScheduler {
			schedOpts := []scheduler.Option{
				scheduler.WithStrategyExecution(func(ctx context.Context, strategy domain.Strategy) error {
					result, err := strategyRunner.RunStrategy(ctx, strategy)
					if err == nil && result != nil && serverRef != nil {
						serverRef.BroadcastRunResult(result)
					}
					return err
				}),
			}

			if cfg.Features.EnableTickerDiscovery && strings.TrimSpace(cfg.DataProviders.Polygon.APIKey) != "" {
				polygonClient := polygon.NewClient(cfg.DataProviders.Polygon.APIKey, logger)
				universeRepo := pgrepo.NewUniverseRepo(db.Pool)
				univ := universe.NewUniverse(universeRepo, polygonClient, logger)
				deps.Universe = univ
				deps.UniverseRepo = universeRepo
				schedOpts = append(schedOpts, scheduler.WithTickerDiscovery(univ, polygonClient, *deps.DiscoveryDeps, scheduler.TickerDiscoveryConfig{
					Cron:       cfg.TickerDiscovery.Cron,
					MinADV:     cfg.TickerDiscovery.MinADV,
					MaxTickers: cfg.TickerDiscovery.MaxTickers,
				}))
			}

			sched = scheduler.NewScheduler(
				strategyRepo,
				nil,
				riskEngine,
				logger,
				schedOpts...,
			)
		}

		// Create the automation orchestrator if universe and discovery are available.
		if deps.Universe != nil && deps.DiscoveryDeps != nil {
			var polygonClientForAuto *polygon.Client
			if strings.TrimSpace(cfg.DataProviders.Polygon.APIKey) != "" {
				polygonClientForAuto = polygon.NewClient(cfg.DataProviders.Polygon.APIKey, logger)
			}
			orch := automation.NewJobOrchestrator(automation.OrchestratorDeps{
				Universe:              deps.Universe,
				Polygon:               polygonClientForAuto,
				DataService:           dataService,
				OptionsProvider:       deps.OptionsProvider,
				LLMProvider:           deps.LLMProvider,
				EventsProvider:        deps.EventsProvider,
				StrategyRepo:          strategyRepo,
				RunRepo:               runRepo,
				JobRunRepo:            jobRunRepo,
				OptionsScanRepo:       optionsScanRepo,
				NewsFeedRepo:          newsFeedRepo,
				PolymarketAccountRepo: polymarketAccountRepo,
				PolymarketCLOBURL:     cfg.Brokers.Polymarket.CLOBURL,
				StrategyTrigger:       sched,
				Logger:                logger,
			})
			orch.RegisterAll()
			if err := orch.Start(); err != nil {
				logger.Warn("automation: failed to start job orchestrator", slog.Any("error", err))
			} else {
				logger.Info("automation: job orchestrator started", slog.Int("jobs", len(orch.Status())))
			}
			deps.Automation = orch
		}
	}

	// Wire signal intelligence: EventStore, WatchIndex, SignalHub, TriggerHandler.
	// Pass the scheduler as the trigger runner (it satisfies signal.StrategyTriggerer).
	// If the scheduler is nil (scheduler feature disabled), buildSignalInfra still
	// returns a live store and watch index for the API, but the hub won't start.
	signalStore, signalWatchIndex, signalShutdown := buildSignalInfra(
		ctx, cfg, strategyRepo, polymarketAccountRepo, deps.LLMProvider, sched, logger,
	)
	deps.SignalStore = signalStore
	deps.WatchIndex = signalWatchIndex

	// Wire universe to API deps if not already set (non-discovery path).
	if deps.Universe == nil && strings.TrimSpace(cfg.DataProviders.Polygon.APIKey) != "" {
		polygonClient := polygon.NewClient(cfg.DataProviders.Polygon.APIKey, logger)
		universeRepo := pgrepo.NewUniverseRepo(db.Pool)
		univ := universe.NewUniverse(universeRepo, polygonClient, logger)
		deps.Universe = univ
		deps.UniverseRepo = universeRepo
	}

	apiCfg := api.DefaultServerConfig()
	apiCfg.Host = cfg.Server.Host
	apiCfg.Port = cfg.Server.Port
	apiCfg.JWTSecret = cfg.Server.JWTSecret
	apiCfg.RefreshTokenTTL = 24 * time.Hour

	server, err := api.NewServer(apiCfg, deps, logger)
	if err != nil {
		closeRedis()
		db.Close()
		return nil, nil, nil, err
	}

	// Wire the WebSocket hub into the strategy runner so that phase events
	// (agent decisions, debate rounds) are streamed in real time.
	// Also set serverRef so scheduled-run closures can call BroadcastRunResult.
	if sr, ok := deps.Runner.(*realStrategyRunner); ok {
		sr.hub = server.Hub()
	}
	serverRef = server

	// Avoid the Go nil-interface trap: explicitly return a nil interface when
	// there is no scheduler so that the caller's nil check works correctly.
	var schedLifecycle cli.SchedulerLifecycle
	if sched != nil {
		schedLifecycle = sched
	}

	return server, schedLifecycle, func() {
		signalShutdown()
		closeRedis()
		db.Close()
	}, nil
}

// throttleLLM wraps a provider with a global concurrency limiter so that
// serial backends (Ollama) aren't overwhelmed by concurrent requests.
func throttleLLM(p llm.Provider) llm.Provider {
	if p == nil {
		return nil
	}
	return llm.NewThrottledProvider(p, 4)
}

// newLLMProviderFromConfig builds an llm.Provider from application config.
// Returns nil (logged as a warning) when no provider is configured or the
// required credentials are missing so callers can handle the absent provider
// gracefully (e.g. returning 501 from the conversations endpoint).
func newLLMProviderFromConfig(cfg config.LLMConfig, logger *slog.Logger) llm.Provider {
	p, err := newLLMProviderForSelection(cfg, cfg.DefaultProvider, cfg.QuickThinkModel, logger)
	if err != nil {
		providerName := strings.ToLower(strings.TrimSpace(cfg.DefaultProvider))
		logger.Warn("LLM provider not available", slog.String("provider", providerName), slog.Any("error", err))
		return nil
	}
	return p
}

func newLLMProviderForSelection(cfg config.LLMConfig, providerName, model string, logger *slog.Logger) (llm.Provider, error) {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	// resolveModel prefers the explicit runtime model and then falls back to the
	// provider-specific configured model.
	resolveModel := func(providerModel string) string {
		if m := strings.TrimSpace(model); m != "" {
			return m
		}
		return strings.TrimSpace(providerModel)
	}

	switch providerName {
	case "openai":
		return openaiProvider.NewProvider(openaiProvider.Config{
			APIKey:  cfg.Providers.OpenAI.APIKey,
			BaseURL: cfg.Providers.OpenAI.BaseURL,
			Model:   resolveModel(cfg.Providers.OpenAI.Model),
		})
	case "anthropic":
		return anthropic.NewProvider(anthropic.Config{
			APIKey:  cfg.Providers.Anthropic.APIKey,
			BaseURL: cfg.Providers.Anthropic.BaseURL,
			Model:   resolveModel(cfg.Providers.Anthropic.Model),
		})
	case "google":
		return google.NewProvider(google.Config{
			APIKey:  cfg.Providers.Google.APIKey,
			BaseURL: cfg.Providers.Google.BaseURL,
			Model:   resolveModel(cfg.Providers.Google.Model),
		})
	case "openrouter":
		return openaiProvider.NewProvider(openaiProvider.Config{
			APIKey:  cfg.Providers.OpenRouter.APIKey,
			BaseURL: cfg.Providers.OpenRouter.BaseURL,
			Model:   resolveModel(cfg.Providers.OpenRouter.Model),
		})
	case "xai":
		return openaiProvider.NewProvider(openaiProvider.Config{
			APIKey:  cfg.Providers.XAI.APIKey,
			BaseURL: cfg.Providers.XAI.BaseURL,
			Model:   resolveModel(cfg.Providers.XAI.Model),
		})
	case "ollama":
		return ollama.NewProvider(ollama.Config{
			BaseURL: cfg.Providers.Ollama.BaseURL,
			Model:   resolveModel(cfg.Providers.Ollama.Model),
		})
	default:
		if providerName == "" {
			return nil, errors.New("llm provider name is required")
		}
		return nil, fmt.Errorf("unsupported provider name %q", providerName)
	}
}

func newNotificationManager(cfg config.Config) *notification.Manager {
	notifiers := map[string]notification.Notifier{
		notification.ChannelN8N: notification.NewWebhookNotifier(
			cfg.Notifications.N8N.URL,
			cfg.Notifications.N8N.Secret,
		),
	}

	if cfg.Notifications.Telegram.BotToken != "" && cfg.Notifications.Telegram.ChatID != "" {
		notifiers[notification.ChannelTelegram] = notification.NewTelegramNotifier(
			cfg.Notifications.Telegram.BotToken,
			cfg.Notifications.Telegram.ChatID,
		)
	}

	if cfg.Notifications.Email.SMTPHost != "" && len(cfg.Notifications.Email.To) > 0 {
		notifiers[notification.ChannelEmail] = notification.NewEmailNotifier(
			cfg.Notifications.Email.SMTPHost,
			cfg.Notifications.Email.SMTPPort,
			cfg.Notifications.Email.Username,
			cfg.Notifications.Email.Password,
			cfg.Notifications.Email.From,
			cfg.Notifications.Email.To,
		)
	}

	if cfg.Notifications.PagerDuty.URL != "" {
		notifiers[notification.ChannelPagerDuty] = notification.NewWebhookNotifier(
			cfg.Notifications.PagerDuty.URL,
			cfg.Notifications.PagerDuty.Secret,
		)
	}

	if cfg.Notifications.Discord.SignalWebhookURL != "" || cfg.Notifications.Discord.DecisionWebhookURL != "" || cfg.Notifications.Discord.AlertWebhookURL != "" {
		notifiers[notification.ChannelDiscord] = notification.NewDiscordNotifier(
			cfg.Notifications.Discord.SignalWebhookURL,
			cfg.Notifications.Discord.DecisionWebhookURL,
			cfg.Notifications.Discord.AlertWebhookURL,
		)
	}

	return notification.NewManager(cfg.Notifications.Alerts, notifiers)
}

func newRedisHealthCheck(cfg config.Config) (api.HealthCheck, func()) {
	if !cfg.Features.EnableRedisCache {
		return api.HealthCheckFunc(func(context.Context) error { return nil }), func() {}
	}

	redisURL := strings.TrimSpace(cfg.Redis.URL)
	if redisURL == "" {
		return api.HealthCheckFunc(func(context.Context) error { return errors.New("redis url is not configured") }), func() {}
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return api.HealthCheckFunc(func(context.Context) error {
			return fmt.Errorf("parse redis url: %w", err)
		}), func() {}
	}

	client := redis.NewClient(opts)
	return api.HealthCheckFunc(func(ctx context.Context) error {
			return client.Ping(ctx).Err()
		}), func() {
			_ = client.Close()
		}
}

type smokeStrategyRunner struct {
	runner              *agent.Runner
	runRepo             repository.PipelineRunRepository
	decisionRepo        repository.AgentDecisionRepository
	orderRepo           repository.OrderRepository
	positionRepo        repository.PositionRepository
	orderManager        *execution.OrderManager
	notificationManager *notification.Manager
}

func newSmokeStrategyRunner(
	runner *agent.Runner,
	runRepo repository.PipelineRunRepository,
	decisionRepo repository.AgentDecisionRepository,
	orderRepo repository.OrderRepository,
	positionRepo repository.PositionRepository,
	tradeRepo repository.TradeRepository,
	auditLogRepo repository.AuditLogRepository,
	agentEventRepo repository.AgentEventRepository,
	riskEngine risk.RiskEngine,
	notificationManager *notification.Manager,
	logger *slog.Logger,
) api.StrategyRunner {
	broker := paper.NewPaperBroker(100_000, 0, 0)
	if engineImpl, ok := riskEngine.(*risk.RiskEngineImpl); ok {
		engineImpl.SetPortfolioSnapshotFunc(func(ctx context.Context) (risk.Portfolio, error) {
			return execution.BuildRiskPortfolioSnapshot(ctx, broker, positionRepo)
		})
	}

	orderManager := execution.NewOrderManager(
		broker,
		"paper",
		riskEngine,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditLogRepo,
		agentEventRepo,
		execution.SizingConfig{
			Method:      execution.PositionSizingMethodFixedFractional,
			FractionPct: 0.05,
		},
		logger,
	)

	return &smokeStrategyRunner{
		runner:              runner,
		runRepo:             runRepo,
		decisionRepo:        decisionRepo,
		orderRepo:           orderRepo,
		positionRepo:        positionRepo,
		orderManager:        orderManager,
		notificationManager: notificationManager,
	}
}

func (r *smokeStrategyRunner) RunStrategy(ctx context.Context, strategy domain.Strategy) (*api.StrategyRunResult, error) {
	result, err := r.runner.RunStrategy(ctx, strategy, agent.GlobalSettings{})
	if err != nil {
		return nil, err
	}

	run, err := r.findRun(ctx, result.Run.ID)
	if err != nil {
		return nil, err
	}

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

	if err := r.orderManager.ProcessSignal(
		ctx,
		execution.FinalSignal{
			Signal:     signal,
			Confidence: state.FinalSignal.Confidence,
		},
		execution.TradingPlan{
			Action:       signal,
			Ticker:       state.TradingPlan.Ticker,
			EntryType:    state.TradingPlan.EntryType,
			EntryPrice:   state.TradingPlan.EntryPrice,
			PositionSize: state.TradingPlan.PositionSize,
			StopLoss:     state.TradingPlan.StopLoss,
			TakeProfit:   state.TradingPlan.TakeProfit,
			TimeHorizon:  state.TradingPlan.TimeHorizon,
			Confidence:   state.TradingPlan.Confidence,
			Rationale:    state.TradingPlan.Rationale,
			RiskReward:   state.TradingPlan.RiskReward,
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

func (r *smokeStrategyRunner) dispatchNotifications(ctx context.Context, strategy domain.Strategy, run *domain.PipelineRun, state *agent.PipelineState) error {
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

	if r.decisionRepo == nil {
		return nil
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

func (r *smokeStrategyRunner) findRun(ctx context.Context, runID uuid.UUID) (*domain.PipelineRun, error) {
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

func newSmokePipeline(
	runRepo repository.PipelineRunRepository,
	snapshotRepo repository.PipelineRunSnapshotRepository,
	decisionRepo repository.AgentDecisionRepository,
	eventRepo repository.AgentEventRepository,
	logger *slog.Logger,
) *agent.Pipeline {
	pipeline := agent.NewPipeline(
		agent.PipelineConfig{
			ResearchDebateRounds: 1,
			RiskDebateRounds:     1,
		},
		agent.NewRepoPersister(runRepo, snapshotRepo, decisionRepo, eventRepo, logger),
		nil,
		logger,
	)

	pipeline.RegisterNode(smokeNode{
		name:  "smoke-market-analyst",
		role:  agent.AgentRoleMarketAnalyst,
		phase: agent.PhaseAnalysis,
		exec: func(state *agent.PipelineState) error {
			const report = "Smoke test market analysis indicates bullish momentum."
			state.SetAnalystReport(agent.AgentRoleMarketAnalyst, report)
			state.RecordDecision(agent.AgentRoleMarketAnalyst, agent.PhaseAnalysis, nil, report, nil)
			return nil
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-bull-researcher",
		role:  agent.AgentRoleBullResearcher,
		phase: agent.PhaseResearchDebate,
		exec: func(state *agent.PipelineState) error {
			return recordResearchDebateContribution(&state.ResearchDebate, agent.AgentRoleBullResearcher, "Bull case: strong setup for a paper-trade entry.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-bear-researcher",
		role:  agent.AgentRoleBearResearcher,
		phase: agent.PhaseResearchDebate,
		exec: func(state *agent.PipelineState) error {
			return recordResearchDebateContribution(&state.ResearchDebate, agent.AgentRoleBearResearcher, "Bear case: downside risk is bounded by the configured stop.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-invest-judge",
		role:  agent.AgentRoleInvestJudge,
		phase: agent.PhaseResearchDebate,
		exec: func(state *agent.PipelineState) error {
			const plan = "Proceed with a small paper buy to validate the execution path."
			state.ResearchDebate.InvestmentPlan = plan
			state.RecordDecision(agent.AgentRoleInvestJudge, agent.PhaseResearchDebate, nil, plan, nil)
			return nil
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-trader",
		role:  agent.AgentRoleTrader,
		phase: agent.PhaseTrading,
		exec: func(state *agent.PipelineState) error {
			state.TradingPlan = agent.TradingPlan{
				Action:       domain.PipelineSignalBuy,
				Ticker:       state.Ticker,
				EntryType:    "market",
				EntryPrice:   100,
				PositionSize: 0.05,
				StopLoss:     95,
				TakeProfit:   110,
				TimeHorizon:  "1d",
				Confidence:   0.92,
				Rationale:    "Smoke test deterministic trading plan",
				RiskReward:   2,
			}
			payload, err := json.Marshal(state.TradingPlan)
			if err != nil {
				return err
			}
			state.RecordDecision(agent.AgentRoleTrader, agent.PhaseTrading, nil, string(payload), nil)
			return nil
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-aggressive-risk",
		role:  agent.AgentRoleAggressiveAnalyst,
		phase: agent.PhaseRiskDebate,
		exec: func(state *agent.PipelineState) error {
			return recordRiskDebateContribution(&state.RiskDebate, agent.AgentRoleAggressiveAnalyst, "Aggressive view: approve the trade.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-conservative-risk",
		role:  agent.AgentRoleConservativeAnalyst,
		phase: agent.PhaseRiskDebate,
		exec: func(state *agent.PipelineState) error {
			return recordRiskDebateContribution(&state.RiskDebate, agent.AgentRoleConservativeAnalyst, "Conservative view: size is acceptable for smoke validation.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-neutral-risk",
		role:  agent.AgentRoleNeutralAnalyst,
		phase: agent.PhaseRiskDebate,
		exec: func(state *agent.PipelineState) error {
			return recordRiskDebateContribution(&state.RiskDebate, agent.AgentRoleNeutralAnalyst, "Neutral view: proceed and observe the paper execution.")
		},
	})
	pipeline.RegisterNode(smokeNode{
		name:  "smoke-risk-manager",
		role:  agent.AgentRoleRiskManager,
		phase: agent.PhaseRiskDebate,
		exec: func(state *agent.PipelineState) error {
			state.FinalSignal = agent.FinalSignal{
				Signal:     domain.PipelineSignalBuy,
				Confidence: 0.92,
			}
			const storedSignal = `{"action":"buy","confidence":0.92}`
			state.RiskDebate.FinalSignal = storedSignal
			state.RecordDecision(agent.AgentRoleRiskManager, agent.PhaseRiskDebate, nil, storedSignal, nil)
			return nil
		},
	})

	return pipeline
}

type smokeNode struct {
	name  string
	role  agent.AgentRole
	phase agent.Phase
	exec  func(state *agent.PipelineState) error
}

func newSmokeRunner(
	runRepo repository.PipelineRunRepository,
	snapshotRepo repository.PipelineRunSnapshotRepository,
	decisionRepo repository.AgentDecisionRepository,
	eventRepo repository.AgentEventRepository,
	logger *slog.Logger,
) *agent.Runner {
	return agent.NewRunner(
		agent.Definition{
			Analysis: []agent.AnalysisAgent{
				smokeAnalysisAgent{
					name:   "smoke-market-analyst",
					role:   agent.AgentRoleMarketAnalyst,
					report: "Smoke test market analysis indicates bullish momentum.",
				},
			},
			Research: agent.ResearchDebateStage{
				Debaters: []agent.DebateAgent{
					smokeDebateAgent{name: "smoke-bull-researcher", role: agent.AgentRoleBullResearcher, contribution: "Bull case: strong setup for a paper-trade entry."},
					smokeDebateAgent{name: "smoke-bear-researcher", role: agent.AgentRoleBearResearcher, contribution: "Bear case: downside risk is bounded by the configured stop."},
				},
				Judge: smokeResearchJudge{name: "smoke-invest-judge", role: agent.AgentRoleInvestJudge, plan: "Proceed with a small paper buy to validate the execution path."},
			},
			Trader: smokeTradeAgent{name: "smoke-trader", role: agent.AgentRoleTrader},
			Risk: agent.RiskDebateStage{
				Debaters: []agent.DebateAgent{
					smokeDebateAgent{name: "smoke-aggressive-risk", role: agent.AgentRoleAggressiveAnalyst, contribution: "Aggressive view: approve the trade."},
					smokeDebateAgent{name: "smoke-conservative-risk", role: agent.AgentRoleConservativeAnalyst, contribution: "Conservative view: size is acceptable for smoke validation."},
					smokeDebateAgent{name: "smoke-neutral-risk", role: agent.AgentRoleNeutralAnalyst, contribution: "Neutral view: proceed and observe the paper execution."},
				},
				Judge: smokeRiskJudge{name: "smoke-risk-manager", role: agent.AgentRoleRiskManager},
			},
		},
		agent.Dependencies{
			Persister: agent.NewRepoPersister(runRepo, snapshotRepo, decisionRepo, eventRepo, logger),
			Logger:    logger,
		},
	)
}



type smokeAnalysisAgent struct {
	name   string
	role   agent.AgentRole
	report string
}

func (a smokeAnalysisAgent) Name() string          { return a.name }
func (a smokeAnalysisAgent) Role() agent.AgentRole { return a.role }
func (a smokeAnalysisAgent) Analyze(context.Context, agent.AnalysisInput) (agent.AnalysisOutput, error) {
	return agent.AnalysisOutput{Report: a.report}, nil
}

type smokeDebateAgent struct {
	name         string
	role         agent.AgentRole
	contribution string
}

func (a smokeDebateAgent) Name() string          { return a.name }
func (a smokeDebateAgent) Role() agent.AgentRole { return a.role }
func (a smokeDebateAgent) Debate(context.Context, agent.DebateInput) (agent.DebateOutput, error) {
	return agent.DebateOutput{Contribution: a.contribution}, nil
}

type smokeResearchJudge struct {
	name string
	role agent.AgentRole
	plan string
}

func (j smokeResearchJudge) Name() string          { return j.name }
func (j smokeResearchJudge) Role() agent.AgentRole { return j.role }
func (j smokeResearchJudge) JudgeResearch(context.Context, agent.DebateInput) (agent.ResearchJudgeOutput, error) {
	return agent.ResearchJudgeOutput{InvestmentPlan: j.plan}, nil
}

type smokeTradeAgent struct {
	name string
	role agent.AgentRole
}

func (a smokeTradeAgent) Name() string          { return a.name }
func (a smokeTradeAgent) Role() agent.AgentRole { return a.role }
func (a smokeTradeAgent) Trade(_ context.Context, input agent.TradingInput) (agent.TradingOutput, error) {
	plan := agent.TradingPlan{
		Action:       domain.PipelineSignalBuy,
		Ticker:       input.Ticker,
		EntryType:    "market",
		EntryPrice:   100,
		PositionSize: 0.05,
		StopLoss:     95,
		TakeProfit:   110,
		TimeHorizon:  "1d",
		Confidence:   0.92,
		Rationale:    "Smoke test deterministic trading plan",
		RiskReward:   2,
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return agent.TradingOutput{}, err
	}
	return agent.TradingOutput{Plan: plan, StoredOutput: string(payload)}, nil
}

type smokeRiskJudge struct {
	name string
	role agent.AgentRole
}

func (j smokeRiskJudge) Name() string          { return j.name }
func (j smokeRiskJudge) Role() agent.AgentRole { return j.role }
func (j smokeRiskJudge) JudgeRisk(_ context.Context, input agent.RiskJudgeInput) (agent.RiskJudgeOutput, error) {
	plan := input.TradingPlan
	plan.Action = domain.PipelineSignalBuy
	plan.Confidence = 0.92
	if plan.Ticker == "" {
		plan.Ticker = input.Ticker
	}
	if plan.EntryType == "" {
		plan.EntryType = "market"
		plan.EntryPrice = 100
		plan.PositionSize = 0.05
		plan.StopLoss = 95
		plan.TakeProfit = 110
		plan.TimeHorizon = "1d"
		plan.Rationale = "Smoke test deterministic trading plan"
		plan.RiskReward = 2
	}
	return agent.RiskJudgeOutput{
		FinalSignal:  agent.FinalSignal{Signal: domain.PipelineSignalBuy, Confidence: 0.92},
		StoredSignal: `{"action":"buy","confidence":0.92}`,
		TradingPlan:  plan,
	}, nil
}

func (n smokeNode) Name() string          { return n.name }
func (n smokeNode) Role() agent.AgentRole { return n.role }
func (n smokeNode) Phase() agent.Phase    { return n.phase }

func (n smokeNode) Execute(ctx context.Context, state *agent.PipelineState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.exec == nil {
		return nil
	}
	return n.exec(state)
}

func recordResearchDebateContribution(state *agent.ResearchDebateState, role agent.AgentRole, contribution string) error {
	if state == nil {
		return fmt.Errorf("research debate state is required")
	}
	return recordDebateRoundContribution(state.Rounds, func(rounds []agent.DebateRound) {
		state.Rounds = rounds
	}, role, contribution)
}

func recordRiskDebateContribution(state *agent.RiskDebateState, role agent.AgentRole, contribution string) error {
	if state == nil {
		return fmt.Errorf("risk debate state is required")
	}
	return recordDebateRoundContribution(state.Rounds, func(rounds []agent.DebateRound) {
		state.Rounds = rounds
	}, role, contribution)
}

func recordDebateRoundContribution(
	rounds []agent.DebateRound,
	setRounds func([]agent.DebateRound),
	role agent.AgentRole,
	contribution string,
) error {
	if len(rounds) == 0 {
		return fmt.Errorf("debate round is not initialized")
	}

	round := rounds[len(rounds)-1]
	if round.Contributions == nil {
		round.Contributions = make(map[agent.AgentRole]string)
	}
	round.Contributions[role] = contribution
	rounds[len(rounds)-1] = round
	setRounds(rounds)

	return nil
}
