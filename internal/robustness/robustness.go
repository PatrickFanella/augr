// Package robustness owns immutable walk-forward and statistical gate evidence.
// It does not select, promote, retire, schedule, deploy, or allocate candidates.
package robustness

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
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	PolicySchemaV1     = "robustness-policy-v1"
	FamilySchemaV1     = "robustness-search-family-v1"
	AssessmentSchemaV1 = "statistical-robustness-assessment-v1"
	GatePass           = "pass"
	GateFail           = "fail"
	timeLayout         = "2006-01-02T15:04:05.000000Z"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type PolicyInput struct {
	Version                    string
	FoldCount                  int
	PurgeSeconds               int64
	EmbargoSeconds             int64
	BootstrapAlgorithm         string
	BootstrapSeed              uint64
	BootstrapIterations        int
	ConfidenceLevel            string
	FamilyWiseAlpha            string
	MaxLargestPositiveShare    string
	MaxTopDecilePositiveShare  string
	MaxPerturbationDegradation string
	RequiredPerturbations      []string
	DecimalScale               int
}

type policyCanonical struct {
	Schema                     string   `json:"schema"`
	Version                    string   `json:"version"`
	FoldCount                  int      `json:"fold_count"`
	PurgeSeconds               int64    `json:"purge_seconds"`
	EmbargoSeconds             int64    `json:"embargo_seconds"`
	BootstrapAlgorithm         string   `json:"bootstrap_algorithm"`
	BootstrapSeed              uint64   `json:"bootstrap_seed"`
	BootstrapIterations        int      `json:"bootstrap_iterations"`
	ConfidenceLevel            string   `json:"confidence_level"`
	FamilyWiseAlpha            string   `json:"family_wise_alpha"`
	MultipleTestingCorrection  string   `json:"multiple_testing_correction"`
	MaxLargestPositiveShare    string   `json:"max_largest_positive_share"`
	MaxTopDecilePositiveShare  string   `json:"max_top_decile_positive_share"`
	MaxPerturbationDegradation string   `json:"max_perturbation_degradation"`
	RequiredPerturbations      []string `json:"required_perturbations"`
	DecimalScale               int      `json:"decimal_scale"`
}

type Policy struct {
	canonical policyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	perturbations := append([]string(nil), input.RequiredPerturbations...)
	sort.Strings(perturbations)
	if !canonicalText(input.Version, 128) || input.FoldCount < 2 || input.FoldCount > 1000 || input.PurgeSeconds < 0 || input.EmbargoSeconds < 0 ||
		input.BootstrapAlgorithm != "xorshift64star-iid-percentile-v1" || input.BootstrapIterations < 100 || input.BootstrapIterations > 100000 ||
		!probability(input.ConfidenceLevel) || decimal.RequireFromString(input.ConfidenceLevel).LessThan(decimal.RequireFromString("0.5")) ||
		!probability(input.FamilyWiseAlpha) || decimal.RequireFromString(input.FamilyWiseAlpha).IsZero() ||
		!probability(input.MaxLargestPositiveShare) || !probability(input.MaxTopDecilePositiveShare) ||
		!nonnegative(input.MaxPerturbationDegradation) || input.DecimalScale < 6 || input.DecimalScale > 18 || len(perturbations) == 0 || len(perturbations) > 64 {
		return nil, fmt.Errorf("robustness policy is invalid")
	}
	for index, value := range perturbations {
		if !canonicalToken(value) || index > 0 && value == perturbations[index-1] {
			return nil, fmt.Errorf("required perturbations are invalid")
		}
	}
	canonical := policyCanonical{
		Schema: PolicySchemaV1, Version: input.Version, FoldCount: input.FoldCount, PurgeSeconds: input.PurgeSeconds,
		EmbargoSeconds: input.EmbargoSeconds, BootstrapAlgorithm: input.BootstrapAlgorithm, BootstrapSeed: input.BootstrapSeed,
		BootstrapIterations: input.BootstrapIterations, ConfidenceLevel: input.ConfidenceLevel, FamilyWiseAlpha: input.FamilyWiseAlpha,
		MultipleTestingCorrection: "holm_bonferroni", MaxLargestPositiveShare: input.MaxLargestPositiveShare,
		MaxTopDecilePositiveShare: input.MaxTopDecilePositiveShare, MaxPerturbationDegradation: input.MaxPerturbationDegradation,
		RequiredPerturbations: perturbations, DecimalScale: input.DecimalScale,
	}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Policy{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID("robustness-policy", PolicySchemaV1+"@sha256:"+digest),
	}, nil
}

func PolicyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Policy, error) {
	var canonical policyCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("robustness policy envelope is invalid")
	}
	value, err := NewPolicy(PolicyInput{
		Version: canonical.Version, FoldCount: canonical.FoldCount, PurgeSeconds: canonical.PurgeSeconds,
		EmbargoSeconds: canonical.EmbargoSeconds, BootstrapAlgorithm: canonical.BootstrapAlgorithm, BootstrapSeed: canonical.BootstrapSeed,
		BootstrapIterations: canonical.BootstrapIterations, ConfidenceLevel: canonical.ConfidenceLevel, FamilyWiseAlpha: canonical.FamilyWiseAlpha,
		MaxLargestPositiveShare: canonical.MaxLargestPositiveShare, MaxTopDecilePositiveShare: canonical.MaxTopDecilePositiveShare,
		MaxPerturbationDegradation: canonical.MaxPerturbationDegradation, RequiredPerturbations: canonical.RequiredPerturbations,
		DecimalScale: canonical.DecimalScale,
	})
	if err != nil || canonical.Schema != PolicySchemaV1 || canonical.MultipleTestingCorrection != "holm_bonferroni" || value.ID() != id ||
		value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("robustness policy identity does not reconstruct")
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

func (p *Policy) FoldCount() int {
	if p == nil {
		return 0
	}
	return p.canonical.FoldCount
}

func (p *Policy) DecimalScale() int {
	if p == nil {
		return 0
	}
	return p.canonical.DecimalScale
}

func (p *Policy) RequiredPerturbations() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.canonical.RequiredPerturbations...)
}

func (p *Policy) Version() string {
	if p == nil {
		return ""
	}
	return p.canonical.Version
}

func (p *Policy) PurgeSeconds() int64 {
	if p == nil {
		return 0
	}
	return p.canonical.PurgeSeconds
}

func (p *Policy) EmbargoSeconds() int64 {
	if p == nil {
		return 0
	}
	return p.canonical.EmbargoSeconds
}

func (p *Policy) BootstrapAlgorithm() string {
	if p == nil {
		return ""
	}
	return p.canonical.BootstrapAlgorithm
}

func (p *Policy) BootstrapSeed() uint64 {
	if p == nil {
		return 0
	}
	return p.canonical.BootstrapSeed
}

func (p *Policy) BootstrapIterations() int {
	if p == nil {
		return 0
	}
	return p.canonical.BootstrapIterations
}

func (p *Policy) ConfidenceLevel() string {
	if p == nil {
		return ""
	}
	return p.canonical.ConfidenceLevel
}

func (p *Policy) FamilyWiseAlpha() string {
	if p == nil {
		return ""
	}
	return p.canonical.FamilyWiseAlpha
}

func (p *Policy) MaxLargestPositiveShare() string {
	if p == nil {
		return ""
	}
	return p.canonical.MaxLargestPositiveShare
}

func (p *Policy) MaxTopDecilePositiveShare() string {
	if p == nil {
		return ""
	}
	return p.canonical.MaxTopDecilePositiveShare
}

func (p *Policy) MaxPerturbationDegradation() string {
	if p == nil {
		return ""
	}
	return p.canonical.MaxPerturbationDegradation
}

type FamilyInput struct {
	Name                string
	HypothesisSHA256    string
	CandidateVersionIDs []uuid.UUID
}

