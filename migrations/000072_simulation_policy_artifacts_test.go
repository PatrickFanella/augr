package migrations_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestSimulationPolicyMigrationDefinesImmutableContentAddressedArtifacts(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000072_simulation_policy_artifacts.up.sql"))
	for _, fragment := range []string{
		"lock table execution_orders in share row exclusive mode",
		"migration 72 cannot attach pre-existing simulation orders without canonical policy artifacts",
		"create table simulation_policy_artifacts",
		"policy_version text not null unique",
		"canonical_bytes bytea not null",
		"canonical_json jsonb not null",
		"sha256 = encode(digest(canonical_bytes, 'sha256'), 'hex')",
		"policy_version = schema_name || '@sha256:' || sha256",
		"canonical_json = convert_from(canonical_bytes, 'utf8')::jsonb",
		"canonical_json ->> 'schema' = schema_name",
		"economic_deterministic_uuid( 'simulation-policy-artifact', policy_version",
		"create function reject_simulation_policy_artifact_mutation",
		"create trigger trg_simulation_policy_artifacts_immutable",
		"create function validate_simulation_order_policy_artifact",
		"create trigger trg_execution_orders_simulation_policy",
		"new.policy_kind = 'simulation'",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Errorf("migration 72 is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"insert into orders",
		"insert into trades",
		"insert into positions",
		"grant insert",
		"grant update",
		"grant delete",
	} {
		if strings.Contains(upSQL, forbidden) {
			t.Errorf("migration 72 contains forbidden activation or legacy mutation %q", forbidden)
		}
	}
}

func TestSimulationPolicyMigrationDefinesEmptyOnlyRollback(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000072_simulation_policy_artifacts.down.sql"))
	for _, fragment := range []string{
		"lock table simulation_policy_artifacts, execution_orders in access exclusive mode",
		"cannot roll back migration 72 while simulation policy artifacts or orders exist",
		"drop trigger if exists trg_execution_orders_simulation_policy on execution_orders",
		"drop trigger if exists trg_simulation_policy_artifacts_immutable on simulation_policy_artifacts",
		"drop table simulation_policy_artifacts",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Errorf("migration 72 rollback is missing %q", fragment)
		}
	}
	if strings.Contains(downSQL, "drop table execution_orders") {
		t.Error("migration 72 rollback must preserve schema-71 execution orders")
	}
}

func TestSimulationPolicyMigrationAppliesAndEmptyRollbackPreservesSchema71(t *testing.T) {
	ctx, pool, _ := newCommonExecutionLifecycleMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000072_simulation_policy_artifacts.up.sql")); err != nil {
		t.Fatalf("apply migration 72: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000072_simulation_policy_artifacts.down.sql")); err != nil {
		t.Fatalf("empty rollback migration 72: %v", err)
	}

	var artifactTable, orderTable, intentTable *string
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass(current_schema() || '.simulation_policy_artifacts')::TEXT,
		to_regclass(current_schema() || '.execution_orders')::TEXT,
		to_regclass(current_schema() || '.execution_intents')::TEXT
	`).Scan(&artifactTable, &orderTable, &intentTable); err != nil {
		t.Fatal(err)
	}
	if artifactTable != nil || orderTable == nil || intentTable == nil {
		t.Fatalf("rollback tables = artifact:%v order:%v intent:%v", artifactTable, orderTable, intentTable)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000072_simulation_policy_artifacts.up.sql")); err != nil {
		t.Fatalf("reapply migration 72 after empty rollback: %v", err)
	}
}

func TestSimulationPolicyMigrationMatchesIdentityAndRejectsForgeryOrMutation(t *testing.T) {
	ctx, pool, _ := newSimulationPolicyMigrationPool(t)
	id, schema, version, digest, canonical := simulationMigrationArtifact()

	if _, err := pool.Exec(ctx, `INSERT INTO simulation_policy_artifacts (
		id, schema_name, policy_version, sha256, canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`,
		id, schema, version, digest, canonical, simulationMigrationTime(),
	); err != nil {
		t.Fatalf("insert valid artifact: %v", err)
	}

	var databaseID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT economic_deterministic_uuid(
		'simulation-policy-artifact', $1
	)`, version).Scan(&databaseID); err != nil {
		t.Fatal(err)
	}
	if databaseID != id {
		t.Fatalf("database artifact ID = %s, want Go %s", databaseID, id)
	}

	for name, statement := range map[string]string{
		"update": `UPDATE simulation_policy_artifacts SET canonical_json = canonical_json || '{"changed":true}'::JSONB WHERE id = '` + id.String() + `'`,
		"delete": `DELETE FROM simulation_policy_artifacts WHERE id = '` + id.String() + `'`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, statement); err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("artifact mutation error = %v, want append-only rejection", err)
			}
		})
	}

	for name, values := range map[string][]any{
		"wrong id":      {uuid.New(), schema, version, digest, canonical},
		"wrong digest":  {id, schema, version, strings.Repeat("0", 64), canonical},
		"wrong version": {id, schema, schema + "@sha256:" + strings.Repeat("0", 64), digest, canonical},
		"wrong json":    {id, schema, version, digest, []byte(`{"schema":"simulation-policy-v1","assets":[1]}`)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `INSERT INTO simulation_policy_artifacts (
				id, schema_name, policy_version, sha256, canonical_bytes, canonical_json, created_at
			) VALUES ($1,$2,$3,$4,$5,'{"schema":"simulation-policy-v1","assets":[]}'::JSONB,$6)`,
				values[0], values[1], values[2], values[3], values[4], simulationMigrationTime(),
			); err == nil {
				t.Fatal("forged artifact unexpectedly inserted")
			}
		})
	}
}

