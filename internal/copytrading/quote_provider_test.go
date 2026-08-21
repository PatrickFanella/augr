package copytrading

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type copyInstrumentResolver struct {
	repository.InstrumentRepository
	value *instrument.Instrument
	err   error
}

func (r copyInstrumentResolver) ResolveAlias(context.Context, string, instrument.AliasType, string, time.Time) (*instrument.Instrument, error) {
	return r.value, r.err
}

type copyQuoteSelector struct {
	repository.QuoteSnapshotRepository
	value    *marketdata.QuoteSnapshot
	err      error
	selector marketdata.QuoteSelector
}

func (r *copyQuoteSelector) LatestQuoteSnapshotAt(_ context.Context, selector marketdata.QuoteSelector) (*marketdata.QuoteSnapshot, error) {
	r.selector = selector
	return r.value, r.err
}

func TestCanonicalQuoteProviderSelectsPointInTimeEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC)
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: "equity:aapl", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "nasdaq", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementCash, Status: instrument.StatusActive, Metadata: json.RawMessage(`{}`), CreatedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	available, exchange := now.Add(-time.Second), now.Add(-2*time.Second)
	bid, ask := decimal.RequireFromString("99.99"), decimal.RequireFromString("100.01")
	quote, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{InstrumentID: reference.ID, Provider: "alpaca", Venue: "nasdaq", Source: "sip", ObservationNamespace: "stocks", ObservationID: "quote-1", ExchangeAt: &exchange, ReceivedAt: exchange.Add(time.Millisecond), AvailableAt: &available, Bid: &bid, Ask: &ask, MarketStatus: "open", SessionStatus: "regular", Metadata: json.RawMessage(`{}`), CreatedAt: available})
	if err != nil {
		t.Fatal(err)
	}
	quotes := &copyQuoteSelector{value: quote}
	provider := CanonicalQuoteProvider{Instruments: copyInstrumentResolver{value: reference}, Quotes: quotes, AliasProvider: "alpaca", QuoteProvider: "alpaca", Venue: "nasdaq", ObservationNamespace: "stocks"}
	values, err := provider.Snapshots(context.Background(), []string{" aapl ", "AAPL"}, now)
	if err != nil {
		t.Fatal(err)
	}
	value := values["AAPL"]
	if value.QuoteSnapshotID != quote.ID || value.Bid != "99.99" || value.Ask != "100.01" || value.AvailableAt == nil || !value.AvailableAt.Equal(available) || quotes.selector.AsOf != now {
		t.Fatalf("value=%+v selector=%+v", value, quotes.selector)
	}

	missing := provider
	missing.Instruments = copyInstrumentResolver{err: repository.ErrNotFound}
	values, err = missing.Snapshots(context.Background(), []string{"AAPL"}, now)
	if err != nil || values["AAPL"].QuoteSnapshotID != uuid.Nil {
		t.Fatalf("missing=%+v/%v", values, err)
	}
}
