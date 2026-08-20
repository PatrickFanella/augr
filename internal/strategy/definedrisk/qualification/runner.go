package qualification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/definedrisk"
	wheelqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/wheel/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type RunnerFixture struct {
	*Fixture
	Base      *wheelqualification.Fixture
	Graph     *experimentrun.EvidenceGraph
	Program   *definedrisk.Program
	Family    *strategycatalog.Family
	Version   *strategycatalog.Version
	Snapshots []*marketdata.QuoteSnapshot
}

func BuildRunner(mode strategycatalog.ExperimentMode) (*RunnerFixture, error) {
	base, err := wheelqualification.Build(mode)
	if err != nil {
		return nil, err
	}
	value, err := Build(mode, definedrisk.ExecutionAtomic, definedrisk.BullCall, "10", "105")
	if err != nil {
		return nil, err
	}
	exchangeTimes := []time.Time{DecisionAt.Add(-200 * time.Millisecond), DecisionAt.Add(-100 * time.Millisecond)}
	snapshots := make([]*marketdata.QuoteSnapshot, 2)
	contents := make([]json.RawMessage, 2)
	contentHashes := make([]string, 2)
	for i := range snapshots {
		exchangeAt := exchangeTimes[i]
		bid := decimal.RequireFromString([]string{"1.8", "0.8"}[i])
		ask := decimal.RequireFromString([]string{"2", "1"}[i])
		depth := decimal.NewFromInt(10)
		snapshot, snapshotErr := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{InstrumentID: value.Options[i].ID, VenueContractID: &value.Contracts[i].ID, Provider: "fixture", Venue: "test-venue", Source: "fixture-feed", ObservationNamespace: "defined-risk/v1", ObservationID: value.Contracts[i].ContractID, SourceRevision: "r1", ExchangeAt: &exchangeAt, ReceivedAt: DecisionAt, AvailableAt: &DecisionAt, Bid: &bid, Ask: &ask, BidSize: &depth, AskSize: &depth, MarketStatus: "open", SessionStatus: "regular", Bids: []marketdata.DepthLevelInput{{Price: bid, Size: depth}}, Asks: []marketdata.DepthLevelInput{{Price: ask, Size: depth}}, Metadata: json.RawMessage(`{"fixture":"defined-risk-v1"}`), CreatedAt: DecisionAt})
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		snapshot.ID = id("snapshot-"+string(rune('0'+i)), mode, definedrisk.ExecutionAtomic, definedrisk.BullCall)
		snapshots[i] = snapshot
		contents[i], _ = json.Marshal(snapshot)
		contentHashes[i] = qualificationHash(contents[i])
	}
	terminalHash := strings.Repeat("9", 64)
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: ExpiryAt, Partitions: []dataset.PartitionInput{{Kind: dataset.KindQuotes, Provider: "fixture", Source: "fixture-feed", Namespace: "defined-risk/v1", RequestSHA256: strings.Repeat("1", 64), MediaType: "application/json", SymbologyVersion: "osi-v1", AdjustmentPolicy: "not_applicable", Timezone: "UTC", Calendar: "explicit", Revision: "r1", License: "test-only", RetentionPolicy: "retain-qualification", Observations: []dataset.ObservationInput{{SourceKey: snapshots[0].ObservationID, InstrumentID: value.Options[0].ID, EffectiveAt: exchangeTimes[0], ObservedAt: DecisionAt, AvailableAt: DecisionAt, Revision: "r1", ContentSHA256: contentHashes[0], Bid: text("1.8"), Ask: text("2")}, {SourceKey: snapshots[1].ObservationID, InstrumentID: value.Options[1].ID, EffectiveAt: exchangeTimes[1], ObservedAt: DecisionAt, AvailableAt: DecisionAt, Revision: "r1", ContentSHA256: contentHashes[1], Bid: text("0.8"), Ask: text("1")}, {SourceKey: "defined-risk-terminal", InstrumentID: value.Underlying.ID, EffectiveAt: ExpiryAt, ObservedAt: ExpiryAt, AvailableAt: ExpiryAt, Revision: "r1", ContentSHA256: terminalHash, Bid: text("105"), Ask: text("105")}}}}})
	if err != nil {
		return nil, err
	}
	partition := manifest.Partitions()[0]
	policy, err := definedrisk.NewPolicy(definedrisk.PolicyInput{Version: "defined-risk-v1-runner", ExecutionMode: definedrisk.ExecutionAtomic, MaximumEvidenceAgeSeconds: 60, MaximumContracts: 5, MaximumPositionCapital: "10000", FeePerContractPerLeg: "1", DecimalScale: 12})
	if err != nil {
		return nil, err
	}
	legs := make([]definedrisk.LegInput, 2)
	for i := range legs {
		legs[i] = definedrisk.LegInput{InstrumentID: value.Options[i].ID, VenueContractID: value.Contracts[i].ID, OCCSymbol: value.Contracts[i].ContractID, Underlying: "DEFINED-RISK-V1", OptionType: "call", Strike: []string{"100", "110"}[i], Expiry: ExpiryAt, Multiplier: "100", Style: "european", Position: []string{"long", "short"}[i], Entry: definedrisk.QuoteInput{Bid: []string{"1.8", "0.8"}[i], Ask: []string{"2", "1"}[i], BidSize: "10", AskSize: "10", AvailableAt: DecisionAt, EvidenceID: snapshots[i].ID, EvidenceSHA256: contentHashes[i], PartitionContentSHA256: partition.ContentSHA256, SourceKey: snapshots[i].ObservationID}}
	}
	scenario, err := definedrisk.NewScenario(definedrisk.ScenarioInput{Policy: policy, Strategy: definedrisk.BullCall, InitialCapital: base.Graph.Account.StartingCapital.String(), RequestedContracts: 2, DecisionAt: DecisionAt, ExpiryAt: ExpiryAt, TerminalUnderlying: "105", TerminalAvailableAt: ExpiryAt, TerminalEvidenceID: id("terminal-runner", mode, definedrisk.ExecutionAtomic, definedrisk.BullCall), TerminalEvidenceSHA256: terminalHash, TerminalPartitionContentSHA256: partition.ContentSHA256, TerminalSourceKey: "defined-risk-terminal", Mode: mode, Legs: legs})
	if err != nil {
		return nil, err
	}
	datasetPolicy, _ := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	quality, err := dataset.Evaluate(dataset.QualityInput{Policy: datasetPolicy, Manifest: manifest, InstrumentWindows: []dataset.InstrumentWindow{{InstrumentID: value.Options[0].ID, ValidFrom: DecisionAt.Add(-time.Hour), EvidenceSHA256: strings.Repeat("2", 64)}, {InstrumentID: value.Options[1].ID, ValidFrom: DecisionAt.Add(-time.Hour), EvidenceSHA256: strings.Repeat("3", 64)}, {InstrumentID: value.Underlying.ID, ValidFrom: DecisionAt.Add(-time.Hour), EvidenceSHA256: strings.Repeat("4", 64)}}, Sessions: []dataset.SessionEvidence{{PartitionContentSHA256: partition.ContentSHA256, ExpectedEffectiveAt: []time.Time{exchangeTimes[0], exchangeTimes[1], ExpiryAt}, EvidenceSHA256: strings.Repeat("5", 64)}}, ExternalAssessments: []dataset.ExternalAssessment{{PartitionContentSHA256: partition.ContentSHA256, Check: dataset.CheckProviderSpotCompare, Status: dataset.CheckPassed, EvidenceSHA256: strings.Repeat("6", 64)}}})
	if err != nil || quality.Quarantined() {
		return nil, err
	}
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{Slug: "defined-risk-options-v1", Name: "Defined-risk options V1", Thesis: "deterministic multi-leg and orphan-risk qualification", AssetClasses: base.Family.AssetClasses()})
	if err != nil {
		return nil, err
	}
	config, _ := json.Marshal(map[string]string{"policy_id": policy.ID().String(), "policy_sha256": policy.Digest(), "scenario_id": scenario.ID().String(), "scenario_sha256": scenario.Digest()})
	version, err := strategycatalog.NewVersion(strategycatalog.VersionInput{FamilyID: family.ID(), CompilerKind: "go", CompilerVersion: "go1.25.8", SourceCommit: strings.Repeat("e", 40), SourceTreeSHA256: strings.Repeat("f", 64), ConfigSchema: definedrisk.PolicySchemaV1, Config: config, DecisionContract: "defined-risk-options-decision-v1", RequiredDatasetKinds: []dataset.Kind{dataset.KindQuotes}})
	if err != nil {
		return nil, err
	}
	experiment, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{VersionID: version.ID(), AccountID: base.Graph.Account.ID, CapitalBindingID: base.Graph.CapitalBinding.ID, ManifestID: manifest.ID(), QualityResultID: quality.ID(), SimulationPolicyVersion: base.Graph.SimulationPolicy.Version(), CapitalPolicyVersion: base.Graph.CapitalPolicy.Version(), Mode: mode, EvaluationStart: DecisionAt, EvaluationEnd: ExpiryAt, Seed: 405, DatasetQuarantined: false})
	if err != nil {
		return nil, err
	}
	identity, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: version.ID(), VersionSHA256: version.Digest(), CompilerKind: version.CompilerKind(), CompilerVersion: version.CompilerVersion(), SourceCommit: version.SourceCommit(), SourceTreeSHA256: version.SourceTreeSHA256(), DecisionContract: version.DecisionContract(), AdapterKind: definedrisk.AdapterKindV1, AdapterVersion: definedrisk.AdapterVersionV1, AdapterSHA256: definedrisk.AdapterSHA256(policy, scenario), RunnerContract: experimentrun.RunnerContractV1})
	if err != nil {
		return nil, err
	}
	program, err := definedrisk.NewProgram(identity, policy, scenario)
	if err != nil {
		return nil, err
	}
	materials := make([]experimentrun.ObservationMaterial, 2)
	for i := range materials {
		materials[i] = experimentrun.ObservationMaterial{PartitionContentSHA256: partition.ContentSHA256, ObservationSourceKey: snapshots[i].ObservationID, ObservationContentSHA256: contentHashes[i], AvailableAt: DecisionAt, CanonicalContent: contents[i], Snapshot: *snapshots[i]}
	}
	graph := *base.Graph
	graph.Experiment = experiment
	graph.Version = version
	graph.Manifest = manifest
	graph.Quality = quality
	graph.Instruments = map[uuid.UUID]*instrument.Instrument{value.Options[0].ID: value.Options[0], value.Options[1].ID: value.Options[1]}
	graph.VenueContracts = map[uuid.UUID]*instrument.VenueContract{value.Contracts[0].ID: value.Contracts[0], value.Contracts[1].ID: value.Contracts[1]}
	graph.Observations = materials
	value.Policy, value.Scenario, value.Report = policy, scenario, program.Report()
	return &RunnerFixture{value, base, &graph, program, family, version, snapshots}, nil
}

func qualificationHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
func text(value string) *string { return &value }
