package main

import (
	"math"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestReconstructPaperOptionsBalanceSurvivesRestart(t *testing.T) {
	trades := []domain.Trade{{AssetClass: domain.AssetClassOption, Side: domain.OrderSideBuy, Quantity: 2, Price: 2.5, Premium: 500, Fee: 1.3, ContractMultiplier: 100, OpenClose: "open"}}
	positions := []domain.Position{{AssetClass: domain.AssetClassOption, Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 2.5, ContractMultiplier: 100}}
	balance, err := reconstructPaperOptionsBalance(100000, trades, positions)
	if err != nil {
		t.Fatalf("reconstructPaperOptionsBalance() error = %v", err)
	}
	if math.Abs(balance.Cash-99498.7) > 1e-9 || math.Abs(balance.Equity-99998.7) > 1e-9 {
		t.Fatalf("unexpected restored balance: %+v", balance)
	}
}
