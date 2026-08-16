package simulation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

func TestGoldenReplayBacktestAndPaperProduceIdenticalOutcomesWithinMode(t *testing.T) {
	outcomes := make(map[domain.AccountEnvironment]*simulation.Outcome)
	for _, environment := range []domain.AccountEnvironment{
		domain.AccountEnvironmentPaperScored,
		domain.AccountEnvironmentPaperStress,
	} {
		t.Run(string(environment), func(t *testing.T) {
			fixture := newGoldenFixture(t, goldenFixtureOptions{environment: environment})
			backtestOutcome, paperOutcome := runGoldenReplay(t, fixture)
			if backtestOutcome.Hash() != paperOutcome.Hash() ||
				!bytes.Equal(backtestOutcome.CanonicalBytes(), paperOutcome.CanonicalBytes()) {
				t.Fatalf("backtest/paper outcome mismatch = %s/%s", backtestOutcome.Hash(), paperOutcome.Hash())
			}
			if backtestOutcome.PolicyVersion != fixture.policy.Version() ||
				backtestOutcome.FinalState != lifecycle.StateFilled ||
				backtestOutcome.Environment != environment ||
				backtestOutcome.EvidenceClass != fixture.account.EvidenceClass ||
				backtestOutcome.StorageNamespace != fixture.account.StorageNamespace ||
				!backtestOutcome.TotalQuantity.Equal(decimal.NewFromInt(10)) ||
				len(backtestOutcome.Fills) != 2 {
				t.Fatalf("golden outcome = %+v", backtestOutcome)
			}
			outcomes[environment] = backtestOutcome
		})
	}

	scored := outcomes[domain.AccountEnvironmentPaperScored]
	stress := outcomes[domain.AccountEnvironmentPaperStress]
	if scored.Hash() == stress.Hash() || bytes.Equal(scored.CanonicalBytes(), stress.CanonicalBytes()) ||
		scored.EvidenceClass == stress.EvidenceClass || scored.StorageNamespace == stress.StorageNamespace {
		t.Fatalf("scored/stress populations converged = %s/%s", scored.Hash(), stress.Hash())
	}
}

func TestGoldenReplayNegativePathParityHasNoEconomicEffect(t *testing.T) {
	tests := []struct {
		name       string
		options    goldenFixtureOptions
		snapshot   func(*testing.T, goldenFixture) marketdata.QuoteSnapshot
		wantCode   simulation.VenueErrorCode
		wantResult simulation.Decision
	}{
		{
			name: "stale data",
			snapshot: func(t *testing.T, fixture goldenFixture) marketdata.QuoteSnapshot {
				return fixture.snapshot(t, "stale", fixture.routeAt.Add(time.Second), fixture.routeAt.Add(-3*time.Second),
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(10)}})
			},
			wantCode: simulation.VenueErrorQuoteNotExecutable,
		},
		{
			name:    "insufficient FOK depth",
			options: goldenFixtureOptions{timeInForce: lifecycle.TimeInForceFOK},
			snapshot: func(t *testing.T, fixture goldenFixture) marketdata.QuoteSnapshot {
				return fixture.snapshot(t, "fok", fixture.routeAt.Add(time.Second), fixture.routeAt.Add(time.Second-50*time.Millisecond),
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(3)}})
			},
			wantResult: simulation.DecisionRejected,
		},
		{
			name: "nonmarketable GTC limit",
			options: goldenFixtureOptions{
				orderType: lifecycle.OrderLimit, timeInForce: lifecycle.TimeInForceGTC,
				limitPrice: goldenDecimalPointer("10.20"),
			},
			snapshot: func(t *testing.T, fixture goldenFixture) marketdata.QuoteSnapshot {
				return fixture.snapshot(t, "resting", fixture.routeAt.Add(time.Second), fixture.routeAt.Add(time.Second-50*time.Millisecond),
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(10)}})
			},
			wantResult: simulation.DecisionResting,
		},
		{
			name: "unsupported stop order",
			options: goldenFixtureOptions{
				orderType: lifecycle.OrderStop, stopPrice: goldenDecimalPointer("10.25"),
			},
			snapshot: func(t *testing.T, fixture goldenFixture) marketdata.QuoteSnapshot {
				return fixture.snapshot(t, "stop", fixture.routeAt.Add(time.Second), fixture.routeAt.Add(time.Second-50*time.Millisecond),
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(10)}})
			},
			wantCode: simulation.VenueErrorUnsupportedInstruction,
		},
		{
			name:    "missing asset policy",
			options: goldenFixtureOptions{policyAssetClass: instrument.AssetClassCryptoSpot},
			snapshot: func(t *testing.T, fixture goldenFixture) marketdata.QuoteSnapshot {
				return fixture.snapshot(t, "missing-policy", fixture.routeAt.Add(time.Second), fixture.routeAt.Add(time.Second-50*time.Millisecond),
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
					[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(10)}})
			},
			wantCode: simulation.VenueErrorUnsupportedInstruction,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newGoldenFixture(t, test.options)
			snapshot := test.snapshot(t, fixture)
			request := fixture.request(cloneGoldenAggregate(t, fixture.routed), snapshot)
			backtestAdapter, err := backtest.NewCommonSimulation(fixture.policy)
			if err != nil {
				t.Fatal(err)
			}
			paperAdapter, err := paper.NewCommonSimulation(fixture.policy)
			if err != nil {
				t.Fatal(err)
			}
			backtestResult, backtestErr := backtestAdapter.Evaluate(request)
			request.Aggregate = cloneGoldenAggregate(t, fixture.routed)
			paperResult, paperErr := paperAdapter.Evaluate(request)
			assertGoldenEvaluationParity(t, backtestResult, backtestErr, paperResult, paperErr)

			if test.wantCode != "" {
				var venueError *simulation.VenueError
				if !errors.As(backtestErr, &venueError) || venueError.Code != test.wantCode || backtestResult != nil {
					t.Fatalf("negative error/result = %#v/%#v", venueError, backtestResult)
				}
				return
			}
			if backtestErr != nil || backtestResult == nil || backtestResult.Decision != test.wantResult {
				t.Fatalf("negative result = %#v, error = %v", backtestResult, backtestErr)
			}
			assertGoldenNoEconomicEffect(t, backtestResult)
		})
	}
}

