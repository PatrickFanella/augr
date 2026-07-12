package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
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
	Action           domain.PipelineSignal `json:"action,omitempty"`
	MarketType       domain.MarketType     `json:"market_type,omitempty"`
	Ticker           string                `json:"ticker,omitempty"`
	EntryType        string                `json:"entry_type,omitempty"`
	EntryPrice       float64               `json:"entry_price,omitempty"`
	PositionSize     float64               `json:"position_size,omitempty"`
	StopLoss         float64               `json:"stop_loss,omitempty"`
	TakeProfit       float64               `json:"take_profit,omitempty"`
	TimeHorizon      string                `json:"time_horizon,omitempty"`
	Confidence       float64               `json:"confidence,omitempty"`
	Rationale        string                `json:"rationale,omitempty"`
	RiskReward       float64               `json:"risk_reward,omitempty"`
	Side             string                `json:"side,omitempty"`
	DecisionMetadata *DecisionMetadata     `json:"decision_metadata,omitempty"`
	OptionGreeks     *domain.OptionGreeks  `json:"option_greeks,omitempty"`
}

// DecisionMetadata captures prompt and LLM usage details for a trading decision.
type DecisionMetadata struct {
	PromptText       string   `json:"prompt_text,omitempty"`
	LLMProvider      string   `json:"llm_provider,omitempty"`
	LLMModel         string   `json:"llm_model,omitempty"`
	PromptTokens     *int     `json:"prompt_tokens,omitempty"`
	CompletionTokens *int     `json:"completion_tokens,omitempty"`
	LatencyMS        *int     `json:"latency_ms,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
}

// SizingConfig holds the parameters used to size positions.
type SizingConfig struct {
	Method          PositionSizingMethod
	RiskPct         float64
	ATRMultiplier   float64
	WinRate         float64
	WinLossRatio    float64
	FractionPct     float64
	MaxPositionUSDC float64
	HalfKelly       bool
}

// OrderManager orchestrates the full order lifecycle:
// Signal → Risk Check → Size → Create → Submit → Track → Update Position → Audit.
type OrderManager struct {
	broker           Broker
	brokerName       string
	riskEngine       risk.RiskEngine
	positionRepo     repository.PositionRepository
	orderRepo        repository.OrderRepository
	tradeRepo        repository.TradeRepository
	auditLogRepo     repository.AuditLogRepository
	agentEventRepo   repository.AgentEventRepository
	decisionRecorder DecisionRecorder
	sizingConfig     SizingConfig
	liveTrading      bool
	liveGate         LiveGateConfig
	logger           *slog.Logger
	nowMu            sync.RWMutex
	nowFunc          func() time.Time
	metrics          OrderMetricsRecorder
}

// OrderMetricsRecorder records order lifecycle metrics.
type OrderMetricsRecorder interface {
	RecordOrder(broker, side, status string)
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

// WithMetrics wires an optional metrics recorder into the manager.
func (m *OrderManager) WithMetrics(metrics OrderMetricsRecorder) *OrderManager {
	if m == nil {
		return nil
	}
	m.metrics = metrics
	return m
}

// WithDecisionRecorder wires an optional decision journal recorder into the manager.
func (m *OrderManager) WithDecisionRecorder(recorder DecisionRecorder) *OrderManager {
	if m == nil {
		return nil
	}
	m.decisionRecorder = recorder
	return m
}

// WithLiveTrading toggles the live-execution path. Paper/default paths should
// leave this disabled.
func (m *OrderManager) WithLiveTrading(enabled bool) *OrderManager {
	if m == nil {
		return nil
	}
	m.liveTrading = enabled
	return m
}

// WithLiveGate configures the explicit live-trading gate.
func (m *OrderManager) WithLiveGate(gate LiveGateConfig) *OrderManager {
	if m == nil {
		return nil
	}
	m.liveGate = gate
	return m
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
	marketType := planMarketType(plan)
	polymarketExitMaxQuantity := 0.0

	// Ignore hold signals — nothing to execute.
	if signal.Signal == domain.PipelineSignalHold {
		m.logger.InfoContext(ctx, "hold signal received, skipping order", "ticker", plan.Ticker)
		return nil
	}

	// A stock SELL signal only makes sense as an exit for a position this
	// strategy already owns. Do not turn discovery sell signals for unowned stock
	// symbols into broker orders; Alpaca will reject them and they are not
	// actionable trades. Non-stock markets have different SELL semantics and are
	// intentionally left to their market-specific execution/risk paths.
	if signal.Signal == domain.PipelineSignalSell && marketType == domain.MarketTypeStock {
		owned, err := m.hasOpenLongPosition(ctx, strategyID, plan.Ticker)
		if err != nil {
			return err
		}
		if !owned {
			m.logger.InfoContext(ctx, "sell signal has no open long position, skipping order", "ticker", plan.Ticker, "strategy_id", strategyID)

			decision := m.newTradeDecision(
				strategyID,
				runID,
				plan,
				marketType,
				string(domain.OrderSideSell),
				0,
				0,
				domain.RiskDecisionRejected,
				[]string{"unowned_sell_no_open_long"},
				domain.TradeDecisionStatusRejected,
			)
			decision.Evidence, _ = json.Marshal(map[string]any{
				"reason":          "unowned_sell_no_open_long",
				"has_open_long":   false,
				"ticker":          plan.Ticker,
				"strategy_id":     strategyID.String(),
				"pipeline_run_id": runID.String(),
				"pipeline_signal": signal.Signal,
			})
			m.recordTradeDecision(ctx, decision)

			if auditErr := m.audit(ctx, "sell_without_position_skipped", "order", nil, map[string]any{
				"ticker":      plan.Ticker,
				"strategy_id": strategyID,
				"run_id":      runID,
				"signal":      signal.Signal,
			}); auditErr != nil {
				m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
			}

			return nil
		}
	}

	if signal.Signal == domain.PipelineSignalSell && marketType == domain.MarketTypePolymarket {
		predictionSide := strings.ToUpper(strings.TrimSpace(plan.Side))
		ownedQuantity, err := m.openPolymarketPositionQuantity(ctx, strategyID, plan.Ticker, predictionSide)
		if err != nil {
			return err
		}
		if ownedQuantity <= 0 {
			m.logger.InfoContext(ctx, "polymarket sell signal has no open side-qualified position, skipping order", "ticker", plan.Ticker, "prediction_side", predictionSide, "strategy_id", strategyID)

			decision := m.newTradeDecision(
				strategyID,
				runID,
				plan,
				marketType,
				string(domain.OrderSideSell),
				0,
				0,
				domain.RiskDecisionRejected,
				[]string{"unowned_polymarket_exit_no_open_position"},
				domain.TradeDecisionStatusRejected,
			)
			decision.Evidence, _ = json.Marshal(map[string]any{
				"reason":            "unowned_polymarket_exit_no_open_position",
				"has_open_position": false,
				"ticker":            plan.Ticker,
				"prediction_side":   predictionSide,
				"strategy_id":       strategyID.String(),
				"pipeline_run_id":   runID.String(),
				"pipeline_signal":   signal.Signal,
			})
			m.recordTradeDecision(ctx, decision)

			if auditErr := m.audit(ctx, "sell_without_position_skipped", "order", nil, map[string]any{
				"ticker":          plan.Ticker,
				"prediction_side": predictionSide,
				"strategy_id":     strategyID,
				"run_id":          runID,
				"signal":          signal.Signal,
				"market_type":     marketType,
			}); auditErr != nil {
				m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
			}

			return nil
		}
		polymarketExitMaxQuantity = ownedQuantity
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

	// 1b. Live execution gate (paper/default paths skip this entirely).
	if m.liveTrading {
		allowed, denial := m.liveGate.Allows(&strategyID, m.brokerName)
		if !allowed {
			m.logger.WarnContext(ctx, "live execution denied", "ticker", plan.Ticker, "strategy_id", strategyID, "broker", m.brokerName, "code", denial.Code, "reason", denial.Message)

			m.recordTradeDecision(ctx, m.newTradeDecision(
				strategyID,
				runID,
				plan,
				marketType,
				strings.ToUpper(strings.TrimSpace(plan.Side)),
				0,
				0,
				domain.RiskDecisionRejected,
				[]string{denial.Code + ": " + denial.Message},
				domain.TradeDecisionStatusRejected,
			))

			return fmt.Errorf("order_manager: live execution denied for %s: %s", plan.Ticker, denial.Message)
		}
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
	if marketType.Normalize() == domain.MarketTypePolymarket {
		quantity = PolymarketPositionSize(PolymarketSizingParams{
			AccountValue:    balance.Equity,
			FractionPct:     m.sizingConfig.FractionPct,
			MaxPositionUSDC: m.sizingConfig.MaxPositionUSDC,
			EntryPrice:      plan.EntryPrice,
		})
		if signal.Signal == domain.PipelineSignalSell && polymarketExitMaxQuantity > 0 && quantity > polymarketExitMaxQuantity {
			quantity = polymarketExitMaxQuantity
		}
	}

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
	if marketType != "" {
		if portfolio.MarketExposurePct == nil {
			portfolio.MarketExposurePct = make(map[domain.MarketType]float64)
		}
		portfolio.MarketExposurePct[marketType] += additionalExposurePct
	}
	approved, reason, err := m.riskEngine.CheckPositionLimits(ctx, plan.Ticker, additionalExposurePct, portfolio)
	if err != nil {
		return fmt.Errorf("order_manager: check position limits: %w", err)
	}

	if !approved {
		m.logger.WarnContext(ctx, "position limits rejected", "ticker", plan.Ticker, "reason", reason)
		m.recordTradeDecision(ctx, m.newTradeDecision(
			strategyID,
			runID,
			plan,
			marketType,
			strings.ToUpper(strings.TrimSpace(plan.Side)),
			quantity,
			0,
			domain.RiskDecisionRejected,
			[]string{reason},
			domain.TradeDecisionStatusRejected,
		))

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
		ID:             uuid.New(),
		StrategyID:     &strategyID,
		PipelineRunID:  &runID,
		Ticker:         plan.Ticker,
		MarketType:     marketType,
		Side:           side,
		OrderType:      orderType,
		Quantity:       quantity,
		Status:         domain.OrderStatusPending,
		Broker:         m.brokerName,
		CreatedAt:      now,
		PredictionSide: strings.ToUpper(strings.TrimSpace(plan.Side)),
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
	m.recordOrderMetric(order.Side, order.Status)

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
		m.recordOrderMetric(order.Side, order.Status)
		m.recordTradeDecision(ctx, m.newTradeDecision(
			strategyID,
			runID,
			plan,
			order.MarketType,
			string(order.Side),
			quantity,
			0,
			domain.RiskDecisionRejected,
			[]string{reason},
			domain.TradeDecisionStatusRejected,
		))

		if auditErr := m.audit(ctx, "pre_trade_rejected", "order", &order.ID, map[string]any{
			"reason": reason,
		}); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		return fmt.Errorf("order_manager: pre-trade check rejected for %s: %s", plan.Ticker, reason)
	}

	decision := m.newTradeDecision(
		strategyID,
		runID,
		plan,
		order.MarketType,
		string(order.Side),
		quantity,
		quantity,
		domain.RiskDecisionApproved,
		nil,
		domain.TradeDecisionStatusCandidate,
	)
	m.recordTradeDecision(ctx, decision)

	// 6. Submit to broker (status = submitted).
	externalID, err := m.broker.SubmitOrder(ctx, order)
	if err != nil {
		order.Status = domain.OrderStatusRejected
		if updateErr := m.orderRepo.Update(ctx, order); updateErr != nil {
			m.logger.ErrorContext(ctx, "failed to update rejected order", "error", updateErr)
		}
		m.recordOrderMetric(order.Side, order.Status)
		m.attachTradeDecisionOrder(ctx, decision.ID, order.ID, m.liveTrading)

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
	m.recordOrderMetric(order.Side, order.Status)
	m.attachTradeDecisionOrder(ctx, decision.ID, order.ID, m.liveTrading)

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
		m.recordOrderMetric(order.Side, order.Status)

		if auditErr := m.audit(ctx, "order_"+string(status), "order", &order.ID, nil); auditErr != nil {
			m.logger.ErrorContext(ctx, "audit log failed", "error", auditErr)
		}

		m.emitOrderEvent(ctx, OrderEventCancelled, order, strategyID, runID)

		return nil
	case domain.OrderStatusRejected:
		if err := m.orderRepo.Update(ctx, order); err != nil {
			return fmt.Errorf("order_manager: update %s order: %w", status, err)
		}
		m.recordOrderMetric(order.Side, order.Status)

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
		m.recordOrderMetric(order.Side, order.Status)

		return nil
	}
}

func planMarketType(plan TradingPlan) domain.MarketType {
	marketType := plan.MarketType.Normalize()
	if marketType == "" {
		return domain.MarketTypeStock
	}
	return marketType
}

func (m *OrderManager) newTradeDecision(
	strategyID, runID uuid.UUID,
	plan TradingPlan,
	marketType domain.MarketType,
	side string,
	proposedSize, approvedSize float64,
	riskStatus domain.RiskDecisionStatus,
	riskReasons []string,
	status domain.TradeDecisionStatus,
) *domain.TradeDecision {
	decision := &domain.TradeDecision{
		ID:              uuid.New(),
		StrategyID:      &strategyID,
		PipelineRunID:   &runID,
		MarketType:      marketType.Normalize(),
		InstrumentKey:   strings.TrimSpace(plan.Ticker),
		Side:            domain.OrderSide(strings.ToLower(strings.TrimSpace(side))),
		ExecutablePrice: plan.EntryPrice,
		ProposedSize:    proposedSize,
		ApprovedSize:    approvedSize,
		RiskStatus:      riskStatus,
		RiskReasons:     append([]string(nil), riskReasons...),
		Status:          status,
		CreatedAt:       m.currentTime(),
		UpdatedAt:       m.currentTime(),
	}
	if plan.DecisionMetadata != nil {
		decision.PromptText = plan.DecisionMetadata.PromptText
		decision.LLMProvider = strings.TrimSpace(plan.DecisionMetadata.LLMProvider)
		decision.LLMModel = strings.TrimSpace(plan.DecisionMetadata.LLMModel)
		decision.PromptTokens = cloneIntPtr(plan.DecisionMetadata.PromptTokens)
		decision.CompletionTokens = cloneIntPtr(plan.DecisionMetadata.CompletionTokens)
		decision.LatencyMS = cloneIntPtr(plan.DecisionMetadata.LatencyMS)
		decision.CostUSD = cloneFloatPtr(plan.DecisionMetadata.CostUSD)
	}
	return decision
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloatPtr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (m *OrderManager) recordTradeDecision(ctx context.Context, decision *domain.TradeDecision) {
	if m == nil || m.decisionRecorder == nil || decision == nil {
		return
	}
	if err := m.decisionRecorder.RecordDecision(ctx, decision); err != nil {
		m.logger.ErrorContext(ctx, "order_manager: record trade decision", "error", err, "decision_id", decision.ID)
	}
}

func (m *OrderManager) attachTradeDecisionOrder(ctx context.Context, decisionID, orderID uuid.UUID, live bool) {
	if m == nil || m.decisionRecorder == nil || decisionID == uuid.Nil || orderID == uuid.Nil {
		return
	}
	var err error
	if live {
		err = m.decisionRecorder.AttachLiveOrder(ctx, decisionID, orderID)
	} else {
		err = m.decisionRecorder.AttachPaperOrder(ctx, decisionID, orderID)
	}
	if err != nil {
		m.logger.ErrorContext(ctx, "order_manager: attach trade decision order", "error", err, "decision_id", decisionID, "order_id", orderID, "live", live)
	}
}

func (m *OrderManager) hasOpenLongPosition(ctx context.Context, strategyID uuid.UUID, ticker string) (bool, error) {
	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return false, fmt.Errorf("order_manager: open long ownership check requires ticker")
	}

	positions, err := m.positionRepo.GetByStrategy(ctx, strategyID, repository.PositionFilter{
		Ticker: ticker,
		Side:   domain.PositionSideLong,
	}, riskSnapshotPositionLimit, 0)
	if err != nil {
		return false, fmt.Errorf("order_manager: get open long position for %s: %w", ticker, err)
	}

	for _, position := range positions {
		if position.ClosedAt == nil && position.Quantity > 0 {
			return true, nil
		}
	}

	return false, nil
}

func (m *OrderManager) openPolymarketPositionQuantity(ctx context.Context, strategyID uuid.UUID, slug, side string) (float64, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return 0, fmt.Errorf("order_manager: polymarket exit ownership check requires ticker")
	}

	positions, err := m.positionRepo.GetByStrategy(ctx, strategyID, repository.PositionFilter{
		Ticker: polymarketPositionTicker(slug, side),
		Side:   domain.PositionSideLong,
	}, riskSnapshotPositionLimit, 0)
	if err != nil {
		return 0, fmt.Errorf("order_manager: get open polymarket position for %s:%s: %w", slug, strings.ToUpper(strings.TrimSpace(side)), err)
	}

	total := 0.0
	for _, position := range positions {
		if position.ClosedAt == nil && position.Quantity > 0 {
			total += position.Quantity
		}
	}

	return total, nil
}

func polymarketPositionTicker(slug, side string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}

	side = strings.ToUpper(strings.TrimSpace(side))
	if side == "" {
		return slug
	}

	return slug + ":" + side
}

func realizedPnL(side domain.PositionSide, avgEntry, fillPrice, quantity float64) float64 {
	if side == domain.PositionSideLong {
		return (fillPrice - avgEntry) * quantity
	}
	return (avgEntry - fillPrice) * quantity
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
	m.recordOrderMetric(order.Side, order.Status)

	// Determine fill price.
	fillPrice := plan.EntryPrice
	if order.FilledAvgPrice != nil {
		fillPrice = *order.FilledAvgPrice
	}

	marketType := order.MarketType.Normalize()
	var position *domain.Position
	if marketType == domain.MarketTypePolymarket && order.Side == domain.OrderSideSell {
		positionTicker := polymarketPositionTicker(order.Ticker, order.PredictionSide)
		positions, err := m.positionRepo.GetByStrategy(ctx, *order.StrategyID, repository.PositionFilter{
			Ticker: positionTicker,
			Side:   domain.PositionSideLong,
		}, riskSnapshotPositionLimit, 0)
		if err != nil {
			return fmt.Errorf("order_manager: get polymarket exit position for %s: %w", positionTicker, err)
		}

		for i := range positions {
			if positions[i].ClosedAt == nil && positions[i].Quantity > 0 {
				position = &positions[i]
				break
			}
		}
		if position == nil {
			return fmt.Errorf("order_manager: polymarket sell fill has no open position for %s", positionTicker)
		}

		closedQuantity := math.Min(position.Quantity, order.FilledQuantity)
		currentPrice := fillPrice
		position.CurrentPrice = &currentPrice
		position.RealizedPnL += realizedPnL(position.Side, position.AvgEntry, fillPrice, closedQuantity)
		if position.Quantity > order.FilledQuantity {
			position.Quantity -= order.FilledQuantity
		} else {
			position.Quantity = 0
			closedAt := now
			position.ClosedAt = &closedAt
		}

		if err := m.positionRepo.Update(ctx, position); err != nil {
			return fmt.Errorf("order_manager: update polymarket position: %w", err)
		}
	} else {
		positionSide := domain.PositionSideLong
		if order.Side == domain.OrderSideSell {
			positionSide = domain.PositionSideShort
		}

		positionTicker := order.Ticker
		if marketType == domain.MarketTypePolymarket {
			positionTicker = polymarketPositionTicker(order.Ticker, order.PredictionSide)
		}

		position = &domain.Position{
			ID:         uuid.New(),
			StrategyID: &strategyID,
			Ticker:     positionTicker,
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
	}

	trade := &domain.Trade{
		ID:         uuid.New(),
		OrderID:    &order.ID,
		PositionID: &position.ID,
		Ticker:     order.Ticker,
		Side:       order.Side,
		Quantity:   order.FilledQuantity,
		Price:      fillPrice,
		ExecutedAt: now,
		CreatedAt:  now,
	}

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

func (m *OrderManager) recordOrderMetric(side domain.OrderSide, status domain.OrderStatus) {
	if m == nil || m.metrics == nil {
		return
	}
	m.metrics.RecordOrder(m.brokerName, string(side), string(status))
}
