package ledger

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// UnitKind identifies the quantity dimension in which postings balance.
type UnitKind string

const (
	UnitKindCurrency   UnitKind = "currency"
	UnitKindInstrument UnitKind = "instrument"
)

// Transaction is one immutable economic event. Debits are positive, credits
// are negative, and Postings must net to zero independently for every unit.
type Transaction struct {
	ID             uuid.UUID       `json:"id"`
	AccountID      uuid.UUID       `json:"account_id"`
	EventType      string          `json:"event_type"`
	IdempotencyKey string          `json:"idempotency_key"`
	OriginType     string          `json:"origin_type"`
	OriginID       string          `json:"origin_id"`
	ReferenceType  string          `json:"reference_type,omitempty"`
	ReferenceID    string          `json:"reference_id,omitempty"`
	EffectiveAt    time.Time       `json:"effective_at"`
	ObservedAt     time.Time       `json:"observed_at"`
	Metadata       json.RawMessage `json:"metadata"`
	Postings       []Posting       `json:"postings"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Posting is one signed debit or credit line within a transaction.
type Posting struct {
	ID             uuid.UUID       `json:"id"`
	TransactionID  uuid.UUID       `json:"transaction_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	LedgerAccount  string          `json:"ledger_account"`
	UnitKind       UnitKind        `json:"unit_kind"`
	Unit           string          `json:"unit"`
	Amount         decimal.Decimal `json:"amount"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"created_at"`
}

// TransactionInput contains the shared provenance and posting lines for a new
// ledger transaction.
type TransactionInput struct {
	AccountID      uuid.UUID
	EventType      string
	IdempotencyKey string
	OriginType     string
	OriginID       string
	ReferenceType  string
	ReferenceID    string
	EffectiveAt    time.Time
	ObservedAt     time.Time
	Metadata       json.RawMessage
	Postings       []PostingInput
}

// PostingInput contains one caller-defined line. IdempotencyKey is unique
// within its transaction.
type PostingInput struct {
	IdempotencyKey string
	LedgerAccount  string
	UnitKind       UnitKind
	Unit           string
	Amount         decimal.Decimal
	Metadata       json.RawMessage
}

// NewTransaction materializes one immutable transaction and its posting IDs.
func NewTransaction(input TransactionInput) (*Transaction, error) {
	transactionID := uuid.New()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	metadata, err := normalizeJSONObject(input.Metadata, "transaction metadata")
	if err != nil {
		return nil, err
	}

	postings := make([]Posting, 0, len(input.Postings))
	for _, candidate := range input.Postings {
		postingMetadata, err := normalizeJSONObject(candidate.Metadata, "posting metadata")
		if err != nil {
			return nil, err
		}
		unit := strings.TrimSpace(candidate.Unit)
		if candidate.UnitKind == UnitKindCurrency {
			unit = strings.ToUpper(unit)
		}
		postings = append(postings, Posting{
			ID:             uuid.New(),
			TransactionID:  transactionID,
			IdempotencyKey: strings.TrimSpace(candidate.IdempotencyKey),
			LedgerAccount:  strings.TrimSpace(candidate.LedgerAccount),
			UnitKind:       candidate.UnitKind,
			Unit:           unit,
			Amount:         candidate.Amount,
			Metadata:       postingMetadata,
			CreatedAt:      createdAt,
		})
	}

	observedAt := input.ObservedAt.UTC().Truncate(time.Microsecond)
	if observedAt.IsZero() {
		observedAt = createdAt
	}
	transaction := &Transaction{
		ID:             transactionID,
		AccountID:      input.AccountID,
		EventType:      strings.TrimSpace(input.EventType),
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		OriginType:     strings.TrimSpace(input.OriginType),
		OriginID:       strings.TrimSpace(input.OriginID),
		ReferenceType:  strings.TrimSpace(input.ReferenceType),
		ReferenceID:    strings.TrimSpace(input.ReferenceID),
		EffectiveAt:    input.EffectiveAt.UTC().Truncate(time.Microsecond),
		ObservedAt:     observedAt,
		Metadata:       metadata,
		Postings:       postings,
		CreatedAt:      createdAt,
	}
	if err := transaction.Validate(); err != nil {
		return nil, err
	}
	return transaction, nil
}

