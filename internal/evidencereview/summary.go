package evidencereview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const SummarySchemaV1 = "evidence-review-summary-v1"

type SummaryInput struct {
	Case        *Case
	ReviewHeads []*Review
}
type summaryReviewCanonical struct {
	Sequence     int    `json:"sequence"`
	ReviewID     string `json:"review_id"`
	ReviewSHA256 string `json:"review_sha256"`
	ReviewerKey  string `json:"reviewer_key"`
	ReviewedAt   string `json:"reviewed_at"`
	Disposition  string `json:"disposition"`
}
type summaryCheckCanonical struct {
	Sequence     int    `json:"sequence"`
	Name         string `json:"name"`
	PassCount    int    `json:"pass_count"`
	FailCount    int    `json:"fail_count"`
	UnknownCount int    `json:"unknown_count"`
}
type summaryCanonical struct {
	Schema                 string                   `json:"schema"`
	State                  string                   `json:"state"`
	CaseID                 string                   `json:"case_id"`
	CaseSHA256             string                   `json:"case_sha256"`
	ReviewHeads            []summaryReviewCanonical `json:"review_heads"`
	Checks                 []summaryCheckCanonical  `json:"checks"`
	Consensus              string                   `json:"consensus"`
	UnresolvedChecks       []string                 `json:"unresolved_checks"`
	EscalationRequired     bool                     `json:"escalation_required"`
	AuthoritativeOutcome   string                   `json:"authoritative_outcome"`
	AuthoritativeNextState string                   `json:"authoritative_next_state"`
}
type Summary struct {
	canonical summaryCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewSummary(input SummaryInput) (*Summary, error) {
	if input.Case == nil || len(input.ReviewHeads) < 2 || len(input.ReviewHeads) > 32 {
		return nil, fmt.Errorf("evidence review summary requires bounded review heads")
	}
	heads := make([]summaryReviewCanonical, 0, len(input.ReviewHeads))
	byReviewer := map[string]struct{}{}
	counts := map[string]*summaryCheckCanonical{}
	dispositions := map[string]int{}
	for _, review := range input.ReviewHeads {
		if review == nil || review.CaseID() != input.Case.ID() {
			return nil, fmt.Errorf("evidence review summary head binding is invalid")
		}
		if _, duplicate := byReviewer[review.ReviewerKey()]; duplicate {
			return nil, fmt.Errorf("evidence review summary reviewer head is duplicated")
		}
		byReviewer[review.ReviewerKey()] = struct{}{}
		heads = append(heads, summaryReviewCanonical{ReviewID: review.ID().String(), ReviewSHA256: review.Digest(), ReviewerKey: review.ReviewerKey(), ReviewedAt: formatTime(review.ReviewedAt()), Disposition: review.Disposition()})
		dispositions[review.Disposition()]++
		for _, check := range review.canonical.Checks {
			row := counts[check.Name]
			if row == nil {
				row = &summaryCheckCanonical{Name: check.Name}
				counts[check.Name] = row
			}
			switch check.State {
			case "pass":
				row.PassCount++
			case "fail":
				row.FailCount++
			case "unknown":
				row.UnknownCount++
			default:
				return nil, fmt.Errorf("evidence review summary check state is invalid")
			}
		}
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].ReviewerKey < heads[j].ReviewerKey })
	for index := range heads {
		heads[index].Sequence = index
	}
	checks := make([]summaryCheckCanonical, 0, len(counts))
	unresolved := []string{}
	for _, name := range requiredChecks {
		row := counts[name]
		if row == nil || row.PassCount+row.FailCount+row.UnknownCount != len(heads) {
			return nil, fmt.Errorf("evidence review summary checks are incomplete")
		}
		if row.FailCount > 0 || row.UnknownCount > 0 {
			unresolved = append(unresolved, name)
		}
		checks = append(checks, *row)
	}
	for index := range checks {
		checks[index].Sequence = index
	}
	consensus := "disagreement"
	if len(dispositions) == 1 {
		for value := range dispositions {
			consensus = value
		}
	}
	escalation := consensus != "evidence_supported" || len(unresolved) > 0
	c := summaryCanonical{SummarySchemaV1, "completed", input.Case.ID().String(), input.Case.Digest(), heads, checks, consensus, unresolved, escalation, input.Case.AuthoritativeOutcome(), input.Case.AuthoritativeNextState()}
	raw, _ := json.Marshal(c)
	digest := hash(raw)
	return &Summary{c, raw, digest, economicid.DeterministicUUID("evidence-review-summary", SummarySchemaV1+"@sha256:"+digest)}, nil
}

func SummaryFromCanonical(id uuid.UUID, digest string, raw json.RawMessage, input SummaryInput) (*Summary, error) {
	var c summaryCanonical
	if id == uuid.Nil || hash(raw) != digest || decodeExact(raw, &c) != nil {
		return nil, fmt.Errorf("stored evidence review summary is invalid")
	}
	rebuilt, err := NewSummary(input)
	if err != nil || rebuilt.id != id || rebuilt.digest != digest || !bytes.Equal(rebuilt.bytes, raw) {
		return nil, fmt.Errorf("stored evidence review summary does not reconstruct")
	}
	return rebuilt, nil
}

func (s *Summary) ID() uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	return s.id
}

func (s *Summary) Digest() string {
	if s == nil {
		return ""
	}
	return s.digest
}

func (s *Summary) CanonicalBytes() json.RawMessage {
	if s == nil {
		return nil
	}
	return append(json.RawMessage(nil), s.bytes...)
}

func (s *Summary) Consensus() string {
	if s == nil {
		return ""
	}
	return s.canonical.Consensus
}
func (s *Summary) EscalationRequired() bool { return s != nil && s.canonical.EscalationRequired }
func (s *Summary) CaseID() uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	return uuid.MustParse(s.canonical.CaseID)
}

func (s *Summary) CaseDigest() string {
	if s == nil {
		return ""
	}
	return s.canonical.CaseSHA256
}

func (s *Summary) AuthoritativeOutcome() string {
	if s == nil {
		return ""
	}
	return s.canonical.AuthoritativeOutcome
}

func (s *Summary) AuthoritativeNextState() string {
	if s == nil {
		return ""
	}
	return s.canonical.AuthoritativeNextState
}
