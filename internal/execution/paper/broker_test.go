package paper

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

// extremeSlippageBps represents 200% slippage (20000 bps) for sell-side clamp coverage.
const extremeSlippageBps = 20000

func TestPaperBrokerSubmitOrder_MarketOrderAppliesSlippage(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 10, 0)
	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if externalID == "" {
		t.Fatal("SubmitOrder() externalID = empty, want non-empty")
	}
	if order.Status != domain.OrderStatusFilled {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusFilled)
	}
	if order.FilledAvgPrice == nil {
		t.Fatal("SubmitOrder() FilledAvgPrice = nil, want non-nil")
	}
	expectedFillPrice := 100 * (1 + 10.0/10000)
	assertFloatClose(t, *order.FilledAvgPrice, expectedFillPrice, 1e-9)

	status, err := broker.GetOrderStatus(context.Background(), externalID)
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusFilled {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusFilled)
	}
}

func TestPaperBrokerSubmitOrder_DeductsFee(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0.01)
	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  2,
		StopPrice: floatPtr(100),
	}

	_, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}

	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}

	assertFloatClose(t, balance.Cash, 798, 1e-9)
	assertFloatClose(t, balance.BuyingPower, 798, 1e-9)
	assertFloatClose(t, balance.Equity, 998, 1e-9)
}

func TestPaperBrokerEvaluationProfilesRemainDistinct(t *testing.T) {
	t.Parallel()

	scoredProfile, err := domain.NewPaperEvaluationProfile(domain.PaperEvaluationModeScored, 100_000, 2, 5, 0.0001)
	if err != nil {
		t.Fatal(err)
	}
	stressProfile, err := domain.NewPaperEvaluationProfile(domain.PaperEvaluationModeStress, 100_000, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	scored, err := NewPaperBrokerWithProfile(scoredProfile)
	if err != nil {
		t.Fatal(err)
	}
	stress, err := NewPaperBrokerWithProfile(stressProfile)
	if err != nil {
		t.Fatal(err)
	}
	if scored.EvaluationProfile().CanShareStorageWith(stress.EvaluationProfile()) {
		t.Fatal("scored and stress brokers share an evidence namespace")
	}
	if !scored.EvaluationProfile().PromotionEligible() || stress.EvaluationProfile().PromotionEligible() {
		t.Fatal("broker promotion eligibility does not match its profile")
	}
}

func TestPaperBrokerSubmitOrder_RejectsInsufficientBalance(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(50, 0, 0.01)
	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Fatalf("SubmitOrder() error = %q, want insufficient balance", err.Error())
	}
	if order.Status != domain.OrderStatusRejected {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusRejected)
	}

	status, statusErr := broker.GetOrderStatus(context.Background(), externalID)
	if statusErr != nil {
		t.Fatalf("GetOrderStatus() error = %v", statusErr)
	}
	if status != domain.OrderStatusRejected {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusRejected)
	}

	balance, balanceErr := broker.GetAccountBalance(context.Background())
	if balanceErr != nil {
		t.Fatalf("GetAccountBalance() error = %v", balanceErr)
	}
	assertFloatClose(t, balance.Cash, 50, 1e-9)
}

func TestPaperBrokerSubmitOrder_LimitOrderWithoutReferenceRemainsSubmitted(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 25, 0)
	order := &domain.Order{
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   1,
		LimitPrice: floatPtr(100),
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.Status != domain.OrderStatusSubmitted {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusSubmitted)
	}
	if order.FilledAvgPrice != nil {
		t.Fatalf("SubmitOrder() FilledAvgPrice = %v, want nil", *order.FilledAvgPrice)
	}

	status, err := broker.GetOrderStatus(context.Background(), externalID)
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusSubmitted {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusSubmitted)
	}
}

