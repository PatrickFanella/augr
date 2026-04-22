package discovery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// CreateOrReusePaperStrategy creates a paper strategy if it does not already
// exist, and returns the existing row when a matching strategy is present.
//
// Matching key: (ticker, market_type, is_paper=true, exact name).
func CreateOrReusePaperStrategy(ctx context.Context, repo repository.StrategyRepository, strategy domain.Strategy) (domain.Strategy, bool, error) {
	if !strategy.IsPaper {
		if err := repo.Create(ctx, &strategy); err != nil {
			return domain.Strategy{}, false, err
		}
		return strategy, true, nil
	}

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
		if existing[i].Name == strategy.Name {
			copy := existing[i]
			return &copy, nil
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