type familyCanonical struct {
	Schema              string   `json:"schema"`
	Name                string   `json:"name"`
	HypothesisSHA256    string   `json:"hypothesis_sha256"`
	CandidateVersionIDs []string `json:"candidate_version_ids"`
}

type Family struct {
	canonical familyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewFamily(input FamilyInput) (*Family, error) {
	ids := append([]uuid.UUID(nil), input.CandidateVersionIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	if !canonicalText(input.Name, 256) || !digestPattern.MatchString(input.HypothesisSHA256) || len(ids) == 0 || len(ids) > 10000 {
		return nil, fmt.Errorf("robustness search family is invalid")
	}
	texts := make([]string, len(ids))
	for index, id := range ids {
		if id == uuid.Nil || index > 0 && id == ids[index-1] {
			return nil, fmt.Errorf("robustness search family candidates are invalid")
		}
		texts[index] = id.String()
	}
	canonical := familyCanonical{Schema: FamilySchemaV1, Name: input.Name, HypothesisSHA256: input.HypothesisSHA256, CandidateVersionIDs: texts}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Family{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID("robustness-search-family", FamilySchemaV1+"@sha256:"+digest),
	}, nil
}

func FamilyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Family, error) {
	var canonical familyCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("robustness search family envelope is invalid")
	}
	ids := make([]uuid.UUID, len(canonical.CandidateVersionIDs))
	for index, value := range canonical.CandidateVersionIDs {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("robustness search family candidate identity is invalid")
		}
		ids[index] = parsed
	}
	value, err := NewFamily(FamilyInput{Name: canonical.Name, HypothesisSHA256: canonical.HypothesisSHA256, CandidateVersionIDs: ids})
	if err != nil || canonical.Schema != FamilySchemaV1 || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("robustness search family identity does not reconstruct")
	}
	return value, nil
}

func (f *Family) ID() uuid.UUID {
	if f == nil {
		return uuid.Nil
	}
	return f.id
}

func (f *Family) Digest() string {
	if f == nil {
		return ""
	}
	return f.digest
}

func (f *Family) CanonicalBytes() json.RawMessage {
	if f == nil {
		return nil
	}
	return append(json.RawMessage(nil), f.bytes...)
}

func (f *Family) CandidateVersionIDs() []uuid.UUID {
	if f == nil {
		return nil
	}
	result := make([]uuid.UUID, len(f.canonical.CandidateVersionIDs))
	for index, value := range f.canonical.CandidateVersionIDs {
		result[index] = uuid.MustParse(value)
	}
	return result
}

func (f *Family) Name() string {
	if f == nil {
		return ""
	}
	return f.canonical.Name
}

func (f *Family) HypothesisSHA256() string {
	if f == nil {
		return ""
	}
	return f.canonical.HypothesisSHA256
}

type ScenarioInput struct {
	Kind     string
	Severity string
	Report   *evaluation.Report
}

type FoldInput struct {
	TrainStart    time.Time
	TrainEnd      time.Time
	Baseline      *evaluation.Report
	Perturbations []ScenarioInput
}

type CandidateInput struct {
	VersionID uuid.UUID
	Folds     []FoldInput
}

type AssessmentInput struct {
	Family     *Family
	Policy     *Policy
	Mode       strategycatalog.ExperimentMode
	Candidates []CandidateInput
}

type ScenarioEvidence struct {
	Kind         string `json:"kind"`
	Severity     string `json:"severity"`
	ReportID     string `json:"report_id"`
	ReportSHA256 string `json:"report_sha256"`
}

type FoldEvidence struct {
	Sequence      int                `json:"sequence"`
	TrainStart    string             `json:"train_start"`
	TrainEnd      string             `json:"train_end"`
	TestStart     string             `json:"test_start"`
	TestEnd       string             `json:"test_end"`
	Baseline      ScenarioEvidence   `json:"baseline"`
	Perturbations []ScenarioEvidence `json:"perturbations"`
}

type Statistic struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
}

