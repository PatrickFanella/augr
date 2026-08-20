package capital

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

const (
	assessmentSchema = "capital-assessment-v1"
	assessmentDomain = "capital-assessment"
)

type ExposureDirection string

const (
	ExposureIncreaseLong  ExposureDirection = "increase_long"
	ExposureIncreaseShort ExposureDirection = "increase_short"
	ExposureReduceLong    ExposureDirection = "reduce_long"
	ExposureReduceShort   ExposureDirection = "reduce_short"
)

type Decision string

const (
	DecisionAdmitted Decision = "admitted"
	DecisionRejected Decision = "rejected"
)

type ReasonCode string

const (
	ReasonAdmitted                ReasonCode = "admitted"
	ReasonStressUnbounded         ReasonCode = "stress_unbounded"
	ReasonShortNotSupported       ReasonCode = "short_not_supported"
	ReasonMaintenanceBreach       ReasonCode = "maintenance_breach"
	ReasonInsufficientSettledCash ReasonCode = "insufficient_settled_cash"
	ReasonReserveBreach           ReasonCode = "reserve_breach"
	ReasonInsufficientBuyingPower ReasonCode = "insufficient_buying_power"
	ReasonGrossExposureBreach     ReasonCode = "gross_exposure_breach"
)

// AssessmentInput joins immutable account/policy/state facts to one exact
// canonical proposed notional already derived from quote/instrument mechanics.
type AssessmentInput struct {
	Account          domain.Account
	Binding          Binding
	Policy           *Policy
	State            *State
	Instrument       instrument.Instrument
	Currency         string
	ScenarioID       string
	Direction        ExposureDirection
	ProposedNotional decimal.Decimal
}

// Assessment is a deterministic pre-route result. Rejection is evidence, not
// an engine error. Canonical bytes remain private and are revalidated at
// persistence boundaries.
type Assessment struct {
	ID                         uuid.UUID
	StateID                    uuid.UUID
	AccountID                  uuid.UUID
	BindingID                  uuid.UUID
	PolicyVersion              string
	InstrumentID               uuid.UUID
	ScenarioID                 string
	Direction                  ExposureDirection
	Currency                   string
	ProposedNotional           decimal.Decimal
	CurrentInitialRequirement  decimal.Decimal
	RequestedInitialMargin     decimal.Decimal
	InitialMarginHeadroom      decimal.Decimal
	GrossHeadroom              decimal.Decimal
	AvailableBuyingPower       decimal.Decimal
	PostGrossExposure          decimal.Decimal
	PostMaintenanceRequirement decimal.Decimal
	Decision                   Decision
	Reason                     ReasonCode
	Unbounded                  bool
	Environment                domain.AccountEnvironment
	EvidenceClass              string
	StorageNamespace           string
	canonicalBytes             json.RawMessage
	hash                       string
}

type canonicalAssessment struct {
	Schema                     string `json:"schema"`
	StateID                    string `json:"state_id"`
	AccountID                  string `json:"account_id"`
	BindingID                  string `json:"binding_id"`
	PolicyVersion              string `json:"policy_version"`
	InstrumentID               string `json:"instrument_id"`
	ScenarioID                 string `json:"scenario_id"`
	Direction                  string `json:"direction"`
	Currency                   string `json:"currency"`
	ProposedNotional           string `json:"proposed_notional"`
	CurrentInitialRequirement  string `json:"current_initial_requirement"`
	RequestedInitialMargin     string `json:"requested_initial_margin"`
	InitialMarginHeadroom      string `json:"initial_margin_headroom"`
	GrossHeadroom              string `json:"gross_headroom"`
	AvailableBuyingPower       string `json:"available_buying_power"`
	PostGrossExposure          string `json:"post_gross_exposure"`
	PostMaintenanceRequirement string `json:"post_maintenance_requirement"`
	Decision                   string `json:"decision"`
	Reason                     string `json:"reason"`
	Unbounded                  bool   `json:"unbounded"`
	Environment                string `json:"environment"`
	EvidenceClass              string `json:"evidence_class"`
	StorageNamespace           string `json:"storage_namespace"`
}