func TestGoldenReplayConcurrentAdapterOrderingIsStable(t *testing.T) {
	fixture := newGoldenFixture(t, goldenFixtureOptions{})
	snapshot := fixture.snapshot(t, "concurrent", fixture.routeAt.Add(time.Second), fixture.routeAt.Add(time.Second-50*time.Millisecond),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(10)}})
	backtestAdapter, err := backtest.NewCommonSimulation(fixture.policy)
	if err != nil {
		t.Fatal(err)
	}
	paperAdapter, err := paper.NewCommonSimulation(fixture.policy)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := backtestAdapter.Evaluate(fixture.request(cloneGoldenAggregate(t, fixture.routed), snapshot))
	if err != nil {
		t.Fatal(err)
	}

	const workers = 32
	start := make(chan struct{})
	results := make(chan error, workers)
	requests := make([]simulation.EvaluationRequest, workers)
	for worker := range workers {
		requests[worker] = fixture.request(cloneGoldenAggregate(t, fixture.routed), snapshot)
	}
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(request simulation.EvaluationRequest, usePaper bool) {
			defer wait.Done()
			<-start
			var result *simulation.Result
			var evaluateErr error
			if usePaper {
				result, evaluateErr = paperAdapter.Evaluate(request)
			} else {
				result, evaluateErr = backtestAdapter.Evaluate(request)
			}
			if evaluateErr != nil {
				results <- evaluateErr
				return
			}
			if !goldenResultsSame(result, baseline) {
				results <- fmt.Errorf("concurrent adapter result changed")
			}
		}(requests[worker], worker%2 == 0)
	}
	close(start)
	wait.Wait()
	close(results)
	for resultErr := range results {
		t.Error(resultErr)
	}
}

type goldenFixtureOptions struct {
	environment      domain.AccountEnvironment
	orderType        lifecycle.OrderType
	timeInForce      lifecycle.TimeInForce
	limitPrice       *decimal.Decimal
	stopPrice        *decimal.Decimal
	policyAssetClass instrument.AssetClass
}

type goldenFixture struct {
	account    domain.Account
	instrument instrument.Instrument
	contract   instrument.VenueContract
	policy     *simulation.Policy
	routed     *lifecycle.Aggregate
	routeAt    time.Time
}

