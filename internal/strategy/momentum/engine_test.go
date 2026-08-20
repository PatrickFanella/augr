package momentum

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestMomentumRanksCapsTurnoverCostsAndRegimesDeterministically(t *testing.T) {
	t.Parallel()
	policy, scenario := momentumFixture(t)
	report, err := NewReport(policy, scenario)
	if err != nil {
		t.Fatal(err)
	}
	rebalances := report.Rebalances()
	if len(rebalances) != 3 || rebalances[0].Regime != "bull" || rebalances[1].Regime != "bear" || rebalances[2].Regime != "sideways" {
		t.Fatalf("regimes=%+v", rebalances)
	}
	for _, value := range rebalances {
		if value.AppliedTurnover > "0.250000000000" {
			t.Fatalf("turnover cap=%s", value.AppliedTurnover)
		}
	}
	if report.CumulativeTurnover() == "0.000000000000" || report.AfterCostTotalReturn() == "" {
		t.Fatalf("report=%s/%s", report.CumulativeTurnover(), report.AfterCostTotalReturn())
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
	if err != nil || restored.ID() != report.ID() {
		t.Fatalf("restore=%v/%v", restored, err)
	}
}

func TestMomentumInputOrderThresholdsAndStaleEvidence(t *testing.T) {
	t.Parallel()
	policy, first := momentumFixture(t)
	input := momentumScenarioInput(policy)
	for i := range input.Rebalances {
		input.Rebalances[i].Members[0], input.Rebalances[i].Members[1] = input.Rebalances[i].Members[1], input.Rebalances[i].Members[0]
	}
	second, err := NewScenario(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != second.ID() || !bytes.Equal(first.CanonicalBytes(), second.CanonicalBytes()) {
		t.Fatal("input order changed identity")
	}
	edge := momentumScenarioInput(policy)
	edge.Rebalances[0].Members[0].ROIC = "0.1"
	edge.Rebalances[0].Members[0].DebtToAssets = "0.6"
	if _, err = NewScenario(edge); err != nil {
		t.Fatal(err)
	}
	stale := momentumScenarioInput(policy)
	stale.Rebalances[0].Members[0].AvailableAt = stale.Rebalances[0].OccurredAt.Add(-49 * time.Hour)
	staleScenario, err := NewScenario(stale)
	if err != nil {
		t.Fatal(err)
	}
	staleReport, err := NewReport(policy, staleScenario)
	if err != nil {
		t.Fatal(err)
	}
	if staleReport.Rebalances()[0].Ranks[0].Reason != "evidence_stale" && staleReport.Rebalances()[0].Ranks[1].Reason != "evidence_stale" {
		t.Fatal("stale evidence admitted")
	}
}

func TestMomentumFailsClosedWhenHeldConstituentDisappears(t *testing.T) {
	t.Parallel()
	policy := momentumPolicy(t)
	input := momentumScenarioInput(policy)
	input.Rebalances[1].Members = input.Rebalances[1].Members[1:]
	scenario, err := NewScenario(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewReport(policy, scenario); err == nil || !strings.Contains(err.Error(), "absent from point-in-time universe") {
		t.Fatalf("missing held constituent error=%v", err)
	}
}

func momentumFixture(t *testing.T) (*Policy, *Scenario) {
	t.Helper()
	policy := momentumPolicy(t)
	scenario, err := NewScenario(momentumScenarioInput(policy))
	if err != nil {
		t.Fatal(err)
	}
	return policy, scenario
}

func momentumPolicy(t *testing.T) *Policy {
	t.Helper()
	value, err := NewPolicy(PolicyInput{Version: "momentum-quality-v1", LookbackDays: 252, SkipDays: 21, MinimumHistoryDays: 252, MinimumROIC: "0.1", MaximumDebtToAssets: "0.6", RequirePositiveFreeCash: true, MaximumEvidenceAgeSeconds: 48 * 3600, MaximumVolatility: "0.5", PortfolioSize: 2, MaximumRebalanceTurnover: "0.25", CostBPS: "10", BullBearTrendThreshold: "0.05", MaximumBullVolatility: "0.25", DecimalScale: 12})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func momentumScenarioInput(policy *Policy) ScenarioInput {
	start := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	at := []time.Time{start, start.Add(30 * 24 * time.Hour), start.Add(60 * 24 * time.Hour)}
	trends := []string{"0.1", "-0.1", "0.01"}
	vols := []string{"0.2", "0.4", "0.3"}
	rebalances := make([]RebalanceInput, 3)
	for i := range rebalances {
		rebalances[i] = RebalanceInput{OccurredAt: at[i], BenchmarkTrend: trends[i], BenchmarkVolatility: vols[i], BenchmarkEvidenceID: momentumID(fmt.Sprintf("benchmark-%d", i)), BenchmarkEvidenceSHA256: strings.Repeat(string(rune('a'+i)), 64), Members: []MemberInput{momentumMember(at[i], i, 1, "100", "120", "119", "120", "0.2", "0.2"), momentumMember(at[i], i, 2, "100", "110", "109", "110", "0.15", "0.15"), momentumMember(at[i], i, 3, "100", "130", "129", "130", "0.01", "0.1")}}
	}
	return ScenarioInput{Policy: policy, InitialCapital: "10000", EvaluationStart: start, EvaluationEnd: at[2], Mode: strategycatalog.ExperimentPaperScored, Rebalances: rebalances}
}

func momentumMember(at time.Time, sequence, salt int, lookback, skip, bid, ask, roic, vol string) MemberInput {
	return MemberInput{InstrumentID: momentumID(fmt.Sprintf("instrument-%d", salt)), VenueContractID: momentumID(fmt.Sprintf("contract-%d", salt)), MembershipEffectiveAt: at.Add(-24 * time.Hour), MembershipAvailableAt: at, HistoryDays: 300, LookbackPrice: lookback, SkipPrice: skip, Bid: bid, Ask: ask, ROIC: roic, DebtToAssets: "0.3", FreeCashFlow: "100", Volatility: vol, PartitionContentSHA256: strings.Repeat("1", 64), SourceKey: fmt.Sprintf("momentum-%d-%d", sequence, salt), EvidenceSHA256: strings.Repeat(string(rune('3'+salt)), 64), AvailableAt: at}
}

func momentumID(value string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("momentum/"+value))
}
