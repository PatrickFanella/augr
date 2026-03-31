package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	apiserver "github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
)

const smokeTestTimeout = 60 * time.Second

func TestSmokeEndToEnd(t *testing.T) {
	if os.Getenv("RUN_SMOKE_TEST") != "1" {
		t.Skip("set RUN_SMOKE_TEST=1 to run the docker-compose smoke test")
	}

	baseURL := firstNonEmpty(os.Getenv("SMOKE_BASE_URL"), "http://127.0.0.1:8080")
	dbURL := firstNonEmpty(os.Getenv("SMOKE_DATABASE_URL"), "postgres://postgres:postgres@127.0.0.1:5432/tradingagent?sslmode=disable")
	jwtSecret := firstNonEmpty(os.Getenv("SMOKE_JWT_SECRET"), "smoke-jwt-secret")

	ctx, cancel := context.WithTimeout(context.Background(), smokeTestTimeout)
	defer cancel()

	waitForSmokeHealth(t, ctx, baseURL+"/healthz")

	authManager, err := apiserver.NewAuthManager(apiserver.AuthConfig{
		JWTSecret:       jwtSecret,
		RefreshTokenTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}
	tokenPair, err := authManager.GenerateTokenPair("smoke-test")
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	strategyRepo := pgrepo.NewStrategyRepo(pool)
	runRepo := pgrepo.NewPipelineRunRepo(pool)
	decisionRepo := pgrepo.NewAgentDecisionRepo(pool)
	orderRepo := pgrepo.NewOrderRepo(pool)
	positionRepo := pgrepo.NewPositionRepo(pool)
	tradeRepo := pgrepo.NewTradeRepo(pool)

	strategyPayload := map[string]any{
		"name":          "Smoke Strategy",
		"ticker":        "SMOKE",
		"market_type":   "stock",
		"is_active":     true,
		"is_paper":      true,
		"schedule_cron": "",
	}
	createResp := doSmokeJSONRequest(t, http.MethodPost, baseURL+"/api/v1/strategies", strategyPayload, tokenPair.AccessToken)
	defer func() {
		_ = createResp.Body.Close()
	}()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create strategy status = %d, want %d", createResp.StatusCode, http.StatusCreated)
	}

	var createdStrategy domain.Strategy
	decodeSmokeJSON(t, createResp, &createdStrategy)
	if createdStrategy.ID == uuid.Nil {
		t.Fatal("created strategy id should be set")
	}

	savedStrategy, err := strategyRepo.Get(ctx, createdStrategy.ID)
	if err != nil {
		t.Fatalf("strategyRepo.Get() error = %v", err)
	}
	if savedStrategy.Ticker != createdStrategy.Ticker {
		t.Fatalf("saved strategy ticker = %q, want %q", savedStrategy.Ticker, createdStrategy.Ticker)
	}

	wsConn := openSmokeWebSocket(t, baseURL, createdStrategy.ID)
	defer func() {
		_ = wsConn.Close()
	}()

	runResp := doSmokeJSONRequest(t, http.MethodPost, fmt.Sprintf("%s/api/v1/strategies/%s/run", baseURL, createdStrategy.ID), nil, tokenPair.AccessToken)
	defer func() {
		_ = runResp.Body.Close()
	}()
	if runResp.StatusCode != http.StatusOK {
		t.Fatalf("manual run status = %d, want %d", runResp.StatusCode, http.StatusOK)
	}
	var runResult apiserver.StrategyRunResult
	decodeSmokeJSON(t, runResp, &runResult)

	if runResult.Run.Status != domain.PipelineStatusCompleted {
		t.Fatalf("run status = %q, want %q", runResult.Run.Status, domain.PipelineStatusCompleted)
	}
	if runResult.Signal != domain.PipelineSignalBuy {
		t.Fatalf("run signal = %q, want %q", runResult.Signal, domain.PipelineSignalBuy)
	}

	eventTypes := readSmokeEvents(t, wsConn, 4)
	for _, want := range []apiserver.EventType{
		apiserver.EventPipelineStart,
		apiserver.EventSignal,
		apiserver.EventOrderSubmitted,
		apiserver.EventPositionUpdate,
	} {
		if !slices.Contains(eventTypes, want) {
			t.Fatalf("websocket events = %v, missing %q", eventTypes, want)
		}
	}

	savedRun, err := runRepo.Get(ctx, runResult.Run.ID, runResult.Run.TradeDate)
	if err != nil {
		t.Fatalf("runRepo.Get() error = %v", err)
	}
	if savedRun.Status != domain.PipelineStatusCompleted {
		t.Fatalf("saved run status = %q, want %q", savedRun.Status, domain.PipelineStatusCompleted)
	}
	if savedRun.Signal != domain.PipelineSignalBuy {
		t.Fatalf("saved run signal = %q, want %q", savedRun.Signal, domain.PipelineSignalBuy)
	}

	decisions, err := decisionRepo.GetByRun(ctx, runResult.Run.ID, repository.AgentDecisionFilter{}, 20, 0)
	if err != nil {
		t.Fatalf("decisionRepo.GetByRun() error = %v", err)
	}
	// The deterministic smoke pipeline persists one decision for each of its
	// nine registered nodes: analysis, two research debaters, judge, trader,
	// three risk debaters, and the risk manager.
	if len(decisions) < 9 {
		t.Fatalf("decision count = %d, want at least 9", len(decisions))
	}

	orders, err := orderRepo.GetByRun(ctx, runResult.Run.ID, repository.OrderFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("orderRepo.GetByRun() error = %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("order count = %d, want 1", len(orders))
	}
	if orders[0].Broker != "paper" {
		t.Fatalf("order broker = %q, want %q", orders[0].Broker, "paper")
	}

	positions, err := positionRepo.GetByStrategy(ctx, createdStrategy.ID, repository.PositionFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("positionRepo.GetByStrategy() error = %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("position count = %d, want 1", len(positions))
	}

	trades, err := tradeRepo.GetByOrder(ctx, orders[0].ID, repository.TradeFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("tradeRepo.GetByOrder() error = %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("trade count = %d, want 1", len(trades))
	}
}

func waitForSmokeHealth(t *testing.T, ctx context.Context, healthURL string) {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			t.Fatalf("http.NewRequestWithContext() error = %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			var health map[string]string
			decodeErr := json.NewDecoder(resp.Body).Decode(&health)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decodeErr == nil && health["status"] == "all-ok" {
				return
			}
		}

		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("timed out waiting for health endpoint %s to return status %q", healthURL, "all-ok")
		}
	}
}

func doSmokeJSONRequest(t *testing.T, method, rawURL string, body any, token string) *http.Response {
	t.Helper()

	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
	}

	req, err := http.NewRequest(method, rawURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("http.Client.Do() error = %v", err)
	}

	return resp
}

func decodeSmokeJSON(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
}

func openSmokeWebSocket(t *testing.T, baseURL string, strategyID uuid.UUID) *websocket.Conn {
	t.Helper()

	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), http.Header{
		"Origin": []string{"http://localhost"},
	})
	if err != nil {
		t.Fatalf("websocket dial error = %v", err)
	}

	if err := conn.WriteJSON(map[string]any{
		"action":       "subscribe",
		"strategy_ids": []string{strategyID.String()},
	}); err != nil {
		t.Fatalf("websocket subscribe error = %v", err)
	}

	return conn
}

func readSmokeEvents(t *testing.T, conn *websocket.Conn, count int) []apiserver.EventType {
	t.Helper()

	types := make([]apiserver.EventType, 0, count)
	for len(types) < count {
		if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline() error = %v", err)
		}
		var msg apiserver.WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("ReadJSON() error = %v", err)
		}
		if msg.Type == "" {
			continue
		}
		types = append(types, msg.Type)
	}
	return types
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
