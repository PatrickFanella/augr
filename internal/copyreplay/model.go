// Package copyreplay owns deterministic point-in-time 13F research replay
// evidence. It does not allocate capital, propose execution, or route orders.
package copyreplay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
)

const SchemaV1 = "point-in-time-13f-replay-v1"

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Policy struct {
	SelectionCutoff time.Time
	TopN            int
}

type ManagerEvidence struct {
	ManagerID     uuid.UUID
	SourceKey     string
	ContentSHA256 string
	AvailableAt   time.Time
	Eligible      bool
	Score         string
}

type FilingEvidence struct {
	ManagerID       uuid.UUID
	SourceKey       string
	ContentSHA256   string
	ReportPeriod    time.Time
	PublishedAt     time.Time
	AvailableAt     time.Time
	AmendmentNumber int
	SupersedesKey   string
}

type Input struct {
	Manifest      *dataset.Manifest
	Policy        Policy
	Managers      []ManagerEvidence
	Filings       []FilingEvidence
	DecisionTimes []time.Time
}

type evidenceCanonical struct {
	ManagerID              string `json:"manager_id"`
	PartitionContentSHA256 string `json:"partition_content_sha256"`
	SourceKey              string `json:"source_key"`
	ContentSHA256          string `json:"content_sha256"`
	AvailableAt            string `json:"available_at"`
}

type managerCanonical struct {
	evidenceCanonical
	Rank  int    `json:"rank"`
	Score string `json:"score"`
}

type managerCandidateCanonical struct {
	evidenceCanonical
	Eligible bool   `json:"eligible"`
	Score    string `json:"score"`
}

type filingCanonical struct {
	evidenceCanonical
	ReportPeriod    string `json:"report_period"`
	PublishedAt     string `json:"published_at"`
	AmendmentNumber int    `json:"amendment_number"`
	SupersedesKey   string `json:"supersedes_key"`
}

type decisionCanonical struct {
	Sequence          int    `json:"sequence"`
	DecisionAt        string `json:"decision_at"`
	ManagerID         string `json:"manager_id"`
	Status            string `json:"status"`
	FilingSourceKey   string `json:"filing_source_key"`
	FilingContentSHA  string `json:"filing_content_sha256"`
	FilingAvailableAt string `json:"filing_available_at"`
	ReportPeriod      string `json:"report_period"`
	AmendmentNumber   int    `json:"amendment_number"`
}

type stepCanonical struct {
	Sequence               int             `json:"sequence"`
	DecisionSequence       int             `json:"decision_sequence"`
	PartitionContentSHA256 string          `json:"partition_content_sha256"`
	ObservationSourceKey   string          `json:"observation_source_key"`
	ObservationContentSHA  string          `json:"observation_content_sha256"`
	AvailableAt            string          `json:"available_at"`
	Decision               json.RawMessage `json:"decision"`
}

type replayCanonical struct {
	Schema            string                      `json:"schema"`
	State             string                      `json:"state"`
	ManifestID        string                      `json:"manifest_id"`
	ManifestSHA256    string                      `json:"manifest_sha256"`
	ManifestCutoff    string                      `json:"manifest_cutoff"`
	SelectionCutoff   string                      `json:"selection_cutoff"`
	TopN              int                         `json:"top_n"`
	CandidateManagers []managerCandidateCanonical `json:"candidate_managers"`
	Filings           []filingCanonical           `json:"filings"`
	Managers          []managerCanonical          `json:"managers"`
	Decisions         []decisionCanonical         `json:"decisions"`
	Steps             []stepCanonical             `json:"steps"`
}

