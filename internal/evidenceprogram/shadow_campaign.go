package evidenceprogram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	ShadowCampaignSchemaV1 = "shadow-campaign-v1"
	ShadowDaySchemaV1      = "shadow-campaign-day-v1"
	ShadowTargetDays       = 30
)

type ShadowCandidate struct {
	Key       string    `json:"key"`
	VersionID uuid.UUID `json:"version_id"`
	SHA256    string    `json:"sha256"`
}

type ShadowCampaignInput struct {
	Key        string
	StartedAt  time.Time
	Benchmark  EvidenceRef
	Candidates []ShadowCandidate
}

type shadowCampaignCanonical struct {
	Schema     string            `json:"schema"`
	Key        string            `json:"key"`
	StartedAt  string            `json:"started_at"`
	TargetDays int               `json:"target_days"`
	Benchmark  EvidenceRef       `json:"benchmark"`
	Candidates []ShadowCandidate `json:"candidates"`
}

type ShadowCampaign struct {
	id        uuid.UUID
	digest    string
	raw       json.RawMessage
	canonical shadowCampaignCanonical
}

type ShadowCandidateDayInput struct {
	Key                string
	CriticalDefects    int
	ExecutableSamples  int
	SimulatedFills     int
	SlippageKnown      bool
	SlippageDivergence string
}

type ShadowDayInput struct {
	Campaign   *ShadowCampaign
	Sequence   int
	ObservedAt time.Time
	Candidates []ShadowCandidateDayInput
	Source     EvidenceRef
}

type ShadowCandidateDay struct {
	Key                string `json:"key"`
	CriticalDefects    int    `json:"critical_defects"`
	ExecutableSamples  int    `json:"executable_samples"`
	SimulatedFills     int    `json:"simulated_fills"`
	SlippageKnown      bool   `json:"slippage_known"`
	SlippageDivergence string `json:"slippage_divergence"`
}

type shadowDayCanonical struct {
	Schema         string               `json:"schema"`
	CampaignID     uuid.UUID            `json:"campaign_id"`
	CampaignSHA256 string               `json:"campaign_sha256"`
	Sequence       int                  `json:"sequence"`
	ObservedAt     string               `json:"observed_at"`
	Candidates     []ShadowCandidateDay `json:"candidates"`
	Source         EvidenceRef          `json:"source"`
}

type ShadowDay struct {
	id        uuid.UUID
	digest    string
	raw       json.RawMessage
	canonical shadowDayCanonical
}

type ShadowCampaignRecord struct {
	ID             uuid.UUID
	SHA256         string
	CanonicalBytes json.RawMessage
	Key            string
	StartedAt      time.Time
	Benchmark      EvidenceRef
	Candidates     []ShadowCandidate
}

type ShadowDayRecord struct {
	ID             uuid.UUID
	SHA256         string
	CanonicalBytes json.RawMessage
	CampaignID     uuid.UUID
	CampaignSHA256 string
	Sequence       int
	ObservedAt     time.Time
	Candidates     []ShadowCandidateDay
	Source         EvidenceRef
}

func NewShadowCampaign(input ShadowCampaignInput) (*ShadowCampaign, error) {
	if !validKey(input.Key) || !canonicalUTC(input.StartedAt) || input.StartedAt.Hour() != 0 || input.StartedAt.Minute() != 0 || input.StartedAt.Second() != 0 {
		return nil, fmt.Errorf("invalid shadow campaign identity or start")
	}
	if input.Benchmark.Kind != "benchmark_opportunity_cost_report" {
		return nil, fmt.Errorf("shadow campaign requires an OVR-401 benchmark report")
	}
	if err := validateRef(input.Benchmark); err != nil {
		return nil, fmt.Errorf("invalid shadow benchmark: %w", err)
	}
	candidates := append([]ShadowCandidate(nil), input.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Key < candidates[j].Key })
	if len(candidates) < 2 || len(candidates) > 16 {
		return nil, fmt.Errorf("shadow campaign requires two to sixteen candidates")
	}
	versions := map[uuid.UUID]bool{}
	for index, candidate := range candidates {
		if !validKey(candidate.Key) || candidate.VersionID == uuid.Nil || len(candidate.SHA256) != 64 {
			return nil, fmt.Errorf("invalid shadow candidate")
		}
		if err := validateRef(EvidenceRef{Kind: "strategy_version", ID: candidate.VersionID, SHA256: candidate.SHA256}); err != nil {
			return nil, fmt.Errorf("invalid shadow candidate: %w", err)
		}
		if index > 0 && candidate.Key == candidates[index-1].Key {
			return nil, fmt.Errorf("duplicate shadow candidate")
		}
		if versions[candidate.VersionID] {
			return nil, fmt.Errorf("shadow candidates must use distinct versions")
		}
		versions[candidate.VersionID] = true
	}
	c := shadowCampaignCanonical{ShadowCampaignSchemaV1, input.Key, formatTime(input.StartedAt), ShadowTargetDays, input.Benchmark, candidates}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	digest := hash(raw)
	return &ShadowCampaign{id: economicid.DeterministicUUID("shadow-campaign", ShadowCampaignSchemaV1+"@sha256:"+digest), digest: digest, raw: raw, canonical: c}, nil
}

