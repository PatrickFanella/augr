package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/automation"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/service"
	"github.com/PatrickFanella/get-rich-quick/internal/signal"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

// Server is the HTTP REST API server that exposes all system functionality.
type Server struct {
	router      chi.Router
	httpServer  *http.Server
	logger      *slog.Logger
	dbHealth    HealthCheck
	redisHealth HealthCheck

	// Repositories
	strategies     repository.StrategyRepository
	runs           repository.PipelineRunRepository
	decisions      repository.AgentDecisionRepository
	orders         repository.OrderRepository
	positions      repository.PositionRepository
	trades         repository.TradeRepository
	tradeDecisions repository.TradeDecisionJournalRepository
	replayEvents   repository.ReplayEventRepository
	memories       repository.MemoryRepository
	users          repository.UserRepository
	auditLog       repository.AuditLogRepository
	conversations  repository.ConversationRepository
	snapshots      repository.PipelineRunSnapshotRepository
	llmProvider    llm.Provider
	events         repository.AgentEventRepository

	// Backtest
	backtestConfigs repository.BacktestConfigRepository
	backtestRuns    repository.BacktestRunRepository
	divergenceSrc   DivergenceSource
	mdStatusSrc     MarketDataStatusSource
	dataService     *data.DataService

	// Discovery
	discoveryDeps    *discovery.DiscoveryDeps
	discoveryRunRepo discovery.RunRepository

	// Universe
	universe     *universe.Universe
	universeRepo universe.UniverseRepository

	// Automation
	automation       *automation.JobOrchestrator
	alpacaReconciler AlpacaAutomationReconciler
	jobRunRepo       *pgrepo.JobRunRepo

	// Risk engine
	risk              risk.RiskEngine
	riskBreaker       risk.Breaker
	riskBreakerLister RiskBreakerLister
	settings          SettingsService
	prompts           *PromptSettingsService
	runner            StrategyRunner

	auth *AuthManager

	// Options data
	optionsProvider data.OptionsDataProvider

	// News feed
	newsFeedRepo *pgrepo.NewsFeedRepo
	// Historical market data
	marketDataHistory repository.HistoricalOHLCVRepository

	// Calendar / events data
	eventsProvider data.EventsProvider

	// WebSocket hub for real-time event streaming.
	hub            *Hub
	wsUpgrader     websocket.Upgrader
	metricsHandler http.Handler

	// Signal intelligence (optional; nil = feature not running).
	signalStore           *signal.EventStore
	watchIndex            *signal.WatchIndex
	polymarketAccountRepo repository.PolymarketAccountRepository
	polymarketWatchedRepo repository.PolymarketWatchedMarketsRepository
	polymarketClient      PolymarketMarketDataFetcher
	kalshiWatchedRepo     repository.KalshiWatchedMarketsRepository
	kalshiSnapshotsRepo   repository.KalshiMarketSnapshotsRepository
	kalshiDiscoveryRuns   repository.KalshiDiscoveryRunRepository

	// Report artifacts (optional; nil = feature not enabled).
	reportArtifacts ReportArtifactStore

	// Report metrics (optional; nil = no metrics).
	reportMetrics ReportMetrics

	// Services — constructed from deps in NewServer.
	backtestSvc     *service.BacktestService
	conversationSvc *service.ConversationService
	runSvc          *service.RunService
	researchSvc     service.ResearchScannerService
}

// StrategyRunResult captures the persisted artifacts created by a manual run.
type StrategyRunResult struct {
	Run       domain.PipelineRun    `json:"run"`
	Signal    domain.PipelineSignal `json:"signal,omitempty"`
	Orders    []domain.Order        `json:"orders,omitempty"`
	Positions []domain.Position     `json:"positions,omitempty"`
}

// StrategyRunner triggers a strategy pipeline run on demand.
type StrategyRunner interface {
	RunStrategy(ctx context.Context, strategy domain.Strategy) (*StrategyRunResult, error)
}

// ErrStrategyAlreadyRunning is returned by StrategyRunner when a run is
// requested for a strategy that already has an in-flight pipeline execution.
var ErrStrategyAlreadyRunning = errors.New("strategy already has an in-flight run")

