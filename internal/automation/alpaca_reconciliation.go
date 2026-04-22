package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// AlpacaReconciliationBroker fetches current broker snapshots needed for local reconciliation.
type AlpacaReconciliationBroker interface {
	GetPositions(ctx context.Context) ([]domain.Position, error)
	ListOrders(ctx context.Context) ([]BrokerOrderSnapshot, error)
	ListFills(ctx context.Context) ([]BrokerFillSnapshot, error)
}

// StrategyLookupRepository is the narrow strategy dependency needed by reconciliation.
type StrategyLookupRepository interface {
	List(ctx context.Context, filter repository.StrategyFilter, limit, offset int) ([]domain.Strategy, error)
}

// OrderPersistence is the narrow order repository surface needed by reconciliation.
type OrderPersistence interface {
	Create(ctx context.Context, order *domain.Order) error
	List(ctx context.Context, filter repository.OrderFilter, limit, offset int) ([]domain.Order, error)
	Update(ctx context.Context, order *domain.Order) error
}

// PositionPersistence is the narrow position repository surface needed by reconciliation.
type PositionPersistence interface {
	Create(ctx context.Context, position *domain.Position) error
	GetOpen(ctx context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error)
	Update(ctx context.Context, position *domain.Position) error
}

// TradePersistence is the narrow trade repository surface needed by reconciliation.
type TradePersistence interface {
	Create(ctx context.Context, trade *domain.Trade) error
	List(ctx context.Context, filter repository.TradeFilter, limit, offset int) ([]domain.Trade, error)
}

// BrokerOrderSnapshot captures the broker-facing order state needed to hydrate local orders.
type BrokerOrderSnapshot struct {
	ExternalID     string
	StrategyIDHint *uuid.UUID
	Ticker         string
	Side           domain.OrderSide
	OrderType      domain.OrderType
	Quantity       float64
	LimitPrice     *float64
	StopPrice      *float64
	FilledQuantity float64
	FilledAvgPrice *float64
	Status         domain.OrderStatus
	SubmittedAt    *time.Time
	FilledAt       *time.Time
	Broker         string
}

// BrokerFillSnapshot captures a single broker fill activity.
type BrokerFillSnapshot struct {
	ActivityID  string
	ExternalID  string
	Ticker      string
	Side        domain.OrderSide
	Quantity    float64
	Price       float64
	Fee         float64
	ExecutedAt  time.Time
	OrderStatus domain.OrderStatus
}

// AlpacaReconcilerDeps bundles repository and broker dependencies.
type AlpacaReconcilerDeps struct {
	Broker       AlpacaReconciliationBroker
	StrategyRepo StrategyLookupRepository
	OrderRepo    OrderPersistence
	PositionRepo PositionPersistence
	TradeRepo    TradePersistence
	AuditLogRepo repository.AuditLogRepository
	Logger       *slog.Logger
}

// AlpacaReconcileSummary reports how many local records changed during a run.
type AlpacaReconcileSummary struct {
	OrdersCreated    int
	OrdersUpdated    int
	PositionsCreated int
	PositionsUpdated int
	TradesCreated    int
}

type AlpacaVerificationMismatch struct {
	Entity  string   `json:"entity"`
	Key     string   `json:"key"`
	Details []string `json:"details,omitempty"`
}

type AlpacaVerificationReport struct {
	OrdersChecked    int                          `json:"orders_checked"`
	PositionsChecked int                          `json:"positions_checked"`
	FillsChecked     int                          `json:"fills_checked"`
	MissingOrders    int                          `json:"missing_orders"`
	MissingPositions int                          `json:"missing_positions"`
	MissingTrades    int                          `json:"missing_trades"`
	Mismatches       []AlpacaVerificationMismatch `json:"mismatches,omitempty"`
	Verified         bool                         `json:"verified"`
}

func (s AlpacaReconcileSummary) Map() map[string]int {
	return map[string]int{
		"orders_created":    s.OrdersCreated,
		"orders_updated":    s.OrdersUpdated,
		"positions_created": s.PositionsCreated,
		"positions_updated": s.PositionsUpdated,
		"trades_created":    s.TradesCreated,
	}
}

// AlpacaReconciler imports Alpaca broker state into local orders, positions, and trades tables.
type AlpacaReconciler struct {
	broker       AlpacaReconciliationBroker
	strategyRepo StrategyLookupRepository
	orderRepo    OrderPersistence
	positionRepo PositionPersistence
	tradeRepo    TradePersistence
	auditLogRepo repository.AuditLogRepository
	logger       *slog.Logger
}

