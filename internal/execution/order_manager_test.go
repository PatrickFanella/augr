package execution_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	paperbroker "github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

// mockBroker implements execution.Broker.
type mockBroker struct {
	submitOrderFn       func(ctx context.Context, order *domain.Order) (string, error)
	cancelOrderFn       func(ctx context.Context, externalID string) error
	getOrderStatusFn    func(ctx context.Context, externalID string) (domain.OrderStatus, error)
	getPositionsFn      func(ctx context.Context) ([]domain.Position, error)
	getAccountBalanceFn func(ctx context.Context) (execution.Balance, error)
}

func (b *mockBroker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b.submitOrderFn != nil {
		return b.submitOrderFn(ctx, order)
	}

	return "ext-123", nil
}

func (b *mockBroker) CancelOrder(ctx context.Context, externalID string) error {
	if b.cancelOrderFn != nil {
		return b.cancelOrderFn(ctx, externalID)
	}

	return nil
}

func (b *mockBroker) GetOrderStatus(ctx context.Context, externalID string) (domain.OrderStatus, error) {
	if b.getOrderStatusFn != nil {
		return b.getOrderStatusFn(ctx, externalID)
	}

	return domain.OrderStatusFilled, nil
}

func (b *mockBroker) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b.getPositionsFn != nil {
		return b.getPositionsFn(ctx)
	}

	return nil, nil
}

func (b *mockBroker) GetAccountBalance(ctx context.Context) (execution.Balance, error) {
	if b.getAccountBalanceFn != nil {
		return b.getAccountBalanceFn(ctx)
	}

	return execution.Balance{Currency: "USD", Cash: 100000, BuyingPower: 100000, Equity: 100000}, nil
}

// mockRiskEngine implements risk.RiskEngine.
type mockRiskEngine struct {
	isKillSwitchActiveFn   func(ctx context.Context) (bool, error)
	checkPositionLimitsFn  func(ctx context.Context, ticker string, quantity float64, portfolio risk.Portfolio) (bool, string, error)
	checkPreTradeFn        func(ctx context.Context, order *domain.Order, portfolio risk.Portfolio) (bool, string, error)
	getStatusFn            func(ctx context.Context) (risk.EngineStatus, error)
	tripCircuitBreakerFn   func(ctx context.Context, reason string) error
	resetCircuitBreakerFn  func(ctx context.Context) error
	activateKillSwitchFn   func(ctx context.Context, reason string) error
	deactivateKillSwitchFn func(ctx context.Context) error
	updateMetricsFn        func(ctx context.Context, dailyPnL, totalDrawdown float64, consecutiveLosses int) error
}

func (r *mockRiskEngine) IsKillSwitchActive(ctx context.Context) (bool, error) {
	if r.isKillSwitchActiveFn != nil {
		return r.isKillSwitchActiveFn(ctx)
	}

	return false, nil
}

func (r *mockRiskEngine) CheckPositionLimits(ctx context.Context, ticker string, quantity float64, portfolio risk.Portfolio) (bool, string, error) {
	if r.checkPositionLimitsFn != nil {
		return r.checkPositionLimitsFn(ctx, ticker, quantity, portfolio)
	}

	return true, "", nil
}

func (r *mockRiskEngine) CheckPreTrade(ctx context.Context, order *domain.Order, portfolio risk.Portfolio) (bool, string, error) {
	if r.checkPreTradeFn != nil {
		return r.checkPreTradeFn(ctx, order, portfolio)
	}

	return true, "", nil
}

func (r *mockRiskEngine) GetStatus(ctx context.Context) (risk.EngineStatus, error) {
	if r.getStatusFn != nil {
		return r.getStatusFn(ctx)
	}

	return risk.EngineStatus{}, nil
}

func (r *mockRiskEngine) TripCircuitBreaker(ctx context.Context, reason string) error {
	if r.tripCircuitBreakerFn != nil {
		return r.tripCircuitBreakerFn(ctx, reason)
	}

	return nil
}

func (r *mockRiskEngine) ResetCircuitBreaker(ctx context.Context) error {
	if r.resetCircuitBreakerFn != nil {
		return r.resetCircuitBreakerFn(ctx)
	}

	return nil
}

func (r *mockRiskEngine) ActivateKillSwitch(ctx context.Context, reason string) error {
	if r.activateKillSwitchFn != nil {
		return r.activateKillSwitchFn(ctx, reason)
	}

	return nil
}

func (r *mockRiskEngine) DeactivateKillSwitch(ctx context.Context) error {
	if r.deactivateKillSwitchFn != nil {
		return r.deactivateKillSwitchFn(ctx)
	}

	return nil
}

func (r *mockRiskEngine) UpdateMetrics(ctx context.Context, dailyPnL, totalDrawdown float64, consecutiveLosses int) error {
	if r.updateMetricsFn != nil {
		return r.updateMetricsFn(ctx, dailyPnL, totalDrawdown, consecutiveLosses)
	}

	return nil
}

func (r *mockRiskEngine) IsMarketKillSwitchActive(_ context.Context, _ domain.MarketType) (bool, error) {
	return false, nil
}

func (r *mockRiskEngine) ActivateMarketKillSwitch(_ context.Context, _ domain.MarketType, _ string) error {
	return nil
}

func (r *mockRiskEngine) DeactivateMarketKillSwitch(_ context.Context, _ domain.MarketType) error {
	return nil
}

// mockOrderRepo implements repository.OrderRepository.
type mockOrderRepo struct {
	mu      sync.Mutex
	orders  []*domain.Order
	updates []*domain.Order

	createFn        func(ctx context.Context, order *domain.Order) error
	getFn           func(ctx context.Context, id uuid.UUID) (*domain.Order, error)
	listFn          func(ctx context.Context, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error)
	updateFn        func(ctx context.Context, order *domain.Order) error
	deleteFn        func(ctx context.Context, id uuid.UUID) error
	getByStrategyFn func(ctx context.Context, strategyID uuid.UUID, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error)
	getByRunFn      func(ctx context.Context, runID uuid.UUID, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error)
}

func (r *mockOrderRepo) Create(ctx context.Context, order *domain.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.createFn != nil {
		return r.createFn(ctx, order)
	}

	cp := *order
	r.orders = append(r.orders, &cp)

	return nil
}

func (r *mockOrderRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Order, error) {
	if r.getFn != nil {
		return r.getFn(ctx, id)
	}

	return nil, nil
}

func (r *mockOrderRepo) List(ctx context.Context, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error) {
	if r.listFn != nil {
		return r.listFn(ctx, filter, limit, offset)
	}

	return nil, nil
}

func (r *mockOrderRepo) Update(ctx context.Context, order *domain.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.updateFn != nil {
		return r.updateFn(ctx, order)
	}

	cp := *order
	r.updates = append(r.updates, &cp)

	return nil
}

func (r *mockOrderRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if r.deleteFn != nil {
		return r.deleteFn(ctx, id)
	}

	return nil
}

func (r *mockOrderRepo) GetByStrategy(ctx context.Context, strategyID uuid.UUID, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error) {
	if r.getByStrategyFn != nil {
		return r.getByStrategyFn(ctx, strategyID, filter, limit, offset)
	}

	return nil, nil
}

func (r *mockOrderRepo) Count(_ context.Context, _ repository.OrderFilter) (int, error) {
	return 0, nil
}

func (r *mockOrderRepo) GetByRun(ctx context.Context, runID uuid.UUID, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error) {
	if r.getByRunFn != nil {
		return r.getByRunFn(ctx, runID, filter, limit, offset)
	}

	return nil, nil
}

// mockPositionRepo implements repository.PositionRepository.
type mockPositionRepo struct {
	mu        sync.Mutex
	positions []*domain.Position
	updates   []*domain.Position

	createFn        func(ctx context.Context, position *domain.Position) error
	getFn           func(ctx context.Context, id uuid.UUID) (*domain.Position, error)
	listFn          func(ctx context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error)
	updateFn        func(ctx context.Context, position *domain.Position) error
	deleteFn        func(ctx context.Context, id uuid.UUID) error
	getOpenFn       func(ctx context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error)
	getByStrategyFn func(ctx context.Context, strategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error)
}

func (r *mockPositionRepo) Create(ctx context.Context, position *domain.Position) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.createFn != nil {
		return r.createFn(ctx, position)
	}

	cp := *position
	r.positions = append(r.positions, &cp)

	return nil
}

func (r *mockPositionRepo) CreateAlpacaOwned(ctx context.Context, position *domain.Position) error {
	return r.Create(ctx, position)
}

func (r *mockPositionRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Position, error) {
	if r.getFn != nil {
		return r.getFn(ctx, id)
	}

	return nil, nil
}

func (r *mockPositionRepo) List(ctx context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	if r.listFn != nil {
		return r.listFn(ctx, filter, limit, offset)
	}

	return nil, nil
}

func (r *mockPositionRepo) Update(ctx context.Context, position *domain.Position) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.updateFn != nil {
		return r.updateFn(ctx, position)
	}

	cp := *position
	r.updates = append(r.updates, &cp)

	return nil
}

func (r *mockPositionRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if r.deleteFn != nil {
		return r.deleteFn(ctx, id)
	}

	return nil
}

func (r *mockPositionRepo) GetOpen(ctx context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	if r.getOpenFn != nil {
		return r.getOpenFn(ctx, filter, limit, offset)
	}

	return nil, nil
}

func (r *mockPositionRepo) ListOpenAlpacaOwned(_ context.Context, _ int, _ int) ([]domain.Position, error) {
	return nil, nil
}

func (r *mockPositionRepo) Count(_ context.Context, _ repository.PositionFilter) (int, error) {
	return 0, nil
}

func (r *mockPositionRepo) CountOpen(_ context.Context, _ repository.PositionFilter) (int, error) {
	return 0, nil
}

func (r *mockPositionRepo) GetByStrategy(ctx context.Context, strategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	if r.getByStrategyFn != nil {
		return r.getByStrategyFn(ctx, strategyID, filter, limit, offset)
	}

	return nil, nil
}

// mockTradeRepo implements repository.TradeRepository.
type mockTradeRepo struct {
	mu     sync.Mutex
	trades []*domain.Trade

	createFn        func(ctx context.Context, trade *domain.Trade) error
	listFn          func(ctx context.Context, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error)
	getByOrderFn    func(ctx context.Context, orderID uuid.UUID, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error)
	getByPositionFn func(ctx context.Context, positionID uuid.UUID, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error)
}

func (r *mockTradeRepo) Create(ctx context.Context, trade *domain.Trade) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.createFn != nil {
		return r.createFn(ctx, trade)
	}

	cp := *trade
	r.trades = append(r.trades, &cp)

	return nil
}

func (r *mockTradeRepo) List(ctx context.Context, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error) {
	if r.listFn != nil {
		return r.listFn(ctx, filter, limit, offset)
	}

	return nil, nil
}

func (r *mockTradeRepo) Count(_ context.Context, _ repository.TradeFilter) (int, error) {
	return 0, nil
}

func (r *mockTradeRepo) GetByOrder(ctx context.Context, orderID uuid.UUID, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error) {
	if r.getByOrderFn != nil {
		return r.getByOrderFn(ctx, orderID, filter, limit, offset)
	}

	return nil, nil
}

func (r *mockTradeRepo) GetByPosition(ctx context.Context, positionID uuid.UUID, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error) {
	if r.getByPositionFn != nil {
		return r.getByPositionFn(ctx, positionID, filter, limit, offset)
	}

	return nil, nil
}

// mockAuditLogRepo implements repository.AuditLogRepository.
type mockAuditLogRepo struct {
	mu      sync.Mutex
	entries []*domain.AuditLogEntry

	createFn func(ctx context.Context, entry *domain.AuditLogEntry) error
	queryFn  func(ctx context.Context, filter repository.AuditLogFilter, limit, offset int) ([]domain.AuditLogEntry, error)
}

