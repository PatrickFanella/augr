package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
)

type RobustnessRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

func NewRobustnessRepo(pool *pgxpool.Pool) *RobustnessRepo { return &RobustnessRepo{pool: pool} }

var _ robustness.Store = (*RobustnessRepo)(nil)

func (repo *RobustnessRepo) GetEvaluation(ctx context.Context, id uuid.UUID) (*evaluation.Report, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: robustness repository is required")
	}
	return NewEvaluationRepo(repo.pool).GetEvaluation(ctx, id)
}

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
	if err = repo.stage("robustness_policy"); err != nil {
		return nil, err
	}
	for i, kind := range value.RequiredPerturbations() {
		if _, err = tx.Exec(ctx, `INSERT INTO robustness_policy_perturbations(policy_id,sequence,kind) VALUES($1,$2,$3) ON CONFLICT(policy_id,sequence) DO NOTHING`, value.ID(), i, kind); err != nil {
			return nil, evaluationWriteError("insert robustness perturbation", err)
		}
		if err = repo.stage("robustness_policy_perturbation"); err != nil {
			return nil, err
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
	if err = repo.stage("robustness_family"); err != nil {
		return nil, err
	}
	for i, id := range value.CandidateVersionIDs() {
		if _, err = tx.Exec(ctx, `INSERT INTO robustness_search_family_candidates(family_id,sequence,version_id) VALUES($1,$2,$3) ON CONFLICT(family_id,sequence) DO NOTHING`, value.ID(), i, id); err != nil {
			return nil, evaluationWriteError("insert robustness family candidate", err)
		}
		if err = repo.stage("robustness_family_candidate"); err != nil {
			return nil, err
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
	if err = repo.stage("robustness_assessment"); err != nil {
		return nil, err
	}
	for _, c := range candidates {
		_, err = tx.Exec(ctx, `INSERT INTO robustness_assessment_candidates(assessment_id,sequence,version_id,fold_count,statistic_count,gate_count) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(assessment_id,sequence) DO NOTHING`, value.ID(), c.Sequence, c.VersionID, len(c.Folds), len(c.Statistics), len(c.Gates))
		if err != nil {
			return nil, evaluationWriteError("insert robustness candidate", err)
		}
		if err = repo.stage("robustness_candidate"); err != nil {
			return nil, err
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
			if err = repo.stage("robustness_fold"); err != nil {
				return nil, err
			}
			scenarios := append([]robustness.ScenarioEvidence{f.Baseline}, f.Perturbations...)
			for i, s := range scenarios {
				if _, err = tx.Exec(ctx, `INSERT INTO robustness_assessment_scenarios(assessment_id,candidate_sequence,fold_sequence,sequence,kind,severity,report_id,report_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(assessment_id,candidate_sequence,fold_sequence,sequence) DO NOTHING`, value.ID(), c.Sequence, f.Sequence, i, s.Kind, s.Severity, s.ReportID, s.ReportSHA256); err != nil {
					return nil, evaluationWriteError("insert robustness scenario", err)
				}
				if err = repo.stage("robustness_scenario"); err != nil {
					return nil, err
				}
			}
		}
		for i, s := range c.Statistics {
			if _, err = tx.Exec(ctx, `INSERT INTO robustness_statistics(assessment_id,candidate_sequence,sequence,name,state,value,unit,reason,description) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(assessment_id,candidate_sequence,sequence) DO NOTHING`, value.ID(), c.Sequence, i, s.Name, s.State, s.Value, s.Unit, s.Reason, s.Description); err != nil {
				return nil, evaluationWriteError("insert robustness statistic", err)
			}
			if err = repo.stage("robustness_statistic"); err != nil {
				return nil, err
			}
		}
		for i, g := range c.Gates {
			if _, err = tx.Exec(ctx, `INSERT INTO robustness_gates(assessment_id,candidate_sequence,sequence,name,state,threshold,observed,reason,description) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(assessment_id,candidate_sequence,sequence) DO NOTHING`, value.ID(), c.Sequence, i, g.Name, g.State, g.Threshold, g.Observed, g.Reason, g.Description); err != nil {
				return nil, evaluationWriteError("insert robustness gate", err)
			}
			if err = repo.stage("robustness_gate"); err != nil {
				return nil, err
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
	value, err := robustness.AssessmentFromCanonical(id, digest, raw, family, policy, reports)
	if err != nil {
		return nil, err
	}
	normalized, err := repo.loadNormalizedCandidates(ctx, id)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(normalized, value.Candidates()) {
		return nil, fmt.Errorf("postgres: normalized robustness assessment %s does not reconstruct", id)
	}
	return value, nil
}

func (repo *RobustnessRepo) ListFamilyAssessments(ctx context.Context, familyID uuid.UUID, limit, offset int) ([]*robustness.Assessment, error) {
	return repo.listAssessments(ctx, `SELECT id FROM statistical_robustness_assessments WHERE family_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, familyID, limit, offset)
}

func (repo *RobustnessRepo) ListCandidateAssessments(ctx context.Context, versionID uuid.UUID, limit, offset int) ([]*robustness.Assessment, error) {
	return repo.listAssessments(ctx, `SELECT a.id FROM statistical_robustness_assessments a JOIN robustness_assessment_candidates c ON c.assessment_id=a.id WHERE c.version_id=$1 ORDER BY a.created_at,a.id LIMIT $2 OFFSET $3`, versionID, limit, offset)
}

func (repo *RobustnessRepo) ListReportAssessments(ctx context.Context, reportID uuid.UUID, limit, offset int) ([]*robustness.Assessment, error) {
	return repo.listAssessments(ctx, `SELECT a.id FROM statistical_robustness_assessments a WHERE EXISTS(SELECT 1 FROM robustness_assessment_scenarios s WHERE s.assessment_id=a.id AND s.report_id=$1) ORDER BY a.created_at,a.id LIMIT $2 OFFSET $3`, reportID, limit, offset)
}

func (repo *RobustnessRepo) listAssessments(ctx context.Context, query string, parent uuid.UUID, limit, offset int) ([]*robustness.Assessment, error) {
	if repo == nil || repo.pool == nil || parent == uuid.Nil || limit <= 0 || limit > 1000 || offset < 0 {
		return nil, fmt.Errorf("postgres: list robustness assessments: valid parent and pagination are required")
	}
	rows, err := repo.pool.Query(ctx, query, parent, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	values := make([]*robustness.Assessment, 0, len(ids))
	for _, id := range ids {
		value, err := repo.GetAssessment(ctx, id)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (repo *RobustnessRepo) stage(name string) error {
	if repo.afterStage == nil {
		return nil
	}
	return repo.afterStage(name)
}

func (repo *RobustnessRepo) loadNormalizedCandidates(ctx context.Context, assessmentID uuid.UUID) ([]robustness.CandidateEvidence, error) {
	rows, err := repo.pool.Query(ctx, `SELECT sequence,version_id::text FROM robustness_assessment_candidates WHERE assessment_id=$1 ORDER BY sequence`, assessmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]robustness.CandidateEvidence, 0)
	for rows.Next() {
		var candidate robustness.CandidateEvidence
		if err := rows.Scan(&candidate.Sequence, &candidate.VersionID); err != nil {
			return nil, err
		}
		candidate.Folds, err = repo.loadNormalizedFolds(ctx, assessmentID, candidate.Sequence)
		if err != nil {
			return nil, err
		}
		candidate.Statistics, err = repo.loadNormalizedStatistics(ctx, assessmentID, candidate.Sequence)
		if err != nil {
			return nil, err
		}
		candidate.Gates, err = repo.loadNormalizedGates(ctx, assessmentID, candidate.Sequence)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (repo *RobustnessRepo) loadNormalizedFolds(ctx context.Context, assessmentID uuid.UUID, candidateSequence int) ([]robustness.FoldEvidence, error) {
	rows, err := repo.pool.Query(ctx, `SELECT sequence,train_start,train_end,test_start,test_end FROM robustness_assessment_folds WHERE assessment_id=$1 AND candidate_sequence=$2 ORDER BY sequence`, assessmentID, candidateSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	folds := make([]robustness.FoldEvidence, 0)
	for rows.Next() {
		var fold robustness.FoldEvidence
		var trainStart, trainEnd, testStart, testEnd time.Time
		if err := rows.Scan(&fold.Sequence, &trainStart, &trainEnd, &testStart, &testEnd); err != nil {
			return nil, err
		}
		fold.TrainStart = trainStart.UTC().Format("2006-01-02T15:04:05.000000Z")
		fold.TrainEnd = trainEnd.UTC().Format("2006-01-02T15:04:05.000000Z")
		fold.TestStart = testStart.UTC().Format("2006-01-02T15:04:05.000000Z")
		fold.TestEnd = testEnd.UTC().Format("2006-01-02T15:04:05.000000Z")
		scenarios, err := repo.loadNormalizedScenarios(ctx, assessmentID, candidateSequence, fold.Sequence)
		if err != nil {
			return nil, err
		}
		if len(scenarios) > 0 {
			fold.Baseline = scenarios[0]
			fold.Perturbations = scenarios[1:]
		}
		folds = append(folds, fold)
	}
	return folds, rows.Err()
}

func (repo *RobustnessRepo) loadNormalizedScenarios(ctx context.Context, assessmentID uuid.UUID, candidateSequence, foldSequence int) ([]robustness.ScenarioEvidence, error) {
	rows, err := repo.pool.Query(ctx, `SELECT kind,severity,report_id::text,report_sha256 FROM robustness_assessment_scenarios WHERE assessment_id=$1 AND candidate_sequence=$2 AND fold_sequence=$3 ORDER BY sequence`, assessmentID, candidateSequence, foldSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]robustness.ScenarioEvidence, 0)
	for rows.Next() {
		var value robustness.ScenarioEvidence
		if err := rows.Scan(&value.Kind, &value.Severity, &value.ReportID, &value.ReportSHA256); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *RobustnessRepo) loadNormalizedStatistics(ctx context.Context, assessmentID uuid.UUID, candidateSequence int) ([]robustness.Statistic, error) {
	rows, err := repo.pool.Query(ctx, `SELECT name,state,value,unit,reason,description FROM robustness_statistics WHERE assessment_id=$1 AND candidate_sequence=$2 ORDER BY sequence`, assessmentID, candidateSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]robustness.Statistic, 0)
	for rows.Next() {
		var value robustness.Statistic
		if err := rows.Scan(&value.Name, &value.State, &value.Value, &value.Unit, &value.Reason, &value.Description); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *RobustnessRepo) loadNormalizedGates(ctx context.Context, assessmentID uuid.UUID, candidateSequence int) ([]robustness.Gate, error) {
	rows, err := repo.pool.Query(ctx, `SELECT name,state,threshold,observed,reason,description FROM robustness_gates WHERE assessment_id=$1 AND candidate_sequence=$2 ORDER BY sequence`, assessmentID, candidateSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]robustness.Gate, 0)
	for rows.Next() {
		var value robustness.Gate
		if err := rows.Scan(&value.Name, &value.State, &value.Threshold, &value.Observed, &value.Reason, &value.Description); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
