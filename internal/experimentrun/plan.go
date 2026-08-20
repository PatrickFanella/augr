package experimentrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	PlanSchemaV1 = "experiment-replay-plan-v1"
	planDomain   = "experiment-replay-plan"
	timeLayout   = "2006-01-02T15:04:05.000000Z"
)

type StepAction string

const (
	ActionNoop     StepAction = "noop"
	ActionRejected StepAction = "rejected"
	ActionExecute  StepAction = "execute"
)

type IntentSpecInput struct {
	InstrumentID    uuid.UUID
	VenueContractID uuid.UUID
	Side            string
	OrderType       string
	TimeInForce     string
	Quantity        string
	LimitPrice      *string
	StopPrice       *string
	DecisionAt      time.Time
	RouteAt         time.Time
}

type StepInput struct {
	PartitionContentSHA256   string
	ObservationSourceKey     string
	ObservationContentSHA256 string
	AvailableAt              time.Time
	Decision                 json.RawMessage
	Action                   StepAction
	RejectionCode            string
	Intent                   *IntentSpecInput
}

type PlanInput struct {
	ExperimentID    uuid.UUID
	ProgramID       uuid.UUID
	ManifestID      uuid.UUID
	ManifestSHA256  string
	EvaluationStart time.Time
	EvaluationEnd   time.Time
	Seed            int64
	Mode            strategycatalog.ExperimentMode
	Steps           []StepInput
}

type intentCanonical struct {
	InstrumentID    string  `json:"instrument_id"`
	VenueContractID string  `json:"venue_contract_id"`
	Side            string  `json:"side"`
	OrderType       string  `json:"order_type"`
	TimeInForce     string  `json:"time_in_force"`
	Quantity        string  `json:"quantity"`
	LimitPrice      *string `json:"limit_price"`
	StopPrice       *string `json:"stop_price"`
	DecisionAt      string  `json:"decision_at"`
	RouteAt         string  `json:"route_at"`
}
type stepCanonical struct {
	Sequence                 int              `json:"sequence"`
	PartitionContentSHA256   string           `json:"partition_content_sha256"`
	ObservationSourceKey     string           `json:"observation_source_key"`
	ObservationContentSHA256 string           `json:"observation_content_sha256"`
	AvailableAt              string           `json:"available_at"`
	Decision                 json.RawMessage  `json:"decision"`
	Action                   StepAction       `json:"action"`
	RejectionCode            string           `json:"rejection_code"`
	Intent                   *intentCanonical `json:"intent"`
}
type planCanonical struct {
	Schema          string                         `json:"schema"`
	ExperimentID    string                         `json:"experiment_id"`
	ProgramID       string                         `json:"program_id"`
	ManifestID      string                         `json:"manifest_id"`
	ManifestSHA256  string                         `json:"manifest_sha256"`
	EvaluationStart string                         `json:"evaluation_start"`
	EvaluationEnd   string                         `json:"evaluation_end"`
	Seed            int64                          `json:"seed"`
	Mode            strategycatalog.ExperimentMode `json:"mode"`
	Steps           []stepCanonical                `json:"steps"`
}

