package ledger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

const economicNormalizationIDDomain = "economic-normalization"

// EconomicEventType identifies one canonical economic interpretation.
type EconomicEventType string

const (
	EconomicEventFillBuy              EconomicEventType = "fill.buy"
	EconomicEventFillSell             EconomicEventType = "fill.sell"
	EconomicEventFee                  EconomicEventType = "cost.fee"
	EconomicEventRebate               EconomicEventType = "cost.rebate"
	EconomicEventOptionCashSettlement EconomicEventType = "settlement.option_cash"
	EconomicEventOptionExpiration     EconomicEventType = "settlement.option_expiration"
	EconomicEventOptionExercise       EconomicEventType = "settlement.option_exercise"
	EconomicEventOptionAssignment     EconomicEventType = "settlement.option_assignment"
	EconomicEventPredictionPayout     EconomicEventType = "settlement.prediction_payout"
)

// ExecutionOriginType identifies who or what caused an economic effect,
// independently of the provider/system that reported it.
type ExecutionOriginType string

const (
	ExecutionOriginStrategyVersion    ExecutionOriginType = "strategy_version"
	ExecutionOriginCopySubscription   ExecutionOriginType = "copy_subscription"
	ExecutionOriginPortfolioRebalance ExecutionOriginType = "portfolio_rebalance"
	ExecutionOriginRiskReduction      ExecutionOriginType = "risk_reduction"
	ExecutionOriginOperator           ExecutionOriginType = "operator"
	ExecutionOriginSettlement         ExecutionOriginType = "settlement"
	ExecutionOriginReconciliation     ExecutionOriginType = "reconciliation"
)

// FillSide expresses account-perspective inventory direction.
type FillSide string

const (
	FillSideBuy  FillSide = "buy"
	FillSideSell FillSide = "sell"
)

// CostKind distinguishes paid fees from received rebates.
type CostKind string

const (
	CostKindFee    CostKind = "fee"
	CostKindRebate CostKind = "rebate"
)

// CostComponent is one optional fill-attached cost in an explicit currency.
type CostComponent struct {
	Kind     CostKind
	Currency string
	Amount   decimal.Decimal
}

// EconomicNormalization is one typed interpretation of a durable raw source
// event together with the exact ledger aggregate it produces.
type EconomicNormalization struct {
	ID                  uuid.UUID
	SourceEvent         *EconomicSourceEvent
	Account             *domain.Account
	EventType           EconomicEventType
	NormalizerVersion   string
	ExecutionOriginType ExecutionOriginType
	ExecutionOriginID   string
	ReferenceType       string
	ReferenceID         string
	Venue               string
	Instrument          *instrument.Instrument
	SecondaryInstrument *instrument.Instrument
	VenueContract       *instrument.VenueContract
	OptionTerms         *instrument.OptionContractTerms
	EffectiveAt         time.Time
	CashCurrency        string
	Quantity            *decimal.Decimal
	Price               *decimal.Decimal
	CostKind            CostKind
	CostCurrency        string
	CostAmount          *decimal.Decimal
	PositionQuantity    *decimal.Decimal
	SettlementPrice     *decimal.Decimal
	Transaction         *Transaction
	CreatedAt           time.Time
}

// EconomicNormalizationBaseInput contains provenance common to every typed
// normalizer. SourceEvent is expected to have been persisted first.
type EconomicNormalizationBaseInput struct {
	SourceEvent         *EconomicSourceEvent
	Account             *domain.Account
	NormalizerVersion   string
	ExecutionOriginType ExecutionOriginType
	ExecutionOriginID   string
	ReferenceType       string
	ReferenceID         string
	EffectiveAt         time.Time
}

// FillEconomicEventInput supplies exact canonical fill mechanics.
type FillEconomicEventInput struct {
	Base          EconomicNormalizationBaseInput
	Instrument    instrument.Instrument
	VenueContract instrument.VenueContract
	Side          FillSide
	Quantity      decimal.Decimal
	Price         decimal.Decimal
	Cost          *CostComponent
}

// CostEconomicEventInput supplies one standalone cash fee or rebate.
type CostEconomicEventInput struct {
	Base     EconomicNormalizationBaseInput
	Kind     CostKind
	Currency string
	Amount   decimal.Decimal
}

// CashSettlementKind selects the allowed canonical cash/zero settlement rule.
type CashSettlementKind string

const (
	CashSettlementOption     CashSettlementKind = "option_cash"
	CashSettlementExpiration CashSettlementKind = "option_expiration"
	CashSettlementPrediction CashSettlementKind = "prediction_payout"
)

// CashSettlementEconomicEventInput supplies a signed open position and exact
// terminal price under one historical venue contract.
type CashSettlementEconomicEventInput struct {
	Base             EconomicNormalizationBaseInput
	Kind             CashSettlementKind
	Instrument       instrument.Instrument
	VenueContract    instrument.VenueContract
	PositionQuantity decimal.Decimal
	SettlementPrice  decimal.Decimal
}

// PhysicalOptionAction distinguishes long exercise from short assignment.
type PhysicalOptionAction string

const (
	PhysicalOptionExercise   PhysicalOptionAction = "exercise"
	PhysicalOptionAssignment PhysicalOptionAction = "assignment"
)

