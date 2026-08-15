package ledger

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

func TestBuildPortfolioProjectionDepositAndMarkedLong(t *testing.T) {
	t.Parallel()

	fixture := newEconomicNormalizationFixture(t)
	deposit := projectionCapitalTransaction(t, fixture.account.ID, fixture.account.BaseCurrency, "deposit", "1000", fixture.base.EffectiveAt.Add(-time.Minute))
	fill := projectionFill(t, fixture, "buy-1", fixture.base.EffectiveAt, FillSideBuy, "1", "10", &CostComponent{
		Kind: CostKindFee, Currency: "USD", Amount: decimal.RequireFromString("0.25"),
	})
	mechanics := projectionMechanics(t, fill)
	mark := projectionMark(t, fixture.primary.ID, "12", fixture.base.EffectiveAt.Add(10*time.Minute), "mark-1")

	projection, err := BuildPortfolioProjection(ProjectionInput{
		Request:      projectionRequest(fixture.account.ID, fixture.base.EffectiveAt.Add(20*time.Minute)),
		BaseCurrency: fixture.account.BaseCurrency,
		Transactions: []*Transaction{fill.Transaction, deposit},
		Mechanics:    []ProjectionMechanics{mechanics},
		Marks:        []*MarkObservation{mark},
	})
	if err != nil {
		t.Fatalf("BuildPortfolioProjection() error = %v", err)
	}
	assertProjectionDecimal(t, "cash", projection.Totals.Cash, "989.75")
	assertProjectionDecimal(t, "net capital", projection.Totals.NetCapital, "1000")
	assertProjectionDecimal(t, "fees", projection.Totals.Fees, "0.25")
	assertProjectionDecimal(t, "realized", projection.Totals.RealizedPnL, "0")
	assertProjectionDecimal(t, "market value", projection.Totals.MarketValue, "12")
	assertProjectionDecimal(t, "unrealized", projection.Totals.UnrealizedPnL, "1.75")
	assertProjectionDecimal(t, "equity", projection.Totals.Equity, "1001.75")
	assertProjectionDecimal(t, "total P&L", projection.Totals.TotalPnL, "1.75")
	if len(projection.Lots) != 1 || projection.Lots[0].InstrumentID != fixture.primary.ID {
		t.Fatalf("lots = %+v, want one primary-instrument lot", projection.Lots)
	}
	assertProjectionDecimal(t, "lot quantity", projection.Lots[0].RemainingQuantity, "1")
	assertProjectionDecimal(t, "lot opening cash", projection.Lots[0].RemainingOpeningCash, "-10.25")
	if len(projection.Positions) != 1 || !projection.Positions[0].Open {
		t.Fatalf("positions = %+v, want one open position", projection.Positions)
	}
	if projection.ThroughTransactionID != fill.Transaction.ID || projection.TransactionCount != 2 {
		t.Fatalf("checkpoint boundary = %s/%d, want %s/2", projection.ThroughTransactionID, projection.TransactionCount, fill.Transaction.ID)
	}
	if projection.InputChecksum == "" || projection.OutputChecksum == "" || len(projection.PayloadBytes) == 0 {
		t.Fatal("projection did not produce canonical evidence")
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(projection.PayloadBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("canonical payload is not JSON: %v", err)
	}
}

func TestBuildPortfolioProjectionMatchesFIFOAcrossLotsAndCrossesDirection(t *testing.T) {
	t.Parallel()

	fixture := newEconomicNormalizationFixture(t)
	base := fixture.base.EffectiveAt
	deposit := projectionCapitalTransaction(t, fixture.account.ID, "USD", "deposit", "1000", base.Add(-time.Minute))
	buyOne := projectionFill(t, fixture, "buy-1", base, FillSideBuy, "2", "10", &CostComponent{
		Kind: CostKindFee, Currency: "USD", Amount: decimal.RequireFromString("0.2"),
	})
	buyTwo := projectionFill(t, fixture, "buy-2", base.Add(time.Minute), FillSideBuy, "1", "12", nil)
	sell := projectionFill(t, fixture, "sell-1", base.Add(2*time.Minute), FillSideSell, "4", "15", &CostComponent{
		Kind: CostKindRebate, Currency: "USD", Amount: decimal.RequireFromString("0.05"),
	})
	mark := projectionMark(t, fixture.primary.ID, "12", base.Add(3*time.Minute), "mark-1")

	projection, err := BuildPortfolioProjection(ProjectionInput{
		Request:      projectionRequest(fixture.account.ID, base.Add(10*time.Minute)),
		BaseCurrency: "USD",
		Transactions: []*Transaction{sell.Transaction, deposit, buyTwo.Transaction, buyOne.Transaction},
		Mechanics: []ProjectionMechanics{
			projectionMechanics(t, sell), projectionMechanics(t, buyTwo), projectionMechanics(t, buyOne),
		},
		Marks: []*MarkObservation{mark},
	})
	if err != nil {
		t.Fatalf("BuildPortfolioProjection() error = %v", err)
	}
	if len(projection.Matches) != 2 {
		t.Fatalf("match count = %d, want 2", len(projection.Matches))
	}
	assertProjectionDecimal(t, "first FIFO match quantity", projection.Matches[0].Quantity, "2")
	assertProjectionDecimal(t, "first FIFO opening cash", projection.Matches[0].OpeningCash, "-20.2")
	assertProjectionDecimal(t, "first FIFO closing cash", projection.Matches[0].ClosingCash, "30.025")
	assertProjectionDecimal(t, "first FIFO realized", projection.Matches[0].RealizedPnL, "9.825")
	assertProjectionDecimal(t, "second FIFO match quantity", projection.Matches[1].Quantity, "1")
	assertProjectionDecimal(t, "second FIFO closing cash", projection.Matches[1].ClosingCash, "15.0125")
	assertProjectionDecimal(t, "second FIFO realized", projection.Matches[1].RealizedPnL, "3.0125")
	if len(projection.Lots) != 3 {
		t.Fatalf("lot count = %d, want two closed plus one crossed short", len(projection.Lots))
	}
	shortLot := projection.Lots[2]
	assertProjectionDecimal(t, "crossed short quantity", shortLot.RemainingQuantity, "-1")
	assertProjectionDecimal(t, "crossed short opening cash", shortLot.RemainingOpeningCash, "15.0125")
	assertProjectionDecimal(t, "cash", projection.Totals.Cash, "1027.85")
	assertProjectionDecimal(t, "rebates", projection.Totals.Rebates, "0.05")
	assertProjectionDecimal(t, "realized", projection.Totals.RealizedPnL, "12.8375")
	assertProjectionDecimal(t, "market value", projection.Totals.MarketValue, "-12")
	assertProjectionDecimal(t, "unrealized", projection.Totals.UnrealizedPnL, "3.0125")
	assertProjectionDecimal(t, "total P&L", projection.Totals.TotalPnL, "15.85")
	if !projection.Totals.RealizedPnL.Add(projection.Totals.UnrealizedPnL).Equal(projection.Totals.TotalPnL) {
		t.Fatal("projection P&L equation does not reconcile")
	}
}

func TestBuildPortfolioProjectionIsPointInTimeAndByteDeterministic(t *testing.T) {
	t.Parallel()

	fixture := newEconomicNormalizationFixture(t)
	base := fixture.base.EffectiveAt
	deposit := projectionCapitalTransaction(t, fixture.account.ID, "USD", "deposit", "100", base.Add(-time.Minute))
	fill := projectionFill(t, fixture, "buy-1", base, FillSideBuy, "1", "10", nil)
	futureFill := projectionFill(t, fixture, "buy-future", base.Add(30*time.Minute), FillSideBuy, "1", "11", nil)
	oldMark := projectionMark(t, fixture.primary.ID, "12", base.Add(time.Minute), "mark-old")
	futureMark := projectionMark(t, fixture.primary.ID, "99", base.Add(30*time.Minute), "mark-future")
	request := projectionRequest(fixture.account.ID, base.Add(10*time.Minute))
	input := ProjectionInput{
		Request:      request,
		BaseCurrency: "USD",
		Transactions: []*Transaction{futureFill.Transaction, fill.Transaction, deposit},
		Mechanics: []ProjectionMechanics{
			projectionMechanics(t, futureFill), projectionMechanics(t, fill),
		},
		Marks: []*MarkObservation{futureMark, oldMark},
	}
	first, err := BuildPortfolioProjection(input)
	if err != nil {
		t.Fatalf("BuildPortfolioProjection() error = %v", err)
	}
	assertProjectionDecimal(t, "point-in-time quantity", first.Positions[0].Quantity, "1")
	assertProjectionDecimal(t, "point-in-time mark", first.Marks[0].Price, "12")

	random := rand.New(rand.NewSource(42))
	random.Shuffle(len(input.Transactions), func(i, j int) {
		input.Transactions[i], input.Transactions[j] = input.Transactions[j], input.Transactions[i]
	})
	random.Shuffle(len(input.Mechanics), func(i, j int) { input.Mechanics[i], input.Mechanics[j] = input.Mechanics[j], input.Mechanics[i] })
	random.Shuffle(len(input.Marks), func(i, j int) { input.Marks[i], input.Marks[j] = input.Marks[j], input.Marks[i] })
	second, err := BuildPortfolioProjection(input)
	if err != nil {
		t.Fatalf("BuildPortfolioProjection(shuffled) error = %v", err)
	}
	if first.InputChecksum != second.InputChecksum || first.OutputChecksum != second.OutputChecksum ||
		!bytes.Equal(first.PayloadBytes, second.PayloadBytes) {
		t.Fatalf("shuffled rebuild changed evidence: input %s/%s output %s/%s", first.InputChecksum, second.InputChecksum, first.OutputChecksum, second.OutputChecksum)
	}
}

func TestBuildPortfolioProjectionAllocatesExactResidualAndStandaloneCosts(t *testing.T) {
	t.Parallel()

	fixture := newEconomicNormalizationFixture(t)
	base := fixture.base.EffectiveAt
	deposit := projectionCapitalTransaction(t, fixture.account.ID, "USD", "deposit", "100", base.Add(-time.Minute))
	transactions := []*Transaction{deposit}
	mechanics := make([]ProjectionMechanics, 0, 6)
	for index := range 3 {
		fill := projectionFill(t, fixture, "zero-buy-"+decimal.NewFromInt(int64(index)).String(), base.Add(time.Duration(index)*time.Minute), FillSideBuy, "1", "0", nil)
		transactions = append(transactions, fill.Transaction)
		mechanics = append(mechanics, projectionMechanics(t, fill))
	}
	closingFill := projectionFill(t, fixture, "rebated-close", base.Add(4*time.Minute), FillSideSell, "3", "0", &CostComponent{
		Kind: CostKindRebate, Currency: "USD", Amount: decimal.NewFromInt(1),
	})
	standaloneFee := projectionCost(t, fixture, "standalone-fee", base.Add(5*time.Minute), CostKindFee, "0.1")
	transactions = append(transactions, closingFill.Transaction, standaloneFee.Transaction)
	mechanics = append(mechanics, projectionMechanics(t, closingFill), projectionMechanics(t, standaloneFee))

	projection, err := BuildPortfolioProjection(ProjectionInput{
		Request: projectionRequest(fixture.account.ID, base.Add(10*time.Minute)), BaseCurrency: "USD",
		Transactions: transactions, Mechanics: mechanics,
	})
	if err != nil {
		t.Fatalf("BuildPortfolioProjection() error = %v", err)
	}
	if len(projection.Matches) != 3 {
		t.Fatalf("match count = %d, want 3", len(projection.Matches))
	}
	assertProjectionDecimal(t, "first residual allocation", projection.Matches[0].ClosingCash, "0.333333333333")
	assertProjectionDecimal(t, "second residual allocation", projection.Matches[1].ClosingCash, "0.333333333333")
	assertProjectionDecimal(t, "final exact residual", projection.Matches[2].ClosingCash, "0.333333333334")
	allocated := projection.Matches[0].ClosingCash.Add(projection.Matches[1].ClosingCash).Add(projection.Matches[2].ClosingCash)
	assertProjectionDecimal(t, "conserved closing cash", allocated, "1")
	assertProjectionDecimal(t, "cash", projection.Totals.Cash, "100.9")
	assertProjectionDecimal(t, "fees", projection.Totals.Fees, "0.1")
	assertProjectionDecimal(t, "rebates", projection.Totals.Rebates, "1")
	assertProjectionDecimal(t, "realized", projection.Totals.RealizedPnL, "0.9")
	assertProjectionDecimal(t, "total", projection.Totals.TotalPnL, "0.9")
}

func TestBuildPortfolioProjectionSettlesCashExpirationAndPredictionInventory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		fixture        func(*testing.T) economicNormalizationFixture
		openingSide    FillSide
		openingPrice   string
		position       string
		settlementKind CashSettlementKind
		settlement     string
		wantRealized   string
	}{
		{
			name: "cash option", fixture: func(t *testing.T) economicNormalizationFixture {
				value := newOptionSettlementFixture(t, instrument.SettlementCash)
				value.primary.Status = instrument.StatusActive
				return value
			}, openingSide: FillSideBuy, openingPrice: "2", position: "1",
			settlementKind: CashSettlementOption, settlement: "3", wantRealized: "100",
		},
		{
			name: "zero expiration", fixture: func(t *testing.T) economicNormalizationFixture {
				value := newOptionSettlementFixture(t, instrument.SettlementPhysical)
				value.primary.Status = instrument.StatusActive
				return value
			}, openingSide: FillSideBuy, openingPrice: "2", position: "1",
			settlementKind: CashSettlementExpiration, settlement: "0", wantRealized: "-200",
		},
		{
			name: "prediction winner", fixture: newPredictionSettlementFixture,
			openingSide: FillSideBuy, openingPrice: "0.4", position: "1",
			settlementKind: CashSettlementPrediction, settlement: "1", wantRealized: "0.6",
		},
		{
			name: "prediction short liability", fixture: newPredictionSettlementFixture,
			openingSide: FillSideSell, openingPrice: "0.6", position: "-1",
			settlementKind: CashSettlementPrediction, settlement: "1", wantRealized: "-0.4",
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := testCase.fixture(t)
			base := fixture.base.EffectiveAt
			openingFixture := fixture
			openingFixture.primary.Status = instrument.StatusActive
			opening := projectionFill(t, openingFixture, "opening", base.Add(-time.Minute), testCase.openingSide, "1", testCase.openingPrice, nil)
			settlementFixture := fixture
			if settlementFixture.primary.AssetClass == instrument.AssetClassOption {
				settlementFixture.primary.Status = instrument.StatusExpired
			}
			settlement := projectionCashSettlement(t, settlementFixture, "settlement", base, testCase.settlementKind, testCase.position, testCase.settlement)
			deposit := projectionCapitalTransaction(t, fixture.account.ID, "USD", "deposit", "1000", base.Add(-2*time.Minute))
			projection, err := BuildPortfolioProjection(ProjectionInput{
				Request: projectionRequest(fixture.account.ID, base.Add(10*time.Minute)), BaseCurrency: "USD",
				Transactions: []*Transaction{settlement.Transaction, opening.Transaction, deposit},
				Mechanics:    []ProjectionMechanics{projectionMechanics(t, settlement), projectionMechanics(t, opening)},
			})
			if err != nil {
				t.Fatalf("BuildPortfolioProjection() error = %v", err)
			}
			assertProjectionDecimal(t, "realized", projection.Totals.RealizedPnL, testCase.wantRealized)
			assertProjectionDecimal(t, "unrealized", projection.Totals.UnrealizedPnL, "0")
			assertProjectionDecimal(t, "total", projection.Totals.TotalPnL, testCase.wantRealized)
			if len(projection.Positions) != 1 || projection.Positions[0].Open || len(projection.Marks) != 0 {
				t.Fatalf("settled projection position/marks = %+v/%+v", projection.Positions, projection.Marks)
			}
		})
	}
}

