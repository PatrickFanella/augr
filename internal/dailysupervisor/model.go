// Package dailysupervisor owns deterministic autonomy admission evidence. It
// has no scheduler, risk-control, execution, settlement, or provider authority.
package dailysupervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/financialscheduler"
)

const AssessmentSchemaV1 = "autonomous-daily-supervisor-assessment-v1"

type CheckName string
type CheckState string
type WorkClass string
type Admission string

const (
	CheckDatabase             CheckName = "database"
	CheckSchema               CheckName = "schema"
	CheckLedgerProjection     CheckName = "ledger_projection"
	CheckMarketData           CheckName = "market_data"
	CheckRiskBrake            CheckName = "risk_brake"
	CheckReconciliation       CheckName = "reconciliation"
	CheckExposureScheduler    CheckName = "exposure_scheduler"
	CheckExitWorker           CheckName = "exit_worker"
	CheckSettlementWorker     CheckName = "settlement_worker"
	CheckReconciliationWorker CheckName = "reconciliation_worker"

	StatePass    CheckState = "pass"
	StateFail    CheckState = "fail"
	StateUnknown CheckState = "unknown"

	WorkNewExposure    WorkClass = "new_exposure"
	WorkProtectiveExit WorkClass = "protective_exit"
	WorkSettlement     WorkClass = "settlement"
	WorkReconciliation WorkClass = "reconciliation"
	WorkEvidenceOnly   WorkClass = "evidence_only"

	AdmissionEligible Admission = "eligible"
	AdmissionHalted   Admission = "halted"
)

var checkOrder = []CheckName{CheckDatabase, CheckSchema, CheckLedgerProjection, CheckMarketData, CheckRiskBrake, CheckReconciliation, CheckExposureScheduler, CheckExitWorker, CheckSettlementWorker, CheckReconciliationWorker}
var workOrder = []WorkClass{WorkNewExposure, WorkProtectiveExit, WorkSettlement, WorkReconciliation, WorkEvidenceOnly}

var requirements = map[WorkClass][]CheckName{
	WorkNewExposure:    {CheckDatabase, CheckSchema, CheckLedgerProjection, CheckMarketData, CheckRiskBrake, CheckReconciliation, CheckExposureScheduler},
	WorkProtectiveExit: {CheckDatabase, CheckSchema, CheckLedgerProjection, CheckRiskBrake, CheckExitWorker},
	WorkSettlement:     {CheckDatabase, CheckSchema, CheckLedgerProjection, CheckRiskBrake, CheckSettlementWorker},
	WorkReconciliation: {CheckDatabase, CheckSchema, CheckLedgerProjection, CheckReconciliationWorker},
	WorkEvidenceOnly:   {CheckDatabase, CheckSchema},
}

type CheckInput struct {
	Name           CheckName
	State          CheckState
	EvidenceID     uuid.UUID
	EvidenceSHA256 string
	ObservedAt     time.Time
	FreshThrough   time.Time
	Reason         string
}

type Prior struct {
	ID          uuid.UUID
	SHA256      string
	EvaluatedAt time.Time
}
type ReconciliationReference struct {
	ID            uuid.UUID
	SHA256        string
	Clean         bool
	IncidentCount int
}

type Input struct {
	OperatingDay        string
	Timezone            string
	EvaluatedAt         time.Time
	PolicyVersion       string
	Reconciliation      ReconciliationReference
	SchedulerOccurrence *financialscheduler.Occurrence
	SchedulerEffect     *financialscheduler.Effect
	Checks              []CheckInput
	Prior               *Prior
}

type Action struct {
	Work      WorkClass   `json:"work_class"`
	Admission Admission   `json:"admission"`
	BlockedBy []CheckName `json:"blocked_by"`
}
type Attention struct {
	Check          CheckName  `json:"check"`
	State          CheckState `json:"state"`
	Reason         string     `json:"reason"`
	EvidenceID     string     `json:"evidence_id"`
	EvidenceSHA256 string     `json:"evidence_sha256"`
}

type Assessment struct {
	id        uuid.UUID
	digest    string
	raw       json.RawMessage
	canonical assessmentCanonical
}

