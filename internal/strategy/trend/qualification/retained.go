package qualification

import (
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/strategy/trend"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type RetainedFixture struct {
	Base      *Fixture
	Policy    *trend.Policy
	Scenarios map[string]*trend.Scenario
	Reports   map[string]*trend.Report
}

func BuildRetainedScenarios() (*RetainedFixture, error) {
	base, err := Build(strategycatalog.ExperimentPaperScored)
	if err != nil {
		return nil, err
	}
	policy, err := trend.NewPolicy(trend.PolicyInput{Version: "etf-trend-v1-retained", Horizons: []trend.Horizon{{Days: 21, Weight: "0.75"}, {Days: 63, Weight: "0.25"}}, SignalThreshold: "0", VolatilityWindowDays: 20, AnnualizationDays: 252, TargetVolatility: "0.2", MaximumInstrumentWeight: "0.8", MaximumGrossWeight: "0.8", MaximumRebalanceTurnover: "0.25", MaximumEvidenceAgeSeconds: 60, CostBPS: "10", DecimalScale: 12})
	if err != nil {
		return nil, err
	}
	type scenarioCase struct {
		mode    strategycatalog.ExperimentMode
		anchors [][]string
		prices  []string
		vols    []string
	}
	cases := map[string]scenarioCase{
		"all_long":        {strategycatalog.ExperimentPaperScored, [][]string{{"80", "90"}, {"90", "100"}}, []string{"100", "110"}, []string{"0.5", "0.5"}},
		"mixed_long_cash": {strategycatalog.ExperimentPaperScored, [][]string{{"80", "90"}, {"90", "100"}}, []string{"100", "110"}, []string{"0.5", "0.5"}},
		"all_cash":        {strategycatalog.ExperimentPaperStress, [][]string{{"120", "130"}, {"120", "130"}}, []string{"100", "90"}, []string{"0.2", "0.2"}},
		"volatility_cap":  {strategycatalog.ExperimentPaperStress, [][]string{{"80", "90"}, {"90", "100"}}, []string{"100", "110"}, []string{"0.1", "0.1"}},
		"turnover_multi":  {strategycatalog.ExperimentPaperStress, [][]string{{"80", "90"}, {"120", "130"}, {"80", "90"}}, []string{"100", "100", "110"}, []string{"0.1", "0.1", "0.1"}},
	}
	result := &RetainedFixture{Base: base, Policy: policy, Scenarios: map[string]*trend.Scenario{}, Reports: map[string]*trend.Report{}}
	for name, spec := range cases {
		rebalances := make([]trend.RebalanceInput, len(spec.prices))
		for index := range spec.prices {
			at := Start.Add(time.Duration(index) * (End.Sub(Start) / time.Duration(len(spec.prices)-1)))
			bid := spec.prices[index]
			rebalances[index] = trend.RebalanceInput{OccurredAt: at, Members: []trend.MemberInput{{InstrumentID: base.Base.Instrument.ID, VenueContractID: base.Base.VenueContract.ID, MembershipEffectiveAt: at.Add(-24 * time.Hour), MembershipAvailableAt: at, HorizonPrices: spec.anchors[index], CurrentPrice: bid, RealizedVolatility: spec.vols[index], Bid: bid, Ask: bid + ".1", LotSize: "1", PartitionContentSHA256: strings.Repeat("1", 64), SourceKey: fmt.Sprintf("retained-trend-%s-%d", name, index), EvidenceSHA256: strings.Repeat(fmt.Sprintf("%x", (len(name)+index)%16), 64), AvailableAt: at}}}
		}
		scenario, scenarioErr := trend.NewScenario(trend.ScenarioInput{Policy: policy, InitialCapital: "25000", EvaluationStart: Start, EvaluationEnd: End, Mode: spec.mode, Rebalances: rebalances})
		if scenarioErr != nil {
			return nil, fmt.Errorf("build retained trend scenario %s: %w", name, scenarioErr)
		}
		report, reportErr := trend.NewReport(policy, scenario)
		if reportErr != nil {
			return nil, fmt.Errorf("build retained trend report %s: %w", name, reportErr)
		}
		result.Scenarios[name], result.Reports[name] = scenario, report
	}
	return result, nil
}
