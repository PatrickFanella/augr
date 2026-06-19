package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	redis "github.com/redis/go-redis/v9"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/automation"
	"github.com/PatrickFanella/get-rich-quick/internal/cli"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	alpacaData "github.com/PatrickFanella/get-rich-quick/internal/data/alpaca"
	"github.com/PatrickFanella/get-rich-quick/internal/data/alphavantage"
	"github.com/PatrickFanella/get-rich-quick/internal/data/binance"
	"github.com/PatrickFanella/get-rich-quick/internal/data/finnhub"
	"github.com/PatrickFanella/get-rich-quick/internal/data/fmp"
	kalshidata "github.com/PatrickFanella/get-rich-quick/internal/data/kalshi"
	"github.com/PatrickFanella/get-rich-quick/internal/data/newsapi"
	"github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
	polymarketData "github.com/PatrickFanella/get-rich-quick/internal/data/polymarket"
	redditData "github.com/PatrickFanella/get-rich-quick/internal/data/reddit"
	stocktwitsData "github.com/PatrickFanella/get-rich-quick/internal/data/stocktwits"
	"github.com/PatrickFanella/get-rich-quick/internal/data/tradier"
	"github.com/PatrickFanella/get-rich-quick/internal/data/yahoo"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	alpacaexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/alpaca"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	polymarketexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	kalshidiscovery "github.com/PatrickFanella/get-rich-quick/internal/kalshidiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/anthropic"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/embedding"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/google"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/ollama"
	openaiProvider "github.com/PatrickFanella/get-rich-quick/internal/llm/openai"
	polymarketws "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	"github.com/PatrickFanella/get-rich-quick/internal/notification"
	"github.com/PatrickFanella/get-rich-quick/internal/observability"
	"github.com/PatrickFanella/get-rich-quick/internal/recorder"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
	"github.com/PatrickFanella/get-rich-quick/internal/service"
	"github.com/PatrickFanella/get-rich-quick/internal/signal"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
	"github.com/prometheus/client_golang/prometheus"
)

type watchedMarketsLoaderAdapter struct {
	repo repository.PolymarketWatchedMarketsRepository
}

type polymarketStatusSource struct {
	feed    *polymarketws.Feed
	metrics *observability.SurfersMetrics
}

func (s polymarketStatusSource) PolymarketStatus(ctx context.Context) (api.PolymarketStatus, error) {
	_ = ctx
	if s.feed == nil {
		return api.PolymarketStatus{Enabled: false}, nil
	}
	stats := s.feed.Stats()
	return api.PolymarketStatus{
		Enabled:       true,
		WSConnections: stats.Pool.Members,
		AvgJitterMS:   stats.Pool.AvgJitterMS,
		Dropped:       stats.Pool.Dropped,
		ReadySlugs:    nil,
		RecorderLagS:  s.metrics.LastRecorderLag(),
		UpdatedAt:     time.Now().UTC(),
	}, nil
}

func (a watchedMarketsLoaderAdapter) ListEnabledSlugs(ctx context.Context) ([]string, error) {
	items, err := a.repo.List(ctx, true)
	if err != nil {
		return nil, err
	}
	slugs := make([]string, 0, len(items))
	for _, item := range items {
		slugs = append(slugs, item.Slug)
	}
	return slugs, nil
}

var (
	runtimeNewDB                = pgrepo.NewDB
	runtimeCurrentSchemaVersion = pgrepo.CurrentSchemaVersion
	runtimeNewServer            = api.NewServer
	runtimeAfterSchemaGate      = func() {}
	runtimeCloseDB              = func(db *pgrepo.DB) {
		if db != nil {
			db.Close()
		}
	}
	runtimeNewPolymarketFeed = polymarketws.NewFeed
	surfersMetricsOnce       sync.Once
	surfersMetricsInst       *observability.SurfersMetrics
	runtimeLLMComposer       = llm.NewComposer(llm.RuntimeProviderFactories{
		OpenAI:     runtimeOpenAIProvider,
		Anthropic:  runtimeAnthropicProvider,
		Google:     runtimeGoogleProvider,
		OpenRouter: runtimeOpenRouterProvider,
		XAI:        runtimeXAIProvider,
		Ollama:     runtimeOllamaProvider,
	})
)

func runtimeOpenAIProvider(cfg llm.OpenAIProviderConfig) (llm.Provider, error) {
	provider, err := openaiProvider.NewProvider(openaiProvider.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model})
	if err != nil {
		return nil, err
	}
	return llm.ProviderFunc(provider.Complete), nil
}

