package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
)

func TestCapitalMarginPolicyMigrationDefinesLockedImmutableBoundary(t *testing.T) {
	rawSQL := readMigrationFile(t, "000074_capital_margin_profiles.up.sql")
	if first := firstExecutableMigrationSQL(rawSQL); !strings.HasPrefix(first, "lock table accounts in share row exclusive mode;") {
		t.Fatalf("migration 74 first executable SQL = %q", first)
	}
	upSQL := normalizeSQL(t, rawSQL)
	for _, fragment := range []string{
		"create function capital_margin_policy_v1_canonical_bytes",
		"create table capital_margin_policy_artifacts",
		"policy_version text not null unique",
		"canonical_bytes = capital_margin_policy_v1_canonical_bytes(canonical_json)",
		"economic_deterministic_uuid( 'capital-margin-policy-artifact', policy_version",
		"create trigger trg_capital_margin_policy_artifacts_immutable",
		"create table account_capital_policy_bindings",
		"foreign key (policy_artifact_id, policy_version)",
		"economic_deterministic_uuid( 'capital-policy-binding', account_id::text, policy_version",
		"create trigger trg_account_capital_policy_bindings_immutable",
		"create function validate_account_capital_policy_binding",
		"capital binding copied account facts do not match",
		"scored capital binding requires a matching finite promotion profile",
		"stress capital binding requires isolated stress-unlimited facts",
		"create trigger trg_account_capital_policy_bindings_validate",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Errorf("migration 74 is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"insert into accounts", "insert into capital_margin_policy_artifacts",
		"grant insert", "grant update", "grant delete", "enable_live_trading",
	} {
		if strings.Contains(upSQL, forbidden) {
			t.Errorf("migration 74 contains forbidden activation or seeding %q", forbidden)
		}
	}
}

func TestCapitalMarginPolicyMigrationDefinesEmptyOnlyRollback(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000074_capital_margin_profiles.down.sql"))
	for _, fragment := range []string{
		"lock table accounts, account_capital_policy_bindings, capital_margin_policy_artifacts in access exclusive mode",
		"cannot roll back migration 74 while capital policy artifacts or bindings exist",
		"drop trigger if exists trg_account_capital_policy_bindings_validate on account_capital_policy_bindings",
		"drop table account_capital_policy_bindings",
		"drop table capital_margin_policy_artifacts",
		"drop function if exists capital_margin_policy_v1_canonical_bytes(jsonb)",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Errorf("migration 74 rollback is missing %q", fragment)
		}
	}
	for _, preserved := range []string{"accounts", "simulation_policy_artifacts", "venue_adapter_policy_artifacts", "venue_observations"} {
		if strings.Contains(downSQL, "drop table "+preserved) {
			t.Errorf("migration 74 rollback must preserve %s", preserved)
		}
	}
}

func TestCapitalMarginPolicyMigrationAppliesAndEmptyRollbackPreservesSchema73(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.up.sql")); err != nil {
		t.Fatalf("apply migration 74: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.down.sql")); err != nil {
		t.Fatalf("empty rollback migration 74: %v", err)
	}

	var capitalArtifact, binding, venueArtifact, observation *string
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass(current_schema() || '.capital_margin_policy_artifacts')::TEXT,
		to_regclass(current_schema() || '.account_capital_policy_bindings')::TEXT,
		to_regclass(current_schema() || '.venue_adapter_policy_artifacts')::TEXT,
		to_regclass(current_schema() || '.venue_observations')::TEXT
	`).Scan(&capitalArtifact, &binding, &venueArtifact, &observation); err != nil {
		t.Fatal(err)
	}
	if capitalArtifact != nil || binding != nil || venueArtifact == nil || observation == nil {
		t.Fatalf("rollback tables = capital:%v binding:%v venue:%v observation:%v", capitalArtifact, binding, venueArtifact, observation)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.up.sql")); err != nil {
		t.Fatalf("reapply migration 74: %v", err)
	}
}

func TestCapitalMarginPolicyMigrationAcceptsOnlyReviewedArtifactAndRefusesNonemptyRollback(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.up.sql")); err != nil {
		t.Fatalf("apply migration 74: %v", err)
	}
	policy, err := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(time.Date(2026, 8, 20, 16, 0, 0, 123456000, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO capital_margin_policy_artifacts (
		id, schema_name, policy_version, sha256, canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`, artifact.ID, artifact.Schema,
		artifact.Version, artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt); err != nil {
		t.Fatalf("insert reviewed artifact: %v", err)
	}

	for _, statement := range []string{
		`UPDATE capital_margin_policy_artifacts SET canonical_json = canonical_json || '{"changed":true}'::JSONB WHERE id = '` + artifact.ID.String() + `'`,
		`DELETE FROM capital_margin_policy_artifacts WHERE id = '` + artifact.ID.String() + `'`,
	} {
		if _, err := pool.Exec(ctx, statement); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("artifact mutation error = %v", err)
		}
	}

	forgedBytes := append(append([]byte(nil), artifact.CanonicalBytes...), ' ')
	if _, err := pool.Exec(ctx, `INSERT INTO capital_margin_policy_artifacts (
		id, schema_name, policy_version, sha256, canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6)`, uuid.New(), artifact.Schema,
		artifact.Version+"-forged", artifact.SHA256, forgedBytes, artifact.CreatedAt); err == nil {
		t.Fatal("forged artifact unexpectedly inserted")
	}

	if _, err := pool.Exec(ctx, readMigrationFile(t, "000074_capital_margin_profiles.down.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot roll back migration 74") {
		t.Fatalf("nonempty rollback error = %v", err)
	}
}
