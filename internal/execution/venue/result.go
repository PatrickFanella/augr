package venue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

// ResultStep is one raw-first provider fact and its optional interpretation.
// A fill additionally carries the matching OVR-103 economic source event.
type ResultStep struct {
	Observation         *Observation
	EconomicSourceEvent *ledger.EconomicSourceEvent
	Transition          *lifecycle.Transition
}

// Result is one ordered provider interpretation plan. Initial is the exact
// aggregate used by the adapter; Aggregate is the result after every step.
type Result struct {
	Initial   *lifecycle.Aggregate
	Aggregate *lifecycle.Aggregate
	Steps     []ResultStep
}

// Persistence is the narrow raw-first boundary required by venue adapters. It
// is declared here to avoid a package cycle through repository.
type Persistence interface {
	RecordVenueObservation(context.Context, *Observation) (*Observation, error)
	RecordEconomicSourceEvent(context.Context, *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error)
	ApplyExecutionFill(context.Context, uuid.UUID, *lifecycle.Transition) (*lifecycle.Aggregate, error)
	ApplyExecutionTransition(context.Context, uuid.UUID, *lifecycle.Transition) (*lifecycle.Aggregate, error)
}

// PolicyStore registers exact reviewed policy artifacts before route.
type PolicyStore interface {
	RegisterVenuePolicy(context.Context, *PolicyArtifact) (*PolicyArtifact, error)
}

// CancellationPersistence appends one local command event independently of
// provider observations.
type CancellationPersistence interface {
	ApplyExecutionTransition(context.Context, uuid.UUID, *lifecycle.Transition) (*lifecycle.Aggregate, error)
}

// RegisterPolicy materializes and stores the exact reviewed artifact.
func RegisterPolicy(
	ctx context.Context,
	store PolicyStore,
	policy *Policy,
	createdAt time.Time,
) (*PolicyArtifact, error) {
	if store == nil {
		return nil, fmt.Errorf("register venue policy: store is required")
	}
	if policy == nil {
		return nil, fmt.Errorf("register venue policy: policy is required")
	}
	artifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		return nil, fmt.Errorf("register venue policy: %w", err)
	}
	registered, err := store.RegisterVenuePolicy(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("register venue policy %q: %w", artifact.Version, err)
	}
	if !SamePolicyArtifactPayload(registered, artifact) {
		return nil, fmt.Errorf("register venue policy %q: store returned mismatched artifact", artifact.Version)
	}
	return registered, nil
}

// PersistResult preflights the complete plan, then records each observation,
// optional economic raw event, and lifecycle interpretation in that order.
// Every child repository operation is idempotent, so a restart can replay from
// an observation-only or observation-plus-economic checkpoint safely.
func PersistResult(
	ctx context.Context,
	store Persistence,
	accountID uuid.UUID,
	result *Result,
) (*lifecycle.Aggregate, error) {
	if store == nil {
		return nil, fmt.Errorf("persist venue result: store is required")
	}
	if err := validateResult(accountID, result); err != nil {
		return nil, fmt.Errorf("persist venue result: %w", err)
	}

	persisted := result.Initial
	for index, step := range result.Steps {
		observation, err := store.RecordVenueObservation(ctx, step.Observation)
		if err != nil {
			return nil, fmt.Errorf("persist venue result observation %d/%s: %w", index, step.Observation.ID, err)
		}
		if !SameObservationPayload(observation, step.Observation) {
			return nil, fmt.Errorf("persist venue result observation %d/%s: store returned mismatched evidence", index, step.Observation.ID)
		}
		if step.EconomicSourceEvent != nil {
			economic, economicErr := store.RecordEconomicSourceEvent(ctx, step.EconomicSourceEvent)
			if economicErr != nil {
				return nil, fmt.Errorf("persist venue result economic source %d/%s: %w", index, step.EconomicSourceEvent.ID, economicErr)
			}
			if !ledger.SameEconomicSourceEventPayload(economic, step.EconomicSourceEvent) {
				return nil, fmt.Errorf("persist venue result economic source %d/%s: store returned mismatched evidence", index, step.EconomicSourceEvent.ID)
			}
		}
		if step.Transition == nil {
			continue
		}
		if step.Transition.Fill != nil {
			persisted, err = store.ApplyExecutionFill(ctx, accountID, step.Transition)
		} else {
			persisted, err = store.ApplyExecutionTransition(ctx, accountID, step.Transition)
		}
		if err != nil {
			return nil, fmt.Errorf("persist venue result transition %d/%s: %w", index, step.Transition.Event.ID, err)
		}
	}
	if !sameAggregateResult(persisted, result.Aggregate) {
		return nil, fmt.Errorf("persist venue result: persisted lifecycle does not match planned aggregate")
	}
	return persisted, nil
}

