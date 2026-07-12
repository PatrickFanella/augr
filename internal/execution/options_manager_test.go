package execution_test

import (
	"context"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

type mockOptionsBroker struct {
	submitOptionOrderFn func(ctx context.Context, order *domain.Order) (string, error)
	submitSpreadOrderFn func(ctx context.Context, spread *domain.OptionSpread, quantity float64) ([]string, error)
	optionFillReportFn  func(ctx context.Context, order *domain.Order) (execution.OptionFillReport, error)
}

func (b *mockOptionsBroker) OptionFillReport(ctx context.Context, order *domain.Order) (execution.OptionFillReport, error) {
	if b.optionFillReportFn != nil {
		return b.optionFillReportFn(ctx, order)
	}
	return execution.OptionFillReport{Premium: order.FilledQuantity * 100 * 2.5, Fee: order.FilledQuantity * 0.65}, nil
}

func (b *mockOptionsBroker) SubmitOptionOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b.submitOptionOrderFn != nil {
		return b.submitOptionOrderFn(ctx, order)
	}
	return "opt-ext-123", nil
}

func (b *mockOptionsBroker) SubmitSpreadOrder(ctx context.Context, spread *domain.OptionSpread, quantity float64) ([]string, error) {
	if b.submitSpreadOrderFn != nil {
		return b.submitSpreadOrderFn(ctx, spread, quantity)
	}
	return []string{"leg-1"}, nil
}

func newTestOptionsManager(broker *mockOptionsBroker, orderRepo *mockOrderRepo, positionRepo *mockPositionRepo, tradeRepo *mockTradeRepo, riskEng *mockRiskEngine) *execution.OptionsOrderManager {
	return execution.NewOptionsOrderManager(broker, orderRepo, positionRepo, tradeRepo, riskEng, slog.Default())
}

func TestProcessOptionSignal_PersistsExplicitContractMetadata(t *testing.T) {
	broker := &mockOptionsBroker{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	riskEng := &mockRiskEngine{}

	mgr := newTestOptionsManager(broker, orderRepo, positionRepo, tradeRepo, riskEng)
	plan := execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryType: "market", EntryPrice: 2.5, PositionSize: 1}
	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, plan, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("ProcessOptionSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}
	if len(orderRepo.updates) != 1 {
		t.Fatalf("expected 1 order update, got %d", len(orderRepo.updates))
	}
	if got := orderRepo.updates[0].Status; got != domain.OrderStatusSubmitted {
		t.Fatalf("order update status = %s, want %s", got, domain.OrderStatusSubmitted)
	}
	created := orderRepo.orders[0]
	if created.MarketType != domain.MarketTypeOptions || created.AssetClass != domain.AssetClassOption {
		t.Fatalf("order classification = %s/%s, want options/option", created.MarketType, created.AssetClass)
	}
	if created.UnderlyingTicker != "AAPL" || created.OptionType == nil || *created.OptionType != domain.OptionTypeCall || created.Strike == nil || *created.Strike != 150 || created.Expiry == nil || created.ContractMultiplier != 100 {
		t.Fatalf("option contract metadata not persisted: %+v", created)
	}
}

func TestProcessOptionSignal_LiveGateAllowsConfiguredBrokerName(t *testing.T) {
	strategyID := uuid.New()
	broker := &mockOptionsBroker{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	riskEng := &mockRiskEngine{}

	mgr := newTestOptionsManager(broker, orderRepo, positionRepo, tradeRepo, riskEng).
		WithBrokerName("alpaca").
		WithLiveTrading(true).
		WithLiveGate(execution.LiveGateConfig{
			EnableLiveTrading: true,
			AllowedStrategies: map[uuid.UUID]bool{strategyID: true},
			AllowedBrokers:    map[string]bool{"alpaca": true},
		})

	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryType: "market", EntryPrice: 2.5, PositionSize: 1}, strategyID, uuid.New())
	if err != nil {
		t.Fatalf("ProcessOptionSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}
	if len(orderRepo.updates) != 1 {
		t.Fatalf("expected 1 order update, got %d", len(orderRepo.updates))
	}
	if got := orderRepo.updates[0].Status; got != domain.OrderStatusSubmitted {
		t.Fatalf("order update status = %s, want %s", got, domain.OrderStatusSubmitted)
	}
}

func TestProcessOptionSignal_LiveGateDenies(t *testing.T) {
	broker := &mockOptionsBroker{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	riskEng := &mockRiskEngine{}

	mgr := newTestOptionsManager(broker, orderRepo, positionRepo, tradeRepo, riskEng).
		WithLiveTrading(true).
		WithLiveGate(execution.LiveGateConfig{EnableLiveTrading: true, AllowedBrokers: map[string]bool{"alpaca": true}})

	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryType: "market", EntryPrice: 2.5, PositionSize: 1}, uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected live gate error")
	}
	if len(orderRepo.orders) != 0 {
		t.Fatalf("expected 0 orders, got %d", len(orderRepo.orders))
	}
}

func TestProcessOptionSignal_RejectsGenericTickerBeforePersistence(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	mgr := newTestOptionsManager(&mockOptionsBroker{}, orderRepo, &mockPositionRepo{}, &mockTradeRepo{}, &mockRiskEngine{})
	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL", EntryPrice: 2.5, PositionSize: 1}, uuid.New(), uuid.New())
	if err == nil || len(orderRepo.orders) != 0 {
		t.Fatalf("generic ticker should fail before persistence: err=%v orders=%d", err, len(orderRepo.orders))
	}
}

func TestProcessOptionSignal_PreTradeRiskRejection(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	riskEng := &mockRiskEngine{checkPreTradeFn: func(context.Context, *domain.Order, risk.Portfolio) (bool, string, error) {
		return false, "options exposure limit", nil
	}}
	mgr := newTestOptionsManager(&mockOptionsBroker{}, orderRepo, &mockPositionRepo{}, &mockTradeRepo{}, riskEng)
	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryPrice: 2.5, PositionSize: 1}, uuid.New(), uuid.New())
	if err == nil || len(orderRepo.orders) != 0 {
		t.Fatalf("risk rejection should fail before persistence: err=%v orders=%d", err, len(orderRepo.orders))
	}
}

