package accountingrecon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestLegacyPaperSourceRetainsFloatProvenanceSignedPositionsAndMissingMetrics(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	instrumentID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	reader := legacyReaderStub{capture: LegacyCapture{
		Balance:    LegacyBalance{Currency: "USD", Cash: 99.9, BuyingPower: 199.8, Equity: 105.4},
		Positions:  []domain.Position{{Ticker: "AAPL", Side: domain.PositionSideShort, Quantity: 2.5}},
		CapturedAt: asOf.Add(time.Second),
	}}
	resolver := legacyResolverStub{id: instrumentID}
	source := NewLegacyPaperSource(reader, resolver, false)
	snapshot, err := source.Capture(context.Background(), SourceRequest{
		AccountID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), AsOf: asOf,
		ProjectionVersion: "ledger_fifo_v1", MarkSource: "polygon", MarkNamespace: "quotes/scored", MaxMarkAge: 5 * time.Minute,
	}, staticLease{accountID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), asOf: asOf, acquiredAt: asOf, id: "fence:test", epoch: 7, active: true, verified: true})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if len(snapshot.Positions) != 1 || !snapshot.Positions[0].Quantity.Equal(mustDecimal(t, "-2.5")) || snapshot.Positions[0].Provenance != ProvenanceBinaryFloat {
		t.Fatalf("positions = %+v", snapshot.Positions)
	}
	for _, kind := range []MetricKind{MetricFees, MetricRealizedPnL, MetricUnrealizedPnL} {
		if _, ok := missingIndex(snapshot)[MetricFactKey(kind)]; !ok {
			t.Fatalf("missing coverage for %s not retained", kind)
		}
	}
	if got := metricIndex(snapshot)[MetricCash]; got.Provenance != ProvenanceBinaryFloat || got.Value.String() != "99.9" {
		t.Fatalf("cash = %+v", got)
	}
	if snapshot.CaptureFenceID != "fence:test" || snapshot.CaptureEpoch != 7 || !snapshot.PositionCoverageComplete {
		t.Fatalf("capture boundary = %+v", snapshot)
	}
}

func TestLegacyPaperSourceRetainsUnresolvedPositionAsCoverageGap(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	accountID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	source := NewLegacyPaperSource(legacyReaderStub{capture: LegacyCapture{
		Balance:   LegacyBalance{Currency: "USD", Cash: 1, BuyingPower: 1, Equity: 1},
		Positions: []domain.Position{{Ticker: "UNKNOWN", Side: domain.PositionSideLong, Quantity: 1}}, CapturedAt: asOf.Add(time.Second),
	}}, legacyResolverStub{err: errors.New("ambiguous")}, false)
	snapshot, err := source.Capture(context.Background(), SourceRequest{AccountID: accountID, AsOf: asOf, ProjectionVersion: "ledger_fifo_v1", MarkSource: "polygon", MarkNamespace: "quotes/scored", MaxMarkAge: time.Minute}, staticLease{accountID: accountID, asOf: asOf, acquiredAt: asOf, id: "fence:test", epoch: 1, active: true, verified: true})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PositionCoverageComplete || len(snapshot.Missing) == 0 {
		t.Fatalf("unresolved position coverage = %+v", snapshot.Missing)
	}
}

type legacyReaderStub struct {
	capture LegacyCapture
	err     error
}

func (reader legacyReaderStub) CaptureLegacyAccounting(context.Context) (LegacyCapture, error) {
	return reader.capture, reader.err
}

type legacyResolverStub struct {
	id  uuid.UUID
	err error
}

func (resolver legacyResolverStub) ResolveLegacyPosition(context.Context, domain.Position, time.Time) (uuid.UUID, error) {
	return resolver.id, resolver.err
}

func mustDecimal(t *testing.T, value string) decimal.Decimal {
	t.Helper()
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
