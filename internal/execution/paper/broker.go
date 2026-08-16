package paper

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

	"github.com/PatrickFanella/get-rich-quick/internal/accountingrecon"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

const (
	bpsToDecimalDivisor = 10000
	minFillPrice        = 1e-9
)

// PaperBroker implements an in-memory execution.Broker for paper trading.
type PaperBroker struct {
	mu            sync.RWMutex
	nowMu         sync.RWMutex
	orders        map[string]*domain.Order
	positions     map[string]*domain.Position
	optionSpreads map[string]float64
	balance       execution.Balance
	slippageBps   float64
	feePct        float64
	evaluation    domain.PaperEvaluationProfile
	nextOrderID   uint64
	now           func() time.Time
}

// NewPaperBroker constructs an in-memory paper trading broker.
func NewPaperBroker(initialBalance, slippageBps, feePct float64) *PaperBroker {
	if slippageBps < 0 {
		slippageBps = 0
	}
	if feePct < 0 {
		feePct = 0
	}
	return newPaperBroker(domain.PaperEvaluationProfile{
		Mode:                  domain.PaperEvaluationModeScored,
		StorageNamespace:      string(domain.PaperEvaluationModeScored),
		EvidenceClass:         domain.PaperEvidenceClassPromotion,
		InitialCapital:        initialBalance,
		BuyingPowerMultiplier: 1,
		SlippageBPS:           slippageBps,
		FeePct:                feePct,
	})
}

// NewPaperBrokerWithProfile constructs a broker carrying an explicit evidence
// namespace. Phase 0 applies profile capital and execution costs; the later
// account/ledger phase will enforce its buying-power policy.
func NewPaperBrokerWithProfile(profile domain.PaperEvaluationProfile) (*PaperBroker, error) {
	validated, err := domain.NewPaperEvaluationProfile(
		profile.Mode,
		profile.InitialCapital,
		profile.BuyingPowerMultiplier,
		profile.SlippageBPS,
		profile.FeePct,
	)
	if err != nil {
		return nil, err
	}
	return newPaperBroker(validated), nil
}

func newPaperBroker(profile domain.PaperEvaluationProfile) *PaperBroker {
	return &PaperBroker{
		orders:        make(map[string]*domain.Order),
		positions:     make(map[string]*domain.Position),
		optionSpreads: make(map[string]float64),
		balance: execution.Balance{
			Currency:    "USD",
			Cash:        profile.InitialCapital,
			BuyingPower: profile.InitialCapital,
			Equity:      profile.InitialCapital,
		},
		slippageBps: profile.SlippageBPS,
		feePct:      profile.FeePct,
		evaluation:  profile,
		now:         time.Now,
	}
}