func TestSimulationPolicyMigrationRequiresArtifactForSimulationOrder(t *testing.T) {
	ctx, pool, fixture := newSimulationPolicyMigrationPool(t)
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture, "missing-artifact")
	_, _, version, _, _ := simulationMigrationArtifact()
	if err := insertMigrationLifecycleOrder(t, ctx, pool, fixture, intentID, "missing-artifact", "simulation", version); err == nil ||
		!strings.Contains(err.Error(), "registered simulation policy artifact") {
		t.Fatalf("simulation order without artifact error = %v", err)
	}
}

func TestSimulationPolicyMigrationPreconditionRejectsExistingSimulationOrderButAllowsVenueOrder(t *testing.T) {
	t.Run("simulation", func(t *testing.T) {
		ctx, pool, fixture := newCommonExecutionLifecycleMigrationPool(t)
		intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture, "preexisting-simulation")
		if err := insertMigrationLifecycleOrder(t, ctx, pool, fixture, intentID, "preexisting-simulation", "simulation", "legacy-simulation-policy"); err != nil {
			t.Fatalf("insert preexisting simulation order: %v", err)
		}
		if _, err := pool.Exec(ctx, readMigrationFile(t, "000072_simulation_policy_artifacts.up.sql")); err == nil ||
			!strings.Contains(err.Error(), "cannot attach pre-existing simulation orders") {
			t.Fatalf("migration precondition error = %v", err)
		}
	})

	t.Run("venue", func(t *testing.T) {
		ctx, pool, fixture := newCommonExecutionLifecycleMigrationPool(t)
		intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture, "preexisting-venue")
		if err := insertMigrationLifecycleOrder(t, ctx, pool, fixture, intentID, "preexisting-venue", "venue", "venue-policy-v1"); err != nil {
			t.Fatalf("insert preexisting venue order: %v", err)
		}
		if _, err := pool.Exec(ctx, readMigrationFile(t, "000072_simulation_policy_artifacts.up.sql")); err != nil {
			t.Fatalf("venue policy order blocked migration 72: %v", err)
		}
	})
}

func TestSimulationPolicyMigrationNonemptyRollbackRefuses(t *testing.T) {
	ctx, pool, _ := newSimulationPolicyMigrationPool(t)
	id, schema, version, digest, canonical := simulationMigrationArtifact()
	if _, err := pool.Exec(ctx, `INSERT INTO simulation_policy_artifacts (
		id, schema_name, policy_version, sha256, canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`,
		id, schema, version, digest, canonical, simulationMigrationTime(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000072_simulation_policy_artifacts.down.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot roll back migration 72") {
		t.Fatalf("nonempty rollback error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM simulation_policy_artifacts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rollback refusal preserved artifact count = %d, want 1", count)
	}
}

func newSimulationPolicyMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool, commonExecutionLifecycleMigrationFixture) {
	t.Helper()
	ctx, pool, fixture := newCommonExecutionLifecycleMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000072_simulation_policy_artifacts.up.sql")); err != nil {
		t.Fatalf("apply migration 72: %v", err)
	}
	return ctx, pool, fixture
}

