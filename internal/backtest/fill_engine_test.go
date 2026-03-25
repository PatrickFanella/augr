package backtest

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ---------- helpers ----------

func testBar(close, high, low, volume float64) domain.OHLCV {
	return domain.OHLCV{
		Timestamp: time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC),
		Open:      close,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
	}
}

func floatClose(t *testing.T, got, want, eps float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Fatalf("float mismatch: got %v, want %v (eps %v)", got, want, eps)
	}
}

func fp(v float64) *float64 { return &v }

// ---------- SlippageModel tests ----------

func TestFixedSlippage_Buy(t *testing.T) {
	t.Parallel()
	s := FixedSlippage{Amount: 0.05}
	got := s.AdjustedPrice(100, domain.OrderSideBuy, domain.OHLCV{})
	floatClose(t, got, 100.05, 1e-9)
}

func TestFixedSlippage_Sell(t *testing.T) {
	t.Parallel()
	s := FixedSlippage{Amount: 0.05}
	got := s.AdjustedPrice(100, domain.OrderSideSell, domain.OHLCV{})
	floatClose(t, got, 99.95, 1e-9)
}

func TestFixedSlippage_SellClampsToMinPrice(t *testing.T) {
	t.Parallel()
	s := FixedSlippage{Amount: 200}
	got := s.AdjustedPrice(100, domain.OrderSideSell, domain.OHLCV{})
	if got < minFillPrice {
		t.Fatalf("got %v, want >= %v", got, minFillPrice)
	}
}

func TestFixedSlippage_NegativeAmountUsesAbsValue(t *testing.T) {
	t.Parallel()
	s := FixedSlippage{Amount: -0.05}
	got := s.AdjustedPrice(100, domain.OrderSideBuy, domain.OHLCV{})
	floatClose(t, got, 100.05, 1e-9)
}

func TestProportionalSlippage_Buy(t *testing.T) {
	t.Parallel()
	s := ProportionalSlippage{BasisPoints: 10} // 10 bps = 0.10%
	got := s.AdjustedPrice(100, domain.OrderSideBuy, domain.OHLCV{})
	floatClose(t, got, 100.10, 1e-9)
}

func TestProportionalSlippage_Sell(t *testing.T) {
	t.Parallel()
	s := ProportionalSlippage{BasisPoints: 10}
	got := s.AdjustedPrice(100, domain.OrderSideSell, domain.OHLCV{})
	floatClose(t, got, 99.90, 1e-9)
}

func TestProportionalSlippage_SellClampsToMinPrice(t *testing.T) {
	t.Parallel()
	s := ProportionalSlippage{BasisPoints: 20000} // 200%
	got := s.AdjustedPrice(100, domain.OrderSideSell, domain.OHLCV{})
	if got < minFillPrice {
		t.Fatalf("got %v, want >= %v", got, minFillPrice)
	}
}

func TestVolatilityScaledSlippage_Buy(t *testing.T) {
	t.Parallel()
	bar := testBar(100, 105, 95, 1000) // range=10, volatility=10/100=0.10
	s := VolatilityScaledSlippage{ScaleFactor: 0.5}
	got := s.AdjustedPrice(100, domain.OrderSideBuy, bar)
	// slippage = 0.5 * 0.10 = 0.05 → 100 * 1.05 = 105
	floatClose(t, got, 105, 1e-9)
}

func TestVolatilityScaledSlippage_Sell(t *testing.T) {
	t.Parallel()
	bar := testBar(100, 105, 95, 1000)
	s := VolatilityScaledSlippage{ScaleFactor: 0.5}
	got := s.AdjustedPrice(100, domain.OrderSideSell, bar)
	// slippage = 0.5 * 0.10 = 0.05 → 100 * 0.95 = 95
	floatClose(t, got, 95, 1e-9)
}