// HealthCheck verifies a runtime dependency is reachable.
type HealthCheck interface {
	Check(ctx context.Context) error
}

// HealthCheckFunc adapts a function into a HealthCheck.
type HealthCheckFunc func(context.Context) error

// Check runs the health check function.
func (f HealthCheckFunc) Check(ctx context.Context) error {
	return f(ctx)
}

// ServerConfig holds configuration for the API server.
type ServerConfig struct {
	Host            string
	Port            int
	CORSConfig      CORSConfig
	RateLimit       int           // requests per window
	RateWindow      time.Duration // window duration
	TrustedProxies  []string      // CIDR ranges of trusted reverse proxies
	JWTSecret       string
	RefreshTokenTTL time.Duration
	APIKeyRateLimit int
	APIKeyWindow    time.Duration
}

// DefaultServerConfig returns a sensible default server configuration.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Host:            "0.0.0.0",
		Port:            8080,
		CORSConfig:      DefaultCORSConfig(),
		RateLimit:       100,
		RateWindow:      time.Minute,
		APIKeyRateLimit: 100,
		APIKeyWindow:    time.Minute,
	}
}

// Deps groups the repository and service dependencies required by the Server.
type Deps struct {
	Strategies        repository.StrategyRepository
	Runs              repository.PipelineRunRepository
	Decisions         repository.AgentDecisionRepository
	Orders            repository.OrderRepository
	Positions         repository.PositionRepository
	Trades            repository.TradeRepository
	TradeDecisions    repository.TradeDecisionJournalRepository
	ReplayEvents      repository.ReplayEventRepository
	Memories          repository.MemoryRepository
	APIKeys           repository.APIKeyRepository
	Users             repository.UserRepository
	Conversations     repository.ConversationRepository
	AuditLog          repository.AuditLogRepository
	Events            repository.AgentEventRepository
	Snapshots         repository.PipelineRunSnapshotRepository
	LLMProvider       llm.Provider
	BacktestConfigs   repository.BacktestConfigRepository
	BacktestRuns      repository.BacktestRunRepository
	DivergenceSrc     DivergenceSource
	MarketDataStatus  MarketDataStatusSource
	DataService       *data.DataService
	OptionsProvider   data.OptionsDataProvider
	EventsProvider    data.EventsProvider
	DiscoveryDeps     *discovery.DiscoveryDeps
	DiscoveryRunRepo  discovery.RunRepository
	Universe          *universe.Universe
	UniverseRepo      universe.UniverseRepository
	Automation        *automation.JobOrchestrator
	AlpacaReconciler  AlpacaAutomationReconciler
	JobRunRepo        *pgrepo.JobRunRepo
	NewsFeedRepo      *pgrepo.NewsFeedRepo
	MarketDataHistory repository.HistoricalOHLCVRepository
	Risk              risk.RiskEngine
	RiskBreaker       risk.Breaker
	RiskBreakerLister RiskBreakerLister
	Settings          SettingsService
	Prompts           *PromptSettingsService
	Runner            StrategyRunner
	ResearchScanner   service.ResearchScannerService
	DBHealth          HealthCheck
	RedisHealth       HealthCheck
	MetricsHandler    http.Handler

	// Signal intelligence (optional; nil = feature not enabled).
	SignalStore            *signal.EventStore
	WatchIndex             *signal.WatchIndex
	PolymarketAccountRepo  repository.PolymarketAccountRepository
	PolymarketWatchedRepo  repository.PolymarketWatchedMarketsRepository
	PolymarketResolvedRepo repository.PolymarketResolvedMarketsRepository
	PolymarketClient       PolymarketMarketDataFetcher
	KalshiWatchedRepo      repository.KalshiWatchedMarketsRepository
	KalshiSnapshotsRepo    repository.KalshiMarketSnapshotsRepository
	KalshiDiscoveryRuns    repository.KalshiDiscoveryRunRepository

	// Report artifacts (optional; nil = feature not enabled).
	ReportArtifacts ReportArtifactStore

	// Report metrics (optional; nil = no metrics).
	ReportMetrics ReportMetrics
}