type Plan struct {
	canonical planCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

var decimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

func NewPlan(input PlanInput) (*Plan, error) {
	if input.ExperimentID == uuid.Nil || input.ProgramID == uuid.Nil || input.ManifestID == uuid.Nil ||
		!digestPattern.MatchString(input.ManifestSHA256) || !canonicalTime(input.EvaluationStart) ||
		!canonicalTime(input.EvaluationEnd) || !input.EvaluationStart.Before(input.EvaluationEnd) ||
		(input.Mode != strategycatalog.ExperimentPaperScored && input.Mode != strategycatalog.ExperimentPaperStress) ||
		len(input.Steps) == 0 || len(input.Steps) > 100000 {
		return nil, fmt.Errorf("experiment replay plan identity is invalid")
	}
	steps := make([]stepCanonical, len(input.Steps))
	seen := make(map[string]struct{}, len(input.Steps))
	for index, source := range input.Steps {
		decision, err := canonicalJSONObject(source.Decision)
		key := source.PartitionContentSHA256 + "\x00" + source.ObservationSourceKey + "\x00" + source.ObservationContentSHA256
		if err != nil || !digestPattern.MatchString(source.PartitionContentSHA256) || !canonicalText(source.ObservationSourceKey, 512) ||
			!digestPattern.MatchString(source.ObservationContentSHA256) || !canonicalTime(source.AvailableAt) ||
			source.AvailableAt.Before(input.EvaluationStart) || source.AvailableAt.After(input.EvaluationEnd) {
			return nil, fmt.Errorf("experiment replay step %d evidence is invalid", index)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("experiment replay step evidence is duplicated")
		}
		seen[key] = struct{}{}
		step := stepCanonical{
			Sequence: index, PartitionContentSHA256: source.PartitionContentSHA256,
			ObservationSourceKey: source.ObservationSourceKey, ObservationContentSHA256: source.ObservationContentSHA256,
			AvailableAt: formatTime(source.AvailableAt), Decision: decision, Action: source.Action, RejectionCode: source.RejectionCode,
		}
		switch source.Action {
		case ActionNoop:
			if source.Intent != nil || source.RejectionCode != "" {
				return nil, fmt.Errorf("noop step carries execution evidence")
			}
		case ActionRejected:
			if source.Intent != nil || !canonicalText(source.RejectionCode, 128) {
				return nil, fmt.Errorf("rejected step is invalid")
			}
		case ActionExecute:
			if source.RejectionCode != "" {
				return nil, fmt.Errorf("executable step carries rejection")
			}
			step.Intent, err = canonicalIntent(source.Intent, input.EvaluationStart, input.EvaluationEnd)
			if err != nil {
				return nil, fmt.Errorf("experiment replay step %d: %w", index, err)
			}
		default:
			return nil, fmt.Errorf("experiment replay step action is invalid")
		}
		steps[index] = step
	}
	canonical := planCanonical{
		Schema: PlanSchemaV1, ExperimentID: input.ExperimentID.String(), ProgramID: input.ProgramID.String(),
		ManifestID: input.ManifestID.String(), ManifestSHA256: input.ManifestSHA256, EvaluationStart: formatTime(input.EvaluationStart),
		EvaluationEnd: formatTime(input.EvaluationEnd), Seed: input.Seed, Mode: input.Mode, Steps: steps,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	digest := hashBytes(encoded)
	return &Plan{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID(planDomain, PlanSchemaV1+"@sha256:"+digest)}, nil
}

func canonicalIntent(input *IntentSpecInput, start, end time.Time) (*intentCanonical, error) {
	if input == nil || input.InstrumentID == uuid.Nil || input.VenueContractID == uuid.Nil ||
		(input.Side != "buy" && input.Side != "sell") || (input.OrderType != "market" && input.OrderType != "limit") ||
		!stringIn(input.TimeInForce, "day", "gtc", "ioc", "fok") || !canonicalPositiveDecimal(input.Quantity) ||
		!canonicalTime(input.DecisionAt) || !canonicalTime(input.RouteAt) || input.DecisionAt.Before(start) || input.RouteAt.Before(input.DecisionAt) || input.RouteAt.After(end) {
		return nil, fmt.Errorf("intent specification is invalid")
	}
	if input.OrderType == "market" && input.LimitPrice != nil || input.OrderType == "limit" && (input.LimitPrice == nil || !canonicalPositiveDecimal(*input.LimitPrice)) ||
		input.StopPrice != nil {
		return nil, fmt.Errorf("intent price specification is invalid")
	}
	return &intentCanonical{
		InstrumentID: input.InstrumentID.String(), VenueContractID: input.VenueContractID.String(), Side: input.Side,
		OrderType: input.OrderType, TimeInForce: input.TimeInForce, Quantity: input.Quantity, LimitPrice: cloneString(input.LimitPrice),
		StopPrice: cloneString(input.StopPrice), DecisionAt: formatTime(input.DecisionAt), RouteAt: formatTime(input.RouteAt),
	}, nil
}

func PlanFromCanonical(id uuid.UUID, digest string, raw []byte) (*Plan, error) {
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("experiment replay plan envelope is invalid")
	}
	var c planCanonical
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(d); err != nil {
		return nil, err
	}
	experimentID, err := uuid.Parse(c.ExperimentID)
	if err != nil {
		return nil, err
	}
	programID, err := uuid.Parse(c.ProgramID)
	if err != nil {
		return nil, err
	}
	manifestID, err := uuid.Parse(c.ManifestID)
	if err != nil {
		return nil, err
	}
	inputs := make([]StepInput, len(c.Steps))
	for i, s := range c.Steps {
		if s.Sequence != i {
			return nil, fmt.Errorf("experiment replay step sequence is invalid")
		}
		available := parseTime(s.AvailableAt)
		var intent *IntentSpecInput
		if s.Intent != nil {
			instrumentID, e := uuid.Parse(s.Intent.InstrumentID)
			if e != nil {
				return nil, e
			}
			contractID, e := uuid.Parse(s.Intent.VenueContractID)
			if e != nil {
				return nil, e
			}
			intent = &IntentSpecInput{InstrumentID: instrumentID, VenueContractID: contractID, Side: s.Intent.Side, OrderType: s.Intent.OrderType, TimeInForce: s.Intent.TimeInForce, Quantity: s.Intent.Quantity, LimitPrice: cloneString(s.Intent.LimitPrice), StopPrice: cloneString(s.Intent.StopPrice), DecisionAt: parseTime(s.Intent.DecisionAt), RouteAt: parseTime(s.Intent.RouteAt)}
		}
		inputs[i] = StepInput{PartitionContentSHA256: s.PartitionContentSHA256, ObservationSourceKey: s.ObservationSourceKey, ObservationContentSHA256: s.ObservationContentSHA256, AvailableAt: available, Decision: s.Decision, Action: s.Action, RejectionCode: s.RejectionCode, Intent: intent}
	}
	plan, err := NewPlan(PlanInput{ExperimentID: experimentID, ProgramID: programID, ManifestID: manifestID, ManifestSHA256: c.ManifestSHA256, EvaluationStart: parseTime(c.EvaluationStart), EvaluationEnd: parseTime(c.EvaluationEnd), Seed: c.Seed, Mode: c.Mode, Steps: inputs})
	if err != nil {
		return nil, err
	}
	if c.Schema != PlanSchemaV1 || plan.ID() != id || plan.Digest() != digest || !bytes.Equal(plan.bytes, raw) {
		return nil, fmt.Errorf("experiment replay plan identity does not reconstruct")
	}
	return plan, nil
}

func (p *Plan) ID() uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return p.id
}

