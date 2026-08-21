// Package evidenceprogram defines immutable, fail-closed Milestone 7 campaign
// assessments. It has no provider, scheduler, account, ledger, risk, allocation,
// deployment, or execution mutation authority.
package evidenceprogram

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const SchemaV1 = "milestone-7-evidence-assessment-v1"

type Outcome string

const (
	OutcomeQualified Outcome = "qualified"
	OutcomeRejected  Outcome = "rejected"
	OutcomeHeld      Outcome = "held"
	OutcomeReady     Outcome = "ready"
	OutcomeNotReady  Outcome = "not_ready"
	OutcomeBlocked   Outcome = "blocked"
)

type EvidenceRef struct {
	Kind   string    `json:"kind"`
	ID     uuid.UUID `json:"id"`
	SHA256 string    `json:"sha256"`
}

type CandidateShadow struct {
	Key                string `json:"key"`
	ObservedDays       int    `json:"observed_days"`
	CriticalDefects    int    `json:"critical_defects"`
	ExecutableSamples  int    `json:"executable_samples"`
	SimulatedFills     int    `json:"simulated_fills"`
	SlippageKnown      bool   `json:"slippage_known"`
	SlippageDivergence string `json:"slippage_divergence"`
}

type ShadowInput struct {
	StartedAt, EndedAt time.Time
	DailyComplete      bool
	Candidates         []CandidateShadow
	Parents            []EvidenceRef
}

type CandidatePaper struct {
	Key                 string `json:"key"`
	Observations        int    `json:"observations"`
	AfterCostExpectancy string `json:"after_cost_expectancy"`
	CostsComplete       bool   `json:"costs_complete"`
	StatisticallyHonest bool   `json:"statistically_honest"`
	MarginBounded       bool   `json:"margin_bounded"`
}

type PaperInput struct {
	Shadow             *Assessment
	StartedAt, EndedAt time.Time
	Candidates         []CandidatePaper
	Parents            []EvidenceRef
}

type PortfolioInput struct {
	Paper                                        *Assessment
	StartedAt, EndedAt                           time.Time
	CombinedRiskAdjusted, BestSingleRiskAdjusted string
	SameInterval, SameCostBasis                  bool
	Parents                                      []EvidenceRef
}

type Capability struct {
	Name     string      `json:"name"`
	Passed   bool        `json:"passed"`
	Evidence EvidenceRef `json:"evidence"`
}

type ReadinessInput struct {
	Portfolio    *Assessment
	Capabilities []Capability
	Parents      []EvidenceRef
}

type canonical struct {
	Schema    string          `json:"schema"`
	Campaign  string          `json:"campaign"`
	Outcome   Outcome         `json:"outcome"`
	StartedAt string          `json:"started_at,omitempty"`
	EndedAt   string          `json:"ended_at,omitempty"`
	Facts     json.RawMessage `json:"facts"`
	Blockers  []string        `json:"blockers"`
	Parents   []EvidenceRef   `json:"parents"`
}

type Assessment struct {
	id     uuid.UUID
	digest string
	raw    json.RawMessage
	value  canonical
}

type Record struct {
	ID             uuid.UUID
	SHA256         string
	CanonicalBytes json.RawMessage
	Campaign       string
	Outcome        Outcome
	Blockers       []string
	Parents        []EvidenceRef
}

type shadowFacts struct {
	DailyComplete bool              `json:"daily_complete"`
	Candidates    []CandidateShadow `json:"candidates"`
}

type paperFacts struct {
	Candidates        []CandidatePaper `json:"candidates"`
	PositiveCandidate bool             `json:"positive_candidate"`
}

type portfolioFacts struct {
	Combined, Best              string
	SameInterval, SameCostBasis bool
}

type readinessFacts struct {
	Capabilities []Capability `json:"capabilities"`
}

func AssessShadow(input ShadowInput) (*Assessment, error) {
	parents, err := normalizeParents(input.Parents)
	if err != nil {
		return nil, err
	}
	candidates := append([]CandidateShadow(nil), input.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Key < candidates[j].Key })
	blockers := []string{}
	if durationDays(input.StartedAt, input.EndedAt) < 30 {
		blockers = append(blockers, "elapsed_days_under_30")
	}
	if !input.DailyComplete {
		blockers = append(blockers, "daily_evidence_incomplete")
	}
	if len(candidates) < 2 {
		blockers = append(blockers, "fewer_than_two_candidates")
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if !validKey(candidate.Key) || seen[candidate.Key] || candidate.ObservedDays < 0 || candidate.CriticalDefects < 0 || candidate.ExecutableSamples < 0 || candidate.SimulatedFills < 0 {
			return nil, fmt.Errorf("invalid shadow candidate")
		}
		seen[candidate.Key] = true
		if candidate.ObservedDays < 30 {
			blockers = append(blockers, candidate.Key+":observed_days_under_30")
		}
		if candidate.CriticalDefects > 0 {
			blockers = append(blockers, candidate.Key+":critical_defect")
		}
		if candidate.ExecutableSamples == 0 || candidate.SimulatedFills == 0 {
			blockers = append(blockers, candidate.Key+":execution_evidence_missing")
		}
		if !candidate.SlippageKnown || !validDecimal(candidate.SlippageDivergence) {
			blockers = append(blockers, candidate.Key+":slippage_unknown")
		}
	}
	facts, _ := json.Marshal(shadowFacts{input.DailyComplete, candidates})
	return build("shadow_30_day", input.StartedAt, input.EndedAt, OutcomeQualified, OutcomeHeld, facts, blockers, parents)
}

