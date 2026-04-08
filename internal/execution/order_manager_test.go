package execution_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
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
	if r.updateFn != nil {
		return r.updateFn(ctx, position)
	}

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

func TestProcessSignal_BuildsPortfolioForRiskChecks(t *testing.T) {
	equity := 10_000.0
	aaplPrice := 160.0
	balance := execution.Balance{Currency: "USD", Cash: 7_400, BuyingPower: 7_400, Equity: equity}
	openPositions := []domain.Position{
		{Ticker: "AAPL", Quantity: 10, AvgEntry: 150, CurrentPrice: &aaplPrice},
		{Ticker: "MSFT", Quantity: 20, AvgEntry: 50},
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

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
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

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

	err := mgr.ProcessSignal(
		context.Background(),
		defaultSignal(),
		defaultPlan(),
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

func TestProcessSignal_SellSignal(t *testing.T) {
	broker := &mockBroker{}
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

	if len(orderRepo.orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orderRepo.orders))
	}

	if orderRepo.orders[0].Side != domain.OrderSideSell {
		t.Errorf("expected sell side, got %s", orderRepo.orders[0].Side)
	}

	if len(positionRepo.positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positionRepo.positions))
	}

	if positionRepo.positions[0].Side != domain.PositionSideShort {
		t.Errorf("expected short position, got %s", positionRepo.positions[0].Side)
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

	mgr := newTestOrderManager(broker, riskEng, orderRepo, positionRepo, tradeRepo, auditRepo)

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
