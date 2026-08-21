package accountingrecon

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestCompareClassifiesEqualUnexplainedExplainedAndNotComparable(t *testing.T) {
	t.Parallel()

	legacyInput := validSnapshotInput(SourceLegacy)
	ledgerInput := validSnapshotInput(SourceLedger)
	legacyInput.Metrics = []MetricInput{
		{Kind: MetricCash, Value: decimal.NewFromInt(100), Provenance: ProvenanceBinaryFloat},
		{Kind: MetricBuyingPower, Value: decimal.NewFromInt(200), Provenance: ProvenanceBinaryFloat},
		{Kind: MetricFees, Value: decimal.RequireFromString("1.25"), Provenance: ProvenanceExactDecimal},
	}
	ledgerInput.Metrics = []MetricInput{
		{Kind: MetricCash, Value: decimal.NewFromInt(100), Provenance: ProvenanceExactDecimal},
		{Kind: MetricBuyingPower, Value: decimal.NewFromInt(199), Provenance: ProvenanceExactDecimal},
	}
	legacyInput.Missing = []MissingFactInput{{FactKey: MetricFactKey(MetricEquity), ReasonCode: MissingSourceUnavailable, EvidenceRef: "legacy:equity"}}
	ledgerInput.Missing = []MissingFactInput{{FactKey: MetricFactKey(MetricFees), ReasonCode: MissingSourceUnavailable, EvidenceRef: "ledger:fees"}, {FactKey: MetricFactKey(MetricEquity), ReasonCode: MissingSourceUnavailable, EvidenceRef: "ledger:equity"}}
	legacy, err := NewSnapshot(legacyInput)
	if err != nil {
		t.Fatal(err)
	}
	ledgerSnapshot, err := NewSnapshot(ledgerInput)
	if err != nil {
		t.Fatal(err)
	}

	run, err := Compare(ComparisonInput{
		Legacy:      legacy,
		Ledger:      ledgerSnapshot,
		Generator:   "dual-run-worker",
		GeneratedAt: legacy.AsOf.Add(2 * time.Hour),
		Explanations: []ExplanationInput{{
			FactKey:          MetricFactKey(MetricBuyingPower),
			Code:             ExplanationSourceCorrection,
			Rationale:        "reviewed source correction explains the historical difference",
			EvidenceRef:      "sha256:policy-review",
			EvidenceChecksum: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Generator:        "dual-run-worker",
			Reviewer:         "accounting-reviewer",
			ReviewedAt:       legacy.AsOf.Add(time.Hour),
		}},
	})
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	assertResultStatus(t, run, MetricFactKey(MetricCash), StatusEqual)
	assertResultStatus(t, run, MetricFactKey(MetricBuyingPower), StatusExplained)
	assertResultStatus(t, run, MetricFactKey(MetricFees), StatusNotComparable)
	assertResultStatus(t, run, MetricFactKey(MetricEquity), StatusNotComparable)
	assertResultStatus(t, run, MetricFactKey(MetricRealizedPnL), StatusNotComparable)
	if run.EqualCount != 1 || run.ExplainedCount != 1 || run.NotComparableCount < 2 || run.UnexplainedCount != 0 {
		t.Fatalf("unexpected counts: equal=%d explained=%d unexplained=%d not_comparable=%d", run.EqualCount, run.ExplainedCount, run.UnexplainedCount, run.NotComparableCount)
	}
	if err := run.Validate(); err != nil {
		t.Fatalf("run Validate() error = %v", err)
	}
}

