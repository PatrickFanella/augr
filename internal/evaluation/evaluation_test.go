package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestReportSeparatesTradeEvidenceFromCurveDescriptor(t *testing.T) {
	input := validReportInput(t)
	first, err := NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewReport(input)
	if err != nil || first.ID() != retry.ID() || first.Digest() != retry.Digest() {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
	if first.ID() != uuid.MustParse("68079024-f88b-9e5f-f57b-8cb254fd3d73") || first.Digest() != "133bdea6283cd87180a26769c65dbf1379a276c2e600519101473d0c0b295181" {
		t.Fatalf("golden identity changed: %s/%s", first.ID(), first.Digest())
	}
	tradeWin := requireMetric(t, first, "trade", "win_rate")
	barPositive := requireMetric(t, first, "curve_diagnostics", "bar_positive_return_rate")
	if tradeWin.Value != "0.5" || tradeWin.Description != "closed_trade_after_cost_win_rate_not_bar_return_rate" {
		t.Fatalf("trade win metric=%+v", tradeWin)
	}
	if barPositive.Value != "0.666666666667" || barPositive.Description != "descriptor_only_not_trade_win_rate" {
		t.Fatalf("curve descriptor=%+v", barPositive)
	}
	if requireMetric(t, first, "trade", "expectancy").Value != "1.5" || requireMetric(t, first, "trade", "profit_factor").Value != "1.5" {
		t.Fatalf("trade metrics=%+v", first.Metrics())
	}
	restored, err := ReportFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes(), input.Result, input.Policy)
	if err != nil || !bytes.Equal(restored.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatalf("restored=%v err=%v", restored, err)
	}
}

func TestReportMetricAvailabilityAndInfinityAreExplicit(t *testing.T) {
	input := validReportInput(t)
	input.ClosedTrades = input.ClosedTrades[:1]
	input.ClosedTrades[0].GrossPnL = "12"
	input.ClosedTrades[0].AfterCostPnL = "9"
	input.Observations[len(input.Observations)-1].CumulativeObservedSlippage = nil
	report, err := NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	profitFactor := requireMetric(t, report, "trade", "profit_factor")
	if profitFactor.State != MetricPositiveInfinity || profitFactor.Value != "" {
		t.Fatalf("profit factor=%+v", profitFactor)
	}
	observed := requireMetric(t, report, "cost", "observed_slippage")
	if observed.State != MetricUnavailable || observed.Reason != "observed_slippage_not_available" || observed.Value != "" {
		t.Fatalf("observed slippage=%+v", observed)
	}

	input.ClosedTrades = nil
	report, err = NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	if metric := requireMetric(t, report, "trade", "win_rate"); metric.State != MetricUnavailable || metric.Reason != "no_closed_trades" {
		t.Fatalf("no-trade metric=%+v", metric)
	}
}

func TestReportRejectsTamperFutureAndInconsistentCosts(t *testing.T) {
	input := validReportInput(t)
	report, err := NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(report.CanonicalBytes(), []byte(`"win_rate","state":"available","value":"0.5"`), []byte(`"win_rate","state":"available","value":"0.9"`), 1)
	if _, err := ReportFromCanonical(economicid.DeterministicUUID("trade-portfolio-evaluation", ReportSchemaV1+"@sha256:"+hash(tampered)), hash(tampered), tampered, input.Result, input.Policy); err == nil {
		t.Fatal("tampered derived metric restored")
	}

	invalid := validReportInput(t)
	invalid.Observations[2].ObservedAt = invalid.EvaluationEnd.Add(time.Microsecond)
	if _, err := NewReport(invalid); err == nil {
		t.Fatal("future observation succeeded")
	}
	invalid = validReportInput(t)
	invalid.ClosedTrades[0].AfterCostPnL = "10"
	if _, err := NewReport(invalid); err == nil {
		t.Fatal("inconsistent after-cost pnl succeeded")
	}
	invalid = validReportInput(t)
	invalid.Observations[2].CumulativeOwnershipCost = "0.5"
	if _, err := NewReport(invalid); err == nil {
		t.Fatal("decreasing cumulative cost succeeded")
	}
}

