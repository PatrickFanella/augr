package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// AccountEnvironment identifies the economic and evidence boundary in which
// an account operates. Paper-scored and paper-stress accounts must never share
// a storage namespace or ranking population.
type AccountEnvironment string

const (
	AccountEnvironmentPaperScored AccountEnvironment = "paper_scored"
	AccountEnvironmentPaperStress AccountEnvironment = "paper_stress"
	AccountEnvironmentShadow      AccountEnvironment = "shadow"
	AccountEnvironmentLive        AccountEnvironment = "live"
)

// MarginProfile records the requested buying-power policy. Enforcement is
// added by the common account and margin services rather than inferred from a
// broker balance.
type MarginProfile string

const (
	MarginProfileCash            MarginProfile = "cash"
	MarginProfileRegT            MarginProfile = "reg_t"
	MarginProfilePortfolio       MarginProfile = "portfolio"
	MarginProfileStressUnlimited MarginProfile = "stress_unlimited"
)

// AccountStatus is the lifecycle state of an explicit economic account.
type AccountStatus string

const (
	AccountStatusActive AccountStatus = "active"
	AccountStatusPaused AccountStatus = "paused"
	AccountStatusClosed AccountStatus = "closed"
)

// CapitalFlowType encodes direction while Amount remains an exact positive
// magnitude.
type CapitalFlowType string

const (
	CapitalFlowTypeDeposit    CapitalFlowType = "deposit"
	CapitalFlowTypeWithdrawal CapitalFlowType = "withdrawal"
)

// CapitalFlowSource identifies the authority that introduced the flow.
type CapitalFlowSource string

const (
	CapitalFlowSourceAccountOpening CapitalFlowSource = "account_opening"
	CapitalFlowSourceOperator       CapitalFlowSource = "operator"
	CapitalFlowSourceVenue          CapitalFlowSource = "venue"
	CapitalFlowSourceReconciliation CapitalFlowSource = "reconciliation"
	CapitalFlowSourceMigration      CapitalFlowSource = "migration"
)

// Account is the durable economic and risk boundary. StartingCapital and all
// subsequent money values use decimal arithmetic; binary floating point is not
// part of the authoritative account contract.
type Account struct {
	ID                    uuid.UUID          `json:"id"`
	Name                  string             `json:"name"`
	Environment           AccountEnvironment `json:"environment"`
	Venue                 string             `json:"venue"`
	ExternalAccountID     string             `json:"external_account_id,omitempty"`
	BaseCurrency          string             `json:"base_currency"`
	StorageNamespace      string             `json:"storage_namespace"`
	EvidenceClass         string             `json:"evidence_class"`
	StartingCapital       decimal.Decimal    `json:"starting_capital"`
	BuyingPowerMultiplier decimal.Decimal    `json:"buying_power_multiplier"`
	MarginProfile         MarginProfile      `json:"margin_profile"`
	Status                AccountStatus      `json:"status"`
	CreatedBy             string             `json:"created_by"`
	CreationMetadata      json.RawMessage    `json:"creation_metadata"`
	CreatedAt             time.Time          `json:"created_at"`
}

// AccountInput contains the immutable identity and opening-capital fields used
// to create an account.
type AccountInput struct {
	Name                  string
	Environment           AccountEnvironment
	Venue                 string
	ExternalAccountID     string
	BaseCurrency          string
	StorageNamespace      string
	StartingCapital       decimal.Decimal
	BuyingPowerMultiplier decimal.Decimal
	MarginProfile         MarginProfile
	CreatedBy             string
	CreationMetadata      json.RawMessage
	CreatedAt             time.Time
}

// CapitalFlow is an append-only deposit or withdrawal. Reusing an idempotency
// key with an identical payload replays the original row; a different payload
// is rejected by the persistence boundary.
type CapitalFlow struct {
	ID                uuid.UUID         `json:"id"`
	AccountID         uuid.UUID         `json:"account_id"`
	Type              CapitalFlowType   `json:"type"`
	Amount            decimal.Decimal   `json:"amount"`
	Currency          string            `json:"currency"`
	IdempotencyKey    string            `json:"idempotency_key"`
	Source            CapitalFlowSource `json:"source"`
	ExternalReference string            `json:"external_reference,omitempty"`
	Metadata          json.RawMessage   `json:"metadata"`
	EffectiveAt       time.Time         `json:"effective_at"`
	ObservedAt        time.Time         `json:"observed_at"`
	CreatedAt         time.Time         `json:"created_at"`
}