func TestVolatilityScaledSlippage_ZeroCloseUsesPriceAsFallback(t *testing.T) {
	t.Parallel()
	bar := domain.OHLCV{High: 105, Low: 95, Close: 0}
	s := VolatilityScaledSlippage{ScaleFactor: 0.5}
	// fallback closePrice = price = 100 → volatility = 10/100 = 0.10
	got := s.AdjustedPrice(100, domain.OrderSideBuy, bar)
	floatClose(t, got, 105, 1e-9)
}

func TestVolatilityScaledSlippage_FlatBarNoSlippage(t *testing.T) {
	t.Parallel()
	bar := testBar(100, 100, 100, 1000) // range = 0
	s := VolatilityScaledSlippage{ScaleFactor: 1.0}
	got := s.AdjustedPrice(100, domain.OrderSideBuy, bar)
	floatClose(t, got, 100, 1e-9)
}

// ---------- SpreadModel tests ----------

func TestFixedSpread_BidAsk(t *testing.T) {
	t.Parallel()
	bar := testBar(100, 105, 95, 1000)
	s := FixedSpread{SpreadBps: 20} // 20 bps = 0.20%, half = 0.10%
	bid, ask := s.BidAsk(bar)
	floatClose(t, bid, 99.90, 1e-9)
	floatClose(t, ask, 100.10, 1e-9)
}

func TestFixedSpread_ZeroSpread(t *testing.T) {
	t.Parallel()
	bar := testBar(100, 105, 95, 1000)
	s := FixedSpread{SpreadBps: 0}
	bid, ask := s.BidAsk(bar)
	floatClose(t, bid, 100, 1e-9)
	floatClose(t, ask, 100, 1e-9)
}

// ---------- TransactionCosts tests ----------

func TestTransactionCosts_Compute(t *testing.T) {
	t.Parallel()
	tc := TransactionCosts{
		CommissionPerOrder: 1.0,
		CommissionPerUnit:  0.005,
		ExchangeFeePct:     0.001,
	}
	comm, fee := tc.Compute(100, 50)
	// comm = 1.0 + 0.005*100 = 1.50
	// fee = 50*100*0.001 = 5.0
	floatClose(t, comm, 1.50, 1e-9)
	floatClose(t, fee, 5.0, 1e-9)
}

func TestTransactionCosts_ZeroCosts(t *testing.T) {
	t.Parallel()
	tc := TransactionCosts{}
	comm, fee := tc.Compute(100, 50)
	floatClose(t, comm, 0, 1e-9)
	floatClose(t, fee, 0, 1e-9)
}

// ---------- FillEngine construction tests ----------

func TestNewFillEngine_NilSlippageReturnsError(t *testing.T) {
	t.Parallel()
	_, err := NewFillEngine(FillConfig{})
	if !errors.Is(err, ErrNilSlippageModel) {
		t.Fatalf("NewFillEngine() error = %v, want %v", err, ErrNilSlippageModel)
	}
}

func TestNewFillEngine_ClampsMaxVolumePct(t *testing.T) {
	t.Parallel()
	eng, err := NewFillEngine(FillConfig{
		Slippage:     ProportionalSlippage{BasisPoints: 0},
		MaxVolumePct: 2.0,
	})
	if err != nil {
		t.Fatalf("NewFillEngine() error = %v", err)
	}
	if eng.config.MaxVolumePct != 1.0 {
		t.Fatalf("MaxVolumePct = %v, want 1.0", eng.config.MaxVolumePct)
	}
}

func TestNewFillEngine_NegativeMaxVolumePctBecomesZero(t *testing.T) {
	t.Parallel()
	eng, err := NewFillEngine(FillConfig{
		Slippage:     ProportionalSlippage{BasisPoints: 0},
		MaxVolumePct: -0.5,
	})
	if err != nil {
		t.Fatalf("NewFillEngine() error = %v", err)
	}
	if eng.config.MaxVolumePct != 0 {
		t.Fatalf("MaxVolumePct = %v, want 0", eng.config.MaxVolumePct)
	}
}

