package capital

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestReviewedPolicyV1HasExactCanonicalIdentity(t *testing.T) {
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":"capital-margin-policy-v1","currency":"USD","scale":12,"tiers":["500","5000","25000","100000","1000000","5000000"],"profiles":[{"name":"cash","initial_long":"1","initial_short":"0","maintenance_long":"1","maintenance_short":"0","maximum_gross":"1","cash_reserve":"0","allow_short":false,"unlimited":false},{"name":"portfolio","initial_long":"0.15","initial_short":"0.3","maintenance_long":"0.15","maintenance_short":"0.3","maximum_gross":"6","cash_reserve":"0","allow_short":true,"unlimited":false},{"name":"reg_t","initial_long":"0.5","initial_short":"1.5","maintenance_long":"0.25","maintenance_short":"0.3","maximum_gross":"2","cash_reserve":"0","allow_short":true,"unlimited":false},{"name":"stress_unlimited","initial_long":"0","initial_short":"0","maintenance_long":"0","maintenance_short":"0","maximum_gross":"0","cash_reserve":"0","allow_short":true,"unlimited":true}]}`
	if !bytes.Equal(policy.CanonicalBytes(), []byte(want)) {
		t.Fatalf("CanonicalBytes() = %s\nwant %s", policy.CanonicalBytes(), want)
	}
	digest := sha256.Sum256([]byte(want))
	wantDigest := hex.EncodeToString(digest[:])
	wantVersion := PolicySchemaV1 + "@sha256:" + wantDigest
	if policy.Schema() != PolicySchemaV1 || policy.Currency() != "USD" || policy.Scale() != 12 ||
		policy.Digest() != wantDigest || policy.Version() != wantVersion {
		t.Fatalf("policy identity = %q/%q/%d/%q/%q", policy.Schema(), policy.Currency(), policy.Scale(), policy.Digest(), policy.Version())
	}
	if wantID := economicid.DeterministicUUID("capital-margin-policy-artifact", wantVersion); policy.ArtifactID() != wantID {
		t.Fatalf("ArtifactID() = %s, want %s", policy.ArtifactID(), wantID)
	}
	if got := policy.Tiers(); !sameDecimals(got, reviewedCapitalTiers()) {
		t.Fatalf("Tiers() = %v", got)
	}
	for _, profileName := range []domain.MarginProfile{
		domain.MarginProfileCash,
		domain.MarginProfilePortfolio,
		domain.MarginProfileRegT,
		domain.MarginProfileStressUnlimited,
	} {
		if _, ok := policy.Profile(profileName); !ok {
			t.Fatalf("Profile(%q) missing", profileName)
		}
	}
}

func TestPolicyCanonicalizationIgnoresInputOrderingAndDefendsStorage(t *testing.T) {
	input := ReviewedPolicyV1Input()
	reverseDecimals(input.Tiers)
	reverseProfiles(input.Profiles)
	policy, err := NewPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	if policy.Version() != baseline.Version() || !bytes.Equal(policy.CanonicalBytes(), baseline.CanonicalBytes()) {
		t.Fatal("input ordering changed canonical policy identity")
	}

	canonical := policy.CanonicalBytes()
	canonical[0] = '['
	if bytes.Equal(canonical, policy.CanonicalBytes()) {
		t.Fatal("CanonicalBytes exposed mutable storage")
	}
	tiers := policy.Tiers()
	tiers[0] = decimal.NewFromInt(1)
	if sameDecimals(tiers, policy.Tiers()) {
		t.Fatal("Tiers exposed mutable storage")
	}
	profile, _ := policy.Profile(domain.MarginProfileRegT)
	profile.InitialLong = decimal.Zero
	reloaded, _ := policy.Profile(domain.MarginProfileRegT)
	if reloaded.InitialLong.IsZero() {
		t.Fatal("Profile exposed mutable storage")
	}
}

