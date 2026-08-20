// Package operatorbrief owns immutable daily operator evidence and open
// incident projections. It has no notification, acknowledgement, risk,
// promotion, scheduling, ledger, provider, or execution mutation authority.
package operatorbrief

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/costattribution"
	"github.com/PatrickFanella/get-rich-quick/internal/dailysupervisor"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	BriefSchemaV1 = "daily-operator-brief-v1"
	timeLayout    = "2006-01-02T15:04:05.000000Z"
)

type PerformanceStatus string

const (
	PerformancePositive    PerformanceStatus = "positive"
	PerformanceFlat        PerformanceStatus = "flat"
	PerformanceNegative    PerformanceStatus = "negative"
	PerformanceUnavailable PerformanceStatus = "unavailable"
)

type (
	FactInput        struct{ Key, Value string }
	PerformanceInput struct {
		EvaluationID          uuid.UUID
		EvaluationSHA256      string
		Status                PerformanceStatus
		Headline, Explanation string
		Facts                 []FactInput
	}
)

type Input struct {
	OperatingDay, Timezone string
	GeneratedAt            time.Time
	Supervisor             *dailysupervisor.Assessment
	Costs                  *costattribution.Report
	Performance            PerformanceInput
}

type Fact struct {
	Sequence int    `json:"sequence"`
	Key      string `json:"key"`
	Value    string `json:"value"`
}
type Section struct {
	Sequence       int    `json:"sequence"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	Headline       string `json:"headline"`
	Explanation    string `json:"explanation"`
	EvidenceKind   string `json:"evidence_kind"`
	EvidenceID     string `json:"evidence_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	Facts          []Fact `json:"facts"`
}
type Incident struct {
	Sequence       int    `json:"sequence"`
	Key            string `json:"key"`
	Severity       string `json:"severity"`
	State          string `json:"state"`
	SourceKind     string `json:"source_kind"`
	SourceID       string `json:"source_id"`
	SourceSHA256   string `json:"source_sha256"`
	Summary        string `json:"summary"`
	RequiredAction string `json:"required_action"`
}
type briefCanonical struct {
	Schema, OperatingDay, Timezone, GeneratedAt string
	SupervisorID                                string     `json:"supervisor_id"`
	SupervisorSHA256                            string     `json:"supervisor_sha256"`
	ReconciliationID                            string     `json:"reconciliation_id"`
	ReconciliationSHA256                        string     `json:"reconciliation_sha256"`
	CostReportID                                string     `json:"cost_report_id"`
	CostReportSHA256                            string     `json:"cost_report_sha256"`
	ReviewSummaryID                             string     `json:"review_summary_id"`
	ReviewSummarySHA256                         string     `json:"review_summary_sha256"`
	PerformanceEvaluationID                     string     `json:"performance_evaluation_id"`
	PerformanceEvaluationSHA256                 string     `json:"performance_evaluation_sha256"`
	Sections                                    []Section  `json:"sections"`
	Incidents                                   []Incident `json:"incidents"`
}

// MarshalJSON fixes the leading scalar field names without exposing mutable
// state on Brief.
func (c briefCanonical) MarshalJSON() ([]byte, error) {
	type wire struct {
		Schema                      string     `json:"schema"`
		OperatingDay                string     `json:"operating_day"`
		Timezone                    string     `json:"timezone"`
		GeneratedAt                 string     `json:"generated_at"`
		SupervisorID                string     `json:"supervisor_id"`
		SupervisorSHA256            string     `json:"supervisor_sha256"`
		ReconciliationID            string     `json:"reconciliation_id"`
		ReconciliationSHA256        string     `json:"reconciliation_sha256"`
		CostReportID                string     `json:"cost_report_id"`
		CostReportSHA256            string     `json:"cost_report_sha256"`
		ReviewSummaryID             string     `json:"review_summary_id"`
		ReviewSummarySHA256         string     `json:"review_summary_sha256"`
		PerformanceEvaluationID     string     `json:"performance_evaluation_id"`
		PerformanceEvaluationSHA256 string     `json:"performance_evaluation_sha256"`
		Sections                    []Section  `json:"sections"`
		Incidents                   []Incident `json:"incidents"`
	}
	return json.Marshal(wire(c))
}

