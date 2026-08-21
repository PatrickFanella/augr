package simulation

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestVenueRequiresSimulationPolicyDigestMatch(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	changedInput := PolicyInput{Schema: PolicySchemaV1, Assets: []AssetPolicy{{
		AssetClass:        instrument.AssetClassEquity,
		OrderTypes:        []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit},
		TimeInForce:       []lifecycle.TimeInForce{lifecycle.TimeInForceDay},
		QuoteRequirements: simulationFixtureQuoteRequirements(), MaxDepthParticipation: decimal.NewFromInt(1),
		FixedLatency: 101 * time.Millisecond, Calendar: CalendarPolicy{Kind: CalendarExplicitSessions, Sessions: []SessionWindow{{
			Label: "regular-2026-08-17", OpenAt: fixture.base, CloseAt: fixture.base.Add(6 * time.Hour),
		}}}, Fees: FeePolicy{Scale: 4},
	}}}
	changed, err := NewPolicy(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	venue, err := NewVenue(changed)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := fixture.snapshot("policy-mismatch", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	_, err = venue.Evaluate(EvaluationRequest{
		Account: fixture.account, Instrument: fixture.instrument, VenueContract: fixture.contract,
		Aggregate: fixture.aggregate, Snapshot: snapshot, EvaluatedAt: *snapshot.AvailableAt,
	})
	assertVenueErrorCode(t, err, VenueErrorPolicyMismatch)
}

func TestVenueFailsClosedOnMissingStaleOrFutureQuote(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	valid := fixture.snapshot("quote-validation", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	tests := map[string]func(*marketdata.QuoteSnapshot, *time.Time){
		"missing availability": func(snapshot *marketdata.QuoteSnapshot, _ *time.Time) { snapshot.AvailableAt = nil },
		"stale": func(snapshot *marketdata.QuoteSnapshot, evaluatedAt *time.Time) {
			exchangeAt := evaluatedAt.Add(-3 * time.Second)
			snapshot.ExchangeAt = &exchangeAt
		},
		"future": func(snapshot *marketdata.QuoteSnapshot, evaluatedAt *time.Time) {
			future := evaluatedAt.Add(time.Microsecond)
			snapshot.AvailableAt = &future
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := valid
			evaluatedAt := *valid.AvailableAt
			mutate(&snapshot, &evaluatedAt)
			_, err := fixture.evaluate(fixture.aggregate, snapshot, evaluatedAt)
			assertVenueErrorCode(t, err, VenueErrorQuoteNotExecutable)
		})
	}
}

func TestVenueFailsClosedOnMissingRequiredQuoteFacts(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	valid := fixture.snapshot("required-facts", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	tests := map[string]func(*marketdata.QuoteSnapshot){
		"source":         func(snapshot *marketdata.QuoteSnapshot) { snapshot.Source = "" },
		"bid":            func(snapshot *marketdata.QuoteSnapshot) { snapshot.Bid, snapshot.BidSize = nil, nil },
		"ask":            func(snapshot *marketdata.QuoteSnapshot) { snapshot.Ask, snapshot.AskSize = nil, nil },
		"bid depth":      func(snapshot *marketdata.QuoteSnapshot) { snapshot.Depth = snapshot.Depth[1:] },
		"ask depth":      func(snapshot *marketdata.QuoteSnapshot) { snapshot.Depth = snapshot.Depth[:1] },
		"market status":  func(snapshot *marketdata.QuoteSnapshot) { snapshot.MarketStatus = "" },
		"session status": func(snapshot *marketdata.QuoteSnapshot) { snapshot.SessionStatus = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := valid
			snapshot.Depth = append([]marketdata.DepthLevel(nil), valid.Depth...)
			mutate(&snapshot)
			_, err := fixture.evaluate(fixture.aggregate, snapshot, *valid.AvailableAt)
			assertVenueErrorCode(t, err, VenueErrorQuoteNotExecutable)
		})
	}
}

func TestVenueRejectsReferenceVenueCurrencyTickAndLotMismatch(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("reference-mismatch", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	tests := map[string]func(*EvaluationRequest){
		"account currency":  func(request *EvaluationRequest) { request.Account.BaseCurrency = "EUR" },
		"contract currency": func(request *EvaluationRequest) { request.VenueContract.Currency = "EUR" },
		"venue":             func(request *EvaluationRequest) { request.VenueContract.Venue = "other-venue" },
		"tick":              func(request *EvaluationRequest) { request.VenueContract.TickSize = decimal.RequireFromString("0.05") },
		"lot":               func(request *EvaluationRequest) { request.VenueContract.LotSize = decimal.NewFromInt(2) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			request := EvaluationRequest{
				Account: fixture.account, Instrument: fixture.instrument, VenueContract: fixture.contract,
				Aggregate: fixture.aggregate, Snapshot: snapshot, EvaluatedAt: *snapshot.AvailableAt,
			}
			mutate(&request)
			_, err := fixture.venue.Evaluate(request)
			assertVenueErrorCode(t, err, VenueErrorReferenceMismatch)
		})
	}
}

func TestVenueRejectsUnsupportedAssetOrderTypeAndTimeInForce(t *testing.T) {
	tests := map[string]func(*venueFixtureConfig){
		"asset": func(config *venueFixtureConfig) {
			config.policyAssetClass = instrument.AssetClassCryptoSpot
		},
		"order type": func(config *venueFixtureConfig) {
			config.policyOrderTypes = []lifecycle.OrderType{lifecycle.OrderLimit}
			config.orderType = lifecycle.OrderMarket
		},
		"time in force": func(config *venueFixtureConfig) {
			config.policyTimeInForce = []lifecycle.TimeInForce{lifecycle.TimeInForceGTC}
			config.timeInForce = lifecycle.TimeInForceDay
		},
	}
	for name, modify := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newVenueFixture(t, modify)
			snapshot := fixture.snapshot("unsupported", fixture.routeAt.Add(time.Second),
				[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
				[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
			)
			_, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
			assertVenueErrorCode(t, err, VenueErrorUnsupportedInstruction)
		})
	}
}