func TestPaperBrokerSubmitOrder_LimitOrderReferencePrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		side       domain.OrderSide
		limit      float64
		reference  float64
		wantStatus domain.OrderStatus
		wantFill   bool
	}{
		{name: "buy fills when reference at limit", side: domain.OrderSideBuy, limit: 0.04, reference: 0.04, wantStatus: domain.OrderStatusFilled, wantFill: true},
		{name: "buy rests when reference above limit", side: domain.OrderSideBuy, limit: 0.04, reference: 0.05, wantStatus: domain.OrderStatusSubmitted},
		{name: "missing reference still rests", side: domain.OrderSideBuy, limit: 0.04, reference: 0, wantStatus: domain.OrderStatusSubmitted},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			broker := NewPaperBroker(1000, 0, 0)
			order := &domain.Order{
				Ticker:         "KXTEST",
				Side:           tc.side,
				OrderType:      domain.OrderTypeLimit,
				Quantity:       10,
				LimitPrice:     floatPtr(tc.limit),
				ReferencePrice: floatPtr(tc.reference),
			}

			externalID, err := broker.SubmitOrder(context.Background(), order)
			if err != nil {
				t.Fatalf("SubmitOrder() error = %v", err)
			}
			if order.Status != tc.wantStatus {
				t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, tc.wantStatus)
			}
			if tc.wantFill {
				if order.FilledAvgPrice == nil || order.FilledAt == nil || order.FilledQuantity != order.Quantity {
					t.Fatalf("filled order facts = %+v, want full fill", order)
				}
				if *order.FilledAvgPrice > *order.LimitPrice+1e-9 {
					t.Fatalf("FilledAvgPrice = %v, want <= limit %v", *order.FilledAvgPrice, *order.LimitPrice)
				}
			}
			status, err := broker.GetOrderStatus(context.Background(), externalID)
			if err != nil {
				t.Fatalf("GetOrderStatus() error = %v", err)
			}
			if status != tc.wantStatus {
				t.Fatalf("GetOrderStatus() = %q, want %q", status, tc.wantStatus)
			}
		})
	}
}

func TestPaperBrokerCloneOrderCopiesReferencePricePointer(t *testing.T) {
	t.Parallel()

	reference := 0.04
	original := &domain.Order{ReferencePrice: &reference}
	cloned := cloneOrder(original)

	if cloned == nil || cloned.ReferencePrice == nil {
		t.Fatalf("cloneOrder() ReferencePrice = %v, want non-nil", cloned.ReferencePrice)
	}
	if cloned.ReferencePrice == original.ReferencePrice {
		t.Fatal("cloneOrder() preserved ReferencePrice pointer alias, want deep copy")
	}
	if *cloned.ReferencePrice != *original.ReferencePrice {
		t.Fatalf("cloneOrder() ReferencePrice = %v, want %v", *cloned.ReferencePrice, *original.ReferencePrice)
	}
}

func TestPaperBrokerSubmitOrder_NormalizesTickerForPositions(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)

	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:     "aapl",
		MarketType: domain.MarketTypeStock,
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeMarket,
		Quantity:   1,
		StopPrice:  floatPtr(100),
	})
	if err != nil {
		t.Fatalf("SubmitOrder(first) error = %v", err)
	}

	_, err = broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:     " AAPL ",
		MarketType: domain.MarketTypeStock,
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeMarket,
		Quantity:   2,
		StopPrice:  floatPtr(100),
	})
	if err != nil {
		t.Fatalf("SubmitOrder(second) error = %v", err)
	}

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("GetPositions() len = %d, want %d", len(positions), 1)
	}
	if positions[0].Ticker != "AAPL" {
		t.Fatalf("positions[0].Ticker = %q, want %q", positions[0].Ticker, "AAPL")
	}
	assertFloatClose(t, positions[0].Quantity, 3, 1e-9)
}

