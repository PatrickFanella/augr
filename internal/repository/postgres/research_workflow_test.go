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
	seedResearchWorkflowParents(t, ctx, pool, fixture)
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

func seedResearchWorkflowParents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture researchqualification.Fixture) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SET session_replication_role='replica'`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SET session_replication_role='origin'`) }()
	created := "2026-08-20 12:00:00+00"
	family := fixture.StrategyFamily
	_, err = conn.Exec(ctx, `WITH j AS (SELECT convert_from($4::bytea,'UTF8')::jsonb v) INSERT INTO strategy_families(id,schema_name,slug,name,thesis,asset_classes,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',j.v->>'slug',j.v->>'name',j.v->>'thesis',j.v->'asset_classes',$2,$4,j.v,$3::timestamptz FROM j`, family.ID(), family.Digest(), created, family.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	version := fixture.Parents.Version
	_, err = conn.Exec(ctx, `WITH j AS (SELECT convert_from($5::bytea,'UTF8')::jsonb v) INSERT INTO strategy_versions(id,schema_name,family_id,compiler_kind,compiler_version,source_commit,source_tree_sha256,config_schema,config_bytes,config,decision_contract,required_kind_count,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',(j.v->>'family_id')::uuid,j.v->>'compiler_kind',j.v->>'compiler_version',j.v->>'source_commit',j.v->>'source_tree_sha256',j.v->>'config_schema',$4,j.v->'config',j.v->>'decision_contract',jsonb_array_length(j.v->'required_dataset_kinds'),$2,$5,j.v,$3::timestamptz FROM j`, version.ID(), version.Digest(), created, version.Config(), version.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	manifest := fixture.Parents.Manifest
	_, err = conn.Exec(ctx, `WITH j AS (SELECT convert_from($4::bytea,'UTF8')::jsonb v) INSERT INTO dataset_manifests(id,schema_name,decision_cutoff,partition_count,observation_count,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',(j.v->>'decision_cutoff')::timestamptz,(j.v->>'partition_count')::int,(j.v->>'observation_count')::int,$2,$4,j.v,$3::timestamptz FROM j`, manifest.ID(), manifest.Digest(), created, manifest.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	policy := fixture.Parents.RobustnessPolicy
	_, err = conn.Exec(ctx, `WITH j AS (SELECT convert_from($4::bytea,'UTF8')::jsonb v) INSERT INTO robustness_policy_artifacts(id,schema_name,version,fold_count,purge_seconds,embargo_seconds,bootstrap_algorithm,bootstrap_seed,bootstrap_iterations,confidence_level,family_wise_alpha,multiple_testing_correction,max_largest_positive_share,max_top_decile_positive_share,max_perturbation_degradation,perturbation_count,decimal_scale,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',j.v->>'version',(j.v->>'fold_count')::int,(j.v->>'purge_seconds')::bigint,(j.v->>'embargo_seconds')::bigint,j.v->>'bootstrap_algorithm',(j.v->>'bootstrap_seed')::numeric,(j.v->>'bootstrap_iterations')::int,j.v->>'confidence_level',j.v->>'family_wise_alpha',j.v->>'multiple_testing_correction',j.v->>'max_largest_positive_share',j.v->>'max_top_decile_positive_share',j.v->>'max_perturbation_degradation',jsonb_array_length(j.v->'required_perturbations'),(j.v->>'decimal_scale')::int,$2,$4,j.v,$3::timestamptz FROM j`, policy.ID(), policy.Digest(), created, policy.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	robustFamily := fixture.Parents.RobustnessFamily
	_, err = conn.Exec(ctx, `WITH j AS (SELECT convert_from($4::bytea,'UTF8')::jsonb v) INSERT INTO robustness_search_families(id,schema_name,name,hypothesis_sha256,candidate_count,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',j.v->>'name',j.v->>'hypothesis_sha256',jsonb_array_length(j.v->'candidate_version_ids'),$2,$4,j.v,$3::timestamptz FROM j`, robustFamily.ID(), robustFamily.Digest(), created, robustFamily.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	assessment := fixture.Parents.Assessment
	_, err = conn.Exec(ctx, `WITH j AS (SELECT convert_from($4::bytea,'UTF8')::jsonb v) INSERT INTO statistical_robustness_assessments(id,schema_name,state,family_id,family_sha256,policy_id,policy_sha256,mode,candidate_count,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',j.v->>'state',(j.v->>'family_id')::uuid,j.v->>'family_sha256',(j.v->>'policy_id')::uuid,j.v->>'policy_sha256',j.v->>'mode',jsonb_array_length(j.v->'candidates'),$2,$4,j.v,$3::timestamptz FROM j`, assessment.ID(), assessment.Digest(), created, assessment.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	spec := fixture.Parents.Spec
	_, err = conn.Exec(ctx, `WITH j AS (SELECT convert_from($3::bytea,'UTF8')::jsonb v) INSERT INTO generated_strategy_specs(id,schema_name,family_id,family_sha256,spec_key,input_count,instrument_count,prohibition_count,property_count,example_count,normalized_row_count,sha256,canonical_bytes,canonical_json) SELECT $1,j.v->>'schema',(j.v->>'family_id')::uuid,j.v->>'family_sha256',j.v->>'spec_key',jsonb_array_length(j.v->'inputs'),jsonb_array_length(j.v->'universe'->'instruments'),jsonb_array_length(j.v->'prohibited_behaviors'),jsonb_array_length(j.v->'property_tests'),jsonb_array_length(j.v->'example_tests'),jsonb_array_length(j.v->'inputs')+jsonb_array_length(j.v->'universe'->'instruments')+jsonb_array_length(j.v->'prohibited_behaviors')+jsonb_array_length(j.v->'property_tests')+jsonb_array_length(j.v->'example_tests'),$2,$3,j.v FROM j`, spec.ID(), spec.Digest(), spec.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	receipt := fixture.Parents.Receipt
	_, err = conn.Exec(ctx, `WITH j AS (SELECT convert_from($3::bytea,'UTF8')::jsonb v) INSERT INTO generated_strategy_compilation_receipts(id,schema_name,state,family_id,family_sha256,spec_id,spec_sha256,version_id,version_sha256,compiler_kind,compiler_version,source_commit,source_tree_sha256,config_schema,decision_contract,config_sha256,sha256,canonical_bytes,canonical_json) SELECT $1,j.v->>'schema',j.v->>'state',(j.v->>'family_id')::uuid,j.v->>'family_sha256',(j.v->>'spec_id')::uuid,j.v->>'spec_sha256',(j.v->>'version_id')::uuid,j.v->>'version_sha256',j.v->>'compiler_kind',j.v->>'compiler_version',j.v->>'source_commit',j.v->>'source_tree_sha256',j.v->>'config_schema',j.v->>'decision_contract',j.v->>'config_sha256',$2,$3,j.v FROM j`, receipt.ID(), receipt.Digest(), receipt.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `SET session_replication_role='origin'`); err != nil {
		t.Fatal(err)
	}
}