func validateResult(accountID uuid.UUID, result *Result) error {
	if accountID == uuid.Nil || result == nil || result.Initial == nil || result.Aggregate == nil ||
		result.Initial.Intent.AccountID != accountID || result.Aggregate.Intent.AccountID != accountID ||
		result.Initial.Intent.ID == uuid.Nil || result.Aggregate.Intent.ID != result.Initial.Intent.ID ||
		result.Initial.Order == nil || result.Aggregate.Order == nil ||
		result.Initial.Order.ID != result.Aggregate.Order.ID || result.Initial.Order.PolicyKind != lifecycle.PolicyVenue ||
		result.Initial.Order.PolicyVersion == "" || len(result.Steps) == 0 {
		return fmt.Errorf("matching venue lifecycle, account, order, and nonempty steps are required")
	}

	current := result.Initial
	for index, step := range result.Steps {
		if step.Observation == nil {
			return fmt.Errorf("step %d observation is required", index)
		}
		if err := step.Observation.Validate(); err != nil {
			return fmt.Errorf("step %d observation is invalid: %w", index, err)
		}
		if err := validateObservationContext(current, step.Observation); err != nil {
			return fmt.Errorf("step %d: %w", index, err)
		}
		if step.Transition == nil {
			if step.EconomicSourceEvent != nil {
				return fmt.Errorf("step %d observation-only result carries economic evidence", index)
			}
			if step.Observation.MappedOutcome != OutcomeNoChange && step.Observation.MappedOutcome != OutcomeFillNotice {
				return fmt.Errorf("step %d mapping %q requires a lifecycle interpretation", index, step.Observation.MappedOutcome)
			}
			continue
		}
		if err := validateTransitionObservation(step); err != nil {
			return fmt.Errorf("step %d: %w", index, err)
		}
		next, err := lifecycle.ApplyTransition(current, step.Transition)
		if err != nil {
			return fmt.Errorf("step %d transition is out of order: %w", index, err)
		}
		current = next
	}
	if !sameAggregateResult(current, result.Aggregate) {
		return fmt.Errorf("planned aggregate does not match ordered steps")
	}
	return nil
}

func validateObservationContext(aggregate *lifecycle.Aggregate, observation *Observation) error {
	if aggregate == nil || aggregate.Order == nil || observation.AccountID != aggregate.Intent.AccountID ||
		observation.IntentID != aggregate.Intent.ID || observation.OrderID != aggregate.Order.ID ||
		observation.VenueContractID != aggregate.Order.VenueContractID || observation.Venue != aggregate.Order.Venue ||
		string(observation.Provider) != aggregate.Order.Venue || observation.PolicyVersion != aggregate.Order.PolicyVersion ||
		observation.ClientOrderID != aggregate.Order.ClientOrderID {
		return fmt.Errorf("observation canonical lifecycle context does not match")
	}
	if aggregate.Binding != nil {
		if observation.ExternalOrderID != aggregate.Binding.ExternalOrderID ||
			(observation.BindingID != nil && *observation.BindingID != aggregate.Binding.ID) {
			return fmt.Errorf("observation external binding context does not match")
		}
	}
	return nil
}