func TestVenueHonorsFixedLatencyWithoutEarlyFill(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("early", fixture.routeAt.Add(50*time.Millisecond),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionWaitingLatency || result.Aggregate.State != lifecycle.StateWorking || len(result.Transitions) != 1 || len(result.Fills) != 0 {
		t.Fatalf("early result = decision:%s state:%s transitions:%d fills:%d", result.Decision, result.Aggregate.State, len(result.Transitions), len(result.Fills))
	}
}

func TestFixedLatencyDoesNotBindImmediateOnlyInstructions(t *testing.T) {
	for _, timeInForce := range []lifecycle.TimeInForce{lifecycle.TimeInForceIOC, lifecycle.TimeInForceFOK} {
		t.Run(string(timeInForce), func(t *testing.T) {
			fixture := newVenueFixture(t, func(config *venueFixtureConfig) { config.timeInForce = timeInForce })
			snapshot := fixture.snapshot("early-"+string(timeInForce), fixture.routeAt.Add(50*time.Millisecond),
				[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
				[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
			)
			result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != DecisionWaitingLatency || result.Aggregate.State != lifecycle.StateRouted ||
				len(result.Transitions) != 0 || result.Aggregate.Binding != nil {
				t.Fatalf("early %s result = decision:%s state:%s transitions:%d binding:%v", timeInForce, result.Decision, result.Aggregate.State, len(result.Transitions), result.Aggregate.Binding)
			}
		})
	}
}

func TestVenueRejectsRouteOnExplicitCalendarHoliday(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.calendar.Sessions = []SessionWindow{{
			Label: "next-session", OpenAt: config.routeAt.Add(24 * time.Hour), CloseAt: config.routeAt.Add(30 * time.Hour),
		}}
	})
	snapshot := fixture.snapshot("holiday", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionRejected || result.Aggregate.State != lifecycle.StateRejected || len(result.Fills) != 0 {
		t.Fatalf("holiday result = decision:%s state:%s fills:%d", result.Decision, result.Aggregate.State, len(result.Fills))
	}
}

func TestVenueRejectsRouteAfterSessionClose(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.calendar.Sessions = []SessionWindow{{
			Label: "closed", OpenAt: config.routeAt.Add(-2 * time.Hour), CloseAt: config.routeAt,
		}}
	})
	snapshot := fixture.snapshot("after-close", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionRejected || result.Aggregate.State != lifecycle.StateRejected {
		t.Fatalf("after-close result = decision:%s state:%s", result.Decision, result.Aggregate.State)
	}
}

func TestContinuous24x7VenueHasNoImplicitClose(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.assetClass = instrument.AssetClassCryptoSpot
		config.timeInForce = lifecycle.TimeInForceGTC
		config.policyTimeInForce = []lifecycle.TimeInForce{lifecycle.TimeInForceGTC, lifecycle.TimeInForceIOC, lifecycle.TimeInForceFOK}
		config.calendar = CalendarPolicy{Kind: CalendarContinuous24x7}
	})
	availableAt := fixture.routeAt.Add(7 * 24 * time.Hour)
	snapshot := fixture.snapshot("continuous", availableAt,
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, availableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionFilled || result.Aggregate.State != lifecycle.StateFilled {
		t.Fatalf("continuous result = decision:%s state:%s", result.Decision, result.Aggregate.State)
	}
}

