package backtest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

var (
	ErrNilFillEngine = errors.New("backtest: fill engine is required")
	ErrMissingBar    = errors.New("backtest: current market bar is required")
)

// BrokerAdapter implements execution.Broker using the backtest fill engine.
type BrokerAdapter struct {
	mu          sync.RWMutex
	orders      map[string]*domain.Order
	positions   map[string]*domain.Position
	bars        map[string]domain.OHLCV
	balance     execution.Balance
	fillEngine  *FillEngine
	nextOrderID uint64
}

var _ execution.Broker = (*BrokerAdapter)(nil)

func NewBrokerAdapter(initialBalance float64, fillEngine *FillEngine) (*BrokerAdapter, error) {
	if fillEngine == nil {
		return nil, ErrNilFillEngine
	}

	return &BrokerAdapter{
		orders:    make(map[string]*domain.Order),
		positions: make(map[string]*domain.Position),
		bars:      make(map[string]domain.OHLCV),
		balance: execution.Balance{
			Currency:    "USD",
			Cash:        initialBalance,
			BuyingPower: initialBalance,
			Equity:      initialBalance,
		},
		fillEngine: fillEngine,
	}, nil
}

func (b *BrokerAdapter) SetMarketBar(ticker string, bar domain.OHLCV) error {
	if b == nil {
		return errors.New("backtest: broker is required")
	}
	if bar.Close <= 0 {
		return ErrInvalidBar
	}

	normalizedTicker, err := normalizeTicker(ticker)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.bars[normalizedTicker] = bar
	if position, ok := b.positions[normalizedTicker]; ok {
		position.CurrentPrice = floatPtr(bar.Close)
	}
	b.balance.Equity = b.markToMarketEquityLocked()
	return nil
}

