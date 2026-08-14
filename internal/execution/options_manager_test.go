package execution_test

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

type mockOptionsBroker struct {
	submitOptionOrderFn func(ctx context.Context, order *domain.Order) (string, error)
	submitSpreadOrderFn func(ctx context.Context, spread *domain.OptionSpread, quantity float64) ([]string, error)
	optionFillReportFn  func(ctx context.Context, order *domain.Order) (execution.OptionFillReport, error)
}

type recordingOptionFillRepo struct {
	batches [][]repository.OptionFillInput
	err     error
}

func (r *recordingOptionFillRepo) ApplyOptionFills(_ context.Context, inputs []repository.OptionFillInput) ([]repository.OptionFillResult, error) {
	if r.err != nil {
		return nil, r.err
	}
	batch := append([]repository.OptionFillInput(nil), inputs...)
	r.batches = append(r.batches, batch)
	results := make([]repository.OptionFillResult, len(inputs))
	for index, input := range inputs {
		positionID := uuid.New()
		if input.PositionID != nil {
			positionID = *input.PositionID
		}
		results[index] = repository.OptionFillResult{OrderID: input.Order.ID, PositionID: positionID, TradeID: uuid.New()}
	}
	return results, nil
}

func (b *mockOptionsBroker) GetAccountBalance(context.Context) (execution.Balance, error) {
	return execution.Balance{Cash: 100000, BuyingPower: 100000, Equity: 100000, Currency: "USD"}, nil
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

func (b *mockOptionsBroker) PreflightSpread(context.Context, *domain.OptionSpread, float64) error {
	return nil
}

func (b *mockOptionsBroker) RollbackOptionOrder(context.Context, string) error { return nil }

func (b *mockOptionsBroker) RollbackOptionSpread(context.Context, []string) error { return nil }

func (b *mockOptionsBroker) FinalizeOptionSpread([]string) error { return nil }

func newTestOptionsManager(broker *mockOptionsBroker, orderRepo *mockOrderRepo, positionRepo *mockPositionRepo, tradeRepo *mockTradeRepo, riskEng *mockRiskEngine) *execution.OptionsOrderManager {
	return newTestOptionsManagerWithFillRepo(broker, orderRepo, positionRepo, tradeRepo, riskEng, &recordingOptionFillRepo{})
}

func newTestOptionsManagerWithFillRepo(broker execution.OptionsBroker, orderRepo *mockOrderRepo, positionRepo *mockPositionRepo, tradeRepo *mockTradeRepo, riskEng *mockRiskEngine, fillRepo repository.OptionFillRepository) *execution.OptionsOrderManager {
	return execution.NewOptionsOrderManager(broker, orderRepo, positionRepo, tradeRepo, riskEng, slog.Default()).WithOptionFillRepo(fillRepo)
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
	greeks := &domain.OptionGreeks{Delta: 0.4, Gamma: 0.02, Theta: -0.1, Vega: 0.2}
	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryPrice: 2.5, PositionSize: 1, OptionGreeks: greeks}, uuid.New(), uuid.New())
	if err == nil || len(orderRepo.orders) != 0 {
		t.Fatalf("risk rejection should fail before persistence: err=%v orders=%d", err, len(orderRepo.orders))
	}
}

func TestProcessOptionSignal_PositionLimitUsesContractMultiplier(t *testing.T) {
	var exposure float64
	riskEng := &mockRiskEngine{checkPositionLimitsFn: func(_ context.Context, _ string, quantity float64, _ risk.Portfolio) (bool, string, error) {
		exposure = quantity
		return true, "", nil
	}}
	mgr := newTestOptionsManager(&mockOptionsBroker{}, &mockOrderRepo{}, &mockPositionRepo{}, &mockTradeRepo{}, riskEng)
	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryPrice: 2.5, PositionSize: 2}, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("ProcessOptionSignal() error = %v", err)
	}
	if math.Abs(exposure-0.005) > 1e-12 {
		t.Fatalf("exposure = %f, want 0.005 including 100x multiplier", exposure)
	}
}