type Brief struct {
	id        uuid.UUID
	digest    string
	raw       json.RawMessage
	canonical briefCanonical
}
type Record struct {
	ID                          uuid.UUID
	SHA256                      string
	CanonicalBytes              json.RawMessage
	OperatingDay, Timezone      string
	GeneratedAt                 time.Time
	SupervisorID                uuid.UUID
	SupervisorSHA256            string
	ReconciliationID            uuid.UUID
	ReconciliationSHA256        string
	CostReportID                uuid.UUID
	CostReportSHA256            string
	ReviewSummaryID             uuid.UUID
	ReviewSummarySHA256         string
	PerformanceEvaluationID     uuid.UUID
	PerformanceEvaluationSHA256 string
	Sections                    []Section
	Incidents                   []Incident
}

func NewBrief(input Input) (*Brief, error) {
	if input.Supervisor == nil || input.Costs == nil {
		return nil, fmt.Errorf("operator brief parents are required")
	}
	supervisor := input.Supervisor.Record()
	costs := input.Costs.Record()
	location, err := time.LoadLocation(input.Timezone)
	generated := normalizeTime(input.GeneratedAt)
	if err != nil || input.Timezone != supervisor.Timezone || input.OperatingDay != supervisor.OperatingDay || generated.IsZero() || input.OperatingDay != generated.In(location).Format("2006-01-02") || generated.Before(supervisor.EvaluatedAt) {
		return nil, fmt.Errorf("operator brief day or timezone is invalid")
	}
	performance, err := performanceSection(input.Performance)
	if err != nil {
		return nil, err
	}
	sections := []Section{performance, decisionSection(costs), driftSection(supervisor), riskSection(supervisor), costSection(costs)}
	for index := range sections {
		sections[index].Sequence = index
	}
	incidents := deriveIncidents(input.Performance, supervisor, costs)
	for index := range incidents {
		incidents[index].Sequence = index
	}
	performanceID := ""
	if input.Performance.EvaluationID != uuid.Nil {
		performanceID = input.Performance.EvaluationID.String()
	}
	c := briefCanonical{BriefSchemaV1, input.OperatingDay, input.Timezone, formatTime(generated), supervisor.ID.String(), supervisor.SHA256, supervisor.ReconciliationID.String(), supervisor.ReconciliationSHA256, costs.ID.String(), costs.SHA256, costs.SummaryID.String(), costs.SummarySHA256, performanceID, input.Performance.EvaluationSHA256, sections, incidents}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	digest := hash(raw)
	return &Brief{economicid.DeterministicUUID("daily-operator-brief", BriefSchemaV1+"@sha256:"+digest), digest, raw, c}, nil
}

func performanceSection(input PerformanceInput) (Section, error) {
	if input.Status != PerformancePositive && input.Status != PerformanceFlat && input.Status != PerformanceNegative && input.Status != PerformanceUnavailable {
		return Section{}, fmt.Errorf("operator brief performance status is invalid")
	}
	if !text(input.Headline, 512) || !text(input.Explanation, 2048) {
		return Section{}, fmt.Errorf("operator brief performance explanation is invalid")
	}
	if input.Status == PerformanceUnavailable {
		if input.EvaluationID != uuid.Nil || input.EvaluationSHA256 != "" {
			return Section{}, fmt.Errorf("unavailable performance invented evidence")
		}
	} else if input.EvaluationID == uuid.Nil || !validSHA(input.EvaluationSHA256) {
		return Section{}, fmt.Errorf("operator brief performance evidence is invalid")
	}
	facts, err := normalizeFacts(input.Facts)
	if err != nil {
		return Section{}, err
	}
	id := ""
	if input.EvaluationID != uuid.Nil {
		id = input.EvaluationID.String()
	}
	return Section{Name: "performance", Status: string(input.Status), Headline: input.Headline, Explanation: input.Explanation, EvidenceKind: "trade_portfolio_evaluation", EvidenceID: id, EvidenceSHA256: input.EvaluationSHA256, Facts: facts}, nil
}

