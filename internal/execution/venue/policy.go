// Package venue defines immutable reviewed provider policies and raw venue
// observations for the common execution lifecycle. It does not activate a
// provider, load credentials, or perform transport calls.
package venue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

const (
	// PolicySchemaV1 is the only venue adapter policy schema reviewed by
	// OVR-205. PolicyFromArtifact accepts only one of the two exact built-ins.
	PolicySchemaV1 = "venue-adapter-policy-v1"

	policyArtifactIDDomain = "venue-adapter-policy-artifact"
)

// Provider is the bounded provider identity carried by policy and evidence.
type Provider string

const (
	ProviderAlpaca Provider = "alpaca"
	ProviderKalshi Provider = "kalshi"
)

// MappingNamespace prevents equal-looking values from separate provider feeds
// from becoming indistinguishable policy facts.
type MappingNamespace string

const (
	MappingOrderStatus     MappingNamespace = "order_status"
	MappingTradeUpdate     MappingNamespace = "trade_update"
	MappingAccountActivity MappingNamespace = "account_activity"
	MappingFillRecord      MappingNamespace = "fill_record"
)

// MappedOutcome is the bounded provider-neutral interpretation vocabulary.
type MappedOutcome string

const (
	OutcomeAcknowledge          MappedOutcome = "acknowledge"
	OutcomeNoChange             MappedOutcome = "no_change"
	OutcomeFillNotice           MappedOutcome = "fill_notice"
	OutcomeFill                 MappedOutcome = "fill"
	OutcomeCancelled            MappedOutcome = "cancelled"
	OutcomeExpired              MappedOutcome = "expired"
	OutcomeRejected             MappedOutcome = "rejected"
	OutcomeCorrection           MappedOutcome = "correction"
	OutcomeBust                 MappedOutcome = "bust"
	OutcomeUnknownState         MappedOutcome = "unknown_state"
	OutcomeContradiction        MappedOutcome = "contradiction"
	OutcomeMalformedObservation MappedOutcome = "malformed_observation"
)

// Capability is one exact asset/order/time-in-force combination.
type Capability struct {
	AssetClass  instrument.AssetClass
	OrderType   lifecycle.OrderType
	TimeInForce lifecycle.TimeInForce
}

// StateMapping assigns one exact provider token in one feed namespace.
type StateMapping struct {
	Namespace MappingNamespace
	Value     string
	Outcome   MappedOutcome
}

// ContractMetadataPolicy pins the immutable venue-contract metadata shape.
// Path order is semantic; Values are canonical lexical order.
type ContractMetadataPolicy struct {
	Required    bool
	WholeObject bool
	Path        []string
	Values      []string
}

// Policy is one immutable reviewed adapter contract. All fields are private so
// callers cannot change routing semantics after content identity is computed.
type Policy struct {
	schema                     string
	provider                   Provider
	venue                      string
	apiRevision                string
	endpointFamilies           []string
	maxClientOrderIDLength     int
	retryLookup                retryLookupPolicy
	authoritativeFillNamespace string
	contractMetadata           ContractMetadataPolicy
	capabilities               []Capability
	mappings                   []StateMapping
	fillIdentityFields         []string
	feeTreatment               string
	canonicalBytes             json.RawMessage
	digest                     string
	version                    string
	artifactID                 uuid.UUID
}

// PolicyArtifact is the exact durable representation registered before a
// venue-policy order may persist. CreatedAt is local evidence, not content.
type PolicyArtifact struct {
	ID             uuid.UUID
	Schema         string
	Provider       Provider
	Venue          string
	Version        string
	SHA256         string
	CanonicalBytes json.RawMessage
	CreatedAt      time.Time
}

// SamePolicyArtifactPayload reports content and identity equality. CreatedAt
// is local persistence evidence and is deliberately excluded from replay
// identity.
func SamePolicyArtifactPayload(left, right *PolicyArtifact) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID && left.Schema == right.Schema && left.Provider == right.Provider &&
		left.Venue == right.Venue && left.Version == right.Version && left.SHA256 == right.SHA256 &&
		bytes.Equal(left.CanonicalBytes, right.CanonicalBytes)
}

