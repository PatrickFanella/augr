// Package capital owns immutable capital-tier and margin-profile simulation
// policy. It does not activate runtime risk admission or mutate accounts.
package capital

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	PolicySchemaV1       = "capital-margin-policy-v1"
	policyArtifactDomain = "capital-margin-policy-artifact"
)

// Profile is one exact simulation approximation. Unsupported short exposure
// is represented explicitly rather than by a magic ratio.
type Profile struct {
	Name             domain.MarginProfile
	InitialLong      decimal.Decimal
	InitialShort     decimal.Decimal
	MaintenanceLong  decimal.Decimal
	MaintenanceShort decimal.Decimal
	MaximumGross     decimal.Decimal
	CashReserve      decimal.Decimal
	AllowShort       bool
	Unlimited        bool
}

// PolicyInput contains every economic field used by the fixed v1 policy.
// NewPolicy accepts reordered tiers/profiles but never fills missing values.
type PolicyInput struct {
	Schema   string
	Currency string
	Scale    int32
	Tiers    []decimal.Decimal
	Profiles []Profile
}

// Policy is an immutable content-addressed capital and margin contract.
type Policy struct {
	schema         string
	currency       string
	scale          int32
	tiers          []decimal.Decimal
	profiles       []Profile
	canonicalBytes json.RawMessage
	digest         string
	version        string
	artifactID     uuid.UUID
}

// PolicyArtifact is the exact durable form registered before an account can
// bind to this policy.
type PolicyArtifact struct {
	ID             uuid.UUID
	Schema         string
	Version        string
	SHA256         string
	CanonicalBytes json.RawMessage
	CreatedAt      time.Time
}

type canonicalPolicy struct {
	Schema   string             `json:"schema"`
	Currency string             `json:"currency"`
	Scale    int32              `json:"scale"`
	Tiers    []string           `json:"tiers"`
	Profiles []canonicalProfile `json:"profiles"`
}

type canonicalProfile struct {
	Name             string `json:"name"`
	InitialLong      string `json:"initial_long"`
	InitialShort     string `json:"initial_short"`
	MaintenanceLong  string `json:"maintenance_long"`
	MaintenanceShort string `json:"maintenance_short"`
	MaximumGross     string `json:"maximum_gross"`
	CashReserve      string `json:"cash_reserve"`
	AllowShort       bool   `json:"allow_short"`
	Unlimited        bool   `json:"unlimited"`
}

// ReviewedPolicyV1Input returns explicit reviewed source values. Callers may
// inspect or reorder the returned slices without mutating package storage.
func ReviewedPolicyV1Input() PolicyInput {
	return PolicyInput{
		Schema:   PolicySchemaV1,
		Currency: "USD",
		Scale:    12,
		Tiers:    reviewedCapitalTiers(),
		Profiles: reviewedProfiles(),
	}
}

func reviewedCapitalTiers() []decimal.Decimal {
	return []decimal.Decimal{
		decimal.NewFromInt(500),
		decimal.NewFromInt(5_000),
		decimal.NewFromInt(25_000),
		decimal.NewFromInt(100_000),
		decimal.NewFromInt(1_000_000),
		decimal.NewFromInt(5_000_000),
	}
}

