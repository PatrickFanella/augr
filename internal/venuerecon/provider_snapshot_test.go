package venuerecon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

var (
	testHorizonStart = time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	testHorizonEnd   = time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	testInstrumentID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	testContractID   = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

type fixedResolver struct {
	err error
}

func (resolver fixedResolver) ResolveVenueContract(_ context.Context, provider venue.Provider, contractID string, at time.Time) (instrument.VenueContract, error) {
	if resolver.err != nil {
		return instrument.VenueContract{}, resolver.err
	}
	if contractID != "AAPL" || at.Before(testHorizonStart) || at.After(testHorizonEnd.Add(24*time.Hour)) {
		return instrument.VenueContract{}, errors.New("missing contract")
	}
	return instrument.VenueContract{
		ID: testContractID, InstrumentID: testInstrumentID,
		Venue: string(provider), ContractID: contractID, Currency: "USD",
	}, nil
}

func validCaptureInput(provider venue.Provider) CaptureInput {
	namespace := "alpaca/account-activities/FILL"
	if provider == venue.ProviderKalshi {
		namespace = "kalshi/portfolio/fills"
	}
	input := CaptureInput{
		Provider: provider, Namespace: namespace, AccountID: "acct-1", Currency: "USD",
		HorizonStart: testHorizonStart, HorizonEnd: testHorizonEnd,
		CaptureStart: testHorizonEnd.Add(time.Minute), CaptureEnd: testHorizonEnd.Add(2 * time.Minute),
		ProviderAsOf: testHorizonEnd, Cash: "100.00", Equity: "125.00",
		Positions: []PositionInput{{ContractID: "AAPL", Quantity: "2.000", Currency: "USD", SourceAt: testHorizonEnd}},
		Fills: []FillInput{{
			SourceID: "fill-1", ObservationClass: lifecycle.ObservationOrdinary,
			ExternalOrderID: "external-1", ClientOrderID: "client-1", ContractID: "AAPL", Side: lifecycle.SideBuy,
			Quantity: "2.0", Price: "10.500", Fee: "0.10", Currency: "USD", SourceRevision: "1", SourceAt: testHorizonEnd,
		}},
	}
	setFixturePages(&input)
	return input
}

func setFixturePages(input *CaptureInput) {
	header := providerFixturePage{
		AccountID: input.AccountID, Currency: input.Currency,
		ProviderAsOf: canonicalTime(input.ProviderAsOf), Cash: input.Cash, Equity: input.Equity,
	}
	first := header
	for _, row := range input.Positions {
		first.Positions = append(first.Positions, wirePosition{
			ContractID: row.ContractID, Quantity: row.Quantity,
			Currency: row.Currency, SourceAt: canonicalTime(row.SourceAt),
		})
	}
	second := header
	for _, row := range input.Fills {
		second.Fills = append(second.Fills, wireFill{
			SourceID: row.SourceID, OriginalSourceID: row.OriginalSourceID,
			ObservationClass: string(row.ObservationClass), ObservationDiscriminator: row.ObservationDiscriminator,
			ExternalOrderID: row.ExternalOrderID, ClientOrderID: row.ClientOrderID, ContractID: row.ContractID,
			Side: string(row.Side), Quantity: row.Quantity, Price: row.Price, Fee: row.Fee, Currency: row.Currency,
			SourceRevision: row.SourceRevision, SourceAt: canonicalTime(row.SourceAt),
		})
	}
	firstRaw, _ := json.Marshal(first)
	secondRaw, _ := json.Marshal(second)
	input.Pages = []RawPage{{Raw: firstRaw, NextCursor: "cursor-2"}, {Cursor: "cursor-2", Terminal: true, Raw: secondRaw}}
}

func normalizeTestCapture(input CaptureInput) (*ProviderCapture, error) {
	if input.Provider == venue.ProviderAlpaca {
		return NormalizeAlpacaCapture(context.Background(), input, fixedResolver{})
	}
	return NormalizeKalshiCapture(context.Background(), input, fixedResolver{})
}

func TestAlpacaAndKalshiNormalizeCanonicalProviderCapture(t *testing.T) {
	t.Parallel()
	for _, provider := range []venue.Provider{venue.ProviderAlpaca, venue.ProviderKalshi} {
		provider := provider
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()
			input := validCaptureInput(provider)
			var capture *ProviderCapture
			var err error
			if provider == venue.ProviderAlpaca {
				capture, err = NormalizeAlpacaCapture(context.Background(), input, fixedResolver{})
			} else {
				capture, err = NormalizeKalshiCapture(context.Background(), input, fixedResolver{})
			}
			if err != nil {
				t.Fatal(err)
			}
			if capture.ID() == uuid.Nil || capture.Digest() == "" || len(capture.CanonicalBytes()) == 0 {
				t.Fatalf("capture identity incomplete: %+v", capture)
			}
			if got := capture.Positions(); len(got) != 1 || got[0].InstrumentID != testInstrumentID || got[0].Quantity != "2" {
				t.Fatalf("positions = %+v", got)
			}
			if got := capture.Fills(); len(got) != 1 || got[0].SourceID != "fill-1" || got[0].Price != "10.5" || got[0].Fee != "0.1" {
				t.Fatalf("fills = %+v", got)
			}
			pages := capture.Pages()
			pages[0].Raw[0] = 'x'
			if bytes.Equal(pages[0].Raw, capture.Pages()[0].Raw) {
				t.Fatal("page getter exposed raw byte storage")
			}
			second, err := normalizeTestCapture(input)
			if err != nil || !sameCapturePayload(capture, second) {
				t.Fatalf("deterministic replay failed: %v", err)
			}
		})
	}
}