func AssessPaper(input PaperInput) (*Assessment, error) {
	if input.Shadow == nil {
		return nil, fmt.Errorf("shadow assessment is required")
	}
	parents, err := normalizeParents(append(input.Parents, input.Shadow.Reference()))
	if err != nil {
		return nil, err
	}
	candidates := append([]CandidatePaper(nil), input.Candidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Key < candidates[j].Key })
	blockers := []string{}
	days := durationDays(input.StartedAt, input.EndedAt)
	if input.Shadow.Outcome() != OutcomeQualified {
		blockers = append(blockers, "shadow_not_qualified")
	}
	if days < 60 {
		blockers = append(blockers, "elapsed_days_under_60")
	}
	if days > 90 {
		blockers = append(blockers, "elapsed_days_over_90")
	}
	if len(candidates) == 0 {
		blockers = append(blockers, "candidates_missing")
	}
	positive := false
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if !validKey(candidate.Key) || seen[candidate.Key] || candidate.Observations < 0 || !validDecimal(candidate.AfterCostExpectancy) {
			return nil, fmt.Errorf("invalid paper candidate")
		}
		seen[candidate.Key] = true
		if candidate.Observations == 0 {
			blockers = append(blockers, candidate.Key+":observations_missing")
		}
		if !candidate.CostsComplete {
			blockers = append(blockers, candidate.Key+":costs_incomplete")
		}
		if !candidate.StatisticallyHonest {
			blockers = append(blockers, candidate.Key+":statistics_incomplete")
		}
		if !candidate.MarginBounded {
			blockers = append(blockers, candidate.Key+":unbounded_margin")
		}
		value, _ := decimal.NewFromString(candidate.AfterCostExpectancy)
		positive = positive || value.GreaterThan(decimal.Zero)
	}
	outcome := OutcomeRejected
	if positive {
		outcome = OutcomeQualified
	}
	facts, _ := json.Marshal(paperFacts{candidates, positive})
	return build("scored_paper_60_90_day", input.StartedAt, input.EndedAt, outcome, OutcomeHeld, facts, blockers, parents)
}

func AssessPortfolio(input PortfolioInput) (*Assessment, error) {
	if input.Paper == nil {
		return nil, fmt.Errorf("paper assessment is required")
	}
	parents, err := normalizeParents(append(input.Parents, input.Paper.Reference()))
	if err != nil {
		return nil, err
	}
	blockers := []string{}
	if input.Paper.Outcome() != OutcomeQualified {
		blockers = append(blockers, "positive_paper_candidate_missing")
	}
	if !input.SameInterval {
		blockers = append(blockers, "comparison_interval_mismatch")
	}
	if !input.SameCostBasis {
		blockers = append(blockers, "comparison_cost_basis_mismatch")
	}
	combined, combinedOK := decimal.NewFromString(input.CombinedRiskAdjusted)
	best, bestOK := decimal.NewFromString(input.BestSingleRiskAdjusted)
	if combinedOK != nil || bestOK != nil {
		return nil, fmt.Errorf("invalid portfolio metric")
	}
	outcome := OutcomeRejected
	if combined.GreaterThanOrEqual(best) {
		outcome = OutcomeQualified
	}
	facts, _ := json.Marshal(portfolioFacts{input.CombinedRiskAdjusted, input.BestSingleRiskAdjusted, input.SameInterval, input.SameCostBasis})
	return build("portfolio_paper", input.StartedAt, input.EndedAt, outcome, OutcomeHeld, facts, blockers, parents)
}

var requiredCapabilities = []string{"accept_deposits", "resize_safely", "run_unattended", "brake", "restart", "reconcile", "daily_explanation"}