type Gate struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Threshold   string `json:"threshold"`
	Observed    string `json:"observed"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
}

type CandidateEvidence struct {
	Sequence   int            `json:"sequence"`
	VersionID  string         `json:"version_id"`
	Folds      []FoldEvidence `json:"folds"`
	Statistics []Statistic    `json:"statistics"`
	Gates      []Gate         `json:"gates"`
}

type assessmentCanonical struct {
	Schema       string                         `json:"schema"`
	State        string                         `json:"state"`
	FamilyID     string                         `json:"family_id"`
	FamilySHA256 string                         `json:"family_sha256"`
	PolicyID     string                         `json:"policy_id"`
	PolicySHA256 string                         `json:"policy_sha256"`
	Mode         strategycatalog.ExperimentMode `json:"mode"`
	Candidates   []CandidateEvidence            `json:"candidates"`
}

type Assessment struct {
	canonical assessmentCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewAssessment(input AssessmentInput) (*Assessment, error) {
	if input.Family == nil || input.Policy == nil || input.Mode != strategycatalog.ExperimentPaperScored && input.Mode != strategycatalog.ExperimentPaperStress {
		return nil, fmt.Errorf("robustness assessment identity is invalid")
	}
	candidates, sources, err := canonicalCandidates(input)
	if err != nil {
		return nil, err
	}
	if err := calculate(input.Policy, candidates, sources); err != nil {
		return nil, err
	}
	canonical := assessmentCanonical{
		Schema: AssessmentSchemaV1, State: "completed", FamilyID: input.Family.ID().String(),
		FamilySHA256: input.Family.Digest(), PolicyID: input.Policy.ID().String(), PolicySHA256: input.Policy.Digest(), Mode: input.Mode, Candidates: candidates,
	}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Assessment{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID("statistical-robustness-assessment", AssessmentSchemaV1+"@sha256:"+digest),
	}, nil
}

func AssessmentFromCanonical(id uuid.UUID, digest string, raw []byte, family *Family, policy *Policy, reports map[uuid.UUID]*evaluation.Report) (*Assessment, error) {
	if id == uuid.Nil || family == nil || policy == nil || !digestPattern.MatchString(digest) || hash(raw) != digest {
		return nil, fmt.Errorf("robustness assessment envelope is invalid")
	}
	var canonical assessmentCanonical
	if err := decodeExact(raw, &canonical); err != nil {
		return nil, err
	}
	candidates := make([]CandidateInput, len(canonical.Candidates))
	for candidateIndex, candidate := range canonical.Candidates {
		versionID, err := uuid.Parse(candidate.VersionID)
		if err != nil {
			return nil, fmt.Errorf("robustness candidate identity is invalid")
		}
		folds := make([]FoldInput, len(candidate.Folds))
		for foldIndex, fold := range candidate.Folds {
			baselineID, err := uuid.Parse(fold.Baseline.ReportID)
			if err != nil || reports[baselineID] == nil {
				return nil, fmt.Errorf("robustness baseline report is missing")
			}
			perturbations := make([]ScenarioInput, len(fold.Perturbations))
			for scenarioIndex, scenario := range fold.Perturbations {
				reportID, err := uuid.Parse(scenario.ReportID)
				if err != nil || reports[reportID] == nil {
					return nil, fmt.Errorf("robustness perturbation report is missing")
				}
				perturbations[scenarioIndex] = ScenarioInput{Kind: scenario.Kind, Severity: scenario.Severity, Report: reports[reportID]}
			}
			folds[foldIndex] = FoldInput{TrainStart: parseTime(fold.TrainStart), TrainEnd: parseTime(fold.TrainEnd), Baseline: reports[baselineID], Perturbations: perturbations}
		}
		candidates[candidateIndex] = CandidateInput{VersionID: versionID, Folds: folds}
	}
	value, err := NewAssessment(AssessmentInput{Family: family, Policy: policy, Mode: canonical.Mode, Candidates: candidates})
	if err != nil || canonical.Schema != AssessmentSchemaV1 || canonical.State != "completed" || canonical.FamilyID != family.ID().String() ||
		canonical.FamilySHA256 != family.Digest() || canonical.PolicyID != policy.ID().String() || canonical.PolicySHA256 != policy.Digest() ||
		value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("robustness assessment identity does not reconstruct")
	}
	return value, nil
}

type reportSources map[uuid.UUID]*evaluation.Report

func canonicalCandidates(input AssessmentInput) ([]CandidateEvidence, reportSources, error) {
	familyIDs := input.Family.CandidateVersionIDs()
	if len(input.Candidates) != len(familyIDs) {
		return nil, nil, fmt.Errorf("assessment candidate population is incomplete")
	}
	values := append([]CandidateInput(nil), input.Candidates...)
	sort.Slice(values, func(i, j int) bool { return values[i].VersionID.String() < values[j].VersionID.String() })
	result := make([]CandidateEvidence, len(values))
	sources := reportSources{}
	for candidateIndex, candidate := range values {
		if candidate.VersionID != familyIDs[candidateIndex] || len(candidate.Folds) != input.Policy.FoldCount() {
			return nil, nil, fmt.Errorf("assessment candidate does not match search family or fold policy")
		}
		folds := append([]FoldInput(nil), candidate.Folds...)
		for _, fold := range folds {
			if fold.Baseline == nil {
				return nil, nil, fmt.Errorf("assessment fold baseline is required")
			}
		}
		sort.Slice(folds, func(i, j int) bool {
			return folds[i].Baseline.EvaluationStart().Before(folds[j].Baseline.EvaluationStart())
		})
		canonicalFolds := make([]FoldEvidence, len(folds))
		for foldIndex, fold := range folds {
			if fold.Baseline == nil || !canonicalTime(fold.TrainStart) || !canonicalTime(fold.TrainEnd) || !fold.TrainStart.Before(fold.TrainEnd) ||
				fold.Baseline.Mode() != input.Mode || fold.TrainEnd.Add(time.Duration(input.Policy.canonical.PurgeSeconds)*time.Second).After(fold.Baseline.EvaluationStart()) {
				return nil, nil, fmt.Errorf("assessment fold %d is invalid", foldIndex)
			}
			if foldIndex > 0 && (folds[foldIndex-1].Baseline.EvaluationEnd().After(fold.Baseline.EvaluationStart()) ||
				folds[foldIndex-1].Baseline.EvaluationEnd().Add(time.Duration(input.Policy.canonical.EmbargoSeconds)*time.Second).After(fold.TrainStart)) {
				return nil, nil, fmt.Errorf("assessment fold embargo or test ordering is invalid")
			}
			baseline := ScenarioEvidence{Kind: "baseline", Severity: "none", ReportID: fold.Baseline.ID().String(), ReportSHA256: fold.Baseline.Digest()}
			if prior, ok := sources[fold.Baseline.ID()]; ok && prior.Digest() != fold.Baseline.Digest() {
				return nil, nil, fmt.Errorf("report identity conflict")
			}
			sources[fold.Baseline.ID()] = fold.Baseline
			perturbations := append([]ScenarioInput(nil), fold.Perturbations...)
			sort.Slice(perturbations, func(i, j int) bool { return perturbations[i].Kind < perturbations[j].Kind })
			required := input.Policy.RequiredPerturbations()
			if len(perturbations) != len(required) {
				return nil, nil, fmt.Errorf("assessment fold perturbations are incomplete")
			}
			canonicalPerturbations := make([]ScenarioEvidence, len(perturbations))
			for index, scenario := range perturbations {
				if scenario.Kind != required[index] || !canonicalText(scenario.Severity, 128) || scenario.Report == nil || scenario.Report.Mode() != input.Mode ||
					scenario.Report.EvaluationStart() != fold.Baseline.EvaluationStart() || scenario.Report.EvaluationEnd() != fold.Baseline.EvaluationEnd() ||
					scenario.Report.PolicyID() != fold.Baseline.PolicyID() {
					return nil, nil, fmt.Errorf("assessment perturbation %s is invalid", scenario.Kind)
				}
				canonicalPerturbations[index] = ScenarioEvidence{
					Kind: scenario.Kind, Severity: scenario.Severity,
					ReportID: scenario.Report.ID().String(), ReportSHA256: scenario.Report.Digest(),
				}
				sources[scenario.Report.ID()] = scenario.Report
			}
			canonicalFolds[foldIndex] = FoldEvidence{
				Sequence: foldIndex, TrainStart: formatTime(fold.TrainStart), TrainEnd: formatTime(fold.TrainEnd),
				TestStart: formatTime(fold.Baseline.EvaluationStart()), TestEnd: formatTime(fold.Baseline.EvaluationEnd()), Baseline: baseline,
				Perturbations: canonicalPerturbations,
			}
		}
		result[candidateIndex] = CandidateEvidence{Sequence: candidateIndex, VersionID: candidate.VersionID.String(), Folds: canonicalFolds}
	}
	return result, sources, nil
}

func (a *Assessment) ID() uuid.UUID {
	if a == nil {
		return uuid.Nil
	}
	return a.id
}

func (a *Assessment) Digest() string {
	if a == nil {
		return ""
	}
	return a.digest
}

func (a *Assessment) CanonicalBytes() json.RawMessage {
	if a == nil {
		return nil
	}
	return append(json.RawMessage(nil), a.bytes...)
}

func (a *Assessment) FamilyID() uuid.UUID {
	if a == nil {
		return uuid.Nil
	}
	return uuid.MustParse(a.canonical.FamilyID)
}

func (a *Assessment) PolicyID() uuid.UUID {
	if a == nil {
		return uuid.Nil
	}
	return uuid.MustParse(a.canonical.PolicyID)
}

func (a *Assessment) FamilyDigest() string {
	if a == nil {
		return ""
	}
	return a.canonical.FamilySHA256
}

func (a *Assessment) PolicyDigest() string {
	if a == nil {
		return ""
	}
	return a.canonical.PolicySHA256
}

func (a *Assessment) Mode() strategycatalog.ExperimentMode {
	if a == nil {
		return ""
	}
	return a.canonical.Mode
}

func (a *Assessment) Candidates() []CandidateEvidence {
	if a == nil {
		return nil
	}
	result := make([]CandidateEvidence, len(a.canonical.Candidates))
	for index, value := range a.canonical.Candidates {
		result[index] = value
		result[index].Folds = append([]FoldEvidence(nil), value.Folds...)
		for foldIndex := range result[index].Folds {
			result[index].Folds[foldIndex].Perturbations = append([]ScenarioEvidence(nil), value.Folds[foldIndex].Perturbations...)
		}
		result[index].Statistics = append([]Statistic(nil), value.Statistics...)
		result[index].Gates = append([]Gate(nil), value.Gates...)
	}
	return result
}

func hash(value []byte) string { digest := sha256.Sum256(value); return hex.EncodeToString(digest[:]) }

func canonicalText(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= maximum
}

func canonicalToken(value string) bool {
	if !canonicalText(value, 128) {
		return false
	}
	for _, r := range value {
		if r != '_' && r != '-' && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func validDecimal(value string) bool {
	d, err := decimal.NewFromString(value)
	return err == nil && len(value) <= 128 && d.String() == value && d.Abs().LessThanOrEqual(decimal.New(1, 30))
}

func nonnegative(value string) bool {
	return validDecimal(value) && !decimal.RequireFromString(value).IsNegative()
}

func probability(value string) bool {
	return nonnegative(value) && decimal.RequireFromString(value).LessThanOrEqual(decimal.NewFromInt(1))
}

func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Equal(value.Truncate(time.Microsecond))
}

func formatTime(value time.Time) string { return value.Format(timeLayout) }

func parseTime(value string) time.Time { parsed, _ := time.Parse(timeLayout, value); return parsed }

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
