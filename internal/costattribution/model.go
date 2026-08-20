// Package costattribution owns immutable cost statements. It reports evidence
// and has no ledger, promotion, deployment, scheduler, risk, or execution
// mutation authority.
package costattribution

import (
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

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/evidencereview"
	"github.com/PatrickFanella/get-rich-quick/internal/researchworkflow"
)

const ReportSchemaV1 = "full-cost-attribution-report-v1"

type Category string
type Status string

const (
	CategoryModel          Category = "model"
	CategoryData           Category = "data"
	CategoryFee            Category = "fee"
	CategoryRebate         Category = "rebate"
	CategoryInfrastructure Category = "infrastructure"

	StatusActual    Status = "actual"
	StatusEstimated Status = "estimated"
	StatusUnknown   Status = "unknown"

	CoverageActualOnly       = "complete_actual"
	CoverageWithEstimates    = "complete_with_estimates"
	CoverageContainsUnknowns = "incomplete_unknown"
)

var categoryOrder = []Category{CategoryModel, CategoryData, CategoryFee, CategoryRebate, CategoryInfrastructure}
var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_./:-]{0,191}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

type LineInput struct {
	Key, Amount                                       string
	Category                                          Category
	Status                                            Status
	EvidenceKind                                      string
	EvidenceID                                        uuid.UUID
	EvidenceSHA256, Method, MethodSHA256, Explanation string
}

type Input struct {
	Case                                *evidencereview.Case
	Summary                             *evidencereview.Summary
	Hypothesis                          *researchworkflow.Hypothesis
	Manifest                            *dataset.Manifest
	AccountID                           uuid.UUID
	WindowStart, WindowEnd, StatementAt time.Time
	Currency                            string
	Lines                               []LineInput
}

type Line struct {
	Sequence       int      `json:"sequence"`
	Key            string   `json:"key"`
	Category       Category `json:"category"`
	Status         Status   `json:"status"`
	Amount         string   `json:"amount"`
	EvidenceKind   string   `json:"evidence_kind"`
	EvidenceID     string   `json:"evidence_id"`
	EvidenceSHA256 string   `json:"evidence_sha256"`
	Method         string   `json:"method"`
	MethodSHA256   string   `json:"method_sha256"`
	Explanation    string   `json:"explanation"`
}

type Totals struct {
	ActualCosts      string `json:"actual_costs"`
	EstimatedCosts   string `json:"estimated_costs"`
	ActualRebates    string `json:"actual_rebates"`
	EstimatedRebates string `json:"estimated_rebates"`
	KnownNetCost     string `json:"known_net_cost"`
	UnknownCount     int    `json:"unknown_count"`
	Coverage         string `json:"coverage"`
}

type reportCanonical struct {
	Schema           string `json:"schema"`
	CaseID           string `json:"case_id"`
	CaseSHA256       string `json:"case_sha256"`
	SummaryID        string `json:"summary_id"`
	SummarySHA256    string `json:"summary_sha256"`
	HypothesisID     string `json:"hypothesis_id"`
	HypothesisSHA256 string `json:"hypothesis_sha256"`
	ManifestID       string `json:"manifest_id"`
	ManifestSHA256   string `json:"manifest_sha256"`
	AccountID        string `json:"account_id"`
	WindowStart      string `json:"window_start"`
	WindowEnd        string `json:"window_end"`
	StatementAt      string `json:"statement_at"`
	Currency         string `json:"currency"`
	Lines            []Line `json:"lines"`
	Totals           Totals `json:"totals"`
}

type Report struct {
	id        uuid.UUID
	digest    string
	raw       json.RawMessage
	canonical reportCanonical
}

type Record struct {
	ID                                  uuid.UUID
	SHA256                              string
	CanonicalBytes                      json.RawMessage
	CaseID                              uuid.UUID
	CaseSHA256                          string
	SummaryID                           uuid.UUID
	SummarySHA256                       string
	HypothesisID                        uuid.UUID
	HypothesisSHA256                    string
	ManifestID                          uuid.UUID
	ManifestSHA256                      string
	AccountID                           uuid.UUID
	WindowStart, WindowEnd, StatementAt time.Time
	Currency                            string
	Lines                               []Line
	Totals                              Totals
}

