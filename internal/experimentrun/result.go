package experimentrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	ResultSchemaV1  = "experiment-run-result-v1"
	resultDomain    = "experiment-run-result"
	ResultCompleted = "completed"
)

type StepOutcomeInput struct {
	Action          StepAction
	DecisionSHA256  string
	IntentID        uuid.UUID
	OrderID         uuid.UUID
	TransitionIDs   []uuid.UUID
	FillIDs         []uuid.UUID
	FilledQuantity  string
	FeeTotal        string
	AggregateSHA256 string
	OutcomeSHA256   string
}

type ResultInput struct {
	Plan                    *Plan
	AccountID               uuid.UUID
	QualityResultID         uuid.UUID
	SimulationPolicyVersion string
	CapitalPolicyVersion    string
	Outcomes                []StepOutcomeInput
}

type stepOutcomeCanonical struct {
	Sequence        int        `json:"sequence"`
	Action          StepAction `json:"action"`
	DecisionSHA256  string     `json:"decision_sha256"`
	IntentID        string     `json:"intent_id"`
	OrderID         string     `json:"order_id"`
	TransitionIDs   []string   `json:"transition_ids"`
	FillIDs         []string   `json:"fill_ids"`
	FilledQuantity  string     `json:"filled_quantity"`
	FeeTotal        string     `json:"fee_total"`
	AggregateSHA256 string     `json:"aggregate_sha256"`
	OutcomeSHA256   string     `json:"outcome_sha256"`
}
type Metrics struct {
	StepCount       int    `json:"step_count"`
	NoopCount       int    `json:"noop_count"`
	RejectedCount   int    `json:"rejected_count"`
	IntentCount     int    `json:"intent_count"`
	OrderCount      int    `json:"order_count"`
	TransitionCount int    `json:"transition_count"`
	FillCount       int    `json:"fill_count"`
	FilledQuantity  string `json:"filled_quantity"`
	FeeTotal        string `json:"fee_total"`
}
type resultCanonical struct {
	Schema                  string                         `json:"schema"`
	State                   string                         `json:"state"`
	ExperimentID            string                         `json:"experiment_id"`
	ProgramID               string                         `json:"program_id"`
	PlanID                  string                         `json:"plan_id"`
	AccountID               string                         `json:"account_id"`
	ManifestID              string                         `json:"manifest_id"`
	QualityResultID         string                         `json:"quality_result_id"`
	SimulationPolicyVersion string                         `json:"simulation_policy_version"`
	CapitalPolicyVersion    string                         `json:"capital_policy_version"`
	Mode                    strategycatalog.ExperimentMode `json:"mode"`
	Metrics                 Metrics                        `json:"metrics"`
	Outcomes                []stepOutcomeCanonical         `json:"outcomes"`
}

