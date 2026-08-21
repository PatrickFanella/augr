package migrations_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/PatrickFanella/get-rich-quick/internal/venuerecon"
)

func TestVenueReconciliationMigrationDefinesLockedAppendOnlyGraph(t *testing.T) {
	raw := readMigrationFile(t, "000075_venue_reconciliation.up.sql")
	if first := firstExecutableMigrationSQL(raw); !strings.HasPrefix(first, "lock table projection_checkpoints") {
		t.Fatalf("migration 75 first executable SQL = %q", first)
	}
	sql := normalizeSQL(t, raw)
	for _, fragment := range []string{
		"create table venue_reconciliation_policy_artifacts",
		"create table venue_provider_snapshots",
		"create table venue_provider_snapshot_pages",
		"create table venue_provider_snapshot_positions",
		"create table venue_provider_snapshot_fills",
		"create table venue_local_snapshots",
		"create table venue_local_snapshot_transactions",
		"create table venue_local_snapshot_positions",
		"create table venue_local_snapshot_fills",
		"create table venue_local_snapshot_issues",
		"create table venue_reconciliation_runs",
		"create table venue_reconciliation_results",
		"create table venue_reconciliation_incidents",
		"create function reject_venue_reconciliation_mutation",
		"create function validate_venue_reconciliation_graph",
		"create function venue_reconciliation_result_identity",
		"create function venue_reconciliation_go_json_string",
		"deferrable initially deferred",
		"economic_deterministic_uuid('venue-reconciliation-policy-artifact'",
		"economic_deterministic_uuid('venue-reconciliation-stable-snapshot'",
		"economic_deterministic_uuid('venue-reconciliation-local-snapshot'",
		"economic_deterministic_uuid('venue-reconciliation-run'",
		"state_sha256 = encode(digest(state_bytes, 'sha256'), 'hex')",
		"canonical_json = convert_from(canonical_bytes, 'utf8')::jsonb",
		"r.clean = not exists",
		"after insert on venue_provider_snapshot_pages deferrable initially deferred",
		"after insert on venue_local_snapshot_fills deferrable initially deferred",
		"after insert on venue_reconciliation_results deferrable initially deferred",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 75 is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"insert into venue_reconciliation_policy_artifacts", "grant insert", "grant update", "grant delete",
		"current_policy", "create extension", "submit_order", "cancel_order", "enable_live_trading", "create schedule",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("migration 75 contains forbidden activation %q", forbidden)
		}
	}
}

func TestVenueReconciliationMigrationSerializesConcurrentEvidenceWriter(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.up.sql")); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	migration, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = migration.Rollback(ctx) }()
	if _, err := migration.Exec(ctx, readMigrationFile(t, "000075_venue_reconciliation.up.sql")); err != nil {
		t.Fatal(err)
	}
	qualifiedSnapshot := pgx.Identifier{schema, "venue_provider_snapshots"}.Sanitize()
	if _, err := pool.Exec(ctx, `INSERT INTO `+qualifiedSnapshot+` DEFAULT VALUES`); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("snapshot attempt during uncommitted migration error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		writer, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			done <- beginErr
			return
		}
		defer func() { _ = writer.Rollback(ctx) }()
		if _, lockErr := writer.Exec(ctx, `LOCK TABLE ledger_transactions IN ROW EXCLUSIVE MODE`); lockErr != nil {
			done <- lockErr
			return
		}
		done <- writer.Commit(ctx)
	}()
	select {
	case err := <-done:
		t.Fatalf("evidence writer escaped migration lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := migration.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serialized source writer: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+qualifiedSnapshot).Scan(&count); err != nil || count != 0 {
		t.Fatalf("serialized snapshot count=%d err=%v", count, err)
	}
}