func NewReport(input Input) (*Report, error) {
	if input.Case == nil || input.Summary == nil || input.Hypothesis == nil || input.Manifest == nil || input.AccountID == uuid.Nil {
		return nil, fmt.Errorf("cost attribution parents are required")
	}
	if input.Summary.CaseID() != input.Case.ID() || input.Summary.CaseDigest() != input.Case.Digest() || input.Case.HypothesisID() != input.Hypothesis.ID() || input.Case.HypothesisDigest() != input.Hypothesis.Digest() || input.Hypothesis.ManifestID() != input.Manifest.ID() || input.Hypothesis.ManifestDigest() != input.Manifest.Digest() {
		return nil, fmt.Errorf("cost attribution parent binding is invalid")
	}
	start, end, statement := normalizeTime(input.WindowStart), normalizeTime(input.WindowEnd), normalizeTime(input.StatementAt)
	if start.IsZero() || !end.After(start) || statement.Before(end) || !canonicalTime(input.WindowStart) || !canonicalTime(input.WindowEnd) || !canonicalTime(input.StatementAt) || !currencyPattern.MatchString(input.Currency) {
		return nil, fmt.Errorf("cost attribution window or currency is invalid")
	}
	lines, totals, err := normalizeLines(input.Lines, input)
	if err != nil {
		return nil, err
	}
	c := reportCanonical{ReportSchemaV1, input.Case.ID().String(), input.Case.Digest(), input.Summary.ID().String(), input.Summary.Digest(), input.Hypothesis.ID().String(), input.Hypothesis.Digest(), input.Manifest.ID().String(), input.Manifest.Digest(), input.AccountID.String(), formatTime(start), formatTime(end), formatTime(statement), input.Currency, lines, totals}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	digest := hash(raw)
	return &Report{economicid.DeterministicUUID("full-cost-attribution-report", ReportSchemaV1+"@sha256:"+digest), digest, raw, c}, nil
}

