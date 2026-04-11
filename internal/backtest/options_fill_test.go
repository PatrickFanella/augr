package backtest

import (
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func optionsBar(close float64) domain.OHLCV {
	return domain.OHLCV{
		Timestamp: time.Date(2026, 4, 1, 15, 0, 0, 0, time.UTC),
		Open:      close,
		High:      close + 0.10,
		Low:       close - 0.10,
		Close:     close,
		Volume:    500,
	}
}

func TestSimulateOptionsFill_MarketBuySlippage(t *testing.T) {
	t.Parallel()
	order := &domain.Order{
		Ticker:             "AAPL260417C00200000",
		Side:               domain.OrderSideBuy,
		OrderType:          domain.OrderTypeMarket,
		Quantity:           5,
		ContractMultiplier: 100,
	}
	cfg := OptionsFillConfig{SpreadSlippageBps: 10, FeePerContract: 0.65}
	bar := optionsBar(3.00) // premium $3.00

	result, err := SimulateOptionsFill(order, bar, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Slippage: 10 bps = 0.001 → fill = 3.00 * 1.001 = 3.003
	wantFill := 3.00 * (1 + 10.0/10000.0)
	if math.Abs(result.FillPrice-wantFill) > 1e-9 {
		t.Errorf("FillPrice = %v, want %v", result.FillPrice, wantFill)
	}
	if result.Quantity != 5 {
		t.Errorf("Quantity = %v, want 5", result.Quantity)
	}
	if result.Multiplier != 100 {
		t.Errorf("Multiplier = %v, want 100", result.Multiplier)
	}

	wantFee := 0.65 * 5
	if math.Abs(result.Fee-wantFee) > 1e-9 {
		t.Errorf("Fee = %v, want %v", result.Fee, wantFee)
	}

	wantCost := wantFill*5*100 + wantFee
	if math.Abs(result.TotalCost-wantCost) > 1e-6 {
		t.Errorf("TotalCost = %v, want %v", result.TotalCost, wantCost)
	}
}

func TestSimulateOptionsFill_MarketSellSlippage(t *testing.T) {
	t.Parallel()
	order := &domain.Order{
		Ticker:             "AAPL260417P00180000",
		Side:               domain.OrderSideSell,
		OrderType:          domain.OrderTypeMarket,
		Quantity:           2,
		ContractMultiplier: 100,
	}
	cfg := DefaultOptionsFillConfig()
	bar := optionsBar(5.00)

	result, err := SimulateOptionsFill(order, bar, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sell side: slippage lowers fill price → 5.00 * (1 - 0.001) = 4.995
	wantFill := 5.00 * (1 - 10.0/10000.0)
	if math.Abs(result.FillPrice-wantFill) > 1e-9 {
		t.Errorf("FillPrice = %v, want %v", result.FillPrice, wantFill)
	}

	wantFee := 0.65 * 2
	wantCost := wantFill*2*100 - wantFee // premium received minus fees
	if math.Abs(result.TotalCost-wantCost) > 1e-6 {
		t.Errorf("TotalCost = %v, want %v", result.TotalCost, wantCost)
	}
}

func TestSimulateOptionsFill_FeeCalculation(t *testing.T) {
	t.Parallel()
	order := &domain.Order{
		Ticker:             "SPY260320C00500000",
		Side:               domain.OrderSideBuy,
		OrderType:          domain.OrderTypeMarket,
		Quantity:           10,
		ContractMultiplier: 100,
	}
	cfg := OptionsFillConfig{SpreadSlippageBps: 0, FeePerContract: 1.25}
	bar := optionsBar(2.50)

	result, err := SimulateOptionsFill(order, bar, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantFee := 1.25 * 10
	if math.Abs(result.Fee-wantFee) > 1e-9 {
		t.Errorf("Fee = %v, want %v", result.Fee, wantFee)
	}
}

func TestSimulateOptionsFill_DefaultMultiplierFallback(t *testing.T) {
	t.Parallel()
	order := &domain.Order{
		Ticker:    "TSLA260501C00250000",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		// ContractMultiplier intentionally left at zero
	}
	cfg := OptionsFillConfig{SpreadSlippageBps: 0, FeePerContract: 0}
	bar := optionsBar(10.00)

	result, err := SimulateOptionsFill(order, bar, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Multiplier != 100 {
		t.Errorf("Multiplier = %v, want 100 (default fallback)", result.Multiplier)
	}

	// TotalCost should use multiplier 100: 10 * 1 * 100 = 1000
	if math.Abs(result.TotalCost-1000.0) > 1e-9 {
		t.Errorf("TotalCost = %v, want 1000", result.TotalCost)
	}
}

func TestSimulateOptionsFill_NilOrder(t *testing.T) {
	t.Parallel()
	_, err := SimulateOptionsFill(nil, optionsBar(1.0), DefaultOptionsFillConfig())
	if err == nil {
		t.Fatal("expected error for nil order")
	}
}

func TestSimulateOptionsFill_ZeroQuantity(t *testing.T) {
	t.Parallel()
	order := &domain.Order{
		Ticker:    "AAPL260417C00200000",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  0,
	}
	_, err := SimulateOptionsFill(order, optionsBar(1.0), DefaultOptionsFillConfig())
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestSimulateOptionsFill_ZeroBarClose(t *testing.T) {
	t.Parallel()
	order := &domain.Order{
		Ticker:    "AAPL260417C00200000",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
	}
	bar := domain.OHLCV{Close: 0}
	_, err := SimulateOptionsFill(order, bar, DefaultOptionsFillConfig())
	if err == nil {
		t.Fatal("expected error for zero bar close")
	}
}
