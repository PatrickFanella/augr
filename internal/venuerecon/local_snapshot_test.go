package venuerecon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

var (
	localAccountID    = uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	localTransaction1 = uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	localTransaction2 = uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	localAsOf         = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
)

func validLocalCheckpoint(t *testing.T) *ledger.ProjectionCheckpoint {
	t.Helper()
	inputChecksum := strings.Repeat("a", 64)
	id := economicid.DeterministicUUID("portfolio-projection-checkpoint", localAccountID.String(),
		ledger.PortfolioProjectionType, ledger.PortfolioProjectionVersion, canonicalTime(localAsOf), inputChecksum)
	payload := struct {
		CheckpointID           string `json:"checkpoint_id"`
		ProjectionType         string `json:"projection_type"`
		Version                string `json:"version"`
		FIFO                   string `json:"fifo"`
		AccountID              string `json:"account_id"`
		BaseCurrency           string `json:"base_currency"`
		AsOf                   string `json:"as_of"`
		MarkSource             string `json:"mark_source"`
		MarkNamespace          string `json:"mark_namespace"`
		MaxMarkAgeMicroseconds int64  `json:"max_mark_age_microseconds"`
		ThroughTransactionID   string `json:"through_transaction_id"`
		TransactionCount       int    `json:"transaction_count"`
		InputChecksum          string `json:"input_checksum"`
		Marks                  []any  `json:"marks"`
		Lots                   []any  `json:"lots"`
		Matches                []any  `json:"matches"`
		Positions              []struct {
			InstrumentID         string `json:"instrument_id"`
			Open                 bool   `json:"open"`
			Quantity             string `json:"quantity"`
			RemainingOpeningCash string `json:"remaining_opening_cash"`
			RealizedPnL          string `json:"realized_pnl"`
			MarketValue          string `json:"market_value"`
			UnrealizedPnL        string `json:"unrealized_pnl"`
			MarkObservationID    string `json:"mark_observation_id"`
			OpenLotCount         int    `json:"open_lot_count"`
		} `json:"positions"`
		Totals struct {
			Cash          string `json:"cash"`
			NetCapital    string `json:"net_capital"`
			Fees          string `json:"fees"`
			Rebates       string `json:"rebates"`
			RealizedPnL   string `json:"realized_pnl"`
			UnrealizedPnL string `json:"unrealized_pnl"`
			MarketValue   string `json:"market_value"`
			Equity        string `json:"equity"`
			TotalPnL      string `json:"total_pnl"`
		} `json:"totals"`
	}{
		CheckpointID: id.String(), ProjectionType: ledger.PortfolioProjectionType, Version: ledger.PortfolioProjectionVersion,
		FIFO: ledger.ProjectionFIFO, AccountID: localAccountID.String(), BaseCurrency: "USD", AsOf: canonicalTime(localAsOf),
		MarkSource: "polygon", MarkNamespace: "consolidated/mark", MaxMarkAgeMicroseconds: int64(time.Hour / time.Microsecond),
		ThroughTransactionID: localTransaction2.String(), TransactionCount: 2, InputChecksum: inputChecksum,
		Marks: []any{}, Lots: []any{}, Matches: []any{},
	}
	payload.Positions = append(payload.Positions, struct {
		InstrumentID         string `json:"instrument_id"`
		Open                 bool   `json:"open"`
		Quantity             string `json:"quantity"`
		RemainingOpeningCash string `json:"remaining_opening_cash"`
		RealizedPnL          string `json:"realized_pnl"`
		MarketValue          string `json:"market_value"`
		UnrealizedPnL        string `json:"unrealized_pnl"`
		MarkObservationID    string `json:"mark_observation_id"`
		OpenLotCount         int    `json:"open_lot_count"`
	}{
		InstrumentID: testInstrumentID.String(), Open: true, Quantity: "2", RemainingOpeningCash: "-20",
		RealizedPnL: "0", MarketValue: "22", UnrealizedPnL: "2", MarkObservationID: uuid.New().String(), OpenLotCount: 1,
	})
	payload.Totals.Cash = "100.25"
	payload.Totals.NetCapital = "100"
	payload.Totals.Fees = "0.25"
	payload.Totals.Rebates = "0"
	payload.Totals.RealizedPnL = "0"
	payload.Totals.UnrealizedPnL = "2"
	payload.Totals.MarketValue = "22"
	payload.Totals.Equity = "122.25"
	payload.Totals.TotalPnL = "2"
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	return &ledger.ProjectionCheckpoint{
		ID: id, AccountID: localAccountID, ProjectionType: ledger.PortfolioProjectionType,
		ThroughTransactionID: localTransaction2, ProjectionVersion: ledger.PortfolioProjectionVersion,
		AsOf: localAsOf, FIFO: ledger.ProjectionFIFO, BaseCurrency: "USD", MarkSource: "polygon",
		MarkNamespace: "consolidated/mark", MaxMarkAge: time.Hour, TransactionCount: 2,
		MarkCount: 0, LotCount: 0, MatchCount: 0, PositionCount: 1,
		InputChecksum: inputChecksum, OutputChecksum: hex.EncodeToString(digest[:]), PayloadBytes: encoded,
		AttestationKeyID: "projection-test-key", AttestationHMAC: bytesOf(32, 7), CreatedAt: localAsOf.Add(time.Minute),
	}
}