type Replay struct {
	canonical replayCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

type manifestObservation struct {
	partition string
	available time.Time
	published time.Time
}

func NewReplay(input Input) (*Replay, error) {
	if input.Manifest == nil || input.Policy.TopN < 1 || !canonicalTime(input.Policy.SelectionCutoff) || input.Policy.SelectionCutoff.After(input.Manifest.DecisionCutoff()) {
		return nil, fmt.Errorf("13f replay manifest and policy are invalid")
	}
	index, err := manifestIndex(input.Manifest)
	if err != nil {
		return nil, err
	}
	managers, selected, candidates, err := selectManagers(input.Managers, input.Policy, index)
	if err != nil {
		return nil, err
	}
	filings, filingInputs, err := validateFilings(input.Filings, selected, index)
	if err != nil {
		return nil, err
	}
	times := append([]time.Time(nil), input.DecisionTimes...)
	for i, value := range times {
		if !canonicalTime(value) || value.Before(input.Policy.SelectionCutoff) || value.After(input.Manifest.DecisionCutoff()) || i > 0 && !times[i-1].Before(value) {
			return nil, fmt.Errorf("13f replay decision calendar is invalid")
		}
	}
	if len(times) == 0 {
		return nil, fmt.Errorf("13f replay decision calendar is empty")
	}
	decisions := make([]decisionCanonical, 0, len(times)*len(managers))
	steps := make([]stepCanonical, 0)
	prior := map[uuid.UUID]string{}
	for _, decisionAt := range times {
		for _, manager := range managers {
			managerID := uuid.MustParse(manager.ManagerID)
			filing := chooseFiling(filings[managerID], decisionAt)
			decision := decisionCanonical{Sequence: len(decisions), DecisionAt: formatTime(decisionAt), ManagerID: manager.ManagerID, Status: "no_filing"}
			if filing != nil {
				decision.FilingSourceKey, decision.FilingContentSHA = filing.SourceKey, filing.ContentSHA256
				decision.FilingAvailableAt, decision.ReportPeriod = formatTime(filing.AvailableAt), formatDate(filing.ReportPeriod)
				decision.AmendmentNumber = filing.AmendmentNumber
				decision.Status = "selected"
				if prior[managerID] == filing.SourceKey {
					decision.Status = "unchanged"
				} else {
					observation := index[evidenceKey(filing.SourceKey, filing.ContentSHA256)]
					decisionJSON, _ := json.Marshal(map[string]any{"amendment_number": filing.AmendmentNumber, "decision_at": formatTime(decisionAt), "filing_source_key": filing.SourceKey, "manager_id": manager.ManagerID, "report_period": formatDate(filing.ReportPeriod), "status": "selected"})
					steps = append(steps, stepCanonical{len(steps), decision.Sequence, observation.partition, filing.SourceKey, filing.ContentSHA256, formatTime(filing.AvailableAt), decisionJSON})
				}
				prior[managerID] = filing.SourceKey
			}
			decisions = append(decisions, decision)
		}
	}
	canonical := replayCanonical{SchemaV1, "completed", input.Manifest.ID().String(), input.Manifest.Digest(), formatTime(input.Manifest.DecisionCutoff()), formatTime(input.Policy.SelectionCutoff), input.Policy.TopN, candidates, filingInputs, managers, decisions, steps}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Replay{canonical, encoded, digest, economicid.DeterministicUUID("point-in-time-13f-replay", SchemaV1+"@sha256:"+digest)}, nil
}

func manifestIndex(manifest *dataset.Manifest) (map[string]manifestObservation, error) {
	result := map[string]manifestObservation{}
	for _, partition := range manifest.Partitions() {
		if partition.Kind != dataset.KindFilings {
			continue
		}
		for _, observation := range partition.Observations {
			key := evidenceKey(observation.SourceKey, observation.ContentSHA256)
			if _, exists := result[key]; exists {
				return nil, fmt.Errorf("13f replay manifest evidence is duplicated")
			}
			available, _ := time.Parse("2006-01-02T15:04:05.000000Z", observation.AvailableAt)
			published := time.Time{}
			if observation.PublishedAt != "" {
				published, _ = time.Parse("2006-01-02T15:04:05.000000Z", observation.PublishedAt)
			}
			result[key] = manifestObservation{partition.ContentSHA256, available, published}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("13f replay manifest has no filing evidence")
	}
	return result, nil
}

func selectManagers(values []ManagerEvidence, policy Policy, index map[string]manifestObservation) ([]managerCanonical, map[uuid.UUID]bool, []managerCandidateCanonical, error) {
	type ranked struct {
		value ManagerEvidence
		score decimal.Decimal
		obs   manifestObservation
	}
	items := make([]ranked, 0, len(values))
	seen := map[uuid.UUID]bool{}
	candidates := make([]managerCandidateCanonical, 0, len(values))
	for _, value := range values {
		score, scoreErr := decimal.NewFromString(value.Score)
		obs, exists := index[evidenceKey(value.SourceKey, value.ContentSHA256)]
		if value.ManagerID == uuid.Nil || seen[value.ManagerID] || scoreErr != nil || score.String() != value.Score || !canonicalTime(value.AvailableAt) || !exists || !obs.available.Equal(value.AvailableAt) {
			return nil, nil, nil, fmt.Errorf("13f replay manager evidence is invalid")
		}
		seen[value.ManagerID] = true
		candidates = append(candidates, managerCandidateCanonical{evidenceCanonical{value.ManagerID.String(), obs.partition, value.SourceKey, value.ContentSHA256, formatTime(value.AvailableAt)}, value.Eligible, value.Score})
		if value.Eligible && !value.AvailableAt.After(policy.SelectionCutoff) {
			items = append(items, ranked{value, score, obs})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].score.Equal(items[j].score) {
			return items[i].score.GreaterThan(items[j].score)
		}
		return items[i].value.ManagerID.String() < items[j].value.ManagerID.String()
	})
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ManagerID < candidates[j].ManagerID })
	if len(items) > policy.TopN {
		items = items[:policy.TopN]
	}
	if len(items) == 0 {
		return nil, nil, nil, fmt.Errorf("13f replay selected no managers")
	}
	result := make([]managerCanonical, len(items))
	selected := map[uuid.UUID]bool{}
	for i, item := range items {
		selected[item.value.ManagerID] = true
		result[i] = managerCanonical{evidenceCanonical{item.value.ManagerID.String(), item.obs.partition, item.value.SourceKey, item.value.ContentSHA256, formatTime(item.value.AvailableAt)}, i, item.value.Score}
	}
	return result, selected, candidates, nil
}

