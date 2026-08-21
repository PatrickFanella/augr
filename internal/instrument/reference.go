package instrument

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// AliasType describes the provider namespace of an alias value.
type AliasType string

const (
	AliasTicker        AliasType = "ticker"
	AliasOCC           AliasType = "occ"
	AliasCUSIP         AliasType = "cusip"
	AliasFIGI          AliasType = "figi"
	AliasVenueContract AliasType = "venue_contract"
	AliasSlug          AliasType = "slug"
	AliasProviderID    AliasType = "provider_id"
)

// AliasAction makes symbol assignments and retirements explicit facts rather
// than mutable validity columns.
type AliasAction string

const (
	AliasAssigned AliasAction = "assigned"
	AliasRetired  AliasAction = "retired"
)

// AliasEvent is one immutable effective-time change to an alias binding.
type AliasEvent struct {
	ID           uuid.UUID       `json:"id"`
	InstrumentID uuid.UUID       `json:"instrument_id"`
	Provider     string          `json:"provider"`
	AliasType    AliasType       `json:"alias_type"`
	AliasValue   string          `json:"alias_value"`
	Action       AliasAction     `json:"action"`
	EffectiveAt  time.Time       `json:"effective_at"`
	Source       string          `json:"source"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"created_at"`
}

// AliasEventInput contains one provider alias assignment or retirement.
type AliasEventInput struct {
	InstrumentID uuid.UUID
	Provider     string
	AliasType    AliasType
	AliasValue   string
	Action       AliasAction
	EffectiveAt  time.Time
	Source       string
	Metadata     json.RawMessage
	CreatedAt    time.Time
}

// VenueContract records the exact mechanics a venue applies to one canonical
// instrument during an effective-time window.
type VenueContract struct {
	ID               uuid.UUID        `json:"id"`
	InstrumentID     uuid.UUID        `json:"instrument_id"`
	Venue            string           `json:"venue"`
	ContractID       string           `json:"contract_id"`
	Currency         string           `json:"currency"`
	TickSize         decimal.Decimal  `json:"tick_size"`
	LotSize          decimal.Decimal  `json:"lot_size"`
	Multiplier       decimal.Decimal  `json:"multiplier"`
	SettlementMethod SettlementMethod `json:"settlement_method"`
	ValidFrom        time.Time        `json:"valid_from"`
	ValidTo          *time.Time       `json:"valid_to,omitempty"`
	Metadata         json.RawMessage  `json:"metadata"`
	CreatedAt        time.Time        `json:"created_at"`
}

// VenueContractInput contains one venue-specific contract window.
type VenueContractInput struct {
	InstrumentID     uuid.UUID
	Venue            string
	ContractID       string
	Currency         string
	TickSize         decimal.Decimal
	LotSize          decimal.Decimal
	Multiplier       decimal.Decimal
	SettlementMethod SettlementMethod
	ValidFrom        time.Time
	ValidTo          *time.Time
	Metadata         json.RawMessage
	CreatedAt        time.Time
}

// CorporateActionType identifies an immutable issuer or contract lifecycle
// fact that may affect aliases, quantities, or settlement.
type CorporateActionType string

const (
	CorporateActionSymbolChange CorporateActionType = "symbol_change"
	CorporateActionSplit        CorporateActionType = "split"
	CorporateActionReverseSplit CorporateActionType = "reverse_split"
	CorporateActionMerger       CorporateActionType = "merger"
	CorporateActionSpinoff      CorporateActionType = "spinoff"
	CorporateActionDelisting    CorporateActionType = "delisting"
	CorporateActionCashDividend CorporateActionType = "cash_dividend"
	CorporateActionFuturesRoll  CorporateActionType = "futures_roll"
)

// CorporateAction records a sourced effective-time fact without rewriting the
// identity or historical aliases of the affected instrument.
type CorporateAction struct {
	ID                    uuid.UUID           `json:"id"`
	InstrumentID          uuid.UUID           `json:"instrument_id"`
	SuccessorInstrumentID *uuid.UUID          `json:"successor_instrument_id,omitempty"`
	ActionType            CorporateActionType `json:"action_type"`
	EffectiveAt           time.Time           `json:"effective_at"`
	RatioNumerator        decimal.Decimal     `json:"ratio_numerator"`
	RatioDenominator      decimal.Decimal     `json:"ratio_denominator"`
	CashAmount            *decimal.Decimal    `json:"cash_amount,omitempty"`
	CashCurrency          string              `json:"cash_currency,omitempty"`
	Source                string              `json:"source"`
	SourceEventID         string              `json:"source_event_id"`
	Metadata              json.RawMessage     `json:"metadata"`
	CreatedAt             time.Time           `json:"created_at"`
}