func TestPaperBrokerPredictionPositionsStaySeparatedAndRestoreUpdatesCanonicalSide(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	if err := broker.RestorePositions([]domain.Position{{Ticker: "MARKET:YES", MarketType: domain.MarketTypePolymarket, Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 0.4, CurrentPrice: floatPtr(0.45)}}); err != nil {
		t.Fatalf("RestorePositions() error = %v", err)
	}
	if _, err := broker.SubmitOrder(context.Background(), &domain.Order{Ticker: "MARKET", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(0.5)}); err != nil {
		t.Fatalf("SubmitOrder(YES) error = %v", err)
	}
	if _, err := broker.SubmitOrder(context.Background(), &domain.Order{Ticker: "MARKET", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideBuy, PredictionSide: "NO", OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(0.5)}); err != nil {
		t.Fatalf("SubmitOrder(NO) error = %v", err)
	}
	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("GetPositions() len = %d, want 2", len(positions))
	}
	if positions[0].Ticker != "MARKET:NO" && positions[1].Ticker != "MARKET:NO" {
		t.Fatalf("missing NO position: %+v", positions)
	}
	if positions[0].Ticker != "MARKET:YES" && positions[1].Ticker != "MARKET:YES" {
		t.Fatalf("missing YES position: %+v", positions)
	}
	for _, pos := range positions {
		if pos.Ticker == "MARKET:YES" {
			assertFloatClose(t, pos.Quantity, 3, 1e-9)
		}
		if pos.Ticker == "MARKET:NO" {
			assertFloatClose(t, pos.Quantity, 1, 1e-9)
		}
	}
}

func TestPaperBrokerPredictionSubmitKeepsBaseOrderTickerAndCanonicalPositionTicker(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	order := &domain.Order{Ticker: " market ", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideBuy, PredictionSide: "yes", OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(0.5)}
	if _, err := broker.SubmitOrder(context.Background(), order); err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.Ticker != "MARKET" {
		t.Fatalf("order ticker = %q, want base ticker", order.Ticker)
	}
	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 || positions[0].Ticker != "MARKET:YES" {
		t.Fatalf("positions = %+v, want canonical side-qualified position", positions)
	}

	alreadyQualified := &domain.Order{Ticker: "MARKET:YES", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(0.5)}
	if _, err := broker.SubmitOrder(context.Background(), alreadyQualified); err != nil {
		t.Fatalf("SubmitOrder(alreadyQualified) error = %v", err)
	}
	if alreadyQualified.Ticker != "MARKET" {
		t.Fatalf("already-qualified order ticker = %q, want base ticker", alreadyQualified.Ticker)
	}
	positions, err = broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 || positions[0].Ticker != "MARKET:YES" {
		t.Fatalf("positions after already-qualified order = %+v, want exactly MARKET:YES", positions)
	}
}

func TestPaperBrokerPredictionSubmitRejectsConflictingTickerSuffix(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	_, err := broker.SubmitOrder(context.Background(), &domain.Order{Ticker: "MARKET:NO", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(0.5)})
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want conflict rejection")
	}
	if got := err.Error(); !strings.Contains(got, "conflicts with prediction side") {
		t.Fatalf("SubmitOrder() error = %q, want conflict rejection", got)
	}
}

func TestPaperBrokerPredictionSubmitInfersMissingSideFromTickerSuffix(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	order := &domain.Order{Ticker: " market : no ", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideBuy, PredictionSide: "", OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(0.5)}
	if _, err := broker.SubmitOrder(context.Background(), order); err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.Ticker != "MARKET" || order.PredictionSide != "NO" {
		t.Fatalf("order = %+v, want base ticker + inferred side", order)
	}
}

func TestPaperBrokerPredictionSubmitRejectsMissingSuffixWithoutSide(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	_, err := broker.SubmitOrder(context.Background(), &domain.Order{Ticker: "MARKET", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideBuy, PredictionSide: "", OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(0.5)})
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want rejection")
	}
}

