package trend

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestTrendSignalsVolatilityScalingTurnoverAndReplay(t *testing.T) {
	policy := trendPolicy(t)
	scenario := trendScenario(t, policy)
	report, err := NewReport(policy, scenario)
	if err != nil {
		t.Fatal(err)
	}
	restoredPolicy, err := PolicyFromCanonical(policy.ID(), policy.Digest(), policy.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	restoredScenario, err := ScenarioFromCanonical(scenario.ID(), scenario.Digest(), scenario.CanonicalBytes(), restoredPolicy)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ReportFromCanonical(report.ID(), report.Digest(), report.CanonicalBytes(), restoredPolicy, restoredScenario)
	if err != nil || !bytes.Equal(restored.CanonicalBytes(), report.CanonicalBytes()) {
		t.Fatalf("replay=%v", err)
	}
	first := report.Rebalances()[0]
	longSignals := 0
	for _, signal := range first.Signals {
		if signal.Long {
			longSignals++
			if signal.Score != "0.500000000000" || signal.TargetWeight != "0.400000000000" {
				t.Fatalf("long signal=%+v", signal)
			}
		}
	}
	if len(first.Signals) != 2 || longSignals != 1 || first.GrossTargetWeight != "0.400000000000" || first.TurnoverScale != "1.000000000000" || len(first.Trades) != 1 {
		t.Fatalf("first=%+v", first)
	}
	if report.AfterCostTotalReturn() == "0.000000000000" {
		t.Fatal("costed return was not derived")
	}
}

func TestTrendInputOrderThresholdAndStaleEvidence(t *testing.T) {
	policy := trendPolicy(t)
	scenario := trendScenario(t, policy)
	input := trendScenarioInput(policy)
	input.Rebalances[0].Members[0], input.Rebalances[0].Members[1] = input.Rebalances[0].Members[1], input.Rebalances[0].Members[0]
	reordered, err := NewScenario(input)
	if err != nil || reordered.ID() != scenario.ID() {
		t.Fatalf("order=%v", err)
	}
	input = trendScenarioInput(policy)
	input.Rebalances[0].Members[0].AvailableAt = input.Rebalances[0].OccurredAt.Add(time.Microsecond)
	if _, err = NewScenario(input); err == nil {
		t.Fatal("future evidence accepted")
	}
	input = trendScenarioInput(policy)
	input.Rebalances[0].Members[0].AvailableAt = input.Rebalances[0].OccurredAt.Add(-61 * time.Second)
	if _, err = NewScenario(input); err == nil {
		t.Fatal("stale evidence accepted")
	}
	raw := bytes.Replace(scenario.CanonicalBytes(), []byte(`"current_price":"120"`), []byte(`"current_price":"121"`), 1)
	if _, err = ScenarioFromCanonical(scenario.ID(), scenario.Digest(), raw, policy); err == nil {
		t.Fatal("revised evidence accepted")
	}
}

func TestTrendFailsWhenHeldETFDisappears(t *testing.T) {
	policy := trendPolicy(t)
	input := trendScenarioInput(policy)
	input.Rebalances[1].Members = input.Rebalances[1].Members[1:]
	scenario, err := NewScenario(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewReport(policy, scenario); err == nil || !strings.Contains(err.Error(), "absent") {
		t.Fatalf("missing held=%v", err)
	}
}

func trendPolicy(t *testing.T) *Policy {
	t.Helper()
	p, err := NewPolicy(PolicyInput{Version: "trend-v1", Horizons: []Horizon{{21, "0.75"}, {63, "0.25"}}, SignalThreshold: "0", VolatilityWindowDays: 20, AnnualizationDays: 252, TargetVolatility: "0.12", MaximumInstrumentWeight: "0.4", MaximumGrossWeight: "0.8", MaximumRebalanceTurnover: "0.25", MaximumEvidenceAgeSeconds: 60, CostBPS: "10", DecimalScale: 12})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func trendScenario(t *testing.T, p *Policy) *Scenario {
	t.Helper()
	s, err := NewScenario(trendScenarioInput(p))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func trendScenarioInput(p *Policy) ScenarioInput {
	start := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	return ScenarioInput{p, "10000", start, end, strategycatalog.ExperimentPaperScored, []RebalanceInput{{start, []MemberInput{trendMember("a", start, []string{"100", "130"}, "120", "0.2"), trendMember("b", start, []string{"120", "130"}, "100", "0.1")}}, {end, []MemberInput{trendMember("a", end, []string{"100", "110"}, "90", "0.2"), trendMember("b", end, []string{"80", "90"}, "110", "0.3")}}}}
}

func trendMember(salt string, at time.Time, anchors []string, current, vol string) MemberInput {
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("trend/"+salt))
	return MemberInput{id, uuid.NewSHA1(uuid.NameSpaceOID, []byte("trend/contract/"+salt)), at.Add(-24 * time.Hour), at, anchors, current, vol, current, current + ".1", "1", strings.Repeat("1", 64), "trend-" + salt + "-" + at.Format("20060102"), strings.Repeat("2", 64), at}
}
