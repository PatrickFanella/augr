package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T) *Server {
	return newTestServerWithDeps(t, testDeps())
}

func newTestServerWithDeps(t *testing.T, deps Deps) *Server {
	return newTestServerWithDepsAndLogger(t, deps, slog.Default())
}

func newTestServerWithDepsAndLogger(t *testing.T, deps Deps, logger *slog.Logger) *Server {
	t.Helper()

	cfg := DefaultServerConfig()
	cfg.JWTSecret = "test-jwt-secret"

	srv, err := NewServer(cfg, deps, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return srv
}

func testDeps() Deps {
	return Deps{
		Strategies: &stubStrategyRepo{
			items: map[uuid.UUID]domain.Strategy{
				stratA.ID: stratA,
				stratB.ID: stratB,
			},
		},
		Runs:        &stubRunRepo{},
		Decisions:   &stubDecisionRepo{},
		Orders:      &stubOrderRepo{},
		Positions:   &stubPositionRepo{},
		Trades:      &stubTradeRepo{},
		Memories:    &stubMemoryRepo{},
		APIKeys:     newStubAPIKeyRepo(),
		Users:       newStubUserRepo(),
		Risk:        &stubRiskEngine{},
		DBHealth:    &stubHealthCheck{},
		RedisHealth: &stubHealthCheck{},
	}
}

var (
	stratA = domain.Strategy{
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name:       "Alpha",
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	stratB = domain.Strategy{
		ID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:       "Beta",
		Ticker:     "MSFT",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusInactive,
		CreatedAt:  time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
	}
)

func doRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(path, "/api/v1") && method != http.MethodOptions {
		tokenPair, err := srv.auth.GenerateTokenPair("test-user")
		if err != nil {
			t.Fatalf("GenerateTokenPair() error = %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+tokenPair.AccessToken)
	}
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	return rr
}

func decodeJSON[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rr.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, rr.Body.String())
	}
	return v
}

func assertValidationError(t *testing.T, rr *httptest.ResponseRecorder, wantSubstrings ...string) {
	t.Helper()
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d\nbody: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}

	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeValidation {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeValidation)
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body.Error, want) {
			t.Fatalf("error = %q, want substring %q", body.Error, want)
		}
	}
}

func TestValidateStrategyConfigPayloadWrapsJSONError(t *testing.T) {
	t.Parallel()

	err := validateStrategyConfigPayload(domain.StrategyConfig(`{"llm_config":`))
	if err == nil {
		t.Fatal("validateStrategyConfigPayload() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "invalid config:") {
		t.Fatalf("error = %q, want prefix %q", err.Error(), "invalid config:")
	}

	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("errors.As(err, *json.SyntaxError) = false, want true; err = %v", err)
	}
}

func doUnauthenticatedRequest(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()
	deps := testDeps()
	dbHealth := &stubHealthCheck{}
	redisHealth := &stubHealthCheck{}
	deps.DBHealth = dbHealth
	deps.RedisHealth = redisHealth
	srv := newTestServerWithDeps(t, deps)

	for _, path := range []string{"/health", "/healthz"} {
		rr := doRequest(t, srv, http.MethodGet, path, nil)

		if rr.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rr.Code, http.StatusOK)
		}
		body := decodeJSON[map[string]string](t, rr)
		if body["status"] != "ok" {
			t.Fatalf("%s status = %q, want %q", path, body["status"], "ok")
		}
		if body["db"] != "ok" {
			t.Fatalf("%s db = %q, want %q", path, body["db"], "ok")
		}
		if body["redis"] != "ok" {
			t.Fatalf("%s redis = %q, want %q", path, body["redis"], "ok")
		}
	}

	if dbHealth.calls.Load() != 2 {
		t.Fatalf("db health calls = %d, want 2", dbHealth.calls.Load())
	}
	if redisHealth.calls.Load() != 2 {
		t.Fatalf("redis health calls = %d, want 2", redisHealth.calls.Load())
	}
}

func TestHealthEndpointDBDown(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	dbHealth := &stubHealthCheck{err: errors.New("db unavailable")}
	redisHealth := &stubHealthCheck{}
	deps.DBHealth = dbHealth
	deps.RedisHealth = redisHealth
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/healthz", nil)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	body := decodeJSON[map[string]string](t, rr)
	if body["status"] != "degraded" {
		t.Fatalf("status = %q, want %q", body["status"], "degraded")
	}
	if body["db"] != "error" {
		t.Fatalf("db = %q, want %q", body["db"], "error")
	}
	if body["redis"] != "ok" {
		t.Fatalf("redis = %q, want %q", body["redis"], "ok")
	}
	if dbHealth.calls.Load() != 1 {
		t.Fatalf("db health calls = %d, want 1", dbHealth.calls.Load())
	}
	if redisHealth.calls.Load() != 1 {
		t.Fatalf("redis health calls = %d, want 1", redisHealth.calls.Load())
	}
}

func TestHealthEndpointRedisDown(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	dbHealth := &stubHealthCheck{}
	redisHealth := &stubHealthCheck{err: errors.New("redis unavailable")}
	deps.DBHealth = dbHealth
	deps.RedisHealth = redisHealth
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/healthz", nil)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	body := decodeJSON[map[string]string](t, rr)
	if body["status"] != "degraded" {
		t.Fatalf("status = %q, want %q", body["status"], "degraded")
	}
	if body["db"] != "ok" {
		t.Fatalf("db = %q, want %q", body["db"], "ok")
	}
	if body["redis"] != "error" {
		t.Fatalf("redis = %q, want %q", body["redis"], "error")
	}
	if dbHealth.calls.Load() != 1 {
		t.Fatalf("db health calls = %d, want 1", dbHealth.calls.Load())
	}
	if redisHealth.calls.Load() != 1 {
		t.Fatalf("redis health calls = %d, want 1", redisHealth.calls.Load())
	}
}