func TestPolicyRestoreAndReportModeSeparation(t *testing.T) {
	policy, err := NewPolicy(PolicyInput{
		Version: "evaluation-policy-v1@reviewed", Frequency: "daily", PeriodsPerYear: 252,
		ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo",
		RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := PolicyFromCanonical(policy.ID(), policy.Digest(), policy.CanonicalBytes())
	if err != nil || restored.ID() != policy.ID() {
		t.Fatalf("policy restore=%v err=%v", restored, err)
	}
	scoredInput := validReportInput(t)
	scored, _ := NewReport(scoredInput)
	stressPlan, err := evaluationPlan(strategycatalog.ExperimentPaperStress)
	if err != nil {
		t.Fatal(err)
	}
	stressResult, err := evaluationResult(stressPlan)
	if err != nil {
		t.Fatal(err)
	}
	stressInput := scoredInput
	stressInput.Result = stressResult
	stress, err := NewReport(stressInput)
	if err != nil || stress.ID() == scored.ID() || stress.Mode() != strategycatalog.ExperimentPaperStress {
		t.Fatalf("stress=%v err=%v", stress, err)
	}
}

func TestReportEdgeCaseAvailability(t *testing.T) {
	input := validReportInput(t)
	input.ClosedTrades = input.ClosedTrades[1:]
	report, err := NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	if metric := requireMetric(t, report, "trade", "profit_factor"); metric.State != MetricAvailable || metric.Value != "0" {
		t.Fatalf("all-loss profit factor=%+v", metric)
	}
	if metric := requireMetric(t, report, "portfolio", "maximum_drawdown_recovery_periods"); metric.State != MetricUnavailable || metric.Reason != "drawdown_unrecovered" {
		t.Fatalf("unrecovered drawdown=%+v", metric)
	}

	input = validReportInput(t)
	input.ClosedTrades = input.ClosedTrades[:1]
	input.ClosedTrades[0].GrossPnL = "0"
	input.ClosedTrades[0].AfterCostPnL = "0"
	input.ClosedTrades[0].EntryFees = "0"
	input.ClosedTrades[0].ExitFees = "0"
	input.ClosedTrades[0].OtherOwnershipCost = "0"
	input.Execution = ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"}
	report, err = NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	if metric := requireMetric(t, report, "trade", "profit_factor"); metric.State != MetricUnavailable || metric.Reason != "no_winning_and_losing_closed_trades" {
		t.Fatalf("breakeven profit factor=%+v", metric)
	}
	for _, name := range []string{"order_fill_ratio", "quantity_fill_ratio"} {
		if metric := requireMetric(t, report, "execution", name); metric.State != MetricUnavailable {
			t.Fatalf("zero-attempt %s=%+v", name, metric)
		}
	}

	input = validReportInput(t)
	input.EvaluationEnd = input.Observations[1].ObservedAt
	input.Observations = input.Observations[:2]
	input.ClosedTrades = nil
	report, err = NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tracking_error", "information_ratio", "sharpe_ratio", "sortino_ratio"} {
		if metric := requireMetric(t, report, "portfolio", name); metric.State != MetricUnavailable || metric.Reason != "insufficient_return_samples" {
			t.Fatalf("short-series %s=%+v", name, metric)
		}
	}
}

func TestReportRejectsWindowFrequencyAndNoncanonicalExecution(t *testing.T) {
	for name, mutate := range map[string]func(*ReportInput){
		"window start gap": func(input *ReportInput) { input.Observations[0].ObservedAt = input.EvaluationStart.Add(time.Hour) },
		"window end gap": func(input *ReportInput) {
			input.Observations[len(input.Observations)-1].ObservedAt = input.EvaluationEnd.Add(-time.Hour)
		},
		"irregular daily": func(input *ReportInput) {
			input.Observations[1].ObservedAt = input.Observations[1].ObservedAt.Add(time.Hour)
		},
		"noncanonical count": func(input *ReportInput) {
			input.Execution.AttemptedOrders = "04"
		},
		"oversized execution": func(input *ReportInput) {
			input.Execution.AttemptedQuantity = "1000000000000000000000000000001"
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := validReportInput(t)
			mutate(&input)
			if _, err := NewReport(input); err == nil {
				t.Fatal("invalid report succeeded")
			}
		})
	}
}

func TestReportCanonicalCloneSafetyAndTradeOrderConvergence(t *testing.T) {
	input := validReportInput(t)
	report, err := NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	raw := report.CanonicalBytes()
	raw[0] = 'x'
	observations := report.Observations()
	*observations[0].CumulativeObservedSlippage = "999"
	trades := report.ClosedTrades()
	trades[0].EntryFillIDs[0] = uuid.Nil
	if !bytes.Equal(report.CanonicalBytes(), inputReportBytes(t, input)) {
		t.Fatal("report accessors leaked mutable canonical state")
	}

	reordered := validReportInput(t)
	reordered.ClosedTrades[0], reordered.ClosedTrades[1] = reordered.ClosedTrades[1], reordered.ClosedTrades[0]
	retry, err := NewReport(reordered)
	if err != nil || retry.ID() != report.ID() || retry.Digest() != report.Digest() {
		t.Fatalf("semantic-free trade reorder did not converge: %v", err)
	}
}