func reviewedProfiles() []Profile {
	return []Profile{
		{
			Name:        domain.MarginProfileCash,
			InitialLong: decimal.NewFromInt(1), InitialShort: decimal.Zero,
			MaintenanceLong: decimal.NewFromInt(1), MaintenanceShort: decimal.Zero,
			MaximumGross: decimal.NewFromInt(1), CashReserve: decimal.Zero,
			AllowShort: false, Unlimited: false,
		},
		{
			Name:        domain.MarginProfilePortfolio,
			InitialLong: decimal.RequireFromString("0.15"), InitialShort: decimal.RequireFromString("0.3"),
			MaintenanceLong: decimal.RequireFromString("0.15"), MaintenanceShort: decimal.RequireFromString("0.3"),
			MaximumGross: decimal.NewFromInt(6), CashReserve: decimal.Zero,
			AllowShort: true, Unlimited: false,
		},
		{
			Name:        domain.MarginProfileRegT,
			InitialLong: decimal.RequireFromString("0.5"), InitialShort: decimal.RequireFromString("1.5"),
			MaintenanceLong: decimal.RequireFromString("0.25"), MaintenanceShort: decimal.RequireFromString("0.3"),
			MaximumGross: decimal.NewFromInt(2), CashReserve: decimal.Zero,
			AllowShort: true, Unlimited: false,
		},
		{
			Name:        domain.MarginProfileStressUnlimited,
			InitialLong: decimal.Zero, InitialShort: decimal.Zero,
			MaintenanceLong: decimal.Zero, MaintenanceShort: decimal.Zero,
			MaximumGross: decimal.Zero, CashReserve: decimal.Zero,
			AllowShort: true, Unlimited: true,
		},
	}
}

// NewPolicy validates the complete reviewed v1 policy and canonicalizes only
// order-insensitive slices.
func NewPolicy(input PolicyInput) (*Policy, error) {
	if input.Schema != PolicySchemaV1 {
		return nil, fmt.Errorf("capital policy schema must be %q", PolicySchemaV1)
	}
	if input.Currency != "USD" {
		return nil, fmt.Errorf("capital policy currency must be USD")
	}
	if input.Scale != 12 {
		return nil, fmt.Errorf("capital policy scale must be 12")
	}
	if len(input.Tiers) != 6 {
		return nil, fmt.Errorf("capital policy requires exactly six tiers")
	}
	tiers := append([]decimal.Decimal(nil), input.Tiers...)
	sort.Slice(tiers, func(left, right int) bool { return tiers[left].LessThan(tiers[right]) })
	wantTiers := reviewedCapitalTiers()
	for index := range tiers {
		if !validPolicyDecimal(tiers[index]) || !tiers[index].Equal(wantTiers[index]) {
			return nil, fmt.Errorf("capital policy tier %d is not the reviewed value", index)
		}
	}

	if len(input.Profiles) != 4 {
		return nil, fmt.Errorf("capital policy requires exactly four profiles")
	}
	profiles := append([]Profile(nil), input.Profiles...)
	sort.Slice(profiles, func(left, right int) bool { return profiles[left].Name < profiles[right].Name })
	wantProfiles := reviewedProfiles()
	for index := range profiles {
		if err := validateProfile(profiles[index]); err != nil {
			return nil, fmt.Errorf("capital policy profile %d: %w", index, err)
		}
		if !sameProfile(profiles[index], wantProfiles[index]) {
			return nil, fmt.Errorf("capital policy profile %q does not match reviewed v1 economics", profiles[index].Name)
		}
	}

	canonical := canonicalPolicy{
		Schema: input.Schema, Currency: input.Currency, Scale: input.Scale,
		Tiers: make([]string, 0, len(tiers)), Profiles: make([]canonicalProfile, 0, len(profiles)),
	}
	for _, tier := range tiers {
		canonical.Tiers = append(canonical.Tiers, tier.String())
	}
	for _, profile := range profiles {
		canonical.Profiles = append(canonical.Profiles, canonicalProfile{
			Name: string(profile.Name), InitialLong: profile.InitialLong.String(),
			InitialShort: profile.InitialShort.String(), MaintenanceLong: profile.MaintenanceLong.String(),
			MaintenanceShort: profile.MaintenanceShort.String(), MaximumGross: profile.MaximumGross.String(),
			CashReserve: profile.CashReserve.String(), AllowShort: profile.AllowShort, Unlimited: profile.Unlimited,
		})
	}
	canonicalBytes, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal capital policy: %w", err)
	}
	digestBytes := sha256.Sum256(canonicalBytes)
	digest := hex.EncodeToString(digestBytes[:])
	version := input.Schema + "@sha256:" + digest
	return &Policy{
		schema: input.Schema, currency: input.Currency, scale: input.Scale,
		tiers: append([]decimal.Decimal(nil), tiers...), profiles: append([]Profile(nil), profiles...),
		canonicalBytes: canonicalBytes, digest: digest, version: version,
		artifactID: economicid.DeterministicUUID(policyArtifactDomain, version),
	}, nil
}

