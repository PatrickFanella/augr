package polymarket

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	marketdata "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
)

type StopGuardConfig struct {
	Broker  templateSender
	Logger  *slog.Logger
	Metrics StopGuardMetrics
}

type StopGuardMetrics interface {
	IncTriggered(slug string)
	IncSendError(slug string)
	ObserveTickToFireSeconds(slug string, seconds float64)
	SetActive(count float64)
}

type Position struct {
	ID           string
	Slug         string
	Side         string
	OutcomeSide  string
	EntryPx      float64
	Size         float64
	StopPx       float64
	TakeProfitPx float64
}

type templateSender interface {
	PrepareTemplate(order *domain.Order) (*OrderTemplate, error)
	SendTemplate(ctx context.Context, tmpl *OrderTemplate) (*CreateOrderResponse, error)
}

type guardState int32

const (
	guardArmed guardState = iota
	guardFiring
	guardFired
)

type guardEntry struct {
	positionID string
	slug       string
	outcome    string
	stopPx     float64
	takePx     float64
	long       bool
	template   *OrderTemplate
	state      atomic.Int32
	receivedAt time.Time
}

type StopGuard struct {
	broker  templateSender
	logger  *slog.Logger
	metrics StopGuardMetrics

	mu     sync.RWMutex
	bySlug map[string][]*guardEntry
	byID   map[string]*guardEntry
	count  atomic.Int32
}

func NewStopGuard(cfg StopGuardConfig) (*StopGuard, error) {
	if cfg.Broker == nil {
		return nil, errors.New("polymarket: stop guard broker is required")
	}
	return &StopGuard{
		broker:  cfg.Broker,
		logger:  cfg.Logger,
		metrics: cfg.Metrics,
		bySlug:  make(map[string][]*guardEntry),
		byID:    make(map[string]*guardEntry),
	}, nil
}

func (g *StopGuard) RegisterEntry(pos Position) error {
	if g == nil {
		return errors.New("polymarket: stop guard is nil")
	}
	positionID := strings.TrimSpace(pos.ID)
	if positionID == "" {
		return errors.New("polymarket: position id is required")
	}
	slug := strings.TrimSpace(pos.Slug)
	if slug == "" {
		return errors.New("polymarket: slug is required")
	}
	if pos.Size <= 0 {
		return errors.New("polymarket: size must be greater than zero")
	}
	if pos.StopPx <= 0 && pos.TakeProfitPx <= 0 {
		return errors.New("polymarket: stop price or take-profit price is required")
	}
	long := strings.EqualFold(strings.TrimSpace(pos.Side), "BUY")
	short := strings.EqualFold(strings.TrimSpace(pos.Side), "SELL")
	if !long && !short {
		return fmt.Errorf("polymarket: unsupported side %q", pos.Side)
	}
	outcome := strings.ToUpper(strings.TrimSpace(pos.OutcomeSide))
	if outcome == "" {
		outcome = "YES"
	}
	if outcome != "YES" && outcome != "NO" {
		return fmt.Errorf("polymarket: unsupported outcome side %q", pos.OutcomeSide)
	}
	intent := "ORDER_INTENT_SELL_LONG"
	switch {
	case outcome == "NO" && long:
		intent = "ORDER_INTENT_SELL_SHORT"
	case outcome == "NO" && short:
		intent = "ORDER_INTENT_BUY_SHORT"
	case short:
		intent = "ORDER_INTENT_BUY_SHORT"
	}
	side := "BUY"
	if long {
		side = "SELL"
	}
	order := &domain.Order{Ticker: slug, Side: domain.OrderSide(side), OrderType: domain.OrderTypeMarket, Quantity: pos.Size, PredictionSide: outcome, PolymarketIntent: intent}
	g.mu.RLock()
	if _, exists := g.byID[positionID]; exists {
		g.mu.RUnlock()
		return nil
	}
	g.mu.RUnlock()
	tmpl, err := g.broker.PrepareTemplate(order)
	if err != nil {
		return err
	}
	entry := &guardEntry{positionID: positionID, slug: slug, outcome: outcome, stopPx: pos.StopPx, takePx: pos.TakeProfitPx, long: long, template: tmpl, receivedAt: time.Now()}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.byID[positionID]; exists {
		return nil
	}
	g.byID[positionID] = entry
	g.bySlug[slug] = append(g.bySlug[slug], entry)
	g.count.Add(1)
	if g.metrics != nil {
		g.metrics.SetActive(float64(g.count.Load()))
	}
	return nil
}