func NewShadowDay(input ShadowDayInput) (*ShadowDay, error) {
	if input.Campaign == nil || input.Sequence < 0 || input.Sequence >= ShadowTargetDays || !canonicalUTC(input.ObservedAt) {
		return nil, fmt.Errorf("invalid shadow day envelope")
	}
	wantObservedAt := input.Campaign.StartedAt().Add(time.Duration(input.Sequence) * 24 * time.Hour)
	if !input.ObservedAt.Equal(wantObservedAt) {
		return nil, fmt.Errorf("shadow day is not the exact campaign date")
	}
	if err := validateRef(input.Source); err != nil {
		return nil, fmt.Errorf("invalid shadow day source: %w", err)
	}
	values := append([]ShadowCandidateDayInput(nil), input.Candidates...)
	sort.Slice(values, func(i, j int) bool { return values[i].Key < values[j].Key })
	want := input.Campaign.Candidates()
	if len(values) != len(want) {
		return nil, fmt.Errorf("shadow day candidate set is incomplete")
	}
	candidates := make([]ShadowCandidateDay, 0, len(values))
	for index, value := range values {
		if value.Key != want[index].Key || value.CriticalDefects < 0 || value.ExecutableSamples < 0 || value.SimulatedFills < 0 {
			return nil, fmt.Errorf("invalid shadow day candidate")
		}
		if value.SlippageKnown {
			if !validDecimal(value.SlippageDivergence) {
				return nil, fmt.Errorf("known shadow slippage is invalid")
			}
		} else if value.SlippageDivergence != "" {
			return nil, fmt.Errorf("unknown shadow slippage invented a value")
		}
		candidates = append(candidates, ShadowCandidateDay(value))
	}
	c := shadowDayCanonical{ShadowDaySchemaV1, input.Campaign.ID(), input.Campaign.Digest(), input.Sequence, formatTime(input.ObservedAt), candidates, input.Source}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	digest := hash(raw)
	return &ShadowDay{id: economicid.DeterministicUUID("shadow-campaign-day", ShadowDaySchemaV1+"@sha256:"+digest), digest: digest, raw: raw, canonical: c}, nil
}

func ShadowCampaignFromCanonical(id uuid.UUID, digest string, raw json.RawMessage) (*ShadowCampaign, error) {
	var value shadowCampaignCanonical
	if id == uuid.Nil || hash(raw) != digest || json.Unmarshal(raw, &value) != nil || value.Schema != ShadowCampaignSchemaV1 || value.TargetDays != ShadowTargetDays {
		return nil, fmt.Errorf("invalid canonical shadow campaign")
	}
	rebuilt, err := NewShadowCampaign(ShadowCampaignInput{Key: value.Key, StartedAt: mustUTC(value.StartedAt), Benchmark: value.Benchmark, Candidates: value.Candidates})
	if err != nil || rebuilt.ID() != id || rebuilt.Digest() != digest || !bytes.Equal(rebuilt.CanonicalBytes(), raw) {
		return nil, fmt.Errorf("canonical shadow campaign does not reconstruct")
	}
	return rebuilt, nil
}

func ShadowDayFromCanonical(id uuid.UUID, digest string, raw json.RawMessage, campaign *ShadowCampaign) (*ShadowDay, error) {
	var value shadowDayCanonical
	if campaign == nil || id == uuid.Nil || hash(raw) != digest || json.Unmarshal(raw, &value) != nil || value.Schema != ShadowDaySchemaV1 || value.CampaignID != campaign.ID() || value.CampaignSHA256 != campaign.Digest() {
		return nil, fmt.Errorf("invalid canonical shadow day")
	}
	inputs := make([]ShadowCandidateDayInput, len(value.Candidates))
	for index, candidate := range value.Candidates {
		inputs[index] = ShadowCandidateDayInput(candidate)
	}
	rebuilt, err := NewShadowDay(ShadowDayInput{Campaign: campaign, Sequence: value.Sequence, ObservedAt: mustUTC(value.ObservedAt), Candidates: inputs, Source: value.Source})
	if err != nil || rebuilt.ID() != id || rebuilt.Digest() != digest || !bytes.Equal(rebuilt.CanonicalBytes(), raw) {
		return nil, fmt.Errorf("canonical shadow day does not reconstruct")
	}
	return rebuilt, nil
}