func NewAlpacaReconciler(deps AlpacaReconcilerDeps) *AlpacaReconciler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &AlpacaReconciler{
		broker:       deps.Broker,
		strategyRepo: deps.StrategyRepo,
		orderRepo:    deps.OrderRepo,
		positionRepo: deps.PositionRepo,
		tradeRepo:    deps.TradeRepo,
		auditLogRepo: deps.AuditLogRepo,
		logger:       logger,
	}
}

func (r *AlpacaReconciler) Reconcile(ctx context.Context) (AlpacaReconcileSummary, error) {
	if r == nil || r.broker == nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: broker is required")
	}
	if r.orderRepo == nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: order repository is required")
	}
	if r.positionRepo == nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: position repository is required")
	}
	if r.tradeRepo == nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: trade repository is required")
	}

	positions, err := r.broker.GetPositions(ctx)
	if err != nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: fetch positions: %w", err)
	}
	orders, err := r.broker.ListOrders(ctx)
	if err != nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: fetch orders: %w", err)
	}
	fills, err := r.broker.ListFills(ctx)
	if err != nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: fetch fills: %w", err)
	}

	strategyByTicker, err := r.loadStrategyIndex(ctx)
	if err != nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: load strategy index: %w", err)
	}

	existingOrders, err := r.orderRepo.List(ctx, repository.OrderFilter{Broker: "alpaca"}, 1000, 0)
	if err != nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: list local orders: %w", err)
	}
	orderByExternalID := make(map[string]*domain.Order, len(existingOrders))
	for i := range existingOrders {
		order := existingOrders[i]
		if strings.TrimSpace(order.ExternalID) == "" {
			continue
		}
		cloned := order
		orderByExternalID[order.ExternalID] = &cloned
	}

	existingPositions, err := r.positionRepo.GetOpen(ctx, repository.PositionFilter{}, 1000, 0)
	if err != nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: list local positions: %w", err)
	}
	positionByTicker := make(map[string]*domain.Position, len(existingPositions))
	for i := range existingPositions {
		position := existingPositions[i]
		cloned := position
		positionByTicker[position.Ticker] = &cloned
	}

	existingTrades, err := r.tradeRepo.List(ctx, repository.TradeFilter{}, 5000, 0)
	if err != nil {
		return AlpacaReconcileSummary{}, fmt.Errorf("alpaca_reconcile: list local trades: %w", err)
	}
	existingTradeKeys := make(map[string]struct{}, len(existingTrades))
	for _, trade := range existingTrades {
		existingTradeKeys[tradeDedupeKey(trade)] = struct{}{}
	}
	fillLegacyKeyCounts := fillLegacyKeyCounts(fills)

	summary := AlpacaReconcileSummary{}

	for _, snapshot := range orders {
		strategyID := snapshot.StrategyIDHint
		if strategyID == nil {
			strategyID = strategyByTicker[snapshot.Ticker]
		}
		if existing, ok := orderByExternalID[snapshot.ExternalID]; ok {
			changed := applyOrderSnapshot(existing, snapshot, strategyID)
			if changed {
				if err := r.orderRepo.Update(ctx, existing); err != nil {
					return summary, fmt.Errorf("alpaca_reconcile: update order %s: %w", snapshot.ExternalID, err)
				}
				summary.OrdersUpdated++
			}
			continue
		}

		order := snapshotToOrder(snapshot, strategyID)
		if err := r.orderRepo.Create(ctx, order); err != nil {
			return summary, fmt.Errorf("alpaca_reconcile: create order %s: %w", snapshot.ExternalID, err)
		}
		orderByExternalID[snapshot.ExternalID] = order
		summary.OrdersCreated++
	}

	for _, snapshot := range positions {
		strategyID := strategyByTicker[snapshot.Ticker]
		if existing, ok := positionByTicker[snapshot.Ticker]; ok {
			changed := applyPositionSnapshot(existing, snapshot, strategyID)
			if changed {
				if err := r.positionRepo.Update(ctx, existing); err != nil {
					return summary, fmt.Errorf("alpaca_reconcile: update position %s: %w", snapshot.Ticker, err)
				}
				summary.PositionsUpdated++
			}
			continue
		}

		position := snapshotToPosition(snapshot, strategyID)
		if err := r.positionRepo.Create(ctx, position); err != nil {
			return summary, fmt.Errorf("alpaca_reconcile: create position %s: %w", snapshot.Ticker, err)
		}
		positionByTicker[position.Ticker] = position
		summary.PositionsCreated++
	}

	sort.Slice(fills, func(i, j int) bool {
		return fills[i].ExecutedAt.Before(fills[j].ExecutedAt)
	})
	for _, fill := range fills {
		fillKeys := dedupeKeysForFill(fill, fillLegacyKeyCounts)
		skip := false
		for _, key := range fillKeys {
			if _, ok := existingTradeKeys[key]; ok {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		order := orderByExternalID[fill.ExternalID]
		if order == nil {
			continue
		}
		position := positionByTicker[fill.Ticker]
		trade := &domain.Trade{
			OrderID:    &order.ID,
			PositionID: nil,
			ExternalID: strings.TrimSpace(fill.ActivityID),
			Ticker:     fill.Ticker,
			Side:       fill.Side,
			Quantity:   fill.Quantity,
			Price:      fill.Price,
			Fee:        fill.Fee,
			ExecutedAt: fill.ExecutedAt,
		}
		if position != nil {
			trade.PositionID = &position.ID
		}
		if err := r.tradeRepo.Create(ctx, trade); err != nil {
			return summary, fmt.Errorf("alpaca_reconcile: create trade for %s: %w", fill.ExternalID, err)
		}
		for _, key := range fillKeys {
			existingTradeKeys[key] = struct{}{}
		}
		summary.TradesCreated++
	}

	if err := r.recordAudit(ctx, "alpaca_reconcile.completed", summary.Map()); err != nil {
		r.logger.Warn("alpaca_reconcile: failed to record audit entry", slog.Any("error", err))
	}

	return summary, nil
}

func (r *AlpacaReconciler) Verify(ctx context.Context) (AlpacaVerificationReport, error) {
	if r == nil || r.broker == nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: broker is required")
	}
	if r.orderRepo == nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: order repository is required")
	}
	if r.positionRepo == nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: position repository is required")
	}
	if r.tradeRepo == nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: trade repository is required")
	}

	positions, err := r.broker.GetPositions(ctx)
	if err != nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: fetch positions: %w", err)
	}
	orders, err := r.broker.ListOrders(ctx)
	if err != nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: fetch orders: %w", err)
	}
	fills, err := r.broker.ListFills(ctx)
	if err != nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: fetch fills: %w", err)
	}

	localOrders, err := r.orderRepo.List(ctx, repository.OrderFilter{Broker: "alpaca"}, 1000, 0)
	if err != nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: list local orders: %w", err)
	}
	localPositions, err := r.positionRepo.GetOpen(ctx, repository.PositionFilter{}, 1000, 0)
	if err != nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: list local positions: %w", err)
	}
	localTrades, err := r.tradeRepo.List(ctx, repository.TradeFilter{}, 5000, 0)
	if err != nil {
		return AlpacaVerificationReport{}, fmt.Errorf("alpaca_reconcile: list local trades: %w", err)
	}

	orderByExternalID := make(map[string]domain.Order, len(localOrders))
	for _, order := range localOrders {
		if strings.TrimSpace(order.ExternalID) == "" {
			continue
		}
		orderByExternalID[order.ExternalID] = order
	}
	positionByTicker := make(map[string]domain.Position, len(localPositions))
	for _, position := range localPositions {
		positionByTicker[position.Ticker] = position
	}
	tradeByKey := make(map[string]domain.Trade, len(localTrades))
	for _, trade := range localTrades {
		tradeByKey[tradeDedupeKey(trade)] = trade
	}
	fillLegacyKeyCounts := fillLegacyKeyCounts(fills)

	report := AlpacaVerificationReport{
		OrdersChecked:    len(orders),
		PositionsChecked: len(positions),
		FillsChecked:     len(fills),
		Verified:         true,
	}

	for _, snapshot := range orders {
		localOrder, ok := orderByExternalID[snapshot.ExternalID]
		if !ok {
			report.MissingOrders++
			report.Mismatches = append(report.Mismatches, AlpacaVerificationMismatch{Entity: "order", Key: snapshot.ExternalID, Details: []string{"missing local order"}})
			continue
		}
		if fields := diffOrderSnapshot(localOrder, snapshot); len(fields) > 0 {
			report.Mismatches = append(report.Mismatches, AlpacaVerificationMismatch{Entity: "order", Key: snapshot.ExternalID, Details: fields})
		}
	}

	for _, snapshot := range positions {
		localPosition, ok := positionByTicker[snapshot.Ticker]
		if !ok {
			report.MissingPositions++
			report.Mismatches = append(report.Mismatches, AlpacaVerificationMismatch{Entity: "position", Key: snapshot.Ticker, Details: []string{"missing local position"}})
			continue
		}
		if fields := diffPositionSnapshot(localPosition, snapshot); len(fields) > 0 {
			report.Mismatches = append(report.Mismatches, AlpacaVerificationMismatch{Entity: "position", Key: snapshot.Ticker, Details: fields})
		}
	}

	for _, fill := range fills {
		fillKeys := dedupeKeysForFill(fill, fillLegacyKeyCounts)
		localTrade, ok := tradeByKey[fillKeys[0]]
		if !ok {
			for _, key := range fillKeys[1:] {
				if localTrade, ok = tradeByKey[key]; ok {
					break
				}
			}
		}
		if !ok {
			report.MissingTrades++
			report.Mismatches = append(report.Mismatches, AlpacaVerificationMismatch{Entity: "trade", Key: fillKeys[0], Details: []string{"missing local trade"}})
			continue
		}
		if fields := diffTradeFill(localTrade, fill); len(fields) > 0 {
			report.Mismatches = append(report.Mismatches, AlpacaVerificationMismatch{Entity: "trade", Key: fillKeys[0], Details: fields})
		}
	}

	report.Verified = report.MissingOrders == 0 && report.MissingPositions == 0 && report.MissingTrades == 0 && len(report.Mismatches) == 0
	if err := r.recordAudit(ctx, "alpaca_reconcile.verified", map[string]any{
		"orders_checked":    report.OrdersChecked,
		"positions_checked": report.PositionsChecked,
		"fills_checked":     report.FillsChecked,
		"missing_orders":    report.MissingOrders,
		"missing_positions": report.MissingPositions,
		"missing_trades":    report.MissingTrades,
		"verified":          report.Verified,
		"mismatches":        report.Mismatches,
	}); err != nil {
		r.logger.Warn("alpaca_reconcile: failed to record verification audit entry", slog.Any("error", err))
	}
	return report, nil
}

