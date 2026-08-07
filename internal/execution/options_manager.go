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

type optionFillCompensator interface {
	RollbackOptionOrder(ctx context.Context, externalID string) error
	RollbackOptionSpread(ctx context.Context, externalIDs []string) error
	FinalizeOptionSpread(externalIDs []string) error
}

type optionsBalanceProvider interface {
	GetAccountBalance(ctx context.Context) (Balance, error)
}

type spreadPreflightBroker interface {
	PreflightSpread(ctx context.Context, spread *domain.OptionSpread, quantity float64) error
}

// OptionsOrderManager handles options order submission for both single-leg
// and multi-leg strategies.
type OptionsOrderManager struct {
	broker         OptionsBroker
	brokerName     string
	orderRepo      repository.OrderRepository
	positionRepo   repository.PositionRepository
	tradeRepo      repository.TradeRepository
	optionFillRepo repository.OptionFillRepository
	riskEngine     risk.RiskEngine
	liveTrading    bool
	liveGate       LiveGateConfig
	logger         *slog.Logger
}

// WithOptionFillRepo wires all-or-nothing option fill persistence.
func (m *OptionsOrderManager) WithOptionFillRepo(repo repository.OptionFillRepository) *OptionsOrderManager {
	if m == nil {
		return nil
	}
	m.optionFillRepo = repo
	return m
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
	if plan.EntryPrice <= 0 {
		return fmt.Errorf("options_manager: explicit executable option price is required")
	}
	if m.riskEngine == nil {
		return fmt.Errorf("options_manager: risk engine is required")
	}
	if m.orderRepo == nil {
		return fmt.Errorf("options_manager: order repository is required")
	}
	if m.positionRepo == nil {
		return fmt.Errorf("options_manager: position repository is required")
	}
	if m.broker == nil {
		return fmt.Errorf("options_manager: broker is required")
	}
	if m.optionFillRepo == nil {
		return fmt.Errorf("options_manager: atomic option fill repository is required")
	}
	if _, synchronous := m.broker.(OptionFillReporter); synchronous {
		if _, compensating := m.broker.(optionFillCompensator); !compensating {
			return fmt.Errorf("options_manager: synchronous option broker requires fill compensation")
		}
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
		OptionGreeks:       plan.OptionGreeks,
		PositionIntent:     &intent,
		CreatedAt:          now,
		Broker:             m.brokerName,
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

	if order.Status == domain.OrderStatusFilled {
		if err := m.persistImmediateFill(ctx, order); err != nil {
			return m.abortOptionOrder(ctx, order, externalID, err)
		}
	} else if err := m.orderRepo.Update(ctx, order); err != nil {
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

func (m *OptionsOrderManager) persistImmediateFill(ctx context.Context, order *domain.Order) error {
	input, err := m.optionFillInput(ctx, order, nil, "")
	if err != nil {
		return err
	}
	_, err = m.optionFillRepo.ApplyOptionFills(ctx, []repository.OptionFillInput{input})
	if err != nil {
		return fmt.Errorf("options_manager: persist atomic option fill: %w", err)
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
	if position.ID == uuid.Nil || position.AssetClass != domain.AssetClassOption || strings.TrimSpace(position.Ticker) == "" || strings.TrimSpace(position.UnderlyingTicker) == "" || position.OptionType == nil || position.Strike == nil || position.Expiry == nil || position.StrategyID == nil {
		return errors.New("options_manager: complete persisted option contract metadata is required")
	}
	if position.Side != domain.PositionSideLong && position.Side != domain.PositionSideShort {
		return errors.New("options_manager: persisted option position side is invalid")
	}
	if executablePrice <= 0 {
		return errors.New("options_manager: executable close price must be greater than zero")
	}
	if m.broker == nil || m.orderRepo == nil || m.positionRepo == nil {
		return errors.New("options_manager: broker, order, and position repositories are required")
	}
	if m.optionFillRepo == nil {
		return errors.New("options_manager: atomic option fill repository is required")
	}
	if _, synchronous := m.broker.(OptionFillReporter); synchronous {
		if _, compensating := m.broker.(optionFillCompensator); !compensating {
			return errors.New("options_manager: synchronous option broker requires fill compensation")
		}
	}
	side, intent := domain.OrderSideSell, domain.PositionIntentSellToClose
	if position.Side == domain.PositionSideShort {
		side, intent = domain.OrderSideBuy, domain.PositionIntentBuyToClose
	}
	now := time.Now().UTC()
	multiplier := position.ContractMultiplier
	if multiplier <= 0 {
		multiplier = 100
	}
	order := &domain.Order{
		ID: uuid.New(), StrategyID: position.StrategyID, PipelineRunID: &runID,
		Ticker: position.Ticker, MarketType: domain.MarketTypeOptions, Side: side,
		OrderType: domain.OrderTypeLimit, Quantity: position.Quantity,
		LimitPrice: &executablePrice, Status: domain.OrderStatusPending,
		AssetClass: domain.AssetClassOption, UnderlyingTicker: position.UnderlyingTicker,
		OptionType: position.OptionType, Strike: position.Strike, Expiry: position.Expiry,
		ContractMultiplier: multiplier, PositionIntent: &intent,
		LegGroupID: position.LegGroupID, CreatedAt: now,
		Broker: m.brokerName,
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
	if order.Status != domain.OrderStatusFilled {
		if err := m.orderRepo.Update(ctx, order); err != nil {
			return fmt.Errorf("options_manager: update close order: %w", err)
		}
		return nil
	}
	if err := m.persistClosingFill(ctx, position, order, reason); err != nil {
		return m.abortOptionOrder(ctx, order, externalID, err)
	}
	return nil
}

func (m *OptionsOrderManager) persistClosingFill(ctx context.Context, position *domain.Position, order *domain.Order, reason string) error {
	input, err := m.optionFillInput(ctx, order, &position.ID, reason)
	if err != nil {
		return err
	}
	_, err = m.optionFillRepo.ApplyOptionFills(ctx, []repository.OptionFillInput{input})
	if err != nil {
		return fmt.Errorf("options_manager: persist atomic option close: %w", err)
	}
	return nil
}

func (m *OptionsOrderManager) optionFillInput(ctx context.Context, order *domain.Order, positionID *uuid.UUID, reason string) (repository.OptionFillInput, error) {
	if m.optionFillRepo == nil {
		return repository.OptionFillInput{}, fmt.Errorf("options_manager: atomic option fill repository is required")
	}
	reporter, ok := m.broker.(OptionFillReporter)
	if !ok || order.FilledAvgPrice == nil || order.FilledAt == nil {
		return repository.OptionFillInput{}, fmt.Errorf("options_manager: filled order %s lacks accounting details", order.ID)
	}
	report, err := reporter.OptionFillReport(ctx, order)
	if err != nil {
		return repository.OptionFillInput{}, fmt.Errorf("options_manager: option fill accounting: %w", err)
	}
	reason = strings.TrimSpace(reason)
	if positionID != nil && reason == "" {
		reason = "strategy close"
	}
	return repository.OptionFillInput{Order: order, PositionID: positionID, FillPrice: *order.FilledAvgPrice, FillQuantity: order.FilledQuantity, Fee: report.Fee, Premium: report.Premium, FilledAt: *order.FilledAt, ExitReason: reason}, nil
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
	if m.orderRepo == nil || m.positionRepo == nil || m.riskEngine == nil || m.broker == nil {
		return errors.New("options_manager: spread lifecycle dependencies are required")
	}
	if m.optionFillRepo == nil {
		return errors.New("options_manager: atomic option fill repository is required")
	}
	if _, synchronous := m.broker.(OptionFillReporter); synchronous {
		if _, compensating := m.broker.(optionFillCompensator); !compensating {
			return errors.New("options_manager: synchronous option broker requires fill compensation")
		}
	}
	opening, closing := 0, 0
	for _, leg := range spread.Legs {
		switch leg.PositionIntent {
		case domain.PositionIntentBuyToOpen, domain.PositionIntentSellToOpen:
			opening++
		case domain.PositionIntentBuyToClose, domain.PositionIntentSellToClose:
			closing++
		}
	}
	if (opening > 0 && closing > 0) || (opening == 0 && closing == 0) {
		return errors.New("options_manager: spread legs must be consistently opening or closing")
	}
	isClosing := closing == len(spread.Legs)
	closePositions := make(map[string]*domain.Position)
	var existingGroupID *uuid.UUID
	if isClosing {
		positions, err := m.positionRepo.GetByStrategy(ctx, strategyID, repository.PositionFilter{}, 100, 0)
		if err != nil {
			return fmt.Errorf("options_manager: load spread positions for close: %w", err)
		}
		for index := range positions {
			if positions[index].AssetClass == domain.AssetClassOption && positions[index].ClosedAt == nil {
				closePositions[positions[index].Ticker] = &positions[index]
			}
		}
		for _, leg := range spread.Legs {
			position := closePositions[leg.Contract.OCCSymbol]
			if position == nil || position.Quantity != quantity*float64(leg.Ratio) || position.LegGroupID == nil {
				return fmt.Errorf("options_manager: matching open spread position required for %s", leg.Contract.OCCSymbol)
			}
			if existingGroupID == nil {
				existingGroupID = position.LegGroupID
			} else if *existingGroupID != *position.LegGroupID {
				return errors.New("options_manager: close legs do not share one persisted leg group")
			}
		}
	}
	preflight, ok := m.broker.(spreadPreflightBroker)
	if !ok {
		return errors.New("options_manager: spread broker preflight is required before persistence")
	}
	if err := preflight.PreflightSpread(ctx, spread, quantity); err != nil {
		return fmt.Errorf("options_manager: spread preflight: %w", err)
	}
	if !isClosing {
		balanceProvider, ok := m.broker.(optionsBalanceProvider)
		if !ok {
			return errors.New("options_manager: spread broker account balance is required")
		}
		balance, err := balanceProvider.GetAccountBalance(ctx)
		if err != nil || balance.Equity <= 0 {
			return fmt.Errorf("options_manager: valid account balance is required for spread risk: %w", err)
		}
		portfolio, err := BuildRiskPortfolioSnapshotFromBalance(ctx, balance, m.positionRepo)
		if err != nil {
			return fmt.Errorf("options_manager: build spread risk portfolio: %w", err)
		}
		additionalExposure := spread.MaxRisk * quantity / balance.Equity
		if portfolio.MarketExposurePct == nil {
			portfolio.MarketExposurePct = make(map[domain.MarketType]float64)
		}
		portfolio.MarketExposurePct[domain.MarketTypeOptions] += additionalExposure
		approved, reason, err := m.riskEngine.CheckPositionLimits(ctx, spread.Underlying, additionalExposure, portfolio)
		if err != nil {
			return fmt.Errorf("options_manager: check spread position limits: %w", err)
		}
		if !approved {
			return fmt.Errorf("options_manager: spread position limits rejected %s: %s", spread.Underlying, reason)
		}
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
	marketActive, err := m.riskEngine.IsMarketKillSwitchActive(ctx, domain.MarketTypeOptions)
	if err != nil {
		return fmt.Errorf("options_manager: options kill switch check for spread: %w", err)
	}
	if marketActive {
		return fmt.Errorf("options_manager: options kill switch active, spread blocked for %s", spread.Underlying)
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
	if existingGroupID != nil {
		legGroupID = *existingGroupID
	}
	now := time.Now().UTC()
	legOrders := make([]*domain.Order, 0, len(spread.Legs))

	for _, leg := range spread.Legs {
		intent := leg.PositionIntent
		orderType := domain.OrderTypeMarket
		var limitPrice *float64
		if leg.ExecutablePrice > 0 {
			orderType = domain.OrderTypeLimit
			price := leg.ExecutablePrice
			limitPrice = &price
		}
		legOrder := &domain.Order{
			ID:                 uuid.New(),
			StrategyID:         &strategyID,
			PipelineRunID:      &runID,
			Ticker:             leg.Contract.OCCSymbol,
			MarketType:         domain.MarketTypeOptions,
			Side:               leg.Side,
			OrderType:          orderType,
			Quantity:           quantity * float64(leg.Ratio),
			LimitPrice:         limitPrice,
			Status:             domain.OrderStatusPending,
			AssetClass:         domain.AssetClassOption,
			UnderlyingTicker:   leg.Contract.Underlying,
			OptionType:         &leg.Contract.OptionType,
			Strike:             &leg.Contract.Strike,
			Expiry:             &leg.Contract.Expiry,
			ContractMultiplier: leg.Contract.Multiplier,
			OptionGreeks:       &leg.Greeks,
			PositionIntent:     &intent,
			LegGroupID:         &legGroupID,
			CreatedAt:          now,
			Broker:             m.brokerName,
		}

		if err := m.orderRepo.Create(ctx, legOrder); err != nil {
			return fmt.Errorf("options_manager: create leg order: %w", err)
		}
		legOrders = append(legOrders, legOrder)
	}

	// 3. Submit spread to broker.
	ids, err := m.broker.SubmitSpreadOrder(ctx, spread, quantity)
	if err != nil {
		for _, order := range legOrders {
			order.Status = domain.OrderStatusRejected
			_ = m.orderRepo.Update(ctx, order)
		}
		return fmt.Errorf("options_manager: submit spread order: %w", err)
	}
	_, synchronous := m.broker.(OptionFillReporter)
	if len(ids) != len(legOrders) && len(ids) != len(legOrders)+1 {
		persistErr := fmt.Errorf("options_manager: spread broker returned %d ids for %d legs", len(ids), len(legOrders))
		if synchronous {
			return m.abortOptionSpread(ctx, legOrders, ids, persistErr)
		}
		return persistErr
	}
	fillInputs := make([]repository.OptionFillInput, 0, len(legOrders))
	for index, order := range legOrders {
		idIndex := index
		if len(ids) == len(legOrders)+1 {
			idIndex++
		}
		if idIndex < len(ids) {
			order.ExternalID = ids[idIndex]
		} else if len(ids) > 0 {
			order.ExternalID = ids[0]
		}
		order.Broker = m.brokerName
		order.SubmittedAt = &now
		order.Status = domain.OrderStatusSubmitted
		if synchronous {
			if order.LimitPrice == nil {
				return m.abortOptionSpread(ctx, legOrders, ids, errors.New("options_manager: synchronous spread fill requires executable leg prices"))
			}
			order.Status, order.FilledQuantity, order.FilledAvgPrice, order.FilledAt = domain.OrderStatusFilled, order.Quantity, order.LimitPrice, &now
		}
		if synchronous {
			var positionID *uuid.UUID
			if isClosing {
				id := closePositions[order.Ticker].ID
				positionID = &id
			}
			input, err := m.optionFillInput(ctx, order, positionID, func() string {
				if isClosing {
					return "strategy spread close"
				}
				return ""
			}())
			if err != nil {
				return m.abortOptionSpread(ctx, legOrders, ids, fmt.Errorf("options_manager: build spread leg fill: %w", err))
			}
			fillInputs = append(fillInputs, input)
		} else if err := m.orderRepo.Update(ctx, order); err != nil {
			return fmt.Errorf("options_manager: update spread leg order: %w", err)
		}
	}
	if synchronous {
		if _, err := m.optionFillRepo.ApplyOptionFills(ctx, fillInputs); err != nil {
			persistErr := fmt.Errorf("options_manager: persist atomic spread fills: %w", err)
			return m.abortOptionSpread(ctx, legOrders, ids, persistErr)
		}
		if err := m.finalizeOptionSpread(ids); err != nil {
			return err
		}
	}

	m.logger.InfoContext(ctx, "options: spread submitted",
		"underlying", spread.Underlying,
		"strategy", spread.StrategyType,
		"quantity", quantity,
		"order_ids", ids,
	)

	return nil
}

func (m *OptionsOrderManager) compensateOptionOrder(ctx context.Context, externalID string) error {
	compensator, ok := m.broker.(optionFillCompensator)
	if !ok {
		return fmt.Errorf("options_manager: broker cannot roll back option fill")
	}
	if err := compensator.RollbackOptionOrder(ctx, externalID); err != nil {
		return fmt.Errorf("options_manager: roll back option fill: %w", err)
	}
	return nil
}

func (m *OptionsOrderManager) abortOptionOrder(ctx context.Context, order *domain.Order, externalID string, cause error) error {
	rollbackErr := m.compensateOptionOrder(ctx, externalID)
	order.Status, order.FilledQuantity, order.FilledAvgPrice, order.FilledAt = domain.OrderStatusRejected, 0, nil, nil
	persistErr := m.orderRepo.Update(ctx, order)
	if persistErr != nil {
		persistErr = fmt.Errorf("options_manager: persist compensated option rejection: %w", persistErr)
	}
	return errors.Join(cause, rollbackErr, persistErr)
}

func (m *OptionsOrderManager) compensateOptionSpread(ctx context.Context, externalIDs []string) error {
	compensator, ok := m.broker.(optionFillCompensator)
	if !ok {
		return fmt.Errorf("options_manager: broker cannot roll back option spread")
	}
	if err := compensator.RollbackOptionSpread(ctx, externalIDs); err != nil {
		return fmt.Errorf("options_manager: roll back option spread: %w", err)
	}
	return nil
}

func (m *OptionsOrderManager) abortOptionSpread(ctx context.Context, orders []*domain.Order, externalIDs []string, cause error) error {
	errs := []error{cause, m.compensateOptionSpread(ctx, externalIDs)}
	for _, order := range orders {
		order.Status, order.FilledQuantity, order.FilledAvgPrice, order.FilledAt = domain.OrderStatusRejected, 0, nil, nil
		if err := m.orderRepo.Update(ctx, order); err != nil {
			errs = append(errs, fmt.Errorf("options_manager: persist compensated spread rejection %s: %w", order.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (m *OptionsOrderManager) finalizeOptionSpread(externalIDs []string) error {
	compensator, ok := m.broker.(optionFillCompensator)
	if !ok {
		return fmt.Errorf("options_manager: broker cannot finalize option spread")
	}
	if err := compensator.FinalizeOptionSpread(externalIDs); err != nil {
		return fmt.Errorf("options_manager: finalize option spread: %w", err)
	}
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