type Result struct {
	canonical resultCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewResult(input ResultInput) (*Result, error) {
	if input.Plan == nil || input.AccountID == uuid.Nil || input.QualityResultID == uuid.Nil || !policyVersion(input.SimulationPolicyVersion, "simulation-policy-v1") || !policyVersion(input.CapitalPolicyVersion, "capital-margin-policy-v1") || len(input.Outcomes) != input.Plan.StepCount() {
		return nil, fmt.Errorf("experiment run result identity is invalid")
	}
	outcomes := make([]stepOutcomeCanonical, len(input.Outcomes))
	metrics := Metrics{StepCount: len(outcomes), FilledQuantity: "0", FeeTotal: "0"}
	quantity := decimal.Zero
	fees := decimal.Zero
	for sequence, value := range input.Outcomes {
		if value.Action != input.Plan.StepAction(sequence) || value.DecisionSHA256 != input.Plan.DecisionSHA256(sequence) ||
			!canonicalNonnegativeDecimal(value.FilledQuantity) || !canonicalNonnegativeDecimal(value.FeeTotal) ||
			!validUUIDs(value.TransitionIDs) || !validUUIDs(value.FillIDs) {
			return nil, fmt.Errorf("experiment run outcome %d is invalid", sequence)
		}
		canonical := stepOutcomeCanonical{Sequence: sequence, Action: value.Action, DecisionSHA256: value.DecisionSHA256, IntentID: uuidText(value.IntentID), OrderID: uuidText(value.OrderID), TransitionIDs: uuidTexts(value.TransitionIDs), FillIDs: uuidTexts(value.FillIDs), FilledQuantity: value.FilledQuantity, FeeTotal: value.FeeTotal, AggregateSHA256: value.AggregateSHA256, OutcomeSHA256: value.OutcomeSHA256}
		switch value.Action {
		case ActionNoop:
			metrics.NoopCount++
			if value.IntentID != uuid.Nil || value.OrderID != uuid.Nil || len(value.TransitionIDs) > 0 || len(value.FillIDs) > 0 || value.AggregateSHA256 != "" || value.OutcomeSHA256 != "" || value.FilledQuantity != "0" || value.FeeTotal != "0" {
				return nil, fmt.Errorf("noop outcome carries execution evidence")
			}
		case ActionRejected:
			metrics.RejectedCount++
			if value.IntentID != uuid.Nil || value.OrderID != uuid.Nil || len(value.TransitionIDs) > 0 || len(value.FillIDs) > 0 || value.AggregateSHA256 != "" || value.OutcomeSHA256 != "" || value.FilledQuantity != "0" || value.FeeTotal != "0" {
				return nil, fmt.Errorf("rejected outcome carries economic evidence")
			}
		case ActionExecute:
			metrics.IntentCount++
			metrics.OrderCount++
			if value.IntentID != input.Plan.IntentID(sequence) || value.OrderID != input.Plan.OrderID(sequence) ||
				!digestPattern.MatchString(value.AggregateSHA256) || !digestPattern.MatchString(value.OutcomeSHA256) {
				return nil, fmt.Errorf("executed outcome lacks exact identity")
			}
		default:
			return nil, fmt.Errorf("outcome action is invalid")
		}
		metrics.TransitionCount += len(value.TransitionIDs)
		metrics.FillCount += len(value.FillIDs)
		quantity = quantity.Add(decimal.RequireFromString(value.FilledQuantity))
		fees = fees.Add(decimal.RequireFromString(value.FeeTotal))
		outcomes[sequence] = canonical
	}
	metrics.FilledQuantity = quantity.String()
	metrics.FeeTotal = fees.String()
	canonical := resultCanonical{Schema: ResultSchemaV1, State: ResultCompleted, ExperimentID: input.Plan.ExperimentID().String(), ProgramID: input.Plan.ProgramID().String(), PlanID: input.Plan.ID().String(), AccountID: input.AccountID.String(), ManifestID: input.Plan.ManifestID().String(), QualityResultID: input.QualityResultID.String(), SimulationPolicyVersion: input.SimulationPolicyVersion, CapitalPolicyVersion: input.CapitalPolicyVersion, Mode: input.Plan.Mode(), Metrics: metrics, Outcomes: outcomes}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	digest := hashBytes(encoded)
	return &Result{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID(resultDomain, ResultSchemaV1+"@sha256:"+digest)}, nil
}

func ResultFromCanonical(id uuid.UUID, digest string, raw []byte) (*Result, error) {
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("experiment result envelope is invalid")
	}
	var canonical resultCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	if canonical.Schema != ResultSchemaV1 || canonical.State != ResultCompleted {
		return nil, fmt.Errorf("experiment result state is invalid")
	}
	if err := validateResultCanonical(canonical); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil || !bytes.Equal(encoded, raw) {
		return nil, fmt.Errorf("experiment result bytes are not canonical")
	}
	computed := hashBytes(raw)
	expected := economicid.DeterministicUUID(resultDomain, ResultSchemaV1+"@sha256:"+computed)
	if expected != id {
		return nil, fmt.Errorf("experiment result identity does not reconstruct")
	}
	return &Result{canonical: canonical, bytes: append(json.RawMessage(nil), raw...), digest: digest, id: id}, nil
}

func (r *Result) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.id
}

func (r *Result) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}

func (r *Result) CanonicalBytes() json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage(nil), r.bytes...)
}

func (r *Result) Metrics() Metrics {
	if r == nil {
		return Metrics{}
	}
	return r.canonical.Metrics
}

