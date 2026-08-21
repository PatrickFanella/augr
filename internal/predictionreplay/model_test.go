package predictionreplay

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
)

func recorderFixture(t *testing.T) Input {
	t.Helper()
	at := func(day, hour int) time.Time { return time.Date(2026, time.January, day, hour, 0, 0, 0, time.UTC) }
	outcomeYes := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	outcomeNo := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	books := []BookInput{
		{MarketID: "market-1", OutcomeID: outcomeYes, Venue: "kalshi", SourceKey: "book-yes-original", ContentSHA256: strings.Repeat("1", 64), ExchangeAt: at(2, 12), AvailableAt: at(2, 13), Bids: []LevelInput{{"0.4", "10"}, {"0.38", "20"}}, Asks: []LevelInput{{"0.42", "10"}, {"0.45", "10"}}},
		{MarketID: "market-1", OutcomeID: outcomeYes, Venue: "kalshi", SourceKey: "book-yes-correction", ContentSHA256: strings.Repeat("2", 64), ExchangeAt: at(2, 12), AvailableAt: at(3, 13), Revision: 1, CorrectionOf: "book-yes-original", Bids: []LevelInput{{"0.39", "8"}, {"0.37", "20"}}, Asks: []LevelInput{{"0.41", "5"}, {"0.44", "10"}}},
		{MarketID: "market-1", OutcomeID: outcomeNo, Venue: "kalshi", SourceKey: "book-no-original", ContentSHA256: strings.Repeat("3", 64), ExchangeAt: at(2, 12), AvailableAt: at(2, 13), Bids: []LevelInput{{"0.56", "12"}, {"0.54", "20"}}, Asks: []LevelInput{{"0.58", "10"}, {"0.6", "20"}}},
	}
	fees := []FeePolicyInput{
		{InstrumentID: outcomeYes, Venue: "kalshi", Role: RoleTaker, SourceKey: "fee-yes-taker", ContentSHA256: strings.Repeat("a", 64), AvailableAt: at(1, 13), EffectiveFrom: at(1, 0), Formula: FeeContractCurve, Rate: "0.07", Scale: 2, Rounding: RoundCeiling},
		{InstrumentID: outcomeYes, Venue: "kalshi", Role: RoleMaker, SourceKey: "fee-yes-maker", ContentSHA256: strings.Repeat("b", 64), AvailableAt: at(1, 13), EffectiveFrom: at(1, 0), Formula: FeeNotionalBPS, Rate: "0", Scale: 2, Rounding: RoundHalfUp},
		{InstrumentID: outcomeNo, Venue: "kalshi", Role: RoleMaker, SourceKey: "fee-no-maker", ContentSHA256: strings.Repeat("c", 64), AvailableAt: at(1, 13), EffectiveFrom: at(1, 0), Formula: FeeNotionalBPS, Rate: "25", Scale: 4, Rounding: RoundHalfUp},
	}
	bookObservations := make([]dataset.ObservationInput, 0, len(books))
	for _, value := range books {
		bookObservations = append(bookObservations, dataset.ObservationInput{SourceKey: value.SourceKey, InstrumentID: value.OutcomeID, EffectiveAt: value.ExchangeAt, ObservedAt: value.AvailableAt, AvailableAt: value.AvailableAt, Revision: "r" + string(rune('1'+value.Revision)), CorrectionOf: value.CorrectionOf, ContentSHA256: value.ContentSHA256, Bid: &value.Bids[0].Price, Ask: &value.Asks[0].Price, Depth: &value.Bids[0].Size})
	}
	feeObservations := make([]dataset.ObservationInput, 0, len(fees))
	for _, value := range fees {
		feeObservations = append(feeObservations, dataset.ObservationInput{SourceKey: value.SourceKey, InstrumentID: value.InstrumentID, EffectiveAt: value.EffectiveFrom, ObservedAt: value.AvailableAt, AvailableAt: value.AvailableAt, Revision: "r1", ContentSHA256: value.ContentSHA256})
	}
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: at(10, 0), Partitions: []dataset.PartitionInput{
		{Kind: dataset.KindPredictionBooks, Provider: "kalshi", Source: "fixture-book-feed", Namespace: "prediction/books/qualification", RequestSHA256: strings.Repeat("d", 64), MediaType: "application/json", SymbologyVersion: "kalshi-market-v1", AdjustmentPolicy: "point_in_time", Timezone: "UTC", Calendar: "continuous", Revision: "r1", License: "synthetic", RetentionPolicy: "retain-qualification", Observations: bookObservations},
		{Kind: dataset.KindPredictionFees, Provider: "kalshi", Source: "fixture-fee-feed", Namespace: "prediction/fees/qualification", RequestSHA256: strings.Repeat("e", 64), MediaType: "application/json", SymbologyVersion: "kalshi-market-v1", AdjustmentPolicy: "point_in_time", Timezone: "UTC", Calendar: "continuous", Revision: "r1", License: "synthetic", RetentionPolicy: "retain-qualification", Observations: feeObservations},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return Input{Manifest: manifest, Books: books, Fees: fees, Replays: []ReplayInput{
		{DecisionAt: at(2, 14), MarketID: "market-1", OutcomeID: outcomeYes, Side: SideBuy, Role: RoleTaker, Quantity: "15", LimitPrice: "0.46"},
		{DecisionAt: at(3, 14), MarketID: "market-1", OutcomeID: outcomeYes, Side: SideBuy, Role: RoleTaker, Quantity: "20", LimitPrice: "0.44"},
		{DecisionAt: at(3, 14), MarketID: "market-1", OutcomeID: outcomeNo, Side: SideSell, Role: RoleMaker, Quantity: "10", LimitPrice: "0.55"},
	}}
}

func TestRecorderUsesDecisionAvailableDepthAndExactFees(t *testing.T) {
	input := recorderFixture(t)
	recorder, err := NewRecorder(input)
	if err != nil {
		t.Fatal(err)
	}
	if recorder.BookCount() != 3 || recorder.FeeCount() != 3 || recorder.ReplayCount() != 3 {
		t.Fatalf("counts=%d/%d/%d", recorder.BookCount(), recorder.FeeCount(), recorder.ReplayCount())
	}
	var canonical struct {
		Replays []replayCanonical `json:"replays"`
	}
	if err := json.Unmarshal(recorder.CanonicalBytes(), &canonical); err != nil {
		t.Fatal(err)
	}
	var before, after, sell replayCanonical
	for _, value := range canonical.Replays {
		switch {
		case value.OutcomeID == input.Books[0].OutcomeID.String() && value.DecisionAt == formatTime(input.Replays[0].DecisionAt):
			before = value
		case value.OutcomeID == input.Books[0].OutcomeID.String():
			after = value
		default:
			sell = value
		}
	}
	if before.BookSourceKey != "book-yes-original" || before.Status != "filled" || before.FilledQuantity != "15" || before.GrossCash != "6.45" || before.Fee != "0.26" || before.NetCash != "6.71" {
		t.Fatalf("before=%+v", before)
	}
	if after.BookSourceKey != "book-yes-correction" || after.Status != "partial" || after.FilledQuantity != "15" || after.ResidualQuantity != "5" || after.GrossCash != "6.45" || after.Fee != "0.26" {
		t.Fatalf("after=%+v", after)
	}
	if sell.Status != "filled" || sell.FilledQuantity != "10" || sell.Fee != "0.014" || sell.NetCash != "5.586" {
		t.Fatalf("sell=%+v", sell)
	}
	reloaded, err := FromCanonical(recorder.ID(), recorder.Digest(), recorder.CanonicalBytes(), input.Manifest)
	if err != nil || reloaded.Digest() != recorder.Digest() {
		t.Fatalf("reload=%v err=%v", reloaded, err)
	}
}

func TestRecorderRetainsNoFeeAndLimitBlockedWithoutSyntheticFill(t *testing.T) {
	input := recorderFixture(t)
	input.Replays = []ReplayInput{
		{DecisionAt: time.Date(2026, time.January, 3, 14, 0, 0, 0, time.UTC), MarketID: "market-1", OutcomeID: input.Books[2].OutcomeID, Side: SideBuy, Role: RoleTaker, Quantity: "1", LimitPrice: "0.9"},
		{DecisionAt: time.Date(2026, time.January, 3, 14, 0, 0, 0, time.UTC), MarketID: "market-1", OutcomeID: input.Books[0].OutcomeID, Side: SideBuy, Role: RoleMaker, Quantity: "1", LimitPrice: "0.4"},
	}
	recorder, err := NewRecorder(input)
	if err != nil {
		t.Fatal(err)
	}
	var canonical struct {
		Replays []replayCanonical `json:"replays"`
	}
	if err := json.Unmarshal(recorder.CanonicalBytes(), &canonical); err != nil {
		t.Fatal(err)
	}
	statuses := map[string]replayCanonical{}
	for _, value := range canonical.Replays {
		statuses[value.Status] = value
	}
	if statuses["no_fee_policy"].FilledQuantity != "0" || len(statuses["no_fee_policy"].Fills) != 0 ||
		statuses["limit_blocked"].FilledQuantity != "0" || len(statuses["limit_blocked"].Fills) != 0 {
		t.Fatalf("rejections=%+v", canonical.Replays)
	}
}

func TestRecorderInputPermutationIsCanonical(t *testing.T) {
	input := recorderFixture(t)
	first, err := NewRecorder(input)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(input.Books)
	slices.Reverse(input.Fees)
	slices.Reverse(input.Replays)
	second, err := NewRecorder(input)
	if err != nil || first.Digest() != second.Digest() {
		t.Fatalf("digests=%s/%s err=%v", first.Digest(), second.Digest(), err)
	}
}

func TestRecorderRejectsUnmanifestedCrossedAndInvalidCorrection(t *testing.T) {
	input := recorderFixture(t)
	input.Books[0].ContentSHA256 = strings.Repeat("0", 64)
	if _, err := NewRecorder(input); err == nil {
		t.Fatal("unmanifested book accepted")
	}
	input = recorderFixture(t)
	input.Books[0].Asks[0].Price = "0.39"
	if _, err := NewRecorder(input); err == nil {
		t.Fatal("crossed book accepted")
	}
	input = recorderFixture(t)
	input.Books[1].AvailableAt = input.Books[0].AvailableAt
	if _, err := NewRecorder(input); err == nil {
		t.Fatal("same-time correction accepted")
	}
}

func TestRecorderExposesDetachedPointInTimeSimulationView(t *testing.T) {
	input := recorderFixture(t)
	recorder, err := NewRecorder(input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := recorder.BookAt(time.Date(2026, time.January, 2, 14, 0, 0, 0, time.UTC), "market-1", input.Books[0].OutcomeID)
	if err != nil || before.SourceKey != "book-yes-original" || before.Bids[0].Price != "0.4" || before.Asks[0].Price != "0.42" {
		t.Fatalf("before=%+v err=%v", before, err)
	}
	after, err := recorder.BookAt(time.Date(2026, time.January, 3, 14, 0, 0, 0, time.UTC), "market-1", input.Books[0].OutcomeID)
	if err != nil || after.SourceKey != "book-yes-correction" || after.Bids[0].Price != "0.39" || after.Revision != 1 {
		t.Fatalf("after=%+v err=%v", after, err)
	}
	before.Bids[0].Price = "0.01"
	again, err := recorder.BookAt(time.Date(2026, time.January, 2, 14, 0, 0, 0, time.UTC), "market-1", input.Books[0].OutcomeID)
	if err != nil || again.Bids[0].Price != "0.4" {
		t.Fatalf("detached=%+v err=%v", again, err)
	}
	fee, err := recorder.MakerFeeAt(time.Date(2026, time.January, 3, 14, 0, 0, 0, time.UTC), input.Books[2].OutcomeID, "KALSHI", "0.56", "10")
	if err != nil || fee.SourceKey != "fee-no-maker" || fee.Amount != "0.014" {
		t.Fatalf("fee=%+v err=%v", fee, err)
	}
	if _, err = recorder.BookAt(time.Date(2026, time.January, 11, 0, 0, 0, 0, time.UTC), "market-1", input.Books[0].OutcomeID); err == nil {
		t.Fatal("post-cutoff book query accepted")
	}
}
