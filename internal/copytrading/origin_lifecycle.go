package copytrading

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

// OriginProposalInput is the exact OVR-203 handoff for one approved copy
// intent. OVR-502 supplies the fresh executable decision snapshot; this
// adapter owns attribution and cannot create a strategy-version origin.
type OriginProposalInput struct {
	Subscription     domain.CopySubscription
	Intent           domain.CopyTradeIntent
	Account          domain.Account
	Instrument       instrument.Instrument
	DecisionSnapshot marketdata.QuoteSnapshot
	QuantityDelta    decimal.Decimal
	DecisionAt       time.Time
	CreatedAt        time.Time
}

// BuildOriginProposal constructs the immutable common-lifecycle proposal for
// a copy subscription without routing or granting execution authority.
func BuildOriginProposal(input OriginProposalInput) (*lifecycle.Aggregate, error) {
	subscription := input.Subscription
	intent := input.Intent
	if subscription.ID == uuid.Nil ||
		subscription.OriginType != "copy_subscription" || subscription.OriginID != subscription.ID ||
		intent.ID == uuid.Nil || intent.SubscriptionID != subscription.ID ||
		intent.OriginType != subscription.OriginType || intent.OriginID != subscription.OriginID {
		return nil, fmt.Errorf("copy origin proposal attribution is invalid")
	}
	if !subscription.IsPaper || subscription.Status != domain.CopySubscriptionPaperActive || intent.PolicyStatus != "approved" {
		return nil, fmt.Errorf("copy origin proposal requires an approved active paper intent")
	}
	if input.QuantityDelta.IsZero() || (intent.Side == domain.OrderSideBuy && input.QuantityDelta.IsNegative()) || (intent.Side == domain.OrderSideSell && input.QuantityDelta.IsPositive()) {
		return nil, fmt.Errorf("copy origin proposal quantity does not match side")
	}
	metadata, err := json.Marshal(map[string]any{
		"calculation_version":   intent.CalculationVersion,
		"copy_intent_id":        intent.ID,
		"source_observation_id": intent.SourceObservationID,
		"subscription_id":       subscription.ID,
	})
	if err != nil {
		return nil, err
	}
	decisionAt := input.DecisionAt.UTC().Truncate(time.Microsecond)
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	key := fmt.Sprintf("copy_subscription/%s/%s/%s/%d", subscription.ID, intent.SourceObservationID, intent.InstrumentKey, intent.CalculationVersion)
	return lifecycle.Propose(lifecycle.ProposeInput{
		Account:              input.Account,
		Instrument:           input.Instrument,
		DecisionSnapshot:     input.DecisionSnapshot,
		IdempotencyKey:       key,
		DesiredQuantityDelta: input.QuantityDelta,
		DecisionAt:           decisionAt,
		OriginType:           ledger.ExecutionOriginCopySubscription,
		OriginID:             subscription.ID.String(),
		StrategyVersionID:    "",
		Metadata:             metadata,
		Event: lifecycle.EventInput{
			Source:          "augr",
			SourceNamespace: "copy_subscription/" + subscription.ID.String(),
			SourceEventID:   intent.ID.String(),
			SourceAt:        decisionAt,
			ReceivedAt:      createdAt,
			Actor:           "copy-rebalance",
			ReasonCode:      "copy_intent_approved",
			Evidence:        metadata,
		},
		CreatedAt: createdAt,
	})
}
