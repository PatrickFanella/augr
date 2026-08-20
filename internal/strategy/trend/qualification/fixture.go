// Package qualification builds explicit deterministic ETF Trend V1 OVR-303
// fixtures. It is test infrastructure, not a runtime adapter registry.
package qualification

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/momentum/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/trend"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

var (
	Start = qualification.Start
	End   = qualification.End
)

type Fixture struct {
	Base     *qualification.Fixture
	Graph    *experimentrun.EvidenceGraph
	Program  *trend.Program
	Policy   *trend.Policy
	Scenario *trend.Scenario
	Family   *strategycatalog.Family
	Version  *strategycatalog.Version
}

func Build(mode strategycatalog.ExperimentMode) (*Fixture, error) {
	base, err := qualification.Build(mode)
	if err != nil {
		return nil, err
	}
	policy, err := trend.NewPolicy(trend.PolicyInput{Version: "etf-trend-v1-qualification", Horizons: []trend.Horizon{{Days: 21, Weight: "0.75"}, {Days: 63, Weight: "0.25"}}, SignalThreshold: "0", VolatilityWindowDays: 20, AnnualizationDays: 252, TargetVolatility: "0.12", MaximumInstrumentWeight: "0.4", MaximumGrossWeight: "0.8", MaximumRebalanceTurnover: "0.25", MaximumEvidenceAgeSeconds: 60, CostBPS: "10", DecimalScale: 12})
	if err != nil {
		return nil, err
	}
	rebalances := make([]trend.RebalanceInput, 2)
	for i, at := range []time.Time{Start, End} {
		observation := base.Graph.Observations[i]
		bid := base.Snapshots[i].Bid.String()
		anchors := [][]string{{"80", "90"}, {"120", "130"}}[i]
		rebalances[i] = trend.RebalanceInput{OccurredAt: at, Members: []trend.MemberInput{{InstrumentID: base.Instrument.ID, VenueContractID: base.VenueContract.ID, MembershipEffectiveAt: at.Add(-24 * time.Hour), MembershipAvailableAt: at, HorizonPrices: anchors, CurrentPrice: bid, RealizedVolatility: "0.2", Bid: bid, Ask: base.Snapshots[i].Ask.String(), LotSize: "1", PartitionContentSHA256: observation.PartitionContentSHA256, SourceKey: observation.ObservationSourceKey, EvidenceSHA256: observation.ObservationContentSHA256, AvailableAt: at}}}
	}
	scenario, err := trend.NewScenario(trend.ScenarioInput{Policy: policy, InitialCapital: base.Graph.Account.StartingCapital.String(), EvaluationStart: Start, EvaluationEnd: End, Mode: mode, Rebalances: rebalances})
	if err != nil {
		return nil, err
	}
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{Slug: "etf-time-series-trend-v1", Name: "ETF time-series trend V1", Thesis: "deterministic multi-horizon volatility-scaled ETF trend qualification", AssetClasses: base.Family.AssetClasses()})
	if err != nil {
		return nil, err
	}
	config, _ := json.Marshal(map[string]string{"policy_id": policy.ID().String(), "policy_sha256": policy.Digest(), "scenario_id": scenario.ID().String(), "scenario_sha256": scenario.Digest()})
	version, err := strategycatalog.NewVersion(strategycatalog.VersionInput{FamilyID: family.ID(), CompilerKind: "go", CompilerVersion: "go1.25.8", SourceCommit: strings.Repeat("e", 40), SourceTreeSHA256: strings.Repeat("f", 64), ConfigSchema: trend.PolicySchemaV1, Config: config, DecisionContract: "etf-time-series-trend-decision-v1", RequiredDatasetKinds: base.Version.RequiredDatasetKinds()})
	if err != nil {
		return nil, err
	}
	experiment, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{VersionID: version.ID(), AccountID: base.Graph.Account.ID, CapitalBindingID: base.Graph.CapitalBinding.ID, ManifestID: base.Graph.Manifest.ID(), QualityResultID: base.Graph.Quality.ID(), SimulationPolicyVersion: base.Graph.SimulationPolicy.Version(), CapitalPolicyVersion: base.Graph.CapitalPolicy.Version(), Mode: mode, EvaluationStart: Start, EvaluationEnd: End, Seed: 404, DatasetQuarantined: false})
	if err != nil {
		return nil, err
	}
	identity, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: version.ID(), VersionSHA256: version.Digest(), CompilerKind: version.CompilerKind(), CompilerVersion: version.CompilerVersion(), SourceCommit: version.SourceCommit(), SourceTreeSHA256: version.SourceTreeSHA256(), DecisionContract: version.DecisionContract(), AdapterKind: trend.AdapterKindV1, AdapterVersion: trend.AdapterVersionV1, AdapterSHA256: trend.AdapterSHA256(policy, scenario), RunnerContract: experimentrun.RunnerContractV1})
	if err != nil {
		return nil, err
	}
	program, err := trend.NewProgram(identity, policy, scenario)
	if err != nil {
		return nil, err
	}
	graph := *base.Graph
	graph.Experiment = experiment
	graph.Version = version
	return &Fixture{base, &graph, program, policy, scenario, family, version}, nil
}

func ID(value string, mode strategycatalog.ExperimentMode) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("trend-v1/%s/%s", mode, value)))
}