func (p *Plan) Digest() string {
	if p == nil {
		return ""
	}
	return p.digest
}

func (p *Plan) CanonicalBytes() json.RawMessage {
	if p == nil {
		return nil
	}
	return append(json.RawMessage(nil), p.bytes...)
}

func (p *Plan) ExperimentID() uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(p.canonical.ExperimentID)
	return id
}

func (p *Plan) ProgramID() uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(p.canonical.ProgramID)
	return id
}

func (p *Plan) ManifestID() uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(p.canonical.ManifestID)
	return id
}

func (p *Plan) ManifestSHA256() string {
	if p == nil {
		return ""
	}
	return p.canonical.ManifestSHA256
}

func (p *Plan) Mode() strategycatalog.ExperimentMode {
	if p == nil {
		return ""
	}
	return p.canonical.Mode
}

func (p *Plan) StepCount() int {
	if p == nil {
		return 0
	}
	return len(p.canonical.Steps)
}

func (p *Plan) IntentID(sequence int) uuid.UUID {
	if p == nil || sequence < 0 || sequence >= len(p.canonical.Steps) || p.canonical.Steps[sequence].Intent == nil {
		return uuid.Nil
	}
	return economicid.DeterministicUUID("experiment-intent", p.id.String(), fmt.Sprint(sequence), hashBytes(p.canonical.Steps[sequence].Decision))
}

func (p *Plan) OrderID(sequence int) uuid.UUID {
	intent := p.IntentID(sequence)
	if intent == uuid.Nil {
		return uuid.Nil
	}
	return economicid.DeterministicUUID("experiment-order", intent.String())
}

func canonicalJSONObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, fmt.Errorf("canonical JSON object is required")
	}
	var value map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("canonical JSON object is invalid")
	}
	if err := requireJSONEOF(d); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil || !bytes.Equal(encoded, raw) {
		return nil, fmt.Errorf("JSON object is not canonical")
	}
	return append(json.RawMessage(nil), encoded...), nil
}

func canonicalTime(v time.Time) bool {
	return !v.IsZero() && v.Location() == time.UTC && v.Nanosecond()%1000 == 0
}
func formatTime(v time.Time) string { return v.Format(timeLayout) }
func parseTime(v string) time.Time  { parsed, _ := time.Parse(timeLayout, v); return parsed }
func canonicalPositiveDecimal(v string) bool {
	if !decimalPattern.MatchString(v) {
		return false
	}
	parsed, err := decimal.NewFromString(v)
	return err == nil && parsed.IsPositive() && parsed.String() == v
}

func stringIn(value string, values ...string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func cloneString(v *string) *string {
	if v == nil {
		return nil
	}
	result := *v
	return &result
}