func bytesOf(count int, value byte) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func validLocalInput(t *testing.T) LocalSnapshotInput {
	t.Helper()
	fill := LocalFill{
		FillID: uuid.New(), IntentID: uuid.New(), OrderID: uuid.New(), AccountID: localAccountID,
		Provider: venue.ProviderAlpaca, Namespace: "alpaca/account-activities/FILL", SourceID: "fill-1", SourceRevision: "1",
		ObservationClass: lifecycle.ObservationOrdinary, ExternalOrderID: "external-1", ClientOrderID: "client-1",
		InstrumentID: testInstrumentID, VenueContractID: testContractID, Side: lifecycle.SideBuy,
		Quantity: decimal.RequireFromString("2"), Price: decimal.RequireFromString("10"), Fee: decimal.RequireFromString("0.25"),
		Currency: "USD", SourceAt: localAsOf.Add(-time.Hour), NormalizationID: uuid.New(), LedgerTransactionID: localTransaction2,
	}
	return LocalSnapshotInput{
		AccountID: localAccountID, Provider: venue.ProviderAlpaca, Namespace: "alpaca/account-activities/FILL",
		HorizonStart: localAsOf.Add(-24 * time.Hour), HorizonEnd: localAsOf, Checkpoint: validLocalCheckpoint(t),
		TransactionIDs: []uuid.UUID{localTransaction2, localTransaction1}, Fills: []LocalFill{fill},
		Issues: []LocalSnapshotIssue{
			{
				Reason: ReasonLocalFillIncomplete, AccountID: localAccountID, Provider: venue.ProviderAlpaca,
				Namespace: "alpaca/account-activities/FILL", SourceID: "incomplete-1", SourceAt: localAsOf.Add(-time.Hour),
				VenueContractID: testContractID, LedgerTransactionID: localTransaction1, EvidenceID: uuid.New(),
			},
			{
				Reason: ReasonLocalFillAfterFrontier, AccountID: localAccountID, Provider: venue.ProviderAlpaca,
				Namespace: "alpaca/account-activities/FILL", SourceID: "future-1", SourceAt: localAsOf.Add(-time.Minute),
				VenueContractID: testContractID, LedgerTransactionID: uuid.New(), EvidenceID: uuid.New(),
			},
		},
	}
}

func TestLocalSnapshotDerivesCheckpointEconomicsAndExactMembership(t *testing.T) {
	t.Parallel()
	input := validLocalInput(t)
	snapshot, err := NewLocalSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID() == uuid.Nil || snapshot.Digest() == "" || !snapshot.Cash().Equal(decimal.RequireFromString("100.25")) {
		t.Fatalf("snapshot identity/cash = %s/%s/%s", snapshot.ID(), snapshot.Digest(), snapshot.Cash())
	}
	if positions := snapshot.Positions(); len(positions) != 1 || positions[0].InstrumentID != testInstrumentID || positions[0].Quantity != "2" {
		t.Fatalf("positions = %+v", positions)
	}
	if fills := snapshot.Fills(); len(fills) != 1 || fills[0].LedgerTransactionID != localTransaction2.String() {
		t.Fatalf("fills = %+v", fills)
	}
	if issues := snapshot.Issues(); len(issues) != 2 || issues[0].Reason != ReasonLocalFillAfterFrontier || issues[1].Reason != ReasonLocalFillIncomplete {
		t.Fatalf("issues = %+v", issues)
	}
	reordered := input
	reordered.TransactionIDs = []uuid.UUID{localTransaction1, localTransaction2}
	second, err := NewLocalSnapshot(reordered)
	if err != nil || !sameLocalSnapshotPayload(snapshot, second) {
		t.Fatalf("membership ordering changed identity: %v", err)
	}
}

