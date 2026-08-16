// Package simulation owns deterministic simulated venue execution and its
// immutable, content-addressed policy contract.
package simulation

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

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

const (
	// PolicySchemaV1 is the canonical JSON schema implemented by this package.
	PolicySchemaV1 = "simulation-policy-v1"

	policyArtifactIDDomain = "simulation-policy-artifact"
	policyTimestampLayout  = "2006-01-02T15:04:05.000000Z"
)

// CalendarKind distinguishes finite exchange-session evidence from venues
// that are continuously executable under their policy.
type CalendarKind string

const (
	CalendarExplicitSessions CalendarKind = "explicit_sessions"
	CalendarContinuous24x7   CalendarKind = "continuous_24x7"
)

// SessionWindow is one immutable half-open executable venue interval.
type SessionWindow struct {
	Label   string
	OpenAt  time.Time
	CloseAt time.Time
}

// CalendarPolicy contains either explicit UTC session windows or one 24/7
// declaration. Explicit windows make holidays and half-days data rather than
// hidden calendar behavior.
type CalendarPolicy struct {
	Kind     CalendarKind
	Sessions []SessionWindow
}

// FeePolicy defines exact fill-attached costs in account/contract currency.
// Scale is the one rounding boundary after all components are summed.
type FeePolicy struct {
	PerOrder    decimal.Decimal
	PerUnit     decimal.Decimal
	NotionalBPS decimal.Decimal
	Scale       int32
}

// AssetPolicy declares the complete canonical simulation capability for one
// asset class.
type AssetPolicy struct {
	AssetClass            instrument.AssetClass
	OrderTypes            []lifecycle.OrderType
	TimeInForce           []lifecycle.TimeInForce
	QuoteRequirements     marketdata.QuoteRequirements
	MaxDepthParticipation decimal.Decimal
	FixedLatency          time.Duration
	Calendar              CalendarPolicy
	Fees                  FeePolicy
}

// PolicyInput is caller-authored policy material. NewPolicy normalizes its
// order-insensitive fields into one fixed-schema canonical artifact.
type PolicyInput struct {
	Schema string
	Assets []AssetPolicy
}

// Policy is an immutable validated simulation contract. Its private fields
// prevent callers from changing economics after identity is calculated.
type Policy struct {
	schema         string
	assets         []AssetPolicy
	canonicalBytes json.RawMessage
	digest         string
	version        string
	artifactID     uuid.UUID
}

// PolicyArtifact is the exact durable representation registered before a
// simulation order can be persisted. CreatedAt is local persistence evidence
// and is excluded from semantic replay equality.
type PolicyArtifact struct {
	ID             uuid.UUID
	Schema         string
	Version        string
	SHA256         string
	CanonicalBytes json.RawMessage
	CreatedAt      time.Time
}

type canonicalPolicy struct {
	Schema string                 `json:"schema"`
	Assets []canonicalAssetPolicy `json:"assets"`
}

type canonicalAssetPolicy struct {
	AssetClass              string                     `json:"asset_class"`
	OrderTypes              []string                   `json:"order_types"`
	TimeInForce             []string                   `json:"time_in_force"`
	QuoteRequirements       canonicalQuoteRequirements `json:"quote_requirements"`
	MaxDepthParticipation   string                     `json:"max_depth_participation"`
	FixedLatencyNanoseconds int64                      `json:"fixed_latency_nanoseconds"`
	Calendar                canonicalCalendar          `json:"calendar"`
	Fees                    canonicalFees              `json:"fees"`
}

type canonicalQuoteRequirements struct {
	RequireSource          bool     `json:"require_source"`
	RequireVenueContract   bool     `json:"require_venue_contract"`
	RequireBid             bool     `json:"require_bid"`
	RequireAsk             bool     `json:"require_ask"`
	RequireBidDepth        bool     `json:"require_bid_depth"`
	RequireAskDepth        bool     `json:"require_ask_depth"`
	RequireMarketStatus    bool     `json:"require_market_status"`
	RequireSessionStatus   bool     `json:"require_session_status"`
	AllowedMarketStatuses  []string `json:"allowed_market_statuses"`
	AllowedSessionStatuses []string `json:"allowed_session_statuses"`
	MaxAgeNanoseconds      int64    `json:"max_age_nanoseconds"`
}

