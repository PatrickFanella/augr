// Package wheel owns the deterministic quality-filtered wheel V1 research
// program. It does not promote, schedule, deploy, or contact a provider.
package wheel

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
	PolicySchemaV1   = "quality-filtered-wheel-policy-v1"
	ScenarioSchemaV1 = "quality-filtered-wheel-scenario-v1"
	ReportSchemaV1   = "quality-filtered-wheel-report-v1"
	timeLayout       = "2006-01-02T15:04:05.000000Z"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type PolicyInput struct {
	Version                     string
	MinimumROIC                 string
	MaximumDebtToAssets         string
	RequirePositiveFreeCash     bool
	MaximumQualityAgeSeconds    int64
	MaximumMarketDataAgeSeconds int64
	PutDeltaMinimum             string
	PutDeltaTarget              string
	PutDeltaMaximum             string
	CallDeltaMinimum            string
	CallDeltaTarget             string
	CallDeltaMaximum            string
	MinimumDTE                  int
	MaximumDTE                  int
	MinimumOpenInterest         string
	MinimumVolume               string
	MaximumSpreadRatio          string
	DeliverableQuantity         string
	MaximumContracts            int
	FeePerContract              string
	FeePerShare                 string
	DecimalScale                int
}

type policyCanonical struct {
	Schema                      string `json:"schema"`
	Version                     string `json:"version"`
	MinimumROIC                 string `json:"minimum_roic"`
	MaximumDebtToAssets         string `json:"maximum_debt_to_assets"`
	RequirePositiveFreeCash     bool   `json:"require_positive_free_cash"`
	MaximumQualityAgeSeconds    int64  `json:"maximum_quality_age_seconds"`
	MaximumMarketDataAgeSeconds int64  `json:"maximum_market_data_age_seconds"`
	PutDeltaMinimum             string `json:"put_delta_minimum"`
	PutDeltaTarget              string `json:"put_delta_target"`
	PutDeltaMaximum             string `json:"put_delta_maximum"`
	CallDeltaMinimum            string `json:"call_delta_minimum"`
	CallDeltaTarget             string `json:"call_delta_target"`
	CallDeltaMaximum            string `json:"call_delta_maximum"`
	MinimumDTE                  int    `json:"minimum_dte"`
	MaximumDTE                  int    `json:"maximum_dte"`
	MinimumOpenInterest         string `json:"minimum_open_interest"`
	MinimumVolume               string `json:"minimum_volume"`
	MaximumSpreadRatio          string `json:"maximum_spread_ratio"`
	DeliverableQuantity         string `json:"deliverable_quantity"`
	MaximumContracts            int    `json:"maximum_contracts"`
	FeePerContract              string `json:"fee_per_contract"`
	FeePerShare                 string `json:"fee_per_share"`
	PricingConvention           string `json:"pricing_convention"`
	CollateralConvention        string `json:"collateral_convention"`
	SettlementConvention        string `json:"settlement_convention"`
	DividendEntitlement         string `json:"dividend_entitlement"`
	DecimalScale                int    `json:"decimal_scale"`
}

type Policy struct {
	canonical policyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	putMin, putTarget, putMax, err := orderedRatios(input.PutDeltaMinimum, input.PutDeltaTarget, input.PutDeltaMaximum)
	if err != nil {
		return nil, fmt.Errorf("wheel put delta policy is invalid")
	}
	callMin, callTarget, callMax, err := orderedRatios(input.CallDeltaMinimum, input.CallDeltaTarget, input.CallDeltaMaximum)
	if err != nil {
		return nil, fmt.Errorf("wheel call delta policy is invalid")
	}
	if input.Version == "" || !ratio(input.MinimumROIC) || !ratio(input.MaximumDebtToAssets) || input.MaximumQualityAgeSeconds <= 0 || input.MaximumQualityAgeSeconds > 366*24*3600 || input.MaximumMarketDataAgeSeconds <= 0 || input.MaximumMarketDataAgeSeconds > 7*24*3600 ||
		input.MinimumDTE < 1 || input.MaximumDTE < input.MinimumDTE || input.MaximumDTE > 730 || !nonnegative(input.MinimumOpenInterest) || !nonnegative(input.MinimumVolume) ||
		!ratio(input.MaximumSpreadRatio) || decimal.RequireFromString(input.MaximumSpreadRatio).IsZero() || !positive(input.DeliverableQuantity) || input.MaximumContracts < 1 || input.MaximumContracts > 100000 ||
		!nonnegative(input.FeePerContract) || !nonnegative(input.FeePerShare) || input.DecimalScale < 6 || input.DecimalScale > 18 {
		return nil, fmt.Errorf("wheel policy is invalid")
	}
	canonical := policyCanonical{
		Schema: PolicySchemaV1, Version: input.Version, MinimumROIC: input.MinimumROIC, MaximumDebtToAssets: input.MaximumDebtToAssets,
		RequirePositiveFreeCash: input.RequirePositiveFreeCash, MaximumQualityAgeSeconds: input.MaximumQualityAgeSeconds, MaximumMarketDataAgeSeconds: input.MaximumMarketDataAgeSeconds, PutDeltaMinimum: putMin.String(), PutDeltaTarget: putTarget.String(), PutDeltaMaximum: putMax.String(),
		CallDeltaMinimum: callMin.String(), CallDeltaTarget: callTarget.String(), CallDeltaMaximum: callMax.String(), MinimumDTE: input.MinimumDTE, MaximumDTE: input.MaximumDTE,
		MinimumOpenInterest: input.MinimumOpenInterest, MinimumVolume: input.MinimumVolume, MaximumSpreadRatio: input.MaximumSpreadRatio, DeliverableQuantity: input.DeliverableQuantity,
		MaximumContracts: input.MaximumContracts, FeePerContract: input.FeePerContract, FeePerShare: input.FeePerShare, PricingConvention: "short_open_at_bid_long_close_at_ask",
		CollateralConvention: "strike_times_deliverable_excluding_premium", SettlementConvention: "automatic_itm_at_expiry_or_sourced_early_assignment",
		DividendEntitlement: "shares_held_at_effective_at", DecimalScale: input.DecimalScale,
	}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Policy{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("quality-filtered-wheel-policy", PolicySchemaV1+"@sha256:"+digest)}, nil
}

func PolicyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Policy, error) {
	var canonical policyCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("wheel policy envelope is invalid")
	}
	value, err := NewPolicy(PolicyInput{
		Version: canonical.Version, MinimumROIC: canonical.MinimumROIC, MaximumDebtToAssets: canonical.MaximumDebtToAssets, RequirePositiveFreeCash: canonical.RequirePositiveFreeCash,
		MaximumQualityAgeSeconds: canonical.MaximumQualityAgeSeconds, MaximumMarketDataAgeSeconds: canonical.MaximumMarketDataAgeSeconds, PutDeltaMinimum: canonical.PutDeltaMinimum, PutDeltaTarget: canonical.PutDeltaTarget, PutDeltaMaximum: canonical.PutDeltaMaximum,
		CallDeltaMinimum: canonical.CallDeltaMinimum, CallDeltaTarget: canonical.CallDeltaTarget, CallDeltaMaximum: canonical.CallDeltaMaximum, MinimumDTE: canonical.MinimumDTE, MaximumDTE: canonical.MaximumDTE,
		MinimumOpenInterest: canonical.MinimumOpenInterest, MinimumVolume: canonical.MinimumVolume, MaximumSpreadRatio: canonical.MaximumSpreadRatio, DeliverableQuantity: canonical.DeliverableQuantity,
		MaximumContracts: canonical.MaximumContracts, FeePerContract: canonical.FeePerContract, FeePerShare: canonical.FeePerShare, DecimalScale: canonical.DecimalScale,
	})
	if err != nil || canonical.Schema != PolicySchemaV1 || canonical.PricingConvention != "short_open_at_bid_long_close_at_ask" || canonical.CollateralConvention != "strike_times_deliverable_excluding_premium" ||
		canonical.SettlementConvention != "automatic_itm_at_expiry_or_sourced_early_assignment" || canonical.DividendEntitlement != "shares_held_at_effective_at" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("wheel policy identity does not reconstruct")
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

func (p *Policy) Version() string {
	if p == nil {
		return ""
	}
	return p.canonical.Version
}

func (p *Policy) DecimalScale() int {
	if p == nil {
		return 0
	}
	return p.canonical.DecimalScale
}

func orderedRatios(minimum, target, maximum string) (decimal.Decimal, decimal.Decimal, decimal.Decimal, error) {
	if !ratio(minimum) || !ratio(target) || !ratio(maximum) {
		return decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("ratio invalid")
	}
	a, b, c := decimal.RequireFromString(minimum), decimal.RequireFromString(target), decimal.RequireFromString(maximum)
	if a.IsZero() || a.GreaterThan(b) || b.GreaterThan(c) {
		return decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("ratio order invalid")
	}
	return a, b, c, nil
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
func signed(value string) bool { return validDecimal(value) }
func ratio(value string) bool {
	return nonnegative(value) && decimal.RequireFromString(value).LessThanOrEqual(decimal.NewFromInt(1))
}

func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Equal(value.Truncate(time.Microsecond))
}
func formatTime(value time.Time) string { return value.Format(timeLayout) }
func parseTime(value string) time.Time  { parsed, _ := time.Parse(timeLayout, value); return parsed }

func hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

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