type retryLookupPolicy struct {
	DedupeKey        string
	DuplicateResult  string
	UnresolvedMiss   string
	HistoricalLookup bool
}

type canonicalPolicy struct {
	Schema                     string                    `json:"schema"`
	Provider                   string                    `json:"provider"`
	Venue                      string                    `json:"venue"`
	APIRevision                string                    `json:"api_revision"`
	EndpointFamilies           []string                  `json:"endpoint_families"`
	MaxClientOrderIDLength     int                       `json:"max_client_order_id_length"`
	RetryLookup                canonicalRetryLookup      `json:"retry_lookup"`
	AuthoritativeFillNamespace string                    `json:"authoritative_fill_namespace"`
	ContractMetadata           canonicalContractMetadata `json:"contract_metadata"`
	Capabilities               []canonicalCapability     `json:"capabilities"`
	Mappings                   []canonicalMapping        `json:"mappings"`
	FillIdentityFields         []string                  `json:"fill_identity_fields"`
	FeeTreatment               string                    `json:"fee_treatment"`
}

type canonicalRetryLookup struct {
	DedupeKey        string `json:"dedupe_key"`
	DuplicateResult  string `json:"duplicate_result"`
	UnresolvedMiss   string `json:"unresolved_miss"`
	HistoricalLookup bool   `json:"historical_lookup"`
}

type canonicalContractMetadata struct {
	Required    bool     `json:"required"`
	WholeObject bool     `json:"whole_object"`
	Path        []string `json:"path"`
	Values      []string `json:"values"`
}

type canonicalCapability struct {
	AssetClass  string `json:"asset_class"`
	OrderType   string `json:"order_type"`
	TimeInForce string `json:"time_in_force"`
}

type canonicalMapping struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
	Outcome   string `json:"outcome"`
}

type policyDefinition struct {
	Provider                   Provider
	Venue                      string
	APIRevision                string
	EndpointFamilies           []string
	MaxClientOrderIDLength     int
	RetryLookup                retryLookupPolicy
	AuthoritativeFillNamespace string
	ContractMetadata           ContractMetadataPolicy
	Capabilities               []Capability
	Mappings                   []StateMapping
	FillIdentityFields         []string
	FeeTreatment               string
}

// ReviewedPolicy returns a newly constructed copy of the fixed reviewed policy
// for one provider. No caller-authored policy material is accepted here.
func ReviewedPolicy(provider Provider) (*Policy, error) {
	definition, err := reviewedPolicyDefinition(provider)
	if err != nil {
		return nil, err
	}
	return newReviewedPolicy(definition)
}