func (r *mockAuditLogRepo) Create(ctx context.Context, entry *domain.AuditLogEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.createFn != nil {
		return r.createFn(ctx, entry)
	}

	cp := *entry
	r.entries = append(r.entries, &cp)

	return nil
}

func (r *mockAuditLogRepo) Count(_ context.Context, _ repository.AuditLogFilter) (int, error) {
	return 0, nil
}

func (r *mockAuditLogRepo) Query(ctx context.Context, filter repository.AuditLogFilter, limit, offset int) ([]domain.AuditLogEntry, error) {
	if r.queryFn != nil {
		return r.queryFn(ctx, filter, limit, offset)
	}

	return nil, nil
}

// mockAgentEventRepo implements repository.AgentEventRepository.
type mockAgentEventRepo struct {
	mu     sync.Mutex
	events []*domain.AgentEvent

	createFn func(ctx context.Context, event *domain.AgentEvent) error
}

type mockMetricsRecorder struct {
	records []struct{ broker, side, status string }
}

func (m *mockMetricsRecorder) RecordOrder(broker, side, status string) {
	m.records = append(m.records, struct{ broker, side, status string }{broker: broker, side: side, status: status})
}

type mockDecisionRecorder struct {
	mu          sync.Mutex
	decisions   []*domain.TradeDecision
	paperAttach []struct{ decisionID, orderID uuid.UUID }
	liveAttach  []struct{ decisionID, orderID uuid.UUID }
}

func (r *mockDecisionRecorder) RecordDecision(_ context.Context, decision *domain.TradeDecision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if decision == nil {
		return nil
	}
	cp := *decision
	r.decisions = append(r.decisions, &cp)
	return nil
}

func (r *mockDecisionRecorder) AttachPaperOrder(_ context.Context, decisionID, orderID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paperAttach = append(r.paperAttach, struct{ decisionID, orderID uuid.UUID }{decisionID: decisionID, orderID: orderID})
	return nil
}

func (r *mockDecisionRecorder) AttachLiveOrder(_ context.Context, decisionID, orderID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.liveAttach = append(r.liveAttach, struct{ decisionID, orderID uuid.UUID }{decisionID: decisionID, orderID: orderID})
	return nil
}

type mockOrderMetrics struct{ records []string }

func (m *mockOrderMetrics) RecordOrder(broker, side, status string) {
	m.records = append(m.records, broker+":"+side+":"+status)
}

func (r *mockAgentEventRepo) Create(ctx context.Context, event *domain.AgentEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.createFn != nil {
		return r.createFn(ctx, event)
	}

	cp := *event
	r.events = append(r.events, &cp)

	return nil
}

func (r *mockAgentEventRepo) List(_ context.Context, _ repository.AgentEventFilter, _, _ int) ([]domain.AgentEvent, error) {
	return nil, nil
}

func (r *mockAgentEventRepo) Count(_ context.Context, _ repository.AgentEventFilter) (int, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestOrderManager(
	broker *mockBroker,
	riskEng *mockRiskEngine,
	orderRepo *mockOrderRepo,
	positionRepo *mockPositionRepo,
	tradeRepo *mockTradeRepo,
	auditRepo *mockAuditLogRepo,
) *execution.OrderManager {
	cfg := execution.SizingConfig{
		Method:      execution.PositionSizingMethodFixedFractional,
		FractionPct: 0.02,
	}

	return execution.NewOrderManager(
		broker,
		"paper",
		riskEng,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditRepo,
		nil, // agentEventRepo
		cfg,
		slog.Default(),
	)
}

func defaultSignal() execution.FinalSignal {
	return execution.FinalSignal{
		Signal:     domain.PipelineSignalBuy,
		Confidence: 0.85,
	}
}

func defaultPlan() execution.TradingPlan {
	return execution.TradingPlan{
		Action:     domain.PipelineSignalBuy,
		MarketType: domain.MarketTypeStock,
		Ticker:     "AAPL",
		EntryType:  "market",
		EntryPrice: 150.0,
		StopLoss:   145.0,
		TakeProfit: 160.0,
		Confidence: 0.85,
		Rationale:  "test rationale",
		RiskReward: 2.0,
	}
}

func floatPtr(value float64) *float64 { return &value }

type fakeFinancialLifecycleRepo struct {
	called int
	input  repository.OrderFillInput
	result repository.OrderFillResult
	err    error
}

func (f *fakeFinancialLifecycleRepo) ApplyOrderFill(_ context.Context, input repository.OrderFillInput) (repository.OrderFillResult, error) {
	f.called++
	f.input = input
	return f.result, f.err
}

func (f *fakeFinancialLifecycleRepo) SettlePredictionDecision(context.Context, repository.PredictionDecisionSettlementInput) (repository.PredictionDecisionSettlementResult, error) {
	return repository.PredictionDecisionSettlementResult{}, nil
}

func TestOrderManagerHandleFillUsesFinancialLifecycleRepository(t *testing.T) {
	strategyID, runID, decisionID := uuid.New(), uuid.New(), uuid.New()
	orderID := uuid.New()
	order := &domain.Order{ID: orderID, StrategyID: &strategyID, Ticker: "AAPL", MarketType: domain.MarketTypeStock, Side: domain.OrderSideBuy, Status: domain.OrderStatusSubmitted, Quantity: 10}
	plan := defaultPlan()
	plan.EntryPrice = 101
	tradeRepo := &mockTradeRepo{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	auditRepo := &mockAuditLogRepo{}
	metrics := &mockMetricsRecorder{}
	financialRepo := &fakeFinancialLifecycleRepo{result: repository.OrderFillResult{OrderID: orderID, TradeID: uuid.New(), Replayed: false, PositionID: nil}}
	mgr := newTestOrderManager(&mockBroker{}, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, auditRepo).WithFinancialLifecycleRepo(financialRepo).WithMetrics(metrics)
	if err := mgr.HandleFillForTest(context.Background(), order, plan, strategyID, runID, decisionID); err != nil {
		t.Fatalf("HandleFillForTest() error = %v", err)
	}
	if financialRepo.called != 1 {
		t.Fatalf("financial repo called %d times, want 1", financialRepo.called)
	}
	if len(orderRepo.updates) != 0 || len(positionRepo.positions) != 0 || len(tradeRepo.trades) != 0 {
		t.Fatalf("legacy persistence should be bypassed: orders=%d positions=%d trades=%d", len(orderRepo.updates), len(positionRepo.positions), len(tradeRepo.trades))
	}
	if len(auditRepo.entries) != 1 || len(metrics.records) != 1 {
		t.Fatalf("expected side effects on non-replay result, got audit=%d metrics=%d", len(auditRepo.entries), len(metrics.records))
	}
	if len(orderRepo.updates) != 0 {
		t.Fatalf("expected no order repo updates")
	}
	replayRepo := &fakeFinancialLifecycleRepo{result: repository.OrderFillResult{OrderID: orderID, TradeID: uuid.New(), Replayed: true}}
	order2 := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "MSFT", MarketType: domain.MarketTypeStock, Side: domain.OrderSideBuy, Status: domain.OrderStatusSubmitted, Quantity: 5}
	auditRepo2 := &mockAuditLogRepo{}
	tradeRepo2 := &mockTradeRepo{}
	positionRepo2 := &mockPositionRepo{}
	metrics2 := &mockMetricsRecorder{}
	mgr2 := newTestOrderManager(&mockBroker{}, &mockRiskEngine{}, &mockOrderRepo{}, positionRepo2, tradeRepo2, auditRepo2).WithFinancialLifecycleRepo(replayRepo).WithMetrics(metrics2)
	if err := mgr2.HandleFillForTest(context.Background(), order2, plan, strategyID, runID, decisionID); err != nil {
		t.Fatalf("HandleFillForTest replay() error = %v", err)
	}
	if len(auditRepo2.entries) != 0 || len(tradeRepo2.trades) != 0 || len(positionRepo2.positions) != 0 || len(metrics2.records) != 0 {
		t.Fatalf("replayed fill should suppress side effects: audit=%d trades=%d positions=%d metrics=%d", len(auditRepo2.entries), len(tradeRepo2.trades), len(positionRepo2.positions), len(metrics2.records))
	}

	failingRepo := &fakeFinancialLifecycleRepo{err: errors.New("boom")}
	order3 := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "GOOG", MarketType: domain.MarketTypeStock, Side: domain.OrderSideBuy, Status: domain.OrderStatusSubmitted, Quantity: 2}
	orderRepo3 := &mockOrderRepo{}
	tradeRepo3 := &mockTradeRepo{}
	positionRepo3 := &mockPositionRepo{}
	auditRepo3 := &mockAuditLogRepo{}
	mgr3 := newTestOrderManager(&mockBroker{}, &mockRiskEngine{}, orderRepo3, positionRepo3, tradeRepo3, auditRepo3).WithFinancialLifecycleRepo(failingRepo)
	if err := mgr3.HandleFillForTest(context.Background(), order3, plan, strategyID, runID, decisionID); err == nil {
		t.Fatal("expected financial repo error")
	}
	if len(orderRepo3.updates) != 0 || len(positionRepo3.positions) != 0 || len(tradeRepo3.trades) != 0 || len(auditRepo3.entries) != 0 {
		t.Fatalf("failed financial fill should not persist legacy state: orders=%d positions=%d trades=%d audit=%d", len(orderRepo3.updates), len(positionRepo3.positions), len(tradeRepo3.trades), len(auditRepo3.entries))
	}
}

