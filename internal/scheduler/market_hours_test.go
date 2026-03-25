package scheduler_test

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

func TestIsMarketOpenStockHours(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation(America/New_York): %v", err)
	}

	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "before open", at: time.Date(2024, time.January, 8, 9, 29, 0, 0, loc), want: false},
		{name: "opening bell", at: time.Date(2024, time.January, 8, 9, 30, 0, 0, loc), want: true},
		{name: "opening bell UTC", at: time.Date(2024, time.January, 8, 14, 30, 0, 0, time.UTC), want: true},
		{name: "mid session", at: time.Date(2024, time.January, 8, 13, 0, 0, 0, loc), want: true},
		{name: "minute before close", at: time.Date(2024, time.January, 8, 15, 59, 0, 0, loc), want: true},
		{name: "closing time", at: time.Date(2024, time.January, 8, 16, 0, 0, 0, loc), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduler.IsMarketOpen(tc.at, domain.MarketTypeStock); got != tc.want {
				t.Errorf("IsMarketOpen(%s, %q) = %v, want %v", tc.at.Format(time.RFC3339), domain.MarketTypeStock, got, tc.want)
			}
		})
	}
}

func TestIsMarketOpenAlwaysOpenMarkets(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation(America/New_York): %v", err)
	}

	at := time.Date(2024, time.January, 6, 2, 0, 0, 0, loc)
	tests := []struct {
		name       string
		marketType domain.MarketType
	}{
		{name: "crypto", marketType: domain.MarketTypeCrypto},
		{name: "polymarket", marketType: domain.MarketTypePolymarket},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !scheduler.IsMarketOpen(at, tc.marketType) {
				t.Errorf("IsMarketOpen(%s, %q) = false, want true", at.Format(time.RFC3339), tc.marketType)
			}
		})
	}
}

func TestIsMarketOpenStockWeekendClosed(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation(America/New_York): %v", err)
	}

	at := time.Date(2024, time.January, 6, 10, 0, 0, 0, loc)
	if scheduler.IsMarketOpen(at, domain.MarketTypeStock) {
		t.Errorf("IsMarketOpen(%s, %q) = true, want false", at.Format(time.RFC3339), domain.MarketTypeStock)
	}
}

func TestIsMarketOpenStockHolidayClosed(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation(America/New_York): %v", err)
	}

	tests := []time.Time{
		time.Date(2024, time.December, 25, 10, 0, 0, 0, loc),
		time.Date(2024, time.November, 28, 10, 0, 0, 0, loc),
		time.Date(2021, time.December, 31, 10, 0, 0, 0, loc),
	}

	for _, at := range tests {
		if scheduler.IsMarketOpen(at, domain.MarketTypeStock) {
			t.Errorf("IsMarketOpen(%s, %q) = true, want false", at.Format(time.RFC3339), domain.MarketTypeStock)
		}
	}
}