// ---------- SimulateFill validation tests ----------

func TestSimulateFill_NilOrderReturnsError(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	_, err := eng.SimulateFill(nil, domain.OHLCV{})
	if !errors.Is(err, ErrNilOrder) {
		t.Fatalf("err = %v, want %v", err, ErrNilOrder)
	}
}

func TestSimulateFill_ZeroQuantityReturnsError(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	order := &domain.Order{Quantity: 0, OrderType: domain.OrderTypeMarket}
	_, err := eng.SimulateFill(order, testBar(100, 105, 95, 1000))
	if !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidQuantity)
	}
}

func TestSimulateFill_ZeroCloseReturnsError(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	order := &domain.Order{Quantity: 10, OrderType: domain.OrderTypeMarket, Side: domain.OrderSideBuy}
	_, err := eng.SimulateFill(order, testBar(0, 0, 0, 1000))
	if !errors.Is(err, ErrInvalidBar) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidBar)
	}
}

func TestSimulateFill_UnsupportedOrderType(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	order := &domain.Order{Quantity: 10, OrderType: domain.OrderTypeStop, Side: domain.OrderSideBuy}
	_, err := eng.SimulateFill(order, testBar(100, 105, 95, 1000))
	if !errors.Is(err, ErrUnsupportedOrderType) {
		t.Fatalf("err = %v, want %v", err, ErrUnsupportedOrderType)
	}
}

func TestSimulateFill_InvalidSideReturnsError(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	order := &domain.Order{Quantity: 10, OrderType: domain.OrderTypeMarket, Side: "invalid"}
	_, err := eng.SimulateFill(order, testBar(100, 105, 95, 1000))
	if !errors.Is(err, ErrInvalidSide) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidSide)
	}
}

func TestSimulateFill_EmptySideReturnsError(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	order := &domain.Order{Quantity: 10, OrderType: domain.OrderTypeMarket}
	_, err := eng.SimulateFill(order, testBar(100, 105, 95, 1000))
	if !errors.Is(err, ErrInvalidSide) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidSide)
	}
}

// ---------- Market order fill tests ----------

func TestSimulateFill_MarketBuyWithProportionalSlippage(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 10},
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  50,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if !res.Filled {
		t.Fatal("expected Filled = true")
	}
	if res.Partial {
		t.Fatal("expected Partial = false")
	}
	// 100 * (1 + 10/10000) = 100.10
	floatClose(t, res.FillPrice, 100.10, 1e-9)
	floatClose(t, res.FillQuantity, 50, 1e-9)
}

func TestSimulateFill_MarketSellWithFixedSlippage(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: FixedSlippage{Amount: 0.02},
	})
	bar := testBar(50, 52, 48, 10000)
	order := &domain.Order{
		Side:      domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket,
		Quantity:  100,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if !res.Filled {
		t.Fatal("expected Filled = true")
	}
	// 50 - 0.02 = 49.98
	floatClose(t, res.FillPrice, 49.98, 1e-9)
	floatClose(t, res.FillQuantity, 100, 1e-9)
}

func TestSimulateFill_MarketBuyWithVolatilityScaledSlippage(t *testing.T) {
	t.Parallel()
	bar := testBar(200, 210, 190, 5000) // volatility = 20/200 = 0.10
	eng, _ := NewFillEngine(FillConfig{
		Slippage: VolatilityScaledSlippage{ScaleFactor: 0.25},
	})
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  10,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// slippage fraction = 0.25 * 0.10 = 0.025 → 200 * 1.025 = 205
	floatClose(t, res.FillPrice, 205, 1e-9)
	floatClose(t, res.Slippage, 5*10, 1e-9) // |205-200| * 10
}

// ---------- Spread model tests ----------

func TestSimulateFill_MarketBuyWithSpreadUsesAsk(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0}, // no additional slippage
		Spread:   FixedSpread{SpreadBps: 20},           // 20 bps total spread
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  10,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// ask = 100 * (1 + 10/10000) = 100.10; no additional slippage
	floatClose(t, res.FillPrice, 100.10, 1e-9)
}

