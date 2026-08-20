package capital

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestNewBindingAcceptsEveryScoredTierAndFiniteProfile(t *testing.T) {
	policy := bindingTestPolicy(t)
	profiles := []struct {
		name       domain.MarginProfile
		multiplier decimal.Decimal
	}{
		{domain.MarginProfileCash, decimal.NewFromInt(1)},
		{domain.MarginProfileRegT, decimal.NewFromInt(2)},
		{domain.MarginProfilePortfolio, decimal.NewFromInt(6)},
	}
	createdAt := bindingTestTime()
	for _, tier := range policy.Tiers() {
		for _, profile := range profiles {
			name := tier.String() + "/" + string(profile.name)
			t.Run(name, func(t *testing.T) {
				account := bindingTestAccount(t, domain.AccountEnvironmentPaperScored, tier, profile.name, profile.multiplier)
				binding, err := NewBinding(*account, policy, tier, profile.name, createdAt)
				if err != nil {
					t.Fatal(err)
				}
				wantID := economicid.DeterministicUUID("capital-policy-binding", account.ID.String(), policy.Version())
				if binding.ID != wantID || binding.AccountID != account.ID || binding.PolicyArtifactID != policy.ArtifactID() ||
					binding.PolicyVersion != policy.Version() || !binding.Tier.Equal(tier) || binding.Profile != profile.name ||
					binding.Environment != account.Environment || binding.EvidenceClass != account.EvidenceClass ||
					binding.StorageNamespace != account.StorageNamespace || binding.Currency != account.BaseCurrency ||
					!binding.StartingCapital.Equal(account.StartingCapital) ||
					!binding.BuyingPowerMultiplier.Equal(account.BuyingPowerMultiplier) || binding.CreatedAt != createdAt {
					t.Fatalf("binding = %+v", binding)
				}
				if err := binding.Validate(*account, policy); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestNewBindingAcceptsOnlyIsolatedStressUnlimited(t *testing.T) {
	policy := bindingTestPolicy(t)
	tier := decimal.NewFromInt(5_000_000)
	account := bindingTestAccount(t, domain.AccountEnvironmentPaperStress, tier, domain.MarginProfileStressUnlimited, decimal.Zero)
	binding, err := NewBinding(*account, policy, tier, domain.MarginProfileStressUnlimited, bindingTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if binding.Environment != domain.AccountEnvironmentPaperStress ||
		binding.EvidenceClass != domain.PaperEvidenceClassSynthetic || binding.Profile != domain.MarginProfileStressUnlimited ||
		!binding.BuyingPowerMultiplier.IsZero() || binding.PromotionEligible() {
		t.Fatalf("stress binding = %+v", binding)
	}
}

func TestBindingRejectsAccountPolicyTierAndProfileMismatch(t *testing.T) {
	policy := bindingTestPolicy(t)
	baseTier := decimal.NewFromInt(100_000)
	tests := map[string]func(*domain.Account, *decimal.Decimal, *domain.MarginProfile){
		"shadow": func(account *domain.Account, _ *decimal.Decimal, _ *domain.MarginProfile) {
			account.Environment = domain.AccountEnvironmentShadow
			account.EvidenceClass = "non_promotion"
			account.StorageNamespace = "shadow/test"
		},
		"live": func(account *domain.Account, _ *decimal.Decimal, _ *domain.MarginProfile) {
			account.Environment = domain.AccountEnvironmentLive
			account.EvidenceClass = "non_promotion"
			account.StorageNamespace = "live/test"
		},
		"currency": func(account *domain.Account, _ *decimal.Decimal, _ *domain.MarginProfile) {
			account.BaseCurrency = "EUR"
		},
		"starting tier mismatch": func(account *domain.Account, tier *decimal.Decimal, _ *domain.MarginProfile) {
			*tier = decimal.NewFromInt(25_000)
		},
		"unknown tier": func(account *domain.Account, tier *decimal.Decimal, _ *domain.MarginProfile) {
			account.StartingCapital = decimal.NewFromInt(99_999)
			*tier = account.StartingCapital
		},
		"profile mismatch": func(_ *domain.Account, _ *decimal.Decimal, profile *domain.MarginProfile) {
			*profile = domain.MarginProfileCash
		},
		"multiplier mismatch": func(account *domain.Account, _ *decimal.Decimal, _ *domain.MarginProfile) {
			account.BuyingPowerMultiplier = decimal.NewFromInt(1)
		},
		"scored unlimited": func(account *domain.Account, _ *decimal.Decimal, profile *domain.MarginProfile) {
			account.MarginProfile = domain.MarginProfileStressUnlimited
			account.BuyingPowerMultiplier = decimal.Zero
			*profile = domain.MarginProfileStressUnlimited
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			account := bindingTestAccount(t, domain.AccountEnvironmentPaperScored, baseTier, domain.MarginProfileRegT, decimal.NewFromInt(2))
			tier := baseTier
			profile := domain.MarginProfileRegT
			mutate(account, &tier, &profile)
			if _, err := NewBinding(*account, policy, tier, profile, bindingTestTime()); err == nil {
				t.Fatal("NewBinding unexpectedly succeeded")
			}
		})
	}

	stressTests := map[string]func(*domain.Account, *domain.MarginProfile){
		"finite stress": func(account *domain.Account, profile *domain.MarginProfile) {
			account.MarginProfile = domain.MarginProfileCash
			account.BuyingPowerMultiplier = decimal.NewFromInt(1)
			*profile = domain.MarginProfileCash
		},
		"nonzero unlimited multiplier": func(account *domain.Account, _ *domain.MarginProfile) {
			account.BuyingPowerMultiplier = decimal.NewFromInt(1)
		},
		"promotion evidence": func(account *domain.Account, _ *domain.MarginProfile) {
			account.EvidenceClass = domain.PaperEvidenceClassPromotion
		},
		"scored namespace": func(account *domain.Account, _ *domain.MarginProfile) {
			account.StorageNamespace = "paper_scored/test"
		},
	}
	for name, mutate := range stressTests {
		t.Run(name, func(t *testing.T) {
			account := bindingTestAccount(t, domain.AccountEnvironmentPaperStress, baseTier, domain.MarginProfileStressUnlimited, decimal.Zero)
			profile := domain.MarginProfileStressUnlimited
			mutate(account, &profile)
			if _, err := NewBinding(*account, policy, baseTier, profile, bindingTestTime()); err == nil {
				t.Fatal("NewBinding unexpectedly succeeded")
			}
		})
	}
}

func TestBindingValidationRejectsEveryChangedPersistedFact(t *testing.T) {
	policy := bindingTestPolicy(t)
	account := bindingTestAccount(t, domain.AccountEnvironmentPaperScored, decimal.NewFromInt(100_000), domain.MarginProfileRegT, decimal.NewFromInt(2))
	binding, err := NewBinding(*account, policy, account.StartingCapital, account.MarginProfile, bindingTestTime())
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*Binding){
		"id":               func(value *Binding) { value.ID = uuid.New() },
		"account":          func(value *Binding) { value.AccountID = uuid.New() },
		"artifact":         func(value *Binding) { value.PolicyArtifactID = uuid.New() },
		"version":          func(value *Binding) { value.PolicyVersion += "x" },
		"tier":             func(value *Binding) { value.Tier = decimal.NewFromInt(25_000) },
		"profile":          func(value *Binding) { value.Profile = domain.MarginProfileCash },
		"environment":      func(value *Binding) { value.Environment = domain.AccountEnvironmentPaperStress },
		"starting capital": func(value *Binding) { value.StartingCapital = decimal.NewFromInt(99_999) },
		"multiplier":       func(value *Binding) { value.BuyingPowerMultiplier = decimal.NewFromInt(1) },
		"evidence":         func(value *Binding) { value.EvidenceClass = domain.PaperEvidenceClassSynthetic },
		"namespace":        func(value *Binding) { value.StorageNamespace += "x" },
		"currency":         func(value *Binding) { value.Currency = "EUR" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := *binding
			mutate(&changed)
			if err := changed.Validate(*account, policy); err == nil {
				t.Fatal("Validate unexpectedly succeeded")
			}
			if SameBindingPayload(binding, &changed) {
				t.Fatal("changed binding payload compared equal")
			}
		})
	}
	for name, mutate := range map[string]func(*Binding){
		"nonmicrosecond time":   func(value *Binding) { value.CreatedAt = value.CreatedAt.Add(time.Nanosecond) },
		"non-UTC creation time": func(value *Binding) { value.CreatedAt = value.CreatedAt.In(time.FixedZone("offset", 3600)) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := *binding
			mutate(&changed)
			if err := changed.Validate(*account, policy); err == nil {
				t.Fatal("Validate unexpectedly succeeded")
			}
		})
	}

	replayed := *binding
	replayed.CreatedAt = replayed.CreatedAt.Add(time.Hour)
	if !SameBindingPayload(binding, &replayed) {
		t.Fatal("local persistence time changed semantic payload")
	}
}

func bindingTestPolicy(t *testing.T) *Policy {
	t.Helper()
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func bindingTestAccount(
	t *testing.T,
	environment domain.AccountEnvironment,
	tier decimal.Decimal,
	profile domain.MarginProfile,
	multiplier decimal.Decimal,
) *domain.Account {
	t.Helper()
	account, err := domain.NewAccount(domain.AccountInput{
		Name:        "capital binding " + tier.String() + " " + string(profile),
		Environment: environment, Venue: "internal", BaseCurrency: "USD",
		StorageNamespace: string(environment) + "/capital-binding-" + uuid.NewString(),
		StartingCapital:  tier, BuyingPowerMultiplier: multiplier, MarginProfile: profile,
		CreatedBy: "capital-binding-test", CreationMetadata: json.RawMessage(`{"fixture":"capital-binding"}`),
		CreatedAt: bindingTestTime().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func bindingTestTime() time.Time {
	return time.Date(2026, 8, 20, 14, 0, 0, 123456000, time.UTC)
}