func validateFilings(values []FilingEvidence, selected map[uuid.UUID]bool, index map[string]manifestObservation) (map[uuid.UUID][]FilingEvidence, []filingCanonical, error) {
	byKey := map[string]FilingEvidence{}
	byManager := map[uuid.UUID][]FilingEvidence{}
	identity := map[string]bool{}
	canonical := make([]filingCanonical, 0, len(values))
	for _, value := range values {
		obs, exists := index[evidenceKey(value.SourceKey, value.ContentSHA256)]
		key := value.ManagerID.String() + "\x00" + formatDate(value.ReportPeriod) + fmt.Sprintf("\x00%d", value.AmendmentNumber)
		if value.ManagerID == uuid.Nil || value.SourceKey == "" || identity[key] || !canonicalDate(value.ReportPeriod) || !canonicalTime(value.PublishedAt) || !canonicalTime(value.AvailableAt) || value.PublishedAt.After(value.AvailableAt) || value.AmendmentNumber < 0 || !exists || !obs.available.Equal(value.AvailableAt) || !obs.published.Equal(value.PublishedAt) {
			return nil, nil, fmt.Errorf("13f replay filing evidence is invalid")
		}
		identity[key] = true
		byKey[value.SourceKey] = value
		canonical = append(canonical, filingCanonical{evidenceCanonical{value.ManagerID.String(), obs.partition, value.SourceKey, value.ContentSHA256, formatTime(value.AvailableAt)}, formatDate(value.ReportPeriod), formatTime(value.PublishedAt), value.AmendmentNumber, value.SupersedesKey})
		if selected[value.ManagerID] {
			byManager[value.ManagerID] = append(byManager[value.ManagerID], value)
		}
	}
	for _, value := range values {
		if value.AmendmentNumber == 0 {
			if value.SupersedesKey != "" {
				return nil, nil, fmt.Errorf("13f replay original filing supersedes evidence")
			}
			continue
		}
		prior, ok := byKey[value.SupersedesKey]
		if !ok || prior.ManagerID != value.ManagerID || !prior.ReportPeriod.Equal(value.ReportPeriod) || prior.AmendmentNumber+1 != value.AmendmentNumber || !prior.AvailableAt.Before(value.AvailableAt) {
			return nil, nil, fmt.Errorf("13f replay amendment chain is invalid")
		}
	}
	for manager := range byManager {
		sort.Slice(byManager[manager], func(i, j int) bool {
			left, right := byManager[manager][i], byManager[manager][j]
			if !left.ReportPeriod.Equal(right.ReportPeriod) {
				return left.ReportPeriod.Before(right.ReportPeriod)
			}
			return left.AmendmentNumber < right.AmendmentNumber
		})
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].ManagerID != canonical[j].ManagerID {
			return canonical[i].ManagerID < canonical[j].ManagerID
		}
		if canonical[i].ReportPeriod != canonical[j].ReportPeriod {
			return canonical[i].ReportPeriod < canonical[j].ReportPeriod
		}
		return canonical[i].AmendmentNumber < canonical[j].AmendmentNumber
	})
	return byManager, canonical, nil
}

func chooseFiling(values []FilingEvidence, decisionAt time.Time) *FilingEvidence {
	var selected *FilingEvidence
	for i := range values {
		value := &values[i]
		if value.AvailableAt.After(decisionAt) {
			continue
		}
		if selected == nil || value.ReportPeriod.After(selected.ReportPeriod) || value.ReportPeriod.Equal(selected.ReportPeriod) && value.AmendmentNumber > selected.AmendmentNumber {
			selectedValue := *value
			selected = &selectedValue
		}
	}
	return selected
}