func newGoldenFixture(t *testing.T, options goldenFixtureOptions) goldenFixture {
	t.Helper()
	base := time.Date(2026, 8, 17, 12, 0, 0, 123456000, time.UTC)
	routeAt := base.Add(time.Hour)
	if options.environment == "" {
		options.environment = domain.AccountEnvironmentPaperScored
	}
	if options.orderType == "" {
		options.orderType = lifecycle.OrderMarket
	}
	if options.timeInForce == "" {
		options.timeInForce = lifecycle.TimeInForceDay
	}
	namespace := string(options.environment) + "/golden-replay"
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "golden replay", Environment: options.environment, Venue: "internal",
		BaseCurrency: "USD", StorageNamespace: namespace, StartingCapital: decimal.NewFromInt(100000),
		BuyingPowerMultiplier: decimal.NewFromInt(1), MarginProfile: domain.MarginProfileCash,
		CreatedBy: "golden-replay", CreationMetadata: json.RawMessage(`{"fixture":"golden-replay"}`),
		CreatedAt: base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "golden:equity", AssetClass: instrument.AssetClassEquity,
		PrimaryVenue: "golden-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive,
		Metadata: json.RawMessage(`{"fixture":"golden-replay"}`), CreatedAt: base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	validTo := base.Add(30 * 24 * time.Hour)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID: reference.ID, Venue: "golden-venue", ContractID: "GOLDEN-EQUITY",
		Currency: "USD", TickSize: reference.TickSize, LotSize: reference.LotSize,
		Multiplier: reference.Multiplier, SettlementMethod: reference.SettlementMethod,
		ValidFrom: base.Add(-24 * time.Hour), ValidTo: &validTo,
		Metadata: json.RawMessage(`{"fixture":"golden-replay"}`), CreatedAt: base.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	policyClass := options.policyAssetClass
	if policyClass == "" {
		policyClass = instrument.AssetClassEquity
	}
	requirements := goldenQuoteRequirements()
	policy, err := simulation.NewPolicy(simulation.PolicyInput{
		Schema: simulation.PolicySchemaV1,
		Assets: []simulation.AssetPolicy{{
			AssetClass: policyClass,
			OrderTypes: []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit},
			TimeInForce: []lifecycle.TimeInForce{
				lifecycle.TimeInForceDay, lifecycle.TimeInForceGTC,
				lifecycle.TimeInForceIOC, lifecycle.TimeInForceFOK,
			},
			QuoteRequirements: requirements, MaxDepthParticipation: decimal.NewFromInt(1),
			FixedLatency: 100 * time.Millisecond,
			Calendar: simulation.CalendarPolicy{
				Kind:     simulation.CalendarExplicitSessions,
				Sessions: []simulation.SessionWindow{{Label: "golden-session", OpenAt: base, CloseAt: base.Add(6 * time.Hour)}},
			},
			Fees: simulation.FeePolicy{
				PerOrder: decimal.RequireFromString("1.25"), PerUnit: decimal.RequireFromString("0.01"),
				NotionalBPS: decimal.RequireFromString("2"), Scale: 4,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := goldenFixture{account: *account, instrument: *reference, contract: *contract, policy: policy, routeAt: routeAt}
	routeSnapshot := fixture.snapshot(t, "route", routeAt.Add(-100*time.Millisecond), routeAt.Add(-200*time.Millisecond),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(20)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(20)}})
	proposed, err := lifecycle.Propose(lifecycle.ProposeInput{
		Account: *account, Instrument: *reference, DecisionSnapshot: routeSnapshot,
		IdempotencyKey: "golden-intent", DesiredQuantityDelta: decimal.NewFromInt(10), DecisionAt: routeAt,
		OriginType: ledger.ExecutionOriginStrategyVersion, OriginID: "golden-strategy-v1",
		StrategyVersionID: "golden-strategy-v1", Metadata: json.RawMessage(`{"signal":"golden"}`),
		Event: goldenEvent("proposal", routeAt.Add(-time.Millisecond), routeAt), CreatedAt: routeAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := lifecycle.Allocate(proposed, decimal.NewFromInt(10), goldenEvent("allocation", routeAt, routeAt), routeAt)
	if err != nil {
		t.Fatal(err)
	}
	allocated, err := lifecycle.ApplyTransition(proposed, allocation)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := lifecycle.ApproveRisk(allocated, goldenEvent("risk", routeAt, routeAt), routeAt)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := lifecycle.ApplyTransition(allocated, approval)
	if err != nil {
		t.Fatal(err)
	}
	route, err := lifecycle.Route(approved, lifecycle.RouteInput{
		OrderIdempotencyKey: "golden-order", Instrument: *reference, VenueContract: *contract,
		RouteSnapshot: routeSnapshot, QuoteRequirements: requirements, OrderType: options.orderType,
		TimeInForce: options.timeInForce, LimitPrice: options.limitPrice, StopPrice: options.stopPrice,
		PolicyKind: lifecycle.PolicySimulation, PolicyVersion: policy.Version(),
		Event: goldenEvent("route", routeAt, routeAt), RoutedAt: routeAt, CreatedAt: routeAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.routed, err = lifecycle.ApplyTransition(approved, route)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func runGoldenReplay(t *testing.T, fixture goldenFixture) (*simulation.Outcome, *simulation.Outcome) {
	t.Helper()
	backtestAdapter, err := backtest.NewCommonSimulation(fixture.policy)
	if err != nil {
		t.Fatal(err)
	}
	paperAdapter, err := paper.NewCommonSimulation(fixture.policy)
	if err != nil {
		t.Fatal(err)
	}
	if backtestAdapter.PolicyVersion() != paperAdapter.PolicyVersion() ||
		backtestAdapter.PolicyDigest() != paperAdapter.PolicyDigest() ||
		backtestAdapter.PolicyVersion() != fixture.policy.Version() {
		t.Fatalf("adapter policies differ = %q/%q", backtestAdapter.PolicyVersion(), paperAdapter.PolicyVersion())
	}

	first := fixture.snapshot(t, "first-depth", fixture.routeAt.Add(time.Second), fixture.routeAt.Add(time.Second-50*time.Millisecond),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(4)}})
	backtestFirst, err := backtestAdapter.Evaluate(fixture.request(cloneGoldenAggregate(t, fixture.routed), first))
	if err != nil {
		t.Fatal(err)
	}
	paperFirst, err := paperAdapter.Evaluate(fixture.request(cloneGoldenAggregate(t, fixture.routed), first))
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenEvaluationParity(t, backtestFirst, nil, paperFirst, nil)
	if backtestFirst.Decision != simulation.DecisionPartiallyFilled || len(backtestFirst.Fills) != 1 {
		t.Fatalf("first golden observation = %+v", backtestFirst)
	}

	second := fixture.snapshot(t, "final-depth", fixture.routeAt.Add(2*time.Second), fixture.routeAt.Add(2*time.Second-50*time.Millisecond),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(10)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.22"), Size: decimal.NewFromInt(6)}})
	backtestFinal, err := backtestAdapter.Evaluate(fixture.request(cloneGoldenAggregate(t, backtestFirst.Aggregate), second))
	if err != nil {
		t.Fatal(err)
	}
	paperFinal, err := paperAdapter.Evaluate(fixture.request(cloneGoldenAggregate(t, paperFirst.Aggregate), second))
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenEvaluationParity(t, backtestFinal, nil, paperFinal, nil)
	if backtestFinal.Decision != simulation.DecisionFilled || len(backtestFinal.Fills) != 1 {
		t.Fatalf("final golden observation = %+v", backtestFinal)
	}
	if !lifecycle.SameOrderPayload(backtestFinal.Aggregate.Order, paperFinal.Aggregate.Order) ||
		!bytes.Equal(goldenJSON(t, backtestFinal.Aggregate.Intent), goldenJSON(t, paperFinal.Aggregate.Intent)) {
		t.Fatal("normalized intent/order semantics differ between paths")
	}

	backtestEffects := append(append([]simulation.FillEffect(nil), backtestFirst.Fills...), backtestFinal.Fills...)
	paperEffects := append(append([]simulation.FillEffect(nil), paperFirst.Fills...), paperFinal.Fills...)
	assertGoldenClassification(t, fixture.account, backtestEffects)
	assertGoldenClassification(t, fixture.account, paperEffects)
	backtestOutcome, err := simulation.NewOutcome(simulation.OutcomeInput{
		Account: fixture.account, VenueContract: fixture.contract, Aggregate: backtestFinal.Aggregate, Fills: backtestEffects,
	})
	if err != nil {
		t.Fatal(err)
	}
	paperOutcome, err := simulation.NewOutcome(simulation.OutcomeInput{
		Account: fixture.account, VenueContract: fixture.contract, Aggregate: paperFinal.Aggregate, Fills: paperEffects,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := range backtestOutcome.Fills {
		left, right := backtestOutcome.Fills[index], paperOutcome.Fills[index]
		if !left.Quantity.Equal(right.Quantity) || !left.Price.Equal(right.Price) || !left.Fee.Equal(right.Fee) {
			t.Fatalf("fill %d differs = %+v/%+v", index, left, right)
		}
	}
	if !backtestOutcome.TotalQuantity.Equal(paperOutcome.TotalQuantity) ||
		!backtestOutcome.GrossCash.Equal(paperOutcome.GrossCash) ||
		!backtestOutcome.TotalFee.Equal(paperOutcome.TotalFee) {
		t.Fatalf("outcome totals differ = %+v/%+v", backtestOutcome, paperOutcome)
	}
	return backtestOutcome, paperOutcome
}

func assertGoldenClassification(t *testing.T, account domain.Account, effects []simulation.FillEffect) {
	t.Helper()
	for index, effect := range effects {
		var evidence struct {
			Environment      string `json:"environment"`
			EvidenceClass    string `json:"evidence_class"`
			StorageNamespace string `json:"storage_namespace"`
		}
		if err := json.Unmarshal(effect.Evidence, &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence.Environment != string(account.Environment) ||
			evidence.EvidenceClass != account.EvidenceClass ||
			evidence.StorageNamespace != account.StorageNamespace {
			t.Fatalf("fill %d classification = %+v, account = %+v", index, evidence, account)
		}
		if account.Environment == domain.AccountEnvironmentPaperStress &&
			evidence.EvidenceClass == domain.PaperEvidenceClassPromotion {
			t.Fatalf("stress fill %d reported promotion evidence", index)
		}
	}
}

func (fixture goldenFixture) request(aggregate *lifecycle.Aggregate, snapshot marketdata.QuoteSnapshot) simulation.EvaluationRequest {
	return simulation.EvaluationRequest{
		Account: fixture.account, Instrument: fixture.instrument, VenueContract: fixture.contract,
		Aggregate: aggregate, Snapshot: snapshot, EvaluatedAt: *snapshot.AvailableAt,
	}
}

func (fixture goldenFixture) snapshot(
	t *testing.T,
	label string,
	availableAt, exchangeAt time.Time,
	bids, asks []marketdata.DepthLevelInput,
) marketdata.QuoteSnapshot {
	t.Helper()
	receivedAt := availableAt.Add(-25 * time.Millisecond)
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
		InstrumentID: fixture.instrument.ID, VenueContractID: &fixture.contract.ID,
		Provider: "golden-replay", Venue: fixture.contract.Venue, Source: "golden-feed",
		ObservationNamespace: "quotes/golden-replay", ObservationID: label, SourceRevision: "v1",
		ExchangeAt: &exchangeAt, ReceivedAt: receivedAt, AvailableAt: &availableAt,
		Bid: bid, BidSize: bidSize, Ask: ask, AskSize: askSize,
		MarketStatus: "open", SessionStatus: "regular", Bids: bids, Asks: asks,
		Metadata: json.RawMessage(`{"fixture":"golden-replay"}`), CreatedAt: availableAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return *snapshot
}

func assertGoldenEvaluationParity(
	t *testing.T,
	backtestResult *simulation.Result,
	backtestErr error,
	paperResult *simulation.Result,
	paperErr error,
) {
	t.Helper()
	if (backtestErr == nil) != (paperErr == nil) {
		t.Fatalf("adapter errors differ = %v/%v", backtestErr, paperErr)
	}
	if backtestErr != nil {
		var backtestVenueError, paperVenueError *simulation.VenueError
		if !errors.As(backtestErr, &backtestVenueError) || !errors.As(paperErr, &paperVenueError) ||
			backtestVenueError.Code != paperVenueError.Code || backtestErr.Error() != paperErr.Error() {
			t.Fatalf("adapter typed errors differ = %#v/%#v", backtestVenueError, paperVenueError)
		}
		return
	}
	if !goldenResultsSame(backtestResult, paperResult) {
		t.Fatal("adapter results, transitions, or evidence differ")
	}
}

func goldenResultsSame(left, right *simulation.Result) bool {
	if left == nil || right == nil || left.Decision != right.Decision ||
		!goldenAggregatesSame(left.Aggregate, right.Aggregate) ||
		len(left.Transitions) != len(right.Transitions) || len(left.Fills) != len(right.Fills) {
		return false
	}
	for index := range left.Transitions {
		if !goldenTransitionsSame(left.Transitions[index], right.Transitions[index]) {
			return false
		}
	}
	for index := range left.Fills {
		if !goldenFillEffectsSame(left.Fills[index], right.Fills[index]) {
			return false
		}
	}
	return true
}

func goldenAggregatesSame(left, right *lifecycle.Aggregate) bool {
	if left == nil || right == nil || !lifecycle.SameIntentPayload(&left.Intent, &right.Intent) ||
		left.State != right.State || !goldenDecimalPointersEqual(left.AllocatedQuantity, right.AllocatedQuantity) ||
		!goldenOrdersSame(left.Order, right.Order) || !goldenBindingsSame(left.Binding, right.Binding) ||
		len(left.Fills) != len(right.Fills) || len(left.Events) != len(right.Events) {
		return false
	}
	for index := range left.Fills {
		if !lifecycle.SameFillPayload(&left.Fills[index], &right.Fills[index]) {
			return false
		}
	}
	for index := range left.Events {
		if !lifecycle.SameEventPayload(&left.Events[index], &right.Events[index]) {
			return false
		}
	}
	return true
}

func goldenTransitionsSame(left, right *lifecycle.Transition) bool {
	if left == nil || right == nil || !lifecycle.SameEventPayload(&left.Event, &right.Event) ||
		!goldenOrdersSame(left.Order, right.Order) || !goldenBindingsSame(left.Binding, right.Binding) ||
		!goldenLifecycleFillsSame(left.Fill, right.Fill) {
		return false
	}
	if left.Normalization == nil || right.Normalization == nil {
		return left.Normalization == nil && right.Normalization == nil
	}
	return ledger.SameEconomicNormalizationPayload(left.Normalization, right.Normalization)
}

func goldenFillEffectsSame(left, right simulation.FillEffect) bool {
	return lifecycle.SameFillPayload(&left.Fill, &right.Fill) && left.DepthSide == right.DepthSide &&
		left.DepthLevel == right.DepthLevel && left.DisplayedSize.Equal(right.DisplayedSize) &&
		left.Capacity.Equal(right.Capacity) && left.Quantity.Equal(right.Quantity) &&
		left.Price.Equal(right.Price) && goldenDecimalPointersEqual(left.Fee, right.Fee) &&
		left.SourceNamespace == right.SourceNamespace && left.SourceEventID == right.SourceEventID &&
		bytes.Equal(left.Evidence, right.Evidence) && left.PolicyVersion == right.PolicyVersion &&
		left.NormalizerVersion == right.NormalizerVersion
}

func goldenOrdersSame(left, right *lifecycle.Order) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return lifecycle.SameOrderPayload(left, right)
}

func goldenBindingsSame(left, right *lifecycle.OrderBinding) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.ID == right.ID && left.OrderID == right.OrderID && left.AccountID == right.AccountID &&
		left.Venue == right.Venue && left.ExternalOrderID == right.ExternalOrderID
}

func goldenLifecycleFillsSame(left, right *lifecycle.Fill) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return lifecycle.SameFillPayload(left, right)
}

func goldenDecimalPointersEqual(left, right *decimal.Decimal) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func assertGoldenNoEconomicEffect(t *testing.T, result *simulation.Result) {
	t.Helper()
	if len(result.Fills) != 0 {
		t.Fatalf("negative result has %d fill effects", len(result.Fills))
	}
	for _, transition := range result.Transitions {
		if transition.Fill != nil || transition.Normalization != nil {
			t.Fatalf("negative transition has an economic effect: %#v", transition)
		}
	}
}

func cloneGoldenAggregate(t *testing.T, aggregate *lifecycle.Aggregate) *lifecycle.Aggregate {
	t.Helper()
	encoded := goldenJSON(t, aggregate)
	var cloned lifecycle.Aggregate
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatal(err)
	}
	return &cloned
}

func goldenJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func goldenQuoteRequirements() marketdata.QuoteRequirements {
	return marketdata.QuoteRequirements{
		RequireSource: true, RequireVenueContract: true, RequireBid: true, RequireAsk: true,
		RequireBidDepth: true, RequireAskDepth: true, RequireMarketStatus: true,
		RequireSessionStatus: true, AllowedMarketStatuses: []string{"open"},
		AllowedSessionStatuses: []string{"regular"}, MaxAge: 2 * time.Second,
	}
}

func goldenEvent(label string, sourceAt, receivedAt time.Time) lifecycle.EventInput {
	return lifecycle.EventInput{
		Source: "golden-replay", SourceNamespace: "simulation/golden-replay", SourceEventID: label,
		SourceAt: sourceAt, ReceivedAt: receivedAt, Actor: "golden-replay",
		ReasonCode: label, Evidence: json.RawMessage(`{"fixture":"golden-replay"}`),
	}
}

func goldenDecimalPointer(value string) *decimal.Decimal {
	parsed := decimal.RequireFromString(value)
	return &parsed
}
