package accountingrecon

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

const DefaultParityDays = 30

// EvidenceVerifier authenticates the exact run bytes and the identities and
// capture fence they bind. Structural validation is deliberately insufficient.
type EvidenceVerifier interface {
	VerifyAccountingRun(*Run) error
}

// EvaluationClock supplies the trusted wall clock used to determine the last
// fully completed UTC evidence day. Deployment code must not derive it from
// reconciliation payloads or a database-writer-controlled timestamp.
type EvaluationClock interface {
	Now() time.Time
}

type CutoverGate struct {
	Ready             bool
	AccountID         uuid.UUID
	ThroughDate       time.Time
	EvaluatedAt       time.Time
	LastCompletedDate time.Time
	RequiredDays      int
	RunIDs            []uuid.UUID
	Reasons           []string
}

func EvaluateCutover(runs []*Run, throughDate time.Time, requiredDays int, clock EvaluationClock, verifier EvidenceVerifier) CutoverGate {
	gate := CutoverGate{ThroughDate: throughDate, RequiredDays: requiredDays}
	if requiredDays <= 0 {
		gate.Reasons = append(gate.Reasons, "required parity days must be positive")
		return gate
	}
	if throughDate.IsZero() {
		gate.Reasons = append(gate.Reasons, "through date is required")
		return gate
	}
	_, offset := throughDate.Zone()
	if offset != 0 || throughDate.Hour() != 0 || throughDate.Minute() != 0 || throughDate.Second() != 0 || throughDate.Nanosecond() != 0 {
		gate.Reasons = append(gate.Reasons, "through date must be UTC midnight")
		return gate
	}
	if verifier == nil {
		gate.Reasons = append(gate.Reasons, "authenticated accounting evidence verifier is required")
		return gate
	}
	if clock == nil {
		gate.Reasons = append(gate.Reasons, "trusted accounting evaluation clock is required")
		return gate
	}
	evaluatedAt := clock.Now()
	if evaluatedAt.IsZero() {
		gate.Reasons = append(gate.Reasons, "trusted accounting evaluation clock is required")
		return gate
	}
	gate.EvaluatedAt = evaluatedAt.UTC()
	gate.LastCompletedDate = utcDay(gate.EvaluatedAt).AddDate(0, 0, -1)
	if throughDate.After(gate.LastCompletedDate) {
		gate.Reasons = append(gate.Reasons, fmt.Sprintf("through date is later than the last completed UTC day %s", gate.LastCompletedDate.Format("2006-01-02")))
		return gate
	}

	byDate := make(map[string][]*Run)
	for _, run := range runs {
		if run == nil {
			continue
		}
		if utcDay(run.AsOf).After(gate.LastCompletedDate) {
			gate.Reasons = append(gate.Reasons, fmt.Sprintf("accounting evidence is later than the last completed UTC day: %s", utcDay(run.AsOf).Format("2006-01-02")))
			continue
		}
		if utcDay(run.AsOf).After(throughDate) {
			gate.Reasons = append(gate.Reasons, fmt.Sprintf("accounting evidence is later than through date: %s", utcDay(run.AsOf).Format("2006-01-02")))
			continue
		}
		day := utcDay(run.AsOf).Format("2006-01-02")
		byDate[day] = append(byDate[day], run)
	}

	var selected []*Run
	for offsetDays := requiredDays - 1; offsetDays >= 0; offsetDays-- {
		day := throughDate.AddDate(0, 0, -offsetDays)
		candidates := byDate[day.Format("2006-01-02")]
		if len(candidates) == 0 {
			gate.Reasons = append(gate.Reasons, fmt.Sprintf("missing qualifying accounting evidence for %s", day.Format("2006-01-02")))
			continue
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID.String() < candidates[j].ID.String() })
		candidate := candidates[0]
		conflicting := false
		for _, other := range candidates[1:] {
			if other.ID != candidate.ID || other.Checksum != candidate.Checksum || !bytes.Equal(other.PayloadBytes, candidate.PayloadBytes) ||
				other.AttestationType != candidate.AttestationType || other.AttestationKeyID != candidate.AttestationKeyID || !bytes.Equal(other.Attestation, candidate.Attestation) {
				conflicting = true
				break
			}
		}
		if conflicting {
			gate.Reasons = append(gate.Reasons, fmt.Sprintf("conflicting accounting evidence for %s", day.Format("2006-01-02")))
			continue
		}
		if reason := nonqualifyingReason(candidate, verifier); reason != "" {
			gate.Reasons = append(gate.Reasons, fmt.Sprintf("accounting evidence for %s is not qualifying: %s", day.Format("2006-01-02"), reason))
			continue
		}
		selected = append(selected, candidate)
	}

	if len(selected) != requiredDays || len(gate.Reasons) != 0 {
		return gate
	}
	baseline := selected[0]
	gate.AccountID = baseline.AccountID
	for _, run := range selected {
		if run.AccountID != baseline.AccountID || run.PolicyVersion != baseline.PolicyVersion ||
			run.ProjectionVersion != baseline.ProjectionVersion || run.MarkSource != baseline.MarkSource ||
			run.MarkNamespace != baseline.MarkNamespace || run.MaxMarkAge != baseline.MaxMarkAge {
			gate.Reasons = append(gate.Reasons, "accounting parity window changes account or comparison/mark policy")
			return gate
		}
		gate.RunIDs = append(gate.RunIDs, run.ID)
	}
	gate.Ready = true
	return gate
}

func nonqualifyingReason(run *Run, verifier EvidenceVerifier) string {
	if err := run.Validate(); err != nil {
		return "canonical run validation failed"
	}
	if run.Synthetic {
		return "synthetic evidence cannot qualify"
	}
	if !run.Legacy.PositionCoverageComplete || !run.Ledger.PositionCoverageComplete {
		return "position coverage is incomplete"
	}
	if run.UnexplainedCount != 0 || run.NotComparableCount != 0 {
		return "unexplained or not-comparable results remain"
	}
	seenRequired := make(map[string]struct{}, len(requiredMetrics))
	for _, result := range run.Results {
		if !result.Status.valid() || result.Status == StatusUnexplained || result.Status == StatusNotComparable {
			return "nonqualifying result status remains"
		}
		if result.Status == StatusExplained && result.Explanation == nil {
			return "explained result lacks reviewed evidence"
		}
		for _, metric := range requiredMetrics {
			if result.FactKey == MetricFactKey(metric) {
				seenRequired[result.FactKey] = struct{}{}
			}
		}
	}
	if len(seenRequired) != len(requiredMetrics) {
		return "required accounting metrics are incomplete"
	}
	if err := verifier.VerifyAccountingRun(run); err != nil {
		return "run attestation is absent, unknown, revoked, or invalid"
	}
	return ""
}

func utcDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
