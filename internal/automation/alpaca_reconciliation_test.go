package automation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type alpacaReconciliationBrokerStub struct {
	positions []domain.Position
	orders    []BrokerOrderSnapshot
	fills     []BrokerFillSnapshot

	positionsErr error
	ordersErr    error
	fillsErr     error
}

func (s *alpacaReconciliationBrokerStub) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if s.positionsErr != nil {
		return nil, s.positionsErr
	}
	out := make([]domain.Position, len(s.positions))
	copy(out, s.positions)
	return out, nil
}

func (s *alpacaReconciliationBrokerStub) ListOrders(ctx context.Context) ([]BrokerOrderSnapshot, error) {
	if s.ordersErr != nil {
		return nil, s.ordersErr
	}
	out := make([]BrokerOrderSnapshot, len(s.orders))
	copy(out, s.orders)
	return out, nil
}

func (s *alpacaReconciliationBrokerStub) ListFills(ctx context.Context) ([]BrokerFillSnapshot, error) {
	if s.fillsErr != nil {
		return nil, s.fillsErr
	}
	out := make([]BrokerFillSnapshot, len(s.fills))
	copy(out, s.fills)
	return out, nil
}

type recordingStrategyRepo struct {
	list []domain.Strategy
	err  error
}

func (r *recordingStrategyRepo) Create(context.Context, *domain.Strategy) error { return nil }
func (r *recordingStrategyRepo) Get(context.Context, uuid.UUID) (*domain.Strategy, error) {
	return nil, repository.ErrNotFound
}
func (r *recordingStrategyRepo) List(context.Context, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make([]domain.Strategy, len(r.list))
	copy(out, r.list)
	return out, nil
}
func (r *recordingStrategyRepo) Count(context.Context, repository.StrategyFilter) (int, error) {
	return len(r.list), nil
}
func (r *recordingStrategyRepo) Update(context.Context, *domain.Strategy) error { return nil }
func (r *recordingStrategyRepo) Delete(context.Context, uuid.UUID) error        { return nil }
func (r *recordingStrategyRepo) ValidateConfig(context.Context, domain.MarketType, []byte) error {
	return nil
}
func (r *recordingStrategyRepo) GetByTicker(context.Context, string, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	return nil, nil
}
func (r *recordingStrategyRepo) CountByTicker(context.Context, string, repository.StrategyFilter) (int, error) {
	return 0, nil
}
func (r *recordingStrategyRepo) UpdateThesis(context.Context, uuid.UUID, json.RawMessage) error {
	return nil
}
func (r *recordingStrategyRepo) GetThesisRaw(context.Context, uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type recordingOrderRepo struct {
	byExternalID map[string]*domain.Order
	created      []*domain.Order
	updated      []*domain.Order
	list         []*domain.Order
}

func newRecordingOrderRepo(existing ...*domain.Order) *recordingOrderRepo {
	idx := make(map[string]*domain.Order)
	list := make([]*domain.Order, 0, len(existing))
	for _, order := range existing {
		cloned := cloneOrder(order)
		if cloned.ExternalID != "" {
			idx[cloned.ExternalID] = cloned
		}
		list = append(list, cloned)
	}
	return &recordingOrderRepo{byExternalID: idx, list: list}
}

func (r *recordingOrderRepo) Create(_ context.Context, order *domain.Order) error {
	cloned := cloneOrder(order)
	if cloned.ID == uuid.Nil {
		cloned.ID = uuid.New()
	}
	if cloned.CreatedAt.IsZero() {
		cloned.CreatedAt = time.Now().UTC()
	}
	*order = *cloned
	r.created = append(r.created, cloneOrder(cloned))
	r.list = append(r.list, cloned)
	if cloned.ExternalID != "" {
		r.byExternalID[cloned.ExternalID] = cloned
	}
	return nil
}

func (r *recordingOrderRepo) Get(_ context.Context, id uuid.UUID) (*domain.Order, error) {
	for _, order := range r.list {
		if order.ID == id {
			return cloneOrder(order), nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *recordingOrderRepo) List(_ context.Context, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error) {
	var filtered []domain.Order
	for _, order := range r.list {
		if filter.Broker != "" && order.Broker != filter.Broker {
			continue
		}
		if filter.Ticker != "" && order.Ticker != filter.Ticker {
			continue
		}
		filtered = append(filtered, *cloneOrder(order))
	}
	return paginateOrders(filtered, limit, offset), nil
}

func (r *recordingOrderRepo) Count(ctx context.Context, filter repository.OrderFilter) (int, error) {
	orders, err := r.List(ctx, filter, 0, 0)
	if err != nil {
		return 0, err
	}
	return len(orders), nil
}

func (r *recordingOrderRepo) Update(_ context.Context, order *domain.Order) error {
	for i, existing := range r.list {
		if existing.ID == order.ID {
			cloned := cloneOrder(order)
			r.list[i] = cloned
			if cloned.ExternalID != "" {
				r.byExternalID[cloned.ExternalID] = cloned
			}
			r.updated = append(r.updated, cloneOrder(cloned))
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *recordingOrderRepo) Delete(_ context.Context, id uuid.UUID) error { return nil }
func (r *recordingOrderRepo) GetByStrategy(_ context.Context, _ uuid.UUID, _ repository.OrderFilter, _, _ int) ([]domain.Order, error) {
	return nil, nil
}
func (r *recordingOrderRepo) GetByRun(_ context.Context, _ uuid.UUID, _ repository.OrderFilter, _, _ int) ([]domain.Order, error) {
	return nil, nil
}

type recordingPositionRepo struct {
	open    []*domain.Position
	created []*domain.Position
	updated []*domain.Position
}

func newRecordingPositionRepo(existing ...*domain.Position) *recordingPositionRepo {
	open := make([]*domain.Position, 0, len(existing))
	for _, position := range existing {
		open = append(open, clonePosition(position))
	}
	return &recordingPositionRepo{open: open}
}

func (r *recordingPositionRepo) Create(_ context.Context, position *domain.Position) error {
	cloned := clonePosition(position)
	if cloned.ID == uuid.Nil {
		cloned.ID = uuid.New()
	}
	if cloned.OpenedAt.IsZero() {
		cloned.OpenedAt = time.Now().UTC()
	}
	*position = *cloned
	r.created = append(r.created, clonePosition(cloned))
	r.open = append(r.open, cloned)
	return nil
}

func (r *recordingPositionRepo) Get(_ context.Context, id uuid.UUID) (*domain.Position, error) {
	for _, position := range r.open {
		if position.ID == id {
			return clonePosition(position), nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *recordingPositionRepo) List(_ context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	var filtered []domain.Position
	for _, position := range r.open {
		if filter.Ticker != "" && position.Ticker != filter.Ticker {
			continue
		}
		filtered = append(filtered, *clonePosition(position))
	}
	return paginatePositions(filtered, limit, offset), nil
}

func (r *recordingPositionRepo) Count(ctx context.Context, filter repository.PositionFilter) (int, error) {
	positions, err := r.List(ctx, filter, 0, 0)
	if err != nil {
		return 0, err
	}
	return len(positions), nil
}

func (r *recordingPositionRepo) Update(_ context.Context, position *domain.Position) error {
	for i, existing := range r.open {
		if existing.ID == position.ID {
			cloned := clonePosition(position)
			r.open[i] = cloned
			r.updated = append(r.updated, clonePosition(cloned))
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *recordingPositionRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }

func (r *recordingPositionRepo) GetOpen(_ context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	var filtered []domain.Position
	for _, position := range r.open {
		if position.ClosedAt != nil {
			continue
		}
		if filter.Ticker != "" && position.Ticker != filter.Ticker {
			continue
		}
		filtered = append(filtered, *clonePosition(position))
	}
	return paginatePositions(filtered, limit, offset), nil
}

func (r *recordingPositionRepo) CountOpen(ctx context.Context, filter repository.PositionFilter) (int, error) {
	positions, err := r.GetOpen(ctx, filter, 0, 0)
	if err != nil {
		return 0, err
	}
	return len(positions), nil
}
func (r *recordingPositionRepo) GetByStrategy(_ context.Context, _ uuid.UUID, _ repository.PositionFilter, _, _ int) ([]domain.Position, error) {
	return nil, nil
}

type recordingTradeRepo struct {
	created           []*domain.Trade
	byOrderExternalID map[string][]*domain.Trade
	orders            *recordingOrderRepo
}

func newRecordingTradeRepo(orders *recordingOrderRepo) *recordingTradeRepo {
	return &recordingTradeRepo{orders: orders, byOrderExternalID: map[string][]*domain.Trade{}}
}

func (r *recordingTradeRepo) seedOrderExternalID(externalID string, trades ...*domain.Trade) {
	for _, trade := range trades {
		cloned := cloneTrade(trade)
		r.byOrderExternalID[externalID] = append(r.byOrderExternalID[externalID], cloned)
	}
}

func (r *recordingTradeRepo) Create(_ context.Context, trade *domain.Trade) error {
	cloned := cloneTrade(trade)
	if cloned.ID == uuid.Nil {
		cloned.ID = uuid.New()
	}
	if cloned.CreatedAt.IsZero() {
		cloned.CreatedAt = time.Now().UTC()
	}
	*trade = *cloned
	r.created = append(r.created, cloneTrade(cloned))
	if trade.OrderID != nil && r.orders != nil {
		if order, err := r.orders.Get(context.Background(), *trade.OrderID); err == nil {
			r.byOrderExternalID[order.ExternalID] = append(r.byOrderExternalID[order.ExternalID], cloneTrade(cloned))
		}
	}
	return nil
}

func (r *recordingTradeRepo) List(_ context.Context, _ repository.TradeFilter, _, _ int) ([]domain.Trade, error) {
	var trades []domain.Trade
	for _, bucket := range r.byOrderExternalID {
		for _, trade := range bucket {
			trades = append(trades, *cloneTrade(trade))
		}
	}
	return trades, nil
}

func (r *recordingTradeRepo) Count(ctx context.Context, filter repository.TradeFilter) (int, error) {
	trades, err := r.List(ctx, filter, 0, 0)
	if err != nil {
		return 0, err
	}
	return len(trades), nil
}

func (r *recordingTradeRepo) GetByOrder(_ context.Context, orderID uuid.UUID, _ repository.TradeFilter, _, _ int) ([]domain.Trade, error) {
	if r.orders == nil {
		return nil, nil
	}
	order, err := r.orders.Get(context.Background(), orderID)
	if err != nil {
		return nil, err
	}
	var trades []domain.Trade
	for _, trade := range r.byOrderExternalID[order.ExternalID] {
		trades = append(trades, *cloneTrade(trade))
	}
	return trades, nil
}

func (r *recordingTradeRepo) GetByPosition(_ context.Context, _ uuid.UUID, _ repository.TradeFilter, _, _ int) ([]domain.Trade, error) {
	return nil, nil
}

type auditLogRepoStub struct {
	entries []*domain.AuditLogEntry
}

func (r *auditLogRepoStub) Create(_ context.Context, entry *domain.AuditLogEntry) error {
	cloned := *entry
	if len(entry.Details) > 0 {
		cloned.Details = append([]byte(nil), entry.Details...)
	}
	r.entries = append(r.entries, &cloned)
	return nil
}
func (r *auditLogRepoStub) Query(context.Context, repository.AuditLogFilter, int, int) ([]domain.AuditLogEntry, error) {
	var out []domain.AuditLogEntry
	for _, entry := range r.entries {
		out = append(out, *entry)
	}
	return out, nil
}
func (r *auditLogRepoStub) Count(context.Context, repository.AuditLogFilter) (int, error) {
	return len(r.entries), nil
}

func TestAlpacaReconcilerReconcile_ImportsOrdersPositionsAndFills(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	strategies := &recordingStrategyRepo{list: []domain.Strategy{{
		ID:         strategyID,
		Name:       "SNAL strategy",
		Ticker:     "SNAL",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		IsPaper:    false,
	}}}
	orders := newRecordingOrderRepo()
	positions := newRecordingPositionRepo()
	trades := newRecordingTradeRepo(orders)
	broker := &alpacaReconciliationBrokerStub{
		positions: []domain.Position{{
			Ticker:        "SNAL",
			Side:          domain.PositionSideLong,
			Quantity:      200,
			AvgEntry:      0.92,
			CurrentPrice:  float64Ptr(0.7611),
			UnrealizedPnL: float64Ptr(-31.78),
		}},
		orders: []BrokerOrderSnapshot{{
			ExternalID:     "e8405b49-6140-46b5-a78a-7305f1086cd1",
			Ticker:         "SNAL",
			Side:           domain.OrderSideBuy,
			OrderType:      domain.OrderTypeLimit,
			Quantity:       200,
			FilledQuantity: 200,
			FilledAvgPrice: float64Ptr(0.92),
			LimitPrice:     float64Ptr(50),
			Status:         domain.OrderStatusFilled,
			SubmittedAt:    timePtr(time.Date(2026, 4, 15, 19, 20, 2, 451351000, time.UTC)),
			FilledAt:       timePtr(time.Date(2026, 4, 15, 19, 20, 4, 943982000, time.UTC)),
			Broker:         "alpaca",
			StrategyIDHint: &strategyID,
		}},
		fills: []BrokerFillSnapshot{{
			ActivityID:  "20260415152002662::04a8500c-8992-4db5-afca-6cd1b74629be",
			ExternalID:  "e8405b49-6140-46b5-a78a-7305f1086cd1",
			Ticker:      "SNAL",
			Side:        domain.OrderSideBuy,
			Quantity:    186,
			Price:       0.92,
			ExecutedAt:  time.Date(2026, 4, 15, 19, 20, 2, 662680000, time.UTC),
			OrderStatus: domain.OrderStatusPartial,
		}, {
			ActivityID:  "20260415152004943::352a24e1-0d2b-469a-9765-43ed6889ca48",
			ExternalID:  "e8405b49-6140-46b5-a78a-7305f1086cd1",
			Ticker:      "SNAL",
			Side:        domain.OrderSideBuy,
			Quantity:    3,
			Price:       0.92,
			ExecutedAt:  time.Date(2026, 4, 15, 19, 20, 4, 943982000, time.UTC),
			OrderStatus: domain.OrderStatusFilled,
		}},
	}

	audit := &auditLogRepoStub{}
	reconciler := NewAlpacaReconciler(AlpacaReconcilerDeps{
		Broker:       broker,
		StrategyRepo: strategies,
		OrderRepo:    orders,
		PositionRepo: positions,
		TradeRepo:    trades,
		AuditLogRepo: audit,
		Logger:       slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	})

	summary, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.OrdersCreated != 1 {
		t.Fatalf("OrdersCreated = %d, want 1", summary.OrdersCreated)
	}
	if summary.PositionsCreated != 1 {
		t.Fatalf("PositionsCreated = %d, want 1", summary.PositionsCreated)
	}
	if summary.TradesCreated != 2 {
		t.Fatalf("TradesCreated = %d, want 2", summary.TradesCreated)
	}
	if len(orders.created) != 1 {
		t.Fatalf("len(orders.created) = %d, want 1", len(orders.created))
	}
	if orders.created[0].ExternalID != "e8405b49-6140-46b5-a78a-7305f1086cd1" {
		t.Fatalf("created order external id = %q", orders.created[0].ExternalID)
	}
	if orders.created[0].StrategyID == nil || *orders.created[0].StrategyID != strategyID {
		t.Fatalf("created order strategy id = %v, want %s", orders.created[0].StrategyID, strategyID)
	}
	if len(positions.created) != 1 {
		t.Fatalf("len(positions.created) = %d, want 1", len(positions.created))
	}
	if positions.created[0].Ticker != "SNAL" {
		t.Fatalf("created position ticker = %q, want SNAL", positions.created[0].Ticker)
	}
	if len(trades.created) != 2 {
		t.Fatalf("len(trades.created) = %d, want 2", len(trades.created))
	}
	for _, trade := range trades.created {
		if trade.PositionID == nil {
			t.Fatalf("trade %+v missing position id", trade)
		}
		if trade.OrderID == nil {
			t.Fatalf("trade %+v missing order id", trade)
		}
	}
	if len(audit.entries) != 1 {
		t.Fatalf("len(audit.entries) = %d, want 1", len(audit.entries))
	}
	if audit.entries[0].EventType != "alpaca_reconcile.completed" {
		t.Fatalf("audit event type = %q, want alpaca_reconcile.completed", audit.entries[0].EventType)
	}
	var details map[string]any
	if err := json.Unmarshal(audit.entries[0].Details, &details); err != nil {
		t.Fatalf("unmarshal audit details: %v", err)
	}
	if got := details["orders_created"]; got != float64(1) {
		t.Fatalf("audit orders_created = %v, want 1", got)
	}
	if got := details["positions_created"]; got != float64(1) {
		t.Fatalf("audit positions_created = %v, want 1", got)
	}
	if got := details["trades_created"]; got != float64(2) {
		t.Fatalf("audit trades_created = %v, want 2", got)
	}
}

func TestAlpacaReconcilerReconcile_UpdatesExistingRecordsAndSkipsKnownFills(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	existingOrderID := uuid.New()
	existingPositionID := uuid.New()
	existingOrder := &domain.Order{
		ID:             existingOrderID,
		StrategyID:     &strategyID,
		ExternalID:     "existing-order",
		Ticker:         "SNAL",
		Side:           domain.OrderSideBuy,
		OrderType:      domain.OrderTypeLimit,
		Quantity:       200,
		FilledQuantity: 186,
		Status:         domain.OrderStatusPartial,
		Broker:         "alpaca",
	}
	existingPosition := &domain.Position{
		ID:         existingPositionID,
		StrategyID: &strategyID,
		Ticker:     "SNAL",
		Side:       domain.PositionSideLong,
		Quantity:   186,
		AvgEntry:   0.92,
	}
	strategies := &recordingStrategyRepo{list: []domain.Strategy{{
		ID:         strategyID,
		Name:       "SNAL strategy",
		Ticker:     "SNAL",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		IsPaper:    false,
	}}}
	orders := newRecordingOrderRepo(existingOrder)
	positions := newRecordingPositionRepo(existingPosition)
	trades := newRecordingTradeRepo(orders)
	trades.seedOrderExternalID("existing-order", &domain.Trade{
		ID:         uuid.New(),
		OrderID:    &existingOrderID,
		PositionID: &existingPositionID,
		Ticker:     "SNAL",
		Side:       domain.OrderSideBuy,
		Quantity:   186,
		Price:      0.92,
		ExecutedAt: time.Date(2026, 4, 15, 19, 20, 2, 662680000, time.UTC),
	})
	broker := &alpacaReconciliationBrokerStub{
		positions: []domain.Position{{
			Ticker:        "SNAL",
			Side:          domain.PositionSideLong,
			Quantity:      200,
			AvgEntry:      0.92,
			CurrentPrice:  float64Ptr(0.7611),
			UnrealizedPnL: float64Ptr(-31.78),
		}},
		orders: []BrokerOrderSnapshot{{
			ExternalID:     "existing-order",
			Ticker:         "SNAL",
			Side:           domain.OrderSideBuy,
			OrderType:      domain.OrderTypeLimit,
			Quantity:       200,
			FilledQuantity: 200,
			FilledAvgPrice: float64Ptr(0.92),
			Status:         domain.OrderStatusFilled,
			Broker:         "alpaca",
			StrategyIDHint: &strategyID,
			SubmittedAt:    timePtr(time.Date(2026, 4, 15, 19, 20, 2, 451351000, time.UTC)),
			FilledAt:       timePtr(time.Date(2026, 4, 15, 19, 20, 4, 943982000, time.UTC)),
		}},
		fills: []BrokerFillSnapshot{{
			ActivityID:  "known-fill",
			ExternalID:  "existing-order",
			Ticker:      "SNAL",
			Side:        domain.OrderSideBuy,
			Quantity:    186,
			Price:       0.92,
			ExecutedAt:  time.Date(2026, 4, 15, 19, 20, 2, 662680000, time.UTC),
			OrderStatus: domain.OrderStatusPartial,
		}, {
			ActivityID:  "new-fill",
			ExternalID:  "existing-order",
			Ticker:      "SNAL",
			Side:        domain.OrderSideBuy,
			Quantity:    14,
			Price:       0.92,
			ExecutedAt:  time.Date(2026, 4, 15, 19, 20, 4, 943982000, time.UTC),
			OrderStatus: domain.OrderStatusFilled,
		}},
	}

	audit := &auditLogRepoStub{}
	reconciler := NewAlpacaReconciler(AlpacaReconcilerDeps{
		Broker:       broker,
		StrategyRepo: strategies,
		OrderRepo:    orders,
		PositionRepo: positions,
		TradeRepo:    trades,
		AuditLogRepo: audit,
		Logger:       slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	})

	summary, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.OrdersUpdated != 1 {
		t.Fatalf("OrdersUpdated = %d, want 1", summary.OrdersUpdated)
	}
	if summary.PositionsUpdated != 1 {
		t.Fatalf("PositionsUpdated = %d, want 1", summary.PositionsUpdated)
	}
	if summary.TradesCreated != 1 {
		t.Fatalf("TradesCreated = %d, want 1", summary.TradesCreated)
	}
	if len(orders.updated) != 1 {
		t.Fatalf("len(orders.updated) = %d, want 1", len(orders.updated))
	}
	if orders.updated[0].Status != domain.OrderStatusFilled {
		t.Fatalf("updated order status = %q, want filled", orders.updated[0].Status)
	}
	if orders.updated[0].FilledQuantity != 200 {
		t.Fatalf("updated order filled quantity = %v, want 200", orders.updated[0].FilledQuantity)
	}
	if len(positions.updated) != 1 {
		t.Fatalf("len(positions.updated) = %d, want 1", len(positions.updated))
	}
	if positions.updated[0].Quantity != 200 {
		t.Fatalf("updated position quantity = %v, want 200", positions.updated[0].Quantity)
	}
	if len(trades.created) != 1 {
		t.Fatalf("len(trades.created) = %d, want 1", len(trades.created))
	}
	if trades.created[0].Quantity != 14 {
		t.Fatalf("new trade quantity = %v, want 14", trades.created[0].Quantity)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("len(audit.entries) = %d, want 1", len(audit.entries))
	}
	var details map[string]any
	if err := json.Unmarshal(audit.entries[0].Details, &details); err != nil {
		t.Fatalf("unmarshal audit details: %v", err)
	}
	if got := details["orders_updated"]; got != float64(1) {
		t.Fatalf("audit orders_updated = %v, want 1", got)
	}
	if got := details["positions_updated"]; got != float64(1) {
		t.Fatalf("audit positions_updated = %v, want 1", got)
	}
	if got := details["trades_created"]; got != float64(1) {
		t.Fatalf("audit trades_created = %v, want 1", got)
	}
}

func TestAlpacaReconcilerReconcile_ReturnsErrorWhenBrokerFails(t *testing.T) {
	t.Parallel()

	reconciler := NewAlpacaReconciler(AlpacaReconcilerDeps{
		Broker:       &alpacaReconciliationBrokerStub{positionsErr: errors.New("boom")},
		OrderRepo:    newRecordingOrderRepo(),
		PositionRepo: newRecordingPositionRepo(),
		TradeRepo:    newRecordingTradeRepo(newRecordingOrderRepo()),
		Logger:       slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	})

	_, err := reconciler.Reconcile(context.Background())
	if err == nil {
		t.Fatal("Reconcile() error = nil, want non-nil")
	}
	if got := err.Error(); got != "alpaca_reconcile: fetch positions: boom" {
		t.Fatalf("Reconcile() error = %q, want wrapped fetch positions error", got)
	}
}

func TestAlpacaReconcilerVerify_NormalizesBrokerOrderPrecisionToStorage(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	orders := newRecordingOrderRepo(&domain.Order{
		ID:             uuid.New(),
		StrategyID:     &strategyID,
		ExternalID:     "expired-order",
		Ticker:         "DAL",
		Side:           domain.OrderSideBuy,
		OrderType:      domain.OrderTypeLimit,
		Quantity:       215.01595699,
		FilledQuantity: 0,
		Status:         domain.OrderStatusCancelled,
		Broker:         "alpaca",
	})
	reconciler := NewAlpacaReconciler(AlpacaReconcilerDeps{
		Broker: &alpacaReconciliationBrokerStub{
			orders: []BrokerOrderSnapshot{{
				ExternalID:     "expired-order",
				Ticker:         "DAL",
				Side:           domain.OrderSideBuy,
				OrderType:      domain.OrderTypeLimit,
				Quantity:       215.015956989,
				FilledQuantity: 0,
				Status:         domain.OrderStatusCancelled,
				Broker:         "alpaca",
			}},
		},
		OrderRepo:    orders,
		PositionRepo: newRecordingPositionRepo(),
		TradeRepo:    newRecordingTradeRepo(orders),
		Logger:       slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	})

	report, err := reconciler.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !report.Verified {
		t.Fatalf("Verified = false, mismatches = %#v", report.Mismatches)
	}
	if len(report.Mismatches) != 0 {
		t.Fatalf("len(Mismatches) = %d, want 0", len(report.Mismatches))
	}
}

func TestAlpacaReconcilerVerify_IgnoresVolatilePositionMarkToMarketFields(t *testing.T) {
	t.Parallel()

	positions := newRecordingPositionRepo(&domain.Position{
		ID:            uuid.New(),
		Ticker:        "SNAL",
		Side:          domain.PositionSideLong,
		Quantity:      200,
		AvgEntry:      0.92,
		CurrentPrice:  float64Ptr(0.7727),
		UnrealizedPnL: float64Ptr(-29.46),
	})
	orders := newRecordingOrderRepo()
	reconciler := NewAlpacaReconciler(AlpacaReconcilerDeps{
		Broker: &alpacaReconciliationBrokerStub{
			positions: []domain.Position{{
				Ticker:        "SNAL",
				Side:          domain.PositionSideLong,
				Quantity:      200,
				AvgEntry:      0.92,
				CurrentPrice:  float64Ptr(0.7695),
				UnrealizedPnL: float64Ptr(-30.10),
			}},
		},
		OrderRepo:    orders,
		PositionRepo: positions,
		TradeRepo:    newRecordingTradeRepo(orders),
		Logger:       slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	})

	report, err := reconciler.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !report.Verified {
		t.Fatalf("Verified = false, mismatches = %#v", report.Mismatches)
	}
	if len(report.Mismatches) != 0 {
		t.Fatalf("len(Mismatches) = %d, want 0", len(report.Mismatches))
	}
}

func TestAlpacaReconcilerReconcile_CreatesDistinctTradesForDuplicateExecutionFieldsWhenActivityIDsDiffer(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	orderID := uuid.New()
	orders := newRecordingOrderRepo(&domain.Order{
		ID:         orderID,
		StrategyID: &strategyID,
		ExternalID: "order-1",
		Ticker:     "SNAL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   20,
		Status:     domain.OrderStatusPartial,
		Broker:     "alpaca",
	})
	trades := newRecordingTradeRepo(orders)
	reconciler := NewAlpacaReconciler(AlpacaReconcilerDeps{
		Broker: &alpacaReconciliationBrokerStub{
			orders: []BrokerOrderSnapshot{{
				ExternalID:     "order-1",
				Ticker:         "SNAL",
				Side:           domain.OrderSideBuy,
				OrderType:      domain.OrderTypeLimit,
				Quantity:       20,
				FilledQuantity: 20,
				Status:         domain.OrderStatusFilled,
				Broker:         "alpaca",
			}},
			fills: []BrokerFillSnapshot{{
				ActivityID:  "fill-1",
				ExternalID:  "order-1",
				Ticker:      "SNAL",
				Side:        domain.OrderSideBuy,
				Quantity:    10,
				Price:       0.92,
				ExecutedAt:  time.Date(2026, 4, 15, 19, 20, 2, 662680000, time.UTC),
				OrderStatus: domain.OrderStatusPartial,
			}, {
				ActivityID:  "fill-2",
				ExternalID:  "order-1",
				Ticker:      "SNAL",
				Side:        domain.OrderSideBuy,
				Quantity:    10,
				Price:       0.92,
				ExecutedAt:  time.Date(2026, 4, 15, 19, 20, 2, 662680000, time.UTC),
				OrderStatus: domain.OrderStatusFilled,
			}},
		},
		OrderRepo:    orders,
		PositionRepo: newRecordingPositionRepo(),
		TradeRepo:    trades,
		Logger:       slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	})

	summary, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if summary.TradesCreated != 2 {
		t.Fatalf("TradesCreated = %d, want 2", summary.TradesCreated)
	}
	if len(trades.created) != 2 {
		t.Fatalf("len(trades.created) = %d, want 2", len(trades.created))
	}
	if trades.created[0].ExternalID == trades.created[1].ExternalID {
		t.Fatalf("created trade external ids = %q and %q, want distinct activity ids", trades.created[0].ExternalID, trades.created[1].ExternalID)
	}
}

func TestAlpacaReconcilerVerify_UsesTradeExternalIDToDetectDuplicateExecutionFills(t *testing.T) {
	t.Parallel()

	trade := &domain.Trade{
		ID:         uuid.New(),
		ExternalID: "fill-1",
		Ticker:     "SNAL",
		Side:       domain.OrderSideBuy,
		Quantity:   10,
		Price:      0.92,
		ExecutedAt: time.Date(2026, 4, 15, 19, 20, 2, 662680000, time.UTC),
	}
	orders := newRecordingOrderRepo(&domain.Order{
		ID:         uuid.New(),
		ExternalID: "order-1",
		Ticker:     "SNAL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   20,
		Status:     domain.OrderStatusFilled,
		Broker:     "alpaca",
	})
	trades := newRecordingTradeRepo(orders)
	trades.seedOrderExternalID("order-1", trade)

	reconciler := NewAlpacaReconciler(AlpacaReconcilerDeps{
		Broker: &alpacaReconciliationBrokerStub{
			fills: []BrokerFillSnapshot{{
				ActivityID:  "fill-1",
				ExternalID:  "order-1",
				Ticker:      "SNAL",
				Side:        domain.OrderSideBuy,
				Quantity:    10,
				Price:       0.92,
				ExecutedAt:  trade.ExecutedAt,
				OrderStatus: domain.OrderStatusPartial,
			}, {
				ActivityID:  "fill-2",
				ExternalID:  "order-1",
				Ticker:      "SNAL",
				Side:        domain.OrderSideBuy,
				Quantity:    10,
				Price:       0.92,
				ExecutedAt:  trade.ExecutedAt,
				OrderStatus: domain.OrderStatusFilled,
			}},
		},
		OrderRepo:    orders,
		PositionRepo: newRecordingPositionRepo(),
		TradeRepo:    trades,
		Logger:       slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	})

	report, err := reconciler.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if report.Verified {
		t.Fatalf("Verified = true, want false with missing duplicate fill trade")
	}
	if report.MissingTrades != 1 {
		t.Fatalf("MissingTrades = %d, want 1", report.MissingTrades)
	}
}

func TestJobOrchestratorRegisterAll_IncludesAlpacaReconcileJob(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.RegisterAll()

	status := singleJobStatus(t, orch, "alpaca_reconcile")
	if status.Name != "alpaca_reconcile" {
		t.Fatalf("status.Name = %q, want alpaca_reconcile", status.Name)
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

func cloneOrder(order *domain.Order) *domain.Order {
	if order == nil {
		return nil
	}
	cloned := *order
	if order.StrategyID != nil {
		id := *order.StrategyID
		cloned.StrategyID = &id
	}
	if order.PipelineRunID != nil {
		id := *order.PipelineRunID
		cloned.PipelineRunID = &id
	}
	if order.LimitPrice != nil {
		value := *order.LimitPrice
		cloned.LimitPrice = &value
	}
	if order.StopPrice != nil {
		value := *order.StopPrice
		cloned.StopPrice = &value
	}
	if order.FilledAvgPrice != nil {
		value := *order.FilledAvgPrice
		cloned.FilledAvgPrice = &value
	}
	if order.SubmittedAt != nil {
		value := *order.SubmittedAt
		cloned.SubmittedAt = &value
	}
	if order.FilledAt != nil {
		value := *order.FilledAt
		cloned.FilledAt = &value
	}
	return &cloned
}

func clonePosition(position *domain.Position) *domain.Position {
	if position == nil {
		return nil
	}
	cloned := *position
	if position.StrategyID != nil {
		id := *position.StrategyID
		cloned.StrategyID = &id
	}
	if position.CurrentPrice != nil {
		value := *position.CurrentPrice
		cloned.CurrentPrice = &value
	}
	if position.UnrealizedPnL != nil {
		value := *position.UnrealizedPnL
		cloned.UnrealizedPnL = &value
	}
	if position.StopLoss != nil {
		value := *position.StopLoss
		cloned.StopLoss = &value
	}
	if position.TakeProfit != nil {
		value := *position.TakeProfit
		cloned.TakeProfit = &value
	}
	if position.ClosedAt != nil {
		value := *position.ClosedAt
		cloned.ClosedAt = &value
	}
	if position.Expiry != nil {
		value := *position.Expiry
		cloned.Expiry = &value
	}
	return &cloned
}

func cloneTrade(trade *domain.Trade) *domain.Trade {
	if trade == nil {
		return nil
	}
	cloned := *trade
	cloned.ExternalID = trade.ExternalID
	if trade.OrderID != nil {
		id := *trade.OrderID
		cloned.OrderID = &id
	}
	if trade.PositionID != nil {
		id := *trade.PositionID
		cloned.PositionID = &id
	}
	return &cloned
}

func paginateOrders(items []domain.Order, limit, offset int) []domain.Order {
	if offset >= len(items) {
		return nil
	}
	if limit <= 0 {
		limit = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func paginatePositions(items []domain.Position, limit, offset int) []domain.Position {
	if offset >= len(items) {
		return nil
	}
	if limit <= 0 {
		limit = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func float64Ptr(v float64) *float64  { return &v }
func timePtr(v time.Time) *time.Time { return &v }