func TestHealthEndpointUsesSharedTimeout(t *testing.T) {
	const maxExpectedElapsed = 175 * time.Millisecond

	originalTimeout := healthCheckTimeout
	healthCheckTimeout = 100 * time.Millisecond
	defer func() {
		healthCheckTimeout = originalTimeout
	}()

	deps := testDeps()
	dbHealth := &blockingHealthCheck{}
	redisHealth := &blockingHealthCheck{}
	deps.DBHealth = dbHealth
	deps.RedisHealth = redisHealth
	srv := newTestServerWithDeps(t, deps)

	start := time.Now()
	rr := doRequest(t, srv, http.MethodGet, "/healthz", nil)
	elapsed := time.Since(start)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	body := decodeJSON[map[string]string](t, rr)
	if body["status"] != "degraded" {
		t.Fatalf("status = %q, want %q", body["status"], "degraded")
	}
	if body["db"] != "error" {
		t.Fatalf("db = %q, want %q", body["db"], "error")
	}
	if body["redis"] != "error" {
		t.Fatalf("redis = %q, want %q", body["redis"], "error")
	}
	if elapsed >= maxExpectedElapsed {
		t.Fatalf("elapsed = %v, want < %v", elapsed, maxExpectedElapsed)
	}
	if dbHealth.calls.Load() != 1 {
		t.Fatalf("db health calls = %d, want 1", dbHealth.calls.Load())
	}
	if redisHealth.calls.Load() != 1 {
		t.Fatalf("redis health calls = %d, want 1", redisHealth.calls.Load())
	}
}

func TestHealthEndpointLogsFailuresAtInfo(t *testing.T) {
	t.Parallel()

	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelDebug}))

	deps := testDeps()
	deps.DBHealth = &stubHealthCheck{err: errors.New("db unavailable")}
	deps.RedisHealth = &stubHealthCheck{}
	srv := newTestServerWithDepsAndLogger(t, deps, logger)

	rr := doRequest(t, srv, http.MethodGet, "/healthz", nil)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(logOutput.String(), "level=INFO") {
		t.Fatalf("log output = %q, want INFO level entry", logOutput.String())
	}
	if strings.Contains(logOutput.String(), "level=WARN") {
		t.Fatalf("log output = %q, want no WARN entries", logOutput.String())
	}
}

func TestMetricsEndpointIsPublic(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); got == "" {
		t.Fatal("missing content type")
	}
}

func TestLoginSuccess(t *testing.T) {
	t.Parallel()

	users := newStubUserRepo()
	users.mustStore(t, "alice", "correct-horse-battery-staple")

	deps := testDeps()
	deps.Users = users
	srv := newTestServerWithDeps(t, deps)

	rr := doUnauthenticatedRequest(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "correct-horse-battery-staple",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	resp := decodeJSON[LoginResponse](t, rr)
	if resp.AccessToken == "" {
		t.Fatal("access_token should not be empty")
	}
	if resp.RefreshToken == "" {
		t.Fatal("refresh_token should not be empty")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", rr.Header().Get("Cache-Control"), "no-store")
	}
	if rr.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("Pragma = %q, want %q", rr.Header().Get("Pragma"), "no-cache")
	}
	if resp.ExpiresAt.IsZero() {
		t.Fatal("expires_at should not be zero")
	}
	if resp.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("expires_at = %v, want future timestamp", resp.ExpiresAt)
	}

	principal, err := srv.auth.ValidateAccessToken(resp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if principal.Subject != "alice" {
		t.Fatalf("principal.Subject = %q, want %q", principal.Subject, "alice")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	t.Parallel()

	users := newStubUserRepo()
	users.mustStore(t, "alice", "correct-horse-battery-staple")

	deps := testDeps()
	deps.Users = users
	srv := newTestServerWithDeps(t, deps)

	rr := doUnauthenticatedRequest(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "wrong-password",
	})

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	resp := decodeJSON[ErrorResponse](t, rr)
	if resp.Error != "invalid username or password" {
		t.Fatalf("error = %q, want %q", resp.Error, "invalid username or password")
	}
}

func TestLoginNonexistentUser(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	rr := doUnauthenticatedRequest(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "missing",
		"password": "secret",
	})

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	resp := decodeJSON[ErrorResponse](t, rr)
	if resp.Error != "invalid username or password" {
		t.Fatalf("error = %q, want %q", resp.Error, "invalid username or password")
	}
}

func TestLoginMalformedRequest(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	resp := decodeJSON[ErrorResponse](t, rr)
	if resp.Error != "invalid request body" {
		t.Fatalf("error = %q, want %q", resp.Error, "invalid request body")
	}
}

func TestLoginRequiresCredentials(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	rr := doUnauthenticatedRequest(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	resp := decodeJSON[ErrorResponse](t, rr)
	if resp.Error != "username and password are required" {
		t.Fatalf("error = %q, want %q", resp.Error, "username and password are required")
	}
}

// ---------------------------------------------------------------------------
// Strategies
// ---------------------------------------------------------------------------

func TestListStrategies(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := decodeJSON[ListResponse](t, rr)
	items, ok := body.Data.([]any)
	if !ok {
		t.Fatalf("data is not a list: %T", body.Data)
	}
	if len(items) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(items))
	}
}

func TestGetStrategy(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies/"+stratA.ID.String(), nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := decodeJSON[domain.Strategy](t, rr)
	if body.Name != "Alpha" {
		t.Fatalf("name = %q, want %q", body.Name, "Alpha")
	}
}

func TestGetStrategyNotFound(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies/"+uuid.New().String(), nil)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeNotFound {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeNotFound)
	}
}

func TestCreateStrategy(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	payload := map[string]any{
		"name":        "Gamma",
		"ticker":      "TSLA",
		"market_type": "stock",
	}

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies", payload)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d\nbody: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	body := decodeJSON[domain.Strategy](t, rr)
	if body.Name != "Gamma" {
		t.Fatalf("name = %q, want %q", body.Name, "Gamma")
	}
	if body.Status != domain.StrategyStatusActive {
		t.Fatalf("status = %q, want %q", body.Status, domain.StrategyStatusActive)
	}
	if body.ID == uuid.Nil {
		t.Fatal("ID should be set")
	}
}

func TestCreateStrategyConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		payload        map[string]any
		wantStatus     int
		wantErrSubstrs []string
		wantModel      string
	}{
		{
			name: "valid partial config",
			payload: map[string]any{
				"name":        "Gamma",
				"ticker":      "TSLA",
				"market_type": "stock",
				"config": map[string]any{
					"llm_config": map[string]any{
						"deep_think_model": "gpt-5.4",
					},
				},
			},
			wantStatus: http.StatusCreated,
			wantModel:  "gpt-5.4",
		},
		{
			name: "invalid model",
			payload: map[string]any{
				"name":        "Gamma",
				"ticker":      "TSLA",
				"market_type": "stock",
				"config": map[string]any{
					"llm_config": map[string]any{
						"deep_think_model": "unknown-model",
					},
				},
			},
			wantStatus:     http.StatusBadRequest,
			wantErrSubstrs: []string{"llm_config.deep_think_model", "unknown-model"},
		},
		{
			name: "out of range risk parameter",
			payload: map[string]any{
				"name":        "Gamma",
				"ticker":      "TSLA",
				"market_type": "stock",
				"config": map[string]any{
					"risk_config": map[string]any{
						"position_size_pct": 101.0,
					},
				},
			},
			wantStatus:     http.StatusBadRequest,
			wantErrSubstrs: []string{"risk_config.position_size_pct", "101"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t)

			rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies", tt.payload)

			if tt.wantStatus == http.StatusCreated {
				if rr.Code != http.StatusCreated {
					t.Fatalf("status = %d, want %d\nbody: %s", rr.Code, http.StatusCreated, rr.Body.String())
				}

				body := decodeJSON[domain.Strategy](t, rr)
				var cfg agent.StrategyConfig
				if err := json.Unmarshal(body.Config, &cfg); err != nil {
					t.Fatalf("json.Unmarshal(config) error = %v", err)
				}
				if cfg.LLMConfig == nil || cfg.LLMConfig.DeepThinkModel == nil {
					t.Fatalf("config = %#v, want deep_think_model to be set", cfg)
				}
				if got := *cfg.LLMConfig.DeepThinkModel; got != tt.wantModel {
					t.Fatalf("deep_think_model = %q, want %q", got, tt.wantModel)
				}
				return
			}

			assertValidationError(t, rr, tt.wantErrSubstrs...)
		})
	}
}

func TestListStrategiesStatusFilter(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	strategyRepo := deps.Strategies.(*stubStrategyRepo)
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies?status=paused", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	filter, ok := strategyRepo.lastListedFilter()
	if !ok {
		t.Fatal("expected strategy repository List to be called")
	}
	if filter.Status != domain.StrategyStatusPaused {
		t.Fatalf("status filter = %q, want %q", filter.Status, domain.StrategyStatusPaused)
	}
}

func TestCreateStrategyValidation(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies", map[string]any{
		"name": "", // missing required field
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestRunStrategy(t *testing.T) {
	t.Parallel()

	run := domain.PipelineRun{
		ID:         uuid.New(),
		StrategyID: stratA.ID,
		Ticker:     stratA.Ticker,
		Status:     domain.PipelineStatusCompleted,
	}
	deps := testDeps()
	deps.Runner = &stubStrategyRunner{
		result: &StrategyRunResult{
			Run:    run,
			Signal: domain.PipelineSignalBuy,
		},
	}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies/"+stratA.ID.String()+"/run", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	body := decodeJSON[StrategyRunResult](t, rr)
	if body.Run.ID != run.ID {
		t.Fatalf("run id = %s, want %s", body.Run.ID, run.ID)
	}
	if body.Signal != domain.PipelineSignalBuy {
		t.Fatalf("signal = %q, want %q", body.Signal, domain.PipelineSignalBuy)
	}
}

func TestRunStrategyRequiresRunner(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies/"+stratA.ID.String()+"/run", nil)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotImplemented)
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeNotImplemented {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeNotImplemented)
	}
}

func TestRunStrategyNotFound(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Runner = &stubStrategyRunner{}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies/"+uuid.New().String()+"/run", nil)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestRunStrategyRejectsNilResult(t *testing.T) {
	t.Parallel()

	deps := testDeps()
	deps.Runner = &stubStrategyRunner{}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies/"+stratA.ID.String()+"/run", nil)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeInternal {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeInternal)
	}
}

func TestBroadcastRunResultUsesRunningStatusForStartEvent(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	runID := uuid.New()
	strategyID := uuid.New()
	result := &StrategyRunResult{
		Run: domain.PipelineRun{
			ID:         runID,
			StrategyID: strategyID,
			Status:     domain.PipelineStatusCompleted,
		},
	}

	srv.broadcastRunResult(result)

	select {
	case msg := <-srv.hub.broadcast:
		var decoded WSMessage
		if err := json.Unmarshal(msg.data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if decoded.Type != EventPipelineStart {
			t.Fatalf("event type = %q, want %q", decoded.Type, EventPipelineStart)
		}
		payload, ok := decoded.Data.(map[string]any)
		if !ok {
			t.Fatalf("payload type = %T, want map[string]any", decoded.Data)
		}
		if got := payload["status"]; got != string(domain.PipelineStatusRunning) {
			t.Fatalf("start-event status = %v, want %q", got, domain.PipelineStatusRunning)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for start event broadcast")
	}
}

func TestUpdateStrategy(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	payload := map[string]any{
		"name":        "Alpha Updated",
		"ticker":      "AAPL",
		"market_type": "stock",
	}

	rr := doRequest(t, srv, http.MethodPut, "/api/v1/strategies/"+stratA.ID.String(), payload)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestUpdateStrategyConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		payload        map[string]any
		wantStatus     int
		wantErrSubstrs []string
	}{
		{
			name: "valid empty config",
			payload: map[string]any{
				"name":        "Alpha Updated",
				"ticker":      "AAPL",
				"market_type": "stock",
				"config":      map[string]any{},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "unknown analyst role",
			payload: map[string]any{
				"name":        "Alpha Updated",
				"ticker":      "AAPL",
				"market_type": "stock",
				"config": map[string]any{
					"analyst_selection": []string{"ghost_role"},
				},
			},
			wantStatus:     http.StatusBadRequest,
			wantErrSubstrs: []string{"analyst_selection[0]", "ghost_role"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t)

			rr := doRequest(t, srv, http.MethodPut, "/api/v1/strategies/"+stratA.ID.String(), tt.payload)

			if tt.wantStatus == http.StatusOK {
				if rr.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d\nbody: %s", rr.Code, http.StatusOK, rr.Body.String())
				}
				return
			}

			assertValidationError(t, rr, tt.wantErrSubstrs...)
		})
	}
}

func TestDeleteStrategy(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodDelete, "/api/v1/strategies/"+stratA.ID.String(), nil)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestDeleteStrategyNotFound(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodDelete, "/api/v1/strategies/"+uuid.New().String(), nil)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