func TestSimulateFill_MarketSellWithSpreadUsesBid(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
		Spread:   FixedSpread{SpreadBps: 20},
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:      domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket,
		Quantity:  10,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// bid = 100 * (1 - 10/10000) = 99.90
	floatClose(t, res.FillPrice, 99.90, 1e-9)
}

func TestSimulateFill_SpreadPlusSlippage(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: FixedSlippage{Amount: 0.05},
		Spread:   FixedSpread{SpreadBps: 20}, // ask = 100.10
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  10,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// ref = ask = 100.10, then fixed slippage +0.05 → 100.15
	floatClose(t, res.FillPrice, 100.15, 1e-9)
}

// ---------- Partial fill tests ----------

func TestSimulateFill_PartialFillWhenVolumeInsufficient(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage:     ProportionalSlippage{BasisPoints: 0},
		MaxVolumePct: 0.10, // max 10% of bar volume
	})
	bar := testBar(100, 105, 95, 1000) // max fillable = 100
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  500, // wants 500, can only get 100
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if !res.Filled {
		t.Fatal("expected Filled = true")
	}
	if !res.Partial {
		t.Fatal("expected Partial = true")
	}
	floatClose(t, res.FillQuantity, 100, 1e-9)
}

func TestSimulateFill_FullFillWhenVolumeAvailable(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage:     ProportionalSlippage{BasisPoints: 0},
		MaxVolumePct: 0.50,
	})
	bar := testBar(100, 105, 95, 1000) // max fillable = 500
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  200, // wants 200, 500 available
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if res.Partial {
		t.Fatal("expected Partial = false")
	}
	floatClose(t, res.FillQuantity, 200, 1e-9)
}

func TestSimulateFill_NoVolumeLimitWhenMaxVolumePctZero(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage:     ProportionalSlippage{BasisPoints: 0},
		MaxVolumePct: 0,
	})
	bar := testBar(100, 105, 95, 10) // tiny volume
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  10000, // huge order
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if res.Partial {
		t.Fatal("expected Partial = false when MaxVolumePct = 0")
	}
	floatClose(t, res.FillQuantity, 10000, 1e-9)
}

func TestSimulateFill_ZeroBarVolumeIgnoresVolumeCap(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage:     ProportionalSlippage{BasisPoints: 0},
		MaxVolumePct: 0.10,
	})
	bar := testBar(100, 105, 95, 0) // zero volume
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  50,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// zero volume → no cap applied
	floatClose(t, res.FillQuantity, 50, 1e-9)
}

// ---------- Transaction cost tests ----------

func TestSimulateFill_TransactionCostsApplied(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
		Costs: TransactionCosts{
			CommissionPerOrder: 1.00,
			CommissionPerUnit:  0.01,
			ExchangeFeePct:     0.001,
		},
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  100,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// commission = 1.00 + 0.01*100 = 2.00
	// exchangeFee = 100*100*0.001 = 10.00
	// totalCost (buy) = 10000 + 2 + 10 = 10012
	floatClose(t, res.Commission, 2.0, 1e-9)
	floatClose(t, res.ExchangeFee, 10.0, 1e-9)
	floatClose(t, res.TotalCost, 10012, 1e-9)
}

func TestSimulateFill_SellTotalCostSubtractsFees(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
		Costs: TransactionCosts{
			CommissionPerOrder: 2.00,
			ExchangeFeePct:     0.001,
		},
	})
	bar := testBar(50, 55, 45, 10000)
	order := &domain.Order{
		Side:      domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket,
		Quantity:  200,
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// notional = 50*200 = 10000
	// commission = 2.00
	// exchangeFee = 10000*0.001 = 10.00
	// totalCost (sell) = 10000 - 2 - 10 = 9988
	floatClose(t, res.TotalCost, 9988, 1e-9)
}

