package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	"github.com/PatrickFanella/get-rich-quick/internal/notification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewAPIServerSchemaBehindFailsFast(t *testing.T) {

	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
	}()

	var closed atomic.Bool
	var proceeded atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion - 1, nil
	}
	runtimeAfterSchemaGate = func() { proceeded.Store(true) }
	runtimeCloseDB = func(*pgrepo.DB) { closed.Store(true) }

	_, _, _, err := newAPIServer(context.Background(), config.Config{}, slogDiscardLogger())
	if err == nil {
		t.Fatal("newAPIServer() error = nil, want schema mismatch")
	}
	var mismatchErr *runtimeSchemaVersionError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("newAPIServer() error = %T, want *runtimeSchemaVersionError", err)
	}
	if mismatchErr.State != "behind" {
		t.Fatalf("mismatchErr.State = %q, want behind", mismatchErr.State)
	}
	if mismatchErr.Current != pgrepo.RequiredSchemaVersion-1 {
		t.Fatalf("mismatchErr.Current = %d, want %d", mismatchErr.Current, pgrepo.RequiredSchemaVersion-1)
	}
	if mismatchErr.Required != pgrepo.RequiredSchemaVersion {
		t.Fatalf("mismatchErr.Required = %d, want %d", mismatchErr.Required, pgrepo.RequiredSchemaVersion)
	}
	for _, want := range []string{"current version 29", "required version 30", "run migrations, then restart the process", "fresh process restart"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
	if proceeded.Load() {
		t.Fatal("runtime proceeded past schema gate on behind schema")
	}
	if !closed.Load() {
		t.Fatal("runtime did not close db on schema mismatch")
	}
}

func TestNewAPIServerSchemaAheadFailsFast(t *testing.T) {

	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
	}()

	var closed atomic.Bool
	var proceeded atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion + 1, nil
	}
	runtimeAfterSchemaGate = func() { proceeded.Store(true) }
	runtimeCloseDB = func(*pgrepo.DB) { closed.Store(true) }

	_, _, _, err := newAPIServer(context.Background(), config.Config{}, slogDiscardLogger())
	if err == nil {
		t.Fatal("newAPIServer() error = nil, want schema mismatch")
	}
	var mismatchErr *runtimeSchemaVersionError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("newAPIServer() error = %T, want *runtimeSchemaVersionError", err)
	}
	if mismatchErr.State != "ahead" {
		t.Fatalf("mismatchErr.State = %q, want ahead", mismatchErr.State)
	}
	if mismatchErr.Current != pgrepo.RequiredSchemaVersion+1 {
		t.Fatalf("mismatchErr.Current = %d, want %d", mismatchErr.Current, pgrepo.RequiredSchemaVersion+1)
	}
	if mismatchErr.Required != pgrepo.RequiredSchemaVersion {
		t.Fatalf("mismatchErr.Required = %d, want %d", mismatchErr.Required, pgrepo.RequiredSchemaVersion)
	}
	for _, want := range []string{"current version 31", "required version 30", "run migrations, then restart the process", "fresh process restart"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
	if proceeded.Load() {
		t.Fatal("runtime proceeded past schema gate on ahead schema")
	}
	if !closed.Load() {
		t.Fatal("runtime did not close db on schema mismatch")
	}
}

