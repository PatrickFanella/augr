package qualification

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/completeset"
	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
)

type Fixture struct {
	RecorderInput predictionreplay.Input
	Outcomes      []uuid.UUID
	Legs          []completeset.LegBinding
}

func (f Fixture) CandidateInput(recorder *predictionreplay.Recorder, key, capital, minimum string) completeset.Input {
	return completeset.Input{Recorder: recorder, CandidateKey: key, MarketID: "three-way", Outcomes: append([]uuid.UUID(nil), f.Outcomes...), Legs: append([]completeset.LegBinding(nil), f.Legs...), SetQuantity: "10", PayoutPerSet: "1", AvailableCapital: capital, MinimumProfit: minimum}
}

func Build() (Fixture, error) {
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
		source := "complete-book-" + outcome.String()
		digest := strings.Repeat(string(rune('1'+i)), 64)
		book := predictionreplay.BookInput{MarketID: "three-way", OutcomeID: outcome, Venue: "fixture", SourceKey: source, ContentSHA256: digest, ExchangeAt: at(2, 12), AvailableAt: at(2, 13), Bids: []predictionreplay.LevelInput{{Price: "0.29", Size: "10"}}, Asks: []predictionreplay.LevelInput{{Price: "0.3", Size: "10"}}}
		books = append(books, book)
		feeSource := "complete-fee-" + outcome.String()
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
		return Fixture{}, err
	}
	return Fixture{RecorderInput: predictionreplay.Input{Manifest: manifest, Books: books, Fees: fees, Replays: replays}, Outcomes: outcomes, Legs: []completeset.LegBinding{{OutcomeID: outcomes[0], EntrySequence: 0, UnwindSequence: 1}, {OutcomeID: outcomes[1], EntrySequence: 2, UnwindSequence: 3}, {OutcomeID: outcomes[2], EntrySequence: 4, UnwindSequence: 5}}}, nil
}
