package venue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

func TestReviewedPoliciesAreCanonicalContentAddressedAndImmutable(t *testing.T) {
	for _, provider := range []Provider{ProviderAlpaca, ProviderKalshi} {
		t.Run(string(provider), func(t *testing.T) {
			policy, err := ReviewedPolicy(provider)
			if err != nil {
				t.Fatalf("ReviewedPolicy() error = %v", err)
			}
			if policy.Schema() != PolicySchemaV1 || policy.Provider() != provider || policy.Venue() != string(provider) {
				t.Fatalf("policy identity = %q/%q/%q", policy.Schema(), policy.Provider(), policy.Venue())
			}
			if !strings.HasPrefix(policy.Version(), PolicySchemaV1+"@sha256:") || len(policy.Digest()) != 64 {
				t.Fatalf("version/digest = %q/%q", policy.Version(), policy.Digest())
			}
			wantID := economicid.DeterministicUUID("venue-adapter-policy-artifact", policy.Version())
			if policy.ArtifactID() != wantID {
				t.Fatalf("ArtifactID() = %s, want %s", policy.ArtifactID(), wantID)
			}
			var object map[string]any
			if err := json.Unmarshal(policy.CanonicalBytes(), &object); err != nil || object["schema"] != PolicySchemaV1 {
				t.Fatalf("canonical bytes = %s, error = %v", policy.CanonicalBytes(), err)
			}

			bytes := policy.CanonicalBytes()
			bytes[0] = '['
			if string(bytes) == string(policy.CanonicalBytes()) {
				t.Fatal("CanonicalBytes() exposed mutable storage")
			}
			capabilities := policy.Capabilities()
			capabilities[0].AssetClass = instrument.AssetClassFuture
			if policy.Capabilities()[0].AssetClass == instrument.AssetClassFuture {
				t.Fatal("Capabilities() exposed mutable storage")
			}
			mappings := policy.Mappings()
			mappings[0].Value = "changed"
			if policy.Mappings()[0].Value == "changed" {
				t.Fatal("Mappings() exposed mutable storage")
			}
		})
	}

	if _, err := ReviewedPolicy(Provider("other")); err == nil {
		t.Fatal("unknown provider unexpectedly received a reviewed policy")
	}
}

func TestPolicyArtifactRoundTripRejectsForgedButRehashedBytes(t *testing.T) {
	policy, err := ReviewedPolicy(ProviderAlpaca)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, time.August, 15, 15, 1, 2, 999, time.FixedZone("offset", -5*60*60))
	artifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.CreatedAt.Equal(createdAt.UTC().Truncate(time.Microsecond)) {
		t.Fatalf("CreatedAt = %s", artifact.CreatedAt)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	restored, err := PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version() != policy.Version() || string(restored.CanonicalBytes()) != string(policy.CanonicalBytes()) {
		t.Fatal("artifact did not restore the reviewed policy exactly")
	}

	var value map[string]any
	if err := json.Unmarshal(artifact.CanonicalBytes, &value); err != nil {
		t.Fatal(err)
	}
	value["fee_treatment"] = "invent_missing_fee_as_zero"
	forgedBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	forgedDigestBytes := sha256.Sum256(forgedBytes)
	forgedDigest := hex.EncodeToString(forgedDigestBytes[:])
	forged := *artifact
	forged.CanonicalBytes = forgedBytes
	forged.SHA256 = forgedDigest
	forged.Version = PolicySchemaV1 + "@sha256:" + forgedDigest
	forged.ID = economicid.DeterministicUUID("venue-adapter-policy-artifact", forged.Version)
	if err := forged.Validate(); err != nil {
		t.Fatalf("self-consistent forged artifact should reach semantic validation: %v", err)
	}
	if _, err := PolicyFromArtifact(forged); err == nil {
		t.Fatal("forged-but-rehashed policy unexpectedly restored")
	}

	noncanonical := *artifact
	noncanonical.CanonicalBytes = append(json.RawMessage(nil), artifact.CanonicalBytes...)
	noncanonical.CanonicalBytes = append(noncanonical.CanonicalBytes, ' ')
	if _, err := PolicyFromArtifact(noncanonical); err == nil {
		t.Fatal("changed canonical bytes unexpectedly restored")
	}
}

