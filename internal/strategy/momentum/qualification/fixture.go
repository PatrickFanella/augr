// Package qualification builds explicit deterministic Momentum V1 OVR-303
// fixtures. It is test infrastructure, not a runtime adapter registry.
package qualification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/momentum"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

var (
	Start = time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	End   = Start.Add(30 * 24 * time.Hour)
)

type Fixture struct {
	Graph              *experimentrun.EvidenceGraph
	Program            *momentum.Program
	Policy             *momentum.Policy
	Scenario           *momentum.Scenario
	Family             *strategycatalog.Family
	Version            *strategycatalog.Version
	DatasetPolicy      *dataset.PolicyArtifact
	CapitalArtifact    *capital.PolicyArtifact
	SimulationArtifact *simulation.PolicyArtifact
	Instrument         *instrument.Instrument
	VenueContract      *instrument.VenueContract
	Snapshots          []*marketdata.QuoteSnapshot
}

func Build(mode strategycatalog.ExperimentMode) (*Fixture, error) {
	if mode != strategycatalog.ExperimentPaperScored && mode != strategycatalog.ExperimentPaperStress {
		return nil, fmt.Errorf("momentum qualification mode must be scored or stress")
	}
	environment, profile, capitalAmount, multiplier, namespace := domain.AccountEnvironmentPaperScored, domain.MarginProfileRegT, decimal.NewFromInt(25_000), decimal.NewFromInt(2), "paper_scored/momentum-v1"
	if mode == strategycatalog.ExperimentPaperStress {
		environment, profile, capitalAmount, multiplier, namespace = domain.AccountEnvironmentPaperStress, domain.MarginProfileStressUnlimited, decimal.NewFromInt(5_000_000), decimal.Zero, "paper_stress/momentum-v1"
	}
	account, err := domain.NewAccount(domain.AccountInput{Name: "Momentum V1 " + string(mode), Environment: environment, Venue: "test-venue", BaseCurrency: "USD", StorageNamespace: namespace, StartingCapital: capitalAmount, BuyingPowerMultiplier: multiplier, MarginProfile: profile, CreatedBy: "ovr403-qualification", CreationMetadata: json.RawMessage(`{"fixture":"momentum-v1"}`), CreatedAt: Start.Add(-2 * time.Hour)})
	if err != nil {
		return nil, err
	}
	account.ID = fixtureID("account", mode)
	inst, err := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: "figi:MOMENTUM-V1", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive, Metadata: json.RawMessage(`{"fixture":"momentum-v1"}`), CreatedAt: Start.Add(-2 * time.Hour)})
	if err != nil {
		return nil, err
	}
	inst.ID = fixtureID("instrument", mode)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{InstrumentID: inst.ID, Venue: "test-venue", ContractID: "MOMENTUM-V1", Currency: "USD", TickSize: inst.TickSize, LotSize: inst.LotSize, Multiplier: inst.Multiplier, SettlementMethod: inst.SettlementMethod, ValidFrom: Start.Add(-24 * time.Hour), Metadata: json.RawMessage(`{"fixture":"momentum-v1"}`), CreatedAt: Start.Add(-2 * time.Hour)})
	if err != nil {
		return nil, err
	}
	contract.ID = fixtureID("contract", mode)
	snapshots := make([]*marketdata.QuoteSnapshot, 2)
	contents := make([][]byte, 2)
	contentSHAs := make([]string, 2)
	observations := make([]dataset.ObservationInput, 2)
	for index, at := range []time.Time{Start, End} {
		bid := decimal.NewFromInt(100 + int64(index*10))
		ask := bid.Add(decimal.RequireFromString("0.1"))
		depth := decimal.NewFromInt(1000)
		exchange := at.Add(-100 * time.Millisecond)
		sourceKey := fmt.Sprintf("momentum-quote-%d", index)
		snapshot, snapshotErr := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{InstrumentID: inst.ID, VenueContractID: &contract.ID, Provider: "fixture", Venue: contract.Venue, Source: "fixture-feed", ObservationNamespace: "momentum/v1/quotes", ObservationID: sourceKey, SourceRevision: "r1", ExchangeAt: &exchange, ReceivedAt: at, AvailableAt: &at, Bid: &bid, Ask: &ask, BidSize: &depth, AskSize: &depth, MarketStatus: "open", SessionStatus: "regular", Bids: []marketdata.DepthLevelInput{{Price: bid, Size: depth}}, Asks: []marketdata.DepthLevelInput{{Price: ask, Size: depth}}, Metadata: json.RawMessage(`{"fixture":"momentum-v1"}`), CreatedAt: at})
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		snapshot.ID = fixtureID(sourceKey, mode)
		snapshots[index] = snapshot
		contents[index], _ = json.Marshal(snapshot)
		contentSHAs[index] = hash(contents[index])
		observations[index] = dataset.ObservationInput{SourceKey: sourceKey, InstrumentID: inst.ID, EffectiveAt: exchange, ObservedAt: at, AvailableAt: at, Revision: "r1", ContentSHA256: contentSHAs[index], Bid: text(bid.String()), Ask: text(ask.String())}
	}
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: End, Partitions: []dataset.PartitionInput{{Kind: dataset.KindQuotes, Provider: "fixture", Source: "fixture-feed", Namespace: "momentum/v1/quotes", RequestSHA256: strings.Repeat("1", 64), MediaType: "application/json", SymbologyVersion: "figi-v1", AdjustmentPolicy: "total_return", Timezone: "UTC", Calendar: "explicit", Revision: "r1", License: "test-only", RetentionPolicy: "retain-qualification", Observations: observations}}})
	if err != nil {
		return nil, err
	}
	partition := manifest.Partitions()[0]
	datasetPolicy, _ := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	datasetArtifact, _ := datasetPolicy.NewArtifact(Start.Add(-3 * time.Hour))
	quality, err := dataset.Evaluate(dataset.QualityInput{Policy: datasetPolicy, Manifest: manifest, InstrumentWindows: []dataset.InstrumentWindow{{InstrumentID: inst.ID, ValidFrom: Start.Add(-24 * time.Hour), EvidenceSHA256: strings.Repeat("2", 64)}}, Sessions: []dataset.SessionEvidence{{PartitionContentSHA256: partition.ContentSHA256, ExpectedEffectiveAt: []time.Time{Start.Add(-100 * time.Millisecond), End.Add(-100 * time.Millisecond)}, EvidenceSHA256: strings.Repeat("3", 64)}}, ExternalAssessments: []dataset.ExternalAssessment{{PartitionContentSHA256: partition.ContentSHA256, Check: dataset.CheckProviderSpotCompare, Status: dataset.CheckPassed, EvidenceSHA256: strings.Repeat("4", 64)}}})
	if err != nil || quality.Quarantined() {
		return nil, fmt.Errorf("momentum qualification quality invalid: %w", err)
	}
	simulationPolicy, err := simulation.NewPolicy(simulation.PolicyInput{Schema: simulation.PolicySchemaV1, Assets: []simulation.AssetPolicy{{AssetClass: instrument.AssetClassEquity, OrderTypes: []lifecycle.OrderType{lifecycle.OrderLimit}, TimeInForce: []lifecycle.TimeInForce{lifecycle.TimeInForceDay}, QuoteRequirements: marketdata.QuoteRequirements{RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true, RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true, RequireSessionStatus: true, AllowedMarketStatuses: []string{"open"}, AllowedSessionStatuses: []string{"regular"}, MaxAge: time.Minute}, MaxDepthParticipation: decimal.NewFromInt(1), Calendar: simulation.CalendarPolicy{Kind: simulation.CalendarExplicitSessions, Sessions: []simulation.SessionWindow{{Label: "momentum-start", OpenAt: Start.Add(-time.Hour), CloseAt: Start.Add(time.Hour)}, {Label: "momentum-end", OpenAt: End.Add(-time.Hour), CloseAt: End.Add(time.Hour)}}}, Fees: simulation.FeePolicy{PerOrder: decimal.NewFromInt(1), PerUnit: decimal.RequireFromString("0.001"), Scale: 6}}}})
	if err != nil {
		return nil, err
	}
	simulationArtifact, _ := simulationPolicy.NewArtifact(Start.Add(-3 * time.Hour))
	capitalPolicy, _ := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	capitalArtifact, _ := capitalPolicy.NewArtifact(Start.Add(-3 * time.Hour))
	binding, err := capital.NewBinding(*account, capitalPolicy, account.StartingCapital, account.MarginProfile, Start.Add(-2*time.Hour))
	if err != nil {
		return nil, err
	}
	state, err := capitalState(*account, *binding, capitalPolicy, mode)
	if err != nil {
		return nil, err
	}
	policy, err := momentum.NewPolicy(momentum.PolicyInput{Version: "momentum-v1-qualification", LookbackDays: 252, SkipDays: 21, MinimumHistoryDays: 252, MinimumROIC: "0.1", MaximumDebtToAssets: "0.6", RequirePositiveFreeCash: true, MaximumEvidenceAgeSeconds: 60, MaximumVolatility: "0.5", PortfolioSize: 1, MaximumRebalanceTurnover: "0.25", CostBPS: "10", BullBearTrendThreshold: "0.05", MaximumBullVolatility: "0.25", DecimalScale: 12})
	if err != nil {
		return nil, err
	}
	rebalances := make([]momentum.RebalanceInput, 2)
	for i, at := range []time.Time{Start, End} {
		bid := fmt.Sprint(100 + i*10)
		ask := decimal.RequireFromString(bid).Add(decimal.RequireFromString("0.1")).String()
		rebalances[i] = momentum.RebalanceInput{OccurredAt: at, BenchmarkTrend: []string{"0.1", "-0.1"}[i], BenchmarkVolatility: []string{"0.2", "0.4"}[i], BenchmarkEvidenceID: fixtureID(fmt.Sprintf("benchmark-%d", i), mode), BenchmarkEvidenceSHA256: strings.Repeat(string(rune('5'+i)), 64), Members: []momentum.MemberInput{{InstrumentID: inst.ID, VenueContractID: contract.ID, MembershipEffectiveAt: at.Add(-24 * time.Hour), MembershipAvailableAt: at, HistoryDays: 300, LookbackPrice: "80", SkipPrice: bid, Bid: bid, Ask: ask, LotSize: "1", ROIC: "0.2", DebtToAssets: "0.3", FreeCashFlow: "100", Volatility: "0.2", PartitionContentSHA256: partition.ContentSHA256, SourceKey: observations[i].SourceKey, EvidenceSHA256: contentSHAs[i], AvailableAt: at}}}
	}
	scenario, err := momentum.NewScenario(momentum.ScenarioInput{Policy: policy, InitialCapital: account.StartingCapital.String(), EvaluationStart: Start, EvaluationEnd: End, Mode: mode, Rebalances: rebalances})
	if err != nil {
		return nil, err
	}
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{Slug: "momentum-quality-v1", Name: "Momentum quality V1", Thesis: "deterministic point-in-time momentum qualification", AssetClasses: []instrument.AssetClass{instrument.AssetClassEquity}})
	if err != nil {
		return nil, err
	}
	config, _ := json.Marshal(map[string]string{"policy_id": policy.ID().String(), "policy_sha256": policy.Digest(), "scenario_id": scenario.ID().String(), "scenario_sha256": scenario.Digest()})
	version, err := strategycatalog.NewVersion(strategycatalog.VersionInput{FamilyID: family.ID(), CompilerKind: "go", CompilerVersion: "go1.25.8", SourceCommit: strings.Repeat("a", 40), SourceTreeSHA256: strings.Repeat("b", 64), ConfigSchema: momentum.PolicySchemaV1, Config: config, DecisionContract: "momentum-quality-decision-v1", RequiredDatasetKinds: []dataset.Kind{dataset.KindQuotes}})
	if err != nil {
		return nil, err
	}
	experiment, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{VersionID: version.ID(), AccountID: account.ID, CapitalBindingID: binding.ID, ManifestID: manifest.ID(), QualityResultID: quality.ID(), SimulationPolicyVersion: simulationPolicy.Version(), CapitalPolicyVersion: capitalPolicy.Version(), Mode: mode, EvaluationStart: Start, EvaluationEnd: End, Seed: 403, DatasetQuarantined: false})
	if err != nil {
		return nil, err
	}
	identity, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: version.ID(), VersionSHA256: version.Digest(), CompilerKind: version.CompilerKind(), CompilerVersion: version.CompilerVersion(), SourceCommit: version.SourceCommit(), SourceTreeSHA256: version.SourceTreeSHA256(), DecisionContract: version.DecisionContract(), AdapterKind: momentum.AdapterKindV1, AdapterVersion: momentum.AdapterVersionV1, AdapterSHA256: momentum.AdapterSHA256(policy, scenario), RunnerContract: experimentrun.RunnerContractV1})
	if err != nil {
		return nil, err
	}
	program, err := momentum.NewProgram(identity, policy, scenario)
	if err != nil {
		return nil, err
	}
	materials := make([]experimentrun.ObservationMaterial, 2)
	for i := range materials {
		materials[i] = experimentrun.ObservationMaterial{PartitionContentSHA256: partition.ContentSHA256, ObservationSourceKey: observations[i].SourceKey, ObservationContentSHA256: contentSHAs[i], AvailableAt: []time.Time{Start, End}[i], CanonicalContent: contents[i], Snapshot: *snapshots[i]}
	}
	return &Fixture{Graph: &experimentrun.EvidenceGraph{Experiment: experiment, Version: version, Manifest: manifest, Quality: quality, Account: account, CapitalBinding: binding, CapitalPolicy: capitalPolicy, CapitalState: state, SimulationPolicy: simulationPolicy, Instruments: map[uuid.UUID]*instrument.Instrument{inst.ID: inst}, VenueContracts: map[uuid.UUID]*instrument.VenueContract{contract.ID: contract}, Observations: materials}, Program: program, Policy: policy, Scenario: scenario, Family: family, Version: version, DatasetPolicy: datasetArtifact, CapitalArtifact: capitalArtifact, SimulationArtifact: simulationArtifact, Instrument: inst, VenueContract: contract, Snapshots: snapshots}, nil
}