func TestPolicyRejectsNonReviewedShapeOrEconomics(t *testing.T) {
	tests := map[string]func(*PolicyInput){
		"schema":          func(input *PolicyInput) { input.Schema = "capital-margin-policy-v2" },
		"currency":        func(input *PolicyInput) { input.Currency = "EUR" },
		"scale":           func(input *PolicyInput) { input.Scale = 8 },
		"missing tier":    func(input *PolicyInput) { input.Tiers = input.Tiers[:5] },
		"extra tier":      func(input *PolicyInput) { input.Tiers = append(input.Tiers, decimal.NewFromInt(10_000_000)) },
		"duplicate tier":  func(input *PolicyInput) { input.Tiers[5] = input.Tiers[4] },
		"changed tier":    func(input *PolicyInput) { input.Tiers[0] = decimal.NewFromInt(501) },
		"fractional tier": func(input *PolicyInput) { input.Tiers[0] = decimal.RequireFromString("500.1") },
		"missing profile": func(input *PolicyInput) { input.Profiles = input.Profiles[:3] },
		"extra profile": func(input *PolicyInput) {
			input.Profiles = append(input.Profiles, input.Profiles[0])
		},
		"duplicate profile": func(input *PolicyInput) { input.Profiles[3].Name = input.Profiles[2].Name },
		"unknown profile":   func(input *PolicyInput) { input.Profiles[0].Name = domain.MarginProfile("future") },
		"changed initial":   func(input *PolicyInput) { input.Profiles[0].InitialLong = decimal.RequireFromString("0.9") },
		"negative":          func(input *PolicyInput) { input.Profiles[1].CashReserve = decimal.NewFromInt(-1) },
		"over scale": func(input *PolicyInput) {
			input.Profiles[1].MaintenanceLong = decimal.RequireFromString("0.1500000000001")
		},
		"finite unbounded": func(input *PolicyInput) { input.Profiles[0].Unlimited = true },
		"stress bounded":   func(input *PolicyInput) { input.Profiles[3].MaximumGross = decimal.NewFromInt(1) },
		"cash short":       func(input *PolicyInput) { input.Profiles[0].AllowShort = true },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := ReviewedPolicyV1Input()
			mutate(&input)
			if _, err := NewPolicy(input); err == nil {
				t.Fatal("NewPolicy unexpectedly succeeded")
			}
		})
	}
}

func TestPolicyArtifactRoundTripAndTamperDetection(t *testing.T) {
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 20, 12, 0, 0, 123456000, time.UTC)
	artifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ID == uuid.Nil || artifact.ID != policy.ArtifactID() || artifact.CreatedAt != createdAt {
		t.Fatalf("artifact = %+v", artifact)
	}
	restored, err := PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version() != policy.Version() || !bytes.Equal(restored.CanonicalBytes(), policy.CanonicalBytes()) {
		t.Fatal("restored policy changed")
	}

	mutations := map[string]func(*PolicyArtifact){
		"id":      func(value *PolicyArtifact) { value.ID = uuid.New() },
		"schema":  func(value *PolicyArtifact) { value.Schema = "wrong" },
		"version": func(value *PolicyArtifact) { value.Version += "x" },
		"digest":  func(value *PolicyArtifact) { value.SHA256 = strings.Repeat("0", 64) },
		"bytes": func(value *PolicyArtifact) {
			var decoded map[string]any
			if err := json.Unmarshal(value.CanonicalBytes, &decoded); err != nil {
				t.Fatal(err)
			}
			value.CanonicalBytes = append(append(json.RawMessage(nil), value.CanonicalBytes...), ' ')
		},
		"time": func(value *PolicyArtifact) { value.CreatedAt = value.CreatedAt.Add(time.Nanosecond) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := *artifact
			changed.CanonicalBytes = append(json.RawMessage(nil), artifact.CanonicalBytes...)
			mutate(&changed)
			if _, err := PolicyFromArtifact(changed); err == nil {
				t.Fatal("PolicyFromArtifact unexpectedly succeeded")
			}
		})
	}
}

func sameDecimals(left, right []decimal.Decimal) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].Equal(right[index]) {
			return false
		}
	}
	return true
}

func reverseDecimals(values []decimal.Decimal) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseProfiles(values []Profile) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