// NewServer creates a new API server with all routes and middleware registered.
func NewServer(cfg ServerConfig, deps Deps, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if deps.Strategies == nil {
		return nil, fmt.Errorf("strategies repository is required")
	}
	if deps.Runs == nil {
		return nil, fmt.Errorf("runs repository is required")
	}
	if deps.Decisions == nil {
		return nil, fmt.Errorf("decisions repository is required")
	}
	if deps.Orders == nil {
		return nil, fmt.Errorf("orders repository is required")
	}
	if deps.Positions == nil {
		return nil, fmt.Errorf("positions repository is required")
	}
	if deps.Trades == nil {
		return nil, fmt.Errorf("trades repository is required")
	}
	if deps.Memories == nil {
		return nil, fmt.Errorf("memories repository is required")
	}
	if deps.Users == nil {
		return nil, fmt.Errorf("users repository is required")
	}
	if deps.Risk == nil {
		return nil, fmt.Errorf("risk engine is required")
	}
	if deps.DBHealth == nil {
		return nil, fmt.Errorf("db health check is required")
	}
	if deps.RedisHealth == nil {
		return nil, fmt.Errorf("redis health check is required")
	}

	if strings.TrimSpace(cfg.JWTSecret) == "" {
		return nil, fmt.Errorf("jwt secret is required")
	}

	authManager, err := NewAuthManager(AuthConfig{
		JWTSecret:       cfg.JWTSecret,
		RefreshTokenTTL: cfg.RefreshTokenTTL,
		APIKeys:         deps.APIKeys,
		APIKeyRateLimit: cfg.APIKeyRateLimit,
		APIKeyWindow:    cfg.APIKeyWindow,
		Logger:          logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create auth manager: %w", err)
	}

	hub := NewHub(logger)

	settingsService := deps.Settings
	if settingsService == nil {
		settingsService = NewMemorySettingsService(SettingsBootstrap{})
	}
	promptService := deps.Prompts
	if promptService == nil {
		promptService = NewPromptSettingsService()
	}

	s := &Server{
		logger:                logger,
		dbHealth:              deps.DBHealth,
		redisHealth:           deps.RedisHealth,
		strategies:            deps.Strategies,
		runs:                  deps.Runs,
		decisions:             deps.Decisions,
		orders:                deps.Orders,
		positions:             deps.Positions,
		trades:                deps.Trades,
		tradeDecisions:        deps.TradeDecisions,
		replayEvents:          deps.ReplayEvents,
		memories:              deps.Memories,
		users:                 deps.Users,
		conversations:         deps.Conversations,
		snapshots:             deps.Snapshots,
		llmProvider:           deps.LLMProvider,
		auditLog:              deps.AuditLog,
		events:                deps.Events,
		backtestConfigs:       deps.BacktestConfigs,
		backtestRuns:          deps.BacktestRuns,
		divergenceSrc:         deps.DivergenceSrc,
		mdStatusSrc:           deps.MarketDataStatus,
		dataService:           deps.DataService,
		optionsProvider:       deps.OptionsProvider,
		eventsProvider:        deps.EventsProvider,
		discoveryDeps:         deps.DiscoveryDeps,
		discoveryRunRepo:      deps.DiscoveryRunRepo,
		universe:              deps.Universe,
		universeRepo:          deps.UniverseRepo,
		automation:            deps.Automation,
		alpacaReconciler:      deps.AlpacaReconciler,
		jobRunRepo:            deps.JobRunRepo,
		newsFeedRepo:          deps.NewsFeedRepo,
		marketDataHistory:     deps.MarketDataHistory,
		risk:                  deps.Risk,
		riskBreaker:           deps.RiskBreaker,
		riskBreakerLister:     deps.RiskBreakerLister,
		settings:              settingsService,
		prompts:               promptService,
		runner:                deps.Runner,
		auth:                  authManager,
		hub:                   hub,
		researchSvc:           deps.ResearchScanner,
		wsUpgrader:            newUpgrader(cfg.CORSConfig.AllowedOrigins),
		metricsHandler:        deps.MetricsHandler,
		signalStore:           deps.SignalStore,
		watchIndex:            deps.WatchIndex,
		polymarketAccountRepo: deps.PolymarketAccountRepo,
		polymarketWatchedRepo: deps.PolymarketWatchedRepo,
		polymarketClient:      deps.PolymarketClient,
		kalshiWatchedRepo:     deps.KalshiWatchedRepo,
		kalshiSnapshotsRepo:   deps.KalshiSnapshotsRepo,
		kalshiDiscoveryRuns:   deps.KalshiDiscoveryRuns,
		reportArtifacts:       deps.ReportArtifacts,
		reportMetrics:         deps.ReportMetrics,
	}
	if s.divergenceSrc == nil {
		// Phase H will wire a real data source; this keeps the route live and harmless for now.
		s.divergenceSrc = divergenceSourceStub{}
	}

	// Construct services from the assembled deps.
	s.backtestSvc = service.NewBacktestService(
		deps.BacktestConfigs, deps.BacktestRuns, deps.Strategies, deps.AuditLog,
		deps.DataService, deps.LLMProvider, logger,
	)
	s.conversationSvc = service.NewConversationService(
		deps.Conversations, deps.Decisions, deps.Snapshots, deps.Memories,
		deps.LLMProvider, logger,
	)
	s.runSvc = service.NewRunService(deps.Runs)

	r := chi.NewRouter()

	// Parse trusted proxy CIDRs for rate limiter IP extraction.
	trustedNets, err := ParseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("parse trusted proxies: %w", err)
	}

	// Global middleware
	r.Use(SecurityHeaders)
	r.Use(RequestLogger(logger))
	r.Use(CORS(cfg.CORSConfig))
	r.Use(MaxRequestBody(maxRequestBodyBytes))
	if cfg.RateLimit > 0 {
		rl := NewRateLimiter(cfg.RateLimit, cfg.RateWindow)
		rl.trustedProxies = trustedNets
		r.Use(rl.Middleware)
	}

	// Health check
	r.Get("/healthz", s.handleHealth)
	r.Get("/health", s.handleHealth)
	r.Get("/metrics", s.handleMetrics)

	// WebSocket endpoint for real-time event streaming.
	r.Get("/ws", s.handleWebSocket)

	// API v1
	r.Route("/api/v1/auth", func(auth chi.Router) {
		auth.Post("/login", s.handleLogin)
		auth.Post("/refresh", s.handleRefreshToken)
		auth.Post("/register", s.handleRegister)
	})

	r.Route("/api/v1", func(v1 chi.Router) {
		v1.Use(s.authMiddleware)

		// Strategies
		v1.Route("/strategies", func(sr chi.Router) {
			sr.Get("/", s.handleListStrategies)
			sr.Post("/", s.handleCreateStrategy)
			sr.Get("/{id}", s.handleGetStrategy)
			sr.Post("/{id}/run", s.handleRunStrategy)
			sr.Put("/{id}", s.handleUpdateStrategy)
			sr.Delete("/{id}", s.handleDeleteStrategy)
			sr.Post("/{id}/pause", s.handlePauseStrategy)
			sr.Post("/{id}/resume", s.handleResumeStrategy)
			sr.Post("/{id}/skip-next", s.handleSkipNextStrategy)

			// Report artifacts (nested under strategy)
			sr.Get("/{id}/reports/latest", s.handleGetLatestReport)
			sr.Get("/{id}/reports", s.handleListReports)
		})

		v1.Route("/polymarket", func(pr chi.Router) {
			pr.Get("/accounts", s.handleListPolymarketAccounts)
			pr.Get("/accounts/{address}", s.handleGetPolymarketAccount)
			pr.Get("/accounts/{address}/trades", s.handleListPolymarketAccountTrades)
			pr.Patch("/accounts/{address}/tracked", s.handlePatchPolymarketAccountTracked)
			pr.Get("/trades/recent", s.handleListPolymarketRecentTrades)
			pr.Get("/signals/recent", s.handleListPolymarketRecentSignals)
			pr.Get("/markets/{slug}", s.handleGetPolymarketMarket)
			pr.Get("/watched", s.handleListPolymarketWatched)
			pr.Post("/watched", s.handleAddPolymarketWatched)
			pr.Delete("/watched/{slug}", s.handleDeletePolymarketWatched)
			pr.Patch("/watched/{slug}", s.handlePatchPolymarketWatched)
			pr.Get("/jobs/status", s.handleGetPolymarketJobsStatus)
			pr.Get("/discovery/last", s.handleGetPolymarketDiscoveryLast)
			pr.Post("/discovery/run", s.handleRunPolymarketDiscovery)
		})

		v1.Get("/event-markets/summary", s.handleGetEventMarketsSummary)

		v1.Route("/kalshi", func(kr chi.Router) {
			kr.Get("/summary", s.handleGetKalshiSummary)
		})

		v1.Get("/marketdata/polymarket/status", s.handlePolymarketStatus)

		// Pipeline runs
		v1.Route("/runs", func(rr chi.Router) {
			rr.Get("/", s.handleListRuns)
			rr.Get("/{id}", s.handleGetRun)
			rr.Get("/{id}/decisions", s.handleGetRunDecisions)
			rr.Post("/{id}/cancel", s.handleCancelRun)
			rr.Get("/{id}/snapshot", s.handleGetRunSnapshot)
		})

		// Portfolio
		v1.Route("/portfolio", func(pr chi.Router) {
			pr.Get("/positions", s.handleListPositions)
			pr.Get("/positions/open", s.handleGetOpenPositions)
			pr.Get("/summary", s.handlePortfolioSummary)
		})

		// Orders
		v1.Route("/orders", func(or chi.Router) {
			or.Get("/", s.handleListOrders)
			or.Get("/{id}", s.handleGetOrder)
		})

		// Decision journal
		v1.Route("/journal", func(jr chi.Router) {
			jr.Get("/decisions", s.handleListTradeDecisions)
			jr.Get("/decisions/{id}", s.handleGetTradeDecision)
		})

		// Replay workbench
		v1.Route("/replay", func(rr chi.Router) {
			rr.Get("/decisions/{id}", s.handleGetReplayDecision)
		})

		// Trades
		v1.Get("/trades", s.handleListTrades)

		// Memories
		v1.Route("/memories", func(mr chi.Router) {
			mr.Get("/", s.handleListMemories)
			mr.Post("/search", s.handleSearchMemories)
			mr.Delete("/{id}", s.handleDeleteMemory)
		})

		// Risk
		v1.Route("/risk", func(rr chi.Router) {
			rr.Get("/status", s.handleRiskStatus)
			rr.Get("/cockpit", s.handleRiskCockpit)
			rr.Get("/breakers", s.handleRiskBreakerList)
			rr.Post("/killswitch", s.handleKillSwitchToggle)
			rr.Post("/breaker/reset", func(w http.ResponseWriter, r *http.Request) {
				s.requireAdmin(http.HandlerFunc(s.handleRiskBreakerReset)).ServeHTTP(w, r)
			})
			rr.Post("/market/{type}/stop", s.handleMarketKillSwitch)
			rr.Post("/market/{type}/resume", s.handleMarketKillSwitch)
		})

		// Settings
		v1.Route("/settings", func(sr chi.Router) {
			sr.Get("/", s.handleGetSettings)
			sr.Put("/", s.handleUpdateSettings)
		})

		// Prompts
		v1.Route("/prompts", func(pr chi.Router) {
			pr.Get("/", s.handleGetPrompts)
			pr.Put("/", s.handleUpdatePrompts)
		})

		// Events
		v1.Get("/events", s.handleListEvents)

		// Conversations
		v1.Route("/conversations", func(cr chi.Router) {
			cr.Get("/", s.handleListConversations)
			cr.Post("/", s.handleCreateConversation)
			cr.Get("/{id}/messages", s.handleGetConversationMessages)
			cr.Post("/{id}/messages", s.handleCreateConversationMessage)
		})

		// Audit log
		v1.Get("/audit-log", s.handleListAuditLog)

		// Options
		v1.Route("/options", func(or chi.Router) {
			or.Get("/chain/{underlying}", s.handleGetOptionsChain)
			or.Get("/contracts/{symbol}/bars", s.handleGetOptionsContractBars)
		})

		// Research scanners
		v1.Route("/research", func(rr chi.Router) {
			rr.Get("/options/opportunities/{underlying}", s.handleGetResearchOptionsOpportunities)
			rr.Get("/polymarket/opportunities", s.handleGetResearchPolymarketOpportunities)
		})

		// Calendar
		v1.Route("/calendar", func(cr chi.Router) {
			cr.Get("/earnings", s.handleGetEarningsCalendar)
			cr.Get("/economic", s.handleGetEconomicCalendar)
			cr.Get("/filings", s.handleGetFilings)
			cr.Post("/filings/analyze", s.handleAnalyzeFiling)
			cr.Get("/ipo", s.handleGetIPOCalendar)
		})

		// Backtests
		v1.Get("/backtest/divergence", s.handleGetBacktestDivergence)
		v1.Route("/backtests", func(bt chi.Router) {
			bt.Get("/divergence", s.handleGetBacktestDivergence)
			bt.Route("/configs", func(cr chi.Router) {
				cr.Get("/", s.handleListBacktestConfigs)
				cr.Post("/", s.handleCreateBacktestConfig)
				cr.Get("/{id}", s.handleGetBacktestConfig)
				cr.Put("/{id}", s.handleUpdateBacktestConfig)
				cr.Delete("/{id}", s.handleDeleteBacktestConfig)
				cr.Post("/{id}/run", s.handleRunBacktestConfig)
			})
			bt.Route("/runs", func(rr chi.Router) {
				rr.Get("/", s.handleListBacktestRuns)
				rr.Get("/{id}", s.handleGetBacktestRun)
			})
		})

		// Discovery
		v1.Route("/discovery", func(dr chi.Router) {
			dr.Post("/run", s.handleRunDiscovery)
			dr.Get("/results", s.handleListDiscoveryRuns)
		})

		// Universe
		v1.Route("/universe", func(ur chi.Router) {
			ur.Get("/", s.handleListUniverse)
			ur.Get("/watchlist", s.handleGetWatchlist)
			ur.Post("/refresh", s.handleRefreshUniverse)
			ur.Post("/scan", s.handleRunPreMarketScan)
		})

		// Automation
		v1.Route("/automation", func(ar chi.Router) {
			ar.Get("/status", s.handleGetAutomationStatus)
			ar.Get("/health", s.handleGetAutomationHealth)
			ar.Get("/runs", s.handleListAutomationRuns)
			ar.Get("/alpaca/verify", s.handleVerifyAlpacaReconcile)
			ar.Post("/alpaca/reconcile", s.handleRunAlpacaReconcile)
			ar.Post("/jobs/{name}/run", s.handleRunAutomationJob)
			ar.Post("/jobs/{name}/enable", s.handleSetAutomationJobEnabled)
		})

		// Current user profile and account management
		v1.Get("/me", s.handleGetCurrentUser)
		v1.Patch("/me", s.handleUpdateMe)

		// API key management
		v1.Route("/api-keys", func(ak chi.Router) {
			ak.Get("/", s.handleListAPIKeys)
			ak.Post("/", s.handleCreateAPIKey)
			ak.Delete("/{id}", s.handleRevokeAPIKey)
		})

		v1.Get("/market/ohlcv/{ticker}", s.handleGetHistoricalOHLCV)
		v1.Get("/social/sentiment/{ticker}", s.handleGetSocialSentiment)
		v1.Get("/news", s.handleListNews)

		// Signal intelligence
		v1.Route("/signals", func(sr chi.Router) {
			sr.Get("/evaluated", s.handleListSignalEvents)
			sr.Get("/triggers", s.handleListTriggerLog)
			sr.Get("/watchlist", s.handleListWatchTerms)
			sr.Post("/watchlist", s.handleAddWatchTerm)
			sr.Delete("/watchlist/{term}", s.handleDeleteWatchTerm)
		})
	})

	s.router = r
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	return s, nil
}

