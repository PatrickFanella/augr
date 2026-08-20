// Package qualification builds an explicit deterministic Wheel V1 fixture for
// OVR-303 runner and PostgreSQL qualification. It is not a runtime registry.
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
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/wheel"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

var (
	Start   = time.Date(2026, 8, 20, 15, 0, 0, 123456000, time.UTC)
	RouteAt = Start.Add(time.Minute)
	End     = RouteAt.Add(24 * time.Hour)
)

type Fixture struct {
	Graph              *experimentrun.EvidenceGraph
	Program            *wheel.Program
	Policy             *wheel.Policy
	Scenario           *wheel.Scenario
	Family             *strategycatalog.Family
	Version            *strategycatalog.Version
	DatasetPolicy      *dataset.PolicyArtifact
	CapitalArtifact    *capital.PolicyArtifact
	SimulationArtifact *simulation.PolicyArtifact
	Underlying         *instrument.Instrument
	Option             *instrument.Instrument
	VenueContract      *instrument.VenueContract
	QuoteSnapshot      *marketdata.QuoteSnapshot
}

func Build(mode strategycatalog.ExperimentMode) (*Fixture, error) {
	return build(mode, nil, "95")
}

// BuildCapitalRejected retains valid wheel collateral but supplies too little
// scored-account equity for the reviewed Reg T short requirement.
func BuildCapitalRejected() (*Fixture, error) {
	value := decimal.NewFromInt(5_000)
	return build(strategycatalog.ExperimentPaperScored, &value, "45")
}

