package instrument

import (
	"encoding/json"
	"testing"
	"testing/quick"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func verifiedEquityInput() InstrumentInput {
	return InstrumentInput{
		IdentityKey:      "figi:BBG000B9XRY4",
		AssetClass:       AssetClassEquity,
		PrimaryVenue:     "NASDAQ",
		Currency:         "usd",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: SettlementPhysical,
		Status:           StatusActive,
	}
}

func TestNewInstrumentBuildsVerifiedEquity(t *testing.T) {
	got, err := NewInstrument(verifiedEquityInput())
	if err != nil {
		t.Fatalf("NewInstrument() error = %v", err)
	}
	if got.ID == uuid.Nil {
		t.Fatal("NewInstrument() ID is nil")
	}
	if got.IdentityKey != "figi:BBG000B9XRY4" || got.AssetClass != AssetClassEquity {
		t.Fatalf("identity = %q/%q", got.IdentityKey, got.AssetClass)
	}
	if got.PrimaryVenue != "nasdaq" || got.Currency != "USD" {
		t.Fatalf("venue/currency = %q/%q, want nasdaq/USD", got.PrimaryVenue, got.Currency)
	}
	if !got.TickSize.Equal(decimal.RequireFromString("0.01")) ||
		!got.LotSize.Equal(decimal.NewFromInt(1)) ||
		!got.Multiplier.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("mechanics = tick:%s lot:%s multiplier:%s", got.TickSize, got.LotSize, got.Multiplier)
	}
	if got.CreatedAt.IsZero() || got.CreatedAt.Nanosecond()%1_000 != 0 {
		t.Fatalf("created_at = %s, want PostgreSQL microsecond precision", got.CreatedAt)
	}
}

func TestInstrumentPropertyRejectsInvalidMechanics(t *testing.T) {
	property := func(raw int64, field uint8) bool {
		input := verifiedEquityInput()
		invalid := decimal.NewFromInt(-raw)
		if invalid.IsPositive() {
			invalid = invalid.Neg()
		}
		switch field % 3 {
		case 0:
			input.TickSize = invalid
		case 1:
			input.LotSize = invalid
		default:
			input.Multiplier = invalid
		}
		_, err := NewInstrument(input)
		return err != nil
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 250}); err != nil {
		t.Fatal(err)
	}
}

func TestInstrumentPropertyRejectsWrongCurrency(t *testing.T) {
	for _, currency := range []string{"", "US", "USDX", "U1D", "EUR!", "€UR"} {
		input := verifiedEquityInput()
		input.Currency = currency
		if _, err := NewInstrument(input); err == nil {
			t.Errorf("NewInstrument() currency %q unexpectedly succeeded", currency)
		}
	}
}

func TestInstrumentRejectsNumericMagnitudeBeyondSchema(t *testing.T) {
	tests := map[string]decimal.Decimal{
		"more than 12 fractional digits": decimal.RequireFromString("0.0000000000001"),
		"more than 26 integer digits":    decimal.RequireFromString("100000000000000000000000000"),
	}
	for name, invalid := range tests {
		t.Run(name, func(t *testing.T) {
			input := verifiedEquityInput()
			input.TickSize = invalid
			if _, err := NewInstrument(input); err == nil {
				t.Fatalf("NewInstrument() tick size %s unexpectedly succeeded", invalid)
			}
		})
	}
}

func TestActiveOptionRequiresTermsAndUnderlying(t *testing.T) {
	expiration := time.Date(2027, time.January, 15, 21, 0, 0, 999, time.UTC)
	underlyingID := uuid.New()
	valid := verifiedEquityInput()
	valid.IdentityKey = "occ:AAPL270115C00150000"
	valid.AssetClass = AssetClassOption
	valid.PrimaryVenue = "CBOE"
	valid.Expiration = &expiration
	valid.ExerciseStyle = ExerciseAmerican
	valid.UnderlyingID = &underlyingID

	got, err := NewInstrument(valid)
	if err != nil {
		t.Fatalf("NewInstrument() complete option error = %v", err)
	}
	if got.Expiration == nil || got.Expiration.Nanosecond()%1_000 != 0 {
		t.Fatalf("expiration = %v, want PostgreSQL microsecond precision", got.Expiration)
	}

	for name, mutate := range map[string]func(*InstrumentInput){
		"expiration":     func(input *InstrumentInput) { input.Expiration = nil },
		"exercise style": func(input *InstrumentInput) { input.ExerciseStyle = "" },
		"underlying":     func(input *InstrumentInput) { input.UnderlyingID = nil },
	} {
		t.Run("missing "+name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := NewInstrument(input); err == nil {
				t.Fatalf("NewInstrument() without %s unexpectedly succeeded", name)
			}
		})
	}
}

func TestQuarantinedInstrumentAllowsUnknownTermsButNotInventedValidity(t *testing.T) {
	quarantined := InstrumentInput{
		IdentityKey:  "legacy:stock:AAPL",
		AssetClass:   AssetClassUnknown,
		PrimaryVenue: "legacy",
		Status:       StatusQuarantined,
		Metadata:     json.RawMessage(`{"source":"legacy_augr_stock"}`),
	}
	got, err := NewInstrument(quarantined)
	if err != nil {
		t.Fatalf("NewInstrument() quarantined placeholder error = %v", err)
	}
	if got.Status != StatusQuarantined || !got.TickSize.IsZero() || got.Currency != "" {
		t.Fatalf("unexpected quarantined placeholder: %+v", got)
	}

	unverifiedActive := quarantined
	unverifiedActive.Status = StatusActive
	if _, err := NewInstrument(unverifiedActive); err == nil {
		t.Fatal("NewInstrument() active placeholder unexpectedly succeeded")
	}

	invalidClaim := quarantined
	invalidClaim.TickSize = decimal.NewFromInt(-1)
	if _, err := NewInstrument(invalidClaim); err == nil {
		t.Fatal("NewInstrument() quarantined invalid tick size unexpectedly succeeded")
	}

	missingProvenance := quarantined
	missingProvenance.Metadata = nil
	if _, err := NewInstrument(missingProvenance); err == nil {
		t.Fatal("NewInstrument() quarantined placeholder without provenance unexpectedly succeeded")
	}
}

func TestInstrumentValidateRejectsUnnormalizedManualFields(t *testing.T) {
	base, err := NewInstrument(verifiedEquityInput())
	if err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*Instrument){
		"identity whitespace": func(value *Instrument) { value.IdentityKey = " " + value.IdentityKey },
		"uppercase venue":     func(value *Instrument) { value.PrimaryVenue = "NASDAQ" },
		"lowercase currency":  func(value *Instrument) { value.Currency = "usd" },
		"sub-microsecond time": func(value *Instrument) {
			value.CreatedAt = value.CreatedAt.Add(time.Nanosecond)
		},
		"non-UTC time": func(value *Instrument) {
			value.CreatedAt = value.CreatedAt.In(time.FixedZone("CST", -6*60*60))
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *base
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("Validate() unexpectedly succeeded")
			}
		})
	}
}
