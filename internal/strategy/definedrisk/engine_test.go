package definedrisk

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestVerticalStructuresReplayAndSettlement(t *testing.T) {
	cases := []struct {
		strategy                              Strategy
		optionType, lowPosition, highPosition string
	}{{BullCall, "call", "long", "short"}, {BearPut, "put", "short", "long"}, {BullPut, "put", "long", "short"}, {BearCall, "call", "short", "long"}}
	for _, tc := range cases {
		t.Run(string(tc.strategy), func(t *testing.T) {
			policy := testPolicy(t, ExecutionAtomic)
			scenario := testScenario(t, policy, tc.strategy, tc.optionType, tc.lowPosition, tc.highPosition, "105", "10")
			report, err := NewReport(policy, scenario)
			if err != nil || report.Outcome() != "settled" || report.Contracts() != 2 || len(report.Fills()) != 2 {
				t.Fatalf("report=%+v err=%v", report, err)
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
		})
	}
}

func TestSequentialOrphanIsUnwoundAndAtomicLeavesNoLeg(t *testing.T) {
	sequential := testPolicy(t, ExecutionSequential)
	scenario := testScenario(t, sequential, BullCall, "call", "long", "short", "105", "0")
	report, err := NewReport(sequential, scenario)
	if err != nil || report.Outcome() != "orphan_unwound" || len(report.Fills()) != 2 || report.OrphanLoss() != "44.000000000000" || report.EndingCash() != "9956.000000000000" {
		t.Fatalf("orphan=%+v err=%v", report, err)
	}
	atomic := testPolicy(t, ExecutionAtomic)
	scenario = testScenario(t, atomic, BullCall, "call", "long", "short", "105", "0")
	report, err = NewReport(atomic, scenario)
	if err != nil || report.Outcome() != "rejected" || len(report.Fills()) != 0 {
		t.Fatalf("atomic=%+v err=%v", report, err)
	}
}

func TestEvidenceStructureAndCapitalFailClosed(t *testing.T) {
	policy := testPolicy(t, ExecutionSequential)
	input := scenarioInput(policy, BullCall, "call", "long", "short", "105", "10")
	input.Legs[0].Entry.AvailableAt = input.DecisionAt.Add(time.Microsecond)
	if _, err := NewScenario(input); err == nil {
		t.Fatal("future quote accepted")
	}
	input = scenarioInput(policy, BullCall, "call", "short", "long", "105", "10")
	if _, err := NewScenario(input); err == nil {
		t.Fatal("invalid structure accepted")
	}
	input = scenarioInput(policy, BullCall, "call", "long", "short", "105", "10")
	input.Legs[0].Strike = "not-a-number"
	if _, err := NewScenario(input); err == nil {
		t.Fatal("invalid strike accepted")
	}
	scenario := testScenario(t, policy, BullCall, "call", "long", "short", "105", "10")
	raw := bytes.Replace(scenario.CanonicalBytes(), []byte(`"terminal_underlying":"105"`), []byte(`"terminal_underlying":"106"`), 1)
	if _, err := ScenarioFromCanonical(scenario.ID(), scenario.Digest(), raw, policy); err == nil {
		t.Fatal("revised terminal accepted")
	}
	lowCapital, err := NewPolicy(PolicyInput{"v1", ExecutionAtomic, 60, 5, "10", "1", 12})
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewReport(lowCapital, testScenario(t, lowCapital, BullCall, "call", "long", "short", "105", "10"))
	if err != nil || report.Outcome() != "rejected" || !strings.Contains(string(report.CanonicalBytes()), "insufficient_capital") {
		t.Fatalf("capital=%+v err=%v", report, err)
	}
}

func testPolicy(t *testing.T, mode ExecutionMode) *Policy {
	t.Helper()
	value, err := NewPolicy(PolicyInput{"v1", mode, 60, 5, "10000", "1", 12})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testScenario(t *testing.T, policy *Policy, strategy Strategy, optionType, lowPosition, highPosition, terminal, shortDepth string) *Scenario {
	t.Helper()
	value, err := NewScenario(scenarioInput(policy, strategy, optionType, lowPosition, highPosition, terminal, shortDepth))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func scenarioInput(policy *Policy, strategy Strategy, optionType, lowPosition, highPosition, terminal, shortDepth string) ScenarioInput {
	decision := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	expiry := decision.Add(24 * time.Hour)
	leg := func(salt, strike, position, bid, ask, bidSize, askSize string) LegInput {
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("defined-risk/"+salt))
		entry := quote(salt+"/entry", decision, bid, ask, bidSize, askSize)
		var unwind *QuoteInput
		if policy.canonical.ExecutionMode == ExecutionSequential && position == "long" {
			value := quote(salt+"/unwind", decision, "1.8", "2.2", "10", "10")
			unwind = &value
		}
		return LegInput{id, uuid.NewSHA1(uuid.NameSpaceOID, []byte("defined-risk/contract/"+salt)), "TEST" + salt, "TEST", optionType, strike, expiry, "100", "european", position, entry, unwind}
	}
	return ScenarioInput{policy, strategy, "10000", 2, decision, expiry, terminal, expiry, uuid.NewSHA1(uuid.NameSpaceOID, []byte("terminal")), strings.Repeat("9", 64), strings.Repeat("8", 64), "defined-risk/terminal", strategycatalog.ExperimentPaperScored, []LegInput{leg("low", "100", lowPosition, "1.8", "2", "10", "10"), leg("high", "110", highPosition, "0.8", "1", shortDepth, "10")}}
}

func quote(salt string, at time.Time, bid, ask, bidSize, askSize string) QuoteInput {
	return QuoteInput{bid, ask, bidSize, askSize, at, uuid.NewSHA1(uuid.NameSpaceOID, []byte(salt)), strings.Repeat("a", 64), strings.Repeat("b", 64), salt}
}