func decisionSection(costs costattribution.Record) Section {
	return Section{Name: "decisions", Status: "reviewed", Headline: "Evidence review decision is retained.", Explanation: "The brief reports the exact review summary referenced by cost attribution and cannot change its authority.", EvidenceKind: "evidence_review_summary", EvidenceID: costs.SummaryID.String(), EvidenceSHA256: costs.SummarySHA256, Facts: []Fact{{0, "summary_id", costs.SummaryID.String()}}}
}

func driftSection(supervisor dailysupervisor.Record) Section {
	state, reason := "clean", "No reconciliation drift was admitted by the supervisor."
	for _, check := range supervisor.Checks {
		if check.Name == dailysupervisor.CheckReconciliation && check.State != dailysupervisor.StatePass {
			state, reason = "attention", check.Reason
		}
	}
	return Section{Name: "drift", Status: state, Headline: "Reconciliation status is retained.", Explanation: reason, EvidenceKind: "venue_reconciliation_run", EvidenceID: supervisor.ReconciliationID.String(), EvidenceSHA256: supervisor.ReconciliationSHA256, Facts: []Fact{{0, "reconciliation_id", supervisor.ReconciliationID.String()}}}
}

func riskSection(supervisor dailysupervisor.Record) Section {
	status := "ready"
	facts := make([]Fact, 0, len(supervisor.Actions))
	for index, action := range supervisor.Actions {
		facts = append(facts, Fact{index, string(action.Work), string(action.Admission)})
		if action.Admission == dailysupervisor.AdmissionHalted {
			status = "restricted"
		}
	}
	return Section{Name: "risk", Status: status, Headline: "Supervisor work admissions are retained.", Explanation: "Admissions are evidence only and do not execute, cancel, flatten, or alter risk.", EvidenceKind: "daily_supervisor_assessment", EvidenceID: supervisor.ID.String(), EvidenceSHA256: supervisor.SHA256, Facts: facts}
}

func costSection(costs costattribution.Record) Section {
	t := costs.Totals
	facts := []Fact{{0, "actual_costs", t.ActualCosts}, {1, "estimated_costs", t.EstimatedCosts}, {2, "actual_rebates", t.ActualRebates}, {3, "estimated_rebates", t.EstimatedRebates}, {4, "known_net_cost", t.KnownNetCost}, {5, "unknown_count", fmt.Sprint(t.UnknownCount)}}
	return Section{Name: "costs", Status: t.Coverage, Headline: "Cost attribution coverage is retained.", Explanation: "Known totals preserve actuals, estimates, rebates, and unknowns separately.", EvidenceKind: "full_cost_attribution_report", EvidenceID: costs.ID.String(), EvidenceSHA256: costs.SHA256, Facts: facts}
}

