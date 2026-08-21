package accountingrecon

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestEvaluateCutoverRequiresThirtyConsecutiveCompleteRealDays(t *testing.T) {
	t.Parallel()

	through := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	runs := make([]*Run, 0, DefaultParityDays)
	for day := DefaultParityDays - 1; day >= 0; day-- {
		runs = append(runs, equalRunAt(t, through.AddDate(0, 0, -day), false))
	}
	clock := completedThroughDateClock(through)
	gate := EvaluateCutover(runs, through, DefaultParityDays, clock, acceptingEvidenceVerifier{})
	if !gate.Ready || len(gate.Reasons) != 0 || len(gate.RunIDs) != DefaultParityDays {
		t.Fatalf("gate = %+v, want ready 30-day evidence", gate)
	}

	if got := EvaluateCutover(runs[:DefaultParityDays-1], through, DefaultParityDays, clock, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("29-day gate unexpectedly ready")
	}
	withGap := append([]*Run(nil), runs...)
	withGap[10] = equalRunAt(t, through.AddDate(0, 0, -100), false)
	if got := EvaluateCutover(withGap, through, DefaultParityDays, clock, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("gapped gate unexpectedly ready")
	}
	synthetic := append([]*Run(nil), runs...)
	synthetic[0] = equalRunAt(t, through.AddDate(0, 0, -(DefaultParityDays-1)), true)
	if got := EvaluateCutover(synthetic, through, DefaultParityDays, clock, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("synthetic evidence unexpectedly qualified")
	}
}

func TestEvaluateCutoverRejectsOneBadDayAndPolicyDrift(t *testing.T) {
	t.Parallel()

	through := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	runs := make([]*Run, 0, DefaultParityDays)
	for day := DefaultParityDays - 1; day >= 0; day-- {
		runs = append(runs, equalRunAt(t, through.AddDate(0, 0, -day), false))
	}
	clock := completedThroughDateClock(through)

	badLegacy := completeSnapshotInput(SourceLegacy)
	badLedger := completeSnapshotInput(SourceLedger)
	badLegacy.AsOf = runs[12].AsOf
	badLegacy.ObservedAt = runs[12].AsOf.Add(time.Second)
	badLedger.AsOf = badLegacy.AsOf
	badLedger.ObservedAt = badLegacy.ObservedAt
	setMetricValue(&badLegacy, MetricCash, decimal.NewFromInt(101))
	legacy, _ := NewSnapshot(badLegacy)
	ledgerSnapshot, _ := NewSnapshot(badLedger)
	bad, err := Compare(ComparisonInput{Legacy: legacy, Ledger: ledgerSnapshot, Generator: "worker", GeneratedAt: legacy.AsOf.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	runs[12] = bad
	if got := EvaluateCutover(runs, through, DefaultParityDays, clock, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("unexplained day unexpectedly qualified")
	}

	runs[12] = equalRunAt(t, badLegacy.AsOf, false)
	runs[12].PolicyVersion = "different-policy"
	if got := EvaluateCutover(runs, through, DefaultParityDays, clock, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("policy drift unexpectedly qualified")
	}
}

func TestEvaluateCutoverRejectsFutureConflictingAndUnauthenticatedEvidence(t *testing.T) {
	t.Parallel()

	through := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	clock := completedThroughDateClock(through)
	run := equalRunAt(t, through, false)
	future := equalRunAt(t, through.AddDate(0, 0, 1), false)
	if got := EvaluateCutover([]*Run{run, future}, through, 1, clock, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("future accounting evidence unexpectedly qualified")
	}

	conflict := *run
	conflict.PayloadBytes = append([]byte(nil), run.PayloadBytes...)
	conflict.PayloadBytes[0] = ' '
	if got := EvaluateCutover([]*Run{&conflict, run}, through, 1, clock, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("conflicting duplicate accounting evidence unexpectedly qualified")
	}

	if got := EvaluateCutover([]*Run{run}, through, 1, clock, rejectingEvidenceVerifier{}); got.Ready {
		t.Fatal("unauthenticated accounting evidence unexpectedly qualified")
	}
}

func TestEvaluateCutoverRequiresTrustedTimeAndOnlyCompletedUTCDays(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	lastCompleted := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	if got := EvaluateCutover([]*Run{equalRunAt(t, lastCompleted, false)}, lastCompleted, 1, nil, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("missing trusted evaluation clock unexpectedly qualified")
	}
	currentDay := utcDay(now)
	if got := EvaluateCutover([]*Run{equalRunAt(t, currentDay, false)}, currentDay, 1, staticEvaluationClock{now: now}, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("incomplete current UTC day unexpectedly qualified")
	}
	futureDay := currentDay.AddDate(0, 0, 1)
	if got := EvaluateCutover([]*Run{equalRunAt(t, futureDay, false)}, futureDay, 1, staticEvaluationClock{now: now}, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("future through date unexpectedly qualified")
	}
	withFutureCandidate := []*Run{equalRunAt(t, lastCompleted, false), equalRunAt(t, futureDay, false)}
	if got := EvaluateCutover(withFutureCandidate, lastCompleted, 1, staticEvaluationClock{now: now}, acceptingEvidenceVerifier{}); got.Ready {
		t.Fatal("future candidate outside the requested window unexpectedly qualified")
	}
}

type acceptingEvidenceVerifier struct{}

func (acceptingEvidenceVerifier) VerifyAccountingRun(*Run) error { return nil }

type rejectingEvidenceVerifier struct{}

func (rejectingEvidenceVerifier) VerifyAccountingRun(*Run) error {
	return errors.New("unknown attestation")
}

type staticEvaluationClock struct{ now time.Time }

func (clock staticEvaluationClock) Now() time.Time { return clock.now }

func completedThroughDateClock(through time.Time) staticEvaluationClock {
	return staticEvaluationClock{now: through.AddDate(0, 0, 1).Add(12 * time.Hour)}
}

func equalRunAt(t *testing.T, asOf time.Time, synthetic bool) *Run {
	t.Helper()
	legacyInput := completeSnapshotInput(SourceLegacy)
	ledgerInput := completeSnapshotInput(SourceLedger)
	legacyInput.AsOf, ledgerInput.AsOf = asOf, asOf
	legacyInput.ObservedAt, ledgerInput.ObservedAt = asOf.Add(time.Second), asOf.Add(time.Second)
	legacyInput.Synthetic, ledgerInput.Synthetic = synthetic, synthetic
	legacyInput.EvidenceID = "legacy:" + asOf.Format(time.RFC3339Nano)
	ledgerInput.EvidenceID = "ledger:" + asOf.Format(time.RFC3339Nano)
	legacy, err := NewSnapshot(legacyInput)
	if err != nil {
		t.Fatal(err)
	}
	ledgerSnapshot, err := NewSnapshot(ledgerInput)
	if err != nil {
		t.Fatal(err)
	}
	run, err := Compare(ComparisonInput{Legacy: legacy, Ledger: ledgerSnapshot, Generator: "worker", GeneratedAt: asOf.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return run
}
