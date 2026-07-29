package risk

import (
	"fmt"
	"math"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	polymarketMaxExposurePct = 0.05
	kalshiMaxExposurePct     = 0.20
)

type marketExposurePolicy struct {
	limits   PositionLimits
	pmLimits PolymarketLimits
}

func newMarketExposurePolicy(limits PositionLimits, pmLimits PolymarketLimits) marketExposurePolicy {
	return marketExposurePolicy{limits: limits, pmLimits: pmLimits}
}

func (p marketExposurePolicy) check(ticker string, quantity float64, portfolio Portfolio) (bool, string) {
	if ticker == "" {
		return false, "ticker is required"
	}
	if quantity <= 0 || math.IsNaN(quantity) || math.IsInf(quantity, 0) {
		return false, "quantity must be a positive finite number"
	}

	currentExposure := portfolio.PositionExposureBySymbol[ticker]
	if currentExposure+quantity > p.limits.MaxPerPositionPct {
		return false, fmt.Sprintf(
			"position size %.2f%% for %s exceeds max %.2f%%",
			(currentExposure+quantity)*100, ticker, p.limits.MaxPerPositionPct*100,
		)
	}

	if portfolio.TotalExposurePct+quantity > p.limits.MaxTotalPct {
		return false, fmt.Sprintf(
			"total exposure %.2f%% exceeds max %.2f%%",
			(portfolio.TotalExposurePct+quantity)*100, p.limits.MaxTotalPct*100,
		)
	}

	if _, exists := portfolio.PositionExposureBySymbol[ticker]; !exists {
		if portfolio.ConcurrentPositions >= p.limits.MaxConcurrent {
			return false, fmt.Sprintf(
				"concurrent positions %d reached max %d",
				portfolio.ConcurrentPositions, p.limits.MaxConcurrent,
			)
		}
	}

	for market, exposure := range portfolio.MarketExposurePct {
		limit := p.limitForMarket(market)
		if exposure > limit {
			return false, fmt.Sprintf(
				"%s market exposure %.2f%% exceeds max %.2f%%",
				market, exposure*100, limit*100,
			)
		}
	}

	return true, ""
}

func (p marketExposurePolicy) limitForMarket(market domain.MarketType) float64 {
	limit := p.limits.MaxPerMarketPct
	if market == domain.MarketTypePolymarket {
		if p.pmLimits.MaxSingleMarketExposurePct > 0 {
			return p.pmLimits.MaxSingleMarketExposurePct
		}
		return polymarketMaxExposurePct
	}
	if market == domain.MarketTypeKalshi {
		if p.limits.MaxKalshiExposurePct > 0 {
			return p.limits.MaxKalshiExposurePct
		}
		return kalshiMaxExposurePct
	}
	return limit
}