func build(mode strategycatalog.ExperimentMode, capitalOverride *decimal.Decimal, strike string) (*Fixture, error) {
	if mode != strategycatalog.ExperimentPaperScored && mode != strategycatalog.ExperimentPaperStress {
		return nil, fmt.Errorf("wheel qualification mode must be scored or stress")
	}
	environment := domain.AccountEnvironmentPaperScored
	marginProfile := domain.MarginProfileRegT
	startingCapital := decimal.NewFromInt(25_000)
	buyingPowerMultiplier := decimal.NewFromInt(2)
	namespace := "paper_scored/wheel-v1-qualification"
	if mode == strategycatalog.ExperimentPaperStress {
		environment = domain.AccountEnvironmentPaperStress
		marginProfile = domain.MarginProfileStressUnlimited
		startingCapital = decimal.NewFromInt(5_000_000)
		buyingPowerMultiplier = decimal.Zero
		namespace = "paper_stress/wheel-v1-qualification"
	}
	if capitalOverride != nil {
		startingCapital = *capitalOverride
	}
	account, err := domain.NewAccount(domain.AccountInput{Name: "Wheel V1 " + string(mode), Environment: environment, Venue: "test-venue", BaseCurrency: "USD", StorageNamespace: namespace, StartingCapital: startingCapital, BuyingPowerMultiplier: buyingPowerMultiplier, MarginProfile: marginProfile, CreatedBy: "ovr402-qualification", CreationMetadata: json.RawMessage(`{"fixture":"wheel-v1"}`), CreatedAt: Start.Add(-2 * time.Hour)})
	if err != nil {
		return nil, err
	}
	account.ID = id(1, mode)
	underlying, err := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: "figi:WHEEL-V1", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive, Metadata: json.RawMessage(`{"fixture":"wheel-v1"}`), CreatedAt: Start.Add(-2 * time.Hour)})
	if err != nil {
		return nil, err
	}
	underlying.ID = id(2, mode)
	optionMetadata, _ := json.Marshal(map[string]string{"contract_type": "put", "strike": strike})
	option, err := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: "osi:WHEEL-V1-PUT-" + strike, AssetClass: instrument.AssetClassOption, PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(100), Expiration: &End, ExerciseStyle: instrument.ExerciseAmerican, SettlementMethod: instrument.SettlementPhysical, UnderlyingID: &underlying.ID, Status: instrument.StatusActive, Metadata: optionMetadata, CreatedAt: Start.Add(-2 * time.Hour)})
	if err != nil {
		return nil, err
	}
	option.ID = id(3, mode)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{InstrumentID: option.ID, Venue: "test-venue", ContractID: "WHEEL-V1-PUT-" + strike, Currency: "USD", TickSize: option.TickSize, LotSize: option.LotSize, Multiplier: option.Multiplier, SettlementMethod: option.SettlementMethod, ValidFrom: Start.Add(-24 * time.Hour), ValidTo: &End, Metadata: json.RawMessage(`{"fixture":"wheel-v1"}`), CreatedAt: Start.Add(-2 * time.Hour)})
	if err != nil {
		return nil, err
	}
	contract.ID = id(4, mode)
	exchangeAt := RouteAt.Add(-100 * time.Millisecond)
	bid, ask, depth := decimal.NewFromInt(2), decimal.RequireFromString("2.1"), decimal.NewFromInt(10)
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{InstrumentID: option.ID, VenueContractID: &contract.ID, Provider: "fixture", Venue: contract.Venue, Source: "fixture-feed", ObservationNamespace: "wheel/v1/put", ObservationID: "wheel-put-95", SourceRevision: "r1", ExchangeAt: &exchangeAt, ReceivedAt: RouteAt, AvailableAt: &RouteAt, Bid: &bid, Ask: &ask, BidSize: &depth, AskSize: &depth, MarketStatus: "open", SessionStatus: "regular", Bids: []marketdata.DepthLevelInput{{Price: bid, Size: depth}}, Asks: []marketdata.DepthLevelInput{{Price: ask, Size: depth}}, Metadata: json.RawMessage(`{"fixture":"wheel-v1"}`), CreatedAt: RouteAt})
	if err != nil {
		return nil, err
	}
	snapshot.ID = id(5, mode)
	content, _ := json.Marshal(snapshot)
	contentSHA := hash(content)
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: End, Partitions: []dataset.PartitionInput{{Kind: dataset.KindQuotes, Provider: "fixture", Source: "fixture-feed", Namespace: "wheel/v1/put", RequestSHA256: strings.Repeat("1", 64), MediaType: "application/json", SymbologyVersion: "osi-v1", AdjustmentPolicy: "not_applicable", Timezone: "UTC", Calendar: "24x7", Revision: "r1", License: "test-only", RetentionPolicy: "retain-qualification", Observations: []dataset.ObservationInput{{SourceKey: snapshot.ObservationID, InstrumentID: option.ID, EffectiveAt: exchangeAt, ObservedAt: RouteAt, AvailableAt: RouteAt, Revision: "r1", ContentSHA256: contentSHA, Bid: text(bid.String()), Ask: text(ask.String())}}}}})
	if err != nil {
		return nil, err
	}
	datasetPolicy, _ := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	datasetArtifact, _ := datasetPolicy.NewArtifact(Start.Add(-3 * time.Hour))
	partition := manifest.Partitions()[0]
	quality, err := dataset.Evaluate(dataset.QualityInput{Policy: datasetPolicy, Manifest: manifest, InstrumentWindows: []dataset.InstrumentWindow{{InstrumentID: option.ID, ValidFrom: Start.Add(-24 * time.Hour), EvidenceSHA256: strings.Repeat("2", 64)}}, Sessions: []dataset.SessionEvidence{{PartitionContentSHA256: partition.ContentSHA256, ExpectedEffectiveAt: []time.Time{exchangeAt}, EvidenceSHA256: strings.Repeat("3", 64)}}, ExternalAssessments: []dataset.ExternalAssessment{{PartitionContentSHA256: partition.ContentSHA256, Check: dataset.CheckProviderSpotCompare, Status: dataset.CheckPassed, EvidenceSHA256: strings.Repeat("4", 64)}}})
	if err != nil || quality.Quarantined() {
		return nil, fmt.Errorf("wheel qualification dataset quality is invalid: %w", err)
	}
	simulationPolicy, err := simulation.NewPolicy(simulation.PolicyInput{Schema: simulation.PolicySchemaV1, Assets: []simulation.AssetPolicy{{AssetClass: instrument.AssetClassOption, OrderTypes: []lifecycle.OrderType{lifecycle.OrderLimit}, TimeInForce: []lifecycle.TimeInForce{lifecycle.TimeInForceDay}, QuoteRequirements: marketdata.QuoteRequirements{RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true, RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true, RequireSessionStatus: true, AllowedMarketStatuses: []string{"open"}, AllowedSessionStatuses: []string{"regular"}, MaxAge: time.Minute}, MaxDepthParticipation: decimal.NewFromInt(1), Calendar: simulation.CalendarPolicy{Kind: simulation.CalendarExplicitSessions, Sessions: []simulation.SessionWindow{{Label: "wheel-v1-session", OpenAt: Start.Add(-time.Hour), CloseAt: Start.Add(8 * time.Hour)}}}, Fees: simulation.FeePolicy{PerOrder: decimal.NewFromInt(1), PerUnit: decimal.RequireFromString("0.01"), Scale: 6}}}})
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
	policy, err := wheel.NewPolicy(wheel.PolicyInput{Version: "wheel-v1-qualification", MinimumROIC: "0.1", MaximumDebtToAssets: "0.6", RequirePositiveFreeCash: true, MaximumQualityAgeSeconds: 48 * 3600, MaximumMarketDataAgeSeconds: 60, PutDeltaMinimum: "0.15", PutDeltaTarget: "0.25", PutDeltaMaximum: "0.35", CallDeltaMinimum: "0.15", CallDeltaTarget: "0.25", CallDeltaMaximum: "0.35", MinimumDTE: 1, MaximumDTE: 1, MinimumOpenInterest: "100", MinimumVolume: "10", MaximumSpreadRatio: "0.1", DeliverableQuantity: "100", MaximumContracts: 1, FeePerContract: "1", FeePerShare: "0", DecimalScale: 12})
	if err != nil {
		return nil, err
	}
	scenario, err := wheel.NewScenario(wheel.ScenarioInput{Policy: policy, UnderlyingID: underlying.ID, InitialCapital: account.StartingCapital.String(), EvaluationStart: Start, EvaluationEnd: End, Mode: mode, Events: []wheel.EventInput{{Kind: wheel.EventAssessQuality, OccurredAt: Start, UnderlyingMark: "100", Quality: &wheel.QualityEvidence{AvailableAt: Start, ROIC: "0.2", DebtToAssets: "0.3", FreeCashFlow: "1000", EvidenceID: id(6, mode), EvidenceSHA256: strings.Repeat("6", 64)}, EvidenceID: id(7, mode), EvidenceSHA256: strings.Repeat("7", 64)}, {Kind: wheel.EventOpenPut, OccurredAt: RouteAt, UnderlyingMark: "100", Candidates: []wheel.Candidate{{InstrumentID: option.ID, VenueContractID: contract.ID, PartitionContentSHA256: partition.ContentSHA256, SourceKey: snapshot.ObservationID, OptionType: "put", Strike: strike, Expiry: End, Delta: "-0.25", Bid: "2", Ask: "2.1", OpenInterest: "1000", Volume: "100", AvailableAt: RouteAt, EvidenceID: snapshot.ID, EvidenceSHA256: contentSHA}}, EvidenceID: id(8, mode), EvidenceSHA256: strings.Repeat("8", 64)}, {Kind: wheel.EventExpiry, OccurredAt: End, UnderlyingMark: "100", EvidenceID: id(9, mode), EvidenceSHA256: strings.Repeat("9", 64)}}})
	if err != nil {
		return nil, err
	}
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{Slug: "quality-filtered-wheel-v1", Name: "Quality-filtered wheel V1", Thesis: "deterministic quality-filtered cash-secured wheel qualification", AssetClasses: []instrument.AssetClass{instrument.AssetClassOption}})
	if err != nil {
		return nil, err
	}
	config, _ := json.Marshal(map[string]string{"policy_id": policy.ID().String(), "policy_sha256": policy.Digest(), "scenario_id": scenario.ID().String(), "scenario_sha256": scenario.Digest()})
	version, err := strategycatalog.NewVersion(strategycatalog.VersionInput{FamilyID: family.ID(), CompilerKind: "go", CompilerVersion: "go1.25.8", SourceCommit: strings.Repeat("a", 40), SourceTreeSHA256: strings.Repeat("b", 64), ConfigSchema: wheel.PolicySchemaV1, Config: config, DecisionContract: "quality-filtered-wheel-decision-v1", RequiredDatasetKinds: []dataset.Kind{dataset.KindQuotes}})
	if err != nil {
		return nil, err
	}
	experiment, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{VersionID: version.ID(), AccountID: account.ID, CapitalBindingID: binding.ID, ManifestID: manifest.ID(), QualityResultID: quality.ID(), SimulationPolicyVersion: simulationPolicy.Version(), CapitalPolicyVersion: capitalPolicy.Version(), Mode: mode, EvaluationStart: Start, EvaluationEnd: End, Seed: 402, DatasetQuarantined: false})
	if err != nil {
		return nil, err
	}
	identity, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: version.ID(), VersionSHA256: version.Digest(), CompilerKind: version.CompilerKind(), CompilerVersion: version.CompilerVersion(), SourceCommit: version.SourceCommit(), SourceTreeSHA256: version.SourceTreeSHA256(), DecisionContract: version.DecisionContract(), AdapterKind: wheel.AdapterKindV1, AdapterVersion: wheel.AdapterVersionV1, AdapterSHA256: wheel.AdapterSHA256(policy, scenario), RunnerContract: experimentrun.RunnerContractV1})
	if err != nil {
		return nil, err
	}
	program, err := wheel.NewProgram(identity, policy, scenario)
	if err != nil {
		return nil, err
	}
	return &Fixture{Graph: &experimentrun.EvidenceGraph{Experiment: experiment, Version: version, Manifest: manifest, Quality: quality, Account: account, CapitalBinding: binding, CapitalPolicy: capitalPolicy, CapitalState: state, SimulationPolicy: simulationPolicy, Instruments: map[uuid.UUID]*instrument.Instrument{option.ID: option}, VenueContracts: map[uuid.UUID]*instrument.VenueContract{contract.ID: contract}, Observations: []experimentrun.ObservationMaterial{{PartitionContentSHA256: partition.ContentSHA256, ObservationSourceKey: snapshot.ObservationID, ObservationContentSHA256: contentSHA, AvailableAt: RouteAt, CanonicalContent: content, Snapshot: *snapshot}}}, Program: program, Policy: policy, Scenario: scenario, Family: family, Version: version, DatasetPolicy: datasetArtifact, CapitalArtifact: capitalArtifact, SimulationArtifact: simulationArtifact, Underlying: underlying, Option: option, VenueContract: contract, QuoteSnapshot: snapshot}, nil
}

