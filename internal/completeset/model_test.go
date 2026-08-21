package completeset

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
)

func candidateFixture(t *testing.T) Input {
	t.Helper()
	at := func(day, hour int) time.Time { return time.Date(2026, time.February, day, hour, 0, 0, 0, time.UTC) }
	outcomes := []uuid.UUID{
		uuid.MustParse("10000000-0000-4000-8000-000000000001"),
		uuid.MustParse("20000000-0000-4000-8000-000000000002"),
		uuid.MustParse("30000000-0000-4000-8000-000000000003"),
	}
	books := make([]predictionreplay.BookInput, 0, 3)
	fees := make([]predictionreplay.FeePolicyInput, 0, 3)
	bookObservations := make([]dataset.ObservationInput, 0, 3)
	feeObservations := make([]dataset.ObservationInput, 0, 3)
	replays := make([]predictionreplay.ReplayInput, 0, 6)
	for i, outcome := range outcomes {
		source := "book-" + outcome.String()
		digest := strings.Repeat(string(rune('1'+i)), 64)
		book := predictionreplay.BookInput{MarketID: "three-way", OutcomeID: outcome, Venue: "fixture", SourceKey: source, ContentSHA256: digest, ExchangeAt: at(2, 12), AvailableAt: at(2, 13), Bids: []predictionreplay.LevelInput{{Price: "0.29", Size: "10"}}, Asks: []predictionreplay.LevelInput{{Price: "0.3", Size: "10"}}}
		books = append(books, book)
		feeSource := "fee-" + outcome.String()
		feeDigest := strings.Repeat(string(rune('a'+i)), 64)
		fee := predictionreplay.FeePolicyInput{InstrumentID: outcome, Venue: "fixture", Role: predictionreplay.RoleTaker, SourceKey: feeSource, ContentSHA256: feeDigest, AvailableAt: at(1, 13), EffectiveFrom: at(1, 0), Formula: predictionreplay.FeeNotionalBPS, Rate: "0", Scale: 2, Rounding: predictionreplay.RoundHalfUp}
		fees = append(fees, fee)
		bookObservations = append(bookObservations, dataset.ObservationInput{SourceKey: source, InstrumentID: outcome, EffectiveAt: book.ExchangeAt, ObservedAt: book.AvailableAt, AvailableAt: book.AvailableAt, Revision: "r1", ContentSHA256: digest, Bid: &book.Bids[0].Price, Ask: &book.Asks[0].Price, Depth: &book.Bids[0].Size})
		feeObservations = append(feeObservations, dataset.ObservationInput{SourceKey: feeSource, InstrumentID: outcome, EffectiveAt: fee.EffectiveFrom, ObservedAt: fee.AvailableAt, AvailableAt: fee.AvailableAt, Revision: "r1", ContentSHA256: feeDigest})
		replays = append(replays,
			predictionreplay.ReplayInput{DecisionAt: at(2, 14), MarketID: "three-way", OutcomeID: outcome, Side: predictionreplay.SideBuy, Role: predictionreplay.RoleTaker, Quantity: "10", LimitPrice: "0.3"},
			predictionreplay.ReplayInput{DecisionAt: at(2, 14), MarketID: "three-way", OutcomeID: outcome, Side: predictionreplay.SideSell, Role: predictionreplay.RoleTaker, Quantity: "10", LimitPrice: "0.29"},
		)
	}
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: at(3, 0), Partitions: []dataset.PartitionInput{
		{Kind: dataset.KindPredictionBooks, Provider: "fixture", Source: "books", Namespace: "complete-set/books", RequestSHA256: strings.Repeat("d", 64), MediaType: "application/json", SymbologyVersion: "fixture-v1", AdjustmentPolicy: "point_in_time", Timezone: "UTC", Calendar: "continuous", Revision: "r1", License: "synthetic", RetentionPolicy: "retain", Observations: bookObservations},
		{Kind: dataset.KindPredictionFees, Provider: "fixture", Source: "fees", Namespace: "complete-set/fees", RequestSHA256: strings.Repeat("e", 64), MediaType: "application/json", SymbologyVersion: "fixture-v1", AdjustmentPolicy: "point_in_time", Timezone: "UTC", Calendar: "continuous", Revision: "r1", License: "synthetic", RetentionPolicy: "retain", Observations: feeObservations},
	}})
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := predictionreplay.NewRecorder(predictionreplay.Input{Manifest: manifest, Books: books, Fees: fees, Replays: replays})
	if err != nil {
		t.Fatal(err)
	}
	return Input{Recorder: recorder, CandidateKey: "qualified-1", MarketID: "three-way", Outcomes: outcomes, Legs: []LegBinding{{outcomes[0], 0, 1}, {outcomes[1], 2, 3}, {outcomes[2], 4, 5}}, SetQuantity: "10", PayoutPerSet: "1", AvailableCapital: "10", MinimumProfit: "0.5"}
}

