package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/dailysupervisor"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

var ErrDailySupervisorConflict = errors.New("postgres: daily supervisor assessment conflict")

type DailySupervisorRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

type PersistedDailySupervisorAssessment struct {
	ID             uuid.UUID
	SHA256         string
	CanonicalBytes []byte
}

func NewDailySupervisorRepo(pool *pgxpool.Pool) *DailySupervisorRepo {
	return &DailySupervisorRepo{pool: pool}
}

func (r *DailySupervisorRepo) RegisterAssessment(ctx context.Context, assessment *dailysupervisor.Assessment) (*PersistedDailySupervisorAssessment, error) {
	if r == nil || r.pool == nil || assessment == nil {
		return nil, fmt.Errorf("postgres: daily supervisor pool and assessment are required")
	}
	record := assessment.Record()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin daily supervisor assessment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	policySHA := strings.TrimPrefix(record.PolicyVersion, dailysupervisor.PolicyVersionPrefix)
	policyID := economicid.DeterministicUUID("daily-supervisor-policy", record.PolicyVersion)
	if _, err := tx.Exec(ctx, `INSERT INTO daily_supervisor_policy_artifacts(id,policy_version,sha256) VALUES($1,$2,$3) ON CONFLICT(policy_version) DO NOTHING`, policyID, record.PolicyVersion, policySHA); err != nil {
		return nil, fmt.Errorf("postgres: persist daily supervisor policy: %w", err)
	}
	if err := r.stage("policy"); err != nil {
		return nil, err
	}

	var existingID uuid.UUID
	var existingSHA string
	var existingRaw []byte
	err = tx.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM daily_supervisor_assessments WHERE scheduler_effect_id=$1`, record.SchedulerEffectID).Scan(&existingID, &existingSHA, &existingRaw)
	if err == nil {
		if existingID != record.ID || existingSHA != record.SHA256 || !bytes.Equal(existingRaw, record.CanonicalBytes) {
			return nil, ErrDailySupervisorConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &PersistedDailySupervisorAssessment{existingID, existingSHA, append([]byte(nil), existingRaw...)}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres: inspect daily supervisor effect: %w", err)
	}

	command, err := tx.Exec(ctx, `INSERT INTO daily_supervisor_assessments(id,schema_name,operating_day,timezone,evaluated_at,policy_version,reconciliation_id,reconciliation_sha256,scheduler_occurrence_id,scheduler_occurrence_sha256,scheduler_effect_id,scheduler_effect_sha256,prior_assessment_id,prior_assessment_sha256,check_count,action_count,attention_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) ON CONFLICT(id) DO NOTHING`,
		record.ID, dailysupervisor.AssessmentSchemaV1, record.OperatingDay, record.Timezone, record.EvaluatedAt, record.PolicyVersion,
		record.ReconciliationID, record.ReconciliationSHA256, record.SchedulerOccurrenceID, record.SchedulerOccurrenceSHA256,
		record.SchedulerEffectID, record.SchedulerEffectSHA256, nullableUUID(record.PriorAssessmentID), record.PriorAssessmentSHA256,
		len(record.Checks), len(record.Actions), len(record.Attention), record.SHA256, []byte(record.CanonicalBytes), record.CanonicalBytes)
	if err != nil {
		return nil, dailySupervisorWriteError("persist assessment", err)
	}
	if command.RowsAffected() == 0 {
		return r.readAndCompare(ctx, tx, record)
	}
	if err := r.stage("assessment"); err != nil {
		return nil, err
	}

	for index, check := range record.Checks {
		if _, err := tx.Exec(ctx, `INSERT INTO daily_supervisor_checks(assessment_id,sequence,check_name,check_state,evidence_id,evidence_sha256,observed_at,fresh_through,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, record.ID, index+1, check.Name, check.State, check.EvidenceID, check.EvidenceSHA256, check.ObservedAt, check.FreshThrough, check.Reason); err != nil {
			return nil, dailySupervisorWriteError("persist checks", err)
		}
	}
	if err := r.stage("checks"); err != nil {
		return nil, err
	}

	for actionIndex, action := range record.Actions {
		sequence := actionIndex + 1
		if _, err := tx.Exec(ctx, `INSERT INTO daily_supervisor_actions(assessment_id,sequence,work_class,admission) VALUES($1,$2,$3,$4)`, record.ID, sequence, action.Work, action.Admission); err != nil {
			return nil, dailySupervisorWriteError("persist actions", err)
		}
		for blockerIndex, blocker := range action.BlockedBy {
			if _, err := tx.Exec(ctx, `INSERT INTO daily_supervisor_action_blockers(assessment_id,action_sequence,sequence,check_name) VALUES($1,$2,$3,$4)`, record.ID, sequence, blockerIndex+1, blocker); err != nil {
				return nil, dailySupervisorWriteError("persist blockers", err)
			}
		}
	}
	if err := r.stage("actions"); err != nil {
		return nil, err
	}

	for index, attention := range record.Attention {
		if _, err := tx.Exec(ctx, `INSERT INTO daily_supervisor_attention(assessment_id,sequence,check_name,check_state,reason,evidence_id,evidence_sha256) VALUES($1,$2,$3,$4,$5,$6,$7)`, record.ID, index+1, attention.Check, attention.State, attention.Reason, attention.EvidenceID, attention.EvidenceSHA256); err != nil {
			return nil, dailySupervisorWriteError("persist attention", err)
		}
	}
	if err := r.stage("attention"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, dailySupervisorWriteError("commit assessment", err)
	}
	return &PersistedDailySupervisorAssessment{record.ID, record.SHA256, append([]byte(nil), record.CanonicalBytes...)}, nil
}

func (r *DailySupervisorRepo) GetAssessment(ctx context.Context, id uuid.UUID) (*PersistedDailySupervisorAssessment, error) {
	var persisted PersistedDailySupervisorAssessment
	if err := r.pool.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM daily_supervisor_assessments WHERE id=$1`, id).Scan(&persisted.ID, &persisted.SHA256, &persisted.CanonicalBytes); err != nil {
		return nil, fmt.Errorf("postgres: get daily supervisor assessment: %w", err)
	}
	return &persisted, nil
}

func (r *DailySupervisorRepo) readAndCompare(ctx context.Context, tx pgx.Tx, record dailysupervisor.Record) (*PersistedDailySupervisorAssessment, error) {
	var persisted PersistedDailySupervisorAssessment
	if err := tx.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM daily_supervisor_assessments WHERE id=$1`, record.ID).Scan(&persisted.ID, &persisted.SHA256, &persisted.CanonicalBytes); err != nil {
		return nil, err
	}
	if persisted.SHA256 != record.SHA256 || !bytes.Equal(persisted.CanonicalBytes, record.CanonicalBytes) {
		return nil, ErrDailySupervisorConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &persisted, nil
}

func (r *DailySupervisorRepo) stage(name string) error {
	if r.afterStage == nil {
		return nil
	}
	return r.afterStage(name)
}

func dailySupervisorWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrDailySupervisorConflict, operation)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}
