package execution

import (
	"context"
	"fmt"
	"math"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

const riskSnapshotPositionLimit = 10_000

// BuildRiskPortfolioSnapshot captures the current portfolio exposure needed for
// truthful pre-trade risk checks and status reporting.
func BuildRiskPortfolioSnapshot(ctx context.Context, broker Broker, positionRepo repository.PositionRepository) (risk.Portfolio, error) {
	if broker == nil {
		return risk.Portfolio{}, fmt.Errorf("broker is required")
	}

	balance, err := broker.GetAccountBalance(ctx)
	if err != nil {
		return risk.Portfolio{}, fmt.Errorf("get account balance: %w", err)
	}

	return BuildRiskPortfolioSnapshotFromBalance(ctx, balance, positionRepo)
}

// BuildRiskPortfolioSnapshotFromBalance converts the current open positions into
// the exposure shape expected by the risk engine using the provided account equity.
func BuildRiskPortfolioSnapshotFromBalance(ctx context.Context, balance Balance, positionRepo repository.PositionRepository) (risk.Portfolio, error) {
	if positionRepo == nil {
		return risk.Portfolio{}, fmt.Errorf("position repository is required")
	}

	positions, err := positionRepo.GetOpen(ctx, repository.PositionFilter{}, riskSnapshotPositionLimit, 0)
	if err != nil {
		return risk.Portfolio{}, fmt.Errorf("get open positions: %w", err)
	}

	portfolio := risk.Portfolio{
		ConcurrentPositions:      len(positions),
		PositionExposureBySymbol: make(map[string]float64, len(positions)),
	}
	if len(positions) == 0 {
		return portfolio, nil
	}
	if balance.Equity <= 0 {
		return risk.Portfolio{}, fmt.Errorf("account equity must be positive")
	}

	for _, position := range positions {
		notional, err := positionNotional(position)
		if err != nil {
			return risk.Portfolio{}, err
		}
		exposure := notional / balance.Equity
		portfolio.TotalExposurePct += exposure
		portfolio.PositionExposureBySymbol[position.Ticker] += exposure
	}

	return portfolio, nil
}

func positionNotional(position domain.Position) (float64, error) {
	if position.Ticker == "" {
		return 0, fmt.Errorf("position ticker is required")
	}
	if position.Quantity == 0 {
		return 0, nil
	}

	price := position.AvgEntry
	if position.CurrentPrice != nil && *position.CurrentPrice > 0 {
		price = *position.CurrentPrice
	}
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return 0, fmt.Errorf("position %s has invalid price", position.Ticker)
	}

	quantity := math.Abs(position.Quantity)
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) {
		return 0, fmt.Errorf("position %s has invalid quantity", position.Ticker)
	}

	return quantity * price, nil
}
