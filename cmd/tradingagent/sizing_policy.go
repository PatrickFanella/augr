package main

import (
	"context"
	"log/slog"
	"math"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
	"github.com/PatrickFanella/get-rich-quick/internal/position"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/google/uuid"
)

const kellyHistoryPageSize = 500

func sizingConfigForStrategy(
	ctx context.Context,
	strategy domain.Strategy,
	strategyConfig *agent.StrategyConfig,
	resolved agent.ResolvedConfig,
	positionRepo repository.PositionRepository,
	logger *slog.Logger,
) execution.SizingConfig {
	useKelly := strategyConfig != nil && strategyConfig.RiskConfig != nil && strategyConfig.RiskConfig.UseKellySizing != nil && *strategyConfig.RiskConfig.UseKellySizing
	stats := position.HistoryStats{}
	if useKelly && positionRepo != nil {
		var err error
		stats, err = closedTradeStatsForStrategy(ctx, positionRepo, strategy.ID)
		if err != nil && logger != nil {
			logger.WarnContext(ctx, "unable to load Kelly sizing history; falling back to market default", "strategy_id", strategy.ID, "error", err)
		}
	}

	cfg := position.ResolveForMarket(strategy.MarketType, resolved.RiskConfig.PositionSizePct, resolved.RiskConfig.StopLossMultiplier, useKelly, stats).ExecutionSizingConfig()
	if strategy.MarketType.Normalize() == domain.MarketTypeKalshi && cfg.Method == "" {
		fractionPct := position.DefaultPolymarketFractionPct
		if resolved.RiskConfig.PositionSizePct > 0 {
			fractionPct = resolved.RiskConfig.PositionSizePct / 100.0
		}
		cfg.Method = execution.PositionSizingMethodFixedFractional
		cfg.FractionPct = fractionPct
	}
	return cfg
}

func applyPolymarketSizingCap(market domain.MarketType, cfg execution.SizingConfig, maxPositionUSDC float64) execution.SizingConfig {
	if !eventmarkets.IsEventMarket(market) {
		return cfg
	}
	if market.Normalize() == domain.MarketTypePolymarket {
		cfg.MaxPositionUSDC = maxPositionUSDC
	}

	return cfg
}

func closedTradeStatsForStrategy(ctx context.Context, positionRepo repository.PositionRepository, strategyID uuid.UUID) (position.HistoryStats, error) {
	var (
		closedTrades int
		wins         int
		losses       int
		totalWin     float64
		totalLoss    float64
	)
	var offset int

	for {
		positions, err := positionRepo.GetByStrategy(ctx, strategyID, repository.PositionFilter{}, kellyHistoryPageSize, offset)
		if err != nil {
			return position.HistoryStats{}, err
		}
		if len(positions) == 0 {
			break
		}

		for _, pos := range positions {
			if pos.ClosedAt == nil {
				continue
			}
			closedTrades++
			switch {
			case pos.RealizedPnL > 0:
				wins++
				totalWin += pos.RealizedPnL
			case pos.RealizedPnL < 0:
				losses++
				totalLoss += math.Abs(pos.RealizedPnL)
			}
		}

		if len(positions) < kellyHistoryPageSize {
			break
		}
		offset += len(positions)
	}

	if closedTrades == 0 {
		return position.HistoryStats{}, nil
	}

	result := position.HistoryStats{ClosedTrades: closedTrades}
	if wins == 0 || losses == 0 || totalWin <= 0 || totalLoss <= 0 {
		return result, nil
	}

	winRate := float64(wins) / float64(closedTrades)
	avgWin := totalWin / float64(wins)
	avgLoss := totalLoss / float64(losses)
	if winRate <= 0 || winRate >= 1 || avgLoss <= 0 || math.IsNaN(winRate) || math.IsNaN(avgWin) || math.IsNaN(avgLoss) || math.IsInf(avgWin, 0) || math.IsInf(avgLoss, 0) {
		return result, nil
	}

	result.WinRate = winRate
	result.WinLossRatio = avgWin / avgLoss
	return result, nil
}