func TestGTCRestsUntilLaterConfiguredSession(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.timeInForce = lifecycle.TimeInForceGTC
		config.calendar.Sessions = append(config.calendar.Sessions, SessionWindow{
			Label: "regular-2026-08-18", OpenAt: config.routeAt.Add(24 * time.Hour), CloseAt: config.routeAt.Add(30 * time.Hour),
		})
	})
	betweenAt := fixture.routeAt.Add(12 * time.Hour)
	between := fixture.snapshot("between-sessions", betweenAt,
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	resting, err := fixture.evaluate(fixture.aggregate, between, betweenAt)
	if err != nil {
		t.Fatal(err)
	}
	if resting.Decision != DecisionResting || resting.Aggregate.State != lifecycle.StateWorking || len(resting.Fills) != 0 {
		t.Fatalf("between-session result = decision:%s state:%s fills:%d", resting.Decision, resting.Aggregate.State, len(resting.Fills))
	}
	laterAt := fixture.routeAt.Add(25 * time.Hour)
	later := fixture.snapshot("later-session", laterAt,
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	filled, err := fixture.evaluate(resting.Aggregate, later, laterAt)
	if err != nil {
		t.Fatal(err)
	}
	if filled.Decision != DecisionFilled || filled.Aggregate.State != lifecycle.StateFilled {
		t.Fatalf("later-session result = decision:%s state:%s", filled.Decision, filled.Aggregate.State)
	}
}

func TestMarketBuyConsumesAskDepthAsExactLevelFills(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("market-buy", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)},
			{Price: decimal.RequireFromString("10.30"), Size: decimal.NewFromInt(6)},
		},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	assertFillEffects(t, result, []string{"4@10.26", "6@10.3"}, lifecycle.StateFilled)
	if result.Fills[0].DepthSide != marketdata.DepthSideAsk || result.Fills[0].DepthLevel != 0 || result.Fills[1].DepthLevel != 1 {
		t.Fatalf("buy fill levels = %#v", result.Fills)
	}
}

func TestMarketSellConsumesBidDepthAsExactLevelFills(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) { config.side = lifecycle.SideSell })
	snapshot := fixture.snapshot("market-sell", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(4)},
			{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(6)},
		},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	assertFillEffects(t, result, []string{"4@10.24", "6@10.2"}, lifecycle.StateFilled)
	if result.Fills[0].DepthSide != marketdata.DepthSideBid {
		t.Fatalf("sell fill side = %s", result.Fills[0].DepthSide)
	}
}

func TestDepthParticipationRoundsDownToVenueLot(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.lotSize = decimal.NewFromInt(2)
		config.quantity = decimal.NewFromInt(4)
		config.maxDepthParticipation = decimal.RequireFromString("0.25")
	})
	snapshot := fixture.snapshot("participation", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(8)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	assertFillEffects(t, result, []string{"2@10.26"}, lifecycle.StatePartiallyFilled)
}