func runtimeAnthropicProvider(cfg llm.AnthropicProviderConfig) (llm.Provider, error) {
	provider, err := anthropic.NewProvider(anthropic.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model})
	if err != nil {
		return nil, err
	}
	return llm.ProviderFunc(provider.Complete), nil
}

func runtimeGoogleProvider(cfg llm.GoogleProviderConfig) (llm.Provider, error) {
	provider, err := google.NewProvider(google.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model})
	if err != nil {
		return nil, err
	}
	return llm.ProviderFunc(provider.Complete), nil
}

func runtimeOpenRouterProvider(cfg llm.OpenAIProviderConfig) (llm.Provider, error) {
	provider, err := openaiProvider.NewProvider(openaiProvider.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model})
	if err != nil {
		return nil, err
	}
	return llm.ProviderFunc(provider.Complete), nil
}

func runtimeXAIProvider(cfg llm.OpenAIProviderConfig) (llm.Provider, error) {
	provider, err := openaiProvider.NewProvider(openaiProvider.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model})
	if err != nil {
		return nil, err
	}
	return llm.ProviderFunc(provider.Complete), nil
}

func runtimeOllamaProvider(cfg llm.OllamaProviderConfig) (llm.Provider, error) {
	provider, err := ollama.NewProvider(ollama.Config{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model})
	if err != nil {
		return nil, err
	}
	return llm.ProviderFunc(provider.Complete), nil
}

type runtimeSchemaVersionError struct {
	State    string
	Current  int
	Required int
}

func (e *runtimeSchemaVersionError) Error() string {
	return fmt.Sprintf(
		"database schema version mismatch (%s): current version %d, required version %d; run migrations, then restart the process. Migrations applied after process start require a fresh process restart",
		e.State,
		e.Current,
		e.Required,
	)
}

func ensureRuntimeSchemaCompatible(ctx context.Context, db *pgrepo.DB) (int, int, string, error) {
	current, err := runtimeCurrentSchemaVersion(ctx, db.Pool)
	if err != nil {
		return 0, 0, "", err
	}
	required := pgrepo.RequiredSchemaVersion
	status := string(pgrepo.CompareSchemaVersion(current, required))

	switch state := status; state {
	case "behind", "ahead":
		return current, required, status, &runtimeSchemaVersionError{
			State:    state,
			Current:  current,
			Required: required,
		}
	default:
		return current, required, status, nil
	}
}