func AssessReadiness(input ReadinessInput) (*Assessment, error) {
	if input.Portfolio == nil {
		return nil, fmt.Errorf("portfolio assessment is required")
	}
	parents, err := normalizeParents(append(input.Parents, input.Portfolio.Reference()))
	if err != nil {
		return nil, err
	}
	capabilities := append([]Capability(nil), input.Capabilities...)
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].Name < capabilities[j].Name })
	byName := map[string]Capability{}
	for _, capability := range capabilities {
		if !validKey(capability.Name) || byName[capability.Name].Name != "" {
			return nil, fmt.Errorf("invalid readiness capability")
		}
		if err := validateRef(capability.Evidence); err != nil {
			return nil, err
		}
		byName[capability.Name] = capability
	}
	blockers := []string{}
	failure := OutcomeNotReady
	if input.Portfolio.Outcome() == OutcomeHeld {
		failure = OutcomeBlocked
	}
	if input.Portfolio.Outcome() != OutcomeQualified {
		blockers = append(blockers, "portfolio_not_qualified")
	}
	for _, name := range requiredCapabilities {
		capability, exists := byName[name]
		if !exists {
			blockers = append(blockers, name+":evidence_missing")
		} else if !capability.Passed {
			blockers = append(blockers, name+":not_passed")
		}
	}
	facts, _ := json.Marshal(readinessFacts{capabilities})
	return build("architecture_readiness", time.Time{}, time.Time{}, OutcomeReady, failure, facts, blockers, parents)
}

func build(campaign string, start, end time.Time, pass, fail Outcome, facts []byte, blockers []string, parents []EvidenceRef) (*Assessment, error) {
	sort.Strings(blockers)
	outcome := pass
	if len(blockers) > 0 {
		outcome = fail
	}
	c := canonical{Schema: SchemaV1, Campaign: campaign, Outcome: outcome, Facts: facts, Blockers: blockers, Parents: parents}
	if !start.IsZero() {
		c.StartedAt = formatTime(start)
		c.EndedAt = formatTime(end)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	digest := hash(raw)
	return &Assessment{id: economicid.DeterministicUUID("milestone-7-evidence", SchemaV1+"@sha256:"+digest), digest: digest, raw: raw, value: c}, nil
}

func (a *Assessment) ID() uuid.UUID          { return a.id }
func (a *Assessment) Digest() string         { return a.digest }
func (a *Assessment) Outcome() Outcome       { return a.value.Outcome }
func (a *Assessment) Campaign() string       { return a.value.Campaign }
func (a *Assessment) Blockers() []string     { return append([]string(nil), a.value.Blockers...) }
func (a *Assessment) CanonicalBytes() []byte { return append([]byte(nil), a.raw...) }
func (a *Assessment) Reference() EvidenceRef {
	return EvidenceRef{Kind: a.value.Campaign, ID: a.id, SHA256: a.digest}
}

func (a *Assessment) Record() Record {
	return Record{a.id, a.digest, append([]byte(nil), a.raw...), a.value.Campaign, a.value.Outcome, append([]string(nil), a.value.Blockers...), append([]EvidenceRef(nil), a.value.Parents...)}
}

// AssessmentFromCanonical reconstructs an assessment through its authoritative
// assessor. Assessment parents must be supplied for dependency campaigns;
// arbitrary non-assessment evidence remains bound by exact reference.
func AssessmentFromCanonical(id uuid.UUID, digest string, raw json.RawMessage, assessmentParents []*Assessment) (*Assessment, error) {
	var value canonical
	if id == uuid.Nil || len(digest) != 64 || hash(raw) != digest || json.Unmarshal(raw, &value) != nil || value.Schema != SchemaV1 {
		return nil, fmt.Errorf("invalid canonical milestone evidence assessment")
	}
	normalizedParents, err := normalizeParents(value.Parents)
	if err != nil || !evidenceRefsEqual(normalizedParents, value.Parents) {
		return nil, fmt.Errorf("canonical milestone evidence parents are invalid")
	}
	start, end, err := canonicalInterval(value)
	if err != nil {
		return nil, err
	}
	var rebuilt *Assessment
	switch value.Campaign {
	case "shadow_30_day":
		var facts shadowFacts
		if len(assessmentParents) != 0 || json.Unmarshal(value.Facts, &facts) != nil {
			return nil, fmt.Errorf("invalid canonical shadow facts")
		}
		rebuilt, err = AssessShadow(ShadowInput{StartedAt: start, EndedAt: end, DailyComplete: facts.DailyComplete, Candidates: facts.Candidates, Parents: value.Parents})
	case "scored_paper_60_90_day":
		var facts paperFacts
		parent, other, parentErr := exactAssessmentParent(value.Parents, assessmentParents, "shadow_30_day")
		if parentErr != nil || json.Unmarshal(value.Facts, &facts) != nil {
			return nil, fmt.Errorf("invalid canonical paper facts or parent")
		}
		rebuilt, err = AssessPaper(PaperInput{Shadow: parent, StartedAt: start, EndedAt: end, Candidates: facts.Candidates, Parents: other})
	case "portfolio_paper":
		var facts portfolioFacts
		parent, other, parentErr := exactAssessmentParent(value.Parents, assessmentParents, "scored_paper_60_90_day")
		if parentErr != nil || json.Unmarshal(value.Facts, &facts) != nil {
			return nil, fmt.Errorf("invalid canonical portfolio facts or parent")
		}
		rebuilt, err = AssessPortfolio(PortfolioInput{Paper: parent, StartedAt: start, EndedAt: end, CombinedRiskAdjusted: facts.Combined, BestSingleRiskAdjusted: facts.Best, SameInterval: facts.SameInterval, SameCostBasis: facts.SameCostBasis, Parents: other})
	case "architecture_readiness":
		var facts readinessFacts
		parent, other, parentErr := exactAssessmentParent(value.Parents, assessmentParents, "portfolio_paper")
		if parentErr != nil || json.Unmarshal(value.Facts, &facts) != nil {
			return nil, fmt.Errorf("invalid canonical readiness facts or parent")
		}
		rebuilt, err = AssessReadiness(ReadinessInput{Portfolio: parent, Capabilities: facts.Capabilities, Parents: other})
	default:
		return nil, fmt.Errorf("unknown milestone evidence campaign")
	}
	if err != nil || rebuilt.ID() != id || rebuilt.Digest() != digest || !bytes.Equal(rebuilt.CanonicalBytes(), raw) {
		return nil, fmt.Errorf("canonical milestone evidence assessment does not reconstruct")
	}
	return rebuilt, nil
}

func canonicalInterval(value canonical) (time.Time, time.Time, error) {
	if value.Campaign == "architecture_readiness" {
		if value.StartedAt != "" || value.EndedAt != "" {
			return time.Time{}, time.Time{}, fmt.Errorf("readiness assessment cannot declare an interval")
		}
		return time.Time{}, time.Time{}, nil
	}
	start, startErr := time.Parse("2006-01-02T15:04:05.000000Z", value.StartedAt)
	end, endErr := time.Parse("2006-01-02T15:04:05.000000Z", value.EndedAt)
	if startErr != nil || endErr != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("canonical milestone evidence interval is invalid")
	}
	return start, end, nil
}

