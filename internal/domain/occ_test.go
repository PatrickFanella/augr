package domain

import (
	"testing"
	"time"
)

func TestParseOCC(t *testing.T) {
	t.Parallel()
	cases := []struct {
		symbol     string
		underlying string
		optType    OptionType
		strike     float64
		expiry     time.Time
	}{
		{"AAPL241220C00150000", "AAPL", OptionTypeCall, 150.0, time.Date(2024, 12, 20, 0, 0, 0, 0, time.UTC)},
		{"SPY251219P00650000", "SPY", OptionTypePut, 650.0, time.Date(2025, 12, 19, 0, 0, 0, 0, time.UTC)},
		{"EVRI240920P00012500", "EVRI", OptionTypePut, 12.5, time.Date(2024, 9, 20, 0, 0, 0, 0, time.UTC)},
		{"O:AAPL241220C00150000", "AAPL", OptionTypeCall, 150.0, time.Date(2024, 12, 20, 0, 0, 0, 0, time.UTC)},
		{"X260101C00005000", "X", OptionTypeCall, 5.0, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.symbol, func(t *testing.T) {
			t.Parallel()
			c, err := ParseOCC(tc.symbol)
			if err != nil {
				t.Fatalf("ParseOCC(%q) error = %v", tc.symbol, err)
			}
			if c.Underlying != tc.underlying {
				t.Errorf("underlying = %q, want %q", c.Underlying, tc.underlying)
			}
			if c.OptionType != tc.optType {
				t.Errorf("type = %q, want %q", c.OptionType, tc.optType)
			}
			if c.Strike != tc.strike {
				t.Errorf("strike = %v, want %v", c.Strike, tc.strike)
			}
			if !c.Expiry.Equal(tc.expiry) {
				t.Errorf("expiry = %v, want %v", c.Expiry, tc.expiry)
			}
			if c.Multiplier != 100 {
				t.Errorf("multiplier = %v, want 100", c.Multiplier)
			}
		})
	}
}

func TestParseOCC_Errors(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"short",
		"241220C00150000",            // no root
		"TOOLONGROOT241220C00150000", // root > 6
		"AAPL241220X00150000",        // bad C/P
	}
	for _, sym := range cases {
		t.Run(sym, func(t *testing.T) {
			t.Parallel()
			_, err := ParseOCC(sym)
			if err == nil {
				t.Errorf("ParseOCC(%q) should have returned error", sym)
			}
		})
	}
}

func TestFormatOCC(t *testing.T) {
	t.Parallel()
	got := FormatOCC("AAPL", OptionTypeCall, 150.0, time.Date(2024, 12, 20, 0, 0, 0, 0, time.UTC))
	want := "AAPL241220C00150000"
	if got != want {
		t.Errorf("FormatOCC = %q, want %q", got, want)
	}

	got2 := FormatOCC("SPY", OptionTypePut, 650.0, time.Date(2025, 12, 19, 0, 0, 0, 0, time.UTC))
	want2 := "SPY251219P00650000"
	if got2 != want2 {
		t.Errorf("FormatOCC = %q, want %q", got2, want2)
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	t.Parallel()
	symbols := []string{
		"AAPL241220C00150000",
		"SPY251219P00650000",
		"EVRI240920P00012500",
		"X260101C00005000",
	}
	for _, sym := range symbols {
		t.Run(sym, func(t *testing.T) {
			t.Parallel()
			c, err := ParseOCC(sym)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := FormatOCC(c.Underlying, c.OptionType, c.Strike, c.Expiry)
			if got != sym {
				t.Errorf("round-trip: got %q, want %q", got, sym)
			}
		})
	}
}

func TestMassiveSymbol(t *testing.T) {
	t.Parallel()
	if got := MassiveSymbol("AAPL241220C00150000"); got != "O:AAPL241220C00150000" {
		t.Errorf("got %q", got)
	}
	if got := MassiveSymbol("O:AAPL241220C00150000"); got != "O:AAPL241220C00150000" {
		t.Errorf("double prefix: got %q", got)
	}
}

func TestAlpacaSymbol(t *testing.T) {
	t.Parallel()
	if got := AlpacaSymbol("O:AAPL241220C00150000"); got != "AAPL241220C00150000" {
		t.Errorf("got %q", got)
	}
	if got := AlpacaSymbol("AAPL241220C00150000"); got != "AAPL241220C00150000" {
		t.Errorf("no prefix: got %q", got)
	}
}