func TestNewAPIServerSchemaMatchSucceeds(t *testing.T) {

	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	origNewServer := runtimeNewServer
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
		runtimeNewServer = origNewServer
	}()

	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	var proceeded atomic.Bool
	var closed atomic.Bool
	var serverBuilt atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{Pool: pool}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion, nil
	}
	runtimeAfterSchemaGate = func() { proceeded.Store(true) }
	runtimeCloseDB = func(*pgrepo.DB) { closed.Store(true) }
	runtimeNewServer = func(api.ServerConfig, api.Deps, *slog.Logger) (*api.Server, error) {
		serverBuilt.Store(true)
		return &api.Server{}, nil
	}

	server, sched, cleanup, err := newAPIServer(context.Background(), config.Config{}, slogDiscardLogger())
	if err != nil {
		t.Fatalf("newAPIServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("newAPIServer() server = nil, want non-nil")
	}
	if sched != nil {
		t.Fatalf("newAPIServer() scheduler = %v, want nil when scheduler disabled", sched)
	}
	if cleanup == nil {
		t.Fatal("newAPIServer() cleanup = nil, want non-nil")
	}
	if !proceeded.Load() {
		t.Fatal("runtime did not proceed past schema gate on matching schema")
	}
	if !serverBuilt.Load() {
		t.Fatal("runtime did not continue to server construction on matching schema")
	}
	if closed.Load() {
		t.Fatal("runtime closed db before cleanup on matching schema")
	}

	cleanup()
	if !closed.Load() {
		t.Fatal("runtime cleanup did not close db on matching schema")
	}
}

func TestNewAPIServerSchemaDBUnreachableFailsBeforeSchemaGate(t *testing.T) {

	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
	}()

	startupErr := errors.New("postgres: ping database: dial tcp 127.0.0.1:5432: connect: connection refused")
	var schemaVersionChecked atomic.Bool
	var proceeded atomic.Bool
	var closed atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return nil, startupErr
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		schemaVersionChecked.Store(true)
		return pgrepo.RequiredSchemaVersion, nil
	}
	runtimeAfterSchemaGate = func() { proceeded.Store(true) }
	runtimeCloseDB = func(*pgrepo.DB) { closed.Store(true) }

	_, _, _, err := newAPIServer(context.Background(), config.Config{}, slogDiscardLogger())
	if !errors.Is(err, startupErr) {
		t.Fatalf("newAPIServer() error = %v, want %v", err, startupErr)
	}
	if schemaVersionChecked.Load() {
		t.Fatal("runtime checked schema version after DB startup failure")
	}
	if proceeded.Load() {
		t.Fatal("runtime proceeded past schema gate after DB startup failure")
	}
	if closed.Load() {
		t.Fatal("runtime closed db on DB startup failure before a db handle existed")
	}
}

func TestNewNotificationManager_DiscordAlertDispatch(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			Discord: config.DiscordNotificationConfig{
				AlertWebhookURL: server.URL,
			},
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelDiscord}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err != nil {
		t.Fatalf("RecordKillSwitchToggle() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("discord requests = %d, want 1", requests.Load())
	}
}

func TestNewNotificationManager_N8NAlertDispatch(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			N8N: config.WebhookNotificationConfig{
				URL: server.URL,
			},
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelN8N}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err != nil {
		t.Fatalf("RecordKillSwitchToggle() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("n8n requests = %d, want 1", requests.Load())
	}
}

func TestNewNotificationManager_N8NChannelNoopsWhenUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelN8N}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err != nil {
		t.Fatalf("RecordKillSwitchToggle() error = %v, want nil", err)
	}
}

func TestNewNotificationManager_SkipsDiscordWhenUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelDiscord}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err == nil {
		t.Fatal("RecordKillSwitchToggle() error = nil, want missing discord notifier error")
	}
}

type stubDecisionRepo struct {
	decisions []domain.AgentDecision
}

type captureProvider struct{}

func (captureProvider) Complete(_ context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	content := ""
	if len(request.Messages) > 0 {
		content = request.Messages[0].Content
	}

	return &llm.CompletionResponse{
		Content: content,
		Model:   request.Model,
	}, nil
}

func (s *stubDecisionRepo) Create(context.Context, *domain.AgentDecision) error { return nil }

func (s *stubDecisionRepo) GetByRun(context.Context, uuid.UUID, repository.AgentDecisionFilter, int, int) ([]domain.AgentDecision, error) {
	return s.decisions, nil
}

