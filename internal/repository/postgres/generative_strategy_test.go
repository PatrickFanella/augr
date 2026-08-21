package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/generativestrategy"
	generatedqualification "github.com/PatrickFanella/get-rich-quick/internal/generativestrategy/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestGenerativeStrategyRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("GENERATIVE_STRATEGY_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set GENERATIVE_STRATEGY_QUALIFICATION_DB_URL to a dedicated empty schema-95 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var versionNumber, existing int
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&versionNumber); err != nil || versionNumber != 95 {
		t.Fatalf("version=%d/%v", versionNumber, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM generated_strategy_specs`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("existing=%d/%v", existing, err)
	}
	fixture, err := generatedqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := generativestrategy.NewSpec(fixture.Input)
	if err != nil {
		t.Fatal(err)
	}
	version, receipt, err := generativestrategy.Compile(spec, strings.Repeat("b", 40), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	catalog := NewStrategyCatalogRepo(pool)
	if _, err = catalog.RegisterStrategyFamily(ctx, fixture.Family); err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.RegisterStrategyVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	repo := NewGenerativeStrategyRepo(pool)
	for _, failed := range []string{"parent", "row", "receipt"} {
		repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, _, _, stageErr := repo.RegisterCompilation(ctx, spec, version, receipt); stageErr == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM generated_strategy_specs`).Scan(&existing); err != nil || existing != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, existing, err)
		}
	}
	repo.afterStage = nil
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, _, writeErr := NewGenerativeStrategyRepo(pool).RegisterCompilation(ctx, spec, version, receipt)
			errs <- writeErr
		}()
	}
	wait.Wait()
	close(errs)
	for writeErr := range errs {
		if writeErr != nil {
			t.Error(writeErr)
		}
	}
	loadedSpec, loadedVersion, loadedReceipt, err := repo.GetCompilation(ctx, spec.ID())
	if err != nil || loadedSpec.Digest() != spec.Digest() || loadedVersion.Digest() != version.Digest() || loadedReceipt.Digest() != receipt.Digest() {
		t.Fatalf("restart=%v/%v/%v/%v", loadedSpec, loadedVersion, loadedReceipt, err)
	}
	changedInput := fixture.Input
	changedInput.Sizing.Value = "0.11"
	changed, err := generativestrategy.NewSpec(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	changedVersion, changedReceipt, err := generativestrategy.Compile(changed, strings.Repeat("b", 40), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.RegisterStrategyVersion(ctx, changedVersion); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = repo.RegisterCompilation(ctx, changed, changedVersion, changedReceipt); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	invalidInput := fixture.Input
	invalidInput.ProhibitedBehaviors = invalidInput.ProhibitedBehaviors[1:]
	if invalid, invalidErr := generativestrategy.NewSpec(invalidInput); invalidErr == nil || invalid != nil {
		t.Fatalf("invalid spec emitted=%v/%v", invalid, invalidErr)
	}
	if invalidVersion, invalidReceipt, invalidErr := generativestrategy.Compile(nil, "invalid", "invalid"); invalidErr == nil || invalidVersion != nil || invalidReceipt != nil {
		t.Fatalf("invalid compile emitted=%v/%v/%v", invalidVersion, invalidReceipt, invalidErr)
	}
	if _, err = pool.Exec(ctx, `UPDATE generated_strategy_specs SET spec_key='forged' WHERE id=$1`, spec.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO generated_strategy_spec_rows(spec_id,kind,sequence,canonical_row) VALUES($1,'input',99,'{}')`, spec.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	var specs, rows, receipts int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM generated_strategy_specs),(SELECT count(*) FROM generated_strategy_spec_rows),(SELECT count(*) FROM generated_strategy_compilation_receipts)`).Scan(&specs, &rows, &receipts); err != nil || specs != 1 || rows != 17 || receipts != 1 {
		t.Fatalf("counts=%d/%d/%d err=%v", specs, rows, receipts, err)
	}
	t.Logf("family=%s spec=%s spec_sha=%s version=%s version_sha=%s receipt=%s receipt_sha=%s rows=%d", fixture.Family.ID(), spec.ID(), spec.Digest(), version.ID(), version.Digest(), receipt.ID(), receipt.Digest(), rows)
}
