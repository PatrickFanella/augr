package definedrisk

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

type Strategy string

const (
	BullCall Strategy = "bull_call"
	BearPut  Strategy = "bear_put"
	BullPut  Strategy = "bull_put"
	BearCall Strategy = "bear_call"
)

type QuoteInput struct {
	Bid, Ask, BidSize, AskSize string
	AvailableAt                time.Time
	EvidenceID                 uuid.UUID
	EvidenceSHA256             string
	PartitionContentSHA256     string
	SourceKey                  string
}

type LegInput struct {
	InstrumentID, VenueContractID uuid.UUID
	OCCSymbol, Underlying         string
	OptionType                    string
	Strike                        string
	Expiry                        time.Time
	Multiplier                    string
	Style                         string
	Position                      string
	Entry                         QuoteInput
	Unwind                        *QuoteInput
}

type ScenarioInput struct {
	Policy                 *Policy
	Strategy               Strategy
	InitialCapital         string
	RequestedContracts     int
	DecisionAt, ExpiryAt   time.Time
	TerminalUnderlying     string
	TerminalAvailableAt    time.Time
	TerminalEvidenceID     uuid.UUID
	TerminalEvidenceSHA256 string
	Mode                   strategycatalog.ExperimentMode
	Legs                   []LegInput
}

type quoteCanonical struct {
	Bid                    string `json:"bid"`
	Ask                    string `json:"ask"`
	BidSize                string `json:"bid_size"`
	AskSize                string `json:"ask_size"`
	AvailableAt            string `json:"available_at"`
	EvidenceID             string `json:"evidence_id"`
	EvidenceSHA256         string `json:"evidence_sha256"`
	PartitionContentSHA256 string `json:"partition_content_sha256"`
	SourceKey              string `json:"source_key"`
}
type legCanonical struct {
	InstrumentID    string          `json:"instrument_id"`
	VenueContractID string          `json:"venue_contract_id"`
	OCCSymbol       string          `json:"occ_symbol"`
	Underlying      string          `json:"underlying"`
	OptionType      string          `json:"option_type"`
	Strike          string          `json:"strike"`
	Expiry          string          `json:"expiry"`
	Multiplier      string          `json:"multiplier"`
	Style           string          `json:"style"`
	Position        string          `json:"position"`
	Entry           quoteCanonical  `json:"entry"`
	Unwind          *quoteCanonical `json:"unwind"`
}
type scenarioCanonical struct {
	Schema                 string                         `json:"schema"`
	State                  string                         `json:"state"`
	PolicyID               string                         `json:"policy_id"`
	PolicySHA256           string                         `json:"policy_sha256"`
	Strategy               Strategy                       `json:"strategy"`
	InitialCapital         string                         `json:"initial_capital"`
	RequestedContracts     int                            `json:"requested_contracts"`
	DecisionAt             string                         `json:"decision_at"`
	ExpiryAt               string                         `json:"expiry_at"`
	TerminalUnderlying     string                         `json:"terminal_underlying"`
	TerminalAvailableAt    string                         `json:"terminal_available_at"`
	TerminalEvidenceID     string                         `json:"terminal_evidence_id"`
	TerminalEvidenceSHA256 string                         `json:"terminal_evidence_sha256"`
	Mode                   strategycatalog.ExperimentMode `json:"mode"`
	Legs                   []legCanonical                 `json:"legs"`
}