func (s *stubDecisionRepo) CountByRun(_ context.Context, _ uuid.UUID, _ repository.AgentDecisionFilter) (int, error) {
	return len(s.decisions), nil
}

func TestSmokeStrategyRunnerDispatchNotifications_RoutesSignalAndDecisionsToN8NAndDiscord(t *testing.T) {
	t.Parallel()

	var n8nRequests atomic.Int32
	n8nServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n8nRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer n8nServer.Close()

	var signalRequests atomic.Int32
	signalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		signalRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer signalServer.Close()

	var decisionRequests atomic.Int32
	decisionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		decisionRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer decisionServer.Close()

	runner := &smokeStrategyRunner{
		decisionRepo: &stubDecisionRepo{decisions: []domain.AgentDecision{
			{AgentRole: domain.AgentRoleTrader, Phase: domain.PhaseTrading, OutputText: `{"action":"buy"}`, CreatedAt: time.Date(2026, 4, 2, 15, 0, 0, 0, time.UTC)},
			{AgentRole: domain.AgentRoleRiskManager, Phase: domain.PhaseRiskDebate, OutputText: `{"action":"buy","confidence":0.92}`, CreatedAt: time.Date(2026, 4, 2, 15, 1, 0, 0, time.UTC)},
		}},
		notificationManager: newNotificationManager(config.Config{
			Notifications: config.NotificationConfig{
				N8N: config.WebhookNotificationConfig{
					URL: n8nServer.URL,
				},
				Discord: config.DiscordNotificationConfig{
					SignalWebhookURL:   signalServer.URL,
					DecisionWebhookURL: decisionServer.URL,
				},
			},
		}),
	}

	runID := uuid.New()
	strategy := domain.Strategy{ID: uuid.New(), Name: "Momentum", Ticker: "AAPL"}
	state := &agent.PipelineState{
		TradingPlan: agent.TradingPlan{Ticker: "AAPL", Rationale: "Breakout confirmed."},
		FinalSignal: agent.FinalSignal{Signal: domain.PipelineSignalBuy, Confidence: 0.92},
	}
	completedAt := time.Date(2026, 4, 2, 15, 2, 0, 0, time.UTC)

	if err := runner.dispatchNotifications(context.Background(), strategy, &domain.PipelineRun{ID: runID, CompletedAt: &completedAt}, state); err != nil {
		t.Fatalf("dispatchNotifications() error = %v", err)
	}

	if n8nRequests.Load() != 3 {
		t.Fatalf("n8n requests = %d, want 3", n8nRequests.Load())
	}
	if signalRequests.Load() != 1 {
		t.Fatalf("signal requests = %d, want 1", signalRequests.Load())
	}
	if decisionRequests.Load() != 2 {
		t.Fatalf("decision requests = %d, want 2", decisionRequests.Load())
	}
}

type stubMarketDataService struct {
	ohlcv        []domain.OHLCV
	fundamentals data.Fundamentals
	news         []data.NewsArticle
	social       []data.SocialSentiment
	errOHLCV     error
	errFund      error
	errNews      error
	errSocial    error
}

func (s *stubMarketDataService) GetOHLCV(context.Context, domain.MarketType, string, data.Timeframe, time.Time, time.Time) ([]domain.OHLCV, error) {
	if s.errOHLCV != nil {
		return nil, s.errOHLCV
	}
	return s.ohlcv, nil
}

func (s *stubMarketDataService) GetFundamentals(context.Context, domain.MarketType, string) (data.Fundamentals, error) {
	if s.errFund != nil {
		return data.Fundamentals{}, s.errFund
	}
	return s.fundamentals, nil
}

func (s *stubMarketDataService) GetNews(context.Context, domain.MarketType, string, time.Time, time.Time) ([]data.NewsArticle, error) {
	if s.errNews != nil {
		return nil, s.errNews
	}
	return s.news, nil
}