// PolicyFromArtifact restores only exact canonical reviewed bytes.
func PolicyFromArtifact(artifact PolicyArtifact) (*Policy, error) {
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("restore capital policy artifact: %w", err)
	}
	var canonical canonicalPolicy
	decoder := json.NewDecoder(bytes.NewReader(artifact.CanonicalBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, fmt.Errorf("restore capital policy artifact: decode: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("restore capital policy artifact: %w", err)
	}
	input, err := policyInputFromCanonical(canonical)
	if err != nil {
		return nil, fmt.Errorf("restore capital policy artifact: %w", err)
	}
	restored, err := NewPolicy(input)
	if err != nil {
		return nil, fmt.Errorf("restore capital policy artifact: %w", err)
	}
	if restored.ArtifactID() != artifact.ID || restored.Schema() != artifact.Schema ||
		restored.Version() != artifact.Version || restored.Digest() != artifact.SHA256 ||
		!bytes.Equal(restored.CanonicalBytes(), artifact.CanonicalBytes) {
		return nil, fmt.Errorf("restore capital policy artifact: canonical bytes or identity do not match")
	}
	return restored, nil
}

func policyInputFromCanonical(value canonicalPolicy) (PolicyInput, error) {
	input := PolicyInput{Schema: value.Schema, Currency: value.Currency, Scale: value.Scale}
	for _, encoded := range value.Tiers {
		parsed, err := decimal.NewFromString(encoded)
		if err != nil || parsed.String() != encoded {
			return PolicyInput{}, fmt.Errorf("capital policy tier %q is not a canonical decimal", encoded)
		}
		input.Tiers = append(input.Tiers, parsed)
	}
	for _, encoded := range value.Profiles {
		values := []*decimal.Decimal{
			new(decimal.Decimal), new(decimal.Decimal), new(decimal.Decimal),
			new(decimal.Decimal), new(decimal.Decimal), new(decimal.Decimal),
		}
		texts := []string{
			encoded.InitialLong, encoded.InitialShort, encoded.MaintenanceLong,
			encoded.MaintenanceShort, encoded.MaximumGross, encoded.CashReserve,
		}
		for index := range values {
			parsed, err := decimal.NewFromString(texts[index])
			if err != nil || parsed.String() != texts[index] {
				return PolicyInput{}, fmt.Errorf("capital policy profile %q has noncanonical decimal %q", encoded.Name, texts[index])
			}
			*values[index] = parsed
		}
		input.Profiles = append(input.Profiles, Profile{
			Name: domain.MarginProfile(encoded.Name), InitialLong: *values[0], InitialShort: *values[1],
			MaintenanceLong: *values[2], MaintenanceShort: *values[3], MaximumGross: *values[4],
			CashReserve: *values[5], AllowShort: encoded.AllowShort, Unlimited: encoded.Unlimited,
		})
	}
	return input, nil
}

// NewArtifact captures the policy's exact durable identity.
func (policy *Policy) NewArtifact(createdAt time.Time) (*PolicyArtifact, error) {
	if policy == nil || policy.artifactID == uuid.Nil {
		return nil, fmt.Errorf("capital policy is required")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	artifact := &PolicyArtifact{
		ID: policy.artifactID, Schema: policy.schema, Version: policy.version,
		SHA256: policy.digest, CanonicalBytes: policy.CanonicalBytes(), CreatedAt: createdAt,
	}
	if err := artifact.Validate(); err != nil {
		return nil, err
	}
	return artifact, nil
}

// Validate checks envelope facts without trusting the decoded policy.
func (artifact PolicyArtifact) Validate() error {
	if artifact.ID == uuid.Nil || artifact.Schema != PolicySchemaV1 {
		return fmt.Errorf("capital policy artifact identity or schema is invalid")
	}
	if artifact.Version == "" || artifact.Version != strings.TrimSpace(artifact.Version) || len(artifact.Version) > 256 {
		return fmt.Errorf("capital policy artifact version is invalid")
	}
	if len(artifact.SHA256) != 64 || strings.ToLower(artifact.SHA256) != artifact.SHA256 {
		return fmt.Errorf("capital policy artifact digest is invalid")
	}
	if len(artifact.CanonicalBytes) == 0 || artifact.CreatedAt.IsZero() ||
		artifact.CreatedAt.Location() != time.UTC || !artifact.CreatedAt.Equal(artifact.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("capital policy artifact bytes or creation time is invalid")
	}
	digestBytes := sha256.Sum256(artifact.CanonicalBytes)
	digest := hex.EncodeToString(digestBytes[:])
	if digest != artifact.SHA256 || artifact.Version != artifact.Schema+"@sha256:"+digest ||
		artifact.ID != economicid.DeterministicUUID(policyArtifactDomain, artifact.Version) {
		return fmt.Errorf("capital policy artifact digest, version, or ID does not match bytes")
	}
	return nil
}

func (policy *Policy) Schema() string {
	if policy == nil {
		return ""
	}
	return policy.schema
}

func (policy *Policy) Currency() string {
	if policy == nil {
		return ""
	}
	return policy.currency
}

func (policy *Policy) Scale() int32 {
	if policy == nil {
		return 0
	}
	return policy.scale
}

func (policy *Policy) Version() string {
	if policy == nil {
		return ""
	}
	return policy.version
}

func (policy *Policy) Digest() string {
	if policy == nil {
		return ""
	}
	return policy.digest
}

func (policy *Policy) ArtifactID() uuid.UUID {
	if policy == nil {
		return uuid.Nil
	}
	return policy.artifactID
}

func (policy *Policy) CanonicalBytes() json.RawMessage {
	if policy == nil {
		return nil
	}
	return append(json.RawMessage(nil), policy.canonicalBytes...)
}

func (policy *Policy) Tiers() []decimal.Decimal {
	if policy == nil {
		return nil
	}
	return append([]decimal.Decimal(nil), policy.tiers...)
}

func (policy *Policy) Profile(name domain.MarginProfile) (Profile, bool) {
	if policy == nil {
		return Profile{}, false
	}
	for _, profile := range policy.profiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return Profile{}, false
}

func validateProfile(profile Profile) error {
	if !profile.Name.IsValid() {
		return fmt.Errorf("profile name %q is invalid", profile.Name)
	}
	values := []decimal.Decimal{
		profile.InitialLong, profile.InitialShort, profile.MaintenanceLong,
		profile.MaintenanceShort, profile.MaximumGross, profile.CashReserve,
	}
	for _, value := range values {
		if !validPolicyDecimal(value) || value.IsNegative() {
			return fmt.Errorf("profile %q has an invalid exact decimal", profile.Name)
		}
	}
	return nil
}

func validPolicyDecimal(value decimal.Decimal) bool {
	if !value.Equal(value.Round(12)) {
		return false
	}
	integer := value.Abs().Truncate(0).String()
	return len(strings.TrimPrefix(integer, "-")) <= 26
}

func sameProfile(left, right Profile) bool {
	return left.Name == right.Name && left.InitialLong.Equal(right.InitialLong) &&
		left.InitialShort.Equal(right.InitialShort) && left.MaintenanceLong.Equal(right.MaintenanceLong) &&
		left.MaintenanceShort.Equal(right.MaintenanceShort) && left.MaximumGross.Equal(right.MaximumGross) &&
		left.CashReserve.Equal(right.CashReserve) && left.AllowShort == right.AllowShort && left.Unlimited == right.Unlimited
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("capital policy contains trailing JSON")
		}
		return fmt.Errorf("capital policy trailing JSON: %w", err)
	}
	return nil
}