func newAPIServer(ctx context.Context, cfg config.Config, logger *slog.Logger) (*api.Server, cli.SchedulerLifecycle, func(), error) {
	db, err := runtimeNewDB(ctx, cfg.Database.URL)
	if err != nil {
		return nil, nil, nil, err
	}
	currentSchemaVersion, requiredSchemaVersion, schemaStatus, err := ensureRuntimeSchemaCompatible(ctx, db)
	if err != nil {
		runtimeCloseDB(db)
		return nil, nil, nil, err
	}
	runtimeAfterSchemaGate()

	redisHealth, closeRedis := newRedisHealthCheck(cfg)

	appMetrics := metrics.New()
	surfersMetricsOnce.Do(func() { surfersMetricsInst = observability.NewSurfersMetrics(prometheus.DefaultRegisterer) })
	surfersMetrics := surfersMetricsInst
	sharedLLMBudget := llm.BuildLLMBudget(cfg.LLM)

	strategyRepo := pgrepo.NewStrategyRepo(db.Pool)
	runRepo := pgrepo.NewPipelineRunRepo(db.Pool)
	snapshotRepo := pgrepo.NewPipelineRunSnapshotRepo(db.Pool)
	decisionRepo := pgrepo.NewAgentDecisionRepo(db.Pool)
	eventRepo := pgrepo.NewAgentEventRepo(db.Pool)
	orderRepo := pgrepo.NewOrderRepo(db.Pool)
	positionRepo := pgrepo.NewPositionRepo(db.Pool)
	tradeRepo := pgrepo.NewTradeRepo(db.Pool)
	tradeDecisionRepo := pgrepo.NewTradeDecisionJournalRepo(db.Pool)
	opportunityRepo := pgrepo.NewOpportunityRepo(db.Pool)
	allocationDecisionRepo := pgrepo.NewAllocationDecisionRepo(db.Pool)
	replayEventRepo := pgrepo.NewReplayEventRepo(db.Pool)
	tradeDecisionRecorder := execution.NewTradeDecisionJournalRecorder(tradeDecisionRepo)
	memoryRepo := pgrepo.NewMemoryRepo(db.Pool)
	apiKeyRepo := pgrepo.NewAPIKeyRepo(db.Pool)
	auditLogRepo := pgrepo.NewAuditLogRepo(db.Pool)
	backtestConfigRepo := pgrepo.NewBacktestConfigRepo(db.Pool)
	backtestRunRepo := pgrepo.NewBacktestRunRepo(db.Pool)
	userRepo := pgrepo.NewUserRepo(db.Pool)
	conversationRepo := pgrepo.NewConversationRepo(db.Pool)
	marketDataCacheRepo := pgrepo.NewMarketDataCacheRepo(db.Pool)
	jobRunRepo := pgrepo.NewJobRunRepo(db.Pool)
	optionsScanRepo := pgrepo.NewOptionsScanRepo(db.Pool)
	newsFeedRepo := pgrepo.NewNewsFeedRepo(db.Pool)
	polymarketAccountRepo := pgrepo.NewPolymarketAccountRepo(db.Pool)
	polymarketWatchedRepo := pgrepo.NewPolymarketWatchedMarketsRepo(db.Pool)
	polymarketResolvedRepo := pgrepo.NewPolymarketResolvedMarketsRepo(db.Pool)
	riskBreakerRepo := pgrepo.NewRiskBreakerRepo(db.Pool)
	riskBreaker := risk.NewDrawdownBreaker(risk.DrawdownBreakerConfig{}, riskBreakerRepo)
	reportArtifactRepo := pgrepo.NewReportArtifactRepo(db.Pool)
	runRegistry := agent.NewRunContextRegistry()

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

	settingsSvc := api.NewMemorySettingsServiceFromConfig(cfg, currentSchemaVersion, requiredSchemaVersion, schemaStatus).
		WithPersister(ctx, pgrepo.NewSettingsPersister(db.Pool), logger)
	promptSettingsSvc := api.NewPromptSettingsService().WithPersister(ctx, pgrepo.NewSettingsPersister(db.Pool))

	deps := api.Deps{
		Strategies:             strategyRepo,
		Runs:                   runRepo,
		Decisions:              decisionRepo,
		Orders:                 orderRepo,
		Positions:              positionRepo,
		OpportunityRepo:        opportunityRepo,
		AllocationDecisionRepo: allocationDecisionRepo,
		Trades:                 tradeRepo,
		TradeDecisions:         tradeDecisionRepo,
		ReplayEvents:           replayEventRepo,
		Memories:               memoryRepo,
		APIKeys:                apiKeyRepo,
		Users:                  userRepo,
		Risk:                   riskEngine,
		RiskBreaker:            riskBreaker,
		RiskBreakerLister:      riskBreakerRepo,
		Settings:               settingsSvc,
		Prompts:                promptSettingsSvc,
		DBHealth:               api.HealthCheckFunc(db.Pool.Ping),
		RedisHealth:            redisHealth,
		Conversations:          conversationRepo,
		AuditLog:               auditLogRepo,
		Events:                 eventRepo,
		MetricsHandler:         appMetrics.Handler(),
		Snapshots:              snapshotRepo,
		LLMProvider:            runtimeLLMComposer.BuildProvider(cfg.LLM, appMetrics, logger, sharedLLMBudget),
		BacktestConfigs:        backtestConfigRepo,
		BacktestRuns:           backtestRunRepo,
		NewsFeedRepo:           newsFeedRepo,
		MarketDataHistory:      marketDataCacheRepo,
		DiscoveryRunRepo:       pgrepo.NewDiscoveryRunRepo(db.Pool),
		JobRunRepo:             jobRunRepo,
		ReportArtifacts:        reportArtifactRepo,
		ReportMetrics:          appMetrics,
		PolymarketAccountRepo:  polymarketAccountRepo,
		PolymarketWatchedRepo:  polymarketWatchedRepo,
		PolymarketClient:       nil,
		KalshiWatchedRepo:      pgrepo.NewKalshiWatchedMarketsRepo(db.Pool),
		KalshiSnapshotsRepo:    pgrepo.NewKalshiMarketSnapshotsRepo(db.Pool),
		KalshiDiscoveryRuns:    pgrepo.NewKalshiDiscoveryRunRepo(db.Pool),
	}
	var polymarketFeed *polymarketws.Feed
	var polymarketRecorder *recorder.Recorder
	var strategyRunner *realStrategyRunner
	if strings.EqualFold(strings.TrimSpace(os.Getenv("POLYMARKET_WS_ENABLED")), "true") {
		pcfg := polymarketws.DefaultConfig()
		if v := strings.TrimSpace(os.Getenv("POLYMARKET_WS_URL")); v != "" {
			pcfg.WSURL = v
		}
		if v := strings.TrimSpace(os.Getenv("POLYMARKET_WS_CONNECTIONS")); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				pcfg.ConnectionsPerFeed = n
			}
		}
		if v := strings.TrimSpace(os.Getenv("POLYMARKET_WS_SLUGS")); v != "" {
			pcfg.PerMarketSlugs = splitCSV(v)
		}
		pcfg.Metrics = surfersMetrics.FeedMetrics()
		var err error
		resolvedIDs, assetIDToSlug, resolveErr := resolvePolymarketAssetIDs(ctx, pcfg.PerMarketSlugs)
		if resolveErr != nil {
			logger.Warn("polymarket ws resolution failed; feed disabled", slog.String("error", resolveErr.Error()))
		} else {
			pcfg.AssetIDs = resolvedIDs
			pcfg.AssetIDToSlug = assetIDToSlug
			polymarketFeed, err = runtimeNewPolymarketFeed(pcfg)
			if err != nil {
				logger.Warn("polymarket ws feed construction failed; feed disabled", slog.String("error", err.Error()))
			} else if err := polymarketFeed.Start(ctx); err != nil {
				logger.Warn("polymarket ws feed start failed; feed disabled", slog.String("error", err.Error()))
				polymarketFeed.Close()
				polymarketFeed = nil
			} else {
				logger.Info("polymarket ws feed started", slog.Int("connections", pcfg.ConnectionsPerFeed), slog.Int("slug_count", len(pcfg.PerMarketSlugs)))
				if strings.EqualFold(strings.TrimSpace(os.Getenv("POLYMARKET_RECORDER_ENABLED")), "true") {
					polymarketRecorder = recorder.New(polymarketFeed, pgrepo.NewPolymarketMarketDataRepo(db.Pool), recorder.RecorderConfig{BatchSize: 5000, FlushInterval: 500 * time.Millisecond, Slugs: pcfg.PerMarketSlugs}, logger, surfersMetrics.RecorderMetrics())
					polymarketRecorder.Start(ctx)
				}
			}
		}
	}
	deps.MarketDataStatus = polymarketStatusSource{feed: polymarketFeed, metrics: surfersMetrics}
	notificationManager := newNotificationManager(cfg)

	var sched *scheduler.Scheduler
	// serverRef is populated after api.NewServer returns. The scheduler only
	// starts (via cli.Execute → runServeLifecycle) after newAPIServer returns,
	// so any closure that reads serverRef will see the final non-nil value.
	var serverRef *api.Server

	if strings.EqualFold(cfg.Environment, "smoke") {
		pipeline := newSmokePipeline(runRepo, snapshotRepo, decisionRepo, eventRepo, logger)
		runner := newSmokeRunner(runRepo, snapshotRepo, decisionRepo, eventRepo, logger)
		strategyRunner := newSmokeStrategyRunner(runner, runRepo, decisionRepo, orderRepo, positionRepo, tradeRepo, auditLogRepo, eventRepo, riskEngine, notificationManager, tradeDecisionRecorder, logger)
		deps.Runner = strategyRunner
		sched = scheduler.NewScheduler(
			strategyRepo,
			pipeline,
			riskEngine,
			logger,
			scheduler.WithJobTimeout(cfg.Features.SchedulerJobTimeout),
			scheduler.WithMetrics(appMetrics),
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
		kalshidata.Register(reg)
		polymarketData.Register(reg)
		stocktwitsData.Register(reg)
		redditData.Register(reg)

		var socialTriage *data.SocialTriageConfig
		if deps.LLMProvider != nil {
			socialTriage = &data.SocialTriageConfig{
				Provider: deps.LLMProvider,
				Model:    cfg.LLM.QuickThinkModel,
			}
		}

		dataService := data.NewDataService(cfg, reg, marketDataCacheRepo, logger, socialTriage)
		deps.DataService = dataService
		var alpacaReconciler *automation.AlpacaReconciler
		var polymarketExecutionReconciler *polymarketexecution.Reconciler
		if polymarketL2Configured(cfg.Brokers.Polymarket) {
			polymarketClient := polymarketexecution.NewClient(cfg.Brokers.Polymarket.KeyID, cfg.Brokers.Polymarket.SecretKey, logger)
			polymarketClient.SetL2Auth(cfg.Brokers.Polymarket.Address, cfg.Brokers.Polymarket.KeyID, cfg.Brokers.Polymarket.SecretKey, cfg.Brokers.Polymarket.Passphrase)
			polymarketClient.SetAPIBaseURL(cfg.Brokers.Polymarket.APIBaseURL)
			polymarketClient.SetGatewayBaseURL(cfg.Brokers.Polymarket.GatewayBaseURL)
			polymarketExecutionReconciler = polymarketexecution.NewReconciler(polymarketexecution.ReconcilerDeps{
				Broker:       polymarketexecution.NewBroker(polymarketClient),
				PositionRepo: positionRepo,
				AuditLogRepo: auditLogRepo,
				Metrics:      appMetrics,
				Logger:       logger,
			})
		}
		if strings.TrimSpace(cfg.Brokers.Alpaca.APIKey) != "" && strings.TrimSpace(cfg.Brokers.Alpaca.APISecret) != "" {
			alpacaClient := alpacaexecution.NewClient(
				cfg.Brokers.Alpaca.APIKey,
				cfg.Brokers.Alpaca.APISecret,
				cfg.Brokers.Alpaca.PaperMode,
				logger,
			)
			alpacaReconciler = automation.NewAlpacaReconciler(automation.AlpacaReconcilerDeps{
				Broker:       automation.NewAlpacaClientAdapter(alpacaClient),
				StrategyRepo: strategyRepo,
				OrderRepo:    orderRepo,
				PositionRepo: positionRepo,
				TradeRepo:    tradeRepo,
				AuditLogRepo: auditLogRepo,
				Logger:       logger,
			})
			deps.AlpacaReconciler = alpacaReconciler
		}
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
		deps.ResearchScanner = service.NewResearchScannerService(deps.OptionsProvider, deps.PolymarketClient, logger)
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
		strategyRunner = newRealStrategyRunner(
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
			appMetrics,
			notificationManager,
			runRegistry,
			sharedLLMBudget,
			promptSettingsSvc,
			tradeDecisionRecorder,
			polymarketFeed,
			logger,
		)
		deps.Runner = strategyRunner
		if err := bootstrapPolymarketStopGuards(ctx, strategyRunner, positionRepo, logger); err != nil {
			logger.Warn("polymarket stop guard bootstrap failed", slog.Any("error", err))
		}

		kalshiDiscoveryClient, err := kalshidata.NewClient(cfg.Brokers.Kalshi.APIBaseURL, cfg.Brokers.Kalshi.APIKeyID, cfg.Brokers.Kalshi.PrivateKeyPEMB64, logger)
		var kalshiCatalog interface {
			ListMarkets(context.Context, kalshidiscovery.ListOptions) ([]kalshidiscovery.MarketCandidate, string, error)
		}
		if err != nil {
			logger.Warn("automation: failed to create kalshi discovery client", slog.Any("error", err))
		} else {
			kalshiCatalog = kalshidiscovery.NewClient(kalshiDiscoveryClient)
		}

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
			backtestSvc := service.NewBacktestService(backtestConfigRepo, backtestRunRepo, strategyRepo, auditLogRepo, dataService, deps.LLMProvider, logger)
			schedOpts = append(schedOpts, scheduler.WithBacktestServiceScheduling(backtestConfigRepo, backtestSvc, "scheduler"))

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
				append([]scheduler.Option{scheduler.WithJobTimeout(cfg.Features.SchedulerJobTimeout), scheduler.WithMetrics(appMetrics)}, schedOpts...)...,
			)
		}

		// Create the automation orchestrator when scheduler support is enabled. Jobs
		// self-register only when their own dependencies are present, so Kalshi
		// discovery does not depend on stock ticker discovery being enabled.
		{
			var polygonClientForAuto *polygon.Client
			if strings.TrimSpace(cfg.DataProviders.Polygon.APIKey) != "" {
				polygonClientForAuto = polygon.NewClient(cfg.DataProviders.Polygon.APIKey, logger)
			}
			embeddingBaseURL := cfg.Embedding.BaseURL
			if embeddingBaseURL == "" {
				embeddingBaseURL = cfg.LLM.Providers.Ollama.BaseURL
			}
			embeddingProvider, err := embedding.NewOllamaProvider(embedding.OllamaConfig{
				BaseURL: embeddingBaseURL,
				Model:   cfg.Embedding.Model,
				Timeout: cfg.Embedding.Timeout,
				APIKey:  cfg.LLM.Providers.Ollama.APIKey,
			})
			if err != nil {
				logger.Warn("automation: failed to create embedding provider", slog.Any("error", err))
			} else {
				overnightBacktestRunRepo := pgrepo.NewOvernightBacktestRunRepo(db.Pool)
				polymarketDiscoveryRunRepo := pgrepo.NewPolymarketDiscoveryRunRepo(db.Pool)
				orch := automation.NewJobOrchestrator(automation.OrchestratorDeps{
					Universe:                  deps.Universe,
					Polygon:                   polygonClientForAuto,
					DataService:               dataService,
					AlpacaReconciler:          alpacaReconciler,
					OptionsProvider:           deps.OptionsProvider,
					LLMProvider:               deps.LLMProvider,
					EmbeddingProvider:         embeddingProvider,
					EventsProvider:            deps.EventsProvider,
					StrategyRepo:              strategyRepo,
					RunRepo:                   runRepo,
					OpportunityRepo:           opportunityRepo,
					AllocationDecisionRepo:    allocationDecisionRepo,
					JobRunRepo:                jobRunRepo,
					OptionsScanRepo:           optionsScanRepo,
					NewsFeedRepo:              newsFeedRepo,
					PolymarketAccountRepo:     polymarketAccountRepo,
					PolymarketReconciler:      polymarketExecutionReconciler,
					PolymarketResolvedRepo:    polymarketResolvedRepo,
					PolymarketWatchedRepo:     polymarketWatchedRepo,
					PolymarketDiscoveryRuns:   polymarketDiscoveryRunRepo,
					PolymarketCLOBURL:         cfg.Brokers.Polymarket.CLOBURL,
					KalshiCatalog:             kalshiCatalog,
					KalshiWatchedRepo:         pgrepo.NewKalshiWatchedMarketsRepo(db.Pool),
					KalshiMarketSnapshotsRepo: pgrepo.NewKalshiMarketSnapshotsRepo(db.Pool),
					KalshiDiscoveryRuns:       pgrepo.NewKalshiDiscoveryRunRepo(db.Pool),
					ReportArtifactRepo:        reportArtifactRepo,
					BacktestConfigRepo:        backtestConfigRepo,
					BacktestRunRepo:           backtestRunRepo,
					OvernightBacktestRuns:     overnightBacktestRunRepo,
					StrategyTrigger:           sched,
					Logger:                    logger,
				})
				orch.WithJobMetrics(appMetrics)
				orch.WithReportMetrics(appMetrics)
				orch.RegisterAll()
				if err := orch.Start(); err != nil {
					logger.Warn("automation: failed to start job orchestrator", slog.Any("error", err))
				} else {
					logger.Info("automation: job orchestrator started", slog.Int("jobs", len(orch.Status())))
				}
				deps.Automation = orch
			}
		}
	}
	deps.ResearchScanner = service.NewResearchScannerService(deps.OptionsProvider, deps.PolymarketClient, logger)

	// Wire signal intelligence: EventStore, WatchIndex, SignalHub, TriggerHandler.
	// Pass the scheduler as the trigger runner (it satisfies signal.StrategyTriggerer).
	// If the scheduler is nil (smoke / scheduler-disabled mode), Start is a no-op but
	// the store and watch index are still wired into API deps for read endpoints.
	clobURL := cfg.Brokers.Polymarket.CLOBURL
	if clobURL == "" {
		clobURL = "https://clob.polymarket.com"
	}
	var signalSources []signal.SignalSource
	signalSources = append(signalSources,
		signal.NewRSSSource(signal.DefaultRSSFeeds(), 60*time.Second, logger),
		signal.NewRedditSource(signal.DefaultSubreddits(), 5*time.Minute, logger),
		signal.NewPolymarketSource(signal.PolymarketSourceConfig{
			CLOBURL:               clobURL,
			Interval:              10 * time.Minute,
			PriceMoveThreshold:    0.05,
			VolumeSpikeMultiplier: 3.0,
			Loader:                watchedMarketsLoaderAdapter{repo: polymarketWatchedRepo},
		}, logger),
	)
	if polymarketAccountRepo != nil {
		signalSources = append(signalSources, signal.NewWhaleSource(signal.WhaleSourceConfig{
			CLOBURL:      clobURL,
			Interval:     30 * time.Second,
			MinTradeUSDC: 5000,
			MinWinRate:   0.65,
		}, polymarketAccountRepo, logger))
	}
	if cfg.Polygon.RPCURL != "" && cfg.Polygon.WSURL != "" {
		logger.Info("polygon mempool source enabled", slog.String("ws_url", cfg.Polygon.WSURL))
		signalSources = append(signalSources, signal.NewPolygonMempoolSource(signal.PolygonMempoolSourceConfig{
			RPCURL:         cfg.Polygon.RPCURL,
			WSURL:          cfg.Polygon.WSURL,
			WatchAddresses: signal.DefaultPolymarketContracts(),
			MaxSeenTxs:     4096,
		}, logger))
	}

	var sigEvaluator *signal.Evaluator
	if deps.LLMProvider != nil {
		sigEvaluator = signal.NewEvaluator(deps.LLMProvider, cfg.LLM.QuickThinkModel, logger).
			WithMetrics(appMetrics).
			WithFallbackMode(os.Getenv("SIGNAL_FALLBACK_MODE"))
	}

	stratProvider := signal.NewStrategyProviderWithCache(
		signal.NewRepositoryStrategyProvider(strategyRepo), 0,
	)
	sigOrch := signal.NewOrchestrator(
		signal.OrchestratorConfig{
			EventStoreSize: 200,
			LLMEvaluator:   sigEvaluator,
			Sources:        signalSources,
		},
		signal.OrchestratorDeps{
			StrategyProvider: stratProvider,
			StrategyLoader:   strategyRepo,
			ThesisLoader:     strategyRepo,
			Runner:           sched,
			Logger:           logger,
		},
	)
	if sched != nil {
		if err := sigOrch.Start(ctx); err != nil {
			logger.Warn("signal hub: failed to start", slog.Any("error", err))
		} else {
			logger.Info("signal intelligence: hub started", slog.Int("sources", len(signalSources)))
		}
	}
	deps.SignalStore = sigOrch.Store()
	deps.WatchIndex = sigOrch.WatchIndex()
	signalShutdown := sigOrch.Stop

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

	server, err := runtimeNewServer(apiCfg, deps, logger)
	if err != nil {
		closeRedis()
		runtimeCloseDB(db)
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

	staleRunTTL := loadStaleRunTTL(logger)
	var staleRunReconcilerCancel context.CancelFunc = func() {}
	if staleRunTTL > 0 {
		reconciler := agent.NewStaleRunReconciler(
			runRepo,
			auditLogRepo,
			runRegistry,
			appMetrics,
			logger,
			agent.StaleRunReconcilerConfig{TTL: staleRunTTL, Interval: time.Minute},
		)
		var reconcileCtx context.Context
		reconcileCtx, staleRunReconcilerCancel = context.WithCancel(context.Background())
		reconciler.Start(reconcileCtx)
		logger.Info("stale run reconciler started", slog.Duration("ttl", staleRunTTL), slog.Duration("interval", time.Minute))
	}

	return server, schedLifecycle, func() {
		if strategyRunner != nil {
			strategyRunner.stopPolymarketTickWorkers()
		}
		if polymarketRecorder != nil {
			polymarketRecorder.Close()
		}
		if polymarketFeed != nil {
			polymarketFeed.Close()
		}
		staleRunReconcilerCancel()
		signalShutdown()
		closeRedis()
		runtimeCloseDB(db)
	}, nil
}

