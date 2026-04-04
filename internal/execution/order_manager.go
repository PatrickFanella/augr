package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

// Order event kinds emitted by the OrderManager.
const (
	OrderEventSubmitted = "order_submitted"
	OrderEventFilled    = "order_filled"
	OrderEventCancelled = "order_cancelled"
	OrderEventRejected  = "order_rejected"
)

// FinalSignal stores the extracted pipeline signal and confidence.
type FinalSignal struct {
	Signal     domain.PipelineSignal `json:"signal,omitempty"`
	Confidence float64               `json:"confidence,omitempty"`
}

// TradingPlan stores the structured output produced by the trader phase.
type TradingPlan struct {
	Action       domain.PipelineSignal `json:"action,omitempty"`
	Ticker       string                `json:"ticker,omitempty"`
	EntryType    string                `json:"entry_type,omitempty"`
	EntryPrice   float64               `json:"entry_price,omitempty"`
	PositionSize float64               `json:"position_size,omitempty"`
	StopLoss     float64               `json:"stop_loss,omitempty"`
	TakeProfit   float64               `json:"take_profit,omitempty"`
	TimeHorizon  string                `json:"time_horizon,omitempty"`
	Confidence   float64               `json:"confidence,omitempty"`
	Rationale    string                `json:"rationale,omitempty"`
	RiskReward   float64               `json:"risk_reward,omitempty"`
}

// SizingConfig holds the parameters used to size positions.
type SizingConfig struct {
	Method        PositionSizingMethod
	RiskPct       float64
	ATRMultiplier float64
	WinRate       float64
	WinLossRatio  float64
	FractionPct   float64
	HalfKelly     bool
}

// OrderManager orchestrates the full order lifecycle:
// Signal → Risk Check → Size → Create → Submit → Track → Update Position → Audit.
type OrderManager struct {
	broker         Broker
	brokerName     string
	riskEngine     risk.RiskEngine
	positionRepo   repository.PositionRepository
	orderRepo      repository.OrderRepository
	tradeRepo      repository.TradeRepository
	auditLogRepo   repository.AuditLogRepository
	agentEventRepo repository.AgentEventRepository
	sizingConfig   SizingConfig
	logger         *slog.Logger
	nowMu          sync.RWMutex
	nowFunc        func() time.Time
}

// NewOrderManager constructs an OrderManager with the given dependencies.
func NewOrderManager(
	broker Broker,
	brokerName string,
	riskEngine risk.RiskEngine,
	positionRepo repository.PositionRepository,
	orderRepo repository.OrderRepository,
	tradeRepo repository.TradeRepository,
	auditLogRepo repository.AuditLogRepository,
	agentEventRepo repository.AgentEventRepository,
	sizingConfig SizingConfig,
	logger *slog.Logger,
) *OrderManager {
	if logger == nil {
		logger = slog.Default()
	}

	return &OrderManager{
		broker:         broker,
		brokerName:     brokerName,
		riskEngine:     riskEngine,
		positionRepo:   positionRepo,
		orderRepo:      orderRepo,
		tradeRepo:      tradeRepo,
		auditLogRepo:   auditLogRepo,
		agentEventRepo: agentEventRepo,
		sizingConfig:   sizingConfig,
		logger:         logger,
		nowFunc:        time.Now,
	}
}

// SetNowFunc overrides the order manager time source, allowing callers to
// drive all execution timestamps from a simulated backtest clock.
func (m *OrderManager) SetNowFunc(now func() time.Time) {
	if m == nil || now == nil {
		return
	}

	m.nowMu.Lock()
	defer m.nowMu.Unlock()

	m.nowFunc = now
}

func (m *OrderManager) currentTime() time.Time {
	if m == nil {
		return time.Now()
	}

	m.nowMu.RLock()
	defer m.nowMu.RUnlock()

	if m.nowFunc == nil {
		return time.Now()
	}

	return m.nowFunc()
}