func TestPaperBrokerSubmitOrder_UsesInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 25, 15, 30, 0, 0, time.UTC)
	broker := NewPaperBroker(1000, 0, 0)
	broker.SetNowFunc(func() time.Time { return now })

	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
	}

	if _, err := broker.SubmitOrder(context.Background(), order); err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.SubmittedAt == nil || !order.SubmittedAt.Equal(now) {
		t.Fatalf("SubmittedAt = %v, want %s", order.SubmittedAt, now)
	}
	if order.FilledAt == nil || !order.FilledAt.Equal(now) {
		t.Fatalf("FilledAt = %v, want %s", order.FilledAt, now)
	}
	if !order.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", order.CreatedAt, now)
	}

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("GetPositions() len = %d, want 1", len(positions))
	}
	if !positions[0].OpenedAt.Equal(now) {
		t.Fatalf("positions[0].OpenedAt = %s, want %s", positions[0].OpenedAt, now)
	}
}

func TestPaperBrokerSubmitOrder_ClampsExtremeSellSlippage(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, extremeSlippageBps, 0)
	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
	}

	_, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.FilledAvgPrice == nil {
		t.Fatal("SubmitOrder() FilledAvgPrice = nil, want non-nil")
	}
	if *order.FilledAvgPrice <= 0 {
		t.Fatalf("SubmitOrder() FilledAvgPrice = %v, want > 0", *order.FilledAvgPrice)
	}
}

func TestPaperBrokerRestoreState(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	if err := broker.RestoreAccount(execution.Balance{Currency: "USD", Cash: 750, BuyingPower: 750, Equity: 820}); err != nil {
		t.Fatalf("RestoreAccount() error = %v", err)
	}
	if err := broker.RestorePositions([]domain.Position{{Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 100, CurrentPrice: floatPtr(110)}}); err != nil {
		t.Fatalf("RestorePositions() error = %v", err)
	}
	if err := broker.RestoreOrderSequence(41); err != nil {
		t.Fatalf("RestoreOrderSequence() error = %v", err)
	}
	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 || positions[0].Ticker != "AAPL" {
		t.Fatalf("unexpected positions: %+v", positions)
	}
	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	assertFloatClose(t, balance.Cash, 750, 1e-9)
	order := &domain.Order{Ticker: "MSFT", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(100)}
	id, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if id != "paper-42" {
		t.Fatalf("next external id = %q, want paper-42", id)
	}
}

func TestPaperBrokerRestoreState_UsesUnmultipliedCashAndMarketValue(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	if err := broker.RestoreAccount(execution.Balance{Currency: "USD", Cash: 700, BuyingPower: 700, Equity: 1010}); err != nil {
		t.Fatalf("RestoreAccount() error = %v", err)
	}
	if err := broker.RestorePositions([]domain.Position{
		{Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 100, CurrentPrice: floatPtr(110), ContractMultiplier: 100},
		{Ticker: "YES", Side: domain.PositionSideLong, Quantity: 3, AvgEntry: 0.25, CurrentPrice: floatPtr(0.30), MarketType: domain.MarketTypeStock, ContractMultiplier: 100},
	}); err != nil {
		t.Fatalf("RestorePositions() error = %v", err)
	}
	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	assertFloatClose(t, balance.Cash, 700, 1e-9)
	assertFloatClose(t, balance.Equity, 1010, 1e-9)
	assertFloatClose(t, balance.BuyingPower, 700, 1e-9)
	_, err = broker.SubmitOrder(context.Background(), &domain.Order{Ticker: "MSFT", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(100)})
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	balance, err = broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	assertFloatClose(t, balance.Cash, 600, 1e-9)
}

func TestPaperBrokerRestorePositions_MergesLotsAndRejectsMixedKeys(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	if err := broker.RestorePositions([]domain.Position{
		{Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 100, CurrentPrice: floatPtr(110)},
		{Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 3, AvgEntry: 120, CurrentPrice: floatPtr(110)},
	}); err != nil {
		t.Fatalf("RestorePositions() error = %v", err)
	}
	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions len = %d, want 1", len(positions))
	}
	assertFloatClose(t, positions[0].Quantity, 5, 1e-9)
	assertFloatClose(t, positions[0].AvgEntry, 112, 1e-9)
	if positions[0].CurrentPrice == nil {
		t.Fatal("CurrentPrice = nil")
	}
	if err := broker.RestorePositions([]domain.Position{
		{Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 100},
		{Ticker: "AAPL", Side: domain.PositionSideShort, Quantity: 1, AvgEntry: 100},
	}); err == nil {
		t.Fatal("RestorePositions() mixed side error = nil, want error")
	}
}