func capitalState(account domain.Account, binding capital.Binding, policy *capital.Policy, mode strategycatalog.ExperimentMode) (*capital.State, error) {
	checkpoint, through := id(10, mode), id(11, mode)
	projection := &ledger.PortfolioProjection{CheckpointID: checkpoint, ProjectionType: ledger.PortfolioProjectionType, Version: ledger.PortfolioProjectionVersion, FIFO: ledger.ProjectionFIFO, AccountID: account.ID, BaseCurrency: account.BaseCurrency, AsOf: Start.Add(-time.Second), ThroughTransactionID: through, TransactionCount: 1, InputChecksum: strings.Repeat("d", 64), Totals: ledger.ProjectionTotals{Cash: account.StartingCapital, NetCapital: account.StartingCapital, Equity: account.StartingCapital}}
	payload := map[string]any{"checkpoint_id": checkpoint.String(), "account_id": account.ID.String(), "base_currency": account.BaseCurrency, "as_of": Start.Add(-time.Second).Format("2006-01-02T15:04:05.000000Z"), "positions": []any{}, "totals": map[string]string{"cash": account.StartingCapital.String(), "market_value": "0", "equity": account.StartingCapital.String()}}
	projection.PayloadBytes, _ = json.Marshal(payload)
	projection.OutputChecksum = hash(projection.PayloadBytes)
	return capital.StateFromProjection(account, binding, policy, projection, nil)
}

func id(value byte, mode strategycatalog.ExperimentMode) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("wheel-v1/"+string(mode)+"/"+string([]byte{value})))
}

func hash(value []byte) string  { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func text(value string) *string { return &value }
