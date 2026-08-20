package experimentrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestRunnerPersistsReloadsAndRetriesExactExecution(t *testing.T) {
	fixture := newRunnerFixture(t)
	runner, err := NewRunner(fixture.loader, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runner.Run(context.Background(), RunRequest{
		ExperimentID: fixture.graph.Experiment.ID(), AttemptID: uuid.MustParse("30300000-0000-4000-8000-000000000301"),
		StartedAt: fixture.start.Add(-time.Second), FinishedAt: fixture.end, Program: fixture.program,
	})
	if err != nil {
		t.Fatal(err)
	}
	metrics := first.Metrics()
	if metrics.StepCount != 1 || metrics.IntentCount != 1 || metrics.OrderCount != 1 || metrics.FillCount != 1 ||
		metrics.FilledQuantity != "10" || metrics.FeeTotal != "1.01" || len(fixture.store.attempts) != 2 || len(fixture.store.raw) != 1 {
		t.Fatalf("first run metrics/store = %+v attempts:%d raw:%d", metrics, len(fixture.store.attempts), len(fixture.store.raw))
	}
	second, err := runner.Run(context.Background(), RunRequest{
		ExperimentID: fixture.graph.Experiment.ID(), AttemptID: uuid.MustParse("30300000-0000-4000-8000-000000000302"),
		StartedAt: fixture.start, FinishedAt: fixture.end.Add(time.Second), Program: fixture.program,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != second.ID() || first.Digest() != second.Digest() || string(first.CanonicalBytes()) != string(second.CanonicalBytes()) ||
		len(fixture.store.raw) != 1 || len(fixture.store.lifecycles) != 1 || len(fixture.store.attempts) != 4 {
		t.Fatalf("retry diverged = %s/%s raw:%d lifecycles:%d attempts:%d", first.ID(), second.ID(), len(fixture.store.raw), len(fixture.store.lifecycles), len(fixture.store.attempts))
	}
	cleanStore := newRunnerStore()
	cleanRunner, _ := NewRunner(fixture.loader, cleanStore)
	clean, err := cleanRunner.Run(context.Background(), RunRequest{
		ExperimentID: fixture.graph.Experiment.ID(), AttemptID: uuid.MustParse("30300000-0000-4000-8000-000000000303"),
		StartedAt: fixture.start.Add(time.Second), FinishedAt: fixture.end.Add(2 * time.Second), Program: fixture.program,
	})
	if err != nil || clean.ID() != first.ID() || clean.Digest() != first.Digest() || string(clean.CanonicalBytes()) != string(first.CanonicalBytes()) {
		t.Fatalf("clean replay diverged = %+v/%v", clean, err)
	}
}

func TestRunnerFailsClosedAndRecordsNoCompletedResult(t *testing.T) {
	tests := map[string]func(*runnerFixture){
		"program identity":   func(f *runnerFixture) { f.program.identity.canonical.RunnerContract = "wrong-runner" },
		"manifest material":  func(f *runnerFixture) { f.graph.Observations[0].CanonicalContent = []byte(`{"tampered":true}`) },
		"result persistence": func(f *runnerFixture) { f.store.failResult = true },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newRunnerFixture(t)
			mutate(fixture)
			runner, _ := NewRunner(fixture.loader, fixture.store)
			result, err := runner.Run(context.Background(), RunRequest{
				ExperimentID: fixture.graph.Experiment.ID(), AttemptID: uuid.New(),
				StartedAt: fixture.start.Add(-time.Second), FinishedAt: fixture.end, Program: fixture.program,
			})
			if err == nil || result != nil || len(fixture.store.results) != 0 || len(fixture.store.attempts) != 2 || fixture.store.attempts[1].Type() != AttemptFailed {
				t.Fatalf("failure result=%+v err=%v results=%d attempts=%d", result, err, len(fixture.store.results), len(fixture.store.attempts))
			}
		})
	}
}

func TestRunnerCancellationAppendsFailedAttemptWithoutEconomics(t *testing.T) {
	fixture := newRunnerFixture(t)
	fixture.program.planErr = context.Canceled
	runner, _ := NewRunner(fixture.loader, fixture.store)
	result, err := runner.Run(context.Background(), RunRequest{
		ExperimentID: fixture.graph.Experiment.ID(), AttemptID: uuid.New(),
		StartedAt: fixture.start.Add(-time.Second), FinishedAt: fixture.end, Program: fixture.program,
	})
	if !errors.Is(err, context.Canceled) || result != nil || len(fixture.store.lifecycles) != 0 || len(fixture.store.results) != 0 ||
		len(fixture.store.attempts) != 2 || fixture.store.attempts[1].Type() != AttemptFailed {
		t.Fatalf("cancellation result=%+v err=%v lifecycles=%d results=%d attempts=%d", result, err, len(fixture.store.lifecycles), len(fixture.store.results), len(fixture.store.attempts))
	}
}

func TestRunnerRetriesAfterAtomicCompletionFailure(t *testing.T) {
	fixture := newRunnerFixture(t)
	fixture.store.failResult = true
	runner, _ := NewRunner(fixture.loader, fixture.store)
	failed, err := runner.Run(context.Background(), RunRequest{
		ExperimentID: fixture.graph.Experiment.ID(), AttemptID: uuid.New(), StartedAt: fixture.start.Add(-time.Second), FinishedAt: fixture.end, Program: fixture.program,
	})
	if err == nil || failed != nil || len(fixture.store.results) != 0 || len(fixture.store.attempts) != 2 {
		t.Fatalf("injected completion failure = %+v/%v", failed, err)
	}
	fixture.store.failResult = false
	completed, err := runner.Run(context.Background(), RunRequest{
		ExperimentID: fixture.graph.Experiment.ID(), AttemptID: uuid.New(), StartedAt: fixture.start, FinishedAt: fixture.end.Add(time.Second), Program: fixture.program,
	})
	if err != nil || completed == nil || len(fixture.store.results) != 1 || len(fixture.store.raw) != 1 || len(fixture.store.lifecycles) != 1 ||
		len(fixture.store.attempts) != 4 || fixture.store.attempts[3].Type() != AttemptCompleted {
		t.Fatalf("completion retry = %+v/%v results:%d raw:%d attempts:%d", completed, err, len(fixture.store.results), len(fixture.store.raw), len(fixture.store.attempts))
	}
}

type runnerFixture struct {
	start, end time.Time
	graph      *EvidenceGraph
	loader     *runnerLoader
	store      *runnerStore
	program    *runnerProgram
}

func newRunnerFixture(t *testing.T) *runnerFixture {
	t.Helper()
	start := time.Date(2026, 8, 20, 15, 0, 0, 123456000, time.UTC)
	routeAt := start.Add(time.Minute)
	end := start.Add(2 * time.Minute)
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "OVR-303 scored", Environment: domain.AccountEnvironmentPaperScored, Venue: "test-venue", BaseCurrency: "USD",
		StorageNamespace: "paper_scored/ovr303", StartingCapital: decimal.NewFromInt(25000), BuyingPowerMultiplier: decimal.NewFromInt(2),
		MarginProfile: domain.MarginProfileRegT, CreatedBy: "runner-test", CreationMetadata: json.RawMessage(`{"fixture":"ovr303"}`), CreatedAt: start.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "figi:OVR303", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "test-venue", Currency: "USD",
		TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, Metadata: json.RawMessage(`{}`), CreatedAt: start.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID: inst.ID, Venue: "test-venue", ContractID: "OVR303", Currency: "USD",
		TickSize: inst.TickSize, LotSize: inst.LotSize, Multiplier: inst.Multiplier, SettlementMethod: inst.SettlementMethod,
		ValidFrom: start.Add(-24 * time.Hour), Metadata: json.RawMessage(`{}`), CreatedAt: start.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	available := routeAt
	exchange := routeAt.Add(-100 * time.Millisecond)
	bid, ask := decimal.RequireFromString("10.18"), decimal.RequireFromString("10.20")
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID: inst.ID, VenueContractID: &contract.ID, Provider: "fixture", Venue: contract.Venue, Source: "fixture-feed",
		ObservationNamespace: "ovr303/quote", ObservationID: "quote-1", SourceRevision: "r1", ExchangeAt: &exchange,
		ReceivedAt: available, AvailableAt: &available, Bid: &bid, Ask: &ask, BidSize: decimalPtr("20"), AskSize: decimalPtr("20"),
		MarketStatus: "open", SessionStatus: "regular", Bids: []marketdata.DepthLevelInput{{Price: bid, Size: decimal.NewFromInt(20)}},
		Asks: []marketdata.DepthLevelInput{{Price: ask, Size: decimal.NewFromInt(20)}}, Metadata: json.RawMessage(`{"fixture":"ovr303"}`), CreatedAt: available,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ID = uuid.MustParse("30300000-0000-4000-8000-000000000311")
	content, _ := json.Marshal(snapshot)
	contentSHA := hashBytes(content)
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: end, Partitions: []dataset.PartitionInput{{
		Kind: dataset.KindQuotes, Provider: "fixture", Source: "fixture-feed", Namespace: "ovr303/quote", RequestSHA256: strings.Repeat("1", 64),
		MediaType: "application/json", SymbologyVersion: "figi-v1", AdjustmentPolicy: "not_applicable", Timezone: "UTC", Calendar: "24x7", Revision: "r1",
		License: "test-only", RetentionPolicy: "retain", Observations: []dataset.ObservationInput{{
			SourceKey: snapshot.ObservationID, InstrumentID: inst.ID, EffectiveAt: exchange, ObservedAt: available, AvailableAt: available,
			Revision: "r1", ContentSHA256: contentSHA, Bid: stringPtr(bid.String()), Ask: stringPtr(ask.String()),
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	partition := manifest.Partitions()[0]
	qualityPolicy, _ := dataset.NewPolicy(dataset.ReviewedPolicyV1Input())
	quality, err := dataset.Evaluate(dataset.QualityInput{
		Policy: qualityPolicy, Manifest: manifest,
		InstrumentWindows:   []dataset.InstrumentWindow{{InstrumentID: inst.ID, ValidFrom: start.Add(-24 * time.Hour), EvidenceSHA256: strings.Repeat("2", 64)}},
		Sessions:            []dataset.SessionEvidence{{PartitionContentSHA256: partition.ContentSHA256, ExpectedEffectiveAt: []time.Time{exchange}, EvidenceSHA256: strings.Repeat("3", 64)}},
		ExternalAssessments: []dataset.ExternalAssessment{{PartitionContentSHA256: partition.ContentSHA256, Check: dataset.CheckProviderSpotCompare, Status: dataset.CheckPassed, EvidenceSHA256: strings.Repeat("4", 64)}},
	})
	if err != nil || quality.Quarantined() {
		t.Fatalf("quality = %+v/%v", quality, err)
	}
	simulationPolicy, err := simulation.NewPolicy(simulation.PolicyInput{Schema: simulation.PolicySchemaV1, Assets: []simulation.AssetPolicy{{
		AssetClass: instrument.AssetClassEquity, OrderTypes: []lifecycle.OrderType{lifecycle.OrderMarket}, TimeInForce: []lifecycle.TimeInForce{lifecycle.TimeInForceGTC},
		QuoteRequirements:     marketdata.QuoteRequirements{RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true, RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true, RequireSessionStatus: true, AllowedMarketStatuses: []string{"open"}, AllowedSessionStatuses: []string{"regular"}, MaxAge: time.Minute},
		MaxDepthParticipation: decimal.NewFromInt(1), Calendar: simulation.CalendarPolicy{Kind: simulation.CalendarContinuous24x7},
		Fees: simulation.FeePolicy{PerOrder: decimal.NewFromInt(1), PerUnit: decimal.RequireFromString("0.001"), Scale: 6},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	capitalPolicy, _ := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	binding, err := capital.NewBinding(*account, capitalPolicy, account.StartingCapital, account.MarginProfile, start.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	state := runnerCapitalState(t, *account, *binding, capitalPolicy, start.Add(-time.Second))
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{Slug: "ovr303-fixture", Name: "OVR-303 fixture", Thesis: "runner qualification", AssetClasses: []instrument.AssetClass{instrument.AssetClassEquity}})
	if err != nil {
		t.Fatal(err)
	}
	version, err := strategycatalog.NewVersion(strategycatalog.VersionInput{
		FamilyID: family.ID(), CompilerKind: "go", CompilerVersion: "go1.25", SourceCommit: strings.Repeat("a", 40), SourceTreeSHA256: strings.Repeat("b", 64),
		ConfigSchema: "ovr303-fixture-v1", Config: json.RawMessage(`{"quantity":"10"}`), DecisionContract: "single-market-order-v1", RequiredDatasetKinds: []dataset.Kind{dataset.KindQuotes},
	})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{
		VersionID: version.ID(), AccountID: account.ID, CapitalBindingID: binding.ID, ManifestID: manifest.ID(), QualityResultID: quality.ID(),
		SimulationPolicyVersion: simulationPolicy.Version(), CapitalPolicyVersion: capitalPolicy.Version(), Mode: strategycatalog.ExperimentPaperScored,
		EvaluationStart: start, EvaluationEnd: end, Seed: 303, DatasetQuarantined: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := NewProgramIdentity(ProgramIdentityInput{
		VersionID: version.ID(), VersionSHA256: version.Digest(), CompilerKind: version.CompilerKind(), CompilerVersion: version.CompilerVersion(),
		SourceCommit: version.SourceCommit(), SourceTreeSHA256: version.SourceTreeSHA256(), DecisionContract: version.DecisionContract(),
		AdapterKind: "fixture", AdapterVersion: "v1", AdapterSHA256: strings.Repeat("c", 64), RunnerContract: RunnerContractV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	graph := &EvidenceGraph{
		Experiment: experiment, Version: version, Manifest: manifest, Quality: quality, Account: account, CapitalBinding: binding,
		CapitalPolicy: capitalPolicy, CapitalState: state, SimulationPolicy: simulationPolicy,
		Instruments: map[uuid.UUID]*instrument.Instrument{inst.ID: inst}, VenueContracts: map[uuid.UUID]*instrument.VenueContract{contract.ID: contract},
		Observations: []ObservationMaterial{{PartitionContentSHA256: partition.ContentSHA256, ObservationSourceKey: snapshot.ObservationID, ObservationContentSHA256: contentSHA, AvailableAt: available, CanonicalContent: content, Snapshot: *snapshot}},
	}
	program := &runnerProgram{identity: identity, build: func(input ProgramInput) (*Plan, error) {
		return NewPlan(PlanInput{
			ExperimentID: input.ExperimentID, ProgramID: identity.ID(), AccountID: input.AccountID, ManifestID: input.ManifestID,
			ManifestSHA256: input.ManifestSHA256, EvaluationStart: start, EvaluationEnd: end, Seed: input.Seed, Mode: input.Mode,
			Steps: []StepInput{{
				PartitionContentSHA256: partition.ContentSHA256, ObservationSourceKey: snapshot.ObservationID, ObservationContentSHA256: contentSHA, AvailableAt: available,
				Decision: json.RawMessage(`{"signal":"buy"}`), Action: ActionExecute,
				Intent: &IntentSpecInput{InstrumentID: inst.ID, VenueContractID: contract.ID, Side: "buy", OrderType: "market", TimeInForce: "gtc", Quantity: "10", DecisionAt: routeAt, RouteAt: routeAt},
			}},
		})
	}}
	store := newRunnerStore()
	return &runnerFixture{start: start, end: end, graph: graph, loader: &runnerLoader{graph: graph}, store: store, program: program}
}

type runnerProgram struct {
	identity *ProgramIdentity
	build    func(ProgramInput) (*Plan, error)
	planErr  error
}

func (program *runnerProgram) Identity() *ProgramIdentity { return program.identity }
func (program *runnerProgram) Plan(_ context.Context, input ProgramInput) (*Plan, error) {
	if program.planErr != nil {
		return nil, program.planErr
	}
	return program.build(input)
}

type runnerLoader struct{ graph *EvidenceGraph }

func (loader *runnerLoader) LoadExperimentEvidence(context.Context, uuid.UUID) (*EvidenceGraph, error) {
	return loader.graph, nil
}

type runnerStore struct {
	programs   map[uuid.UUID]*ProgramIdentity
	plans      map[uuid.UUID]*Plan
	attempts   []*AttemptEvent
	results    map[uuid.UUID]*Result
	lifecycles map[uuid.UUID]*lifecycle.Aggregate
	raw        map[uuid.UUID]*ledger.EconomicSourceEvent
	failResult bool
}

func newRunnerStore() *runnerStore {
	return &runnerStore{programs: map[uuid.UUID]*ProgramIdentity{}, plans: map[uuid.UUID]*Plan{}, results: map[uuid.UUID]*Result{}, lifecycles: map[uuid.UUID]*lifecycle.Aggregate{}, raw: map[uuid.UUID]*ledger.EconomicSourceEvent{}}
}

func (store *runnerStore) RecordProgram(_ context.Context, value *ProgramIdentity) (*ProgramIdentity, error) {
	if old := store.programs[value.ID()]; old != nil && !sameProgram(old, value) {
		return nil, errors.New("program conflict")
	}
	store.programs[value.ID()] = value
	return value, nil
}

func (store *runnerStore) RecordPlan(_ context.Context, value *Plan) (*Plan, error) {
	if old := store.plans[value.ID()]; old != nil && !samePlan(old, value) {
		return nil, errors.New("plan conflict")
	}
	store.plans[value.ID()] = value
	return value, nil
}

func (store *runnerStore) RecordAttemptEvent(_ context.Context, _, _ uuid.UUID, value *AttemptEvent) (*AttemptEvent, error) {
	for _, old := range store.attempts {
		if old.ID() == value.ID() {
			return old, nil
		}
	}
	store.attempts = append(store.attempts, value)
	return value, nil
}

func (store *runnerStore) RecordCompletedResult(_ context.Context, _ uuid.UUID, value *Result, event *AttemptEvent) (*Result, *AttemptEvent, error) {
	if store.failResult {
		return nil, nil, errors.New("injected result failure")
	}
	if old := store.results[value.ID()]; old != nil && !sameResult(old, value) {
		return nil, nil, errors.New("result conflict")
	}
	store.results[value.ID()] = value
	store.attempts = append(store.attempts, event)
	return value, event, nil
}

func (store *runnerStore) ProposeExecutionIntent(_ context.Context, value *lifecycle.Aggregate) (*lifecycle.Aggregate, error) {
	if old := store.lifecycles[value.Intent.ID]; old != nil {
		return old, nil
	}
	store.lifecycles[value.Intent.ID] = value
	return value, nil
}

func (store *runnerStore) RecordEconomicSourceEvent(_ context.Context, value *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error) {
	if old := store.raw[value.ID]; old != nil {
		return old, nil
	}
	store.raw[value.ID] = value
	return value, nil
}

func (store *runnerStore) ApplyExecutionFill(ctx context.Context, account uuid.UUID, value *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	return store.apply(ctx, account, value)
}

func (store *runnerStore) ApplyExecutionTransition(ctx context.Context, account uuid.UUID, value *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	return store.apply(ctx, account, value)
}

func (store *runnerStore) apply(_ context.Context, account uuid.UUID, value *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	current := store.lifecycles[value.Event.IntentID]
	if current == nil || current.Intent.AccountID != account {
		return nil, errors.New("lifecycle missing")
	}
	for _, event := range current.Events {
		if event.ID == value.Event.ID {
			return current, nil
		}
	}
	next, err := lifecycle.ApplyTransition(current, value)
	if err != nil {
		return nil, err
	}
	store.lifecycles[value.Event.IntentID] = next
	return next, nil
}

func (store *runnerStore) GetExecutionLifecycle(_ context.Context, account, intentID uuid.UUID) (*lifecycle.Aggregate, error) {
	value := store.lifecycles[intentID]
	if value == nil || value.Intent.AccountID != account {
		return nil, errors.New("lifecycle missing")
	}
	return value, nil
}

func runnerCapitalState(t *testing.T, account domain.Account, binding capital.Binding, policy *capital.Policy, asOf time.Time) *capital.State {
	t.Helper()
	projection := &ledger.PortfolioProjection{
		CheckpointID: uuid.New(), ProjectionType: ledger.PortfolioProjectionType, Version: ledger.PortfolioProjectionVersion, FIFO: ledger.ProjectionFIFO,
		AccountID: account.ID, BaseCurrency: account.BaseCurrency, AsOf: asOf, ThroughTransactionID: uuid.New(), TransactionCount: 1,
		InputChecksum: strings.Repeat("d", 64), Totals: ledger.ProjectionTotals{Cash: account.StartingCapital, NetCapital: account.StartingCapital, Equity: account.StartingCapital},
	}
	payload := struct {
		CheckpointID string `json:"checkpoint_id"`
		AccountID    string `json:"account_id"`
		BaseCurrency string `json:"base_currency"`
		AsOf         string `json:"as_of"`
		Positions    []struct {
			InstrumentID string `json:"instrument_id"`
			Open         bool   `json:"open"`
			Quantity     string `json:"quantity"`
			MarketValue  string `json:"market_value"`
		} `json:"positions"`
		Totals struct {
			Cash        string `json:"cash"`
			MarketValue string `json:"market_value"`
			Equity      string `json:"equity"`
		} `json:"totals"`
	}{CheckpointID: projection.CheckpointID.String(), AccountID: account.ID.String(), BaseCurrency: account.BaseCurrency, AsOf: asOf.Format(timeLayout), Positions: []struct {
		InstrumentID string `json:"instrument_id"`
		Open         bool   `json:"open"`
		Quantity     string `json:"quantity"`
		MarketValue  string `json:"market_value"`
	}{}}
	payload.Totals.Cash, payload.Totals.MarketValue, payload.Totals.Equity = account.StartingCapital.String(), "0", account.StartingCapital.String()
	projection.PayloadBytes, _ = json.Marshal(payload)
	digest := sha256.Sum256(projection.PayloadBytes)
	projection.OutputChecksum = hex.EncodeToString(digest[:])
	state, err := capital.StateFromProjection(account, binding, policy, projection, nil)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func decimalPtr(value string) *decimal.Decimal {
	parsed := decimal.RequireFromString(value)
	return &parsed
}
func stringPtr(value string) *string { return &value }