func TestProcessOptionSignal_PreservesImmediatePaperFill(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	fillRepo := &recordingOptionFillRepo{}
	broker := &mockOptionsBroker{submitOptionOrderFn: func(_ context.Context, order *domain.Order) (string, error) {
		price := 2.5
		filledAt := time.Now().UTC()
		order.Status = domain.OrderStatusFilled
		order.FilledQuantity = order.Quantity
		order.FilledAvgPrice = &price
		order.FilledAt = &filledAt
		return "paper-option-1", nil
	}}
	mgr := newTestOptionsManagerWithFillRepo(broker, orderRepo, positionRepo, tradeRepo, &mockRiskEngine{}, fillRepo)
	greeks := &domain.OptionGreeks{Delta: 0.4, Gamma: 0.02, Theta: -0.1, Vega: 0.2}
	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryPrice: 2.5, PositionSize: 1, OptionGreeks: greeks}, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("ProcessOptionSignal() error = %v", err)
	}
	if len(orderRepo.updates) != 0 {
		t.Fatalf("filled order must be committed only by the atomic repository, got %d pre-updates", len(orderRepo.updates))
	}
	if len(fillRepo.batches) != 1 || len(fillRepo.batches[0]) != 1 {
		t.Fatalf("filled option lifecycle was not one atomic batch: %+v", fillRepo.batches)
	}
	input := fillRepo.batches[0][0]
	if input.PositionID != nil || input.Order.UnderlyingTicker != "AAPL" || input.Order.OptionGreeks == nil || input.Order.OptionGreeks.Delta != 0.4 || input.Premium != 250 || input.Fee != 0.65 {
		t.Fatalf("filled option accounting not preserved: %+v", input)
	}
}

func TestProcessOptionSignal_RollsBackPaperFillWhenAtomicPersistenceFails(t *testing.T) {
	broker := paper.NewPaperBroker(10000, 0, 0)
	fillRepo := &recordingOptionFillRepo{err: errors.New("database unavailable")}
	orderRepo := &mockOrderRepo{}
	mgr := newTestOptionsManagerWithFillRepo(broker, orderRepo, &mockPositionRepo{}, &mockTradeRepo{}, &mockRiskEngine{}, fillRepo).WithBrokerName("paper")
	err := mgr.ProcessOptionSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalBuy}, execution.TradingPlan{Ticker: "AAPL271217C00150000", EntryPrice: 2.5, PositionSize: 1}, uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected atomic persistence failure")
	}
	balance, balanceErr := broker.GetAccountBalance(context.Background())
	if balanceErr != nil || balance.Cash != 10000 {
		t.Fatalf("paper fill was not compensated: balance=%+v err=%v", balance, balanceErr)
	}
	if len(orderRepo.updates) != 1 || orderRepo.updates[0].Status != domain.OrderStatusRejected || orderRepo.updates[0].FilledQuantity != 0 || orderRepo.updates[0].FilledAt != nil {
		t.Fatalf("compensated order was not durably rejected: %+v", orderRepo.updates)
	}
}

func TestCloseOptionPositionPersistsLifecycle(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	fillRepo := &recordingOptionFillRepo{}
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
	mgr := newTestOptionsManagerWithFillRepo(broker, orderRepo, positionRepo, tradeRepo, &mockRiskEngine{}, fillRepo)
	if err := mgr.CloseOptionPosition(context.Background(), position, 3.5, runID, "profit target"); err != nil {
		t.Fatalf("CloseOptionPosition() error = %v", err)
	}
	if len(orderRepo.orders) != 1 || orderRepo.orders[0].PositionIntent == nil || *orderRepo.orders[0].PositionIntent != domain.PositionIntentSellToClose {
		t.Fatalf("close order missing sell-to-close intent: %+v", orderRepo.orders)
	}
	if len(fillRepo.batches) != 1 || len(fillRepo.batches[0]) != 1 {
		t.Fatalf("close lifecycle was not one atomic batch: %+v", fillRepo.batches)
	}
	input := fillRepo.batches[0][0]
	if input.PositionID == nil || *input.PositionID != position.ID || input.ExitReason != "profit target" || input.Premium != 700 || input.Fee != 0.65 {
		t.Fatalf("closing fill not preserved: %+v", input)
	}
}