// ---------- Limit order tests ----------

func TestSimulateFill_LimitBuyFillsWhenBarLowBelowLimit(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 10},
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   50,
		LimitPrice: fp(101),
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if !res.Filled {
		t.Fatal("expected Filled = true")
	}
	// ref = 100 (close), slipped = 100.10, capped at limit 101 → 100.10
	floatClose(t, res.FillPrice, 100.10, 1e-9)
}

func TestSimulateFill_LimitBuyCapsAtLimitPrice(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 500}, // 5% slippage
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   10,
		LimitPrice: fp(101),
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// slipped = 100 * 1.05 = 105, capped at 101
	floatClose(t, res.FillPrice, 101, 1e-9)
}

func TestSimulateFill_LimitSellFillsWhenBarHighAboveLimit(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 10},
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:       domain.OrderSideSell,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   50,
		LimitPrice: fp(99),
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if !res.Filled {
		t.Fatal("expected Filled = true")
	}
	// ref = 100, slipped = 99.90, capped at limit 99 → max(99.90, 99) = 99.90
	floatClose(t, res.FillPrice, 99.90, 1e-9)
}

func TestSimulateFill_LimitSellCapsAtLimitPrice(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 500}, // 5% slippage
	})
	bar := testBar(100, 105, 95, 10000)
	order := &domain.Order{
		Side:       domain.OrderSideSell,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   10,
		LimitPrice: fp(99),
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	// slipped = 100 * 0.95 = 95, capped at max(95, 99) = 99
	floatClose(t, res.FillPrice, 99, 1e-9)
}

func TestSimulateFill_LimitBuyNoFillWhenLowAboveLimit(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
	})
	bar := testBar(100, 105, 98, 10000) // low = 98
	order := &domain.Order{
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   10,
		LimitPrice: fp(97), // limit below bar's low
	}

	_, err := eng.SimulateFill(order, bar)
	if !errors.Is(err, ErrNoFill) {
		t.Fatalf("err = %v, want %v", err, ErrNoFill)
	}
}

func TestSimulateFill_LimitSellNoFillWhenHighBelowLimit(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
	})
	bar := testBar(100, 102, 95, 10000) // high = 102
	order := &domain.Order{
		Side:       domain.OrderSideSell,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   10,
		LimitPrice: fp(103), // limit above bar's high
	}

	_, err := eng.SimulateFill(order, bar)
	if !errors.Is(err, ErrNoFill) {
		t.Fatalf("err = %v, want %v", err, ErrNoFill)
	}
}

func TestSimulateFill_LimitOrderNilLimitPriceReturnsError(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeLimit,
		Quantity:  10,
	}
	_, err := eng.SimulateFill(order, testBar(100, 105, 95, 1000))
	if !errors.Is(err, ErrLimitPriceRequired) {
		t.Fatalf("err = %v, want %v", err, ErrLimitPriceRequired)
	}
}

func TestSimulateFill_LimitOrderZeroLimitPriceReturnsError(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	order := &domain.Order{
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   10,
		LimitPrice: fp(0),
	}
	_, err := eng.SimulateFill(order, testBar(100, 105, 95, 1000))
	if !errors.Is(err, ErrLimitPricePositive) {
		t.Fatalf("err = %v, want %v", err, ErrLimitPricePositive)
	}
}

// ---------- Limit order with partial fill ----------

func TestSimulateFill_LimitOrderWithPartialFill(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage:     ProportionalSlippage{BasisPoints: 0},
		MaxVolumePct: 0.05,
	})
	bar := testBar(100, 105, 95, 2000) // max = 100
	order := &domain.Order{
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   300,
		LimitPrice: fp(101),
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if !res.Partial {
		t.Fatal("expected Partial = true")
	}
	floatClose(t, res.FillQuantity, 100, 1e-9)
}

// ---------- Combined integration tests ----------

