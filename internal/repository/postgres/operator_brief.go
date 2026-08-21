package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/operatorbrief"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrOperatorBriefConflict = errors.New("postgres: daily operator brief conflict")

type OperatorBriefRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}
type PersistedOperatorBrief struct {
	ID             uuid.UUID
	SHA256         string
	CanonicalBytes []byte
}

func NewOperatorBriefRepo(pool *pgxpool.Pool) *OperatorBriefRepo {
	return &OperatorBriefRepo{pool: pool}
}

func (r *OperatorBriefRepo) RegisterBrief(ctx context.Context, brief *operatorbrief.Brief) (*PersistedOperatorBrief, error) {
	if r == nil || r.pool == nil || brief == nil {
		return nil, fmt.Errorf("postgres: operator brief pool and brief are required")
	}
	record := brief.Record()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing PersistedOperatorBrief
	err = tx.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM daily_operator_briefs WHERE operating_day=$1 AND timezone=$2`, record.OperatingDay, record.Timezone).Scan(&existing.ID, &existing.SHA256, &existing.CanonicalBytes)
	if err == nil {
		if existing.ID != record.ID || existing.SHA256 != record.SHA256 || !bytes.Equal(existing.CanonicalBytes, record.CanonicalBytes) {
			return nil, ErrOperatorBriefConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	command, err := tx.Exec(ctx, `INSERT INTO daily_operator_briefs(id,schema_name,operating_day,timezone,generated_at,supervisor_id,supervisor_sha256,reconciliation_id,reconciliation_sha256,cost_report_id,cost_report_sha256,review_summary_id,review_summary_sha256,performance_evaluation_id,performance_evaluation_sha256,section_count,incident_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) ON CONFLICT(id) DO NOTHING`, record.ID, operatorbrief.BriefSchemaV1, record.OperatingDay, record.Timezone, record.GeneratedAt, record.SupervisorID, record.SupervisorSHA256, record.ReconciliationID, record.ReconciliationSHA256, record.CostReportID, record.CostReportSHA256, record.ReviewSummaryID, record.ReviewSummarySHA256, nullableUUID(record.PerformanceEvaluationID), record.PerformanceEvaluationSHA256, len(record.Sections), len(record.Incidents), record.SHA256, []byte(record.CanonicalBytes), record.CanonicalBytes)
	if err != nil {
		return nil, operatorBriefWriteError("persist brief", err)
	}
	if command.RowsAffected() == 0 {
		return r.readAndCompareBrief(ctx, tx, record)
	}
	if err := r.stage("brief"); err != nil {
		return nil, err
	}
	for _, section := range record.Sections {
		raw, _ := json.Marshal(section)
		evidenceID, _ := uuid.Parse(section.EvidenceID)
		_, err := tx.Exec(ctx, `INSERT INTO daily_operator_brief_sections(brief_id,sequence,section_name,section_status,headline,explanation,evidence_kind,evidence_id,evidence_sha256,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, record.ID, section.Sequence, section.Name, section.Status, section.Headline, section.Explanation, section.EvidenceKind, nullableUUID(evidenceID), section.EvidenceSHA256, raw)
		if err != nil {
			return nil, operatorBriefWriteError("persist section", err)
		}
	}
	if err := r.stage("sections"); err != nil {
		return nil, err
	}
	for _, section := range record.Sections {
		for _, fact := range section.Facts {
			raw, _ := json.Marshal(fact)
			_, err := tx.Exec(ctx, `INSERT INTO daily_operator_brief_facts(brief_id,section_sequence,sequence,fact_key,fact_value,canonical_row) VALUES($1,$2,$3,$4,$5,$6)`, record.ID, section.Sequence, fact.Sequence, fact.Key, fact.Value, raw)
			if err != nil {
				return nil, operatorBriefWriteError("persist fact", err)
			}
		}
	}
	if err := r.stage("facts"); err != nil {
		return nil, err
	}
	for _, incident := range record.Incidents {
		raw, _ := json.Marshal(incident)
		sourceID, _ := uuid.Parse(incident.SourceID)
		_, err := tx.Exec(ctx, `INSERT INTO daily_operator_brief_incidents(brief_id,sequence,incident_key,severity,incident_state,source_kind,source_id,source_sha256,summary,required_action,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, record.ID, incident.Sequence, incident.Key, incident.Severity, incident.State, incident.SourceKind, nullableUUID(sourceID), incident.SourceSHA256, incident.Summary, incident.RequiredAction, raw)
		if err != nil {
			return nil, operatorBriefWriteError("persist incident", err)
		}
	}
	if err := r.stage("incidents"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, operatorBriefWriteError("commit brief", err)
	}
	return &PersistedOperatorBrief{record.ID, record.SHA256, append([]byte(nil), record.CanonicalBytes...)}, nil
}

func (r *OperatorBriefRepo) GetBrief(ctx context.Context, id uuid.UUID) (*PersistedOperatorBrief, error) {
	var p PersistedOperatorBrief
	if err := r.pool.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM daily_operator_briefs WHERE id=$1`, id).Scan(&p.ID, &p.SHA256, &p.CanonicalBytes); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *OperatorBriefRepo) readAndCompareBrief(ctx context.Context, tx pgx.Tx, record operatorbrief.Record) (*PersistedOperatorBrief, error) {
	var p PersistedOperatorBrief
	if err := tx.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM daily_operator_briefs WHERE id=$1`, record.ID).Scan(&p.ID, &p.SHA256, &p.CanonicalBytes); err != nil {
		return nil, err
	}
	if p.SHA256 != record.SHA256 || !bytes.Equal(p.CanonicalBytes, record.CanonicalBytes) {
		return nil, ErrOperatorBriefConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *OperatorBriefRepo) stage(name string) error {
	if r.afterStage == nil {
		return nil
	}
	return r.afterStage(name)
}

func operatorBriefWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrOperatorBriefConflict, operation)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}
