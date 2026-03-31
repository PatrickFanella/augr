package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/cli/tui"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/spf13/cobra"
)

const (
	defaultAPIURL           = "http://127.0.0.1:8080"
	formatTable             = "table"
	formatJSON              = "json"
	serverShutdownTimeout   = 10 * time.Second
	forcedShutdownTimeout   = 30 * time.Second
	forcedShutdownExitCode  = 1
	inFlightPipelineRunsKey = "in_flight_pipeline_runs"
	shutdownSignalKey       = "signal"
	shutdownTimeoutKey      = "timeout"
)

// SchedulerLifecycle is an optional hook for a background job scheduler that
// must be started before the HTTP server begins serving and stopped (gracefully
// draining in-flight runs) before the database connection pool is closed.
type SchedulerLifecycle interface {
	// Start registers cron jobs and begins dispatching them.
	Start() error
	// Stop cancels in-flight job contexts, waits for all running jobs to
	// complete, and prevents new jobs from being dispatched.
	Stop()
	// InFlightCount returns the current number of in-flight pipeline runs.
	InFlightCount() int
}

type Dependencies struct {
	Version      string
	NewAPIServer func(context.Context, config.Config, *slog.Logger) (*api.Server, SchedulerLifecycle, func(), error)
	Stdout       io.Writer
	Stderr       io.Writer
}

type rootState struct {
	stdout       io.Writer
	stderr       io.Writer
	apiURL       string
	token        string
	apiKey       string
	format       string
	version      string
	newAPIServer func(context.Context, config.Config, *slog.Logger) (*api.Server, SchedulerLifecycle, func(), error)
}

type createStrategyOptions struct {
	Name         string
	Description  string
	Ticker       string
	MarketType   string
	ScheduleCron string
	Config       string
	Active       bool
	Paper        bool
}

type shutdownGuard struct {
	logger   *slog.Logger
	timeout  time.Duration
	exitFunc func(int)

	mu        sync.Mutex
	onceStart sync.Once
	onceDone  sync.Once
	timer     stopTimer
	done      bool
	afterFunc func(time.Duration, func()) stopTimer
}

type stopTimer interface {
	Stop() bool
}

func newShutdownGuard(logger *slog.Logger, timeout time.Duration, exitFunc func(int)) *shutdownGuard {
	if logger == nil {
		logger = slog.Default()
	}
	if exitFunc == nil {
		exitFunc = os.Exit
	}
	return &shutdownGuard{
		logger:   logger,
		timeout:  timeout,
		exitFunc: exitFunc,
		afterFunc: func(timeout time.Duration, fn func()) stopTimer {
			return time.AfterFunc(timeout, fn)
		},
	}
}

func (g *shutdownGuard) Begin(sig os.Signal, inFlightCount int) {
	g.onceStart.Do(func() {
		attrs := []slog.Attr{
			slog.Int(inFlightPipelineRunsKey, inFlightCount),
			slog.Duration(shutdownTimeoutKey, g.timeout),
		}
		if sig != nil {
			attrs = append(attrs, slog.String(shutdownSignalKey, sig.String()))
		}

		g.logger.LogAttrs(context.Background(), slog.LevelInfo, "shutdown initiated", attrs...)
		g.logger.LogAttrs(
			context.Background(),
			slog.LevelInfo,
			"waiting for in-flight pipeline runs",
			slog.Int(inFlightPipelineRunsKey, inFlightCount),
		)

		g.timer = g.afterFunc(g.timeout, func() {
			g.forceExit(inFlightCount)
		})
	})
}

func (g *shutdownGuard) Finish() {
	g.onceDone.Do(func() {
		g.mu.Lock()
		g.done = true
		timer := g.timer
		g.mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		g.logger.Info("shutdown complete")
	})
}

func (g *shutdownGuard) forceExit(inFlightCount int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done {
		return
	}

	g.logger.LogAttrs(context.Background(), slog.LevelError, "shutdown timed out; forcing exit",
		slog.Int(inFlightPipelineRunsKey, inFlightCount),
		slog.Duration(shutdownTimeoutKey, g.timeout),
	)
	g.exitFunc(forcedShutdownExitCode)
}

