// Package qualification builds explicit deterministic Defined-Risk Options V1
// fixtures. It is test infrastructure, not a runtime registry.
package qualification

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/definedrisk"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

var (
	DecisionAt = time.Date(2026, 8, 20, 15, 0, 0, 123456000, time.UTC)
	ExpiryAt   = DecisionAt.Add(24 * time.Hour)
)

type Fixture struct {
	Policy     *definedrisk.Policy
	Scenario   *definedrisk.Scenario
	Report     *definedrisk.Report
	Underlying *instrument.Instrument
	Options    []*instrument.Instrument
	Contracts  []*instrument.VenueContract
}

func Build(mode strategycatalog.ExperimentMode, execution definedrisk.ExecutionMode, strategy definedrisk.Strategy, shortDepth, terminal string) (*Fixture, error) {
	if mode != strategycatalog.ExperimentPaperScored && mode != strategycatalog.ExperimentPaperStress {
		return nil, fmt.Errorf("defined-risk qualification mode is invalid")
	}
	underlying, err := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: "figi:DEFINED-RISK-V1", AssetClass: instrument.AssetClassEquity, PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive, Metadata: json.RawMessage(`{"fixture":"defined-risk-v1"}`), CreatedAt: DecisionAt.Add(-time.Hour)})
	if err != nil {
		return nil, err
	}
	underlying.ID = id("underlying", mode, execution, strategy)
	optionType, lowPosition, highPosition := structure(strategy)
	options := make([]*instrument.Instrument, 2)
	contracts := make([]*instrument.VenueContract, 2)
	strikes := []string{"100", "110"}
	for i := range options {
		metadata, _ := json.Marshal(map[string]string{"contract_type": optionType, "strike": strikes[i]})
		value, createErr := instrument.NewInstrument(instrument.InstrumentInput{IdentityKey: fmt.Sprintf("osi:DEFINED-RISK-%s-%s", optionType, strikes[i]), AssetClass: instrument.AssetClassOption, PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(100), Expiration: &ExpiryAt, ExerciseStyle: instrument.ExerciseEuropean, SettlementMethod: instrument.SettlementCash, UnderlyingID: &underlying.ID, Status: instrument.StatusActive, Metadata: metadata, CreatedAt: DecisionAt.Add(-time.Hour)})
		if createErr != nil {
			return nil, createErr
		}
		value.ID = id(fmt.Sprintf("option-%d", i), mode, execution, strategy)
		options[i] = value
		contract, contractErr := instrument.NewVenueContract(instrument.VenueContractInput{InstrumentID: value.ID, Venue: "test-venue", ContractID: fmt.Sprintf("DEFINED-RISK-%s-%s", optionType, strikes[i]), Currency: "USD", TickSize: value.TickSize, LotSize: value.LotSize, Multiplier: value.Multiplier, SettlementMethod: value.SettlementMethod, ValidFrom: DecisionAt.Add(-time.Hour), ValidTo: &ExpiryAt, Metadata: json.RawMessage(`{"fixture":"defined-risk-v1"}`), CreatedAt: DecisionAt.Add(-time.Hour)})
		if contractErr != nil {
			return nil, contractErr
		}
		contract.ID = id(fmt.Sprintf("contract-%d", i), mode, execution, strategy)
		contracts[i] = contract
	}
	policy, err := definedrisk.NewPolicy(definedrisk.PolicyInput{Version: "defined-risk-v1-qualification", ExecutionMode: execution, MaximumEvidenceAgeSeconds: 60, MaximumContracts: 5, MaximumPositionCapital: "10000", FeePerContractPerLeg: "1", DecimalScale: 12})
	if err != nil {
		return nil, err
	}
	positions := []string{lowPosition, highPosition}
	bids := []string{"1.8", "0.8"}
	asks := []string{"2", "1"}
	legs := make([]definedrisk.LegInput, 2)
	for i := range legs {
		entry := quote(fmt.Sprintf("leg-%d-entry", i), bids[i], asks[i], map[bool]string{true: shortDepth, false: "10"}[positions[i] == "short"], "10")
		var unwind *definedrisk.QuoteInput
		if execution == definedrisk.ExecutionSequential && positions[i] == "long" {
			unwindBid := "1.8"
			if asks[i] == "1" {
				unwindBid = "0.8"
			}
			value := quote(fmt.Sprintf("leg-%d-unwind", i), unwindBid, asks[i], "10", "10")
			unwind = &value
		}
		legs[i] = definedrisk.LegInput{InstrumentID: options[i].ID, VenueContractID: contracts[i].ID, OCCSymbol: contracts[i].ContractID, Underlying: "DEFINED-RISK-V1", OptionType: optionType, Strike: strikes[i], Expiry: ExpiryAt, Multiplier: "100", Style: "european", Position: positions[i], Entry: entry, Unwind: unwind}
	}
	scenario, err := definedrisk.NewScenario(definedrisk.ScenarioInput{Policy: policy, Strategy: strategy, InitialCapital: "10000", RequestedContracts: 2, DecisionAt: DecisionAt, ExpiryAt: ExpiryAt, TerminalUnderlying: terminal, TerminalAvailableAt: ExpiryAt, TerminalEvidenceID: id("terminal", mode, execution, strategy), TerminalEvidenceSHA256: strings.Repeat("9", 64), TerminalPartitionContentSHA256: strings.Repeat("8", 64), TerminalSourceKey: "defined-risk-terminal", Mode: mode, Legs: legs})
	if err != nil {
		return nil, err
	}
	report, err := definedrisk.NewReport(policy, scenario)
	if err != nil {
		return nil, err
	}
	return &Fixture{policy, scenario, report, underlying, options, contracts}, nil
}

func structure(strategy definedrisk.Strategy) (optionType, low, high string) {
	switch strategy {
	case definedrisk.BullCall:
		return "call", "long", "short"
	case definedrisk.BearPut:
		return "put", "short", "long"
	case definedrisk.BullPut:
		return "put", "long", "short"
	default:
		return "call", "short", "long"
	}
}

func quote(salt, bid, ask, bidSize, askSize string) definedrisk.QuoteInput {
	return definedrisk.QuoteInput{Bid: bid, Ask: ask, BidSize: bidSize, AskSize: askSize, AvailableAt: DecisionAt, EvidenceID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("defined-risk/"+salt)), EvidenceSHA256: strings.Repeat("a", 64), PartitionContentSHA256: strings.Repeat("b", 64), SourceKey: "defined-risk-" + salt}
}

func id(salt string, mode strategycatalog.ExperimentMode, execution definedrisk.ExecutionMode, strategy definedrisk.Strategy) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("defined-risk/%s/%s/%s/%s", mode, execution, strategy, salt)))
}