func (s *stubMarketDataService) GetSocialSentiment(context.Context, domain.MarketType, string, time.Time, time.Time) ([]data.SocialSentiment, error) {
	if s.errSocial != nil {
		return nil, s.errSocial
	}
	return s.social, nil
}

type stubPositionRepo struct{}

func (stubPositionRepo) Create(context.Context, *domain.Position) error { return nil }
func (stubPositionRepo) Get(_ context.Context, _ uuid.UUID) (*domain.Position, error) {
	return nil, repository.ErrNotFound
}

func (stubPositionRepo) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (stubPositionRepo) Update(context.Context, *domain.Position) error { return nil }
func (stubPositionRepo) Delete(context.Context, uuid.UUID) error        { return nil }
func (stubPositionRepo) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (stubPositionRepo) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (stubPositionRepo) Count(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}

func (stubPositionRepo) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}

type metricPositionRepo struct{ count int }

func (m metricPositionRepo) Create(context.Context, *domain.Position) error { return nil }
func (m metricPositionRepo) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	return nil, repository.ErrNotFound
}
func (m metricPositionRepo) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (m metricPositionRepo) Update(context.Context, *domain.Position) error { return nil }
func (m metricPositionRepo) Delete(context.Context, uuid.UUID) error        { return nil }
func (m metricPositionRepo) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (m metricPositionRepo) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (m metricPositionRepo) Count(context.Context, repository.PositionFilter) (int, error) {
	return m.count, nil
}
func (m metricPositionRepo) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return m.count, nil
}

func TestSelectedAnalysisRoles_RejectsNonAnalysisRoles(t *testing.T) {
	t.Parallel()

	_, err := selectedAnalysisRoles([]agent.AgentRole{agent.AgentRoleTrader})
	if err == nil {
		t.Fatal("selectedAnalysisRoles() error = nil, want invalid role error")
	}
}

func TestBuildAnalysisAgents_RespectsAnalystSelection(t *testing.T) {
	t.Parallel()

	resolved := agent.ResolvedConfig{
		LLMConfig: agent.ResolvedLLMConfig{QuickThinkModel: "gpt-5-mini"},
		AnalystSelection: []agent.AgentRole{
			agent.AgentRoleNewsAnalyst,
			agent.AgentRoleMarketAnalyst,
		},
	}

	agents, err := buildAnalysisAgents(nil, "openai", resolved, nil, nil)
	if err != nil {
		t.Fatalf("buildAnalysisAgents() error = %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("len(agents) = %d, want 2", len(agents))
	}
	if got := agents[0].Role(); got != agent.AgentRoleMarketAnalyst {
		t.Fatalf("agents[0].Role() = %s, want %s", got, agent.AgentRoleMarketAnalyst)
	}
	if got := agents[1].Role(); got != agent.AgentRoleNewsAnalyst {
		t.Fatalf("agents[1].Role() = %s, want %s", got, agent.AgentRoleNewsAnalyst)
	}
}

func TestRealStrategyRunnerLoadInitialState_PopulatesSeededInputs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	runner := &realStrategyRunner{
		dataService: &stubMarketDataService{
			ohlcv: []domain.OHLCV{
				{Timestamp: now.Add(-24 * time.Hour), Open: 100, High: 105, Low: 99, Close: 104, Volume: 1000},
				{Timestamp: now, Open: 104, High: 109, Low: 103, Close: 108, Volume: 1200},
			},
			fundamentals: data.Fundamentals{Ticker: "AAPL", MarketCap: 3_000_000_000_000, FetchedAt: now},
			news:         []data.NewsArticle{{Title: "AAPL beats", PublishedAt: now, Sentiment: 0.8}},
			social: []data.SocialSentiment{
				{Ticker: "AAPL", Score: 0.2, MeasuredAt: now.Add(-2 * time.Hour)},
				{Ticker: "AAPL", Score: 0.9, MeasuredAt: now.Add(-1 * time.Hour)},
			},
		},
		logger: slogDiscardLogger(),
	}

	seed, err := runner.loadInitialState(context.Background(), domain.Strategy{Ticker: "AAPL", MarketType: domain.MarketTypeStock})
	if err != nil {
		t.Fatalf("loadInitialState() error = %v", err)
	}
	if seed.Market == nil || len(seed.Market.Bars) != 2 {
		t.Fatalf("seed.Market = %+v, want two bars", seed.Market)
	}
	if len(seed.Market.Indicators) == 0 {
		t.Fatal("seed.Market.Indicators is empty, want computed indicators")
	}
	if seed.Fundamentals == nil || seed.Fundamentals.Ticker != "AAPL" {
		t.Fatalf("seed.Fundamentals = %+v, want AAPL fundamentals", seed.Fundamentals)
	}
	if len(seed.News) != 1 || seed.News[0].Title != "AAPL beats" {
		t.Fatalf("seed.News = %+v, want seeded news", seed.News)
	}
	if seed.Social == nil || seed.Social.Score != 0.9 {
		t.Fatalf("seed.Social = %+v, want latest social snapshot", seed.Social)
	}
	if seed.Market.Indicators[0].Timestamp != now {
		t.Fatalf("indicator timestamp = %s, want %s", seed.Market.Indicators[0].Timestamp, now)
	}
}