func TestReviewedPolicyCapabilitiesAreExactAndSorted(t *testing.T) {
	tests := []struct {
		provider Provider
		count    int
		allowed  []Capability
		denied   []Capability
	}{
		{
			provider: ProviderAlpaca,
			count:    29,
			allowed: []Capability{
				{AssetClass: instrument.AssetClassEquity, OrderType: lifecycle.OrderStopLimit, TimeInForce: lifecycle.TimeInForceGTC},
				{AssetClass: instrument.AssetClassETF, OrderType: lifecycle.OrderMarket, TimeInForce: lifecycle.TimeInForceFOK},
				{AssetClass: instrument.AssetClassCryptoSpot, OrderType: lifecycle.OrderStopLimit, TimeInForce: lifecycle.TimeInForceGTC},
				{AssetClass: instrument.AssetClassCryptoSpot, OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceIOC},
			},
			denied: []Capability{
				{AssetClass: instrument.AssetClassOption, OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceDay},
				{AssetClass: instrument.AssetClassEquity, OrderType: lifecycle.OrderStop, TimeInForce: lifecycle.TimeInForceIOC},
				{AssetClass: instrument.AssetClassCryptoSpot, OrderType: lifecycle.OrderMarket, TimeInForce: lifecycle.TimeInForceDay},
			},
		},
		{
			provider: ProviderKalshi,
			count:    3,
			allowed: []Capability{
				{AssetClass: instrument.AssetClassPredictionContract, OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceGTC},
				{AssetClass: instrument.AssetClassPredictionContract, OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceIOC},
				{AssetClass: instrument.AssetClassPredictionContract, OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceFOK},
			},
			denied: []Capability{
				{AssetClass: instrument.AssetClassPredictionContract, OrderType: lifecycle.OrderLimit, TimeInForce: lifecycle.TimeInForceDay},
				{AssetClass: instrument.AssetClassPredictionContract, OrderType: lifecycle.OrderMarket, TimeInForce: lifecycle.TimeInForceIOC},
			},
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.provider), func(t *testing.T) {
			policy, err := ReviewedPolicy(tc.provider)
			if err != nil {
				t.Fatal(err)
			}
			got := policy.Capabilities()
			if len(got) != tc.count {
				t.Fatalf("capability count = %d, want %d", len(got), tc.count)
			}
			if !sort.SliceIsSorted(got, func(i, j int) bool { return capabilityKey(got[i]) < capabilityKey(got[j]) }) {
				t.Fatalf("capabilities are not canonical: %#v", got)
			}
			for _, capability := range tc.allowed {
				if !policy.Supports(capability.AssetClass, capability.OrderType, capability.TimeInForce) {
					t.Fatalf("expected capability denied: %#v", capability)
				}
			}
			for _, capability := range tc.denied {
				if policy.Supports(capability.AssetClass, capability.OrderType, capability.TimeInForce) {
					t.Fatalf("unsupported capability accepted: %#v", capability)
				}
			}
		})
	}
}