// PhysicalOptionEconomicEventInput supplies immutable option terms and
// canonical references. Strike, call/put, and deliverable are never fields on
// this input independently of OptionTerms.
type PhysicalOptionEconomicEventInput struct {
	Base                 EconomicNormalizationBaseInput
	Action               PhysicalOptionAction
	OptionInstrument     instrument.Instrument
	UnderlyingInstrument instrument.Instrument
	VenueContract        instrument.VenueContract
	OptionTerms          instrument.OptionContractTerms
	PositionQuantity     decimal.Decimal
}

// NewFillEconomicNormalization converts one fill and optional cost into exact
// inventory/cash/clearing postings.
func NewFillEconomicNormalization(input FillEconomicEventInput) (*EconomicNormalization, error) {
	eventType := EconomicEventFillBuy
	if input.Side == FillSideSell {
		eventType = EconomicEventFillSell
	} else if input.Side != FillSideBuy {
		return nil, fmt.Errorf("fill side %q is invalid", input.Side)
	}
	normalization, err := newEconomicNormalization(input.Base, eventType)
	if err != nil {
		return nil, err
	}
	primary := input.Instrument
	contract := input.VenueContract
	normalization.Venue = contract.Venue
	normalization.Instrument = &primary
	normalization.VenueContract = &contract
	normalization.Quantity = cloneEconomicDecimal(input.Quantity)
	normalization.Price = cloneEconomicDecimal(input.Price)
	if input.Cost != nil {
		normalization.CostKind = input.Cost.Kind
		normalization.CostCurrency = strings.ToUpper(strings.TrimSpace(input.Cost.Currency))
		normalization.CostAmount = cloneEconomicDecimal(input.Cost.Amount)
	}
	if err := normalization.materializeAndValidate(); err != nil {
		return nil, err
	}
	return normalization, nil
}

// NewCostEconomicNormalization converts one standalone fee or rebate into two
// explicit account-base-currency postings.
func NewCostEconomicNormalization(input CostEconomicEventInput) (*EconomicNormalization, error) {
	eventType := EconomicEventFee
	if input.Kind == CostKindRebate {
		eventType = EconomicEventRebate
	} else if input.Kind != CostKindFee {
		return nil, fmt.Errorf("cost kind %q is invalid", input.Kind)
	}
	normalization, err := newEconomicNormalization(input.Base, eventType)
	if err != nil {
		return nil, err
	}
	normalization.CostKind = input.Kind
	normalization.CostCurrency = strings.ToUpper(strings.TrimSpace(input.Currency))
	normalization.CostAmount = cloneEconomicDecimal(input.Amount)
	if err := normalization.materializeAndValidate(); err != nil {
		return nil, err
	}
	return normalization, nil
}

// NewCashSettlementEconomicNormalization extinguishes signed inventory and
// records exact nonzero cash payout or liability when applicable.
func NewCashSettlementEconomicNormalization(input CashSettlementEconomicEventInput) (*EconomicNormalization, error) {
	var eventType EconomicEventType
	switch input.Kind {
	case CashSettlementOption:
		eventType = EconomicEventOptionCashSettlement
	case CashSettlementExpiration:
		eventType = EconomicEventOptionExpiration
	case CashSettlementPrediction:
		eventType = EconomicEventPredictionPayout
	default:
		return nil, fmt.Errorf("cash settlement kind %q is invalid", input.Kind)
	}
	normalization, err := newEconomicNormalization(input.Base, eventType)
	if err != nil {
		return nil, err
	}
	primary := input.Instrument
	contract := input.VenueContract
	normalization.Venue = contract.Venue
	normalization.Instrument = &primary
	normalization.VenueContract = &contract
	normalization.PositionQuantity = cloneEconomicDecimal(input.PositionQuantity)
	normalization.SettlementPrice = cloneEconomicDecimal(input.SettlementPrice)
	if err := normalization.materializeAndValidate(); err != nil {
		return nil, err
	}
	return normalization, nil
}

// NewPhysicalOptionEconomicNormalization closes option inventory and derives
// delivery/cash solely from persisted immutable contract terms.
func NewPhysicalOptionEconomicNormalization(input PhysicalOptionEconomicEventInput) (*EconomicNormalization, error) {
	var eventType EconomicEventType
	switch input.Action {
	case PhysicalOptionExercise:
		eventType = EconomicEventOptionExercise
	case PhysicalOptionAssignment:
		eventType = EconomicEventOptionAssignment
	default:
		return nil, fmt.Errorf("physical option action %q is invalid", input.Action)
	}
	normalization, err := newEconomicNormalization(input.Base, eventType)
	if err != nil {
		return nil, err
	}
	optionInstrument := input.OptionInstrument
	underlyingInstrument := input.UnderlyingInstrument
	contract := input.VenueContract
	terms := input.OptionTerms
	normalization.Venue = contract.Venue
	normalization.Instrument = &optionInstrument
	normalization.SecondaryInstrument = &underlyingInstrument
	normalization.VenueContract = &contract
	normalization.OptionTerms = &terms
	normalization.PositionQuantity = cloneEconomicDecimal(input.PositionQuantity)
	if err := normalization.materializeAndValidate(); err != nil {
		return nil, err
	}
	return normalization, nil
}