// CapitalFlowInput contains caller-supplied fields for a new capital flow.
type CapitalFlowInput struct {
	AccountID         uuid.UUID
	Type              CapitalFlowType
	Amount            decimal.Decimal
	Currency          string
	IdempotencyKey    string
	Source            CapitalFlowSource
	ExternalReference string
	Metadata          json.RawMessage
	EffectiveAt       time.Time
	ObservedAt        time.Time
}

// AccountCapitalSummary reconciles the immutable opening amount with all
// append-only deposits and withdrawals. StartingCapital never changes when
// later flows arrive.
type AccountCapitalSummary struct {
	AccountID       uuid.UUID       `json:"account_id"`
	Currency        string          `json:"currency"`
	StartingCapital decimal.Decimal `json:"starting_capital"`
	Deposits        decimal.Decimal `json:"deposits"`
	Withdrawals     decimal.Decimal `json:"withdrawals"`
	NetCapital      decimal.Decimal `json:"net_capital"`
	FlowCount       int64           `json:"flow_count"`
}

// NewAccount validates and materializes an explicit account identity. The
// repository persists its matching opening-capital flow atomically.
func NewAccount(input AccountInput) (*Account, error) {
	name := strings.TrimSpace(input.Name)
	venue := strings.ToLower(strings.TrimSpace(input.Venue))
	currency := strings.ToUpper(strings.TrimSpace(input.BaseCurrency))
	namespace := strings.TrimSpace(input.StorageNamespace)
	createdBy := strings.TrimSpace(input.CreatedBy)

	if name == "" {
		return nil, fmt.Errorf("account name is required")
	}
	if !input.Environment.IsValid() {
		return nil, fmt.Errorf("invalid account environment %q", input.Environment)
	}
	if venue == "" {
		return nil, fmt.Errorf("account venue is required")
	}
	if !isCurrencyCode(currency) {
		return nil, fmt.Errorf("account base currency must be a three-letter code")
	}
	if namespace == "" {
		return nil, fmt.Errorf("account storage namespace is required")
	}
	if !input.StartingCapital.IsPositive() {
		return nil, fmt.Errorf("account starting capital must be greater than zero")
	}
	if !hasDecimalScaleAtMost(input.StartingCapital, 8) {
		return nil, fmt.Errorf("account starting capital supports at most 8 decimal places")
	}
	if input.BuyingPowerMultiplier.IsNegative() {
		return nil, fmt.Errorf("account buying-power multiplier must be non-negative")
	}
	if !hasDecimalScaleAtMost(input.BuyingPowerMultiplier, 8) {
		return nil, fmt.Errorf("account buying-power multiplier supports at most 8 decimal places")
	}
	if input.Environment != AccountEnvironmentPaperStress && !input.BuyingPowerMultiplier.IsPositive() {
		return nil, fmt.Errorf("non-stress account buying-power multiplier must be greater than zero")
	}
	if !input.MarginProfile.IsValid() {
		return nil, fmt.Errorf("invalid account margin profile %q", input.MarginProfile)
	}
	if input.MarginProfile == MarginProfileStressUnlimited && input.Environment != AccountEnvironmentPaperStress {
		return nil, fmt.Errorf("unlimited margin is restricted to stress accounts")
	}
	if input.Environment == AccountEnvironmentPaperStress && input.BuyingPowerMultiplier.IsZero() && input.MarginProfile != MarginProfileStressUnlimited {
		return nil, fmt.Errorf("zero buying-power multiplier requires the stress-unlimited margin profile")
	}
	if createdBy == "" {
		return nil, fmt.Errorf("account creator is required")
	}
	creationMetadata, err := normalizeJSONObject(input.CreationMetadata, "account creation metadata")
	if err != nil {
		return nil, err
	}

	evidenceClass := PaperEvidenceClassSynthetic
	if input.Environment == AccountEnvironmentPaperScored {
		evidenceClass = PaperEvidenceClassPromotion
	} else if input.Environment != AccountEnvironmentPaperStress {
		evidenceClass = "non_promotion"
	}

	createdAt := input.CreatedAt
	if input.CreatedAt.IsZero() {
		createdAt = time.Now()
	}
	createdAt = createdAt.UTC().Truncate(time.Microsecond)

	account := &Account{
		ID:                    uuid.New(),
		Name:                  name,
		Environment:           input.Environment,
		Venue:                 venue,
		ExternalAccountID:     strings.TrimSpace(input.ExternalAccountID),
		BaseCurrency:          currency,
		StorageNamespace:      namespace,
		EvidenceClass:         evidenceClass,
		StartingCapital:       input.StartingCapital,
		BuyingPowerMultiplier: input.BuyingPowerMultiplier,
		MarginProfile:         input.MarginProfile,
		Status:                AccountStatusActive,
		CreatedBy:             createdBy,
		CreationMetadata:      creationMetadata,
		CreatedAt:             createdAt,
	}
	if err := account.Validate(); err != nil {
		return nil, err
	}
	return account, nil
}