func TestCandidateQualifiesOnlyAfterWorstOrphanGuard(t *testing.T) {
	input := candidateFixture(t)
	candidate, err := NewCandidate(input)
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Qualified() || candidate.LegCount() != 3 || candidate.ScenarioCount() != 6 {
		t.Fatalf("candidate=%t/%s counts=%d/%d", candidate.Qualified(), candidate.Reason(), candidate.LegCount(), candidate.ScenarioCount())
	}
	var canonical candidateCanonical
	if err := json.Unmarshal(candidate.CanonicalBytes(), &canonical); err != nil {
		t.Fatal(err)
	}
	if canonical.EntryCost != "9" || canonical.Payout != "10" || canonical.AfterCostProfit != "1" || canonical.WorstOrphanLoss != "0.2" || canonical.ReservedCapital != "9.2" || canonical.ProfitAfterOrphanGuard != "0.8" {
		t.Fatalf("economics=%+v", canonical)
	}
	if canonical.WorstOrphanKey != input.Outcomes[0].String()+"+"+input.Outcomes[1].String() {
		t.Fatalf("worst=%s", canonical.WorstOrphanKey)
	}
	reloaded, err := FromCanonical(candidate.ID(), candidate.Digest(), candidate.CanonicalBytes(), input.Recorder)
	if err != nil || reloaded.Digest() != candidate.Digest() {
		t.Fatalf("reload=%v/%v", reloaded, err)
	}
}

func TestCandidateRejectsCapitalProfitAndOrphanBoundaries(t *testing.T) {
	input := candidateFixture(t)
	input.AvailableCapital = "9.19"
	value, err := NewCandidate(input)
	if err != nil || value.Reason() != "insufficient_capital" {
		t.Fatalf("capital=%v/%v", value, err)
	}
	input = candidateFixture(t)
	input.MinimumProfit = "1"
	value, err = NewCandidate(input)
	if err != nil || value.Reason() != "nonpositive_complete_set_profit" {
		t.Fatalf("profit=%v/%v", value, err)
	}
	input = candidateFixture(t)
	input.MinimumProfit = "0.8"
	value, err = NewCandidate(input)
	if err != nil || value.Reason() != "orphan_guard_failure" {
		t.Fatalf("orphan=%v/%v", value, err)
	}
}

func TestCandidateRetainsIncompleteAndInvalidReplayRejections(t *testing.T) {
	input := candidateFixture(t)
	input.Legs = input.Legs[:2]
	value, err := NewCandidate(input)
	if err != nil || value.Reason() != "incomplete_set" {
		t.Fatalf("incomplete=%v/%v", value, err)
	}
	if _, err = FromCanonical(value.ID(), value.Digest(), value.CanonicalBytes(), input.Recorder); err != nil {
		t.Fatal(err)
	}
	input = candidateFixture(t)
	input.Legs[0].EntrySequence = 1
	input.Legs[0].UnwindSequence = 0
	value, err = NewCandidate(input)
	if err != nil || value.Reason() != "invalid_replay" {
		t.Fatalf("invalid=%v/%v", value, err)
	}
}

func TestCandidateInputPermutationIsCanonical(t *testing.T) {
	input := candidateFixture(t)
	first, err := NewCandidate(input)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(input.Outcomes)
	slices.Reverse(input.Legs)
	second, err := NewCandidate(input)
	if err != nil || first.Digest() != second.Digest() {
		t.Fatalf("digests=%s/%s err=%v", first.Digest(), second.Digest(), err)
	}
}
