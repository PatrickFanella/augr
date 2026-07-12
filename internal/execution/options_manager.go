package execution

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

// OptionsBroker is the interface for options order submission.
type OptionsBroker interface {
	SubmitOptionOrder(ctx context.Context, order *domain.Order) (string, error)
	SubmitSpreadOrder(ctx context.Context, spread *domain.OptionSpread, quantity float64) ([]string, error)
}

// OptionsOrderManager handles options order submission for both single-leg
// and multi-leg strategies.
type OptionsOrderManager struct {
	broker       OptionsBroker
	brokerName   string
	orderRepo    repository.OrderRepository
	positionRepo repository.PositionRepository
	tradeRepo    repository.TradeRepository
	riskEngine   risk.RiskEngine
	liveTrading  bool
	liveGate     LiveGateConfig
	logger       *slog.Logger
}

// NewOptionsOrderManager constructs an OptionsOrderManager with the given dependencies.
func NewOptionsOrderManager(
	broker OptionsBroker,
	orderRepo repository.OrderRepository,
	positionRepo repository.PositionRepository,
	tradeRepo repository.TradeRepository,
	riskEngine risk.RiskEngine,
	logger *slog.Logger,
) *OptionsOrderManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &OptionsOrderManager{
		broker:       broker,
		brokerName:   "options",
		orderRepo:    orderRepo,
		positionRepo: positionRepo,
		tradeRepo:    tradeRepo,
		riskEngine:   riskEngine,
		logger:       logger,
	}
}