// Router returns the underlying chi.Router. Useful for testing.
func (s *Server) Router() http.Handler {
	return s.router
}

// Start begins listening for HTTP requests. It blocks until the server is
// stopped or encounters an error.
func (s *Server) Start() error {
	go s.hub.Run()
	s.logger.Info("api server starting", slog.String("addr", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.hub.Stop()
		return fmt.Errorf("api server: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("api server shutting down")
	s.hub.Stop()
	return s.httpServer.Shutdown(ctx)
}

// Hub returns the WebSocket hub for broadcasting events.
func (s *Server) Hub() *Hub {
	return s.hub
}

// BroadcastRunResult sends WebSocket events for the result of a strategy run.
// It is safe to call from goroutines outside the API package (e.g. the scheduler).
func (s *Server) BroadcastRunResult(result *StrategyRunResult) {
	if s.hub == nil || result == nil {
		return
	}

	run := result.Run
	s.hub.Broadcast(WSMessage{
		Type:       EventPipelineStart,
		StrategyID: run.StrategyID,
		RunID:      run.ID,
		Data: map[string]any{
			"status": domain.PipelineStatusRunning,
		},
		Timestamp: time.Now().UTC(),
	})

	if result.Signal != "" {
		s.hub.Broadcast(WSMessage{
			Type:       EventSignal,
			StrategyID: run.StrategyID,
			RunID:      run.ID,
			Data: map[string]any{
				"signal": result.Signal,
			},
			Timestamp: time.Now().UTC(),
		})
	}

	for _, order := range result.Orders {
		s.hub.Broadcast(WSMessage{
			Type:       EventOrderSubmitted,
			StrategyID: run.StrategyID,
			RunID:      run.ID,
			Data:       order,
			Timestamp:  time.Now().UTC(),
		})
	}

	for _, position := range result.Positions {
		s.hub.Broadcast(WSMessage{
			Type:       EventPositionUpdate,
			StrategyID: run.StrategyID,
			RunID:      run.ID,
			Data:       position,
			Timestamp:  time.Now().UTC(),
		})
	}
}

type healthStatusResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Redis  string `json:"redis"`
}

var healthCheckTimeout = 2 * time.Second

type healthCheckResult struct {
	dependency string
	err        error
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthStatusResponse{
		Status: "ok",
		DB:     "ok",
		Redis:  "ok",
	}
	statusCode := http.StatusOK

	checkCtx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	results := make(chan healthCheckResult, 2)
	go s.runDependencyHealthCheck(checkCtx, "db", s.dbHealth, results)
	go s.runDependencyHealthCheck(checkCtx, "redis", s.redisHealth, results)

	for range 2 {
		result := <-results
		if result.err == nil {
			continue
		}

		resp.Status = "degraded"
		statusCode = http.StatusServiceUnavailable
		switch result.dependency {
		case "db":
			resp.DB = "error"
		case "redis":
			resp.Redis = "error"
		}
		s.logger.Info("health check failed", "dependency", result.dependency, "error", result.err)
	}

	respondJSON(w, statusCode, resp)
}

func (s *Server) runDependencyHealthCheck(ctx context.Context, dependency string, check HealthCheck, results chan<- healthCheckResult) {
	results <- healthCheckResult{
		dependency: dependency,
		err:        s.runHealthCheck(ctx, check),
	}
}

func (s *Server) runHealthCheck(ctx context.Context, check HealthCheck) error {
	return check.Check(ctx)
}

// handleMetrics serves Prometheus metrics. Falls back to a placeholder if no
// metrics handler is configured.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metricsHandler != nil {
		s.metricsHandler.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("# metrics placeholder\n"))
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if isGuestObservationRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		result, err := s.auth.AuthenticateRequest(r)
		if err != nil {
			respondError(w, http.StatusUnauthorized, "authentication required", ErrCodeUnauthorized)
			return
		}

		if result.APIKey != nil && !s.auth.keyLimiter.Allow(result.APIKey.ID.String(), s.auth.rateLimitForWindow(result.APIKey.RateLimitPerMinute)) {
			respondError(w, http.StatusTooManyRequests, "rate limit exceeded", ErrCodeRateLimited)
			return
		}

		ctx := context.WithValue(r.Context(), authPrincipalContextKey, result.Principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isGuestObservationRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}

	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "/api/v1/news" ||
		strings.HasPrefix(path, "/api/v1/market/ohlcv") ||
		strings.HasPrefix(path, "/api/v1/social/sentiment") ||
		path == "/api/v1/calendar/earnings" ||
		path == "/api/v1/calendar/economic" ||
		path == "/api/v1/calendar/filings" ||
		path == "/api/v1/calendar/ipo" ||
		path == "/api/v1/universe" ||
		path == "/api/v1/universe/watchlist" {
		return true
	}

	for _, prefix := range []string{
		"/api/v1/options/chain",
		"/api/v1/options/contracts",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	return false
}
