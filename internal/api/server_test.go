package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

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
	t.Helper()

	cfg := DefaultServerConfig()
	cfg.JWTSecret = "test-jwt-secret"

	srv, err := NewServer(cfg, deps, slog.Default())
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
		Runs:      &stubRunRepo{},
		Decisions: &stubDecisionRepo{},
		Orders:    &stubOrderRepo{},
		Positions: &stubPositionRepo{},
		Trades:    &stubTradeRepo{},
		Memories:  &stubMemoryRepo{},
		APIKeys:   newStubAPIKeyRepo(),
		Risk:      &stubRiskEngine{},
	}
}

var (
	stratA = domain.Strategy{
		ID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name:       "Alpha",
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		IsActive:   true,
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	stratB = domain.Strategy{
		ID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:       "Beta",
		Ticker:     "MSFT",
		MarketType: domain.MarketTypeStock,
		IsActive:   false,
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

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/health", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := decodeJSON[map[string]string](t, rr)
	if body["status"] != "ok" {
		t.Fatalf("health status = %q, want %q", body["status"], "ok")
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
	if body.ID == uuid.Nil {
		t.Fatal("ID should be set")
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
	srv := newTestServer(t)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/trades", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
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
	items map[uuid.UUID]domain.Strategy
}

type stubAPIKeyRepo struct {
	mu    sync.Mutex
	items map[string]domain.APIKey
}

func newStubAPIKeyRepo() *stubAPIKeyRepo {
	return &stubAPIKeyRepo{items: make(map[string]domain.APIKey)}
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
	copy := key
	return &copy, nil
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
	return fmt.Errorf("api key %s: %w", id, repository.ErrNotFound)
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
	return fmt.Errorf("api key %s: %w", id, repository.ErrNotFound)
}

func (s *stubStrategyRepo) Create(_ context.Context, strategy *domain.Strategy) error {
	s.items[strategy.ID] = *strategy
	return nil
}

func (s *stubStrategyRepo) Get(_ context.Context, id uuid.UUID) (*domain.Strategy, error) {
	st, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("strategy %s: %w", id, repository.ErrNotFound)
	}
	return &st, nil
}

func (s *stubStrategyRepo) List(_ context.Context, _ repository.StrategyFilter, _, _ int) ([]domain.Strategy, error) {
	out := make([]domain.Strategy, 0, len(s.items))
	for _, v := range s.items {
		out = append(out, v)
	}
	return out, nil
}

func (s *stubStrategyRepo) Update(_ context.Context, strategy *domain.Strategy) error {
	if _, ok := s.items[strategy.ID]; !ok {
		return fmt.Errorf("strategy %s: %w", strategy.ID, repository.ErrNotFound)
	}
	s.items[strategy.ID] = *strategy
	return nil
}

func (s *stubStrategyRepo) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := s.items[id]; !ok {
		return fmt.Errorf("strategy %s: %w", id, repository.ErrNotFound)
	}
	delete(s.items, id)
	return nil
}

// stubRunRepo

type stubRunRepo struct{}

func (stubRunRepo) Create(context.Context, *domain.PipelineRun) error { return nil }
func (stubRunRepo) Get(_ context.Context, _ uuid.UUID, _ time.Time) (*domain.PipelineRun, error) {
	return nil, fmt.Errorf("run: %w", repository.ErrNotFound)
}
func (stubRunRepo) List(context.Context, repository.PipelineRunFilter, int, int) ([]domain.PipelineRun, error) {
	return nil, nil
}
func (stubRunRepo) UpdateStatus(context.Context, uuid.UUID, time.Time, repository.PipelineRunStatusUpdate) error {
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

type stubTradeRepo struct{}

func (stubTradeRepo) Create(context.Context, *domain.Trade) error { return nil }
func (stubTradeRepo) List(context.Context, repository.TradeFilter, int, int) ([]domain.Trade, error) {
	return nil, nil
}
func (stubTradeRepo) GetByOrder(context.Context, uuid.UUID, repository.TradeFilter, int, int) ([]domain.Trade, error) {
	return nil, nil
}
func (stubTradeRepo) GetByPosition(context.Context, uuid.UUID, repository.TradeFilter, int, int) ([]domain.Trade, error) {
	return nil, nil
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
func (stubRiskEngine) TripCircuitBreaker(context.Context, string) error           { return nil }
func (stubRiskEngine) ResetCircuitBreaker(context.Context) error                  { return nil }
func (stubRiskEngine) IsKillSwitchActive(context.Context) (bool, error)           { return false, nil }
func (stubRiskEngine) ActivateKillSwitch(context.Context, string) error           { return nil }
func (stubRiskEngine) DeactivateKillSwitch(context.Context) error                 { return nil }
func (stubRiskEngine) UpdateMetrics(context.Context, float64, float64, int) error { return nil }