func TestRealStrategyRunnerNewBrokerForStrategy_ReusesFallbackPaperBroker(t *testing.T) {
	t.Parallel()

	runner := &realStrategyRunner{logger: slogDiscardLogger()}
	strategy := domain.Strategy{
		ID:         uuid.New(),
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		IsPaper:    true,
	}

	first, firstName, err := runner.newBrokerForStrategy(strategy)
	if err != nil {
		t.Fatalf("newBrokerForStrategy(first) error = %v", err)
	}
	second, secondName, err := runner.newBrokerForStrategy(strategy)
	if err != nil {
		t.Fatalf("newBrokerForStrategy(second) error = %v", err)
	}
	if firstName != "paper" || secondName != "paper" {
		t.Fatalf("broker names = (%q, %q), want (paper, paper)", firstName, secondName)
	}

	firstPaper, ok := first.(*paper.PaperBroker)
	if !ok {
		t.Fatalf("first broker type = %T, want *paper.PaperBroker", first)
	}
	secondPaper, ok := second.(*paper.PaperBroker)
	if !ok {
		t.Fatalf("second broker type = %T, want *paper.PaperBroker", second)
	}
	if firstPaper != secondPaper {
		t.Fatal("fallback paper broker was recreated, want shared broker instance")
	}
}

func TestRealStrategyRunnerNewOrderManager_WiresRiskPortfolioSnapshot(t *testing.T) {
	t.Parallel()

	positionRepo := stubPositionRepo{}
	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), risk.DefaultCircuitBreakerConfig(), positionRepo, slogDiscardLogger())
	runner := &realStrategyRunner{
		positionRepo: positionRepo,
		riskEngine:   engine,
		logger:       slogDiscardLogger(),
	}

	_, err := runner.newOrderManager(
		domain.Strategy{ID: uuid.New(), Ticker: "AAPL", MarketType: domain.MarketTypeStock, IsPaper: true},
		agent.ResolvedConfig{RiskConfig: agent.ResolvedRiskConfig{PositionSizePct: 10}},
	)
	if err != nil {
		t.Fatalf("newOrderManager() error = %v", err)
	}

	status, err := engine.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if status.PositionLimits.CurrentOpenPositions == nil || *status.PositionLimits.CurrentOpenPositions != 0 {
		t.Fatalf("CurrentOpenPositions = %+v, want pointer to 0", status.PositionLimits.CurrentOpenPositions)
	}
	if status.PositionLimits.CurrentTotalExposurePct == nil || *status.PositionLimits.CurrentTotalExposurePct != 0 {
		t.Fatalf("CurrentTotalExposurePct = %+v, want pointer to 0", status.PositionLimits.CurrentTotalExposurePct)
	}
}

