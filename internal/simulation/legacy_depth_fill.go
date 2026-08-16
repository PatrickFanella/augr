package simulation

import (
	"errors"
	"math"
	"sort"

	marketdata "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
)

type FillSide string

const (
	FillBuy  FillSide = "BUY"
	FillSell FillSide = "SELL"
)

var ErrEmptyBook = errors.New("backtest: empty book")

type DepthFillResult struct {
	AvgPrice   float64
	FilledSize float64
	LevelsHit  int
	Partial    bool
}

// FillFromBook models event-market fills when a historical order-book snapshot
// is available. Bar-based stock/options simulations use their versioned fill
// assumptions instead; callers must not silently substitute a synthetic book.
func FillFromBook(side FillSide, requestedSize float64, book marketdata.BookSnapshot) (DepthFillResult, error) {
	if requestedSize <= 0 {
		return DepthFillResult{}, nil
	}

	levels := make([]marketdata.Level, 0)
	switch side {
	case FillBuy:
		levels = append(levels, book.Asks...)
		sort.Slice(levels, func(i, j int) bool { return levels[i].Price < levels[j].Price })
	case FillSell:
		levels = append(levels, book.Bids...)
		sort.Slice(levels, func(i, j int) bool { return levels[i].Price > levels[j].Price })
	default:
		return DepthFillResult{}, errors.New("backtest: invalid fill side")
	}

	if len(levels) == 0 {
		return DepthFillResult{}, ErrEmptyBook
	}

	var cumValue, cumSize float64
	levelsHit := 0
	for _, level := range levels {
		if level.Size <= 0 || level.Price <= 0 || math.IsNaN(level.Price) || math.IsInf(level.Price, 0) || math.IsNaN(level.Size) || math.IsInf(level.Size, 0) {
			continue
		}
		remaining := requestedSize - cumSize
		if remaining <= 0 {
			break
		}
		take := math.Min(level.Size, remaining)
		if take <= 0 {
			continue
		}
		cumValue += level.Price * take
		cumSize += take
		levelsHit++
	}

	if cumSize == 0 {
		return DepthFillResult{}, ErrEmptyBook
	}

	return DepthFillResult{
		AvgPrice:   cumValue / cumSize,
		FilledSize: cumSize,
		LevelsHit:  levelsHit,
		Partial:    cumSize < requestedSize,
	}, nil
}
