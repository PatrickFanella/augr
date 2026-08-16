package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

const outcomeSchemaV1 = "simulation-outcome-v1"

// OutcomeInput contains the complete economic effects for one replayed
// lifecycle. Effects may be accumulated across several observations.
type OutcomeInput struct {
	Account       domain.Account
	VenueContract instrument.VenueContract
	Aggregate     *lifecycle.Aggregate
	Fills         []FillEffect
}

// Outcome is a canonical economic projection. Process-local timestamps,
// account/order UUIDs, and provider source IDs are deliberately absent from
// its content hash.
type Outcome struct {
	PolicyVersion    string
	FinalState       lifecycle.State
	Environment      domain.AccountEnvironment
	EvidenceClass    string
	StorageNamespace string
	Side             lifecycle.Side
	Currency         string
	Multiplier       decimal.Decimal
	TotalQuantity    decimal.Decimal
	GrossCash        decimal.Decimal
	TotalFee         decimal.Decimal
	Fills            []OutcomeFill
	canonicalBytes   json.RawMessage
	hash             string
}

// OutcomeFill is one ordered economic fill in the canonical projection.
type OutcomeFill struct {
	Quantity decimal.Decimal
	Price    decimal.Decimal
	Fee      decimal.Decimal
}

type canonicalOutcome struct {
	Schema           string                 `json:"schema"`
	PolicyVersion    string                 `json:"policy_version"`
	FinalState       string                 `json:"final_state"`
	Environment      string                 `json:"environment"`
	EvidenceClass    string                 `json:"evidence_class"`
	StorageNamespace string                 `json:"storage_namespace"`
	Side             string                 `json:"side"`
	Currency         string                 `json:"currency"`
	Multiplier       string                 `json:"multiplier"`
	TotalQuantity    string                 `json:"total_quantity"`
	GrossCash        string                 `json:"gross_cash"`
	TotalFee         string                 `json:"total_fee"`
	Fills            []canonicalOutcomeFill `json:"fills"`
}

type canonicalOutcomeFill struct {
	Quantity string `json:"quantity"`
	Price    string `json:"price"`
	Fee      string `json:"fee"`
}