type checkCanonical struct {
	Name           CheckName  `json:"name"`
	State          CheckState `json:"state"`
	EvidenceID     string     `json:"evidence_id"`
	EvidenceSHA256 string     `json:"evidence_sha256"`
	ObservedAt     string     `json:"observed_at"`
	FreshThrough   string     `json:"fresh_through"`
	Reason         string     `json:"reason"`
}
type assessmentCanonical struct {
	Schema                    string           `json:"schema"`
	OperatingDay              string           `json:"operating_day"`
	Timezone                  string           `json:"timezone"`
	EvaluatedAt               string           `json:"evaluated_at"`
	PolicyVersion             string           `json:"policy_version"`
	ReconciliationID          string           `json:"reconciliation_id"`
	ReconciliationSHA256      string           `json:"reconciliation_sha256"`
	SchedulerOccurrenceID     string           `json:"scheduler_occurrence_id"`
	SchedulerOccurrenceSHA256 string           `json:"scheduler_occurrence_sha256"`
	SchedulerEffectID         string           `json:"scheduler_effect_id"`
	SchedulerEffectSHA256     string           `json:"scheduler_effect_sha256"`
	PriorAssessmentID         string           `json:"prior_assessment_id"`
	PriorAssessmentSHA256     string           `json:"prior_assessment_sha256"`
	Checks                    []checkCanonical `json:"checks"`
	Actions                   []Action         `json:"actions"`
	Attention                 []Attention      `json:"attention"`
}

func NewAssessment(input Input) (*Assessment, error) {
	evaluatedAt := normalizeTime(input.EvaluatedAt)
	location, locationErr := time.LoadLocation(input.Timezone)
	if locationErr != nil || evaluatedAt.IsZero() || input.OperatingDay != evaluatedAt.In(location).Format("2006-01-02") {
		return nil, fmt.Errorf("daily supervisor: operating day/timezone does not match evaluation")
	}
	if strings.TrimSpace(input.PolicyVersion) == "" || input.PolicyVersion != strings.TrimSpace(input.PolicyVersion) {
		return nil, fmt.Errorf("daily supervisor: policy version is required")
	}
	if input.Reconciliation.ID == uuid.Nil || !validSHA(input.Reconciliation.SHA256) || input.Reconciliation.IncidentCount < 0 || input.Reconciliation.Clean != (input.Reconciliation.IncidentCount == 0) {
		return nil, fmt.Errorf("daily supervisor: reconciliation reference is invalid")
	}
	if input.SchedulerOccurrence == nil || input.SchedulerEffect == nil {
		return nil, fmt.Errorf("daily supervisor: scheduler evidence is required")
	}
	if err := input.SchedulerOccurrence.Validate(); err != nil {
		return nil, err
	}
	if err := input.SchedulerEffect.Validate(); err != nil {
		return nil, err
	}
	if input.SchedulerOccurrence.JobKey != "daily_supervisor" || input.SchedulerEffect.Kind != financialscheduler.EffectSupervisor || input.SchedulerEffect.OccurrenceID != input.SchedulerOccurrence.ID {
		return nil, fmt.Errorf("daily supervisor: scheduler evidence scope is invalid")
	}
	checks, states, err := canonicalChecks(input.Checks, evaluatedAt)
	if err != nil {
		return nil, err
	}
	if !input.Reconciliation.Clean && states[CheckReconciliation] == StatePass {
		return nil, fmt.Errorf("daily supervisor: drifting reconciliation cannot pass")
	}
	actions := make([]Action, 0, len(workOrder))
	for _, work := range workOrder {
		blocked := make([]CheckName, 0)
		for _, required := range requirements[work] {
			if states[required] != StatePass {
				blocked = append(blocked, required)
			}
		}
		admission := AdmissionEligible
		if len(blocked) > 0 {
			admission = AdmissionHalted
		}
		actions = append(actions, Action{work, admission, blocked})
	}
	attention := make([]Attention, 0)
	for _, check := range checks {
		if check.State != StatePass {
			attention = append(attention, Attention{check.Name, check.State, check.Reason, check.EvidenceID, check.EvidenceSHA256})
		}
	}
	priorID, priorSHA := "", ""
	if input.Prior != nil {
		if input.Prior.ID == uuid.Nil || !validSHA(input.Prior.SHA256) || !normalizeTime(input.Prior.EvaluatedAt).Before(evaluatedAt) {
			return nil, fmt.Errorf("daily supervisor: prior assessment is invalid")
		}
		priorID, priorSHA = input.Prior.ID.String(), input.Prior.SHA256
	}
	c := assessmentCanonical{AssessmentSchemaV1, input.OperatingDay, input.Timezone, formatTime(evaluatedAt), input.PolicyVersion, input.Reconciliation.ID.String(), input.Reconciliation.SHA256, input.SchedulerOccurrence.ID.String(), input.SchedulerOccurrence.SHA256, input.SchedulerEffect.ID.String(), input.SchedulerEffect.SHA256, priorID, priorSHA, checks, actions, attention}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	digest := hash(raw)
	return &Assessment{economicid.DeterministicUUID("autonomous-daily-supervisor-assessment", AssessmentSchemaV1+"@sha256:"+digest), digest, raw, c}, nil
}