func newSignalContext(parent context.Context, signals ...os.Signal) (context.Context, func(), func() os.Signal) {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signals...)

	var (
		mu          sync.Mutex
		receivedSig os.Signal
	)
	done := make(chan struct{})

	go func() {
		defer close(done)
		select {
		case sig := <-sigCh:
			mu.Lock()
			receivedSig = sig
			mu.Unlock()
			cancel()
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	stop := func() {
		signal.Stop(sigCh)
		cancel()
		<-done
	}

	currentSignal := func() os.Signal {
		mu.Lock()
		defer mu.Unlock()
		return receivedSig
	}

	return ctx, stop, currentSignal
}

func Execute(ctx context.Context, deps Dependencies) error {
	return NewRootCommand(ctx, deps).ExecuteContext(ctx)
}

func NewRootCommand(ctx context.Context, deps Dependencies) *cobra.Command {
	if ctx == nil {
		ctx = context.Background()
	}

	state := &rootState{
		stdout:       deps.Stdout,
		stderr:       deps.Stderr,
		apiURL:       firstNonEmpty(os.Getenv("TRADINGAGENT_API_URL"), defaultAPIURL),
		token:        os.Getenv("TRADINGAGENT_TOKEN"),
		apiKey:       os.Getenv("TRADINGAGENT_API_KEY"),
		format:       formatTable,
		version:      deps.Version,
		newAPIServer: deps.NewAPIServer,
	}
	if state.stdout == nil {
		state.stdout = os.Stdout
	}
	if state.stderr == nil {
		state.stderr = os.Stderr
	}

	rootCmd := &cobra.Command{
		Use:           "tradingagent",
		Short:         "Control the trading agent from the command line",
		Long:          "Control the local trading agent API server, strategies, portfolio, risk state, and memories.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       firstNonEmpty(state.version, "dev"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	rootCmd.SetContext(ctx)
	rootCmd.SetOut(state.stdout)
	rootCmd.SetErr(state.stderr)
	rootCmd.PersistentFlags().StringVar(&state.apiURL, "api-url", state.apiURL, "Base URL for the local trading agent API")
	rootCmd.PersistentFlags().StringVar(&state.token, "token", state.token, "Bearer token for authenticated API requests (or set TRADINGAGENT_TOKEN)")
	rootCmd.PersistentFlags().StringVar(&state.apiKey, "api-key", state.apiKey, "API key for authenticated API requests (or set TRADINGAGENT_API_KEY)")
	rootCmd.PersistentFlags().StringVar(&state.format, "format", state.format, "Output format: table or json")

	rootCmd.AddCommand(state.newServeCommand())
	rootCmd.AddCommand(state.newRunCommand())
	rootCmd.AddCommand(state.newStrategiesCommand())
	rootCmd.AddCommand(state.newDashboardCommand())
	rootCmd.AddCommand(state.newPortfolioCommand())
	rootCmd.AddCommand(state.newRiskCommand())
	rootCmd.AddCommand(state.newMemoriesCommand())

	return rootCmd
}

func (s *rootState) newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP and WebSocket API server",
		Long:  "Start the local trading agent API server using environment-based application configuration.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if s.newAPIServer == nil {
				return errors.New("api server is not configured")
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			level := firstNonEmpty(os.Getenv("LOG_LEVEL"), "info")
			logger := config.SetDefaultLogger(cfg.Environment, level)
			logger.Info("starting trading agent",
				slog.String("env", cfg.Environment),
				slog.String("log_level", level),
			)

			addr := net.JoinHostPort(cfg.Server.Host, fmt.Sprintf("%d", cfg.Server.Port))
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Trading Agent configured for %s on %s\n", cfg.Environment, addr); err != nil {
				return err
			}

			apiServer, sched, cleanup, err := s.newAPIServer(cmd.Context(), cfg, logger)
			if err != nil {
				return fmt.Errorf("build api server: %w", err)
			}
			shutdown := newShutdownGuard(logger, forcedShutdownTimeout, nil)
			defer shutdown.Finish()
			// cleanup closes the DB pool; it must run after the scheduler has
			// drained so in-flight pipeline runs can still write their final
			// status before the pool is closed.
			defer cleanup()

			// Register signal handling BEFORE starting the scheduler so that
			// a SIGTERM arriving during startup is captured (suppressing the
			// default OS-level termination) rather than killing the process
			// mid-startup and skipping all defers.
			ctx, stop, currentSignal := newSignalContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Start the scheduler (if provided) before accepting HTTP traffic.
			// It is stopped in the deferred call below, which runs before
			// cleanup() because defers execute in LIFO order.
			if sched != nil {
				if err := sched.Start(); err != nil {
					return fmt.Errorf("start scheduler: %w", err)
				}
				// Stop runs BEFORE cleanup() (LIFO order), giving in-flight
				// pipeline runs time to persist their terminal status while
				// the DB pool is still open.
				defer sched.Stop()
			}

			if err := runServerLifecycleWithHook(ctx, apiServer.Start, apiServer.Shutdown, func() {
				inFlightCount := 0
				if sched != nil {
					inFlightCount = sched.InFlightCount()
				}
				shutdown.Begin(currentSignal(), inFlightCount)
			}); err != nil {
				return fmt.Errorf("serve http: %w", err)
			}

			logger.Info("trading agent stopped")
			return nil
		},
	}
}