func TestRealStrategyRunnerExecutionMetricsHelpers(t *testing.T) {
	t.Parallel()

	positionRepo := metricPositionRepo{count: 2}
	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), risk.DefaultCircuitBreakerConfig(), positionRepo, slogDiscardLogger())
	if err := engine.ActivateKillSwitch(context.Background(), "test"); err != nil {
		t.Fatalf("ActivateKillSwitch() error = %v", err)
	}
	if err := engine.TripCircuitBreaker(context.Background(), "trip"); err != nil {
		t.Fatalf("TripCircuitBreaker() error = %v", err)
	}
	m := metrics.New()
	runner := &realStrategyRunner{positionRepo: positionRepo, riskEngine: engine, metrics: m}
	completedAt := time.Date(2026, 4, 11, 12, 30, 0, 0, time.UTC)
	runner.recordPipelineMetrics(domain.PipelineRun{
		Ticker:      "AAPL",
		Signal:      domain.PipelineSignalBuy,
		Status:      domain.PipelineStatusCompleted,
		StartedAt:   completedAt.Add(-2 * time.Minute),
		CompletedAt: &completedAt,
	})
	runner.refreshExecutionMetrics(context.Background())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"tradingagent_pipeline_runs_total", "ticker=\"AAPL\"", "tradingagent_pipeline_duration_seconds", "tradingagent_positions_open 2", "tradingagent_circuit_breaker_state 1", "tradingagent_kill_switch_active 1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics output missing %q", want)
		}
	}
}

func TestBuildRunnerDefinition_AppliesPromptOverridesBeyondAnalysis(t *testing.T) {
	t.Parallel()

	resolved := agent.ResolvedConfig{
		LLMConfig: agent.ResolvedLLMConfig{
			QuickThinkModel: "gpt-5-mini",
			DeepThinkModel:  "gpt-5",
		},
		PromptOverrides: map[agent.AgentRole]string{
			agent.AgentRoleBullResearcher:      "custom bull prompt",
			agent.AgentRoleBearResearcher:      "custom bear prompt",
			agent.AgentRoleInvestJudge:         "custom invest judge prompt",
			agent.AgentRoleTrader:              "custom trader prompt",
			agent.AgentRoleAggressiveAnalyst:   "custom aggressive prompt",
			agent.AgentRoleConservativeAnalyst: "custom conservative prompt",
			agent.AgentRoleNeutralAnalyst:      "custom neutral prompt",
			agent.AgentRoleRiskManager:         "custom risk manager prompt",
		},
	}

	definition, err := buildRunnerDefinition(captureProvider{}, "openai", resolved, 30*time.Second, nil, slogDiscardLogger())
	if err != nil {
		t.Fatalf("buildRunnerDefinition() error = %v", err)
	}

	assertPromptContains := func(label, got, want string) {
		t.Helper()
		if !strings.Contains(got, want) {
			t.Fatalf("%s prompt = %q, want substring %q", label, got, want)
		}
	}

	bullOut, err := definition.Research.Debaters[0].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("bull Debate() error = %v", err)
	}
	assertPromptContains("bull", bullOut.LLMResponse.PromptText, "custom bull prompt")

	bearOut, err := definition.Research.Debaters[1].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("bear Debate() error = %v", err)
	}
	assertPromptContains("bear", bearOut.LLMResponse.PromptText, "custom bear prompt")

	judgeOut, err := definition.Research.Judge.JudgeResearch(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("JudgeResearch() error = %v", err)
	}
	assertPromptContains("invest_judge", judgeOut.LLMResponse.PromptText, "custom invest judge prompt")

	traderOut, err := definition.Trader.Trade(context.Background(), agent.TradingInput{Ticker: "AAPL", InvestmentPlan: `{"direction":"buy"}`})
	if err != nil {
		t.Fatalf("Trader.Trade() error = %v", err)
	}
	assertPromptContains("trader", traderOut.LLMResponse.PromptText, "custom trader prompt")

	aggressiveOut, err := definition.Risk.Debaters[0].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("aggressive Debate() error = %v", err)
	}
	assertPromptContains("aggressive", aggressiveOut.LLMResponse.PromptText, "custom aggressive prompt")

	conservativeOut, err := definition.Risk.Debaters[1].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("conservative Debate() error = %v", err)
	}
	assertPromptContains("conservative", conservativeOut.LLMResponse.PromptText, "custom conservative prompt")

	neutralOut, err := definition.Risk.Debaters[2].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("neutral Debate() error = %v", err)
	}
	assertPromptContains("neutral", neutralOut.LLMResponse.PromptText, "custom neutral prompt")

	riskOut, err := definition.Risk.Judge.JudgeRisk(context.Background(), agent.RiskJudgeInput{Ticker: "AAPL", TradingPlan: agent.TradingPlan{Ticker: "AAPL"}})
	if err != nil {
		t.Fatalf("JudgeRisk() error = %v", err)
	}
	assertPromptContains("risk_manager", riskOut.LLMResponse.PromptText, "custom risk manager prompt")
}

