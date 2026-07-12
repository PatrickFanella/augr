package main

import (
	"context"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func bootstrapPaperOptionsAccount(ctx context.Context, broker *paper.PaperBroker, positionRepo repository.PositionRepository, tradeRepo repository.TradeRepository) error {
	if broker == nil || positionRepo == nil || tradeRepo == nil {
		return fmt.Errorf("paper options account dependencies are required")
	}
	var allTrades []domain.Trade
	for offset := 0; ; offset += 250 {
		trades, err := tradeRepo.List(ctx, repository.TradeFilter{}, 250, offset)
		if err != nil {
			return err
		}
		allTrades = append(allTrades, trades...)
		if len(trades) < 250 {
			break
		}
	}
	var allPositions []domain.Position
	for offset := 0; ; offset += 250 {
		positions, err := positionRepo.GetOpen(ctx, repository.PositionFilter{}, 250, offset)
		if err != nil {
			return err
		}
		allPositions = append(allPositions, positions...)
		if len(positions) < 250 {
			break
		}
	}
	balance, err := reconstructPaperOptionsBalance(localPaperBuyingPower, allTrades, allPositions)
	if err != nil {
		return err
	}
	return broker.RestoreAccount(balance)
}

func reconstructPaperOptionsBalance(initialCash float64, trades []domain.Trade, positions []domain.Position) (execution.Balance, error) {
	cash := initialCash
	for _, trade := range trades {
		if trade.AssetClass != domain.AssetClassOption {
			continue
		}
		cashFlow := trade.Premium
		if cashFlow == 0 {
			cashFlow = trade.Price * trade.Quantity * trade.ContractMultiplier
		}
		if trade.Side == domain.OrderSideBuy {
			cash -= cashFlow + trade.Fee
		} else {
			cash += cashFlow - trade.Fee
		}
	}
	equity := cash
	for _, position := range positions {
		if position.AssetClass != domain.AssetClassOption || position.ClosedAt != nil {
			continue
		}
		price := position.AvgEntry
		if position.CurrentPrice != nil {
			price = *position.CurrentPrice
		}
		value := price * position.Quantity * position.ContractMultiplier
		if position.Side == domain.PositionSideShort {
			equity -= value
		} else {
			equity += value
		}
	}
	if cash < 0 || equity < 0 {
		return execution.Balance{}, fmt.Errorf("reconstructed paper options account is negative: cash=%.2f equity=%.2f", cash, equity)
	}
	return execution.Balance{Currency: "USD", Cash: cash, BuyingPower: cash, Equity: equity}, nil
}
