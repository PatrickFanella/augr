package backtest

import (
	"math"
	"sort"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// HoldingPeriodStats summarizes the distribution of closed-trade holding
// periods.
type HoldingPeriodStats struct {
	Min    time.Duration
	Max    time.Duration
	Mean   time.Duration
	Median time.Duration
}

// TradeAnalytics contains trade-level diagnostics derived from backtest
// executions.
type TradeAnalytics struct {
	HoldingPeriods       HoldingPeriodStats
	ClosedTrades         int
	TradeFrequencyPerDay float64
	LargestSingleWin     float64
	LargestSingleLoss    float64
	MaxConsecutiveWins   int
	MaxConsecutiveLosses int
}

type openLot struct {
	isLong          bool
	quantity        float64
	entryPrice      float64
	openedAt        time.Time
	entryFeePerUnit float64
}

type closedTrade struct {
	pnl        float64
	holdingFor time.Duration
}

// ComputeTradeAnalytics derives closed-trade analytics from the backtest trade
// log. Trades are matched FIFO per ticker to form closed trades.
func ComputeTradeAnalytics(trades []domain.Trade, periodStart, periodEnd time.Time) TradeAnalytics {
	if len(trades) == 0 {
		return TradeAnalytics{}
	}

	sortedTrades := append([]domain.Trade(nil), trades...)
	sort.SliceStable(sortedTrades, func(i, j int) bool {
		return sortedTrades[i].ExecutedAt.Before(sortedTrades[j].ExecutedAt)
	})

	openLotsByTicker := make(map[string][]openLot, len(sortedTrades))
	closed := make([]closedTrade, 0, len(sortedTrades))

	for _, trade := range sortedTrades {
		if trade.Quantity <= 0 || trade.Price <= 0 {
			continue
		}

		isBuy := trade.Side == domain.OrderSideBuy
		remaining := trade.Quantity
		closeFeePerUnit := 0.0
		if trade.Fee > 0 {
			closeFeePerUnit = trade.Fee / trade.Quantity
		}

		lots := openLotsByTicker[trade.Ticker]
		for remaining > 0 && len(lots) > 0 && lots[0].isLong != isBuy {
			lot := lots[0]
			closedQty := math.Min(remaining, lot.quantity)

			gross := (trade.Price - lot.entryPrice) * closedQty
			if !lot.isLong {
				gross = (lot.entryPrice - trade.Price) * closedQty
			}
			pnl := gross - (lot.entryFeePerUnit+closeFeePerUnit)*closedQty

			holdingFor := time.Duration(0)
			if !trade.ExecutedAt.IsZero() && !lot.openedAt.IsZero() && !trade.ExecutedAt.Before(lot.openedAt) {
				holdingFor = trade.ExecutedAt.Sub(lot.openedAt)
			}

			closed = append(closed, closedTrade{
				pnl:        pnl,
				holdingFor: holdingFor,
			})

			remaining -= closedQty
			lot.quantity -= closedQty
			if lot.quantity <= 1e-12 {
				lots = lots[1:]
			} else {
				lots[0] = lot
			}
		}

		if remaining > 0 {
			entryFeePerUnit := 0.0
			if trade.Fee > 0 {
				entryFeePerUnit = trade.Fee / trade.Quantity
			}
			lots = append(lots, openLot{
				isLong:          isBuy,
				quantity:        remaining,
				entryPrice:      trade.Price,
				openedAt:        trade.ExecutedAt,
				entryFeePerUnit: entryFeePerUnit,
			})
		}
		openLotsByTicker[trade.Ticker] = lots
	}

	if len(closed) == 0 {
		return TradeAnalytics{}
	}

	analytics := TradeAnalytics{
		ClosedTrades:      len(closed),
		LargestSingleLoss: math.Inf(1),
	}

	holdingPeriods := make([]time.Duration, 0, len(closed))
	var totalHolding time.Duration
	winStreak, lossStreak := 0, 0

	for _, ct := range closed {
		holdingPeriods = append(holdingPeriods, ct.holdingFor)
		totalHolding += ct.holdingFor

		if ct.pnl > analytics.LargestSingleWin {
			analytics.LargestSingleWin = ct.pnl
		}
		if ct.pnl < 0 && ct.pnl < analytics.LargestSingleLoss {
			analytics.LargestSingleLoss = ct.pnl
		}

		switch {
		case ct.pnl > 0:
			winStreak++
			lossStreak = 0
		case ct.pnl < 0:
			lossStreak++
			winStreak = 0
		default:
			winStreak = 0
			lossStreak = 0
		}

		if winStreak > analytics.MaxConsecutiveWins {
			analytics.MaxConsecutiveWins = winStreak
		}
		if lossStreak > analytics.MaxConsecutiveLosses {
			analytics.MaxConsecutiveLosses = lossStreak
		}
	}

	if math.IsInf(analytics.LargestSingleLoss, 1) {
		analytics.LargestSingleLoss = 0
	}

	sort.Slice(holdingPeriods, func(i, j int) bool { return holdingPeriods[i] < holdingPeriods[j] })
	analytics.HoldingPeriods.Min = holdingPeriods[0]
	analytics.HoldingPeriods.Max = holdingPeriods[len(holdingPeriods)-1]
	analytics.HoldingPeriods.Mean = totalHolding / time.Duration(len(holdingPeriods))
	if len(holdingPeriods)%2 == 1 {
		analytics.HoldingPeriods.Median = holdingPeriods[len(holdingPeriods)/2]
	} else {
		mid := len(holdingPeriods) / 2
		analytics.HoldingPeriods.Median = (holdingPeriods[mid-1] + holdingPeriods[mid]) / 2
	}

	if !periodStart.IsZero() && !periodEnd.IsZero() && !periodEnd.Before(periodStart) {
		days := periodEnd.Sub(periodStart).Hours() / 24
		if days > 0 {
			analytics.TradeFrequencyPerDay = float64(analytics.ClosedTrades) / days
		}
	}

	return analytics
}