func newReviewedPolicy(definition policyDefinition) (*Policy, error) {
	if !validProvider(definition.Provider) || definition.Venue != string(definition.Provider) {
		return nil, fmt.Errorf("venue policy provider and venue are invalid")
	}
	if !normalizedRequired(definition.APIRevision, 128) || !normalizedRequired(definition.AuthoritativeFillNamespace, 256) ||
		!normalizedRequired(definition.FeeTreatment, 128) || definition.MaxClientOrderIDLength <= 0 || definition.MaxClientOrderIDLength > 256 {
		return nil, fmt.Errorf("venue policy scalar fields are invalid")
	}
	if !normalizedRequired(definition.RetryLookup.DedupeKey, 128) ||
		!normalizedRequired(definition.RetryLookup.DuplicateResult, 128) ||
		!normalizedRequired(definition.RetryLookup.UnresolvedMiss, 128) {
		return nil, fmt.Errorf("venue policy retry contract is invalid")
	}

	endpoints, err := sortedUniqueStrings(definition.EndpointFamilies, "endpoint family")
	if err != nil {
		return nil, err
	}
	identityFields, err := sortedUniqueStrings(definition.FillIdentityFields, "fill identity field")
	if err != nil {
		return nil, err
	}
	metadata, err := normalizeContractMetadata(definition.ContractMetadata)
	if err != nil {
		return nil, err
	}
	capabilities, err := normalizeCapabilities(definition.Capabilities)
	if err != nil {
		return nil, err
	}
	mappings, err := normalizeMappings(definition.Mappings)
	if err != nil {
		return nil, err
	}

	canonical := canonicalPolicy{
		Schema: PolicySchemaV1, Provider: string(definition.Provider), Venue: definition.Venue,
		APIRevision: definition.APIRevision, EndpointFamilies: append([]string(nil), endpoints...),
		MaxClientOrderIDLength: definition.MaxClientOrderIDLength,
		RetryLookup: canonicalRetryLookup{
			DedupeKey: definition.RetryLookup.DedupeKey, DuplicateResult: definition.RetryLookup.DuplicateResult,
			UnresolvedMiss: definition.RetryLookup.UnresolvedMiss, HistoricalLookup: definition.RetryLookup.HistoricalLookup,
		},
		AuthoritativeFillNamespace: definition.AuthoritativeFillNamespace,
		ContractMetadata: canonicalContractMetadata{
			Required: metadata.Required, WholeObject: metadata.WholeObject,
			Path: append([]string(nil), metadata.Path...), Values: append([]string(nil), metadata.Values...),
		},
		Capabilities:       make([]canonicalCapability, 0, len(capabilities)),
		Mappings:           make([]canonicalMapping, 0, len(mappings)),
		FillIdentityFields: append([]string(nil), identityFields...),
		FeeTreatment:       definition.FeeTreatment,
	}
	for _, capability := range capabilities {
		canonical.Capabilities = append(canonical.Capabilities, canonicalCapability{
			AssetClass: string(capability.AssetClass), OrderType: string(capability.OrderType),
			TimeInForce: string(capability.TimeInForce),
		})
	}
	for _, mapping := range mappings {
		canonical.Mappings = append(canonical.Mappings, canonicalMapping{
			Namespace: string(mapping.Namespace), Value: mapping.Value, Outcome: string(mapping.Outcome),
		})
	}
	canonicalBytes, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical venue policy: %w", err)
	}
	digestBytes := sha256.Sum256(canonicalBytes)
	digest := hex.EncodeToString(digestBytes[:])
	version := PolicySchemaV1 + "@sha256:" + digest
	return &Policy{
		schema: PolicySchemaV1, provider: definition.Provider, venue: definition.Venue,
		apiRevision: definition.APIRevision, endpointFamilies: endpoints,
		maxClientOrderIDLength: definition.MaxClientOrderIDLength, retryLookup: definition.RetryLookup,
		authoritativeFillNamespace: definition.AuthoritativeFillNamespace, contractMetadata: metadata,
		capabilities: capabilities, mappings: mappings, fillIdentityFields: identityFields,
		feeTreatment: definition.FeeTreatment, canonicalBytes: canonicalBytes, digest: digest,
		version: version, artifactID: economicid.DeterministicUUID(policyArtifactIDDomain, version),
	}, nil
}

// PolicyFromArtifact reconstructs only one of the exact reviewed policies. A
// self-consistent hash over caller-authored JSON is deliberately insufficient.
func PolicyFromArtifact(artifact PolicyArtifact) (*Policy, error) {
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("restore venue policy artifact: %w", err)
	}
	var identity struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(artifact.CanonicalBytes, &identity); err != nil {
		return nil, fmt.Errorf("restore venue policy artifact: decode provider: %w", err)
	}
	reviewed, err := ReviewedPolicy(Provider(identity.Provider))
	if err != nil {
		return nil, fmt.Errorf("restore venue policy artifact: %w", err)
	}
	if artifact.ID != reviewed.ArtifactID() || artifact.Schema != reviewed.Schema() ||
		artifact.Provider != reviewed.Provider() || artifact.Venue != reviewed.Venue() ||
		artifact.Version != reviewed.Version() || artifact.SHA256 != reviewed.Digest() ||
		!bytes.Equal(artifact.CanonicalBytes, reviewed.CanonicalBytes()) {
		return nil, fmt.Errorf("restore venue policy artifact: bytes or reviewed identity do not match")
	}
	return reviewed, nil
}