func validateResultCanonical(canonical resultCanonical) error {
	for _, value := range []string{canonical.ExperimentID, canonical.ProgramID, canonical.PlanID, canonical.AccountID, canonical.ManifestID, canonical.QualityResultID} {
		if _, err := uuid.Parse(value); err != nil {
			return fmt.Errorf("experiment result parent identity is invalid")
		}
	}
	if !policyVersion(canonical.SimulationPolicyVersion, "simulation-policy-v1") ||
		!policyVersion(canonical.CapitalPolicyVersion, "capital-margin-policy-v1") ||
		(canonical.Mode != strategycatalog.ExperimentPaperScored && canonical.Mode != strategycatalog.ExperimentPaperStress) ||
		len(canonical.Outcomes) == 0 || canonical.Metrics.StepCount != len(canonical.Outcomes) {
		return fmt.Errorf("experiment result graph is invalid")
	}
	metrics := Metrics{StepCount: len(canonical.Outcomes), FilledQuantity: "0", FeeTotal: "0"}
	quantity, fees := decimal.Zero, decimal.Zero
	for sequence, outcome := range canonical.Outcomes {
		if outcome.Sequence != sequence || !digestPattern.MatchString(outcome.DecisionSHA256) ||
			!canonicalNonnegativeDecimal(outcome.FilledQuantity) || !canonicalNonnegativeDecimal(outcome.FeeTotal) ||
			!validUUIDTexts(outcome.TransitionIDs) || !validUUIDTexts(outcome.FillIDs) {
			return fmt.Errorf("experiment result outcome graph is invalid")
		}
		switch outcome.Action {
		case ActionNoop:
			metrics.NoopCount++
			if outcome.IntentID != "" || outcome.OrderID != "" || len(outcome.TransitionIDs) > 0 || len(outcome.FillIDs) > 0 || outcome.AggregateSHA256 != "" || outcome.OutcomeSHA256 != "" || outcome.FilledQuantity != "0" || outcome.FeeTotal != "0" {
				return fmt.Errorf("noop result outcome carries execution evidence")
			}
		case ActionRejected:
			metrics.RejectedCount++
			if outcome.IntentID != "" || outcome.OrderID != "" || len(outcome.TransitionIDs) > 0 || len(outcome.FillIDs) > 0 || outcome.AggregateSHA256 != "" || outcome.OutcomeSHA256 != "" || outcome.FilledQuantity != "0" || outcome.FeeTotal != "0" {
				return fmt.Errorf("rejected result outcome carries execution evidence")
			}
		case ActionExecute:
			metrics.IntentCount++
			metrics.OrderCount++
			if _, err := uuid.Parse(outcome.IntentID); err != nil {
				return fmt.Errorf("executed result intent is invalid")
			}
			if _, err := uuid.Parse(outcome.OrderID); err != nil {
				return fmt.Errorf("executed result order is invalid")
			}
			if !digestPattern.MatchString(outcome.AggregateSHA256) || !digestPattern.MatchString(outcome.OutcomeSHA256) {
				return fmt.Errorf("executed result hashes are invalid")
			}
		default:
			return fmt.Errorf("experiment result action is invalid")
		}
		metrics.TransitionCount += len(outcome.TransitionIDs)
		metrics.FillCount += len(outcome.FillIDs)
		quantity = quantity.Add(decimal.RequireFromString(outcome.FilledQuantity))
		fees = fees.Add(decimal.RequireFromString(outcome.FeeTotal))
	}
	metrics.FilledQuantity, metrics.FeeTotal = quantity.String(), fees.String()
	if metrics != canonical.Metrics {
		return fmt.Errorf("experiment result metrics do not reconstruct")
	}
	return nil
}

func canonicalNonnegativeDecimal(value string) bool {
	if value == "0" {
		return true
	}
	return canonicalPositiveDecimal(value)
}

func policyVersion(value, schema string) bool {
	return strings.HasPrefix(value, schema+"@sha256:") && digestPattern.MatchString(strings.TrimPrefix(value, schema+"@sha256:"))
}