func TestAlpacaRevisionUsesOriginalFillIdentity(t *testing.T) {
	t.Parallel()
	input := validCaptureInput(venue.ProviderAlpaca)
	input.Fills = append(input.Fills, FillInput{
		SourceID: "revision-event-9", OriginalSourceID: "fill-1",
		ObservationClass: lifecycle.ObservationCorrection, ObservationDiscriminator: "corr-1",
		ExternalOrderID: "external-1", ClientOrderID: "client-1", ContractID: "AAPL", Side: lifecycle.SideBuy,
		Quantity: "2", Price: "10.25", Fee: "0.1", Currency: "USD", SourceRevision: "2", SourceAt: testHorizonEnd,
	})
	setFixturePages(&input)
	capture, err := NormalizeAlpacaCapture(context.Background(), input, fixedResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if fills := capture.Fills(); len(fills) != 2 || fills[1].OriginalSourceID != "fill-1" || fills[1].SourceID != "revision-event-9" {
		t.Fatalf("revision evidence = %+v", fills)
	}
	input.Provider = venue.ProviderKalshi
	input.Namespace = "kalshi/portfolio/fills"
	if _, err := NormalizeKalshiCapture(context.Background(), input, fixedResolver{}); err == nil {
		t.Fatal("Kalshi accepted unsupported revision evidence")
	}
}

func TestProviderCaptureAllowsZeroPriceAndOutOfHorizonRevisionOriginal(t *testing.T) {
	t.Parallel()
	input := validCaptureInput(venue.ProviderAlpaca)
	input.Fills = []FillInput{{
		SourceID: "revision-event-9", OriginalSourceID: "older-fill",
		ObservationClass: lifecycle.ObservationBust, ObservationDiscriminator: "bust-1",
		ExternalOrderID: "external-1", ClientOrderID: "client-1", ContractID: "AAPL", Side: lifecycle.SideBuy,
		Quantity: "2", Price: "0", Fee: "0", Currency: "USD", SourceRevision: "2", SourceAt: testHorizonEnd,
	}}
	setFixturePages(&input)
	capture, err := NormalizeAlpacaCapture(context.Background(), input, fixedResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if fills := capture.Fills(); len(fills) != 1 || fills[0].Price != "0" || fills[0].OriginalSourceID != "older-fill" {
		t.Fatalf("revision evidence = %+v", fills)
	}
}

func TestProviderCaptureRejectsIncompleteOrContradictoryEvidence(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*CaptureInput){
		"namespace":          func(v *CaptureInput) { v.Namespace = "wrong" },
		"account":            func(v *CaptureInput) { v.AccountID = " " },
		"currency":           func(v *CaptureInput) { v.Currency = "usd" },
		"cash":               func(v *CaptureInput) { v.Cash = "1e2"; setFixturePages(v) },
		"page bytes":         func(v *CaptureInput) { v.Pages[0].Raw = []byte(`not-json`) },
		"page chain":         func(v *CaptureInput) { v.Pages[0].NextCursor = "wrong" },
		"terminal":           func(v *CaptureInput) { v.Pages[1].Terminal = false },
		"duplicate cursor":   func(v *CaptureInput) { v.Pages[1].Cursor = ""; v.Pages[0].NextCursor = "" },
		"position duplicate": func(v *CaptureInput) { v.Positions = append(v.Positions, v.Positions[0]); setFixturePages(v) },
		"fill duplicate":     func(v *CaptureInput) { v.Fills = append(v.Fills, v.Fills[0]); setFixturePages(v) },
		"fill horizon":       func(v *CaptureInput) { v.Fills[0].SourceAt = testHorizonEnd.Add(time.Second); setFixturePages(v) },
		"fill side":          func(v *CaptureInput) { v.Fills[0].Side = "hold"; setFixturePages(v) },
		"mapping":            func(v *CaptureInput) { v.Positions[0].ContractID = "UNKNOWN"; setFixturePages(v) },
		"missing order":      func(v *CaptureInput) { v.Fills[0].ExternalOrderID = ""; setFixturePages(v) },
		"missing client":     func(v *CaptureInput) { v.Fills[0].ClientOrderID = ""; setFixturePages(v) },
		"missing revision":   func(v *CaptureInput) { v.Fills[0].SourceRevision = ""; setFixturePages(v) },
		"lossy row time": func(v *CaptureInput) {
			v.Pages[1].Raw = bytes.Replace(v.Pages[1].Raw, []byte(".000000Z"), []byte(".000000001Z"), 1)
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := validCaptureInput(venue.ProviderAlpaca)
			mutate(&input)
			if _, err := normalizeTestCapture(input); err == nil {
				t.Fatal("invalid capture was accepted")
			}
		})
	}
}