func FromCanonical(id uuid.UUID, digest string, raw []byte, manifest *dataset.Manifest) (*Replay, error) {
	var canonical replayCanonical
	if id == uuid.Nil || manifest == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil || canonical.Schema != SchemaV1 || canonical.State != "completed" || canonical.ManifestID != manifest.ID().String() || canonical.ManifestSHA256 != manifest.Digest() || canonical.ManifestCutoff != formatTime(manifest.DecisionCutoff()) {
		return nil, fmt.Errorf("13f replay envelope is invalid")
	}
	rebuiltInput, err := canonicalInput(canonical, manifest)
	if err != nil {
		return nil, err
	}
	rebuilt, err := NewReplay(rebuiltInput)
	if err != nil || rebuilt.Digest() != digest || !bytes.Equal(rebuilt.CanonicalBytes(), raw) {
		return nil, fmt.Errorf("13f replay canonical graph does not reconstruct")
	}
	encoded, _ := json.Marshal(canonical)
	value := &Replay{canonical, encoded, digest, economicid.DeterministicUUID("point-in-time-13f-replay", SchemaV1+"@sha256:"+digest)}
	if value.id != id || !bytes.Equal(encoded, raw) {
		return nil, fmt.Errorf("13f replay identity does not reconstruct")
	}
	return value, nil
}

func canonicalInput(c replayCanonical, manifest *dataset.Manifest) (Input, error) {
	selection, err := time.Parse("2006-01-02T15:04:05.000000Z", c.SelectionCutoff)
	if err != nil || c.TopN < 1 || len(c.CandidateManagers) == 0 || len(c.Decisions) == 0 {
		return Input{}, fmt.Errorf("13f replay canonical policy is invalid")
	}
	input := Input{Manifest: manifest, Policy: Policy{SelectionCutoff: selection, TopN: c.TopN}}
	for _, value := range c.CandidateManagers {
		managerID, idErr := uuid.Parse(value.ManagerID)
		available, timeErr := time.Parse("2006-01-02T15:04:05.000000Z", value.AvailableAt)
		if idErr != nil || timeErr != nil {
			return Input{}, fmt.Errorf("13f replay canonical manager is invalid")
		}
		input.Managers = append(input.Managers, ManagerEvidence{managerID, value.SourceKey, value.ContentSHA256, available, value.Eligible, value.Score})
	}
	for _, value := range c.Filings {
		managerID, idErr := uuid.Parse(value.ManagerID)
		report, reportErr := time.Parse("2006-01-02", value.ReportPeriod)
		published, publishedErr := time.Parse("2006-01-02T15:04:05.000000Z", value.PublishedAt)
		available, availableErr := time.Parse("2006-01-02T15:04:05.000000Z", value.AvailableAt)
		if idErr != nil || reportErr != nil || publishedErr != nil || availableErr != nil {
			return Input{}, fmt.Errorf("13f replay canonical filing is invalid")
		}
		input.Filings = append(input.Filings, FilingEvidence{managerID, value.SourceKey, value.ContentSHA256, report, published, available, value.AmendmentNumber, value.SupersedesKey})
	}
	managerCount := len(c.Managers)
	if managerCount == 0 || len(c.Decisions)%managerCount != 0 {
		return Input{}, fmt.Errorf("13f replay canonical decisions are invalid")
	}
	for i := 0; i < len(c.Decisions); i += managerCount {
		decisionAt, timeErr := time.Parse("2006-01-02T15:04:05.000000Z", c.Decisions[i].DecisionAt)
		if timeErr != nil {
			return Input{}, fmt.Errorf("13f replay canonical decision time is invalid")
		}
		input.DecisionTimes = append(input.DecisionTimes, decisionAt)
	}
	return input, nil
}

func evidenceKey(source, digest string) string { return source + "\x00" + digest }
func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}

func canonicalDate(value time.Time) bool {
	return canonicalTime(value) && value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0
}
func formatTime(value time.Time) string { return value.Format("2006-01-02T15:04:05.000000Z") }
func formatDate(value time.Time) string { return value.Format("2006-01-02") }
func hash(value []byte) string          { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("canonical JSON contains extra data")
	}
	return nil
}

func (r *Replay) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.id
}

func (r *Replay) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}

func (r *Replay) CanonicalBytes() json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage(nil), r.bytes...)
}

func (r *Replay) ManifestID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	value, _ := uuid.Parse(r.canonical.ManifestID)
	return value
}

func (r *Replay) Managers() int {
	if r == nil {
		return 0
	}
	return len(r.canonical.Managers)
}

func (r *Replay) Decisions() int {
	if r == nil {
		return 0
	}
	return len(r.canonical.Decisions)
}

func (r *Replay) PlanSteps() []experimentrun.StepInput {
	if r == nil {
		return nil
	}
	result := make([]experimentrun.StepInput, len(r.canonical.Steps))
	for i, step := range r.canonical.Steps {
		available, _ := time.Parse("2006-01-02T15:04:05.000000Z", step.AvailableAt)
		result[i] = experimentrun.StepInput{PartitionContentSHA256: step.PartitionContentSHA256, ObservationSourceKey: step.ObservationSourceKey, ObservationContentSHA256: step.ObservationContentSHA, AvailableAt: available, Decision: append(json.RawMessage(nil), step.Decision...), Action: experimentrun.ActionNoop}
	}
	return result
}
