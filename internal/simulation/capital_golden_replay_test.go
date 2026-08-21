package simulation_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

type capitalGoldenReplayEvidence struct {
	assessment *capital.Assessment
	quote      []byte
	outcome    *simulation.Outcome
}

func TestCapitalGoldenReplayAssessesBeforeRouteAcrossEveryTier(t *testing.T) {
	policy, err := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	tiers := policy.Tiers()
	results := make([]capitalGoldenReplayEvidence, 0, len(tiers)+1)
	for index, tier := range tiers {
		options := goldenFixtureOptions{
			environment: domain.AccountEnvironmentPaperScored, startingCapital: tier,
			marginProfile: domain.MarginProfileRegT, buyingPower: decimal.NewFromInt(2),
			storageNamespace: fmt.Sprintf("paper_scored/capital-golden-%d", index),
		}
		results = append(results, runCapitalAdmittedGoldenReplay(t, policy, options))
	}
	results = append(results, runCapitalAdmittedGoldenReplay(t, policy, goldenFixtureOptions{
		environment: domain.AccountEnvironmentPaperStress, startingCapital: decimal.NewFromInt(5_000_000),
		marginProfile:    domain.MarginProfileStressUnlimited,
		storageNamespace: "paper_stress/capital-golden-unlimited",
	}))

	baseline := results[0]
	for index, result := range results[1:] {
		if !bytes.Equal(result.quote, baseline.quote) {
			t.Fatalf("replay %d changed the canonical route quote", index+1)
		}
		if result.assessment.Hash() == baseline.assessment.Hash() ||
			bytes.Equal(result.assessment.CanonicalBytes(), baseline.assessment.CanonicalBytes()) {
			t.Fatalf("replay %d reused tier/profile capital evidence", index+1)
		}
		if !result.outcome.TotalQuantity.Equal(baseline.outcome.TotalQuantity) ||
			!result.outcome.GrossCash.Equal(baseline.outcome.GrossCash) ||
			!result.outcome.TotalFee.Equal(baseline.outcome.TotalFee) || len(result.outcome.Fills) != len(baseline.outcome.Fills) {
			t.Fatalf("replay %d changed admitted downstream economics", index+1)
		}
		for fillIndex := range result.outcome.Fills {
			left, right := result.outcome.Fills[fillIndex], baseline.outcome.Fills[fillIndex]
			if !left.Quantity.Equal(right.Quantity) || !left.Price.Equal(right.Price) || !left.Fee.Equal(right.Fee) {
				t.Fatalf("replay %d fill %d changed = %+v/%+v", index+1, fillIndex, left, right)
			}
		}
	}
	if results[len(results)-1].assessment.PromotionEligible() {
		t.Fatal("stress replay became promotion eligible")
	}
}

func TestCapitalGoldenReplayRejectionCreatesNoRouteOrEconomics(t *testing.T) {
	policy, err := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	fixture := newGoldenApprovedFixture(t, goldenFixtureOptions{
		environment: domain.AccountEnvironmentPaperScored, startingCapital: decimal.NewFromInt(500),
		marginProfile: domain.MarginProfileCash, buyingPower: decimal.NewFromInt(1),
		storageNamespace: "paper_scored/capital-golden-rejected", desiredQuantity: decimal.NewFromInt(100),
	})
	binding, state := capitalGoldenContext(t, policy, fixture)
	assessment, err := capital.Assess(capital.AssessmentInput{
		Account: fixture.account, Binding: binding, Policy: policy, State: state,
		Instrument: fixture.instrument, Currency: "USD", ScenarioID: "capital-golden-rejected",
		Direction: capital.ExposureIncreaseLong, ProposedNotional: decimal.NewFromInt(1_020),
	})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Decision != capital.DecisionRejected || assessment.Reason != capital.ReasonInsufficientSettledCash {
		t.Fatalf("rejected assessment = %+v", assessment)
	}
	if fixture.approved.Order != nil || fixture.approved.Binding != nil || len(fixture.approved.Fills) != 0 ||
		fixture.routed != nil {
		t.Fatalf("rejection leaked routed/economic facts = %+v", fixture)
	}
}