func deriveIncidents(performance PerformanceInput, supervisor dailysupervisor.Record, costs costattribution.Record) []Incident {
	values := []Incident{}
	for _, attention := range supervisor.Attention {
		severity := "high"
		if attention.Check == dailysupervisor.CheckDatabase || attention.Check == dailysupervisor.CheckSchema || attention.Check == dailysupervisor.CheckLedgerProjection || attention.Check == dailysupervisor.CheckRiskBrake {
			severity = "critical"
		}
		values = append(values, Incident{Key: "supervisor_check:" + string(attention.Check), Severity: severity, State: "open", SourceKind: "daily_supervisor_assessment", SourceID: supervisor.ID.String(), SourceSHA256: supervisor.SHA256, Summary: attention.Reason, RequiredAction: "Inspect and repair the exact failed supervisor dependency; do not override it in the brief."})
	}
	for _, action := range supervisor.Actions {
		if action.Admission == dailysupervisor.AdmissionHalted {
			severity := "high"
			if action.Work == dailysupervisor.WorkProtectiveExit {
				severity = "critical"
			}
			values = append(values, Incident{Key: "work_halted:" + string(action.Work), Severity: severity, State: "open", SourceKind: "daily_supervisor_assessment", SourceID: supervisor.ID.String(), SourceSHA256: supervisor.SHA256, Summary: string(action.Work) + " is halted.", RequiredAction: "Inspect the named blockers before authorizing this work class."})
		}
	}
	for _, line := range costs.Lines {
		if line.Status == costattribution.StatusUnknown {
			values = append(values, Incident{Key: "cost_unknown:" + line.Key, Severity: "high", State: "open", SourceKind: "full_cost_attribution_report", SourceID: costs.ID.String(), SourceSHA256: costs.SHA256, Summary: line.Explanation, RequiredAction: "Retain unknown until an attributable source and reviewed method exist."})
		}
	}
	if performance.Status == PerformanceUnavailable || performance.Status == PerformanceNegative {
		summary := "Performance evidence is unavailable."
		if performance.Status == PerformanceNegative {
			summary = "Performance evidence is negative."
		}
		id := ""
		if performance.EvaluationID != uuid.Nil {
			id = performance.EvaluationID.String()
		}
		values = append(values, Incident{Key: "performance:" + string(performance.Status), Severity: "high", State: "open", SourceKind: "trade_portfolio_evaluation", SourceID: id, SourceSHA256: performance.EvaluationSHA256, Summary: summary, RequiredAction: "Review exact performance evidence before changing strategy state."})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Key < values[j].Key })
	return values
}

func normalizeFacts(values []FactInput) ([]Fact, error) {
	if len(values) == 0 || len(values) > 64 {
		return nil, fmt.Errorf("operator brief facts are invalid")
	}
	copyValues := append([]FactInput(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i].Key < copyValues[j].Key })
	facts := make([]Fact, 0, len(copyValues))
	for index, value := range copyValues {
		if !key(value.Key) || !text(value.Value, 1024) || index > 0 && copyValues[index-1].Key == value.Key {
			return nil, fmt.Errorf("operator brief fact is invalid")
		}
		facts = append(facts, Fact{index, value.Key, value.Value})
	}
	return facts, nil
}

func (b *Brief) ID() uuid.UUID {
	if b == nil {
		return uuid.Nil
	}
	return b.id
}

func (b *Brief) Digest() string {
	if b == nil {
		return ""
	}
	return b.digest
}

func (b *Brief) CanonicalBytes() json.RawMessage {
	if b == nil {
		return nil
	}
	return append(json.RawMessage(nil), b.raw...)
}

func (b *Brief) Sections() []Section {
	if b == nil {
		return nil
	}
	return append([]Section(nil), b.canonical.Sections...)
}

func (b *Brief) Incidents() []Incident {
	if b == nil {
		return nil
	}
	return append([]Incident(nil), b.canonical.Incidents...)
}

func (b *Brief) Record() Record {
	if b == nil {
		return Record{}
	}
	c := b.canonical
	supervisorID, _ := uuid.Parse(c.SupervisorID)
	reconciliationID, _ := uuid.Parse(c.ReconciliationID)
	costID, _ := uuid.Parse(c.CostReportID)
	summaryID, _ := uuid.Parse(c.ReviewSummaryID)
	performanceID, _ := uuid.Parse(c.PerformanceEvaluationID)
	generated, _ := time.Parse(timeLayout, c.GeneratedAt)
	return Record{b.id, b.digest, b.CanonicalBytes(), c.OperatingDay, c.Timezone, generated, supervisorID, c.SupervisorSHA256, reconciliationID, c.ReconciliationSHA256, costID, c.CostReportSHA256, summaryID, c.ReviewSummarySHA256, performanceID, c.PerformanceEvaluationSHA256, b.Sections(), b.Incidents()}
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC().Truncate(time.Microsecond)
}
func formatTime(value time.Time) string { return value.Format(timeLayout) }
func validSHA(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func text(value string, limit int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= limit && !strings.ContainsRune(value, '\x00')
}

func key(value string) bool {
	if value == "" || len(value) > 192 {
		return false
	}
	for _, r := range value {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				if !strings.ContainsRune("_./:-", r) {
					return false
				}
			}
		}
	}
	return true
}
func hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
