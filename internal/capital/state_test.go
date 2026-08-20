package capital

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

func TestStateFromProjectionDerivesExactSupportedExposure(t *testing.T) {
	fixture := newCapitalStateFixture(t, domain.MarginProfileRegT, decimal.NewFromInt(2), []capitalPosition{
		{assetClass: instrument.AssetClassEquity, quantity: "10", marketValue: "1000"},
		{assetClass: instrument.AssetClassETF, quantity: "-4", marketValue: "-400"},
	})
	state, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Cash().Equal(decimal.NewFromInt(100_000)) || !state.Equity().Equal(decimal.NewFromInt(100_600)) ||
		!state.LongExposure().Equal(decimal.NewFromInt(1_000)) || !state.ShortExposure().Equal(decimal.NewFromInt(400)) ||
		!state.GrossExposure().Equal(decimal.NewFromInt(1_400)) ||
		!state.MaintenanceRequirement().Equal(decimal.NewFromInt(370)) || state.Hash() == "" || len(state.CanonicalBytes()) == 0 {
		t.Fatalf("state = %+v", state)
	}
	bytes := state.CanonicalBytes()
	bytes[0] = '['
	if string(bytes) == string(state.CanonicalBytes()) {
		t.Fatal("CanonicalBytes exposed mutable storage")
	}
	replayed, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments)
	if err != nil || replayed.Hash() != state.Hash() || string(replayed.CanonicalBytes()) != string(state.CanonicalBytes()) {
		t.Fatalf("replayed state = %+v/%v", replayed, err)
	}
}

func TestStateCanonicalRestoreRequiresExactContextAndBytes(t *testing.T) {
	fixture := newCapitalStateFixture(t, domain.MarginProfileRegT, decimal.NewFromInt(2), nil)
	state, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := StateFromCanonical(state.ID(), state.Hash(), state.CanonicalBytes(), fixture.account, fixture.binding, fixture.policy)
	if err != nil || restored.ID() != state.ID() || restored.Hash() != state.Hash() || !bytes.Equal(restored.CanonicalBytes(), state.CanonicalBytes()) {
		t.Fatalf("restored state = %+v/%v", restored, err)
	}
	tampered := state.CanonicalBytes()
	tampered[len(tampered)-2] ^= 1
	if _, err := StateFromCanonical(state.ID(), state.Hash(), tampered, fixture.account, fixture.binding, fixture.policy); err == nil {
		t.Fatal("tampered capital state restored")
	}
}

func TestStateFromProjectionRejectsUnsupportedOrUnprovenPositionIdentity(t *testing.T) {
	tests := map[string]func(*capitalStateFixture){
		"unsupported option": func(value *capitalStateFixture) {
			value.instruments[0].AssetClass = instrument.AssetClassOption
		},
		"unsupported prediction": func(value *capitalStateFixture) {
			value.instruments[0].AssetClass = instrument.AssetClassPredictionContract
		},
		"missing instrument": func(value *capitalStateFixture) {
			value.instruments = nil
		},
		"extra instrument": func(value *capitalStateFixture) {
			value.instruments = append(value.instruments, value.instruments[0])
			value.instruments[1].ID = uuid.New()
		},
		"duplicate instrument": func(value *capitalStateFixture) {
			value.instruments = append(value.instruments, value.instruments[0])
		},
		"wrong instrument ID": func(value *capitalStateFixture) {
			value.instruments[0].ID = uuid.New()
		},
		"wrong currency": func(value *capitalStateFixture) {
			value.instruments[0].Currency = "EUR"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newCapitalStateFixture(t, domain.MarginProfileRegT, decimal.NewFromInt(2), []capitalPosition{
				{assetClass: instrument.AssetClassEquity, quantity: "10", marketValue: "1000"},
			})
			mutate(&fixture)
			if _, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments); err == nil {
				t.Fatal("StateFromProjection unexpectedly succeeded")
			}
		})
	}
}

func TestStateFromProjectionRejectsMismatchedOrTamperedProjection(t *testing.T) {
	tests := map[string]func(*capitalStateFixture){
		"account":  func(value *capitalStateFixture) { value.projection.AccountID = uuid.New() },
		"currency": func(value *capitalStateFixture) { value.projection.BaseCurrency = "EUR" },
		"as of":    func(value *capitalStateFixture) { value.projection.AsOf = value.projection.AsOf.Add(time.Nanosecond) },
		"checksum": func(value *capitalStateFixture) { value.projection.OutputChecksum = "bad" },
		"payload":  func(value *capitalStateFixture) { value.projection.PayloadBytes[0] = '[' },
		"equity": func(value *capitalStateFixture) {
			value.projection.Totals.Equity = value.projection.Totals.Equity.Add(decimal.NewFromInt(1))
		},
		"market total": func(value *capitalStateFixture) {
			value.projection.Totals.MarketValue = value.projection.Totals.MarketValue.Add(decimal.NewFromInt(1))
		},
		"position quantity sign": func(value *capitalStateFixture) {
			value.projection.Positions[0].Quantity = value.projection.Positions[0].Quantity.Neg()
		},
		"position payload drift": func(value *capitalStateFixture) {
			value.projection.Positions[0].MarketValue = value.projection.Positions[0].MarketValue.Add(decimal.NewFromInt(1))
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newCapitalStateFixture(t, domain.MarginProfileRegT, decimal.NewFromInt(2), []capitalPosition{
				{assetClass: instrument.AssetClassEquity, quantity: "10", marketValue: "1000"},
			})
			mutate(&fixture)
			if _, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments); err == nil {
				t.Fatal("StateFromProjection unexpectedly succeeded")
			}
		})
	}
}

