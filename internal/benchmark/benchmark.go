// Package benchmark owns immutable passive-control declarations and
// opportunity-cost reports. It does not select, promote, schedule, or trade.
package benchmark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	DeclarationSchemaV1 = "passive-benchmark-declaration-v1"
	ReportSchemaV1      = "benchmark-opportunity-cost-report-v1"
	timeLayout          = "2006-01-02T15:04:05.000000Z"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ObservationInput struct {
	ObservedAt     time.Time
	Value          string
	CashReturn     string
	EvidenceID     uuid.UUID
	EvidenceSHA256 string
}

type DeclarationInput struct {
	Experiment            *strategycatalog.Experiment
	Manifest              *dataset.Manifest
	BenchmarkInstrumentID uuid.UUID
	BenchmarkKind         string
	Weighting             string
	DistributionTreatment string
	CashConvention        string
	Frequency             string
	InitialNotional       string
	DecimalScale          int
	Observations          []ObservationInput
}

type observationCanonical struct {
	Sequence       int    `json:"sequence"`
	ObservedAt     string `json:"observed_at"`
	Value          string `json:"value"`
	CashReturn     string `json:"cash_return"`
	EvidenceID     string `json:"evidence_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

type declarationCanonical struct {
	Schema                string                 `json:"schema"`
	State                 string                 `json:"state"`
	ExperimentID          string                 `json:"experiment_id"`
	ExperimentSHA256      string                 `json:"experiment_sha256"`
	ManifestID            string                 `json:"manifest_id"`
	ManifestSHA256        string                 `json:"manifest_sha256"`
	BenchmarkInstrumentID string                 `json:"benchmark_instrument_id"`
	BenchmarkKind         string                 `json:"benchmark_kind"`
	Weighting             string                 `json:"weighting"`
	DistributionTreatment string                 `json:"distribution_treatment"`
	CashConvention        string                 `json:"cash_convention"`
	Frequency             string                 `json:"frequency"`
	EvaluationStart       string                 `json:"evaluation_start"`
	EvaluationEnd         string                 `json:"evaluation_end"`
	InitialNotional       string                 `json:"initial_notional"`
	DecimalScale          int                    `json:"decimal_scale"`
	Observations          []observationCanonical `json:"observations"`
}

type Declaration struct {
	canonical declarationCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewDeclaration(input DeclarationInput) (*Declaration, error) {
	if input.Experiment == nil || input.Manifest == nil || input.BenchmarkInstrumentID == uuid.Nil ||
		input.Experiment.ManifestID() != input.Manifest.ID() || input.Experiment.State() != strategycatalog.ExperimentDeclared ||
		!oneOf(input.BenchmarkKind, "buy_and_hold", "total_return_index") || input.Weighting != "single_asset" ||
		input.DistributionTreatment != "reinvested" || input.CashConvention != "explicit_per_period" ||
		!oneOf(input.Frequency, "minute", "daily", "weekly", "monthly") || !positive(input.InitialNotional) ||
		input.DecimalScale < 6 || input.DecimalScale > 18 {
		return nil, fmt.Errorf("passive benchmark declaration is invalid")
	}
	observations, err := canonicalObservations(input.Observations, input.Experiment.EvaluationStart(), input.Experiment.EvaluationEnd(), input.Frequency)
	if err != nil {
		return nil, err
	}
	canonical := declarationCanonical{
		Schema: DeclarationSchemaV1, State: "declared", ExperimentID: input.Experiment.ID().String(), ExperimentSHA256: input.Experiment.Digest(),
		ManifestID: input.Manifest.ID().String(), ManifestSHA256: input.Manifest.Digest(), BenchmarkInstrumentID: input.BenchmarkInstrumentID.String(),
		BenchmarkKind: input.BenchmarkKind, Weighting: input.Weighting, DistributionTreatment: input.DistributionTreatment,
		CashConvention: input.CashConvention, Frequency: input.Frequency, EvaluationStart: formatTime(input.Experiment.EvaluationStart()),
		EvaluationEnd: formatTime(input.Experiment.EvaluationEnd()), InitialNotional: input.InitialNotional, DecimalScale: input.DecimalScale,
		Observations: observations,
	}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Declaration{canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID("passive-benchmark-declaration", DeclarationSchemaV1+"@sha256:"+digest)}, nil
}

func DeclarationFromCanonical(id uuid.UUID, digest string, raw []byte, experiment *strategycatalog.Experiment, manifest *dataset.Manifest) (*Declaration, error) {
	var canonical declarationCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("passive benchmark declaration envelope is invalid")
	}
	observations := make([]ObservationInput, len(canonical.Observations))
	for i, value := range canonical.Observations {
		evidenceID, err := uuid.Parse(value.EvidenceID)
		if err != nil {
			return nil, err
		}
		observations[i] = ObservationInput{ObservedAt: parseTime(value.ObservedAt), Value: value.Value, CashReturn: value.CashReturn, EvidenceID: evidenceID, EvidenceSHA256: value.EvidenceSHA256}
	}
	instrumentID, err := uuid.Parse(canonical.BenchmarkInstrumentID)
	if err != nil {
		return nil, err
	}
	value, err := NewDeclaration(DeclarationInput{Experiment: experiment, Manifest: manifest, BenchmarkInstrumentID: instrumentID,
		BenchmarkKind: canonical.BenchmarkKind, Weighting: canonical.Weighting, DistributionTreatment: canonical.DistributionTreatment,
		CashConvention: canonical.CashConvention, Frequency: canonical.Frequency, InitialNotional: canonical.InitialNotional,
		DecimalScale: canonical.DecimalScale, Observations: observations})
	if err != nil || canonical.Schema != DeclarationSchemaV1 || canonical.State != "declared" || value.ID() != id || value.Digest() != digest ||
		!bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("passive benchmark declaration identity does not reconstruct")
	}
	return value, nil
}

func (d *Declaration) ID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return d.id
}
func (d *Declaration) Digest() string {
	if d == nil {
		return ""
	}
	return d.digest
}
func (d *Declaration) CanonicalBytes() json.RawMessage {
	if d == nil {
		return nil
	}
	return append(json.RawMessage(nil), d.bytes...)
}
func (d *Declaration) ExperimentID() uuid.UUID {
	return canonicalUUID(d, func(v declarationCanonical) string { return v.ExperimentID })
}
func (d *Declaration) ExperimentDigest() string {
	if d == nil {
		return ""
	}
	return d.canonical.ExperimentSHA256
}
func (d *Declaration) ManifestID() uuid.UUID {
	return canonicalUUID(d, func(v declarationCanonical) string { return v.ManifestID })
}
func (d *Declaration) ManifestDigest() string {
	if d == nil {
		return ""
	}
	return d.canonical.ManifestSHA256
}
func (d *Declaration) BenchmarkInstrumentID() uuid.UUID {
	return canonicalUUID(d, func(v declarationCanonical) string { return v.BenchmarkInstrumentID })
}
func (d *Declaration) BenchmarkKind() string {
	if d == nil {
		return ""
	}
	return d.canonical.BenchmarkKind
}
func (d *Declaration) Weighting() string {
	if d == nil {
		return ""
	}
	return d.canonical.Weighting
}
func (d *Declaration) DistributionTreatment() string {
	if d == nil {
		return ""
	}
	return d.canonical.DistributionTreatment
}
func (d *Declaration) CashConvention() string {
	if d == nil {
		return ""
	}
	return d.canonical.CashConvention
}
func (d *Declaration) Frequency() string {
	if d == nil {
		return ""
	}
	return d.canonical.Frequency
}
func (d *Declaration) EvaluationStart() time.Time {
	if d == nil {
		return time.Time{}
	}
	return parseTime(d.canonical.EvaluationStart)
}
func (d *Declaration) EvaluationEnd() time.Time {
	if d == nil {
		return time.Time{}
	}
	return parseTime(d.canonical.EvaluationEnd)
}
func (d *Declaration) InitialNotional() string {
	if d == nil {
		return ""
	}
	return d.canonical.InitialNotional
}
func (d *Declaration) DecimalScale() int {
	if d == nil {
		return 0
	}
	return d.canonical.DecimalScale
}
func (d *Declaration) Observations() []ObservationInput {
	if d == nil {
		return nil
	}
	result := make([]ObservationInput, len(d.canonical.Observations))
	for i, value := range d.canonical.Observations {
		result[i] = ObservationInput{ObservedAt: parseTime(value.ObservedAt), Value: value.Value, CashReturn: value.CashReturn,
			EvidenceID: uuid.MustParse(value.EvidenceID), EvidenceSHA256: value.EvidenceSHA256}
	}
	return result
}

type reportCanonical struct {
	Schema                    string `json:"schema"`
	State                     string `json:"state"`
	DeclarationID             string `json:"declaration_id"`
	DeclarationSHA256         string `json:"declaration_sha256"`
	EvaluationID              string `json:"evaluation_id"`
	EvaluationSHA256          string `json:"evaluation_sha256"`
	ExperimentID              string `json:"experiment_id"`
	ManifestID                string `json:"manifest_id"`
	BenchmarkInstrumentID     string `json:"benchmark_instrument_id"`
	StrategyTotalReturn       string `json:"strategy_total_return"`
	BenchmarkTotalReturn      string `json:"benchmark_total_return"`
	CashTotalReturn           string `json:"cash_total_return"`
	BenchmarkOpportunityCost  string `json:"benchmark_opportunity_cost"`
	CashOpportunityCost       string `json:"cash_opportunity_cost"`
	StrategyTerminalWealth    string `json:"strategy_terminal_wealth"`
	BenchmarkTerminalWealth   string `json:"benchmark_terminal_wealth"`
	CashTerminalWealth        string `json:"cash_terminal_wealth"`
	BenchmarkWealthDifference string `json:"benchmark_wealth_difference"`
	CashWealthDifference      string `json:"cash_wealth_difference"`
	ObservationCount          int    `json:"observation_count"`
}

type Report struct {
	canonical reportCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewReport(declaration *Declaration, evaluationReport *evaluation.Report) (*Report, error) {
	if declaration == nil || evaluationReport == nil || declaration.ExperimentID() != evaluationReport.ExperimentID() ||
		declaration.ManifestID() != evaluationReport.ManifestID() || !declaration.EvaluationStart().Equal(evaluationReport.EvaluationStart()) ||
		!declaration.EvaluationEnd().Equal(evaluationReport.EvaluationEnd()) {
		return nil, fmt.Errorf("benchmark opportunity-cost parents do not match")
	}
	declared, observed := declaration.Observations(), evaluationReport.Observations()
	if len(declared) != len(observed) {
		return nil, fmt.Errorf("benchmark opportunity-cost curve length does not match")
	}
	for i := range declared {
		if !declared[i].ObservedAt.Equal(observed[i].ObservedAt) || declared[i].Value != observed[i].BenchmarkValue || declared[i].CashReturn != observed[i].CashReturn ||
			declared[i].EvidenceID != observed[i].EvidenceID || declared[i].EvidenceSHA256 != observed[i].EvidenceSHA256 {
			return nil, fmt.Errorf("benchmark opportunity-cost observation %d does not match declaration", i)
		}
	}
	strategy := decimal.RequireFromString(observed[len(observed)-1].Equity).Div(decimal.RequireFromString(observed[0].Equity)).Sub(decimal.NewFromInt(1))
	benchmarkReturn := decimal.RequireFromString(declared[len(declared)-1].Value).Div(decimal.RequireFromString(declared[0].Value)).Sub(decimal.NewFromInt(1))
	cashGrowth := decimal.NewFromInt(1)
	for i := 1; i < len(declared); i++ {
		cashGrowth = cashGrowth.Mul(decimal.NewFromInt(1).Add(decimal.RequireFromString(declared[i].CashReturn)))
	}
	cashReturn := cashGrowth.Sub(decimal.NewFromInt(1))
	notional := decimal.RequireFromString(declaration.InitialNotional())
	strategyWealth := notional.Mul(decimal.NewFromInt(1).Add(strategy))
	benchmarkWealth := notional.Mul(decimal.NewFromInt(1).Add(benchmarkReturn))
	cashWealth := notional.Mul(cashGrowth)
	q := func(value decimal.Decimal) string {
		return value.RoundBank(int32(declaration.DecimalScale())).StringFixed(int32(declaration.DecimalScale()))
	}
	canonical := reportCanonical{Schema: ReportSchemaV1, State: "completed", DeclarationID: declaration.ID().String(), DeclarationSHA256: declaration.Digest(),
		EvaluationID: evaluationReport.ID().String(), EvaluationSHA256: evaluationReport.Digest(), ExperimentID: declaration.ExperimentID().String(),
		ManifestID: declaration.ManifestID().String(), BenchmarkInstrumentID: declaration.BenchmarkInstrumentID().String(),
		StrategyTotalReturn: q(strategy), BenchmarkTotalReturn: q(benchmarkReturn), CashTotalReturn: q(cashReturn),
		BenchmarkOpportunityCost: q(benchmarkReturn.Sub(strategy)), CashOpportunityCost: q(cashReturn.Sub(strategy)),
		StrategyTerminalWealth: q(strategyWealth), BenchmarkTerminalWealth: q(benchmarkWealth), CashTerminalWealth: q(cashWealth),
		BenchmarkWealthDifference: q(benchmarkWealth.Sub(strategyWealth)), CashWealthDifference: q(cashWealth.Sub(strategyWealth)), ObservationCount: len(declared)}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Report{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("benchmark-opportunity-cost-report", ReportSchemaV1+"@sha256:"+digest)}, nil
}

func ReportFromCanonical(id uuid.UUID, digest string, raw []byte, declaration *Declaration, evaluationReport *evaluation.Report) (*Report, error) {
	var canonical reportCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("benchmark opportunity-cost envelope is invalid")
	}
	value, err := NewReport(declaration, evaluationReport)
	if err != nil || canonical.Schema != ReportSchemaV1 || canonical.State != "completed" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("benchmark opportunity-cost identity does not reconstruct")
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
func (r *Report) DeclarationID() uuid.UUID {
	return reportUUID(r, func(v reportCanonical) string { return v.DeclarationID })
}
func (r *Report) DeclarationDigest() string {
	if r == nil {
		return ""
	}
	return r.canonical.DeclarationSHA256
}
func (r *Report) EvaluationID() uuid.UUID {
	return reportUUID(r, func(v reportCanonical) string { return v.EvaluationID })
}
func (r *Report) EvaluationDigest() string {
	if r == nil {
		return ""
	}
	return r.canonical.EvaluationSHA256
}
func (r *Report) ExperimentID() uuid.UUID {
	return reportUUID(r, func(v reportCanonical) string { return v.ExperimentID })
}
func (r *Report) ManifestID() uuid.UUID {
	return reportUUID(r, func(v reportCanonical) string { return v.ManifestID })
}
func (r *Report) BenchmarkInstrumentID() uuid.UUID {
	return reportUUID(r, func(v reportCanonical) string { return v.BenchmarkInstrumentID })
}
func (r *Report) StrategyTotalReturn() string {
	if r == nil {
		return ""
	}
	return r.canonical.StrategyTotalReturn
}
func (r *Report) BenchmarkTotalReturn() string {
	if r == nil {
		return ""
	}
	return r.canonical.BenchmarkTotalReturn
}
func (r *Report) CashTotalReturn() string {
	if r == nil {
		return ""
	}
	return r.canonical.CashTotalReturn
}
func (r *Report) BenchmarkOpportunityCost() string {
	if r == nil {
		return ""
	}
	return r.canonical.BenchmarkOpportunityCost
}
func (r *Report) CashOpportunityCost() string {
	if r == nil {
		return ""
	}
	return r.canonical.CashOpportunityCost
}
func (r *Report) StrategyTerminalWealth() string {
	if r == nil {
		return ""
	}
	return r.canonical.StrategyTerminalWealth
}
func (r *Report) BenchmarkTerminalWealth() string {
	if r == nil {
		return ""
	}
	return r.canonical.BenchmarkTerminalWealth
}
func (r *Report) CashTerminalWealth() string {
	if r == nil {
		return ""
	}
	return r.canonical.CashTerminalWealth
}
func (r *Report) BenchmarkWealthDifference() string {
	if r == nil {
		return ""
	}
	return r.canonical.BenchmarkWealthDifference
}
func (r *Report) CashWealthDifference() string {
	if r == nil {
		return ""
	}
	return r.canonical.CashWealthDifference
}
func (r *Report) ObservationCount() int {
	if r == nil {
		return 0
	}
	return r.canonical.ObservationCount
}

func canonicalObservations(values []ObservationInput, start, end time.Time, frequency string) ([]observationCanonical, error) {
	if len(values) < 2 || len(values) > 100000 {
		return nil, fmt.Errorf("passive benchmark requires at least two bounded observations")
	}
	result := make([]observationCanonical, len(values))
	for i, value := range values {
		if !canonicalTime(value.ObservedAt) || value.ObservedAt.Before(start) || value.ObservedAt.After(end) || !positive(value.Value) || !signed(value.CashReturn) ||
			decimal.RequireFromString(value.CashReturn).LessThanOrEqual(decimal.NewFromInt(-1)) || value.EvidenceID == uuid.Nil || !digestPattern.MatchString(value.EvidenceSHA256) {
			return nil, fmt.Errorf("passive benchmark observation %d is invalid", i)
		}
		if i > 0 && !nextTime(values[i-1].ObservedAt, value.ObservedAt, frequency) {
			return nil, fmt.Errorf("passive benchmark observation %d violates declared frequency", i)
		}
		result[i] = observationCanonical{Sequence: i, ObservedAt: formatTime(value.ObservedAt), Value: value.Value, CashReturn: value.CashReturn, EvidenceID: value.EvidenceID.String(), EvidenceSHA256: value.EvidenceSHA256}
	}
	if !values[0].ObservedAt.Equal(start) || !values[len(values)-1].ObservedAt.Equal(end) {
		return nil, fmt.Errorf("passive benchmark observations do not span experiment window")
	}
	return result, nil
}

func nextTime(prior, current time.Time, frequency string) bool {
	switch frequency {
	case "minute":
		return current.Equal(prior.Add(time.Minute))
	case "daily":
		return current.Equal(prior.Add(24 * time.Hour))
	case "weekly":
		return current.Equal(prior.Add(7 * 24 * time.Hour))
	case "monthly":
		return current.Equal(prior.AddDate(0, 1, 0))
	}
	return false
}
func canonicalUUID(d *Declaration, selectValue func(declarationCanonical) string) uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(selectValue(d.canonical))
	return id
}
func reportUUID(r *Report, selectValue func(reportCanonical) string) uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(selectValue(r.canonical))
	return id
}
func validDecimal(value string) bool {
	d, err := decimal.NewFromString(value)
	return err == nil && value == d.String() && len(value) <= 128 && d.Abs().LessThanOrEqual(decimal.New(1, 30))
}
func positive(value string) bool {
	return validDecimal(value) && decimal.RequireFromString(value).IsPositive()
}
func signed(value string) bool { return validDecimal(value) }
func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Equal(value.Truncate(time.Microsecond))
}
func formatTime(value time.Time) string { return value.Format(timeLayout) }
func parseTime(value string) time.Time  { parsed, _ := time.Parse(timeLayout, value); return parsed }
func hash(value []byte) string          { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) == nil {
		return fmt.Errorf("extra json")
	}
	return nil
}
