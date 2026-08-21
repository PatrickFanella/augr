package copydrift

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func testSubscription() domain.CopySubscription {
	id := uuid.New()
	value := domain.DefaultCopySubscription()
	value.ID, value.OriginType, value.OriginID = id, "copy_subscription", id
	value.IsPaper, value.CapitalBudget, value.MaxTurnoverPct = true, 10000, 0.25
	return value
}

func TestRunConvergesAcrossSessionsWithoutNewObservation(t *testing.T) {
	subscription, observation := testSubscription(), uuid.New()
	targets := []Value{{"MSFT", decimal.NewFromInt(3000)}, {"AAPL", decimal.NewFromInt(6000)}}
	currents := []Value{}
	residuals := []string{"6500.00", "4000.00", "1500.00", "0.00"}
	for i, session := range []string{"2026-08-20/regular", "2026-08-21/regular", "2026-08-24/regular", "2026-08-25/regular"} {
		run, err := NewRun(Input{subscription, observation, session, decimal.NewFromInt(2500), targets, currents})
		if err != nil {
			t.Fatal(err)
		}
		if run.SourceObservationID() != observation || run.ResidualDrift() != residuals[i] || run.PreparedTurnover() != []string{"2500.00", "2500.00", "2500.00", "1500.00"}[i] {
			t.Fatalf("session %d = turnover %s residual %s", i, run.PreparedTurnover(), run.ResidualDrift())
		}
		var envelope struct {
			Legs []struct {
				InstrumentKey  string `json:"instrument_key"`
				ProjectedValue string `json:"projected_value"`
			} `json:"legs"`
		}
		if json.Unmarshal(run.CanonicalBytes(), &envelope) != nil {
			t.Fatal("decode")
		}
		currentMap := map[string]decimal.Decimal{}
		for _, value := range currents {
			currentMap[value.InstrumentKey] = value.Amount
		}
		for _, leg := range envelope.Legs {
			currentMap[leg.InstrumentKey] = decimal.RequireFromString(leg.ProjectedValue)
		}
		currents = currents[:0]
		for key, value := range currentMap {
			currents = append(currents, Value{key, value})
		}
		reloaded, err := FromCanonical(run.ID(), run.Digest(), run.CanonicalBytes())
		if err != nil || reloaded.Digest() != run.Digest() {
			t.Fatalf("reload = %v/%v", reloaded, err)
		}
	}
}

func TestRunHandlesMixedBuySellAndPermutation(t *testing.T) {
	subscription, observation := testSubscription(), uuid.New()
	left, err := NewRun(Input{subscription, observation, "2026-08-20/regular", decimal.NewFromInt(2500), []Value{{"MSFT", decimal.NewFromInt(3000)}, {"AAPL", decimal.NewFromInt(6000)}}, []Value{{"MSFT", decimal.NewFromInt(5000)}, {"AAPL", decimal.NewFromInt(5000)}}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewRun(Input{subscription, observation, "2026-08-20/regular", decimal.NewFromInt(2500), []Value{{"AAPL", decimal.NewFromInt(6000)}, {"MSFT", decimal.NewFromInt(3000)}}, []Value{{"AAPL", decimal.NewFromInt(5000)}, {"MSFT", decimal.NewFromInt(5000)}}})
	if err != nil || left.Digest() != right.Digest() || left.PreparedTurnover() != "2500.00" || left.ResidualDrift() != "500.00" {
		t.Fatalf("permutation = %s/%s err=%v", left.Digest(), right.Digest(), err)
	}
}

func TestRunRejectsInvalidInputs(t *testing.T) {
	subscription, observation := testSubscription(), uuid.New()
	cases := []Input{
		{subscription, observation, "bad", decimal.NewFromInt(1), []Value{{"A", decimal.NewFromInt(1)}}, nil},
		{subscription, observation, "2026-08-20/regular", decimal.RequireFromString("0.001"), []Value{{"A", decimal.NewFromInt(1)}}, nil},
		{subscription, observation, "2026-08-20/regular", decimal.NewFromInt(2501), []Value{{"A", decimal.NewFromInt(1)}}, nil},
		{subscription, observation, "2026-08-20/regular", decimal.NewFromInt(1), []Value{{"A", decimal.NewFromInt(1)}, {"a", decimal.NewFromInt(1)}}, nil},
		{subscription, observation, "2026-08-20/regular", decimal.Zero, []Value{{"A", decimal.NewFromInt(1)}}, nil},
	}
	for i, input := range cases {
		if _, err := NewRun(input); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestRunAllowsExplicitZeroDrift(t *testing.T) {
	subscription, observation := testSubscription(), uuid.New()
	run, err := NewRun(Input{subscription, observation, "2026-08-20/regular", decimal.Zero, []Value{{"AAPL", decimal.NewFromInt(10)}}, []Value{{"AAPL", decimal.NewFromInt(10)}}})
	if err != nil || !run.Converged() || run.PreparedTurnover() != "0.00" || run.ResidualDrift() != "0.00" {
		t.Fatalf("run=%v err=%v", run, err)
	}
}
