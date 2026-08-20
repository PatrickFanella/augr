// Package momentum owns the deterministic momentum/quality baseline research
// program. It has no provider, scheduler, promotion, or runtime authority.
package momentum

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
	PolicySchemaV1   = "momentum-quality-policy-v1"
	ScenarioSchemaV1 = "momentum-quality-scenario-v1"
	ReportSchemaV1   = "momentum-quality-report-v1"
	timeLayout       = "2006-01-02T15:04:05.000000Z"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type PolicyInput struct {
	Version                   string
	LookbackDays              int
	SkipDays                  int
	MinimumHistoryDays        int
	MinimumROIC               string
	MaximumDebtToAssets       string
	RequirePositiveFreeCash   bool
	MaximumEvidenceAgeSeconds int64
	MaximumVolatility         string
	PortfolioSize             int
	MaximumRebalanceTurnover  string
	CostBPS                   string
	BullBearTrendThreshold    string
	MaximumBullVolatility     string
	DecimalScale              int
}

type policyCanonical struct {
	Schema                    string `json:"schema"`
	Version                   string `json:"version"`
	LookbackDays              int    `json:"lookback_days"`
	SkipDays                  int    `json:"skip_days"`
	MinimumHistoryDays        int    `json:"minimum_history_days"`
	MinimumROIC               string `json:"minimum_roic"`
	MaximumDebtToAssets       string `json:"maximum_debt_to_assets"`
	RequirePositiveFreeCash   bool   `json:"require_positive_free_cash"`
	MaximumEvidenceAgeSeconds int64  `json:"maximum_evidence_age_seconds"`
	MaximumVolatility         string `json:"maximum_volatility"`
	PortfolioSize             int    `json:"portfolio_size"`
	MaximumRebalanceTurnover  string `json:"maximum_rebalance_turnover"`
	CostBPS                   string `json:"cost_bps"`
	BullBearTrendThreshold    string `json:"bull_bear_trend_threshold"`
	MaximumBullVolatility     string `json:"maximum_bull_volatility"`
	Weighting                 string `json:"weighting"`
	PricingConvention         string `json:"pricing_convention"`
	TurnoverConvention        string `json:"turnover_convention"`
	RegimeConvention          string `json:"regime_convention"`
	DecimalScale              int    `json:"decimal_scale"`
}

type Policy struct {
	canonical policyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	if input.Version == "" || input.LookbackDays < 20 || input.LookbackDays > 1000 || input.SkipDays < 0 || input.SkipDays >= input.LookbackDays || input.MinimumHistoryDays < input.LookbackDays || input.MinimumHistoryDays > 5000 ||
		!ratio(input.MinimumROIC) || !ratio(input.MaximumDebtToAssets) || input.MaximumEvidenceAgeSeconds < 1 || input.MaximumEvidenceAgeSeconds > 366*24*3600 || !positive(input.MaximumVolatility) || input.PortfolioSize < 1 || input.PortfolioSize > 10000 ||
		!positiveRatio(input.MaximumRebalanceTurnover) || !nonnegative(input.CostBPS) || decimal.RequireFromString(input.CostBPS).GreaterThan(decimal.NewFromInt(10000)) || !positiveRatio(input.BullBearTrendThreshold) || !positive(input.MaximumBullVolatility) || input.DecimalScale < 6 || input.DecimalScale > 18 {
		return nil, fmt.Errorf("momentum policy is invalid")
	}
	canonical := policyCanonical{Schema: PolicySchemaV1, Version: input.Version, LookbackDays: input.LookbackDays, SkipDays: input.SkipDays, MinimumHistoryDays: input.MinimumHistoryDays, MinimumROIC: input.MinimumROIC, MaximumDebtToAssets: input.MaximumDebtToAssets, RequirePositiveFreeCash: input.RequirePositiveFreeCash, MaximumEvidenceAgeSeconds: input.MaximumEvidenceAgeSeconds, MaximumVolatility: input.MaximumVolatility, PortfolioSize: input.PortfolioSize, MaximumRebalanceTurnover: input.MaximumRebalanceTurnover, CostBPS: input.CostBPS, BullBearTrendThreshold: input.BullBearTrendThreshold, MaximumBullVolatility: input.MaximumBullVolatility, Weighting: "equal_weight", PricingConvention: "sell_bid_buy_ask", TurnoverConvention: "half_absolute_weight_change", RegimeConvention: "benchmark_trend_and_volatility", DecimalScale: input.DecimalScale}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Policy{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("momentum-quality-policy", PolicySchemaV1+"@sha256:"+digest)}, nil
}

func PolicyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Policy, error) {
	var canonical policyCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("momentum policy envelope is invalid")
	}
	value, err := NewPolicy(PolicyInput{Version: canonical.Version, LookbackDays: canonical.LookbackDays, SkipDays: canonical.SkipDays, MinimumHistoryDays: canonical.MinimumHistoryDays, MinimumROIC: canonical.MinimumROIC, MaximumDebtToAssets: canonical.MaximumDebtToAssets, RequirePositiveFreeCash: canonical.RequirePositiveFreeCash, MaximumEvidenceAgeSeconds: canonical.MaximumEvidenceAgeSeconds, MaximumVolatility: canonical.MaximumVolatility, PortfolioSize: canonical.PortfolioSize, MaximumRebalanceTurnover: canonical.MaximumRebalanceTurnover, CostBPS: canonical.CostBPS, BullBearTrendThreshold: canonical.BullBearTrendThreshold, MaximumBullVolatility: canonical.MaximumBullVolatility, DecimalScale: canonical.DecimalScale})
	if err != nil || canonical.Schema != PolicySchemaV1 || canonical.Weighting != "equal_weight" || canonical.PricingConvention != "sell_bid_buy_ask" || canonical.TurnoverConvention != "half_absolute_weight_change" || canonical.RegimeConvention != "benchmark_trend_and_volatility" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("momentum policy identity does not reconstruct")
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
	return err == nil && parsed.String() == value && parsed.Abs().LessThan(decimal.New(1, 30))
}

func positive(value string) bool {
	return validDecimal(value) && decimal.RequireFromString(value).IsPositive()
}

func nonnegative(value string) bool {
	return validDecimal(value) && !decimal.RequireFromString(value).IsNegative()
}

func ratio(value string) bool {
	return nonnegative(value) && decimal.RequireFromString(value).LessThanOrEqual(decimal.NewFromInt(1))
}

func positiveRatio(value string) bool {
	return ratio(value) && decimal.RequireFromString(value).IsPositive()
}

func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Equal(value.Truncate(time.Microsecond))
}
func formatTime(value time.Time) string { return value.Format(timeLayout) }
func parseTime(value string) time.Time {
	parsed, _ := time.Parse(timeLayout, value)
	return parsed
}

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