func TestBuildPortfolioProjectionTransfersPhysicalOptionBasisInAllFourCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		contractType    instrument.OptionContractType
		openingSide     FillSide
		position        string
		action          PhysicalOptionAction
		underlyingMark  string
		wantQuantity    string
		wantOpeningCash string
		wantUnrealized  string
	}{
		{"long call exercise", instrument.OptionContractCall, FillSideBuy, "1", PhysicalOptionExercise, "130", "100", "-12700", "300"},
		{"long put exercise", instrument.OptionContractPut, FillSideBuy, "1", PhysicalOptionExercise, "120", "-100", "12300", "300"},
		{"short call assignment", instrument.OptionContractCall, FillSideSell, "-1", PhysicalOptionAssignment, "120", "-100", "12700", "700"},
		{"short put assignment", instrument.OptionContractPut, FillSideSell, "-1", PhysicalOptionAssignment, "120", "100", "-12300", "-300"},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newPhysicalOptionFixture(t, testCase.contractType)
			base := fixture.base.EffectiveAt
			openingFixture := fixture.economicNormalizationFixture
			openingFixture.primary.Status = instrument.StatusActive
			opening := projectionFill(t, openingFixture, "option-opening", base.Add(-time.Minute), testCase.openingSide, "1", "2", nil)
			physical := projectionPhysicalSettlement(t, fixture, "physical-settlement", base, testCase.action, testCase.position)
			deposit := projectionCapitalTransaction(t, fixture.account.ID, "USD", "deposit", "20000", base.Add(-2*time.Minute))
			mark := projectionMark(t, fixture.secondary.ID, testCase.underlyingMark, base.Add(time.Minute), "underlying-mark")
			projection, err := BuildPortfolioProjection(ProjectionInput{
				Request: projectionRequest(fixture.account.ID, base.Add(10*time.Minute)), BaseCurrency: "USD",
				Transactions: []*Transaction{physical.Transaction, opening.Transaction, deposit},
				Mechanics:    []ProjectionMechanics{projectionMechanics(t, physical), projectionMechanics(t, opening)},
				Marks:        []*MarkObservation{mark},
			})
			if err != nil {
				t.Fatalf("BuildPortfolioProjection() error = %v", err)
			}
			optionPosition := projectionPositionByInstrument(t, projection, fixture.primary.ID)
			underlyingPosition := projectionPositionByInstrument(t, projection, fixture.secondary.ID)
			if optionPosition.Open || !underlyingPosition.Open {
				t.Fatalf("physical position states = option %t underlying %t", optionPosition.Open, underlyingPosition.Open)
			}
			assertProjectionDecimal(t, "underlying quantity", underlyingPosition.Quantity, testCase.wantQuantity)
			assertProjectionDecimal(t, "underlying opening cash", underlyingPosition.RemainingOpeningCash, testCase.wantOpeningCash)
			assertProjectionDecimal(t, "realized", projection.Totals.RealizedPnL, "0")
			assertProjectionDecimal(t, "unrealized", projection.Totals.UnrealizedPnL, testCase.wantUnrealized)
			assertProjectionDecimal(t, "total", projection.Totals.TotalPnL, testCase.wantUnrealized)
			if len(projection.Matches) != 1 || projection.Matches[0].Disposition != "basis_transfer" ||
				!projection.Matches[0].RealizedPnL.IsZero() {
				t.Fatalf("physical transfer match = %+v", projection.Matches)
			}
			underlyingLot := projectionOpenLotByInstrument(t, projection, fixture.secondary.ID)
			assertProjectionDecimal(t, "delivered multiplier", underlyingLot.MarkMultiplier, "1")
		})
	}
}