type canonicalCalendar struct {
	Kind     string             `json:"kind"`
	Sessions []canonicalSession `json:"sessions"`
}

type canonicalSession struct {
	Label   string `json:"label"`
	OpenAt  string `json:"open_at"`
	CloseAt string `json:"close_at"`
}

type canonicalFees struct {
	PerOrder    string `json:"per_order"`
	PerUnit     string `json:"per_unit"`
	NotionalBPS string `json:"notional_bps"`
	Scale       int32  `json:"scale"`
}

// NewPolicy validates and canonicalizes an explicit simulation policy.
func NewPolicy(input PolicyInput) (*Policy, error) {
	schema := strings.TrimSpace(input.Schema)
	if schema != PolicySchemaV1 {
		return nil, fmt.Errorf("simulation policy schema must be %q", PolicySchemaV1)
	}
	if len(input.Assets) == 0 {
		return nil, fmt.Errorf("simulation policy requires at least one asset policy")
	}

	assets := make([]AssetPolicy, 0, len(input.Assets))
	seenAssets := make(map[instrument.AssetClass]struct{}, len(input.Assets))
	for _, candidate := range input.Assets {
		if _, duplicate := seenAssets[candidate.AssetClass]; duplicate {
			return nil, fmt.Errorf("simulation policy asset class %q is duplicated", candidate.AssetClass)
		}
		seenAssets[candidate.AssetClass] = struct{}{}
		normalized, err := normalizeAssetPolicy(candidate)
		if err != nil {
			return nil, fmt.Errorf("simulation policy asset %q: %w", candidate.AssetClass, err)
		}
		assets = append(assets, normalized)
	}
	sort.Slice(assets, func(left, right int) bool {
		return assets[left].AssetClass < assets[right].AssetClass
	})

	canonical := canonicalPolicy{Schema: schema, Assets: make([]canonicalAssetPolicy, 0, len(assets))}
	for _, asset := range assets {
		canonical.Assets = append(canonical.Assets, canonicalAsset(asset))
	}
	canonicalBytes, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical simulation policy: %w", err)
	}
	digestBytes := sha256.Sum256(canonicalBytes)
	digest := hex.EncodeToString(digestBytes[:])
	version := schema + "@sha256:" + digest
	return &Policy{
		schema:         schema,
		assets:         cloneAssetPolicies(assets),
		canonicalBytes: append(json.RawMessage(nil), canonicalBytes...),
		digest:         digest,
		version:        version,
		artifactID:     economicid.DeterministicUUID(policyArtifactIDDomain, version),
	}, nil
}

// PolicyFromArtifact reconstructs a policy only when the durable identity,
// bytes, and their canonical interpretation all agree.
func PolicyFromArtifact(artifact PolicyArtifact) (*Policy, error) {
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("restore simulation policy artifact: %w", err)
	}
	canonical, err := decodeCanonicalPolicy(artifact.CanonicalBytes)
	if err != nil {
		return nil, fmt.Errorf("restore simulation policy artifact: %w", err)
	}
	input, err := policyInputFromCanonical(canonical)
	if err != nil {
		return nil, fmt.Errorf("restore simulation policy artifact: %w", err)
	}
	restored, err := NewPolicy(input)
	if err != nil {
		return nil, fmt.Errorf("restore simulation policy artifact: %w", err)
	}
	if restored.ArtifactID() != artifact.ID || restored.Schema() != artifact.Schema ||
		restored.Version() != artifact.Version || restored.Digest() != artifact.SHA256 ||
		!bytes.Equal(restored.CanonicalBytes(), artifact.CanonicalBytes) {
		return nil, fmt.Errorf("restore simulation policy artifact: canonical bytes or identity do not match")
	}
	return restored, nil
}