// Assess returns one canonical admission/rejection result or an error when
// input identity is malformed or contradictory.
func Assess(input AssessmentInput) (*Assessment, error) {
	if err := validateAssessmentInput(input); err != nil {
		return nil, fmt.Errorf("assess capital: %w", err)
	}
	profile, _ := input.Policy.Profile(input.Binding.Profile)
	assessment := &Assessment{
		StateID: input.State.ID(), AccountID: input.Account.ID, BindingID: input.Binding.ID,
		PolicyVersion: input.Policy.Version(), InstrumentID: input.Instrument.ID,
		ScenarioID: input.ScenarioID, Direction: input.Direction, Currency: input.Currency,
		ProposedNotional: input.ProposedNotional, Environment: input.Account.Environment,
		EvidenceClass: input.Account.EvidenceClass, StorageNamespace: input.Account.StorageNamespace,
	}
	if profile.Unlimited {
		assessment.Unbounded = true
		assessment.Decision = DecisionAdmitted
		assessment.Reason = ReasonStressUnbounded
		assessment.PostGrossExposure = postGrossExposure(input.State, input.Direction, input.ProposedNotional)
		return sealAssessment(assessment, input)
	}

	assessment.CurrentInitialRequirement = roundCapitalUp(
		input.State.longExposure.Mul(profile.InitialLong).Add(input.State.shortExposure.Mul(profile.InitialShort)),
		input.Policy.Scale(),
	)
	assessment.InitialMarginHeadroom = nonnegative(input.State.equity.Sub(assessment.CurrentInitialRequirement))
	assessment.GrossHeadroom = nonnegative(input.State.equity.Mul(profile.MaximumGross).Sub(input.State.grossExposure))

	ratio := decimal.Zero
	switch input.Direction {
	case ExposureIncreaseLong:
		ratio = profile.InitialLong
	case ExposureIncreaseShort:
		if !profile.AllowShort {
			assessment.Decision = DecisionRejected
			assessment.Reason = ReasonShortNotSupported
			return sealAssessment(assessment, input)
		}
		ratio = profile.InitialShort
	case ExposureReduceLong, ExposureReduceShort:
		// Reductions require no new initial margin.
	}
	assessment.RequestedInitialMargin = roundCapitalUp(input.ProposedNotional.Mul(ratio), input.Policy.Scale())
	assessment.AvailableBuyingPower = assessment.GrossHeadroom
	if ratio.IsPositive() {
		marginCapacity := assessment.InitialMarginHeadroom.Div(ratio).RoundFloor(input.Policy.Scale())
		assessment.AvailableBuyingPower = decimal.Min(assessment.GrossHeadroom, marginCapacity)
	}
	assessment.PostGrossExposure = postGrossExposure(input.State, input.Direction, input.ProposedNotional)
	postLong, postShort := postDirectionalExposure(input.State, input.Direction, input.ProposedNotional)
	assessment.PostMaintenanceRequirement = roundCapitalUp(
		postLong.Mul(profile.MaintenanceLong).Add(postShort.Mul(profile.MaintenanceShort)),
		input.Policy.Scale(),
	)

	if isExposureReduction(input.Direction) {
		assessment.Decision = DecisionAdmitted
		assessment.Reason = ReasonAdmitted
		return sealAssessment(assessment, input)
	}
	if input.State.maintenanceRequirement.GreaterThan(input.State.equity) {
		assessment.Decision = DecisionRejected
		assessment.Reason = ReasonMaintenanceBreach
		return sealAssessment(assessment, input)
	}
	if input.Binding.Profile == domain.MarginProfileCash && input.Direction == ExposureIncreaseLong {
		if input.ProposedNotional.GreaterThan(input.State.cash) {
			assessment.Decision = DecisionRejected
			assessment.Reason = ReasonInsufficientSettledCash
			return sealAssessment(assessment, input)
		}
		reserve := input.Binding.Tier.Mul(profile.CashReserve)
		if input.State.cash.Sub(input.ProposedNotional).LessThan(reserve) {
			assessment.Decision = DecisionRejected
			assessment.Reason = ReasonReserveBreach
			return sealAssessment(assessment, input)
		}
	}
	if assessment.RequestedInitialMargin.GreaterThan(assessment.InitialMarginHeadroom) {
		assessment.Decision = DecisionRejected
		assessment.Reason = ReasonInsufficientBuyingPower
		return sealAssessment(assessment, input)
	}
	if input.ProposedNotional.GreaterThan(assessment.GrossHeadroom) {
		assessment.Decision = DecisionRejected
		assessment.Reason = ReasonGrossExposureBreach
		return sealAssessment(assessment, input)
	}
	if assessment.PostMaintenanceRequirement.GreaterThan(input.State.equity) {
		assessment.Decision = DecisionRejected
		assessment.Reason = ReasonMaintenanceBreach
		return sealAssessment(assessment, input)
	}
	assessment.Decision = DecisionAdmitted
	assessment.Reason = ReasonAdmitted
	return sealAssessment(assessment, input)
}

