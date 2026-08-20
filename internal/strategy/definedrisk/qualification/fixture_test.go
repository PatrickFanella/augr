package qualification

import "testing"

func TestRetainedScenarioMatrix(t *testing.T) {
	values, err := BuildRetainedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 7 {
		t.Fatalf("scenarios=%d", len(values))
	}
	want := map[string]string{"atomic_bull_call_winner": "settled", "atomic_bear_put_winner": "settled", "atomic_bull_put_loser": "settled", "atomic_bear_call_exact_strike": "settled", "atomic_depth_rejected": "rejected", "sequential_success": "settled", "sequential_orphan_unwind": "orphan_unwound"}
	for name, outcome := range want {
		if values[name] == nil || values[name].Report.Outcome() != outcome {
			t.Fatalf("%s outcome=%v", name, values[name])
		}
	}
}