// Schema returns the canonical policy schema.
func (policy *Policy) Schema() string {
	if policy == nil {
		return ""
	}
	return policy.schema
}

// Version returns the schema-qualified content digest recorded on orders.
func (policy *Policy) Version() string {
	if policy == nil {
		return ""
	}
	return policy.version
}

// Digest returns the lowercase SHA-256 of CanonicalBytes.
func (policy *Policy) Digest() string {
	if policy == nil {
		return ""
	}
	return policy.digest
}

// ArtifactID returns the deterministic durable artifact UUID.
func (policy *Policy) ArtifactID() uuid.UUID {
	if policy == nil {
		return uuid.Nil
	}
	return policy.artifactID
}

// CanonicalBytes returns a clone of the exact content-addressed policy bytes.
func (policy *Policy) CanonicalBytes() json.RawMessage {
	if policy == nil {
		return nil
	}
	return append(json.RawMessage(nil), policy.canonicalBytes...)
}

// NewArtifact captures the validated policy bytes with normalized local
// creation evidence.
func (policy *Policy) NewArtifact(createdAt time.Time) (*PolicyArtifact, error) {
	if policy == nil || policy.artifactID == uuid.Nil {
		return nil, fmt.Errorf("simulation policy is required")
	}
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	artifact := &PolicyArtifact{
		ID:             policy.artifactID,
		Schema:         policy.schema,
		Version:        policy.version,
		SHA256:         policy.digest,
		CanonicalBytes: policy.CanonicalBytes(),
		CreatedAt:      createdAt,
	}
	if err := artifact.Validate(); err != nil {
		return nil, err
	}
	return artifact, nil
}

// AssetPolicy returns a deep clone of one configured asset policy.
func (policy *Policy) AssetPolicy(assetClass instrument.AssetClass) (AssetPolicy, bool) {
	if policy == nil {
		return AssetPolicy{}, false
	}
	for _, candidate := range policy.assets {
		if candidate.AssetClass == assetClass {
			return cloneAssetPolicy(candidate), true
		}
	}
	return AssetPolicy{}, false
}

// RouteSession resolves the explicit half-open session containing routedAt.
// A continuous 24/7 policy returns nil because it has no close boundary.
func (policy *Policy) RouteSession(assetClass instrument.AssetClass, routedAt time.Time) (*SessionWindow, error) {
	asset, ok := policy.AssetPolicy(assetClass)
	if !ok {
		return nil, fmt.Errorf("simulation policy has no asset policy for %q", assetClass)
	}
	if err := validatePolicyTime(routedAt, "route"); err != nil {
		return nil, err
	}
	if asset.Calendar.Kind == CalendarContinuous24x7 {
		return nil, nil
	}
	for _, session := range asset.Calendar.Sessions {
		if !routedAt.Before(session.OpenAt) && routedAt.Before(session.CloseAt) {
			cloned := session
			return &cloned, nil
		}
	}
	return nil, fmt.Errorf("simulation route time %s is outside explicit sessions for %q", routedAt.Format(policyTimestampLayout), assetClass)
}

