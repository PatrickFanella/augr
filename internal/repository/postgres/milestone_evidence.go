package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/evidenceprogram"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

var ErrMilestoneEvidenceConflict = errors.New("postgres: milestone evidence conflict")

type MilestoneEvidenceRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

func NewMilestoneEvidenceRepo(pool *pgxpool.Pool) *MilestoneEvidenceRepo {
	return &MilestoneEvidenceRepo{pool: pool}
}

func (r *MilestoneEvidenceRepo) RecordAssessment(ctx context.Context, assessment *evidenceprogram.Assessment) error {
	if r == nil || r.pool == nil || assessment == nil {
		return fmt.Errorf("postgres: milestone assessment is required")
	}
	record := assessment.Record()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO milestone_evidence_assessments(id,schema_name,campaign,outcome,blocker_count,parent_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING`,
		record.ID, evidenceprogram.SchemaV1, record.Campaign, record.Outcome, len(record.Blockers), len(record.Parents), record.SHA256, []byte(record.CanonicalBytes), record.CanonicalBytes)
	if err != nil {
		return milestoneEvidenceWriteError("persist assessment", err)
	}
	if command.RowsAffected() == 0 {
		return r.compareAssessment(ctx, tx, record.ID, record.SHA256, record.CanonicalBytes)
	}
	if err = r.stage("assessment"); err != nil {
		return err
	}
	for sequence, blocker := range record.Blockers {
		if _, err = tx.Exec(ctx, `INSERT INTO milestone_evidence_blockers(assessment_id,sequence,blocker) VALUES($1,$2,$3)`, record.ID, sequence, blocker); err != nil {
			return milestoneEvidenceWriteError("persist blocker", err)
		}
	}
	if err = r.stage("blockers"); err != nil {
		return err
	}
	for sequence, parent := range record.Parents {
		if _, err = tx.Exec(ctx, `INSERT INTO milestone_evidence_parents(assessment_id,sequence,kind,evidence_id,evidence_sha256) VALUES($1,$2,$3,$4,$5)`, record.ID, sequence, parent.Kind, parent.ID, parent.SHA256); err != nil {
			return milestoneEvidenceWriteError("persist parent", err)
		}
	}
	if err = r.stage("parents"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *MilestoneEvidenceRepo) GetAssessment(ctx context.Context, id uuid.UUID) (*evidenceprogram.Assessment, error) {
	value, err := r.getAssessment(ctx, id, map[uuid.UUID]bool{})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return value, err
}

func (r *MilestoneEvidenceRepo) getAssessment(ctx context.Context, id uuid.UUID, loading map[uuid.UUID]bool) (*evidenceprogram.Assessment, error) {
	if r == nil || r.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: milestone assessment identity is required")
	}
	if loading[id] {
		return nil, fmt.Errorf("postgres: milestone assessment parent cycle")
	}
	loading[id] = true
	defer delete(loading, id)
	var digest string
	var raw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM milestone_evidence_assessments WHERE id=$1`, id).Scan(&digest, &raw); err != nil {
		return nil, err
	}
	parents, err := r.loadParents(ctx, id)
	if err != nil {
		return nil, err
	}
	assessmentParents := []*evidenceprogram.Assessment{}
	for _, parent := range parents {
		if !isAssessmentCampaign(parent.Kind) {
			continue
		}
		loaded, loadErr := r.getAssessment(ctx, parent.ID, loading)
		if loadErr != nil || loaded.Digest() != parent.SHA256 || loaded.Campaign() != parent.Kind {
			return nil, fmt.Errorf("postgres: milestone assessment parent does not reconstruct")
		}
		assessmentParents = append(assessmentParents, loaded)
	}
	value, err := evidenceprogram.AssessmentFromCanonical(id, digest, raw, assessmentParents)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct milestone assessment %s: %w", id, err)
	}
	record := value.Record()
	blockers, err := r.loadBlockers(ctx, id)
	if err != nil {
		return nil, err
	}
	if !stringSlicesEqual(blockers, record.Blockers) || !evidenceParentsEqual(parents, record.Parents) {
		return nil, fmt.Errorf("postgres: normalized milestone assessment %s does not reconstruct", id)
	}
	return value, nil
}

func (r *MilestoneEvidenceRepo) loadBlockers(ctx context.Context, id uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT blocker FROM milestone_evidence_blockers WHERE assessment_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r *MilestoneEvidenceRepo) loadParents(ctx context.Context, id uuid.UUID) ([]evidenceprogram.EvidenceRef, error) {
	rows, err := r.pool.Query(ctx, `SELECT kind,evidence_id,evidence_sha256 FROM milestone_evidence_parents WHERE assessment_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []evidenceprogram.EvidenceRef{}
	for rows.Next() {
		var value evidenceprogram.EvidenceRef
		if err = rows.Scan(&value.Kind, &value.ID, &value.SHA256); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r *MilestoneEvidenceRepo) compareAssessment(ctx context.Context, tx pgx.Tx, id uuid.UUID, digest string, raw []byte) error {
	var gotDigest string
	var gotRaw []byte
	if err := tx.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM milestone_evidence_assessments WHERE id=$1`, id).Scan(&gotDigest, &gotRaw); err != nil {
		return err
	}
	if gotDigest != digest || !bytes.Equal(gotRaw, raw) {
		return ErrMilestoneEvidenceConflict
	}
	return tx.Commit(ctx)
}

func (r *MilestoneEvidenceRepo) stage(name string) error {
	if r.afterStage == nil {
		return nil
	}
	return r.afterStage(name)
}

func milestoneEvidenceWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrMilestoneEvidenceConflict, operation)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}

func isAssessmentCampaign(value string) bool {
	switch value {
	case "shadow_30_day", "scored_paper_60_90_day", "portfolio_paper", "architecture_readiness":
		return true
	default:
		return false
	}
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func evidenceParentsEqual(left, right []evidenceprogram.EvidenceRef) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
