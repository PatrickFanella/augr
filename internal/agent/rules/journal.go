package rules

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// EventType classifies a journal entry.
type EventType string

const (
	EventEntry        EventType = "entry"
	EventHold         EventType = "hold"
	EventSignalReview EventType = "signal_review"
	EventStopHit      EventType = "stop_hit"
	EventTakeProfit   EventType = "take_profit"
	EventExit         EventType = "exit"
)

// JournalEntry records a single decision event for a position.
type JournalEntry struct {
	Type       EventType             `json:"type"`
	Timestamp  time.Time             `json:"timestamp"`
	BarIndex   int                   `json:"bar_index"`
	Signal     domain.PipelineSignal `json:"signal,omitempty"`
	Verdict    string                `json:"verdict,omitempty"`
	Confidence float64               `json:"confidence,omitempty"`
	Reasoning  string                `json:"reasoning,omitempty"`
	Indicators map[string]float64    `json:"indicators,omitempty"`
	Price      float64               `json:"price"`
}

// OpenPosition tracks a live position with its full context.
type OpenPosition struct {
	Ticker            string              `json:"ticker"`
	Side              domain.PositionSide `json:"side"`
	EntryPrice        float64             `json:"entry_price"`
	EntryDate         time.Time           `json:"entry_date"`
	Quantity          float64             `json:"quantity"`
	CostBasis         float64             `json:"cost_basis"`
	HardStopLoss      float64             `json:"hard_stop_loss"`
	TrailingStopPct   float64             `json:"trailing_stop_pct,omitempty"`
	TrailingStopLevel float64             `json:"trailing_stop_level,omitempty"`
	TakeProfit        float64             `json:"take_profit"`
	HoldingStrategy   string              `json:"holding_strategy,omitempty"`
	Journal           []JournalEntry      `json:"journal"`
}

// UnrealizedPnL returns the per-share unrealized P&L at the given price.
func (p *OpenPosition) UnrealizedPnL(currentPrice float64) float64 {
	if p.Side == domain.PositionSideLong {
		return currentPrice - p.EntryPrice
	}
	return p.EntryPrice - currentPrice
}

// UnrealizedPnLPct returns the unrealized P&L as a percentage.
func (p *OpenPosition) UnrealizedPnLPct(currentPrice float64) float64 {
	if p.EntryPrice == 0 {
		return 0
	}
	return p.UnrealizedPnL(currentPrice) / p.EntryPrice * 100
}

// HoldingDays returns the number of calendar days since entry.
func (p *OpenPosition) HoldingDays(now time.Time) int {
	return int(now.Sub(p.EntryDate).Hours() / 24)
}

// AddEntry appends a journal entry.
func (p *OpenPosition) AddEntry(entry JournalEntry) {
	p.Journal = append(p.Journal, entry)
}

// UpdateTrailingStop advances the trailing stop if the new price is higher (for longs).
func (p *OpenPosition) UpdateTrailingStop(currentPrice float64) {
	if p.TrailingStopPct <= 0 {
		return
	}
	newLevel := currentPrice * (1 - p.TrailingStopPct/100)
	if p.Side == domain.PositionSideShort {
		newLevel = currentPrice * (1 + p.TrailingStopPct/100)
		if p.TrailingStopLevel == 0 || newLevel < p.TrailingStopLevel {
			p.TrailingStopLevel = newLevel
		}
		return
	}
	if newLevel > p.TrailingStopLevel {
		p.TrailingStopLevel = newLevel
	}
}

// IsStopHit checks if the bar's price action hit the hard stop or trailing stop.
func (p *OpenPosition) IsStopHit(bar domain.OHLCV) (bool, string) {
	if p.Side == domain.PositionSideLong {
		if p.HardStopLoss > 0 && bar.Low <= p.HardStopLoss {
			return true, fmt.Sprintf("hard stop hit at $%.2f (bar low $%.2f)", p.HardStopLoss, bar.Low)
		}
		if p.TrailingStopLevel > 0 && bar.Low <= p.TrailingStopLevel {
			return true, fmt.Sprintf("trailing stop hit at $%.2f (bar low $%.2f)", p.TrailingStopLevel, bar.Low)
		}
	} else {
		if p.HardStopLoss > 0 && bar.High >= p.HardStopLoss {
			return true, fmt.Sprintf("hard stop hit at $%.2f (bar high $%.2f)", p.HardStopLoss, bar.High)
		}
		if p.TrailingStopLevel > 0 && bar.High >= p.TrailingStopLevel {
			return true, fmt.Sprintf("trailing stop hit at $%.2f (bar high $%.2f)", p.TrailingStopLevel, bar.High)
		}
	}
	return false, ""
}

// IsTakeProfitHit checks if the bar hit the take-profit level.
func (p *OpenPosition) IsTakeProfitHit(bar domain.OHLCV) bool {
	if p.TakeProfit <= 0 {
		return false
	}
	if p.Side == domain.PositionSideLong {
		return bar.High >= p.TakeProfit
	}
	return bar.Low <= p.TakeProfit
}

// ClosedPosition is the final record after a position is closed.
type ClosedPosition struct {
	OpenPosition `json:"position"`
	ExitPrice    float64   `json:"exit_price"`
	ExitDate     time.Time `json:"exit_date"`
	ExitReason   string    `json:"exit_reason"`
	RealizedPnL  float64   `json:"realized_pnl"`
	HoldingDaysN int       `json:"holding_days"`
}

// TradeJournal tracks all open and closed positions with their decision history.
type TradeJournal struct {
	Open   map[string]*OpenPosition `json:"open"`
	Closed []ClosedPosition         `json:"closed"`
}