func (r *AlpacaReconciler) recordAudit(ctx context.Context, eventType string, details any) error {
	if r.auditLogRepo == nil {
		return nil
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	return r.auditLogRepo.Create(ctx, &domain.AuditLogEntry{
		EventType:  eventType,
		EntityType: "automation_job",
		Actor:      "alpaca_reconciler",
		Details:    payload,
	})
}

func (r *AlpacaReconciler) loadStrategyIndex(ctx context.Context) (map[string]*uuid.UUID, error) {
	if r.strategyRepo == nil {
		return map[string]*uuid.UUID{}, nil
	}
	strategies, err := r.strategyRepo.List(ctx, repository.StrategyFilter{Status: domain.StrategyStatusActive}, 1000, 0)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*uuid.UUID, len(strategies))
	for _, strategy := range strategies {
		id := strategy.ID
		result[strategy.Ticker] = &id
	}
	return result, nil
}

func snapshotToOrder(snapshot BrokerOrderSnapshot, strategyID *uuid.UUID) *domain.Order {
	return &domain.Order{
		StrategyID:     strategyID,
		ExternalID:     snapshot.ExternalID,
		Ticker:         snapshot.Ticker,
		Side:           snapshot.Side,
		OrderType:      snapshot.OrderType,
		Quantity:       snapshot.Quantity,
		LimitPrice:     cloneFloatPtr(snapshot.LimitPrice),
		StopPrice:      cloneFloatPtr(snapshot.StopPrice),
		FilledQuantity: snapshot.FilledQuantity,
		FilledAvgPrice: cloneFloatPtr(snapshot.FilledAvgPrice),
		Status:         snapshot.Status,
		Broker:         fallbackBroker(snapshot.Broker),
		SubmittedAt:    cloneTimePtr(snapshot.SubmittedAt),
		FilledAt:       cloneTimePtr(snapshot.FilledAt),
	}
}

