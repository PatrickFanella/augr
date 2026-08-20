package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
)

type RobustnessRepo struct {
	pool *pgxpool.Pool
}

func NewRobustnessRepo(pool *pgxpool.Pool) *RobustnessRepo { return &RobustnessRepo{pool: pool} }

func (repo *RobustnessRepo) RegisterPolicy(ctx context.Context, value *robustness.Policy) (*robustness.Policy, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: robustness policy is required")
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO robustness_policy_artifacts(id,schema_name,version,fold_count,purge_seconds,embargo_seconds,bootstrap_algorithm,
		bootstrap_seed,bootstrap_iterations,confidence_level,family_wise_alpha,multiple_testing_correction,max_largest_positive_share,
		max_top_decile_positive_share,max_perturbation_degradation,perturbation_count,decimal_scale,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,'robustness-policy-v1',$2,$3,$4,$5,$6,$7,$8,$9,$10,'holm_bonferroni',$11,$12,$13,$14,$15,$16,$17,convert_from($17,'UTF8')::jsonb,$18)
		ON CONFLICT(id) DO NOTHING`, value.ID(), value.Version(), value.FoldCount(), value.PurgeSeconds(), value.EmbargoSeconds(), value.BootstrapAlgorithm(),
		value.BootstrapSeed(), value.BootstrapIterations(), value.ConfidenceLevel(), value.FamilyWiseAlpha(), value.MaxLargestPositiveShare(),
		value.MaxTopDecilePositiveShare(), value.MaxPerturbationDegradation(), len(value.RequiredPerturbations()), value.DecimalScale(), value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert robustness policy", err)
	}
	for i, kind := range value.RequiredPerturbations() {
		if _, err = tx.Exec(ctx, `INSERT INTO robustness_policy_perturbations(policy_id,sequence,kind) VALUES($1,$2,$3) ON CONFLICT(policy_id,sequence) DO NOTHING`, value.ID(), i, kind); err != nil {
			return nil, evaluationWriteError("insert robustness perturbation", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit robustness policy", err)
	}
	got, err := repo.GetPolicy(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() {
		return nil, fmt.Errorf("postgres: robustness policy conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *RobustnessRepo) GetPolicy(ctx context.Context, id uuid.UUID) (*robustness.Policy, error) {
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM robustness_policy_artifacts WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return robustness.PolicyFromCanonical(id, digest, raw)
}

func (repo *RobustnessRepo) RegisterFamily(ctx context.Context, value *robustness.Family) (*robustness.Family, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: robustness family is required")
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO robustness_search_families(id,schema_name,name,hypothesis_sha256,candidate_count,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,'robustness-search-family-v1',$2,$3,$4,$5,$6,convert_from($6,'UTF8')::jsonb,$7) ON CONFLICT(id) DO NOTHING`, value.ID(), value.Name(), value.HypothesisSHA256(), len(value.CandidateVersionIDs()), value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert robustness family", err)
	}
	for i, id := range value.CandidateVersionIDs() {
		if _, err = tx.Exec(ctx, `INSERT INTO robustness_search_family_candidates(family_id,sequence,version_id) VALUES($1,$2,$3) ON CONFLICT(family_id,sequence) DO NOTHING`, value.ID(), i, id); err != nil {
			return nil, evaluationWriteError("insert robustness family candidate", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit robustness family", err)
	}
	got, err := repo.GetFamily(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() {
		return nil, fmt.Errorf("postgres: robustness family conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *RobustnessRepo) GetFamily(ctx context.Context, id uuid.UUID) (*robustness.Family, error) {
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM robustness_search_families WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return robustness.FamilyFromCanonical(id, digest, raw)
}

func (repo *RobustnessRepo) RecordAssessment(ctx context.Context, value *robustness.Assessment) (*robustness.Assessment, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: robustness assessment is required")
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created := databaseNow()
	candidates := value.Candidates()
	_, err = tx.Exec(ctx, `INSERT INTO statistical_robustness_assessments(id,schema_name,state,family_id,family_sha256,policy_id,policy_sha256,mode,candidate_count,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,'statistical-robustness-assessment-v1','completed',$2,$3,$4,$5,$6,$7,$8,$9,convert_from($9,'UTF8')::jsonb,$10) ON CONFLICT(id) DO NOTHING`, value.ID(), value.FamilyID(), value.FamilyDigest(), value.PolicyID(), value.PolicyDigest(), value.Mode(), len(candidates), value.Digest(), value.CanonicalBytes(), created)
	if err != nil {
		return nil, evaluationWriteError("insert robustness assessment", err)
	}
	for _, c := range candidates {
		_, err = tx.Exec(ctx, `INSERT INTO robustness_assessment_candidates(assessment_id,sequence,version_id,fold_count,statistic_count,gate_count) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(assessment_id,sequence) DO NOTHING`, value.ID(), c.Sequence, c.VersionID, len(c.Folds), len(c.Statistics), len(c.Gates))
		if err != nil {
			return nil, evaluationWriteError("insert robustness candidate", err)
		}
		for _, f := range c.Folds {
			start, _ := time.Parse("2006-01-02T15:04:05.000000Z", f.TrainStart)
			end, _ := time.Parse("2006-01-02T15:04:05.000000Z", f.TrainEnd)
			ts, _ := time.Parse("2006-01-02T15:04:05.000000Z", f.TestStart)
			te, _ := time.Parse("2006-01-02T15:04:05.000000Z", f.TestEnd)
			_, err = tx.Exec(ctx, `INSERT INTO robustness_assessment_folds(assessment_id,candidate_sequence,sequence,train_start,train_end,test_start,test_end,scenario_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(assessment_id,candidate_sequence,sequence) DO NOTHING`, value.ID(), c.Sequence, f.Sequence, start, end, ts, te, 1+len(f.Perturbations))
			if err != nil {
				return nil, evaluationWriteError("insert robustness fold", err)
			}
			scenarios := append([]robustness.ScenarioEvidence{f.Baseline}, f.Perturbations...)
			for i, s := range scenarios {
				if _, err = tx.Exec(ctx, `INSERT INTO robustness_assessment_scenarios(assessment_id,candidate_sequence,fold_sequence,sequence,kind,severity,report_id,report_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(assessment_id,candidate_sequence,fold_sequence,sequence) DO NOTHING`, value.ID(), c.Sequence, f.Sequence, i, s.Kind, s.Severity, s.ReportID, s.ReportSHA256); err != nil {
					return nil, evaluationWriteError("insert robustness scenario", err)
				}
			}
		}
		for i, s := range c.Statistics {
			if _, err = tx.Exec(ctx, `INSERT INTO robustness_statistics(assessment_id,candidate_sequence,sequence,name,state,value,unit,reason,description) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(assessment_id,candidate_sequence,sequence) DO NOTHING`, value.ID(), c.Sequence, i, s.Name, s.State, s.Value, s.Unit, s.Reason, s.Description); err != nil {
				return nil, evaluationWriteError("insert robustness statistic", err)
			}
		}
		for i, g := range c.Gates {
			if _, err = tx.Exec(ctx, `INSERT INTO robustness_gates(assessment_id,candidate_sequence,sequence,name,state,threshold,observed,reason,description) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(assessment_id,candidate_sequence,sequence) DO NOTHING`, value.ID(), c.Sequence, i, g.Name, g.State, g.Threshold, g.Observed, g.Reason, g.Description); err != nil {
				return nil, evaluationWriteError("insert robustness gate", err)
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit robustness assessment", err)
	}
	got, err := repo.GetAssessment(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: robustness assessment conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *RobustnessRepo) GetAssessment(ctx context.Context, id uuid.UUID) (*robustness.Assessment, error) {
	var digest string
	var raw []byte
	var familyID, policyID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,family_id,policy_id FROM statistical_robustness_assessments WHERE id=$1`, id).Scan(&digest, &raw, &familyID, &policyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	family, err := repo.GetFamily(ctx, familyID)
	if err != nil {
		return nil, err
	}
	policy, err := repo.GetPolicy(ctx, policyID)
	if err != nil {
		return nil, err
	}
	rows, err := repo.pool.Query(ctx, `SELECT DISTINCT report_id FROM robustness_assessment_scenarios WHERE assessment_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := map[uuid.UUID]*evaluation.Report{}
	evalRepo := NewEvaluationRepo(repo.pool)
	for rows.Next() {
		var reportID uuid.UUID
		if err = rows.Scan(&reportID); err != nil {
			return nil, err
		}
		reports[reportID], err = evalRepo.GetEvaluation(ctx, reportID)
		if err != nil {
			return nil, err
		}
	}
	return robustness.AssessmentFromCanonical(id, digest, raw, family, policy, reports)
}
