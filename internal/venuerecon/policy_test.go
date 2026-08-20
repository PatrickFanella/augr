package venuerecon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

func TestReviewedPolicyV1HasExactCanonicalIdentityAndVocabulary(t *testing.T) {
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":"venue-reconciliation-policy-v1","capture_count":2,"exact_decimals":true,"complete_pagination":true,"complete_fill_coverage":true,"canonical_contracts":true,"providers":[{"provider":"alpaca","authoritative_fill_namespace":"alpaca/account-activities/FILL","supports_revisions":true},{"provider":"kalshi","authoritative_fill_namespace":"kalshi/portfolio/fills","supports_revisions":false}],"kinds":["cash","fill","position","snapshot"],"statuses":["drift","matched","not_comparable"],"reasons":[{"code":"bust_pending","kind":"fill","status":"not_comparable","severity":"high"},{"code":"cash_matched","kind":"cash","status":"matched","severity":"none"},{"code":"cash_mismatch","kind":"cash","status":"drift","severity":"critical"},{"code":"correction_pending","kind":"fill","status":"not_comparable","severity":"high"},{"code":"equity_basis_not_comparable","kind":"cash","status":"not_comparable","severity":"high"},{"code":"fill_fee_mismatch","kind":"fill","status":"drift","severity":"critical"},{"code":"fill_instrument_mismatch","kind":"fill","status":"drift","severity":"critical"},{"code":"fill_local_missing","kind":"fill","status":"drift","severity":"critical"},{"code":"fill_matched","kind":"fill","status":"matched","severity":"none"},{"code":"fill_order_mismatch","kind":"fill","status":"drift","severity":"critical"},{"code":"fill_price_mismatch","kind":"fill","status":"drift","severity":"critical"},{"code":"fill_provider_missing","kind":"fill","status":"drift","severity":"critical"},{"code":"fill_quantity_mismatch","kind":"fill","status":"drift","severity":"critical"},{"code":"fill_side_mismatch","kind":"fill","status":"drift","severity":"critical"},{"code":"local_fill_after_frontier","kind":"snapshot","status":"not_comparable","severity":"high"},{"code":"local_fill_incomplete","kind":"snapshot","status":"not_comparable","severity":"high"},{"code":"position_local_missing","kind":"position","status":"drift","severity":"critical"},{"code":"position_matched","kind":"position","status":"matched","severity":"none"},{"code":"position_provider_missing","kind":"position","status":"drift","severity":"critical"},{"code":"position_quantity_mismatch","kind":"position","status":"drift","severity":"critical"},{"code":"provider_unavailable","kind":"snapshot","status":"not_comparable","severity":"high"},{"code":"snapshot_incomplete","kind":"snapshot","status":"not_comparable","severity":"high"},{"code":"snapshot_mapping_failure","kind":"snapshot","status":"not_comparable","severity":"high"},{"code":"snapshot_matched","kind":"snapshot","status":"matched","severity":"none"},{"code":"snapshot_unstable","kind":"snapshot","status":"not_comparable","severity":"high"},{"code":"unsupported_fact","kind":"snapshot","status":"not_comparable","severity":"high"}]}`
	if !bytes.Equal(policy.CanonicalBytes(), []byte(want)) {
		t.Fatalf("CanonicalBytes() = %s\nwant %s", policy.CanonicalBytes(), want)
	}
	digestBytes := sha256.Sum256([]byte(want))
	wantDigest := hex.EncodeToString(digestBytes[:])
	wantVersion := PolicySchemaV1 + "@sha256:" + wantDigest
	if policy.Schema() != PolicySchemaV1 || policy.CaptureCount() != 2 || !policy.ExactDecimals() ||
		!policy.CompletePagination() || !policy.CompleteFillCoverage() || !policy.CanonicalContracts() ||
		policy.Digest() != wantDigest || policy.Version() != wantVersion {
		t.Fatalf("policy scalar identity is invalid")
	}
	if policy.ArtifactID() != economicid.DeterministicUUID("venue-reconciliation-policy-artifact", wantVersion) {
		t.Fatal("policy artifact ID differs from reviewed identity")
	}
	if rule, ok := policy.ProviderRule(venue.ProviderAlpaca); !ok || !rule.SupportsRevisions {
		t.Fatalf("Alpaca rule = %+v/%v", rule, ok)
	}
	if rule, ok := policy.ProviderRule(venue.ProviderKalshi); !ok || rule.SupportsRevisions {
		t.Fatalf("Kalshi rule = %+v/%v", rule, ok)
	}
	for _, code := range []ReasonCode{ReasonCashMismatch, ReasonFillMatched, ReasonCorrectionPending, ReasonSnapshotUnstable} {
		if _, ok := policy.Reason(code); !ok {
			t.Fatalf("Reason(%q) missing", code)
		}
	}
}