func uuidText(value uuid.UUID) string {
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

func uuidTexts(values []uuid.UUID) []string {
	result := make([]string, len(values))
	for i, value := range values {
		if value == uuid.Nil {
			return nil
		}
		result[i] = value.String()
	}
	return result
}

func validUUIDs(values []uuid.UUID) bool {
	for _, value := range values {
		if value == uuid.Nil {
			return false
		}
	}
	return true
}

func validUUIDTexts(values []string) bool {
	for _, value := range values {
		if _, err := uuid.Parse(value); err != nil {
			return false
		}
	}
	return true
}

type AttemptEventType string

const (
	AttemptStarted   AttemptEventType = "started"
	AttemptCompleted AttemptEventType = "completed"
	AttemptFailed    AttemptEventType = "failed"
)

const (
	AttemptEventSchemaV1 = "experiment-attempt-event-v1"
	attemptEventDomain   = "experiment-attempt-event"
)

type AttemptEventInput struct {
	AttemptID   uuid.UUID
	Sequence    int
	Type        AttemptEventType
	OccurredAt  time.Time
	ResultID    uuid.UUID
	ErrorCode   string
	ErrorSHA256 string
}

type attemptEventCanonical struct {
	Schema      string           `json:"schema"`
	AttemptID   string           `json:"attempt_id"`
	Sequence    int              `json:"sequence"`
	Type        AttemptEventType `json:"type"`
	OccurredAt  string           `json:"occurred_at"`
	ResultID    string           `json:"result_id"`
	ErrorCode   string           `json:"error_code"`
	ErrorSHA256 string           `json:"error_sha256"`
}

type AttemptEvent struct {
	canonical attemptEventCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewAttemptEvent(value AttemptEventInput) (*AttemptEvent, error) {
	if value.AttemptID == uuid.Nil || value.Sequence < 0 || !canonicalTime(value.OccurredAt) {
		return nil, fmt.Errorf("experiment attempt event is invalid")
	}
	switch value.Type {
	case AttemptStarted:
		if value.Sequence != 0 || value.ResultID != uuid.Nil || value.ErrorCode != "" || value.ErrorSHA256 != "" {
			return nil, fmt.Errorf("started attempt event is invalid")
		}
	case AttemptCompleted:
		if value.Sequence != 1 || value.ResultID == uuid.Nil || value.ErrorCode != "" || value.ErrorSHA256 != "" {
			return nil, fmt.Errorf("completed attempt event is invalid")
		}
	case AttemptFailed:
		if value.Sequence != 1 || value.ResultID != uuid.Nil || !canonicalText(value.ErrorCode, 128) || !digestPattern.MatchString(value.ErrorSHA256) {
			return nil, fmt.Errorf("failed attempt event is invalid")
		}
	default:
		return nil, fmt.Errorf("attempt event type is invalid")
	}
	canonical := attemptEventCanonical{
		Schema: AttemptEventSchemaV1, AttemptID: value.AttemptID.String(), Sequence: value.Sequence,
		Type: value.Type, OccurredAt: formatTime(value.OccurredAt), ResultID: uuidText(value.ResultID),
		ErrorCode: value.ErrorCode, ErrorSHA256: value.ErrorSHA256,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	digest := hashBytes(encoded)
	return &AttemptEvent{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(attemptEventDomain, value.AttemptID.String(), fmt.Sprint(value.Sequence), string(value.Type), digest),
	}, nil
}

func AttemptEventFromCanonical(id uuid.UUID, digest string, raw []byte) (*AttemptEvent, error) {
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("attempt event envelope is invalid")
	}
	var canonical attemptEventCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	attemptID, err := uuid.Parse(canonical.AttemptID)
	if err != nil {
		return nil, err
	}
	var resultID uuid.UUID
	if canonical.ResultID != "" {
		resultID, err = uuid.Parse(canonical.ResultID)
		if err != nil {
			return nil, err
		}
	}
	event, err := NewAttemptEvent(AttemptEventInput{
		AttemptID: attemptID, Sequence: canonical.Sequence, Type: canonical.Type,
		OccurredAt: parseTime(canonical.OccurredAt), ResultID: resultID, ErrorCode: canonical.ErrorCode, ErrorSHA256: canonical.ErrorSHA256,
	})
	if err != nil {
		return nil, err
	}
	if canonical.Schema != AttemptEventSchemaV1 || event.ID() != id || event.Digest() != digest || !bytes.Equal(event.bytes, raw) {
		return nil, fmt.Errorf("attempt event identity does not reconstruct")
	}
	return event, nil
}

func (event *AttemptEvent) ID() uuid.UUID {
	if event == nil {
		return uuid.Nil
	}
	return event.id
}

func (event *AttemptEvent) Digest() string {
	if event == nil {
		return ""
	}
	return event.digest
}

func (event *AttemptEvent) CanonicalBytes() json.RawMessage {
	if event == nil {
		return nil
	}
	return append(json.RawMessage(nil), event.bytes...)
}

func (event *AttemptEvent) AttemptID() uuid.UUID {
	if event == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(event.canonical.AttemptID)
	return id
}

func (event *AttemptEvent) Sequence() int {
	if event == nil {
		return -1
	}
	return event.canonical.Sequence
}

func (event *AttemptEvent) Type() AttemptEventType {
	if event == nil {
		return ""
	}
	return event.canonical.Type
}

func (event *AttemptEvent) ResultID() uuid.UUID {
	if event == nil || event.canonical.ResultID == "" {
		return uuid.Nil
	}
	id, _ := uuid.Parse(event.canonical.ResultID)
	return id
}

func ValidateAttempt(events []*AttemptEvent) error {
	if len(events) != 2 || events[0] == nil || events[1] == nil || events[0].Sequence() != 0 || events[0].Type() != AttemptStarted ||
		events[1].Sequence() != 1 || (events[1].Type() != AttemptCompleted && events[1].Type() != AttemptFailed) ||
		events[0].AttemptID() != events[1].AttemptID() {
		return fmt.Errorf("experiment attempt lifecycle is incomplete or invalid")
	}
	return nil
}
