package qualification

import "testing"

func TestLifecycleScenariosCoverRequiredWheelOutcomes(t *testing.T) {
	t.Parallel()
	fixture, err := BuildLifecycleScenarios()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"put_expiry": "option_expired", "put_assignment": "put_assigned", "dividend": "dividend_credited", "covered_call_expiry": "option_expired", "call_away": "shares_called_away"}
	for name, action := range want {
		transitions := fixture.Reports[name].Transitions()
		if transitions[len(transitions)-1].Action != action {
			t.Fatalf("%s final action=%s", name, transitions[len(transitions)-1].Action)
		}
	}
	if fixture.Reports["call_away"].CappedUpside() != "1000.000000000000" {
		t.Fatalf("call-away cap=%s", fixture.Reports["call_away"].CappedUpside())
	}
}