func TestPolicyCanonicalizesOrderingAndDefendsStorage(t *testing.T) {
	input := ReviewedPolicyV1Input()
	reversePolicySlice(input.Providers)
	reversePolicySlice(input.Kinds)
	reversePolicySlice(input.Statuses)
	reversePolicySlice(input.Reasons)
	policy, err := NewPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	if policy.Version() != baseline.Version() || !bytes.Equal(policy.CanonicalBytes(), baseline.CanonicalBytes()) {
		t.Fatal("input ordering changed canonical policy")
	}
	encoded := policy.CanonicalBytes()
	encoded[0] = '['
	if bytes.Equal(encoded, policy.CanonicalBytes()) {
		t.Fatal("CanonicalBytes exposed mutable storage")
	}
	providers := policy.Providers()
	providers[0].AuthoritativeFillNamespace = "changed"
	if fresh := policy.Providers(); fresh[0].AuthoritativeFillNamespace == "changed" {
		t.Fatal("Providers exposed mutable storage")
	}
	reasons := policy.Reasons()
	reasons[0].Severity = SeverityNone
	if fresh := policy.Reasons(); fresh[0].Severity == SeverityNone {
		t.Fatal("Reasons exposed mutable storage")
	}
}

func TestPolicyRejectsEveryNonReviewedFact(t *testing.T) {
	tests := map[string]func(*PolicyInput){
		"schema":           func(v *PolicyInput) { v.Schema = "venue-reconciliation-policy-v2" },
		"capture count":    func(v *PolicyInput) { v.CaptureCount = 1 },
		"exact decimals":   func(v *PolicyInput) { v.ExactDecimals = false },
		"pagination":       func(v *PolicyInput) { v.CompletePagination = false },
		"fills":            func(v *PolicyInput) { v.CompleteFillCoverage = false },
		"contracts":        func(v *PolicyInput) { v.CanonicalContracts = false },
		"missing provider": func(v *PolicyInput) { v.Providers = v.Providers[:1] },
		"extra provider":   func(v *PolicyInput) { v.Providers = append(v.Providers, v.Providers[0]) },
		"unknown provider": func(v *PolicyInput) { v.Providers[0].Provider = venue.Provider("future") },
		"namespace":        func(v *PolicyInput) { v.Providers[0].AuthoritativeFillNamespace = "alpaca/fills" },
		"revision support": func(v *PolicyInput) { v.Providers[1].SupportsRevisions = true },
		"missing kind":     func(v *PolicyInput) { v.Kinds = v.Kinds[:3] },
		"duplicate kind":   func(v *PolicyInput) { v.Kinds[3] = v.Kinds[2] },
		"unknown kind":     func(v *PolicyInput) { v.Kinds[0] = ComparisonKind("order") },
		"missing status":   func(v *PolicyInput) { v.Statuses = v.Statuses[:2] },
		"unknown status":   func(v *PolicyInput) { v.Statuses[0] = ResultStatus("equal") },
		"missing reason":   func(v *PolicyInput) { v.Reasons = v.Reasons[:len(v.Reasons)-1] },
		"duplicate reason": func(v *PolicyInput) { v.Reasons[len(v.Reasons)-1] = v.Reasons[0] },
		"unknown reason":   func(v *PolicyInput) { v.Reasons[0].Code = ReasonCode("future") },
		"reason kind":      func(v *PolicyInput) { v.Reasons[0].Kind = KindCash },
		"reason status":    func(v *PolicyInput) { v.Reasons[0].Status = StatusMatched },
		"reason severity":  func(v *PolicyInput) { v.Reasons[0].Severity = SeverityCritical },
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
	restored, err := PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version() != policy.Version() || !bytes.Equal(restored.CanonicalBytes(), policy.CanonicalBytes()) ||
		!SamePolicyArtifactPayload(artifact, artifact) {
		t.Fatal("artifact round trip changed policy")
	}

	mutations := map[string]func(*PolicyArtifact){
		"id":      func(v *PolicyArtifact) { v.ID = uuid.New() },
		"schema":  func(v *PolicyArtifact) { v.Schema = "wrong" },
		"version": func(v *PolicyArtifact) { v.Version += "x" },
		"digest":  func(v *PolicyArtifact) { v.SHA256 = strings.Repeat("0", 64) },
		"bytes":   func(v *PolicyArtifact) { v.CanonicalBytes = append(v.CanonicalBytes, ' ') },
		"unknown field": func(v *PolicyArtifact) {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(v.CanonicalBytes, &object); err != nil {
				t.Fatal(err)
			}
			object["future"] = json.RawMessage(`true`)
			v.CanonicalBytes, _ = json.Marshal(object)
			digest := sha256.Sum256(v.CanonicalBytes)
			v.SHA256 = hex.EncodeToString(digest[:])
			v.Version = v.Schema + "@sha256:" + v.SHA256
			v.ID = economicid.DeterministicUUID("venue-reconciliation-policy-artifact", v.Version)
		},
		"time": func(v *PolicyArtifact) { v.CreatedAt = v.CreatedAt.Add(time.Nanosecond) },
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

func reversePolicySlice[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