// FillFee computes one exact positive fill-attached fee, or nil when the
// configured result rounds to zero.
func (policy *Policy) FillFee(
	assetClass instrument.AssetClass,
	quantity, price, multiplier decimal.Decimal,
	firstFill bool,
) (*decimal.Decimal, error) {
	asset, ok := policy.AssetPolicy(assetClass)
	if !ok {
		return nil, fmt.Errorf("simulation policy has no asset policy for %q", assetClass)
	}
	if err := validatePositivePolicyDecimal("fill quantity", quantity); err != nil {
		return nil, err
	}
	if err := validateNonnegativePolicyDecimal("fill price", price); err != nil {
		return nil, err
	}
	if err := validatePositivePolicyDecimal("fill multiplier", multiplier); err != nil {
		return nil, err
	}
	fee := asset.Fees.PerUnit.Mul(quantity)
	if firstFill {
		fee = fee.Add(asset.Fees.PerOrder)
	}
	notionalFee := price.Mul(quantity).Mul(multiplier).Mul(asset.Fees.NotionalBPS).Div(decimal.NewFromInt(10000))
	fee = fee.Add(notionalFee).Round(asset.Fees.Scale)
	if fee.IsZero() {
		return nil, nil
	}
	if err := validatePositivePolicyDecimal("computed fill fee", fee); err != nil {
		return nil, err
	}
	cloned := fee
	return &cloned, nil
}

// Validate checks artifact identity and exact bytes independently of policy
// interpretation. PolicyFromArtifact additionally requires canonical v1 bytes.
func (artifact PolicyArtifact) Validate() error {
	if artifact.ID == uuid.Nil {
		return fmt.Errorf("simulation policy artifact ID is required")
	}
	if artifact.Schema != PolicySchemaV1 {
		return fmt.Errorf("simulation policy artifact schema must be %q", PolicySchemaV1)
	}
	if len(artifact.SHA256) != 64 || artifact.SHA256 != strings.ToLower(artifact.SHA256) {
		return fmt.Errorf("simulation policy artifact SHA-256 is invalid")
	}
	if _, err := hex.DecodeString(artifact.SHA256); err != nil {
		return fmt.Errorf("simulation policy artifact SHA-256 is invalid: %w", err)
	}
	if len(artifact.CanonicalBytes) == 0 || !json.Valid(artifact.CanonicalBytes) {
		return fmt.Errorf("simulation policy artifact canonical bytes must be valid JSON")
	}
	var object map[string]any
	if err := json.Unmarshal(artifact.CanonicalBytes, &object); err != nil || object == nil {
		return fmt.Errorf("simulation policy artifact canonical bytes must be a JSON object")
	}
	digestBytes := sha256.Sum256(artifact.CanonicalBytes)
	digest := hex.EncodeToString(digestBytes[:])
	if artifact.SHA256 != digest || artifact.Version != artifact.Schema+"@sha256:"+digest {
		return fmt.Errorf("simulation policy artifact version or digest does not match canonical bytes")
	}
	if artifact.ID != economicid.DeterministicUUID(policyArtifactIDDomain, artifact.Version) {
		return fmt.Errorf("simulation policy artifact ID does not match its version")
	}
	if err := validatePolicyTime(artifact.CreatedAt, "artifact creation"); err != nil {
		return err
	}
	return nil
}

// SamePolicyArtifactPayload reports exact semantic retry equality while
// excluding local creation evidence.
func SamePolicyArtifactPayload(left, right *PolicyArtifact) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID && left.Schema == right.Schema && left.Version == right.Version &&
		left.SHA256 == right.SHA256 && bytes.Equal(left.CanonicalBytes, right.CanonicalBytes)
}

func normalizeAssetPolicy(input AssetPolicy) (AssetPolicy, error) {
	if !supportedSimulationAsset(input.AssetClass) {
		return AssetPolicy{}, fmt.Errorf("asset class is unsupported")
	}
	orderTypes, err := normalizeOrderTypes(input.OrderTypes)
	if err != nil {
		return AssetPolicy{}, err
	}
	timeInForce, err := normalizeTimeInForce(input.TimeInForce)
	if err != nil {
		return AssetPolicy{}, err
	}
	requirements, err := normalizeQuoteRequirements(input.QuoteRequirements)
	if err != nil {
		return AssetPolicy{}, err
	}
	if err := validatePositivePolicyDecimal("maximum depth participation", input.MaxDepthParticipation); err != nil {
		return AssetPolicy{}, err
	}
	if input.MaxDepthParticipation.GreaterThan(decimal.NewFromInt(1)) {
		return AssetPolicy{}, fmt.Errorf("maximum depth participation cannot exceed one")
	}
	if input.FixedLatency < 0 {
		return AssetPolicy{}, fmt.Errorf("fixed latency cannot be negative")
	}
	calendar, err := normalizeCalendar(input.Calendar, timeInForce)
	if err != nil {
		return AssetPolicy{}, err
	}
	fees, err := normalizeFees(input.Fees)
	if err != nil {
		return AssetPolicy{}, err
	}
	return AssetPolicy{
		AssetClass:            input.AssetClass,
		OrderTypes:            orderTypes,
		TimeInForce:           timeInForce,
		QuoteRequirements:     requirements,
		MaxDepthParticipation: input.MaxDepthParticipation,
		FixedLatency:          input.FixedLatency,
		Calendar:              calendar,
		Fees:                  fees,
	}, nil
}

