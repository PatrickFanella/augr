package wheel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type EventKind string

const (
	EventAssessQuality EventKind = "assess_quality"
	EventOpenPut       EventKind = "open_put"
	EventOpenCall      EventKind = "open_call"
	EventMark          EventKind = "mark"
	EventCloseOption   EventKind = "close_option"
	EventAssignment    EventKind = "assignment"
	EventExpiry        EventKind = "expiry"
	EventDividend      EventKind = "dividend"
)

type QualityEvidence struct {
	AvailableAt    time.Time
	ROIC           string
	DebtToAssets   string
	FreeCashFlow   string
	EvidenceID     uuid.UUID
	EvidenceSHA256 string
}

type Candidate struct {
	InstrumentID           uuid.UUID
	VenueContractID        uuid.UUID
	PartitionContentSHA256 string
	SourceKey              string
	OptionType             string
	Strike                 string
	Expiry                 time.Time
	Delta                  string
	Bid                    string
	Ask                    string
	OpenInterest           string
	Volume                 string
	AvailableAt            time.Time
	EvidenceID             uuid.UUID
	EvidenceSHA256         string
}

type EventInput struct {
	Kind               EventKind
	OccurredAt         time.Time
	UnderlyingMark     string
	Quality            *QualityEvidence
	Candidates         []Candidate
	OptionMarkAsk      *string
	DividendPerShare   *string
	AssignmentOptionID uuid.UUID
	EvidenceID         uuid.UUID
	EvidenceSHA256     string
}

type ScenarioInput struct {
	Policy          *Policy
	UnderlyingID    uuid.UUID
	InitialCapital  string
	EvaluationStart time.Time
	EvaluationEnd   time.Time
	Mode            strategycatalog.ExperimentMode
	Events          []EventInput
}

type (
	qualityCanonical   struct{ AvailableAt, ROIC, DebtToAssets, FreeCashFlow, EvidenceID, EvidenceSHA256 string }
	candidateCanonical struct {
		InstrumentID           string `json:"instrument_id"`
		VenueContractID        string `json:"venue_contract_id"`
		PartitionContentSHA256 string `json:"partition_content_sha256"`
		SourceKey              string `json:"source_key"`
		OptionType             string `json:"option_type"`
		Strike                 string `json:"strike"`
		Expiry                 string `json:"expiry"`
		Delta                  string `json:"delta"`
		Bid                    string `json:"bid"`
		Ask                    string `json:"ask"`
		OpenInterest           string `json:"open_interest"`
		Volume                 string `json:"volume"`
		AvailableAt            string `json:"available_at"`
		EvidenceID             string `json:"evidence_id"`
		EvidenceSHA256         string `json:"evidence_sha256"`
	}
)