// ProcessSignal executes the full order lifecycle for a trading signal.
func (m *OrderManager) ProcessSignal(
	ctx context.Context,
	signal FinalSignal,
	plan TradingPlan,
	strategyID, runID uuid.UUID,
) error {
	// Ignore hold signals — nothing to execute.
	if signal.Signal == domain.PipelineSignalHold {
		m.logger.InfoContext(ctx, "hold signal received, skipping order", "ticker", plan.Ticker)
		return nil
	}

	// 1. Check kill switch via risk engine.
	active, err := m.riskEngine.IsKillSwitchActive(ctx)
	if err != nil {
		return fmt.Errorf("order_manager: kill switch check: %w", err)
	}

	if active {
		m.logger.WarnContext(ctx, "kill switch active, order blocked", "ticker", plan.Ticker)

		if auditErr := m.audit(ctx, "kill_switch_blocked", "order", nil, map[string]any{
			"ticker":      plan.Ticker,
			"strategy_id": strategyID,
			"run_id":      runID,
			"signal":      signal.Signal,
		}); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		return fmt.Errorf("order_manager: kill switch active, order blocked for %s", plan.Ticker)
	}

	// 2. Calculate position size.
	balance, err := m.broker.GetAccountBalance(ctx)
	if err != nil {
		return fmt.Errorf("order_manager: get account balance: %w", err)
	}

	quantity := CalculatePositionSize(m.sizingConfig.Method, PositionSizingParams{
		AccountValue:  balance.Equity,
		RiskPct:       m.sizingConfig.RiskPct,
		ATR:           math.Abs(plan.EntryPrice - plan.StopLoss),
		Multiplier:    m.sizingConfig.ATRMultiplier,
		WinRate:       m.sizingConfig.WinRate,
		WinLossRatio:  m.sizingConfig.WinLossRatio,
		FractionPct:   m.sizingConfig.FractionPct,
		PricePerShare: plan.EntryPrice,
		HalfKelly:     m.sizingConfig.HalfKelly,
	})

	if quantity <= 0 {
		m.logger.WarnContext(ctx, "calculated position size is zero", "ticker", plan.Ticker)
		return fmt.Errorf("order_manager: calculated position size is zero for %s", plan.Ticker)
	}

	// 3. Check position limits via risk engine.
	// Convert the position size (in units) into additional portfolio exposure (0–1 fraction)
	// for the risk engine. This aligns with RiskEngine.CheckPositionLimits expectations.
	if balance.Equity <= 0 {
		return fmt.Errorf("order_manager: account equity is zero or negative for %s", plan.Ticker)
	}

	additionalExposurePct := (quantity * plan.EntryPrice) / balance.Equity

	portfolio, err := BuildRiskPortfolioSnapshotFromBalance(ctx, balance, m.positionRepo)
	if err != nil {
		return fmt.Errorf("order_manager: build risk portfolio: %w", err)
	}
	approved, reason, err := m.riskEngine.CheckPositionLimits(ctx, plan.Ticker, additionalExposurePct, portfolio)
	if err != nil {
		return fmt.Errorf("order_manager: check position limits: %w", err)
	}

	if !approved {
		m.logger.WarnContext(ctx, "position limits rejected", "ticker", plan.Ticker, "reason", reason)

		if auditErr := m.audit(ctx, "risk_check_rejected", "order", nil, map[string]any{
			"ticker":      plan.Ticker,
			"strategy_id": strategyID,
			"run_id":      runID,
			"reason":      reason,
			"quantity":    quantity,
		}); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		return fmt.Errorf("order_manager: risk check rejected for %s: %s", plan.Ticker, reason)
	}

	// 4. Create order (status = pending).
	now := m.currentTime()
	side := m.signalToSide(signal.Signal)
	orderType := m.entryTypeToOrderType(plan.EntryType)

	order := &domain.Order{
		ID:            uuid.New(),
		StrategyID:    &strategyID,
		PipelineRunID: &runID,
		Ticker:        plan.Ticker,
		Side:          side,
		OrderType:     orderType,
		Quantity:      quantity,
		Status:        domain.OrderStatusPending,
		Broker:        m.brokerName,
		CreatedAt:     now,
	}

	if plan.EntryPrice > 0 {
		order.LimitPrice = &plan.EntryPrice
	}

	if plan.StopLoss > 0 {
		order.StopPrice = &plan.StopLoss
	}

	if err := m.orderRepo.Create(ctx, order); err != nil {
		return fmt.Errorf("order_manager: create order: %w", err)
	}

	if auditErr := m.audit(ctx, "order_created", "order", &order.ID, map[string]any{
		"ticker":      plan.Ticker,
		"side":        side,
		"order_type":  orderType,
		"quantity":    quantity,
		"strategy_id": strategyID,
		"run_id":      runID,
	}); auditErr != nil {
		m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
	}

	// 5. Pre-trade risk check (circuit breaker + order validation).
	approved, reason, err = m.riskEngine.CheckPreTrade(ctx, order, portfolio)
	if err != nil {
		return fmt.Errorf("order_manager: pre-trade check: %w", err)
	}

	if !approved {
		order.Status = domain.OrderStatusRejected
		if updateErr := m.orderRepo.Update(ctx, order); updateErr != nil {
			m.logger.ErrorContext(ctx, "failed to update rejected order", "error", updateErr)
		}

		if auditErr := m.audit(ctx, "pre_trade_rejected", "order", &order.ID, map[string]any{
			"reason": reason,
		}); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		return fmt.Errorf("order_manager: pre-trade check rejected for %s: %s", plan.Ticker, reason)
	}

	// 6. Submit to broker (status = submitted).
	externalID, err := m.broker.SubmitOrder(ctx, order)
	if err != nil {
		order.Status = domain.OrderStatusRejected
		if updateErr := m.orderRepo.Update(ctx, order); updateErr != nil {
			m.logger.ErrorContext(ctx, "failed to update rejected order", "error", updateErr)
		}

		if auditErr := m.audit(ctx, "order_rejected", "order", &order.ID, map[string]any{
			"error": err.Error(),
		}); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		m.emitOrderEvent(ctx, OrderEventRejected, order, strategyID, runID)

		return fmt.Errorf("order_manager: submit order: %w", err)
	}

	submittedAt := m.currentTime()
	order.ExternalID = externalID
	order.Status = domain.OrderStatusSubmitted
	order.SubmittedAt = &submittedAt

	if err := m.orderRepo.Update(ctx, order); err != nil {
		return fmt.Errorf("order_manager: update submitted order: %w", err)
	}

	if auditErr := m.audit(ctx, "order_submitted", "order", &order.ID, map[string]any{
		"external_id": externalID,
	}); auditErr != nil {
		m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
	}

	m.emitOrderEvent(ctx, OrderEventSubmitted, order, strategyID, runID)

	// 7. Check order status and handle fill.
	status, err := m.broker.GetOrderStatus(ctx, externalID)
	if err != nil {
		return fmt.Errorf("order_manager: get order status: %w", err)
	}

	order.Status = status

	switch status {
	case domain.OrderStatusFilled:
		return m.handleFill(ctx, order, plan, strategyID, runID)
	case domain.OrderStatusCancelled:
		if err := m.orderRepo.Update(ctx, order); err != nil {
			return fmt.Errorf("order_manager: update %s order: %w", status, err)
		}

		if auditErr := m.audit(ctx, "order_"+string(status), "order", &order.ID, nil); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		m.emitOrderEvent(ctx, OrderEventCancelled, order, strategyID, runID)

		return nil
	case domain.OrderStatusRejected:
		if err := m.orderRepo.Update(ctx, order); err != nil {
			return fmt.Errorf("order_manager: update %s order: %w", status, err)
		}

		if auditErr := m.audit(ctx, "order_"+string(status), "order", &order.ID, nil); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		m.emitOrderEvent(ctx, OrderEventRejected, order, strategyID, runID)

		return nil
	default:
		// Partially filled or still submitted — persist the latest status.
		if err := m.orderRepo.Update(ctx, order); err != nil {
			return fmt.Errorf("order_manager: update order status: %w", err)
		}

		return nil
	}
}

