package simulation

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

// FillEffect is the exact simulator economic result for one consumed depth
// level. Fee is nil when the policy rounds the charge to zero.
type FillEffect struct {
	Fill              lifecycle.Fill
	DepthSide         marketdata.DepthSide
	DepthLevel        int
	DisplayedSize     decimal.Decimal
	Capacity          decimal.Decimal
	Quantity          decimal.Decimal
	Price             decimal.Decimal
	Fee               *decimal.Decimal
	SourceNamespace   string
	SourceEventID     string
	Evidence          []byte
	PolicyVersion     string
	NormalizerVersion string
}

type simulationFillInput struct {
	Policy        *Policy
	Asset         AssetPolicy
	Account       domain.Account
	Instrument    instrument.Instrument
	VenueContract instrument.VenueContract
	Aggregate     *lifecycle.Aggregate
	Snapshot      marketdata.QuoteSnapshot
	Assessment    marketdata.QuoteAssessment
	EvaluatedAt   time.Time
	RouteSession  *SessionWindow
	Level         marketdata.DepthLevel
	Capacity      decimal.Decimal
	Quantity      decimal.Decimal
	FirstFill     bool
}

func buildSimulationFill(input simulationFillInput) (*lifecycle.Transition, FillEffect, error) {
	if input.Policy == nil || input.Aggregate == nil || input.Aggregate.Order == nil {
		return nil, FillEffect{}, fmt.Errorf("build simulation fill: policy and routed lifecycle are required")
	}
	fee, err := input.Policy.FillFee(
		input.Instrument.AssetClass,
		input.Quantity,
		input.Level.Price,
		input.VenueContract.Multiplier,
		input.FirstFill,
	)
	if err != nil {
		return nil, FillEffect{}, fmt.Errorf("build simulation fill fee: %w", err)
	}
	evidence, err := marshalSimulationFillEvidence(
		input.Policy,
		input.Asset,
		input.Account,
		input.Aggregate,
		input.Snapshot,
		input.Assessment,
		input.EvaluatedAt,
		input.RouteSession,
		input.Level,
		input.Capacity,
		input.Quantity,
		input.VenueContract.Multiplier,
		fee,
		input.FirstFill,
	)
	if err != nil {
		return nil, FillEffect{}, err
	}
	namespace, err := simulationSourceNamespace(input.Account, "fill")
	if err != nil {
		return nil, FillEffect{}, err
	}
	sourceEventID := simulationFillSourceEventID(
		input.Aggregate.Order.ID.String(),
		input.Snapshot.ID.String(),
		input.Level.Side,
		input.Level.Level,
	)
	sourceEvent, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID: input.Account.ID, Source: "simulation", SourceNamespace: namespace,
		SourceEventID: sourceEventID, SourceRevision: input.Snapshot.SourceRevision,
		ObservedAt: input.Snapshot.ReceivedAt, RawPayload: evidence, CreatedAt: input.EvaluatedAt,
	})
	if err != nil {
		return nil, FillEffect{}, fmt.Errorf("build simulation raw fill event: %w", err)
	}
	fillID := lifecycle.FillID(input.Aggregate.Order.ID, sourceEvent.ID)
	var cost *ledger.CostComponent
	if fee != nil {
		cost = &ledger.CostComponent{Kind: ledger.CostKindFee, Currency: input.VenueContract.Currency, Amount: *fee}
	}
	effectiveAt := input.Snapshot.ReceivedAt
	if input.Snapshot.ExchangeAt != nil {
		effectiveAt = *input.Snapshot.ExchangeAt
	}
	normalizerVersion := fillEvidenceSchema + "@sha256:" + input.Policy.Digest()
	side := ledger.FillSideBuy
	if input.Aggregate.Order.Side == lifecycle.SideSell {
		side = ledger.FillSideSell
	}
	normalization, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: ledger.EconomicNormalizationBaseInput{
			SourceEvent: sourceEvent, Account: &input.Account, NormalizerVersion: normalizerVersion,
			ExecutionOriginType: input.Aggregate.Intent.OriginType,
			ExecutionOriginID:   input.Aggregate.Intent.OriginID,
			ReferenceType:       "execution_fill", ReferenceID: fillID.String(), EffectiveAt: effectiveAt,
		},
		Instrument: input.Instrument, VenueContract: input.VenueContract, Side: side,
		Quantity: input.Quantity, Price: input.Level.Price, Cost: cost,
	})
	if err != nil {
		return nil, FillEffect{}, fmt.Errorf("build simulation fill normalization: %w", err)
	}
	transition, err := lifecycle.RecordFill(input.Aggregate, lifecycle.FillInput{
		Normalization: normalization, ExternalOrderID: simulationExternalOrderID(input.Aggregate.Order.ID.String()),
		Event: lifecycle.EventInput{
			Source: "simulation", SourceNamespace: namespace, SourceEventID: sourceEventID,
			SourceRevision: input.Snapshot.SourceRevision, SourceAt: effectiveAt,
			ReceivedAt: input.Snapshot.ReceivedAt, Actor: "simulation-venue",
			ReasonCode: "depth_fill", Evidence: evidence,
		},
		CreatedAt: input.EvaluatedAt,
	})
	if err != nil {
		return nil, FillEffect{}, fmt.Errorf("build simulation lifecycle fill: %w", err)
	}
	return transition, FillEffect{
		Fill: *transition.Fill, DepthSide: input.Level.Side, DepthLevel: input.Level.Level,
		DisplayedSize: input.Level.Size, Capacity: input.Capacity, Quantity: input.Quantity,
		Price: input.Level.Price, Fee: cloneSimulationDecimal(fee), SourceNamespace: namespace,
		SourceEventID: sourceEventID, Evidence: append([]byte(nil), evidence...),
		PolicyVersion: input.Policy.Version(), NormalizerVersion: normalizerVersion,
	}, nil
}

func cloneSimulationDecimal(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
