package migrations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

func TestVenueAdapterMigrationRejectsArtifactFieldForgery(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	artifact := venueMigrationArtifact(t, venue.ProviderAlpaca)

	prettyBytes := new(bytes.Buffer)
	if err := json.Indent(prettyBytes, artifact.CanonicalBytes, "", "  "); err != nil {
		t.Fatal(err)
	}
	pretty := forgedVenueMigrationArtifact(prettyBytes.Bytes(), venue.ProviderAlpaca)
	if err := insertVenueMigrationArtifact(ctx, pool, pretty); err == nil {
		t.Fatal("noncanonical but semantically equal policy bytes unexpectedly inserted")
	}

	zeroDigest := strings.Repeat("0", 64)
	zeroVersion := venue.PolicySchemaV1 + "@sha256:" + zeroDigest
	wrongID := economicid.DeterministicUUID("venue-adapter-policy-artifact", zeroVersion)
	for name, values := range map[string]struct {
		id            uuid.UUID
		provider      string
		venue         string
		policyVersion string
		digest        string
		canonicalJSON []byte
	}{
		"hash": {
			id: wrongID, provider: "alpaca", venue: "alpaca", policyVersion: zeroVersion,
			digest: zeroDigest, canonicalJSON: artifact.CanonicalBytes,
		},
		"version": {
			id: artifact.ID, provider: "alpaca", venue: "alpaca", policyVersion: zeroVersion,
			digest: artifact.SHA256, canonicalJSON: artifact.CanonicalBytes,
		},
		"provider": {
			id: artifact.ID, provider: "kalshi", venue: "kalshi", policyVersion: artifact.Version,
			digest: artifact.SHA256, canonicalJSON: artifact.CanonicalBytes,
		},
		"parsed json": {
			id: artifact.ID, provider: "alpaca", venue: "alpaca", policyVersion: artifact.Version,
			digest: artifact.SHA256, canonicalJSON: []byte(`{"changed":true}`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `INSERT INTO venue_adapter_policy_artifacts (
				id, schema_name, provider, venue, policy_version, sha256,
				canonical_bytes, canonical_json, created_at
			) VALUES ($1,'venue-adapter-policy-v1',$2,$3,$4,$5,$6,$7::JSONB,$8)`,
				values.id, values.provider, values.venue, values.policyVersion, values.digest,
				[]byte(artifact.CanonicalBytes), values.canonicalJSON, venueMigrationTime(),
			); err == nil {
				t.Fatal("forged artifact fields unexpectedly inserted")
			}
		})
	}
}

func TestVenueAdapterMigrationRejectsWrongVenuePolicy(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact := venueMigrationArtifact(t, venue.ProviderKalshi)
	if err := insertVenueMigrationArtifact(ctx, pool, artifact); err != nil {
		t.Fatal(err)
	}
	fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderAlpaca, `{}`)
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "wrong-venue-policy")
	if _, err := insertVenueMigrationOrder(
		ctx, pool, fixture, intentID, "wrong-venue-policy", artifact.Version, "day",
	); err == nil || !strings.Contains(err.Error(), "registered same-venue") {
		t.Fatalf("wrong-venue route error = %v", err)
	}
}