func TestOrderManager_CarriesKalshiReferencePriceToBroker(t *testing.T) {
	strategyID := uuid.New()
	runID := uuid.New()
	reference := 0.04
	entry := 0.04
	filledAt := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	var submitted *domain.Order
	broker := &mockBroker{
		submitOrderFn: func(ctx context.Context, order *domain.Order) (string, error) {
			submitted = order
			if order.ReferencePrice == nil || *order.ReferencePrice != reference {
				t.Fatalf("broker ReferencePrice = %v, want %v", order.ReferencePrice, reference)
			}
			if order.LimitPrice == nil || *order.LimitPrice != entry {
				t.Fatalf("broker LimitPrice = %v, want %v", order.LimitPrice, entry)
			}
			order.Status = domain.OrderStatusFilled
			order.FilledQuantity = order.Quantity
			order.FilledAvgPrice = floatPtr(0.039)
			order.FilledAt = &filledAt
			return "paper-123", nil
		},
		getOrderStatusFn: func(context.Context, string) (domain.OrderStatus, error) {
			return domain.OrderStatusFilled, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	mgr := newTestOrderManager(broker, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, &mockAuditLogRepo{})

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeKalshi
	plan.EntryType = "limit"
	plan.EntryPrice = entry
	plan.ReferencePrice = reference
	plan.Ticker = "KXTEST"
	plan.Side = "YES"

	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, strategyID, runID); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if submitted == nil {
		t.Fatal("expected broker submission")
	}
	if len(orderRepo.orders) != 1 || orderRepo.orders[0].ReferencePrice == nil || *orderRepo.orders[0].ReferencePrice != reference {
		t.Fatalf("persisted order reference = %+v, want %v", orderRepo.orders, reference)
	}
}

func TestOrderManager_PersistsExactPaperFillPriceAcrossRepos(t *testing.T) {
	strategyID := uuid.New()
	runID := uuid.New()
	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeStock
	plan.EntryType = "market"
	plan.EntryPrice = 99.5
	plan.StopLoss = 100
	plan.Ticker = "AAPL"

	broker := paperbroker.NewPaperBroker(100000, 150, 0)
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	var createdOrder *domain.Order
	var persistedOrder *domain.Order
	var persistedTrade *domain.Trade
	var persistedPosition *domain.Position

	orderRepo.createFn = func(_ context.Context, order *domain.Order) error {
		createdOrder = order
		return nil
	}
	orderRepo.updateFn = func(_ context.Context, order *domain.Order) error {
		persistedOrder = order
		return nil
	}
	tradeRepo.createFn = func(_ context.Context, trade *domain.Trade) error {
		persistedTrade = trade
		return nil
	}
	positionRepo.createFn = func(_ context.Context, position *domain.Position) error {
		persistedPosition = position
		return nil
	}

	mgr := execution.NewOrderManager(broker, "paper", &mockRiskEngine{}, positionRepo, orderRepo, tradeRepo, &mockAuditLogRepo{}, nil, execution.SizingConfig{Method: execution.PositionSizingMethodFixedFractional, FractionPct: 0.02}, slog.Default())
	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, strategyID, runID); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if createdOrder == nil || persistedOrder == nil || persistedTrade == nil || persistedPosition == nil {
		t.Fatalf("missing persisted entities: order=%v trade=%v position=%v", createdOrder, persistedTrade, persistedPosition)
	}
	if createdOrder.FilledAvgPrice == nil || persistedOrder.FilledAvgPrice == nil {
		t.Fatal("FilledAvgPrice = nil, want filled order price to be persisted")
	}
	if *persistedOrder.FilledAvgPrice == plan.EntryPrice || persistedTrade.Price == plan.EntryPrice || persistedPosition.AvgEntry == plan.EntryPrice {
		t.Fatalf("persisted fill price unexpectedly matched plan entry price: order=%v trade=%v position=%v entry=%v", *persistedOrder.FilledAvgPrice, persistedTrade.Price, persistedPosition.AvgEntry, plan.EntryPrice)
	}
	if *persistedOrder.FilledAvgPrice != persistedTrade.Price || persistedTrade.Price != persistedPosition.AvgEntry {
		t.Fatalf("persisted fill price mismatch: order=%v trade=%v position=%v", *persistedOrder.FilledAvgPrice, persistedTrade.Price, persistedPosition.AvgEntry)
	}
	if *persistedOrder.FilledAvgPrice <= plan.EntryPrice {
		t.Fatalf("filled avg price = %v, want slippage-adjusted fill above plan entry %v", *persistedOrder.FilledAvgPrice, plan.EntryPrice)
	}
}

// auditEventTypes extracts the event types from the audit log entries.
func auditEventTypes(entries []*domain.AuditLogEntry) []string {
	var types []string
	for _, e := range entries {
		types = append(types, e.EventType)
	}

	return types
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestProcessSignal_HappyPath(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeCrypto
	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		plan,
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	// Verify an order was created.
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}

	order := orderRepo.orders[0]
	if order.Status != domain.OrderStatusPending {
		t.Errorf("expected order status pending, got %s", order.Status)
	}
	if order.Ticker != "AAPL" {
		t.Errorf("expected ticker AAPL, got %s", order.Ticker)
	}
	if order.Side != domain.OrderSideBuy {
		t.Errorf("expected side buy, got %s", order.Side)
	}
	if order.Broker != "paper" {
		t.Errorf("expected broker 'paper', got %q", order.Broker)
	}

	// Verify order was updated (submitted, then filled).
	if len(orderRepo.updates) < 1 {
		t.Fatalf("expected at least 1 order update, got %d", len(orderRepo.updates))
	}

	// The final update should have filled status.
	lastUpdate := orderRepo.updates[len(orderRepo.updates)-1]
	if lastUpdate.Status != domain.OrderStatusFilled {
		t.Errorf("expected last order update status filled, got %s", lastUpdate.Status)
	}

	// Verify a trade was created.
	if len(tradeRepo.trades) != 1 {
		t.Fatalf("expected 1 trade created, got %d", len(tradeRepo.trades))
	}

	trade := tradeRepo.trades[0]
	if trade.Ticker != "AAPL" {
		t.Errorf("expected trade ticker AAPL, got %s", trade.Ticker)
	}
	if trade.Side != domain.OrderSideBuy {
		t.Errorf("expected trade side buy, got %s", trade.Side)
	}

	// Verify a position was created.
	if len(positionRepo.positions) != 1 {
		t.Fatalf("expected 1 position created, got %d", len(positionRepo.positions))
	}

	position := positionRepo.positions[0]
	if position.Ticker != "AAPL" {
		t.Errorf("expected position ticker AAPL, got %s", position.Ticker)
	}
	if position.Side != domain.PositionSideLong {
		t.Errorf("expected position side long, got %s", position.Side)
	}

	// Verify trade references the position.
	if trade.PositionID == nil {
		t.Fatal("expected trade to reference position")
	}
	if *trade.PositionID != position.ID {
		t.Errorf("expected trade position_id %s, got %s", position.ID, *trade.PositionID)
	}
}

func TestProcessSignal_PolymarketPositionTickerIncludesPredictionSide(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	mgr := newTestOrderManager(
		&mockBroker{},
		&mockRiskEngine{},
		orderRepo,
		positionRepo,
		tradeRepo,
		&mockAuditLogRepo{},
	)

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.EntryPrice = 0.43
	plan.Side = "NO"

	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if len(positionRepo.positions) != 1 {
		t.Fatalf("expected 1 position created, got %d", len(positionRepo.positions))
	}
	if got := positionRepo.positions[0].Ticker; got != "will-example-happen:NO" {
		t.Fatalf("position ticker = %q, want side-qualified ticker", got)
	}
	if len(orderRepo.orders) != 1 || orderRepo.orders[0].Ticker != "will-example-happen" || orderRepo.orders[0].PredictionSide != "NO" {
		t.Fatalf("unexpected order identity: %+v", orderRepo.orders)
	}
}

func TestProcessSignal_PredictionSuffixInferencePersistsBaseTickerAndSide(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	mgr := newTestOrderManager(&mockBroker{}, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, &mockAuditLogRepo{})

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeKalshi
	plan.Ticker = "will-example-happen:yes"
	plan.Side = ""
	plan.EntryPrice = 0.43

	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}
	if got := orderRepo.orders[0].Ticker; got != "WILL-EXAMPLE-HAPPEN" {
		t.Fatalf("order ticker = %q, want base ticker", got)
	}
	if got := orderRepo.orders[0].PredictionSide; got != "YES" {
		t.Fatalf("order prediction side = %q, want YES", got)
	}
	if len(positionRepo.positions) != 1 || positionRepo.positions[0].Ticker != "WILL-EXAMPLE-HAPPEN:YES" {
		t.Fatalf("positions = %+v, want inferred side-qualified position", positionRepo.positions)
	}
}

func TestProcessSignal_PredictionMixedCaseSuffixBuyAndSellReuseNormalizedIdentity(t *testing.T) {
	strategyID := uuid.New()
	positionID := uuid.New()
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	orderRepo := &mockOrderRepo{}
	mgr := newTestOrderManager(&mockBroker{}, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, auditRepo)

	buyPlan := defaultPlan()
	buyPlan.MarketType = domain.MarketTypePolymarket
	buyPlan.Ticker = " market : yes "
	buyPlan.Side = ""
	buyPlan.EntryPrice = 0.50

	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), buyPlan, strategyID, uuid.New()); err != nil {
		t.Fatalf("buy ProcessSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 buy order, got %d", len(orderRepo.orders))
	}
	if got := orderRepo.orders[0].Ticker; got != "MARKET" {
		t.Fatalf("buy order ticker = %q, want MARKET", got)
	}
	if got := orderRepo.orders[0].PredictionSide; got != "YES" {
		t.Fatalf("buy order prediction side = %q, want YES", got)
	}

	positionRepo.getByStrategyFn = func(ctx context.Context, gotStrategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
		if gotStrategyID != strategyID {
			t.Fatalf("strategyID = %s, want %s", gotStrategyID, strategyID)
		}
		if filter.Ticker != "MARKET:YES" || filter.Side != domain.PositionSideLong {
			t.Fatalf("filter = %+v, want MARKET:YES long", filter)
		}
		return []domain.Position{{ID: positionID, StrategyID: &strategyID, Ticker: "MARKET:YES", Side: domain.PositionSideLong, Quantity: 3, AvgEntry: 0.50, OpenedAt: time.Now()}}, nil
	}

	sellPlan := defaultPlan()
	sellPlan.MarketType = domain.MarketTypePolymarket
	sellPlan.Ticker = " market : yes "
	sellPlan.Side = ""
	sellPlan.EntryPrice = 0.60
	sellPlan.Action = domain.PipelineSignalSell

	if err := mgr.ProcessSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9}, sellPlan, strategyID, uuid.New()); err != nil {
		t.Fatalf("sell ProcessSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 2 {
		t.Fatalf("expected sell order to be created after owned-position lookup, got %d orders", len(orderRepo.orders))
	}
	if got := orderRepo.orders[1].Ticker; got != "MARKET" || orderRepo.orders[1].PredictionSide != "YES" {
		t.Fatalf("sell order identity = %q:%q, want MARKET:YES", got, orderRepo.orders[1].PredictionSide)
	}
}

func TestProcessSignal_PredictionNoSuffixRejectedBeforeCreateOrSubmit(t *testing.T) {
	var submitCalls int
	broker := &mockBroker{submitOrderFn: func(context.Context, *domain.Order) (string, error) { submitCalls++; return "", nil }}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	mgr := newTestOrderManager(broker, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, &mockAuditLogRepo{})

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.Side = ""

	err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error")
	}
	if len(orderRepo.orders) != 0 || submitCalls != 0 {
		t.Fatalf("expected no order creation or submission, got orders=%d submitCalls=%d", len(orderRepo.orders), submitCalls)
	}
}

func TestProcessSignal_PredictionInvalidIdentityRejectedBeforeOwnershipQuery(t *testing.T) {
	var ownershipCalls int
	positionRepo := &mockPositionRepo{getByStrategyFn: func(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
		ownershipCalls++
		t.Fatal("ownership lookup should not be called for invalid prediction identity")
		return nil, nil
	}}
	orderRepo := &mockOrderRepo{}
	tradeRepo := &mockTradeRepo{}
	mgr := newTestOrderManager(&mockBroker{}, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, &mockAuditLogRepo{})

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.Side = ""
	plan.Action = domain.PipelineSignalSell

	err := mgr.ProcessSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9}, plan, uuid.New(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "requires valid side YES or NO") {
		t.Fatalf("unexpected error: %v", err)
	}
	if ownershipCalls != 0 || len(orderRepo.orders) != 0 {
		t.Fatalf("expected no ownership lookup/order write, got ownershipCalls=%d orders=%d", ownershipCalls, len(orderRepo.orders))
	}
}

func TestProcessSignal_PredictionQualifiedTickerNormalizesBeforePersistence(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	var submitCalls int
	broker := &mockBroker{
		submitOrderFn: func(ctx context.Context, order *domain.Order) (string, error) {
			submitCalls++
			if order.Ticker != "WILL-EXAMPLE-HAPPEN" {
				t.Fatalf("submitted ticker = %q, want base ticker", order.Ticker)
			}
			if order.PredictionSide != "YES" {
				t.Fatalf("submitted prediction side = %q, want YES", order.PredictionSide)
			}
			return "ext-123", nil
		},
	}
	mgr := newTestOrderManager(broker, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, &mockAuditLogRepo{})

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeKalshi
	plan.Ticker = "will-example-happen:yes"
	plan.Side = "YES"
	plan.EntryPrice = 0.43

	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if submitCalls != 1 {
		t.Fatalf("submit calls = %d, want 1", submitCalls)
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}
	if got := orderRepo.orders[0].Ticker; got != "WILL-EXAMPLE-HAPPEN" {
		t.Fatalf("order ticker = %q, want base ticker", got)
	}
	if got := positionRepo.positions[0].Ticker; got != "WILL-EXAMPLE-HAPPEN:YES" {
		t.Fatalf("position ticker = %q, want base-qualified position ticker", got)
	}
}

