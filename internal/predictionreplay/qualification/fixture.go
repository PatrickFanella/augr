package qualification

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
)

var (
	OutcomeYes = uuid.MustParse("10000000-0000-4000-8000-000000000001")
	OutcomeNo  = uuid.MustParse("20000000-0000-4000-8000-000000000002")
)

func At(day, hour int) time.Time {
	return time.Date(2026, time.January, day, hour, 0, 0, 0, time.UTC)
}

func Build() (predictionreplay.Input, error) {
	books := []predictionreplay.BookInput{
		{MarketID: "market-1", OutcomeID: OutcomeYes, Venue: "kalshi", SourceKey: "book-yes-original", ContentSHA256: strings.Repeat("1", 64), ExchangeAt: At(2, 12), AvailableAt: At(2, 13), Bids: []predictionreplay.LevelInput{{Price: "0.4", Size: "10"}, {Price: "0.38", Size: "20"}}, Asks: []predictionreplay.LevelInput{{Price: "0.42", Size: "10"}, {Price: "0.45", Size: "10"}}},
		{MarketID: "market-1", OutcomeID: OutcomeYes, Venue: "kalshi", SourceKey: "book-yes-correction", ContentSHA256: strings.Repeat("2", 64), ExchangeAt: At(2, 12), AvailableAt: At(3, 13), Revision: 1, CorrectionOf: "book-yes-original", Bids: []predictionreplay.LevelInput{{Price: "0.39", Size: "8"}, {Price: "0.37", Size: "20"}}, Asks: []predictionreplay.LevelInput{{Price: "0.41", Size: "5"}, {Price: "0.44", Size: "10"}}},
		{MarketID: "market-1", OutcomeID: OutcomeNo, Venue: "kalshi", SourceKey: "book-no-original", ContentSHA256: strings.Repeat("3", 64), ExchangeAt: At(2, 12), AvailableAt: At(2, 13), Bids: []predictionreplay.LevelInput{{Price: "0.56", Size: "12"}, {Price: "0.54", Size: "20"}}, Asks: []predictionreplay.LevelInput{{Price: "0.58", Size: "10"}, {Price: "0.6", Size: "20"}}},
	}
	fees := []predictionreplay.FeePolicyInput{
		{InstrumentID: OutcomeYes, Venue: "kalshi", Role: predictionreplay.RoleTaker, SourceKey: "fee-yes-taker", ContentSHA256: strings.Repeat("a", 64), AvailableAt: At(1, 13), EffectiveFrom: At(1, 0), Formula: predictionreplay.FeeContractCurve, Rate: "0.07", Scale: 2, Rounding: predictionreplay.RoundCeiling},
		{InstrumentID: OutcomeYes, Venue: "kalshi", Role: predictionreplay.RoleMaker, SourceKey: "fee-yes-maker", ContentSHA256: strings.Repeat("b", 64), AvailableAt: At(1, 13), EffectiveFrom: At(1, 0), Formula: predictionreplay.FeeNotionalBPS, Rate: "0", Scale: 2, Rounding: predictionreplay.RoundHalfUp},
		{InstrumentID: OutcomeNo, Venue: "kalshi", Role: predictionreplay.RoleMaker, SourceKey: "fee-no-maker", ContentSHA256: strings.Repeat("c", 64), AvailableAt: At(1, 13), EffectiveFrom: At(1, 0), Formula: predictionreplay.FeeNotionalBPS, Rate: "25", Scale: 4, Rounding: predictionreplay.RoundHalfUp},
	}
	bookObservations := make([]dataset.ObservationInput, 0, len(books))
	for _, value := range books {
		revision := "r1"
		if value.Revision == 1 {
			revision = "r2"
		}
		bookObservations = append(bookObservations, dataset.ObservationInput{SourceKey: value.SourceKey, InstrumentID: value.OutcomeID, EffectiveAt: value.ExchangeAt, ObservedAt: value.AvailableAt, AvailableAt: value.AvailableAt, Revision: revision, CorrectionOf: value.CorrectionOf, ContentSHA256: value.ContentSHA256, Bid: &value.Bids[0].Price, Ask: &value.Asks[0].Price, Depth: &value.Bids[0].Size})
	}
	feeObservations := make([]dataset.ObservationInput, 0, len(fees))
	for _, value := range fees {
		feeObservations = append(feeObservations, dataset.ObservationInput{SourceKey: value.SourceKey, InstrumentID: value.InstrumentID, EffectiveAt: value.EffectiveFrom, ObservedAt: value.AvailableAt, AvailableAt: value.AvailableAt, Revision: "r1", ContentSHA256: value.ContentSHA256})
	}
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: At(10, 0), Partitions: []dataset.PartitionInput{
		{Kind: dataset.KindPredictionBooks, Provider: "kalshi", Source: "fixture-book-feed", Namespace: "prediction/books/qualification", RequestSHA256: strings.Repeat("d", 64), MediaType: "application/json", SymbologyVersion: "kalshi-market-v1", AdjustmentPolicy: "point_in_time", Timezone: "UTC", Calendar: "continuous", Revision: "r1", License: "synthetic", RetentionPolicy: "retain-qualification", Observations: bookObservations},
		{Kind: dataset.KindPredictionFees, Provider: "kalshi", Source: "fixture-fee-feed", Namespace: "prediction/fees/qualification", RequestSHA256: strings.Repeat("e", 64), MediaType: "application/json", SymbologyVersion: "kalshi-market-v1", AdjustmentPolicy: "point_in_time", Timezone: "UTC", Calendar: "continuous", Revision: "r1", License: "synthetic", RetentionPolicy: "retain-qualification", Observations: feeObservations},
	}})
	if err != nil {
		return predictionreplay.Input{}, err
	}
	return predictionreplay.Input{Manifest: manifest, Books: books, Fees: fees, Replays: []predictionreplay.ReplayInput{
		{DecisionAt: At(2, 14), MarketID: "market-1", OutcomeID: OutcomeYes, Side: predictionreplay.SideBuy, Role: predictionreplay.RoleTaker, Quantity: "15", LimitPrice: "0.46"},
		{DecisionAt: At(3, 14), MarketID: "market-1", OutcomeID: OutcomeYes, Side: predictionreplay.SideBuy, Role: predictionreplay.RoleTaker, Quantity: "20", LimitPrice: "0.44"},
		{DecisionAt: At(3, 14), MarketID: "market-1", OutcomeID: OutcomeNo, Side: predictionreplay.SideSell, Role: predictionreplay.RoleMaker, Quantity: "10", LimitPrice: "0.55"},
	}}, nil
}
