package costattribution

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	reviewqualification "github.com/PatrickFanella/get-rich-quick/internal/evidencereview/qualification"
	researchqualification "github.com/PatrickFanella/get-rich-quick/internal/researchworkflow/qualification"
)

func reportInput(t *testing.T) Input {
	t.Helper()
	fixture, err := reviewqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	research, err := researchqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	return Input{Case: fixture.Case, Summary: fixture.Summary, Hypothesis: research.Hypothesis, Manifest: research.Parents.Manifest, AccountID: uuid.MustParse("60600000-0000-4000-8000-000000000001"), WindowStart: start, WindowEnd: end, StatementAt: end, Currency: research.Hypothesis.ProvenanceCurrency(), Lines: []LineInput{
		{Key: "model_generation", Category: CategoryModel, Status: StatusActual, Amount: research.Hypothesis.ProvenanceCost(), EvidenceKind: "research_hypothesis", EvidenceID: research.Hypothesis.ID(), EvidenceSHA256: research.Hypothesis.Digest(), Explanation: "Actual retained model invocation cost."},
		{Key: "licensed_dataset", Category: CategoryData, Status: StatusEstimated, Amount: "2.5", EvidenceKind: "dataset_manifest", EvidenceID: research.Hypothesis.ManifestID(), EvidenceSHA256: research.Hypothesis.ManifestDigest(), Method: "partition_rate_v1", MethodSHA256: strings.Repeat("a", 64), Explanation: "Estimated from retained partition volume and reviewed unit rate."},
		{Key: "execution_fee", Category: CategoryFee, Status: StatusActual, Amount: "1.25", EvidenceKind: "ledger_transaction", EvidenceID: uuid.MustParse("60600000-0000-4000-8000-000000000002"), EvidenceSHA256: strings.Repeat("b", 64), Explanation: "Actual immutable ledger fee."},
		{Key: "venue_rebate", Category: CategoryRebate, Status: StatusActual, Amount: "0.4", EvidenceKind: "ledger_transaction", EvidenceID: uuid.MustParse("60600000-0000-4000-8000-000000000003"), EvidenceSHA256: strings.Repeat("c", 64), Explanation: "Actual immutable ledger rebate."},
		{Key: "shared_infrastructure", Category: CategoryInfrastructure, Status: StatusUnknown, Explanation: "No attributable infrastructure invoice or allocation evidence was retained."},
	}}
}

func TestActualEstimatedAndUnknownRemainDistinct(t *testing.T) {
	in := reportInput(t)
	report, err := NewReport(in)
	if err != nil {
		t.Fatal(err)
	}
	totals := report.Totals()
	if totals.ActualCosts != "1.65" || totals.EstimatedCosts != "2.5" || totals.ActualRebates != "0.4" || totals.KnownNetCost != "3.75" || totals.UnknownCount != 1 || totals.Coverage != CoverageContainsUnknowns {
		t.Fatalf("totals=%+v", totals)
	}
}

func TestUnknownCannotInventZeroOrEvidence(t *testing.T) {
	in := reportInput(t)
	in.Lines[4].Amount = "0"
	if _, err := NewReport(in); err == nil {
		t.Fatal("unknown zero accepted")
	}
	in = reportInput(t)
	in.Lines[4].EvidenceSHA256 = strings.Repeat("d", 64)
	if _, err := NewReport(in); err == nil {
		t.Fatal("unknown evidence accepted")
	}
}

func TestEveryCategoryIsMandatory(t *testing.T) {
	in := reportInput(t)
	in.Lines = in.Lines[:4]
	if _, err := NewReport(in); err == nil {
		t.Fatal("missing infrastructure category accepted")
	}
}

func TestFeeAndRebateSignsAreSemantic(t *testing.T) {
	in := reportInput(t)
	in.Lines[2].Amount = "2"
	in.Lines[3].Amount = "3"
	report, err := NewReport(in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals().KnownNetCost != "1.9" {
		t.Fatalf("net=%s", report.Totals().KnownNetCost)
	}
}

func TestCurrencyAndActualModelProvenanceFailClosed(t *testing.T) {
	in := reportInput(t)
	in.Currency = "EUR"
	if _, err := NewReport(in); err == nil {
		t.Fatal("model currency mismatch accepted")
	}
	in = reportInput(t)
	in.Lines[0].Amount = "0"
	if _, err := NewReport(in); err == nil {
		t.Fatal("model cost mismatch accepted")
	}
}

func TestPermutationConvergesAndSemanticChangeDoesNot(t *testing.T) {
	in := reportInput(t)
	first, err := NewReport(in)
	if err != nil {
		t.Fatal(err)
	}
	for left, right := 0, len(in.Lines)-1; left < right; left, right = left+1, right-1 {
		in.Lines[left], in.Lines[right] = in.Lines[right], in.Lines[left]
	}
	second, err := NewReport(in)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != second.ID() || first.Digest() != second.Digest() {
		t.Fatal("permutation diverged")
	}
	in.Lines[0].Explanation += " Changed."
	third, err := NewReport(in)
	if err != nil {
		t.Fatal(err)
	}
	if third.ID() == first.ID() {
		t.Fatal("semantic change reused identity")
	}
}

func TestCompleteEstimateCoverage(t *testing.T) {
	in := reportInput(t)
	in.Lines[4] = LineInput{Key: "shared_infrastructure", Category: CategoryInfrastructure, Status: StatusEstimated, Amount: "0.75", EvidenceKind: "external_artifact", EvidenceID: uuid.MustParse("60600000-0000-4000-8000-000000000004"), EvidenceSHA256: strings.Repeat("d", 64), Method: "cpu_hour_allocation_v1", MethodSHA256: strings.Repeat("e", 64), Explanation: "Estimated from retained CPU hours and reviewed allocation rate."}
	report, err := NewReport(in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals().Coverage != CoverageWithEstimates || report.Totals().UnknownCount != 0 || report.Totals().KnownNetCost != "4.5" {
		t.Fatalf("totals=%+v", report.Totals())
	}
}

func TestReportHasNoMutationAuthoritySurface(t *testing.T) {
	report, err := NewReport(reportInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if report.ID() == uuid.Nil || len(report.CanonicalBytes()) == 0 {
		t.Fatal("missing immutable report evidence")
	}
}
