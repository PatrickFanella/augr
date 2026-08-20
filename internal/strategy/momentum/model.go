package momentum

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
	InstrumentID, VenueContractID                     uuid.UUID
	MembershipEffectiveAt, MembershipAvailableAt      time.Time
	HistoryDays                                       int
	LookbackPrice, SkipPrice, Bid, Ask                string
	ROIC, DebtToAssets, FreeCashFlow, Volatility      string
	PartitionContentSHA256, SourceKey, EvidenceSHA256 string
	AvailableAt                                       time.Time
}

type RebalanceInput struct {
	OccurredAt                          time.Time
	BenchmarkTrend, BenchmarkVolatility string
	BenchmarkEvidenceID                 uuid.UUID
	BenchmarkEvidenceSHA256             string
	Members                             []MemberInput
}

type ScenarioInput struct {
	Policy                         *Policy
	InitialCapital                 string
	EvaluationStart, EvaluationEnd time.Time
	Mode                           strategycatalog.ExperimentMode
	Rebalances                     []RebalanceInput
}

type memberCanonical struct {
	InstrumentID           string `json:"instrument_id"`
	VenueContractID        string `json:"venue_contract_id"`
	MembershipEffectiveAt  string `json:"membership_effective_at"`
	MembershipAvailableAt  string `json:"membership_available_at"`
	HistoryDays            int    `json:"history_days"`
	LookbackPrice          string `json:"lookback_price"`
	SkipPrice              string `json:"skip_price"`
	Bid                    string `json:"bid"`
	Ask                    string `json:"ask"`
	ROIC                   string `json:"roic"`
	DebtToAssets           string `json:"debt_to_assets"`
	FreeCashFlow           string `json:"free_cash_flow"`
	Volatility             string `json:"volatility"`
	PartitionContentSHA256 string `json:"partition_content_sha256"`
	SourceKey              string `json:"source_key"`
	EvidenceSHA256         string `json:"evidence_sha256"`
	AvailableAt            string `json:"available_at"`
}
type rebalanceCanonical struct {
	Sequence                int               `json:"sequence"`
	OccurredAt              string            `json:"occurred_at"`
	BenchmarkTrend          string            `json:"benchmark_trend"`
	BenchmarkVolatility     string            `json:"benchmark_volatility"`
	BenchmarkEvidenceID     string            `json:"benchmark_evidence_id"`
	BenchmarkEvidenceSHA256 string            `json:"benchmark_evidence_sha256"`
	Members                 []memberCanonical `json:"members"`
}
type scenarioCanonical struct {
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
type Scenario struct {
	canonical scenarioCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewScenario(input ScenarioInput) (*Scenario, error) {
	if input.Policy == nil || !positive(input.InitialCapital) || !canonicalTime(input.EvaluationStart) || !canonicalTime(input.EvaluationEnd) || !input.EvaluationStart.Before(input.EvaluationEnd) || (input.Mode != strategycatalog.ExperimentPaperScored && input.Mode != strategycatalog.ExperimentPaperStress) || len(input.Rebalances) < 2 || len(input.Rebalances) > 10000 {
		return nil, fmt.Errorf("momentum scenario is invalid")
	}
	rebalances := make([]rebalanceCanonical, len(input.Rebalances))
	last := time.Time{}
	for index, source := range input.Rebalances {
		if !canonicalTime(source.OccurredAt) || source.OccurredAt.Before(input.EvaluationStart) || source.OccurredAt.After(input.EvaluationEnd) || !last.IsZero() && !source.OccurredAt.After(last) || !validDecimal(source.BenchmarkTrend) || !positive(source.BenchmarkVolatility) || source.BenchmarkEvidenceID == uuid.Nil || !digestPattern.MatchString(source.BenchmarkEvidenceSHA256) || len(source.Members) == 0 {
			return nil, fmt.Errorf("momentum rebalance %d is invalid", index)
		}
		members := append([]MemberInput(nil), source.Members...)
		sort.Slice(members, func(i, j int) bool { return members[i].InstrumentID.String() < members[j].InstrumentID.String() })
		canonicalMembers := make([]memberCanonical, len(members))
		for memberIndex, member := range members {
			if member.InstrumentID == uuid.Nil || member.VenueContractID == uuid.Nil || !canonicalTime(member.MembershipEffectiveAt) || member.MembershipEffectiveAt.After(source.OccurredAt) || !canonicalTime(member.MembershipAvailableAt) || member.MembershipAvailableAt.After(source.OccurredAt) || member.HistoryDays < 0 || !positive(member.LookbackPrice) || !positive(member.SkipPrice) || !positive(member.Bid) || !positive(member.Ask) || decimal.RequireFromString(member.Ask).LessThan(decimal.RequireFromString(member.Bid)) || !ratio(member.ROIC) || !ratio(member.DebtToAssets) || !validDecimal(member.FreeCashFlow) || !positive(member.Volatility) || !digestPattern.MatchString(member.PartitionContentSHA256) || member.SourceKey == "" || !digestPattern.MatchString(member.EvidenceSHA256) || !canonicalTime(member.AvailableAt) || member.AvailableAt.After(source.OccurredAt) {
				return nil, fmt.Errorf("momentum member %d/%d is invalid", index, memberIndex)
			}
			if memberIndex > 0 && members[memberIndex-1].InstrumentID == member.InstrumentID {
				return nil, fmt.Errorf("momentum universe member is duplicated")
			}
			canonicalMembers[memberIndex] = memberCanonical{InstrumentID: member.InstrumentID.String(), VenueContractID: member.VenueContractID.String(), MembershipEffectiveAt: formatTime(member.MembershipEffectiveAt), MembershipAvailableAt: formatTime(member.MembershipAvailableAt), HistoryDays: member.HistoryDays, LookbackPrice: member.LookbackPrice, SkipPrice: member.SkipPrice, Bid: member.Bid, Ask: member.Ask, ROIC: member.ROIC, DebtToAssets: member.DebtToAssets, FreeCashFlow: member.FreeCashFlow, Volatility: member.Volatility, PartitionContentSHA256: member.PartitionContentSHA256, SourceKey: member.SourceKey, EvidenceSHA256: member.EvidenceSHA256, AvailableAt: formatTime(member.AvailableAt)}
		}
		rebalances[index] = rebalanceCanonical{Sequence: index, OccurredAt: formatTime(source.OccurredAt), BenchmarkTrend: source.BenchmarkTrend, BenchmarkVolatility: source.BenchmarkVolatility, BenchmarkEvidenceID: source.BenchmarkEvidenceID.String(), BenchmarkEvidenceSHA256: source.BenchmarkEvidenceSHA256, Members: canonicalMembers}
		last = source.OccurredAt
	}
	if !input.Rebalances[0].OccurredAt.Equal(input.EvaluationStart) || !input.Rebalances[len(input.Rebalances)-1].OccurredAt.Equal(input.EvaluationEnd) {
		return nil, fmt.Errorf("momentum rebalances do not span evaluation window")
	}
	canonical := scenarioCanonical{Schema: ScenarioSchemaV1, State: "declared", PolicyID: input.Policy.ID().String(), PolicySHA256: input.Policy.Digest(), InitialCapital: input.InitialCapital, EvaluationStart: formatTime(input.EvaluationStart), EvaluationEnd: formatTime(input.EvaluationEnd), Mode: input.Mode, Rebalances: rebalances}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Scenario{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("momentum-quality-scenario", ScenarioSchemaV1+"@sha256:"+digest)}, nil
}

func ScenarioFromCanonical(id uuid.UUID, digest string, raw []byte, policy *Policy) (*Scenario, error) {
	var canonical scenarioCanonical
	if id == uuid.Nil || policy == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("momentum scenario envelope is invalid")
	}
	rebalances := make([]RebalanceInput, len(canonical.Rebalances))
	for i, r := range canonical.Rebalances {
		members := make([]MemberInput, len(r.Members))
		for j, m := range r.Members {
			members[j] = MemberInput{InstrumentID: uuid.MustParse(m.InstrumentID), VenueContractID: uuid.MustParse(m.VenueContractID), MembershipEffectiveAt: parseTime(m.MembershipEffectiveAt), MembershipAvailableAt: parseTime(m.MembershipAvailableAt), HistoryDays: m.HistoryDays, LookbackPrice: m.LookbackPrice, SkipPrice: m.SkipPrice, Bid: m.Bid, Ask: m.Ask, ROIC: m.ROIC, DebtToAssets: m.DebtToAssets, FreeCashFlow: m.FreeCashFlow, Volatility: m.Volatility, PartitionContentSHA256: m.PartitionContentSHA256, SourceKey: m.SourceKey, EvidenceSHA256: m.EvidenceSHA256, AvailableAt: parseTime(m.AvailableAt)}
		}
		rebalances[i] = RebalanceInput{OccurredAt: parseTime(r.OccurredAt), BenchmarkTrend: r.BenchmarkTrend, BenchmarkVolatility: r.BenchmarkVolatility, BenchmarkEvidenceID: uuid.MustParse(r.BenchmarkEvidenceID), BenchmarkEvidenceSHA256: r.BenchmarkEvidenceSHA256, Members: members}
	}
	value, err := NewScenario(ScenarioInput{Policy: policy, InitialCapital: canonical.InitialCapital, EvaluationStart: parseTime(canonical.EvaluationStart), EvaluationEnd: parseTime(canonical.EvaluationEnd), Mode: canonical.Mode, Rebalances: rebalances})
	if err != nil || canonical.Schema != ScenarioSchemaV1 || canonical.State != "declared" || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("momentum scenario identity does not reconstruct")
	}
	return value, nil
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
