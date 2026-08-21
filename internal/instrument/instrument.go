// Package instrument owns canonical instrument identity and immutable reference
// facts. Legacy ticker-based domains remain separate until an explicit cutover.
package instrument

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// AssetClass identifies the economic kind of an instrument independently of
// any provider-specific symbol.
type AssetClass string

const (
	AssetClassUnknown            AssetClass = "unknown"
	AssetClassEquity             AssetClass = "equity"
	AssetClassETF                AssetClass = "etf"
	AssetClassOption             AssetClass = "option"
	AssetClassCryptoSpot         AssetClass = "crypto_spot"
	AssetClassPredictionContract AssetClass = "prediction_contract"
	AssetClassFuture             AssetClass = "future"
)

// Status controls whether an instrument may be used as verified reference
// data. Quarantined records preserve identity evidence without asserting
// unknown trading mechanics.
type Status string

const (
	StatusActive      Status = "active"
	StatusInactive    Status = "inactive"
	StatusExpired     Status = "expired"
	StatusQuarantined Status = "quarantined"
)

// SettlementMethod records what is delivered when an instrument settles.
type SettlementMethod string

const (
	SettlementCash     SettlementMethod = "cash"
	SettlementPhysical SettlementMethod = "physical"
	SettlementCrypto   SettlementMethod = "crypto"
	SettlementBinary   SettlementMethod = "binary"
)

// ExerciseStyle records how an option may be exercised.
type ExerciseStyle string

const (
	ExerciseAmerican ExerciseStyle = "american"
	ExerciseEuropean ExerciseStyle = "european"
)

// Instrument is a stable economic identity. Provider symbols are dated alias
// events and are deliberately not fields on this aggregate.
type Instrument struct {
	ID               uuid.UUID        `json:"id"`
	IdentityKey      string           `json:"identity_key"`
	AssetClass       AssetClass       `json:"asset_class"`
	PrimaryVenue     string           `json:"primary_venue"`
	Currency         string           `json:"currency,omitempty"`
	TickSize         decimal.Decimal  `json:"tick_size"`
	LotSize          decimal.Decimal  `json:"lot_size"`
	Multiplier       decimal.Decimal  `json:"multiplier"`
	Expiration       *time.Time       `json:"expiration,omitempty"`
	ExerciseStyle    ExerciseStyle    `json:"exercise_style,omitempty"`
	SettlementMethod SettlementMethod `json:"settlement_method,omitempty"`
	UnderlyingID     *uuid.UUID       `json:"underlying_id,omitempty"`
	Status           Status           `json:"status"`
	Metadata         json.RawMessage  `json:"metadata"`
	CreatedAt        time.Time        `json:"created_at"`
}

// InstrumentInput contains the caller-supplied canonical identity and trading
// mechanics for a new instrument.
type InstrumentInput struct {
	IdentityKey      string
	AssetClass       AssetClass
	PrimaryVenue     string
	Currency         string
	TickSize         decimal.Decimal
	LotSize          decimal.Decimal
	Multiplier       decimal.Decimal
	Expiration       *time.Time
	ExerciseStyle    ExerciseStyle
	SettlementMethod SettlementMethod
	UnderlyingID     *uuid.UUID
	Status           Status
	Metadata         json.RawMessage
	CreatedAt        time.Time
}

// NewInstrument normalizes a caller-supplied instrument and validates it as a
// complete canonical record or an explicitly quarantined evidence record.
func NewInstrument(input InstrumentInput) (*Instrument, error) {
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	status := input.Status
	if status == "" {
		status = StatusActive
	}
	metadata, err := normalizeJSONObject(input.Metadata, "instrument metadata")
	if err != nil {
		return nil, err
	}
	expiration := normalizeOptionalTime(input.Expiration)
	instrument := &Instrument{
		ID:               uuid.New(),
		IdentityKey:      strings.TrimSpace(input.IdentityKey),
		AssetClass:       input.AssetClass,
		PrimaryVenue:     strings.ToLower(strings.TrimSpace(input.PrimaryVenue)),
		Currency:         strings.ToUpper(strings.TrimSpace(input.Currency)),
		TickSize:         input.TickSize,
		LotSize:          input.LotSize,
		Multiplier:       input.Multiplier,
		Expiration:       expiration,
		ExerciseStyle:    input.ExerciseStyle,
		SettlementMethod: input.SettlementMethod,
		UnderlyingID:     cloneUUID(input.UnderlyingID),
		Status:           status,
		Metadata:         metadata,
		CreatedAt:        createdAt,
	}
	if err := instrument.Validate(); err != nil {
		return nil, err
	}
	return instrument, nil
}