func TestNewLLMProviderForSelection_SupportsOpenRouterAndXAI(t *testing.T) {
	t.Parallel()

	baseCfg := config.LLMConfig{Providers: config.LLMProviderConfigs{OpenRouter: config.LLMProviderConfig{APIKey: "openrouter-key", BaseURL: "https://openrouter.example/v1", Model: "openai/gpt-4.1-mini"}, XAI: config.LLMProviderConfig{APIKey: "xai-key", BaseURL: "https://api.x.ai/v1", Model: "grok-3-mini"}}}

	if provider, err := newLLMProviderForSelection(baseCfg, "openrouter", "", nil); err != nil || provider == nil {
		t.Fatalf("newLLMProviderForSelection(openrouter) = (%v, %v), want non-nil provider", provider, err)
	}
	if provider, err := newLLMProviderForSelection(baseCfg, "xai", "", nil); err != nil || provider == nil {
		t.Fatalf("newLLMProviderForSelection(xai) = (%v, %v), want non-nil provider", provider, err)
	}
}

func TestLLMCacheEnabled(t *testing.T) {
	t.Setenv("LLM_CACHE_ENABLED", "")
	if !llmCacheEnabled() {
		t.Fatal("llmCacheEnabled() = false, want true when unset")
	}

	t.Setenv("LLM_CACHE_ENABLED", "true")
	if !llmCacheEnabled() {
		t.Fatal("llmCacheEnabled() = false, want true when env=true")
	}

	t.Setenv("LLM_CACHE_ENABLED", "false")
	if llmCacheEnabled() {
		t.Fatal("llmCacheEnabled() = true, want false when env=false")
	}
}

func TestBuildProviderChain_PrimaryOnly(t *testing.T) {
	cfg := config.LLMConfig{
		DefaultProvider:     "openai",
		ThrottleConcurrency: 2,
		RetryMaxAttempts:    1, // no retry layer (needs >1)
		CallTimeout:         5 * time.Minute,
		Providers: config.LLMProviderConfigs{
			OpenAI: config.LLMProviderConfig{
				APIKey: "test-key",
				Model:  "gpt-5-mini",
			},
		},
	}

	// Cache enabled by default (env unset).
	t.Setenv("LLM_CACHE_ENABLED", "true")

	provider := buildProviderChain(cfg, metrics.New(), slogDiscardLogger(), buildLLMBudget(cfg))
	if provider == nil {
		t.Fatal("buildProviderChain() = nil, want non-nil provider")
	}
}