func TestStableProviderSnapshotRequiresCompleteStateEquality(t *testing.T) {
	t.Parallel()
	base := validCaptureInput(venue.ProviderAlpaca)
	first, err := normalizeTestCapture(base)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := base
	secondInput.CaptureStart = secondInput.CaptureStart.Add(5 * time.Minute)
	secondInput.CaptureEnd = secondInput.CaptureEnd.Add(5 * time.Minute)
	second, err := normalizeTestCapture(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := AdmitStableProviderSnapshot(first, second)
	if err != nil || admission.Snapshot == nil || admission.Reason != "" {
		t.Fatalf("stable admission = %+v, %v", admission, err)
	}
	if admission.Snapshot.ID() == uuid.Nil || admission.Snapshot.Capture().Digest() != first.Digest() {
		t.Fatal("stable snapshot identity or state is missing")
	}
	mutations := map[string]func(*CaptureInput){
		"cash":        func(v *CaptureInput) { v.Cash = "100.01" },
		"equity":      func(v *CaptureInput) { v.Equity = "125.01" },
		"position":    func(v *CaptureInput) { v.Positions[0].Quantity = "3" },
		"fill":        func(v *CaptureInput) { v.Fills[0].Fee = "0.11" },
		"cursor":      func(v *CaptureInput) { v.Pages[0].NextCursor = "new"; v.Pages[1].Cursor = "new" },
		"raw page":    func(v *CaptureInput) { v.Pages[0].Raw = append(v.Pages[0].Raw, '\n') },
		"source time": func(v *CaptureInput) { v.Fills[0].SourceAt = v.Fills[0].SourceAt.Add(-time.Microsecond) },
	}
	for name, mutate := range mutations {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			changed := validCaptureInput(venue.ProviderAlpaca)
			mutate(&changed)
			if name != "cursor" && name != "raw page" {
				setFixturePages(&changed)
			}
			capture, buildErr := normalizeTestCapture(changed)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			result, admitErr := AdmitStableProviderSnapshot(first, capture)
			if admitErr != nil || result.Snapshot != nil || result.Reason != ReasonSnapshotUnstable {
				t.Fatalf("unstable admission = %+v, %v", result, admitErr)
			}
		})
	}
}

type sequenceReader struct {
	values []*ProviderCapture
	index  int
}

type failingCaptureReader struct{ err error }

func (reader failingCaptureReader) Capture(context.Context) (*ProviderCapture, error) {
	return nil, reader.err
}

func (reader *sequenceReader) Capture(context.Context) (*ProviderCapture, error) {
	value := reader.values[reader.index]
	reader.index++
	return value, nil
}

func TestCaptureTwiceHasReadOnlySurface(t *testing.T) {
	t.Parallel()
	input := validCaptureInput(venue.ProviderKalshi)
	first, _ := normalizeTestCapture(input)
	second, _ := normalizeTestCapture(input)
	result, err := CaptureTwice(context.Background(), &sequenceReader{values: []*ProviderCapture{first, second}})
	if err != nil || result.Snapshot == nil {
		t.Fatalf("CaptureTwice() = %+v, %v", result, err)
	}
}

func TestCaptureTwicePreservesIncompleteAndMappingFailureReasons(t *testing.T) {
	t.Parallel()
	for _, reason := range []ReasonCode{ReasonSnapshotIncomplete, ReasonSnapshotMappingFailure} {
		admission, err := CaptureTwice(context.Background(), failingCaptureReader{
			err: NewCaptureFailure(reason, errors.New("captured evidence failed")),
		})
		if err != nil || admission.Snapshot != nil || admission.Reason != reason {
			t.Fatalf("reason %s admission = %+v, %v", reason, admission, err)
		}
	}
	input := validCaptureInput(venue.ProviderAlpaca)
	input.Pages[0].Raw = []byte(`not-json`)
	if _, err := NormalizeAlpacaCapture(context.Background(), input, fixedResolver{}); captureFailureReason(err) != ReasonSnapshotIncomplete {
		t.Fatalf("malformed page reason = %v", err)
	}
	input = validCaptureInput(venue.ProviderAlpaca)
	if _, err := NormalizeAlpacaCapture(context.Background(), input, fixedResolver{err: errors.New("ambiguous")}); captureFailureReason(err) != ReasonSnapshotMappingFailure {
		t.Fatalf("mapping reason = %v", err)
	}
}
