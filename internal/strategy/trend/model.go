package trend

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

type MemberInput struct {
	InstrumentID, VenueContractID                       uuid.UUID
	MembershipEffectiveAt, MembershipAvailableAt        time.Time
	HorizonPrices                                       []string
	CurrentPrice, RealizedVolatility, Bid, Ask, LotSize string
	PartitionContentSHA256, SourceKey, EvidenceSHA256   string
	AvailableAt                                         time.Time
}
type (
	RebalanceInput struct {
		OccurredAt time.Time
		Members    []MemberInput
	}
	ScenarioInput struct {
		Policy                         *Policy
		InitialCapital                 string
		EvaluationStart, EvaluationEnd time.Time
		Mode                           strategycatalog.ExperimentMode
		Rebalances                     []RebalanceInput
	}
	memberCanonical struct {
		InstrumentID           string   `json:"instrument_id"`
		VenueContractID        string   `json:"venue_contract_id"`
		MembershipEffectiveAt  string   `json:"membership_effective_at"`
		MembershipAvailableAt  string   `json:"membership_available_at"`
		HorizonPrices          []string `json:"horizon_prices"`
		CurrentPrice           string   `json:"current_price"`
		RealizedVolatility     string   `json:"realized_volatility"`
		Bid                    string   `json:"bid"`
		Ask                    string   `json:"ask"`
		LotSize                string   `json:"lot_size"`
		PartitionContentSHA256 string   `json:"partition_content_sha256"`
		SourceKey              string   `json:"source_key"`
		EvidenceSHA256         string   `json:"evidence_sha256"`
		AvailableAt            string   `json:"available_at"`
	}
)

type (
	rebalanceCanonical struct {
		Sequence   int               `json:"sequence"`
		OccurredAt string            `json:"occurred_at"`
		Members    []memberCanonical `json:"members"`
	}
	scenarioCanonical struct {
		Schema          string                         `json:"schema"`
		State           string                         `json:"state"`
		PolicyID        string                         `json:"policy_id"`
		PolicySHA256    string                         `json:"policy_sha256"`
		InitialCapital  string                         `json:"initial_capital"`
		EvaluationStart string                         `json:"evaluation_start"`
		EvaluationEnd   string                         `json:"evaluation_end"`
		Mode            strategycatalog.ExperimentMode `json:"mode"`
		Rebalances      []rebalanceCanonical           `json:"rebalances"`
	}
	Scenario struct {
		canonical scenarioCanonical
		bytes     json.RawMessage
		digest    string
		id        uuid.UUID
	}
)

