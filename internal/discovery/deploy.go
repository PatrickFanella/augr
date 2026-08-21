package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const researchLifecycleConfigKey = "research_lifecycle"

// PrepareResearchIdea converts a generated strategy into a fail-closed research
// artifact. Discovery is allowed to propose and persist ideas, but activation
// is a separate, deterministic promotion action.
func PrepareResearchIdea(strategy domain.Strategy) (domain.Strategy, error) {
	if !strategy.IsPaper {
		return domain.Strategy{}, errors.New("discovery: generated strategies must be paper-only")
	}

	config := make(map[string]any)
	if len(strategy.Config) > 0 {
		if err := json.Unmarshal(strategy.Config, &config); err != nil {
			return domain.Strategy{}, fmt.Errorf("discovery: generated strategy config must be a JSON object: %w", err)
		}
		if config == nil {
			return domain.Strategy{}, errors.New("discovery: generated strategy config must be a JSON object")
		}
	}
	config[researchLifecycleConfigKey] = map[string]any{
		"stage":                   "idea",
		"activation":              "manual_promotion_only",
		"auto_activation_blocked": true,
	}
	preparedConfig, err := json.Marshal(config)
	if err != nil {
		return domain.Strategy{}, fmt.Errorf("discovery: marshal research lifecycle: %w", err)
	}

	strategy.IsPaper = true
	strategy.Status = domain.StrategyStatusInactive
	strategy.ScheduleCron = ""
	strategy.SkipNextRun = false
	strategy.Config = preparedConfig
	return strategy, nil
}

// CreateOrReusePaperStrategy creates an inactive, unscheduled research idea if
// it does not already exist, and returns the existing row when a matching
// strategy is present. Existing rows are never implicitly activated, paused,
// rescheduled, or otherwise modified by discovery.
//
// Matching key: (ticker, market_type, is_paper=true, exact name). For native
// event markets (Polymarket and Kalshi), ticker is the canonical market ID, so
// any existing paper strategy for the same market is reused even if an older
// display name differs.
func CreateOrReusePaperStrategy(ctx context.Context, repo repository.StrategyRepository, strategy domain.Strategy) (domain.Strategy, bool, error) {
	prepared, err := PrepareResearchIdea(strategy)
	if err != nil {
		return domain.Strategy{}, false, err
	}
	strategy = prepared

	existing, err := findExistingPaperStrategy(ctx, repo, strategy)
	if err != nil {
		return domain.Strategy{}, false, err
	}
	if existing != nil {
		return *existing, false, nil
	}

	if err := repo.Create(ctx, &strategy); err != nil {
		// Handle races where another runner inserted the same strategy between
		// the List and Create calls.
		if !isUniqueViolation(err) {
			return domain.Strategy{}, false, err
		}

		existingAfterConflict, lookupErr := findExistingPaperStrategy(ctx, repo, strategy)
		if lookupErr != nil {
			return domain.Strategy{}, false, fmt.Errorf("lookup existing strategy after unique conflict: %w", lookupErr)
		}
		if existingAfterConflict == nil {
			return domain.Strategy{}, false, err
		}
		return *existingAfterConflict, false, nil
	}

	return strategy, true, nil
}

func findExistingPaperStrategy(ctx context.Context, repo repository.StrategyRepository, strategy domain.Strategy) (*domain.Strategy, error) {
	isPaper := true
	existing, err := repo.List(ctx, repository.StrategyFilter{
		Ticker:     strategy.Ticker,
		MarketType: strategy.MarketType,
		IsPaper:    &isPaper,
	}, 200, 0)
	if err != nil {
		return nil, fmt.Errorf("list existing strategies for %s: %w", strategy.Ticker, err)
	}

	for i := range existing {
		if eventmarkets.ReuseByTickerOnly(strategy.MarketType) {
			cloned := existing[i]
			return &cloned, nil
		}
		if existing[i].Name == strategy.Name {
			cloned := existing[i]
			return &cloned, nil
		}
	}

	return nil, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "duplicate key") || strings.Contains(errText, "unique")
}
