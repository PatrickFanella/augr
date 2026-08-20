package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	researchqualification "github.com/PatrickFanella/get-rich-quick/internal/researchworkflow/qualification"
)

func TestResearchWorkflowRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("RESEARCH_WORKFLOW_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set RESEARCH_WORKFLOW_QUALIFICATION_DB_URL to a dedicated empty schema-96 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var existing int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM research_hypotheses`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("existing=%d/%v", existing, err)
	}
	fixture, err := researchqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	// The synthetic qualification graph exercises OVR-602 itself. Its immutable
	// parent artifacts are validated by the domain constructors; this dedicated
	// database intentionally omits their independently qualified persistence.
	if _, err = pool.Exec(ctx, `ALTER TABLE research_hypotheses DISABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `ALTER TABLE research_hypotheses ENABLE TRIGGER ALL`) })
	repo := NewResearchWorkflowRepo(pool)
	for _, failed := range []string{"hypothesis", "sources", "searches", "tests", "critic", "findings", "checks"} {
		repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, _, stageErr := repo.RegisterWorkflow(ctx, fixture.Hypothesis, fixture.ReadyCritic, fixture.Parents); stageErr == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM research_hypotheses`).Scan(&existing); err != nil || existing != 0 {
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
			_, _, writeErr := NewResearchWorkflowRepo(pool).RegisterWorkflow(ctx, fixture.Hypothesis, fixture.ReadyCritic, fixture.Parents)
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
	loadedHypothesis, loadedCritic, err := repo.GetWorkflow(ctx, fixture.Hypothesis.ID(), fixture.ReadyCritic.ID(), fixture.Parents)
	if err != nil || loadedHypothesis.Digest() != fixture.Hypothesis.Digest() || loadedCritic.Digest() != fixture.ReadyCritic.Digest() {
		t.Fatalf("restart=%v/%v/%v", loadedHypothesis, loadedCritic, err)
	}
	if _, _, err = repo.RegisterWorkflow(ctx, fixture.Hypothesis, fixture.ConflictCritic, fixture.Parents); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	if _, _, err = repo.RegisterWorkflow(ctx, fixture.Hypothesis, fixture.RejectCritic, fixture.Parents); err != nil {
		t.Fatal(err)
	}
	if _, loadedReject, loadErr := repo.GetWorkflow(ctx, fixture.Hypothesis.ID(), fixture.RejectCritic.ID(), fixture.Parents); loadErr != nil || loadedReject.Recommendation() != "reject" {
		t.Fatalf("rejection=%v/%v", loadedReject, loadErr)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE research_hypotheses ENABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE research_critic_checks SET check_state=check_state WHERE critic_id=$1`, fixture.ReadyCritic.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO research_hypothesis_tests(hypothesis_id,sequence,test_key,test_type,canonical_row) VALUES($1,99,'forged','leakage','{}')`, fixture.Hypothesis.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	var hypotheses, sources, sourceKeys, searches, results, tests, critics, findings, checks int
	err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM research_hypotheses),(SELECT count(*) FROM research_hypothesis_sources),(SELECT count(*) FROM research_hypothesis_source_manifest_keys),(SELECT count(*) FROM research_hypothesis_searches),(SELECT count(*) FROM research_hypothesis_search_results),(SELECT count(*) FROM research_hypothesis_tests),(SELECT count(*) FROM research_critics),(SELECT count(*) FROM research_critic_findings),(SELECT count(*) FROM research_critic_checks)`).Scan(&hypotheses, &sources, &sourceKeys, &searches, &results, &tests, &critics, &findings, &checks)
	if err != nil || hypotheses != 1 || sources != 2 || sourceKeys != 2 || searches != 2 || results != 4 || tests != 10 || critics != 2 || findings != 3 || checks != 12 {
		t.Fatalf("counts=%d/%d/%d/%d/%d/%d/%d/%d/%d err=%v", hypotheses, sources, sourceKeys, searches, results, tests, critics, findings, checks, err)
	}
	if _, err = pool.Exec(ctx, repositoryMigrationSQL(t, "000096_hypothesis_critic_workflows.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back") {
		t.Fatalf("nonempty rollback=%v", err)
	}
	t.Logf("hypothesis=%s sha=%s ready_critic=%s sha=%s reject_critic=%s sha=%s", fixture.Hypothesis.ID(), fixture.Hypothesis.Digest(), fixture.ReadyCritic.ID(), fixture.ReadyCritic.Digest(), fixture.RejectCritic.ID(), fixture.RejectCritic.Digest())
}