func snapshotToPosition(snapshot domain.Position, strategyID *uuid.UUID) *domain.Position {
	return &domain.Position{
		StrategyID:    strategyID,
		Ticker:        snapshot.Ticker,
		Side:          snapshot.Side,
		Quantity:      snapshot.Quantity,
		AvgEntry:      snapshot.AvgEntry,
		CurrentPrice:  cloneFloatPtr(snapshot.CurrentPrice),
		UnrealizedPnL: cloneFloatPtr(snapshot.UnrealizedPnL),
	}
}

func applyOrderSnapshot(order *domain.Order, snapshot BrokerOrderSnapshot, strategyID *uuid.UUID) bool {
	changed := false
	if !uuidPtrEqual(order.StrategyID, strategyID) {
		order.StrategyID = cloneUUIDPtr(strategyID)
		changed = true
	}
	if order.Ticker != snapshot.Ticker {
		order.Ticker = snapshot.Ticker
		changed = true
	}
	if order.Side != snapshot.Side {
		order.Side = snapshot.Side
		changed = true
	}
	if order.OrderType != snapshot.OrderType {
		order.OrderType = snapshot.OrderType
		changed = true
	}
	if order.Quantity != snapshot.Quantity {
		order.Quantity = snapshot.Quantity
		changed = true
	}
	if !floatPtrEqual(order.LimitPrice, snapshot.LimitPrice) {
		order.LimitPrice = cloneFloatPtr(snapshot.LimitPrice)
		changed = true
	}
	if !floatPtrEqual(order.StopPrice, snapshot.StopPrice) {
		order.StopPrice = cloneFloatPtr(snapshot.StopPrice)
		changed = true
	}
	if order.FilledQuantity != snapshot.FilledQuantity {
		order.FilledQuantity = snapshot.FilledQuantity
		changed = true
	}
	if !floatPtrEqual(order.FilledAvgPrice, snapshot.FilledAvgPrice) {
		order.FilledAvgPrice = cloneFloatPtr(snapshot.FilledAvgPrice)
		changed = true
	}
	if order.Status != snapshot.Status {
		order.Status = snapshot.Status
		changed = true
	}
	broker := fallbackBroker(snapshot.Broker)
	if order.Broker != broker {
		order.Broker = broker
		changed = true
	}
	if !timePtrEqual(order.SubmittedAt, snapshot.SubmittedAt) {
		order.SubmittedAt = cloneTimePtr(snapshot.SubmittedAt)
		changed = true
	}
	if !timePtrEqual(order.FilledAt, snapshot.FilledAt) {
		order.FilledAt = cloneTimePtr(snapshot.FilledAt)
		changed = true
	}
	return changed
}