func TestProcessSignal_PredictionConflictingTickerSuffixRejectedBeforeCreate(t *testing.T) {
	var submitCalls int
	broker := &mockBroker{
		submitOrderFn: func(context.Context, *domain.Order) (string, error) {
			submitCalls++
			return "", nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	mgr := newTestOrderManager(broker, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, &mockAuditLogRepo{})

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen:NO"
	plan.Side = "YES"

	err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "conflicts with prediction side") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 0 {
		t.Fatalf("expected zero orders created, got %d", len(orderRepo.orders))
	}
	if submitCalls != 0 {
		t.Fatalf("submit calls = %d, want 0", submitCalls)
	}
}

func TestProcessSignal_PolymarketBuyUsesUSDCCapForQuantity(t *testing.T) {
	broker := &mockBroker{
		getAccountBalanceFn: func(context.Context) (execution.Balance, error) {
			return execution.Balance{Currency: "USD", Cash: 100000, BuyingPower: 100000, Equity: 100000}, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.EntryPrice = 0.25
	plan.Side = "YES"

	mgr := execution.NewOrderManager(
		broker,
		"polymarket",
		&mockRiskEngine{},
		positionRepo,
		orderRepo,
		tradeRepo,
		&mockAuditLogRepo{},
		nil,
		execution.SizingConfig{Method: execution.PositionSizingMethodFixedFractional, FractionPct: 0.02, MaxPositionUSDC: 500},
		slog.Default(),
	)

	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}
	if got, want := orderRepo.orders[0].Quantity, 2000.0; math.Abs(got-want) > 1e-9 {
		t.Fatalf("order quantity = %v, want %v", got, want)
	}
}

func TestProcessSignal_PolymarketExitClosesSideQualifiedPosition(t *testing.T) {
	strategyID := uuid.New()
	positionID := uuid.New()
	broker := &mockBroker{
		getAccountBalanceFn: func(context.Context) (execution.Balance, error) {
			return execution.Balance{Currency: "USD", Cash: 250, BuyingPower: 250, Equity: 250}, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{
		getByStrategyFn: func(ctx context.Context, gotStrategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
			if gotStrategyID != strategyID {
				t.Fatalf("strategyID = %s, want %s", gotStrategyID, strategyID)
			}
			if filter.Ticker != "will-example-happen:YES" || filter.Side != domain.PositionSideLong {
				t.Fatalf("filter = %+v, want will-example-happen:YES long", filter)
			}
			return []domain.Position{{
				ID:         positionID,
				StrategyID: &strategyID,
				Ticker:     "will-example-happen:YES",
				Side:       domain.PositionSideLong,
				Quantity:   10,
				AvgEntry:   0.30,
				OpenedAt:   time.Now(),
			}}, nil
		},
	}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, auditRepo)

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.Side = "YES"
	plan.EntryPrice = 0.50
	plan.Action = domain.PipelineSignalSell

	if err := mgr.ProcessSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9}, plan, strategyID, uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}
	if orderRepo.orders[0].Side != domain.OrderSideSell || orderRepo.orders[0].PredictionSide != "YES" {
		t.Fatalf("unexpected order identity: %+v", orderRepo.orders[0])
	}
	if len(positionRepo.positions) != 0 {
		t.Fatalf("expected no new position creation, got %d", len(positionRepo.positions))
	}
	if len(positionRepo.updates) != 1 {
		t.Fatalf("expected 1 position update, got %d", len(positionRepo.updates))
	}
	updated := positionRepo.updates[0]
	if updated.Ticker != "will-example-happen:YES" || updated.Quantity != 0 {
		t.Fatalf("updated position = %+v, want closed YES position", updated)
	}
	if updated.ClosedAt == nil {
		t.Fatal("expected ClosedAt to be set")
	}
	if math.Abs(updated.RealizedPnL-2.0) > 1e-9 {
		t.Fatalf("RealizedPnL = %v, want 2.0", updated.RealizedPnL)
	}
	if len(tradeRepo.trades) != 1 || tradeRepo.trades[0].PositionID == nil || *tradeRepo.trades[0].PositionID != positionID {
		t.Fatalf("unexpected trade position linkage: %+v", tradeRepo.trades)
	}
}

func TestProcessSignal_PolymarketExitQuantityCappedToOwnedPosition(t *testing.T) {
	strategyID := uuid.New()
	positionID := uuid.New()
	broker := &mockBroker{
		getAccountBalanceFn: func(context.Context) (execution.Balance, error) {
			return execution.Balance{Currency: "USD", Cash: 100000, BuyingPower: 100000, Equity: 100000}, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{
		getByStrategyFn: func(ctx context.Context, gotStrategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
			if gotStrategyID != strategyID {
				t.Fatalf("strategyID = %s, want %s", gotStrategyID, strategyID)
			}
			if filter.Ticker != "will-example-happen:YES" || filter.Side != domain.PositionSideLong {
				t.Fatalf("filter = %+v, want will-example-happen:YES long", filter)
			}
			return []domain.Position{{
				ID:         positionID,
				StrategyID: &strategyID,
				Ticker:     "will-example-happen:YES",
				Side:       domain.PositionSideLong,
				Quantity:   3,
				AvgEntry:   0.30,
				OpenedAt:   time.Now(),
			}}, nil
		},
	}
	tradeRepo := &mockTradeRepo{}
	mgr := newTestOrderManager(broker, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, &mockAuditLogRepo{})

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.Side = "YES"
	plan.EntryPrice = 0.50
	plan.Action = domain.PipelineSignalSell

	if err := mgr.ProcessSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9}, plan, strategyID, uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}
	if got := orderRepo.orders[0].Quantity; got != 3 {
		t.Fatalf("exit order quantity = %v, want owned quantity cap 3", got)
	}
	if len(positionRepo.updates) != 1 || positionRepo.updates[0].Quantity != 0 || positionRepo.updates[0].ClosedAt == nil {
		t.Fatalf("expected position closed without oversized sell: %+v", positionRepo.updates)
	}
}

func TestProcessSignal_PolymarketPartialCloseReducesQuantity(t *testing.T) {
	strategyID := uuid.New()
	positionID := uuid.New()
	broker := &mockBroker{
		getAccountBalanceFn: func(context.Context) (execution.Balance, error) {
			return execution.Balance{Currency: "USD", Cash: 250, BuyingPower: 250, Equity: 250}, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{
		getByStrategyFn: func(ctx context.Context, gotStrategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
			if gotStrategyID != strategyID {
				t.Fatalf("strategyID = %s, want %s", gotStrategyID, strategyID)
			}
			if filter.Ticker != "will-example-happen:YES" || filter.Side != domain.PositionSideLong {
				t.Fatalf("filter = %+v, want will-example-happen:YES long", filter)
			}
			return []domain.Position{{
				ID:         positionID,
				StrategyID: &strategyID,
				Ticker:     "will-example-happen:YES",
				Side:       domain.PositionSideLong,
				Quantity:   15,
				AvgEntry:   0.30,
				OpenedAt:   time.Now(),
			}}, nil
		},
	}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, auditRepo)

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.Side = "YES"
	plan.EntryPrice = 0.50
	plan.Action = domain.PipelineSignalSell

	if err := mgr.ProcessSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9}, plan, strategyID, uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(positionRepo.updates) != 1 {
		t.Fatalf("expected 1 position update, got %d", len(positionRepo.updates))
	}
	updated := positionRepo.updates[0]
	if updated.Quantity != 5 {
		t.Fatalf("updated quantity = %v, want 5", updated.Quantity)
	}
	if updated.ClosedAt != nil {
		t.Fatal("expected partial close to keep position open")
	}
	if math.Abs(updated.RealizedPnL-2.0) > 1e-9 {
		t.Fatalf("RealizedPnL = %v, want 2.0", updated.RealizedPnL)
	}
	if len(tradeRepo.trades) != 1 || tradeRepo.trades[0].PositionID == nil || *tradeRepo.trades[0].PositionID != positionID {
		t.Fatalf("unexpected trade position linkage: %+v", tradeRepo.trades)
	}
}

func TestProcessSignal_KalshiExitUsesOwnedQuantityAndClosesPosition(t *testing.T) {
	strategyID := uuid.New()
	positionID := uuid.New()
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{
		getByStrategyFn: func(_ context.Context, gotStrategyID uuid.UUID, filter repository.PositionFilter, _, _ int) ([]domain.Position, error) {
			if gotStrategyID != strategyID || filter.Ticker != "KX-TEST:YES" || filter.Side != domain.PositionSideLong {
				t.Fatalf("unexpected ownership query: strategy=%s filter=%+v", gotStrategyID, filter)
			}
			return []domain.Position{{ID: positionID, StrategyID: &strategyID, MarketType: domain.MarketTypeKalshi, Ticker: "KX-TEST:YES", Side: domain.PositionSideLong, Quantity: 100, AvgEntry: 0.40, OpenedAt: time.Now()}}, nil
		},
	}
	tradeRepo := &mockTradeRepo{}
	mgr := newTestOrderManager(&mockBroker{}, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, &mockAuditLogRepo{})
	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeKalshi
	plan.Ticker = "KX-TEST"
	plan.Side = "YES"
	plan.EntryPrice = 0.50
	plan.PositionSize = 100
	plan.Action = domain.PipelineSignalSell

	if err := mgr.ProcessSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9}, plan, strategyID, uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 1 || orderRepo.orders[0].Side != domain.OrderSideSell || orderRepo.orders[0].Quantity != 100 {
		t.Fatalf("unexpected Kalshi exit order: %+v", orderRepo.orders)
	}
	if len(positionRepo.updates) != 1 || positionRepo.updates[0].Quantity != 0 || positionRepo.updates[0].ClosedAt == nil {
		t.Fatalf("expected Kalshi position to close: %+v", positionRepo.updates)
	}
	if math.Abs(positionRepo.updates[0].RealizedPnL-10) > 1e-9 {
		t.Fatalf("realized P/L = %v, want 10", positionRepo.updates[0].RealizedPnL)
	}
}

func TestProcessSignal_PolymarketSellWithoutMatchingPositionSkipped(t *testing.T) {
	broker := &mockBroker{
		submitOrderFn: func(context.Context, *domain.Order) (string, error) {
			t.Fatal("SubmitOrder should not be called for unowned polymarket exit")
			return "", nil
		},
	}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}
	mgr := newTestOrderManager(broker, &mockRiskEngine{}, &mockOrderRepo{}, positionRepo, tradeRepo, auditRepo).WithDecisionRecorder(recorder)

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.Side = "YES"
	plan.EntryPrice = 0.50
	plan.Action = domain.PipelineSignalSell

	if err := mgr.ProcessSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9}, plan, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(recorder.decisions) != 1 {
		t.Fatalf("expected 1 recorded decision, got %d", len(recorder.decisions))
	}
	if recorder.decisions[0].Status != domain.TradeDecisionStatusRejected || !slices.Contains(recorder.decisions[0].RiskReasons, "unowned_polymarket_exit_no_open_position") {
		t.Fatalf("unexpected decision: %+v", recorder.decisions[0])
	}
	if len(positionRepo.positions) != 0 || len(positionRepo.updates) != 0 || len(tradeRepo.trades) != 0 {
		t.Fatalf("expected no order lifecycle changes, got positions=%d updates=%d trades=%d", len(positionRepo.positions), len(positionRepo.updates), len(tradeRepo.trades))
	}
}