func TestReportExtremeBoundedDecimalsMakeOverflowExplicit(t *testing.T) {
	input := validReportInput(t)
	input.Observations[0].Equity = "0.000000000000000000000000000001"
	input.Observations[1].Equity = "1"
	input.Observations[2].Equity = "1000000000000000000000000000000"
	input.Observations[3].Equity = "1000000000000000000000000000000"
	report, err := NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	if metric := requireMetric(t, report, "portfolio", "after_cost_annualized_return"); metric.State != MetricUnavailable || metric.Reason != "numeric_range_exceeded" || metric.Value != "" {
		t.Fatalf("overflowing annualized return=%+v", metric)
	}
	if metric := requireMetric(t, report, "portfolio", "calmar_ratio"); metric.State != MetricUnavailable || metric.Reason != "zero_maximum_drawdown" {
		t.Fatalf("zero-drawdown calmar=%+v", metric)
	}
}

func TestServiceReloadsResultAndPersistsExactReport(t *testing.T) {
	input := validReportInput(t)
	store := &evaluationStore{result: input.Result}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	resultID := input.Result.ID()
	input.Result = nil
	report, err := service.Evaluate(context.Background(), Request{ResultID: resultID, ReportInput: input})
	if err != nil {
		t.Fatal(err)
	}
	if store.loaded != resultID || store.policy == nil || store.report == nil || report.ID() != store.report.ID() {
		t.Fatalf("service calls loaded=%s policy=%v report=%v", store.loaded, store.policy, store.report)
	}
}