func NewScenario(input ScenarioInput) (*Scenario, error) {
	if input.Policy == nil || !positive(input.InitialCapital) || !canonicalTime(input.EvaluationStart) || !canonicalTime(input.EvaluationEnd) || !input.EvaluationStart.Before(input.EvaluationEnd) || input.Mode != strategycatalog.ExperimentPaperScored && input.Mode != strategycatalog.ExperimentPaperStress || len(input.Rebalances) < 2 || len(input.Rebalances) > 10000 {
		return nil, fmt.Errorf("trend scenario is invalid")
	}
	values := make([]rebalanceCanonical, len(input.Rebalances))
	last := time.Time{}
	for i, source := range input.Rebalances {
		if !canonicalTime(source.OccurredAt) || source.OccurredAt.Before(input.EvaluationStart) || source.OccurredAt.After(input.EvaluationEnd) || !last.IsZero() && !source.OccurredAt.After(last) || len(source.Members) == 0 {
			return nil, fmt.Errorf("trend rebalance %d is invalid", i)
		}
		members := append([]MemberInput(nil), source.Members...)
		sort.Slice(members, func(i, j int) bool { return members[i].InstrumentID.String() < members[j].InstrumentID.String() })
		canonicalMembers := make([]memberCanonical, len(members))
		for j, m := range members {
			if m.InstrumentID == uuid.Nil || m.VenueContractID == uuid.Nil || !canonicalTime(m.MembershipEffectiveAt) || m.MembershipEffectiveAt.After(source.OccurredAt) || !canonicalTime(m.MembershipAvailableAt) || m.MembershipAvailableAt.After(source.OccurredAt) || source.OccurredAt.Sub(m.MembershipAvailableAt).Seconds() > float64(input.Policy.canonical.MaximumEvidenceAgeSeconds) || len(m.HorizonPrices) != len(input.Policy.canonical.Horizons) || !positive(m.CurrentPrice) || !positive(m.RealizedVolatility) || !positive(m.Bid) || !positive(m.Ask) || decimal.RequireFromString(m.Ask).LessThan(decimal.RequireFromString(m.Bid)) || !positive(m.LotSize) || !digestPattern.MatchString(m.PartitionContentSHA256) || m.SourceKey == "" || !digestPattern.MatchString(m.EvidenceSHA256) || !canonicalTime(m.AvailableAt) || m.AvailableAt.After(source.OccurredAt) || source.OccurredAt.Sub(m.AvailableAt).Seconds() > float64(input.Policy.canonical.MaximumEvidenceAgeSeconds) {
				return nil, fmt.Errorf("trend member %d/%d is invalid", i, j)
			}
			for _, price := range m.HorizonPrices {
				if !positive(price) {
					return nil, fmt.Errorf("trend horizon price is invalid")
				}
			}
			if j > 0 && members[j-1].InstrumentID == m.InstrumentID {
				return nil, fmt.Errorf("trend universe member is duplicated")
			}
			canonicalMembers[j] = memberCanonical{m.InstrumentID.String(), m.VenueContractID.String(), formatTime(m.MembershipEffectiveAt), formatTime(m.MembershipAvailableAt), append([]string(nil), m.HorizonPrices...), m.CurrentPrice, m.RealizedVolatility, m.Bid, m.Ask, m.LotSize, m.PartitionContentSHA256, m.SourceKey, m.EvidenceSHA256, formatTime(m.AvailableAt)}
		}
		values[i] = rebalanceCanonical{i, formatTime(source.OccurredAt), canonicalMembers}
		last = source.OccurredAt
	}
	if !input.Rebalances[0].OccurredAt.Equal(input.EvaluationStart) || !input.Rebalances[len(input.Rebalances)-1].OccurredAt.Equal(input.EvaluationEnd) {
		return nil, fmt.Errorf("trend rebalances do not span evaluation window")
	}
	canonical := scenarioCanonical{ScenarioSchemaV1, "declared", input.Policy.ID().String(), input.Policy.Digest(), input.InitialCapital, formatTime(input.EvaluationStart), formatTime(input.EvaluationEnd), input.Mode, values}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Scenario{canonical, encoded, digest, economicid.DeterministicUUID("etf-time-series-trend-scenario", ScenarioSchemaV1+"@sha256:"+digest)}, nil
}

func ScenarioFromCanonical(id uuid.UUID, digest string, raw []byte, policy *Policy) (*Scenario, error) {
	var c scenarioCanonical
	if id == uuid.Nil || policy == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &c) != nil {
		return nil, fmt.Errorf("trend scenario envelope is invalid")
	}
	rs := make([]RebalanceInput, len(c.Rebalances))
	for i, r := range c.Rebalances {
		ms := make([]MemberInput, len(r.Members))
		for j, m := range r.Members {
			ms[j] = MemberInput{mID(m.InstrumentID), mID(m.VenueContractID), parseTime(m.MembershipEffectiveAt), parseTime(m.MembershipAvailableAt), m.HorizonPrices, m.CurrentPrice, m.RealizedVolatility, m.Bid, m.Ask, m.LotSize, m.PartitionContentSHA256, m.SourceKey, m.EvidenceSHA256, parseTime(m.AvailableAt)}
		}
		rs[i] = RebalanceInput{parseTime(r.OccurredAt), ms}
	}
	v, err := NewScenario(ScenarioInput{policy, c.InitialCapital, parseTime(c.EvaluationStart), parseTime(c.EvaluationEnd), c.Mode, rs})
	if err != nil || c.Schema != ScenarioSchemaV1 || c.State != "declared" || v.ID() != id || v.Digest() != digest || !bytes.Equal(v.bytes, raw) {
		return nil, fmt.Errorf("trend scenario identity does not reconstruct")
	}
	return v, nil
}
func mID(v string) uuid.UUID { return uuid.MustParse(v) }
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
	return mID(s.canonical.PolicyID)
}

func (s *Scenario) PolicyDigest() string {
	if s == nil {
		return ""
	}
	return s.canonical.PolicySHA256
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