func exactAssessmentParent(refs []EvidenceRef, values []*Assessment, campaign string) (*Assessment, []EvidenceRef, error) {
	if len(values) != 1 || values[0] == nil || values[0].Campaign() != campaign {
		return nil, nil, fmt.Errorf("exact %s parent is required", campaign)
	}
	want := values[0].Reference()
	other := make([]EvidenceRef, 0, len(refs)-1)
	found := false
	for _, ref := range refs {
		if ref.Kind == campaign {
			if found || ref != want {
				return nil, nil, fmt.Errorf("assessment parent reference mismatch")
			}
			found = true
			continue
		}
		other = append(other, ref)
	}
	if !found {
		return nil, nil, fmt.Errorf("assessment parent reference is missing")
	}
	return values[0], other, nil
}

func evidenceRefsEqual(left, right []EvidenceRef) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeParents(values []EvidenceRef) ([]EvidenceRef, error) {
	parents := append([]EvidenceRef(nil), values...)
	sort.Slice(parents, func(i, j int) bool {
		if parents[i].Kind == parents[j].Kind {
			return parents[i].ID.String() < parents[j].ID.String()
		}
		return parents[i].Kind < parents[j].Kind
	})
	for i, parent := range parents {
		if err := validateRef(parent); err != nil {
			return nil, err
		}
		if i > 0 && parent.Kind == parents[i-1].Kind && parent.ID == parents[i-1].ID {
			return nil, fmt.Errorf("duplicate evidence parent")
		}
	}
	return parents, nil
}

func validateRef(value EvidenceRef) error {
	if !validKey(value.Kind) || value.ID == uuid.Nil || len(value.SHA256) != 64 {
		return fmt.Errorf("invalid evidence reference")
	}
	if _, err := hex.DecodeString(value.SHA256); err != nil {
		return fmt.Errorf("invalid evidence reference")
	}
	return nil
}

func validKey(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				if r != '_' && r != '-' && r != ':' {
					return false
				}
			}
		}
	}
	return true
}

func validDecimal(value string) bool {
	if strings.TrimSpace(value) != value || value == "" {
		return false
	}
	_, err := decimal.NewFromString(value)
	return err == nil
}

func durationDays(start, end time.Time) int {
	start = start.UTC()
	end = end.UTC()
	if start.IsZero() || !end.After(start) || start.Nanosecond()%1000 != 0 || end.Nanosecond()%1000 != 0 {
		return -1
	}
	return int(end.Sub(start) / (24 * time.Hour))
}

func formatTime(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")
}
func hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