func TestVenueAdapterMigrationRetainsDistinctNoChangeObservations(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact, fixture, intentID, orderID := seedRoutedKalshiVenueOrder(t, ctx, pool, base, "no-change")

	for _, sourceEventID := range []string{"snapshot-1", "snapshot-2"} {
		observation := venueMigrationAcknowledgement(t, fixture, intentID, orderID, artifact.Version, sourceEventID)
		observation.MappedOutcome = venue.OutcomeNoChange
		if err := insertVenueMigrationObservation(ctx, pool, observation); err != nil {
			t.Fatalf("insert %s no-change observation: %v", sourceEventID, err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM venue_observations
		WHERE order_id=$1 AND mapped_outcome='no_change'`, orderID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("durable no-change observation count = %d, want 2", count)
	}
}

func TestVenueAdapterMigrationRejectsMalformedObservationFields(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact, fixture, intentID, orderID := seedRoutedKalshiVenueOrder(t, ctx, pool, base, "malformed-fields")

	for name, mutate := range map[string]func(*venue.Observation){
		"identity": func(observation *venue.Observation) { observation.ID = uuid.New() },
		"digest": func(observation *venue.Observation) {
			observation.PayloadSHA256 = strings.Repeat("0", 64)
		},
		"time order": func(observation *venue.Observation) {
			observation.SourceAt = observation.ReceivedAt.Add(time.Second)
		},
		"local label": func(observation *venue.Observation) {
			observation.IdentityKind = venue.SourceIdentityLocalResponse
		},
		"price scale": func(observation *venue.Observation) {
			price := decimal.RequireFromString("0.5800000000001")
			observation.ProviderPrice = &price
		},
	} {
		t.Run(name, func(t *testing.T) {
			observation := venueMigrationObservation(
				t, fixture, intentID, orderID, artifact.Version, "malformed-"+strings.ReplaceAll(name, " ", "-"),
			)
			mutate(observation)
			if err := insertVenueMigrationObservation(ctx, pool, observation); err == nil {
				t.Fatal("malformed observation fields unexpectedly inserted")
			}
		})
	}
}

func TestVenueAdapterMigrationRejectsObservationBindingAddedLaterWithDifferentExternalID(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact, fixture, intentID, orderID := seedRoutedKalshiVenueOrder(t, ctx, pool, base, "deferred-binding")
	observation := venueMigrationAcknowledgement(t, fixture, intentID, orderID, artifact.Version, "deferred-binding-ack")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertVenueMigrationObservation(ctx, tx, observation); err != nil {
		t.Fatal(err)
	}
	bindingID := economicid.DeterministicUUID("execution-order-binding", orderID.String())
	if _, err := tx.Exec(ctx, `INSERT INTO execution_order_bindings (
		id, order_id, account_id, venue, external_order_id, created_at
	) VALUES ($1,$2,$3,'kalshi',$4,$5)`, bindingID, orderID, fixture.Common.AccountID,
		"different-external-id", observation.ReceivedAt,
	); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil || !strings.Contains(err.Error(), "provider identity contradicts canonical context") {
		t.Fatalf("deferred binding mismatch error = %v", err)
	}
}

func TestVenueAdapterMigrationRequiresExactObservationEvidence(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact, fixture, intentID, orderID := seedRoutedKalshiVenueOrder(t, ctx, pool, base, "event-evidence")
	observation := venueMigrationAcknowledgement(t, fixture, intentID, orderID, artifact.Version, "event-evidence-ack")
	if err := insertVenueMigrationObservation(ctx, pool, observation); err != nil {
		t.Fatal(err)
	}
	wrongEvidence := []byte(`{"different":true}`)
	if err := insertVenueMigrationProviderEventWithBinding(
		ctx, pool, fixture, intentID, orderID, artifact.Version,
		observation, "order_working", "routed", "working", "provider_acknowledged", wrongEvidence,
	); err == nil || !strings.Contains(err.Error(), "exact prior raw observation") {
		t.Fatalf("mismatched provider event evidence error = %v", err)
	}
	if err := insertVenueMigrationProviderEventWithBinding(
		ctx, pool, fixture, intentID, orderID, artifact.Version,
		observation, "order_working", "routed", "working", "provider_acknowledged", observation.RawPayload,
	); err != nil {
		t.Fatalf("exact provider event evidence: %v", err)
	}
}

func TestVenueAdapterMigrationAllowsExactLocalCancelCommandWithoutObservation(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact, fixture, intentID, orderID := seedRoutedKalshiVenueOrder(t, ctx, pool, base, "cancel-command")
	ack := venueMigrationAcknowledgement(t, fixture, intentID, orderID, artifact.Version, "cancel-command-ack")
	if err := insertVenueMigrationObservation(ctx, pool, ack); err != nil {
		t.Fatal(err)
	}
	if err := insertVenueMigrationAcknowledgement(ctx, pool, fixture, intentID, orderID, artifact.Version, ack); err != nil {
		t.Fatal(err)
	}

	bindingID := economicid.DeterministicUUID("execution-order-binding", orderID.String())
	evidence := venueMigrationCancelEvidence(t, orderID, bindingID, ack.ExternalOrderID, artifact.Version)
	if err := insertVenueMigrationCancelCommand(
		ctx, pool, fixture, intentID, orderID, bindingID, artifact.Version, evidence,
	); err != nil {
		t.Fatalf("insert exact local cancellation command: %v", err)
	}
	var observationCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM venue_observations
		WHERE source_namespace LIKE '%cancel-request-v1'`).Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if observationCount != 0 {
		t.Fatalf("local cancellation command created %d provider observations", observationCount)
	}
}

func TestVenueAdapterMigrationRequiresRawTerminalAndFailureObservations(t *testing.T) {
	for name, state := range map[string]struct {
		providerState string
		outcome       venue.MappedOutcome
		eventKind     string
		nextState     string
		reasonCode    string
	}{
		"terminal": {
			providerState: "canceled", outcome: venue.OutcomeCancelled,
			eventKind: "order_cancelled", nextState: "cancelled", reasonCode: "provider_cancelled",
		},
		"unknown": {
			providerState: "new-v2-state", outcome: venue.OutcomeUnknownState,
			eventKind: "unknown_venue_state", nextState: "failed_reconciliation", reasonCode: "unknown_provider_state",
		},
		"contradiction": {
			providerState: "resting", outcome: venue.OutcomeContradiction,
			eventKind: "contradictory_venue_state", nextState: "failed_reconciliation", reasonCode: "provider_contradiction",
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, pool, base := newVenueAdapterMigrationPool(t)
			artifact, fixture, intentID, orderID := seedRoutedKalshiVenueOrder(t, ctx, pool, base, "state-"+name)
			ack := venueMigrationAcknowledgement(t, fixture, intentID, orderID, artifact.Version, "state-"+name+"-ack")
			if err := insertVenueMigrationObservation(ctx, pool, ack); err != nil {
				t.Fatal(err)
			}
			if err := insertVenueMigrationAcknowledgement(ctx, pool, fixture, intentID, orderID, artifact.Version, ack); err != nil {
				t.Fatal(err)
			}
			bindingID := economicid.DeterministicUUID("execution-order-binding", orderID.String())
			observation := venueMigrationStateObservation(
				t, fixture, intentID, orderID, artifact.Version, "state-"+name,
				state.providerState, state.outcome,
			)

			if err := insertVenueMigrationProviderEvent(
				ctx, pool, fixture, intentID, orderID, bindingID, artifact.Version, observation,
				state.eventKind, "working", state.nextState, state.reasonCode, observation.RawPayload,
			); err == nil || !strings.Contains(err.Error(), "exact prior raw observation") {
				t.Fatalf("provider %s event without raw observation error = %v", name, err)
			}
			if err := insertVenueMigrationObservation(ctx, pool, observation); err != nil {
				t.Fatal(err)
			}
			if err := insertVenueMigrationProviderEvent(
				ctx, pool, fixture, intentID, orderID, bindingID, artifact.Version, observation,
				state.eventKind, "working", state.nextState, state.reasonCode, observation.RawPayload,
			); err != nil {
				t.Fatalf("provider %s event with raw observation: %v", name, err)
			}
		})
	}
}

func TestVenueAdapterMigrationLeavesSimulationOrdersUnchanged(t *testing.T) {
	ctx, pool, fixture := newVenueAdapterMigrationPool(t)
	artifactID, schema, version, digest, canonical := simulationMigrationArtifact(t)
	if _, err := pool.Exec(ctx, `INSERT INTO simulation_policy_artifacts (
		id, schema_name, policy_version, sha256, canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`, artifactID, schema,
		version, digest, canonical, simulationMigrationTime(),
	); err != nil {
		t.Fatal(err)
	}
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture, "simulation-after-73")
	if err := insertMigrationLifecycleOrder(
		t, ctx, pool, fixture, intentID, "simulation-after-73", "simulation", version,
	); err != nil {
		t.Fatalf("schema-72 simulation order after migration 73: %v", err)
	}
}