func applyPositionSnapshot(position *domain.Position, snapshot domain.Position, strategyID *uuid.UUID) bool {
	changed := false
	if !uuidPtrEqual(position.StrategyID, strategyID) {
		position.StrategyID = cloneUUIDPtr(strategyID)
		changed = true
	}
	if position.Side != snapshot.Side {
		position.Side = snapshot.Side
		changed = true
	}
	if position.Quantity != snapshot.Quantity {
		position.Quantity = snapshot.Quantity
		changed = true
	}
	if position.AvgEntry != snapshot.AvgEntry {
		position.AvgEntry = snapshot.AvgEntry
		changed = true
	}
	if !floatPtrEqual(position.CurrentPrice, snapshot.CurrentPrice) {
		position.CurrentPrice = cloneFloatPtr(snapshot.CurrentPrice)
		changed = true
	}
	if !floatPtrEqual(position.UnrealizedPnL, snapshot.UnrealizedPnL) {
		position.UnrealizedPnL = cloneFloatPtr(snapshot.UnrealizedPnL)
		changed = true
	}
	return changed
}

func fillDedupeKey(fill BrokerFillSnapshot) string {
	if activityID := strings.TrimSpace(fill.ActivityID); activityID != "" {
		return strings.Join([]string{"activity", activityID}, "|")
	}
	return tradeExecutionDedupeKey(fill.Ticker, fill.Side, fill.Quantity, fill.Price, fill.ExecutedAt)
}

func fillLegacyKey(fill BrokerFillSnapshot) string {
	return tradeExecutionDedupeKey(fill.Ticker, fill.Side, fill.Quantity, fill.Price, fill.ExecutedAt)
}

func fillLegacyKeyCounts(fills []BrokerFillSnapshot) map[string]int {
	counts := make(map[string]int, len(fills))
	for _, fill := range fills {
		counts[fillLegacyKey(fill)]++
	}
	return counts
}

func dedupeKeysForFill(fill BrokerFillSnapshot, legacyCounts map[string]int) []string {
	primary := fillDedupeKey(fill)
	legacy := fillLegacyKey(fill)
	if legacy == primary || legacyCounts[legacy] > 1 {
		return []string{primary}
	}
	return []string{primary, legacy}
}

