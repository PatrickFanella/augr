// Package evaluation owns immutable, result-bound trade and portfolio reports.
// It does not select, approve, promote, schedule, or deploy strategies.
package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	PolicySchemaV1         = "evaluation-policy-v1"
	ReportSchemaV1         = "trade-portfolio-evaluation-v1"
	MetricAvailable        = "available"
	MetricUnavailable      = "unavailable"
	MetricPositiveInfinity = "positive_infinity"
	timeLayout             = "2006-01-02T15:04:05.000000Z"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type PolicyInput struct {
	Version            string
	Frequency          string
	PeriodsPerYear     int
	ReturnKind         string
	CashConvention     string
	LotMethod          string
	RecoveryDefinition string
	DecimalScale       int
}

type policyCanonical struct {
	Schema             string `json:"schema"`
	Version            string `json:"version"`
	Frequency          string `json:"frequency"`
	PeriodsPerYear     int    `json:"periods_per_year"`
	ReturnKind         string `json:"return_kind"`
	CashConvention     string `json:"cash_convention"`
	LotMethod          string `json:"lot_method"`
	RecoveryDefinition string `json:"recovery_definition"`
	DecimalScale       int    `json:"decimal_scale"`
}

type Policy struct {
	canonical policyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	if !canonicalText(input.Version, 128) || !oneOf(input.Frequency, "daily", "weekly", "monthly") || input.PeriodsPerYear <= 0 || input.PeriodsPerYear > 100000 ||
		input.ReturnKind != "simple" || input.CashConvention != "explicit_per_period" || input.LotMethod != "fifo" ||
		input.RecoveryDefinition != "first_equity_at_or_above_prior_peak" || input.DecimalScale < 6 || input.DecimalScale > 18 {
		return nil, fmt.Errorf("evaluation policy is invalid")
	}
	canonical := policyCanonical{
		Schema: PolicySchemaV1, Version: input.Version, Frequency: input.Frequency, PeriodsPerYear: input.PeriodsPerYear,
		ReturnKind: input.ReturnKind, CashConvention: input.CashConvention, LotMethod: input.LotMethod,
		RecoveryDefinition: input.RecoveryDefinition, DecimalScale: input.DecimalScale,
	}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Policy{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("evaluation-policy", PolicySchemaV1+"@sha256:"+digest)}, nil
}

func PolicyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Policy, error) {
	var canonical policyCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("evaluation policy envelope is invalid")
	}
	value, err := NewPolicy(PolicyInput{
		Version: canonical.Version, Frequency: canonical.Frequency, PeriodsPerYear: canonical.PeriodsPerYear,
		ReturnKind: canonical.ReturnKind, CashConvention: canonical.CashConvention, LotMethod: canonical.LotMethod,
		RecoveryDefinition: canonical.RecoveryDefinition, DecimalScale: canonical.DecimalScale,
	})
	if err != nil || canonical.Schema != PolicySchemaV1 || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("evaluation policy identity does not reconstruct")
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

func (p *Policy) Frequency() string {
	if p == nil {
		return ""
	}
	return p.canonical.Frequency
}

func (p *Policy) PeriodsPerYear() int {
	if p == nil {
		return 0
	}
	return p.canonical.PeriodsPerYear
}

func (p *Policy) ReturnKind() string {
	if p == nil {
		return ""
	}
	return p.canonical.ReturnKind
}

func (p *Policy) CashConvention() string {
	if p == nil {
		return ""
	}
	return p.canonical.CashConvention
}

func (p *Policy) LotMethod() string {
	if p == nil {
		return ""
	}
	return p.canonical.LotMethod
}

func (p *Policy) RecoveryDefinition() string {
	if p == nil {
		return ""
	}
	return p.canonical.RecoveryDefinition
}

func (p *Policy) DecimalScale() int {
	if p == nil {
		return 0
	}
	return p.canonical.DecimalScale
}