func TestLimitCrossingPreservesPriceImprovementAndLimit(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.orderType = lifecycle.OrderLimit
		config.limitPrice = decimalTestPointer("10.30")
	})
	snapshot := fixture.snapshot("limit-cross", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(4)},
			{Price: decimal.RequireFromString("10.30"), Size: decimal.NewFromInt(4)},
			{Price: decimal.RequireFromString("10.31"), Size: decimal.NewFromInt(10)},
		},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	assertFillEffects(t, result, []string{"4@10.22", "4@10.3"}, lifecycle.StatePartiallyFilled)
}

func TestNonMarketableLimitAcknowledgesWithoutInventingFill(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.orderType = lifecycle.OrderLimit
		config.limitPrice = decimalTestPointer("10.20")
	})
	snapshot := fixture.snapshot("limit-rest", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionResting || result.Aggregate.State != lifecycle.StateWorking || len(result.Fills) != 0 || result.Aggregate.Binding == nil {
		t.Fatalf("resting result = decision:%s state:%s fills:%d binding:%v", result.Decision, result.Aggregate.State, len(result.Fills), result.Aggregate.Binding)
	}
}

func TestFOKRejectsWithoutPartialEconomicEffect(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) { config.timeInForce = lifecycle.TimeInForceFOK })
	snapshot := fixture.snapshot("fok", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionRejected || result.Aggregate.State != lifecycle.StateRejected || len(result.Fills) != 0 || result.Aggregate.Binding != nil {
		t.Fatalf("FOK result = decision:%s state:%s fills:%d binding:%v", result.Decision, result.Aggregate.State, len(result.Fills), result.Aggregate.Binding)
	}
}

func TestFOKFillsAllWhenExactCapacityAvailable(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) { config.timeInForce = lifecycle.TimeInForceFOK })
	snapshot := fixture.snapshot("fok-full", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{
			{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)},
			{Price: decimal.RequireFromString("10.30"), Size: decimal.NewFromInt(6)},
		},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	assertFillEffects(t, result, []string{"4@10.26", "6@10.3"}, lifecycle.StateFilled)
}

func TestIOCPartiallyFillsThenCancelsRemainder(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) { config.timeInForce = lifecycle.TimeInForceIOC })
	snapshot := fixture.snapshot("ioc", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionCancelled || result.Aggregate.State != lifecycle.StateCancelled || len(result.Fills) != 1 || len(result.Transitions) != 2 {
		t.Fatalf("IOC result = decision:%s state:%s transitions:%d fills:%d", result.Decision, result.Aggregate.State, len(result.Transitions), len(result.Fills))
	}
}

func TestIOCNoLiquidityCancelsWithoutBinding(t *testing.T) {
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.orderType = lifecycle.OrderLimit
		config.timeInForce = lifecycle.TimeInForceIOC
		config.limitPrice = decimalTestPointer("10.20")
	})
	snapshot := fixture.snapshot("ioc-empty", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionCancelled || result.Aggregate.State != lifecycle.StateCancelled ||
		len(result.Fills) != 0 || len(result.Transitions) != 1 || result.Aggregate.Binding != nil {
		t.Fatalf("empty IOC result = decision:%s state:%s transitions:%d fills:%d binding:%v", result.Decision, result.Aggregate.State, len(result.Transitions), len(result.Fills), result.Aggregate.Binding)
	}
}