func validateTransitionObservation(step ResultStep) error {
	observation := step.Observation
	transition := step.Transition
	event := &transition.Event
	if event.Source != string(observation.Provider) || event.SourceNamespace != observation.SourceNamespace ||
		event.SourceEventID != observation.SourceEventID || event.SourceRevision != observation.SourceRevision ||
		!event.SourceAt.Equal(observation.SourceAt) || !event.ReceivedAt.Equal(observation.ReceivedAt) ||
		!bytes.Equal(event.Evidence, observation.RawPayload) || event.EvidenceSHA256 != observation.PayloadSHA256 {
		return fmt.Errorf("lifecycle transition differs from exact venue observation")
	}
	if transition.Binding != nil && (observation.ExternalOrderID != transition.Binding.ExternalOrderID ||
		(observation.BindingID != nil && *observation.BindingID != transition.Binding.ID)) {
		return fmt.Errorf("lifecycle binding differs from exact venue observation")
	}
	wanted, ok := mappedOutcomeForEvent(event.Kind)
	if !ok || !mappedOutcomeAccepted(observation.MappedOutcome, wanted) {
		return fmt.Errorf("mapping %q is incompatible with lifecycle event %q", observation.MappedOutcome, event.Kind)
	}

	isFill := event.Kind == lifecycle.EventFillAcknowledged || event.Kind == lifecycle.EventFillRecorded
	if isFill {
		if transition.Fill == nil || transition.Normalization == nil || transition.Normalization.SourceEvent == nil ||
			step.EconomicSourceEvent == nil {
			return fmt.Errorf("fill interpretation requires venue, economic, normalization, and fill evidence")
		}
		economic := step.EconomicSourceEvent
		if !ledger.SameEconomicSourceEventPayload(economic, transition.Normalization.SourceEvent) ||
			economic.AccountID != observation.AccountID || economic.Source != string(observation.Provider) ||
			economic.SourceNamespace != observation.SourceNamespace || economic.SourceEventID != observation.SourceEventID ||
			economic.SourceRevision != observation.SourceRevision || !economic.ObservedAt.Equal(observation.ReceivedAt) ||
			!bytes.Equal(economic.RawPayload, observation.RawPayload) || economic.PayloadSHA256 != observation.PayloadSHA256 ||
			!transition.Fill.EffectiveAt.Equal(observation.SourceAt) || !transition.Fill.ReceivedAt.Equal(observation.ReceivedAt) {
			return fmt.Errorf("economic fill evidence differs from exact venue observation")
		}
		return nil
	}
	if step.EconomicSourceEvent != nil || transition.Fill != nil || transition.Normalization != nil {
		return fmt.Errorf("non-fill interpretation carries economic or fill evidence")
	}
	return nil
}

func mappedOutcomeForEvent(kind lifecycle.EventKind) ([]MappedOutcome, bool) {
	switch kind {
	case lifecycle.EventOrderWorking:
		return []MappedOutcome{OutcomeAcknowledge}, true
	case lifecycle.EventFillAcknowledged, lifecycle.EventFillRecorded:
		return []MappedOutcome{OutcomeFill}, true
	case lifecycle.EventOrderCancelled:
		return []MappedOutcome{OutcomeCancelled}, true
	case lifecycle.EventOrderExpired:
		return []MappedOutcome{OutcomeExpired}, true
	case lifecycle.EventOrderRejected:
		return []MappedOutcome{OutcomeRejected}, true
	case lifecycle.EventUnknownVenueState:
		return []MappedOutcome{OutcomeUnknownState}, true
	case lifecycle.EventContradictoryVenueState:
		return []MappedOutcome{OutcomeContradiction, OutcomeMalformedObservation}, true
	case lifecycle.EventFillCorrectionObserved:
		return []MappedOutcome{OutcomeCorrection}, true
	case lifecycle.EventFillBustObserved:
		return []MappedOutcome{OutcomeBust}, true
	default:
		return nil, false
	}
}