func TestListRuns(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/runs", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestListRunsAppliesDateRangeFilters(t *testing.T) {
	t.Parallel()

	runRepo := &stubRunRepo{}
	deps := testDeps()
	deps.Runs = runRepo
	srv := newTestServerWithDeps(t, deps)

	startDate := "2026-03-14T09:30:00.000Z"
	endDate := "2026-03-15T16:00:00.999Z"
	rr := doRequest(
		t,
		srv,
		http.MethodGet,
		fmt.Sprintf(
			"/api/v1/runs?status=completed&strategy_id=%s&start_date=%s&end_date=%s",
			stratA.ID,
			startDate,
			endDate,
		),
		nil,
	)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	if runRepo.lastFilter.StrategyID == nil || *runRepo.lastFilter.StrategyID != stratA.ID {
		t.Fatalf("strategy_id filter = %v, want %s", runRepo.lastFilter.StrategyID, stratA.ID)
	}
	if runRepo.lastFilter.Status != domain.PipelineStatusCompleted {
		t.Fatalf("status filter = %q, want %q", runRepo.lastFilter.Status, domain.PipelineStatusCompleted)
	}

	expectedStartDate, err := time.Parse(time.RFC3339Nano, startDate)
	if err != nil {
		t.Fatalf("time.Parse() start_date error = %v", err)
	}
	expectedEndDate, err := time.Parse(time.RFC3339Nano, endDate)
	if err != nil {
		t.Fatalf("time.Parse() end_date error = %v", err)
	}
	if runRepo.lastFilter.StartedAfter == nil || !runRepo.lastFilter.StartedAfter.Equal(expectedStartDate) {
		t.Fatalf("start_date filter = %v, want %v", runRepo.lastFilter.StartedAfter, expectedStartDate)
	}
	if runRepo.lastFilter.StartedBefore == nil || !runRepo.lastFilter.StartedBefore.Equal(expectedEndDate) {
		t.Fatalf("end_date filter = %v, want %v", runRepo.lastFilter.StartedBefore, expectedEndDate)
	}
}

// ---------------------------------------------------------------------------
// Portfolio
// ---------------------------------------------------------------------------

func TestPortfolioSummary(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/portfolio/summary", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := decodeJSON[map[string]any](t, rr)
	if _, ok := body["open_positions"]; !ok {
		t.Fatal("missing open_positions in summary")
	}
}

// ---------------------------------------------------------------------------
// Orders
// ---------------------------------------------------------------------------

func TestListOrders(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/orders", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Trades
// ---------------------------------------------------------------------------

func TestListTrades(t *testing.T) {
	t.Parallel()
	tradeRepo := &stubTradeRepo{
		listTrades: []domain.Trade{
			{
				ID:         uuid.MustParse("33333333-3333-3333-3333-333333333333"),
				Ticker:     "AAPL",
				Side:       domain.OrderSideBuy,
				Quantity:   2,
				Price:      123.45,
				Fee:        0.12,
				ExecutedAt: time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC),
				CreatedAt:  time.Date(2024, 3, 1, 10, 0, 1, 0, time.UTC),
			},
		},
	}
	deps := testDeps()
	deps.Trades = tradeRepo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/trades?limit=1&offset=2", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	body := decodeJSON[tradeListResponse](t, rr)
	if tradeRepo.listCalls != 1 {
		t.Fatalf("List() calls = %d, want 1", tradeRepo.listCalls)
	}
	if tradeRepo.getByOrderCalls != 0 {
		t.Fatalf("GetByOrder() calls = %d, want 0", tradeRepo.getByOrderCalls)
	}
	if tradeRepo.getByPositionCalls != 0 {
		t.Fatalf("GetByPosition() calls = %d, want 0", tradeRepo.getByPositionCalls)
	}
	if tradeRepo.lastLimit != 1 || tradeRepo.lastOffset != 2 {
		t.Fatalf("pagination = (%d,%d), want (1,2)", tradeRepo.lastLimit, tradeRepo.lastOffset)
	}
	if len(body.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(body.Data))
	}
	if body.Data[0].ID != tradeRepo.listTrades[0].ID {
		t.Fatalf("trade id = %s, want %s", body.Data[0].ID, tradeRepo.listTrades[0].ID)
	}
	if body.Limit != 1 || body.Offset != 2 {
		t.Fatalf("response pagination = (%d,%d), want (1,2)", body.Limit, body.Offset)
	}
}

func TestListTradesByOrderID(t *testing.T) {
	t.Parallel()
	orderID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	tradeRepo := &stubTradeRepo{
		getByOrderTrades: []domain.Trade{
			{
				ID:         uuid.MustParse("55555555-5555-5555-5555-555555555555"),
				OrderID:    &orderID,
				Ticker:     "MSFT",
				Side:       domain.OrderSideSell,
				Quantity:   1,
				Price:      234.56,
				Fee:        0.22,
				ExecutedAt: time.Date(2024, 3, 2, 10, 0, 0, 0, time.UTC),
				CreatedAt:  time.Date(2024, 3, 2, 10, 0, 1, 0, time.UTC),
			},
		},
	}
	deps := testDeps()
	deps.Trades = tradeRepo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/trades?order_id="+orderID.String(), nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	body := decodeJSON[tradeListResponse](t, rr)
	if tradeRepo.listCalls != 0 {
		t.Fatalf("List() calls = %d, want 0", tradeRepo.listCalls)
	}
	if tradeRepo.getByOrderCalls != 1 {
		t.Fatalf("GetByOrder() calls = %d, want 1", tradeRepo.getByOrderCalls)
	}
	if tradeRepo.getByPositionCalls != 0 {
		t.Fatalf("GetByPosition() calls = %d, want 0", tradeRepo.getByPositionCalls)
	}
	if tradeRepo.lastOrderID == nil || *tradeRepo.lastOrderID != orderID {
		t.Fatalf("order_id = %v, want %s", tradeRepo.lastOrderID, orderID)
	}
	if tradeRepo.lastFilter.OrderID == nil || *tradeRepo.lastFilter.OrderID != orderID {
		t.Fatalf("filter.OrderID = %v, want %s", tradeRepo.lastFilter.OrderID, orderID)
	}
	if len(body.Data) != 1 || body.Data[0].OrderID == nil || *body.Data[0].OrderID != orderID {
		t.Fatalf("response data = %+v, want order_id %s", body.Data, orderID)
	}
}