func TestDayOrderRemainsPartialForLaterSnapshot(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	first := fixture.snapshot("day-first", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)}},
	)
	firstResult, err := fixture.evaluate(fixture.aggregate, first, *first.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	assertFillEffects(t, firstResult, []string{"4@10.26"}, lifecycle.StatePartiallyFilled)
	second := fixture.snapshot("day-second", fixture.routeAt.Add(2*time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.28"), Size: decimal.NewFromInt(6)}},
	)
	secondResult, err := fixture.evaluate(firstResult.Aggregate, second, *second.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	assertFillEffects(t, secondResult, []string{"6@10.28"}, lifecycle.StateFilled)
}

func TestDayOrderExpiresAtNormalSessionClose(t *testing.T) {
	assertDayExpiry(t, 6*time.Hour)
}

func TestDayOrderExpiresAtExplicitHalfDayClose(t *testing.T) {
	assertDayExpiry(t, 3*time.Hour+30*time.Minute)
}

func TestLaterSnapshotCannotFillExpiredDayOrder(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	closeAt := fixture.base.Add(6 * time.Hour)
	snapshot := fixture.snapshot("expiry", closeAt,
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	expired, err := fixture.evaluate(fixture.aggregate, snapshot, closeAt)
	if err != nil {
		t.Fatal(err)
	}
	later := fixture.snapshot("after-expiry", closeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(expired.Aggregate, later, *later.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionNoop || len(result.Transitions) != 0 || len(result.Fills) != 0 || result.Aggregate.State != lifecycle.StateExpired {
		t.Fatalf("post-expiry result = decision:%s state:%s transitions:%d fills:%d", result.Decision, result.Aggregate.State, len(result.Transitions), len(result.Fills))
	}
}

func TestSameSnapshotLevelCannotFillTwice(t *testing.T) {
	fixture := newVenueFixture(t, nil)
	snapshot := fixture.snapshot("duplicate-level", fixture.routeAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(4)}},
	)
	first, err := fixture.evaluate(fixture.aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.evaluate(first.Aggregate, snapshot, *snapshot.AvailableAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Fills) != 1 || len(second.Fills) != 0 || len(second.Transitions) != 0 || second.Aggregate.State != lifecycle.StatePartiallyFilled {
		t.Fatalf("duplicate replay = first:%d second:%d transitions:%d state:%s", len(first.Fills), len(second.Fills), len(second.Transitions), second.Aggregate.State)
	}
	if first.Fills[0].SourceEventID == "" || first.Fills[0].SourceEventID == uuid.Nil.String() {
		t.Fatalf("first fill source identity = %q", first.Fills[0].SourceEventID)
	}
}

func assertDayExpiry(t *testing.T, closeOffset time.Duration) {
	t.Helper()
	fixture := newVenueFixture(t, func(config *venueFixtureConfig) {
		config.calendar.Sessions[0].CloseAt = time.Date(2026, 8, 17, 12, 0, 0, 123456000, time.UTC).Add(closeOffset)
	})
	closeAt := fixture.base.Add(closeOffset)
	snapshot := fixture.snapshot("expiry", closeAt,
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.24"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.26"), Size: decimal.NewFromInt(10)}},
	)
	result, err := fixture.evaluate(fixture.aggregate, snapshot, closeAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionExpired || result.Aggregate.State != lifecycle.StateExpired || len(result.Transitions) != 1 || len(result.Fills) != 0 {
		t.Fatalf("expiry result = decision:%s state:%s transitions:%d fills:%d", result.Decision, result.Aggregate.State, len(result.Transitions), len(result.Fills))
	}
	if !result.Transitions[0].Event.SourceAt.Equal(closeAt) {
		t.Fatalf("expiry source time = %s, want %s", result.Transitions[0].Event.SourceAt, closeAt)
	}
}

func assertFillEffects(t *testing.T, result *Result, want []string, state lifecycle.State) {
	t.Helper()
	if result.Aggregate.State != state || len(result.Fills) != len(want) {
		t.Fatalf("fill result = state:%s fills:%d, want %s/%d", result.Aggregate.State, len(result.Fills), state, len(want))
	}
	for index, expected := range want {
		got := result.Fills[index].Quantity.String() + "@" + result.Fills[index].Price.String()
		if got != expected {
			t.Fatalf("fill %d = %s, want %s", index, got, expected)
		}
	}
}

func assertVenueErrorCode(t *testing.T, err error, code VenueErrorCode) {
	t.Helper()
	var venueError *VenueError
	if !errors.As(err, &venueError) || venueError.Code != code {
		t.Fatalf("venue error = %v, want code %s", err, code)
	}
}