// NewOutcome validates and hashes one fully ordered economic projection.
func NewOutcome(input OutcomeInput) (*Outcome, error) {
	if input.Aggregate == nil || input.Aggregate.Order == nil {
		return nil, fmt.Errorf("simulation outcome requires a routed lifecycle")
	}
	if err := input.Account.Validate(); err != nil {
		return nil, fmt.Errorf("simulation outcome account: %w", err)
	}
	if err := input.VenueContract.Validate(); err != nil {
		return nil, fmt.Errorf("simulation outcome venue contract: %w", err)
	}
	aggregate := input.Aggregate
	if err := aggregate.Intent.Validate(); err != nil {
		return nil, fmt.Errorf("simulation outcome intent: %w", err)
	}
	if err := aggregate.Order.Validate(); err != nil {
		return nil, fmt.Errorf("simulation outcome order: %w", err)
	}
	if aggregate.Intent.AccountID != input.Account.ID || aggregate.Intent.Environment != input.Account.Environment ||
		aggregate.Order.AccountID != input.Account.ID || aggregate.Order.VenueContractID != input.VenueContract.ID ||
		aggregate.Order.Venue != input.VenueContract.Venue || input.Account.BaseCurrency != input.VenueContract.Currency {
		return nil, fmt.Errorf("simulation outcome account, lifecycle, and contract context mismatch")
	}
	if aggregate.Order.PolicyKind != lifecycle.PolicySimulation {
		return nil, fmt.Errorf("simulation outcome requires a simulation policy order")
	}
	effects := make(map[uuid.UUID]FillEffect, len(input.Fills))
	for _, effect := range input.Fills {
		if effect.Fill.ID == uuid.Nil {
			return nil, fmt.Errorf("simulation outcome fill effect identity is required")
		}
		if _, duplicate := effects[effect.Fill.ID]; duplicate {
			return nil, fmt.Errorf("simulation outcome fill effect %s is duplicated", effect.Fill.ID)
		}
		effects[effect.Fill.ID] = effect
	}
	if len(effects) != len(aggregate.Fills) {
		return nil, fmt.Errorf("simulation outcome requires exactly one effect per lifecycle fill")
	}
	outcome := &Outcome{
		PolicyVersion: aggregate.Order.PolicyVersion, FinalState: aggregate.State,
		Environment: input.Account.Environment, EvidenceClass: input.Account.EvidenceClass,
		StorageNamespace: input.Account.StorageNamespace, Side: aggregate.Order.Side,
		Currency: input.VenueContract.Currency, Multiplier: input.VenueContract.Multiplier,
		Fills: make([]OutcomeFill, 0, len(aggregate.Fills)),
	}
	canonical := canonicalOutcome{
		Schema: outcomeSchemaV1, PolicyVersion: outcome.PolicyVersion, FinalState: string(outcome.FinalState),
		Environment: string(outcome.Environment), EvidenceClass: outcome.EvidenceClass,
		StorageNamespace: outcome.StorageNamespace, Side: string(outcome.Side),
		Currency: outcome.Currency, Multiplier: outcome.Multiplier.String(),
		Fills: make([]canonicalOutcomeFill, 0, len(aggregate.Fills)),
	}
	for _, fill := range aggregate.Fills {
		if err := fill.Validate(); err != nil {
			return nil, fmt.Errorf("simulation outcome lifecycle fill %s: %w", fill.ID, err)
		}
		effect, ok := effects[fill.ID]
		if !ok || !lifecycle.SameFillPayload(&fill, &effect.Fill) || effect.PolicyVersion != outcome.PolicyVersion ||
			!effect.Quantity.Equal(fill.Quantity) || !effect.Price.Equal(fill.Price) {
			return nil, fmt.Errorf("simulation outcome effect does not match lifecycle fill %s", fill.ID)
		}
		fee := decimal.Zero
		if effect.Fee != nil {
			if effect.Fee.IsNegative() {
				return nil, fmt.Errorf("simulation outcome fee cannot be negative")
			}
			fee = *effect.Fee
		}
		outcome.Fills = append(outcome.Fills, OutcomeFill{Quantity: fill.Quantity, Price: fill.Price, Fee: fee})
		outcome.TotalQuantity = outcome.TotalQuantity.Add(fill.Quantity)
		notional := fill.Quantity.Mul(fill.Price).Mul(outcome.Multiplier)
		if outcome.Side == lifecycle.SideBuy {
			notional = notional.Neg()
		}
		outcome.GrossCash = outcome.GrossCash.Add(notional)
		outcome.TotalFee = outcome.TotalFee.Add(fee)
		canonical.Fills = append(canonical.Fills, canonicalOutcomeFill{
			Quantity: fill.Quantity.String(), Price: fill.Price.String(), Fee: fee.String(),
		})
	}
	canonical.TotalQuantity = outcome.TotalQuantity.String()
	canonical.GrossCash = outcome.GrossCash.String()
	canonical.TotalFee = outcome.TotalFee.String()
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal simulation outcome: %w", err)
	}
	digest := sha256.Sum256(encoded)
	outcome.canonicalBytes = encoded
	outcome.hash = hex.EncodeToString(digest[:])
	return outcome, nil
}

// CanonicalBytes returns a clone of the exact hashed outcome projection.
func (outcome *Outcome) CanonicalBytes() json.RawMessage {
	if outcome == nil {
		return nil
	}
	return append(json.RawMessage(nil), outcome.canonicalBytes...)
}

// Hash returns the lowercase SHA-256 of CanonicalBytes.
func (outcome *Outcome) Hash() string {
	if outcome == nil {
		return ""
	}
	return outcome.hash
}
