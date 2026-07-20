package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func bootstrapPaperOptionsAccount(ctx context.Context, broker *paper.PaperBroker, paperRepo repository.PaperAccountRepository) error {
	if broker == nil || paperRepo == nil {
		return fmt.Errorf("paper options account dependencies are required")
	}
	var allTrades []domain.Trade
	for offset := 0; ; offset += 250 {
		trades, err := paperRepo.ListPaperTrades(ctx, 250, offset)
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
		positions, err := paperRepo.GetOpenPaperPositions(ctx, 250, offset)
		if err != nil {
			return err
		}
		allPositions = append(allPositions, positions...)
		if len(positions) < 250 {
			break
		}
	}
	var allOrders []domain.Order
	for offset := 0; ; offset += 250 {
		orders, err := paperRepo.ListOpenPaperOrders(ctx, 250, offset)
		if err != nil {
			return err
		}
		allOrders = append(allOrders, orders...)
		if len(orders) < 250 {
			break
		}
	}
	balance, err := reconstructPaperBalance(localPaperBuyingPower, allTrades, allPositions)
	if err != nil {
		return err
	}
	if err := broker.RestoreAccount(balance); err != nil {
		return err
	}
	if err := broker.RestorePositions(allPositions); err != nil {
		return err
	}
	if err := broker.RestoreOrders(allOrders); err != nil {
		return err
	}
	maxSeq, err := paperRepo.GetMaxPaperExternalIDSequence(ctx)
	if err != nil {
		return err
	}
	if err := broker.RestoreOrderSequence(maxSeq); err != nil {
		return err
	}
	return nil
}

func reconstructPaperBalance(initialCash float64, trades []domain.Trade, positions []domain.Position) (execution.Balance, error) {
	cash := initialCash
	for _, trade := range trades {
		cashFlow := tradeNotional(trade)
		if cashFlow == 0 {
			continue
		}
		if trade.Side == domain.OrderSideBuy {
			cash -= cashFlow + trade.Fee
		} else {
			cash += cashFlow - trade.Fee
		}
	}
	equity := cash
	for _, position := range positions {
		if position.ClosedAt != nil {
			continue
		}
		value := positionMarketValue(position)
		if position.Side == domain.PositionSideShort {
			equity -= value
		} else {
			equity += value
		}
	}
	if cash < 0 || equity < 0 {
		return execution.Balance{}, fmt.Errorf("reconstructed paper options account is negative: cash=%.2f equity=%.2f", cash, equity)
	}
	if math.IsNaN(cash) || math.IsInf(cash, 0) || math.IsNaN(equity) || math.IsInf(equity, 0) {
		return execution.Balance{}, fmt.Errorf("invalid reconstructed balance")
	}
	return execution.Balance{Currency: "USD", Cash: cash, BuyingPower: cash, Equity: equity}, nil
}

func tradeNotional(trade domain.Trade) float64 {
	return trade.Price * trade.Quantity
}

func positionMarketValue(position domain.Position) float64 {
	price := position.AvgEntry
	if position.CurrentPrice != nil && *position.CurrentPrice > 0 {
		price = *position.CurrentPrice
	}
	return price * position.Quantity
}

func parsePaperSequence(externalID string) (uint64, bool) {
	externalID = strings.TrimSpace(externalID)
	if !strings.HasPrefix(externalID, "paper-") {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(externalID, "paper-"), 10, 64)
	return n, err == nil
}
