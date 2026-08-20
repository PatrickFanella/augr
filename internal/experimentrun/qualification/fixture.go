// Package qualification contains one explicit, deterministic OVR-303 fixture
// adapter. It exists only for local/golden qualification and is not a general
// strategy compiler or a production adapter registry.
package qualification

import (
	"context"
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
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

var (
	Start   = time.Date(2026, 8, 20, 15, 0, 0, 123456000, time.UTC)
	RouteAt = Start.Add(time.Minute)
	End     = Start.Add(2 * time.Minute)
)

type Fixture struct {
	Graph              *experimentrun.EvidenceGraph
	Program            experimentrun.Program
	Family             *strategycatalog.Family
	Version            *strategycatalog.Version
	DatasetPolicy      *dataset.PolicyArtifact
	CapitalArtifact    *capital.PolicyArtifact
	SimulationArtifact *simulation.PolicyArtifact
	Instrument         *instrument.Instrument
	VenueContract      *instrument.VenueContract
	QuoteSnapshot      *marketdata.QuoteSnapshot
}

type fixtureProgram struct {
	identity *experimentrun.ProgramIdentity
	step     experimentrun.StepInput
}

func (program *fixtureProgram) Identity() *experimentrun.ProgramIdentity { return program.identity }

func (program *fixtureProgram) Plan(_ context.Context, input experimentrun.ProgramInput) (*experimentrun.Plan, error) {
	return experimentrun.NewPlan(experimentrun.PlanInput{
		ExperimentID: input.ExperimentID, ProgramID: program.identity.ID(), AccountID: input.AccountID,
		CapitalStateID: input.CapitalStateID, CapitalStateSHA256: input.CapitalStateSHA256,
		CapitalProjectionCheckpointID: input.CapitalProjectionCheckpointID, CapitalStateBytes: input.CapitalStateBytes,
		ManifestID: input.ManifestID, ManifestSHA256: input.ManifestSHA256,
		EvaluationStart: parseTime(input.EvaluationStart), EvaluationEnd: parseTime(input.EvaluationEnd), Seed: input.Seed, Mode: input.Mode,
		Steps: []experimentrun.StepInput{program.step},
	})
}

func Build(mode strategycatalog.ExperimentMode) (*Fixture, error) {
	return build(mode, "10", "c", experimentrun.ActionExecute)
}

// BuildPartial creates the same pinned fixture with an order larger than the
// declared depth, proving partial-fill persistence without changing venue code.
func BuildPartial(mode strategycatalog.ExperimentMode) (*Fixture, error) {
	return build(mode, "25", "d", experimentrun.ActionExecute)
}

func BuildNoop(mode strategycatalog.ExperimentMode) (*Fixture, error) {
	return build(mode, "0", "e", experimentrun.ActionNoop)
}

func BuildRejected(mode strategycatalog.ExperimentMode) (*Fixture, error) {
	return build(mode, "0", "f", experimentrun.ActionRejected)
}

func build(mode strategycatalog.ExperimentMode, orderQuantity, adapterDigestCharacter string, action experimentrun.StepAction) (*Fixture, error) {
	if mode != strategycatalog.ExperimentPaperScored && mode != strategycatalog.ExperimentPaperStress {
		return nil, fmt.Errorf("qualification mode must be scored or stress")
	}
	environment := domain.AccountEnvironmentPaperScored
	profile := domain.MarginProfileRegT
	capitalAmount := decimal.NewFromInt(25_000)
	multiplier := decimal.NewFromInt(2)
	namespace := "paper_scored/ovr303-golden"
	if mode == strategycatalog.ExperimentPaperStress {
		environment = domain.AccountEnvironmentPaperStress
		profile = domain.MarginProfileStressUnlimited
		capitalAmount = decimal.NewFromInt(5_000_000)
		multiplier = decimal.Zero
		namespace = "paper_stress/ovr303-golden"
	}
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "OVR-303 golden " + string(mode), Environment: environment, Venue: "test-venue", BaseCurrency: "USD",
		StorageNamespace: namespace, StartingCapital: capitalAmount, BuyingPowerMultiplier: multiplier, MarginProfile: profile,
		CreatedBy: "ovr303-qualification", CreationMetadata: json.RawMessage(`{"fixture":"ovr303-golden"}`), CreatedAt: Start.Add(-2 * time.Hour),
	})
	if err != nil {
		return nil, err
	}
	account.ID = uuid.MustParse("30300000-0000-4000-8000-000000000321")
	if mode == strategycatalog.ExperimentPaperStress {
		account.ID = uuid.MustParse("30300000-0000-4000-8000-000000000421")
	}
	inst, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "figi:OVR303-GOLDEN", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "test-venue", Currency: "USD",
		TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive, Metadata: json.RawMessage(`{"fixture":"ovr303-golden"}`),
		CreatedAt: Start.Add(-2 * time.Hour),
	})
	if err != nil {
		return nil, err
	}
	inst.ID = uuid.MustParse("30300000-0000-4000-8000-000000000322")
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID: inst.ID, Venue: "test-venue", ContractID: "OVR303-GOLDEN", Currency: "USD",
		TickSize: inst.TickSize, LotSize: inst.LotSize, Multiplier: inst.Multiplier, SettlementMethod: inst.SettlementMethod,
		ValidFrom: Start.Add(-24 * time.Hour), Metadata: json.RawMessage(`{"fixture":"ovr303-golden"}`), CreatedAt: Start.Add(-2 * time.Hour),
	})
	if err != nil {
		return nil, err
	}
	contract.ID = uuid.MustParse("30300000-0000-4000-8000-000000000323")
	exchange := RouteAt.Add(-100 * time.Millisecond)
	available := RouteAt
	bid, ask := decimal.RequireFromString("10.18"), decimal.RequireFromString("10.20")
	nearDepth, farDepth := decimal.NewFromInt(5), decimal.NewFromInt(15)
	farBid, farAsk := decimal.RequireFromString("10.17"), decimal.RequireFromString("10.21")
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID: inst.ID, VenueContractID: &contract.ID, Provider: "fixture", Venue: contract.Venue, Source: "fixture-feed",
		ObservationNamespace: "ovr303/golden/quote", ObservationID: "quote-1", SourceRevision: "r1", ExchangeAt: &exchange,
		ReceivedAt: available, AvailableAt: &available, Bid: &bid, Ask: &ask, BidSize: &nearDepth, AskSize: &nearDepth,
		MarketStatus: "open", SessionStatus: "regular", Bids: []marketdata.DepthLevelInput{{Price: bid, Size: nearDepth}, {Price: farBid, Size: farDepth}},
		Asks: []marketdata.DepthLevelInput{{Price: ask, Size: nearDepth}, {Price: farAsk, Size: farDepth}}, Metadata: json.RawMessage(`{"fixture":"ovr303-golden"}`), CreatedAt: available,
	})
	if err != nil {
		return nil, err
	}
	snapshot.ID = uuid.MustParse("30300000-0000-4000-8000-000000000311")
	content, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	contentSHA := hash(content)
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: End, Partitions: []dataset.PartitionInput{{
		Kind: dataset.KindQuotes, Provider: "fixture", Source: "fixture-feed", Namespace: "ovr303/golden/quote",
		RequestSHA256: strings.Repeat("1", 64), MediaType: "application/json", SymbologyVersion: "figi-v1", AdjustmentPolicy: "not_applicable",
		Timezone: "UTC", Calendar: "24x7", Revision: "r1", License: "test-only", RetentionPolicy: "retain-golden",
		Observations: []dataset.ObservationInput{{
			SourceKey: snapshot.ObservationID, InstrumentID: inst.ID, EffectiveAt: exchange,
			ObservedAt: available, AvailableAt: available, Revision: "r1", ContentSHA256: contentSHA, Bid: text(bid.String()), Ask: text(ask.String()),
		}},
	}}})
	if err != nil {
		return nil, err
	}
	datasetPolicy, err := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	if err != nil {
		return nil, err
	}
	datasetArtifact, err := datasetPolicy.NewArtifact(Start.Add(-3 * time.Hour))
	if err != nil {
		return nil, err
	}
	partition := manifest.Partitions()[0]
	quality, err := dataset.Evaluate(dataset.QualityInput{
		Policy: datasetPolicy, Manifest: manifest,
		InstrumentWindows: []dataset.InstrumentWindow{{InstrumentID: inst.ID, ValidFrom: Start.Add(-24 * time.Hour), EvidenceSHA256: strings.Repeat("2", 64)}},
		Sessions:          []dataset.SessionEvidence{{PartitionContentSHA256: partition.ContentSHA256, ExpectedEffectiveAt: []time.Time{exchange}, EvidenceSHA256: strings.Repeat("3", 64)}},
		ExternalAssessments: []dataset.ExternalAssessment{{
			PartitionContentSHA256: partition.ContentSHA256, Check: dataset.CheckProviderSpotCompare,
			Status: dataset.CheckPassed, EvidenceSHA256: strings.Repeat("4", 64),
		}},
	})
	if err != nil || quality.Quarantined() {
		return nil, fmt.Errorf("qualification quality is invalid or quarantined: %w", err)
	}
	simulationPolicy, err := simulation.NewPolicy(simulation.PolicyInput{Schema: simulation.PolicySchemaV1, Assets: []simulation.AssetPolicy{{
		AssetClass: instrument.AssetClassEquity, OrderTypes: []lifecycle.OrderType{lifecycle.OrderMarket}, TimeInForce: []lifecycle.TimeInForce{lifecycle.TimeInForceGTC},
		QuoteRequirements: marketdata.QuoteRequirements{
			RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
			RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true, RequireSessionStatus: true,
			AllowedMarketStatuses: []string{"open"}, AllowedSessionStatuses: []string{"regular"}, MaxAge: time.Minute,
		},
		MaxDepthParticipation: decimal.NewFromInt(1), Calendar: simulation.CalendarPolicy{Kind: simulation.CalendarContinuous24x7},
		Fees: simulation.FeePolicy{PerOrder: decimal.NewFromInt(1), PerUnit: decimal.RequireFromString("0.001"), Scale: 6},
	}}})
	if err != nil {
		return nil, err
	}
	simulationArtifact, err := simulationPolicy.NewArtifact(Start.Add(-3 * time.Hour))
	if err != nil {
		return nil, err
	}
	capitalPolicy, err := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	if err != nil {
		return nil, err
	}
	capitalArtifact, err := capitalPolicy.NewArtifact(Start.Add(-3 * time.Hour))
	if err != nil {
		return nil, err
	}
	binding, err := capital.NewBinding(*account, capitalPolicy, account.StartingCapital, account.MarginProfile, Start.Add(-2*time.Hour))
	if err != nil {
		return nil, err
	}
	state, err := capitalState(*account, *binding, capitalPolicy, Start.Add(-time.Second), mode)
	if err != nil {
		return nil, err
	}
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{Slug: "ovr303-golden", Name: "OVR-303 golden", Thesis: "retained runner qualification", AssetClasses: []instrument.AssetClass{instrument.AssetClassEquity}})
	if err != nil {
		return nil, err
	}
	version, err := strategycatalog.NewVersion(strategycatalog.VersionInput{
		FamilyID: family.ID(), CompilerKind: "go", CompilerVersion: "go1.25", SourceCommit: strings.Repeat("a", 40), SourceTreeSHA256: strings.Repeat("b", 64),
		ConfigSchema: "ovr303-golden-v1", Config: json.RawMessage(`{"action":"` + string(action) + `","quantity":"` + orderQuantity + `"}`), DecisionContract: "single-market-order-v1",
		RequiredDatasetKinds: []dataset.Kind{dataset.KindQuotes},
	})
	if err != nil {
		return nil, err
	}
	experiment, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{
		VersionID: version.ID(), AccountID: account.ID, CapitalBindingID: binding.ID, ManifestID: manifest.ID(), QualityResultID: quality.ID(),
		SimulationPolicyVersion: simulationPolicy.Version(), CapitalPolicyVersion: capitalPolicy.Version(), Mode: mode,
		EvaluationStart: Start, EvaluationEnd: End, Seed: 303, DatasetQuarantined: false,
	})
	if err != nil {
		return nil, err
	}
	identity, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{
		VersionID: version.ID(), VersionSHA256: version.Digest(), CompilerKind: version.CompilerKind(), CompilerVersion: version.CompilerVersion(),
		SourceCommit: version.SourceCommit(), SourceTreeSHA256: version.SourceTreeSHA256(), DecisionContract: version.DecisionContract(),
		AdapterKind: "ovr303-qualification-fixture", AdapterVersion: "v1", AdapterSHA256: strings.Repeat(adapterDigestCharacter, 64), RunnerContract: experimentrun.RunnerContractV1,
	})
	if err != nil {
		return nil, err
	}
	step := experimentrun.StepInput{
		PartitionContentSHA256: partition.ContentSHA256, ObservationSourceKey: snapshot.ObservationID,
		ObservationContentSHA256: contentSHA, AvailableAt: available, Decision: json.RawMessage(`{"signal":"buy"}`), Action: action,
	}
	if action == experimentrun.ActionExecute {
		step.Intent = &experimentrun.IntentSpecInput{
			InstrumentID: inst.ID, VenueContractID: contract.ID, Side: "buy", OrderType: "market",
			TimeInForce: "gtc", Quantity: orderQuantity, DecisionAt: RouteAt, RouteAt: RouteAt,
		}
	} else if action == experimentrun.ActionRejected {
		step.Decision = json.RawMessage(`{"signal":"rejected"}`)
		step.RejectionCode = "fixture_policy_rejection"
	} else {
		step.Decision = json.RawMessage(`{"signal":"hold"}`)
	}
	return &Fixture{
		Graph: &experimentrun.EvidenceGraph{
			Experiment: experiment, Version: version, Manifest: manifest, Quality: quality, Account: account,
			CapitalBinding: binding, CapitalPolicy: capitalPolicy, CapitalState: state, SimulationPolicy: simulationPolicy,
			Instruments: map[uuid.UUID]*instrument.Instrument{inst.ID: inst}, VenueContracts: map[uuid.UUID]*instrument.VenueContract{contract.ID: contract},
			Observations: []experimentrun.ObservationMaterial{{
				PartitionContentSHA256: partition.ContentSHA256, ObservationSourceKey: snapshot.ObservationID,
				ObservationContentSHA256: contentSHA, AvailableAt: available, CanonicalContent: content, Snapshot: *snapshot,
			}},
		},
		Program: &fixtureProgram{identity: identity, step: step}, Family: family, Version: version, DatasetPolicy: datasetArtifact,
		CapitalArtifact: capitalArtifact, SimulationArtifact: simulationArtifact, Instrument: inst, VenueContract: contract, QuoteSnapshot: snapshot,
	}, nil
}