type capitalPosition struct {
	assetClass  instrument.AssetClass
	quantity    string
	marketValue string
}

type capitalStateFixture struct {
	account     domain.Account
	binding     Binding
	policy      *Policy
	projection  *ledger.PortfolioProjection
	instruments []instrument.Instrument
}

func newCapitalStateFixture(
	t *testing.T,
	profile domain.MarginProfile,
	multiplier decimal.Decimal,
	positions []capitalPosition,
) capitalStateFixture {
	return newCapitalStateFixtureForMode(
		t, domain.AccountEnvironmentPaperScored, decimal.NewFromInt(100_000),
		profile, multiplier, decimal.NewFromInt(100_000), positions,
	)
}

func newCapitalStateFixtureForMode(
	t *testing.T,
	environment domain.AccountEnvironment,
	tier decimal.Decimal,
	profile domain.MarginProfile,
	multiplier decimal.Decimal,
	cash decimal.Decimal,
	positions []capitalPosition,
) capitalStateFixture {
	t.Helper()
	policy := bindingTestPolicy(t)
	account := bindingTestAccount(t, environment, tier, profile, multiplier)
	binding, err := NewBinding(*account, policy, account.StartingCapital, profile, bindingTestTime())
	if err != nil {
		t.Fatal(err)
	}
	projection := &ledger.PortfolioProjection{
		CheckpointID: uuid.New(), ProjectionType: ledger.PortfolioProjectionType, Version: ledger.PortfolioProjectionVersion,
		FIFO: ledger.ProjectionFIFO, AccountID: account.ID, BaseCurrency: account.BaseCurrency,
		AsOf: bindingTestTime(), ThroughTransactionID: uuid.New(), TransactionCount: 1,
		InputChecksum: strings64("a"), Totals: ledger.ProjectionTotals{Cash: cash},
	}
	for _, value := range positions {
		reference := capitalTestInstrument(t, value.assetClass)
		quantity := decimal.RequireFromString(value.quantity)
		marketValue := decimal.RequireFromString(value.marketValue)
		projection.Positions = append(projection.Positions, ledger.ProjectionPosition{
			InstrumentID: reference.ID, Open: true, Quantity: quantity, MarketValue: marketValue,
			MarkObservationID: uuid.New(), OpenLotCount: 1,
		})
		projection.Totals.MarketValue = projection.Totals.MarketValue.Add(marketValue)
		projection.Marks = append(projection.Marks, ledger.ProjectionMark{ID: projection.Positions[len(projection.Positions)-1].MarkObservationID, InstrumentID: reference.ID})
	}
	projection.Totals.Equity = projection.Totals.Cash.Add(projection.Totals.MarketValue)
	projection.Totals.NetCapital = projection.Totals.Equity
	projection.Totals.TotalPnL = decimal.Zero

	instruments := make([]instrument.Instrument, 0, len(positions))
	for index, value := range positions {
		reference := capitalTestInstrument(t, value.assetClass)
		reference.ID = projection.Positions[index].InstrumentID
		instruments = append(instruments, *reference)
	}
	projection.PayloadBytes = capitalProjectionPayload(t, projection)
	digest := sha256.Sum256(projection.PayloadBytes)
	projection.OutputChecksum = hex.EncodeToString(digest[:])
	return capitalStateFixture{account: *account, binding: *binding, policy: policy, projection: projection, instruments: instruments}
}

func capitalProjectionPayload(t *testing.T, projection *ledger.PortfolioProjection) []byte {
	t.Helper()
	type payloadPosition struct {
		InstrumentID string `json:"instrument_id"`
		Open         bool   `json:"open"`
		Quantity     string `json:"quantity"`
		MarketValue  string `json:"market_value"`
	}
	value := struct {
		CheckpointID string            `json:"checkpoint_id"`
		AccountID    string            `json:"account_id"`
		BaseCurrency string            `json:"base_currency"`
		AsOf         string            `json:"as_of"`
		Positions    []payloadPosition `json:"positions"`
		Totals       struct {
			Cash        string `json:"cash"`
			MarketValue string `json:"market_value"`
			Equity      string `json:"equity"`
		} `json:"totals"`
	}{
		CheckpointID: projection.CheckpointID.String(), AccountID: projection.AccountID.String(),
		BaseCurrency: projection.BaseCurrency, AsOf: projection.AsOf.Format("2006-01-02T15:04:05.000000Z"),
	}
	for _, position := range projection.Positions {
		value.Positions = append(value.Positions, payloadPosition{
			InstrumentID: position.InstrumentID.String(), Open: position.Open,
			Quantity: position.Quantity.String(), MarketValue: position.MarketValue.String(),
		})
	}
	value.Totals.Cash = projection.Totals.Cash.String()
	value.Totals.MarketValue = projection.Totals.MarketValue.String()
	value.Totals.Equity = projection.Totals.Equity.String()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func capitalTestInstrument(t *testing.T, assetClass instrument.AssetClass) *instrument.Instrument {
	t.Helper()
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "capital-test:" + uuid.NewString(), AssetClass: assetClass,
		PrimaryVenue: "test", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive,
		Metadata: json.RawMessage(`{"fixture":"capital-state"}`), CreatedAt: bindingTestTime().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return reference
}

func strings64(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}