func capitalState(account domain.Account, binding capital.Binding, policy *capital.Policy, mode strategycatalog.ExperimentMode) (*capital.State, error) {
	checkpoint, through := fixtureID("checkpoint", mode), fixtureID("through", mode)
	projection := &ledger.PortfolioProjection{CheckpointID: checkpoint, ProjectionType: ledger.PortfolioProjectionType, Version: ledger.PortfolioProjectionVersion, FIFO: ledger.ProjectionFIFO, AccountID: account.ID, BaseCurrency: account.BaseCurrency, AsOf: Start.Add(-time.Second), ThroughTransactionID: through, TransactionCount: 1, InputChecksum: strings.Repeat("d", 64), Totals: ledger.ProjectionTotals{Cash: account.StartingCapital, NetCapital: account.StartingCapital, Equity: account.StartingCapital}}
	payload := map[string]any{"checkpoint_id": checkpoint.String(), "account_id": account.ID.String(), "base_currency": account.BaseCurrency, "as_of": Start.Add(-time.Second).Format("2006-01-02T15:04:05.000000Z"), "positions": []any{}, "totals": map[string]string{"cash": account.StartingCapital.String(), "market_value": "0", "equity": account.StartingCapital.String()}}
	projection.PayloadBytes, _ = json.Marshal(payload)
	projection.OutputChecksum = hash(projection.PayloadBytes)
	return capital.StateFromProjection(account, binding, policy, projection, nil)
}

func fixtureID(value string, mode strategycatalog.ExperimentMode) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("momentum-v1/"+string(mode)+"/"+value))
}
func hash(value []byte) string  { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func text(value string) *string { return &value }