func (s *rootState) newRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run TICKER",
		Short: "Run the first matching strategy pipeline for a ticker",
		Long:  "Resolve a strategy by ticker through the local API and trigger a manual pipeline run.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := s.client()
			if err != nil {
				return err
			}

			ticker := strings.TrimSpace(args[0])
			strategy, err := s.resolveStrategyForTicker(cmd.Context(), client, ticker)
			if err != nil {
				return err
			}

			var result api.StrategyRunResult
			if err := client.post(cmd.Context(), "/api/v1/strategies/"+strategy.ID.String()+"/run", nil, nil, &result); err != nil {
				return err
			}

			output := runOutput{
				Strategy: *strategy,
				Result:   result,
			}
			if s.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), output)
			}
			return renderRunTable(cmd.OutOrStdout(), output)
		},
	}
}

func (s *rootState) newStrategiesCommand() *cobra.Command {
	commands := &cobra.Command{
		Use:   "strategies",
		Short: "List and create trading strategies",
		Long:  "Manage strategy records through the local trading agent API.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	commands.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List strategies",
		Long:  "List strategies from the local API.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := s.client()
			if err != nil {
				return err
			}

			var response listResponse[domain.Strategy]
			if err := client.get(cmd.Context(), "/api/v1/strategies", nil, &response); err != nil {
				return err
			}

			if s.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), response)
			}
			return renderStrategiesTable(cmd.OutOrStdout(), response.Data)
		},
	})

	var options createStrategyOptions
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a strategy",
		Long:  "Create a strategy through the local API using flag-provided fields.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := s.client()
			if err != nil {
				return err
			}

			strategy, err := options.strategy()
			if err != nil {
				return err
			}

			var created domain.Strategy
			if err := client.post(cmd.Context(), "/api/v1/strategies", nil, strategy, &created); err != nil {
				return err
			}

			if s.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), created)
			}
			return renderStrategiesTable(cmd.OutOrStdout(), []domain.Strategy{created})
		},
	}
	createCmd.Flags().StringVar(&options.Name, "name", "", "Strategy name")
	createCmd.Flags().StringVar(&options.Description, "description", "", "Strategy description")
	createCmd.Flags().StringVar(&options.Ticker, "ticker", "", "Ticker symbol for the strategy")
	createCmd.Flags().StringVar(&options.MarketType, "market-type", "", "Market type: stock, crypto, or polymarket")
	createCmd.Flags().StringVar(&options.ScheduleCron, "schedule-cron", "", "Optional cron expression for scheduled runs")
	createCmd.Flags().StringVar(&options.Config, "config", "", "Optional JSON object for strategy-specific configuration")
	createCmd.Flags().BoolVar(&options.Active, "active", true, "Whether the strategy is active")
	createCmd.Flags().BoolVar(&options.Paper, "paper", true, "Whether the strategy uses paper trading")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("ticker")
	_ = createCmd.MarkFlagRequired("market-type")
	commands.AddCommand(createCmd)

	return commands
}

