package capital

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

func TestRunMatrixEvaluatesSixOrderedScoredTiersPlusSeparateStress(t *testing.T) {
	policy := bindingTestPolicy(t)
	spec := matrixTestSpec()
	result, err := RunMatrix(spec, policy, domain.MarginProfileRegT, func(context ScenarioContext) (Scenario, error) {
		return matrixScenario(t, context, context.Tier.Mul(decimal.RequireFromString("0.5"))), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Outcomes) != 7 || len(result.ScoredOutcomes()) != 6 || result.StressOutcome() == nil {
		t.Fatalf("matrix result = %+v", result)
	}
	for index, tier := range policy.Tiers() {
		outcome := result.Outcomes[index]
		if outcome.Ordinal != index || outcome.Environment != domain.AccountEnvironmentPaperScored ||
			!outcome.Tier.Equal(tier) || outcome.Profile != domain.MarginProfileRegT ||
			outcome.Assessment.Decision != DecisionAdmitted || !outcome.Assessment.PromotionEligible() {
			t.Fatalf("scored outcome %d = %+v", index, outcome)
		}
	}
	stress := result.Outcomes[6]
	if stress.Ordinal != 6 || stress.Environment != domain.AccountEnvironmentPaperStress ||
		stress.Profile != domain.MarginProfileStressUnlimited || stress.Assessment.Reason != ReasonStressUnbounded ||
		stress.Assessment.PromotionEligible() || result.StressOutcome() != stress {
		t.Fatalf("stress outcome = %+v", stress)
	}
	if result.Hash() == "" || len(result.CanonicalBytes()) == 0 {
		t.Fatal("matrix result lacks canonical evidence")
	}
}

func TestRunMatrixRetainsTierCapacityRejectionsAsResults(t *testing.T) {
	policy := bindingTestPolicy(t)
	result, err := RunMatrix(matrixTestSpec(), policy, domain.MarginProfileRegT, func(context ScenarioContext) (Scenario, error) {
		return matrixScenario(t, context, decimal.NewFromInt(100_000)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Decision{
		DecisionRejected, DecisionRejected, DecisionRejected,
		DecisionAdmitted, DecisionAdmitted, DecisionAdmitted,
		DecisionAdmitted,
	}
	for index, decision := range want {
		if result.Outcomes[index].Assessment.Decision != decision {
			t.Fatalf("outcome %d decision = %q, want %q", index, result.Outcomes[index].Assessment.Decision, decision)
		}
	}
	if result.Outcomes[0].Assessment.Reason != ReasonInsufficientBuyingPower ||
		result.Outcomes[6].Assessment.Reason != ReasonStressUnbounded {
		t.Fatalf("capacity reasons = %q/%q", result.Outcomes[0].Assessment.Reason, result.Outcomes[6].Assessment.Reason)
	}
}

func TestRunMatrixIsDeterministicAndDoesNotMixStressIntoScoredHash(t *testing.T) {
	policy := bindingTestPolicy(t)
	factory := func(context ScenarioContext) (Scenario, error) {
		return matrixScenario(t, context, context.Tier), nil
	}
	first, err := RunMatrix(matrixTestSpec(), policy, domain.MarginProfileCash, factory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunMatrix(matrixTestSpec(), policy, domain.MarginProfileCash, factory)
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash() != second.Hash() || string(first.CanonicalBytes()) != string(second.CanonicalBytes()) {
		t.Fatal("identical matrix replay changed canonical identity")
	}
	if first.ScoredHash() == "" || first.ScoredHash() == first.Hash() || first.ScoredHash() != second.ScoredHash() {
		t.Fatalf("matrix hashes = %q/%q", first.Hash(), first.ScoredHash())
	}
	bytes := first.CanonicalBytes()
	bytes[0] = '['
	if string(bytes) == string(first.CanonicalBytes()) {
		t.Fatal("CanonicalBytes exposed mutable matrix storage")
	}
}

func TestRunMatrixStopsOnFactoryErrorWithoutPartialResult(t *testing.T) {
	policy := bindingTestPolicy(t)
	wantErr := errors.New("scenario failed")
	calls := 0
	result, err := RunMatrix(matrixTestSpec(), policy, domain.MarginProfileRegT, func(context ScenarioContext) (Scenario, error) {
		calls++
		if calls == 3 {
			return Scenario{}, wantErr
		}
		return matrixScenario(t, context, decimal.NewFromInt(1)), nil
	})
	if !errors.Is(err, wantErr) || result != nil || calls != 3 {
		t.Fatalf("RunMatrix = %+v/%v, calls %d", result, err, calls)
	}
}

func TestRunMatrixRejectsMutableSharedScenarioFacts(t *testing.T) {
	policy := bindingTestPolicy(t)
	tests := map[string]func(*Scenario){
		"as of":             func(value *Scenario) { value.Projection.AsOf = value.Projection.AsOf.Add(time.Microsecond) },
		"market digest":     func(value *Scenario) { value.MarketInputDigest = "changed" },
		"simulation policy": func(value *Scenario) { value.SimulationPolicyVersion = "changed" },
		"seed":              func(value *Scenario) { value.Seed++ },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := RunMatrix(matrixTestSpec(), policy, domain.MarginProfileRegT, func(context ScenarioContext) (Scenario, error) {
				scenario := matrixScenario(t, context, decimal.NewFromInt(1))
				mutate(&scenario)
				return scenario, nil
			})
			if err == nil || result != nil {
				t.Fatalf("RunMatrix = %+v/%v", result, err)
			}
		})
	}
}

func matrixTestSpec() MatrixSpec {
	return MatrixSpec{
		ScenarioID: "matrix-test", MarketInputDigest: strings64("b"),
		SimulationPolicyVersion: "simulation-policy-v1@sha256:" + strings64("c"),
		Seed:                    42, AsOf: bindingTestTime(),
	}
}

func matrixScenario(t *testing.T, context ScenarioContext, notional decimal.Decimal) Scenario {
	t.Helper()
	projection := matrixProjection(t, context, context.Tier)
	proposed := capitalTestInstrument(t, instrument.AssetClassEquity)
	proposed.ID = economicid.DeterministicUUID("matrix-proposed-instrument", context.Spec.ScenarioID)
	return Scenario{
		Projection: projection, Instruments: nil,
		ProposedInstrument: *proposed,
		Direction:          ExposureIncreaseLong, ProposedNotional: notional,
		MarketInputDigest:       context.Spec.MarketInputDigest,
		SimulationPolicyVersion: context.Spec.SimulationPolicyVersion,
		Seed:                    context.Spec.Seed,
	}
}

func matrixProjection(t *testing.T, context ScenarioContext, cash decimal.Decimal) *ledger.PortfolioProjection {
	t.Helper()
	projection := &ledger.PortfolioProjection{
		CheckpointID:   economicid.DeterministicUUID("matrix-projection", context.Account.ID.String(), context.Spec.AsOf.String()),
		ProjectionType: ledger.PortfolioProjectionType, Version: ledger.PortfolioProjectionVersion,
		FIFO: ledger.ProjectionFIFO, AccountID: context.Account.ID, BaseCurrency: context.Account.BaseCurrency,
		AsOf: context.Spec.AsOf, ThroughTransactionID: economicid.DeterministicUUID("matrix-through", context.Account.ID.String()),
		TransactionCount: 1, InputChecksum: strings64("d"),
		Totals: ledger.ProjectionTotals{Cash: cash, NetCapital: cash, Equity: cash},
	}
	projection.PayloadBytes = capitalProjectionPayload(t, projection)
	digest := sha256.Sum256(projection.PayloadBytes)
	projection.OutputChecksum = hex.EncodeToString(digest[:])
	return projection
}