func tradeDedupeKey(trade domain.Trade) string {
	if externalID := strings.TrimSpace(trade.ExternalID); externalID != "" {
		return strings.Join([]string{"activity", externalID}, "|")
	}
	return legacyTradeDedupeKey(trade)
}

func legacyTradeDedupeKey(trade domain.Trade) string {
	return tradeExecutionDedupeKey(trade.Ticker, trade.Side, trade.Quantity, trade.Price, trade.ExecutedAt)
}

func tradeExecutionDedupeKey(ticker string, side domain.OrderSide, quantity, price float64, executedAt time.Time) string {
	return strings.Join([]string{
		ticker,
		side.String(),
		formatFloat(quantity),
		formatFloat(price),
		executedAt.UTC().Format(time.RFC3339Nano),
	}, "|")
}

func fallbackBroker(broker string) string {
	trimmed := strings.TrimSpace(broker)
	if trimmed == "" {
		return "alpaca"
	}
	return trimmed
}

func diffOrderSnapshot(order domain.Order, snapshot BrokerOrderSnapshot) []string {
	var fields []string
	if order.Ticker != snapshot.Ticker {
		fields = append(fields, "ticker")
	}
	if order.Side != snapshot.Side {
		fields = append(fields, "side")
	}
	if order.OrderType != snapshot.OrderType {
		fields = append(fields, "order_type")
	}
	if !normalizedQuantityEqual(order.Quantity, snapshot.Quantity) {
		fields = append(fields, "quantity")
	}
	if !floatPtrEqual(order.LimitPrice, snapshot.LimitPrice) {
		fields = append(fields, "limit_price")
	}
	if !floatPtrEqual(order.StopPrice, snapshot.StopPrice) {
		fields = append(fields, "stop_price")
	}
	if !normalizedQuantityEqual(order.FilledQuantity, snapshot.FilledQuantity) {
		fields = append(fields, "filled_quantity")
	}
	if !floatPtrEqual(order.FilledAvgPrice, snapshot.FilledAvgPrice) {
		fields = append(fields, "filled_avg_price")
	}
	if order.Status != snapshot.Status {
		fields = append(fields, "status")
	}
	if order.Broker != fallbackBroker(snapshot.Broker) {
		fields = append(fields, "broker")
	}
	if !timePtrEqual(order.SubmittedAt, snapshot.SubmittedAt) {
		fields = append(fields, "submitted_at")
	}
	if !timePtrEqual(order.FilledAt, snapshot.FilledAt) {
		fields = append(fields, "filled_at")
	}
	return fields
}

func diffPositionSnapshot(position domain.Position, snapshot domain.Position) []string {
	var fields []string
	if position.Ticker != snapshot.Ticker {
		fields = append(fields, "ticker")
	}
	if position.Side != snapshot.Side {
		fields = append(fields, "side")
	}
	if position.Quantity != snapshot.Quantity {
		fields = append(fields, "quantity")
	}
	if position.AvgEntry != snapshot.AvgEntry {
		fields = append(fields, "avg_entry")
	}
	return fields
}

func diffTradeFill(trade domain.Trade, fill BrokerFillSnapshot) []string {
	var fields []string
	if activityID := strings.TrimSpace(fill.ActivityID); activityID != "" && strings.TrimSpace(trade.ExternalID) != activityID {
		fields = append(fields, "external_id")
	}
	if trade.Ticker != fill.Ticker {
		fields = append(fields, "ticker")
	}
	if trade.Side != fill.Side {
		fields = append(fields, "side")
	}
	if trade.Quantity != fill.Quantity {
		fields = append(fields, "quantity")
	}
	if trade.Price != fill.Price {
		fields = append(fields, "price")
	}
	if trade.Fee != fill.Fee {
		fields = append(fields, "fee")
	}
	if !trade.ExecutedAt.Equal(fill.ExecutedAt) {
		fields = append(fields, "executed_at")
	}
	return fields
}

func cloneFloatPtr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func cloneUUIDPtr(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func floatPtrEqual(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func timePtrEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func uuidPtrEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func formatFloat(v float64) string {
	return fmt.Sprintf("%.10f", v)
}

func normalizedQuantityEqual(left, right float64) bool {
	return formatStorageNumeric(left) == formatStorageNumeric(right)
}

func formatStorageNumeric(v float64) string {
	return fmt.Sprintf("%.8f", v)
}