func TestListTradesByPositionID(t *testing.T) {
	t.Parallel()
	positionID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	tradeRepo := &stubTradeRepo{
		getByPositionTrades: []domain.Trade{
			{
				ID:         uuid.MustParse("77777777-7777-7777-7777-777777777777"),
				PositionID: &positionID,
				Ticker:     "NVDA",
				Side:       domain.OrderSideBuy,
				Quantity:   3,
				Price:      345.67,
				Fee:        0.32,
				ExecutedAt: time.Date(2024, 3, 3, 10, 0, 0, 0, time.UTC),
				CreatedAt:  time.Date(2024, 3, 3, 10, 0, 1, 0, time.UTC),
			},
		},
	}
	deps := testDeps()
	deps.Trades = tradeRepo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/trades?position_id="+positionID.String(), nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	body := decodeJSON[tradeListResponse](t, rr)
	if tradeRepo.listCalls != 0 {
		t.Fatalf("List() calls = %d, want 0", tradeRepo.listCalls)
	}
	if tradeRepo.getByOrderCalls != 0 {
		t.Fatalf("GetByOrder() calls = %d, want 0", tradeRepo.getByOrderCalls)
	}
	if tradeRepo.getByPositionCalls != 1 {
		t.Fatalf("GetByPosition() calls = %d, want 1", tradeRepo.getByPositionCalls)
	}
	if tradeRepo.lastPositionID == nil || *tradeRepo.lastPositionID != positionID {
		t.Fatalf("position_id = %v, want %s", tradeRepo.lastPositionID, positionID)
	}
	if tradeRepo.lastFilter.PositionID == nil || *tradeRepo.lastFilter.PositionID != positionID {
		t.Fatalf("filter.PositionID = %v, want %s", tradeRepo.lastFilter.PositionID, positionID)
	}
	if len(body.Data) != 1 || body.Data[0].PositionID == nil || *body.Data[0].PositionID != positionID {
		t.Fatalf("response data = %+v, want position_id %s", body.Data, positionID)
	}
}

func TestListTradesRejectsOrderIDAndPositionIDTogether(t *testing.T) {
	t.Parallel()
	orderID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	positionID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	tradeRepo := &stubTradeRepo{}
	deps := testDeps()
	deps.Trades = tradeRepo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/trades?order_id="+orderID.String()+"&position_id="+positionID.String(), nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeBadRequest {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeBadRequest)
	}
	if tradeRepo.listCalls != 0 || tradeRepo.getByOrderCalls != 0 || tradeRepo.getByPositionCalls != 0 {
		t.Fatalf("trade repo should not be called, got list=%d order=%d position=%d", tradeRepo.listCalls, tradeRepo.getByOrderCalls, tradeRepo.getByPositionCalls)
	}
}

func TestListTradesEmptyDatabaseReturnsEmptyArray(t *testing.T) {
	t.Parallel()
	tradeRepo := &stubTradeRepo{}
	deps := testDeps()
	deps.Trades = tradeRepo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/trades", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	body := decodeJSON[tradeListResponse](t, rr)
	if body.Data == nil {
		t.Fatal("data = nil, want empty slice")
	}
	if len(body.Data) != 0 {
		t.Fatalf("len(data) = %d, want 0", len(body.Data))
	}
}

func TestListTradesInvalidSide(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/trades?side=invalid", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Memories
// ---------------------------------------------------------------------------

func TestListMemories(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/memories", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestSearchMemories(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/memories/search", map[string]string{"query": "test"})

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestSearchMemoriesValidation(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/memories/search", map[string]string{"query": ""})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeValidation {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeValidation)
	}
}

func TestSearchMemoriesInvalidJSON(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/memories/search", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	tokenPair, err := srv.auth.GenerateTokenPair("test-user")
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenPair.AccessToken)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeBadRequest {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeBadRequest)
	}
}

func TestDeleteMemory(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodDelete, "/api/v1/memories/"+uuid.New().String(), nil)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

// ---------------------------------------------------------------------------
// Risk
// ---------------------------------------------------------------------------

func TestRiskStatus(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/risk/status", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestKillSwitchToggle(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/risk/killswitch", map[string]any{
		"active": true,
		"reason": "test",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestKillSwitchToggleRequiresReason(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/risk/killswitch", map[string]any{
		"active": true,
		"reason": "",
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeValidation {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeValidation)
	}
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

func TestCORSPreflight(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodOptions, "/api/v1/strategies", nil)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("missing CORS origin header")
	}
}

func TestCORSEchoesMatchingOrigin(t *testing.T) {
	t.Parallel()

	cfg := DefaultServerConfig()
	cfg.JWTSecret = "test-jwt-secret"
	cfg.CORSConfig = CORSConfig{
		AllowedOrigins: []string{"https://example.com", "https://other.com"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	cfg.RateLimit = 0 // disable rate limiting for this test
	srv, err := NewServer(cfg, testDeps(), slog.Default())
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	// Matching origin → echoed back.
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/strategies", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("origin = %q, want %q", got, "https://example.com")
	}

	// Non-matching origin → no Access-Control-Allow-Origin header.
	req2 := httptest.NewRequest(http.MethodOptions, "/api/v1/strategies", nil)
	req2.Header.Set("Origin", "https://evil.com")
	rr2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr2, req2)

	if got := rr2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("origin should be empty for non-matching, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

func TestRateLimiter(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if !rl.Allow("test-client") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	if rl.Allow("test-client") {
		t.Fatal("4th request should be rate limited")
	}
}

func TestRateLimiterWindowReset(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(1, time.Millisecond)
	// Override nowFunc for testability.
	now := time.Now()
	rl.nowFunc = func() time.Time { return now }

	if !rl.Allow("client") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("client") {
		t.Fatal("second request should be limited")
	}

	// Advance past window.
	now = now.Add(2 * time.Millisecond)
	if !rl.Allow("client") {
		t.Fatal("request after window reset should be allowed")
	}
}

func TestClientIPTrustedProxy(t *testing.T) {
	t.Parallel()

	trustedNets, err := ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("ParseTrustedProxies() error = %v", err)
	}

	// Request from trusted proxy: should use X-Forwarded-For.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	if got := clientIP(req, trustedNets); got != "203.0.113.50" {
		t.Fatalf("clientIP from trusted proxy = %q, want %q", got, "203.0.113.50")
	}

	// Request from untrusted peer: should ignore X-Forwarded-For.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "192.168.1.100:5678"
	req2.Header.Set("X-Forwarded-For", "203.0.113.99")
	if got := clientIP(req2, trustedNets); got != "192.168.1.100" {
		t.Fatalf("clientIP from untrusted peer = %q, want %q", got, "192.168.1.100")
	}

	// No trusted proxies configured: should use RemoteAddr.
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = "10.0.0.1:1234"
	req3.Header.Set("X-Forwarded-For", "203.0.113.50")
	if got := clientIP(req3, nil); got != "10.0.0.1" {
		t.Fatalf("clientIP with no trusted proxies = %q, want %q", got, "10.0.0.1")
	}
}