func supportedSimulationAsset(value instrument.AssetClass) bool {
	switch value {
	case instrument.AssetClassEquity, instrument.AssetClassETF, instrument.AssetClassOption,
		instrument.AssetClassCryptoSpot, instrument.AssetClassPredictionContract:
		return true
	default:
		return false
	}
}

func normalizeOrderTypes(values []lifecycle.OrderType) ([]lifecycle.OrderType, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one order type is required")
	}
	result := append([]lifecycle.OrderType(nil), values...)
	seen := make(map[lifecycle.OrderType]struct{}, len(result))
	for _, value := range result {
		if value != lifecycle.OrderMarket && value != lifecycle.OrderLimit {
			return nil, fmt.Errorf("order type %q is unsupported", value)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("order type %q is duplicated", value)
		}
		seen[value] = struct{}{}
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}

func normalizeTimeInForce(values []lifecycle.TimeInForce) ([]lifecycle.TimeInForce, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one time in force is required")
	}
	result := append([]lifecycle.TimeInForce(nil), values...)
	seen := make(map[lifecycle.TimeInForce]struct{}, len(result))
	for _, value := range result {
		switch value {
		case lifecycle.TimeInForceDay, lifecycle.TimeInForceGTC, lifecycle.TimeInForceIOC, lifecycle.TimeInForceFOK:
		default:
			return nil, fmt.Errorf("time in force %q is unsupported", value)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("time in force %q is duplicated", value)
		}
		seen[value] = struct{}{}
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}

func normalizeQuoteRequirements(input marketdata.QuoteRequirements) (marketdata.QuoteRequirements, error) {
	if !input.RequireSource || !input.RequireVenueContract || !input.RequireBid || !input.RequireAsk ||
		!input.RequireBidDepth || !input.RequireAskDepth || !input.RequireMarketStatus || !input.RequireSessionStatus {
		return marketdata.QuoteRequirements{}, fmt.Errorf("source, venue contract, bid, ask, both depth sides, and both statuses are required")
	}
	if input.MaxAge <= 0 {
		return marketdata.QuoteRequirements{}, fmt.Errorf("positive maximum quote age is required")
	}
	marketStatuses, err := normalizeStatuses(input.AllowedMarketStatuses, "market")
	if err != nil {
		return marketdata.QuoteRequirements{}, err
	}
	sessionStatuses, err := normalizeStatuses(input.AllowedSessionStatuses, "session")
	if err != nil {
		return marketdata.QuoteRequirements{}, err
	}
	input.AllowedMarketStatuses = marketStatuses
	input.AllowedSessionStatuses = sessionStatuses
	return input, nil
}

