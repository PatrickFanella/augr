package makerquote

import (
	"bytes"
	"slices"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
	predictionqualification "github.com/PatrickFanella/get-rich-quick/internal/predictionreplay/qualification"
)

func fixture(t *testing.T) (*predictionreplay.Recorder, Input) {
	t.Helper()
	recorderInput, err := predictionqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := predictionreplay.NewRecorder(recorderInput)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Recorder: recorder, CandidateKey: "sell-inside", MarketID: "market-1", OutcomeID: predictionqualification.OutcomeNo, Side: predictionreplay.SideSell, DecisionAt: predictionqualification.At(3, 14), QuotePrice: "0.58", QuoteQuantity: "5", PriorQueue: "0", StartingInventory: "0", InventoryLimit: "10", HourlyInventoryCostRate: "0.001", MinimumExpectedNet: "0.01", Scenarios: []ScenarioInput{
		{Key: "no-fill", Weight: "0.25", HorizonAt: predictionqualification.At(3, 15), QueueOutflow: "10"},
		{Key: "full-fill", Weight: "0.75", HorizonAt: predictionqualification.At(3, 15), QueueOutflow: "15"},
	}}
	return recorder, input
}

func TestCandidateQualifiesPositiveExpectedNetAfterFeesAndInventoryCost(t *testing.T) {
	recorder, input := fixture(t)
	candidate, err := NewCandidate(input)
	if err != nil || candidate.State() != "qualified" || candidate.Reason() != "" || candidate.ExpectedNetCapture() != "0.02985" || candidate.ScenarioCount() != 2 {
		t.Fatalf("candidate=%+v err=%v", candidate, err)
	}
	reloaded, err := FromCanonical(candidate.ID(), candidate.Digest(), candidate.CanonicalBytes(), recorder)
	if err != nil || reloaded.Digest() != candidate.Digest() {
		t.Fatalf("reload=%v err=%v", reloaded, err)
	}
	tampered := bytes.Replace(candidate.CanonicalBytes(), []byte(`"expected_net_capture":"0.02985"`), []byte(`"expected_net_capture":"9.99999"`), 1)
	if _, err = FromCanonical(candidate.ID(), candidate.Digest(), tampered, recorder); err == nil {
		t.Fatal("tampered candidate accepted")
	}
}

func TestCandidateRetainsStrictBoundariesAndRiskRejections(t *testing.T) {
	_, input := fixture(t)
	input.MinimumExpectedNet = "0.02985"
	boundary, err := NewCandidate(input)
	if err != nil || boundary.Reason() != "nonpositive_net_capture" {
		t.Fatalf("boundary=%v err=%v", boundary, err)
	}
	_, input = fixture(t)
	for i := range input.Scenarios {
		input.Scenarios[i].QueueOutflow = "10"
	}
	noFill, err := NewCandidate(input)
	if err != nil || noFill.Reason() != "no_fill" {
		t.Fatalf("no-fill=%v err=%v", noFill, err)
	}
	_, input = fixture(t)
	input.InventoryLimit = "4"
	limit, err := NewCandidate(input)
	if err != nil || limit.Reason() != "inventory_limit" {
		t.Fatalf("limit=%v err=%v", limit, err)
	}
	_, input = fixture(t)
	input.HourlyInventoryCostRate = "0.1"
	adverse, err := NewCandidate(input)
	if err != nil || adverse.Reason() != "nonpositive_net_capture" {
		t.Fatalf("adverse=%v err=%v", adverse, err)
	}
}

func TestCandidateBuySideQueueAndInputPermutation(t *testing.T) {
	_, input := fixture(t)
	input.CandidateKey = "buy-inside"
	input.OutcomeID = predictionqualification.OutcomeYes
	input.Side = predictionreplay.SideBuy
	input.QuotePrice = "0.39"
	input.Scenarios = []ScenarioInput{{Key: "partial", Weight: "0.5", HorizonAt: predictionqualification.At(3, 15), QueueOutflow: "10"}, {Key: "full", Weight: "0.5", HorizonAt: predictionqualification.At(3, 15), QueueOutflow: "13"}}
	first, err := NewCandidate(input)
	if err != nil || first.State() != "qualified" {
		t.Fatalf("buy=%v err=%v", first, err)
	}
	slices.Reverse(input.Scenarios)
	second, err := NewCandidate(input)
	if err != nil || second.Digest() != first.Digest() {
		t.Fatalf("permutation=%v err=%v", second, err)
	}
}

func TestCandidateRejectsInvalidQuoteAndScenarios(t *testing.T) {
	_, input := fixture(t)
	input.QuotePrice = "0.59"
	invalidQuote, err := NewCandidate(input)
	if err != nil || invalidQuote.Reason() != "invalid_quote" {
		t.Fatalf("quote=%v err=%v", invalidQuote, err)
	}
	_, input = fixture(t)
	input.Scenarios[0].Weight = "0.5"
	invalidScenarios, err := NewCandidate(input)
	if err != nil || invalidScenarios.Reason() != "invalid_scenarios" {
		t.Fatalf("scenarios=%v err=%v", invalidScenarios, err)
	}
}