// ---------------------------------------------------------------------------
// Error response consistency
// ---------------------------------------------------------------------------

func TestInvalidUUIDReturns400(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/strategies/not-a-uuid", nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeBadRequest {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeBadRequest)
	}
}

func TestInvalidJSONReturns400(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/strategies", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	tokenPair, err := srv.auth.GenerateTokenPair("test-user")
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenPair.AccessToken)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Server construction validation
// ---------------------------------------------------------------------------

func TestNewServerRequiresDeps(t *testing.T) {
	t.Parallel()

	cfg := DefaultServerConfig()
	cfg.JWTSecret = "test-jwt-secret"

	_, err := NewServer(cfg, Deps{}, slog.Default())
	if err == nil {
		t.Fatal("expected error for missing deps")
	}
}

// ---------------------------------------------------------------------------
// Stub implementations
// ---------------------------------------------------------------------------

type stubStrategyRepo struct {
	mu         sync.Mutex
	items      map[uuid.UUID]domain.Strategy
	lastFilter repository.StrategyFilter
	sawList    bool
}

type stubAPIKeyRepo struct {
	mu    sync.Mutex
	items map[string]domain.APIKey
}

type stubUserRepo struct {
	mu               sync.Mutex
	items            map[string]domain.User
	getByUsernameErr error
}

func newStubAPIKeyRepo() *stubAPIKeyRepo {
	return &stubAPIKeyRepo{items: make(map[string]domain.APIKey)}
}

func newStubUserRepo() *stubUserRepo {
	return &stubUserRepo{items: make(map[string]domain.User)}
}

func (s *stubUserRepo) mustStore(t *testing.T, username, password string) {
	t.Helper()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	now := time.Now().UTC()
	user := domain.User{
		ID:           uuid.New(),
		Username:     username,
		PasswordHash: string(passwordHash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[username] = user
}

func (s *stubAPIKeyRepo) Create(_ context.Context, key *domain.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if key.ID == uuid.Nil {
		key.ID = uuid.New()
	}
	now := time.Now().UTC()
	key.CreatedAt = now
	key.UpdatedAt = now
	s.items[key.KeyPrefix] = *key
	return nil
}

func (s *stubAPIKeyRepo) GetByPrefix(_ context.Context, prefix string) (*domain.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, ok := s.items[prefix]
	if !ok {
		return nil, fmt.Errorf("api key %s: %w", prefix, repository.ErrNotFound)
	}
	keyCopy := key
	return &keyCopy, nil
}

func (s *stubAPIKeyRepo) List(_ context.Context, _, _ int) ([]domain.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.APIKey, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	return out, nil
}

func (s *stubAPIKeyRepo) Revoke(_ context.Context, id uuid.UUID, revokedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for prefix, item := range s.items {
		if item.ID == id {
			item.RevokedAt = &revokedAt
			item.UpdatedAt = revokedAt
			s.items[prefix] = item
			return nil
		}
	}
	return fmt.Errorf("api key %v: %w", id, repository.ErrNotFound)
}

func (s *stubAPIKeyRepo) TouchLastUsed(_ context.Context, id uuid.UUID, lastUsedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for prefix, item := range s.items {
		if item.ID == id {
			item.LastUsedAt = &lastUsedAt
			item.UpdatedAt = lastUsedAt
			s.items[prefix] = item
			return nil
		}
	}
	return fmt.Errorf("api key %v: %w", id, repository.ErrNotFound)
}

func (s *stubUserRepo) Create(_ context.Context, user *domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items[user.Username] = *user
	return nil
}

func (s *stubUserRepo) GetByUsername(_ context.Context, username string) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.getByUsernameErr != nil {
		return nil, s.getByUsernameErr
	}

	user, ok := s.items[username]
	if !ok {
		return nil, fmt.Errorf("user %s: %w", username, repository.ErrNotFound)
	}
	userCopy := user
	return &userCopy, nil
}

func (s *stubUserRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, user := range s.items {
		if user.ID == id {
			userCopy := user
			return &userCopy, nil
		}
	}
	return nil, fmt.Errorf("user %v: %w", id, repository.ErrNotFound)
}

func (s *stubStrategyRepo) Create(_ context.Context, strategy *domain.Strategy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[strategy.ID] = *strategy
	return nil
}

func (s *stubStrategyRepo) Get(_ context.Context, id uuid.UUID) (*domain.Strategy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("strategy %v: %w", id, repository.ErrNotFound)
	}
	return &st, nil
}

func (s *stubStrategyRepo) List(_ context.Context, filter repository.StrategyFilter, _, _ int) ([]domain.Strategy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFilter = filter
	s.sawList = true
	out := make([]domain.Strategy, 0, len(s.items))
	for _, v := range s.items {
		out = append(out, v)
	}
	return out, nil
}

func (s *stubStrategyRepo) Update(_ context.Context, strategy *domain.Strategy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[strategy.ID]; !ok {
		return fmt.Errorf("strategy %v: %w", strategy.ID, repository.ErrNotFound)
	}
	s.items[strategy.ID] = *strategy
	return nil
}

func (s *stubStrategyRepo) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return fmt.Errorf("strategy %v: %w", id, repository.ErrNotFound)
	}
	delete(s.items, id)
	return nil
}

func (s *stubStrategyRepo) lastListedFilter() (repository.StrategyFilter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sawList {
		return repository.StrategyFilter{}, false
	}
	return s.lastFilter, true
}

