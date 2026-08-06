package universe

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
)

// Universe manages the ticker universe: discovery, grouping, and watchlists.
type Universe struct {
	repo    UniverseRepository
	polygon *polygon.Client
	logger  *slog.Logger
}

// NewUniverse constructs a Universe manager.
func NewUniverse(repo UniverseRepository, polygonClient *polygon.Client, logger *slog.Logger) *Universe {
	if logger == nil {
		logger = slog.Default()
	}
	return &Universe{
		repo:    repo,
		polygon: polygonClient,
		logger:  logger,
	}
}

// RefreshConstituents loads all active US common stocks from Polygon,
// assigns index groups by exchange, and upserts into DB. Returns the count
// of tickers upserted.
func (u *Universe) RefreshConstituents(ctx context.Context) (int, error) {
	u.logger.Info("universe: refreshing constituents from Polygon")

	tickers, err := u.polygon.ListActiveTickers(ctx, "stocks", "CS")
	if err != nil {
		return 0, fmt.Errorf("universe: list active tickers: %w", err)
	}

	u.logger.Info("universe: fetched tickers from Polygon", slog.Int("count", len(tickers)))

	tracked, duplicateCount := trackedTickersFromPolygon(tickers)
	if duplicateCount > 0 {
		u.logger.Warn("universe: dropped duplicate Polygon tickers",
			slog.Int("duplicates", duplicateCount),
			slog.Int("unique", len(tracked)),
		)
	}

	if err := u.repo.UpsertBatch(ctx, tracked); err != nil {
		return 0, fmt.Errorf("universe: upsert batch: %w", err)
	}

	u.logger.Info("universe: refresh complete", slog.Int("upserted", len(tracked)))
	return len(tracked), nil
}

func trackedTickersFromPolygon(tickers []polygon.TickerInfo) ([]TrackedTicker, int) {
	tracked := make([]TrackedTicker, 0, len(tickers))
	seen := make(map[string]struct{}, len(tickers))
	duplicateCount := 0

	for _, t := range tickers {
		symbol := strings.ToUpper(strings.TrimSpace(t.Ticker))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			duplicateCount++
			continue
		}
		seen[symbol] = struct{}{}

		group := exchangeToGroup(t.PrimaryExchange)
		tracked = append(tracked, TrackedTicker{
			Ticker:     symbol,
			Name:       t.Name,
			Exchange:   t.PrimaryExchange,
			IndexGroup: group,
			Active:     true,
		})
	}

	return tracked, duplicateCount
}

// GetWatchlist returns the top N tickers by watch_score.
func (u *Universe) GetWatchlist(ctx context.Context, topN int) ([]TrackedTicker, error) {
	return u.repo.Watchlist(ctx, topN)
}

// GetActiveTickers returns all active ticker symbols, optionally filtered by
// index group, up to the given limit.
func (u *Universe) GetActiveTickers(ctx context.Context, indexGroup string, limit int) ([]string, error) {
	active := boolPtr(true)
	filter := ListFilter{
		IndexGroup: indexGroup,
		Active:     active,
	}

	tickers, err := u.repo.List(ctx, filter, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("universe: get active tickers: %w", err)
	}

	symbols := make([]string, 0, len(tickers))
	seen := make(map[string]struct{}, len(tickers))
	duplicates := 0
	for _, t := range tickers {
		symbol := strings.ToUpper(strings.TrimSpace(t.Ticker))
		if symbol == "" {
			continue
		}
		if _, exists := seen[symbol]; exists {
			duplicates++
			continue
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	if duplicates > 0 {
		u.logger.Warn("universe: omitted duplicate active tickers",
			slog.Int("duplicates", duplicates),
			slog.Int("unique", len(symbols)),
		)
	}
	return symbols, nil
}

// RunPreMarketScreen is a convenience method that delegates to the package-level
// RunPreMarketScreen function using the Universe's polygon client and repo.
func (u *Universe) RunPreMarketScreen(ctx context.Context, minADV float64, maxTickers int) ([]ScoredTicker, error) {
	cfg := DefaultPreMarketConfig()
	if minADV > 0 {
		cfg.MinADV = minADV
	}
	if maxTickers > 0 {
		cfg.TopN = maxTickers
	}
	return RunPreMarketScreen(ctx, u.polygon, u.repo, cfg, u.logger)
}

// UpdateScore updates the watch_score for a single ticker.
func (u *Universe) UpdateScore(ctx context.Context, ticker string, score float64) error {
	return u.repo.UpdateScore(ctx, ticker, score)
}

// exchangeToGroup maps a Polygon exchange code to a simplified index group.
func exchangeToGroup(exchange string) string {
	switch exchange {
	case "XNAS":
		return "nasdaq"
	case "XNYS":
		return "nyse"
	default:
		return "other"
	}
}

func boolPtr(v bool) *bool {
	return &v
}