func TestProcessSignal_PolymarketExitClosesNoPosition(t *testing.T) {
	strategyID := uuid.New()
	positionID := uuid.New()
	broker := &mockBroker{
		getAccountBalanceFn: func(context.Context) (execution.Balance, error) {
			return execution.Balance{Currency: "USD", Cash: 250, BuyingPower: 250, Equity: 250}, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{
		getByStrategyFn: func(ctx context.Context, gotStrategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
			if gotStrategyID != strategyID {
				t.Fatalf("strategyID = %s, want %s", gotStrategyID, strategyID)
			}
			if filter.Ticker != "will-example-happen:NO" || filter.Side != domain.PositionSideLong {
				t.Fatalf("filter = %+v, want will-example-happen:NO long", filter)
			}
			return []domain.Position{{
				ID:         positionID,
				StrategyID: &strategyID,
				Ticker:     "will-example-happen:NO",
				Side:       domain.PositionSideLong,
				Quantity:   10,
				AvgEntry:   0.40,
				OpenedAt:   time.Now(),
			}}, nil
		},
	}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, &mockRiskEngine{}, orderRepo, positionRepo, tradeRepo, auditRepo)

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypePolymarket
	plan.Ticker = "will-example-happen"
	plan.Side = "NO"
	plan.EntryPrice = 0.50
	plan.Action = domain.PipelineSignalSell

	if err := mgr.ProcessSignal(context.Background(), execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9}, plan, strategyID, uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(positionRepo.updates) != 1 {
		t.Fatalf("expected 1 position update, got %d", len(positionRepo.updates))
	}
	updated := positionRepo.updates[0]
	if updated.Ticker != "will-example-happen:NO" || updated.Quantity != 0 || updated.ClosedAt == nil {
		t.Fatalf("updated position = %+v, want closed NO position", updated)
	}
	if math.Abs(updated.RealizedPnL-1.0) > 1e-9 {
		t.Fatalf("RealizedPnL = %v, want 1.0", updated.RealizedPnL)
	}
	if len(tradeRepo.trades) != 1 || tradeRepo.trades[0].PositionID == nil || *tradeRepo.trades[0].PositionID != positionID {
		t.Fatalf("unexpected trade position linkage: %+v", tradeRepo.trades)
	}
}

func TestProcessSignal_BuildsPortfolioForRiskChecks(t *testing.T) {
	equity := 10_000.0
	aaplPrice := 160.0
	balance := execution.Balance{Currency: "USD", Cash: 7_400, BuyingPower: 7_400, Equity: equity}
	openPositions := []domain.Position{
		{Ticker: "AAPL", MarketType: domain.MarketTypeStock, Quantity: 10, AvgEntry: 150, CurrentPrice: &aaplPrice},
		{Ticker: "MSFT", MarketType: domain.MarketTypeStock, Quantity: 20, AvgEntry: 50},
	}

	broker := &mockBroker{
		getAccountBalanceFn: func(context.Context) (execution.Balance, error) {
			return balance, nil
		},
	}
	positionRepo := &mockPositionRepo{
		getOpenFn: func(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
			return openPositions, nil
		},
	}

	var checkPositionPortfolio risk.Portfolio
	var preTradePortfolio risk.Portfolio
	riskEng := &mockRiskEngine{
		checkPositionLimitsFn: func(_ context.Context, _ string, _ float64, portfolio risk.Portfolio) (bool, string, error) {
			checkPositionPortfolio = portfolio
			return true, "", nil
		},
		checkPreTradeFn: func(_ context.Context, _ *domain.Order, portfolio risk.Portfolio) (bool, string, error) {
			preTradePortfolio = portfolio
			return true, "", nil
		},
	}
	orderRepo := &mockOrderRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if checkPositionPortfolio.ConcurrentPositions != 2 {
		t.Fatalf("checkPositionPortfolio.ConcurrentPositions = %d, want 2", checkPositionPortfolio.ConcurrentPositions)
	}
	if got := checkPositionPortfolio.TotalExposurePct; got != 0.26 {
		t.Fatalf("checkPositionPortfolio.TotalExposurePct = %v, want 0.26", got)
	}
	if got := checkPositionPortfolio.PositionExposureBySymbol["AAPL"]; got != 0.16 {
		t.Fatalf("checkPositionPortfolio.PositionExposureBySymbol[AAPL] = %v, want 0.16", got)
	}
	if got := checkPositionPortfolio.PositionExposureBySymbol["MSFT"]; got != 0.10 {
		t.Fatalf("checkPositionPortfolio.PositionExposureBySymbol[MSFT] = %v, want 0.10", got)
	}
	if got := checkPositionPortfolio.MarketExposurePct[domain.MarketTypeStock]; got != 0.28 {
		t.Fatalf("checkPositionPortfolio.MarketExposurePct[stock] = %v, want 0.28", got)
	}
	if preTradePortfolio.ConcurrentPositions != checkPositionPortfolio.ConcurrentPositions {
		t.Fatalf("preTradePortfolio.ConcurrentPositions = %d, want %d", preTradePortfolio.ConcurrentPositions, checkPositionPortfolio.ConcurrentPositions)
	}
	if preTradePortfolio.TotalExposurePct != checkPositionPortfolio.TotalExposurePct {
		t.Fatalf("preTradePortfolio.TotalExposurePct = %v, want %v", preTradePortfolio.TotalExposurePct, checkPositionPortfolio.TotalExposurePct)
	}
	if got := preTradePortfolio.PositionExposureBySymbol["AAPL"]; got != checkPositionPortfolio.PositionExposureBySymbol["AAPL"] {
		t.Fatalf("preTradePortfolio.PositionExposureBySymbol[AAPL] = %v, want %v", got, checkPositionPortfolio.PositionExposureBySymbol["AAPL"])
	}
	if got := preTradePortfolio.PositionExposureBySymbol["MSFT"]; got != checkPositionPortfolio.PositionExposureBySymbol["MSFT"] {
		t.Fatalf("preTradePortfolio.PositionExposureBySymbol[MSFT] = %v, want %v", got, checkPositionPortfolio.PositionExposureBySymbol["MSFT"])
	}
	if got := preTradePortfolio.MarketExposurePct[domain.MarketTypeStock]; got != checkPositionPortfolio.MarketExposurePct[domain.MarketTypeStock] {
		t.Fatalf("preTradePortfolio.MarketExposurePct[stock] = %v, want %v", got, checkPositionPortfolio.MarketExposurePct[domain.MarketTypeStock])
	}
}

func TestProcessSignal_RejectsWhenPerMarketExposureWouldExceedLimit(t *testing.T) {
	tests := []struct {
		name      string
		market    domain.MarketType
		positions []domain.Position
		plan      execution.TradingPlan
	}{
		{
			name:   "stock",
			market: domain.MarketTypeStock,
			positions: []domain.Position{
				{Ticker: "MSFT", MarketType: domain.MarketTypeStock, Quantity: 10, AvgEntry: 190},
				{Ticker: "NVDA", MarketType: domain.MarketTypeStock, Quantity: 10, AvgEntry: 150},
				{Ticker: "IBM", MarketType: domain.MarketTypeStock, Quantity: 15, AvgEntry: 100},
			},
			plan: defaultPlan(),
		},
		{
			name:   "polymarket",
			market: domain.MarketTypePolymarket,
			positions: []domain.Position{
				{Ticker: "POLY-1", MarketType: domain.MarketTypePolymarket, Quantity: 4, AvgEntry: 100},
			},
			plan: func() execution.TradingPlan {
				p := defaultPlan()
				p.MarketType = domain.MarketTypePolymarket
				p.Ticker = "POLY-2"
				p.Side = "YES"
				return p
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			broker := &mockBroker{}
			broker.getAccountBalanceFn = func(context.Context) (execution.Balance, error) {
				return execution.Balance{Currency: "USD", Cash: 10_000, BuyingPower: 10_000, Equity: 10_000}, nil
			}
			positionRepo := &mockPositionRepo{
				getOpenFn: func(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
					return tc.positions, nil
				},
			}
			orderRepo := &mockOrderRepo{}
			tradeRepo := &mockTradeRepo{}
			auditRepo := &mockAuditLogRepo{}

			realRiskEng := risk.NewRiskEngine(risk.DefaultPositionLimits(), risk.DefaultCircuitBreakerConfig(), positionRepo, slog.Default())
			realRiskEng.SetFileExistsFunc(func(string) bool { return false })
			realRiskEng.SetGetEnvFunc(func(string) string { return "" })

			var captured risk.Portfolio
			riskEng := &mockRiskEngine{
				checkPositionLimitsFn: func(ctx context.Context, ticker string, quantity float64, portfolio risk.Portfolio) (bool, string, error) {
					captured = portfolio
					return realRiskEng.CheckPositionLimits(ctx, ticker, quantity, portfolio)
				},
				checkPreTradeFn: func(ctx context.Context, order *domain.Order, portfolio risk.Portfolio) (bool, string, error) {
					return realRiskEng.CheckPreTrade(ctx, order, portfolio)
				},
			}

			mgr := execution.NewOrderManager(
				broker,
				"paper",
				riskEng,
				positionRepo,
				orderRepo,
				tradeRepo,
				auditRepo,
				nil,
				execution.SizingConfig{Method: execution.PositionSizingMethodFixedFractional, FractionPct: 0.02},
				slog.Default(),
			)

			err := mgr.ProcessSignal(context.Background(), defaultSignal(), tc.plan, uuid.New(), uuid.New())
			if err == nil {
				t.Fatalf("ProcessSignal() expected error when per-market exposure would exceed limit; captured=%v", captured.MarketExposurePct)
			}
			if got := captured.MarketExposurePct[tc.market]; got <= 0.05 && tc.market == domain.MarketTypePolymarket {
				t.Fatalf("expected captured polymarket exposure to exceed limit, got %v", got)
			}
			if got := captured.MarketExposurePct[tc.market]; got <= 0.50 && tc.market == domain.MarketTypeStock {
				t.Fatalf("expected captured stock exposure to exceed limit, got %v", got)
			}
			if len(orderRepo.orders) != 0 {
				t.Fatalf("expected 0 orders, got %d", len(orderRepo.orders))
			}
			if !strings.Contains(strings.ToLower(err.Error()), "market exposure") {
				t.Fatalf("expected market exposure rejection, got %v", err)
			}
		})
	}
}

func TestProcessSignal_KillSwitchActive(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{
		isKillSwitchActiveFn: func(_ context.Context) (bool, error) {
			return true, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).WithDecisionRecorder(recorder)

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeCrypto

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		plan,
		uuid.New(),
		uuid.New(),
	)

	if err == nil {
		t.Fatal("ProcessSignal() expected error when kill switch active")
	}

	// Verify no order was created.
	if len(orderRepo.orders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(orderRepo.orders))
	}

	// Verify audit log recorded the kill switch event.
	if len(auditRepo.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditRepo.entries))
	}
	if auditRepo.entries[0].EventType != "kill_switch_blocked" {
		t.Errorf("expected audit event type kill_switch_blocked, got %s", auditRepo.entries[0].EventType)
	}
}

