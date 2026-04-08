package api

import (
	"context"
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
	strategies    repository.StrategyRepository
	runs          repository.PipelineRunRepository
	decisions     repository.AgentDecisionRepository
	orders        repository.OrderRepository
	positions     repository.PositionRepository
	trades        repository.TradeRepository
	memories      repository.MemoryRepository
	users         repository.UserRepository
	auditLog      repository.AuditLogRepository
	conversations repository.ConversationRepository
	snapshots     repository.PipelineRunSnapshotRepository
	llmProvider   llm.Provider
	events        repository.AgentEventRepository

	// Backtest
	backtestConfigs repository.BacktestConfigRepository
	backtestRuns    repository.BacktestRunRepository
	dataService     *data.DataService

	// Discovery
	discoveryDeps    *discovery.DiscoveryDeps
	discoveryRunRepo discovery.RunRepository

	// Universe
	universe     *universe.Universe
	universeRepo universe.UniverseRepository

	// Automation
	automation *automation.JobOrchestrator

	// Risk engine
	risk     risk.RiskEngine
	settings SettingsService
	runner   StrategyRunner

	auth *AuthManager

	// Options data
	optionsProvider data.OptionsDataProvider

	// News feed
	newsFeedRepo *pgrepo.NewsFeedRepo

	// Calendar / events data
	eventsProvider data.EventsProvider

	// WebSocket hub for real-time event streaming.
	hub            *Hub
	wsUpgrader     websocket.Upgrader
	metricsHandler http.Handler

	// Signal intelligence (optional; nil = feature not running).
	signalStore *signal.EventStore
	watchIndex  *signal.WatchIndex
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
	Strategies     repository.StrategyRepository
	Runs           repository.PipelineRunRepository
	Decisions      repository.AgentDecisionRepository
	Orders         repository.OrderRepository
	Positions      repository.PositionRepository
	Trades         repository.TradeRepository
	Memories       repository.MemoryRepository
	APIKeys        repository.APIKeyRepository
	Users          repository.UserRepository
	Conversations  repository.ConversationRepository
	AuditLog       repository.AuditLogRepository
	Events          repository.AgentEventRepository
	Snapshots       repository.PipelineRunSnapshotRepository
	LLMProvider     llm.Provider
	BacktestConfigs  repository.BacktestConfigRepository
	BacktestRuns     repository.BacktestRunRepository
	DataService      *data.DataService
	OptionsProvider  data.OptionsDataProvider
	EventsProvider   data.EventsProvider
	DiscoveryDeps    *discovery.DiscoveryDeps
	DiscoveryRunRepo discovery.RunRepository
	Universe         *universe.Universe
	UniverseRepo     universe.UniverseRepository
	Automation       *automation.JobOrchestrator
	NewsFeedRepo     *pgrepo.NewsFeedRepo
	Risk            risk.RiskEngine
	Settings       SettingsService
	Runner         StrategyRunner
	DBHealth       HealthCheck
	RedisHealth    HealthCheck
	MetricsHandler http.Handler

	// Signal intelligence (optional; nil = feature not enabled).
	SignalStore *signal.EventStore
	WatchIndex  *signal.WatchIndex
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

	s := &Server{
		logger:         logger,
		dbHealth:       deps.DBHealth,
		redisHealth:    deps.RedisHealth,
		strategies:     deps.Strategies,
		runs:           deps.Runs,
		decisions:      deps.Decisions,
		orders:         deps.Orders,
		positions:      deps.Positions,
		trades:         deps.Trades,
		memories:       deps.Memories,
		users:          deps.Users,
		conversations:  deps.Conversations,
		snapshots:      deps.Snapshots,
		llmProvider:    deps.LLMProvider,
		auditLog:       deps.AuditLog,
		events:         deps.Events,
		backtestConfigs: deps.BacktestConfigs,
		backtestRuns:    deps.BacktestRuns,
		dataService:      deps.DataService,
		optionsProvider:   deps.OptionsProvider,
		eventsProvider:    deps.EventsProvider,
		discoveryDeps:    deps.DiscoveryDeps,
		discoveryRunRepo: deps.DiscoveryRunRepo,
		universe:         deps.Universe,
		universeRepo:     deps.UniverseRepo,
		automation:       deps.Automation,
		newsFeedRepo:     deps.NewsFeedRepo,
		risk:             deps.Risk,
		settings:        settingsService,
		runner:          deps.Runner,
		auth:           authManager,
		hub:            hub,
		wsUpgrader:     newUpgrader(cfg.CORSConfig.AllowedOrigins),
		metricsHandler: deps.MetricsHandler,
		signalStore:    deps.SignalStore,
		watchIndex:     deps.WatchIndex,
	}

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
		})

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
			rr.Post("/killswitch", s.handleKillSwitchToggle)
			rr.Post("/market/{type}/stop", s.handleMarketKillSwitch)
			rr.Post("/market/{type}/resume", s.handleMarketKillSwitch)
		})

		// Settings
		v1.Route("/settings", func(sr chi.Router) {
			sr.Get("/", s.handleGetSettings)
			sr.Put("/", s.handleUpdateSettings)
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

		// Calendar
		v1.Route("/calendar", func(cr chi.Router) {
			cr.Get("/earnings", s.handleGetEarningsCalendar)
			cr.Get("/economic", s.handleGetEconomicCalendar)
			cr.Get("/filings", s.handleGetFilings)
			cr.Post("/filings/analyze", s.handleAnalyzeFiling)
			cr.Get("/ipo", s.handleGetIPOCalendar)
		})

		// Backtests
		v1.Route("/backtests", func(bt chi.Router) {
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
			ar.Post("/jobs/{name}/run", s.handleRunAutomationJob)
			ar.Post("/jobs/{name}/enable", s.handleSetAutomationJobEnabled)
		})

		// Current user profile
		v1.Get("/me", s.handleGetCurrentUser)

		// API key management
		v1.Route("/api-keys", func(ak chi.Router) {
			ak.Get("/", s.handleListAPIKeys)
			ak.Post("/", s.handleCreateAPIKey)
			ak.Delete("/{id}", s.handleRevokeAPIKey)
		})

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