func canonicalChecks(inputs []CheckInput, evaluatedAt time.Time) ([]checkCanonical, map[CheckName]CheckState, error) {
	byName := make(map[CheckName]CheckInput, len(inputs))
	for _, in := range inputs {
		if _, ok := byName[in.Name]; ok {
			return nil, nil, fmt.Errorf("daily supervisor: duplicate check %q", in.Name)
		}
		byName[in.Name] = in
	}
	checks := make([]checkCanonical, 0, len(checkOrder))
	states := make(map[CheckName]CheckState, len(checkOrder))
	for _, name := range checkOrder {
		in, ok := byName[name]
		if !ok {
			return nil, nil, fmt.Errorf("daily supervisor: missing check %q", name)
		}
		if in.State != StatePass && in.State != StateFail && in.State != StateUnknown {
			return nil, nil, fmt.Errorf("daily supervisor: invalid check state")
		}
		observed, fresh := normalizeTime(in.ObservedAt), normalizeTime(in.FreshThrough)
		if in.EvidenceID == uuid.Nil || !validSHA(in.EvidenceSHA256) || observed.IsZero() || fresh.IsZero() || fresh.Before(observed) {
			return nil, nil, fmt.Errorf("daily supervisor: invalid check evidence")
		}
		state := in.State
		reason := strings.TrimSpace(in.Reason)
		if fresh.Before(evaluatedAt) {
			state = StateFail
			if reason == "" {
				reason = "stale"
			}
		}
		if state != StatePass && reason == "" {
			return nil, nil, fmt.Errorf("daily supervisor: failed or unknown check requires reason")
		}
		checks = append(checks, checkCanonical{name, state, in.EvidenceID.String(), in.EvidenceSHA256, formatTime(observed), formatTime(fresh), reason})
		states[name] = state
	}
	if len(byName) != len(checkOrder) {
		return nil, nil, fmt.Errorf("daily supervisor: unknown check")
	}
	return checks, states, nil
}

func (a *Assessment) ID() uuid.UUID                   { return a.id }
func (a *Assessment) Digest() string                  { return a.digest }
func (a *Assessment) CanonicalBytes() json.RawMessage { return append(json.RawMessage(nil), a.raw...) }
func (a *Assessment) Actions() []Action               { return append([]Action(nil), a.canonical.Actions...) }
func (a *Assessment) Admission(work WorkClass) Admission {
	for _, v := range a.canonical.Actions {
		if v.Work == work {
			return v.Admission
		}
	}
	return AdmissionHalted
}
func (a *Assessment) Attention() []Attention {
	return append([]Attention(nil), a.canonical.Attention...)
}

func normalizeTime(v time.Time) time.Time {
	if v.IsZero() {
		return time.Time{}
	}
	return v.UTC().Truncate(time.Microsecond)
}
func formatTime(v time.Time) string { return normalizeTime(v).Format("2006-01-02T15:04:05.000000Z") }
func hash(raw []byte) string        { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
func validSHA(v string) bool {
	if len(v) != 64 || v != strings.ToLower(v) {
		return false
	}
	_, e := hex.DecodeString(v)
	return e == nil
}
