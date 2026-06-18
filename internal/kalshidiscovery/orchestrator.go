package kalshidiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	defaultKalshiScheduleCron   = "0 */6 * * *"
	defaultKalshiFetchLimit     = 100
	defaultKalshiMaxDeployments = 3
	defaultKalshiMinConviction  = 0.60
	kalshiProposalTemplate      = "microstructure"
)

// Config bundles the Kalshi discovery fetch, screening, and deployment settings.
type Config struct {
	FetchLimit     int
	Screener       ScreenerConfig
	ScheduleCron   string
	MaxDeployments int
	DryRun         bool
	MinConviction  float64
}

// Deps bundles external dependencies for one Kalshi discovery run.
type Deps struct {
	Catalog interface {
		ListMarkets(context.Context, ListOptions) ([]MarketCandidate, string, error)
	}
	Strategies    repository.StrategyRepository
	Watched       repository.KalshiWatchedMarketsRepository
	Snapshots     repository.KalshiMarketSnapshotsRepository
	DiscoveryRuns repository.KalshiDiscoveryRunRepository
	Logger        *slog.Logger
}

// DeployedStrategy summarizes one strategy created or reused by the pipeline.
type DeployedStrategy struct {
	StrategyID uuid.UUID `json:"strategy_id"`
	Ticker     string    `json:"ticker"`
	Template   string    `json:"template"`
	Name       string    `json:"name"`
	Direction  string    `json:"direction"`
	Conviction float64   `json:"conviction"`
	Reused     bool      `json:"reused"`
}

// Result summarizes one full discovery run.
type Result struct {
	StartedAt  time.Time          `json:"started_at"`
	Duration   time.Duration      `json:"duration"`
	FetchedAll int                `json:"fetched_all"`
	Screened   int                `json:"screened"`
	Proposed   int                `json:"proposed"`
	Skipped    int                `json:"skipped"`
	Deployed   []DeployedStrategy `json:"deployed"`
	Errors     []string           `json:"errors,omitempty"`
	DryRun     bool               `json:"dry_run"`
}

