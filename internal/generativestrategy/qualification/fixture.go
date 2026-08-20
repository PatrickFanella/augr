package qualification

import (
	"strings"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/generativestrategy"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type Fixture struct {
	Family *strategycatalog.Family
	Input  generativestrategy.SpecInput
}

func Build() (Fixture, error) {
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{Slug: "generated-momentum", Name: "Generated momentum", Thesis: "A typed momentum qualification hypothesis.", AssetClasses: []instrument.AssetClass{instrument.AssetClassEquity}})
	if err != nil {
		return Fixture{}, err
	}
	input := generativestrategy.SpecInput{
		Family: family, SpecKey: "momentum_v1",
		Inputs: []generativestrategy.InputField{
			{Name: "price", Type: "decimal", DatasetKind: dataset.KindQuotes, Field: "midpoint", FreshnessSeconds: 60, MissingPolicy: "abstain"},
			{Name: "average", Type: "decimal", DatasetKind: dataset.KindBars, Field: "close", FreshnessSeconds: 86400, MissingPolicy: "abstain"},
		},
		Universe: generativestrategy.Universe{AssetClass: instrument.AssetClassEquity, Instruments: []uuid.UUID{uuid.MustParse("10000000-0000-4000-8000-000000000001"), uuid.MustParse("20000000-0000-4000-8000-000000000002")}, Benchmark: uuid.MustParse("30000000-0000-4000-8000-000000000003")},
		Entry:    generativestrategy.Expr{Op: "gt", Args: []generativestrategy.Expr{{Op: "ref", Ref: "price"}, {Op: "ref", Ref: "average"}}},
		Exit:     generativestrategy.Expr{Op: "lt", Args: []generativestrategy.Expr{{Op: "ref", Ref: "price"}, {Op: "ref", Ref: "average"}}},
		Sizing:   generativestrategy.Sizing{Mode: "fixed_fraction", Value: "0.1", MaxPosition: "0.2"}, MaximumHoldingSeconds: 604800,
		Costs: generativestrategy.Costs{SpreadBPS: "5", FeeBPS: "1", SlippageBPS: "2"}, Capacity: generativestrategy.Capacity{MaximumDailyTurnover: "100000", MaximumParticipation: "0.05"},
		ProhibitedBehaviors: []string{"evidence_mutation", "live_order_submission", "lookahead", "network_access", "promotion", "risk_limit_mutation", "secret_access"},
		PropertyTests:       []string{"cost_hurdle_required", "missing_input_abstains", "no_lookahead", "size_bounded", "stale_input_abstains"},
		ExampleTests:        []generativestrategy.ExampleTest{{Key: "entry_true", Values: map[string]string{"price": "101", "average": "100"}, ExpectedEntry: true}},
		Retirement:          generativestrategy.Retirement{MaximumDrawdown: "0.2", MinimumSamples: 100, MaximumFailedChecks: 3},
		Authoring:           generativestrategy.Authoring{Provider: "openai", Model: "gpt-5.6", PromptSHA256: strings.Repeat("a", 64), InputTokens: 1000, OutputTokens: 500, Currency: "USD", Cost: "0.25"},
	}
	return Fixture{family, input}, nil
}