// NewCapitalFlow validates and materializes a capital-flow event before it is
// handed to the append-only repository.
func NewCapitalFlow(input CapitalFlowInput) (*CapitalFlow, error) {
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if input.AccountID == uuid.Nil {
		return nil, fmt.Errorf("capital-flow account ID is required")
	}
	if !input.Type.IsValid() {
		return nil, fmt.Errorf("invalid capital-flow type %q", input.Type)
	}
	if !input.Amount.IsPositive() {
		return nil, fmt.Errorf("capital-flow amount must be greater than zero")
	}
	if !hasDecimalScaleAtMost(input.Amount, 8) {
		return nil, fmt.Errorf("capital-flow amount supports at most 8 decimal places")
	}
	if !isCurrencyCode(currency) {
		return nil, fmt.Errorf("capital-flow currency must be a three-letter code")
	}
	if idempotencyKey == "" {
		return nil, fmt.Errorf("capital-flow idempotency key is required")
	}
	if !input.Source.IsValid() {
		return nil, fmt.Errorf("invalid capital-flow source %q", input.Source)
	}
	if input.EffectiveAt.IsZero() {
		return nil, fmt.Errorf("capital-flow effective time is required")
	}

	metadata, err := normalizeJSONObject(input.Metadata, "capital-flow metadata")
	if err != nil {
		return nil, err
	}

	observedAt := input.ObservedAt
	if input.ObservedAt.IsZero() {
		observedAt = time.Now()
	}
	observedAt = observedAt.UTC().Truncate(time.Microsecond)

	flow := &CapitalFlow{
		ID:                uuid.New(),
		AccountID:         input.AccountID,
		Type:              input.Type,
		Amount:            input.Amount,
		Currency:          currency,
		IdempotencyKey:    idempotencyKey,
		Source:            input.Source,
		ExternalReference: strings.TrimSpace(input.ExternalReference),
		Metadata:          metadata,
		EffectiveAt:       input.EffectiveAt.UTC().Truncate(time.Microsecond),
		ObservedAt:        observedAt,
	}
	if err := flow.Validate(); err != nil {
		return nil, err
	}
	return flow, nil
}

// Validate checks an account value at persistence boundaries. Constructors
// normalize inputs, but repositories call this again so manually assembled
// values cannot bypass financial invariants.
func (a Account) Validate() error {
	if a.ID == uuid.Nil {
		return fmt.Errorf("account ID is required")
	}
	if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Venue) == "" {
		return fmt.Errorf("account name and venue are required")
	}
	if !a.Environment.IsValid() || !a.MarginProfile.IsValid() || !a.Status.IsValid() {
		return fmt.Errorf("account environment, margin profile, or status is invalid")
	}
	if !isCurrencyCode(a.BaseCurrency) {
		return fmt.Errorf("account base currency must be a three-letter code")
	}
	if strings.TrimSpace(a.StorageNamespace) == "" {
		return fmt.Errorf("account storage namespace is required")
	}
	if (a.Environment == AccountEnvironmentPaperScored || a.Environment == AccountEnvironmentPaperStress) &&
		!strings.HasPrefix(a.StorageNamespace, string(a.Environment)+"/") {
		return fmt.Errorf("paper account storage namespace must be environment scoped")
	}
	if !a.StartingCapital.IsPositive() || !hasDecimalScaleAtMost(a.StartingCapital, 8) {
		return fmt.Errorf("account starting capital must be positive with at most 8 decimal places")
	}
	if a.BuyingPowerMultiplier.IsNegative() || !hasDecimalScaleAtMost(a.BuyingPowerMultiplier, 8) {
		return fmt.Errorf("account buying-power multiplier must be non-negative with at most 8 decimal places")
	}
	if a.Environment != AccountEnvironmentPaperStress && !a.BuyingPowerMultiplier.IsPositive() {
		return fmt.Errorf("non-stress account buying-power multiplier must be greater than zero")
	}
	if a.MarginProfile == MarginProfileStressUnlimited && a.Environment != AccountEnvironmentPaperStress {
		return fmt.Errorf("unlimited margin is restricted to stress accounts")
	}
	if a.Environment == AccountEnvironmentPaperStress && a.BuyingPowerMultiplier.IsZero() && a.MarginProfile != MarginProfileStressUnlimited {
		return fmt.Errorf("zero buying-power multiplier requires the stress-unlimited margin profile")
	}
	expectedEvidence := "non_promotion"
	switch a.Environment {
	case AccountEnvironmentPaperScored:
		expectedEvidence = PaperEvidenceClassPromotion
	case AccountEnvironmentPaperStress:
		expectedEvidence = PaperEvidenceClassSynthetic
	}
	if a.EvidenceClass != expectedEvidence {
		return fmt.Errorf("account evidence class %q does not match environment %q", a.EvidenceClass, a.Environment)
	}
	if strings.TrimSpace(a.CreatedBy) == "" || a.CreatedAt.IsZero() {
		return fmt.Errorf("account creation identity is required")
	}
	if !hasPostgresTimestampPrecision(a.CreatedAt) {
		return fmt.Errorf("account creation time must use PostgreSQL microsecond precision")
	}
	if _, err := normalizeJSONObject(a.CreationMetadata, "account creation metadata"); err != nil {
		return err
	}
	return nil
}