func newEconomicNormalization(input EconomicNormalizationBaseInput, eventType EconomicEventType) (*EconomicNormalization, error) {
	if input.SourceEvent == nil || input.Account == nil {
		return nil, fmt.Errorf("economic normalization source event and account are required")
	}
	source := cloneEconomicSourceEvent(input.SourceEvent)
	account := cloneEconomicAccount(input.Account)
	normalizerVersion := strings.TrimSpace(input.NormalizerVersion)
	normalization := &EconomicNormalization{
		SourceEvent:         source,
		Account:             account,
		EventType:           eventType,
		NormalizerVersion:   normalizerVersion,
		ExecutionOriginType: input.ExecutionOriginType,
		ExecutionOriginID:   strings.TrimSpace(input.ExecutionOriginID),
		ReferenceType:       strings.TrimSpace(input.ReferenceType),
		ReferenceID:         strings.TrimSpace(input.ReferenceID),
		EffectiveAt:         input.EffectiveAt.UTC().Truncate(time.Microsecond),
		CashCurrency:        account.BaseCurrency,
		CreatedAt:           time.Now().UTC().Truncate(time.Microsecond),
	}
	normalization.ID = economicid.DeterministicUUID(
		economicNormalizationIDDomain,
		source.ID.String(),
		normalizerVersion,
	)
	return normalization, nil
}

func (normalization *EconomicNormalization) materializeAndValidate() error {
	transaction, err := normalization.expectedTransaction()
	if err != nil {
		return err
	}
	normalization.Transaction = transaction
	return normalization.Validate()
}

// Validate checks common provenance, typed event shape and reference mechanics,
// then independently rebuilds and compares the deterministic ledger aggregate.
func (normalization EconomicNormalization) Validate() error {
	if normalization.SourceEvent == nil || normalization.Account == nil {
		return fmt.Errorf("economic normalization source event and account are required")
	}
	if err := normalization.SourceEvent.Validate(); err != nil {
		return fmt.Errorf("economic normalization source event: %w", err)
	}
	if err := normalization.Account.Validate(); err != nil {
		return fmt.Errorf("economic normalization account: %w", err)
	}
	if normalization.SourceEvent.AccountID != normalization.Account.ID {
		return fmt.Errorf("economic normalization source account does not match account reference")
	}
	if !isNormalizedRequired(normalization.NormalizerVersion) {
		return fmt.Errorf("economic normalization version must be non-empty and normalized")
	}
	if !validExecutionOriginType(normalization.ExecutionOriginType) || !isNormalizedRequired(normalization.ExecutionOriginID) {
		return fmt.Errorf("economic normalization execution origin is invalid or missing")
	}
	if !isNormalizedRequired(normalization.ReferenceType) || !isNormalizedRequired(normalization.ReferenceID) {
		return fmt.Errorf("economic normalization reference type and ID are required and normalized")
	}
	if normalization.EffectiveAt.IsZero() || normalization.EffectiveAt.Location() != time.UTC ||
		!hasPostgresTimestampPrecision(normalization.EffectiveAt) {
		return fmt.Errorf("economic normalization effective time must use normalized UTC microsecond precision")
	}
	if normalization.EffectiveAt.After(normalization.SourceEvent.ObservedAt) {
		return fmt.Errorf("economic normalization effective time cannot follow source observation time")
	}
	if normalization.CreatedAt.IsZero() || normalization.CreatedAt.Location() != time.UTC ||
		!hasPostgresTimestampPrecision(normalization.CreatedAt) {
		return fmt.Errorf("economic normalization creation time must use normalized UTC microsecond precision")
	}
	if normalization.CashCurrency != normalization.Account.BaseCurrency {
		return fmt.Errorf("economic normalization cash currency must match account base currency")
	}
	expectedID := economicid.DeterministicUUID(
		economicNormalizationIDDomain,
		normalization.SourceEvent.ID.String(),
		normalization.NormalizerVersion,
	)
	if normalization.ID != expectedID {
		return fmt.Errorf("economic normalization ID is not deterministic for its source and version")
	}
	expectedTransaction, err := normalization.expectedTransaction()
	if err != nil {
		return err
	}
	if !sameEconomicTransaction(normalization.Transaction, expectedTransaction) {
		return fmt.Errorf("economic normalization ledger aggregate does not match typed event facts")
	}
	return nil
}