func TestBuildProviderChain_WithFallback(t *testing.T) {
	cfg := config.LLMConfig{
		DefaultProvider:     "openai",
		FallbackProvider:    "anthropic",
		FallbackModel:       "claude-sonnet-4-20250514",
		ThrottleConcurrency: 4,
		RetryMaxAttempts:    2,
		CallTimeout:         5 * time.Minute,
		Providers: config.LLMProviderConfigs{
			OpenAI: config.LLMProviderConfig{
				APIKey: "test-openai-key",
				Model:  "gpt-5-mini",
			},
			Anthropic: config.LLMProviderConfig{
				APIKey: "test-anthropic-key",
				Model:  "claude-sonnet-4-20250514",
			},
		},
	}

	t.Setenv("LLM_CACHE_ENABLED", "false")

	provider := buildProviderChain(cfg, metrics.New(), slogDiscardLogger(), buildLLMBudget(cfg))
	if provider == nil {
		t.Fatal("buildProviderChain() = nil, want non-nil provider with fallback")
	}
}

func TestBuildProviderChain_NilWhenNoProvider(t *testing.T) {
	t.Parallel()

	cfg := config.LLMConfig{} // no provider configured
	provider := buildProviderChain(cfg, metrics.New(), slogDiscardLogger(), buildLLMBudget(cfg))
	if provider != nil {
		t.Fatalf("buildProviderChain() = %v, want nil when no provider configured", provider)
	}
}

func TestBuildProviderChain_BudgetExhausted(t *testing.T) {
	t.Parallel()

	// Build a chain with captureProvider + budget of 1 request.
	budget := llm.NewBudget(1, 0)
	chain := llm.NewProviderChain(captureProvider{}, slogDiscardLogger(), llm.WithBudget(budget))

	// First call succeeds.
	resp, err := chain.Complete(context.Background(), llm.CompletionRequest{
		Model:    "test",
		Messages: []llm.Message{{Role: "user", Content: "first"}},
	})
	if err != nil {
		t.Fatalf("first Complete() error = %v, want nil", err)
	}
	if resp == nil {
		t.Fatal("first Complete() response = nil, want non-nil")
	}

	// Record usage so budget is consumed.
	budget.Record(10, 20)

	// Second call should be rejected by budget guard.
	_, err = chain.Complete(context.Background(), llm.CompletionRequest{
		Model:    "test",
		Messages: []llm.Message{{Role: "user", Content: "second"}},
	})
	if err == nil {
		t.Fatal("second Complete() error = nil, want ErrBudgetExhausted")
	}
	if !errors.Is(err, llm.ErrBudgetExhausted) {
		t.Fatalf("second Complete() error = %v, want ErrBudgetExhausted", err)
	}
}

func TestBuildProviderChain_InvalidFallbackSkipped(t *testing.T) {
	cfg := config.LLMConfig{
		DefaultProvider:     "openai",
		FallbackProvider:    "nonexistent-provider",
		ThrottleConcurrency: 1,
		Providers: config.LLMProviderConfigs{
			OpenAI: config.LLMProviderConfig{
				APIKey: "test-key",
				Model:  "gpt-5-mini",
			},
		},
	}

	t.Setenv("LLM_CACHE_ENABLED", "false")

	// Should not panic; invalid fallback is logged and skipped.
	provider := buildProviderChain(cfg, metrics.New(), slogDiscardLogger(), buildLLMBudget(cfg))
	if provider == nil {
		t.Fatal("buildProviderChain() = nil, want non-nil provider (fallback skipped)")
	}
}

func TestConfiguredPrimaryRetryProviderLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "trimmed provider", input: " openai ", expected: "configured_primary:openai"},
		{name: "empty provider", input: "   ", expected: "configured_primary:unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := configuredPrimaryRetryProviderLabel(tt.input); got != tt.expected {
				t.Fatalf("configuredPrimaryRetryProviderLabel(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func slogDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