// Validate checks content identity and durable shape without granting semantic
// approval. PolicyFromArtifact performs the fixed-policy semantic comparison.
func (artifact PolicyArtifact) Validate() error {
	if artifact.Schema != PolicySchemaV1 || !validProvider(artifact.Provider) || artifact.Venue != string(artifact.Provider) {
		return fmt.Errorf("venue policy artifact identity is invalid")
	}
	if artifact.CreatedAt.IsZero() || artifact.CreatedAt.Location() != time.UTC ||
		!artifact.CreatedAt.Equal(artifact.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("venue policy artifact creation time must use UTC microsecond precision")
	}
	var object map[string]json.RawMessage
	if len(artifact.CanonicalBytes) == 0 || json.Unmarshal(artifact.CanonicalBytes, &object) != nil || object == nil {
		return fmt.Errorf("venue policy artifact canonical bytes must be a JSON object")
	}
	digestBytes := sha256.Sum256(artifact.CanonicalBytes)
	digest := hex.EncodeToString(digestBytes[:])
	if artifact.SHA256 != digest || artifact.Version != PolicySchemaV1+"@sha256:"+digest {
		return fmt.Errorf("venue policy artifact digest or version does not match bytes")
	}
	if artifact.ID != economicid.DeterministicUUID(policyArtifactIDDomain, artifact.Version) {
		return fmt.Errorf("venue policy artifact ID does not match version")
	}
	return nil
}

// NewArtifact captures exact reviewed bytes with normalized local evidence.
func (policy *Policy) NewArtifact(createdAt time.Time) (*PolicyArtifact, error) {
	if policy == nil || policy.artifactID == uuid.Nil {
		return nil, fmt.Errorf("venue policy is required")
	}
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	artifact := &PolicyArtifact{
		ID: policy.artifactID, Schema: policy.schema, Provider: policy.provider, Venue: policy.venue,
		Version: policy.version, SHA256: policy.digest, CanonicalBytes: policy.CanonicalBytes(), CreatedAt: createdAt,
	}
	if err := artifact.Validate(); err != nil {
		return nil, err
	}
	return artifact, nil
}

func (policy *Policy) Schema() string {
	if policy == nil {
		return ""
	}
	return policy.schema
}

func (policy *Policy) Provider() Provider {
	if policy == nil {
		return ""
	}
	return policy.provider
}

func (policy *Policy) Venue() string {
	if policy == nil {
		return ""
	}
	return policy.venue
}