func TestProcessSignal_RiskCheckRejection(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{
		checkPositionLimitsFn: func(_ context.Context, _ string, _ float64, _ risk.Portfolio) (bool, string, error) {
			return false, "exceeds max position size", nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).WithDecisionRecorder(recorder)

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeCrypto

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		plan,
		uuid.New(),
		uuid.New(),
	)

	if err == nil {
		t.Fatal("ProcessSignal() expected error when risk check rejects")
	}

	// Verify no order was created.
	if len(orderRepo.orders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(orderRepo.orders))
	}

	// Verify audit log recorded the rejection.
	if len(auditRepo.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditRepo.entries))
	}
	if auditRepo.entries[0].EventType != "risk_check_rejected" {
		t.Errorf("expected audit event type risk_check_rejected, got %s", auditRepo.entries[0].EventType)
	}

	// Verify the reason is in the audit details.
	var details map[string]any
	if err := json.Unmarshal(auditRepo.entries[0].Details, &details); err != nil {
		t.Fatalf("unmarshal audit details: %v", err)
	}

	if reason, ok := details["reason"].(string); !ok || reason != "exceeds max position size" {
		t.Errorf("expected reason 'exceeds max position size', got %v", details["reason"])
	}
	if len(recorder.decisions) == 0 {
		t.Fatal("expected recorded trade decision")
	}
	if recorder.decisions[0].MarketType != domain.MarketTypeCrypto {
		t.Fatalf("decision market type = %s, want crypto", recorder.decisions[0].MarketType)
	}
}

func TestProcessSignal_RecordsPaperDecisionAndAttachesOrder(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).WithDecisionRecorder(recorder)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(recorder.decisions) != 1 {
		t.Fatalf("expected 1 recorded decision, got %d", len(recorder.decisions))
	}
	decision := recorder.decisions[0]
	if decision.Status != domain.TradeDecisionStatusCandidate {
		t.Fatalf("decision status = %s, want %s", decision.Status, domain.TradeDecisionStatusCandidate)
	}
	if decision.RiskStatus != domain.RiskDecisionApproved {
		t.Fatalf("decision risk status = %s, want %s", decision.RiskStatus, domain.RiskDecisionApproved)
	}
	if decision.InstrumentKey != "AAPL" {
		t.Fatalf("decision instrument key = %q, want AAPL", decision.InstrumentKey)
	}

	if len(recorder.paperAttach) != 1 {
		t.Fatalf("expected 1 paper attachment, got %d", len(recorder.paperAttach))
	}
	if len(recorder.liveAttach) != 0 {
		t.Fatalf("expected 0 live attachments, got %d", len(recorder.liveAttach))
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 created order, got %d", len(orderRepo.orders))
	}
	if got, want := recorder.paperAttach[0].orderID, orderRepo.orders[0].ID; got != want {
		t.Fatalf("paper attachment orderID = %s, want %s", got, want)
	}
	if got, want := recorder.paperAttach[0].decisionID, decision.ID; got != want {
		t.Fatalf("paper attachment decisionID = %s, want %s", got, want)
	}
}

func TestProcessSignal_RecordsTradeDecisionWithLLMMetadata(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).WithDecisionRecorder(recorder)

	promptTokens := 123
	completionTokens := 45
	latencyMS := 678
	costUSD := 0.0
	plan := defaultPlan()
	plan.DecisionMetadata = &execution.DecisionMetadata{
		PromptText:       " system: trade carefully \n",
		LLMProvider:      " openai ",
		LLMModel:         " gpt-4.1 ",
		PromptTokens:     &promptTokens,
		CompletionTokens: &completionTokens,
		LatencyMS:        &latencyMS,
		CostUSD:          &costUSD,
	}

	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() error = %v", err)
	}
	if len(recorder.decisions) == 0 {
		t.Fatal("expected recorded trade decision")
	}
	decision := recorder.decisions[0]
	if decision.PromptText != " system: trade carefully \n" || decision.LLMProvider != "openai" || decision.LLMModel != "gpt-4.1" {
		t.Fatalf("unexpected LLM string metadata: %+v", decision)
	}
	if decision.PromptTokens == nil || *decision.PromptTokens != promptTokens {
		t.Fatalf("PromptTokens = %v, want %d", decision.PromptTokens, promptTokens)
	}
	if decision.CompletionTokens == nil || *decision.CompletionTokens != completionTokens {
		t.Fatalf("CompletionTokens = %v, want %d", decision.CompletionTokens, completionTokens)
	}
	if decision.LatencyMS == nil || *decision.LatencyMS != latencyMS {
		t.Fatalf("LatencyMS = %v, want %d", decision.LatencyMS, latencyMS)
	}
	if decision.CostUSD == nil || *decision.CostUSD != 0 {
		t.Fatalf("CostUSD = %v, want %f", decision.CostUSD, costUSD)
	}
}

func TestProcessSignal_LiveGateBlocksBrokerSubmission(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}
	strategyID := uuid.New()

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).
		WithDecisionRecorder(recorder).
		WithLiveTrading(true).
		WithLiveGate(execution.LiveGateConfig{EnableLiveTrading: true, AllowedStrategies: map[uuid.UUID]bool{}, AllowedBrokers: map[string]bool{}})

	plan := defaultPlan()
	plan.MarketType = domain.MarketTypeCrypto
	err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, strategyID, uuid.New())
	if err == nil {
		t.Fatal("expected live gate error")
	}
	if len(orderRepo.orders) != 0 {
		t.Fatalf("expected 0 orders, got %d", len(orderRepo.orders))
	}
	if len(recorder.decisions) != 1 {
		t.Fatalf("expected 1 recorded decision, got %d", len(recorder.decisions))
	}
	if recorder.decisions[0].MarketType != domain.MarketTypeCrypto {
		t.Fatalf("decision market type = %s, want crypto", recorder.decisions[0].MarketType)
	}
	if recorder.decisions[0].Status != domain.TradeDecisionStatusRejected {
		t.Fatalf("decision status = %s, want %s", recorder.decisions[0].Status, domain.TradeDecisionStatusRejected)
	}
	if len(recorder.paperAttach) != 0 || len(recorder.liveAttach) != 0 {
		t.Fatalf("expected no attachments, got paper=%d live=%d", len(recorder.paperAttach), len(recorder.liveAttach))
	}
}

func TestProcessSignal_RecordsLiveDecisionAndAttachesLiveOrder(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}
	strategyID := uuid.New()
	gate := execution.LiveGateConfig{
		EnableLiveTrading: true,
		AllowedStrategies: map[uuid.UUID]bool{strategyID: true},
		AllowedBrokers:    map[string]bool{"alpaca": true},
	}

	mgr := execution.NewOrderManager(
		broker,
		"alpaca",
		riskEng,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditRepo,
		nil,
		execution.SizingConfig{Method: execution.PositionSizingMethodFixedFractional, FractionPct: 0.02},
		slog.Default(),
	).WithDecisionRecorder(recorder).WithLiveTrading(true).WithLiveGate(gate)

	err := mgr.ProcessSignal(context.Background(), defaultSignal(), defaultPlan(), strategyID, uuid.New())
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if len(recorder.decisions) != 1 {
		t.Fatalf("expected 1 recorded decision, got %d", len(recorder.decisions))
	}
	if len(recorder.liveAttach) != 1 {
		t.Fatalf("expected 1 live attachment, got %d", len(recorder.liveAttach))
	}
	if len(recorder.paperAttach) != 0 {
		t.Fatalf("expected 0 paper attachments, got %d", len(recorder.paperAttach))
	}
	if got, want := recorder.liveAttach[0].decisionID, recorder.decisions[0].ID; got != want {
		t.Fatalf("live attachment decisionID = %s, want %s", got, want)
	}
}

func TestProcessSignal_PreTradeRejection(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{
		checkPreTradeFn: func(_ context.Context, _ *domain.Order, _ risk.Portfolio) (bool, string, error) {
			return false, "circuit breaker tripped", nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)

	if err == nil {
		t.Fatal("ProcessSignal() expected error when pre-trade check rejects")
	}

	// Order should have been created (pending) then updated to rejected.
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}

	if len(orderRepo.updates) < 1 {
		t.Fatalf("expected at least 1 order update, got %d", len(orderRepo.updates))
	}

	lastUpdate := orderRepo.updates[len(orderRepo.updates)-1]
	if lastUpdate.Status != domain.OrderStatusRejected {
		t.Errorf("expected rejected status, got %s", lastUpdate.Status)
	}

	// Verify audit log has order_created and pre_trade_rejected.
	types := auditEventTypes(auditRepo.entries)
	wantTypes := []string{"order_created", "pre_trade_rejected"}

	if len(types) != len(wantTypes) {
		t.Fatalf("expected %d audit entries, got %d: %v", len(wantTypes), len(types), types)
	}

	for i, want := range wantTypes {
		if types[i] != want {
			t.Errorf("audit[%d] = %q, want %q", i, types[i], want)
		}
	}
}

func TestProcessSignal_AuditLogEntries(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	// Verify the audit log has entries for: order_created, order_submitted, order_filled.
	types := auditEventTypes(auditRepo.entries)
	wantTypes := []string{"order_created", "order_submitted", "order_filled"}

	if len(types) != len(wantTypes) {
		t.Fatalf("expected %d audit entries, got %d: %v", len(wantTypes), len(types), types)
	}

	for i, want := range wantTypes {
		if types[i] != want {
			t.Errorf("audit[%d] = %q, want %q", i, types[i], want)
		}
	}

	// Verify all audit entries have actor = "order_manager".
	for i, entry := range auditRepo.entries {
		if entry.Actor != "order_manager" {
			t.Errorf("audit[%d] actor = %q, want %q", i, entry.Actor, "order_manager")
		}
	}
}

func TestProcessSignal_HoldSignalSkipped(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		execution.FinalSignal{Signal: domain.PipelineSignalHold, Confidence: 0.5},
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error for hold signal: %v", err)
	}

	// No order, trade, or position should be created.
	if len(orderRepo.orders) != 0 {
		t.Errorf("expected 0 orders for hold signal, got %d", len(orderRepo.orders))
	}
	if len(tradeRepo.trades) != 0 {
		t.Errorf("expected 0 trades for hold signal, got %d", len(tradeRepo.trades))
	}
	if len(positionRepo.positions) != 0 {
		t.Errorf("expected 0 positions for hold signal, got %d", len(positionRepo.positions))
	}
}

func TestProcessSignal_HoldSignalSkipsPredictionNormalizationForPredictionMarkets(t *testing.T) {
	for _, marketType := range []domain.MarketType{domain.MarketTypeKalshi, domain.MarketTypePolymarket} {
		marketType := marketType
		t.Run(string(marketType), func(t *testing.T) {
			broker := &mockBroker{}
			riskEng := &mockRiskEngine{}
			orderRepo := &mockOrderRepo{createFn: func(context.Context, *domain.Order) error {
				t.Fatal("unexpected order write")
				return nil
			}, updateFn: func(context.Context, *domain.Order) error {
				t.Fatal("unexpected order update")
				return nil
			}}
			positionRepo := &mockPositionRepo{createFn: func(context.Context, *domain.Position) error {
				t.Fatal("unexpected position write")
				return nil
			}, updateFn: func(context.Context, *domain.Position) error {
				t.Fatal("unexpected position update")
				return nil
			}, getByStrategyFn: func(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
				t.Fatal("unexpected position lookup")
				return nil, nil
			}}
			tradeRepo := &mockTradeRepo{createFn: func(context.Context, *domain.Trade) error {
				t.Fatal("unexpected trade write")
				return nil
			}}
			auditRepo := &mockAuditLogRepo{}

			mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

			err := mgr.ProcessSignal(
				context.Background(),
				execution.FinalSignal{Signal: domain.PipelineSignalHold, Confidence: 0.5},
				execution.TradingPlan{MarketType: marketType, Ticker: "MARKET", Side: "", Action: domain.PipelineSignalBuy},
				uuid.New(),
				uuid.New(),
			)
			if err != nil {
				t.Fatalf("ProcessSignal() unexpected error for %s hold signal: %v", marketType, err)
			}
			if len(orderRepo.orders) != 0 || len(orderRepo.updates) != 0 {
				t.Fatalf("expected no order writes for %s hold signal", marketType)
			}
			if len(positionRepo.positions) != 0 || len(positionRepo.updates) != 0 {
				t.Fatalf("expected no position writes for %s hold signal", marketType)
			}
			if len(tradeRepo.trades) != 0 {
				t.Fatalf("expected no trade writes for %s hold signal", marketType)
			}
		})
	}
}