// EvaluationProfile returns the broker's immutable scored-or-stress identity.
func (b *PaperBroker) EvaluationProfile() domain.PaperEvaluationProfile {
	if b == nil {
		return domain.PaperEvaluationProfile{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.evaluation
}

// RestoreOrders replaces the in-memory order book during startup.
func (b *PaperBroker) RestoreOrders(orders []domain.Order) error {
	if b == nil {
		return errors.New("paper: broker is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.orders == nil {
		b.orders = make(map[string]*domain.Order, len(orders))
	}
	for i := range orders {
		order := cloneOrder(&orders[i])
		if order == nil || strings.TrimSpace(order.ExternalID) == "" {
			return errors.New("paper: restored order requires external id")
		}
		b.orders[order.ExternalID] = order
		if seq, ok := parsePaperSequenceLocal(order.ExternalID); ok && seq > b.nextOrderID {
			b.nextOrderID = seq
		}
	}
	return nil
}

func parsePaperSequenceLocal(externalID string) (uint64, bool) {
	externalID = strings.TrimSpace(externalID)
	if !strings.HasPrefix(externalID, "paper-") {
		return 0, false
	}
	var seq uint64
	if _, err := fmt.Sscanf(strings.TrimPrefix(externalID, "paper-"), "%d", &seq); err != nil {
		return 0, false
	}
	return seq, true
}

// SetNowFunc overrides the broker time source, allowing callers to inject a
// simulated clock during backtests.
func (b *PaperBroker) SetNowFunc(now func() time.Time) {
	if b == nil || now == nil {
		return
	}

	b.nowMu.Lock()
	defer b.nowMu.Unlock()

	b.now = now
}

// RestoreAccount replaces the in-memory paper account snapshot after durable
// orders, trades, and positions have been reconciled during startup.
func (b *PaperBroker) RestoreAccount(balance execution.Balance) error {
	if b == nil {
		return errors.New("paper: broker is required")
	}
	if balance.Cash < 0 || balance.BuyingPower < 0 || balance.Equity < 0 {
		return errors.New("paper: restored account values must be non-negative")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.balance = balance
	return nil
}

// RestorePositions replaces the open paper position book during startup
// reconstruction.
func (b *PaperBroker) RestorePositions(positions []domain.Position) error {
	if b == nil {
		return errors.New("paper: broker is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.positions = make(map[string]*domain.Position, len(positions))
	grouped := make(map[string]*domain.Position)
	for i := range positions {
		position := clonePosition(&positions[i])
		if position == nil {
			continue
		}
		if position.Ticker == "" || position.Quantity <= 0 {
			return fmt.Errorf("paper: restored position must have ticker and positive quantity")
		}
		key := positionKey(position)
		if existing, ok := grouped[key]; ok {
			merged, err := mergeRestoredPositions(existing, position)
			if err != nil {
				return err
			}
			grouped[key] = merged
			continue
		}
		grouped[key] = position
	}
	for key, position := range grouped {
		b.positions[key] = position
	}
	return nil
}

// RestoreOrderSequence restores the next external paper order sequence number.
func (b *PaperBroker) RestoreOrderSequence(nextOrderID uint64) error {
	if b == nil {
		return errors.New("paper: broker is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextOrderID = nextOrderID
	return nil
}

// SubmitOrder simulates an immediate paper-trading fill when the order is marketable.
func (b *PaperBroker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil {
		return "", errors.New("paper: broker is required")
	}
	if order == nil {
		return "", errors.New("paper: order is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("paper: submit order: %w", err)
	}

	baseTicker, err := normalizeTicker(order.Ticker)
	if err != nil {
		return "", err
	}
	baseTicker, normalizedSide, err := execution.NormalizePredictionOrderTicker(order.MarketType, baseTicker, order.PredictionSide)
	if err != nil {
		return "", err
	}
	order.PredictionSide = normalizedSide
	positionTicker, err := canonicalPositionTicker(order.MarketType, baseTicker, order.PredictionSide)
	if err != nil {
		return "", err
	}
	if err := validateOrder(order); err != nil {
		return "", err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.currentTime().UTC()
	externalID := b.nextExternalIDLocked()

	order.ExternalID = externalID
	order.Status = domain.OrderStatusSubmitted
	order.SubmittedAt = timePtr(now)
	order.Ticker = baseTicker
	if order.CreatedAt.IsZero() {
		order.CreatedAt = now
	}
	order.FilledQuantity = 0
	order.FilledAvgPrice = nil
	order.FilledAt = nil

	fillPrice, shouldFill, err := b.simulateFillPrice(order)
	if err != nil {
		order.Status = domain.OrderStatusRejected
		b.orders[externalID] = cloneOrder(order)
		return externalID, err
	}
	if !shouldFill {
		b.orders[externalID] = cloneOrder(order)
		return externalID, nil
	}

	notional := fillPrice * order.Quantity
	fee := notional * b.feePct
	if order.Side == domain.OrderSideBuy {
		totalCost := notional + fee
		if b.balance.Cash < totalCost {
			order.Status = domain.OrderStatusRejected
			b.orders[externalID] = cloneOrder(order)
			return externalID, fmt.Errorf("paper: insufficient balance: need %.2f, have %.2f", totalCost, b.balance.Cash)
		}
		b.balance.Cash -= totalCost
	} else {
		b.balance.Cash += notional - fee
	}

	order.Status = domain.OrderStatusFilled
	order.FilledQuantity = order.Quantity
	order.FilledAvgPrice = floatPtr(fillPrice)
	order.FilledAt = timePtr(now)

	b.applyFillLocked(positionTicker, order.MarketType, order.Side, order.Quantity, fillPrice, now)
	b.balance.BuyingPower = b.balance.Cash
	b.balance.Equity = b.markToMarketEquityLocked()
	b.orders[externalID] = cloneOrder(order)

	return externalID, nil
}

// CancelOrder cancels an existing resting paper order.
func (b *PaperBroker) CancelOrder(ctx context.Context, externalID string) error {
	if b == nil {
		return errors.New("paper: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("paper: cancel order: %w", err)
	}

	id := strings.TrimSpace(externalID)
	if id == "" {
		return errors.New("paper: external order id is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	order, ok := b.orders[id]
	if !ok {
		return fmt.Errorf("paper: order %q not found", id)
	}
	if order.Status == domain.OrderStatusFilled || order.Status == domain.OrderStatusRejected {
		return fmt.Errorf("paper: order %q cannot be cancelled from status %q", id, order.Status)
	}

	order.Status = domain.OrderStatusCancelled
	b.orders[id] = cloneOrder(order)
	return nil
}

// GetOrderStatus returns the tracked paper order status.
func (b *PaperBroker) GetOrderStatus(ctx context.Context, externalID string) (domain.OrderStatus, error) {
	if b == nil {
		return "", errors.New("paper: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("paper: get order status: %w", err)
	}

	id := strings.TrimSpace(externalID)
	if id == "" {
		return "", errors.New("paper: external order id is required")
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	order, ok := b.orders[id]
	if !ok {
		return "", fmt.Errorf("paper: order %q not found", id)
	}

	return order.Status, nil
}

// GetPositions returns a copy of the current open paper positions.
func (b *PaperBroker) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b == nil {
		return nil, errors.New("paper: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("paper: get positions: %w", err)
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

// GetAccountBalance returns the paper account balance snapshot.
func (b *PaperBroker) GetAccountBalance(ctx context.Context) (execution.Balance, error) {
	if b == nil {
		return execution.Balance{}, errors.New("paper: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return execution.Balance{}, fmt.Errorf("paper: get account balance: %w", err)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.balance, nil
}

// CaptureLegacyAccounting returns balance and open positions under one read
// lock so the two parts cannot describe different paper-broker mutations.
// Cross-system atomicity with the ledger still requires the OVR-105 external
// account-scoped capture fence; this method alone does not provide it.
func (b *PaperBroker) CaptureLegacyAccounting(ctx context.Context) (accountingrecon.LegacyCapture, error) {
	if b == nil {
		return accountingrecon.LegacyCapture{}, errors.New("paper: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return accountingrecon.LegacyCapture{}, fmt.Errorf("paper: capture legacy accounting: %w", err)
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
	return accountingrecon.LegacyCapture{
		Balance: accountingrecon.LegacyBalance{
			Currency: b.balance.Currency, Cash: b.balance.Cash,
			BuyingPower: b.balance.BuyingPower, Equity: b.balance.Equity,
		},
		Positions: positions, CapturedAt: b.currentTime().UTC().Truncate(time.Microsecond),
	}, nil
}

func (b *PaperBroker) nextExternalIDLocked() string {
	b.nextOrderID++
	return fmt.Sprintf("paper-%d", b.nextOrderID)
}

func (b *PaperBroker) currentTime() time.Time {
	if b == nil {
		return time.Now()
	}

	b.nowMu.RLock()
	defer b.nowMu.RUnlock()

	if b.now == nil {
		return time.Now()
	}

	return b.now()
}

func (b *PaperBroker) simulateFillPrice(order *domain.Order) (float64, bool, error) {
	switch order.OrderType {
	case domain.OrderTypeMarket:
		referencePrice, ok := resolveReferencePrice(order)
		if !ok {
			return 0, false, errors.New("paper: executable reference price is required for market order")
		}
		return applySlippage(referencePrice, order.Side, b.slippageBps), true, nil
	case domain.OrderTypeLimit:
		if order.LimitPrice == nil {
			return 0, false, errors.New("paper: limit order requires limit price")
		}
		if *order.LimitPrice <= 0 {
			return 0, false, errors.New("paper: limit price must be greater than zero")
		}

		limitPrice := *order.LimitPrice
		referencePrice, ok := resolveReferencePrice(order)
		if !ok {
			return 0, false, nil
		}
		if !limitCrossed(order.Side, referencePrice, limitPrice) {
			return 0, false, nil
		}

		slippedPrice := applySlippage(referencePrice, order.Side, b.slippageBps)
		if order.Side == domain.OrderSideBuy {
			return math.Min(slippedPrice, limitPrice), true, nil
		}
		return math.Max(slippedPrice, limitPrice), true, nil
	default:
		return 0, false, fmt.Errorf("paper: unsupported order type %q", order.OrderType)
	}
}

func (b *PaperBroker) applyFillLocked(ticker string, marketType domain.MarketType, side domain.OrderSide, quantity, fillPrice float64, filledAt time.Time) {
	currentPrice := floatPtr(fillPrice)
	position, ok := b.positions[ticker]
	if !ok {
		b.positions[ticker] = &domain.Position{
			ID:           uuid.New(),
			Ticker:       ticker,
			MarketType:   marketType,
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
		MarketType:   marketType,
		Side:         fillSide,
		Quantity:     remainingQuantity,
		AvgEntry:     fillPrice,
		CurrentPrice: currentPrice,
		OpenedAt:     filledAt,
		RealizedPnL:  carryOverPnL,
	}
}

func (b *PaperBroker) markToMarketEquityLocked() float64 {
	equity := b.balance.Cash
	for _, position := range b.positions {
		price := position.AvgEntry
		if position.CurrentPrice != nil {
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

func positionKey(position *domain.Position) string {
	if position == nil {
		return ""
	}
	return canonicalPositionKey(position.MarketType, position.Ticker, "")
}

func mergeRestoredPositions(a, b *domain.Position) (*domain.Position, error) {
	if a == nil || b == nil {
		return nil, errors.New("paper: restored position is required")
	}
	if a.Ticker != b.Ticker || a.Side != b.Side || a.AssetClass != b.AssetClass || a.MarketType != b.MarketType {
		return nil, fmt.Errorf("paper: irreconcilable restored positions for %s", a.Ticker)
	}
	a.Quantity += b.Quantity
	a.AvgEntry = ((a.AvgEntry * (a.Quantity - b.Quantity)) + (b.AvgEntry * b.Quantity)) / a.Quantity
	if a.CurrentPrice == nil {
		a.CurrentPrice = cloneFloatPtr(b.CurrentPrice)
	} else if b.CurrentPrice != nil && *a.CurrentPrice != *b.CurrentPrice {
		return nil, fmt.Errorf("paper: irreconcilable restored current prices for %s", a.Ticker)
	}
	a.UnrealizedPnL = nil
	if a.CurrentPrice != nil {
		pnl := (*a.CurrentPrice - a.AvgEntry) * a.Quantity
		if a.Side == domain.PositionSideShort {
			pnl = (a.AvgEntry - *a.CurrentPrice) * a.Quantity
		}
		a.UnrealizedPnL = floatPtr(pnl)
	}
	return a, nil
}

func validateOrder(order *domain.Order) error {
	switch order.Side {
	case domain.OrderSideBuy, domain.OrderSideSell:
	default:
		return fmt.Errorf("paper: unsupported order side %q", order.Side)
	}
	if order.Quantity <= 0 {
		return errors.New("paper: order quantity must be greater than zero")
	}
	return nil
}

func normalizeTicker(ticker string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(ticker))
	if normalized == "" {
		return "", errors.New("paper: order ticker is required")
	}
	return normalized, nil
}

func canonicalPositionTicker(marketType domain.MarketType, ticker, predictionSide string) (string, error) {
	normalizedTicker, err := normalizeTicker(ticker)
	if err != nil {
		return "", err
	}
	if marketType.Normalize() != domain.MarketTypePolymarket && marketType.Normalize() != domain.MarketTypeKalshi {
		return normalizedTicker, nil
	}
	if slug, side, ok := splitPredictionTicker(normalizedTicker); ok {
		return slug + ":" + side, nil
	}
	side := strings.ToUpper(strings.TrimSpace(predictionSide))
	if side != "YES" && side != "NO" {
		return normalizedTicker, nil
	}
	return normalizedTicker + ":" + side, nil
}

func canonicalPositionKey(marketType domain.MarketType, ticker, predictionSide string) string {
	key, err := canonicalPositionTicker(marketType, ticker, predictionSide)
	if err != nil {
		return strings.ToUpper(strings.TrimSpace(ticker))
	}
	return key
}

func splitPredictionTicker(ticker string) (string, string, bool) {
	slug, side, found := strings.Cut(ticker, ":")
	slug = strings.TrimSpace(slug)
	side = strings.ToUpper(strings.TrimSpace(side))
	if !found || slug == "" || (side != "YES" && side != "NO") {
		return "", "", false
	}
	return strings.ToUpper(slug), side, true
}

func resolveReferencePrice(order *domain.Order) (float64, bool) {
	if order.ReferencePrice != nil && *order.ReferencePrice > 0 {
		return *order.ReferencePrice, true
	}
	if order.FilledAvgPrice != nil && *order.FilledAvgPrice > 0 {
		return *order.FilledAvgPrice, true
	}
	// For market orders, LimitPrice carries the intended entry price set by the
	// order manager. For limit orders it represents the limit itself and must
	// NOT be used as the market reference (that would be a self-comparison).
	if order.OrderType == domain.OrderTypeMarket && order.LimitPrice != nil && *order.LimitPrice > 0 {
		return *order.LimitPrice, true
	}
	if order.StopPrice != nil && *order.StopPrice > 0 {
		return *order.StopPrice, true
	}
	return 0, false
}

func limitCrossed(side domain.OrderSide, referencePrice, limitPrice float64) bool {
	if side == domain.OrderSideBuy {
		return referencePrice <= limitPrice
	}
	return referencePrice >= limitPrice
}

func applySlippage(price float64, side domain.OrderSide, slippageBps float64) float64 {
	slippageFraction := slippageBps / bpsToDecimalDivisor
	if side == domain.OrderSideBuy {
		return price * (1 + slippageFraction)
	}
	return math.Max(price*(1-slippageFraction), minFillPrice)
}

func realizedPnL(side domain.PositionSide, avgEntry, fillPrice, quantity float64) float64 {
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
	cloned.ReferencePrice = cloneFloatPtr(order.ReferencePrice)
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