// Run executes a full Kalshi discovery pipeline.
func Run(ctx context.Context, cfg Config, deps Deps) (res *Result, err error) {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.FetchLimit <= 0 {
		cfg.FetchLimit = defaultKalshiFetchLimit
	}
	if cfg.MaxDeployments <= 0 {
		cfg.MaxDeployments = defaultKalshiMaxDeployments
	}
	if strings.TrimSpace(cfg.ScheduleCron) == "" {
		cfg.ScheduleCron = defaultKalshiScheduleCron
	}
	if cfg.MinConviction <= 0 {
		cfg.MinConviction = defaultKalshiMinConviction
	}
	if cfg.Screener.MaxCandidates == 0 && cfg.Screener.MinVolume == 0 && cfg.Screener.MinOpenInterest == 0 && cfg.Screener.MaxSpreadPct == 0 && cfg.Screener.MinDaysToClose == 0 && len(cfg.Screener.Categories) == 0 {
		cfg.Screener = DefaultScreenerConfig()
	}
	if deps.Catalog == nil {
		return nil, fmt.Errorf("kalshidiscovery: Catalog client required")
	}
	if !cfg.DryRun && deps.Strategies == nil {
		return nil, fmt.Errorf("kalshidiscovery: StrategyRepository required")
	}

	res = &Result{StartedAt: time.Now().UTC(), DryRun: cfg.DryRun}
	run := &domain.KalshiDiscoveryRun{
		StartedAt: res.StartedAt,
		Status:    domain.KalshiDiscoveryStatusRunning,
		Result: domain.KalshiDiscoveryResult{
			Summary: mustJSON(map[string]any{
				"source":          "kalshi_discovery",
				"dry_run":         cfg.DryRun,
				"fetch_limit":     cfg.FetchLimit,
				"max_deployments": cfg.MaxDeployments,
				"min_conviction":  cfg.MinConviction,
				"schedule_cron":   cfg.ScheduleCron,
			}),
		},
	}
	if !cfg.DryRun && deps.DiscoveryRuns != nil {
		if createErr := deps.DiscoveryRuns.Create(ctx, run); createErr != nil {
			return nil, fmt.Errorf("kalshidiscovery: create discovery run: %w", createErr)
		}
		defer func() {
			if run == nil {
				return
			}
			run.Result.Fetched = res.FetchedAll
			run.Result.Screened = res.Screened
			run.Result.Proposed = res.Proposed
			run.Result.Deployed = len(res.Deployed)
			run.Result.Errors = append([]string(nil), res.Errors...)
			run.FinishedAt = ptrTime(time.Now().UTC())
			if err != nil {
				run.Status = domain.KalshiDiscoveryStatusFailed
			} else {
				run.Status = domain.KalshiDiscoveryStatusCompleted
			}
			if finishErr := deps.DiscoveryRuns.Finish(ctx, run); finishErr != nil && err == nil {
				err = finishErr
			} else if finishErr != nil {
				logger.Warn("kalshidiscovery: discovery run finish failed", slog.Any("error", finishErr), slog.String("run_id", run.ID.String()))
			}
		}()
	}

	start := res.StartedAt
	pageOpts := ListOptions{Limit: cfg.FetchLimit, Status: "open"}
	var all []MarketCandidate
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		page, cursor, fetchErr := deps.Catalog.ListMarkets(ctx, pageOpts)
		if fetchErr != nil {
			return res, fmt.Errorf("kalshidiscovery: fetch markets: %w", fetchErr)
		}
		for _, candidate := range page {
			all = append(all, candidate)
			if !cfg.DryRun && deps.Snapshots != nil {
				if snapErr := deps.Snapshots.Create(ctx, candidate.ToSnapshot()); snapErr != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("snapshot %s: %v", candidate.Ticker, snapErr))
				}
			}
		}
		if strings.TrimSpace(cursor) == "" {
			break
		}
		pageOpts.Cursor = cursor
	}
	res.FetchedAll = len(all)

	accepted, rejected := ScreenMarketsDetailed(all, cfg.Screener, time.Now().UTC())
	res.Screened = len(accepted)
	if len(rejected) > 0 {
		logger.Info("kalshidiscovery: candidates screened", slog.Int("fetched", len(all)), slog.Int("accepted", len(accepted)), slog.Int("rejected", len(rejected)))
	} else {
		logger.Info("kalshidiscovery: candidates screened", slog.Int("fetched", len(all)), slog.Int("accepted", len(accepted)))
	}

	if !cfg.DryRun && deps.Watched != nil {
		for _, candidate := range accepted {
			if watchErr := deps.Watched.Upsert(ctx, candidate.ToWatchedMarket()); watchErr != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("watch %s: %v", candidate.Ticker, watchErr))
			}
		}
	}

	for _, candidate := range accepted {
		if len(res.Deployed) >= cfg.MaxDeployments {
			break
		}
		proposal, buildErr := buildDeterministicProposal(candidate, cfg)
		if buildErr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("proposal %s: %v", candidate.Ticker, buildErr))
			continue
		}
		res.Proposed++
		if proposal.Conviction < cfg.MinConviction {
			res.Skipped++
			continue
		}
		deployed, deployErr := DeployStrategy(ctx, cfg, deps, candidate, proposal)
		if deployErr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("deploy %s: %v", candidate.Ticker, deployErr))
			continue
		}
		res.Deployed = append(res.Deployed, deployed)
	}

	res.Duration = time.Since(start)
	logger.Info("kalshidiscovery: run complete",
		slog.Int("fetched", res.FetchedAll),
		slog.Int("screened", res.Screened),
		slog.Int("proposed", res.Proposed),
		slog.Int("skipped", res.Skipped),
		slog.Int("deployed", len(res.Deployed)),
		slog.Duration("duration", res.Duration),
		slog.Bool("dry_run", cfg.DryRun),
	)
	return res, nil
}

// DeployStrategy validates a Kalshi proposal and creates or reuses an active
// paper strategy for the market.
func DeployStrategy(
	ctx context.Context,
	cfg Config,
	deps Deps,
	candidate MarketCandidate,
	proposal Proposal,
) (DeployedStrategy, error) {
	if err := ctx.Err(); err != nil {
		return DeployedStrategy{}, err
	}
	if proposal.Skip {
		return DeployedStrategy{}, fmt.Errorf("cannot deploy skipped proposal for %s: %s", candidate.Ticker, strings.TrimSpace(proposal.SkipReason))
	}
	if err := ValidateProposalForMarket(&proposal, candidate); err != nil {
		return DeployedStrategy{}, fmt.Errorf("quarantine invalid proposal for %s: %w", candidate.Ticker, err)
	}

	if strings.TrimSpace(cfg.ScheduleCron) == "" {
		cfg.ScheduleCron = defaultKalshiScheduleCron
	}

	strategy := domain.Strategy{
		ID:           uuid.New(),
		Name:         kalshiStrategyName(candidate),
		Description:  proposal.Summary,
		Ticker:       candidate.Ticker,
		MarketType:   domain.MarketTypeKalshi,
		ScheduleCron: cfg.ScheduleCron,
		IsPaper:      true,
		Status:       domain.StrategyStatusActive,
	}
	strategy.Config = mustJSON(map[string]any{
		"discovery_meta": map[string]any{
			"source":                    "kalshi_discovery",
			"market_ticker":             candidate.Ticker,
			"event_ticker":              candidate.EventTicker,
			"template":                  proposal.Template,
			"direction":                 proposal.Direction,
			"conviction":                proposal.Conviction,
			"confidence":                proposal.Conviction,
			"time_horizon":              proposal.TimeHorizon,
			"entry_price_max":           proposal.EntryPriceMax,
			"price_ceiling":             proposal.EntryPriceMax,
			"source_references":         proposal.SourceReferences,
			"max_spread_pct":            proposal.MaxSpreadPct,
			"min_liquidity":             proposal.MinLiquidity,
			"stop_policy":               proposal.StopPolicy,
			"target_policy":             proposal.TargetPolicy,
			"watch_terms":               proposal.WatchTerms,
			"invalidate_if":             proposal.InvalidateIf,
			"native_execution_required": true,
		},
	})

	out := DeployedStrategy{
		StrategyID: strategy.ID,
		Ticker:     candidate.Ticker,
		Template:   proposal.Template,
		Name:       strategy.Name,
		Direction:  proposal.Direction,
		Conviction: proposal.Conviction,
	}
	if cfg.DryRun {
		return out, nil
	}
	if deps.Strategies == nil {
		return DeployedStrategy{}, fmt.Errorf("kalshidiscovery: StrategyRepository required for deployment")
	}

	created, createdNew, createErr := discovery.CreateOrReusePaperStrategy(ctx, deps.Strategies, strategy)
	if createErr != nil {
		return DeployedStrategy{}, createErr
	}
	out.StrategyID = created.ID
	out.Reused = !createdNew
	return out, nil
}