func mappedOutcomeAccepted(actual MappedOutcome, accepted []MappedOutcome) bool {
	for _, candidate := range accepted {
		if actual == candidate {
			return true
		}
	}
	return false
}

// NewCancellationCommand constructs the one canonical local DELETE command.
// It never claims that a provider has accepted or completed cancellation.
func NewCancellationCommand(
	aggregate *lifecycle.Aggregate,
	requestedAt time.Time,
) (*lifecycle.Transition, error) {
	if aggregate == nil || aggregate.Order == nil || aggregate.Binding == nil ||
		aggregate.Order.PolicyKind != lifecycle.PolicyVenue {
		return nil, fmt.Errorf("new venue cancellation command: bound venue lifecycle is required")
	}
	provider := Provider(aggregate.Order.Venue)
	if !validProvider(provider) {
		return nil, fmt.Errorf("new venue cancellation command: reviewed provider is required")
	}
	evidence, namespace, sourceEventID, err := cancellationCommandEvidence(aggregate, provider)
	if err != nil {
		return nil, err
	}
	for index := range aggregate.Events {
		existing := aggregate.Events[index]
		if existing.Kind == lifecycle.EventCancelRequested && existing.Source == "venue_command" &&
			existing.SourceNamespace == namespace && existing.SourceEventID == sourceEventID {
			if !bytes.Equal(existing.Evidence, evidence) {
				return nil, fmt.Errorf("new venue cancellation command: existing command evidence conflicts")
			}
			return &lifecycle.Transition{Event: existing}, nil
		}
	}
	requestedAt = requestedAt.UTC().Truncate(time.Microsecond)
	if requestedAt.IsZero() {
		return nil, fmt.Errorf("new venue cancellation command: request time is required")
	}
	transition, err := lifecycle.RequestCancel(aggregate, lifecycle.EventInput{
		Source: "venue_command", SourceNamespace: namespace, SourceEventID: sourceEventID,
		SourceAt: requestedAt, ReceivedAt: requestedAt, Actor: "venue-adapter",
		ReasonCode: "cancel_requested", Evidence: evidence,
	}, requestedAt)
	if err != nil {
		return nil, fmt.Errorf("new venue cancellation command: %w", err)
	}
	return transition, nil
}

// PersistCancellationCommand validates and commits a canonical local command
// before a caller is allowed to attempt provider DELETE transport.
func PersistCancellationCommand(
	ctx context.Context,
	store CancellationPersistence,
	accountID uuid.UUID,
	aggregate *lifecycle.Aggregate,
	command *lifecycle.Transition,
) (*lifecycle.Aggregate, error) {
	if store == nil || accountID == uuid.Nil || aggregate == nil || aggregate.Order == nil ||
		aggregate.Intent.AccountID != accountID || command == nil {
		return nil, fmt.Errorf("persist venue cancellation command: matching store, account, aggregate, and command are required")
	}
	provider := Provider(aggregate.Order.Venue)
	evidence, namespace, sourceEventID, err := cancellationCommandEvidence(aggregate, provider)
	if err != nil {
		return nil, err
	}
	event := &command.Event
	if event.Kind != lifecycle.EventCancelRequested || event.Source != "venue_command" ||
		event.SourceNamespace != namespace || event.SourceEventID != sourceEventID || event.SourceRevision != "" ||
		!bytes.Equal(event.Evidence, evidence) {
		return nil, fmt.Errorf("persist venue cancellation command: identity or canonical evidence is invalid")
	}
	for index := range aggregate.Events {
		if aggregate.Events[index].ID == event.ID {
			if !lifecycle.SameEventPayload(&aggregate.Events[index], event) {
				return nil, fmt.Errorf("persist venue cancellation command: existing command payload conflicts")
			}
			return aggregate, nil
		}
	}
	expected, err := lifecycle.ApplyTransition(aggregate, command)
	if err != nil {
		return nil, fmt.Errorf("persist venue cancellation command: %w", err)
	}
	persisted, err := store.ApplyExecutionTransition(ctx, accountID, command)
	if err != nil {
		return nil, fmt.Errorf("persist venue cancellation command %s: %w", event.ID, err)
	}
	if !sameAggregateResult(persisted, expected) {
		return nil, fmt.Errorf("persist venue cancellation command: persisted lifecycle differs from command")
	}
	return persisted, nil
}