func (policy *Policy) APIRevision() string {
	if policy == nil {
		return ""
	}
	return policy.apiRevision
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

func (policy *Policy) EndpointFamilies() []string {
	if policy == nil {
		return nil
	}
	return append([]string(nil), policy.endpointFamilies...)
}

func (policy *Policy) MaxClientOrderIDLength() int {
	if policy == nil {
		return 0
	}
	return policy.maxClientOrderIDLength
}

func (policy *Policy) AuthoritativeFillNamespace() string {
	if policy == nil {
		return ""
	}
	return policy.authoritativeFillNamespace
}

func (policy *Policy) ContractMetadata() ContractMetadataPolicy {
	if policy == nil {
		return ContractMetadataPolicy{}
	}
	return cloneContractMetadata(policy.contractMetadata)
}

func (policy *Policy) Capabilities() []Capability {
	if policy == nil {
		return nil
	}
	return append([]Capability(nil), policy.capabilities...)
}

func (policy *Policy) Mappings() []StateMapping {
	if policy == nil {
		return nil
	}
	return append([]StateMapping(nil), policy.mappings...)
}

func (policy *Policy) Supports(assetClass instrument.AssetClass, orderType lifecycle.OrderType, timeInForce lifecycle.TimeInForce) bool {
	if policy == nil {
		return false
	}
	want := capabilityKey(Capability{AssetClass: assetClass, OrderType: orderType, TimeInForce: timeInForce})
	index := sort.Search(len(policy.capabilities), func(index int) bool {
		return capabilityKey(policy.capabilities[index]) >= want
	})
	return index < len(policy.capabilities) && capabilityKey(policy.capabilities[index]) == want
}

func (policy *Policy) Mapping(namespace MappingNamespace, value string) (MappedOutcome, bool) {
	if policy == nil {
		return "", false
	}
	want := mappingKey(StateMapping{Namespace: namespace, Value: value})
	index := sort.Search(len(policy.mappings), func(index int) bool {
		return mappingKey(policy.mappings[index]) >= want
	})
	if index >= len(policy.mappings) || mappingKey(policy.mappings[index]) != want {
		return "", false
	}
	return policy.mappings[index].Outcome, true
}

func reviewedPolicyDefinition(provider Provider) (policyDefinition, error) {
	switch provider {
	case ProviderAlpaca:
		return alpacaPolicyDefinition(), nil
	case ProviderKalshi:
		return kalshiPolicyDefinition(), nil
	default:
		return policyDefinition{}, fmt.Errorf("venue policy provider %q is not reviewed", provider)
	}
}

func alpacaPolicyDefinition() policyDefinition {
	return policyDefinition{
		Provider: ProviderAlpaca, Venue: "alpaca", APIRevision: "trading-api-v2",
		EndpointFamilies: []string{
			"/v2/account/activities/FILL", "/v2/orders", "/v2/orders/{order_id}",
			"/v2/orders:by_client_order_id", "activity-sse", "trade_updates",
		},
		MaxClientOrderIDLength: 128,
		RetryLookup: retryLookupPolicy{
			DedupeKey: "client_order_id", DuplicateResult: "lookup_same_client_id",
			UnresolvedMiss: "retain_routed", HistoricalLookup: false,
		},
		AuthoritativeFillNamespace: "alpaca/account-activities/FILL",
		ContractMetadata:           ContractMetadataPolicy{},
		Capabilities:               alpacaCapabilities(),
		Mappings: []StateMapping{
			{MappingOrderStatus, "accepted", OutcomeAcknowledge},
			{MappingOrderStatus, "accepted_for_bidding", OutcomeAcknowledge},
			{MappingOrderStatus, "calculated", OutcomeAcknowledge},
			{MappingOrderStatus, "canceled", OutcomeCancelled},
			{MappingOrderStatus, "done_for_day", OutcomeAcknowledge},
			{MappingOrderStatus, "expired", OutcomeExpired},
			{MappingOrderStatus, "filled", OutcomeFillNotice},
			{MappingOrderStatus, "held", OutcomeAcknowledge},
			{MappingOrderStatus, "new", OutcomeAcknowledge},
			{MappingOrderStatus, "partially_filled", OutcomeFillNotice},
			{MappingOrderStatus, "pending_cancel", OutcomeAcknowledge},
			{MappingOrderStatus, "pending_new", OutcomeAcknowledge},
			{MappingOrderStatus, "pending_replace", OutcomeContradiction},
			{MappingOrderStatus, "rejected", OutcomeRejected},
			{MappingOrderStatus, "replaced", OutcomeContradiction},
			{MappingOrderStatus, "stopped", OutcomeAcknowledge},
			{MappingOrderStatus, "suspended", OutcomeAcknowledge},
			{MappingTradeUpdate, "calculated", OutcomeAcknowledge},
			{MappingTradeUpdate, "canceled", OutcomeCancelled},
			{MappingTradeUpdate, "done_for_day", OutcomeAcknowledge},
			{MappingTradeUpdate, "expired", OutcomeExpired},
			{MappingTradeUpdate, "fill", OutcomeFillNotice},
			{MappingTradeUpdate, "new", OutcomeAcknowledge},
			{MappingTradeUpdate, "order_cancel_rejected", OutcomeNoChange},
			{MappingTradeUpdate, "order_replace_rejected", OutcomeNoChange},
			{MappingTradeUpdate, "partial_fill", OutcomeFillNotice},
			{MappingTradeUpdate, "pending_cancel", OutcomeAcknowledge},
			{MappingTradeUpdate, "pending_new", OutcomeAcknowledge},
			{MappingTradeUpdate, "pending_replace", OutcomeContradiction},
			{MappingTradeUpdate, "rejected", OutcomeRejected},
			{MappingTradeUpdate, "replaced", OutcomeContradiction},
			{MappingTradeUpdate, "stopped", OutcomeAcknowledge},
			{MappingTradeUpdate, "suspended", OutcomeAcknowledge},
			{MappingAccountActivity, "FILL", OutcomeFill},
			{MappingAccountActivity, "trade_bust", OutcomeBust},
			{MappingAccountActivity, "trade_correct", OutcomeCorrection},
		},
		FillIdentityFields: []string{"id", "order_id", "price", "qty", "side", "symbol", "transaction_time"},
		FeeTreatment:       "present_exact_per_fill_commission_only",
	}
}

func kalshiPolicyDefinition() policyDefinition {
	return policyDefinition{
		Provider: ProviderKalshi, Venue: "kalshi", APIRevision: "trade-api-v2",
		EndpointFamilies: []string{
			"/historical/fills", "/historical/orders",
			"/portfolio/events/orders", "/portfolio/events/orders/{order_id}",
			"/portfolio/fills", "/portfolio/orders",
		},
		MaxClientOrderIDLength: 64,
		RetryLookup: retryLookupPolicy{
			DedupeKey: "client_order_id", DuplicateResult: "conflict_then_current_historical_lookup",
			UnresolvedMiss: "retain_routed", HistoricalLookup: true,
		},
		AuthoritativeFillNamespace: "kalshi/portfolio/fills",
		ContractMetadata: ContractMetadataPolicy{
			Required: true, WholeObject: true, Path: []string{"kalshi_v2", "outcome"}, Values: []string{"no", "yes"},
		},
		Capabilities: []Capability{
			{instrument.AssetClassPredictionContract, lifecycle.OrderLimit, lifecycle.TimeInForceGTC},
			{instrument.AssetClassPredictionContract, lifecycle.OrderLimit, lifecycle.TimeInForceIOC},
			{instrument.AssetClassPredictionContract, lifecycle.OrderLimit, lifecycle.TimeInForceFOK},
		},
		Mappings: []StateMapping{
			{MappingOrderStatus, "resting", OutcomeAcknowledge},
			{MappingOrderStatus, "canceled", OutcomeCancelled},
			{MappingOrderStatus, "executed", OutcomeFillNotice},
			{MappingOrderStatus, "v2_cancel", OutcomeNoChange},
			{MappingOrderStatus, "v2_submit", OutcomeFillNotice},
			{MappingFillRecord, "fill", OutcomeFill},
		},
		FillIdentityFields: []string{
			"count_fp", "fee_cost", "fill_id", "no_price_dollars", "order_id", "side", "ticker", "trade_id", "yes_price_dollars",
		},
		FeeTreatment: "optional_exact_fee_cost_usd_attached",
	}
}

func alpacaCapabilities() []Capability {
	var capabilities []Capability
	for _, assetClass := range []instrument.AssetClass{instrument.AssetClassEquity, instrument.AssetClassETF} {
		for _, orderType := range []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit, lifecycle.OrderStop, lifecycle.OrderStopLimit} {
			for _, timeInForce := range []lifecycle.TimeInForce{lifecycle.TimeInForceDay, lifecycle.TimeInForceGTC} {
				capabilities = append(capabilities, Capability{assetClass, orderType, timeInForce})
			}
		}
		for _, orderType := range []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit} {
			for _, timeInForce := range []lifecycle.TimeInForce{lifecycle.TimeInForceIOC, lifecycle.TimeInForceFOK} {
				capabilities = append(capabilities, Capability{assetClass, orderType, timeInForce})
			}
		}
	}
	for _, orderType := range []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit, lifecycle.OrderStopLimit} {
		capabilities = append(capabilities, Capability{instrument.AssetClassCryptoSpot, orderType, lifecycle.TimeInForceGTC})
	}
	for _, orderType := range []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit} {
		capabilities = append(capabilities, Capability{instrument.AssetClassCryptoSpot, orderType, lifecycle.TimeInForceIOC})
	}
	return capabilities
}