// Validate checks the durable identity and shape of a transaction.
func (transaction Transaction) Validate() error {
	if transaction.ID == uuid.Nil || transaction.AccountID == uuid.Nil {
		return fmt.Errorf("ledger transaction and account IDs are required")
	}
	if !isNormalizedRequired(transaction.EventType) || !isNormalizedRequired(transaction.IdempotencyKey) {
		return fmt.Errorf("ledger event type and idempotency key must be non-empty and normalized")
	}
	if !isNormalizedRequired(transaction.OriginType) || !isNormalizedRequired(transaction.OriginID) {
		return fmt.Errorf("ledger origin type and ID must be non-empty and normalized")
	}
	if (transaction.ReferenceType == "") != (transaction.ReferenceID == "") {
		return fmt.Errorf("ledger reference type and ID must be provided together")
	}
	if transaction.ReferenceType != "" && (!isNormalizedRequired(transaction.ReferenceType) || !isNormalizedRequired(transaction.ReferenceID)) {
		return fmt.Errorf("ledger reference type and ID must be normalized")
	}
	if transaction.EffectiveAt.IsZero() || transaction.ObservedAt.IsZero() || transaction.CreatedAt.IsZero() {
		return fmt.Errorf("ledger effective, observed, and creation times are required")
	}
	if !hasPostgresTimestampPrecision(transaction.EffectiveAt) ||
		!hasPostgresTimestampPrecision(transaction.ObservedAt) ||
		!hasPostgresTimestampPrecision(transaction.CreatedAt) {
		return fmt.Errorf("ledger timestamps must use PostgreSQL microsecond precision")
	}
	if _, err := normalizeJSONObject(transaction.Metadata, "transaction metadata"); err != nil {
		return err
	}
	if len(transaction.Postings) < 2 {
		return fmt.Errorf("ledger transaction requires at least two postings")
	}
	balances := make(map[postingUnit]decimal.Decimal)
	postingKeys := make(map[string]struct{}, len(transaction.Postings))
	for _, posting := range transaction.Postings {
		if posting.ID == uuid.Nil || posting.TransactionID != transaction.ID {
			return fmt.Errorf("posting identity does not match ledger transaction")
		}
		if !isNormalizedRequired(posting.IdempotencyKey) || !isNormalizedRequired(posting.LedgerAccount) {
			return fmt.Errorf("posting idempotency key and ledger account must be non-empty and normalized")
		}
		if _, duplicate := postingKeys[posting.IdempotencyKey]; duplicate {
			return fmt.Errorf("duplicate posting idempotency key %q", posting.IdempotencyKey)
		}
		postingKeys[posting.IdempotencyKey] = struct{}{}
		if posting.UnitKind != UnitKindCurrency && posting.UnitKind != UnitKindInstrument {
			return fmt.Errorf("posting unit kind %q is invalid", posting.UnitKind)
		}
		if !isNormalizedRequired(posting.Unit) {
			return fmt.Errorf("posting unit must be non-empty and normalized")
		}
		if posting.UnitKind == UnitKindCurrency && !isCurrencyUnit(posting.Unit) {
			return fmt.Errorf("posting currency unit %q must be a three-letter code", posting.Unit)
		}
		if posting.Amount.IsZero() {
			return fmt.Errorf("posting amount must be non-zero")
		}
		if posting.CreatedAt.IsZero() {
			return fmt.Errorf("posting creation time is required")
		}
		if !hasPostgresTimestampPrecision(posting.CreatedAt) {
			return fmt.Errorf("posting creation time must use PostgreSQL microsecond precision")
		}
		if !posting.Amount.Equal(posting.Amount.Round(12)) {
			return fmt.Errorf("posting amount supports at most 12 decimal places")
		}
		integerDigits := posting.Amount.NumDigits() + int(posting.Amount.Exponent())
		if integerDigits > 26 {
			return fmt.Errorf("posting amount exceeds NUMERIC(38,12) magnitude")
		}
		if _, err := normalizeJSONObject(posting.Metadata, "posting metadata"); err != nil {
			return err
		}
		unit := postingUnit{kind: posting.UnitKind, value: posting.Unit}
		balances[unit] = balances[unit].Add(posting.Amount)
	}
	for unit, balance := range balances {
		if !balance.IsZero() {
			return fmt.Errorf("ledger postings do not balance for %s %s: %s", unit.kind, unit.value, balance)
		}
	}
	return nil
}

func isNormalizedRequired(value string) bool {
	return value != "" && value == strings.TrimSpace(value)
}

func hasPostgresTimestampPrecision(value time.Time) bool {
	return value.Equal(value.Truncate(time.Microsecond))
}

type postingUnit struct {
	kind  UnitKind
	value string
}

func isCurrencyUnit(value string) bool {
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
