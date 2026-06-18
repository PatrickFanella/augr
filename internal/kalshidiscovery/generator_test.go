package kalshidiscovery

import "testing"

func TestValidateProposalAcceptsNonSkipProposal(t *testing.T) {
	t.Parallel()

	p := &Proposal{
		Template:         "microstructure",
		Name:             "Weather Spread Repricing",
		Summary:          "Buy YES when the book lags the catalyst and spreads remain tight.",
		Direction:        "yes",
		Conviction:       0.72,
		TimeHorizon:      "Days",
		EntryPriceMax:    0.62,
		WatchTerms:       []string{"NOAA", "rain"},
		InvalidateIf:     []string{"official forecast flips"},
		SourceReferences: []string{"kalshi_market:KAL-RAIN"},
		MaxSpreadPct:     8,
		MinLiquidity:     1000,
		StopPolicy:       "hold until invalidation or close window",
		TargetPolicy:     "take profit on repricing",
	}

	if err := ValidateProposal(p); err != nil {
		t.Fatalf("ValidateProposal() error = %v", err)
	}
	if p.Direction != "YES" || p.TimeHorizon != "days" {
		t.Fatalf("proposal normalization failed: %#v", p)
	}
}

func TestValidateProposalRejectsMissingFieldsAndProhibitedLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    *Proposal
	}{
		{name: "skip missing reason", p: &Proposal{Skip: true}},
		{name: "bad direction", p: &Proposal{Template: "microstructure", Name: "x", Summary: "y", Direction: "maybe", Conviction: 0.5, TimeHorizon: "days", EntryPriceMax: 0.5, WatchTerms: []string{"x"}, SourceReferences: []string{"ref"}, MaxSpreadPct: 1, MinLiquidity: 1, StopPolicy: "stop", TargetPolicy: "target"}},
		{name: "missing watch terms", p: &Proposal{Template: "microstructure", Name: "x", Summary: "y", Direction: "yes", Conviction: 0.5, TimeHorizon: "days", EntryPriceMax: 0.5, WatchTerms: nil, SourceReferences: []string{"ref"}, MaxSpreadPct: 1, MinLiquidity: 1, StopPolicy: "stop", TargetPolicy: "target"}},
		{name: "missing source refs", p: &Proposal{Template: "microstructure", Name: "x", Summary: "y", Direction: "yes", Conviction: 0.5, TimeHorizon: "days", EntryPriceMax: 0.5, WatchTerms: []string{"x"}, SourceReferences: []string{""}, MaxSpreadPct: 1, MinLiquidity: 1, StopPolicy: "stop", TargetPolicy: "target"}},
		{name: "bad entry price", p: &Proposal{Template: "microstructure", Name: "x", Summary: "y", Direction: "yes", Conviction: 0.5, TimeHorizon: "days", EntryPriceMax: 0, WatchTerms: []string{"x"}, SourceReferences: []string{"ref"}, MaxSpreadPct: 1, MinLiquidity: 1, StopPolicy: "stop", TargetPolicy: "target"}},
		{name: "stock language", p: &Proposal{Template: "microstructure", Name: "Stock Reversion", Summary: "Use stock candles and RSI", Direction: "yes", Conviction: 0.5, TimeHorizon: "days", EntryPriceMax: 0.5, WatchTerms: []string{"stock"}, SourceReferences: []string{"ref"}, MaxSpreadPct: 1, MinLiquidity: 1, StopPolicy: "stop", TargetPolicy: "target"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateProposal(tt.p); err == nil {
				t.Fatal("ValidateProposal() error = nil, want non-nil")
			}
		})
	}
}

func TestValidateProposalForMarketAllowsSkipWithReason(t *testing.T) {
	t.Parallel()

	p := &Proposal{Skip: true, SkipReason: "no edge"}
	if err := ValidateProposalForMarket(p, MarketCandidate{Ticker: "KAL-1"}); err != nil {
		t.Fatalf("ValidateProposalForMarket() error = %v", err)
	}
}

func TestValidateProposalForMarketRequiresMatchingKalshiSourceReference(t *testing.T) {
	t.Parallel()

	valid := validKalshiProposal()
	if err := ValidateProposalForMarket(valid, MarketCandidate{Ticker: "KAL-RAIN"}); err != nil {
		t.Fatalf("ValidateProposalForMarket() matching ref error = %v", err)
	}

	wrong := validKalshiProposal()
	wrong.SourceReferences = []string{"kalshi_market:KAL-OTHER"}
	if err := ValidateProposalForMarket(wrong, MarketCandidate{Ticker: "KAL-RAIN"}); err == nil {
		t.Fatal("ValidateProposalForMarket() wrong ref error = nil, want non-nil")
	}
}

func validKalshiProposal() *Proposal {
	return &Proposal{
		Template:         "microstructure",
		Name:             "Weather Spread Repricing",
		Summary:          "Buy YES when the book lags the catalyst and spreads remain tight.",
		Direction:        "YES",
		Conviction:       0.72,
		TimeHorizon:      "days",
		EntryPriceMax:    0.62,
		WatchTerms:       []string{"NOAA", "rain"},
		InvalidateIf:     []string{"official forecast flips"},
		SourceReferences: []string{"kalshi_market:KAL-RAIN"},
		MaxSpreadPct:     8,
		MinLiquidity:     1000,
		StopPolicy:       "hold until invalidation or close window",
		TargetPolicy:     "take profit on repricing",
	}
}