func (g *StopGuard) RegisterPosition(pos domain.Position) error {
	if g == nil {
		return errors.New("polymarket: stop guard is nil")
	}
	positionID := pos.ID.String()
	if positionID == "" || pos.ID == uuid.Nil {
		return errors.New("polymarket: position id is required")
	}
	slug, outcome, err := polymarketPositionParts(pos.Ticker)
	if err != nil {
		return err
	}
	entry := Position{ID: positionID, Slug: slug, OutcomeSide: outcome, EntryPx: pos.AvgEntry, Size: pos.Quantity}
	switch pos.Side {
	case domain.PositionSideLong:
		entry.Side = "BUY"
	case domain.PositionSideShort:
		entry.Side = "SELL"
	default:
		return fmt.Errorf("polymarket: unsupported position side %q", pos.Side)
	}
	if pos.StopLoss != nil {
		entry.StopPx = *pos.StopLoss
	}
	if pos.TakeProfit != nil {
		entry.TakeProfitPx = *pos.TakeProfit
	}
	return g.RegisterEntry(entry)
}

func (g *StopGuard) Cancel(positionID string) {
	if g == nil {
		return
	}
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.byID[positionID]
	if !ok {
		return
	}
	entry.state.Store(int32(guardFired))
	delete(g.byID, positionID)
	entries := g.bySlug[entry.slug]
	for i := range entries {
		if entries[i] == entry {
			entries[i] = entries[len(entries)-1]
			entries = entries[:len(entries)-1]
			break
		}
	}
	if len(entries) == 0 {
		delete(g.bySlug, entry.slug)
	} else {
		g.bySlug[entry.slug] = entries
	}
	g.count.Add(-1)
	if g.metrics != nil {
		g.metrics.SetActive(float64(g.count.Load()))
	}
}

func (g *StopGuard) Active() int {
	if g == nil {
		return 0
	}
	return int(g.count.Load())
}

func (g *StopGuard) OnTick(ctx context.Context, t marketdata.Tick) {
	if g == nil {
		return
	}
	g.mu.RLock()
	entries := append([]*guardEntry(nil), g.bySlug[t.Slug]...)
	g.mu.RUnlock()
	for _, entry := range entries {
		if !tickMatchesOutcome(t, entry.outcome) {
			continue
		}
		if entry == nil || !entry.state.CompareAndSwap(int32(guardArmed), int32(guardFiring)) {
			continue
		}
		crossed := (entry.long && ((entry.stopPx > 0 && t.Price <= entry.stopPx) || (entry.takePx > 0 && t.Price >= entry.takePx))) ||
			(!entry.long && ((entry.stopPx > 0 && t.Price >= entry.stopPx) || (entry.takePx > 0 && t.Price <= entry.takePx)))
		if !crossed {
			entry.state.Store(int32(guardArmed))
			continue
		}
		if g.metrics != nil {
			g.metrics.IncTriggered(entry.slug)
		}
		if g.metrics != nil {
			g.metrics.ObserveTickToFireSeconds(entry.slug, time.Since(t.ReceivedAt).Seconds())
		}
		_, err := g.broker.SendTemplate(ctx, entry.template)
		if err != nil {
			if g.metrics != nil {
				g.metrics.IncSendError(entry.slug)
			}
			if g.logger != nil {
				g.logger.Error("polymarket stop guard send failed", "slug", entry.slug, "position_id", entry.positionID, "err", err)
			}
			entry.state.Store(int32(guardArmed))
			continue
		}
		entry.state.Store(int32(guardFired))
		g.Cancel(entry.positionID)
	}
}

func polymarketPositionParts(ticker string) (string, string, error) {
	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return "", "", errors.New("polymarket: ticker is required")
	}
	slug, outcome, found := strings.Cut(ticker, ":")
	slug = strings.TrimSpace(slug)
	outcome = strings.ToUpper(strings.TrimSpace(outcome))
	if !found || slug == "" || (outcome != "YES" && outcome != "NO") {
		return "", "", fmt.Errorf("polymarket: ticker %q is not a polymarket position ticker", ticker)
	}
	return slug, outcome, nil
}

func tickMatchesOutcome(t marketdata.Tick, outcome string) bool {
	side := strings.ToUpper(strings.TrimSpace(t.Side))
	if side != "YES" && side != "NO" {
		return false
	}
	return side == strings.ToUpper(strings.TrimSpace(outcome))
}