func BuildShadowAssessment(campaign *ShadowCampaign, days []*ShadowDay) (*Assessment, error) {
	if campaign == nil {
		return nil, fmt.Errorf("shadow campaign is required")
	}
	ordered := append([]*ShadowDay(nil), days...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Sequence() < ordered[j].Sequence() })
	dailyComplete := len(ordered) == ShadowTargetDays
	type aggregate struct {
		critical, executable, fills int
		known                       bool
		divergence                  decimal.Decimal
	}
	aggregates := map[string]*aggregate{}
	for _, candidate := range campaign.Candidates() {
		aggregates[candidate.Key] = &aggregate{known: true}
	}
	parents := []EvidenceRef{campaign.Reference()}
	for index, day := range ordered {
		if day == nil || day.CampaignID() != campaign.ID() || day.CampaignDigest() != campaign.Digest() || day.Sequence() != index {
			dailyComplete = false
			continue
		}
		parents = append(parents, day.Reference())
		for _, candidate := range day.Candidates() {
			aggregate := aggregates[candidate.Key]
			if aggregate == nil {
				dailyComplete = false
				continue
			}
			aggregate.critical += candidate.CriticalDefects
			aggregate.executable += candidate.ExecutableSamples
			aggregate.fills += candidate.SimulatedFills
			aggregate.known = aggregate.known && candidate.SlippageKnown
			if candidate.SlippageKnown {
				value, _ := decimal.NewFromString(candidate.SlippageDivergence)
				aggregate.divergence = aggregate.divergence.Add(value)
			}
		}
	}
	candidates := make([]CandidateShadow, 0, len(aggregates))
	for _, candidate := range campaign.Candidates() {
		aggregate := aggregates[candidate.Key]
		divergence := ""
		if aggregate.known && len(ordered) > 0 {
			divergence = aggregate.divergence.Div(decimal.NewFromInt(int64(len(ordered)))).String()
		}
		candidates = append(candidates, CandidateShadow{Key: candidate.Key, ObservedDays: len(ordered), CriticalDefects: aggregate.critical, ExecutableSamples: aggregate.executable, SimulatedFills: aggregate.fills, SlippageKnown: aggregate.known, SlippageDivergence: divergence})
	}
	return AssessShadow(ShadowInput{StartedAt: campaign.StartedAt(), EndedAt: campaign.StartedAt().Add(ShadowTargetDays * 24 * time.Hour), DailyComplete: dailyComplete, Candidates: candidates, Parents: parents})
}

func (c *ShadowCampaign) ID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return c.id
}

func (c *ShadowCampaign) Digest() string {
	if c == nil {
		return ""
	}
	return c.digest
}

func (c *ShadowCampaign) CanonicalBytes() []byte {
	if c == nil {
		return nil
	}
	return append([]byte(nil), c.raw...)
}

func (c *ShadowCampaign) StartedAt() time.Time {
	if c == nil {
		return time.Time{}
	}
	return mustUTC(c.canonical.StartedAt)
}

func (c *ShadowCampaign) Candidates() []ShadowCandidate {
	if c == nil {
		return nil
	}
	return append([]ShadowCandidate(nil), c.canonical.Candidates...)
}

func (c *ShadowCampaign) Reference() EvidenceRef {
	return EvidenceRef{Kind: "shadow_campaign", ID: c.ID(), SHA256: c.Digest()}
}

func (c *ShadowCampaign) Record() ShadowCampaignRecord {
	return ShadowCampaignRecord{c.ID(), c.Digest(), c.CanonicalBytes(), c.canonical.Key, c.StartedAt(), c.canonical.Benchmark, c.Candidates()}
}

func (d *ShadowDay) ID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return d.id
}

func (d *ShadowDay) Digest() string {
	if d == nil {
		return ""
	}
	return d.digest
}

func (d *ShadowDay) CanonicalBytes() []byte {
	if d == nil {
		return nil
	}
	return append([]byte(nil), d.raw...)
}

func (d *ShadowDay) CampaignID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return d.canonical.CampaignID
}

func (d *ShadowDay) CampaignDigest() string {
	if d == nil {
		return ""
	}
	return d.canonical.CampaignSHA256
}

func (d *ShadowDay) Sequence() int {
	if d == nil {
		return -1
	}
	return d.canonical.Sequence
}

func (d *ShadowDay) ObservedAt() time.Time {
	if d == nil {
		return time.Time{}
	}
	return mustUTC(d.canonical.ObservedAt)
}

func (d *ShadowDay) Candidates() []ShadowCandidateDay {
	if d == nil {
		return nil
	}
	return append([]ShadowCandidateDay(nil), d.canonical.Candidates...)
}

func (d *ShadowDay) Reference() EvidenceRef {
	return EvidenceRef{Kind: "shadow_campaign_day", ID: d.ID(), SHA256: d.Digest()}
}

func (d *ShadowDay) Record() ShadowDayRecord {
	return ShadowDayRecord{d.ID(), d.Digest(), d.CanonicalBytes(), d.CampaignID(), d.CampaignDigest(), d.Sequence(), d.ObservedAt(), d.Candidates(), d.canonical.Source}
}

func canonicalUTC(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}

func mustUTC(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", value)
	return parsed
}
