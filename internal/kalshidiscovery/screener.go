package kalshidiscovery

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ScreenerConfig controls which Kalshi markets become discovery candidates.
type ScreenerConfig struct {
	MaxCandidates   int
	MinVolume       float64
	MinOpenInterest float64
	MaxSpreadPct    float64
	MinDaysToClose  float64
	Categories      []string
}

// ScreenRejection explains why a candidate was filtered out.
type ScreenRejection struct {
	Ticker  string   `json:"ticker"`
	Reasons []string `json:"reasons"`
}

// DefaultScreenerConfig returns conservative but useful screening defaults.
func DefaultScreenerConfig() ScreenerConfig {
	return ScreenerConfig{
		MaxCandidates:   15,
		MinVolume:       1_000,
		MinOpenInterest: 500,
		MaxSpreadPct:    12,
		MinDaysToClose:  3,
		Categories:      nil,
	}
}

// ScreenMarkets applies the configured filters and returns accepted candidates.
func ScreenMarkets(markets []MarketCandidate, cfg ScreenerConfig, now time.Time) []MarketCandidate {
	accepted, _ := ScreenMarketsDetailed(markets, cfg, now)
	return accepted
}

// ScreenMarketsDetailed applies the configured filters and returns accepted
// candidates plus rejection reasons for discarded candidates.
func ScreenMarketsDetailed(markets []MarketCandidate, cfg ScreenerConfig, now time.Time) ([]MarketCandidate, []ScreenRejection) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	accepted := make([]MarketCandidate, 0, len(markets))
	rejected := make([]ScreenRejection, 0)
	for _, market := range markets {
		reasons := screenMarket(market, cfg, now)
		if len(reasons) == 0 {
			accepted = append(accepted, market)
			continue
		}
		rejected = append(rejected, ScreenRejection{Ticker: market.Ticker, Reasons: reasons})
	}

	sort.SliceStable(accepted, func(i, j int) bool {
		if accepted[i].Volume != accepted[j].Volume {
			return accepted[i].Volume > accepted[j].Volume
		}
		if accepted[i].OpenInterest != accepted[j].OpenInterest {
			return accepted[i].OpenInterest > accepted[j].OpenInterest
		}
		if accepted[i].CloseTime != nil && accepted[j].CloseTime != nil && !accepted[i].CloseTime.Equal(*accepted[j].CloseTime) {
			return accepted[i].CloseTime.Before(*accepted[j].CloseTime)
		}
		return accepted[i].Ticker < accepted[j].Ticker
	})

	if cfg.MaxCandidates > 0 && len(accepted) > cfg.MaxCandidates {
		accepted = accepted[:cfg.MaxCandidates]
	}

	return accepted, rejected
}

func screenMarket(market MarketCandidate, cfg ScreenerConfig, now time.Time) []string {
	reasons := make([]string, 0, 8)
	if isClosedOrSettledStatus(market.Status) {
		reasons = append(reasons, fmt.Sprintf("status %q is closed or settled", strings.TrimSpace(market.Status)))
	}
	if market.CloseTime == nil {
		reasons = append(reasons, "missing close time")
	} else {
		if !market.CloseTime.After(now) {
			reasons = append(reasons, "market is closed")
		} else if cfg.MinDaysToClose > 0 {
			daysToClose := market.CloseTime.Sub(now).Hours() / 24
			if daysToClose < cfg.MinDaysToClose {
				reasons = append(reasons, fmt.Sprintf("closes in %.2f days, below minimum %.2f", daysToClose, cfg.MinDaysToClose))
			}
		}
	}

	if len(cfg.Categories) > 0 && !categoryAllowed(market.Category, cfg.Categories) {
		reasons = append(reasons, fmt.Sprintf("category %q not in configured set", strings.TrimSpace(market.Category)))
	}
	if cfg.MinVolume > 0 && market.Volume < cfg.MinVolume {
		reasons = append(reasons, fmt.Sprintf("volume %.2f below minimum %.2f", market.Volume, cfg.MinVolume))
	}
	if cfg.MinOpenInterest > 0 && market.OpenInterest < cfg.MinOpenInterest {
		reasons = append(reasons, fmt.Sprintf("open interest %.2f below minimum %.2f", market.OpenInterest, cfg.MinOpenInterest))
	}

	yesSpread, err := executableSpreadPct(market.YesBid, market.YesAsk)
	if err != nil {
		reasons = append(reasons, "YES book "+err.Error())
	} else if cfg.MaxSpreadPct > 0 && yesSpread > cfg.MaxSpreadPct {
		reasons = append(reasons, fmt.Sprintf("YES spread %.2f%% above maximum %.2f%%", yesSpread, cfg.MaxSpreadPct))
	}

	noSpread, err := executableSpreadPct(market.NoBid, market.NoAsk)
	if err != nil {
		reasons = append(reasons, "NO book "+err.Error())
	} else if cfg.MaxSpreadPct > 0 && noSpread > cfg.MaxSpreadPct {
		reasons = append(reasons, fmt.Sprintf("NO spread %.2f%% above maximum %.2f%%", noSpread, cfg.MaxSpreadPct))
	}

	return reasons
}

func categoryAllowed(category string, allowed []string) bool {
	category = strings.ToLower(strings.TrimSpace(category))
	if category == "" {
		return false
	}
	for _, candidate := range allowed {
		if category == strings.ToLower(strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func executableSpreadPct(bid, ask float64) (float64, error) {
	if ask <= 0 {
		return 0, fmt.Errorf("missing executable ask (%.4f)", ask)
	}
	if bid < 0 {
		return 0, fmt.Errorf("negative bid (%.4f)", bid)
	}
	if bid > ask {
		return 0, fmt.Errorf("bid %.4f exceeds ask %.4f", bid, ask)
	}
	return ((ask - bid) / ask) * 100, nil
}

func isClosedOrSettledStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" {
		return false
	}
	for _, term := range []string{
		"closed",
		"settled",
		"expired",
		"resolved",
		"canceled",
		"cancelled",
		"void",
		"terminated",
		"rejected",
	} {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}
