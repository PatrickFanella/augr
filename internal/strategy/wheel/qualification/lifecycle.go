package qualification

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/wheel"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type LifecycleFixture struct {
	Policy         *wheel.Policy
	Manifest       *dataset.Manifest
	Underlying     *instrument.Instrument
	Put            *instrument.Instrument
	Call           *instrument.Instrument
	PutContract    *instrument.VenueContract
	CallContract   *instrument.VenueContract
	Scenarios      map[string]*wheel.Scenario
	Reports        map[string]*wheel.Report
	CreatedAt      time.Time
	PutContent     []byte
	CallContent    []byte
	PutContentSHA  string
	CallContentSHA string
}

func BuildLifecycleScenarios() (*LifecycleFixture, error) {
	start := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	putAt, putExpiry := start.Add(24*time.Hour), start.Add(31*24*time.Hour)
	dividendAt, callAt, callExpiry := start.Add(32*24*time.Hour), start.Add(33*24*time.Hour), start.Add(63*24*time.Hour)
	underlying, err := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: "figi:WHEEL-LIFECYCLE", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive, Metadata: json.RawMessage(`{"fixture":"wheel-lifecycle"}`), CreatedAt: start.Add(-time.Hour)})
	if err != nil {
		return nil, err
	}
	underlying.ID = lifecycleID("underlying")
	put, err := lifecycleOption("put", "95", putExpiry, underlying.ID, start)
	if err != nil {
		return nil, err
	}
	call, err := lifecycleOption("call", "100", callExpiry, underlying.ID, start)
	if err != nil {
		return nil, err
	}
	putContract, err := lifecycleContract(put, "WHEEL-LIFECYCLE-PUT-95", start, putExpiry)
	if err != nil {
		return nil, err
	}
	callContract, err := lifecycleContract(call, "WHEEL-LIFECYCLE-CALL-100", start, callExpiry)
	if err != nil {
		return nil, err
	}
	putContent, _ := json.Marshal(map[string]any{"instrument_id": put.ID, "venue_contract_id": putContract.ID, "bid": "2", "ask": "2.1", "delta": "-0.25", "available_at": putAt})
	callContent, _ := json.Marshal(map[string]any{"instrument_id": call.ID, "venue_contract_id": callContract.ID, "bid": "1.5", "ask": "1.6", "delta": "0.2", "available_at": callAt})
	putSHA, callSHA := hash(putContent), hash(callContent)
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: callExpiry, Partitions: []dataset.PartitionInput{{Kind: dataset.KindQuotes, Provider: "fixture", Source: "fixture-feed", Namespace: "wheel/lifecycle/options", RequestSHA256: strings.Repeat("1", 64), MediaType: "application/json", SymbologyVersion: "osi-v1", AdjustmentPolicy: "not_applicable", Timezone: "UTC", Calendar: "24x7", Revision: "r1", License: "test-only", RetentionPolicy: "retain-ovr402", Observations: []dataset.ObservationInput{{SourceKey: "wheel-lifecycle-put", InstrumentID: put.ID, EffectiveAt: putAt, ObservedAt: putAt, AvailableAt: putAt, Revision: "r1", ContentSHA256: putSHA, Bid: text("2"), Ask: text("2.1")}, {SourceKey: "wheel-lifecycle-call", InstrumentID: call.ID, EffectiveAt: callAt, ObservedAt: callAt, AvailableAt: callAt, Revision: "r1", ContentSHA256: callSHA, Bid: text("1.5"), Ask: text("1.6")}}}}})
	if err != nil {
		return nil, err
	}
	partition := manifest.Partitions()[0]
	policy, err := wheel.NewPolicy(wheel.PolicyInput{Version: "wheel-v1-retained", MinimumROIC: "0.1", MaximumDebtToAssets: "0.6", RequirePositiveFreeCash: true, MaximumQualityAgeSeconds: 48 * 3600, MaximumMarketDataAgeSeconds: 60, PutDeltaMinimum: "0.15", PutDeltaTarget: "0.25", PutDeltaMaximum: "0.35", CallDeltaMinimum: "0.15", CallDeltaTarget: "0.2", CallDeltaMaximum: "0.35", MinimumDTE: 30, MaximumDTE: 31, MinimumOpenInterest: "100", MinimumVolume: "10", MaximumSpreadRatio: "0.1", DeliverableQuantity: "100", MaximumContracts: 1, FeePerContract: "1", FeePerShare: "0.01", DecimalScale: 12})
	if err != nil {
		return nil, err
	}
	quality := func() wheel.EventInput {
		return wheel.EventInput{Kind: wheel.EventAssessQuality, OccurredAt: start, UnderlyingMark: "100", Quality: &wheel.QualityEvidence{AvailableAt: start, ROIC: "0.2", DebtToAssets: "0.3", FreeCashFlow: "1000", EvidenceID: lifecycleID("quality-value"), EvidenceSHA256: strings.Repeat("2", 64)}, EvidenceID: lifecycleID("quality-event"), EvidenceSHA256: strings.Repeat("3", 64)}
	}
	openPut := func() wheel.EventInput {
		return wheel.EventInput{Kind: wheel.EventOpenPut, OccurredAt: putAt, UnderlyingMark: "100", Candidates: []wheel.Candidate{{InstrumentID: put.ID, VenueContractID: putContract.ID, PartitionContentSHA256: partition.ContentSHA256, SourceKey: "wheel-lifecycle-put", OptionType: "put", Strike: "95", Expiry: putExpiry, Delta: "-0.25", Bid: "2", Ask: "2.1", OpenInterest: "1000", Volume: "100", AvailableAt: putAt, EvidenceID: lifecycleID("put-quote"), EvidenceSHA256: putSHA}}, EvidenceID: lifecycleID("put-open"), EvidenceSHA256: strings.Repeat("4", 64)}
	}
	putSettlement := func(mark string) wheel.EventInput {
		return wheel.EventInput{Kind: wheel.EventExpiry, OccurredAt: putExpiry, UnderlyingMark: mark, EvidenceID: lifecycleID("put-expiry-" + mark), EvidenceSHA256: strings.Repeat("5", 64)}
	}
	dividend := wheel.EventInput{Kind: wheel.EventDividend, OccurredAt: dividendAt, UnderlyingMark: "92", DividendPerShare: text("1"), EvidenceID: lifecycleID("dividend"), EvidenceSHA256: strings.Repeat("6", 64)}
	openCall := wheel.EventInput{Kind: wheel.EventOpenCall, OccurredAt: callAt, UnderlyingMark: "95", Candidates: []wheel.Candidate{{InstrumentID: call.ID, VenueContractID: callContract.ID, PartitionContentSHA256: partition.ContentSHA256, SourceKey: "wheel-lifecycle-call", OptionType: "call", Strike: "100", Expiry: callExpiry, Delta: "0.2", Bid: "1.5", Ask: "1.6", OpenInterest: "1000", Volume: "100", AvailableAt: callAt, EvidenceID: lifecycleID("call-quote"), EvidenceSHA256: callSHA}}, EvidenceID: lifecycleID("call-open"), EvidenceSHA256: strings.Repeat("7", 64)}
	callSettlement := func(mark string) wheel.EventInput {
		return wheel.EventInput{Kind: wheel.EventExpiry, OccurredAt: callExpiry, UnderlyingMark: mark, EvidenceID: lifecycleID("call-expiry-" + mark), EvidenceSHA256: strings.Repeat("8", 64)}
	}
	inputs := map[string][]wheel.EventInput{
		"put_expiry":          {quality(), openPut(), putSettlement("100")},
		"put_assignment":      {quality(), openPut(), putSettlement("90")},
		"dividend":            {quality(), openPut(), putSettlement("90"), dividend},
		"covered_call_expiry": {quality(), openPut(), putSettlement("90"), dividend, openCall, callSettlement("95")},
		"call_away":           {quality(), openPut(), putSettlement("90"), dividend, openCall, callSettlement("110")},
	}
	scenarios := make(map[string]*wheel.Scenario, len(inputs))
	reports := make(map[string]*wheel.Report, len(inputs))
	for name, events := range inputs {
		scenario, scenarioErr := wheel.NewScenario(wheel.ScenarioInput{Policy: policy, UnderlyingID: underlying.ID, InitialCapital: "10000", EvaluationStart: start, EvaluationEnd: events[len(events)-1].OccurredAt, Mode: strategycatalog.ExperimentPaperScored, Events: events})
		if scenarioErr != nil {
			return nil, fmt.Errorf("build lifecycle %s: %w", name, scenarioErr)
		}
		report, reportErr := wheel.NewReport(policy, scenario)
		if reportErr != nil {
			return nil, fmt.Errorf("report lifecycle %s: %w", name, reportErr)
		}
		scenarios[name], reports[name] = scenario, report
	}
	return &LifecycleFixture{Policy: policy, Manifest: manifest, Underlying: underlying, Put: put, Call: call, PutContract: putContract, CallContract: callContract, Scenarios: scenarios, Reports: reports, CreatedAt: start.Add(-time.Hour), PutContent: putContent, CallContent: callContent, PutContentSHA: putSHA, CallContentSHA: callSHA}, nil
}