func (s *rootState) newPortfolioCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "portfolio",
		Short: "Show open positions and portfolio summary",
		Long:  "Fetch portfolio summary information and current open positions from the local API.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := s.client()
			if err != nil {
				return err
			}

			var summary portfolioSummary
			if err := client.get(cmd.Context(), "/api/v1/portfolio/summary", nil, &summary); err != nil {
				return err
			}

			var positions listResponse[domain.Position]
			if err := client.get(cmd.Context(), "/api/v1/portfolio/positions/open", nil, &positions); err != nil {
				return err
			}

			output := portfolioOutput{
				Summary:   summary,
				Positions: positions.Data,
			}
			if s.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), output)
			}
			return renderPortfolioTable(cmd.OutOrStdout(), output)
		},
	}
}

func (s *rootState) newRiskCommand() *cobra.Command {
	commands := &cobra.Command{
		Use:   "risk",
		Short: "Inspect or change risk controls",
		Long:  "Inspect current risk state or activate the kill switch through the local API.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	commands.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show risk state",
		Long:  "Show the current risk engine status, circuit breaker, and kill switch state.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := s.client()
			if err != nil {
				return err
			}

			var status risk.EngineStatus
			if err := client.get(cmd.Context(), "/api/v1/risk/status", nil, &status); err != nil {
				return err
			}

			if s.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), status)
			}
			return renderRiskStatusTable(cmd.OutOrStdout(), status)
		},
	})

	var reason string
	killCmd := &cobra.Command{
		Use:   "kill",
		Short: "Activate the risk kill switch",
		Long:  "Activate the local API risk kill switch. Provide a reason for auditability.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := s.client()
			if err != nil {
				return err
			}

			response := map[string]bool{}
			if err := client.post(cmd.Context(), "/api/v1/risk/killswitch", nil, map[string]any{
				"active": true,
				"reason": reason,
			}, &response); err != nil {
				return err
			}

			if s.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), response)
			}
			return writeTable(cmd.OutOrStdout(), []string{"FIELD", "VALUE"}, [][]string{
				{"Kill switch active", fmt.Sprintf("%t", response["active"])},
				{"Reason", reason},
			})
		},
	}
	killCmd.Flags().StringVar(&reason, "reason", "activated from CLI", "Reason recorded when activating the kill switch")
	commands.AddCommand(killCmd)

	return commands
}

func (s *rootState) newMemoriesCommand() *cobra.Command {
	commands := &cobra.Command{
		Use:   "memories",
		Short: "Search stored agent memories",
		Long:  "Search the agent memory index through the local API.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	commands.AddCommand(&cobra.Command{
		Use:   "search QUERY",
		Short: "Search memories",
		Long:  "Search stored memories by natural-language query text.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := s.client()
			if err != nil {
				return err
			}

			var response listResponse[domain.AgentMemory]
			if err := client.post(cmd.Context(), "/api/v1/memories/search", nil, map[string]string{"query": args[0]}, &response); err != nil {
				return err
			}

			if s.format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), response)
			}
			return renderMemoriesTable(cmd.OutOrStdout(), response.Data)
		},
	})

	return commands
}

func (s *rootState) client() (*apiClient, error) {
	if s.format != formatTable && s.format != formatJSON {
		return nil, fmt.Errorf("unsupported format %q", s.format)
	}
	baseURL, err := url.Parse(s.apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid api url: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid api url %q", s.apiURL)
	}
	return newAPIClient(baseURL.String(), s.token, s.apiKey), nil
}