func buildDeterministicProposal(candidate MarketCandidate, cfg Config) (Proposal, error) {
	if strings.TrimSpace(candidate.Ticker) == "" {
		return Proposal{}, fmt.Errorf("missing ticker")
	}
	direction, selectedAsk, selectedBid := chooseKalshiDirection(candidate)
	entryMax := math.Min(1, round4(maxFloat(selectedAsk+0.02, 0.01)))
	if entryMax <= 0 {
		entryMax = 0.5
	}
	spreadPct, _ := executableSpreadPct(selectedBid, selectedAsk)
	if math.IsNaN(spreadPct) || math.IsInf(spreadPct, 0) {
		spreadPct = 100
	}
	liquidityScore := clamp01(math.Log10(1+math.Max(0, math.Min(candidate.Volume, candidate.OpenInterest))) / 4)
	spreadScore := clamp01(1 - (spreadPct / 25))
	conviction := 0.58 + (0.22 * liquidityScore) + (0.20 * spreadScore)
	conviction = clamp(conviction, 0.55, 0.88)
	watchTerms := compactStrings([]string{candidate.EventTicker, candidate.Ticker})
	if len(watchTerms) == 0 {
		watchTerms = []string{candidate.Ticker}
	}
	horizon := kalshiTimeHorizon(candidate)
	proposal := Proposal{
		Template:         kalshiProposalTemplate,
		Name:             kalshiStrategyName(candidate),
		Summary:          fmt.Sprintf("Conservative microstructure setup for Kalshi market %s.", candidate.Ticker),
		Direction:        direction,
		Conviction:       conviction,
		TimeHorizon:      horizon,
		EntryPriceMax:    entryMax,
		WatchTerms:       watchTerms,
		InvalidateIf:     []string{"market closes or settles against the thesis", "executed side no longer stays tightly priced"},
		SourceReferences: []string{"kalshi_market:" + candidate.Ticker},
		MaxSpreadPct:     math.Max(1, math.Min(100, round4(spreadPct+2))),
		MinLiquidity:     math.Max(1000, math.Min(candidate.Volume, candidate.OpenInterest)),
		StopPolicy:       "hold until invalidation or close window",
		TargetPolicy:     "paper-only target based on repricing toward fair value",
	}
	if err := ValidateProposal(&proposal); err != nil {
		return Proposal{}, err
	}
	if proposal.Direction != direction {
		return Proposal{}, fmt.Errorf("direction normalization changed from %s to %s", direction, proposal.Direction)
	}
	if proposal.EntryPriceMax > 1 {
		proposal.EntryPriceMax = 1
	}
	return proposal, nil
}

func chooseKalshiDirection(candidate MarketCandidate) (direction string, ask float64, bid float64) {
	if candidate.NoAsk > 0 && (candidate.YesAsk <= 0 || candidate.NoAsk < candidate.YesAsk) {
		return "NO", candidate.NoAsk, candidate.NoBid
	}
	return "YES", candidate.YesAsk, candidate.YesBid
}

func kalshiTimeHorizon(candidate MarketCandidate) string {
	if candidate.CloseTime == nil || candidate.CloseTime.IsZero() {
		return "days"
	}
	remaining := time.Until(*candidate.CloseTime)
	switch {
	case remaining <= 48*time.Hour:
		return "hours"
	case remaining <= 14*24*time.Hour:
		return "days"
	default:
		return "weeks"
	}
}

func kalshiStrategyName(candidate MarketCandidate) string {
	return fmt.Sprintf("auto: kalshi %s", strings.TrimSpace(candidate.Ticker))
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func ptrTime(t time.Time) *time.Time { return &t }

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clamp01(v float64) float64 { return clamp(v, 0, 1) }

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