type Scenario struct {
	canonical scenarioCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewScenario(input ScenarioInput) (*Scenario, error) {
	if input.Policy == nil || !supported(input.Strategy) || !positive(input.InitialCapital) || input.RequestedContracts < 1 || input.RequestedContracts > input.Policy.canonical.MaximumContracts || !canonicalTime(input.DecisionAt) || !canonicalTime(input.ExpiryAt) || !input.DecisionAt.Before(input.ExpiryAt) || !positive(input.TerminalUnderlying) || !canonicalTime(input.TerminalAvailableAt) || input.TerminalAvailableAt.After(input.ExpiryAt) || input.ExpiryAt.Sub(input.TerminalAvailableAt) > time.Duration(input.Policy.canonical.MaximumEvidenceAgeSeconds)*time.Second || input.TerminalEvidenceID == uuid.Nil || !digestPattern.MatchString(input.TerminalEvidenceSHA256) || input.Mode != strategycatalog.ExperimentPaperScored && input.Mode != strategycatalog.ExperimentPaperStress || len(input.Legs) != 2 {
		return nil, fmt.Errorf("defined-risk scenario is invalid")
	}
	legs := append([]LegInput(nil), input.Legs...)
	for _, leg := range legs {
		if !positive(leg.Strike) {
			return nil, fmt.Errorf("defined-risk leg strike is invalid")
		}
	}
	sort.Slice(legs, func(i, j int) bool {
		return decimal.RequireFromString(legs[i].Strike).LessThan(decimal.RequireFromString(legs[j].Strike))
	})
	canonicalLegs := make([]legCanonical, 2)
	for i, leg := range legs {
		value, err := canonicalizeLeg(leg, input.DecisionAt, input.Policy)
		if err != nil {
			return nil, fmt.Errorf("defined-risk leg %d: %w", i, err)
		}
		canonicalLegs[i] = value
	}
	if err := validateStructure(input.Strategy, canonicalLegs, input.ExpiryAt); err != nil {
		return nil, err
	}
	canonical := scenarioCanonical{Schema: ScenarioSchemaV1, State: "declared", PolicyID: input.Policy.ID().String(), PolicySHA256: input.Policy.Digest(), Strategy: input.Strategy, InitialCapital: input.InitialCapital, RequestedContracts: input.RequestedContracts, DecisionAt: formatTime(input.DecisionAt), ExpiryAt: formatTime(input.ExpiryAt), TerminalUnderlying: input.TerminalUnderlying, TerminalAvailableAt: formatTime(input.TerminalAvailableAt), TerminalEvidenceID: input.TerminalEvidenceID.String(), TerminalEvidenceSHA256: input.TerminalEvidenceSHA256, Mode: input.Mode, Legs: canonicalLegs}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Scenario{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("defined-risk-options-scenario", ScenarioSchemaV1+"@sha256:"+digest)}, nil
}

func canonicalizeLeg(leg LegInput, decisionAt time.Time, policy *Policy) (legCanonical, error) {
	if leg.InstrumentID == uuid.Nil || leg.VenueContractID == uuid.Nil || leg.OCCSymbol == "" || leg.Underlying == "" || leg.OptionType != "call" && leg.OptionType != "put" || !positive(leg.Strike) || !canonicalTime(leg.Expiry) || !leg.Expiry.After(decisionAt) || !positive(leg.Multiplier) || leg.Style != "european" || leg.Position != "long" && leg.Position != "short" {
		return legCanonical{}, fmt.Errorf("contract is invalid")
	}
	entry, err := canonicalizeQuote(leg.Entry, decisionAt, policy)
	if err != nil {
		return legCanonical{}, fmt.Errorf("entry quote: %w", err)
	}
	var unwind *quoteCanonical
	if leg.Unwind != nil {
		value, err := canonicalizeQuote(*leg.Unwind, decisionAt, policy)
		if err != nil {
			return legCanonical{}, fmt.Errorf("unwind quote: %w", err)
		}
		unwind = &value
	}
	return legCanonical{leg.InstrumentID.String(), leg.VenueContractID.String(), leg.OCCSymbol, leg.Underlying, leg.OptionType, leg.Strike, formatTime(leg.Expiry), leg.Multiplier, leg.Style, leg.Position, entry, unwind}, nil
}

func canonicalizeQuote(q QuoteInput, at time.Time, policy *Policy) (quoteCanonical, error) {
	if !positive(q.Bid) || !positive(q.Ask) || decimal.RequireFromString(q.Ask).LessThan(decimal.RequireFromString(q.Bid)) || !nonnegative(q.BidSize) || !nonnegative(q.AskSize) || !canonicalTime(q.AvailableAt) || q.AvailableAt.After(at) || at.Sub(q.AvailableAt) > time.Duration(policy.canonical.MaximumEvidenceAgeSeconds)*time.Second || q.EvidenceID == uuid.Nil || !digestPattern.MatchString(q.EvidenceSHA256) || !digestPattern.MatchString(q.PartitionContentSHA256) || q.SourceKey == "" {
		return quoteCanonical{}, fmt.Errorf("quote evidence is invalid")
	}
	return quoteCanonical{q.Bid, q.Ask, q.BidSize, q.AskSize, formatTime(q.AvailableAt), q.EvidenceID.String(), q.EvidenceSHA256, q.PartitionContentSHA256, q.SourceKey}, nil
}

func validateStructure(strategy Strategy, legs []legCanonical, expiry time.Time) error {
	low, high := legs[0], legs[1]
	if low.InstrumentID == high.InstrumentID || low.VenueContractID == high.VenueContractID || low.Underlying != high.Underlying || low.OptionType != high.OptionType || low.Expiry != high.Expiry || low.Expiry != formatTime(expiry) || low.Multiplier != high.Multiplier || low.Strike == high.Strike {
		return fmt.Errorf("defined-risk legs do not form one vertical")
	}
	valid := strategy == BullCall && low.OptionType == "call" && low.Position == "long" && high.Position == "short" || strategy == BearPut && low.OptionType == "put" && low.Position == "short" && high.Position == "long" || strategy == BullPut && low.OptionType == "put" && low.Position == "long" && high.Position == "short" || strategy == BearCall && low.OptionType == "call" && low.Position == "short" && high.Position == "long"
	if !valid {
		return fmt.Errorf("defined-risk strategy structure is invalid")
	}
	return nil
}

func ScenarioFromCanonical(id uuid.UUID, digest string, raw []byte, policy *Policy) (*Scenario, error) {
	var c scenarioCanonical
	if id == uuid.Nil || policy == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &c) != nil || c.PolicyID != policy.ID().String() || c.PolicySHA256 != policy.Digest() {
		return nil, fmt.Errorf("defined-risk scenario envelope is invalid")
	}
	legs := make([]LegInput, len(c.Legs))
	for i, leg := range c.Legs {
		legs[i] = legInput(leg)
	}
	value, err := NewScenario(ScenarioInput{policy, c.Strategy, c.InitialCapital, c.RequestedContracts, parseTime(c.DecisionAt), parseTime(c.ExpiryAt), c.TerminalUnderlying, parseTime(c.TerminalAvailableAt), uuid.MustParse(c.TerminalEvidenceID), c.TerminalEvidenceSHA256, c.Mode, legs})
	if err != nil || c.Schema != ScenarioSchemaV1 || c.State != "declared" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("defined-risk scenario identity does not reconstruct")
	}
	return value, nil
}

func legInput(v legCanonical) LegInput {
	var unwind *QuoteInput
	if v.Unwind != nil {
		q := quoteInput(*v.Unwind)
		unwind = &q
	}
	return LegInput{uuid.MustParse(v.InstrumentID), uuid.MustParse(v.VenueContractID), v.OCCSymbol, v.Underlying, v.OptionType, v.Strike, parseTime(v.Expiry), v.Multiplier, v.Style, v.Position, quoteInput(v.Entry), unwind}
}

func quoteInput(v quoteCanonical) QuoteInput {
	return QuoteInput{v.Bid, v.Ask, v.BidSize, v.AskSize, parseTime(v.AvailableAt), uuid.MustParse(v.EvidenceID), v.EvidenceSHA256, v.PartitionContentSHA256, v.SourceKey}
}

func supported(v Strategy) bool {
	return v == BullCall || v == BearPut || v == BullPut || v == BearCall
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

func (s *Scenario) Mode() strategycatalog.ExperimentMode {
	if s == nil {
		return ""
	}
	return s.canonical.Mode
}
