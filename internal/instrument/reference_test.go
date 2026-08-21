package instrument

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNewAliasEventNormalizesProviderTickerAndTime(t *testing.T) {
	effectiveAt := time.Date(2026, time.August, 15, 12, 0, 0, 123456789, time.UTC)
	event, err := NewAliasEvent(AliasEventInput{
		InstrumentID: uuid.New(),
		Provider:     " Legacy_Augr_Stock ",
		AliasType:    AliasTicker,
		AliasValue:   " aapl ",
		Action:       AliasAssigned,
		EffectiveAt:  effectiveAt,
		Source:       "migration-000066",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Provider != "legacy_augr_stock" || event.AliasValue != "AAPL" ||
		!event.EffectiveAt.Equal(effectiveAt.Truncate(time.Microsecond)) {
		t.Fatalf("unexpected alias event: %+v", event)
	}
}

func TestAliasRetirementRetainsInstrumentIdentity(t *testing.T) {
	instrumentID := uuid.New()
	retirement, err := NewAliasEvent(AliasEventInput{
		InstrumentID: instrumentID,
		Provider:     "polygon",
		AliasType:    AliasTicker,
		AliasValue:   "AAPL",
		Action:       AliasRetired,
		EffectiveAt:  time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC),
		Source:       "corporate-action-feed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retirement.InstrumentID != instrumentID || retirement.Action != AliasRetired {
		t.Fatalf("retirement changed identity: %+v", retirement)
	}
}

func TestVenueContractRejectsInvalidWindow(t *testing.T) {
	validFrom := time.Date(2026, time.August, 15, 9, 30, 0, 999, time.UTC)
	validTo := validFrom.Add(24 * time.Hour)
	input := VenueContractInput{
		InstrumentID:     uuid.New(),
		Venue:            " Alpaca ",
		ContractID:       " aapl ",
		Currency:         "usd",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: SettlementPhysical,
		ValidFrom:        validFrom,
		ValidTo:          &validTo,
	}
	contract, err := NewVenueContract(input)
	if err != nil {
		t.Fatalf("NewVenueContract() error = %v", err)
	}
	if contract.Venue != "alpaca" || contract.ContractID != "AAPL" || contract.Currency != "USD" {
		t.Fatalf("unexpected normalized contract: %+v", contract)
	}
	if contract.ValidFrom.Nanosecond()%1_000 != 0 {
		t.Fatalf("valid_from = %s, want microsecond precision", contract.ValidFrom)
	}

	for name, invalidTo := range map[string]time.Time{
		"equal":  validFrom,
		"before": validFrom.Add(-time.Nanosecond),
	} {
		t.Run(name, func(t *testing.T) {
			candidate := input
			candidate.ValidTo = &invalidTo
			if _, err := NewVenueContract(candidate); err == nil {
				t.Fatal("NewVenueContract() unexpectedly succeeded")
			}
		})
	}
}

func TestSplitRequiresPositiveRatio(t *testing.T) {
	input := CorporateActionInput{
		InstrumentID:     uuid.New(),
		ActionType:       CorporateActionSplit,
		EffectiveAt:      time.Date(2026, time.August, 15, 0, 0, 0, 777, time.UTC),
		RatioNumerator:   decimal.NewFromInt(4),
		RatioDenominator: decimal.NewFromInt(1),
		Source:           "issuer-feed",
		SourceEventID:    "split-123",
	}
	action, err := NewCorporateAction(input)
	if err != nil {
		t.Fatalf("NewCorporateAction() error = %v", err)
	}
	if action.RatioNumerator.String() != "4" || action.RatioDenominator.String() != "1" {
		t.Fatalf("unexpected split ratio: %+v", action)
	}

	for name, mutate := range map[string]func(*CorporateActionInput){
		"zero numerator":       func(candidate *CorporateActionInput) { candidate.RatioNumerator = decimal.Zero },
		"negative denominator": func(candidate *CorporateActionInput) { candidate.RatioDenominator = decimal.NewFromInt(-1) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := input
			mutate(&candidate)
			if _, err := NewCorporateAction(candidate); err == nil {
				t.Fatal("NewCorporateAction() unexpectedly succeeded")
			}
		})
	}
}

func TestMergerRequiresDifferentSuccessor(t *testing.T) {
	instrumentID := uuid.New()
	successorID := uuid.New()
	input := CorporateActionInput{
		InstrumentID:          instrumentID,
		SuccessorInstrumentID: &successorID,
		ActionType:            CorporateActionMerger,
		EffectiveAt:           time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		Source:                "issuer-feed",
		SourceEventID:         "merger-123",
	}
	if _, err := NewCorporateAction(input); err != nil {
		t.Fatalf("NewCorporateAction() merger error = %v", err)
	}

	missing := input
	missing.SuccessorInstrumentID = nil
	if _, err := NewCorporateAction(missing); err == nil {
		t.Fatal("NewCorporateAction() merger without successor unexpectedly succeeded")
	}

	self := input
	self.SuccessorInstrumentID = &instrumentID
	if _, err := NewCorporateAction(self); err == nil {
		t.Fatal("NewCorporateAction() self-successor unexpectedly succeeded")
	}
}

func TestCashDividendRequiresAmountAndCurrency(t *testing.T) {
	amount := decimal.RequireFromString("0.25")
	input := CorporateActionInput{
		InstrumentID:  uuid.New(),
		ActionType:    CorporateActionCashDividend,
		EffectiveAt:   time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		CashAmount:    &amount,
		CashCurrency:  "usd",
		Source:        "issuer-feed",
		SourceEventID: "dividend-123",
	}
	action, err := NewCorporateAction(input)
	if err != nil {
		t.Fatalf("NewCorporateAction() dividend error = %v", err)
	}
	if action.CashAmount == nil || action.CashCurrency != "USD" || !action.CashAmount.Equal(amount) {
		t.Fatalf("unexpected dividend terms: %+v", action)
	}

	missingAmount := input
	missingAmount.CashAmount = nil
	if _, err := NewCorporateAction(missingAmount); err == nil {
		t.Fatal("NewCorporateAction() dividend without amount unexpectedly succeeded")
	}

	missingCurrency := input
	missingCurrency.CashCurrency = ""
	if _, err := NewCorporateAction(missingCurrency); err == nil {
		t.Fatal("NewCorporateAction() dividend without currency unexpectedly succeeded")
	}
}
