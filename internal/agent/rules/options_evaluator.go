package rules

import (
	"math"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// OptionsSnapshot extends Snapshot with options-specific fields.
type OptionsSnapshot struct {
	Snapshot               // embed underlying indicator snapshot
	IVRank         float64 // IV rank (0-100)
	IVPercentile   float64 // IV percentile (0-100)
	ATMImpliedVol  float64 // at-the-money IV
	PutCallRatio   float64 // volume put/call ratio
	DTE            int     // days to expiry of current position (0 if flat)
	PositionPnLPct float64 // current position P&L % (0 if flat)
}

// NewOptionsSnapshot builds an OptionsSnapshot from underlying indicators, an
// option chain, an optional open position, and the current time.
//
// Chain-derived fields:
//   - atm_iv: IV of the contract whose strike is closest to the underlying close
//   - put_call_ratio: total put volume / total call volume
//
// Position-derived fields (when pos is non-nil):
//   - dte: calendar days from now to position expiry
//   - pnl_pct: unrealised P&L as a fraction of cost basis
//
// iv_rank and iv_percentile must be injected via the snapshot Values map
// before calling this constructor (they require historical IV data the chain
// does not carry).
func NewOptionsSnapshot(snap Snapshot, chain []domain.OptionSnapshot, pos *OpenPosition, now time.Time) OptionsSnapshot {
	os := OptionsSnapshot{Snapshot: snap}

	// Derive ATM IV: find the contract with strike closest to close.
	closePrice, hasClose := snap.Values["close"]
	if hasClose && len(chain) > 0 {
		bestDist := math.Inf(1)
		for _, c := range chain {
			dist := math.Abs(c.Contract.Strike - closePrice)
			if dist < bestDist {
				bestDist = dist
				os.ATMImpliedVol = c.Greeks.IV
			}
		}
	}

	// Derive put/call volume ratio.
	var putVol, callVol float64
	for _, c := range chain {
		switch c.Contract.OptionType {
		case domain.OptionTypePut:
			putVol += c.Volume
		case domain.OptionTypeCall:
			callVol += c.Volume
		}
	}
	if callVol > 0 {
		os.PutCallRatio = putVol / callVol
	}

	// Position-derived fields.
	if pos != nil {
		os.PositionPnLPct = snap.Values["pnl_pct"] // caller injects this
		os.DTE = int(snap.Values["dte"])
	}

	// Propagate IV rank/percentile from snap values (caller injects them).
	os.IVRank = snap.Values["iv_rank"]
	os.IVPercentile = snap.Values["iv_percentile"]

	// Write derived values back into the embedded Values map so the generic
	// EvaluateGroup / EvaluateCondition can reference them.
	os.Values["atm_iv"] = os.ATMImpliedVol
	os.Values["put_call_ratio"] = os.PutCallRatio
	os.Values["iv_rank"] = os.IVRank
	os.Values["iv_percentile"] = os.IVPercentile
	if pos != nil {
		os.Values["dte"] = float64(os.DTE)
		os.Values["pnl_pct"] = os.PositionPnLPct
	}

	return os
}