func TestProcessSignal_SellSignal(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	strategyID := uuid.New()
	positionRepo := &mockPositionRepo{
		getByStrategyFn: func(ctx context.Context, gotStrategyID uuid.UUID, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
			if gotStrategyID != strategyID {
				t.Fatalf("strategyID = %s, want %s", gotStrategyID, strategyID)
			}
			if filter.Ticker != "TSLA" || filter.Side != domain.PositionSideLong {
				t.Fatalf("filter = %+v, want TSLA long", filter)
			}
			return []domain.Position{{
				ID:         uuid.New(),
				StrategyID: &strategyID,
				Ticker:     "TSLA",
				Side:       domain.PositionSideLong,
				Quantity:   10,
				AvgEntry:   190,
				OpenedAt:   time.Now(),
			}}, nil
		},
	}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9},
		execution.TradingPlan{
			Action:     domain.PipelineSignalSell,
			MarketType: domain.MarketTypeStock,
			Ticker:     "TSLA",
			EntryType:  "market",
			EntryPrice: 200.0,
			StopLoss:   210.0,
			TakeProfit: 180.0,
		},
		strategyID,
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orderRepo.orders))
	}

	if orderRepo.orders[0].Side != domain.OrderSideSell {
		t.Errorf("expected sell side, got %s", orderRepo.orders[0].Side)
	}

	if len(positionRepo.positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positionRepo.positions))
	}

	if positionRepo.positions[0].Side != domain.PositionSideLong {
		t.Errorf("expected long position, got %s", positionRepo.positions[0].Side)
	}
}

func TestProcessSignal_SellSignalWithoutOpenLongPositionSkipped(t *testing.T) {
	broker := &mockBroker{
		submitOrderFn: func(ctx context.Context, order *domain.Order) (string, error) {
			t.Fatal("SubmitOrder should not be called for unowned sell signal")
			return "", nil
		},
	}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).WithDecisionRecorder(recorder)

	err := mgr.ProcessSignal(
		context.Background(),
		execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9},
		execution.TradingPlan{
			Action:     domain.PipelineSignalSell,
			Ticker:     "TSLA",
			EntryType:  "market",
			EntryPrice: 200.0,
			StopLoss:   210.0,
			TakeProfit: 180.0,
		},
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(orderRepo.orders) != 0 {
		t.Fatalf("expected 0 orders, got %d", len(orderRepo.orders))
	}
	if len(tradeRepo.trades) != 0 {
		t.Fatalf("expected 0 trades, got %d", len(tradeRepo.trades))
	}
	if len(positionRepo.positions) != 0 {
		t.Fatalf("expected 0 positions, got %d", len(positionRepo.positions))
	}
	if len(recorder.decisions) != 1 {
		t.Fatalf("expected 1 rejected decision, got %d", len(recorder.decisions))
	}
	decision := recorder.decisions[0]
	if decision.Side != domain.OrderSideSell {
		t.Fatalf("decision side = %s, want sell", decision.Side)
	}
	if decision.Status != domain.TradeDecisionStatusRejected {
		t.Fatalf("decision status = %s, want %s", decision.Status, domain.TradeDecisionStatusRejected)
	}
	if decision.RiskStatus != domain.RiskDecisionRejected {
		t.Fatalf("decision risk status = %s, want %s", decision.RiskStatus, domain.RiskDecisionRejected)
	}
	if decision.ProposedSize != 0 || decision.ApprovedSize != 0 {
		t.Fatalf("decision sizes = (%v,%v), want (0,0)", decision.ProposedSize, decision.ApprovedSize)
	}
	if !slices.Contains(decision.RiskReasons, "unowned_sell_no_open_long") {
		t.Fatalf("decision risk reasons = %v, want unowned_sell_no_open_long", decision.RiskReasons)
	}
	if !strings.Contains(string(decision.Evidence), "unowned_sell_no_open_long") {
		t.Fatalf("decision evidence = %s, want reason metadata", string(decision.Evidence))
	}
}

func TestProcessSignal_NonStockSellWithoutOpenLongIsNotStockGuarded(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).WithDecisionRecorder(recorder)

	err := mgr.ProcessSignal(
		context.Background(),
		execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9},
		execution.TradingPlan{
			Action:     domain.PipelineSignalSell,
			MarketType: domain.MarketTypeCrypto,
			Ticker:     "BTCUSD",
			EntryType:  "market",
			EntryPrice: 100,
		},
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected non-stock sell to continue to order path, got %d orders", len(orderRepo.orders))
	}
	if orderRepo.orders[0].MarketType != domain.MarketTypeCrypto {
		t.Fatalf("order market type = %s, want crypto", orderRepo.orders[0].MarketType)
	}
	if len(recorder.decisions) == 0 {
		t.Fatal("expected recorded decision")
	}
	if recorder.decisions[0].MarketType != domain.MarketTypeCrypto {
		t.Fatalf("decision market type = %s, want crypto", recorder.decisions[0].MarketType)
	}
	for _, decision := range recorder.decisions {
		if decision.Status == domain.TradeDecisionStatusRejected && slices.Contains(decision.RiskReasons, "unowned_sell_no_open_long") {
			t.Fatalf("expected no stock-ownership rejection decision for non-stock sell, got %+v", decision)
		}
	}
}

func TestProcessSignal_StockSellRequiresTickerForOwnershipCheck(t *testing.T) {
	broker := &mockBroker{
		submitOrderFn: func(ctx context.Context, order *domain.Order) (string, error) {
			t.Fatal("SubmitOrder should not be called when stock sell ticker is empty")
			return "", nil
		},
	}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		execution.FinalSignal{Signal: domain.PipelineSignalSell, Confidence: 0.9},
		execution.TradingPlan{
			Action:     domain.PipelineSignalSell,
			MarketType: domain.MarketTypeStock,
			Ticker:     " ",
			EntryType:  "market",
			EntryPrice: 100,
		},
		uuid.New(),
		uuid.New(),
	)
	if err == nil {
		t.Fatal("ProcessSignal() error = nil, want ticker ownership error")
	}
	if !strings.Contains(err.Error(), "requires ticker") {
		t.Fatalf("ProcessSignal() error = %v, want requires ticker", err)
	}
	if len(orderRepo.orders) != 0 {
		t.Fatalf("expected no orders when ticker is empty, got %d", len(orderRepo.orders))
	}
}

func TestProcessSignal_BrokerSubmitError(t *testing.T) {
	broker := &mockBroker{
		submitOrderFn: func(_ context.Context, _ *domain.Order) (string, error) {
			return "", errors.New("broker unavailable")
		},
	}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	recorder := &mockDecisionRecorder{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).WithDecisionRecorder(recorder)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)

	if err == nil {
		t.Fatal("ProcessSignal() expected error on broker submit failure")
	}

	// Order should have been created then updated to rejected.
	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}

	if len(orderRepo.updates) < 1 {
		t.Fatalf("expected at least 1 order update, got %d", len(orderRepo.updates))
	}

	lastUpdate := orderRepo.updates[len(orderRepo.updates)-1]
	if lastUpdate.Status != domain.OrderStatusRejected {
		t.Errorf("expected rejected status, got %s", lastUpdate.Status)
	}
	if len(recorder.decisions) != 1 {
		t.Fatalf("expected 1 decision recorded, got %d", len(recorder.decisions))
	}
	if len(recorder.paperAttach) != 1 {
		t.Fatalf("expected 1 paper attachment, got %d", len(recorder.paperAttach))
	}
	if got, want := recorder.paperAttach[0].decisionID, recorder.decisions[0].ID; got != want {
		t.Fatalf("paper attachment decisionID = %s, want %s", got, want)
	}
	if got, want := recorder.paperAttach[0].orderID, orderRepo.orders[0].ID; got != want {
		t.Fatalf("paper attachment orderID = %s, want %s", got, want)
	}

	// Verify audit log has order_created and order_rejected.
	types := auditEventTypes(auditRepo.entries)
	wantTypes := []string{"order_created", "order_rejected"}

	if len(types) != len(wantTypes) {
		t.Fatalf("expected %d audit entries, got %d: %v", len(wantTypes), len(types), types)
	}

	for i, want := range wantTypes {
		if types[i] != want {
			t.Errorf("audit[%d] = %q, want %q", i, types[i], want)
		}
	}
}

func TestProcessSignal_OrderCancelled(t *testing.T) {
	broker := &mockBroker{
		getOrderStatusFn: func(_ context.Context, _ string) (domain.OrderStatus, error) {
			return domain.OrderStatusCancelled, nil
		},
	}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	// No trade or position should be created for a cancelled order.
	if len(tradeRepo.trades) != 0 {
		t.Errorf("expected 0 trades for cancelled order, got %d", len(tradeRepo.trades))
	}
	if len(positionRepo.positions) != 0 {
		t.Errorf("expected 0 positions for cancelled order, got %d", len(positionRepo.positions))
	}
}

func TestProcessSignal_LimitOrder(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	plan := defaultPlan()
	plan.EntryType = "limit"

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		plan,
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orderRepo.orders))
	}

	order := orderRepo.orders[0]
	if order.OrderType != domain.OrderTypeLimit {
		t.Errorf("expected limit order type, got %s", order.OrderType)
	}
	if order.LimitPrice == nil {
		t.Error("expected limit price to be set")
	} else if *order.LimitPrice != 150.0 {
		t.Errorf("expected limit price 150.0, got %f", *order.LimitPrice)
	}
}

func TestProcessSignal_RecordsOrderMetrics(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	metrics := &mockOrderMetrics{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo).WithMetrics(metrics)

	err := mgr.ProcessSignal(context.Background(), defaultSignal(), defaultPlan(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	for _, want := range []string{"paper:buy:pending", "paper:buy:submitted", "paper:buy:filled"} {
		if !slices.Contains(metrics.records, want) {
			t.Fatalf("metrics records = %v, want entry %q", metrics.records, want)
		}
	}
}

func TestNewOrderManager_NilLogger(t *testing.T) {
	mgr := execution.NewOrderManager(
		&mockBroker{},
		"paper",
		&mockRiskEngine{},
		&mockPositionRepo{},
		&mockOrderRepo{},
		&mockTradeRepo{},
		&mockAuditLogRepo{},
		nil, // agentEventRepo
		execution.SizingConfig{},
		nil, // nil logger should not panic
	)

	if mgr == nil {
		t.Fatal("expected non-nil OrderManager")
	}
}

func TestProcessSignal_UsesInjectedClockForLifecycleTimestamps(t *testing.T) {
	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)
	now := time.Date(2026, 3, 25, 14, 45, 0, 0, time.UTC)
	mgr.SetNowFunc(func() time.Time { return now })

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 created order, got %d", len(orderRepo.orders))
	}
	if got := orderRepo.orders[0].CreatedAt; !got.Equal(now) {
		t.Fatalf("order.CreatedAt = %s, want %s", got, now)
	}

	if len(orderRepo.updates) == 0 {
		t.Fatal("expected at least 1 order update")
	}
	lastUpdate := orderRepo.updates[len(orderRepo.updates)-1]
	if lastUpdate.SubmittedAt == nil || !lastUpdate.SubmittedAt.Equal(now) {
		t.Fatalf("order.SubmittedAt = %v, want %s", lastUpdate.SubmittedAt, now)
	}
	if lastUpdate.FilledAt == nil || !lastUpdate.FilledAt.Equal(now) {
		t.Fatalf("order.FilledAt = %v, want %s", lastUpdate.FilledAt, now)
	}

	if len(tradeRepo.trades) != 1 {
		t.Fatalf("expected 1 created trade, got %d", len(tradeRepo.trades))
	}
	if got := tradeRepo.trades[0].ExecutedAt; !got.Equal(now) {
		t.Fatalf("trade.ExecutedAt = %s, want %s", got, now)
	}
	if got := tradeRepo.trades[0].CreatedAt; !got.Equal(now) {
		t.Fatalf("trade.CreatedAt = %s, want %s", got, now)
	}

	if len(positionRepo.positions) != 1 {
		t.Fatalf("expected 1 created position, got %d", len(positionRepo.positions))
	}
	if got := positionRepo.positions[0].OpenedAt; !got.Equal(now) {
		t.Fatalf("position.OpenedAt = %s, want %s", got, now)
	}

	if len(auditRepo.entries) == 0 {
		t.Fatal("expected at least 1 audit entry")
	}
	for i, entry := range auditRepo.entries {
		if !entry.CreatedAt.Equal(now) {
			t.Fatalf("audit[%d].CreatedAt = %s, want %s", i, entry.CreatedAt, now)
		}
	}
}

