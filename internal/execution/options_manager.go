package execution

import (
	"context"
	"errors"
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

// OptionFillReport carries accounting fields that are not represented on Order.
type OptionFillReport struct {
	Premium float64
	Fee     float64
}

// OptionFillReporter is implemented by brokers that can synchronously account
// for an immediate fill. Submitted live orders are persisted later by reconciliation.
type OptionFillReporter interface {
	OptionFillReport(ctx context.Context, order *domain.Order) (OptionFillReport, error)
}

type optionsBalanceProvider interface {
	GetAccountBalance(ctx context.Context) (Balance, error)
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
	balanceProvider, ok := m.broker.(optionsBalanceProvider)
	if !ok {
		return fmt.Errorf("options_manager: broker account balance is required for multiplier-aware risk")
	}
	balance, err := balanceProvider.GetAccountBalance(ctx)
	if err != nil {
		return fmt.Errorf("options_manager: get account balance: %w", err)
	}
	if balance.Equity <= 0 {
		return fmt.Errorf("options_manager: account equity must be positive")
	}
	portfolio, err := BuildRiskPortfolioSnapshotFromBalance(ctx, balance, m.positionRepo)
	if err != nil {
		return fmt.Errorf("options_manager: build risk portfolio: %w", err)
	}
	notional := order.Quantity * plan.EntryPrice * order.ContractMultiplier
	additionalExposure := notional / balance.Equity
	if portfolio.MarketExposurePct == nil {
		portfolio.MarketExposurePct = make(map[domain.MarketType]float64)
	}
	portfolio.MarketExposurePct[domain.MarketTypeOptions] += additionalExposure
	approved, reason, err := m.riskEngine.CheckPositionLimits(ctx, order.Ticker, additionalExposure, portfolio)
	if err != nil {
		return fmt.Errorf("options_manager: check position limits: %w", err)
	}
	if !approved {
		return fmt.Errorf("options_manager: position limits rejected %s: %s", plan.Ticker, reason)
	}

	approved, reason, err = m.riskEngine.CheckPreTrade(ctx, order, portfolio)
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
	if order.Status == domain.OrderStatusPending {
		order.Status = domain.OrderStatusSubmitted
	}
	if order.SubmittedAt == nil {
		order.SubmittedAt = &submittedAt
	}

	if err := m.orderRepo.Update(ctx, order); err != nil {
		return fmt.Errorf("options_manager: update submitted order: %w", err)
	}
	if order.Status == domain.OrderStatusFilled {
		if err := m.persistImmediateFill(ctx, order); err != nil {
			return err
		}
	}

	m.logger.InfoContext(ctx, "options: order submitted",
		"ticker", plan.Ticker,
		"external_id", externalID,
		"side", side,
		"quantity", plan.PositionSize,
	)

	return nil
}

func (m *OptionsOrderManager) persistImmediateFill(ctx context.Context, order *domain.Order) error {
	reporter, ok := m.broker.(OptionFillReporter)
	if !ok {
		return fmt.Errorf("options_manager: filled order %s lacks accounting report", order.ID)
	}
	if m.positionRepo == nil || m.tradeRepo == nil {
		return fmt.Errorf("options_manager: position and trade repositories are required for filled orders")
	}
	if order.FilledAvgPrice == nil || order.FilledQuantity <= 0 || order.FilledAt == nil {
		return fmt.Errorf("options_manager: filled order %s is missing fill price, quantity, or timestamp", order.ID)
	}
	report, err := reporter.OptionFillReport(ctx, order)
	if err != nil {
		return fmt.Errorf("options_manager: option fill accounting: %w", err)
	}
	positionSide := domain.PositionSideLong
	if order.Side == domain.OrderSideSell {
		positionSide = domain.PositionSideShort
	}
	position := &domain.Position{
		ID: uuid.New(), StrategyID: order.StrategyID, MarketType: domain.MarketTypeOptions,
		Ticker: order.Ticker, Side: positionSide, Quantity: order.FilledQuantity,
		AvgEntry: *order.FilledAvgPrice, OpenedAt: *order.FilledAt,
		AssetClass: order.AssetClass, UnderlyingTicker: order.UnderlyingTicker,
		OptionType: order.OptionType, Strike: order.Strike, Expiry: order.Expiry,
		ContractMultiplier: order.ContractMultiplier, LegGroupID: order.LegGroupID,
	}
	if err := m.positionRepo.Create(ctx, position); err != nil {
		return fmt.Errorf("options_manager: persist filled position: %w", err)
	}
	orderID, positionID := order.ID, position.ID
	trade := &domain.Trade{
		ID: uuid.New(), OrderID: &orderID, PositionID: &positionID, ExternalID: order.ExternalID,
		Ticker: order.Ticker, Side: order.Side, Quantity: order.FilledQuantity,
		Price: *order.FilledAvgPrice, Fee: report.Fee, ExecutedAt: *order.FilledAt,
		AssetClass: domain.AssetClassOption, OpenClose: "open",
		ContractMultiplier: order.ContractMultiplier, Premium: report.Premium,
	}
	if err := m.tradeRepo.Create(ctx, trade); err != nil {
		return fmt.Errorf("options_manager: persist filled trade: %w", err)
	}
	return nil
}

// CloseOptionPosition closes an entire persisted option position at an explicit
// executable price. Partial closes and rolls require a separate atomic plan.
func (m *OptionsOrderManager) CloseOptionPosition(ctx context.Context, position *domain.Position, executablePrice float64, runID uuid.UUID, reason string) error {
	if m == nil || position == nil {
		return errors.New("options_manager: position is required")
	}
	if position.ClosedAt != nil || position.Quantity <= 0 {
		return errors.New("options_manager: position is not open")
	}
	if position.AssetClass != domain.AssetClassOption || position.OptionType == nil || position.Strike == nil || position.Expiry == nil || position.StrategyID == nil {
		return errors.New("options_manager: complete persisted option contract metadata is required")
	}
	if executablePrice <= 0 {
		return errors.New("options_manager: executable close price must be greater than zero")
	}
	if m.broker == nil || m.orderRepo == nil || m.positionRepo == nil || m.tradeRepo == nil {
		return errors.New("options_manager: broker and lifecycle repositories are required")
	}
	side, intent := domain.OrderSideSell, domain.PositionIntentSellToClose
	if position.Side == domain.PositionSideShort {
		side, intent = domain.OrderSideBuy, domain.PositionIntentBuyToClose
	}
	now := time.Now().UTC()
	order := &domain.Order{
		ID: uuid.New(), StrategyID: position.StrategyID, PipelineRunID: &runID,
		Ticker: position.Ticker, MarketType: domain.MarketTypeOptions, Side: side,
		OrderType: domain.OrderTypeLimit, Quantity: position.Quantity,
		LimitPrice: &executablePrice, Status: domain.OrderStatusPending,
		AssetClass: domain.AssetClassOption, UnderlyingTicker: position.UnderlyingTicker,
		OptionType: position.OptionType, Strike: position.Strike, Expiry: position.Expiry,
		ContractMultiplier: position.ContractMultiplier, PositionIntent: &intent,
		LegGroupID: position.LegGroupID, CreatedAt: now,
	}
	if err := m.orderRepo.Create(ctx, order); err != nil {
		return fmt.Errorf("options_manager: create close order: %w", err)
	}
	externalID, err := m.broker.SubmitOptionOrder(ctx, order)
	if err != nil {
		order.Status = domain.OrderStatusRejected
		_ = m.orderRepo.Update(ctx, order)
		return fmt.Errorf("options_manager: submit close order: %w", err)
	}
	order.ExternalID = externalID
	if order.Status == domain.OrderStatusPending {
		order.Status = domain.OrderStatusSubmitted
	}
	if order.SubmittedAt == nil {
		order.SubmittedAt = &now
	}
	if err := m.orderRepo.Update(ctx, order); err != nil {
		return fmt.Errorf("options_manager: update close order: %w", err)
	}
	if order.Status != domain.OrderStatusFilled {
		return nil
	}
	reporter, ok := m.broker.(OptionFillReporter)
	if !ok || order.FilledAvgPrice == nil || order.FilledAt == nil {
		return fmt.Errorf("options_manager: filled close order %s lacks accounting details", order.ID)
	}
	report, err := reporter.OptionFillReport(ctx, order)
	if err != nil {
		return fmt.Errorf("options_manager: close fill accounting: %w", err)
	}
	multiplier := position.ContractMultiplier
	if multiplier <= 0 {
		multiplier = 100
	}
	realized := (*order.FilledAvgPrice - position.AvgEntry) * position.Quantity * multiplier
	if position.Side == domain.PositionSideShort {
		realized = (position.AvgEntry - *order.FilledAvgPrice) * position.Quantity * multiplier
	}
	realized -= report.Fee
	position.RealizedPnL += realized
	position.Quantity = 0
	position.CurrentPrice = order.FilledAvgPrice
	position.ClosedAt = order.FilledAt
	if err := m.positionRepo.Update(ctx, position); err != nil {
		return fmt.Errorf("options_manager: persist closed position: %w", err)
	}
	orderID, positionID := order.ID, position.ID
	trade := &domain.Trade{ID: uuid.New(), OrderID: &orderID, PositionID: &positionID, ExternalID: externalID, Ticker: order.Ticker, Side: side, Quantity: order.FilledQuantity, Price: *order.FilledAvgPrice, Fee: report.Fee, ExecutedAt: *order.FilledAt, AssetClass: domain.AssetClassOption, OpenClose: "close", ContractMultiplier: multiplier, Premium: report.Premium, ExitReason: strings.TrimSpace(reason)}
	if err := m.tradeRepo.Create(ctx, trade); err != nil {
		return fmt.Errorf("options_manager: persist closing trade: %w", err)
	}
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