// Validate checks stable identity, normalized trading mechanics, derivative
// terms, and explicit quarantine provenance.
func (instrument Instrument) Validate() error {
	if instrument.ID == uuid.Nil {
		return fmt.Errorf("instrument ID is required")
	}
	if instrument.IdentityKey == "" || instrument.IdentityKey != strings.TrimSpace(instrument.IdentityKey) {
		return fmt.Errorf("instrument identity key must be non-empty and normalized")
	}
	if !validAssetClass(instrument.AssetClass) {
		return fmt.Errorf("instrument asset class %q is invalid", instrument.AssetClass)
	}
	if !validStatus(instrument.Status) {
		return fmt.Errorf("instrument status %q is invalid", instrument.Status)
	}
	quarantined := instrument.Status == StatusQuarantined
	if !quarantined && instrument.AssetClass == AssetClassUnknown {
		return fmt.Errorf("verified instrument asset class cannot be unknown")
	}
	if instrument.PrimaryVenue == "" || instrument.PrimaryVenue != strings.ToLower(strings.TrimSpace(instrument.PrimaryVenue)) {
		return fmt.Errorf("instrument primary venue must be non-empty and normalized")
	}
	if (!quarantined || instrument.Currency != "") && !isCurrency(instrument.Currency) {
		return fmt.Errorf("instrument currency %q must be a normalized three-letter code", instrument.Currency)
	}
	for name, value := range map[string]decimal.Decimal{
		"tick size":  instrument.TickSize,
		"lot size":   instrument.LotSize,
		"multiplier": instrument.Multiplier,
	} {
		if quarantined && value.IsZero() {
			continue
		}
		if !value.IsPositive() {
			return fmt.Errorf("instrument %s must be positive when supplied", name)
		}
		if !value.Equal(value.Round(12)) {
			return fmt.Errorf("instrument %s supports at most 12 decimal places", name)
		}
		if value.NumDigits()+int(value.Exponent()) > 26 {
			return fmt.Errorf("instrument %s exceeds NUMERIC(38,12) magnitude", name)
		}
	}
	if (!quarantined || instrument.SettlementMethod != "") && !validSettlementMethod(instrument.SettlementMethod) {
		return fmt.Errorf("instrument settlement method %q is invalid", instrument.SettlementMethod)
	}
	if instrument.Expiration != nil && (instrument.Expiration.Location() != time.UTC ||
		!instrument.Expiration.Equal(instrument.Expiration.Truncate(time.Microsecond))) {
		return fmt.Errorf("instrument expiration must use normalized UTC microsecond precision")
	}
	if instrument.ExerciseStyle != "" && !validExerciseStyle(instrument.ExerciseStyle) {
		return fmt.Errorf("instrument exercise style %q is invalid", instrument.ExerciseStyle)
	}
	if instrument.UnderlyingID != nil && *instrument.UnderlyingID == uuid.Nil {
		return fmt.Errorf("instrument underlying ID cannot be nil")
	}
	if instrument.UnderlyingID != nil && *instrument.UnderlyingID == instrument.ID {
		return fmt.Errorf("instrument cannot be its own underlying")
	}
	if !quarantined && instrument.AssetClass == AssetClassOption {
		if instrument.Expiration == nil || !validExerciseStyle(instrument.ExerciseStyle) || instrument.UnderlyingID == nil {
			return fmt.Errorf("option instrument requires expiration, exercise style, and underlying ID")
		}
	}
	if instrument.CreatedAt.IsZero() || instrument.CreatedAt.Location() != time.UTC ||
		!instrument.CreatedAt.Equal(instrument.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("instrument creation time must use normalized UTC microsecond precision")
	}
	metadata, err := normalizeJSONObject(instrument.Metadata, "instrument metadata")
	if err != nil {
		return err
	}
	if quarantined {
		var provenance map[string]any
		if err := json.Unmarshal(metadata, &provenance); err != nil || len(provenance) == 0 {
			return fmt.Errorf("quarantined instrument metadata must record provenance")
		}
	}
	return nil
}

func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}

func cloneUUID(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func normalizeJSONObject(value json.RawMessage, label string) (json.RawMessage, error) {
	normalized := append(json.RawMessage(nil), value...)
	if len(normalized) == 0 {
		normalized = json.RawMessage(`{}`)
	}
	var object map[string]any
	if err := json.Unmarshal(normalized, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	return normalized, nil
}

func isCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func validAssetClass(value AssetClass) bool {
	switch value {
	case AssetClassUnknown, AssetClassEquity, AssetClassETF, AssetClassOption,
		AssetClassCryptoSpot, AssetClassPredictionContract, AssetClassFuture:
		return true
	default:
		return false
	}
}

func validStatus(value Status) bool {
	switch value {
	case StatusActive, StatusInactive, StatusExpired, StatusQuarantined:
		return true
	default:
		return false
	}
}

func validSettlementMethod(value SettlementMethod) bool {
	switch value {
	case SettlementCash, SettlementPhysical, SettlementCrypto, SettlementBinary:
		return true
	default:
		return false
	}
}

func validExerciseStyle(value ExerciseStyle) bool {
	return value == ExerciseAmerican || value == ExerciseEuropean
}