// CorporateActionInput contains one sourced corporate-action fact.
type CorporateActionInput struct {
	InstrumentID          uuid.UUID
	SuccessorInstrumentID *uuid.UUID
	ActionType            CorporateActionType
	EffectiveAt           time.Time
	RatioNumerator        decimal.Decimal
	RatioDenominator      decimal.Decimal
	CashAmount            *decimal.Decimal
	CashCurrency          string
	Source                string
	SourceEventID         string
	Metadata              json.RawMessage
	CreatedAt             time.Time
}

// NewAliasEvent normalizes an immutable provider alias fact.
func NewAliasEvent(input AliasEventInput) (*AliasEvent, error) {
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	metadata, err := normalizeJSONObject(input.Metadata, "alias metadata")
	if err != nil {
		return nil, err
	}
	provider, aliasValue, err := NormalizeAlias(input.Provider, input.AliasType, input.AliasValue)
	if err != nil {
		return nil, err
	}
	event := &AliasEvent{
		ID:           uuid.New(),
		InstrumentID: input.InstrumentID,
		Provider:     provider,
		AliasType:    input.AliasType,
		AliasValue:   aliasValue,
		Action:       input.Action,
		EffectiveAt:  input.EffectiveAt.UTC().Truncate(time.Microsecond),
		Source:       strings.TrimSpace(input.Source),
		Metadata:     metadata,
		CreatedAt:    createdAt,
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return event, nil
}

// NormalizeAlias canonicalizes a lookup key without requiring callers to
// manufacture an alias event solely to share normalization rules.
func NormalizeAlias(provider string, aliasType AliasType, aliasValue string) (string, string, error) {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if normalizedProvider == "" {
		return "", "", fmt.Errorf("alias provider must be non-empty")
	}
	if !validAliasType(aliasType) {
		return "", "", fmt.Errorf("alias type %q is invalid", aliasType)
	}
	normalizedValue := normalizeAliasValue(aliasType, aliasValue)
	if normalizedValue == "" {
		return "", "", fmt.Errorf("alias value must be non-empty")
	}
	return normalizedProvider, normalizedValue, nil
}

// Validate checks one alias fact independently. Cross-event transition rules
// are enforced atomically by the repository and database trigger.
func (event AliasEvent) Validate() error {
	if event.ID == uuid.Nil || event.InstrumentID == uuid.Nil {
		return fmt.Errorf("alias event and instrument IDs are required")
	}
	if event.Provider == "" || event.Provider != strings.ToLower(strings.TrimSpace(event.Provider)) {
		return fmt.Errorf("alias provider must be non-empty and normalized")
	}
	if !validAliasType(event.AliasType) {
		return fmt.Errorf("alias type %q is invalid", event.AliasType)
	}
	if event.AliasValue == "" || event.AliasValue != normalizeAliasValue(event.AliasType, event.AliasValue) {
		return fmt.Errorf("alias value must be non-empty and normalized")
	}
	if event.Action != AliasAssigned && event.Action != AliasRetired {
		return fmt.Errorf("alias action %q is invalid", event.Action)
	}
	if event.EffectiveAt.IsZero() || !isNormalizedReferenceTime(event.EffectiveAt) {
		return fmt.Errorf("alias effective time must use normalized UTC microsecond precision")
	}
	if event.Source == "" || event.Source != strings.TrimSpace(event.Source) {
		return fmt.Errorf("alias source must be non-empty and normalized")
	}
	if event.CreatedAt.IsZero() || !isNormalizedReferenceTime(event.CreatedAt) {
		return fmt.Errorf("alias creation time must use normalized UTC microsecond precision")
	}
	if _, err := normalizeJSONObject(event.Metadata, "alias metadata"); err != nil {
		return err
	}
	return nil
}

// NewVenueContract normalizes and validates one immutable venue contract.
func NewVenueContract(input VenueContractInput) (*VenueContract, error) {
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	metadata, err := normalizeJSONObject(input.Metadata, "venue contract metadata")
	if err != nil {
		return nil, err
	}
	contract := &VenueContract{
		ID:               uuid.New(),
		InstrumentID:     input.InstrumentID,
		Venue:            strings.ToLower(strings.TrimSpace(input.Venue)),
		ContractID:       strings.ToUpper(strings.TrimSpace(input.ContractID)),
		Currency:         strings.ToUpper(strings.TrimSpace(input.Currency)),
		TickSize:         input.TickSize,
		LotSize:          input.LotSize,
		Multiplier:       input.Multiplier,
		SettlementMethod: input.SettlementMethod,
		ValidFrom:        input.ValidFrom.UTC().Truncate(time.Microsecond),
		ValidTo:          normalizeOptionalTime(input.ValidTo),
		Metadata:         metadata,
		CreatedAt:        createdAt,
	}
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	return contract, nil
}

// Validate checks the independent shape and effective-time window of a venue
// contract. Overlap prevention is a repository/database concern.
func (contract VenueContract) Validate() error {
	if contract.ID == uuid.Nil || contract.InstrumentID == uuid.Nil {
		return fmt.Errorf("venue contract and instrument IDs are required")
	}
	if contract.Venue == "" || contract.Venue != strings.ToLower(strings.TrimSpace(contract.Venue)) {
		return fmt.Errorf("venue contract venue must be non-empty and normalized")
	}
	if contract.ContractID == "" || contract.ContractID != strings.ToUpper(strings.TrimSpace(contract.ContractID)) {
		return fmt.Errorf("venue contract ID must be non-empty and normalized")
	}
	if !isCurrency(contract.Currency) {
		return fmt.Errorf("venue contract currency %q must be a normalized three-letter code", contract.Currency)
	}
	for name, value := range map[string]decimal.Decimal{
		"tick size":  contract.TickSize,
		"lot size":   contract.LotSize,
		"multiplier": contract.Multiplier,
	} {
		if err := validatePositiveReferenceDecimal(name, value); err != nil {
			return err
		}
	}
	if !validSettlementMethod(contract.SettlementMethod) {
		return fmt.Errorf("venue contract settlement method %q is invalid", contract.SettlementMethod)
	}
	if contract.ValidFrom.IsZero() || !isNormalizedReferenceTime(contract.ValidFrom) {
		return fmt.Errorf("venue contract valid-from time must use normalized UTC microsecond precision")
	}
	if contract.ValidTo != nil {
		if !isNormalizedReferenceTime(*contract.ValidTo) {
			return fmt.Errorf("venue contract valid-to time must use normalized UTC microsecond precision")
		}
		if !contract.ValidTo.After(contract.ValidFrom) {
			return fmt.Errorf("venue contract valid-to time must be after valid-from time")
		}
	}
	if contract.CreatedAt.IsZero() || !isNormalizedReferenceTime(contract.CreatedAt) {
		return fmt.Errorf("venue contract creation time must use normalized UTC microsecond precision")
	}
	if _, err := normalizeJSONObject(contract.Metadata, "venue contract metadata"); err != nil {
		return err
	}
	return nil
}

// NewCorporateAction normalizes and validates one immutable corporate-action
// fact. Recording corresponding alias events is a separate repository
// operation that a service may coordinate in one database transaction.
func NewCorporateAction(input CorporateActionInput) (*CorporateAction, error) {
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	metadata, err := normalizeJSONObject(input.Metadata, "corporate action metadata")
	if err != nil {
		return nil, err
	}
	action := &CorporateAction{
		ID:                    uuid.New(),
		InstrumentID:          input.InstrumentID,
		SuccessorInstrumentID: cloneUUID(input.SuccessorInstrumentID),
		ActionType:            input.ActionType,
		EffectiveAt:           input.EffectiveAt.UTC().Truncate(time.Microsecond),
		RatioNumerator:        input.RatioNumerator,
		RatioDenominator:      input.RatioDenominator,
		CashAmount:            cloneDecimal(input.CashAmount),
		CashCurrency:          strings.ToUpper(strings.TrimSpace(input.CashCurrency)),
		Source:                strings.TrimSpace(input.Source),
		SourceEventID:         strings.TrimSpace(input.SourceEventID),
		Metadata:              metadata,
		CreatedAt:             createdAt,
	}
	if err := action.Validate(); err != nil {
		return nil, err
	}
	return action, nil
}

// Validate checks action-specific terms and durable source identity.
func (action CorporateAction) Validate() error {
	if action.ID == uuid.Nil || action.InstrumentID == uuid.Nil {
		return fmt.Errorf("corporate action and instrument IDs are required")
	}
	if !validCorporateActionType(action.ActionType) {
		return fmt.Errorf("corporate action type %q is invalid", action.ActionType)
	}
	if action.EffectiveAt.IsZero() || !isNormalizedReferenceTime(action.EffectiveAt) {
		return fmt.Errorf("corporate action effective time must use normalized UTC microsecond precision")
	}
	if action.SuccessorInstrumentID != nil {
		if *action.SuccessorInstrumentID == uuid.Nil || *action.SuccessorInstrumentID == action.InstrumentID {
			return fmt.Errorf("corporate action successor must be a different non-nil instrument")
		}
	}
	if action.ActionType == CorporateActionSplit || action.ActionType == CorporateActionReverseSplit ||
		!action.RatioNumerator.IsZero() || !action.RatioDenominator.IsZero() {
		if err := validatePositiveReferenceDecimal("corporate action ratio numerator", action.RatioNumerator); err != nil {
			return err
		}
		if err := validatePositiveReferenceDecimal("corporate action ratio denominator", action.RatioDenominator); err != nil {
			return err
		}
	}
	if action.CashAmount != nil {
		if err := validatePositiveReferenceDecimal("corporate action cash amount", *action.CashAmount); err != nil {
			return err
		}
		if !isCurrency(action.CashCurrency) {
			return fmt.Errorf("corporate action cash currency must be a normalized three-letter code")
		}
	} else if action.CashCurrency != "" {
		return fmt.Errorf("corporate action cash amount and currency must be supplied together")
	}
	switch action.ActionType {
	case CorporateActionMerger, CorporateActionSpinoff, CorporateActionFuturesRoll:
		if action.SuccessorInstrumentID == nil {
			return fmt.Errorf("corporate action type %q requires a successor instrument", action.ActionType)
		}
	case CorporateActionCashDividend:
		if action.CashAmount == nil || !isCurrency(action.CashCurrency) {
			return fmt.Errorf("cash dividend requires amount and currency")
		}
	}
	if action.Source == "" || action.Source != strings.TrimSpace(action.Source) ||
		action.SourceEventID == "" || action.SourceEventID != strings.TrimSpace(action.SourceEventID) {
		return fmt.Errorf("corporate action source and source event ID must be non-empty and normalized")
	}
	if action.CreatedAt.IsZero() || !isNormalizedReferenceTime(action.CreatedAt) {
		return fmt.Errorf("corporate action creation time must use normalized UTC microsecond precision")
	}
	if _, err := normalizeJSONObject(action.Metadata, "corporate action metadata"); err != nil {
		return err
	}
	return nil
}

func normalizeAliasValue(aliasType AliasType, value string) string {
	normalized := strings.TrimSpace(value)
	switch aliasType {
	case AliasTicker, AliasOCC, AliasCUSIP, AliasFIGI, AliasVenueContract:
		return strings.ToUpper(normalized)
	default:
		return normalized
	}
}

func validAliasType(value AliasType) bool {
	switch value {
	case AliasTicker, AliasOCC, AliasCUSIP, AliasFIGI, AliasVenueContract, AliasSlug, AliasProviderID:
		return true
	default:
		return false
	}
}

func validCorporateActionType(value CorporateActionType) bool {
	switch value {
	case CorporateActionSymbolChange, CorporateActionSplit, CorporateActionReverseSplit,
		CorporateActionMerger, CorporateActionSpinoff, CorporateActionDelisting,
		CorporateActionCashDividend, CorporateActionFuturesRoll:
		return true
	default:
		return false
	}
}

func isNormalizedReferenceTime(value time.Time) bool {
	return value.Location() == time.UTC && value.Equal(value.Truncate(time.Microsecond))
}

func validatePositiveReferenceDecimal(name string, value decimal.Decimal) error {
	if !value.IsPositive() {
		return fmt.Errorf("%s must be positive", name)
	}
	if !value.Equal(value.Round(12)) {
		return fmt.Errorf("%s supports at most 12 decimal places", name)
	}
	if value.NumDigits()+int(value.Exponent()) > 26 {
		return fmt.Errorf("%s exceeds NUMERIC(38,12) magnitude", name)
	}
	return nil
}

func cloneDecimal(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