// Validate checks a capital flow at the append-only persistence boundary.
func (f CapitalFlow) Validate() error {
	if f.ID == uuid.Nil || f.AccountID == uuid.Nil {
		return fmt.Errorf("capital-flow ID and account ID are required")
	}
	if !f.Type.IsValid() || !f.Source.IsValid() {
		return fmt.Errorf("capital-flow type or source is invalid")
	}
	if !f.Amount.IsPositive() || !hasDecimalScaleAtMost(f.Amount, 8) {
		return fmt.Errorf("capital-flow amount must be positive with at most 8 decimal places")
	}
	if !isCurrencyCode(f.Currency) {
		return fmt.Errorf("capital-flow currency must be a three-letter code")
	}
	if strings.TrimSpace(f.IdempotencyKey) == "" {
		return fmt.Errorf("capital-flow idempotency key is required")
	}
	if f.EffectiveAt.IsZero() || f.ObservedAt.IsZero() {
		return fmt.Errorf("capital-flow effective and observed times are required")
	}
	if !hasPostgresTimestampPrecision(f.EffectiveAt) || !hasPostgresTimestampPrecision(f.ObservedAt) {
		return fmt.Errorf("capital-flow timestamps must use PostgreSQL microsecond precision")
	}
	if _, err := normalizeJSONObject(f.Metadata, "capital-flow metadata"); err != nil {
		return err
	}
	return nil
}

// PromotionEligible reports whether the account can produce promotion
// evidence. Stress, shadow, and future live identities are excluded here.
func (a Account) PromotionEligible() bool {
	return a.Environment == AccountEnvironmentPaperScored && a.EvidenceClass == PaperEvidenceClassPromotion
}

// CanShareStorageWith is true only when both accounts have the same explicit
// environment and namespace. Matching namespace text alone is never enough to
// mix scored and synthetic evidence.
func (a Account) CanShareStorageWith(other Account) bool {
	return a.StorageNamespace != "" &&
		a.StorageNamespace == other.StorageNamespace &&
		a.Environment == other.Environment
}

func (e AccountEnvironment) IsValid() bool {
	switch e {
	case AccountEnvironmentPaperScored, AccountEnvironmentPaperStress, AccountEnvironmentShadow, AccountEnvironmentLive:
		return true
	default:
		return false
	}
}

func (p MarginProfile) IsValid() bool {
	switch p {
	case MarginProfileCash, MarginProfileRegT, MarginProfilePortfolio, MarginProfileStressUnlimited:
		return true
	default:
		return false
	}
}

func (s AccountStatus) IsValid() bool {
	switch s {
	case AccountStatusActive, AccountStatusPaused, AccountStatusClosed:
		return true
	default:
		return false
	}
}

func (t CapitalFlowType) IsValid() bool {
	return t == CapitalFlowTypeDeposit || t == CapitalFlowTypeWithdrawal
}

func (s CapitalFlowSource) IsValid() bool {
	switch s {
	case CapitalFlowSourceAccountOpening, CapitalFlowSourceOperator, CapitalFlowSourceVenue, CapitalFlowSourceReconciliation, CapitalFlowSourceMigration:
		return true
	default:
		return false
	}
}

func hasPostgresTimestampPrecision(value time.Time) bool {
	return value.Equal(value.Truncate(time.Microsecond))
}

func isCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func hasDecimalScaleAtMost(value decimal.Decimal, places int32) bool {
	return value.Equal(value.Round(places))
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