// stubRunRepo

type stubRunRepo struct {
	lastFilter repository.PipelineRunFilter
	runs       []domain.PipelineRun
}

func (*stubRunRepo) Create(context.Context, *domain.PipelineRun) error { return nil }

func (s *stubRunRepo) Get(_ context.Context, _ uuid.UUID, _ time.Time) (*domain.PipelineRun, error) {
	return nil, fmt.Errorf("run: %w", repository.ErrNotFound)
}

func (s *stubRunRepo) List(_ context.Context, filter repository.PipelineRunFilter, _, _ int) ([]domain.PipelineRun, error) {
	s.lastFilter = filter
	return s.runs, nil
}

func (*stubRunRepo) UpdateStatus(context.Context, uuid.UUID, time.Time, repository.PipelineRunStatusUpdate) error {
	return nil
}

type stubStrategyRunner struct {
	result *StrategyRunResult
	err    error
}

func (s *stubStrategyRunner) RunStrategy(context.Context, domain.Strategy) (*StrategyRunResult, error) {
	return s.result, s.err
}

// stubDecisionRepo

type stubDecisionRepo struct{}

func (stubDecisionRepo) Create(context.Context, *domain.AgentDecision) error { return nil }
func (stubDecisionRepo) GetByRun(context.Context, uuid.UUID, repository.AgentDecisionFilter, int, int) ([]domain.AgentDecision, error) {
	return nil, nil
}

// stubOrderRepo

type stubOrderRepo struct{}

func (stubOrderRepo) Create(context.Context, *domain.Order) error { return nil }
func (stubOrderRepo) Get(_ context.Context, _ uuid.UUID) (*domain.Order, error) {
	return nil, fmt.Errorf("order: %w", repository.ErrNotFound)
}

func (stubOrderRepo) List(context.Context, repository.OrderFilter, int, int) ([]domain.Order, error) {
	return nil, nil
}
func (stubOrderRepo) Update(context.Context, *domain.Order) error { return nil }
func (stubOrderRepo) Delete(context.Context, uuid.UUID) error     { return nil }
func (stubOrderRepo) GetByStrategy(context.Context, uuid.UUID, repository.OrderFilter, int, int) ([]domain.Order, error) {
	return nil, nil
}

func (stubOrderRepo) GetByRun(context.Context, uuid.UUID, repository.OrderFilter, int, int) ([]domain.Order, error) {
	return nil, nil
}

// stubPositionRepo

type stubPositionRepo struct{}