func (normalization EconomicNormalization) expectedTransaction() (*Transaction, error) {
	postings, err := normalization.expectedPostings()
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(map[string]string{
		"economic_normalization_id": normalization.ID.String(),
		"execution_origin_id":       normalization.ExecutionOriginID,
		"execution_origin_type":     string(normalization.ExecutionOriginType),
		"normalizer_version":        normalization.NormalizerVersion,
		"raw_payload_sha256":        normalization.SourceEvent.PayloadSHA256,
		"source_event_id":           normalization.SourceEvent.ID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal economic normalization metadata: %w", err)
	}
	return newDeterministicTransaction(normalization.SourceEvent.ID, normalization.NormalizerVersion, TransactionInput{
		AccountID:      normalization.SourceEvent.AccountID,
		EventType:      string(normalization.EventType),
		IdempotencyKey: "economic-source-event:" + normalization.SourceEvent.ID.String(),
		OriginType:     "economic_source_event",
		OriginID:       normalization.SourceEvent.ID.String(),
		ReferenceType:  normalization.ReferenceType,
		ReferenceID:    normalization.ReferenceID,
		EffectiveAt:    normalization.EffectiveAt,
		ObservedAt:     normalization.SourceEvent.ObservedAt,
		Metadata:       metadata,
		Postings:       postings,
	})
}

func (normalization EconomicNormalization) expectedPostings() ([]PostingInput, error) {
	switch normalization.EventType {
	case EconomicEventFillBuy, EconomicEventFillSell:
		return normalization.fillPostings()
	case EconomicEventFee, EconomicEventRebate:
		return normalization.costPostings()
	case EconomicEventOptionCashSettlement, EconomicEventOptionExpiration, EconomicEventPredictionPayout:
		return normalization.cashSettlementPostings()
	case EconomicEventOptionExercise, EconomicEventOptionAssignment:
		return normalization.physicalOptionPostings()
	default:
		return nil, fmt.Errorf("economic event type %q is not supported", normalization.EventType)
	}
}

func (normalization EconomicNormalization) cashSettlementPostings() ([]PostingInput, error) {
	if normalization.Instrument == nil || normalization.VenueContract == nil ||
		normalization.PositionQuantity == nil || normalization.SettlementPrice == nil {
		return nil, fmt.Errorf("cash settlement requires instrument, venue contract, position quantity, and settlement price")
	}
	if normalization.SecondaryInstrument != nil || normalization.OptionTerms != nil ||
		normalization.Quantity != nil || normalization.Price != nil || normalization.CostAmount != nil ||
		normalization.CostKind != "" || normalization.CostCurrency != "" {
		return nil, fmt.Errorf("cash settlement contains irrelevant fill, cost, or physical-delivery fields")
	}
	if err := normalization.validateSettlementReferences(); err != nil {
		return nil, err
	}
	contract := normalization.VenueContract
	instrumentReference := normalization.Instrument
	switch normalization.EventType {
	case EconomicEventOptionCashSettlement:
		if instrumentReference.AssetClass != instrument.AssetClassOption ||
			instrumentReference.SettlementMethod != instrument.SettlementCash ||
			contract.SettlementMethod != instrument.SettlementCash {
			return nil, fmt.Errorf("cash option settlement requires cash-settled option mechanics")
		}
	case EconomicEventOptionExpiration:
		if instrumentReference.AssetClass != instrument.AssetClassOption || !normalization.SettlementPrice.IsZero() {
			return nil, fmt.Errorf("option expiration requires an option and zero settlement price")
		}
		if contract.SettlementMethod != instrumentReference.SettlementMethod {
			return nil, fmt.Errorf("option expiration contract mechanics do not match the instrument")
		}
	case EconomicEventPredictionPayout:
		if instrumentReference.AssetClass != instrument.AssetClassPredictionContract ||
			instrumentReference.SettlementMethod != instrument.SettlementBinary ||
			contract.SettlementMethod != instrument.SettlementBinary {
			return nil, fmt.Errorf("prediction payout requires binary prediction mechanics")
		}
		if !normalization.SettlementPrice.Equal(decimal.Zero) && !normalization.SettlementPrice.Equal(decimal.NewFromInt(1)) {
			return nil, fmt.Errorf("prediction payout must be exactly zero or one")
		}
	}
	if normalization.PositionQuantity.IsZero() {
		return nil, fmt.Errorf("settlement position quantity must be nonzero")
	}
	if err := validateEconomicDecimalShape("settlement position quantity", *normalization.PositionQuantity); err != nil {
		return nil, err
	}
	if !isExactMultiple(normalization.PositionQuantity.Abs(), contract.LotSize) {
		return nil, fmt.Errorf("settlement position quantity is not an exact venue lot multiple")
	}
	if err := validateNonnegativeEconomicDecimal("settlement price", *normalization.SettlementPrice); err != nil {
		return nil, err
	}
	if !isExactMultiple(*normalization.SettlementPrice, contract.TickSize) {
		return nil, fmt.Errorf("settlement price is not an exact venue tick multiple")
	}
	inventoryAccount, err := inventoryLedgerAccount(instrumentReference.AssetClass)
	if err != nil {
		return nil, err
	}
	inventoryAmount := normalization.PositionQuantity.Neg()
	postings := []PostingInput{
		postingInput("inventory-settlement", inventoryAccount, UnitKindInstrument, instrumentReference.ID.String(), inventoryAmount),
		postingInput("clearing-inventory-settlement", "clearing:settlement", UnitKindInstrument, instrumentReference.ID.String(), inventoryAmount.Neg()),
	}
	payout := normalization.PositionQuantity.Mul(*normalization.SettlementPrice).Mul(contract.Multiplier)
	if err := validateEconomicDecimalShape("settlement cash", payout); err != nil {
		return nil, err
	}
	if !payout.IsZero() {
		postings = append(postings,
			postingInput("settlement-cash", "asset:cash", UnitKindCurrency, normalization.CashCurrency, payout),
			postingInput("clearing-settlement-cash", "clearing:settlement", UnitKindCurrency, normalization.CashCurrency, payout.Neg()),
		)
	}
	return postings, nil
}

func (normalization EconomicNormalization) validateSettlementReferences() error {
	if err := normalization.Instrument.Validate(); err != nil {
		return fmt.Errorf("settlement instrument: %w", err)
	}
	if normalization.Instrument.Status == instrument.StatusQuarantined {
		return fmt.Errorf("settlement cannot use a quarantined instrument")
	}
	if err := normalization.VenueContract.Validate(); err != nil {
		return fmt.Errorf("settlement venue contract: %w", err)
	}
	contract := normalization.VenueContract
	if contract.InstrumentID != normalization.Instrument.ID || normalization.Venue != contract.Venue {
		return fmt.Errorf("settlement venue contract does not match canonical instrument and venue")
	}
	if normalization.Instrument.Currency != contract.Currency || contract.Currency != normalization.CashCurrency {
		return fmt.Errorf("settlement cash requires instrument, contract, and account base currency agreement")
	}
	if contract.ValidFrom.After(normalization.EffectiveAt) {
		return fmt.Errorf("settlement cannot use a venue contract that had not begun")
	}
	if !normalization.Instrument.Multiplier.Equal(contract.Multiplier) {
		return fmt.Errorf("settlement instrument and venue multipliers must agree")
	}
	return nil
}

func (normalization EconomicNormalization) physicalOptionPostings() ([]PostingInput, error) {
	if normalization.Instrument == nil || normalization.SecondaryInstrument == nil ||
		normalization.VenueContract == nil || normalization.OptionTerms == nil || normalization.PositionQuantity == nil {
		return nil, fmt.Errorf("physical option normalization requires option, underlying, contract, terms, and position")
	}
	if normalization.Quantity != nil || normalization.Price != nil || normalization.SettlementPrice != nil ||
		normalization.CostAmount != nil || normalization.CostKind != "" || normalization.CostCurrency != "" {
		return nil, fmt.Errorf("physical option normalization contains irrelevant fill, cost, or cash-settlement fields")
	}
	optionReference := normalization.Instrument
	underlyingReference := normalization.SecondaryInstrument
	contract := normalization.VenueContract
	terms := normalization.OptionTerms
	if err := optionReference.Validate(); err != nil {
		return nil, fmt.Errorf("physical option instrument: %w", err)
	}
	if err := underlyingReference.Validate(); err != nil {
		return nil, fmt.Errorf("physical option underlying: %w", err)
	}
	if optionReference.Status == instrument.StatusQuarantined || underlyingReference.Status == instrument.StatusQuarantined {
		return nil, fmt.Errorf("physical option settlement cannot use quarantined references")
	}
	if optionReference.AssetClass != instrument.AssetClassOption || optionReference.UnderlyingID == nil ||
		*optionReference.UnderlyingID != underlyingReference.ID {
		return nil, fmt.Errorf("physical option and canonical underlying do not match")
	}
	if err := contract.Validate(); err != nil {
		return nil, fmt.Errorf("physical option venue contract: %w", err)
	}
	if contract.InstrumentID != optionReference.ID || contract.Venue != normalization.Venue ||
		contract.SettlementMethod != instrument.SettlementPhysical ||
		optionReference.SettlementMethod != instrument.SettlementPhysical {
		return nil, fmt.Errorf("physical option requires matching physical-settlement venue mechanics")
	}
	if contract.ValidFrom.After(normalization.EffectiveAt) {
		return nil, fmt.Errorf("physical option cannot use a venue contract that had not begun")
	}
	if err := terms.Validate(); err != nil {
		return nil, fmt.Errorf("physical option terms: %w", err)
	}
	if terms.OptionInstrumentID != optionReference.ID || terms.UnderlyingInstrumentID != underlyingReference.ID {
		return nil, fmt.Errorf("physical option terms do not match option and underlying")
	}
	if terms.EffectiveAt.After(normalization.EffectiveAt) {
		return nil, fmt.Errorf("physical option terms are not yet effective")
	}
	if terms.ObservedAt.After(normalization.SourceEvent.ObservedAt) {
		return nil, fmt.Errorf("physical option terms were not observed by source-event observation time")
	}
	if optionReference.Currency != contract.Currency || contract.Currency != normalization.CashCurrency ||
		terms.StrikeCurrency != normalization.CashCurrency {
		return nil, fmt.Errorf("physical option strike, contract, instrument, and account currencies must agree")
	}
	if !contract.Multiplier.Equal(terms.DeliverableQuantity) {
		return nil, fmt.Errorf("physical option venue multiplier and deliverable quantity must agree")
	}
	if normalization.PositionQuantity.IsZero() {
		return nil, fmt.Errorf("physical option position quantity must be nonzero")
	}
	if err := validateEconomicDecimalShape("physical option position quantity", *normalization.PositionQuantity); err != nil {
		return nil, err
	}
	if !isExactMultiple(normalization.PositionQuantity.Abs(), contract.LotSize) {
		return nil, fmt.Errorf("physical option position is not an exact venue lot multiple")
	}
	if normalization.EventType == EconomicEventOptionExercise && !normalization.PositionQuantity.IsPositive() {
		return nil, fmt.Errorf("option exercise must close a positive long position")
	}
	if normalization.EventType == EconomicEventOptionAssignment && !normalization.PositionQuantity.IsNegative() {
		return nil, fmt.Errorf("option assignment must close a negative short position")
	}
	optionClose := normalization.PositionQuantity.Neg()
	delivered := normalization.PositionQuantity.Mul(terms.DeliverableQuantity)
	if terms.ContractType == instrument.OptionContractPut {
		delivered = delivered.Neg()
	}
	if err := validateEconomicDecimalShape("physical option delivered quantity", delivered); err != nil {
		return nil, err
	}
	if !isExactMultiple(delivered.Abs(), underlyingReference.LotSize) {
		return nil, fmt.Errorf("physical option delivery is not an exact underlying lot multiple")
	}
	strikeCash := delivered.Mul(terms.StrikePrice).Neg()
	if err := validateEconomicDecimalShape("physical option strike cash", strikeCash); err != nil {
		return nil, err
	}
	underlyingAccount, err := inventoryLedgerAccount(underlyingReference.AssetClass)
	if err != nil {
		return nil, err
	}
	return []PostingInput{
		postingInput("option-close", "asset:option_inventory", UnitKindInstrument, optionReference.ID.String(), optionClose),
		postingInput("clearing-option-close", "clearing:settlement", UnitKindInstrument, optionReference.ID.String(), optionClose.Neg()),
		postingInput("underlying-delivery", underlyingAccount, UnitKindInstrument, underlyingReference.ID.String(), delivered),
		postingInput("clearing-underlying-delivery", "clearing:settlement", UnitKindInstrument, underlyingReference.ID.String(), delivered.Neg()),
		postingInput("strike-cash", "asset:cash", UnitKindCurrency, normalization.CashCurrency, strikeCash),
		postingInput("clearing-strike-cash", "clearing:settlement", UnitKindCurrency, normalization.CashCurrency, strikeCash.Neg()),
	}, nil
}

func (normalization EconomicNormalization) fillPostings() ([]PostingInput, error) {
	if normalization.Instrument == nil || normalization.VenueContract == nil ||
		normalization.Quantity == nil || normalization.Price == nil {
		return nil, fmt.Errorf("fill normalization requires instrument, venue contract, quantity, and price")
	}
	if normalization.SecondaryInstrument != nil || normalization.OptionTerms != nil ||
		normalization.PositionQuantity != nil || normalization.SettlementPrice != nil {
		return nil, fmt.Errorf("fill normalization contains irrelevant settlement references or values")
	}
	if err := normalization.Instrument.Validate(); err != nil {
		return nil, fmt.Errorf("fill instrument: %w", err)
	}
	if normalization.Instrument.Status != instrument.StatusActive {
		return nil, fmt.Errorf("fill instrument must be active")
	}
	if err := normalization.VenueContract.Validate(); err != nil {
		return nil, fmt.Errorf("fill venue contract: %w", err)
	}
	contract := normalization.VenueContract
	if contract.InstrumentID != normalization.Instrument.ID || normalization.Venue != contract.Venue {
		return nil, fmt.Errorf("fill venue contract does not match the canonical instrument and venue")
	}
	if normalization.Instrument.Currency != contract.Currency || contract.Currency != normalization.CashCurrency {
		return nil, fmt.Errorf("fill cash legs require instrument, contract, and account base currency agreement")
	}
	if normalization.EffectiveAt.Before(contract.ValidFrom) ||
		(contract.ValidTo != nil && !normalization.EffectiveAt.Before(*contract.ValidTo)) {
		return nil, fmt.Errorf("fill venue contract is not effective at fill time")
	}
	if err := validatePositiveEconomicDecimal("fill quantity", *normalization.Quantity); err != nil {
		return nil, err
	}
	if !isExactMultiple(*normalization.Quantity, contract.LotSize) {
		return nil, fmt.Errorf("fill quantity is not an exact venue lot multiple")
	}
	if err := validateNonnegativeEconomicDecimal("fill price", *normalization.Price); err != nil {
		return nil, err
	}
	if !isExactMultiple(*normalization.Price, contract.TickSize) {
		return nil, fmt.Errorf("fill price is not an exact venue tick multiple")
	}
	if normalization.Instrument.AssetClass == instrument.AssetClassPredictionContract &&
		(normalization.Price.IsNegative() || normalization.Price.GreaterThan(decimal.NewFromInt(1))) {
		return nil, fmt.Errorf("prediction fill price must be between zero and one")
	}
	inventoryAccount, err := inventoryLedgerAccount(normalization.Instrument.AssetClass)
	if err != nil {
		return nil, err
	}
	direction := decimal.NewFromInt(1)
	if normalization.EventType == EconomicEventFillSell {
		direction = direction.Neg()
	}
	inventoryAmount := normalization.Quantity.Mul(direction)
	postings := []PostingInput{
		postingInput("inventory", inventoryAccount, UnitKindInstrument, normalization.Instrument.ID.String(), inventoryAmount),
		postingInput("clearing-inventory", "clearing:execution", UnitKindInstrument, normalization.Instrument.ID.String(), inventoryAmount.Neg()),
	}
	gross := normalization.Price.Mul(*normalization.Quantity).Mul(contract.Multiplier)
	if err := validateNonnegativeEconomicDecimal("fill gross cash", gross); err != nil {
		return nil, err
	}
	if !gross.IsZero() {
		cashAmount := gross.Mul(direction).Neg()
		postings = append(postings,
			postingInput("gross-cash", "asset:cash", UnitKindCurrency, normalization.CashCurrency, cashAmount),
			postingInput("clearing-gross-cash", "clearing:execution", UnitKindCurrency, normalization.CashCurrency, cashAmount.Neg()),
		)
	}
	costPostings, err := normalization.optionalCostPostings()
	if err != nil {
		return nil, err
	}
	return append(postings, costPostings...), nil
}

func (normalization EconomicNormalization) costPostings() ([]PostingInput, error) {
	if normalization.Instrument != nil || normalization.SecondaryInstrument != nil ||
		normalization.VenueContract != nil || normalization.OptionTerms != nil || normalization.Venue != "" ||
		normalization.Quantity != nil || normalization.Price != nil ||
		normalization.PositionQuantity != nil || normalization.SettlementPrice != nil {
		return nil, fmt.Errorf("standalone cost normalization cannot carry instrument, venue, fill, or settlement fields")
	}
	expectedKind := CostKindFee
	if normalization.EventType == EconomicEventRebate {
		expectedKind = CostKindRebate
	}
	if normalization.CostKind != expectedKind {
		return nil, fmt.Errorf("standalone cost kind does not match event type")
	}
	return normalization.requiredCostPostings()
}

func (normalization EconomicNormalization) optionalCostPostings() ([]PostingInput, error) {
	if normalization.CostAmount == nil {
		if normalization.CostKind != "" || normalization.CostCurrency != "" {
			return nil, fmt.Errorf("fill cost kind/currency require an amount")
		}
		return nil, nil
	}
	return normalization.requiredCostPostings()
}

func (normalization EconomicNormalization) requiredCostPostings() ([]PostingInput, error) {
	if normalization.CostAmount == nil {
		return nil, fmt.Errorf("cost amount is required")
	}
	if normalization.CostKind != CostKindFee && normalization.CostKind != CostKindRebate {
		return nil, fmt.Errorf("cost kind %q is invalid", normalization.CostKind)
	}
	if normalization.CostCurrency != normalization.CashCurrency {
		return nil, fmt.Errorf("cost currency must match account base currency")
	}
	if normalization.VenueContract != nil && normalization.CostCurrency != normalization.VenueContract.Currency {
		return nil, fmt.Errorf("fill-attached cost currency must match venue contract currency")
	}
	if err := validatePositiveEconomicDecimal("cost amount", *normalization.CostAmount); err != nil {
		return nil, err
	}
	if normalization.CostKind == CostKindFee {
		return []PostingInput{
			postingInput("fee-expense", "expense:fees", UnitKindCurrency, normalization.CashCurrency, *normalization.CostAmount),
			postingInput("fee-cash", "asset:cash", UnitKindCurrency, normalization.CashCurrency, normalization.CostAmount.Neg()),
		}, nil
	}
	return []PostingInput{
		postingInput("rebate-cash", "asset:cash", UnitKindCurrency, normalization.CashCurrency, *normalization.CostAmount),
		postingInput("rebate-income", "income:rebates", UnitKindCurrency, normalization.CashCurrency, normalization.CostAmount.Neg()),
	}, nil
}

func postingInput(key, account string, kind UnitKind, unit string, amount decimal.Decimal) PostingInput {
	return PostingInput{
		IdempotencyKey: key,
		LedgerAccount:  account,
		UnitKind:       kind,
		Unit:           unit,
		Amount:         amount,
		Metadata:       json.RawMessage(`{}`),
	}
}

func inventoryLedgerAccount(assetClass instrument.AssetClass) (string, error) {
	switch assetClass {
	case instrument.AssetClassEquity, instrument.AssetClassETF, instrument.AssetClassFuture:
		return "asset:security_inventory", nil
	case instrument.AssetClassCryptoSpot:
		return "asset:crypto_inventory", nil
	case instrument.AssetClassOption:
		return "asset:option_inventory", nil
	case instrument.AssetClassPredictionContract:
		return "asset:event_contract_inventory", nil
	default:
		return "", fmt.Errorf("instrument asset class %q has no ledger inventory account", assetClass)
	}
}

func validatePositiveEconomicDecimal(label string, value decimal.Decimal) error {
	if !value.IsPositive() {
		return fmt.Errorf("%s must be positive", label)
	}
	return validateEconomicDecimalShape(label, value)
}

func validateNonnegativeEconomicDecimal(label string, value decimal.Decimal) error {
	if value.IsNegative() {
		return fmt.Errorf("%s must be nonnegative", label)
	}
	return validateEconomicDecimalShape(label, value)
}

func validateEconomicDecimalShape(label string, value decimal.Decimal) error {
	if !value.Equal(value.Round(12)) {
		return fmt.Errorf("%s supports at most 12 decimal places", label)
	}
	if value.NumDigits()+int(value.Exponent()) > 26 {
		return fmt.Errorf("%s exceeds NUMERIC(38,12) magnitude", label)
	}
	return nil
}

func isExactMultiple(value, increment decimal.Decimal) bool {
	return increment.IsPositive() && value.Mod(increment).IsZero()
}

func cloneEconomicDecimal(value decimal.Decimal) *decimal.Decimal {
	cloned := value
	return &cloned
}

func cloneEconomicSourceEvent(value *EconomicSourceEvent) *EconomicSourceEvent {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.RawPayload = append(json.RawMessage(nil), value.RawPayload...)
	return &cloned
}

func cloneEconomicAccount(value *domain.Account) *domain.Account {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.CreationMetadata = append(json.RawMessage(nil), value.CreationMetadata...)
	return &cloned
}

func validExecutionOriginType(value ExecutionOriginType) bool {
	switch value {
	case ExecutionOriginStrategyVersion, ExecutionOriginCopySubscription,
		ExecutionOriginPortfolioRebalance, ExecutionOriginRiskReduction,
		ExecutionOriginOperator, ExecutionOriginSettlement, ExecutionOriginReconciliation:
		return true
	default:
		return false
	}
}

func sameEconomicTransaction(left, right *Transaction) bool {
	if left == nil || right == nil || left.ID != right.ID || left.AccountID != right.AccountID ||
		left.EventType != right.EventType || left.IdempotencyKey != right.IdempotencyKey ||
		left.OriginType != right.OriginType || left.OriginID != right.OriginID ||
		left.ReferenceType != right.ReferenceType || left.ReferenceID != right.ReferenceID ||
		!left.EffectiveAt.Equal(right.EffectiveAt) || !left.ObservedAt.Equal(right.ObservedAt) ||
		!jsonSemanticEqual(left.Metadata, right.Metadata) || len(left.Postings) != len(right.Postings) {
		return false
	}
	rightByKey := make(map[string]Posting, len(right.Postings))
	for _, posting := range right.Postings {
		rightByKey[posting.IdempotencyKey] = posting
	}
	for _, posting := range left.Postings {
		expected, ok := rightByKey[posting.IdempotencyKey]
		if !ok || posting.ID != expected.ID || posting.TransactionID != expected.TransactionID ||
			posting.LedgerAccount != expected.LedgerAccount || posting.UnitKind != expected.UnitKind ||
			posting.Unit != expected.Unit || !posting.Amount.Equal(expected.Amount) ||
			!jsonSemanticEqual(posting.Metadata, expected.Metadata) {
			return false
		}
	}
	return true
}

// SameEconomicNormalizationPayload reports whether two values represent the
// same applied economic effect. Local creation timestamps are intentionally
// excluded; every source, typed, reference-mechanics, and ledger field is not.
func SameEconomicNormalizationPayload(left, right *EconomicNormalization) bool {
	if left == nil || right == nil || left.ID != right.ID ||
		!SameEconomicSourceEventPayload(left.SourceEvent, right.SourceEvent) ||
		left.Account == nil || right.Account == nil || left.Account.ID != right.Account.ID ||
		left.Account.BaseCurrency != right.Account.BaseCurrency ||
		left.EventType != right.EventType || left.NormalizerVersion != right.NormalizerVersion ||
		left.ExecutionOriginType != right.ExecutionOriginType || left.ExecutionOriginID != right.ExecutionOriginID ||
		left.ReferenceType != right.ReferenceType || left.ReferenceID != right.ReferenceID ||
		left.Venue != right.Venue || !left.EffectiveAt.Equal(right.EffectiveAt) ||
		left.CashCurrency != right.CashCurrency || left.CostKind != right.CostKind ||
		left.CostCurrency != right.CostCurrency ||
		!sameEconomicDecimalPointer(left.Quantity, right.Quantity) ||
		!sameEconomicDecimalPointer(left.Price, right.Price) ||
		!sameEconomicDecimalPointer(left.CostAmount, right.CostAmount) ||
		!sameEconomicDecimalPointer(left.PositionQuantity, right.PositionQuantity) ||
		!sameEconomicDecimalPointer(left.SettlementPrice, right.SettlementPrice) ||
		!sameNormalizationInstrument(left.Instrument, right.Instrument) ||
		!sameNormalizationInstrument(left.SecondaryInstrument, right.SecondaryInstrument) ||
		!sameNormalizationContract(left.VenueContract, right.VenueContract) ||
		!sameNormalizationOptionTerms(left.OptionTerms, right.OptionTerms) ||
		!sameEconomicTransaction(left.Transaction, right.Transaction) {
		return false
	}
	return true
}

func sameNormalizationOptionTerms(left, right *instrument.OptionContractTerms) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return instrument.SameOptionContractTermsPayload(left, right)
}