func TestLocalSnapshotRejectsCheckpointDriftIncompleteGraphsAndFrontierLeakage(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*LocalSnapshotInput){
		"checkpoint hash":       func(v *LocalSnapshotInput) { v.Checkpoint.OutputChecksum = strings.Repeat("b", 64) },
		"account":               func(v *LocalSnapshotInput) { v.AccountID = uuid.New() },
		"horizon":               func(v *LocalSnapshotInput) { v.HorizonEnd = v.HorizonEnd.Add(-time.Microsecond) },
		"membership count":      func(v *LocalSnapshotInput) { v.TransactionIDs = v.TransactionIDs[:1] },
		"membership duplicate":  func(v *LocalSnapshotInput) { v.TransactionIDs[1] = v.TransactionIDs[0] },
		"fill after as of":      func(v *LocalSnapshotInput) { v.Fills[0].SourceAt = v.HorizonEnd.Add(time.Microsecond) },
		"missing normalization": func(v *LocalSnapshotInput) { v.Fills[0].NormalizationID = uuid.Nil },
		"wrong provider":        func(v *LocalSnapshotInput) { v.Fills[0].Provider = venue.ProviderKalshi },
		"foreign issue":         func(v *LocalSnapshotInput) { v.Issues[0].AccountID = uuid.New() },
		"incomplete not member": func(v *LocalSnapshotInput) { v.Issues[0].LedgerTransactionID = uuid.New() },
		"after frontier member": func(v *LocalSnapshotInput) { v.Issues[1].LedgerTransactionID = localTransaction1 },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := validLocalInput(t)
			mutate(&input)
			if _, err := NewLocalSnapshot(input); err == nil {
				t.Fatal("invalid local snapshot was accepted")
			}
		})
	}
}

func TestLocalSnapshotRetainsAfterFrontierFillAsExcludedIssue(t *testing.T) {
	t.Parallel()
	input := validLocalInput(t)
	input.Issues = nil
	input.Fills[0].LedgerTransactionID = uuid.New()
	snapshot, err := NewLocalSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Fills()) != 0 || len(snapshot.Issues()) != 1 || snapshot.Issues()[0].Reason != ReasonLocalFillAfterFrontier {
		t.Fatalf("frontier evidence = fills=%+v issues=%+v", snapshot.Fills(), snapshot.Issues())
	}
}

func TestLocalRevisionUsesOriginalIdentity(t *testing.T) {
	t.Parallel()
	input := validLocalInput(t)
	input.Fills[0].ObservationClass = lifecycle.ObservationCorrection
	input.Fills[0].ObservationDiscriminator = "corr-1"
	input.Fills[0].OriginalFillID = uuid.New()
	input.Fills[0].OriginalSourceID = "fill-original"
	snapshot, err := NewLocalSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if fill := snapshot.Fills()[0]; fill.OriginalSourceID != "fill-original" || fill.OriginalFillID == "" {
		t.Fatalf("revision = %+v", fill)
	}
}

type recordingLocalReader struct {
	input LocalSnapshotInput
	calls int
	err   error
}

func (reader *recordingLocalReader) ReadLocalEvidenceInRepeatableRead(context.Context, LocalSnapshotRequest) (LocalSnapshotInput, error) {
	reader.calls++
	return reader.input, reader.err
}

func TestLocalSourceUsesOneTransactionOwningRead(t *testing.T) {
	t.Parallel()
	input := validLocalInput(t)
	reader := &recordingLocalReader{input: input}
	source := NewLocalSource(reader)
	request := LocalSnapshotRequest{
		AccountID: input.AccountID, Provider: input.Provider, Namespace: input.Namespace,
		HorizonStart: input.HorizonStart, HorizonEnd: input.HorizonEnd, CheckpointID: input.Checkpoint.ID,
	}
	snapshot, err := source.Capture(context.Background(), request)
	if err != nil || snapshot == nil || reader.calls != 1 {
		t.Fatalf("Capture() = %+v, calls=%d, err=%v", snapshot, reader.calls, err)
	}
	reader.err = errors.New("transaction unavailable")
	if _, err := source.Capture(context.Background(), request); err == nil {
		t.Fatal("reader error was hidden")
	}
}