type Loader struct{ Graph *experimentrun.EvidenceGraph }

func (loader Loader) LoadExperimentEvidence(_ context.Context, experimentID uuid.UUID) (*experimentrun.EvidenceGraph, error) {
	if loader.Graph == nil || loader.Graph.Experiment == nil || loader.Graph.Experiment.ID() != experimentID {
		return nil, fmt.Errorf("qualification experiment evidence not found")
	}
	return loader.Graph, nil
}

func capitalState(account domain.Account, binding capital.Binding, policy *capital.Policy, asOf time.Time, mode strategycatalog.ExperimentMode) (*capital.State, error) {
	checkpoint := uuid.MustParse("30300000-0000-4000-8000-000000000398")
	through := uuid.MustParse("30300000-0000-4000-8000-000000000399")
	if mode == strategycatalog.ExperimentPaperStress {
		checkpoint = uuid.MustParse("30300000-0000-4000-8000-000000000498")
		through = uuid.MustParse("30300000-0000-4000-8000-000000000499")
	}
	projection := &ledger.PortfolioProjection{
		CheckpointID: checkpoint, ProjectionType: ledger.PortfolioProjectionType, Version: ledger.PortfolioProjectionVersion, FIFO: ledger.ProjectionFIFO,
		AccountID: account.ID, BaseCurrency: account.BaseCurrency, AsOf: asOf, ThroughTransactionID: through, TransactionCount: 1,
		InputChecksum: strings.Repeat("d", 64), Totals: ledger.ProjectionTotals{Cash: account.StartingCapital, NetCapital: account.StartingCapital, Equity: account.StartingCapital},
	}
	payload := struct {
		CheckpointID string `json:"checkpoint_id"`
		AccountID    string `json:"account_id"`
		BaseCurrency string `json:"base_currency"`
		AsOf         string `json:"as_of"`
		Positions    []any  `json:"positions"`
		Totals       struct {
			Cash        string `json:"cash"`
			MarketValue string `json:"market_value"`
			Equity      string `json:"equity"`
		} `json:"totals"`
	}{
		CheckpointID: projection.CheckpointID.String(), AccountID: account.ID.String(), BaseCurrency: account.BaseCurrency,
		AsOf: asOf.Format("2006-01-02T15:04:05.000000Z"), Positions: []any{},
	}
	payload.Totals.Cash, payload.Totals.MarketValue, payload.Totals.Equity = account.StartingCapital.String(), "0", account.StartingCapital.String()
	projection.PayloadBytes, _ = json.Marshal(payload)
	projection.OutputChecksum = hash(projection.PayloadBytes)
	return capital.StateFromProjection(account, binding, policy, projection, nil)
}

func hash(value []byte) string  { digest := sha256.Sum256(value); return hex.EncodeToString(digest[:]) }
func text(value string) *string { return &value }
func parseTime(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", value)
	return parsed
}
