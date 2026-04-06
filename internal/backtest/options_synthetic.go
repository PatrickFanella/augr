package backtest

import (
	"fmt"
	"math"
	"time"

	gpo "github.com/jasonmerecki/gopriceoptions"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// SyntheticChainConfig controls how a synthetic options chain is built.
type SyntheticChainConfig struct {
	StrikeInterval float64 // $1 for stocks < $50, $5 for > $50
	NumStrikes     int     // strikes above + below ATM (default 10)
	SpreadBps      float64 // bid-ask spread in basis points (default 50)
	RiskFreeRate   float64 // default 0.05
	Dividend       float64 // default 0.0
}

// DefaultSyntheticChainConfig returns sensible defaults.
func DefaultSyntheticChainConfig() SyntheticChainConfig {
	return SyntheticChainConfig{
		NumStrikes:   10,
		SpreadBps:    50,
		RiskFreeRate: 0.05,
		Dividend:     0.0,
	}
}

// SynthesizeChain generates a synthetic options chain using Black-Scholes.
// This is used for backtesting when real historical chain data is unavailable.
//
// Parameters:
//   - underlying: current stock price
//   - vol: annualised implied volatility (e.g. 0.30 for 30%)
//   - dte: days to expiration
//   - now: current timestamp (for expiry date)
//   - cfg: chain configuration
func SynthesizeChain(underlying, vol float64, dte int, now time.Time, cfg SyntheticChainConfig) []domain.OptionSnapshot {
	if underlying <= 0 || vol <= 0 || dte <= 0 {
		return nil
	}

	interval := cfg.StrikeInterval
	if interval <= 0 {
		if underlying < 50 {
			interval = 1
		} else {
			interval = 5
		}
	}

	numStrikes := cfg.NumStrikes
	if numStrikes <= 0 {
		numStrikes = 10
	}

	t := float64(dte) / 365.25
	r := cfg.RiskFreeRate
	d := cfg.Dividend
	expiry := now.AddDate(0, 0, dte)
	spreadFrac := cfg.SpreadBps / 10000

	// Round ATM strike to nearest interval.
	atmStrike := math.Round(underlying/interval) * interval

	var chain []domain.OptionSnapshot

	for i := -numStrikes; i <= numStrikes; i++ {
		strike := atmStrike + float64(i)*interval
		if strike <= 0 {
			continue
		}

		for _, isCall := range []bool{true, false} {
			price := gpo.PriceBlackScholes(isCall, underlying, strike, t, vol, r, d)
			delta := gpo.BSDelta(isCall, underlying, strike, t, vol, r, d)
			gamma := gpo.BSGamma(underlying, strike, t, vol, r, d)
			theta := gpo.BSTheta(isCall, underlying, strike, t, vol, r, d)
			vega := gpo.BSVega(underlying, strike, t, vol, r, d)
			rho := gpo.BSRho(isCall, underlying, strike, t, vol, r, d)

			halfSpread := price * spreadFrac / 2
			bid := math.Max(0, price-halfSpread)
			ask := price + halfSpread
			mid := (bid + ask) / 2

			optType := domain.OptionTypeCall
			if !isCall {
				optType = domain.OptionTypePut
			}

			occ := fmt.Sprintf("%s%s%s%08d",
				"SYN",
				expiry.Format("060102"),
				string(optType[0:1]),
				int(strike*1000),
			)

			chain = append(chain, domain.OptionSnapshot{
				Contract: domain.OptionContract{
					OCCSymbol:  occ,
					Underlying: "SYN",
					OptionType: optType,
					Strike:     strike,
					Expiry:     expiry,
					Multiplier: 100,
					Style:      "american",
				},
				Greeks: domain.OptionGreeks{
					Delta: delta,
					Gamma: gamma,
					Theta: theta,
					Vega:  vega,
					Rho:   rho,
					IV:    vol,
				},
				Bid:          bid,
				Ask:          ask,
				Mid:          mid,
				Last:         mid,
				Volume:       1000, // synthetic — assume liquid
				OpenInterest: 5000,
			})
		}
	}

	return chain
}
