package backtest

import (
	marketdata "github.com/PatrickFanella/get-rich-quick/internal/marketdata/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

type FillSide = simulation.FillSide

const (
	FillBuy  = simulation.FillBuy
	FillSell = simulation.FillSell
)

var ErrEmptyBook = simulation.ErrEmptyBook

type DepthFillResult = simulation.DepthFillResult

func FillFromBook(side FillSide, requestedSize float64, book marketdata.BookSnapshot) (DepthFillResult, error) {
	return simulation.FillFromBook(side, requestedSize, book)
}