func TestProcessSignal_EntryTypeVariants(t *testing.T) {
	tests := []struct {
		name      string
		entryType string
		wantType  domain.OrderType
	}{
		{name: "stop entry becomes stop order", entryType: "stop", wantType: domain.OrderTypeStop},
		{name: "stop limit entry becomes stop limit order", entryType: "stop_limit", wantType: domain.OrderTypeStopLimit},
		{name: "unknown entry type defaults to market", entryType: "surprise", wantType: domain.OrderTypeMarket},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			broker := &mockBroker{
				getOrderStatusFn: func(_ context.Context, _ string) (domain.OrderStatus, error) {
					return domain.OrderStatusSubmitted, nil
				},
			}
			riskEng := &mockRiskEngine{}
			orderRepo := &mockOrderRepo{}
			positionRepo := &mockPositionRepo{}
			tradeRepo := &mockTradeRepo{}
			auditRepo := &mockAuditLogRepo{}

			mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)
			plan := defaultPlan()
			plan.EntryType = tc.entryType

			err := mgr.ProcessSignal(
				context.Background(),
				defaultSignal(),
				plan,
				uuid.New(),
				uuid.New(),
			)
			if err != nil {
				t.Fatalf("ProcessSignal() unexpected error: %v", err)
			}
			if len(orderRepo.orders) != 1 {
				t.Fatalf("expected 1 order, got %d", len(orderRepo.orders))
			}
			if got := orderRepo.orders[0].OrderType; got != tc.wantType {
				t.Fatalf("order type = %s, want %s", got, tc.wantType)
			}
		})
	}
}

func TestProcessSignal_EmitsOrderEvents(t *testing.T) {
	t.Parallel()

	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	eventRepo := &mockAgentEventRepo{}

	cfg := execution.SizingConfig{
		Method:      execution.PositionSizingMethodFixedFractional,
		FractionPct: 0.02,
	}

	mgr := execution.NewOrderManager(
		broker,
		"paper",
		riskEng,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditRepo,
		eventRepo,
		cfg,
		slog.Default(),
	)

	strategyID := uuid.New()
	runID := uuid.New()

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		strategyID,
		runID,
	)
	if err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	eventRepo.mu.Lock()
	events := eventRepo.events
	eventRepo.mu.Unlock()

	// Happy path: broker submits then returns filled → expect submitted + filled.
	if len(events) != 2 {
		t.Fatalf("expected 2 agent events, got %d", len(events))
	}

	wantKinds := []string{execution.OrderEventSubmitted, execution.OrderEventFilled}
	for i, want := range wantKinds {
		if events[i].EventKind != want {
			t.Errorf("event[%d].EventKind = %q, want %q", i, events[i].EventKind, want)
		}
		if events[i].PipelineRunID == nil || *events[i].PipelineRunID != runID {
			t.Errorf("event[%d].PipelineRunID = %v, want %s", i, events[i].PipelineRunID, runID)
		}
		if events[i].StrategyID == nil || *events[i].StrategyID != strategyID {
			t.Errorf("event[%d].StrategyID = %v, want %s", i, events[i].StrategyID, strategyID)
		}
		if events[i].Metadata == nil {
			t.Errorf("event[%d].Metadata is nil", i)
		} else {
			var meta map[string]any
			if err := json.Unmarshal(events[i].Metadata, &meta); err != nil {
				t.Fatalf("event[%d] metadata unmarshal: %v", i, err)
			}
			if meta["ticker"] != "AAPL" {
				t.Errorf("event[%d] metadata ticker = %v, want AAPL", i, meta["ticker"])
			}
		}
	}
}

func TestProcessSignal_NilEventRepo_NoPanic(t *testing.T) {
	t.Parallel()

	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	cfg := execution.SizingConfig{
		Method:      execution.PositionSizingMethodFixedFractional,
		FractionPct: 0.02,
	}

	mgr := execution.NewOrderManager(
		broker,
		"paper",
		riskEng,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditRepo,
		nil, // nil agentEventRepo — must not panic
		cfg,
		slog.Default(),
	)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() with nil event repo: %v", err)
	}
}

func TestProcessSignal_EventRepoError_DoesNotFailOrder(t *testing.T) {
	t.Parallel()

	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}
	eventRepo := &mockAgentEventRepo{
		createFn: func(_ context.Context, _ *domain.AgentEvent) error {
			return errors.New("event repo down")
		},
	}

	cfg := execution.SizingConfig{
		Method:      execution.PositionSizingMethodFixedFractional,
		FractionPct: 0.02,
	}

	mgr := execution.NewOrderManager(
		broker,
		"paper",
		riskEng,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditRepo,
		eventRepo,
		cfg,
		slog.Default(),
	)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("ProcessSignal() should succeed even when event repo fails: %v", err)
	}

	// Order should still be created successfully.
	orderRepo.mu.Lock()
	gotOrders := len(orderRepo.orders)
	orderRepo.mu.Unlock()
	if gotOrders != 1 {
		t.Fatalf("expected 1 order, got %d", gotOrders)
	}
}

func TestProcessSignal_ZeroPositionSize(t *testing.T) {
	t.Parallel()

	broker := &mockBroker{
		getAccountBalanceFn: func(_ context.Context) (execution.Balance, error) {
			return execution.Balance{Currency: "USD", Cash: 100, BuyingPower: 100, Equity: 100}, nil
		},
	}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	// FractionPct zero causes FixedFractionalSize to return 0.
	cfg := execution.SizingConfig{
		Method:      execution.PositionSizingMethodFixedFractional,
		FractionPct: 0,
	}

	mgr := execution.NewOrderManager(
		broker,
		"paper",
		riskEng,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditRepo,
		nil,
		cfg,
		slog.Default(),
	)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error for zero position size")
	}

	// No order should be created.
	if len(orderRepo.orders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(orderRepo.orders))
	}
}

func TestProcessSignal_KellySizingUsesUnitsAndExposure(t *testing.T) {
	t.Parallel()

	var capturedExposure float64
	riskEng := &mockRiskEngine{
		checkPositionLimitsFn: func(_ context.Context, _ string, exposurePct float64, _ risk.Portfolio) (bool, string, error) {
			capturedExposure = exposurePct
			return true, "", nil
		},
	}
	broker := &mockBroker{
		getAccountBalanceFn: func(_ context.Context) (execution.Balance, error) {
			return execution.Balance{Currency: "USD", Cash: 100000, BuyingPower: 100000, Equity: 100000}, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := execution.NewOrderManager(
		broker,
		"paper",
		riskEng,
		positionRepo,
		orderRepo,
		tradeRepo,
		auditRepo,
		nil,
		execution.SizingConfig{
			Method:       execution.PositionSizingMethodKelly,
			WinRate:      0.60,
			WinLossRatio: 2,
			HalfKelly:    false,
		},
		slog.Default(),
	)

	plan := defaultPlan()
	plan.EntryPrice = 50
	plan.StopLoss = 45

	if err := mgr.ProcessSignal(context.Background(), defaultSignal(), plan, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("ProcessSignal() unexpected error: %v", err)
	}

	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderRepo.orders))
	}
	if got := orderRepo.orders[0].Quantity; got != 800 {
		t.Fatalf("order quantity = %v, want 800", got)
	}
	if math.Abs(capturedExposure-0.4) > 1e-9 {
		t.Fatalf("exposure = %v, want 0.4", capturedExposure)
	}
}

func TestProcessSignal_ZeroEquity(t *testing.T) {
	t.Parallel()

	broker := &mockBroker{
		getAccountBalanceFn: func(_ context.Context) (execution.Balance, error) {
			return execution.Balance{Currency: "USD", Cash: 0, BuyingPower: 0, Equity: 0}, nil
		},
	}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error for zero equity")
	}

	// No order should be created.
	if len(orderRepo.orders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(orderRepo.orders))
	}
}

func TestProcessSignal_TradeCreationFailure_PartialFill(t *testing.T) {
	t.Parallel()

	broker := &mockBroker{}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{
		createFn: func(_ context.Context, _ *domain.Trade) error {
			return errors.New("trade repo down")
		},
	}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error when trade creation fails")
	}

	// Position should still have been created (before trade).
	positionRepo.mu.Lock()
	posCount := len(positionRepo.positions)
	positionRepo.mu.Unlock()
	if posCount != 1 {
		t.Errorf("expected 1 position (partial state), got %d", posCount)
	}

	// Audit log should contain an incomplete fill record.
	auditRepo.mu.Lock()
	auditTypes := auditEventTypes(auditRepo.entries)
	auditRepo.mu.Unlock()

	hasIncompleteFill := false
	for _, at := range auditTypes {
		if at == "order_fill_incomplete" {
			hasIncompleteFill = true
			break
		}
	}
	if !hasIncompleteFill {
		t.Errorf("expected 'order_fill_incomplete' audit event, got %v", auditTypes)
	}
}

func TestProcessSignal_BalanceError(t *testing.T) {
	t.Parallel()

	broker := &mockBroker{
		getAccountBalanceFn: func(_ context.Context) (execution.Balance, error) {
			return execution.Balance{}, errors.New("broker unreachable")
		},
	}
	riskEng := &mockRiskEngine{}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	if err == nil {
		t.Fatal("expected error when broker is unreachable")
	}

	// No order should be created.
	if len(orderRepo.orders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(orderRepo.orders))
	}
}

func TestProcessSignal_AuditLogFailure_NonFatal(t *testing.T) {
	t.Parallel()

	broker := &mockBroker{}
	riskEng := &mockRiskEngine{
		isKillSwitchActiveFn: func(_ context.Context) (bool, error) {
			return true, nil
		},
	}
	orderRepo := &mockOrderRepo{}
	positionRepo := &mockPositionRepo{}
	tradeRepo := &mockTradeRepo{}
	auditRepo := &mockAuditLogRepo{
		createFn: func(_ context.Context, _ *domain.AuditLogEntry) error {
			return errors.New("audit repo down")
		},
	}

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	// Kill switch is active, so ProcessSignal will try to audit and then return error.
	// The audit failure itself should be non-fatal (logged, not propagated).
	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
		uuid.New(),
		uuid.New(),
	)
	// Error should be about kill switch, not about audit failure.
	if err == nil {
		t.Fatal("expected kill switch error")
	}
	if !contains(err.Error(), "kill switch") {
		t.Errorf("expected kill switch error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
