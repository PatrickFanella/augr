package domain

import "testing"

func TestPaperEvaluationProfilesAreIsolated(t *testing.T) {
	t.Parallel()

	scored, err := NewPaperEvaluationProfile(PaperEvaluationModeScored, 100_000, 2, 5, 0.0001)
	if err != nil {
		t.Fatal(err)
	}
	stress, err := NewPaperEvaluationProfile(PaperEvaluationModeStress, 100_000, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !scored.PromotionEligible() || stress.PromotionEligible() {
		t.Fatalf("promotion eligibility = scored:%v stress:%v", scored.PromotionEligible(), stress.PromotionEligible())
	}
	if scored.StorageNamespace == stress.StorageNamespace || scored.CanShareStorageWith(stress) {
		t.Fatalf("scored and stress profiles share storage: scored=%+v stress=%+v", scored, stress)
	}
	if !scored.CanShareStorageWith(scored) || !stress.CanShareStorageWith(stress) {
		t.Fatal("same-mode profiles should share their own namespace")
	}
}

func TestPaperEvaluationProfileValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mode       PaperEvaluationMode
		capital    float64
		multiplier float64
		slippage   float64
		fee        float64
	}{
		{name: "unknown mode", mode: "paper_unknown", capital: 100_000, multiplier: 1},
		{name: "zero capital", mode: PaperEvaluationModeScored, multiplier: 1},
		{name: "scored unlimited buying power", mode: PaperEvaluationModeScored, capital: 100_000},
		{name: "negative multiplier", mode: PaperEvaluationModeStress, capital: 100_000, multiplier: -1},
		{name: "negative slippage", mode: PaperEvaluationModeScored, capital: 100_000, multiplier: 1, slippage: -1},
		{name: "fee over one", mode: PaperEvaluationModeScored, capital: 100_000, multiplier: 1, fee: 1.1},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewPaperEvaluationProfile(test.mode, test.capital, test.multiplier, test.slippage, test.fee); err == nil {
				t.Fatal("NewPaperEvaluationProfile() error = nil, want error")
			}
		})
	}
}