// WithBrokerName overrides the broker label used by the live-trading gate.
func (m *OptionsOrderManager) WithBrokerName(name string) *OptionsOrderManager {
	if m == nil {
		return nil
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "" {
		m.brokerName = name
	}
	return m
}

// WithLiveTrading toggles the live-execution path for options orders.
func (m *OptionsOrderManager) WithLiveTrading(enabled bool) *OptionsOrderManager {
	if m == nil {
		return nil
	}
	m.liveTrading = enabled
	return m
}

// WithLiveGate configures the live trading gate for options orders.
func (m *OptionsOrderManager) WithLiveGate(gate LiveGateConfig) *OptionsOrderManager {
	if m == nil {
		return nil
	}
	m.liveGate = gate
	return m
}

// ProcessOptionSignal handles a single-leg options trade: validate → risk check → submit → track.
func (m *OptionsOrderManager) ProcessOptionSignal(
	ctx context.Context,
	signal FinalSignal,
	plan TradingPlan,
	strategyID, runID uuid.UUID,
) error {
	if m == nil {
		return fmt.Errorf("options_manager: manager is nil")
	}

	// Ignore hold signals.
	if signal.Signal == domain.PipelineSignalHold {
		m.logger.InfoContext(ctx, "options: hold signal, skipping", "ticker", plan.Ticker)
		return nil
	}

	contract, err := domain.ParseOCC(plan.Ticker)
	if err != nil {
		return fmt.Errorf("options_manager: explicit OCC contract is required: %w", err)
	}
	if plan.PositionSize <= 0 {
		return fmt.Errorf("options_manager: contract quantity must be greater than zero")
	}
	if m.riskEngine == nil {
		return fmt.Errorf("options_manager: risk engine is required")
	}
	if m.orderRepo == nil {
		return fmt.Errorf("options_manager: order repository is required")
	}

	// 1. Kill switch check.
	active, err := m.riskEngine.IsKillSwitchActive(ctx)
	if err != nil {
		return fmt.Errorf("options_manager: kill switch check: %w", err)
	}
	if active {
		m.logger.WarnContext(ctx, "options: kill switch active", "ticker", plan.Ticker)
		return fmt.Errorf("options_manager: kill switch active, order blocked for %s", plan.Ticker)
	}
	marketActive, err := m.riskEngine.IsMarketKillSwitchActive(ctx, domain.MarketTypeOptions)
	if err != nil {
		return fmt.Errorf("options_manager: options kill switch check: %w", err)
	}
	if marketActive {
		return fmt.Errorf("options_manager: options kill switch active, order blocked for %s", plan.Ticker)
	}

	if m.liveTrading {
		allowed, denial := m.liveGate.Allows(&strategyID, m.brokerName)
		if !allowed {
			m.logger.WarnContext(ctx, "options: live execution denied", "ticker", plan.Ticker, "strategy_id", strategyID, "broker", m.brokerName, "code", denial.Code, "reason", denial.Message)
			return fmt.Errorf("options_manager: live execution denied for %s: %s", plan.Ticker, denial.Message)
		}
	}

	// 2. Build the order.
	now := time.Now().UTC()
	side := signalToSide(signal.Signal)
	intent := inferPositionIntent(side, true) // opening trade

	order := &domain.Order{
		ID:                 uuid.New(),
		StrategyID:         &strategyID,
		PipelineRunID:      &runID,
		Ticker:             plan.Ticker,
		MarketType:         domain.MarketTypeOptions,
		Side:               side,
		OrderType:          entryTypeToOrderType(plan.EntryType),
		Quantity:           plan.PositionSize,
		Status:             domain.OrderStatusPending,
		AssetClass:         domain.AssetClassOption,
		UnderlyingTicker:   contract.Underlying,
		OptionType:         &contract.OptionType,
		Strike:             &contract.Strike,
		Expiry:             &contract.Expiry,
		ContractMultiplier: contract.Multiplier,
		PositionIntent:     &intent,
		CreatedAt:          now,
	}

	if plan.EntryPrice > 0 {
		order.LimitPrice = &plan.EntryPrice
	}
	if plan.StopLoss > 0 {
		order.StopPrice = &plan.StopLoss
	}

	approved, reason, err := m.riskEngine.CheckPreTrade(ctx, order, risk.Portfolio{})
	if err != nil {
		return fmt.Errorf("options_manager: pre-trade risk check: %w", err)
	}
	if !approved {
		return fmt.Errorf("options_manager: pre-trade risk rejected %s: %s", plan.Ticker, reason)
	}

	// 3. Persist the pending order.
	if err := m.orderRepo.Create(ctx, order); err != nil {
		return fmt.Errorf("options_manager: create order: %w", err)
	}
	if m.broker == nil {
		order.Status = domain.OrderStatusRejected
		if updateErr := m.orderRepo.Update(ctx, order); updateErr != nil {
			m.logger.ErrorContext(ctx, "options: failed to persist unavailable broker rejection", "error", updateErr)
		}
		return fmt.Errorf("options_manager: options broker is required")
	}

	// 4. Submit to broker.
	externalID, err := m.broker.SubmitOptionOrder(ctx, order)
	if err != nil {
		order.Status = domain.OrderStatusRejected
		if updateErr := m.orderRepo.Update(ctx, order); updateErr != nil {
			m.logger.ErrorContext(ctx, "options: failed to update rejected order", "error", updateErr)
		}
		return fmt.Errorf("options_manager: submit option order: %w", err)
	}

	// 5. Update order status.
	submittedAt := time.Now().UTC()
	order.ExternalID = externalID
	order.Status = domain.OrderStatusSubmitted
	order.SubmittedAt = &submittedAt

	if err := m.orderRepo.Update(ctx, order); err != nil {
		return fmt.Errorf("options_manager: update submitted order: %w", err)
	}

	m.logger.InfoContext(ctx, "options: order submitted",
		"ticker", plan.Ticker,
		"external_id", externalID,
		"side", side,
		"quantity", plan.PositionSize,
	)

	return nil
}

// ProcessSpreadSignal handles a multi-leg spread trade: validate → risk check → submit → track.
func (m *OptionsOrderManager) ProcessSpreadSignal(
	ctx context.Context,
	spread *domain.OptionSpread,
	quantity float64,
	strategyID, runID uuid.UUID,
) error {
	if m == nil {
		return fmt.Errorf("options_manager: manager is nil")
	}
	if spread == nil {
		return fmt.Errorf("options_manager: spread is required")
	}
	if len(spread.Legs) == 0 {
		return fmt.Errorf("options_manager: spread must have at least one leg")
	}
	if quantity <= 0 {
		return fmt.Errorf("options_manager: spread quantity must be greater than zero")
	}

	// 1. Kill switch check.
	active, err := m.riskEngine.IsKillSwitchActive(ctx)
	if err != nil {
		return fmt.Errorf("options_manager: kill switch check: %w", err)
	}
	if active {
		m.logger.WarnContext(ctx, "options: kill switch active for spread", "underlying", spread.Underlying)
		return fmt.Errorf("options_manager: kill switch active, spread blocked for %s", spread.Underlying)
	}

	if m.liveTrading {
		allowed, denial := m.liveGate.Allows(&strategyID, m.brokerName)
		if !allowed {
			m.logger.WarnContext(ctx, "options: live execution denied for spread", "underlying", spread.Underlying, "strategy_id", strategyID, "broker", m.brokerName, "code", denial.Code, "reason", denial.Message)
			return fmt.Errorf("options_manager: live execution denied for spread %s: %s", spread.Underlying, denial.Message)
		}
	}

	// 2. Create per-leg orders for tracking.
	legGroupID := uuid.New()
	now := time.Now().UTC()

	for _, leg := range spread.Legs {
		intent := leg.PositionIntent
		legOrder := &domain.Order{
			ID:             uuid.New(),
			StrategyID:     &strategyID,
			PipelineRunID:  &runID,
			Ticker:         leg.Contract.OCCSymbol,
			Side:           leg.Side,
			OrderType:      domain.OrderTypeMarket,
			Quantity:       quantity * float64(leg.Ratio),
			Status:         domain.OrderStatusPending,
			AssetClass:     domain.AssetClassOption,
			PositionIntent: &intent,
			LegGroupID:     &legGroupID,
			CreatedAt:      now,
		}

		if err := m.orderRepo.Create(ctx, legOrder); err != nil {
			return fmt.Errorf("options_manager: create leg order: %w", err)
		}
	}

	// 3. Submit spread to broker.
	ids, err := m.broker.SubmitSpreadOrder(ctx, spread, quantity)
	if err != nil {
		return fmt.Errorf("options_manager: submit spread order: %w", err)
	}

	m.logger.InfoContext(ctx, "options: spread submitted",
		"underlying", spread.Underlying,
		"strategy", spread.StrategyType,
		"quantity", quantity,
		"order_ids", ids,
	)

	return nil
}

// signalToSide maps a pipeline signal to an order side.
func signalToSide(signal domain.PipelineSignal) domain.OrderSide {
	switch signal {
	case domain.PipelineSignalBuy:
		return domain.OrderSideBuy
	default:
		return domain.OrderSideSell
	}
}

// entryTypeToOrderType converts a trading plan entry type to an order type.
func entryTypeToOrderType(entryType string) domain.OrderType {
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

// inferPositionIntent determines the position intent from side and whether it's
// an opening or closing trade.
func inferPositionIntent(side domain.OrderSide, opening bool) domain.PositionIntent {
	if opening {
		if side == domain.OrderSideBuy {
			return domain.PositionIntentBuyToOpen
		}
		return domain.PositionIntentSellToOpen
	}
	if side == domain.OrderSideBuy {
		return domain.PositionIntentBuyToClose
	}
	return domain.PositionIntentSellToClose
}
