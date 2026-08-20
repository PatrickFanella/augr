package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	evidencequalification "github.com/PatrickFanella/get-rich-quick/internal/evidencereview/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestEvidenceReviewRetainedQualification(t *testing.T) {
	url := os.Getenv("EVIDENCE_REVIEW_QUALIFICATION_DB_URL")
	if url == "" {
		t.Skip("set EVIDENCE_REVIEW_QUALIFICATION_DB_URL to a dedicated schema-97 database containing the OVR-602 fixture")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM evidence_review_cases`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("existing=%d/%v", count, err)
	}
	fixture, err := evidencequalification.Build()
	if err != nil {
		t.Fatal(err)
	}
	seedEvidenceReviewParents(t, ctx, pool, fixture)
	repo := NewEvidenceReviewRepo(pool)
	for _, failed := range []string{"case", "case_references", "review", "check", "check_reference", "summary", "summary_head", "summary_check", "summary_rows"} {
		repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, _, _, stageErr := repo.RegisterBundle(ctx, fixture.Case, fixture.Reviews, fixture.Summary, fixture.Parents); stageErr == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM evidence_review_cases`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, count, err)
		}
	}
	repo.afterStage = nil
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, _, writeErr := NewEvidenceReviewRepo(pool).RegisterBundle(ctx, fixture.Case, fixture.Reviews, fixture.Summary, fixture.Parents)
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
	loadedCase, loadedReviews, loadedSummary, err := repo.GetBundle(ctx, fixture.Case.ID(), fixture.Summary.ID(), fixture.Parents)
	if err != nil || loadedCase.Digest() != fixture.Case.Digest() || len(loadedReviews) != 2 || loadedSummary.Digest() != fixture.Summary.Digest() {
		t.Fatalf("restart=%v/%d/%v/%v", loadedCase, len(loadedReviews), loadedSummary, err)
	}
	if _, _, _, err = repo.RegisterBundle(ctx, fixture.Case, fixture.ConflictReviews, fixture.ConflictSummary, fixture.Parents); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	if _, _, _, err = repo.RegisterBundle(ctx, fixture.HeldCase, fixture.HeldReviews, fixture.HeldSummary, fixture.HeldParents); err != nil {
		t.Fatal(err)
	}
	if _, _, heldSummary, loadErr := repo.GetBundle(ctx, fixture.HeldCase.ID(), fixture.HeldSummary.ID(), fixture.HeldParents); loadErr != nil || heldSummary.Consensus() != "evidence_supported" || heldSummary.AuthoritativeOutcome() != "held" {
		t.Fatalf("held supported=%v/%v", heldSummary, loadErr)
	}
	if _, err = pool.Exec(ctx, `UPDATE evidence_review_checks SET check_state=check_state WHERE review_id=$1`, fixture.Reviews[0].ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO evidence_review_summary_checks(summary_id,sequence,check_name,canonical_row) VALUES($1,99,'forged','{}')`, fixture.Summary.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	var cases, caseRefs, reviews, checks, refs, summaries, heads, summaryChecks int
	err = pool.QueryRow(ctx, `SELECT(SELECT count(*) FROM evidence_review_cases),(SELECT count(*) FROM evidence_review_case_references),(SELECT count(*) FROM evidence_reviews),(SELECT count(*) FROM evidence_review_checks),(SELECT count(*) FROM evidence_review_check_references),(SELECT count(*) FROM evidence_review_summaries),(SELECT count(*) FROM evidence_review_summary_heads),(SELECT count(*) FROM evidence_review_summary_checks)`).Scan(&cases, &caseRefs, &reviews, &checks, &refs, &summaries, &heads, &summaryChecks)
	if err != nil || cases != 2 || caseRefs < 14 || reviews != 4 || checks != 24 || refs != 24 || summaries != 2 || heads != 4 || summaryChecks != 12 {
		t.Fatalf("counts=%d/%d/%d/%d/%d/%d/%d/%d err=%v", cases, caseRefs, reviews, checks, refs, summaries, heads, summaryChecks, err)
	}
	if _, err = pool.Exec(ctx, repositoryMigrationSQL(t, "000097_evidence_review_workflow.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back") {
		t.Fatalf("nonempty rollback=%v", err)
	}
	t.Logf("case=%s sha=%s reviews=%s/%s summary=%s sha=%s", fixture.Case.ID(), fixture.Case.Digest(), fixture.Reviews[0].ID(), fixture.Reviews[1].ID(), fixture.Summary.ID(), fixture.Summary.Digest())
}

func seedEvidenceReviewParents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture evidencequalification.Fixture) {
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
	created := "2026-08-20 19:00:00+00"
	deployment := fixture.Parents.Deployment
	_, err = conn.Exec(ctx, `WITH j AS(SELECT convert_from($4::bytea,'UTF8')::jsonb v) INSERT INTO strategy_deployments(id,schema_name,state,activation_authority,version_id,account_id,capital_binding_id,budget,schedule_cron,timezone_name,risk_policy_version,mode,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',j.v->>'state',j.v->>'activation_authority',(j.v->>'version_id')::uuid,(j.v->>'account_id')::uuid,(j.v->>'capital_binding_id')::uuid,(j.v->>'budget')::numeric,j.v->>'schedule_cron',j.v->>'timezone',j.v->>'risk_policy_version',j.v->>'mode',$2,$4,j.v,$3::timestamptz FROM j`, deployment.ID(), deployment.Digest(), created, deployment.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	policy := fixture.Parents.PromotionPolicy
	_, err = conn.Exec(ctx, `WITH j AS(SELECT convert_from($4::bytea,'UTF8')::jsonb v) INSERT INTO promotion_policy_artifacts(id,schema_name,version,pass_action,failure_action,required_gate_count,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',j.v->>'version',j.v->>'pass_action',j.v->>'failure_action',jsonb_array_length(j.v->'required_gates'),$2,$4,j.v,$3::timestamptz FROM j`, policy.ID(), policy.Digest(), created, policy.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range []interface {
		ID() uuid.UUID
		Digest() string
		CanonicalBytes() json.RawMessage
	}{fixture.Parents.PromotionDecision, fixture.HeldParents.PromotionDecision} {
		_, err = conn.Exec(ctx, `WITH j AS(SELECT convert_from($4::bytea,'UTF8')::jsonb v) INSERT INTO promotion_retirement_decisions(id,schema_name,deployment_id,deployment_sha256,version_id,assessment_id,assessment_sha256,family_id,robustness_policy_id,mode,policy_id,policy_sha256,prior_decision_id,prior_decision_sha256,candidate_sequence,prior_state,next_state,outcome,reason,observed_gate_count,sha256,canonical_bytes,canonical_json,created_at) SELECT $1,j.v->>'schema',(j.v->>'deployment_id')::uuid,j.v->>'deployment_sha256',(j.v->>'version_id')::uuid,(j.v->>'assessment_id')::uuid,j.v->>'assessment_sha256',(j.v->>'family_id')::uuid,(j.v->>'robustness_policy_id')::uuid,j.v->>'mode',(j.v->>'policy_id')::uuid,j.v->>'policy_sha256',NULLIF(j.v->>'prior_decision_id','')::uuid,j.v->>'prior_decision_sha256',0,j.v->>'prior_state',j.v->>'next_state',j.v->>'outcome',j.v->>'reason',jsonb_array_length(j.v->'observed_gates'),$2,$4,j.v,$3::timestamptz FROM j`, decision.ID(), decision.Digest(), created, decision.CanonicalBytes())
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = conn.Exec(ctx, `SET session_replication_role='origin'`); err != nil {
		t.Fatal(err)
	}
}