func sameEconomicDecimalPointer(left, right *decimal.Decimal) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameNormalizationInstrument(left, right *instrument.Instrument) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.ID == right.ID && left.AssetClass == right.AssetClass &&
		left.Currency == right.Currency && left.TickSize.Equal(right.TickSize) &&
		left.LotSize.Equal(right.LotSize) && left.Multiplier.Equal(right.Multiplier) &&
		left.SettlementMethod == right.SettlementMethod && left.Status == right.Status &&
		sameUUIDPointer(left.UnderlyingID, right.UnderlyingID)
}

func sameNormalizationContract(left, right *instrument.VenueContract) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.ID == right.ID && left.InstrumentID == right.InstrumentID &&
		left.Venue == right.Venue && left.ContractID == right.ContractID &&
		left.Currency == right.Currency && left.TickSize.Equal(right.TickSize) &&
		left.LotSize.Equal(right.LotSize) && left.Multiplier.Equal(right.Multiplier) &&
		left.SettlementMethod == right.SettlementMethod && left.ValidFrom.Equal(right.ValidFrom) &&
		sameTimePointer(left.ValidTo, right.ValidTo)
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func jsonSemanticEqual(left, right json.RawMessage) bool {
	decode := func(value json.RawMessage) (any, error) {
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		var decoded any
		if err := decoder.Decode(&decoded); err != nil {
			return nil, err
		}
		return decoded, nil
	}
	leftValue, leftErr := decode(left)
	rightValue, rightErr := decode(right)
	return leftErr == nil && rightErr == nil && reflect.DeepEqual(leftValue, rightValue)
}
