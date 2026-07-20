package main

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
)

func TestPaperRoutingSharesLocalFallbackBrokerOnly(t *testing.T) {
	shared := paper.NewPaperBroker(1000, 0, 0)
	runner := &realStrategyRunner{localPaperBroker: shared}
	if got := runner.fallbackPaperBroker(); got != shared {
		t.Fatal("fallback paper broker was not shared")
	}
	if got := runner.fallbackPaperBroker(); got != shared {
		t.Fatal("fallback paper broker was recreated, want shared broker instance")
	}
	if got := brokerNameForStrategy(domain.Strategy{MarketType: domain.MarketTypeStock}); got != "alpaca" {
		t.Fatalf("stock route = %q, want alpaca", got)
	}
	if got := brokerNameForStrategy(domain.Strategy{MarketType: domain.MarketTypeCrypto}); got != "binance" {
		t.Fatalf("crypto route = %q, want binance", got)
	}
	if got := brokerNameForStrategy(domain.Strategy{MarketType: domain.MarketTypePolymarket}); got != "polymarket" {
		t.Fatalf("polymarket route = %q, want polymarket", got)
	}
	if got := brokerNameForStrategy(domain.Strategy{MarketType: domain.MarketTypeKalshi}); got != "kalshi" {
		t.Fatalf("kalshi route = %q, want kalshi", got)
	}
}