type eventCanonical struct {
	Sequence           int                  `json:"sequence"`
	Kind               EventKind            `json:"kind"`
	OccurredAt         string               `json:"occurred_at"`
	UnderlyingMark     string               `json:"underlying_mark"`
	Quality            *qualityCanonical    `json:"quality"`
	Candidates         []candidateCanonical `json:"candidates"`
	OptionMarkAsk      *string              `json:"option_mark_ask"`
	DividendPerShare   *string              `json:"dividend_per_share"`
	AssignmentOptionID string               `json:"assignment_option_id"`
	EvidenceID         string               `json:"evidence_id"`
	EvidenceSHA256     string               `json:"evidence_sha256"`
}
type scenarioCanonical struct {
	Schema          string                         `json:"schema"`
	State           string                         `json:"state"`
	PolicyID        string                         `json:"policy_id"`
	PolicySHA256    string                         `json:"policy_sha256"`
	UnderlyingID    string                         `json:"underlying_id"`
	InitialCapital  string                         `json:"initial_capital"`
	EvaluationStart string                         `json:"evaluation_start"`
	EvaluationEnd   string                         `json:"evaluation_end"`
	Mode            strategycatalog.ExperimentMode `json:"mode"`
	Events          []eventCanonical               `json:"events"`
}
type Scenario struct {
	canonical scenarioCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewScenario(input ScenarioInput) (*Scenario, error) {
	if input.Policy == nil || input.UnderlyingID == uuid.Nil || !positive(input.InitialCapital) || !canonicalTime(input.EvaluationStart) || !canonicalTime(input.EvaluationEnd) || !input.EvaluationStart.Before(input.EvaluationEnd) || (input.Mode != strategycatalog.ExperimentPaperScored && input.Mode != strategycatalog.ExperimentPaperStress) || len(input.Events) == 0 || len(input.Events) > 100000 {
		return nil, fmt.Errorf("wheel scenario is invalid")
	}
	events := make([]eventCanonical, len(input.Events))
	last := time.Time{}
	for i, event := range input.Events {
		if !canonicalTime(event.OccurredAt) || event.OccurredAt.Before(input.EvaluationStart) || event.OccurredAt.After(input.EvaluationEnd) || !last.IsZero() && !event.OccurredAt.After(last) || !positive(event.UnderlyingMark) || event.EvidenceID == uuid.Nil || !digestPattern.MatchString(event.EvidenceSHA256) {
			return nil, fmt.Errorf("wheel event %d is invalid", i)
		}
		canonicalEvent, err := canonicalizeEvent(i, event, input.Policy)
		if err != nil {
			return nil, fmt.Errorf("wheel event %d: %w", i, err)
		}
		events[i] = canonicalEvent
		last = event.OccurredAt
	}
	if !input.Events[0].OccurredAt.Equal(input.EvaluationStart) || !input.Events[len(input.Events)-1].OccurredAt.Equal(input.EvaluationEnd) {
		return nil, fmt.Errorf("wheel events do not span evaluation window")
	}
	canonical := scenarioCanonical{Schema: ScenarioSchemaV1, State: "declared", PolicyID: input.Policy.ID().String(), PolicySHA256: input.Policy.Digest(), UnderlyingID: input.UnderlyingID.String(), InitialCapital: input.InitialCapital, EvaluationStart: formatTime(input.EvaluationStart), EvaluationEnd: formatTime(input.EvaluationEnd), Mode: input.Mode, Events: events}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Scenario{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("quality-filtered-wheel-scenario", ScenarioSchemaV1+"@sha256:"+digest)}, nil
}

func canonicalizeEvent(sequence int, event EventInput, policy *Policy) (eventCanonical, error) {
	value := eventCanonical{Sequence: sequence, Kind: event.Kind, OccurredAt: formatTime(event.OccurredAt), UnderlyingMark: event.UnderlyingMark, OptionMarkAsk: cloneString(event.OptionMarkAsk), DividendPerShare: cloneString(event.DividendPerShare), AssignmentOptionID: "", EvidenceID: event.EvidenceID.String(), EvidenceSHA256: event.EvidenceSHA256}
	if event.AssignmentOptionID != uuid.Nil {
		value.AssignmentOptionID = event.AssignmentOptionID.String()
	}
	if event.Quality != nil {
		quality := event.Quality
		if !canonicalTime(quality.AvailableAt) || quality.AvailableAt.After(event.OccurredAt) || !ratio(quality.ROIC) || !ratio(quality.DebtToAssets) || !signed(quality.FreeCashFlow) || quality.EvidenceID == uuid.Nil || !digestPattern.MatchString(quality.EvidenceSHA256) {
			return eventCanonical{}, fmt.Errorf("quality evidence is invalid")
		}
		value.Quality = &qualityCanonical{AvailableAt: formatTime(quality.AvailableAt), ROIC: quality.ROIC, DebtToAssets: quality.DebtToAssets, FreeCashFlow: quality.FreeCashFlow, EvidenceID: quality.EvidenceID.String(), EvidenceSHA256: quality.EvidenceSHA256}
	}
	candidates := append([]Candidate(nil), event.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidateKey(candidates[i]) < candidateKey(candidates[j]) })
	value.Candidates = make([]candidateCanonical, len(candidates))
	for i, candidate := range candidates {
		normalized, err := canonicalizeCandidate(candidate, event.OccurredAt, policy)
		if err != nil {
			return eventCanonical{}, err
		}
		if i > 0 && candidateKey(candidates[i-1]) == candidateKey(candidate) {
			return eventCanonical{}, fmt.Errorf("candidate is duplicated")
		}
		value.Candidates[i] = normalized
	}
	switch event.Kind {
	case EventAssessQuality:
		if value.Quality == nil || len(candidates) > 0 || event.OptionMarkAsk != nil || event.DividendPerShare != nil || event.AssignmentOptionID != uuid.Nil {
			return eventCanonical{}, fmt.Errorf("quality event payload is invalid")
		}
	case EventOpenPut, EventOpenCall:
		if value.Quality != nil || len(candidates) == 0 || event.OptionMarkAsk != nil || event.DividendPerShare != nil || event.AssignmentOptionID != uuid.Nil {
			return eventCanonical{}, fmt.Errorf("opening event payload is invalid")
		}
	case EventMark, EventCloseOption:
		if value.Quality != nil || len(candidates) > 0 || event.OptionMarkAsk == nil || !positive(*event.OptionMarkAsk) || event.DividendPerShare != nil || event.AssignmentOptionID != uuid.Nil {
			return eventCanonical{}, fmt.Errorf("option mark event payload is invalid")
		}
	case EventAssignment:
		if value.Quality != nil || len(candidates) > 0 || event.OptionMarkAsk != nil || event.DividendPerShare != nil || event.AssignmentOptionID == uuid.Nil {
			return eventCanonical{}, fmt.Errorf("assignment event payload is invalid")
		}
	case EventExpiry:
		if value.Quality != nil || len(candidates) > 0 || event.OptionMarkAsk != nil || event.DividendPerShare != nil || event.AssignmentOptionID != uuid.Nil {
			return eventCanonical{}, fmt.Errorf("expiry event payload is invalid")
		}
	case EventDividend:
		if value.Quality != nil || len(candidates) > 0 || event.OptionMarkAsk != nil || event.DividendPerShare == nil || !positive(*event.DividendPerShare) || event.AssignmentOptionID != uuid.Nil {
			return eventCanonical{}, fmt.Errorf("dividend event payload is invalid")
		}
	default:
		return eventCanonical{}, fmt.Errorf("event kind is invalid")
	}
	return value, nil
}