type ObservationInput struct {
	ObservedAt                 time.Time
	Equity                     string
	BenchmarkValue             string
	CashReturn                 string
	GrossExposure              string
	NetExposure                string
	LargestPositionWeight      string
	CumulativeOwnershipCost    string
	CumulativeTurnover         string
	CumulativeModeledSlippage  string
	CumulativeObservedSlippage *string
	EvidenceID                 uuid.UUID
	EvidenceSHA256             string
}

type ClosedTradeInput struct {
	InstrumentID       uuid.UUID
	Side               string
	Quantity           string
	EntryFillIDs       []uuid.UUID
	ExitFillIDs        []uuid.UUID
	EntryAt            time.Time
	ExitAt             time.Time
	EntryPrice         string
	ExitPrice          string
	EntryFees          string
	ExitFees           string
	OtherOwnershipCost string
	GrossPnL           string
	AfterCostPnL       string
}

type ExecutionInput struct{ AttemptedOrders, FilledOrders, AttemptedQuantity, FilledQuantity string }

type Metric struct {
	Section     string `json:"section"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
}

type observationCanonical struct {
	Sequence                   int     `json:"sequence"`
	ObservedAt                 string  `json:"observed_at"`
	Equity                     string  `json:"equity"`
	BenchmarkValue             string  `json:"benchmark_value"`
	CashReturn                 string  `json:"cash_return"`
	GrossExposure              string  `json:"gross_exposure"`
	NetExposure                string  `json:"net_exposure"`
	LargestPositionWeight      string  `json:"largest_position_weight"`
	CumulativeOwnershipCost    string  `json:"cumulative_ownership_cost"`
	CumulativeTurnover         string  `json:"cumulative_turnover"`
	CumulativeModeledSlippage  string  `json:"cumulative_modeled_slippage"`
	CumulativeObservedSlippage *string `json:"cumulative_observed_slippage"`
	EvidenceID                 string  `json:"evidence_id"`
	EvidenceSHA256             string  `json:"evidence_sha256"`
}

type tradeCanonical struct {
	Sequence           int      `json:"sequence"`
	InstrumentID       string   `json:"instrument_id"`
	Side               string   `json:"side"`
	Quantity           string   `json:"quantity"`
	EntryFillIDs       []string `json:"entry_fill_ids"`
	ExitFillIDs        []string `json:"exit_fill_ids"`
	EntryAt            string   `json:"entry_at"`
	ExitAt             string   `json:"exit_at"`
	EntryPrice         string   `json:"entry_price"`
	ExitPrice          string   `json:"exit_price"`
	EntryFees          string   `json:"entry_fees"`
	ExitFees           string   `json:"exit_fees"`
	OtherOwnershipCost string   `json:"other_ownership_cost"`
	GrossPnL           string   `json:"gross_pnl"`
	AfterCostPnL       string   `json:"after_cost_pnl"`
}

type reportCanonical struct {
	Schema          string                         `json:"schema"`
	State           string                         `json:"state"`
	ResultID        string                         `json:"result_id"`
	ResultSHA256    string                         `json:"result_sha256"`
	ExperimentID    string                         `json:"experiment_id"`
	ProgramID       string                         `json:"program_id"`
	PlanID          string                         `json:"plan_id"`
	AccountID       string                         `json:"account_id"`
	ManifestID      string                         `json:"manifest_id"`
	QualityResultID string                         `json:"quality_result_id"`
	Mode            strategycatalog.ExperimentMode `json:"mode"`
	PolicyID        string                         `json:"policy_id"`
	PolicySHA256    string                         `json:"policy_sha256"`
	EvaluationStart string                         `json:"evaluation_start"`
	EvaluationEnd   string                         `json:"evaluation_end"`
	OpenLotCount    int                            `json:"open_lot_count"`
	Execution       ExecutionInput                 `json:"execution"`
	Observations    []observationCanonical         `json:"observations"`
	ClosedTrades    []tradeCanonical               `json:"closed_trades"`
	Metrics         []Metric                       `json:"metrics"`
}

type ReportInput struct {
	Result          *experimentrun.Result
	Policy          *Policy
	EvaluationStart time.Time
	EvaluationEnd   time.Time
	OpenLotCount    int
	Execution       ExecutionInput
	Observations    []ObservationInput
	ClosedTrades    []ClosedTradeInput
}

type Report struct {
	canonical reportCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewReport(input ReportInput) (*Report, error) {
	if input.Result == nil || input.Policy == nil || input.OpenLotCount < 0 || !canonicalTime(input.EvaluationStart) || !canonicalTime(input.EvaluationEnd) ||
		!input.EvaluationStart.Before(input.EvaluationEnd) || input.Result.Mode() != strategycatalog.ExperimentPaperScored && input.Result.Mode() != strategycatalog.ExperimentPaperStress {
		return nil, fmt.Errorf("evaluation report identity is invalid")
	}
	observations, err := canonicalObservations(input.Observations, input.EvaluationStart, input.EvaluationEnd)
	if err != nil {
		return nil, err
	}
	if err := validateFrequency(input.Policy, observations); err != nil {
		return nil, err
	}
	trades, err := canonicalTrades(input.ClosedTrades, input.EvaluationStart, input.EvaluationEnd)
	if err != nil {
		return nil, err
	}
	metrics, execution, err := calculate(input.Policy, observations, trades, input.OpenLotCount, input.Execution, input.EvaluationStart, input.EvaluationEnd)
	if err != nil {
		return nil, err
	}
	canonical := reportCanonical{
		Schema: ReportSchemaV1, State: "completed", ResultID: input.Result.ID().String(), ResultSHA256: input.Result.Digest(),
		ExperimentID: input.Result.ExperimentID().String(), ProgramID: input.Result.ProgramID().String(), PlanID: input.Result.PlanID().String(),
		AccountID: input.Result.AccountID().String(), ManifestID: input.Result.ManifestID().String(), QualityResultID: input.Result.QualityResultID().String(),
		Mode: input.Result.Mode(), PolicyID: input.Policy.ID().String(), PolicySHA256: input.Policy.Digest(), EvaluationStart: formatTime(input.EvaluationStart),
		EvaluationEnd: formatTime(input.EvaluationEnd), OpenLotCount: input.OpenLotCount, Execution: execution, Observations: observations, ClosedTrades: trades, Metrics: metrics,
	}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Report{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("trade-portfolio-evaluation", ReportSchemaV1+"@sha256:"+digest)}, nil
}

func ReportFromCanonical(id uuid.UUID, digest string, raw []byte, result *experimentrun.Result, policy *Policy) (*Report, error) {
	if id == uuid.Nil || result == nil || policy == nil || hash(raw) != digest || !digestPattern.MatchString(digest) {
		return nil, fmt.Errorf("evaluation report envelope is invalid")
	}
	var canonical reportCanonical
	if err := decodeExact(raw, &canonical); err != nil {
		return nil, err
	}
	observations := make([]ObservationInput, len(canonical.Observations))
	for i, value := range canonical.Observations {
		observations[i] = observationInput(value)
	}
	trades := make([]ClosedTradeInput, len(canonical.ClosedTrades))
	for i, value := range canonical.ClosedTrades {
		trades[i] = tradeInput(value)
	}
	value, err := NewReport(ReportInput{
		Result: result, Policy: policy, EvaluationStart: parseTime(canonical.EvaluationStart), EvaluationEnd: parseTime(canonical.EvaluationEnd),
		OpenLotCount: canonical.OpenLotCount, Execution: canonical.Execution, Observations: observations, ClosedTrades: trades,
	})
	if err != nil || canonical.Schema != ReportSchemaV1 || canonical.State != "completed" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("evaluation report identity does not reconstruct")
	}
	return value, nil
}

func (r *Report) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.id
}

func (r *Report) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}

func (r *Report) CanonicalBytes() json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage(nil), r.bytes...)
}

func (r *Report) ResultID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.ResultID)
}

func (r *Report) ResultDigest() string {
	if r == nil {
		return ""
	}
	return r.canonical.ResultSHA256
}

func (r *Report) ExperimentID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.ExperimentID)
}

func (r *Report) ProgramID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.ProgramID)
}

func (r *Report) PlanID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.PlanID)
}

func (r *Report) AccountID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.AccountID)
}

func (r *Report) ManifestID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.ManifestID)
}

func (r *Report) QualityResultID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.QualityResultID)
}

func (r *Report) PolicyID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.PolicyID)
}

func (r *Report) PolicyDigest() string {
	if r == nil {
		return ""
	}
	return r.canonical.PolicySHA256
}

func (r *Report) EvaluationStart() time.Time {
	if r == nil {
		return time.Time{}
	}
	return parseTime(r.canonical.EvaluationStart)
}

func (r *Report) EvaluationEnd() time.Time {
	if r == nil {
		return time.Time{}
	}
	return parseTime(r.canonical.EvaluationEnd)
}

func (r *Report) OpenLotCount() int {
	if r == nil {
		return 0
	}
	return r.canonical.OpenLotCount
}

func (r *Report) Execution() ExecutionInput {
	if r == nil {
		return ExecutionInput{}
	}
	return r.canonical.Execution
}

func (r *Report) Mode() strategycatalog.ExperimentMode {
	if r == nil {
		return ""
	}
	return r.canonical.Mode
}

func (r *Report) Metrics() []Metric {
	if r == nil {
		return nil
	}
	return append([]Metric(nil), r.canonical.Metrics...)
}

func (r *Report) Observations() []ObservationInput {
	if r == nil {
		return nil
	}
	values := make([]ObservationInput, len(r.canonical.Observations))
	for i, v := range r.canonical.Observations {
		values[i] = observationInput(v)
	}
	return values
}

func (r *Report) ClosedTrades() []ClosedTradeInput {
	if r == nil {
		return nil
	}
	values := make([]ClosedTradeInput, len(r.canonical.ClosedTrades))
	for i, v := range r.canonical.ClosedTrades {
		values[i] = tradeInput(v)
	}
	return values
}

func canonicalObservations(values []ObservationInput, start, end time.Time) ([]observationCanonical, error) {
	if len(values) < 2 || len(values) > 100000 {
		return nil, fmt.Errorf("evaluation requires at least two bounded observations")
	}
	result := make([]observationCanonical, len(values))
	last := time.Time{}
	for i, value := range values {
		if !canonicalTime(value.ObservedAt) || value.ObservedAt.Before(start) || value.ObservedAt.After(end) || !last.IsZero() && !value.ObservedAt.After(last) ||
			!positive(value.Equity) || !positive(value.BenchmarkValue) || !signed(value.CashReturn) || !nonnegative(value.GrossExposure) || !signed(value.NetExposure) ||
			!ratio(value.LargestPositionWeight) || !nonnegative(value.CumulativeOwnershipCost) || !nonnegative(value.CumulativeTurnover) ||
			!nonnegative(value.CumulativeModeledSlippage) || value.CumulativeObservedSlippage != nil && !nonnegative(*value.CumulativeObservedSlippage) ||
			value.EvidenceID == uuid.Nil || !digestPattern.MatchString(value.EvidenceSHA256) {
			return nil, fmt.Errorf("evaluation observation %d is invalid", i)
		}
		if i > 0 && (decimal.RequireFromString(value.CumulativeOwnershipCost).LessThan(decimal.RequireFromString(values[i-1].CumulativeOwnershipCost)) ||
			decimal.RequireFromString(value.CumulativeTurnover).LessThan(decimal.RequireFromString(values[i-1].CumulativeTurnover)) ||
			decimal.RequireFromString(value.CumulativeModeledSlippage).LessThan(decimal.RequireFromString(values[i-1].CumulativeModeledSlippage)) ||
			value.CumulativeObservedSlippage != nil && values[i-1].CumulativeObservedSlippage != nil &&
				decimal.RequireFromString(*value.CumulativeObservedSlippage).LessThan(decimal.RequireFromString(*values[i-1].CumulativeObservedSlippage))) {
			return nil, fmt.Errorf("evaluation cumulative evidence decreases")
		}
		result[i] = observationCanonical{
			Sequence: i, ObservedAt: formatTime(value.ObservedAt), Equity: value.Equity, BenchmarkValue: value.BenchmarkValue,
			CashReturn: value.CashReturn, GrossExposure: value.GrossExposure, NetExposure: value.NetExposure, LargestPositionWeight: value.LargestPositionWeight,
			CumulativeOwnershipCost: value.CumulativeOwnershipCost, CumulativeTurnover: value.CumulativeTurnover, CumulativeModeledSlippage: value.CumulativeModeledSlippage,
			CumulativeObservedSlippage: cloneString(value.CumulativeObservedSlippage), EvidenceID: value.EvidenceID.String(), EvidenceSHA256: value.EvidenceSHA256,
		}
		last = value.ObservedAt
	}
	if !values[0].ObservedAt.Equal(start) || !values[len(values)-1].ObservedAt.Equal(end) {
		return nil, fmt.Errorf("evaluation observations do not span the declared window")
	}
	return result, nil
}

func canonicalTrades(values []ClosedTradeInput, start, end time.Time) ([]tradeCanonical, error) {
	result := make([]tradeCanonical, len(values))
	seen := map[uuid.UUID]struct{}{}
	for i, value := range values {
		if value.InstrumentID == uuid.Nil || !oneOf(value.Side, "long", "short") || !positive(value.Quantity) || !canonicalTime(value.EntryAt) || !canonicalTime(value.ExitAt) ||
			value.EntryAt.Before(start) || value.ExitAt.After(end) || value.ExitAt.Before(value.EntryAt) || !positive(value.EntryPrice) || !positive(value.ExitPrice) ||
			!nonnegative(value.EntryFees) || !nonnegative(value.ExitFees) || !nonnegative(value.OtherOwnershipCost) || !signed(value.GrossPnL) || !signed(value.AfterCostPnL) ||
			len(value.EntryFillIDs) == 0 || len(value.ExitFillIDs) == 0 {
			return nil, fmt.Errorf("evaluation closed trade %d is invalid", i)
		}
		for _, id := range append(append([]uuid.UUID(nil), value.EntryFillIDs...), value.ExitFillIDs...) {
			if id == uuid.Nil {
				return nil, fmt.Errorf("evaluation trade fill identity is invalid")
			}
			if _, ok := seen[id]; ok {
				return nil, fmt.Errorf("evaluation trade fill is reused")
			}
			seen[id] = struct{}{}
		}
		expectedAfter := decimal.RequireFromString(value.GrossPnL).Sub(decimal.RequireFromString(value.EntryFees)).Sub(decimal.RequireFromString(value.ExitFees)).Sub(decimal.RequireFromString(value.OtherOwnershipCost))
		if !expectedAfter.Equal(decimal.RequireFromString(value.AfterCostPnL)) {
			return nil, fmt.Errorf("evaluation trade after-cost pnl does not reconcile")
		}
		result[i] = tradeCanonical{
			Sequence: i, InstrumentID: value.InstrumentID.String(), Side: value.Side, Quantity: value.Quantity,
			EntryFillIDs: uuidTexts(value.EntryFillIDs), ExitFillIDs: uuidTexts(value.ExitFillIDs), EntryAt: formatTime(value.EntryAt), ExitAt: formatTime(value.ExitAt),
			EntryPrice: value.EntryPrice, ExitPrice: value.ExitPrice, EntryFees: value.EntryFees, ExitFees: value.ExitFees,
			OtherOwnershipCost: value.OtherOwnershipCost, GrossPnL: value.GrossPnL, AfterCostPnL: value.AfterCostPnL,
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].ExitAt < result[j].ExitAt || result[i].ExitAt == result[j].ExitAt && result[i].Sequence < result[j].Sequence
	})
	for i := range result {
		result[i].Sequence = i
	}
	return result, nil
}

func observationInput(v observationCanonical) ObservationInput {
	return ObservationInput{ObservedAt: parseTime(v.ObservedAt), Equity: v.Equity, BenchmarkValue: v.BenchmarkValue, CashReturn: v.CashReturn, GrossExposure: v.GrossExposure, NetExposure: v.NetExposure, LargestPositionWeight: v.LargestPositionWeight, CumulativeOwnershipCost: v.CumulativeOwnershipCost, CumulativeTurnover: v.CumulativeTurnover, CumulativeModeledSlippage: v.CumulativeModeledSlippage, CumulativeObservedSlippage: cloneString(v.CumulativeObservedSlippage), EvidenceID: uuid.MustParse(v.EvidenceID), EvidenceSHA256: v.EvidenceSHA256}
}

func tradeInput(v tradeCanonical) ClosedTradeInput {
	return ClosedTradeInput{InstrumentID: uuid.MustParse(v.InstrumentID), Side: v.Side, Quantity: v.Quantity, EntryFillIDs: parseUUIDs(v.EntryFillIDs), ExitFillIDs: parseUUIDs(v.ExitFillIDs), EntryAt: parseTime(v.EntryAt), ExitAt: parseTime(v.ExitAt), EntryPrice: v.EntryPrice, ExitPrice: v.ExitPrice, EntryFees: v.EntryFees, ExitFees: v.ExitFees, OtherOwnershipCost: v.OtherOwnershipCost, GrossPnL: v.GrossPnL, AfterCostPnL: v.AfterCostPnL}
}
func hash(v []byte) string { d := sha256.Sum256(v); return hex.EncodeToString(d[:]) }
func canonicalText(v string, maximum int) bool {
	return v != "" && v == strings.TrimSpace(v) && len(v) <= maximum
}

func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}

func canonicalTime(v time.Time) bool {
	return !v.IsZero() && v.Location() == time.UTC && v.Equal(v.Truncate(time.Microsecond))
}
func formatTime(v time.Time) string { return v.Format(timeLayout) }
func parseTime(v string) time.Time  { p, _ := time.Parse(timeLayout, v); return p }
func validDecimal(v string) bool {
	d, err := decimal.NewFromString(v)
	return err == nil && len(v) <= 128 && d.String() == v && d.Abs().LessThanOrEqual(decimal.New(1, 30))
}

func positive(v string) bool { return validDecimal(v) && decimal.RequireFromString(v).IsPositive() }

func nonnegative(v string) bool { return validDecimal(v) && !decimal.RequireFromString(v).IsNegative() }
func signed(v string) bool      { return validDecimal(v) }
func ratio(v string) bool {
	return nonnegative(v) && decimal.RequireFromString(v).LessThanOrEqual(decimal.NewFromInt(1))
}

func cloneString(v *string) *string {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

func uuidTexts(v []uuid.UUID) []string {
	r := make([]string, len(v))
	for i := range v {
		r[i] = v[i].String()
	}
	return r
}

func parseUUIDs(v []string) []uuid.UUID {
	r := make([]uuid.UUID, len(v))
	for i := range v {
		r[i] = uuid.MustParse(v[i])
	}
	return r
}

func decodeExact(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) == nil {
		return fmt.Errorf("extra json")
	}
	return nil
}

func validateFrequency(policy *Policy, observations []observationCanonical) error {
	for index := 1; index < len(observations); index++ {
		prior, current := parseTime(observations[index-1].ObservedAt), parseTime(observations[index].ObservedAt)
		valid := false
		switch policy.Frequency() {
		case "daily":
			valid = current.Equal(prior.Add(24 * time.Hour))
		case "weekly":
			valid = current.Equal(prior.Add(7 * 24 * time.Hour))
		case "monthly":
			valid = current.Equal(prior.AddDate(0, 1, 0))
		}
		if !valid {
			return fmt.Errorf("evaluation observation %d violates declared %s frequency", index, policy.Frequency())
		}
	}
	return nil
}