func TestVenueReconciliationResultIdentityMatchesGoJSONEscaping(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000075_venue_reconciliation.up.sql")); err != nil {
		t.Fatal(err)
	}
	providerID, localID := uuid.New().String(), uuid.New().String()
	key, providerValue, delta := "fill:<A&B>\u2028line", "<provider>&\u2029", "0"
	identity := struct {
		PolicyVersion, ProviderSnapshotID, LocalSnapshotID string
		Key, Kind, Status, Reason, Severity                string
		ProviderValue, LocalValue, Delta                   *string
	}{
		PolicyVersion: "policy<&>", ProviderSnapshotID: providerID, LocalSnapshotID: localID,
		Key: key, Kind: "fill", Status: "drift", Reason: "fill_price_mismatch", Severity: "high",
		ProviderValue: &providerValue, Delta: &delta,
	}
	// Preserve the production field names and order used by deterministicResultID.
	expected, err := json.Marshal(struct {
		PolicyVersion      string  `json:"policy_version"`
		ProviderSnapshotID string  `json:"provider_snapshot_id"`
		LocalSnapshotID    string  `json:"local_snapshot_id"`
		Key                string  `json:"key"`
		Kind               string  `json:"kind"`
		Status             string  `json:"status"`
		Reason             string  `json:"reason"`
		Severity           string  `json:"severity"`
		ProviderValue      *string `json:"provider_value"`
		LocalValue         *string `json:"local_value"`
		Delta              *string `json:"delta"`
	}{
		identity.PolicyVersion, identity.ProviderSnapshotID, identity.LocalSnapshotID, identity.Key, identity.Kind,
		identity.Status, identity.Reason, identity.Severity, identity.ProviderValue, identity.LocalValue, identity.Delta,
	})
	if err != nil {
		t.Fatal(err)
	}
	var actual string
	if err := pool.QueryRow(ctx, `SELECT venue_reconciliation_result_identity($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		identity.PolicyVersion, identity.ProviderSnapshotID, identity.LocalSnapshotID, identity.Key, identity.Kind,
		identity.Status, identity.Reason, identity.Severity, identity.ProviderValue, identity.LocalValue, identity.Delta).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != string(expected) {
		t.Fatalf("SQL result identity differs from Go JSON\nSQL: %s\n Go: %s", actual, expected)
	}
}

func TestVenueReconciliationRollbackSerializesAndRefusesConcurrentEvidence(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000075_venue_reconciliation.up.sql")); err != nil {
		t.Fatal(err)
	}
	policy, err := venuerecon.NewPolicy(venuerecon.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(time.Date(2026, 8, 20, 18, 45, 0, 123456000, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(ctx) }()
	if _, err := writer.Exec(ctx, `INSERT INTO venue_reconciliation_policy_artifacts(
		id,schema_name,policy_version,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`, artifact.ID, artifact.Schema,
		artifact.Version, artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, downErr := pool.Exec(ctx, readMigrationFile(t, "000075_venue_reconciliation.down.sql"))
		done <- downErr
	}()
	select {
	case err := <-done:
		t.Fatalf("rollback escaped concurrent evidence writer: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil || !strings.Contains(err.Error(), "cannot roll back migration 75") {
		t.Fatalf("rollback after concurrent evidence error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM venue_reconciliation_policy_artifacts`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("preserved evidence count=%d err=%v", count, err)
	}
}

func TestVenueReconciliationMigrationAppliesRollsBackEmptyAndRefusesNonempty(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.up.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000075_venue_reconciliation.up.sql")); err != nil {
		t.Fatalf("apply migration 75: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000075_venue_reconciliation.down.sql")); err != nil {
		t.Fatalf("empty rollback migration 75: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000075_venue_reconciliation.up.sql")); err != nil {
		t.Fatalf("reapply migration 75: %v", err)
	}
	policy, err := venuerecon.NewPolicy(venuerecon.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(time.Date(2026, 8, 20, 18, 0, 0, 123456000, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO venue_reconciliation_policy_artifacts(
		id,schema_name,policy_version,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`, artifact.ID, artifact.Schema,
		artifact.Version, artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt); err != nil {
		t.Fatalf("insert reviewed policy: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE venue_reconciliation_policy_artifacts SET id=$1 WHERE id=$2`, uuid.New(), artifact.ID); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("policy mutation error = %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000075_venue_reconciliation.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 75") {
		t.Fatalf("nonempty rollback error = %v", err)
	}
}

func TestVenueReconciliationMigrationDefinesEmptyOnlyRollback(t *testing.T) {
	sql := normalizeSQL(t, readMigrationFile(t, "000075_venue_reconciliation.down.sql"))
	for _, fragment := range []string{
		"in access exclusive mode",
		"cannot roll back migration 75 while venue reconciliation evidence exists",
		"drop table venue_reconciliation_incidents",
		"drop table venue_reconciliation_policy_artifacts",
		"drop function validate_venue_reconciliation_graph()",
		"drop function venue_reconciliation_result_identity(text,text,text,text,text,text,text,text,text,text,text)",
		"drop function venue_reconciliation_go_json_string(text)",
		"drop function reject_venue_reconciliation_mutation()",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration 75 rollback is missing %q", fragment)
		}
	}
	for _, preserved := range []string{"projection_checkpoints", "execution_fills", "venue_observations", "ledger_transactions"} {
		if strings.Contains(sql, "drop table "+preserved) {
			t.Errorf("migration 75 rollback must preserve %s", preserved)
		}
	}
}