func TestReviewedPolicyMappingsAreCompleteDistinctAndSorted(t *testing.T) {
	tests := []struct {
		provider Provider
		want     map[MappingNamespace]map[string]MappedOutcome
		legacy   []string
	}{
		{
			provider: ProviderAlpaca,
			want: map[MappingNamespace]map[string]MappedOutcome{
				MappingOrderStatus: {
					"accepted": OutcomeAcknowledge, "accepted_for_bidding": OutcomeAcknowledge,
					"calculated": OutcomeAcknowledge, "canceled": OutcomeCancelled,
					"done_for_day": OutcomeAcknowledge, "expired": OutcomeExpired,
					"filled": OutcomeFillNotice, "held": OutcomeAcknowledge,
					"new": OutcomeAcknowledge, "partially_filled": OutcomeFillNotice,
					"pending_cancel": OutcomeAcknowledge, "pending_new": OutcomeAcknowledge,
					"pending_replace": OutcomeContradiction, "rejected": OutcomeRejected,
					"replaced": OutcomeContradiction, "stopped": OutcomeAcknowledge,
					"suspended": OutcomeAcknowledge,
				},
				MappingTradeUpdate: {
					"calculated": OutcomeAcknowledge, "canceled": OutcomeCancelled,
					"done_for_day": OutcomeAcknowledge, "expired": OutcomeExpired,
					"fill": OutcomeFillNotice, "new": OutcomeAcknowledge,
					"order_cancel_rejected": OutcomeNoChange, "order_replace_rejected": OutcomeNoChange,
					"partial_fill": OutcomeFillNotice, "pending_cancel": OutcomeAcknowledge,
					"pending_new": OutcomeAcknowledge, "pending_replace": OutcomeContradiction,
					"rejected": OutcomeRejected, "replaced": OutcomeContradiction,
					"stopped": OutcomeAcknowledge, "suspended": OutcomeAcknowledge,
				},
				MappingAccountActivity: {
					"FILL": OutcomeFill, "trade_bust": OutcomeBust, "trade_correct": OutcomeCorrection,
				},
			},
			legacy: []string{"open", "cancelled", "complete"},
		},
		{
			provider: ProviderKalshi,
			want: map[MappingNamespace]map[string]MappedOutcome{
				MappingOrderStatus: {"resting": OutcomeAcknowledge, "canceled": OutcomeCancelled, "executed": OutcomeFillNotice},
				MappingFillRecord:  {"fill": OutcomeFill},
			},
			legacy: []string{"open", "filled", "partial", "cancelled", "rejected"},
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.provider), func(t *testing.T) {
			policy, err := ReviewedPolicy(tc.provider)
			if err != nil {
				t.Fatal(err)
			}
			all := policy.Mappings()
			if !sort.SliceIsSorted(all, func(i, j int) bool { return mappingKey(all[i]) < mappingKey(all[j]) }) {
				t.Fatalf("mappings are not canonical: %#v", all)
			}
			seen := make(map[string]struct{}, len(all))
			for _, mapping := range all {
				key := mappingKey(mapping)
				if _, duplicate := seen[key]; duplicate {
					t.Fatalf("duplicate mapping %q", key)
				}
				seen[key] = struct{}{}
			}
			wantCount := 0
			for namespace, values := range tc.want {
				wantCount += len(values)
				for value, outcome := range values {
					got, ok := policy.Mapping(namespace, value)
					if !ok || got != outcome {
						t.Fatalf("Mapping(%q, %q) = %q/%v, want %q", namespace, value, got, ok, outcome)
					}
				}
			}
			if len(all) != wantCount {
				t.Fatalf("mapping count = %d, want %d", len(all), wantCount)
			}
			for _, synonym := range tc.legacy {
				if _, ok := policy.Mapping(MappingOrderStatus, synonym); ok {
					t.Fatalf("legacy synonym %q entered reviewed policy", synonym)
				}
			}
			if _, ok := policy.Mapping(MappingOrderStatus, "provider_added_later"); ok {
				t.Fatal("unknown provider state unexpectedly mapped")
			}
		})
	}
}

func TestReviewedPolicyPinsFillAuthorityAndKalshiMetadataGrammar(t *testing.T) {
	alpaca, err := ReviewedPolicy(ProviderAlpaca)
	if err != nil {
		t.Fatal(err)
	}
	if alpaca.AuthoritativeFillNamespace() != "alpaca/account-activities/FILL" {
		t.Fatalf("Alpaca fill namespace = %q", alpaca.AuthoritativeFillNamespace())
	}
	if alpaca.ContractMetadata().Required {
		t.Fatalf("Alpaca contract metadata = %#v", alpaca.ContractMetadata())
	}

	kalshi, err := ReviewedPolicy(ProviderKalshi)
	if err != nil {
		t.Fatal(err)
	}
	if kalshi.AuthoritativeFillNamespace() != "kalshi/portfolio/fills" {
		t.Fatalf("Kalshi fill namespace = %q", kalshi.AuthoritativeFillNamespace())
	}
	metadata := kalshi.ContractMetadata()
	if !metadata.Required || !metadata.WholeObject || strings.Join(metadata.Path, "/") != "kalshi_v2/outcome" ||
		strings.Join(metadata.Values, ",") != "no,yes" {
		t.Fatalf("Kalshi contract metadata = %#v", metadata)
	}
	metadata.Path[0] = "changed"
	if kalshi.ContractMetadata().Path[0] != "kalshi_v2" {
		t.Fatal("ContractMetadata() exposed mutable storage")
	}
}