func normalizeCapabilities(values []Capability) ([]Capability, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("venue policy requires capabilities")
	}
	normalized := append([]Capability(nil), values...)
	sort.Slice(normalized, func(i, j int) bool { return capabilityKey(normalized[i]) < capabilityKey(normalized[j]) })
	for index, value := range normalized {
		if value.AssetClass == "" || value.OrderType == "" || value.TimeInForce == "" {
			return nil, fmt.Errorf("venue policy capability is incomplete")
		}
		if index > 0 && capabilityKey(normalized[index-1]) == capabilityKey(value) {
			return nil, fmt.Errorf("venue policy capability %q is duplicated", capabilityKey(value))
		}
	}
	return normalized, nil
}

func normalizeMappings(values []StateMapping) ([]StateMapping, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("venue policy requires state mappings")
	}
	normalized := append([]StateMapping(nil), values...)
	sort.Slice(normalized, func(i, j int) bool { return mappingKey(normalized[i]) < mappingKey(normalized[j]) })
	for index, value := range normalized {
		if !validMappingNamespace(value.Namespace) || !normalizedRequired(value.Value, 128) || !validMappedOutcome(value.Outcome) {
			return nil, fmt.Errorf("venue policy mapping is invalid")
		}
		if index > 0 && mappingKey(normalized[index-1]) == mappingKey(value) {
			return nil, fmt.Errorf("venue policy mapping %q is duplicated", mappingKey(value))
		}
	}
	return normalized, nil
}