// NewTradeJournal creates an empty journal.
func NewTradeJournal() *TradeJournal {
	return &TradeJournal{
		Open:   make(map[string]*OpenPosition),
		Closed: make([]ClosedPosition, 0),
	}
}

// IsHolding returns true if there's an open position for the ticker.
func (j *TradeJournal) IsHolding(ticker string) bool {
	_, ok := j.Open[ticker]
	return ok
}

// GetOpen returns the open position for the ticker, or nil.
func (j *TradeJournal) GetOpen(ticker string) *OpenPosition {
	return j.Open[ticker]
}

// OpenNewPosition records a new position entry.
func (j *TradeJournal) OpenNewPosition(pos OpenPosition) {
	pos.CostBasis = pos.EntryPrice * pos.Quantity
	if pos.Journal == nil {
		pos.Journal = make([]JournalEntry, 0)
	}
	j.Open[pos.Ticker] = &pos
}

// ClosePosition moves a position from open to closed.
func (j *TradeJournal) ClosePosition(ticker string, exitPrice float64, exitDate time.Time, exitReason string) *ClosedPosition {
	pos, ok := j.Open[ticker]
	if !ok {
		return nil
	}

	pnlPerShare := pos.UnrealizedPnL(exitPrice)
	closed := ClosedPosition{
		OpenPosition: *pos,
		ExitPrice:    exitPrice,
		ExitDate:     exitDate,
		ExitReason:   exitReason,
		RealizedPnL:  pnlPerShare * pos.Quantity,
		HoldingDaysN: pos.HoldingDays(exitDate),
	}
	j.Closed = append(j.Closed, closed)
	delete(j.Open, ticker)
	return &closed
}

// FormatDecisionHistory returns a human-readable string of all journal entries
// for an open position, suitable for LLM context.
func FormatDecisionHistory(pos *OpenPosition, currentPrice float64, currentDate time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== CURRENT POSITION ===\n")
	fmt.Fprintf(&b, "Holding: %s %s since %s (%d trading days)\n",
		strings.ToUpper(string(pos.Side)), pos.Ticker,
		pos.EntryDate.Format("2006-01-02"), pos.HoldingDays(currentDate))
	pnl := pos.UnrealizedPnL(currentPrice)
	pnlPct := pos.UnrealizedPnLPct(currentPrice)
	fmt.Fprintf(&b, "Entry Price: $%.2f | Current: $%.2f | P&L: %+.2f/share (%+.1f%%)\n",
		pos.EntryPrice, currentPrice, pnl, pnlPct)
	fmt.Fprintf(&b, "Cost Basis: $%.2f (%.0f shares)\n", pos.CostBasis, pos.Quantity)
	fmt.Fprintf(&b, "Hard Stop: $%.2f", pos.HardStopLoss)
	if pos.TrailingStopLevel > 0 {
		fmt.Fprintf(&b, " | Trailing Stop: $%.2f", pos.TrailingStopLevel)
	}
	fmt.Fprintf(&b, " | Take Profit: $%.2f\n", pos.TakeProfit)

	if pos.HoldingStrategy != "" {
		fmt.Fprintf(&b, "\nHolding Strategy (from entry review):\n\"%s\"\n", pos.HoldingStrategy)
	}

	if len(pos.Journal) > 0 {
		fmt.Fprintf(&b, "\n=== DECISION HISTORY ===\n")
		for _, entry := range pos.Journal {
			date := entry.Timestamp.Format("2006-01-02")
			switch entry.Type {
			case EventEntry:
				fmt.Fprintf(&b, "[%s] ENTRY: %s at $%.2f", date, strings.ToUpper(entry.Signal.String()), entry.Price)
				if entry.Verdict != "" {
					fmt.Fprintf(&b, ", LLM %s (%.2f)", entry.Verdict, entry.Confidence)
				}
				if entry.Reasoning != "" {
					fmt.Fprintf(&b, " — %q", entry.Reasoning)
				}
				fmt.Fprintf(&b, "\n")
			case EventHold:
				fmt.Fprintf(&b, "[%s] HOLD at $%.2f", date, entry.Price)
				if entry.Reasoning != "" {
					fmt.Fprintf(&b, " — %s", entry.Reasoning)
				}
				fmt.Fprintf(&b, "\n")
			case EventSignalReview:
				fmt.Fprintf(&b, "[%s] SIGNAL_REVIEW: %s trigger at $%.2f, LLM %s (%.2f) — %q\n",
					date, strings.ToUpper(entry.Signal.String()), entry.Price, entry.Verdict, entry.Confidence, entry.Reasoning)
			case EventStopHit:
				fmt.Fprintf(&b, "[%s] STOP_HIT at $%.2f — %s\n", date, entry.Price, entry.Reasoning)
			case EventTakeProfit:
				fmt.Fprintf(&b, "[%s] TAKE_PROFIT at $%.2f\n", date, entry.Price)
			case EventExit:
				fmt.Fprintf(&b, "[%s] EXIT at $%.2f — %s\n", date, entry.Price, entry.Reasoning)
			}
		}
	}

	// Risk metrics
	if pos.EntryPrice > 0 {
		riskDistance := math.Abs(pos.EntryPrice - pos.HardStopLoss)
		rewardDistance := math.Abs(pos.TakeProfit - pos.EntryPrice)
		rr := 0.0
		if riskDistance > 0 {
			rr = rewardDistance / riskDistance
		}
		fmt.Fprintf(&b, "\nRisk/Reward from entry: %.2f | Max risk: $%.2f/share | Target reward: $%.2f/share\n",
			rr, riskDistance, rewardDistance)
	}

	return b.String()
}
