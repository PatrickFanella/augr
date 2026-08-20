package evidenceprogram

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func ref(kind string, n byte) EvidenceRef {
	return EvidenceRef{Kind: kind, ID: uuid.MustParse(fmt.Sprintf("70000000-0000-4000-8000-%012x", n)), SHA256: fmt.Sprintf("%064x", n)}
}

func interval(days int) (time.Time, time.Time) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return start, start.Add(time.Duration(days) * 24 * time.Hour)
}

func qualifiedShadow(t *testing.T) *Assessment {
	t.Helper()
	start, end := interval(30)
	value, err := AssessShadow(ShadowInput{StartedAt: start, EndedAt: end, DailyComplete: true, Parents: []EvidenceRef{ref("candidate_set", 1)}, Candidates: []CandidateShadow{{"alpha", 30, 0, 100, 80, true, "0.12"}, {"beta", 30, 0, 90, 70, true, "-0.03"}}})
	if err != nil || value.Outcome() != OutcomeQualified {
		t.Fatalf("shadow=%v/%v", value, err)
	}
	return value
}

func qualifiedPaper(t *testing.T) *Assessment {
	t.Helper()
	start, end := interval(60)
	value, err := AssessPaper(PaperInput{Shadow: qualifiedShadow(t), StartedAt: start, EndedAt: end, Parents: []EvidenceRef{ref("cost_report", 2)}, Candidates: []CandidatePaper{{"alpha", 120, "0.004", true, true, true}, {"beta", 110, "-0.002", true, true, true}}})
	if err != nil || value.Outcome() != OutcomeQualified {
		t.Fatalf("paper=%v/%v", value, err)
	}
	return value
}

func qualifiedPortfolio(t *testing.T) *Assessment {
	t.Helper()
	start, end := interval(60)
	value, err := AssessPortfolio(PortfolioInput{Paper: qualifiedPaper(t), StartedAt: start, EndedAt: end, CombinedRiskAdjusted: "1.05", BestSingleRiskAdjusted: "1.00", SameInterval: true, SameCostBasis: true, Parents: []EvidenceRef{ref("allocation", 3)}})
	if err != nil || value.Outcome() != OutcomeQualified {
		t.Fatalf("portfolio=%v/%v", value, err)
	}
	return value
}

func TestShadowRequiresRealCampaignShape(t *testing.T) {
	start, end := interval(29)
	value, err := AssessShadow(ShadowInput{StartedAt: start, EndedAt: end, Candidates: []CandidateShadow{{"alpha", 29, 1, 0, 0, false, ""}}, Parents: []EvidenceRef{ref("candidate_set", 1)}})
	if err != nil || value.Outcome() != OutcomeHeld || len(value.Blockers()) < 5 {
		t.Fatalf("shadow=%+v err=%v", value.Record(), err)
	}
}

func TestPaperAcceptsPositiveOrHonestRejection(t *testing.T) {
	_ = qualifiedPaper(t)
	start, end := interval(90)
	rejected, err := AssessPaper(PaperInput{Shadow: qualifiedShadow(t), StartedAt: start, EndedAt: end, Candidates: []CandidatePaper{{"alpha", 100, "-0.01", true, true, true}, {"beta", 100, "0", true, true, true}}})
	if err != nil || rejected.Outcome() != OutcomeRejected || len(rejected.Blockers()) != 0 {
		t.Fatalf("rejected=%+v err=%v", rejected.Record(), err)
	}
	held, err := AssessPaper(PaperInput{Shadow: qualifiedShadow(t), StartedAt: start, EndedAt: end, Candidates: []CandidatePaper{{"alpha", 100, "0.01", false, true, false}}})
	if err != nil || held.Outcome() != OutcomeHeld {
		t.Fatalf("held=%+v err=%v", held.Record(), err)
	}
}

func TestPortfolioRequiresComparableImprovement(t *testing.T) {
	start, end := interval(60)
	rejected, err := AssessPortfolio(PortfolioInput{Paper: qualifiedPaper(t), StartedAt: start, EndedAt: end, CombinedRiskAdjusted: "0.99", BestSingleRiskAdjusted: "1.00", SameInterval: true, SameCostBasis: true})
	if err != nil || rejected.Outcome() != OutcomeRejected || len(rejected.Blockers()) != 0 {
		t.Fatalf("portfolio=%+v err=%v", rejected.Record(), err)
	}
	held, err := AssessPortfolio(PortfolioInput{Paper: qualifiedPaper(t), StartedAt: start, EndedAt: end, CombinedRiskAdjusted: "1.1", BestSingleRiskAdjusted: "1.0", SameInterval: false, SameCostBasis: false})
	if err != nil || held.Outcome() != OutcomeHeld {
		t.Fatalf("held=%+v err=%v", held.Record(), err)
	}
}

func TestReadinessRequiresEveryExactCapability(t *testing.T) {
	capabilities := make([]Capability, 0, len(requiredCapabilities))
	for i, name := range requiredCapabilities {
		capabilities = append(capabilities, Capability{Name: name, Passed: true, Evidence: ref("qualification", byte(i+1))})
	}
	ready, err := AssessReadiness(ReadinessInput{Portfolio: qualifiedPortfolio(t), Capabilities: capabilities})
	if err != nil {
		t.Fatal(err)
	}
	if ready.Outcome() != OutcomeReady {
		t.Fatalf("ready=%+v", ready.Record())
	}
	capabilities[2].Passed = false
	notReady, err := AssessReadiness(ReadinessInput{Portfolio: qualifiedPortfolio(t), Capabilities: capabilities})
	if err != nil {
		t.Fatal(err)
	}
	if notReady.Outcome() != OutcomeNotReady {
		t.Fatalf("not_ready=%+v", notReady.Record())
	}
	start, end := interval(60)
	heldPortfolio, err := AssessPortfolio(PortfolioInput{Paper: qualifiedPaper(t), StartedAt: start, EndedAt: end, CombinedRiskAdjusted: "1.1", BestSingleRiskAdjusted: "1.0", SameInterval: false, SameCostBasis: true})
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := AssessReadiness(ReadinessInput{Portfolio: heldPortfolio, Capabilities: capabilities})
	if err != nil || blocked.Outcome() != OutcomeBlocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
}

func TestAssessmentsAreDeterministicAndHaveNoMutationSurface(t *testing.T) {
	first := qualifiedPortfolio(t)
	second := qualifiedPortfolio(t)
	if first.ID() != second.ID() || first.Digest() != second.Digest() || !bytes.Equal(first.CanonicalBytes(), second.CanonicalBytes()) {
		t.Fatal("assessment diverged")
	}
	copyBytes := first.CanonicalBytes()
	copyBytes[0] = 'x'
	if bytes.Equal(copyBytes, first.CanonicalBytes()) {
		t.Fatal("canonical bytes alias mutable storage")
	}
}
