package data

import (
	"context"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// EventsProvider defines the abstraction for retrieving calendar and event data.
type EventsProvider interface {
	// GetEarningsCalendar returns earnings events in the given date range.
	GetEarningsCalendar(ctx context.Context, from, to time.Time) ([]domain.EarningsEvent, error)

	// GetNextEarnings returns the next upcoming earnings event for a ticker.
	GetNextEarnings(ctx context.Context, ticker string) (*domain.EarningsEvent, error)

	// GetFilings returns SEC filings for a ticker filtered by form type and date range.
	GetFilings(ctx context.Context, ticker, formType string, from, to time.Time) ([]domain.SECFiling, error)

	// GetEconomicCalendar returns upcoming economic events.
	GetEconomicCalendar(ctx context.Context) ([]domain.EconomicEvent, error)

	// GetIPOCalendar returns IPO events in the given date range.
	GetIPOCalendar(ctx context.Context, from, to time.Time) ([]domain.IPOEvent, error)
}