func validReportInput(t *testing.T) ReportInput {
	t.Helper()
	plan, err := evaluationPlan(strategycatalog.ExperimentPaperScored)
	if err != nil {
		t.Fatal(err)
	}
	result, err := evaluationResult(plan)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy(PolicyInput{
		Version: "evaluation-policy-v1@reviewed", Frequency: "daily", PeriodsPerYear: 252,
		ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo",
		RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 20, 15, 0, 0, 123456000, time.UTC)
	observed := "2.5"
	return ReportInput{
		Result: result, Policy: policy, EvaluationStart: start, EvaluationEnd: start.Add(72 * time.Hour), OpenLotCount: 1,
		Execution: ExecutionInput{AttemptedOrders: "4", FilledOrders: "3", AttemptedQuantity: "40", FilledQuantity: "30"},
		Observations: []ObservationInput{
			{ObservedAt: start, Equity: "100", BenchmarkValue: "100", CashReturn: "0", GrossExposure: "50", NetExposure: "50", LargestPositionWeight: "0.5", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", CumulativeObservedSlippage: text("0"), EvidenceID: uuid.MustParse("30400000-0000-4000-8000-000000000001"), EvidenceSHA256: strings.Repeat("1", 64)},
			{ObservedAt: start.Add(24 * time.Hour), Equity: "110", BenchmarkValue: "102", CashReturn: "0.0001", GrossExposure: "60", NetExposure: "40", LargestPositionWeight: "0.4", CumulativeOwnershipCost: "1", CumulativeTurnover: "0.4", CumulativeModeledSlippage: "1", CumulativeObservedSlippage: text("0.8"), EvidenceID: uuid.MustParse("30400000-0000-4000-8000-000000000002"), EvidenceSHA256: strings.Repeat("2", 64)},
			{ObservedAt: start.Add(48 * time.Hour), Equity: "121", BenchmarkValue: "101", CashReturn: "0.0001", GrossExposure: "70", NetExposure: "30", LargestPositionWeight: "0.35", CumulativeOwnershipCost: "2", CumulativeTurnover: "0.8", CumulativeModeledSlippage: "2", CumulativeObservedSlippage: text("1.6"), EvidenceID: uuid.MustParse("30400000-0000-4000-8000-000000000003"), EvidenceSHA256: strings.Repeat("3", 64)},
			{ObservedAt: start.Add(72 * time.Hour), Equity: "108.9", BenchmarkValue: "103", CashReturn: "0.0001", GrossExposure: "55", NetExposure: "20", LargestPositionWeight: "0.3", CumulativeOwnershipCost: "3", CumulativeTurnover: "1.2", CumulativeModeledSlippage: "3", CumulativeObservedSlippage: &observed, EvidenceID: uuid.MustParse("30400000-0000-4000-8000-000000000004"), EvidenceSHA256: strings.Repeat("4", 64)},
		},
		ClosedTrades: []ClosedTradeInput{
			{InstrumentID: uuid.MustParse("30400000-0000-4000-8000-000000000010"), Side: "long", Quantity: "5", EntryFillIDs: []uuid.UUID{uuid.MustParse("30400000-0000-4000-8000-000000000011")}, ExitFillIDs: []uuid.UUID{uuid.MustParse("30400000-0000-4000-8000-000000000012")}, EntryAt: start.Add(time.Hour), ExitAt: start.Add(25 * time.Hour), EntryPrice: "10", ExitPrice: "12.4", EntryFees: "1", ExitFees: "1", OtherOwnershipCost: "1", GrossPnL: "12", AfterCostPnL: "9"},
			{InstrumentID: uuid.MustParse("30400000-0000-4000-8000-000000000020"), Side: "short", Quantity: "2", EntryFillIDs: []uuid.UUID{uuid.MustParse("30400000-0000-4000-8000-000000000021")}, ExitFillIDs: []uuid.UUID{uuid.MustParse("30400000-0000-4000-8000-000000000022")}, EntryAt: start.Add(26 * time.Hour), ExitAt: start.Add(50 * time.Hour), EntryPrice: "20", ExitPrice: "22", EntryFees: "1", ExitFees: "1", OtherOwnershipCost: "0", GrossPnL: "-4", AfterCostPnL: "-6"},
		},
	}
}

func evaluationPlan(mode strategycatalog.ExperimentMode) (*experimentrun.Plan, error) {
	start := time.Date(2026, 8, 20, 15, 0, 0, 123456000, time.UTC)
	state := json.RawMessage(`{"schema":"capital-state-test-v1"}`)
	stateSHA := hash(state)
	return experimentrun.NewPlan(experimentrun.PlanInput{
		ExperimentID: uuid.MustParse("30400000-0000-4000-8000-000000000101"), ProgramID: uuid.MustParse("30400000-0000-4000-8000-000000000102"), AccountID: uuid.MustParse("30400000-0000-4000-8000-000000000103"),
		CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA, CapitalProjectionCheckpointID: uuid.MustParse("30400000-0000-4000-8000-000000000104"), CapitalStateBytes: state,
		ManifestID: uuid.MustParse("30400000-0000-4000-8000-000000000105"), ManifestSHA256: strings.Repeat("a", 64), EvaluationStart: start, EvaluationEnd: start.Add(72 * time.Hour), Seed: 304, Mode: mode,
		Steps: []experimentrun.StepInput{{PartitionContentSHA256: strings.Repeat("b", 64), ObservationSourceKey: "quote-1", ObservationContentSHA256: strings.Repeat("c", 64), AvailableAt: start.Add(time.Minute), Decision: json.RawMessage(`{"signal":"hold"}`), Action: experimentrun.ActionNoop}},
	})
}

func evaluationResult(plan *experimentrun.Plan) (*experimentrun.Result, error) {
	return experimentrun.NewResult(experimentrun.ResultInput{
		Plan: plan, AccountID: plan.AccountID(), QualityResultID: uuid.MustParse("30400000-0000-4000-8000-000000000106"), SimulationPolicyVersion: "simulation-policy-v1@sha256:" + strings.Repeat("d", 64), CapitalPolicyVersion: "capital-margin-policy-v1@sha256:" + strings.Repeat("e", 64),
		Outcomes: []experimentrun.StepOutcomeInput{{Action: experimentrun.ActionNoop, DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0"}},
	})
}

func requireMetric(t *testing.T, report *Report, section, name string) Metric {
	t.Helper()
	for _, metric := range report.Metrics() {
		if metric.Section == section && metric.Name == name {
			return metric
		}
	}
	t.Fatalf("metric %s.%s not found", section, name)
	return Metric{}
}

func inputReportBytes(t *testing.T, input ReportInput) []byte {
	t.Helper()
	report, err := NewReport(input)
	if err != nil {
		t.Fatal(err)
	}
	return report.CanonicalBytes()
}

func text(value string) *string { return &value }

type evaluationStore struct {
	result *experimentrun.Result
	loaded uuid.UUID
	policy *Policy
	report *Report
}

func (store *evaluationStore) GetResult(_ context.Context, id uuid.UUID) (*experimentrun.Result, error) {
	store.loaded = id
	return store.result, nil
}

func (store *evaluationStore) RegisterPolicy(_ context.Context, value *Policy) (*Policy, error) {
	store.policy = value
	return value, nil
}

func (store *evaluationStore) RecordEvaluation(_ context.Context, value *Report) (*Report, error) {
	store.report = value
	return value, nil
}