func validateAssessmentInput(input AssessmentInput) error {
	if input.Policy == nil || input.State == nil {
		return fmt.Errorf("policy and builder-produced state are required")
	}
	if err := input.Binding.Validate(input.Account, input.Policy); err != nil {
		return err
	}
	if err := input.State.validate(input.Account, input.Binding, input.Policy); err != nil {
		return err
	}
	if err := input.Instrument.Validate(); err != nil {
		return fmt.Errorf("proposed instrument: %w", err)
	}
	if input.Instrument.AssetClass != instrument.AssetClassEquity && input.Instrument.AssetClass != instrument.AssetClassETF {
		return fmt.Errorf("proposed instrument asset class %q is unsupported", input.Instrument.AssetClass)
	}
	if input.Currency != "USD" || input.Currency != input.Account.BaseCurrency ||
		input.Instrument.Currency != input.Currency {
		return fmt.Errorf("assessment currency context does not match")
	}
	if input.ScenarioID == "" || input.ScenarioID != strings.TrimSpace(input.ScenarioID) || len(input.ScenarioID) > 256 {
		return fmt.Errorf("canonical scenario ID is required")
	}
	if !input.Direction.IsValid() || !input.ProposedNotional.IsPositive() || !validCapitalAmount(input.ProposedNotional) {
		return fmt.Errorf("direction and positive exact proposed notional are required")
	}
	if input.Direction == ExposureReduceLong && input.ProposedNotional.GreaterThan(input.State.longExposure) {
		return fmt.Errorf("long reduction exceeds current long exposure")
	}
	if input.Direction == ExposureReduceShort && input.ProposedNotional.GreaterThan(input.State.shortExposure) {
		return fmt.Errorf("short reduction exceeds current short exposure")
	}
	return nil
}

func sealAssessment(assessment *Assessment, input AssessmentInput) (*Assessment, error) {
	canonical := assessment.canonical()
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal capital assessment: %w", err)
	}
	digestBytes := sha256.Sum256(encoded)
	assessment.canonicalBytes = encoded
	assessment.hash = hex.EncodeToString(digestBytes[:])
	assessment.ID = economicid.DeterministicUUID(assessmentDomain, assessment.hash)
	if err := assessment.Validate(input.Account, input.Binding, input.Policy, input.State); err != nil {
		return nil, fmt.Errorf("seal capital assessment: %w", err)
	}
	return assessment, nil
}

func (assessment *Assessment) Validate(account domain.Account, binding Binding, policy *Policy, state *State) error {
	if assessment == nil || policy == nil || state == nil || assessment.ID == uuid.Nil || len(assessment.hash) != 64 {
		return fmt.Errorf("capital assessment identity is invalid")
	}
	if err := binding.Validate(account, policy); err != nil {
		return err
	}
	if err := state.validate(account, binding, policy); err != nil {
		return err
	}
	if assessment.StateID != state.ID() || assessment.AccountID != account.ID || assessment.BindingID != binding.ID ||
		assessment.PolicyVersion != policy.Version() || assessment.Environment != account.Environment ||
		assessment.EvidenceClass != account.EvidenceClass || assessment.StorageNamespace != account.StorageNamespace ||
		assessment.InstrumentID == uuid.Nil || assessment.ScenarioID == "" || !assessment.Direction.IsValid() ||
		assessment.Currency != account.BaseCurrency || !assessment.ProposedNotional.IsPositive() {
		return fmt.Errorf("capital assessment context is invalid")
	}
	for _, value := range []decimal.Decimal{
		assessment.ProposedNotional, assessment.CurrentInitialRequirement, assessment.RequestedInitialMargin,
		assessment.InitialMarginHeadroom, assessment.GrossHeadroom, assessment.AvailableBuyingPower,
		assessment.PostGrossExposure, assessment.PostMaintenanceRequirement,
	} {
		if !validCapitalAmount(value) || value.IsNegative() {
			return fmt.Errorf("capital assessment amount is invalid")
		}
	}
	if !assessment.Decision.IsValid() || !assessment.Reason.IsValid() ||
		(assessment.Unbounded != (assessment.Reason == ReasonStressUnbounded)) ||
		(assessment.Decision == DecisionAdmitted) != (assessment.Reason == ReasonAdmitted || assessment.Reason == ReasonStressUnbounded) {
		return fmt.Errorf("capital assessment decision or reason is inconsistent")
	}
	encoded, err := json.Marshal(assessment.canonical())
	if err != nil {
		return err
	}
	digestBytes := sha256.Sum256(encoded)
	digest := hex.EncodeToString(digestBytes[:])
	if !bytes.Equal(encoded, assessment.canonicalBytes) || digest != assessment.hash ||
		assessment.ID != economicid.DeterministicUUID(assessmentDomain, digest) {
		return fmt.Errorf("capital assessment canonical evidence does not match fields")
	}
	return nil
}

