// Package definedrisk owns the deterministic defined-risk vertical-spread V1
// research program. It does not route, promote, schedule, deploy, or contact a
// provider.
package definedrisk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	PolicySchemaV1   = "defined-risk-options-policy-v1"
	ScenarioSchemaV1 = "defined-risk-options-scenario-v1"
	ReportSchemaV1   = "defined-risk-options-report-v1"
	timeLayout       = "2006-01-02T15:04:05.000000Z"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ExecutionMode string

const (
	ExecutionAtomic     ExecutionMode = "atomic_package"
	ExecutionSequential ExecutionMode = "sequential_protective_first"
)

type PolicyInput struct {
	Version                   string
	ExecutionMode             ExecutionMode
	MaximumEvidenceAgeSeconds int64
	MaximumContracts          int
	MaximumPositionCapital    string
	FeePerContractPerLeg      string
	DecimalScale              int
}

type policyCanonical struct {
	Schema                    string        `json:"schema"`
	Version                   string        `json:"version"`
	ExecutionMode             ExecutionMode `json:"execution_mode"`
	LegOrder                  string        `json:"leg_order"`
	PricingConvention         string        `json:"pricing_convention"`
	OrphanConvention          string        `json:"orphan_convention"`
	SettlementConvention      string        `json:"settlement_convention"`
	MaximumEvidenceAgeSeconds int64         `json:"maximum_evidence_age_seconds"`
	MaximumContracts          int           `json:"maximum_contracts"`
	MaximumPositionCapital    string        `json:"maximum_position_capital"`
	FeePerContractPerLeg      string        `json:"fee_per_contract_per_leg"`
	DecimalScale              int           `json:"decimal_scale"`
}

type Policy struct {
	canonical policyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	if input.Version == "" || input.ExecutionMode != ExecutionAtomic && input.ExecutionMode != ExecutionSequential || input.MaximumEvidenceAgeSeconds < 1 || input.MaximumEvidenceAgeSeconds > 7*24*3600 || input.MaximumContracts < 1 || input.MaximumContracts > 100000 || !positive(input.MaximumPositionCapital) || !nonnegative(input.FeePerContractPerLeg) || input.DecimalScale < 6 || input.DecimalScale > 18 {
		return nil, fmt.Errorf("defined-risk policy is invalid")
	}
	canonical := policyCanonical{Schema: PolicySchemaV1, Version: input.Version, ExecutionMode: input.ExecutionMode, LegOrder: "protective_long_then_short", PricingConvention: "buy_ask_sell_bid_depth_capped", OrphanConvention: "immediate_opposite_side_executable_unwind", SettlementConvention: "european_cash_intrinsic_at_expiry", MaximumEvidenceAgeSeconds: input.MaximumEvidenceAgeSeconds, MaximumContracts: input.MaximumContracts, MaximumPositionCapital: input.MaximumPositionCapital, FeePerContractPerLeg: input.FeePerContractPerLeg, DecimalScale: input.DecimalScale}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Policy{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("defined-risk-options-policy", PolicySchemaV1+"@sha256:"+digest)}, nil
}

func PolicyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Policy, error) {
	var canonical policyCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("defined-risk policy envelope is invalid")
	}
	value, err := NewPolicy(PolicyInput{canonical.Version, canonical.ExecutionMode, canonical.MaximumEvidenceAgeSeconds, canonical.MaximumContracts, canonical.MaximumPositionCapital, canonical.FeePerContractPerLeg, canonical.DecimalScale})
	if err != nil || canonical.Schema != PolicySchemaV1 || canonical.LegOrder != "protective_long_then_short" || canonical.PricingConvention != "buy_ask_sell_bid_depth_capped" || canonical.OrphanConvention != "immediate_opposite_side_executable_unwind" || canonical.SettlementConvention != "european_cash_intrinsic_at_expiry" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("defined-risk policy identity does not reconstruct")
	}
	return value, nil
}

func (p *Policy) ID() uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return p.id
}

func (p *Policy) Digest() string {
	if p == nil {
		return ""
	}
	return p.digest
}

func (p *Policy) CanonicalBytes() json.RawMessage {
	if p == nil {
		return nil
	}
	return append(json.RawMessage(nil), p.bytes...)
}

func (p *Policy) DecimalScale() int {
	if p == nil {
		return 0
	}
	return p.canonical.DecimalScale
}

func validDecimal(value string) bool {
	parsed, err := decimal.NewFromString(value)
	return err == nil && value == parsed.String() && len(value) <= 128 && parsed.Abs().LessThanOrEqual(decimal.New(1, 30))
}

func positive(value string) bool {
	return validDecimal(value) && decimal.RequireFromString(value).IsPositive()
}

func nonnegative(value string) bool {
	return validDecimal(value) && !decimal.RequireFromString(value).IsNegative()
}

func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Equal(value.Truncate(time.Microsecond))
}

func formatTime(value time.Time) string {
	return value.Format(timeLayout)
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(timeLayout, value)
	return parsed
}

func hash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("canonical JSON contains extra data")
	}
	return nil
}
