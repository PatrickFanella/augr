package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func newStrategyCatalogMigrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool := newProjectionIntegrationPool(t, ctx).owner
	for _, migration := range []string{
		"000070_accounting_dual_run.up.sql", "000071_common_execution_lifecycle.up.sql",
		"000072_simulation_policy_artifacts.up.sql", "000073_venue_adapter_observations.up.sql",
		"000074_capital_margin_profiles.up.sql", "000075_venue_reconciliation.up.sql",
		"000076_dataset_manifests_quality.up.sql", "000077_strategy_catalog_experiments.up.sql",
	} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migration)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	return pool
}

func TestStrategyCatalogMigrationConcurrentFamilyRetriesConverge(t *testing.T) {
	ctx := context.Background()
	pool := newStrategyCatalogMigrationPool(t)
	family, evidence := migrationFamily(t)

	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsFound <- insertMigrationFamily(ctx, pool, family, evidence)
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Error(err)
		}
	}
	var families, events int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM strategy_families WHERE id=$1),
		(SELECT count(*) FROM strategy_catalog_lifecycle_events WHERE entity_kind='family' AND entity_id=$1)`, family.ID()).Scan(&families, &events); err != nil {
		t.Fatal(err)
	}
	if families != 1 || events != 1 {
		t.Fatalf("family graph counts=%d/%d want 1/1", families, events)
	}
}

func TestStrategyCatalogMigrationRejectsForgedAndMutableFamily(t *testing.T) {
	ctx := context.Background()
	pool := newStrategyCatalogMigrationPool(t)
	family, evidence := migrationFamily(t)
	if err := insertMigrationFamily(ctx, pool, family, evidence); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE strategy_families SET thesis=thesis WHERE id=$1`, family.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("family update error=%v, want append-only rejection", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, digest string
	var raw []byte
	if err := tx.QueryRow(ctx, `WITH identity AS (
		SELECT strategy_family_identity('forged-order','Forged order','Must fail canonical set ordering','["equity","crypto_spot"]'::jsonb) AS value
	) SELECT economic_deterministic_uuid('strategy-family','forged-order')::text,
		encode(digest(convert_to(value,'UTF8'),'sha256'),'hex'),convert_to(value,'UTF8') FROM identity`).Scan(&id, &digest, &raw); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO strategy_families(
		id,schema_name,slug,name,thesis,asset_classes,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'strategy-family-v1','forged-order','Forged order','Must fail canonical set ordering','["equity","crypto_spot"]'::jsonb,$2,$3,convert_from($3,'UTF8')::jsonb,$4)`,
		id, digest, raw, time.Date(2026, 8, 20, 20, 0, 0, 123456000, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `WITH identity AS (
		SELECT strategy_lifecycle_identity('family',$1,'registered','registered',$2) AS value
	) INSERT INTO strategy_catalog_lifecycle_events(
		id,schema_name,entity_kind,entity_id,event_kind,prior_state,next_state,evidence_sha256,sha256,canonical_bytes,canonical_json,created_at
	) SELECT economic_deterministic_uuid('strategy-catalog-lifecycle-event','family',$1,'registered',$2),
		'strategy-catalog-lifecycle-event-v1','family',$1,'registered','','registered',$2,
		encode(digest(convert_to(value,'UTF8'),'sha256'),'hex'),convert_to(value,'UTF8'),value::jsonb,$3 FROM identity`, id, digest,
		time.Date(2026, 8, 20, 20, 0, 0, 123456000, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil || !strings.Contains(err.Error(), "asset classes are not canonical") {
		t.Fatalf("forged family commit error=%v, want canonical ordering rejection", err)
	}
}

func migrationFamily(t *testing.T) (*strategycatalog.Family, *strategycatalog.LifecycleEvidence) {
	t.Helper()
	family, err := strategycatalog.NewFamily(strategycatalog.FamilyInput{
		Slug: "migration-family", Name: "Migration family", Thesis: "Qualify migration 77",
		AssetClasses: []instrument.AssetClass{instrument.AssetClassEquity, instrument.AssetClassCryptoSpot},
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := strategycatalog.NewInitialLifecycleEvidence(strategycatalog.EntityFamily, family.ID(), family.Digest())
	if err != nil {
		t.Fatal(err)
	}
	return family, evidence
}

func insertMigrationFamily(ctx context.Context, pool *pgxpool.Pool, family *strategycatalog.Family, evidence *strategycatalog.LifecycleEvidence) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	assetClasses, err := json.Marshal(family.AssetClasses())
	if err != nil {
		return err
	}
	createdAt := time.Date(2026, 8, 20, 20, 0, 0, 123456000, time.UTC)
	if _, err := tx.Exec(ctx, `INSERT INTO strategy_families(
		id,schema_name,slug,name,thesis,asset_classes,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'strategy-family-v1',$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::jsonb,$8) ON CONFLICT(id) DO NOTHING`,
		family.ID(), family.Slug(), family.Name(), family.Thesis(), string(assetClasses), family.Digest(), family.CanonicalBytes(), createdAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO strategy_catalog_lifecycle_events(
		id,schema_name,entity_kind,entity_id,event_kind,prior_state,next_state,evidence_sha256,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'strategy-catalog-lifecycle-event-v1',$2,$3,$4,'',$5,$6,$7,$8,convert_from($8,'UTF8')::jsonb,$9) ON CONFLICT(id) DO NOTHING`,
		evidence.ID(), evidence.EntityKind(), evidence.EntityID(), evidence.EventKind(), evidence.NextState(), evidence.EvidenceSHA256(), evidence.Digest(), evidence.CanonicalBytes(), createdAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