func TestCloseOptionPositionRejectsIncompletePersistence(t *testing.T) {
	mgr := newTestOptionsManager(&mockOptionsBroker{}, &mockOrderRepo{}, &mockPositionRepo{}, &mockTradeRepo{}, &mockRiskEngine{})
	err := mgr.CloseOptionPosition(context.Background(), &domain.Position{AssetClass: domain.AssetClassOption, Quantity: 1}, 2, uuid.New(), "")
	if err == nil {
		t.Fatal("expected incomplete persisted contract to fail closed")
	}
}

func TestProcessSpreadSignalPreflightPreventsOrphanOrders(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	mgr := execution.NewOptionsOrderManager(paper.NewPaperBroker(100000, 0, 0), orderRepo, &mockPositionRepo{}, &mockTradeRepo{}, &mockRiskEngine{}, slog.Default())
	spread := &domain.OptionSpread{Underlying: "AAPL", MaxRisk: 500, Legs: []domain.SpreadLeg{{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00150000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 150, Expiry: time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC), Multiplier: 100}, Side: domain.OrderSideBuy, PositionIntent: domain.PositionIntentBuyToOpen, Ratio: 1}}}
	err := mgr.ProcessSpreadSignal(context.Background(), spread, 1, uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected unsupported paper spread to fail preflight")
	}
	if len(orderRepo.orders) != 0 {
		t.Fatalf("preflight failure persisted %d orphan leg orders", len(orderRepo.orders))
	}
}

func TestProcessSpreadSignalPersistsAtomicPaperLegs(t *testing.T) {
	orderRepo, positionRepo, tradeRepo := &mockOrderRepo{}, &mockPositionRepo{}, &mockTradeRepo{}
	broker := paper.NewPaperBroker(100000, 0, 0)
	fillRepo := &recordingOptionFillRepo{}
	mgr := newTestOptionsManagerWithFillRepo(broker, orderRepo, positionRepo, tradeRepo, &mockRiskEngine{}, fillRepo).WithBrokerName("paper")
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	spread := &domain.OptionSpread{StrategyType: domain.StrategyBullCallSpread, Underlying: "AAPL", MaxRisk: 150, MaxReward: 350, Legs: []domain.SpreadLeg{
		{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00150000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 150, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideBuy, PositionIntent: domain.PositionIntentBuyToOpen, Ratio: 1, ExecutablePrice: 2.5},
		{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00155000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 155, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideSell, PositionIntent: domain.PositionIntentSellToOpen, Ratio: 1, ExecutablePrice: 1},
	}}
	if err := mgr.ProcessSpreadSignal(context.Background(), spread, 1, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("ProcessSpreadSignal() error = %v", err)
	}
	if len(fillRepo.batches) != 1 || len(fillRepo.batches[0]) != 2 {
		t.Fatalf("spread lifecycle was not one atomic batch: %+v", fillRepo.batches)
	}
	if fillRepo.batches[0][0].Order.LegGroupID == nil || fillRepo.batches[0][1].Order.LegGroupID == nil || *fillRepo.batches[0][0].Order.LegGroupID != *fillRepo.batches[0][1].Order.LegGroupID {
		t.Fatalf("spread legs not atomically grouped: %+v", fillRepo.batches[0])
	}
}