// handleFill creates a Trade and creates or updates the Position.
func (m *OrderManager) handleFill(
	ctx context.Context,
	order *domain.Order,
	plan TradingPlan,
	strategyID, runID uuid.UUID,
) error {
	now := m.currentTime()
	order.FilledQuantity = order.Quantity
	order.FilledAt = &now

	if plan.EntryPrice > 0 {
		order.FilledAvgPrice = &plan.EntryPrice
	}

	if err := m.orderRepo.Update(ctx, order); err != nil {
		return fmt.Errorf("order_manager: update filled order: %w", err)
	}

	// Determine fill price.
	fillPrice := plan.EntryPrice
	if order.FilledAvgPrice != nil {
		fillPrice = *order.FilledAvgPrice
	}

	// Create trade.
	trade := &domain.Trade{
		ID:         uuid.New(),
		OrderID:    &order.ID,
		Ticker:     order.Ticker,
		Side:       order.Side,
		Quantity:   order.FilledQuantity,
		Price:      fillPrice,
		ExecutedAt: now,
		CreatedAt:  now,
	}

	// Create or update position.
	positionSide := domain.PositionSideLong
	if order.Side == domain.OrderSideSell {
		positionSide = domain.PositionSideShort
	}

	position := &domain.Position{
		ID:         uuid.New(),
		StrategyID: &strategyID,
		Ticker:     order.Ticker,
		Side:       positionSide,
		Quantity:   order.FilledQuantity,
		AvgEntry:   fillPrice,
		OpenedAt:   now,
	}

	if plan.StopLoss > 0 {
		position.StopLoss = &plan.StopLoss
	}

	if plan.TakeProfit > 0 {
		position.TakeProfit = &plan.TakeProfit
	}

	if err := m.positionRepo.Create(ctx, position); err != nil {
		return fmt.Errorf("order_manager: create position: %w", err)
	}

	trade.PositionID = &position.ID

	if err := m.tradeRepo.Create(ctx, trade); err != nil {
		// Audit the incomplete fill so it can be reconciled later.
		if auditErr := m.audit(ctx, "order_fill_incomplete", "order", &order.ID, map[string]any{
			"fill_price":  fillPrice,
			"quantity":    order.FilledQuantity,
			"position_id": position.ID,
			"error":       err.Error(),
		}); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		return fmt.Errorf("order_manager: create trade: %w", err)
	}

	if auditErr := m.audit(ctx, "order_filled", "order", &order.ID, map[string]any{
		"fill_price":  fillPrice,
		"quantity":    order.FilledQuantity,
		"trade_id":    trade.ID,
		"position_id": position.ID,
	}); auditErr != nil {
		m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
	}

	m.emitOrderEvent(ctx, OrderEventFilled, order, strategyID, runID)

	return nil
}