func TestBuildPortfolioProjectionFailsClosedForUnsupportedOrIncompleteEvidence(t *testing.T) {
	t.Parallel()

	fixture := newEconomicNormalizationFixture(t)
	base := fixture.base.EffectiveAt
	deposit := projectionCapitalTransaction(t, fixture.account.ID, "USD", "deposit", "100", base.Add(-time.Minute))
	fill := projectionFill(t, fixture, "buy", base, FillSideBuy, "1", "10", nil)
	mechanics := projectionMechanics(t, fill)
	validMark := projectionMark(t, fixture.primary.ID, "12", base.Add(time.Minute), "mark")
	request := projectionRequest(fixture.account.ID, base.Add(10*time.Minute))

	tests := map[string]func(*ProjectionInput){
		"missing mechanics": func(input *ProjectionInput) { input.Mechanics = nil },
		"missing mark":      func(input *ProjectionInput) { input.Marks = nil },
		"stale mark": func(input *ProjectionInput) {
			input.Request.MaxMarkAge = time.Minute
		},
		"wrong mark currency": func(input *ProjectionInput) {
			changed := *validMark
			changed.PriceCurrency = "EUR"
			changed.ID = markObservationID(&changed)
			input.Marks = []*MarkObservation{&changed}
		},
		"forged physical close": func(input *ProjectionInput) {
			changed := mechanics
			changed.EventType = EconomicEventOptionExpiration
			input.Mechanics = []ProjectionMechanics{changed}
		},
		"forged mechanics price": func(input *ProjectionInput) {
			changed := mechanics
			forged := decimal.NewFromInt(999)
			changed.Price = &forged
			input.Mechanics = []ProjectionMechanics{changed}
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := ProjectionInput{
				Request: request, BaseCurrency: "USD", Transactions: []*Transaction{deposit, fill.Transaction},
				Mechanics: []ProjectionMechanics{mechanics}, Marks: []*MarkObservation{validMark},
			}
			mutate(&input)
			if _, err := BuildPortfolioProjection(input); err == nil {
				t.Fatal("BuildPortfolioProjection() unexpectedly succeeded")
			}
		})
	}

	unknown := *deposit
	unknown.ID = uuid.New()
	unknown.EventType = "corporate_action.dividend"
	for index := range unknown.Postings {
		unknown.Postings[index].TransactionID = unknown.ID
		unknown.Postings[index].ID = uuid.New()
	}
	if _, err := BuildPortfolioProjection(ProjectionInput{
		Request: request, BaseCurrency: "USD", Transactions: []*Transaction{&unknown},
	}); err == nil {
		t.Fatal("BuildPortfolioProjection() accepted an unknown transaction type")
	}
}

