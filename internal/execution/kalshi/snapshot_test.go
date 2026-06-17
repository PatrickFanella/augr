package kalshi

import (
	"math"
	"testing"
	"time"
)

func TestSnapshotValidateExecutableSide(t *testing.T) {
	now := time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC)
	future := now.Add(2 * time.Hour)

	valid := Snapshot{
		Ticker:       "KXTEST-YESNO",
		Title:        "Will test happen?",
		Status:       "active",
		BestBidYes:   0.45,
		BestAskYes:   0.47,
		BestBidNo:    0.53,
		BestAskNo:    0.55,
		Volume:       1000,
		OpenInterest: 500,
		CloseTime:    future,
		FetchedAt:    now,
	}
	for _, side := range []string{"YES", "NO"} {
		if err := valid.ValidateExecutableSide(side, 100, now); err != nil {
			t.Fatalf("valid %s snapshot rejected: %v", side, err)
		}
	}

	cases := []struct {
		name string
		side string
		mod  func(*Snapshot)
	}{
		{name: "missing ticker", side: "YES", mod: func(s *Snapshot) { s.Ticker = "" }},
		{name: "missing title", side: "YES", mod: func(s *Snapshot) { s.Title = "" }},
		{name: "missing status", side: "YES", mod: func(s *Snapshot) { s.Status = "" }},
		{name: "closed status", side: "YES", mod: func(s *Snapshot) { s.Status = "closed" }},
		{name: "settled status", side: "YES", mod: func(s *Snapshot) { s.Status = "settled" }},
		{name: "expired status", side: "YES", mod: func(s *Snapshot) { s.Status = "expired" }},
		{name: "nil close time", side: "YES", mod: func(s *Snapshot) { s.CloseTime = time.Time{} }},
		{name: "past close time", side: "YES", mod: func(s *Snapshot) { past := now.Add(-time.Minute); s.CloseTime = past }},
		{name: "low liquidity", side: "YES", mod: func(s *Snapshot) { s.Volume = 10; s.OpenInterest = 5 }},
		{name: "invalid side", side: "MAYBE", mod: func(s *Snapshot) {}},
		{name: "malformed yes quotes", side: "YES", mod: func(s *Snapshot) { s.BestBidYes = 0.5; s.BestAskYes = 0.4 }},
		{name: "malformed no quotes", side: "NO", mod: func(s *Snapshot) { s.BestBidNo = 0.6; s.BestAskNo = 0.5 }},
		{name: "missing no quote", side: "NO", mod: func(s *Snapshot) { s.BestBidNo = 0; s.BestAskNo = 0 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.mod(&s)
			if err := s.ValidateExecutableSide(tc.side, 100, now); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestEntryPriceAndSpreadForSideRequireExplicitQuotes(t *testing.T) {
	s := Snapshot{
		BestBidYes: 0.45,
		BestAskYes: 0.47,
	}

	if got, ok := s.EntryPriceForSide("YES"); !ok || got != 0.47 {
		t.Fatalf("YES entry price = %v, %v; want 0.47, true", got, ok)
	}
	if got, ok := s.SpreadForSide("YES"); !ok || math.Abs(got-0.02) > 1e-9 {
		t.Fatalf("YES spread = %v, %v; want 0.02, true", got, ok)
	}
	if got, ok := s.EntryPriceForSide("NO"); ok || got != 0 {
		t.Fatalf("NO entry price = %v, %v; want 0, false without NO quotes", got, ok)
	}
	if got, ok := s.SpreadForSide("NO"); ok || got != 0 {
		t.Fatalf("NO spread = %v, %v; want 0, false without NO quotes", got, ok)
	}

	s.BestBidNo = 0.53
	s.BestAskNo = 0.55
	if got, ok := s.EntryPriceForSide("NO"); !ok || got != 0.55 {
		t.Fatalf("NO entry price = %v, %v; want 0.55, true", got, ok)
	}
	if got, ok := s.SpreadForSide("NO"); !ok || math.Abs(got-0.02) > 1e-9 {
		t.Fatalf("NO spread = %v, %v; want 0.02, true", got, ok)
	}
}
