package simulation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

// Persistence is the narrow raw-first boundary needed by the simulator. It is
// intentionally declared here so simulation does not import repository and
// create a package cycle.
type Persistence interface {
	RecordEconomicSourceEvent(context.Context, *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error)
	ApplyExecutionFill(context.Context, uuid.UUID, *lifecycle.Transition) (*lifecycle.Aggregate, error)
	ApplyExecutionTransition(context.Context, uuid.UUID, *lifecycle.Transition) (*lifecycle.Aggregate, error)
}

// PolicyStore is the narrow immutable artifact-registration boundary.
type PolicyStore interface {
	RegisterSimulationPolicy(context.Context, *PolicyArtifact) (*PolicyArtifact, error)
}

// RegisterPolicy materializes and stores the exact policy artifact before a
// route using that version may be persisted.
func RegisterPolicy(
	ctx context.Context,
	store PolicyStore,
	policy *Policy,
	createdAt time.Time,
) (*PolicyArtifact, error) {
	if store == nil {
		return nil, fmt.Errorf("register simulation policy: store is required")
	}
	artifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		return nil, fmt.Errorf("register simulation policy: %w", err)
	}
	registered, err := store.RegisterSimulationPolicy(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("register simulation policy %q: %w", artifact.Version, err)
	}
	if !SamePolicyArtifactPayload(registered, artifact) {
		return nil, fmt.Errorf("register simulation policy %q: store returned mismatched artifact", artifact.Version)
	}
	return registered, nil
}

// PersistResult records each raw fill source event before asking the existing
// lifecycle repository to atomically apply its normalization, ledger, fill,
// binding, and lifecycle event. Non-fill transitions never write economic raw
// evidence.
func PersistResult(
	ctx context.Context,
	store Persistence,
	accountID uuid.UUID,
	result *Result,
) (*lifecycle.Aggregate, error) {
	if store == nil {
		return nil, fmt.Errorf("persist simulation result: store is required")
	}
	if accountID == uuid.Nil || result == nil || result.Aggregate == nil || result.Aggregate.Intent.AccountID != accountID {
		return nil, fmt.Errorf("persist simulation result: matching account and result aggregate are required")
	}
	if len(result.Transitions) == 0 {
		return result.Aggregate, nil
	}
	var persisted *lifecycle.Aggregate
	for index, transition := range result.Transitions {
		if transition == nil {
			return nil, fmt.Errorf("persist simulation result: transition %d is nil", index)
		}
		if transition.Fill != nil {
			if transition.Normalization == nil || transition.Normalization.SourceEvent == nil {
				return nil, fmt.Errorf("persist simulation result: fill transition %d lacks raw normalization graph", index)
			}
			raw := transition.Normalization.SourceEvent
			persistedRaw, err := store.RecordEconomicSourceEvent(ctx, raw)
			if err != nil {
				return nil, fmt.Errorf("persist simulation result raw fill %s: %w", raw.ID, err)
			}
			if !ledger.SameEconomicSourceEventPayload(persistedRaw, raw) {
				return nil, fmt.Errorf("persist simulation result raw fill %s: store returned mismatched event", raw.ID)
			}
			persisted, err = store.ApplyExecutionFill(ctx, accountID, transition)
			if err != nil {
				return nil, fmt.Errorf("persist simulation result fill transition %s: %w", transition.Event.ID, err)
			}
			continue
		}
		if transition.Normalization != nil {
			return nil, fmt.Errorf("persist simulation result: non-fill transition %d carries economic normalization", index)
		}
		var err error
		persisted, err = store.ApplyExecutionTransition(ctx, accountID, transition)
		if err != nil {
			return nil, fmt.Errorf("persist simulation result transition %s: %w", transition.Event.ID, err)
		}
	}
	if persisted == nil || persisted.Intent.ID != result.Aggregate.Intent.ID || persisted.State != result.Aggregate.State ||
		len(persisted.Fills) != len(result.Aggregate.Fills) {
		return nil, fmt.Errorf("persist simulation result: persisted lifecycle does not match evaluated result")
	}
	return persisted, nil
}
