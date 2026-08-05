package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const polymarketBootstrapPageSize = 1000

func bootstrapPolymarketStopGuards(ctx context.Context, runner *realStrategyRunner, positionRepo repository.PositionRepository, logger *slog.Logger) error {
	if runner == nil || runner.polymarketStopGuard == nil || positionRepo == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	var (
		totalRegistered int
		firstErr        error
	)

	for offset := 0; ; offset += polymarketBootstrapPageSize {
		positions, err := positionRepo.GetOpen(ctx, repository.PositionFilter{}, polymarketBootstrapPageSize, offset)
		if err != nil {
			return fmt.Errorf("bootstrap polymarket stop guards: fetch open positions: %w", err)
		}
		if len(positions) == 0 {
			break
		}

		filtered := make([]domain.Position, 0, len(positions))
		for _, position := range positions {
			if !isBootstrapPolymarketPosition(position) {
				continue
			}
			filtered = append(filtered, position)
		}
		if len(filtered) > 0 {
			if err := runner.registerPolymarketPositions(filtered); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("bootstrap polymarket stop guards: register positions: %w", err)
				}
				logger.Warn("polymarket stop guard bootstrap encountered registration error",
					slog.Int("page_offset", offset),
					slog.Any("error", err),
				)
			}
			totalRegistered += len(filtered)
		}

		if len(positions) < polymarketBootstrapPageSize {
			break
		}
	}

	logger.Info("polymarket stop guards bootstrapped",
		slog.Int("registered", totalRegistered),
		slog.Int("active", runner.polymarketStopGuard.Active()),
	)
	return firstErr
}

func isBootstrapPolymarketPosition(position domain.Position) bool {
	if position.ClosedAt != nil || position.Quantity <= 0 {
		return false
	}
	if _, _, ok := polymarketSideQualifiedTicker(position.Ticker); !ok {
		return false
	}
	marketType := position.MarketType.Normalize()
	return marketType == "" || marketType == domain.MarketTypePolymarket
}

func polymarketSideQualifiedTicker(ticker string) (slug, side string, ok bool) {
	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return "", "", false
	}
	slug, side, found := strings.Cut(ticker, ":")
	slug = strings.TrimSpace(slug)
	side = strings.ToUpper(strings.TrimSpace(side))
	if !found || slug == "" || (side != "YES" && side != "NO") {
		return "", "", false
	}
	return slug, side, true
}
