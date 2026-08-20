package capital

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const bindingIDDomain = "capital-policy-binding"

// Binding pins one immutable account identity to one reviewed policy, capital
// tier, and margin profile. CreatedAt is local persistence evidence and is not
// part of semantic replay equality.
type Binding struct {
	ID                    uuid.UUID
	AccountID             uuid.UUID
	PolicyArtifactID      uuid.UUID
	PolicyVersion         string
	Tier                  decimal.Decimal
	Profile               domain.MarginProfile
	Environment           domain.AccountEnvironment
	StartingCapital       decimal.Decimal
	BuyingPowerMultiplier decimal.Decimal
	EvidenceClass         string
	StorageNamespace      string
	Currency              string
	CreatedAt             time.Time
}

// NewBinding validates account and policy identity before materializing an
// append-only binding.
func NewBinding(
	account domain.Account,
	policy *Policy,
	tier decimal.Decimal,
	profile domain.MarginProfile,
	createdAt time.Time,
) (*Binding, error) {
	if policy == nil {
		return nil, fmt.Errorf("capital binding policy is required")
	}
	if err := account.Validate(); err != nil {
		return nil, fmt.Errorf("capital binding account: %w", err)
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	binding := &Binding{
		ID:        economicid.DeterministicUUID(bindingIDDomain, account.ID.String(), policy.Version()),
		AccountID: account.ID, PolicyArtifactID: policy.ArtifactID(), PolicyVersion: policy.Version(),
		Tier: tier, Profile: profile, Environment: account.Environment,
		StartingCapital: account.StartingCapital, BuyingPowerMultiplier: account.BuyingPowerMultiplier,
		EvidenceClass: account.EvidenceClass, StorageNamespace: account.StorageNamespace,
		Currency: account.BaseCurrency, CreatedAt: createdAt,
	}
	if err := binding.Validate(account, policy); err != nil {
		return nil, err
	}
	return binding, nil
}

// Validate checks the persisted binding against independently loaded account
// and policy facts.
func (binding Binding) Validate(account domain.Account, policy *Policy) error {
	if policy == nil {
		return fmt.Errorf("capital binding policy is required")
	}
	if err := account.Validate(); err != nil {
		return fmt.Errorf("capital binding account: %w", err)
	}
	if binding.ID == uuid.Nil || binding.AccountID == uuid.Nil || binding.PolicyArtifactID == uuid.Nil {
		return fmt.Errorf("capital binding identity is required")
	}
	if binding.PolicyVersion == "" || binding.PolicyVersion != strings.TrimSpace(binding.PolicyVersion) ||
		len(binding.PolicyVersion) > 256 {
		return fmt.Errorf("capital binding policy version is invalid")
	}
	if binding.CreatedAt.IsZero() || binding.CreatedAt.Location() != time.UTC ||
		!binding.CreatedAt.Equal(binding.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("capital binding creation time must be UTC microsecond precision")
	}
	if binding.AccountID != account.ID || binding.PolicyArtifactID != policy.ArtifactID() ||
		binding.PolicyVersion != policy.Version() ||
		binding.ID != economicid.DeterministicUUID(bindingIDDomain, account.ID.String(), policy.Version()) {
		return fmt.Errorf("capital binding account or policy identity does not match")
	}
	if binding.Environment != account.Environment || !binding.StartingCapital.Equal(account.StartingCapital) ||
		!binding.BuyingPowerMultiplier.Equal(account.BuyingPowerMultiplier) || binding.Profile != account.MarginProfile ||
		binding.EvidenceClass != account.EvidenceClass || binding.StorageNamespace != account.StorageNamespace ||
		binding.Currency != account.BaseCurrency {
		return fmt.Errorf("capital binding copied account facts do not match")
	}
	if binding.Currency != policy.Currency() || binding.Currency != "USD" {
		return fmt.Errorf("capital binding currency is not supported by policy")
	}
	if !validPolicyDecimal(binding.Tier) || !binding.Tier.IsPositive() ||
		!binding.Tier.Equal(binding.StartingCapital) || !policyHasTier(policy, binding.Tier) {
		return fmt.Errorf("capital binding tier must equal one reviewed account starting-capital tier")
	}
	policyProfile, ok := policy.Profile(binding.Profile)
	if !ok {
		return fmt.Errorf("capital binding profile is not present in policy")
	}

	switch binding.Environment {
	case domain.AccountEnvironmentPaperScored:
		if binding.EvidenceClass != domain.PaperEvidenceClassPromotion ||
			!strings.HasPrefix(binding.StorageNamespace, string(domain.AccountEnvironmentPaperScored)+"/") ||
			binding.Profile == domain.MarginProfileStressUnlimited || policyProfile.Unlimited ||
			!binding.BuyingPowerMultiplier.Equal(policyProfile.MaximumGross) ||
			!binding.BuyingPowerMultiplier.IsPositive() {
			return fmt.Errorf("scored capital binding requires a matching finite promotion profile")
		}
	case domain.AccountEnvironmentPaperStress:
		if binding.EvidenceClass != domain.PaperEvidenceClassSynthetic ||
			!strings.HasPrefix(binding.StorageNamespace, string(domain.AccountEnvironmentPaperStress)+"/") ||
			binding.Profile != domain.MarginProfileStressUnlimited || !policyProfile.Unlimited ||
			!binding.BuyingPowerMultiplier.IsZero() {
			return fmt.Errorf("stress capital binding requires isolated stress-unlimited facts")
		}
	default:
		return fmt.Errorf("capital policy v1 supports paper-scored and paper-stress accounts only")
	}
	return nil
}

// PromotionEligible preserves the ADR-018 evidence boundary at the binding.
func (binding Binding) PromotionEligible() bool {
	return binding.Environment == domain.AccountEnvironmentPaperScored &&
		binding.EvidenceClass == domain.PaperEvidenceClassPromotion &&
		binding.Profile != domain.MarginProfileStressUnlimited
}

// SameBindingPayload compares semantic binding facts and excludes only local
// persistence time.
func SameBindingPayload(left, right *Binding) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID && left.AccountID == right.AccountID &&
		left.PolicyArtifactID == right.PolicyArtifactID && left.PolicyVersion == right.PolicyVersion &&
		left.Tier.Equal(right.Tier) && left.Profile == right.Profile && left.Environment == right.Environment &&
		left.StartingCapital.Equal(right.StartingCapital) &&
		left.BuyingPowerMultiplier.Equal(right.BuyingPowerMultiplier) &&
		left.EvidenceClass == right.EvidenceClass && left.StorageNamespace == right.StorageNamespace &&
		left.Currency == right.Currency
}

func policyHasTier(policy *Policy, tier decimal.Decimal) bool {
	for _, candidate := range policy.Tiers() {
		if candidate.Equal(tier) {
			return true
		}
	}
	return false
}
