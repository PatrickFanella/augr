package wheel

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWheelAssignmentDividendCoveredCallAndCappedUpside(t *testing.T) {
	t.Parallel()
	policy, scenario := wheelFixture(t, false)
	report, err := NewReport(policy, scenario)
	if err != nil {
		t.Fatal(err)
	}
	transitions := report.Transitions()
	if len(transitions) != 6 || transitions[1].Action != "short_put_opened" || transitions[1].Collateral != "9500.000000000000" || transitions[2].Action != "put_assigned" || transitions[3].Action != "dividend_credited" || transitions[4].Action != "covered_call_opened" || transitions[5].Action != "shares_called_away" {
		t.Fatalf("transitions=%+v", transitions)
	}
	if report.EndingShares() != "0.000000000000" || report.EndingCash() != "10948.000000000000" || report.CappedUpside() != "1000.000000000000" || report.AfterCostTotalReturn() != "0.094800000000" {
		t.Fatalf("ending cash=%s shares=%s cap=%s return=%s", report.EndingCash(), report.EndingShares(), report.CappedUpside(), report.AfterCostTotalReturn())
	}
	restoredPolicy, err := PolicyFromCanonical(policy.ID(), policy.Digest(), policy.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	restoredScenario, err := ScenarioFromCanonical(scenario.ID(), scenario.Digest(), scenario.CanonicalBytes(), restoredPolicy)
	if err != nil {
		t.Fatal(err)
	}
	restoredReport, err := ReportFromCanonical(report.ID(), report.Digest(), report.CanonicalBytes(), restoredPolicy, restoredScenario)
	if err != nil || restoredReport.ID() != report.ID() {
		t.Fatalf("restore=%v/%v", restoredReport, err)
	}
}

func TestWheelCandidateInputOrderDoesNotChangeIdentity(t *testing.T) {
	t.Parallel()
	policy, first := wheelFixture(t, true)
	input := wheelScenarioInput(policy, true)
	for i := range input.Events {
		if len(input.Events[i].Candidates) > 1 {
			input.Events[i].Candidates[0], input.Events[i].Candidates[1] = input.Events[i].Candidates[1], input.Events[i].Candidates[0]
		}
	}
	second, err := NewScenario(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != second.ID() || !bytes.Equal(first.CanonicalBytes(), second.CanonicalBytes()) {
		t.Fatal("candidate input order changed scenario identity")
	}
	left, _ := NewReport(policy, first)
	right, _ := NewReport(policy, second)
	if left.ID() != right.ID() {
		t.Fatal("candidate input order changed report")
	}
}

func TestWheelQualityThresholdEdgesPass(t *testing.T) {
	t.Parallel()
	policy := wheelPolicy(t)
	input := wheelScenarioInput(policy, false)
	input.Events[0].Quality.ROIC = "0.1"
	input.Events[0].Quality.DebtToAssets = "0.6"
	scenario, err := NewScenario(input)
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewReport(policy, scenario)
	if err != nil || report.Transitions()[0].Reason != "quality_passed" {
		t.Fatalf("threshold edge=%v/%v", report, err)
	}
}

func TestWheelFailsClosedOnQualityNakedRiskAndForgedEvidence(t *testing.T) {
	t.Parallel()
	policy, _ := wheelFixture(t, false)
	t.Run("failed quality rejects put", func(t *testing.T) {
		input := wheelScenarioInput(policy, false)
		input.Events[0].Quality.ROIC = "0.01"
		scenario, err := NewScenario(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = NewReport(policy, scenario); err == nil { // rejected put leaves no option for the later expiry
			t.Fatalf("report error=%v", err)
		}
	})
	t.Run("call without shares", func(t *testing.T) {
		input := wheelScenarioInput(policy, false)
		input.Events = input.Events[:2]
		input.EvaluationEnd = input.EvaluationStart.Add(24 * time.Hour)
		input.Events[1] = wheelCallEvent(input.EvaluationEnd, input.UnderlyingID, false)
		input.Events[1].Candidates[0].AvailableAt = input.EvaluationEnd
		scenario, err := NewScenario(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = NewReport(policy, scenario); err == nil || !strings.Contains(err.Error(), "covered call requires") {
			t.Fatalf("naked call error=%v", err)
		}
	})
	t.Run("tampered canonical", func(t *testing.T) {
		_, scenario := wheelFixture(t, false)
		raw := bytes.Replace(scenario.CanonicalBytes(), []byte(`"strike":"95"`), []byte(`"strike":"96"`), 1)
		if _, err := ScenarioFromCanonical(scenario.ID(), scenario.Digest(), raw, policy); err == nil {
			t.Fatal("tampered scenario restored")
		}
	})
}

func TestWheelOutOfMoneyExpiryRetainsPremiumAndReleasesCollateral(t *testing.T) {
	t.Parallel()
	policy := wheelPolicy(t)
	input := wheelScenarioInput(policy, false)
	input.Events = input.Events[:3]
	input.EvaluationEnd = input.Events[2].OccurredAt
	input.Events[2].UnderlyingMark = "100"
	scenario, err := NewScenario(input)
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewReport(policy, scenario)
	if err != nil {
		t.Fatal(err)
	}
	last := report.Transitions()[2]
	if last.Action != "option_expired" || last.Collateral != "0.000000000000" || report.EndingShares() != "0.000000000000" || report.EndingCash() != "10199.000000000000" {
		t.Fatalf("expiry=%+v cash=%s", last, report.EndingCash())
	}
}

func TestWheelSourcedEarlyPutAssignmentAndStaleMarketData(t *testing.T) {
	t.Parallel()
	policy := wheelPolicy(t)
	input := wheelScenarioInput(policy, false)
	input.Events = input.Events[:3]
	input.EvaluationEnd = input.EvaluationStart.Add(10 * 24 * time.Hour)
	input.Events[2] = EventInput{Kind: EventAssignment, OccurredAt: input.EvaluationEnd, UnderlyingMark: "98", AssignmentOptionID: input.Events[1].Candidates[0].InstrumentID, EvidenceID: wheelID(77), EvidenceSHA256: strings.Repeat("7", 64)}
	scenario, err := NewScenario(input)
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewReport(policy, scenario)
	if err != nil || report.Transitions()[2].Action != "put_assigned" || report.EndingShares() != "100.000000000000" {
		t.Fatalf("early assignment=%v/%v", report, err)
	}

	stale := wheelScenarioInput(policy, false)
	stale.Events[1].Candidates[0].AvailableAt = stale.Events[1].OccurredAt.Add(-3 * 24 * time.Hour)
	staleScenario, err := NewScenario(stale)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewReport(policy, staleScenario); err == nil {
		t.Fatal("stale option evidence admitted a complete lifecycle")
	}
}

func wheelFixture(t *testing.T, extraCandidates bool) (*Policy, *Scenario) {
	t.Helper()
	policy := wheelPolicy(t)
	scenario, err := NewScenario(wheelScenarioInput(policy, extraCandidates))
	if err != nil {
		t.Fatal(err)
	}
	return policy, scenario
}

func wheelPolicy(t *testing.T) *Policy {
	t.Helper()
	value, err := NewPolicy(PolicyInput{Version: "wheel-v1@reviewed", MinimumROIC: "0.1", MaximumDebtToAssets: "0.6", RequirePositiveFreeCash: true, MaximumQualityAgeSeconds: 10 * 24 * 3600, MaximumMarketDataAgeSeconds: 2 * 24 * 3600, PutDeltaMinimum: "0.2", PutDeltaTarget: "0.25", PutDeltaMaximum: "0.3", CallDeltaMinimum: "0.15", CallDeltaTarget: "0.2", CallDeltaMaximum: "0.25", MinimumDTE: 30, MaximumDTE: 45, MinimumOpenInterest: "100", MinimumVolume: "10", MaximumSpreadRatio: "0.2", DeliverableQuantity: "100", MaximumContracts: 2, FeePerContract: "1", FeePerShare: "0", DecimalScale: 12})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func wheelScenarioInput(policy *Policy, extraCandidates bool) ScenarioInput {
	start := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	underlying := uuid.MustParse("40200000-0000-4000-8000-000000000001")
	put := wheelCandidate("put", "95", "-0.25", start.Add(31*24*time.Hour), 2)
	putCandidates := []Candidate{put}
	if extraCandidates {
		putCandidates = append(putCandidates, wheelCandidate("put", "90", "-0.21", start.Add(32*24*time.Hour), 22))
	}
	return ScenarioInput{Policy: policy, UnderlyingID: underlying, InitialCapital: "10000", EvaluationStart: start, EvaluationEnd: start.Add(63 * 24 * time.Hour), Events: []EventInput{
		{Kind: EventAssessQuality, OccurredAt: start, UnderlyingMark: "100", Quality: &QualityEvidence{AvailableAt: start, ROIC: "0.2", DebtToAssets: "0.3", FreeCashFlow: "1000", EvidenceID: wheelID(1), EvidenceSHA256: strings.Repeat("1", 64)}, EvidenceID: wheelID(11), EvidenceSHA256: strings.Repeat("a", 64)},
		{Kind: EventOpenPut, OccurredAt: start.Add(24 * time.Hour), UnderlyingMark: "100", Candidates: putCandidates, EvidenceID: wheelID(12), EvidenceSHA256: strings.Repeat("b", 64)},
		{Kind: EventExpiry, OccurredAt: start.Add(31 * 24 * time.Hour), UnderlyingMark: "90", EvidenceID: wheelID(13), EvidenceSHA256: strings.Repeat("c", 64)},
		{Kind: EventDividend, OccurredAt: start.Add(32 * 24 * time.Hour), UnderlyingMark: "92", DividendPerShare: text("1"), EvidenceID: wheelID(14), EvidenceSHA256: strings.Repeat("d", 64)},
		wheelCallEvent(start.Add(33*24*time.Hour), underlying, extraCandidates),
		{Kind: EventExpiry, OccurredAt: start.Add(63 * 24 * time.Hour), UnderlyingMark: "110", EvidenceID: wheelID(16), EvidenceSHA256: strings.Repeat("f", 64)},
	}}
}

func wheelCallEvent(at time.Time, _ uuid.UUID, extra bool) EventInput {
	start := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	candidates := []Candidate{wheelCandidate("call", "100", "0.2", start.Add(63*24*time.Hour), 5)}
	if extra {
		candidates = append(candidates, wheelCandidate("call", "105", "0.24", start.Add(64*24*time.Hour), 25))
	}
	return EventInput{Kind: EventOpenCall, OccurredAt: at, UnderlyingMark: "95", Candidates: candidates, EvidenceID: wheelID(15), EvidenceSHA256: strings.Repeat("e", 64)}
}

func wheelCandidate(kind, strike, delta string, expiry time.Time, salt int) Candidate {
	return Candidate{InstrumentID: wheelID(100 + salt), VenueContractID: wheelID(200 + salt), OptionType: kind, Strike: strike, Expiry: expiry, Delta: delta, Bid: map[string]string{"put": "2", "call": "1.5"}[kind], Ask: map[string]string{"put": "2.1", "call": "1.6"}[kind], OpenInterest: "1000", Volume: "100", AvailableAt: expiry.Add(-31 * 24 * time.Hour), EvidenceID: wheelID(300 + salt), EvidenceSHA256: strings.Repeat("9", 64)}
}

func wheelID(value int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(value >> 8), byte(value)})
}
func text(value string) *string { return &value }