func TestProcessOptionSignal_PreservesImmediatePaperFill(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	broker := &mockOptionsBroker{submitOptionOrderFn: func(_ context.Context, order *domain.Order) (string, error) {
		price := 2.5
		filledAt := time.Now().UTC()
		order.Status = domain.OrderStatusFilled
		order.FilledQuantity = order.Quantity
		order.FilledAvgPrice = &price
		order.FilledAt = &filledAt
		return "paper-option-1", nil
	}}
	mgr := newTestOptionsManager(broker, orderRepo, positionRepo, tradeRepo, &mockRiskEngine{})
	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryPrice: 2.5, PositionSize: 1}, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("ProcessOptionSignal() error = %v", err)
	}
	if got := orderRepo.updates[0].Status; got != domain.OrderStatusFilled {
		t.Fatalf("paper fill status = %s, want filled", got)
	}
	if len(positionRepo.positions) != 1 || positionRepo.positions[0].UnderlyingTicker != "AAPL" {
		t.Fatalf("filled option position not persisted: %+v", positionRepo.positions)
	}
	if len(tradeRepo.trades) != 1 || tradeRepo.trades[0].Premium != 250 || tradeRepo.trades[0].Fee != 0.65 || tradeRepo.trades[0].OpenClose != "open" {
		t.Fatalf("filled option trade not persisted: %+v", tradeRepo.trades)
	}
}

func TestCloseOptionPositionPersistsLifecycle(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	broker := &mockOptionsBroker{submitOptionOrderFn: func(_ context.Context, order *domain.Order) (string, error) {
		price := *order.LimitPrice
		filledAt := time.Now().UTC()
		order.Status, order.FilledQuantity, order.FilledAvgPrice, order.FilledAt = domain.OrderStatusFilled, order.Quantity, &price, &filledAt
		return "paper-close-1", nil
	}, optionFillReportFn: func(_ context.Context, order *domain.Order) (execution.OptionFillReport, error) {
		return execution.OptionFillReport{Premium: *order.FilledAvgPrice * order.FilledQuantity * 100, Fee: 0.65}, nil
	}}
	strategyID, runID := uuid.New(), uuid.New()
	optionType, strike := domain.OptionTypeCall, 150.0
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	position := &domain.Position{ID: uuid.New(), StrategyID: &strategyID, MarketType: domain.MarketTypeOptions, Ticker: "AAPL271217C00150000", Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 2.5, AssetClass: domain.AssetClassOption, UnderlyingTicker: "AAPL", OptionType: &optionType, Strike: &strike, Expiry: &expiry, ContractMultiplier: 100}
	mgr := newTestOptionsManager(broker, orderRepo, positionRepo, tradeRepo, &mockRiskEngine{})
	if err := mgr.CloseOptionPosition(context.Background(), position, 3.5, runID, "profit target"); err != nil {
		t.Fatalf("CloseOptionPosition() error = %v", err)
	}
	if len(orderRepo.orders) != 1 || orderRepo.orders[0].PositionIntent == nil || *orderRepo.orders[0].PositionIntent != domain.PositionIntentSellToClose {
		t.Fatalf("close order missing sell-to-close intent: %+v", orderRepo.orders)
	}
	if len(positionRepo.updates) != 1 || positionRepo.updates[0].ClosedAt == nil || positionRepo.updates[0].Quantity != 0 || math.Abs(positionRepo.updates[0].RealizedPnL-199.35) > 1e-9 {
		t.Fatalf("position not closed correctly: %+v", positionRepo.updates)
	}
	if len(tradeRepo.trades) != 1 || tradeRepo.trades[0].OpenClose != "close" || tradeRepo.trades[0].ExitReason != "profit target" || tradeRepo.trades[0].Premium != 700 {
		t.Fatalf("closing trade not persisted: %+v", tradeRepo.trades)
	}
}

func TestCloseOptionPositionRejectsIncompletePersistence(t *testing.T) {
	mgr := newTestOptionsManager(&mockOptionsBroker{}, &mockOrderRepo{}, &mockPositionRepo{}, &mockTradeRepo{}, &mockRiskEngine{})
	err := mgr.CloseOptionPosition(context.Background(), &domain.Position{AssetClass: domain.AssetClassOption, Quantity: 1}, 2, uuid.New(), "")
	if err == nil {
		t.Fatal("expected incomplete persisted contract to fail closed")
	}
}
