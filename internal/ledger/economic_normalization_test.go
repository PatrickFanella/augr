package ledger

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

func TestNewFillEconomicNormalizationBalancesBuyAndSell(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		side          FillSide
		wantInventory string
		wantCash      string
	}{
		{name: "buy", side: FillSideBuy, wantInventory: "2", wantCash: "-20.5"},
		{name: "sell", side: FillSideSell, wantInventory: "-2", wantCash: "20.5"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newEconomicNormalizationFixture(t)
			normalization, err := NewFillEconomicNormalization(FillEconomicEventInput{
				Base:          fixture.base,
				Instrument:    fixture.primary,
				VenueContract: fixture.contract,
				Side:          testCase.side,
				Quantity:      decimal.NewFromInt(2),
				Price:         decimal.RequireFromString("10.25"),
			})
			if err != nil {
				t.Fatalf("NewFillEconomicNormalization() error = %v", err)
			}
			assertPostingAmount(t, normalization.Transaction, "inventory", testCase.wantInventory)
			assertPostingAmount(t, normalization.Transaction, "clearing-inventory", decimal.RequireFromString(testCase.wantInventory).Neg().String())
			assertPostingAmount(t, normalization.Transaction, "gross-cash", testCase.wantCash)
			assertPostingAmount(t, normalization.Transaction, "clearing-gross-cash", decimal.RequireFromString(testCase.wantCash).Neg().String())
			if err := normalization.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestNewFillEconomicNormalizationUsesMultiplierAndTypedInventory(t *testing.T) {
	for _, testCase := range []struct {
		assetClass instrument.AssetClass
		account    string
	}{
		{instrument.AssetClassEquity, "asset:security_inventory"},
		{instrument.AssetClassETF, "asset:security_inventory"},
		{instrument.AssetClassFuture, "asset:security_inventory"},
		{instrument.AssetClassCryptoSpot, "asset:crypto_inventory"},
		{instrument.AssetClassOption, "asset:option_inventory"},
		{instrument.AssetClassPredictionContract, "asset:event_contract_inventory"},
	} {
		t.Run(string(testCase.assetClass), func(t *testing.T) {
			fixture := newEconomicNormalizationFixture(t)
			fixture.primary.AssetClass = testCase.assetClass
			fixture.primary.Multiplier = decimal.NewFromInt(100)
			fixture.contract.Multiplier = decimal.NewFromInt(100)
			if testCase.assetClass == instrument.AssetClassOption {
				expiration := fixture.base.EffectiveAt.Add(24 * time.Hour)
				underlyingID := uuid.New()
				fixture.primary.Expiration = &expiration
				fixture.primary.ExerciseStyle = instrument.ExerciseAmerican
				fixture.primary.UnderlyingID = &underlyingID
			}
			if testCase.assetClass == instrument.AssetClassPredictionContract {
				fixture.primary.SettlementMethod = instrument.SettlementBinary
				fixture.contract.SettlementMethod = instrument.SettlementBinary
			}
			normalization, err := NewFillEconomicNormalization(FillEconomicEventInput{
				Base: fixture.base, Instrument: fixture.primary, VenueContract: fixture.contract,
				Side: FillSideBuy, Quantity: decimal.NewFromInt(2), Price: decimal.RequireFromString("0.25"),
			})
			if err != nil {
				t.Fatal(err)
			}
			posting := postingByKey(t, normalization.Transaction, "inventory")
			if posting.LedgerAccount != testCase.account {
				t.Fatalf("inventory account = %q, want %q", posting.LedgerAccount, testCase.account)
			}
			assertPostingAmount(t, normalization.Transaction, "gross-cash", "-50")
		})
	}
}

func TestNewFillEconomicNormalizationZeroPriceOmitsCashAndAddsCost(t *testing.T) {
	fixture := newEconomicNormalizationFixture(t)
	normalization, err := NewFillEconomicNormalization(FillEconomicEventInput{
		Base: fixture.base, Instrument: fixture.primary, VenueContract: fixture.contract,
		Side: FillSideBuy, Quantity: decimal.NewFromInt(2), Price: decimal.Zero,
		Cost: &CostComponent{Kind: CostKindFee, Currency: "USD", Amount: decimal.RequireFromString("0.15")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hasPosting(normalization.Transaction, "gross-cash") || hasPosting(normalization.Transaction, "clearing-gross-cash") {
		t.Fatal("zero-price fill created zero-valued gross-cash postings")
	}
	assertPostingAmount(t, normalization.Transaction, "fee-expense", "0.15")
	assertPostingAmount(t, normalization.Transaction, "fee-cash", "-0.15")
}

func TestNewFillEconomicNormalizationAddsRebateWithoutCollapsingGross(t *testing.T) {
	fixture := newEconomicNormalizationFixture(t)
	normalization, err := NewFillEconomicNormalization(FillEconomicEventInput{
		Base: fixture.base, Instrument: fixture.primary, VenueContract: fixture.contract,
		Side: FillSideSell, Quantity: decimal.NewFromInt(2), Price: decimal.RequireFromString("10.25"),
		Cost: &CostComponent{Kind: CostKindRebate, Currency: "USD", Amount: decimal.RequireFromString("0.05")},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPostingAmount(t, normalization.Transaction, "gross-cash", "20.5")
	assertPostingAmount(t, normalization.Transaction, "rebate-cash", "0.05")
	assertPostingAmount(t, normalization.Transaction, "rebate-income", "-0.05")
}

func TestNewFillEconomicNormalizationIDsAreDeterministic(t *testing.T) {
	fixture := newEconomicNormalizationFixture(t)
	input := FillEconomicEventInput{
		Base: fixture.base, Instrument: fixture.primary, VenueContract: fixture.contract,
		Side: FillSideBuy, Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(10),
	}
	first, err := NewFillEconomicNormalization(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewFillEconomicNormalization(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != retry.ID || first.Transaction.ID != retry.Transaction.ID {
		t.Fatalf("normalization IDs differ: %s/%s and %s/%s", first.ID, retry.ID, first.Transaction.ID, retry.Transaction.ID)
	}
	for index := range first.Transaction.Postings {
		if first.Transaction.Postings[index].ID != retry.Transaction.Postings[index].ID {
			t.Fatalf("posting %d ID differs", index)
		}
	}
}

func TestNewFillEconomicNormalizationFailsClosed(t *testing.T) {
	for name, mutate := range map[string]func(*FillEconomicEventInput){
		"inactive instrument": func(input *FillEconomicEventInput) { input.Instrument.Status = instrument.StatusInactive },
		"wrong contract":      func(input *FillEconomicEventInput) { input.VenueContract.InstrumentID = uuid.New() },
		"contract not begun": func(input *FillEconomicEventInput) {
			input.VenueContract.ValidFrom = input.Base.EffectiveAt.Add(time.Second)
		},
		"contract ended": func(input *FillEconomicEventInput) {
			ended := input.Base.EffectiveAt
			input.VenueContract.ValidTo = &ended
		},
		"off tick":      func(input *FillEconomicEventInput) { input.Price = decimal.RequireFromString("10.251") },
		"off lot":       func(input *FillEconomicEventInput) { input.Quantity = decimal.RequireFromString("1.5") },
		"foreign venue": func(input *FillEconomicEventInput) { input.VenueContract.Currency = "EUR" },
		"foreign fee": func(input *FillEconomicEventInput) {
			input.Cost = &CostComponent{Kind: CostKindFee, Currency: "EUR", Amount: decimal.NewFromInt(1)}
		},
		"negative price": func(input *FillEconomicEventInput) { input.Price = decimal.NewFromInt(-1) },
		"zero quantity":  func(input *FillEconomicEventInput) { input.Quantity = decimal.Zero },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newEconomicNormalizationFixture(t)
			input := FillEconomicEventInput{
				Base: fixture.base, Instrument: fixture.primary, VenueContract: fixture.contract,
				Side: FillSideBuy, Quantity: decimal.NewFromInt(2), Price: decimal.NewFromInt(10),
			}
			mutate(&input)
			if _, err := NewFillEconomicNormalization(input); err == nil {
				t.Fatal("NewFillEconomicNormalization() unexpectedly succeeded")
			}
		})
	}
}

func TestNewFillEconomicNormalizationRejectsPredictionPriceOutsideUnitRange(t *testing.T) {
	fixture := newEconomicNormalizationFixture(t)
	fixture.primary.AssetClass = instrument.AssetClassPredictionContract
	fixture.primary.SettlementMethod = instrument.SettlementBinary
	fixture.contract.SettlementMethod = instrument.SettlementBinary
	input := FillEconomicEventInput{
		Base: fixture.base, Instrument: fixture.primary, VenueContract: fixture.contract,
		Side: FillSideBuy, Quantity: decimal.NewFromInt(1), Price: decimal.RequireFromString("1.01"),
	}
	if _, err := NewFillEconomicNormalization(input); err == nil {
		t.Fatal("prediction fill above 1 unexpectedly succeeded")
	}
}

func TestNewFillEconomicNormalizationRejectsProductPrecisionOverflow(t *testing.T) {
	fixture := newEconomicNormalizationFixture(t)
	fixture.primary.TickSize = decimal.RequireFromString("0.000000000001")
	fixture.primary.LotSize = decimal.RequireFromString("0.1")
	fixture.contract.TickSize = fixture.primary.TickSize
	fixture.contract.LotSize = fixture.primary.LotSize
	input := FillEconomicEventInput{
		Base: fixture.base, Instrument: fixture.primary, VenueContract: fixture.contract,
		Side: FillSideBuy, Quantity: decimal.RequireFromString("0.1"), Price: decimal.RequireFromString("0.000000000001"),
	}
	if _, err := NewFillEconomicNormalization(input); err == nil {
		t.Fatal("fill product beyond 12 decimal places unexpectedly succeeded")
	}
}

func TestNewCostEconomicNormalizationBalancesFeeAndRebate(t *testing.T) {
	for _, testCase := range []struct {
		kind      CostKind
		firstKey  string
		firstWant string
		secondKey string
	}{
		{CostKindFee, "fee-expense", "1.25", "fee-cash"},
		{CostKindRebate, "rebate-cash", "1.25", "rebate-income"},
	} {
		t.Run(string(testCase.kind), func(t *testing.T) {
			fixture := newEconomicNormalizationFixture(t)
			normalization, err := NewCostEconomicNormalization(CostEconomicEventInput{
				Base: fixture.base, Kind: testCase.kind, Currency: "usd", Amount: decimal.RequireFromString("1.25"),
			})
			if err != nil {
				t.Fatal(err)
			}
			assertPostingAmount(t, normalization.Transaction, testCase.firstKey, testCase.firstWant)
			assertPostingAmount(t, normalization.Transaction, testCase.secondKey, "-1.25")
		})
	}
}

func TestNewCostEconomicNormalizationRejectsZeroNegativeOrForeignAmount(t *testing.T) {
	for name, testCase := range map[string]struct {
		currency string
		amount   decimal.Decimal
	}{
		"zero":     {currency: "USD", amount: decimal.Zero},
		"negative": {currency: "USD", amount: decimal.NewFromInt(-1)},
		"foreign":  {currency: "EUR", amount: decimal.NewFromInt(1)},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newEconomicNormalizationFixture(t)
			_, err := NewCostEconomicNormalization(CostEconomicEventInput{
				Base: fixture.base, Kind: CostKindFee, Currency: testCase.currency, Amount: testCase.amount,
			})
			if err == nil {
				t.Fatal("NewCostEconomicNormalization() unexpectedly succeeded")
			}
		})
	}
}

func TestNewCashSettlementEconomicNormalizationClosesLongPredictionWinner(t *testing.T) {
	fixture := newPredictionSettlementFixture(t)
	normalization, err := NewCashSettlementEconomicNormalization(CashSettlementEconomicEventInput{
		Base: fixture.base, Kind: CashSettlementPrediction,
		Instrument: fixture.primary, VenueContract: fixture.contract,
		PositionQuantity: decimal.NewFromInt(2), SettlementPrice: decimal.NewFromInt(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPostingAmount(t, normalization.Transaction, "inventory-settlement", "-2")
	assertPostingAmount(t, normalization.Transaction, "clearing-inventory-settlement", "2")
	assertPostingAmount(t, normalization.Transaction, "settlement-cash", "2")
	assertPostingAmount(t, normalization.Transaction, "clearing-settlement-cash", "-2")
}

func TestNewCashSettlementEconomicNormalizationChargesShortWinner(t *testing.T) {
	fixture := newPredictionSettlementFixture(t)
	normalization, err := NewCashSettlementEconomicNormalization(CashSettlementEconomicEventInput{
		Base: fixture.base, Kind: CashSettlementPrediction,
		Instrument: fixture.primary, VenueContract: fixture.contract,
		PositionQuantity: decimal.NewFromInt(-3), SettlementPrice: decimal.NewFromInt(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPostingAmount(t, normalization.Transaction, "inventory-settlement", "3")
	assertPostingAmount(t, normalization.Transaction, "settlement-cash", "-3")
}

func TestNewCashSettlementEconomicNormalizationZeroExpirationOmitsCash(t *testing.T) {
	fixture := newOptionSettlementFixture(t, instrument.SettlementPhysical)
	normalization, err := NewCashSettlementEconomicNormalization(CashSettlementEconomicEventInput{
		Base: fixture.base, Kind: CashSettlementExpiration,
		Instrument: fixture.primary, VenueContract: fixture.contract,
		PositionQuantity: decimal.NewFromInt(1), SettlementPrice: decimal.Zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPostingAmount(t, normalization.Transaction, "inventory-settlement", "-1")
	if hasPosting(normalization.Transaction, "settlement-cash") || hasPosting(normalization.Transaction, "clearing-settlement-cash") {
		t.Fatal("zero expiration created zero-valued cash postings")
	}
}

func TestNewCashSettlementEconomicNormalizationUsesOptionMultiplierAfterContractExpiry(t *testing.T) {
	fixture := newOptionSettlementFixture(t, instrument.SettlementCash)
	ended := fixture.base.EffectiveAt.Add(-time.Second)
	fixture.contract.ValidTo = &ended
	normalization, err := NewCashSettlementEconomicNormalization(CashSettlementEconomicEventInput{
		Base: fixture.base, Kind: CashSettlementOption,
		Instrument: fixture.primary, VenueContract: fixture.contract,
		PositionQuantity: decimal.NewFromInt(2), SettlementPrice: decimal.RequireFromString("2.50"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPostingAmount(t, normalization.Transaction, "settlement-cash", "500")
}

func TestNewCashSettlementEconomicNormalizationFailsClosed(t *testing.T) {
	for name, mutate := range map[string]func(*CashSettlementEconomicEventInput){
		"zero position": func(input *CashSettlementEconomicEventInput) { input.PositionQuantity = decimal.Zero },
		"off lot": func(input *CashSettlementEconomicEventInput) {
			input.PositionQuantity = decimal.RequireFromString("1.5")
		},
		"negative price": func(input *CashSettlementEconomicEventInput) { input.SettlementPrice = decimal.NewFromInt(-1) },
		"off tick": func(input *CashSettlementEconomicEventInput) {
			input.SettlementPrice = decimal.RequireFromString("0.501")
		},
		"fractional payout": func(input *CashSettlementEconomicEventInput) {
			input.SettlementPrice = decimal.RequireFromString("0.5")
		},
		"foreign currency": func(input *CashSettlementEconomicEventInput) { input.VenueContract.Currency = "EUR" },
		"future contract": func(input *CashSettlementEconomicEventInput) {
			input.VenueContract.ValidFrom = input.Base.EffectiveAt.Add(time.Second)
		},
		"wrong mechanics": func(input *CashSettlementEconomicEventInput) {
			input.VenueContract.SettlementMethod = instrument.SettlementCash
		},
		"quarantined": func(input *CashSettlementEconomicEventInput) {
			input.Instrument.Status = instrument.StatusQuarantined
			input.Instrument.Metadata = json.RawMessage(`{"reason":"unknown"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newPredictionSettlementFixture(t)
			input := CashSettlementEconomicEventInput{
				Base: fixture.base, Kind: CashSettlementPrediction,
				Instrument: fixture.primary, VenueContract: fixture.contract,
				PositionQuantity: decimal.NewFromInt(2), SettlementPrice: decimal.NewFromInt(1),
			}
			mutate(&input)
			if _, err := NewCashSettlementEconomicNormalization(input); err == nil {
				t.Fatal("NewCashSettlementEconomicNormalization() unexpectedly succeeded")
			}
		})
	}
}

func TestNewCashSettlementEconomicNormalizationRejectsWrongOptionMechanics(t *testing.T) {
	fixture := newOptionSettlementFixture(t, instrument.SettlementPhysical)
	input := CashSettlementEconomicEventInput{
		Base: fixture.base, Kind: CashSettlementOption,
		Instrument: fixture.primary, VenueContract: fixture.contract,
		PositionQuantity: decimal.NewFromInt(1), SettlementPrice: decimal.NewFromInt(1),
	}
	if _, err := NewCashSettlementEconomicNormalization(input); err == nil {
		t.Fatal("cash option settlement under physical mechanics unexpectedly succeeded")
	}
}

func TestNewPhysicalOptionEconomicNormalizationSignMatrix(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		action          PhysicalOptionAction
		contractType    instrument.OptionContractType
		position        string
		wantOptionClose string
		wantUnderlying  string
		wantStrikeCash  string
	}{
		{"long call exercise", PhysicalOptionExercise, instrument.OptionContractCall, "2", "-2", "200", "-25000"},
		{"long put exercise", PhysicalOptionExercise, instrument.OptionContractPut, "2", "-2", "-200", "25000"},
		{"short call assignment", PhysicalOptionAssignment, instrument.OptionContractCall, "-2", "2", "-200", "25000"},
		{"short put assignment", PhysicalOptionAssignment, instrument.OptionContractPut, "-2", "2", "200", "-25000"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newPhysicalOptionFixture(t, testCase.contractType)
			normalization, err := NewPhysicalOptionEconomicNormalization(PhysicalOptionEconomicEventInput{
				Base: fixture.base, Action: testCase.action,
				OptionInstrument: fixture.primary, UnderlyingInstrument: fixture.secondary,
				VenueContract: fixture.contract, OptionTerms: fixture.terms,
				PositionQuantity: decimal.RequireFromString(testCase.position),
			})
			if err != nil {
				t.Fatal(err)
			}
			assertPostingAmount(t, normalization.Transaction, "option-close", testCase.wantOptionClose)
			assertPostingAmount(t, normalization.Transaction, "underlying-delivery", testCase.wantUnderlying)
			assertPostingAmount(t, normalization.Transaction, "strike-cash", testCase.wantStrikeCash)
			if err := normalization.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestNewPhysicalOptionEconomicNormalizationAcceptsHistoricalExpiredContract(t *testing.T) {
	fixture := newPhysicalOptionFixture(t, instrument.OptionContractCall)
	ended := fixture.base.EffectiveAt.Add(-time.Second)
	fixture.contract.ValidTo = &ended
	_, err := NewPhysicalOptionEconomicNormalization(PhysicalOptionEconomicEventInput{
		Base: fixture.base, Action: PhysicalOptionExercise,
		OptionInstrument: fixture.primary, UnderlyingInstrument: fixture.secondary,
		VenueContract: fixture.contract, OptionTerms: fixture.terms,
		PositionQuantity: decimal.NewFromInt(1),
	})
	if err != nil {
		t.Fatalf("historical physical settlement failed: %v", err)
	}
}

func TestNewPhysicalOptionEconomicNormalizationFailsClosed(t *testing.T) {
	for name, mutate := range map[string]func(*PhysicalOptionEconomicEventInput){
		"wrong underlying": func(input *PhysicalOptionEconomicEventInput) {
			input.OptionTerms.UnderlyingInstrumentID = uuid.New()
		},
		"cash contract": func(input *PhysicalOptionEconomicEventInput) {
			input.VenueContract.SettlementMethod = instrument.SettlementCash
		},
		"multiplier mismatch": func(input *PhysicalOptionEconomicEventInput) {
			input.VenueContract.Multiplier = decimal.NewFromInt(50)
		},
		"foreign strike": func(input *PhysicalOptionEconomicEventInput) {
			input.OptionTerms.StrikeCurrency = "EUR"
		},
		"future terms": func(input *PhysicalOptionEconomicEventInput) {
			input.OptionTerms.EffectiveAt = input.Base.EffectiveAt.Add(time.Second)
		},
		"unobserved terms": func(input *PhysicalOptionEconomicEventInput) {
			input.OptionTerms.ObservedAt = input.Base.SourceEvent.ObservedAt.Add(time.Second)
		},
		"future contract": func(input *PhysicalOptionEconomicEventInput) {
			input.VenueContract.ValidFrom = input.Base.EffectiveAt.Add(time.Second)
		},
		"underlying off lot": func(input *PhysicalOptionEconomicEventInput) {
			input.UnderlyingInstrument.LotSize = decimal.NewFromInt(3)
		},
		"exercise short": func(input *PhysicalOptionEconomicEventInput) {
			input.PositionQuantity = decimal.NewFromInt(-1)
		},
		"quarantined option": func(input *PhysicalOptionEconomicEventInput) {
			input.OptionInstrument.Status = instrument.StatusQuarantined
			input.OptionInstrument.Metadata = json.RawMessage(`{"reason":"unknown"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newPhysicalOptionFixture(t, instrument.OptionContractCall)
			input := PhysicalOptionEconomicEventInput{
				Base: fixture.base, Action: PhysicalOptionExercise,
				OptionInstrument: fixture.primary, UnderlyingInstrument: fixture.secondary,
				VenueContract: fixture.contract, OptionTerms: fixture.terms,
				PositionQuantity: decimal.NewFromInt(1),
			}
			mutate(&input)
			if _, err := NewPhysicalOptionEconomicNormalization(input); err == nil {
				t.Fatal("NewPhysicalOptionEconomicNormalization() unexpectedly succeeded")
			}
		})
	}
}

func TestEconomicNormalizationValidateRejectsForgedPosting(t *testing.T) {
	fixture := newEconomicNormalizationFixture(t)
	normalization, err := NewFillEconomicNormalization(FillEconomicEventInput{
		Base: fixture.base, Instrument: fixture.primary, VenueContract: fixture.contract,
		Side: FillSideBuy, Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	normalization.Transaction.Postings[0].Amount = decimal.NewFromInt(999)
	if err := normalization.Validate(); err == nil {
		t.Fatal("Validate() accepted a forged posting amount")
	}
}

type economicNormalizationFixture struct {
	account  *domain.Account
	source   *EconomicSourceEvent
	base     EconomicNormalizationBaseInput
	primary  instrument.Instrument
	contract instrument.VenueContract
}

type physicalOptionFixture struct {
	economicNormalizationFixture
	secondary instrument.Instrument
	terms     instrument.OptionContractTerms
}

func newEconomicNormalizationFixture(t *testing.T) economicNormalizationFixture {
	t.Helper()
	createdAt := time.Date(2026, time.August, 15, 14, 0, 0, 0, time.UTC)
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "Economic test", Environment: domain.AccountEnvironmentPaperScored,
		Venue: "alpaca", BaseCurrency: "USD", StorageNamespace: "paper_scored/economic-test",
		StartingCapital: decimal.NewFromInt(10_000), BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile: domain.MarginProfileCash, CreatedBy: "test", CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	effectiveAt := createdAt.Add(time.Hour)
	source, err := NewEconomicSourceEvent(EconomicSourceEventInput{
		AccountID: account.ID, Source: "simulator", SourceNamespace: "fills/run-1", SourceEventID: "event-1",
		SourceRevision: "v1", ObservedAt: effectiveAt.Add(time.Second), RawPayload: json.RawMessage(`{"event":"event-1"}`), CreatedAt: effectiveAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "figi:TEST", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "alpaca",
		Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1),
		Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical,
		Status: instrument.StatusActive, CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	validTo := effectiveAt.Add(time.Hour)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID: primary.ID, Venue: "alpaca", ContractID: "TEST", Currency: "USD",
		TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, ValidFrom: effectiveAt.Add(-time.Hour), ValidTo: &validTo, CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return economicNormalizationFixture{
		account: account,
		source:  source,
		base: EconomicNormalizationBaseInput{
			SourceEvent: source, Account: account, NormalizerVersion: "economic_event_v1",
			ExecutionOriginType: ExecutionOriginStrategyVersion, ExecutionOriginID: "strategy-version-1",
			ReferenceType: "fill", ReferenceID: "fill-1", EffectiveAt: effectiveAt,
		},
		primary:  *primary,
		contract: *contract,
	}
}

func newPredictionSettlementFixture(t *testing.T) economicNormalizationFixture {
	t.Helper()
	fixture := newEconomicNormalizationFixture(t)
	fixture.primary.AssetClass = instrument.AssetClassPredictionContract
	fixture.primary.SettlementMethod = instrument.SettlementBinary
	fixture.contract.SettlementMethod = instrument.SettlementBinary
	fixture.base.ReferenceType = "settlement"
	fixture.base.ReferenceID = "prediction-resolution-1"
	fixture.base.ExecutionOriginType = ExecutionOriginSettlement
	fixture.base.ExecutionOriginID = "settlement-batch-1"
	return fixture
}

func newOptionSettlementFixture(t *testing.T, method instrument.SettlementMethod) economicNormalizationFixture {
	t.Helper()
	fixture := newEconomicNormalizationFixture(t)
	underlyingID := uuid.New()
	expiration := fixture.base.EffectiveAt.Add(-time.Hour)
	fixture.primary.AssetClass = instrument.AssetClassOption
	fixture.primary.IdentityKey = "occ:TEST"
	fixture.primary.Multiplier = decimal.NewFromInt(100)
	fixture.primary.Expiration = &expiration
	fixture.primary.ExerciseStyle = instrument.ExerciseAmerican
	fixture.primary.SettlementMethod = method
	fixture.primary.UnderlyingID = &underlyingID
	fixture.primary.Status = instrument.StatusExpired
	fixture.contract.InstrumentID = fixture.primary.ID
	fixture.contract.Multiplier = decimal.NewFromInt(100)
	fixture.contract.SettlementMethod = method
	fixture.base.ReferenceType = "settlement"
	fixture.base.ReferenceID = "option-settlement-1"
	fixture.base.ExecutionOriginType = ExecutionOriginSettlement
	fixture.base.ExecutionOriginID = "settlement-batch-1"
	return fixture
}

func newPhysicalOptionFixture(t *testing.T, contractType instrument.OptionContractType) physicalOptionFixture {
	t.Helper()
	fixture := newOptionSettlementFixture(t, instrument.SettlementPhysical)
	underlying, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "figi:UNDERLYING", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "alpaca",
		Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1),
		Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical,
		Status: instrument.StatusActive, CreatedAt: fixture.account.CreatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.primary.UnderlyingID = &underlying.ID
	terms, err := instrument.NewOptionContractTerms(instrument.OptionContractTermsInput{
		OptionInstrumentID: fixture.primary.ID, UnderlyingInstrumentID: underlying.ID,
		ContractType: contractType, StrikePrice: decimal.NewFromInt(125), StrikeCurrency: "USD",
		DeliverableQuantity: decimal.NewFromInt(100), Source: "occ-feed", SourceNamespace: "option/terms",
		SourceRecordID: "terms-1", SourceRevision: "v1",
		EffectiveAt: fixture.base.EffectiveAt.Add(-2 * time.Hour),
		ObservedAt:  fixture.base.EffectiveAt.Add(-time.Hour),
		RawPayload:  json.RawMessage(`{"terms":"terms-1"}`), Metadata: json.RawMessage(`{"authority":"occ"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return physicalOptionFixture{
		economicNormalizationFixture: fixture,
		secondary:                    *underlying,
		terms:                        *terms,
	}
}

func postingByKey(t *testing.T, transaction *Transaction, key string) Posting {
	t.Helper()
	for _, posting := range transaction.Postings {
		if posting.IdempotencyKey == key {
			return posting
		}
	}
	t.Fatalf("posting %q not found in %+v", key, transaction.Postings)
	return Posting{}
}

func assertPostingAmount(t *testing.T, transaction *Transaction, key, want string) {
	t.Helper()
	posting := postingByKey(t, transaction, key)
	if posting.Amount.String() != want {
		t.Fatalf("posting %q amount = %s, want %s", key, posting.Amount, want)
	}
}

func hasPosting(transaction *Transaction, key string) bool {
	for _, posting := range transaction.Postings {
		if posting.IdempotencyKey == key {
			return true
		}
	}
	return false
}