func seedRoutedKalshiVenueOrder(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	base commonExecutionLifecycleMigrationFixture,
	key string,
) (venue.PolicyArtifact, venueAdapterMigrationFixture, uuid.UUID, uuid.UUID) {
	t.Helper()
	artifact := venueMigrationArtifact(t, venue.ProviderKalshi)
	if err := insertVenueMigrationArtifact(ctx, pool, artifact); err != nil {
		t.Fatal(err)
	}
	fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
		`{"kalshi_v2":{"outcome":"yes"}}`)
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, key)
	orderID, err := insertVenueMigrationOrder(ctx, pool, fixture, intentID, key, artifact.Version, "gtc")
	if err != nil {
		t.Fatal(err)
	}
	return artifact, fixture, intentID, orderID
}

func venueMigrationStateObservation(
	t *testing.T,
	fixture venueAdapterMigrationFixture,
	intentID, orderID uuid.UUID,
	policyVersion, sourceEventID, providerState string,
	outcome venue.MappedOutcome,
) *venue.Observation {
	t.Helper()
	input := venueObservationInput(fixture, intentID, orderID, policyVersion, sourceEventID)
	input.Kind = venue.ObservationOrderSnapshot
	input.ProviderState = providerState
	input.MappedOutcome = outcome
	input.SourceNamespace = "kalshi/portfolio/order-snapshots"
	providerPrice := decimal.RequireFromString(fixture.LimitPrice)
	input.ProviderPrice = &providerPrice
	input.ProviderBookSide = "bid"
	input.RawPayload = json.RawMessage(`{"order_id":"external-` + orderID.String() + `","status":"` + providerState + `"}`)
	observation, err := venue.NewObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func insertVenueMigrationProviderEventWithBinding(
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture venueAdapterMigrationFixture,
	intentID, orderID uuid.UUID,
	policyVersion string,
	observation *venue.Observation,
	kind, priorState, nextState, reasonCode string,
	evidence []byte,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	bindingID := economicid.DeterministicUUID("execution-order-binding", orderID.String())
	if _, err := tx.Exec(ctx, `INSERT INTO execution_order_bindings (
		id, order_id, account_id, venue, external_order_id, created_at
	) VALUES ($1,$2,$3,$4,$5,$6)`, bindingID, orderID, fixture.Common.AccountID,
		fixture.Provider, observation.ExternalOrderID, observation.ReceivedAt,
	); err != nil {
		return err
	}
	if err := insertVenueMigrationProviderEvent(
		ctx, tx, fixture, intentID, orderID, bindingID, policyVersion, observation,
		kind, priorState, nextState, reasonCode, evidence,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertVenueMigrationProviderEvent(
	ctx context.Context,
	database venueObservationExecer,
	fixture venueAdapterMigrationFixture,
	intentID, orderID, bindingID uuid.UUID,
	policyVersion string,
	observation *venue.Observation,
	kind, priorState, nextState, reasonCode string,
	evidence []byte,
) error {
	eventID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", string(fixture.Provider),
		observation.SourceNamespace, observation.SourceEventID, "",
	)
	_, err := database.Exec(ctx, `INSERT INTO execution_lifecycle_events (
		id, intent_id, order_id, binding_id, kind, observation_class, prior_state,
		next_state, account_id, environment, origin_type, origin_id,
		strategy_version_id, policy_kind, policy_version, quantity_delta, source,
		source_namespace, source_event_id, source_revision, source_at, received_at,
		actor, reason_code, evidence_bytes, evidence_sha256, evidence, created_at
	) VALUES ($1,$2,$3,$4,$5,'ordinary',$6,$7,$8,'paper_scored',
		'strategy_version','strategy-version-1','strategy-version-1','venue',$9,8,$10,$11,$12,$13,
		$14,$15,'venue-adapter',$16,$17,$18,convert_from($17,'UTF8')::JSONB,$15)`,
		eventID, intentID, orderID, bindingID, kind, priorState, nextState,
		fixture.Common.AccountID, policyVersion, fixture.Provider, observation.SourceNamespace,
		observation.SourceEventID, observation.SourceRevision, observation.SourceAt,
		observation.ReceivedAt, reasonCode, evidence, migrationLifecycleSHA(evidence),
	)
	return err
}

func venueMigrationCancelEvidence(
	t *testing.T,
	orderID, bindingID uuid.UUID,
	externalOrderID, policyVersion string,
) []byte {
	t.Helper()
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(struct {
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
		Schema: "venue-cancel-request-v1", OrderID: orderID.String(), Provider: "kalshi", Venue: "kalshi",
		PolicyVersion: policyVersion, ClientOrderID: orderID.String(), BindingID: bindingID.String(),
		ExternalOrderID: externalOrderID, Method: "DELETE",
		PathTemplate: "/portfolio/events/orders/{external_order_id}", RequestBody: "<empty>",
	})
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
}

func insertVenueMigrationCancelCommand(
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture venueAdapterMigrationFixture,
	intentID, orderID, bindingID uuid.UUID,
	policyVersion string,
	evidence []byte,
) error {
	sourceNamespace := "venue-adapter-policy-v1/kalshi/" + policyVersion + "/cancel-request-v1"
	sourceEventID := orderID.String() + "/cancel-request-v1"
	eventID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", "venue_command",
		sourceNamespace, sourceEventID, "",
	)
	at := venueMigrationTime().Add(3 * time.Hour)
	_, err := pool.Exec(ctx, `INSERT INTO execution_lifecycle_events (
		id, intent_id, order_id, binding_id, kind, observation_class, prior_state,
		next_state, account_id, environment, origin_type, origin_id,
		strategy_version_id, policy_kind, policy_version, quantity_delta, source,
		source_namespace, source_event_id, source_at, received_at, actor,
		reason_code, evidence_bytes, evidence_sha256, evidence, created_at
	) VALUES ($1,$2,$3,$4,'cancel_requested','ordinary','working','working',$5,'paper_scored',
		'strategy_version','strategy-version-1','strategy-version-1','venue',$6,8,'venue_command',
		$7,$8,$9,$9,'venue-adapter','cancel_requested',$10,$11,convert_from($10,'UTF8')::JSONB,$9)`,
		eventID, intentID, orderID, bindingID, fixture.Common.AccountID, policyVersion,
		sourceNamespace, sourceEventID, at, evidence, migrationLifecycleSHA(evidence),
	)
	return err
}