func TestPaperBrokerCancelOrder_CancelsSubmittedOrder(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	order := &domain.Order{
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   1,
		LimitPrice: floatPtr(100),
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.Status != domain.OrderStatusSubmitted {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusSubmitted)
	}

	if err := broker.CancelOrder(context.Background(), externalID); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}

	status, err := broker.GetOrderStatus(context.Background(), externalID)
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusCancelled {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusCancelled)
	}
}

func TestPaperBrokerGetOrderStatus_ReturnsTrackedStatus(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	broker.orders["paper-42"] = &domain.Order{
		ExternalID: "paper-42",
		Status:     domain.OrderStatusPartial,
	}

	status, err := broker.GetOrderStatus(context.Background(), "paper-42")
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusPartial {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusPartial)
	}
}

func TestPaperBrokerGetPositions_ReturnsClonedSortedPositions(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	broker.positions["MSFT"] = &domain.Position{
		Ticker:       "MSFT",
		Side:         domain.PositionSideLong,
		Quantity:     2,
		AvgEntry:     250,
		CurrentPrice: floatPtr(255),
	}
	broker.positions["AAPL"] = &domain.Position{
		Ticker:       "AAPL",
		Side:         domain.PositionSideLong,
		Quantity:     1,
		AvgEntry:     100,
		CurrentPrice: floatPtr(105),
	}

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("GetPositions() len = %d, want %d", len(positions), 2)
	}
	if positions[0].Ticker != "AAPL" || positions[1].Ticker != "MSFT" {
		t.Fatalf("GetPositions() tickers = [%q %q], want [\"AAPL\" \"MSFT\"]", positions[0].Ticker, positions[1].Ticker)
	}

	positions[0].Quantity = 99
	*positions[0].CurrentPrice = 999

	refetched, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() second call error = %v", err)
	}
	assertFloatClose(t, refetched[0].Quantity, 1, 1e-9)
	assertFloatClose(t, *refetched[0].CurrentPrice, 105, 1e-9)
}

func TestPaperBrokerGetAccountBalance_ReturnsSnapshot(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	broker.balance = execution.Balance{
		Currency:    "USD",
		Cash:        850,
		BuyingPower: 850,
		Equity:      910,
	}

	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	if balance.Currency != "USD" {
		t.Fatalf("GetAccountBalance() currency = %q, want %q", balance.Currency, "USD")
	}
	assertFloatClose(t, balance.Cash, 850, 1e-9)
	assertFloatClose(t, balance.BuyingPower, 850, 1e-9)
	assertFloatClose(t, balance.Equity, 910, 1e-9)

	balance.Cash = 1

	refetched, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() second call error = %v", err)
	}
	assertFloatClose(t, refetched.Cash, 850, 1e-9)
}

func TestPaperBrokerSubmitOrder_MarketOrderPrefersLimitPriceOverStopPrice(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(100000, 0, 0)
	entryPrice := 150.0
	stopPrice := 145.0
	order := &domain.Order{
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeMarket,
		Quantity:   1,
		LimitPrice: &entryPrice,
		StopPrice:  &stopPrice,
	}

	_, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.FilledAvgPrice == nil {
		t.Fatal("FilledAvgPrice = nil, want non-nil")
	}
	// Should fill at entry price (150), not stop price (145).
	assertFloatClose(t, *order.FilledAvgPrice, 150, 1e-9)
}

func assertFloatClose(t *testing.T, got, want, epsilon float64) {
	t.Helper()
	if math.Abs(got-want) > epsilon {
		t.Fatalf("float mismatch: got %v, want %v (epsilon %v)", got, want, epsilon)
	}
}
