// Package trend owns the deterministic ETF time-series trend research program.
// It has no provider, scheduler, promotion, allocation, or runtime authority.
package trend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	PolicySchemaV1   = "etf-time-series-trend-policy-v1"
	ScenarioSchemaV1 = "etf-time-series-trend-scenario-v1"
	ReportSchemaV1   = "etf-time-series-trend-report-v1"
	timeLayout       = "2006-01-02T15:04:05.000000Z"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Horizon struct {
	Days   int    `json:"days"`
	Weight string `json:"weight"`
}

type PolicyInput struct {
	Version                   string
	Horizons                  []Horizon
	SignalThreshold           string
	VolatilityWindowDays      int
	AnnualizationDays         int
	TargetVolatility          string
	MaximumInstrumentWeight   string
	MaximumGrossWeight        string
	MaximumRebalanceTurnover  string
	MaximumEvidenceAgeSeconds int64
	CostBPS                   string
	DecimalScale              int
}

type policyCanonical struct {
	Schema                    string    `json:"schema"`
	Version                   string    `json:"version"`
	Horizons                  []Horizon `json:"horizons"`
	SignalThreshold           string    `json:"signal_threshold"`
	VolatilityWindowDays      int       `json:"volatility_window_days"`
	AnnualizationDays         int       `json:"annualization_days"`
	TargetVolatility          string    `json:"target_volatility"`
	MaximumInstrumentWeight   string    `json:"maximum_instrument_weight"`
	MaximumGrossWeight        string    `json:"maximum_gross_weight"`
	MaximumRebalanceTurnover  string    `json:"maximum_rebalance_turnover"`
	MaximumEvidenceAgeSeconds int64     `json:"maximum_evidence_age_seconds"`
	CostBPS                   string    `json:"cost_bps"`
	SignalConvention          string    `json:"signal_convention"`
	SizingConvention          string    `json:"sizing_convention"`
	PricingConvention         string    `json:"pricing_convention"`
	DecimalScale              int       `json:"decimal_scale"`
}

type Policy struct {
	canonical policyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	horizons := append([]Horizon(nil), input.Horizons...)
	sort.Slice(horizons, func(i, j int) bool { return horizons[i].Days < horizons[j].Days })
	weight := decimal.Zero
	for index, horizon := range horizons {
		if horizon.Days < 2 || index > 0 && horizons[index-1].Days == horizon.Days || !positiveRatio(horizon.Weight) {
			return nil, fmt.Errorf("trend policy horizon is invalid")
		}
		weight = weight.Add(decimal.RequireFromString(horizon.Weight))
	}
	if input.Version == "" || len(horizons) < 2 || len(horizons) > 12 || !weight.Equal(decimal.NewFromInt(1)) || !nonnegative(input.SignalThreshold) || decimal.RequireFromString(input.SignalThreshold).GreaterThanOrEqual(decimal.NewFromInt(1)) || input.VolatilityWindowDays < 2 || input.AnnualizationDays < 2 || !positiveRatio(input.TargetVolatility) || !positiveRatio(input.MaximumInstrumentWeight) || !positiveRatio(input.MaximumGrossWeight) || !positiveRatio(input.MaximumRebalanceTurnover) || input.MaximumEvidenceAgeSeconds < 1 || !nonnegative(input.CostBPS) || decimal.RequireFromString(input.CostBPS).GreaterThan(decimal.NewFromInt(10000)) || input.DecimalScale < 6 || input.DecimalScale > 18 {
		return nil, fmt.Errorf("trend policy is invalid")
	}
	canonical := policyCanonical{Schema: PolicySchemaV1, Version: input.Version, Horizons: horizons, SignalThreshold: input.SignalThreshold, VolatilityWindowDays: input.VolatilityWindowDays, AnnualizationDays: input.AnnualizationDays, TargetVolatility: input.TargetVolatility, MaximumInstrumentWeight: input.MaximumInstrumentWeight, MaximumGrossWeight: input.MaximumGrossWeight, MaximumRebalanceTurnover: input.MaximumRebalanceTurnover, MaximumEvidenceAgeSeconds: input.MaximumEvidenceAgeSeconds, CostBPS: input.CostBPS, SignalConvention: "weighted_horizon_return_sign", SizingConvention: "target_volatility_capped_no_upward_normalization", PricingConvention: "sell_bid_buy_ask_whole_lot", DecimalScale: input.DecimalScale}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Policy{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("etf-time-series-trend-policy", PolicySchemaV1+"@sha256:"+digest)}, nil
}

func PolicyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Policy, error) {
	var canonical policyCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("trend policy envelope is invalid")
	}
	value, err := NewPolicy(PolicyInput{Version: canonical.Version, Horizons: canonical.Horizons, SignalThreshold: canonical.SignalThreshold, VolatilityWindowDays: canonical.VolatilityWindowDays, AnnualizationDays: canonical.AnnualizationDays, TargetVolatility: canonical.TargetVolatility, MaximumInstrumentWeight: canonical.MaximumInstrumentWeight, MaximumGrossWeight: canonical.MaximumGrossWeight, MaximumRebalanceTurnover: canonical.MaximumRebalanceTurnover, MaximumEvidenceAgeSeconds: canonical.MaximumEvidenceAgeSeconds, CostBPS: canonical.CostBPS, DecimalScale: canonical.DecimalScale})
	if err != nil || canonical.Schema != PolicySchemaV1 || canonical.SignalConvention != "weighted_horizon_return_sign" || canonical.SizingConvention != "target_volatility_capped_no_upward_normalization" || canonical.PricingConvention != "sell_bid_buy_ask_whole_lot" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("trend policy identity does not reconstruct")
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

func positiveRatio(value string) bool {
	return positive(value) && decimal.RequireFromString(value).LessThanOrEqual(decimal.NewFromInt(1))
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
