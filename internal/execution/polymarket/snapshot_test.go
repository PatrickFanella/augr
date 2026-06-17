package polymarket

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
)

func TestSnapshotValidateExecutableSide(t *testing.T) {
	now := time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC)
	future := now.Add(2 * time.Hour)

	valid := Snapshot{
		Slug:       "will-example-happen",
		YesTokenID: "yes-token",
		NoTokenID:  "no-token",
		EndDate:    &future,
		Liquidity:  1250,
		BestBidYes: 0.42,
		BestAskYes: 0.45,
		BestBidNo:  0.55,
		BestAskNo:  0.58,
		FetchedAt:  now,
	}
	for _, side := range []string{"YES", "NO"} {
		if err := valid.ValidateExecutableSide(side, 1000, now); err != nil {
			t.Fatalf("valid %s snapshot rejected: %v", side, err)
		}
	}

	cases := []struct {
		name string
		side string
		mod  func(*Snapshot)
	}{
		{name: "missing slug", side: "YES", mod: func(s *Snapshot) { s.Slug = "" }},
		{name: "nil end date", side: "YES", mod: func(s *Snapshot) { s.EndDate = nil }},
		{name: "past end date", side: "YES", mod: func(s *Snapshot) { past := now.Add(-time.Minute); s.EndDate = &past }},
		{name: "low liquidity", side: "YES", mod: func(s *Snapshot) { s.Liquidity = 999.99 }},
		{name: "missing no book", side: "NO", mod: func(s *Snapshot) { s.BestBidNo = 0; s.BestAskNo = 0 }},
		{name: "invalid side", side: "MAYBE", mod: func(s *Snapshot) {}},
		{name: "malformed yes quotes", side: "YES", mod: func(s *Snapshot) { s.BestBidYes = 0.5; s.BestAskYes = 0.4 }},
		{name: "malformed no quotes", side: "NO", mod: func(s *Snapshot) { s.BestBidNo = 0.6; s.BestAskNo = 0.5 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.mod(&s)
			if err := s.ValidateExecutableSide(tc.side, 1000, now); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestSnapshotValidateActivationRequiresBothSideBooks(t *testing.T) {
	now := time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC)
	future := now.Add(2 * time.Hour)
	s := Snapshot{
		Slug:       "will-example-happen",
		YesTokenID: "yes-token",
		NoTokenID:  "no-token",
		EndDate:    &future,
		Liquidity:  1250,
		BestBidYes: 0.42,
		BestAskYes: 0.45,
	}
	if err := s.ValidateActivation(1000, now); err == nil {
		t.Fatal("expected missing NO book to fail activation validation")
	}

	s.BestBidNo = 0.55
	s.BestAskNo = 0.58
	if err := s.ValidateActivation(1000, now); err != nil {
		t.Fatalf("activation validation rejected complete side books: %v", err)
	}
}

func TestEntryPriceForSideRequiresExecutableAsk(t *testing.T) {
	s := Snapshot{BestBidYes: 0.42, BestAskYes: 0.45, NoPrice: 0.58}
	if got, ok := s.EntryPriceForSide("YES"); !ok || got != 0.45 {
		t.Fatalf("YES entry price = %v, %v; want ask, true", got, ok)
	}
	if got, ok := s.EntryPriceForSide("NO"); ok || got != 0 {
		t.Fatalf("NO entry price = %v, %v; want 0, false without NO ask", got, ok)
	}
	s.BestAskNo = 0.59
	if got, ok := s.EntryPriceForSide("NO"); !ok || got != 0.59 {
		t.Fatalf("NO entry price = %v, %v; want NO ask, true", got, ok)
	}
}

func TestSpreadForSidePreservesYesFallbackWithMissingBid(t *testing.T) {
	s := Snapshot{BestAskYes: 0.45}

	got, ok := s.SpreadForSide("YES")
	if !ok || got != 0.45 {
		t.Fatalf("YES spread fallback = %v, %v; want ask-minus-zero, true", got, ok)
	}
}

func TestSnapshotFromPredictionMarketData(t *testing.T) {
	now := time.Date(2026, time.June, 13, 13, 0, 0, 0, time.UTC)
	end := now.Add(24 * time.Hour)
	m := &agent.PredictionMarketData{
		Slug:               "will-example-happen",
		Question:           "Will example happen?",
		Description:        "Example market",
		ResolutionCriteria: "Example resolves on official source",
		ResolutionSource:   "official",
		EndDate:            &end,
		ConditionID:        "cond",
		YesTokenID:         "yes",
		NoTokenID:          "no",
		YesPrice:           0.61,
		NoPrice:            0.39,
		BestBidYes:         0.60,
		BestAskYes:         0.62,
		SpreadYes:          0.02,
		Liquidity:          2000,
		Volume24h:          500,
		OpenInterest:       750,
		BestBidNo:          0.38,
		BestAskNo:          0.40,
	}

	s := SnapshotFromPredictionMarketData(m, now)
	if s.Slug != m.Slug || s.YesTokenID != m.YesTokenID || s.NoTokenID != m.NoTokenID || s.FetchedAt != now {
		t.Fatalf("unexpected snapshot conversion: %+v", s)
	}
}