func canonicalizeCandidate(value Candidate, decisionAt time.Time, policy *Policy) (candidateCanonical, error) {
	if value.InstrumentID == uuid.Nil || value.VenueContractID == uuid.Nil || !digestPattern.MatchString(value.PartitionContentSHA256) || value.SourceKey == "" || (value.OptionType != "put" && value.OptionType != "call") || !positive(value.Strike) || !canonicalTime(value.Expiry) || !value.Expiry.After(decisionAt) || !signed(value.Delta) || !positive(value.Bid) || !positive(value.Ask) || decimal.RequireFromString(value.Ask).LessThan(decimal.RequireFromString(value.Bid)) || !nonnegative(value.OpenInterest) || !nonnegative(value.Volume) || !canonicalTime(value.AvailableAt) || value.AvailableAt.After(decisionAt) || value.EvidenceID == uuid.Nil || !digestPattern.MatchString(value.EvidenceSHA256) {
		return candidateCanonical{}, fmt.Errorf("option candidate is invalid")
	}
	delta := decimal.RequireFromString(value.Delta)
	if delta.Abs().GreaterThan(decimal.NewFromInt(1)) || value.OptionType == "put" && !delta.IsNegative() || value.OptionType == "call" && !delta.IsPositive() {
		return candidateCanonical{}, fmt.Errorf("option candidate delta sign is invalid")
	}
	spread := decimal.RequireFromString(value.Ask).Sub(decimal.RequireFromString(value.Bid)).Div(decimal.RequireFromString(value.Ask).Add(decimal.RequireFromString(value.Bid)).Div(decimal.NewFromInt(2)))
	if spread.GreaterThan(decimal.RequireFromString(policy.canonical.MaximumSpreadRatio)) {
		return candidateCanonical{}, fmt.Errorf("option candidate spread exceeds policy")
	}
	return candidateCanonical{InstrumentID: value.InstrumentID.String(), VenueContractID: value.VenueContractID.String(), PartitionContentSHA256: value.PartitionContentSHA256, SourceKey: value.SourceKey, OptionType: value.OptionType, Strike: value.Strike, Expiry: formatTime(value.Expiry), Delta: value.Delta, Bid: value.Bid, Ask: value.Ask, OpenInterest: value.OpenInterest, Volume: value.Volume, AvailableAt: formatTime(value.AvailableAt), EvidenceID: value.EvidenceID.String(), EvidenceSHA256: value.EvidenceSHA256}, nil
}

