package generativestrategy

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func specFixture(t *testing.T) (*strategycatalog.Family, SpecInput) {
	t.Helper()
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{Slug: "generated-momentum", Name: "Generated momentum", Thesis: "A typed momentum hypothesis.", AssetClasses: []instrument.AssetClass{instrument.AssetClassEquity}})
	if err != nil {
		t.Fatal(err)
	}
	first := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	second := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	benchmark := uuid.MustParse("30000000-0000-4000-8000-000000000003")
	input := SpecInput{
		Family: family, SpecKey: "momentum_v1",
		Inputs: []InputField{
			{Name: "price", Type: "decimal", DatasetKind: dataset.KindQuotes, Field: "midpoint", FreshnessSeconds: 60, MissingPolicy: "abstain"},
			{Name: "average", Type: "decimal", DatasetKind: dataset.KindBars, Field: "close", FreshnessSeconds: 86400, MissingPolicy: "abstain"},
			{Name: "eligible", Type: "boolean", DatasetKind: dataset.KindBenchmarkMembership, Field: "member", FreshnessSeconds: 86400, MissingPolicy: "abstain"},
		},
		Universe: Universe{AssetClass: instrument.AssetClassEquity, Instruments: []uuid.UUID{first, second}, Benchmark: benchmark},
		Entry:    Expr{Op: "and", Args: []Expr{{Op: "gt", Args: []Expr{{Op: "ref", Ref: "price"}, {Op: "ref", Ref: "average"}}}, {Op: "ref", Ref: "eligible"}}},
		Exit:     Expr{Op: "or", Args: []Expr{{Op: "lt", Args: []Expr{{Op: "ref", Ref: "price"}, {Op: "ref", Ref: "average"}}}, {Op: "not", Args: []Expr{{Op: "ref", Ref: "eligible"}}}}},
		Sizing:   Sizing{Mode: "fixed_fraction", Value: "0.1", MaxPosition: "0.2"}, MaximumHoldingSeconds: 604800,
		Costs: Costs{SpreadBPS: "5", FeeBPS: "1", SlippageBPS: "2"}, Capacity: Capacity{MaximumDailyTurnover: "100000", MaximumParticipation: "0.05"},
		ProhibitedBehaviors: append([]string(nil), requiredProhibitions...), PropertyTests: append([]string(nil), requiredProperties...),
		ExampleTests: []ExampleTest{{Key: "entry_true", Values: map[string]string{"price": "101", "average": "100", "eligible": "true"}, ExpectedEntry: true, ExpectedExit: false}},
		Retirement:   Retirement{MaximumDrawdown: "0.2", MinimumSamples: 100, MaximumFailedChecks: 3},
		Authoring:    Authoring{Provider: "openai", Model: "gpt-5.6", PromptSHA256: strings.Repeat("a", 64), InputTokens: 1000, OutputTokens: 500, Currency: "USD", Cost: "0.25"},
	}
	return family, input
}

func TestSpecCanonicalPermutationRestoreAndCloneSafety(t *testing.T) {
	family, input := specFixture(t)
	first, err := NewSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(input.Inputs)
	slices.Reverse(input.Universe.Instruments)
	slices.Reverse(input.ProhibitedBehaviors)
	slices.Reverse(input.PropertyTests)
	second, err := NewSpec(input)
	if err != nil || first.Digest() != second.Digest() {
		t.Fatalf("permutation=%v err=%v", second, err)
	}
	reloaded, err := SpecFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes(), family)
	if err != nil || reloaded.Digest() != first.Digest() {
		t.Fatalf("reload=%v err=%v", reloaded, err)
	}
	raw := first.CanonicalBytes()
	raw[0] = 'x'
	if bytes.Equal(raw, first.CanonicalBytes()) {
		t.Fatal("canonical bytes alias internal state")
	}
	kinds := first.RequiredDatasetKinds()
	if len(kinds) != 3 || kinds[0] != dataset.KindBars || kinds[1] != dataset.KindBenchmarkMembership || kinds[2] != dataset.KindQuotes {
		t.Fatalf("kinds=%v", kinds)
	}
}

func TestSpecSemanticEditsChangeIdentity(t *testing.T) {
	_, input := specFixture(t)
	first, err := NewSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Sizing.Value = "0.11"
	second, err := NewSpec(input)
	if err != nil || first.ID() == second.ID() {
		t.Fatalf("edited=%v err=%v", second, err)
	}
}

func TestSpecRejectsInvalidAndNondeterministicLanguage(t *testing.T) {
	tests := map[string]func(*SpecInput){
		"missing prohibition":  func(value *SpecInput) { value.ProhibitedBehaviors = value.ProhibitedBehaviors[1:] },
		"unknown operator":     func(value *SpecInput) { value.Entry.Op = "random" },
		"unbound input":        func(value *SpecInput) { value.Entry.Args[0].Args[0].Ref = "future_price" },
		"type mismatch":        func(value *SpecInput) { value.Entry.Args[0].Args[0].Ref = "eligible" },
		"noncanonical decimal": func(value *SpecInput) { value.Sizing.Value = "1e-1" },
		"missing example":      func(value *SpecInput) { value.ExampleTests = nil },
		"dynamic universe":     func(value *SpecInput) { value.Universe.Instruments = nil },
		"stale can proceed":    func(value *SpecInput) { value.Inputs[0].MissingPolicy = "continue" },
		"missing provenance":   func(value *SpecInput) { value.Authoring.PromptSHA256 = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			_, input := specFixture(t)
			mutate(&input)
			if spec, err := NewSpec(input); err == nil || spec != nil {
				t.Fatalf("accepted=%v err=%v", spec, err)
			}
		})
	}
}

func TestSpecRejectsLiteralDivisionByZeroAndTampering(t *testing.T) {
	family, input := specFixture(t)
	input.Entry = Expr{Op: "gt", Args: []Expr{{Op: "div", Args: []Expr{{Op: "ref", Ref: "price"}, {Op: "decimal", Value: "0"}}}, {Op: "decimal", Value: "1"}}}
	if spec, err := NewSpec(input); err == nil || spec != nil {
		t.Fatalf("division accepted=%v err=%v", spec, err)
	}
	_, input = specFixture(t)
	valid, err := NewSpec(input)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(valid.CanonicalBytes(), []byte(`"maximum_holding_seconds":604800`), []byte(`"maximum_holding_seconds":604801`), 1)
	if _, err = SpecFromCanonical(valid.ID(), valid.Digest(), tampered, family); err == nil {
		t.Fatal("tampered spec accepted")
	}
}
