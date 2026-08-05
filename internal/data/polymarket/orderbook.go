package polymarket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type flexibleFloat float64

func (f *flexibleFloat) UnmarshalJSON(data []byte) error {
	v, ok, err := parseFlexibleFloat(data)
	if err != nil {
		return err
	}
	if !ok {
		*f = 0
		return nil
	}
	*f = flexibleFloat(v)
	return nil
}

func parseFlexibleFloat(data []byte) (float64, bool, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, false, nil
	}
	if trimmed[0] == '"' {
		var raw string
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return 0, false, err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return 0, false, nil
		}
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return 0, false, err
		}
		return parsed, true, nil
	}
	var parsed float64
	if err := json.Unmarshal(trimmed, &parsed); err == nil {
		return parsed, true, nil
	}
	var num json.Number
	if err := json.Unmarshal(trimmed, &num); err == nil {
		parsed, err := num.Float64()
		if err != nil {
			return 0, false, err
		}
		return parsed, true, nil
	}
	return 0, false, fmt.Errorf("polymarket: invalid numeric value %q", string(trimmed))
}

func normalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeOutcome(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeOutcomes(values []string) []string {
	outcomes := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := normalizeOutcome(value); normalized != "" {
			outcomes = append(outcomes, normalized)
		}
	}
	return outcomes
}

func maxFlexible(values ...flexibleFloat) float64 {
	var maximum float64
	for _, value := range values {
		f := float64(value)
		if f > maximum {
			maximum = f
		}
	}
	return maximum
}

type rawOrderBook struct {
	Slug           string              `json:"slug"`
	Market         string              `json:"market"`
	ConditionID    string              `json:"condition_id"`
	AssetID        string              `json:"asset_id"`
	TokenID        string              `json:"token_id"`
	Outcome        string              `json:"outcome"`
	MinOrderSize   flexibleFloat       `json:"min_order_size"`
	TickSize       flexibleFloat       `json:"tick_size"`
	NegRisk        bool                `json:"neg_risk"`
	LastTradePrice flexibleFloat       `json:"last_trade_price"`
	Bids           []rawOrderBookLevel `json:"bids"`
	Asks           []rawOrderBookLevel `json:"asks"`
}

type rawOrderBookLevel struct {
	Price flexibleFloat `json:"price"`
	Size  flexibleFloat `json:"size"`
}

type OrderBook struct {
	Slug           string
	ConditionID    string
	TokenID        string
	Outcome        string
	MinOrderSize   float64
	TickSize       float64
	NegRisk        bool
	LastTradePrice float64
	Bids           []domain.PolymarketBookLevel
	Asks           []domain.PolymarketBookLevel
}

func (b OrderBook) Snapshot() domain.PolymarketBookSnapshot {
	bids := cloneAndSortLevels(b.Bids, true)
	asks := cloneAndSortLevels(b.Asks, false)
	bestBid := levelPrice(bids)
	bestAsk := levelPrice(asks)
	bidDepth := depthUSD(bids)
	askDepth := depthUSD(asks)
	snapshot := domain.PolymarketBookSnapshot{
		Slug:        normalizeSlug(firstNonEmpty(b.Slug)),
		TokenID:     strings.TrimSpace(b.TokenID),
		Outcome:     normalizeOutcome(b.Outcome),
		BestBid:     bestBid,
		BestAsk:     bestAsk,
		Midpoint:    midpoint(bestBid, bestAsk),
		Spread:      spread(bestBid, bestAsk),
		BidDepthUSD: bidDepth,
		AskDepthUSD: askDepth,
		DepthUSD:    askDepth,
		Bids:        bids,
		Asks:        asks,
	}
	return snapshot
}

func parseOrderBook(body []byte) (OrderBook, error) {
	var raw rawOrderBook
	if err := json.Unmarshal(body, &raw); err != nil {
		return OrderBook{}, fmt.Errorf("polymarket: decode order book: %w", err)
	}
	return normalizeOrderBook(raw), nil
}

func normalizeOrderBook(raw rawOrderBook) OrderBook {
	book := OrderBook{
		Slug:           normalizeSlug(firstNonEmpty(raw.Slug)),
		ConditionID:    strings.TrimSpace(firstNonEmpty(raw.ConditionID, raw.Market)),
		TokenID:        strings.TrimSpace(firstNonEmpty(raw.TokenID, raw.AssetID)),
		Outcome:        normalizeOutcome(raw.Outcome),
		MinOrderSize:   float64(raw.MinOrderSize),
		TickSize:       float64(raw.TickSize),
		NegRisk:        raw.NegRisk,
		LastTradePrice: float64(raw.LastTradePrice),
	}
	if book.TokenID == "" {
		book.TokenID = strings.TrimSpace(firstNonEmpty(raw.AssetID, raw.TokenID))
	}
	book.Bids = make([]domain.PolymarketBookLevel, 0, len(raw.Bids))
	for _, level := range raw.Bids {
		price := float64(level.Price)
		size := float64(level.Size)
		if !isFiniteLevel(price, size) {
			continue
		}
		book.Bids = append(book.Bids, domain.PolymarketBookLevel{Price: price, Size: size})
	}
	book.Asks = make([]domain.PolymarketBookLevel, 0, len(raw.Asks))
	for _, level := range raw.Asks {
		price := float64(level.Price)
		size := float64(level.Size)
		if !isFiniteLevel(price, size) {
			continue
		}
		book.Asks = append(book.Asks, domain.PolymarketBookLevel{Price: price, Size: size})
	}
	return book
}

func cloneAndSortLevels(levels []domain.PolymarketBookLevel, bids bool) []domain.PolymarketBookLevel {
	if len(levels) == 0 {
		return nil
	}
	clone := append([]domain.PolymarketBookLevel(nil), levels...)
	sort.Slice(clone, func(i, j int) bool {
		if bids {
			return clone[i].Price > clone[j].Price
		}
		return clone[i].Price < clone[j].Price
	})
	return clone
}

func levelPrice(levels []domain.PolymarketBookLevel) float64 {
	if len(levels) == 0 {
		return 0
	}
	return levels[0].Price
}

func depthUSD(levels []domain.PolymarketBookLevel) float64 {
	var total float64
	for _, level := range levels {
		if !isFiniteLevel(level.Price, level.Size) {
			continue
		}
		total += level.Price * level.Size
	}
	return total
}

func midpoint(bestBid, bestAsk float64) float64 {
	if !isFiniteLevel(bestBid, 1) || !isFiniteLevel(bestAsk, 1) {
		return 0
	}
	return (bestBid + bestAsk) / 2
}

func spread(bestBid, bestAsk float64) float64 {
	if !isFiniteLevel(bestBid, 1) || !isFiniteLevel(bestAsk, 1) {
		return 0
	}
	value := bestAsk - bestBid
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func isFiniteLevel(price, size float64) bool {
	return !math.IsNaN(price) && !math.IsInf(price, 0) && !math.IsNaN(size) && !math.IsInf(size, 0) && price >= 0 && size >= 0
}