func TestProcessSpreadSignalCompensatesPaperDebitWhenAtomicPersistenceFails(t *testing.T) {
	orderRepo, positionRepo, tradeRepo := &mockOrderRepo{}, &mockPositionRepo{}, &mockTradeRepo{}
	broker := paper.NewPaperBroker(100000, 0, 0)
	fillRepo := &recordingOptionFillRepo{err: errors.New("database unavailable")}
	mgr := newTestOptionsManagerWithFillRepo(broker, orderRepo, positionRepo, tradeRepo, &mockRiskEngine{}, fillRepo).WithBrokerName("paper")
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	spread := &domain.OptionSpread{StrategyType: domain.StrategyBullCallSpread, Underlying: "AAPL", MaxRisk: 150, MaxReward: 350, Legs: []domain.SpreadLeg{
		{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00150000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 150, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideBuy, PositionIntent: domain.PositionIntentBuyToOpen, Ratio: 1, ExecutablePrice: 2.5},
		{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00155000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 155, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideSell, PositionIntent: domain.PositionIntentSellToOpen, Ratio: 1, ExecutablePrice: 1},
	}}
	if err := mgr.ProcessSpreadSignal(context.Background(), spread, 1, uuid.New(), uuid.New()); err == nil {
		t.Fatal("expected atomic spread persistence failure")
	}
	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil || balance.Cash != 100000 {
		t.Fatalf("paper spread was not compensated: balance=%+v err=%v", balance, err)
	}
	if len(orderRepo.updates) != 2 || orderRepo.updates[0].Status != domain.OrderStatusRejected || orderRepo.updates[1].Status != domain.OrderStatusRejected {
		t.Fatalf("compensated spread orders were not rejected: %+v", orderRepo.updates)
	}
}

func TestProcessSpreadSignalAtomicallyClosesPersistedLegGroup(t *testing.T) {
	orderRepo, positionRepo, tradeRepo := &mockOrderRepo{}, &mockPositionRepo{}, &mockTradeRepo{}
	strategyID, groupID := uuid.New(), uuid.New()
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	optionType, longStrike, shortStrike := domain.OptionTypeCall, 150.0, 155.0
	positions := []domain.Position{
		{ID: uuid.New(), StrategyID: &strategyID, Ticker: "AAPL271217C00150000", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 2.5, AssetClass: domain.AssetClassOption, UnderlyingTicker: "AAPL", OptionType: &optionType, Strike: &longStrike, Expiry: &expiry, ContractMultiplier: 100, LegGroupID: &groupID},
		{ID: uuid.New(), StrategyID: &strategyID, Ticker: "AAPL271217C00155000", Side: domain.PositionSideShort, Quantity: 1, AvgEntry: 1, AssetClass: domain.AssetClassOption, UnderlyingTicker: "AAPL", OptionType: &optionType, Strike: &shortStrike, Expiry: &expiry, ContractMultiplier: 100, LegGroupID: &groupID},
	}
	positionRepo.getByStrategyFn = func(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
		return positions, nil
	}
	spread := &domain.OptionSpread{StrategyType: domain.StrategyBullCallSpread, Underlying: "AAPL", Legs: []domain.SpreadLeg{
		{Contract: domain.OptionContract{OCCSymbol: positions[0].Ticker, Underlying: "AAPL", OptionType: optionType, Strike: longStrike, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideSell, PositionIntent: domain.PositionIntentSellToClose, Ratio: 1, ExecutablePrice: 3},
		{Contract: domain.OptionContract{OCCSymbol: positions[1].Ticker, Underlying: "AAPL", OptionType: optionType, Strike: shortStrike, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideBuy, PositionIntent: domain.PositionIntentBuyToClose, Ratio: 1, ExecutablePrice: 1.2},
	}}
	fillRepo := &recordingOptionFillRepo{}
	riskEng := &mockRiskEngine{isKillSwitchActiveFn: func(context.Context) (bool, error) { return true, nil }}
	mgr := newTestOptionsManagerWithFillRepo(paper.NewPaperBroker(100000, 0, 0), orderRepo, positionRepo, tradeRepo, riskEng, fillRepo).WithBrokerName("paper")
	if err := mgr.ProcessSpreadSignal(context.Background(), spread, 1, strategyID, uuid.New()); err != nil {
		t.Fatalf("ProcessSpreadSignal(close) error = %v", err)
	}
	if len(fillRepo.batches) != 1 || len(fillRepo.batches[0]) != 2 {
		t.Fatalf("spread close lifecycle was not one atomic batch: %+v", fillRepo.batches)
	}
	for index := range fillRepo.batches[0] {
		if fillRepo.batches[0][index].PositionID == nil || fillRepo.batches[0][index].ExitReason != "strategy spread close" {
			t.Fatalf("spread close leg %d incomplete: %+v", index, fillRepo.batches[0][index])
		}
	}
}