func (s *rootState) tuiSnapshot(
	ctx context.Context,
	client *apiClient,
) (tui.Snapshot, error) {
	var strategies listResponse[domain.Strategy]
	if err := client.get(ctx, "/api/v1/strategies", nil, &strategies); err != nil {
		return tui.Snapshot{}, err
	}

	var summary portfolioSummary
	if err := client.get(ctx, "/api/v1/portfolio/summary", nil, &summary); err != nil {
		return tui.Snapshot{}, err
	}

	var positions listResponse[domain.Position]
	if err := client.get(ctx, "/api/v1/portfolio/positions/open", nil, &positions); err != nil {
		return tui.Snapshot{}, err
	}

	var status risk.EngineStatus
	if err := client.get(ctx, "/api/v1/risk/status", nil, &status); err != nil {
		return tui.Snapshot{}, err
	}

	var runs listResponse[domain.PipelineRun]
	runQuery := url.Values{}
	runQuery.Set("limit", "10")
	if err := client.get(ctx, "/api/v1/runs", runQuery, &runs); err != nil {
		return tui.Snapshot{}, err
	}

	var settings api.SettingsResponse
	if err := client.get(ctx, "/api/v1/settings", nil, &settings); err != nil {
		return tui.Snapshot{}, err
	}

	snapshot := tui.Snapshot{
		Portfolio: tui.PortfolioSummary{
			OpenPositions: summary.OpenPositions,
			UnrealizedPnL: summary.UnrealizedPnL,
			RealizedPnL:   summary.RealizedPnL,
		},
		Positions:  positions.Data,
		Strategies: activeStrategies(strategies.Data),
		Risk:       status,
		Settings:   settings,
	}
	if len(runs.Data) > 0 {
		run := runs.Data[0]
		snapshot.LatestRun = &run
		snapshot.Activity = append(snapshot.Activity, tui.ActivityItem{
			OccurredAt: run.StartedAt,
			Title:      "Latest pipeline run",
			Details:    fmt.Sprintf("%s • %s • %s", emptyDash(run.Ticker), run.Status.String(), run.ID.String()),
		})
	}
	return snapshot, nil
}

func activeStrategies(strategies []domain.Strategy) []domain.Strategy {
	active := make([]domain.Strategy, 0, len(strategies))
	for _, strategy := range strategies {
		if strategy.IsActive {
			active = append(active, strategy)
		}
	}
	return active
}

func (s *rootState) resolveStrategyForTicker(ctx context.Context, client *apiClient, ticker string) (*domain.Strategy, error) {
	var response listResponse[domain.Strategy]
	query := url.Values{}
	query.Set("ticker", ticker)
	query.Set("limit", "100")
	if err := client.get(ctx, "/api/v1/strategies", query, &response); err != nil {
		return nil, err
	}

	exactMatches := make([]domain.Strategy, 0, len(response.Data))
	activeMatches := make([]domain.Strategy, 0, len(response.Data))
	for _, strategy := range response.Data {
		if !strings.EqualFold(strategy.Ticker, ticker) {
			continue
		}
		exactMatches = append(exactMatches, strategy)
		if strategy.IsActive {
			activeMatches = append(activeMatches, strategy)
		}
	}

	switch {
	case len(exactMatches) == 0:
		return nil, fmt.Errorf("no strategy found for ticker %q", ticker)
	case len(exactMatches) == 1:
		return &exactMatches[0], nil
	case len(activeMatches) == 1:
		return &activeMatches[0], nil
	default:
		return nil, fmt.Errorf("multiple strategies found for ticker %q; use `tradingagent strategies list` to resolve the ambiguity", ticker)
	}
}

func (o createStrategyOptions) strategy() (domain.Strategy, error) {
	strategy := domain.Strategy{
		Name:         strings.TrimSpace(o.Name),
		Description:  strings.TrimSpace(o.Description),
		Ticker:       strings.TrimSpace(o.Ticker),
		MarketType:   domain.MarketType(strings.ToLower(strings.TrimSpace(o.MarketType))),
		ScheduleCron: strings.TrimSpace(o.ScheduleCron),
		IsActive:     o.Active,
		IsPaper:      o.Paper,
	}
	if strings.TrimSpace(o.Config) != "" {
		raw := strings.TrimSpace(o.Config)
		if !json.Valid([]byte(raw)) {
			return domain.Strategy{}, fmt.Errorf("config must be valid JSON")
		}
		strategy.Config = domain.StrategyConfig([]byte(raw))
	}
	if err := strategy.Validate(); err != nil {
		return domain.Strategy{}, err
	}
	return strategy, nil
}

func runServerLifecycleWithHook(
	ctx context.Context,
	serve func() error,
	shutdown func(context.Context) error,
	onShutdownInitiated func(),
) error {
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- serve()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		if onShutdownInitiated != nil {
			onShutdownInitiated()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()

	if err := shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	err := <-serverErr
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runServerLifecycle(ctx context.Context, serve func() error, shutdown func(context.Context) error) error {
	return runServerLifecycleWithHook(ctx, serve, shutdown, nil)
}

// RunServerLifecycle starts a server and shuts it down when the context is canceled.
func RunServerLifecycle(ctx context.Context, serve func() error, shutdown func(context.Context) error) error {
	return runServerLifecycle(ctx, serve, shutdown)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