func normalizeContractMetadata(value ContractMetadataPolicy) (ContractMetadataPolicy, error) {
	result := cloneContractMetadata(value)
	if !result.Required {
		if result.WholeObject || len(result.Path) != 0 || len(result.Values) != 0 {
			return ContractMetadataPolicy{}, fmt.Errorf("optional contract metadata policy must be empty")
		}
		return result, nil
	}
	if !result.WholeObject || len(result.Path) == 0 {
		return ContractMetadataPolicy{}, fmt.Errorf("required contract metadata must pin the whole object and path")
	}
	for _, component := range result.Path {
		if !normalizedRequired(component, 64) {
			return ContractMetadataPolicy{}, fmt.Errorf("contract metadata path is invalid")
		}
	}
	values, err := sortedUniqueStrings(result.Values, "contract metadata value")
	if err != nil {
		return ContractMetadataPolicy{}, err
	}
	result.Values = values
	return result, nil
}

func sortedUniqueStrings(values []string, label string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("venue policy requires at least one %s", label)
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	for index, value := range result {
		if !normalizedRequired(value, 256) {
			return nil, fmt.Errorf("venue policy %s is invalid", label)
		}
		if index > 0 && result[index-1] == value {
			return nil, fmt.Errorf("venue policy %s %q is duplicated", label, value)
		}
	}
	return result, nil
}

func capabilityKey(value Capability) string {
	return strings.Join([]string{string(value.AssetClass), string(value.OrderType), string(value.TimeInForce)}, "\x1f")
}

func mappingKey(value StateMapping) string {
	return strings.Join([]string{string(value.Namespace), value.Value}, "\x1f")
}

func cloneContractMetadata(value ContractMetadataPolicy) ContractMetadataPolicy {
	return ContractMetadataPolicy{
		Required: value.Required, WholeObject: value.WholeObject,
		Path: append([]string(nil), value.Path...), Values: append([]string(nil), value.Values...),
	}
}

func validProvider(value Provider) bool {
	return value == ProviderAlpaca || value == ProviderKalshi
}

func validMappingNamespace(value MappingNamespace) bool {
	switch value {
	case MappingOrderStatus, MappingTradeUpdate, MappingAccountActivity, MappingFillRecord:
		return true
	default:
		return false
	}
}

func validMappedOutcome(value MappedOutcome) bool {
	switch value {
	case OutcomeAcknowledge, OutcomeNoChange, OutcomeFillNotice, OutcomeFill,
		OutcomeCancelled, OutcomeExpired, OutcomeRejected, OutcomeCorrection,
		OutcomeBust, OutcomeUnknownState, OutcomeContradiction, OutcomeMalformedObservation:
		return true
	default:
		return false
	}
}

func normalizedRequired(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= maximum
}
