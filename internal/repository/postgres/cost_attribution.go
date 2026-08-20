package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/costattribution"
)

var ErrCostAttributionConflict = errors.New("postgres: cost attribution report conflict")

type CostAttributionRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

type PersistedCostAttributionReport struct {
	ID             uuid.UUID
	SHA256         string
	CanonicalBytes []byte
}

func NewCostAttributionRepo(pool *pgxpool.Pool) *CostAttributionRepo {
	return &CostAttributionRepo{pool: pool}
}

func (r *CostAttributionRepo) RegisterReport(ctx context.Context, report *costattribution.Report) (*PersistedCostAttributionReport, error) {
	if r == nil || r.pool == nil || report == nil {
		return nil, fmt.Errorf("postgres: cost attribution pool and report are required")
	}
	record := report.Record()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing PersistedCostAttributionReport
	err = tx.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM full_cost_attribution_reports WHERE summary_id=$1`, record.SummaryID).Scan(&existing.ID, &existing.SHA256, &existing.CanonicalBytes)
	if err == nil {
		if existing.ID != record.ID || existing.SHA256 != record.SHA256 || !bytes.Equal(existing.CanonicalBytes, record.CanonicalBytes) {
			return nil, ErrCostAttributionConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	command, err := tx.Exec(ctx, `INSERT INTO full_cost_attribution_reports(id,schema_name,case_id,case_sha256,summary_id,summary_sha256,hypothesis_id,hypothesis_sha256,manifest_id,manifest_sha256,account_id,window_start,window_end,statement_at,currency,line_count,actual_costs,estimated_costs,actual_rebates,estimated_rebates,known_net_cost,unknown_count,coverage,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26) ON CONFLICT(id) DO NOTHING`, record.ID, costattribution.ReportSchemaV1, record.CaseID, record.CaseSHA256, record.SummaryID, record.SummarySHA256, record.HypothesisID, record.HypothesisSHA256, record.ManifestID, record.ManifestSHA256, record.AccountID, record.WindowStart, record.WindowEnd, record.StatementAt, record.Currency, len(record.Lines), record.Totals.ActualCosts, record.Totals.EstimatedCosts, record.Totals.ActualRebates, record.Totals.EstimatedRebates, record.Totals.KnownNetCost, record.Totals.UnknownCount, record.Totals.Coverage, record.SHA256, []byte(record.CanonicalBytes), record.CanonicalBytes)
	if err != nil {
		return nil, costAttributionWriteError("persist report", err)
	}
	if command.RowsAffected() == 0 {
		return r.readAndCompareCost(ctx, tx, record)
	}
	if err := r.stage("report"); err != nil {
		return nil, err
	}
	for _, line := range record.Lines {
		raw, err := json.Marshal(line)
		if err != nil {
			return nil, err
		}
		evidenceID, err := uuid.Parse(line.EvidenceID)
		if err != nil && line.EvidenceID != "" {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO full_cost_attribution_lines(report_id,sequence,line_key,category,knowledge_status,amount,evidence_kind,evidence_id,evidence_sha256,method,method_sha256,explanation,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, record.ID, line.Sequence, line.Key, line.Category, line.Status, line.Amount, line.EvidenceKind, nullableUUID(evidenceID), line.EvidenceSHA256, line.Method, line.MethodSHA256, line.Explanation, raw)
		if err != nil {
			return nil, costAttributionWriteError("persist line", err)
		}
	}
	if err := r.stage("lines"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, costAttributionWriteError("commit report", err)
	}
	return &PersistedCostAttributionReport{record.ID, record.SHA256, append([]byte(nil), record.CanonicalBytes...)}, nil
}

func (r *CostAttributionRepo) GetReport(ctx context.Context, id uuid.UUID) (*PersistedCostAttributionReport, error) {
	var persisted PersistedCostAttributionReport
	if err := r.pool.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM full_cost_attribution_reports WHERE id=$1`, id).Scan(&persisted.ID, &persisted.SHA256, &persisted.CanonicalBytes); err != nil {
		return nil, fmt.Errorf("postgres: get cost attribution report: %w", err)
	}
	return &persisted, nil
}

func (r *CostAttributionRepo) readAndCompareCost(ctx context.Context, tx pgx.Tx, record costattribution.Record) (*PersistedCostAttributionReport, error) {
	var persisted PersistedCostAttributionReport
	if err := tx.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM full_cost_attribution_reports WHERE id=$1`, record.ID).Scan(&persisted.ID, &persisted.SHA256, &persisted.CanonicalBytes); err != nil {
		return nil, err
	}
	if persisted.SHA256 != record.SHA256 || !bytes.Equal(persisted.CanonicalBytes, record.CanonicalBytes) {
		return nil, ErrCostAttributionConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &persisted, nil
}
func (r *CostAttributionRepo) stage(name string) error {
	if r.afterStage == nil {
		return nil
	}
	return r.afterStage(name)
}
func costAttributionWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrCostAttributionConflict, operation)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}