func (assessment *Assessment) canonical() canonicalAssessment {
	return canonicalAssessment{
		Schema: assessmentSchema, StateID: assessment.StateID.String(), AccountID: assessment.AccountID.String(),
		BindingID: assessment.BindingID.String(), PolicyVersion: assessment.PolicyVersion,
		InstrumentID: assessment.InstrumentID.String(), ScenarioID: assessment.ScenarioID,
		Direction: string(assessment.Direction), Currency: assessment.Currency,
		ProposedNotional:          assessment.ProposedNotional.String(),
		CurrentInitialRequirement: assessment.CurrentInitialRequirement.String(),
		RequestedInitialMargin:    assessment.RequestedInitialMargin.String(),
		InitialMarginHeadroom:     assessment.InitialMarginHeadroom.String(), GrossHeadroom: assessment.GrossHeadroom.String(),
		AvailableBuyingPower: assessment.AvailableBuyingPower.String(), PostGrossExposure: assessment.PostGrossExposure.String(),
		PostMaintenanceRequirement: assessment.PostMaintenanceRequirement.String(), Decision: string(assessment.Decision),
		Reason: string(assessment.Reason), Unbounded: assessment.Unbounded, Environment: string(assessment.Environment),
		EvidenceClass: assessment.EvidenceClass, StorageNamespace: assessment.StorageNamespace,
	}
}

func postDirectionalExposure(state *State, direction ExposureDirection, notional decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	longExposure := state.longExposure
	shortExposure := state.shortExposure
	switch direction {
	case ExposureIncreaseLong:
		longExposure = longExposure.Add(notional)
	case ExposureIncreaseShort:
		shortExposure = shortExposure.Add(notional)
	case ExposureReduceLong:
		longExposure = longExposure.Sub(notional)
	case ExposureReduceShort:
		shortExposure = shortExposure.Sub(notional)
	}
	return longExposure, shortExposure
}

func postGrossExposure(state *State, direction ExposureDirection, notional decimal.Decimal) decimal.Decimal {
	longExposure, shortExposure := postDirectionalExposure(state, direction, notional)
	return longExposure.Add(shortExposure)
}

func nonnegative(value decimal.Decimal) decimal.Decimal {
	if value.IsNegative() {
		return decimal.Zero
	}
	return value
}

func isExposureReduction(direction ExposureDirection) bool {
	return direction == ExposureReduceLong || direction == ExposureReduceShort
}

func (direction ExposureDirection) IsValid() bool {
	switch direction {
	case ExposureIncreaseLong, ExposureIncreaseShort, ExposureReduceLong, ExposureReduceShort:
		return true
	default:
		return false
	}
}

func (decision Decision) IsValid() bool {
	return decision == DecisionAdmitted || decision == DecisionRejected
}

func (reason ReasonCode) IsValid() bool {
	switch reason {
	case ReasonAdmitted, ReasonStressUnbounded, ReasonShortNotSupported, ReasonMaintenanceBreach,
		ReasonInsufficientSettledCash, ReasonReserveBreach, ReasonInsufficientBuyingPower, ReasonGrossExposureBreach:
		return true
	default:
		return false
	}
}

func (assessment *Assessment) PromotionEligible() bool {
	return assessment != nil && !assessment.Unbounded &&
		assessment.Environment == domain.AccountEnvironmentPaperScored &&
		assessment.EvidenceClass == domain.PaperEvidenceClassPromotion
}

func (assessment *Assessment) CanonicalBytes() json.RawMessage {
	if assessment == nil {
		return nil
	}
	return append(json.RawMessage(nil), assessment.canonicalBytes...)
}

func (assessment *Assessment) Hash() string {
	if assessment == nil {
		return ""
	}
	return assessment.hash
}