func FuzzAllocateProjectionAmountConservesResidual(f *testing.F) {
	f.Add(int64(1_000_000), uint8(3), uint8(1))
	f.Add(int64(-9_999_999), uint8(7), uint8(6))
	f.Fuzz(func(t *testing.T, rawTotal int64, rawParts, rawIndex uint8) {
		parts := int(rawParts%20) + 1
		index := int(rawIndex) % parts
		total := decimal.NewFromInt(rawTotal).Div(decimal.NewFromInt(1_000_000))
		remainingAmount := total
		remainingQuantity := decimal.NewFromInt(int64(parts))
		allocated := decimal.Zero
		for current := range parts {
			value := allocateProjectionAmount(
				total, decimal.NewFromInt(1), decimal.NewFromInt(int64(parts)),
				&remainingAmount, &remainingQuantity, current == parts-1,
			)
			allocated = allocated.Add(value)
			if current == index && !validProjectionDecimal(value) {
				t.Fatalf("allocation %s exceeds projection precision", value)
			}
		}
		if !allocated.Equal(total) || !remainingAmount.IsZero() || !remainingQuantity.IsZero() {
			t.Fatalf("allocated %s remaining %s/%s, want %s/0/0", allocated, remainingAmount, remainingQuantity, total)
		}
	})
}

