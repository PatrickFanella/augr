package simulation

import (
	"bytes"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestSimulationOutcomeProjectsOrderedEconomicsAndStableHash(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("outcome", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)},
			{Price: decimal.RequireFromString("10.30"), Size: decimal.NewFromInt(6)},
		},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := NewOutcome(OutcomeInput{
		Account: fixture.account, VenueContract: fixture.contract, Aggregate: result.Aggregate, Fills: result.Fills,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantGross := decimal.RequireFromString("-102.84")
	wantFee := result.Fills[0].Fee.Add(*result.Fills[1].Fee)
	if outcome.PolicyVersion != fixture.policy.Version() || outcome.FinalState != lifecycle.StateFilled ||
		outcome.Environment != domain.AccountEnvironmentPaperScored || outcome.EvidenceClass != fixture.account.EvidenceClass ||
		outcome.StorageNamespace != fixture.account.StorageNamespace || !outcome.TotalQuantity.Equal(decimal.NewFromInt(10)) ||
		!outcome.GrossCash.Equal(wantGross) || !outcome.TotalFee.Equal(wantFee) || len(outcome.Fills) != 2 || len(outcome.Hash()) != 64 {
		t.Fatalf("outcome = %+v", outcome)
	}
	bytesCopy := outcome.CanonicalBytes()
	bytesCopy[0] = '['
	if bytes.Equal(bytesCopy, outcome.CanonicalBytes()) {
		t.Fatal("CanonicalBytes exposed mutable outcome storage")
	}
}

func TestSimulationOutcomeHashExcludesOpaqueIDsAndCreationTimes(t *testing.T) {
	firstFixture := newVenueFixture(t, nil)
	secondFixture := newVenueFixture(t, nil)
	build := func(fixture venueFixture, label string) *Outcome {
		t.Helper()
		snapshot := fixture.snapshot(label, fixture.routeAt.Add(time.Second),
			[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
			[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
		)
		result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := NewOutcome(OutcomeInput{Account: fixture.account, VenueContract: fixture.contract, Aggregate: result.Aggregate, Fills: result.Fills})
		if err != nil {
			t.Fatal(err)
		}
		return outcome
	}
	first := build(firstFixture, "opaque-a")
	second := build(secondFixture, "opaque-b")
	if firstFixture.account.ID == secondFixture.account.ID || firstFixture.aggregate.Order.ID == secondFixture.aggregate.Order.ID {
		t.Fatal("fixture opaque identities unexpectedly match")
	}
	if first.Hash() != second.Hash() || !bytes.Equal(first.CanonicalBytes(), second.CanonicalBytes()) {
		t.Fatalf("opaque identities changed outcome hash: %s/%s", first.Hash(), second.Hash())
	}
}

func TestSimulationOutcomeHashSeparatesScoredAndStressEvidence(t *testing.T) {
	build := func(environment domain.AccountEnvironment) *Outcome {
		t.Helper()
		fixture := newVenueFixture(t, func(config *venueFixtureConfig) { config.accountEnvironment = environment })
		snapshot := fixture.snapshot("classification", fixture.routeAt.Add(time.Second),
			[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
			[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
		)
		result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := NewOutcome(OutcomeInput{Account: fixture.account, VenueContract: fixture.contract, Aggregate: result.Aggregate, Fills: result.Fills})
		if err != nil {
			t.Fatal(err)
		}
		return outcome
	}
	scored := build(domain.AccountEnvironmentPaperScored)
	stress := build(domain.AccountEnvironmentPaperStress)
	if scored.Hash() == stress.Hash() || scored.EvidenceClass == stress.EvidenceClass || scored.StorageNamespace == stress.StorageNamespace {
		t.Fatalf("scored/stress outcome classification converged: %s", scored.Hash())
	}
}