func TestCompareUsesExactDecimalsAndPositionUnion(t *testing.T) {
	t.Parallel()

	instrumentID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	extraID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	legacyInput := completeSnapshotInput(SourceLegacy)
	ledgerInput := completeSnapshotInput(SourceLedger)
	setMetricValue(&legacyInput, MetricCash, decimal.RequireFromString("100.000000000001"))
	setMetricValue(&ledgerInput, MetricCash, decimal.NewFromInt(100))
	legacyInput.Positions = []PositionInput{{InstrumentID: instrumentID, Quantity: decimal.NewFromInt(2), Provenance: ProvenanceExactDecimal}}
	ledgerInput.Positions = []PositionInput{{InstrumentID: instrumentID, Quantity: decimal.NewFromInt(1), Provenance: ProvenanceExactDecimal}, {InstrumentID: extraID, Quantity: decimal.NewFromInt(-3), Provenance: ProvenanceExactDecimal}}
	legacy, _ := NewSnapshot(legacyInput)
	ledgerSnapshot, _ := NewSnapshot(ledgerInput)
	run, err := Compare(ComparisonInput{Legacy: legacy, Ledger: ledgerSnapshot, Generator: "worker", GeneratedAt: legacy.AsOf.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	assertResultStatus(t, run, MetricFactKey(MetricCash), StatusUnexplained)
	assertResultStatus(t, run, PositionFactKey(instrumentID), StatusUnexplained)
	assertResultStatus(t, run, PositionFactKey(extraID), StatusUnexplained)
}

func TestCompareRejectsForgedOrInapplicableExplanation(t *testing.T) {
	t.Parallel()

	legacy, ledgerSnapshot := completePair(t)
	base := ExplanationInput{
		FactKey: MetricFactKey(MetricCash), Code: ExplanationLegacyFloat,
		Rationale:        "reviewed exact float representation difference",
		EvidenceRef:      "sha256:evidence",
		EvidenceChecksum: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Generator:        "worker", Reviewer: "reviewer", ReviewedAt: legacy.AsOf.Add(time.Hour),
	}
	tests := map[string]func(*ExplanationInput){
		"unknown code":    func(value *ExplanationInput) { value.Code = "anything" },
		"same reviewer":   func(value *ExplanationInput) { value.Reviewer = value.Generator },
		"bad hash":        func(value *ExplanationInput) { value.EvidenceChecksum = "bad" },
		"blank rationale": func(value *ExplanationInput) { value.Rationale = " " },
		"future review":   func(value *ExplanationInput) { value.ReviewedAt = time.Now().UTC().Add(24 * time.Hour) },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := base
			mutate(&value)
			if _, err := Compare(ComparisonInput{Legacy: legacy, Ledger: ledgerSnapshot, Generator: "worker", GeneratedAt: legacy.AsOf.Add(2 * time.Hour), Explanations: []ExplanationInput{value}}); err == nil {
				t.Fatal("Compare() unexpectedly succeeded")
			}
		})
	}
}

func assertResultStatus(t *testing.T, run *Run, key string, want ResultStatus) {
	t.Helper()
	for _, result := range run.Results {
		if result.FactKey == key {
			if result.Status != want {
				t.Fatalf("result %q status = %q, want %q", key, result.Status, want)
			}
			return
		}
	}
	t.Fatalf("result %q not found", key)
}

func completeSnapshotInput(source SnapshotSource) SnapshotInput {
	input := validSnapshotInput(source)
	for _, kind := range RequiredMetrics() {
		input.Metrics = append(input.Metrics, MetricInput{Kind: kind, Value: decimal.NewFromInt(100), Provenance: ProvenanceExactDecimal})
	}
	return input
}

func completePair(t *testing.T) (*Snapshot, *Snapshot) {
	t.Helper()
	legacyInput := completeSnapshotInput(SourceLegacy)
	ledgerInput := completeSnapshotInput(SourceLedger)
	setMetricValue(&legacyInput, MetricCash, decimal.RequireFromString("100.1"))
	legacy, err := NewSnapshot(legacyInput)
	if err != nil {
		t.Fatal(err)
	}
	ledgerSnapshot, err := NewSnapshot(ledgerInput)
	if err != nil {
		t.Fatal(err)
	}
	return legacy, ledgerSnapshot
}

func setMetricValue(input *SnapshotInput, kind MetricKind, value decimal.Decimal) {
	for index := range input.Metrics {
		if input.Metrics[index].Kind == kind {
			input.Metrics[index].Value = value
			return
		}
	}
}