func cancellationCommandEvidence(
	aggregate *lifecycle.Aggregate,
	provider Provider,
) ([]byte, string, string, error) {
	if aggregate == nil || aggregate.Order == nil || aggregate.Binding == nil || !validProvider(provider) ||
		aggregate.Order.Venue != string(provider) || aggregate.Order.PolicyKind != lifecycle.PolicyVenue ||
		aggregate.Order.PolicyVersion == "" || aggregate.Order.ClientOrderID == "" {
		return nil, "", "", fmt.Errorf("venue cancellation command canonical order context is invalid")
	}
	path := "/v2/orders/{external_order_id}"
	if provider == ProviderKalshi {
		path = "/portfolio/events/orders/{external_order_id}"
	}
	value := struct {
		Schema          string `json:"schema"`
		OrderID         string `json:"order_id"`
		Provider        string `json:"provider"`
		Venue           string `json:"venue"`
		PolicyVersion   string `json:"policy_version"`
		ClientOrderID   string `json:"client_order_id"`
		BindingID       string `json:"binding_id"`
		ExternalOrderID string `json:"external_order_id"`
		Method          string `json:"method"`
		PathTemplate    string `json:"path_template"`
		RequestBody     string `json:"request_body"`
	}{
		Schema: "venue-cancel-request-v1", OrderID: aggregate.Order.ID.String(),
		Provider: string(provider), Venue: aggregate.Order.Venue, PolicyVersion: aggregate.Order.PolicyVersion,
		ClientOrderID: aggregate.Order.ClientOrderID, BindingID: aggregate.Binding.ID.String(),
		ExternalOrderID: aggregate.Binding.ExternalOrderID, Method: "DELETE", PathTemplate: path,
		RequestBody: "<empty>",
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, "", "", fmt.Errorf("encode venue cancellation command: %w", err)
	}
	evidence := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	namespace := PolicySchemaV1 + "/" + string(provider) + "/" + aggregate.Order.PolicyVersion + "/cancel-request-v1"
	sourceEventID := aggregate.Order.ID.String() + "/cancel-request-v1"
	return evidence, namespace, sourceEventID, nil
}

func sameAggregateResult(left, right *lifecycle.Aggregate) bool {
	if left == nil || right == nil || left.Intent.ID != right.Intent.ID || left.Intent.AccountID != right.Intent.AccountID ||
		left.State != right.State || len(left.Events) != len(right.Events) || len(left.Fills) != len(right.Fills) ||
		(left.Order == nil) != (right.Order == nil) || (left.Binding == nil) != (right.Binding == nil) {
		return false
	}
	if left.Order != nil && left.Order.ID != right.Order.ID {
		return false
	}
	if left.Binding != nil && left.Binding.ID != right.Binding.ID {
		return false
	}
	for index := range left.Events {
		if left.Events[index].ID != right.Events[index].ID ||
			!lifecycle.SameEventPayload(&left.Events[index], &right.Events[index]) {
			return false
		}
	}
	for index := range left.Fills {
		if !lifecycle.SameFillPayload(&left.Fills[index], &right.Fills[index]) {
			return false
		}
	}
	return true
}