func TestSimulateFill_IntegrationBuyMarketWithAllFeatures(t *testing.T) {
	t.Parallel()
	eng, _ := NewFillEngine(FillConfig{
		Slippage:     VolatilityScaledSlippage{ScaleFactor: 0.5},
		Spread:       FixedSpread{SpreadBps: 10},
		MaxVolumePct: 0.20,
		Costs: TransactionCosts{
			CommissionPerOrder: 1.50,
			CommissionPerUnit:  0.005,
			ExchangeFeePct:     0.0003,
		},
	})
	// bar: close=100, high=104, low=96 → volatility = 8/100 = 0.08
	bar := testBar(100, 104, 96, 500)
	order := &domain.Order{
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  200, // max fill = 500*0.20 = 100
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if !res.Partial {
		t.Fatal("expected Partial = true")
	}
	floatClose(t, res.FillQuantity, 100, 1e-9)

	// ask = 100 * (1 + 5/10000) = 100.05
	// vol-slippage = 0.5 * 0.08 = 0.04 → 100.05 * 1.04 = 104.052
	expectedPrice := 100.05 * 1.04
	floatClose(t, res.FillPrice, expectedPrice, 1e-6)

	// commission = 1.50 + 0.005*100 = 2.0
	floatClose(t, res.Commission, 2.0, 1e-9)
	// exchangeFee = 104.052 * 100 * 0.0003 = 3.12156
	floatClose(t, res.ExchangeFee, expectedPrice*100*0.0003, 1e-6)
}

// ---------- Spread-aware limit marketability tests ----------

func TestSimulateFill_LimitBuyWithSpreadNoFillWhenAskAboveLimit(t *testing.T) {
	t.Parallel()
	// Bar low = 99, but with a 200 bps spread the ask at low = 99 * (1 + 100/10000) = 99.99
	// A buy limit at 99.98 should NOT fill because the effective ask never drops below the limit.
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
		Spread:   FixedSpread{SpreadBps: 200},
	})
	bar := testBar(100, 105, 99, 10000) // low = 99
	order := &domain.Order{
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   10,
		LimitPrice: fp(99.98), // below ask at low (99.99)
	}

	_, err := eng.SimulateFill(order, bar)
	if !errors.Is(err, ErrNoFill) {
		t.Fatalf("err = %v, want %v", err, ErrNoFill)
	}
}

func TestSimulateFill_LimitSellWithSpreadNoFillWhenBidBelowLimit(t *testing.T) {
	t.Parallel()
	// Bar high = 101, with 200 bps spread the bid at high = 101 * (1 - 100/10000) = 99.99
	// A sell limit at 100.00 should NOT fill because the effective bid never reaches the limit.
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
		Spread:   FixedSpread{SpreadBps: 200},
	})
	bar := testBar(100, 101, 95, 10000) // high = 101
	order := &domain.Order{
		Side:       domain.OrderSideSell,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   10,
		LimitPrice: fp(100.00), // above bid at high (99.99)
	}

	_, err := eng.SimulateFill(order, bar)
	if !errors.Is(err, ErrNoFill) {
		t.Fatalf("err = %v, want %v", err, ErrNoFill)
	}
}

func TestSimulateFill_LimitBuyWithSpreadFillsWhenAskBelowLimit(t *testing.T) {
	t.Parallel()
	// Bar low = 99, with 200 bps spread the ask at low = 99 * (1 + 100/10000) = 99.99
	// A buy limit at 100.00 should fill since the effective ask (99.99) is below the limit.
	eng, _ := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
		Spread:   FixedSpread{SpreadBps: 200},
	})
	bar := testBar(100, 105, 99, 10000) // low = 99
	order := &domain.Order{
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   10,
		LimitPrice: fp(100.00), // above ask at low (99.99)
	}

	res, err := eng.SimulateFill(order, bar)
	if err != nil {
		t.Fatalf("SimulateFill() error = %v", err)
	}
	if !res.Filled {
		t.Fatal("expected Filled = true")
	}
}