func candidateKey(value Candidate) string {
	return value.InstrumentID.String() + "\x00" + value.VenueContractID.String() + "\x00" + formatTime(value.Expiry) + "\x00" + value.Strike
}

func (s *Scenario) ID() uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	return s.id
}

func (s *Scenario) Digest() string {
	if s == nil {
		return ""
	}
	return s.digest
}

func (s *Scenario) CanonicalBytes() json.RawMessage {
	if s == nil {
		return nil
	}
	return append(json.RawMessage(nil), s.bytes...)
}

func (s *Scenario) PolicyID() uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	return uuid.MustParse(s.canonical.PolicyID)
}

func (s *Scenario) PolicyDigest() string {
	if s == nil {
		return ""
	}
	return s.canonical.PolicySHA256
}

func (s *Scenario) UnderlyingID() uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	return uuid.MustParse(s.canonical.UnderlyingID)
}

func (s *Scenario) InitialCapital() string {
	if s == nil {
		return ""
	}
	return s.canonical.InitialCapital
}

func (s *Scenario) EvaluationStart() time.Time {
	if s == nil {
		return time.Time{}
	}
	return parseTime(s.canonical.EvaluationStart)
}

func (s *Scenario) EvaluationEnd() time.Time {
	if s == nil {
		return time.Time{}
	}
	return parseTime(s.canonical.EvaluationEnd)
}

func (s *Scenario) Mode() strategycatalog.ExperimentMode {
	if s == nil {
		return ""
	}
	return s.canonical.Mode
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func ScenarioFromCanonical(id uuid.UUID, digest string, raw []byte, policy *Policy) (*Scenario, error) {
	var canonical scenarioCanonical
	if id == uuid.Nil || policy == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("wheel scenario envelope is invalid")
	}
	events := make([]EventInput, len(canonical.Events))
	for i, event := range canonical.Events {
		quality := qualityInput(event.Quality)
		candidates := make([]Candidate, len(event.Candidates))
		for j, candidate := range event.Candidates {
			candidates[j] = candidateInput(candidate)
		}
		assignmentID := uuid.Nil
		if event.AssignmentOptionID != "" {
			assignmentID = uuid.MustParse(event.AssignmentOptionID)
		}
		events[i] = EventInput{Kind: event.Kind, OccurredAt: parseTime(event.OccurredAt), UnderlyingMark: event.UnderlyingMark, Quality: quality, Candidates: candidates, OptionMarkAsk: cloneString(event.OptionMarkAsk), DividendPerShare: cloneString(event.DividendPerShare), AssignmentOptionID: assignmentID, EvidenceID: uuid.MustParse(event.EvidenceID), EvidenceSHA256: event.EvidenceSHA256}
	}
	value, err := NewScenario(ScenarioInput{Policy: policy, UnderlyingID: uuid.MustParse(canonical.UnderlyingID), InitialCapital: canonical.InitialCapital, EvaluationStart: parseTime(canonical.EvaluationStart), EvaluationEnd: parseTime(canonical.EvaluationEnd), Mode: canonical.Mode, Events: events})
	if err != nil || canonical.Schema != ScenarioSchemaV1 || canonical.State != "declared" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("wheel scenario identity does not reconstruct")
	}
	return value, nil
}

func qualityInput(value *qualityCanonical) *QualityEvidence {
	if value == nil {
		return nil
	}
	return &QualityEvidence{AvailableAt: parseTime(value.AvailableAt), ROIC: value.ROIC, DebtToAssets: value.DebtToAssets, FreeCashFlow: value.FreeCashFlow, EvidenceID: uuid.MustParse(value.EvidenceID), EvidenceSHA256: value.EvidenceSHA256}
}

func candidateInput(value candidateCanonical) Candidate {
	return Candidate{InstrumentID: uuid.MustParse(value.InstrumentID), VenueContractID: uuid.MustParse(value.VenueContractID), PartitionContentSHA256: value.PartitionContentSHA256, SourceKey: value.SourceKey, OptionType: value.OptionType, Strike: value.Strike, Expiry: parseTime(value.Expiry), Delta: value.Delta, Bid: value.Bid, Ask: value.Ask, OpenInterest: value.OpenInterest, Volume: value.Volume, AvailableAt: parseTime(value.AvailableAt), EvidenceID: uuid.MustParse(value.EvidenceID), EvidenceSHA256: value.EvidenceSHA256}
}
