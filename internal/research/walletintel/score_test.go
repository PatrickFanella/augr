package walletintel

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestScoreWalletSanitizesAndIsJSONSafe(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	score := ScoreWallet(WalletSample{
		WalletID:         "  alpha  ",
		RealizedROI:      math.Inf(1),
		TradeCount:       -3,
		CalibrationScore: math.NaN(),
		LastTradeAt:      time.Time{},
		CategoryExposure: map[string]float64{"": 3, "defi": math.Inf(1), "sports": 4},
	}, ScoreConfig{Now: now})

	if score.WalletID != "alpha" {
		t.Fatalf("WalletID = %q", score.WalletID)
	}
	if score.Score < 0 || score.Score > 1 || math.IsNaN(score.Score) || math.IsInf(score.Score, 0) {
		t.Fatalf("Score = %v", score.Score)
	}
	if score.CategoryConcentration < 0 || score.CategoryConcentration > 1 || math.IsNaN(score.CategoryConcentration) || math.IsInf(score.CategoryConcentration, 0) {
		t.Fatalf("CategoryConcentration = %v", score.CategoryConcentration)
	}
	for name, value := range score.Components {
		if value < 0 || value > 1 || math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("component %s = %v", name, value)
		}
	}
	if len(score.Reasons) == 0 {
		t.Fatal("expected reasons for invalid inputs")
	}
	if _, err := json.Marshal(score); err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
}

func TestScoreWalletsSortsByScoreThenWalletID(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	inputs := []WalletSample{
		{
			WalletID:         "winner",
			RealizedROI:      1.8,
			TradeCount:       80,
			CalibrationScore: 0.95,
			LastTradeAt:      now.Add(-time.Hour),
			CategoryExposure: map[string]float64{"defi": 1, "gaming": 1},
		},
		{
			WalletID:         "beta",
			RealizedROI:      0.25,
			TradeCount:       12,
			CalibrationScore: 0.60,
			LastTradeAt:      now.Add(-2 * time.Hour),
			CategoryExposure: map[string]float64{"defi": 1},
		},
		{
			WalletID:         "alpha",
			RealizedROI:      0.25,
			TradeCount:       12,
			CalibrationScore: 0.60,
			LastTradeAt:      now.Add(-2 * time.Hour),
			CategoryExposure: map[string]float64{"defi": 1},
		},
	}

	got := ScoreWallets(inputs, ScoreConfig{Now: now})
	if len(got) != len(inputs) {
		t.Fatalf("len(ScoreWallets()) = %d", len(got))
	}
	if got[0].WalletID != "winner" {
		t.Fatalf("top wallet = %q, want winner", got[0].WalletID)
	}
	if got[1].Score != got[2].Score {
		t.Fatalf("tie scores differ: %v vs %v", got[1].Score, got[2].Score)
	}
	if got[1].WalletID != "alpha" || got[2].WalletID != "beta" {
		t.Fatalf("tie sort order = [%q %q], want [alpha beta]", got[1].WalletID, got[2].WalletID)
	}
}

func TestScoreSourceDoesNotContainExecutionOrOrderTerms(t *testing.T) {
	path := "score.go"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	text := string(src)
	for _, forbidden := range []string{
		"github.com/",
		"internal/execution",
		"internal/api",
		"internal/order",
		"broker",
		"live trading",
		"order intent",
		"trade instruction",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("score.go contains forbidden text %q", forbidden)
		}
	}
}
