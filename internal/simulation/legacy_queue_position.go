package simulation

import (
	"errors"
	"math"
	"sync"
	"time"

	marketdata "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
)

type QueueEntry struct {
	OrderID       string
	Slug          string
	Side          FillSide
	Price         float64
	Size          float64
	RemainingSize float64
	SignalTime    time.Time
	AheadVolume   float64
	Filled        bool
	FilledAt      time.Time
}

type QueueTracker struct {
	mu    sync.RWMutex
	items map[string]*QueueEntry
	order []string
}

func NewQueueTracker() *QueueTracker { return &QueueTracker{items: make(map[string]*QueueEntry)} }

func (q *QueueTracker) RegisterPassive(entry QueueEntry, book marketdata.BookSnapshot) error {
	if entry.OrderID == "" {
		return errors.New("backtest: missing order id")
	}
	var ahead float64
	switch entry.Side {
	case FillBuy:
		for _, lvl := range book.Bids {
			if math.Abs(lvl.Price-entry.Price) <= 1e-9 {
				ahead = lvl.Size
				break
			}
		}
	case FillSell:
		for _, lvl := range book.Asks {
			if math.Abs(lvl.Price-entry.Price) <= 1e-9 {
				ahead = lvl.Size
				break
			}
		}
	default:
		return errors.New("backtest: invalid fill side")
	}
	entry.AheadVolume = ahead
	entry.RemainingSize = entry.Size
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items[entry.OrderID] = &entry
	q.order = append(q.order, entry.OrderID)
	return nil
}

// ApplyTrade represents executed prints at a price.
// We model only own-side queue priority; Side is retained for attribution.
func (q *QueueTracker) ApplyTrade(slug string, price, size float64, ts time.Time) {
	if size <= 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	remaining := size
	for _, orderID := range q.order {
		entry := q.items[orderID]
		if entry == nil || entry.Filled || entry.Slug != slug || math.Abs(entry.Price-price) > 1e-9 {
			continue
		}
		drain := math.Min(entry.AheadVolume, remaining)
		entry.AheadVolume -= drain
		remaining -= drain
		if entry.AheadVolume == 0 && remaining > 0 {
			fill := math.Min(entry.RemainingSize, remaining)
			entry.RemainingSize -= fill
			remaining -= fill
			if entry.RemainingSize == 0 {
				entry.Filled = true
				entry.FilledAt = ts
				continue
			}
		}
		if remaining <= 0 {
			break
		}
	}
}

func (q *QueueTracker) Get(orderID string) (QueueEntry, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	entry, ok := q.items[orderID]
	if !ok {
		return QueueEntry{}, false
	}
	return *entry, true
}

func (q *QueueTracker) ActiveCount() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	count := 0
	for _, entry := range q.items {
		if !entry.Filled {
			count++
		}
	}
	return count
}
