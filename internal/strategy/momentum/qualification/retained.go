package qualification

import (
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/strategy/momentum"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type RetainedFixture struct {
	Base      *Fixture
	Scenarios map[string]*momentum.Scenario
	Reports   map[string]*momentum.Report
}

// BuildRetainedScenarios covers every declared regime, transitions between
// them, repeated capped rebalances, and both scored and stress identities.
func BuildRetainedScenarios() (*RetainedFixture, error) {
	base, err := Build(strategycatalog.ExperimentPaperScored)
	if err != nil {
		return nil, err
	}
	type scenarioCase struct {
		mode   strategycatalog.ExperimentMode
		trends []string
		vols   []string
		prices []string
	}
	cases := map[string]scenarioCase{
		"bull_cap_hit":       {strategycatalog.ExperimentPaperScored, []string{"0.1", "0.08"}, []string{"0.2", "0.2"}, []string{"100", "120"}},
		"sideways_drift":     {strategycatalog.ExperimentPaperScored, []string{"0", "0.01"}, []string{"0.3", "0.3"}, []string{"100", "102"}},
		"bear_rebalance":     {strategycatalog.ExperimentPaperStress, []string{"-0.1", "-0.08"}, []string{"0.4", "0.4"}, []string{"100", "80"}},
		"regime_transitions": {strategycatalog.ExperimentPaperStress, []string{"0.1", "0", "-0.1", "0.1"}, []string{"0.2", "0.3", "0.4", "0.2"}, []string{"100", "110", "90", "115"}},
	}
	result := &RetainedFixture{Base: base, Scenarios: map[string]*momentum.Scenario{}, Reports: map[string]*momentum.Report{}}
	for name, spec := range cases {
		rebalances := make([]momentum.RebalanceInput, len(spec.trends))
		for index := range spec.trends {
			at := Start.Add(time.Duration(index) * (End.Sub(Start) / time.Duration(len(spec.trends)-1)))
			bid := spec.prices[index]
			ask := fmt.Sprintf("%s.1", bid)
			rebalances[index] = momentum.RebalanceInput{
				OccurredAt: at, BenchmarkTrend: spec.trends[index], BenchmarkVolatility: spec.vols[index],
				BenchmarkEvidenceID:     fixtureID(fmt.Sprintf("retained/%s/benchmark/%d", name, index), spec.mode),
				BenchmarkEvidenceSHA256: strings.Repeat(fmt.Sprintf("%x", (index+len(name))%16), 64),
				Members:                 []momentum.MemberInput{{InstrumentID: base.Instrument.ID, VenueContractID: base.VenueContract.ID, MembershipEffectiveAt: at.Add(-24 * time.Hour), MembershipAvailableAt: at, HistoryDays: 300, LookbackPrice: "80", SkipPrice: bid, Bid: bid, Ask: ask, LotSize: "1", ROIC: "0.2", DebtToAssets: "0.3", FreeCashFlow: "100", Volatility: "0.2", PartitionContentSHA256: strings.Repeat("1", 64), SourceKey: fmt.Sprintf("retained-%s-%d", name, index), EvidenceSHA256: strings.Repeat(fmt.Sprintf("%x", (index+len(name)+1)%16), 64), AvailableAt: at}},
			}
		}
		scenario, scenarioErr := momentum.NewScenario(momentum.ScenarioInput{Policy: base.Policy, InitialCapital: "25000", EvaluationStart: Start, EvaluationEnd: End, Mode: spec.mode, Rebalances: rebalances})
		if scenarioErr != nil {
			return nil, fmt.Errorf("build retained scenario %s: %w", name, scenarioErr)
		}
		report, reportErr := momentum.NewReport(base.Policy, scenario)
		if reportErr != nil {
			return nil, fmt.Errorf("build retained report %s: %w", name, reportErr)
		}
		result.Scenarios[name], result.Reports[name] = scenario, report
	}
	return result, nil
}