func (b *BrokerAdapter) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil {
		return "", errors.New("backtest: broker is required")
	}
	if order == nil {
		return "", errors.New("backtest: order is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("backtest: submit order: %w", err)
	}

	ticker, err := normalizeTicker(order.Ticker)
	if err != nil {
		return "", err
	}
	if err := validateOrder(order); err != nil {
		return "", err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	externalID := b.nextExternalIDLocked()
	order.Ticker = ticker
	order.ExternalID = externalID
	order.Status = domain.OrderStatusSubmitted

	bar, ok := b.bars[ticker]
	if !ok {
		order.SubmittedAt = timePtr(time.Now().UTC())
		if order.CreatedAt.IsZero() {
			order.CreatedAt = *order.SubmittedAt
		}
		order.Status = domain.OrderStatusRejected
		b.orders[externalID] = cloneOrder(order)
		return externalID, fmt.Errorf("%w for %s", ErrMissingBar, ticker)
	}
	order.SubmittedAt = timePtr(currentBarTime(bar))
	if order.CreatedAt.IsZero() {
		order.CreatedAt = *order.SubmittedAt
	}
	order.FilledQuantity = 0
	order.FilledAvgPrice = nil
	order.FilledAt = nil

	result, err := b.fillEngine.SimulateFill(order, bar)
	switch {
	case err == nil:
	case errors.Is(err, ErrNoFill):
		b.orders[externalID] = cloneOrder(order)
		return externalID, nil
	default:
		order.Status = domain.OrderStatusRejected
		b.orders[externalID] = cloneOrder(order)
		return externalID, fmt.Errorf("backtest: simulate fill: %w", err)
	}

	if order.Side == domain.OrderSideBuy && b.balance.Cash < result.TotalCost {
		order.Status = domain.OrderStatusRejected
		b.orders[externalID] = cloneOrder(order)
		return externalID, fmt.Errorf("backtest: insufficient balance: need %.2f, have %.2f", result.TotalCost, b.balance.Cash)
	}

	if order.Side == domain.OrderSideBuy {
		b.balance.Cash -= result.TotalCost
	} else {
		b.balance.Cash += result.TotalCost
	}

	if result.Partial {
		order.Status = domain.OrderStatusPartial
	} else {
		order.Status = domain.OrderStatusFilled
	}
	order.FilledQuantity = result.FillQuantity
	order.FilledAvgPrice = floatPtr(result.FillPrice)
	order.FilledAt = timePtr(currentBarTime(bar))

	b.applyFillLocked(ticker, order.Side, result.FillQuantity, result.FillPrice, bar.Close, *order.FilledAt)
	b.balance.BuyingPower = b.balance.Cash
	b.balance.Equity = b.markToMarketEquityLocked()
	b.orders[externalID] = cloneOrder(order)

	return externalID, nil
}

func (b *BrokerAdapter) CancelOrder(ctx context.Context, externalID string) error {
	if b == nil {
		return errors.New("backtest: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("backtest: cancel order: %w", err)
	}

	id := strings.TrimSpace(externalID)
	if id == "" {
		return errors.New("backtest: external order id is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	order, ok := b.orders[id]
	if !ok {
		return fmt.Errorf("backtest: order %q not found", id)
	}
	if order.Status == domain.OrderStatusFilled || order.Status == domain.OrderStatusRejected {
		return fmt.Errorf("backtest: order %q cannot be cancelled from status %q", id, order.Status)
	}

	order.Status = domain.OrderStatusCancelled
	b.orders[id] = cloneOrder(order)
	return nil
}

func (b *BrokerAdapter) GetOrderStatus(ctx context.Context, externalID string) (domain.OrderStatus, error) {
	if b == nil {
		return "", errors.New("backtest: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("backtest: get order status: %w", err)
	}

	id := strings.TrimSpace(externalID)
	if id == "" {
		return "", errors.New("backtest: external order id is required")
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	order, ok := b.orders[id]
	if !ok {
		return "", fmt.Errorf("backtest: order %q not found", id)
	}

	return order.Status, nil
}

func (b *BrokerAdapter) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b == nil {
		return nil, errors.New("backtest: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("backtest: get positions: %w", err)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	tickers := make([]string, 0, len(b.positions))
	for ticker := range b.positions {
		tickers = append(tickers, ticker)
	}
	sort.Strings(tickers)

	positions := make([]domain.Position, 0, len(tickers))
	for _, ticker := range tickers {
		positions = append(positions, *clonePosition(b.positions[ticker]))
	}

	return positions, nil
}

func (b *BrokerAdapter) GetAccountBalance(ctx context.Context) (execution.Balance, error) {
	if b == nil {
		return execution.Balance{}, errors.New("backtest: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return execution.Balance{}, fmt.Errorf("backtest: get account balance: %w", err)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	balance := b.balance
	balance.Equity = b.markToMarketEquityLocked()
	return balance, nil
}

func (b *BrokerAdapter) nextExternalIDLocked() string {
	b.nextOrderID++
	return fmt.Sprintf("backtest-%d", b.nextOrderID)
}

func (b *BrokerAdapter) applyFillLocked(ticker string, side domain.OrderSide, quantity float64, fillPrice float64, markPrice float64, filledAt time.Time) {
	currentPrice := floatPtr(markPrice)
	position, ok := b.positions[ticker]
	if !ok {
		b.positions[ticker] = &domain.Position{
			ID:           uuid.New(),
			Ticker:       ticker,
			Side:         sideToPositionSide(side),
			Quantity:     quantity,
			AvgEntry:     fillPrice,
			CurrentPrice: currentPrice,
			OpenedAt:     filledAt,
		}
		return
	}

	position.CurrentPrice = currentPrice
	fillSide := sideToPositionSide(side)
	if position.Side == fillSide {
		totalQuantity := position.Quantity + quantity
		position.AvgEntry = ((position.AvgEntry * position.Quantity) + (fillPrice * quantity)) / totalQuantity
		position.Quantity = totalQuantity
		return
	}

	closedQuantity := math.Min(position.Quantity, quantity)
	position.RealizedPnL += realizedPnL(position.Side, position.AvgEntry, fillPrice, closedQuantity)
	carryOverPnL := position.RealizedPnL

	if position.Quantity > quantity {
		position.Quantity -= quantity
		return
	}
	if position.Quantity == quantity {
		delete(b.positions, ticker)
		return
	}

	remainingQuantity := quantity - position.Quantity
	b.positions[ticker] = &domain.Position{
		ID:           uuid.New(),
		Ticker:       ticker,
		Side:         fillSide,
		Quantity:     remainingQuantity,
		AvgEntry:     fillPrice,
		CurrentPrice: currentPrice,
		OpenedAt:     filledAt,
		RealizedPnL:  carryOverPnL,
	}
}

func (b *BrokerAdapter) markToMarketEquityLocked() float64 {
	equity := b.balance.Cash
	for ticker, position := range b.positions {
		price := position.AvgEntry
		if bar, ok := b.bars[ticker]; ok && bar.Close > 0 {
			price = bar.Close
		} else if position.CurrentPrice != nil {
			price = *position.CurrentPrice
		}

		if position.Side == domain.PositionSideLong {
			equity += position.Quantity * price
			continue
		}
		equity -= position.Quantity * price
	}
	return equity
}

func normalizeTicker(ticker string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(ticker))
	if normalized == "" {
		return "", errors.New("backtest: order ticker is required")
	}
	return normalized, nil
}

func validateOrder(order *domain.Order) error {
	switch order.Side {
	case domain.OrderSideBuy, domain.OrderSideSell:
	default:
		return fmt.Errorf("backtest: unsupported order side %q", order.Side)
	}
	if order.Quantity <= 0 {
		return errors.New("backtest: order quantity must be greater than zero")
	}
	return nil
}

func realizedPnL(side domain.PositionSide, avgEntry float64, fillPrice float64, quantity float64) float64 {
	if side == domain.PositionSideLong {
		return (fillPrice - avgEntry) * quantity
	}
	return (avgEntry - fillPrice) * quantity
}

func sideToPositionSide(side domain.OrderSide) domain.PositionSide {
	if side == domain.OrderSideSell {
		return domain.PositionSideShort
	}
	return domain.PositionSideLong
}

func cloneOrder(order *domain.Order) *domain.Order {
	if order == nil {
		return nil
	}
	cloned := *order
	cloned.LimitPrice = cloneFloatPtr(order.LimitPrice)
	cloned.StopPrice = cloneFloatPtr(order.StopPrice)
	cloned.FilledAvgPrice = cloneFloatPtr(order.FilledAvgPrice)
	cloned.SubmittedAt = cloneTimePtr(order.SubmittedAt)
	cloned.FilledAt = cloneTimePtr(order.FilledAt)
	return &cloned
}

func clonePosition(position *domain.Position) *domain.Position {
	if position == nil {
		return nil
	}
	cloned := *position
	cloned.CurrentPrice = cloneFloatPtr(position.CurrentPrice)
	cloned.UnrealizedPnL = cloneFloatPtr(position.UnrealizedPnL)
	cloned.StopLoss = cloneFloatPtr(position.StopLoss)
	cloned.TakeProfit = cloneFloatPtr(position.TakeProfit)
	cloned.ClosedAt = cloneTimePtr(position.ClosedAt)
	return &cloned
}

func cloneFloatPtr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func floatPtr(value float64) *float64 {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func currentBarTime(bar domain.OHLCV) time.Time {
	if bar.Timestamp.IsZero() {
		return time.Now().UTC()
	}
	return bar.Timestamp.UTC()
}
