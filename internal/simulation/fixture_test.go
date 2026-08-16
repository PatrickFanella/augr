package simulation

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

type venueFixtureConfig struct {
	assetClass            instrument.AssetClass
	policyAssetClass      instrument.AssetClass
	orderType             lifecycle.OrderType
	timeInForce           lifecycle.TimeInForce
	side                  lifecycle.Side
	quantity              decimal.Decimal
	limitPrice            *decimal.Decimal
	lotSize               decimal.Decimal
	multiplier            decimal.Decimal
	maxDepthParticipation decimal.Decimal
	fixedLatency          time.Duration
	calendar              CalendarPolicy
	policyOrderTypes      []lifecycle.OrderType
	policyTimeInForce     []lifecycle.TimeInForce
	perOrderFee           decimal.Decimal
	perUnitFee            decimal.Decimal
	notionalBPS           decimal.Decimal
	accountEnvironment    domain.AccountEnvironment
	accountStorage        string
	routeAt               time.Time
}

type venueFixture struct {
	t          *testing.T
	base       time.Time
	routeAt    time.Time
	account    domain.Account
	instrument instrument.Instrument
	contract   instrument.VenueContract
	policy     *Policy
	venue      *Venue
	aggregate  *lifecycle.Aggregate
}

func newVenueFixture(t *testing.T, modify func(*venueFixtureConfig)) venueFixture {
	t.Helper()
	base := time.Date(2026, 8, 17, 12, 0, 0, 123456000, time.UTC)
	limit := decimal.RequireFromString("10.25")
	config := venueFixtureConfig{
		assetClass:            instrument.AssetClassEquity,
		orderType:             lifecycle.OrderMarket,
		timeInForce:           lifecycle.TimeInForceDay,
		side:                  lifecycle.SideBuy,
		quantity:              decimal.NewFromInt(10),
		limitPrice:            &limit,
		lotSize:               decimal.NewFromInt(1),
		multiplier:            decimal.NewFromInt(1),
		maxDepthParticipation: decimal.NewFromInt(1),
		fixedLatency:          100 * time.Millisecond,
		calendar: CalendarPolicy{
			Kind: CalendarExplicitSessions,
			Sessions: []SessionWindow{{
				Label: "regular-2026-08-17", OpenAt: base, CloseAt: base.Add(6 * time.Hour),
			}},
		},
		policyOrderTypes:   []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit},
		policyTimeInForce:  []lifecycle.TimeInForce{lifecycle.TimeInForceDay, lifecycle.TimeInForceGTC, lifecycle.TimeInForceIOC, lifecycle.TimeInForceFOK},
		perOrderFee:        decimal.RequireFromString("1.25"),
		perUnitFee:         decimal.RequireFromString("0.01"),
		notionalBPS:        decimal.RequireFromString("2"),
		accountEnvironment: domain.AccountEnvironmentPaperScored,
		accountStorage:     "paper_scored/simulation-tests",
		routeAt:            base.Add(time.Hour),
	}
	if modify != nil {
		modify(&config)
	}
	if config.routeAt.IsZero() {
		config.routeAt = base.Add(time.Hour)
	}
	if config.accountEnvironment == domain.AccountEnvironmentPaperStress && config.accountStorage == "paper_scored/simulation-tests" {
		config.accountStorage = "paper_stress/simulation-tests"
	}

	account, err := domain.NewAccount(domain.AccountInput{
		Name: "simulation fixture", Environment: config.accountEnvironment, Venue: "test-venue",
		BaseCurrency: "USD", StorageNamespace: config.accountStorage,
		StartingCapital: decimal.NewFromInt(100000), BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile: domain.MarginProfileCash, CreatedBy: "simulation-test",
		CreationMetadata: json.RawMessage(`{"fixture":"simulation"}`), CreatedAt: base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	settlement := instrument.SettlementPhysical
	if config.assetClass == instrument.AssetClassCryptoSpot {
		settlement = instrument.SettlementCrypto
	} else if config.assetClass == instrument.AssetClassPredictionContract {
		settlement = instrument.SettlementBinary
	}
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "simulation:fixture:" + string(config.assetClass), AssetClass: config.assetClass,
		PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: config.lotSize, Multiplier: config.multiplier, SettlementMethod: settlement,
		Status: instrument.StatusActive, Metadata: json.RawMessage(`{"fixture":"simulation"}`),
		CreatedAt: base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	validTo := base.Add(30 * 24 * time.Hour)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID: reference.ID, Venue: "test-venue", ContractID: "SIMULATION-FIXTURE",
		Currency: "USD", TickSize: reference.TickSize, LotSize: config.lotSize,
		Multiplier: config.multiplier, SettlementMethod: settlement,
		ValidFrom: base.Add(-24 * time.Hour), ValidTo: &validTo,
		Metadata: json.RawMessage(`{"fixture":"simulation"}`), CreatedAt: base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	requirements := simulationFixtureQuoteRequirements()
	policyAssetClass := config.policyAssetClass
	if policyAssetClass == "" {
		policyAssetClass = config.assetClass
	}
	policy, err := NewPolicy(PolicyInput{
		Schema: PolicySchemaV1,
		Assets: []AssetPolicy{{
			AssetClass: policyAssetClass, OrderTypes: config.policyOrderTypes,
			TimeInForce: config.policyTimeInForce, QuoteRequirements: requirements,
			MaxDepthParticipation: config.maxDepthParticipation, FixedLatency: config.fixedLatency,
			Calendar: config.calendar,
			Fees:     FeePolicy{PerOrder: config.perOrderFee, PerUnit: config.perUnitFee, NotionalBPS: config.notionalBPS, Scale: 4},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	routeSnapshot := newVenueFixtureSnapshot(t, *reference, *contract, "route", config.routeAt.Add(-300*time.Millisecond), config.routeAt.Add(-200*time.Millisecond), config.routeAt.Add(-100*time.Millisecond),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(20)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(20)}},
	)
	delta := config.quantity
	if config.side == lifecycle.SideSell {
		delta = delta.Neg()
	}
	proposed, err := lifecycle.Propose(lifecycle.ProposeInput{
		Account: *account, Instrument: *reference, DecisionSnapshot: routeSnapshot,
		IdempotencyKey: "simulation-fixture", DesiredQuantityDelta: delta, DecisionAt: config.routeAt,
		OriginType: ledger.ExecutionOriginStrategyVersion, OriginID: "strategy-version-1",
		StrategyVersionID: "strategy-version-1", Metadata: json.RawMessage(`{"signal":"entry"}`),
		Event:     simulationFixtureEvent("proposal", config.routeAt.Add(-time.Millisecond), config.routeAt, json.RawMessage(`{"signal":"entry"}`)),
		CreatedAt: config.routeAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := lifecycle.Allocate(proposed, delta, simulationFixtureEvent("allocation", config.routeAt, config.routeAt, json.RawMessage(`{"quantity":"10"}`)), config.routeAt)
	if err != nil {
		t.Fatal(err)
	}
	allocated, err := lifecycle.ApplyTransition(proposed, allocation)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := lifecycle.ApproveRisk(allocated, simulationFixtureEvent("risk", config.routeAt, config.routeAt, json.RawMessage(`{"approved":true}`)), config.routeAt)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := lifecycle.ApplyTransition(allocated, approval)
	if err != nil {
		t.Fatal(err)
	}
	var limitPrice *decimal.Decimal
	if config.orderType == lifecycle.OrderLimit || config.orderType == lifecycle.OrderStopLimit {
		limitPrice = config.limitPrice
	}
	route, err := lifecycle.Route(approved, lifecycle.RouteInput{
		OrderIdempotencyKey: "simulation-order", Instrument: *reference, VenueContract: *contract,
		RouteSnapshot: routeSnapshot, QuoteRequirements: requirements,
		OrderType: config.orderType, TimeInForce: config.timeInForce, LimitPrice: limitPrice,
		PolicyKind: lifecycle.PolicySimulation, PolicyVersion: policy.Version(),
		Event:    simulationFixtureEvent("route", config.routeAt, config.routeAt, json.RawMessage(`{"route":"simulation"}`)),
		RoutedAt: config.routeAt, CreatedAt: config.routeAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	routed, err := lifecycle.ApplyTransition(approved, route)
	if err != nil {
		t.Fatal(err)
	}
	venue, err := NewVenue(policy)
	if err != nil {
		t.Fatal(err)
	}
	return venueFixture{
		t: t, base: base, routeAt: config.routeAt, account: *account, instrument: *reference,
		contract: *contract, policy: policy, venue: venue, aggregate: routed,
	}
}

func (fixture venueFixture) snapshot(
	label string,
	availableAt time.Time,
	bids, asks []marketdata.DepthLevelInput,
) marketdata.QuoteSnapshot {
	fixture.t.Helper()
	return newVenueFixtureSnapshot(
		fixture.t, fixture.instrument, fixture.contract, label,
		availableAt.Add(-50*time.Millisecond), availableAt.Add(-25*time.Millisecond), availableAt,
		bids, asks,
	)
}

func (fixture venueFixture) evaluate(
	aggregate *lifecycle.Aggregate,
	snapshot marketdata.QuoteSnapshot,
	evaluatedAt time.Time,
) (*Result, error) {
	fixture.t.Helper()
	return fixture.venue.Evaluate(EvaluationRequest{
		Account: fixture.account, Instrument: fixture.instrument, VenueContract: fixture.contract,
		Aggregate: aggregate, Snapshot: snapshot, EvaluatedAt: evaluatedAt,
	})
}

func newVenueFixtureSnapshot(
	t *testing.T,
	reference instrument.Instrument,
	contract instrument.VenueContract,
	label string,
	exchangeAt, receivedAt, availableAt time.Time,
	bids, asks []marketdata.DepthLevelInput,
) marketdata.QuoteSnapshot {
	t.Helper()
	var bid, bidSize, ask, askSize *decimal.Decimal
	if len(bids) > 0 {
		bidValue, sizeValue := bids[0].Price, bids[0].Size
		bid, bidSize = &bidValue, &sizeValue
	}
	if len(asks) > 0 {
		askValue, sizeValue := asks[0].Price, asks[0].Size
		ask, askSize = &askValue, &sizeValue
	}
	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID: reference.ID, VenueContractID: &contract.ID, Provider: "simulation-fixture",
		Venue: contract.Venue, Source: "fixture-feed", ObservationNamespace: "quotes/simulation",
		ObservationID: label, SourceRevision: "v1", ExchangeAt: &exchangeAt,
		ReceivedAt: receivedAt, AvailableAt: &availableAt,
		Bid: bid, BidSize: bidSize, Ask: ask, AskSize: askSize,
		MarketStatus: "open", SessionStatus: "regular", Bids: bids, Asks: asks,
		Metadata: json.RawMessage(`{"fixture":"simulation"}`), CreatedAt: availableAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return *snapshot
}

func simulationFixtureQuoteRequirements() marketdata.QuoteRequirements {
	return marketdata.QuoteRequirements{
		RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
		RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true,
		RequireSessionStatus: true, AllowedMarketStatuses: []string{"open"},
		AllowedSessionStatuses: []string{"regular"}, MaxAge: 2 * time.Second,
	}
}

func simulationFixtureEvent(id string, sourceAt, receivedAt time.Time, evidence json.RawMessage) lifecycle.EventInput {
	return lifecycle.EventInput{
		Source: "simulation-test", SourceNamespace: "simulation/fixture", SourceEventID: id,
		SourceAt: sourceAt, ReceivedAt: receivedAt, Actor: "simulation-test",
		ReasonCode: id, Evidence: evidence,
	}
}

func decimalTestPointer(value string) *decimal.Decimal {
	parsed := decimal.RequireFromString(value)
	return &parsed
}
