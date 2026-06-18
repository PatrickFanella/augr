package kalshidiscovery

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Proposal is the discovery contract for a Kalshi market.
type Proposal struct {
	Template         string   `json:"template"`
	Skip             bool     `json:"skip,omitempty"`
	SkipReason       string   `json:"skip_reason,omitempty"`
	Name             string   `json:"name"`
	Summary          string   `json:"summary"`
	Direction        string   `json:"direction"`
	Conviction       float64  `json:"conviction"`
	TimeHorizon      string   `json:"time_horizon"`
	EntryPriceMax    float64  `json:"entry_price_max,omitempty"`
	WatchTerms       []string `json:"watch_terms"`
	InvalidateIf     []string `json:"invalidate_if"`
	SourceReferences []string `json:"source_references,omitempty"`
	MaxSpreadPct     float64  `json:"max_spread_pct,omitempty"`
	MinLiquidity     float64  `json:"min_liquidity,omitempty"`
	StopPolicy       string   `json:"stop_policy,omitempty"`
	TargetPolicy     string   `json:"target_policy,omitempty"`
}

// ValidateProposal checks the proposal contract and rejects stock/OHLCV-style language.
func ValidateProposal(p *Proposal) error {
	if p == nil {
		return errors.New("proposal is nil")
	}
	if p.Skip {
		if strings.TrimSpace(p.SkipReason) == "" {
			return errors.New("skip=true requires skip_reason")
		}
		return nil
	}

	if strings.TrimSpace(p.Template) == "" {
		return errors.New("template required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name required")
	}
	if strings.TrimSpace(p.Summary) == "" {
		return errors.New("summary required")
	}

	side := strings.ToUpper(strings.TrimSpace(p.Direction))
	if side != "YES" && side != "NO" {
		return fmt.Errorf("direction must be YES or NO, got %q", p.Direction)
	}
	p.Direction = side

	if math.IsNaN(p.Conviction) || math.IsInf(p.Conviction, 0) || p.Conviction <= 0 || p.Conviction > 1 {
		return fmt.Errorf("conviction out of range: %.3f", p.Conviction)
	}

	switch horizon := strings.ToLower(strings.TrimSpace(p.TimeHorizon)); horizon {
	case "hours", "days", "weeks":
		p.TimeHorizon = horizon
	default:
		return fmt.Errorf("invalid time_horizon %q", p.TimeHorizon)
	}

	if math.IsNaN(p.EntryPriceMax) || math.IsInf(p.EntryPriceMax, 0) || p.EntryPriceMax <= 0 || p.EntryPriceMax > 1 {
		return fmt.Errorf("entry_price_max must be > 0 and <= 1: %.3f", p.EntryPriceMax)
	}

	p.WatchTerms = compactStrings(p.WatchTerms)
	if len(p.WatchTerms) == 0 {
		return errors.New("watch_terms must not be empty")
	}
	p.InvalidateIf = compactStrings(p.InvalidateIf)
	p.SourceReferences = compactStrings(p.SourceReferences)
	if len(p.SourceReferences) == 0 {
		return errors.New("source_references must not be empty")
	}

	if math.IsNaN(p.MaxSpreadPct) || math.IsInf(p.MaxSpreadPct, 0) || p.MaxSpreadPct <= 0 || p.MaxSpreadPct > 100 {
		return fmt.Errorf("max_spread_pct out of range: %.3f", p.MaxSpreadPct)
	}
	if math.IsNaN(p.MinLiquidity) || math.IsInf(p.MinLiquidity, 0) || p.MinLiquidity <= 0 {
		return errors.New("min_liquidity must be > 0")
	}
	if strings.TrimSpace(p.StopPolicy) == "" {
		return errors.New("stop_policy required")
	}
	if strings.TrimSpace(p.TargetPolicy) == "" {
		return errors.New("target_policy required")
	}

	if term := prohibitedProposalLanguage(p); term != "" {
		return fmt.Errorf("proposal text contains prohibited stock/OHLCV language %q", term)
	}

	return nil
}

// ValidateProposalForMarket binds a proposal to the concrete Kalshi market it
// was generated for. Non-skip proposals must cite the market ticker explicitly
// so stale or cross-market proposals cannot be deployed accidentally.
func ValidateProposalForMarket(p *Proposal, market MarketCandidate) error {
	if err := ValidateProposal(p); err != nil {
		return err
	}
	if p == nil || p.Skip {
		return nil
	}
	ticker := strings.TrimSpace(market.Ticker)
	if ticker == "" {
		return errors.New("market ticker required")
	}
	want := strings.ToUpper("kalshi_market:" + ticker)
	for _, ref := range p.SourceReferences {
		if strings.ToUpper(strings.TrimSpace(ref)) == want {
			return nil
		}
	}
	return fmt.Errorf("source_references must include %s", "kalshi_market:"+ticker)
}

var prohibitedProposalLanguagePatterns = []struct {
	term string
	re   *regexp.Regexp
}{
	{term: "rsi", re: regexp.MustCompile(`(?i)\brsi\b`)},
	{term: "macd", re: regexp.MustCompile(`(?i)\bmacd\b`)},
	{term: "sma", re: regexp.MustCompile(`(?i)\bsma\b`)},
	{term: "ema", re: regexp.MustCompile(`(?i)\bema\b`)},
	{term: "bollinger", re: regexp.MustCompile(`(?i)\bbollinger\b`)},
	{term: "atr", re: regexp.MustCompile(`(?i)\batr\b`)},
	{term: "ohlcv", re: regexp.MustCompile(`(?i)\bohlcv\b`)},
	{term: "candles", re: regexp.MustCompile(`(?i)\bcandles\b`)},
	{term: "vwap", re: regexp.MustCompile(`(?i)\bvwap\b`)},
	{term: "z-score", re: regexp.MustCompile(`(?i)\bz-score\b`)},
	{term: "mean reversion", re: regexp.MustCompile(`(?i)\bmean reversion\b`)},
	{term: "stock", re: regexp.MustCompile(`(?i)\bstocks?\b`)},
	{term: "equity", re: regexp.MustCompile(`(?i)\bequities?\b`)},
	{term: "share", re: regexp.MustCompile(`(?i)\bshares?\b`)},
}

func prohibitedProposalLanguage(p *Proposal) string {
	if p == nil {
		return ""
	}
	text := strings.ToLower(strings.Join([]string{
		p.Template,
		p.Name,
		p.Summary,
		strings.Join(p.WatchTerms, " "),
		strings.Join(p.InvalidateIf, " "),
		strings.Join(p.SourceReferences, " "),
		p.StopPolicy,
		p.TargetPolicy,
	}, " "))
	for _, item := range prohibitedProposalLanguagePatterns {
		if item.re.MatchString(text) {
			return item.term
		}
	}
	return ""
}

func compactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