func lifecycleOption(kind, strike string, expiry time.Time, underlyingID uuid.UUID, createdAt time.Time) (*instrument.Instrument, error) {
	metadata, _ := json.Marshal(map[string]string{"contract_type": kind, "strike": strike})
	value, err := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: "osi:WHEEL-LIFECYCLE-" + strings.ToUpper(kind) + "-" + strike, AssetClass: instrument.AssetClassOption, PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(100), Expiration: &expiry, ExerciseStyle: instrument.ExerciseAmerican, SettlementMethod: instrument.SettlementPhysical, UnderlyingID: &underlyingID, Status: instrument.StatusActive, Metadata: metadata, CreatedAt: createdAt.Add(-time.Hour)})
	if err == nil {
		value.ID = lifecycleID(kind + "-" + strike)
	}
	return value, err
}

func lifecycleContract(value *instrument.Instrument, contractID string, start, end time.Time) (*instrument.VenueContract, error) {
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{InstrumentID: value.ID, Venue: "test-venue", ContractID: contractID, Currency: "USD", TickSize: value.TickSize, LotSize: value.LotSize, Multiplier: value.Multiplier, SettlementMethod: value.SettlementMethod, ValidFrom: start.Add(-24 * time.Hour), ValidTo: &end, Metadata: json.RawMessage(`{"fixture":"wheel-lifecycle"}`), CreatedAt: start.Add(-time.Hour)})
	if err == nil {
		contract.ID = lifecycleID(contractID)
	}
	return contract, err
}

func lifecycleID(value string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("wheel-lifecycle/"+value))
}