func runCapitalAdmittedGoldenReplay(
	t *testing.T,
	policy *capital.Policy,
	options goldenFixtureOptions,
) capitalGoldenReplayEvidence {
	t.Helper()
	fixture := newGoldenApprovedFixture(t, options)
	binding, state := capitalGoldenContext(t, policy, fixture)
	assessment, err := capital.Assess(capital.AssessmentInput{
		Account: fixture.account, Binding: binding, Policy: policy, State: state,
		Instrument: fixture.instrument, Currency: "USD", ScenarioID: "capital-golden-admitted",
		Direction: capital.ExposureIncreaseLong, ProposedNotional: decimal.NewFromInt(102),
	})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Decision != capital.DecisionAdmitted || fixture.approved.Order != nil {
		t.Fatalf("pre-route assessment = %+v, order = %+v", assessment, fixture.approved.Order)
	}
	quote, err := json.Marshal(fixture.routeQuote)
	if err != nil {
		t.Fatal(err)
	}
	routeGoldenFixture(t, &fixture)
	backtestOutcome, paperOutcome := runGoldenReplay(t, fixture)
	if backtestOutcome.Hash() != paperOutcome.Hash() ||
		!bytes.Equal(backtestOutcome.CanonicalBytes(), paperOutcome.CanonicalBytes()) {
		t.Fatalf("capital golden backtest/paper mismatch = %s/%s", backtestOutcome.Hash(), paperOutcome.Hash())
	}
	return capitalGoldenReplayEvidence{assessment: assessment, quote: quote, outcome: backtestOutcome}
}

func capitalGoldenContext(t *testing.T, policy *capital.Policy, fixture goldenFixture) (capital.Binding, *capital.State) {
	t.Helper()
	binding, err := capital.NewBinding(
		fixture.account, policy, fixture.account.StartingCapital, fixture.account.MarginProfile,
		fixture.routeAt.Add(-time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	projection := &ledger.PortfolioProjection{
		CheckpointID: uuid.New(), ProjectionType: ledger.PortfolioProjectionType, Version: ledger.PortfolioProjectionVersion,
		FIFO: ledger.ProjectionFIFO, AccountID: fixture.account.ID, BaseCurrency: fixture.account.BaseCurrency,
		AsOf: fixture.routeAt.Add(-time.Second), ThroughTransactionID: uuid.New(), TransactionCount: 1,
		InputChecksum: strings.Repeat("a", 64),
		Totals: ledger.ProjectionTotals{
			Cash: fixture.account.StartingCapital, NetCapital: fixture.account.StartingCapital,
			Equity: fixture.account.StartingCapital,
		},
	}
	projection.PayloadBytes = capitalGoldenProjectionPayload(t, projection)
	digest := sha256.Sum256(projection.PayloadBytes)
	projection.OutputChecksum = hex.EncodeToString(digest[:])
	state, err := capital.StateFromProjection(fixture.account, *binding, policy, projection, nil)
	if err != nil {
		t.Fatal(err)
	}
	return *binding, state
}

func capitalGoldenProjectionPayload(t *testing.T, projection *ledger.PortfolioProjection) []byte {
	t.Helper()
	value := struct {
		CheckpointID string `json:"checkpoint_id"`
		AccountID    string `json:"account_id"`
		BaseCurrency string `json:"base_currency"`
		AsOf         string `json:"as_of"`
		Positions    []struct {
			InstrumentID string `json:"instrument_id"`
			Open         bool   `json:"open"`
			Quantity     string `json:"quantity"`
			MarketValue  string `json:"market_value"`
		} `json:"positions"`
		Totals struct {
			Cash        string `json:"cash"`
			MarketValue string `json:"market_value"`
			Equity      string `json:"equity"`
		} `json:"totals"`
	}{
		CheckpointID: projection.CheckpointID.String(), AccountID: projection.AccountID.String(),
		BaseCurrency: projection.BaseCurrency, AsOf: projection.AsOf.Format("2006-01-02T15:04:05.000000Z"),
	}
	value.Positions = make([]struct {
		InstrumentID string `json:"instrument_id"`
		Open         bool   `json:"open"`
		Quantity     string `json:"quantity"`
		MarketValue  string `json:"market_value"`
	}, 0)
	value.Totals.Cash = projection.Totals.Cash.String()
	value.Totals.MarketValue = projection.Totals.MarketValue.String()
	value.Totals.Equity = projection.Totals.Equity.String()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
