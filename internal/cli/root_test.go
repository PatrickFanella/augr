package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

type rootCommandTestContextKey struct{}

func TestCommandHelp(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"--help"},
		{"serve", "--help"},
		{"run", "--help"},
		{"strategies", "--help"},
		{"strategies", "list", "--help"},
		{"strategies", "create", "--help"},
		{"portfolio", "--help"},
		{"risk", "--help"},
		{"risk", "status", "--help"},
		{"risk", "kill", "--help"},
		{"memories", "--help"},
		{"memories", "search", "--help"},
	}

	for _, args := range cases {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()

			stdout, stderr, err := executeCLI(t, nil, args...)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			output := stdout + stderr
			if !strings.Contains(output, "Usage:") {
				t.Fatalf("help output missing usage for %v:\n%s", args, output)
			}
		})
	}
}

func TestCLICommands(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	runID := uuid.New()
	positionID := uuid.New()
	now := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)
	currentPrice := 187.25
	unrealized := 12.5
	relevance := 0.92

	strategy := domain.Strategy{
		ID:         strategyID,
		Name:       "AAPL Trend",
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		IsActive:   true,
		IsPaper:    true,
		UpdatedAt:  now,
	}
	runResult := api.StrategyRunResult{
		Run: domain.PipelineRun{
			ID:         runID,
			StrategyID: strategyID,
			Ticker:     "AAPL",
			Status:     domain.PipelineStatusCompleted,
			Signal:     domain.PipelineSignalBuy,
			StartedAt:  now,
		},
		Signal: domain.PipelineSignalBuy,
		Orders: []domain.Order{{ID: uuid.New()}},
		Positions: []domain.Position{{
			ID:            positionID,
			Ticker:        "AAPL",
			Side:          domain.PositionSideLong,
			Quantity:      5,
			AvgEntry:      184.75,
			CurrentPrice:  &currentPrice,
			UnrealizedPnL: &unrealized,
			RealizedPnL:   2.5,
			OpenedAt:      now,
		}},
	}

	var handlerErrs []error
	var handlerErrsMu sync.Mutex
	recordHandlerError := func(err error) {
		handlerErrsMu.Lock()
		handlerErrs = append(handlerErrs, err)
		handlerErrsMu.Unlock()
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/strategies":
			_ = json.NewEncoder(w).Encode(listResponse[domain.Strategy]{
				Data:  []domain.Strategy{strategy},
				Limit: 100,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/strategies":
			var created domain.Strategy
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				recordHandlerError(err)
				http.Error(w, "invalid create strategy request", http.StatusBadRequest)
				return
			}
			created.ID = strategyID
			created.UpdatedAt = now
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(created)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/strategies/"+strategyID.String()+"/run":
			_ = json.NewEncoder(w).Encode(runResult)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/portfolio/summary":
			_ = json.NewEncoder(w).Encode(portfolioSummary{
				OpenPositions: 1,
				UnrealizedPnL: 12.5,
				RealizedPnL:   2.5,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/portfolio/positions/open":
			_ = json.NewEncoder(w).Encode(listResponse[domain.Position]{
				Data: runResult.Positions,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/risk/status":
			_ = json.NewEncoder(w).Encode(risk.EngineStatus{
				RiskStatus: domain.RiskStatusNormal,
				CircuitBreaker: risk.CircuitBreakerStatus{
					State: risk.CircuitBreakerPhaseOpen,
				},
				KillSwitch: risk.KillSwitchStatus{
					Active: false,
				},
				PositionLimits: risk.PositionLimits{
					MaxPerPositionPct: 0.1,
					MaxTotalPct:       1.0,
					MaxConcurrent:     10,
					MaxPerMarketPct:   0.5,
				},
				UpdatedAt: now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/risk/killswitch":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				recordHandlerError(err)
				http.Error(w, "invalid kill switch request", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"active": true})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/memories/search":
			_ = json.NewEncoder(w).Encode(listResponse[domain.AgentMemory]{
				Data: []domain.AgentMemory{{
					ID:             uuid.New(),
					AgentRole:      domain.AgentRoleMarketAnalyst,
					Situation:      "AAPL breakout above resistance",
					Recommendation: "Increase conviction on momentum trades",
					RelevanceScore: &relevance,
					CreatedAt:      now,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Cleanup(func() {
		handlerErrsMu.Lock()
		defer handlerErrsMu.Unlock()
		if len(handlerErrs) == 0 {
			return
		}
		t.Fatalf("mock api handler errors: %v", handlerErrs)
	})

	t.Run("run command resolves ticker and returns json", func(t *testing.T) {
		stdout, _, err := executeCLI(t, nil, "--api-url", server.URL, "--format", "json", "run", "AAPL")
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		var output runOutput
		if err := json.Unmarshal([]byte(stdout), &output); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
		}
		if output.Strategy.ID != strategyID {
			t.Fatalf("strategy id = %s, want %s", output.Strategy.ID, strategyID)
		}
		if output.Result.Run.ID != runID {
			t.Fatalf("run id = %s, want %s", output.Result.Run.ID, runID)
		}
	})

	t.Run("strategies list prints table rows", func(t *testing.T) {
		stdout, _, err := executeCLI(t, nil, "--api-url", server.URL, "strategies", "list")
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !strings.Contains(stdout, "NAME") || !strings.Contains(stdout, "AAPL Trend") {
			t.Fatalf("unexpected strategies output:\n%s", stdout)
		}
	})

	t.Run("strategies create sends request and returns json", func(t *testing.T) {
		stdout, _, err := executeCLI(
			t,
			nil,
			"--api-url", server.URL,
			"--format", "json",
			"strategies", "create",
			"--name", "AAPL Trend",
			"--ticker", "AAPL",
			"--market-type", "stock",
			"--config", `{"window":20}`,
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		var created domain.Strategy
		if err := json.Unmarshal([]byte(stdout), &created); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
		}
		var configBody map[string]int
		if err := json.Unmarshal(created.Config, &configBody); err != nil {
			t.Fatalf("config is not valid json: %v", err)
		}
		if configBody["window"] != 20 {
			t.Fatalf("window = %d, want %d", configBody["window"], 20)
		}
	})

	t.Run("portfolio prints summary and positions", func(t *testing.T) {
		stdout, _, err := executeCLI(t, nil, "--api-url", server.URL, "portfolio")
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !strings.Contains(stdout, "Open positions") || !strings.Contains(stdout, "AAPL") {
			t.Fatalf("unexpected portfolio output:\n%s", stdout)
		}
	})

	t.Run("risk commands support json and table output", func(t *testing.T) {
		stdout, _, err := executeCLI(t, nil, "--api-url", server.URL, "--format", "json", "risk", "status")
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		var status risk.EngineStatus
		if err := json.Unmarshal([]byte(stdout), &status); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
		}
		if status.RiskStatus != domain.RiskStatusNormal {
			t.Fatalf("risk status = %s, want %s", status.RiskStatus, domain.RiskStatusNormal)
		}

		stdout, _, err = executeCLI(t, nil, "--api-url", server.URL, "risk", "kill", "--reason", "manual test")
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !strings.Contains(stdout, "manual test") || !strings.Contains(stdout, "true") {
			t.Fatalf("unexpected kill output:\n%s", stdout)
		}
	})

	t.Run("memories search prints results", func(t *testing.T) {
		stdout, _, err := executeCLI(t, nil, "--api-url", server.URL, "memories", "search", "AAPL breakout")
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !strings.Contains(stdout, "market_analyst") || !strings.Contains(stdout, "AAPL breakout") {
			t.Fatalf("unexpected memories output:\n%s", stdout)
		}
	})
}

func TestAPIClientIncludesNonJSONErrorDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.Error(w, "backend exploded", http.StatusBadGateway)
	}))
	defer server.Close()

	client := newAPIClient(server.URL, "", "")
	err := client.get(context.Background(), "/broken", nil, nil)
	if err == nil {
		t.Fatal("client.get() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "backend exploded") {
		t.Fatalf("error = %q, want response snippet", err)
	}
	if !strings.Contains(err.Error(), "text/plain") {
		t.Fatalf("error = %q, want content type", err)
	}
}

func TestNewRootCommandUsesProvidedContext(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), rootCommandTestContextKey{}, "value")
	cmd := NewRootCommand(ctx, Dependencies{})
	if cmd.Context() != ctx {
		t.Fatalf("command context = %v, want provided context", cmd.Context())
	}
}

func TestCommandsRejectUnexpectedArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		args []string
	}{
		{name: "strategies", args: []string{"strategies", "extra"}},
		{name: "strategies list", args: []string{"strategies", "list", "extra"}},
		{name: "strategies create", args: []string{"strategies", "create", "--name", "AAPL Trend", "--ticker", "AAPL", "--market-type", "stock", "extra"}},
		{name: "portfolio", args: []string{"portfolio", "extra"}},
		{name: "risk", args: []string{"risk", "extra"}},
		{name: "risk status", args: []string{"risk", "status", "extra"}},
		{name: "risk kill", args: []string{"risk", "kill", "extra"}},
		{name: "memories", args: []string{"memories", "extra"}},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := executeCLI(t, nil, tc.args...)
			if err == nil {
				t.Fatal("Execute() error = nil, want error")
			}
			if !strings.Contains(err.Error(), "unknown command") && !strings.Contains(err.Error(), "accepts 0 arg(s), received 1") {
				t.Fatalf("error = %q, want arg validation error", err)
			}
		})
	}
}

func executeCLI(t *testing.T, deps *Dependencies, args ...string) (string, string, error) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	var rootDeps Dependencies
	if deps != nil {
		rootDeps = *deps
	}
	rootDeps.Stdout = &stdout
	rootDeps.Stderr = &stderr

	cmd := NewRootCommand(context.Background(), rootDeps)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}
