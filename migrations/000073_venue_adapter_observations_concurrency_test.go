package migrations_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

func TestVenueAdapterMigrationConcurrentExactArtifactAndObservationRetriesConverge(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact := venueMigrationArtifact(t, venue.ProviderKalshi)
	runVenueMigrationWriters(t, 8, func() error {
		_, err := pool.Exec(ctx, `INSERT INTO venue_adapter_policy_artifacts (
			id, schema_name, provider, venue, policy_version, sha256,
			canonical_bytes, canonical_json, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::JSONB,$8)
		ON CONFLICT (policy_version) DO NOTHING`, artifact.ID, artifact.Schema,
			artifact.Provider, artifact.Venue, artifact.Version, artifact.SHA256,
			[]byte(artifact.CanonicalBytes), artifact.CreatedAt,
		)
		return err
	})

	fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
		`{"kalshi_v2":{"outcome":"yes"}}`)
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "concurrent-observation")
	orderID, err := insertVenueMigrationOrder(
		ctx, pool, fixture, intentID, "concurrent-observation", artifact.Version, "gtc",
	)
	if err != nil {
		t.Fatal(err)
	}
	observation := venueMigrationAcknowledgement(
		t, fixture, intentID, orderID, artifact.Version, "concurrent-observation-ack",
	)
	runVenueMigrationWriters(t, 8, func() error {
		_, err := pool.Exec(
			ctx, venueMigrationObservationInsertSQL+" ON CONFLICT DO NOTHING",
			venueMigrationObservationArgs(observation)...,
		)
		return err
	})

	var artifactCount, observationCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM venue_adapter_policy_artifacts WHERE policy_version=$1),
		(SELECT COUNT(*) FROM venue_observations WHERE id=$2)`,
		artifact.Version, observation.ID,
	).Scan(&artifactCount, &observationCount); err != nil {
		t.Fatal(err)
	}
	if artifactCount != 1 || observationCount != 1 {
		t.Fatalf("concurrent exact retry counts = artifact:%d observation:%d, want 1/1", artifactCount, observationCount)
	}

	changed := *observation
	changed.SourceRevision = "changed"
	if err := insertVenueMigrationObservation(ctx, pool, &changed); err == nil {
		t.Fatal("changed observation retry unexpectedly replaced the converged fact")
	}
}

func TestVenueAdapterMigrationUpRaceCannotLeaveUnregisteredVenueOrder(t *testing.T) {
	t.Run("writer wins lock", func(t *testing.T) {
		ctx, pool, base := newSimulationPolicyMigrationPool(t)
		fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
			`{"kalshi_v2":{"outcome":"yes"}}`)
		intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "up-race-writer")
		artifact := venueMigrationArtifact(t, venue.ProviderKalshi)

		writer, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = writer.Rollback(ctx) }()
		if _, err := insertVenueMigrationOrderInTx(
			ctx, writer, fixture, intentID, "up-race-writer", artifact.Version, "gtc",
		); err != nil {
			t.Fatal(err)
		}

		migrationResult := runVenueMigrationSQLAsync(ctx, pool, readMigrationFile(t, "000073_venue_adapter_observations.up.sql"))
		assertVenueMigrationCallBlocked(t, migrationResult)
		if err := writer.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-migrationResult; err == nil || !strings.Contains(err.Error(), "pre-existing venue orders") {
			t.Fatalf("migration after schema-72 writer error = %v", err)
		}

		var orderCount int
		var artifactTable *string
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT COUNT(*) FROM execution_orders WHERE policy_kind='venue'),
			to_regclass(current_schema() || '.venue_adapter_policy_artifacts')::TEXT`,
		).Scan(&orderCount, &artifactTable); err != nil {
			t.Fatal(err)
		}
		if orderCount != 1 || artifactTable != nil {
			t.Fatalf("writer-first race left orders=%d artifact_table=%v", orderCount, artifactTable)
		}
	})

	t.Run("migration wins lock", func(t *testing.T) {
		ctx, pool, base := newSimulationPolicyMigrationPool(t)
		fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
			`{"kalshi_v2":{"outcome":"yes"}}`)
		intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "up-race-migration")
		artifact := venueMigrationArtifact(t, venue.ProviderKalshi)

		migration, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = migration.Rollback(ctx) }()
		if _, err := migration.Exec(ctx, readMigrationFile(t, "000073_venue_adapter_observations.up.sql")); err != nil {
			t.Fatal(err)
		}
		writerResult := make(chan error, 1)
		go func() {
			_, writerErr := insertVenueMigrationOrder(
				ctx, pool, fixture, intentID, "up-race-migration", artifact.Version, "gtc",
			)
			writerResult <- writerErr
		}()
		assertVenueMigrationCallBlocked(t, writerResult)
		if err := migration.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-writerResult; err == nil || !strings.Contains(err.Error(), "registered same-venue") {
			t.Fatalf("schema-72 writer after migration lock error = %v", err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM execution_orders WHERE policy_kind='venue'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("migration-first race left %d unregistered venue orders", count)
		}
	})
}

func TestVenueAdapterMigrationDownRaceRefusesCommittedFacts(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
		`{"kalshi_v2":{"outcome":"yes"}}`)
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "down-race-writer")
	artifact := venueMigrationArtifact(t, venue.ProviderKalshi)

	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(ctx) }()
	if err := insertVenueMigrationArtifact(ctx, writer, artifact); err != nil {
		t.Fatal(err)
	}
	if _, err := insertVenueMigrationOrderInTx(
		ctx, writer, fixture, intentID, "down-race-writer", artifact.Version, "gtc",
	); err != nil {
		t.Fatal(err)
	}

	downResult := runVenueMigrationSQLAsync(ctx, pool, readMigrationFile(t, "000073_venue_adapter_observations.down.sql"))
	assertVenueMigrationCallBlocked(t, downResult)
	if err := writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-downResult; err == nil || !strings.Contains(err.Error(), "cannot roll back migration 73") {
		t.Fatalf("down migration after concurrent fact error = %v", err)
	}

	var orderCount, artifactCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM execution_orders WHERE policy_kind='venue'),
		(SELECT COUNT(*) FROM venue_adapter_policy_artifacts)`,
	).Scan(&orderCount, &artifactCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 || artifactCount != 1 {
		t.Fatalf("failed down race preserved orders=%d artifacts=%d, want 1/1", orderCount, artifactCount)
	}
}

func runVenueMigrationWriters(t *testing.T, count int, write func() error) {
	t.Helper()
	start := make(chan struct{})
	errors := make(chan error, count)
	var writers sync.WaitGroup
	writers.Add(count)
	for range count {
		go func() {
			defer writers.Done()
			<-start
			errors <- write()
		}()
	}
	close(start)
	writers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Errorf("concurrent migration writer: %v", err)
		}
	}
}

func runVenueMigrationSQLAsync(ctx context.Context, pool *pgxpool.Pool, sql string) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, sql)
		result <- err
	}()
	return result
}

func assertVenueMigrationCallBlocked(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("concurrent migration call completed before lock release: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
}