func normalizeStatuses(values []string, label string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one allowed %s status is required", label)
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			return nil, fmt.Errorf("allowed %s status cannot be empty", label)
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeCalendar(input CalendarPolicy, timeInForce []lifecycle.TimeInForce) (CalendarPolicy, error) {
	switch input.Kind {
	case CalendarContinuous24x7:
		if len(input.Sessions) != 0 {
			return CalendarPolicy{}, fmt.Errorf("continuous 24/7 calendar cannot contain explicit sessions")
		}
		for _, value := range timeInForce {
			if value == lifecycle.TimeInForceDay {
				return CalendarPolicy{}, fmt.Errorf("continuous 24/7 calendar cannot support DAY")
			}
		}
		return CalendarPolicy{Kind: CalendarContinuous24x7}, nil
	case CalendarExplicitSessions:
		if len(input.Sessions) == 0 {
			return CalendarPolicy{}, fmt.Errorf("explicit calendar requires at least one session")
		}
	default:
		return CalendarPolicy{}, fmt.Errorf("calendar kind %q is invalid", input.Kind)
	}

	sessions := append([]SessionWindow(nil), input.Sessions...)
	labels := make(map[string]struct{}, len(sessions))
	for index := range sessions {
		sessions[index].Label = strings.TrimSpace(sessions[index].Label)
		if sessions[index].Label == "" {
			return CalendarPolicy{}, fmt.Errorf("session label is required")
		}
		if _, duplicate := labels[sessions[index].Label]; duplicate {
			return CalendarPolicy{}, fmt.Errorf("session label %q is duplicated", sessions[index].Label)
		}
		labels[sessions[index].Label] = struct{}{}
		if err := validatePolicyTime(sessions[index].OpenAt, "session open"); err != nil {
			return CalendarPolicy{}, err
		}
		if err := validatePolicyTime(sessions[index].CloseAt, "session close"); err != nil {
			return CalendarPolicy{}, err
		}
		if !sessions[index].CloseAt.After(sessions[index].OpenAt) {
			return CalendarPolicy{}, fmt.Errorf("session %q close must follow open", sessions[index].Label)
		}
	}
	sort.Slice(sessions, func(left, right int) bool {
		if sessions[left].OpenAt.Equal(sessions[right].OpenAt) {
			return sessions[left].Label < sessions[right].Label
		}
		return sessions[left].OpenAt.Before(sessions[right].OpenAt)
	})
	for index := 1; index < len(sessions); index++ {
		if sessions[index].OpenAt.Before(sessions[index-1].CloseAt) {
			return CalendarPolicy{}, fmt.Errorf("sessions %q and %q overlap", sessions[index-1].Label, sessions[index].Label)
		}
	}
	return CalendarPolicy{Kind: CalendarExplicitSessions, Sessions: sessions}, nil
}

func normalizeFees(input FeePolicy) (FeePolicy, error) {
	for name, value := range map[string]decimal.Decimal{
		"per-order fee": input.PerOrder, "per-unit fee": input.PerUnit, "notional basis points": input.NotionalBPS,
	} {
		if err := validateNonnegativePolicyDecimal(name, value); err != nil {
			return FeePolicy{}, err
		}
	}
	if input.Scale < 0 || input.Scale > 12 {
		return FeePolicy{}, fmt.Errorf("fee scale must be between 0 and 12")
	}
	return input, nil
}

func canonicalAsset(asset AssetPolicy) canonicalAssetPolicy {
	orderTypes := make([]string, len(asset.OrderTypes))
	for index, value := range asset.OrderTypes {
		orderTypes[index] = string(value)
	}
	timeInForce := make([]string, len(asset.TimeInForce))
	for index, value := range asset.TimeInForce {
		timeInForce[index] = string(value)
	}
	sessions := make([]canonicalSession, len(asset.Calendar.Sessions))
	for index, session := range asset.Calendar.Sessions {
		sessions[index] = canonicalSession{
			Label: session.Label, OpenAt: session.OpenAt.Format(policyTimestampLayout), CloseAt: session.CloseAt.Format(policyTimestampLayout),
		}
	}
	return canonicalAssetPolicy{
		AssetClass:  string(asset.AssetClass),
		OrderTypes:  orderTypes,
		TimeInForce: timeInForce,
		QuoteRequirements: canonicalQuoteRequirements{
			RequireSource:          asset.QuoteRequirements.RequireSource,
			RequireVenueContract:   asset.QuoteRequirements.RequireVenueContract,
			RequireBid:             asset.QuoteRequirements.RequireBid,
			RequireAsk:             asset.QuoteRequirements.RequireAsk,
			RequireBidDepth:        asset.QuoteRequirements.RequireBidDepth,
			RequireAskDepth:        asset.QuoteRequirements.RequireAskDepth,
			RequireMarketStatus:    asset.QuoteRequirements.RequireMarketStatus,
			RequireSessionStatus:   asset.QuoteRequirements.RequireSessionStatus,
			AllowedMarketStatuses:  append([]string(nil), asset.QuoteRequirements.AllowedMarketStatuses...),
			AllowedSessionStatuses: append([]string(nil), asset.QuoteRequirements.AllowedSessionStatuses...),
			MaxAgeNanoseconds:      int64(asset.QuoteRequirements.MaxAge),
		},
		MaxDepthParticipation:   asset.MaxDepthParticipation.String(),
		FixedLatencyNanoseconds: int64(asset.FixedLatency),
		Calendar:                canonicalCalendar{Kind: string(asset.Calendar.Kind), Sessions: sessions},
		Fees: canonicalFees{
			PerOrder: asset.Fees.PerOrder.String(), PerUnit: asset.Fees.PerUnit.String(),
			NotionalBPS: asset.Fees.NotionalBPS.String(), Scale: asset.Fees.Scale,
		},
	}
}

func decodeCanonicalPolicy(value json.RawMessage) (canonicalPolicy, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var decoded canonicalPolicy
	if err := decoder.Decode(&decoded); err != nil {
		return canonicalPolicy{}, fmt.Errorf("decode canonical simulation policy: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return canonicalPolicy{}, fmt.Errorf("decode canonical simulation policy: trailing JSON value")
		}
		return canonicalPolicy{}, fmt.Errorf("decode canonical simulation policy: %w", err)
	}
	return decoded, nil
}

func policyInputFromCanonical(value canonicalPolicy) (PolicyInput, error) {
	input := PolicyInput{Schema: value.Schema, Assets: make([]AssetPolicy, 0, len(value.Assets))}
	for _, asset := range value.Assets {
		participation, err := decimal.NewFromString(asset.MaxDepthParticipation)
		if err != nil {
			return PolicyInput{}, fmt.Errorf("parse maximum depth participation: %w", err)
		}
		perOrder, err := decimal.NewFromString(asset.Fees.PerOrder)
		if err != nil {
			return PolicyInput{}, fmt.Errorf("parse per-order fee: %w", err)
		}
		perUnit, err := decimal.NewFromString(asset.Fees.PerUnit)
		if err != nil {
			return PolicyInput{}, fmt.Errorf("parse per-unit fee: %w", err)
		}
		notionalBPS, err := decimal.NewFromString(asset.Fees.NotionalBPS)
		if err != nil {
			return PolicyInput{}, fmt.Errorf("parse notional basis points: %w", err)
		}
		orderTypes := make([]lifecycle.OrderType, len(asset.OrderTypes))
		for index, orderType := range asset.OrderTypes {
			orderTypes[index] = lifecycle.OrderType(orderType)
		}
		timeInForce := make([]lifecycle.TimeInForce, len(asset.TimeInForce))
		for index, value := range asset.TimeInForce {
			timeInForce[index] = lifecycle.TimeInForce(value)
		}
		sessions := make([]SessionWindow, len(asset.Calendar.Sessions))
		for index, session := range asset.Calendar.Sessions {
			openAt, err := time.Parse(policyTimestampLayout, session.OpenAt)
			if err != nil {
				return PolicyInput{}, fmt.Errorf("parse session open: %w", err)
			}
			closeAt, err := time.Parse(policyTimestampLayout, session.CloseAt)
			if err != nil {
				return PolicyInput{}, fmt.Errorf("parse session close: %w", err)
			}
			sessions[index] = SessionWindow{Label: session.Label, OpenAt: openAt, CloseAt: closeAt}
		}
		input.Assets = append(input.Assets, AssetPolicy{
			AssetClass:  instrument.AssetClass(asset.AssetClass),
			OrderTypes:  orderTypes,
			TimeInForce: timeInForce,
			QuoteRequirements: marketdata.QuoteRequirements{
				RequireSource:          asset.QuoteRequirements.RequireSource,
				RequireVenueContract:   asset.QuoteRequirements.RequireVenueContract,
				RequireBid:             asset.QuoteRequirements.RequireBid,
				RequireAsk:             asset.QuoteRequirements.RequireAsk,
				RequireBidDepth:        asset.QuoteRequirements.RequireBidDepth,
				RequireAskDepth:        asset.QuoteRequirements.RequireAskDepth,
				RequireMarketStatus:    asset.QuoteRequirements.RequireMarketStatus,
				RequireSessionStatus:   asset.QuoteRequirements.RequireSessionStatus,
				AllowedMarketStatuses:  append([]string(nil), asset.QuoteRequirements.AllowedMarketStatuses...),
				AllowedSessionStatuses: append([]string(nil), asset.QuoteRequirements.AllowedSessionStatuses...),
				MaxAge:                 time.Duration(asset.QuoteRequirements.MaxAgeNanoseconds),
			},
			MaxDepthParticipation: participation,
			FixedLatency:          time.Duration(asset.FixedLatencyNanoseconds),
			Calendar:              CalendarPolicy{Kind: CalendarKind(asset.Calendar.Kind), Sessions: sessions},
			Fees:                  FeePolicy{PerOrder: perOrder, PerUnit: perUnit, NotionalBPS: notionalBPS, Scale: asset.Fees.Scale},
		})
	}
	return input, nil
}

func cloneAssetPolicies(values []AssetPolicy) []AssetPolicy {
	cloned := make([]AssetPolicy, len(values))
	for index := range values {
		cloned[index] = cloneAssetPolicy(values[index])
	}
	return cloned
}

func cloneAssetPolicy(value AssetPolicy) AssetPolicy {
	value.OrderTypes = append([]lifecycle.OrderType(nil), value.OrderTypes...)
	value.TimeInForce = append([]lifecycle.TimeInForce(nil), value.TimeInForce...)
	value.QuoteRequirements.AllowedMarketStatuses = append([]string(nil), value.QuoteRequirements.AllowedMarketStatuses...)
	value.QuoteRequirements.AllowedSessionStatuses = append([]string(nil), value.QuoteRequirements.AllowedSessionStatuses...)
	value.Calendar.Sessions = append([]SessionWindow(nil), value.Calendar.Sessions...)
	return value
}

func validatePolicyTime(value time.Time, label string) error {
	if value.IsZero() || value.Location() != time.UTC || !value.Equal(value.Truncate(time.Microsecond)) {
		return fmt.Errorf("simulation policy %s time must use UTC microsecond precision", label)
	}
	return nil
}

func validatePositivePolicyDecimal(label string, value decimal.Decimal) error {
	if !value.IsPositive() {
		return fmt.Errorf("simulation policy %s must be positive", label)
	}
	return validatePolicyDecimalShape(label, value)
}

func validateNonnegativePolicyDecimal(label string, value decimal.Decimal) error {
	if value.IsNegative() {
		return fmt.Errorf("simulation policy %s cannot be negative", label)
	}
	return validatePolicyDecimalShape(label, value)
}

func validatePolicyDecimalShape(label string, value decimal.Decimal) error {
	if !value.Equal(value.Round(12)) {
		return fmt.Errorf("simulation policy %s supports at most 12 decimal places", label)
	}
	if value.Abs().NumDigits()+int(value.Exponent()) > 26 {
		return fmt.Errorf("simulation policy %s exceeds exact decimal magnitude", label)
	}
	return nil
}