func normalizeLines(inputs []LineInput, parent Input) ([]Line, Totals, error) {
	if len(inputs) < len(categoryOrder) || len(inputs) > 256 {
		return nil, Totals{}, fmt.Errorf("cost attribution lines are incomplete")
	}
	values := append([]LineInput(nil), inputs...)
	order := map[Category]int{}
	for i, category := range categoryOrder {
		order[category] = i
	}
	sort.Slice(values, func(i, j int) bool {
		if order[values[i].Category] == order[values[j].Category] {
			return values[i].Key < values[j].Key
		}
		return order[values[i].Category] < order[values[j].Category]
	})
	seenKeys, categories := map[string]bool{}, map[Category]bool{}
	actualCosts, estimatedCosts, actualRebates, estimatedRebates := decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
	unknown, estimates := 0, 0
	lines := make([]Line, 0, len(values))
	for sequence, value := range values {
		if !keyPattern.MatchString(value.Key) || seenKeys[value.Key] || order[value.Category] < 0 || (value.Category != CategoryModel && value.Category != CategoryData && value.Category != CategoryFee && value.Category != CategoryRebate && value.Category != CategoryInfrastructure) {
			return nil, Totals{}, fmt.Errorf("cost attribution line identity is invalid")
		}
		seenKeys[value.Key], categories[value.Category] = true, true
		if value.Explanation == "" || value.Explanation != strings.TrimSpace(value.Explanation) || len(value.Explanation) > 2048 {
			return nil, Totals{}, fmt.Errorf("cost attribution line explanation is invalid")
		}
		line := Line{Sequence: sequence, Key: value.Key, Category: value.Category, Status: value.Status, Amount: value.Amount, EvidenceKind: value.EvidenceKind, EvidenceSHA256: value.EvidenceSHA256, Method: value.Method, MethodSHA256: value.MethodSHA256, Explanation: value.Explanation}
		if value.EvidenceID != uuid.Nil {
			line.EvidenceID = value.EvidenceID.String()
		}
		switch value.Status {
		case StatusUnknown:
			if value.Amount != "" || value.EvidenceKind != "" || value.EvidenceID != uuid.Nil || value.EvidenceSHA256 != "" || value.Method != "" || value.MethodSHA256 != "" {
				return nil, Totals{}, fmt.Errorf("unknown cost attribution line invented facts")
			}
			unknown++
		case StatusActual, StatusEstimated:
			amount, err := exactAmount(value.Amount)
			if err != nil || !keyPattern.MatchString(value.EvidenceKind) || value.EvidenceID == uuid.Nil || !shaPattern.MatchString(value.EvidenceSHA256) {
				return nil, Totals{}, fmt.Errorf("known cost attribution line is invalid")
			}
			if value.Status == StatusEstimated {
				if !keyPattern.MatchString(value.Method) || !shaPattern.MatchString(value.MethodSHA256) {
					return nil, Totals{}, fmt.Errorf("estimated cost attribution method is invalid")
				}
				estimates++
			} else if value.Method != "" || value.MethodSHA256 != "" {
				return nil, Totals{}, fmt.Errorf("actual cost attribution cannot use an estimate method")
			}
			if value.Category == CategoryModel && value.Status == StatusActual && (value.EvidenceKind != "research_hypothesis" || value.EvidenceID != parent.Hypothesis.ID() || value.EvidenceSHA256 != parent.Hypothesis.Digest() || value.Amount != parent.Hypothesis.ProvenanceCost() || parent.Currency != parent.Hypothesis.ProvenanceCurrency()) {
				return nil, Totals{}, fmt.Errorf("actual model cost does not match provenance")
			}
			if value.Category == CategoryFee && value.Status == StatusActual && value.EvidenceKind != "ledger_transaction" || value.Category == CategoryRebate && value.Status == StatusActual && value.EvidenceKind != "ledger_transaction" {
				return nil, Totals{}, fmt.Errorf("actual ledger cost evidence is invalid")
			}
			if value.Category == CategoryRebate {
				if value.Status == StatusActual {
					actualRebates = actualRebates.Add(amount)
				} else {
					estimatedRebates = estimatedRebates.Add(amount)
				}
			} else if value.Status == StatusActual {
				actualCosts = actualCosts.Add(amount)
			} else {
				estimatedCosts = estimatedCosts.Add(amount)
			}
		default:
			return nil, Totals{}, fmt.Errorf("cost attribution status is invalid")
		}
		lines = append(lines, line)
	}
	for _, category := range categoryOrder {
		if !categories[category] {
			return nil, Totals{}, fmt.Errorf("cost attribution category %s is missing", category)
		}
	}
	coverage := CoverageActualOnly
	if estimates > 0 {
		coverage = CoverageWithEstimates
	}
	if unknown > 0 {
		coverage = CoverageContainsUnknowns
	}
	totals := Totals{actualCosts.String(), estimatedCosts.String(), actualRebates.String(), estimatedRebates.String(), actualCosts.Add(estimatedCosts).Sub(actualRebates).Sub(estimatedRebates).String(), unknown, coverage}
	return lines, totals, nil
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
	return append(json.RawMessage(nil), r.raw...)
}
func (r *Report) Totals() Totals {
	if r == nil {
		return Totals{}
	}
	return r.canonical.Totals
}
func (r *Report) Lines() []Line {
	if r == nil {
		return nil
	}
	return append([]Line(nil), r.canonical.Lines...)
}
func (r *Report) Record() Record {
	if r == nil {
		return Record{}
	}
	c := r.canonical
	caseID, _ := uuid.Parse(c.CaseID)
	summaryID, _ := uuid.Parse(c.SummaryID)
	hypothesisID, _ := uuid.Parse(c.HypothesisID)
	manifestID, _ := uuid.Parse(c.ManifestID)
	accountID, _ := uuid.Parse(c.AccountID)
	return Record{r.id, r.digest, r.CanonicalBytes(), caseID, c.CaseSHA256, summaryID, c.SummarySHA256, hypothesisID, c.HypothesisSHA256, manifestID, c.ManifestSHA256, accountID, mustTime(c.WindowStart), mustTime(c.WindowEnd), mustTime(c.StatementAt), c.Currency, r.Lines(), c.Totals}
}
func exactAmount(value string) (decimal.Decimal, error) {
	if value == "" || len(value) > 80 || strings.ContainsAny(value, "eE+") {
		return decimal.Zero, fmt.Errorf("invalid amount")
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.IsNegative() || parsed.String() != value {
		return decimal.Zero, fmt.Errorf("invalid amount")
	}
	return parsed, nil
}
func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}
func normalizeTime(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }
func formatTime(value time.Time) string       { return value.Format("2006-01-02T15:04:05.000000Z") }
func mustTime(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", value)
	return parsed
}
func hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
