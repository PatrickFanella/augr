package polygon

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestMapChainResult_FullSnapshot(t *testing.T) {
	t.Parallel()
	iv := 0.35
	r := optionsChainResult{
		BreakEvenPrice: 155.0,
		Details: &optionsChainDetails{
			Ticker:            "O:AAPL241220C00150000",
			ContractType:      "call",
			ExerciseStyle:     "american",
			ExpirationDate:    "2024-12-20",
			StrikePrice:       150.0,
			SharesPerContract: 100,
		},
		Greeks: &optionsChainGreeks{
			Delta: 0.65, Gamma: 0.03, Theta: -0.05, Vega: 0.15,
		},
		ImpliedVolatility: &iv,
		OpenInterest:      5000,
		LastQuote: &optionsChainQuote{
			Bid: 5.10, Ask: 5.30, Midpoint: 5.20,
		},
		LastTrade: &optionsChainTrade{Price: 5.25, Size: 10},
		Day:       &optionsChainDay{Volume: 1200},
	}

	snap := mapChainResult(r)

	if snap.Contract.Underlying != "AAPL" {
		t.Errorf("underlying = %q, want AAPL", snap.Contract.Underlying)
	}
	if snap.Contract.OptionType != domain.OptionTypeCall {
		t.Errorf("type = %q, want call", snap.Contract.OptionType)
	}
	if snap.Contract.Strike != 150.0 {
		t.Errorf("strike = %v, want 150", snap.Contract.Strike)
	}
	if !snap.Contract.Expiry.Equal(time.Date(2024, 12, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expiry = %v", snap.Contract.Expiry)
	}
	if snap.Contract.Multiplier != 100 {
		t.Errorf("multiplier = %v, want 100", snap.Contract.Multiplier)
	}
	if snap.Greeks.Delta != 0.65 {
		t.Errorf("delta = %v, want 0.65", snap.Greeks.Delta)
	}
	if snap.Greeks.IV != 0.35 {
		t.Errorf("iv = %v, want 0.35", snap.Greeks.IV)
	}
	if snap.Bid != 5.10 {
		t.Errorf("bid = %v, want 5.10", snap.Bid)
	}
	if snap.Ask != 5.30 {
		t.Errorf("ask = %v, want 5.30", snap.Ask)
	}
	if snap.Mid != 5.20 {
		t.Errorf("mid = %v, want 5.20", snap.Mid)
	}
	if snap.Last != 5.25 {
		t.Errorf("last = %v, want 5.25", snap.Last)
	}
	if snap.Volume != 1200 {
		t.Errorf("volume = %v, want 1200", snap.Volume)
	}
	if snap.OpenInterest != 5000 {
		t.Errorf("oi = %v, want 5000", snap.OpenInterest)
	}
}

func TestMapChainResult_MidpointFallback(t *testing.T) {
	t.Parallel()
	r := optionsChainResult{
		Details: &optionsChainDetails{
			Ticker:         "O:SPY251219P00650000",
			ContractType:   "put",
			ExpirationDate: "2025-12-19",
			StrikePrice:    650,
		},
		LastQuote: &optionsChainQuote{Bid: 10.0, Ask: 12.0, Midpoint: 0},
	}
	snap := mapChainResult(r)
	if snap.Mid != 11.0 {
		t.Errorf("mid fallback = %v, want 11.0", snap.Mid)
	}
	if snap.Contract.OptionType != domain.OptionTypePut {
		t.Errorf("type = %q, want put", snap.Contract.OptionType)
	}
}

func TestMapChainResult_NilGreeks(t *testing.T) {
	t.Parallel()
	r := optionsChainResult{
		Details: &optionsChainDetails{
			Ticker:         "O:AAPL241220C00150000",
			ContractType:   "call",
			ExpirationDate: "2024-12-20",
			StrikePrice:    150,
		},
	}
	snap := mapChainResult(r)
	if snap.Greeks.Delta != 0 {
		t.Errorf("delta should be 0 for nil greeks, got %v", snap.Greeks.Delta)
	}
}