func simulationMigrationArtifact() (uuid.UUID, string, string, string, []byte) {
	canonical := []byte(`{"schema":"simulation-policy-v1","assets":[]}`)
	digestBytes := sha256.Sum256(canonical)
	digest := hex.EncodeToString(digestBytes[:])
	schema := "simulation-policy-v1"
	version := schema + "@sha256:" + digest
	return economicid.DeterministicUUID("simulation-policy-artifact", version), schema, version, digest, canonical
}

func simulationMigrationTime() time.Time {
	return time.Date(2026, 8, 15, 20, 0, 0, 123456000, time.UTC)
}

func persistRiskApprovedMigrationLifecycle(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture commonExecutionLifecycleMigrationFixture,
	key string,
) uuid.UUID {
	t.Helper()
	intentID, _ := insertMigrationLifecycleProposal(t, ctx, pool, fixture, key)
	steps := []struct {
		kind, prior, next, source, eventID, actor, reason string
		evidence                                          []byte
	}{
		{"intent_allocated", "proposed", "allocated", "allocator", "allocation-" + key, "allocator", "allocated", []byte(`{"quantity":"8"}`)},
		{"risk_approved", "allocated", "risk_approved", "risk", "risk-" + key, "risk-engine", "approved", []byte(`{"approved":true}`)},
	}
	for index, step := range steps {
		eventID := economicid.DeterministicUUID(
			"execution-lifecycle-event", intentID.String(), "ordinary", step.source,
			"lifecycle/test", step.eventID, "",
		)
		at := fixture.DecisionAt.Add(time.Duration(index+1) * time.Second)
		if _, err := pool.Exec(ctx, migrationLifecycleEventSQL,
			eventID, intentID, step.kind, step.prior, step.next,
			fixture.AccountID, "paper_scored", "strategy_version", "strategy-version-1", "strategy-version-1",
			"8", at, step.source, "lifecycle/test", step.eventID,
			step.actor, step.reason, step.evidence, migrationLifecycleSHA(step.evidence),
		); err != nil {
			t.Fatalf("insert %s event: %v", step.kind, err)
		}
	}
	return intentID
}

func insertMigrationLifecycleOrder(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture commonExecutionLifecycleMigrationFixture,
	intentID uuid.UUID,
	key, policyKind, policyVersion string,
) error {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	orderID := economicid.DeterministicUUID("execution-order", intentID.String(), "order-"+key)
	routedAt := fixture.DecisionAt.Add(3 * time.Second)
	if _, err := tx.Exec(ctx, `INSERT INTO execution_orders (
		id, intent_id, account_id, instrument_id, idempotency_key, client_order_id,
		side, order_type, time_in_force, quantity, limit_price, venue,
		venue_contract_id, route_quote_snapshot_id, routed_at, policy_kind,
		policy_version, created_at
	) VALUES ($1,$2,$3,$4,$5,$1::TEXT,'buy','limit','day',8,10.25,'test-venue',$6,$7,$8,$9,$10,$8)`,
		orderID, intentID, fixture.AccountID, fixture.InstrumentID, "order-"+key,
		fixture.VenueContractID, fixture.QuoteSnapshotID, routedAt, policyKind, policyVersion,
	); err != nil {
		return err
	}
	evidence := []byte(`{"route":"test"}`)
	sourceEventID := "route-" + key
	eventID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", "router",
		"lifecycle/test", sourceEventID, "",
	)
	if _, err := tx.Exec(ctx, `INSERT INTO execution_lifecycle_events (
		id, intent_id, order_id, kind, observation_class, prior_state, next_state,
		account_id, environment, origin_type, origin_id, strategy_version_id,
		policy_kind, policy_version, quantity_delta, quote_snapshot_id, source,
		source_namespace, source_event_id, source_at, received_at, actor,
		reason_code, evidence_bytes, evidence_sha256, evidence, created_at
	) VALUES ($1,$2,$3,'order_routed','ordinary','risk_approved','routed',$4,'paper_scored',
		'strategy_version','strategy-version-1','strategy-version-1',$5,$6,8,$7,'router',
		'lifecycle/test',$8,$9,$9,'router','order_routed',$10,$11,convert_from($10,'UTF8')::JSONB,$9)`,
		eventID, intentID, orderID, fixture.AccountID, policyKind, policyVersion,
		fixture.QuoteSnapshotID, sourceEventID, routedAt, evidence, migrationLifecycleSHA(evidence),
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