// signalToSide maps a pipeline signal to an order side.
func (m *OrderManager) signalToSide(signal domain.PipelineSignal) domain.OrderSide {
	switch signal {
	case domain.PipelineSignalBuy:
		return domain.OrderSideBuy
	default:
		return domain.OrderSideSell
	}
}

// entryTypeToOrderType converts a trading plan entry type to an order type.
func (m *OrderManager) entryTypeToOrderType(entryType string) domain.OrderType {
	switch entryType {
	case "limit":
		return domain.OrderTypeLimit
	case "stop":
		return domain.OrderTypeStop
	case "stop_limit":
		return domain.OrderTypeStopLimit
	default:
		return domain.OrderTypeMarket
	}
}

// audit is a helper that creates an AuditLogEntry.
func (m *OrderManager) audit(
	ctx context.Context,
	eventType, entityType string,
	entityID *uuid.UUID,
	details map[string]any,
) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}

	entry := &domain.AuditLogEntry{
		ID:         uuid.New(),
		EventType:  eventType,
		EntityType: entityType,
		EntityID:   entityID,
		Actor:      "order_manager",
		Details:    raw,
		CreatedAt:  m.currentTime(),
	}

	return m.auditLogRepo.Create(ctx, entry)
}

// emitOrderEvent persists an AgentEvent for order lifecycle transitions.
// If the agentEventRepo is nil or persistence fails, the error is logged
// but does not propagate — order flow must not break on event emission.
func (m *OrderManager) emitOrderEvent(
	ctx context.Context,
	eventKind string,
	order *domain.Order,
	strategyID, runID uuid.UUID,
) {
	if m.agentEventRepo == nil {
		return
	}

	meta, err := json.Marshal(map[string]any{
		"ticker":   order.Ticker,
		"side":     order.Side,
		"quantity": order.Quantity,
		"price":    order.LimitPrice,
		"broker":   order.Broker,
		"order_id": order.ID,
	})
	if err != nil {
		m.logger.ErrorContext(ctx, "order_manager: marshal event metadata", "error", err)
		return
	}

	title := fmt.Sprintf("Order %s: %s %.4g %s", eventKind, order.Side, order.Quantity, order.Ticker)

	event := &domain.AgentEvent{
		ID:            uuid.New(),
		PipelineRunID: &runID,
		StrategyID:    &strategyID,
		AgentRole:     domain.AgentRoleTrader,
		EventKind:     eventKind,
		Title:         title,
		Tags:          []string{"order", eventKind},
		Metadata:      meta,
		CreatedAt:     m.currentTime(),
	}

	if err := m.agentEventRepo.Create(ctx, event); err != nil {
		m.logger.ErrorContext(ctx, "order_manager: emit order event", "error", err, "kind", eventKind)
	}
}