func (stubPositionRepo) Create(context.Context, *domain.Position) error { return nil }
func (stubPositionRepo) Get(_ context.Context, _ uuid.UUID) (*domain.Position, error) {
	return nil, fmt.Errorf("position: %w", repository.ErrNotFound)
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

// stubTradeRepo

type tradeListResponse struct {
	Data   []domain.Trade `json:"data"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type stubTradeRepo struct {
	listTrades          []domain.Trade
	getByOrderTrades    []domain.Trade
	getByPositionTrades []domain.Trade
	lastFilter          repository.TradeFilter
	lastLimit           int
	lastOffset          int
	lastOrderID         *uuid.UUID
	lastPositionID      *uuid.UUID
	listCalls           int
	getByOrderCalls     int
	getByPositionCalls  int
}

func (stubTradeRepo) Create(context.Context, *domain.Trade) error { return nil }

func (s *stubTradeRepo) List(_ context.Context, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	s.lastOrderID = nil
	s.lastPositionID = nil
	s.listCalls++
	return s.listTrades, nil
}

func (s *stubTradeRepo) GetByOrder(_ context.Context, orderID uuid.UUID, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	s.lastOrderID = &orderID
	s.lastPositionID = nil
	s.getByOrderCalls++
	return s.getByOrderTrades, nil
}

func (s *stubTradeRepo) GetByPosition(_ context.Context, positionID uuid.UUID, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	s.lastOrderID = nil
	s.lastPositionID = &positionID
	s.getByPositionCalls++
	return s.getByPositionTrades, nil
}

// stubMemoryRepo

type stubMemoryRepo struct{}

func (stubMemoryRepo) Create(context.Context, *domain.AgentMemory) error { return nil }
func (stubMemoryRepo) Search(context.Context, string, repository.MemorySearchFilter, int, int) ([]domain.AgentMemory, error) {
	return nil, nil
}
func (stubMemoryRepo) Delete(context.Context, uuid.UUID) error { return nil }

// stubRiskEngine

type stubRiskEngine struct{}

func (stubRiskEngine) CheckPreTrade(context.Context, *domain.Order, risk.Portfolio) (bool, string, error) {
	return true, "", nil
}

func (stubRiskEngine) CheckPositionLimits(context.Context, string, float64, risk.Portfolio) (bool, string, error) {
	return true, "", nil
}

func (stubRiskEngine) GetStatus(context.Context) (risk.EngineStatus, error) {
	return risk.EngineStatus{
		RiskStatus: domain.RiskStatusNormal,
		UpdatedAt:  time.Now(),
	}, nil
}
func (stubRiskEngine) TripCircuitBreaker(context.Context, string) error { return nil }
func (stubRiskEngine) ResetCircuitBreaker(context.Context) error        { return nil }

type stubHealthCheck struct {
	err   error
	calls atomic.Int32
}

func (s *stubHealthCheck) Check(context.Context) error {
	s.calls.Add(1)
	return s.err
}

type blockingHealthCheck struct {
	calls atomic.Int32
}

func (b *blockingHealthCheck) Check(ctx context.Context) error {
	b.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func (stubRiskEngine) IsKillSwitchActive(context.Context) (bool, error)           { return false, nil }
func (stubRiskEngine) ActivateKillSwitch(context.Context, string) error           { return nil }
func (stubRiskEngine) DeactivateKillSwitch(context.Context) error                 { return nil }
func (stubRiskEngine) UpdateMetrics(context.Context, float64, float64, int) error { return nil }

// ---------------------------------------------------------------------------
// Stubs for new repository dependencies
// ---------------------------------------------------------------------------

type stubConversationRepo struct {
	mu    sync.Mutex
	convs []domain.Conversation
	msgs  map[uuid.UUID][]domain.ConversationMessage
}

func newStubConversationRepo() *stubConversationRepo {
	return &stubConversationRepo{msgs: make(map[uuid.UUID][]domain.ConversationMessage)}
}

func (s *stubConversationRepo) CreateConversation(_ context.Context, conv *domain.Conversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if conv.ID == uuid.Nil {
		conv.ID = uuid.New()
	}
	conv.CreatedAt = time.Now()
	conv.UpdatedAt = time.Now()
	s.convs = append(s.convs, *conv)
	return nil
}

func (s *stubConversationRepo) GetConversation(_ context.Context, id uuid.UUID) (*domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.convs {
		if s.convs[i].ID == id {
			return &s.convs[i], nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *stubConversationRepo) ListConversations(_ context.Context, _ repository.ConversationFilter, _, _ int) ([]domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.convs, nil
}

func (s *stubConversationRepo) AddMessage(_ context.Context, convID uuid.UUID, msg *domain.ConversationMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg.ID = uuid.New()
	msg.ConversationID = convID
	msg.CreatedAt = time.Now()
	s.msgs[convID] = append(s.msgs[convID], *msg)
	return nil
}

func (s *stubConversationRepo) GetMessages(_ context.Context, convID uuid.UUID, _, _ int) ([]domain.ConversationMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msgs[convID], nil
}

type stubAuditLogRepo struct {
	entries []domain.AuditLogEntry
}

func (s *stubAuditLogRepo) Create(_ context.Context, entry *domain.AuditLogEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	entry.CreatedAt = time.Now()
	s.entries = append(s.entries, *entry)
	return nil
}

func (s *stubAuditLogRepo) Query(_ context.Context, _ repository.AuditLogFilter, _, _ int) ([]domain.AuditLogEntry, error) {
	return s.entries, nil
}

type stubEventRepo struct {
	events []domain.AgentEvent
}

func (s *stubEventRepo) Create(_ context.Context, event *domain.AgentEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	event.CreatedAt = time.Now()
	s.events = append(s.events, *event)
	return nil
}

func (s *stubEventRepo) List(_ context.Context, _ repository.AgentEventFilter, _, _ int) ([]domain.AgentEvent, error) {
	return s.events, nil
}

// ---------------------------------------------------------------------------
// Tests for new handlers
// ---------------------------------------------------------------------------

func TestRefreshTokenEndpoint(t *testing.T) {
	t.Parallel()
	deps := testDeps()
	deps.Users.(*stubUserRepo).mustStore(t, "testuser", "password123")
	srv := newTestServerWithDeps(t, deps)

	// First get a valid token pair via login.
	rr := doUnauthenticatedRequest(t, srv, http.MethodPost, "/api/v1/auth/login", loginRequest{
		Username: "testuser",
		Password: "password123",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var loginResp LoginResponse
	if err := json.NewDecoder(rr.Body).Decode(&loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	// Use the refresh token.
	rr = doUnauthenticatedRequest(t, srv, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": loginResp.RefreshToken,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var refreshResp LoginResponse
	if err := json.NewDecoder(rr.Body).Decode(&refreshResp); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if refreshResp.AccessToken == "" {
		t.Fatal("expected non-empty access_token in refresh response")
	}
}

func TestRefreshTokenInvalidToken(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doUnauthenticatedRequest(t, srv, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": "invalid-token",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestRefreshTokenMissingBody(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doUnauthenticatedRequest(t, srv, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": "",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestPauseStrategy(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	// stratA is status=active by default in testDeps.
	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies/"+stratA.ID.String()+"/pause", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := decodeJSON[domain.Strategy](t, rr)
	if body.Status != domain.StrategyStatusPaused {
		t.Fatalf("status = %q, want %q", body.Status, domain.StrategyStatusPaused)
	}
}

func TestPauseStrategyAlreadyPaused(t *testing.T) {
	t.Parallel()
	// stratB has status=active too; pause it first, then pause again.
	deps := testDeps()
	pausedStrat := stratB
	pausedStrat.Status = domain.StrategyStatusPaused
	deps.Strategies.(*stubStrategyRepo).items[pausedStrat.ID] = pausedStrat
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies/"+pausedStrat.ID.String()+"/pause", nil)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusConflict, rr.Body.String())
	}
}

func TestResumeStrategy(t *testing.T) {
	t.Parallel()
	deps := testDeps()
	pausedStrat := stratA
	pausedStrat.Status = domain.StrategyStatusPaused
	deps.Strategies.(*stubStrategyRepo).items[pausedStrat.ID] = pausedStrat
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies/"+pausedStrat.ID.String()+"/resume", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := decodeJSON[domain.Strategy](t, rr)
	if body.Status != domain.StrategyStatusActive {
		t.Fatalf("status = %q, want %q", body.Status, domain.StrategyStatusActive)
	}
}

func TestSkipNextStrategy(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/strategies/"+stratA.ID.String()+"/skip-next", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := decodeJSON[domain.Strategy](t, rr)
	if !body.SkipNextRun {
		t.Fatal("skip_next_run should be true")
	}
}

func TestListEventsEndpoint(t *testing.T) {
	t.Parallel()
	deps := testDeps()
	deps.Events = &stubEventRepo{}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/events", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestListAuditLogEndpoint(t *testing.T) {
	t.Parallel()
	deps := testDeps()
	deps.AuditLog = &stubAuditLogRepo{}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/audit-log", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestListConversationsEndpoint(t *testing.T) {
	t.Parallel()
	deps := testDeps()
	deps.Conversations = newStubConversationRepo()
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/conversations", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestCreateConversationEndpoint(t *testing.T) {
	t.Parallel()
	deps := testDeps()

	// Seed a pipeline run so the handler can verify it exists.
	runID := uuid.New()
	deps.Runs = &stubRunRepo{
		runs: []domain.PipelineRun{{
			ID:         runID,
			StrategyID: stratA.ID,
			Ticker:     "AAPL",
			Status:     domain.PipelineStatusCompleted,
		}},
	}
	deps.Conversations = newStubConversationRepo()
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodPost, "/api/v1/conversations", map[string]any{
		"pipeline_run_id": runID.String(),
		"agent_role":      "bull_researcher",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	var conv domain.Conversation
	if err := json.NewDecoder(rr.Body).Decode(&conv); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if conv.PipelineRunID != runID {
		t.Fatalf("pipeline_run_id = %s, want %s", conv.PipelineRunID, runID)
	}
}