func projectionRequest(accountID uuid.UUID, asOf time.Time) ProjectionRequest {
	return ProjectionRequest{
		AccountID: accountID, AsOf: asOf, MarkSource: "polygon",
		MarkNamespace: "consolidated/mark", MaxMarkAge: time.Hour,
	}
}

func projectionCapitalTransaction(t *testing.T, accountID uuid.UUID, currency, kind, amount string, effectiveAt time.Time) *Transaction {
	t.Helper()
	value := decimal.RequireFromString(amount)
	eventType := "capital_flow.deposit"
	if kind == "withdrawal" {
		eventType = "capital_flow.withdrawal"
		value = value.Neg()
	}
	transaction, err := NewTransaction(TransactionInput{
		AccountID: accountID, EventType: eventType, IdempotencyKey: kind + ":" + effectiveAt.Format(time.RFC3339Nano),
		OriginType: "capital_flow", OriginID: kind + "-1", ReferenceType: "capital_flow", ReferenceID: kind + "-1",
		EffectiveAt: effectiveAt, ObservedAt: effectiveAt.Add(time.Microsecond), Metadata: json.RawMessage(`{"source":"test"}`),
		Postings: []PostingInput{
			{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: UnitKindCurrency, Unit: currency, Amount: value},
			{IdempotencyKey: "capital", LedgerAccount: "equity:contributed_capital", UnitKind: UnitKindCurrency, Unit: currency, Amount: value.Neg()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

func projectionFill(t *testing.T, fixture economicNormalizationFixture, sourceID string, effectiveAt time.Time, side FillSide, quantity, price string, cost *CostComponent) *EconomicNormalization {
	t.Helper()
	source, err := NewEconomicSourceEvent(EconomicSourceEventInput{
		AccountID: fixture.account.ID, Source: "simulator", SourceNamespace: "fills/run-1", SourceEventID: sourceID,
		SourceRevision: "v1", ObservedAt: effectiveAt.Add(time.Second), RawPayload: json.RawMessage(`{"event":"` + sourceID + `"}`),
		CreatedAt: effectiveAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	base := fixture.base
	base.SourceEvent = source
	base.EffectiveAt = effectiveAt
	base.ReferenceID = sourceID
	normalization, err := NewFillEconomicNormalization(FillEconomicEventInput{
		Base: base, Instrument: fixture.primary, VenueContract: fixture.contract, Side: side,
		Quantity: decimal.RequireFromString(quantity), Price: decimal.RequireFromString(price), Cost: cost,
	})
	if err != nil {
		t.Fatal(err)
	}
	return normalization
}

func projectionCost(t *testing.T, fixture economicNormalizationFixture, sourceID string, effectiveAt time.Time, kind CostKind, amount string) *EconomicNormalization {
	t.Helper()
	source := projectionSourceEvent(t, fixture, sourceID, effectiveAt)
	base := fixture.base
	base.SourceEvent = source
	base.EffectiveAt = effectiveAt
	base.ReferenceType = "cost"
	base.ReferenceID = sourceID
	normalization, err := NewCostEconomicNormalization(CostEconomicEventInput{
		Base: base, Kind: kind, Currency: fixture.account.BaseCurrency, Amount: decimal.RequireFromString(amount),
	})
	if err != nil {
		t.Fatal(err)
	}
	return normalization
}

func projectionCashSettlement(t *testing.T, fixture economicNormalizationFixture, sourceID string, effectiveAt time.Time, kind CashSettlementKind, position, price string) *EconomicNormalization {
	t.Helper()
	source := projectionSourceEvent(t, fixture, sourceID, effectiveAt)
	base := fixture.base
	base.SourceEvent = source
	base.EffectiveAt = effectiveAt
	base.ReferenceType = "settlement"
	base.ReferenceID = sourceID
	base.ExecutionOriginType = ExecutionOriginSettlement
	base.ExecutionOriginID = "settlement-batch-1"
	normalization, err := NewCashSettlementEconomicNormalization(CashSettlementEconomicEventInput{
		Base: base, Kind: kind, Instrument: fixture.primary, VenueContract: fixture.contract,
		PositionQuantity: decimal.RequireFromString(position), SettlementPrice: decimal.RequireFromString(price),
	})
	if err != nil {
		t.Fatal(err)
	}
	return normalization
}

func projectionPhysicalSettlement(t *testing.T, fixture physicalOptionFixture, sourceID string, effectiveAt time.Time, action PhysicalOptionAction, position string) *EconomicNormalization {
	t.Helper()
	source := projectionSourceEvent(t, fixture.economicNormalizationFixture, sourceID, effectiveAt)
	base := fixture.base
	base.SourceEvent = source
	base.EffectiveAt = effectiveAt
	base.ReferenceType = "settlement"
	base.ReferenceID = sourceID
	base.ExecutionOriginType = ExecutionOriginSettlement
	base.ExecutionOriginID = "settlement-batch-1"
	normalization, err := NewPhysicalOptionEconomicNormalization(PhysicalOptionEconomicEventInput{
		Base: base, Action: action, OptionInstrument: fixture.primary, UnderlyingInstrument: fixture.secondary,
		VenueContract: fixture.contract, OptionTerms: fixture.terms, PositionQuantity: decimal.RequireFromString(position),
	})
	if err != nil {
		t.Fatal(err)
	}
	return normalization
}

func projectionSourceEvent(t *testing.T, fixture economicNormalizationFixture, sourceID string, effectiveAt time.Time) *EconomicSourceEvent {
	t.Helper()
	source, err := NewEconomicSourceEvent(EconomicSourceEventInput{
		AccountID: fixture.account.ID, Source: "simulator", SourceNamespace: "fills/run-1", SourceEventID: sourceID,
		SourceRevision: "v1", ObservedAt: effectiveAt.Add(time.Second), RawPayload: json.RawMessage(`{"event":"` + sourceID + `"}`),
		CreatedAt: effectiveAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func projectionMechanics(t *testing.T, normalization *EconomicNormalization) ProjectionMechanics {
	t.Helper()
	mechanics, err := ProjectionMechanicsFromNormalization(normalization)
	if err != nil {
		t.Fatal(err)
	}
	return mechanics
}

func projectionMark(t *testing.T, instrumentID uuid.UUID, price string, effectiveAt time.Time, observationID string) *MarkObservation {
	t.Helper()
	mark, err := NewMarkObservation(MarkObservationInput{
		InstrumentID: instrumentID, Price: decimal.RequireFromString(price), PriceCurrency: "USD",
		Source: "polygon", SourceNamespace: "consolidated/mark", SourceObservationID: observationID,
		SourceRevision: "v1", EffectiveAt: effectiveAt, ObservedAt: effectiveAt.Add(time.Second), Metadata: json.RawMessage(`{"quality":"official"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return mark
}

func assertProjectionDecimal(t *testing.T, label string, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(decimal.RequireFromString(want)) {
		t.Fatalf("%s = %s, want %s", label, got, want)
	}
}

func projectionPositionByInstrument(t *testing.T, projection *PortfolioProjection, instrumentID uuid.UUID) ProjectionPosition {
	t.Helper()
	for _, position := range projection.Positions {
		if position.InstrumentID == instrumentID {
			return position
		}
	}
	t.Fatalf("position for instrument %s not found", instrumentID)
	return ProjectionPosition{}
}

func projectionOpenLotByInstrument(t *testing.T, projection *PortfolioProjection, instrumentID uuid.UUID) ProjectionLot {
	t.Helper()
	for _, lot := range projection.Lots {
		if lot.InstrumentID == instrumentID && !lot.RemainingQuantity.IsZero() {
			return lot
		}
	}
	t.Fatalf("open lot for instrument %s not found", instrumentID)
	return ProjectionLot{}
}