func polymarketL2Configured(pm config.PolymarketConfig) bool {
	return strings.TrimSpace(pm.Address) != "" &&
		strings.TrimSpace(pm.KeyID) != "" &&
		strings.TrimSpace(pm.SecretKey) != "" &&
		strings.TrimSpace(pm.Passphrase) != ""
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var runtimeHTTPClient = http.DefaultClient

func resolvePolymarketAssetIDs(ctx context.Context, slugs []string) ([]string, map[string]string, error) {
	if len(slugs) == 0 {
		return nil, map[string]string{}, nil
	}
	assetIDs := make([]string, 0, len(slugs))
	assetIDToSlug := make(map[string]string, len(slugs))
	for _, slug := range slugs {
		ids, err := fetchPolymarketAssetIDsBySlug(ctx, slug)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve slug %q: %w", slug, err)
		}
		if len(ids) == 0 {
			return nil, nil, fmt.Errorf("resolve slug %q: no clob token ids returned", slug)
		}
		for _, id := range ids {
			assetIDs = append(assetIDs, id)
			assetIDToSlug[id] = slug
		}
	}
	return assetIDs, assetIDToSlug, nil
}

func fetchPolymarketAssetIDsBySlug(ctx context.Context, slug string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gamma-api.polymarket.com/markets/slug/"+slug, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "augr-tradingagent/1.0")
	resp, err := runtimeHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("gamma status %d", resp.StatusCode)
	}
	var payload struct {
		RawIDs    json.RawMessage `json:"clobTokenIds"`
		RawIDsAlt json.RawMessage `json:"clob_token_ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if ids, ok := decodeAssetIDPayload(payload.RawIDs); ok {
		return ids, nil
	}
	if ids, ok := decodeAssetIDPayload(payload.RawIDsAlt); ok {
		return ids, nil
	}
	return nil, nil
}

func decodeAssetIDPayload(raw json.RawMessage) ([]string, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err == nil {
		return ids, true
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil && strings.TrimSpace(encoded) != "" {
		if err := json.Unmarshal([]byte(encoded), &ids); err == nil {
			return ids, true
		}
	}
	return nil, false
}

func loadStaleRunTTL(logger *slog.Logger) time.Duration {
	const fallback = 50 * time.Minute
	raw := strings.TrimSpace(os.Getenv("STALE_RUN_TTL"))
	if raw == "" {
		return fallback
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl <= 0 {
		if logger != nil {
			logger.Warn("invalid STALE_RUN_TTL, using default", slog.String("value", raw), slog.Duration("default", fallback))
		}
		return fallback
	}
	return ttl
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
	runner                *agent.Runner
	runRepo               repository.PipelineRunRepository
	decisionRepo          repository.AgentDecisionRepository
	orderRepo             repository.OrderRepository
	positionRepo          repository.PositionRepository
	broker                execution.Broker
	riskEngine            risk.RiskEngine
	tradeRepo             repository.TradeRepository
	auditLogRepo          repository.AuditLogRepository
	agentEventRepo        repository.AgentEventRepository
	tradeDecisionRecorder execution.DecisionRecorder
	notificationManager   *notification.Manager
	logger                *slog.Logger
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
	tradeDecisionRecorder execution.DecisionRecorder,
	logger *slog.Logger,
) api.StrategyRunner {
	broker := paper.NewPaperBroker(100_000, 0, 0)
	if engineImpl, ok := riskEngine.(*risk.RiskEngineImpl); ok {
		engineImpl.SetPortfolioSnapshotFunc(func(ctx context.Context) (risk.Portfolio, error) {
			return execution.BuildRiskPortfolioSnapshot(ctx, broker, positionRepo)
		})
	}

	return &smokeStrategyRunner{
		runner:                runner,
		runRepo:               runRepo,
		decisionRepo:          decisionRepo,
		orderRepo:             orderRepo,
		positionRepo:          positionRepo,
		broker:                broker,
		riskEngine:            riskEngine,
		tradeRepo:             tradeRepo,
		auditLogRepo:          auditLogRepo,
		agentEventRepo:        agentEventRepo,
		tradeDecisionRecorder: tradeDecisionRecorder,
		notificationManager:   notificationManager,
		logger:                logger,
	}
}

func (r *smokeStrategyRunner) RunStrategy(ctx context.Context, strategy domain.Strategy) (*api.StrategyRunResult, error) {
	strategyConfig, err := parseStrategyConfig(strategy.Config)
	if err != nil {
		return nil, err
	}
	resolved := agent.ResolveConfig(strategyConfig, agent.GlobalSettings{})

	result, err := r.runner.RunStrategy(ctx, strategy, agent.GlobalSettings{})
	if err != nil {
		return nil, err
	}

	run, err := r.findRun(ctx, result.Run.ID)
	if err != nil {
		return nil, err
	}

	signal := result.Signal
	state := agent.PipelineStateFromView(result.State)
	planTicker := state.TradingPlan.Ticker
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

	orderManager, err := r.newOrderManager(ctx, strategy, strategyConfig, resolved)
	if err != nil {
		return nil, err
	}

	if err := orderManager.ProcessSignal(
		ctx,
		execution.FinalSignal{
			Signal:     signal,
			Confidence: state.FinalSignal.Confidence,
		},
		execution.TradingPlan{
			Action:           signal,
			MarketType:       strategy.MarketType.Normalize(),
			Ticker:           planTicker,
			EntryType:        state.TradingPlan.EntryType,
			EntryPrice:       state.TradingPlan.EntryPrice,
			PositionSize:     state.TradingPlan.PositionSize,
			StopLoss:         state.TradingPlan.StopLoss,
			TakeProfit:       state.TradingPlan.TakeProfit,
			TimeHorizon:      state.TradingPlan.TimeHorizon,
			Confidence:       state.TradingPlan.Confidence,
			Rationale:        state.TradingPlan.Rationale,
			RiskReward:       state.TradingPlan.RiskReward,
			Side:             state.TradingPlan.Side,
			DecisionMetadata: executionDecisionMetadata(ctx, r.decisionRepo, r.logger, run.ID),
		},
		strategy.ID,
		run.ID,
	); err != nil {
		return nil, err
	}

	if err := r.dispatchNotifications(ctx, strategy, run, state); err != nil {
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

func (r *smokeStrategyRunner) newOrderManager(ctx context.Context, strategy domain.Strategy, strategyConfig *agent.StrategyConfig, resolved agent.ResolvedConfig) (*execution.OrderManager, error) {
	return execution.NewOrderManager(
		r.broker,
		"paper",
		r.riskEngine,
		r.positionRepo,
		r.orderRepo,
		r.tradeRepo,
		r.auditLogRepo,
		r.agentEventRepo,
		sizingConfigForStrategy(ctx, strategy, strategyConfig, resolved, r.positionRepo, r.logger),
		r.logger,
	).WithDecisionRecorder(r.tradeDecisionRecorder), nil
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
	run, err := r.runRepo.GetByID(ctx, runID)
	if err == nil {
		return run, nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("run %s: %w", runID, repository.ErrNotFound)
	}
	return nil, err
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
