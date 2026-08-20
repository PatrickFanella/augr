package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/benchmark"
	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type BenchmarkRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ benchmark.Store = (*BenchmarkRepo)(nil)

func NewBenchmarkRepo(pool *pgxpool.Pool) *BenchmarkRepo { return &BenchmarkRepo{pool: pool} }

func (repo *BenchmarkRepo) GetResearchExperiment(ctx context.Context, id uuid.UUID) (*strategycatalog.Experiment, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: benchmark repository is required")
	}
	return NewStrategyCatalogRepo(repo.pool).GetResearchExperiment(ctx, id)
}

func (repo *BenchmarkRepo) GetDatasetManifest(ctx context.Context, id uuid.UUID) (*dataset.Manifest, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: benchmark repository is required")
	}
	return NewDatasetRepo(repo.pool).GetDatasetManifest(ctx, id)
}

func (repo *BenchmarkRepo) GetEvaluation(ctx context.Context, id uuid.UUID) (*evaluation.Report, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: benchmark repository is required")
	}
	return NewEvaluationRepo(repo.pool).GetEvaluation(ctx, id)
}

func (repo *BenchmarkRepo) RegisterDeclaration(ctx context.Context, value *benchmark.Declaration) (*benchmark.Declaration, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: benchmark declaration is required")
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO passive_benchmark_declarations(id,schema_name,state,experiment_id,experiment_sha256,manifest_id,manifest_sha256,
		benchmark_instrument_id,benchmark_kind,weighting,distribution_treatment,cash_convention,frequency,evaluation_start,evaluation_end,
		initial_notional,decimal_scale,observation_count,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,'passive-benchmark-declaration-v1','declared',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,convert_from($18,'UTF8')::jsonb,$19)
		ON CONFLICT(id) DO NOTHING`, value.ID(), value.ExperimentID(), value.ExperimentDigest(), value.ManifestID(), value.ManifestDigest(), value.BenchmarkInstrumentID(),
		value.BenchmarkKind(), value.Weighting(), value.DistributionTreatment(), value.CashConvention(), value.Frequency(), value.EvaluationStart(), value.EvaluationEnd(),
		value.InitialNotional(), value.DecimalScale(), len(value.Observations()), value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert benchmark declaration", err)
	}
	if err = repo.stage("benchmark_declaration"); err != nil {
		return nil, err
	}
	for sequence, observation := range value.Observations() {
		_, err = tx.Exec(ctx, `INSERT INTO passive_benchmark_observations(declaration_id,sequence,observed_at,benchmark_value,cash_return,evidence_id,evidence_sha256)
			VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(declaration_id,sequence) DO NOTHING`, value.ID(), sequence, observation.ObservedAt, observation.Value, observation.CashReturn, observation.EvidenceID, observation.EvidenceSHA256)
		if err != nil {
			return nil, evaluationWriteError("insert benchmark observation", err)
		}
		if err = repo.stage("benchmark_observation"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit benchmark declaration", err)
	}
	got, err := repo.GetDeclaration(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: benchmark declaration conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *BenchmarkRepo) GetDeclaration(ctx context.Context, id uuid.UUID) (*benchmark.Declaration, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: benchmark declaration identity is required")
	}
	var digest string
	var raw []byte
	var experimentID, manifestID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,experiment_id,manifest_id FROM passive_benchmark_declarations WHERE id=$1`, id).Scan(&digest, &raw, &experimentID, &manifestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	experiment, err := repo.GetResearchExperiment(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	manifest, err := repo.GetDatasetManifest(ctx, manifestID)
	if err != nil {
		return nil, err
	}
	value, err := benchmark.DeclarationFromCanonical(id, digest, raw, experiment, manifest)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct benchmark declaration %s: %w", id, err)
	}
	rows, err := repo.loadBenchmarkObservations(ctx, id)
	if err != nil {
		return nil, err
	}
	expected := value.Observations()
	if len(rows) != len(expected) {
		return nil, fmt.Errorf("postgres: normalized benchmark declaration %s does not reconstruct", id)
	}
	for i := range rows {
		if rows[i] != expected[i] {
			return nil, fmt.Errorf("postgres: normalized benchmark declaration %s does not reconstruct", id)
		}
	}
	return value, nil
}

func (repo *BenchmarkRepo) RecordReport(ctx context.Context, value *benchmark.Report) (*benchmark.Report, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: benchmark report is required")
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO benchmark_opportunity_cost_reports(id,schema_name,state,declaration_id,declaration_sha256,evaluation_id,evaluation_sha256,
		experiment_id,manifest_id,benchmark_instrument_id,strategy_total_return,benchmark_total_return,cash_total_return,benchmark_opportunity_cost,cash_opportunity_cost,
		strategy_terminal_wealth,benchmark_terminal_wealth,cash_terminal_wealth,benchmark_wealth_difference,cash_wealth_difference,observation_count,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,'benchmark-opportunity-cost-report-v1','completed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,convert_from($21,'UTF8')::jsonb,$22)
		ON CONFLICT(id) DO NOTHING`, value.ID(), value.DeclarationID(), value.DeclarationDigest(), value.EvaluationID(), value.EvaluationDigest(), value.ExperimentID(), value.ManifestID(), value.BenchmarkInstrumentID(),
		value.StrategyTotalReturn(), value.BenchmarkTotalReturn(), value.CashTotalReturn(), value.BenchmarkOpportunityCost(), value.CashOpportunityCost(), value.StrategyTerminalWealth(),
		value.BenchmarkTerminalWealth(), value.CashTerminalWealth(), value.BenchmarkWealthDifference(), value.CashWealthDifference(), value.ObservationCount(), value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert benchmark report", err)
	}
	if err = repo.stage("benchmark_report"); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit benchmark report", err)
	}
	got, err := repo.GetReport(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: benchmark report conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *BenchmarkRepo) GetReport(ctx context.Context, id uuid.UUID) (*benchmark.Report, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: benchmark report identity is required")
	}
	var digest string
	var raw []byte
	var declarationID, evaluationID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,declaration_id,evaluation_id FROM benchmark_opportunity_cost_reports WHERE id=$1`, id).Scan(&digest, &raw, &declarationID, &evaluationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	declaration, err := repo.GetDeclaration(ctx, declarationID)
	if err != nil {
		return nil, err
	}
	evaluationReport, err := repo.GetEvaluation(ctx, evaluationID)
	if err != nil {
		return nil, err
	}
	value, err := benchmark.ReportFromCanonical(id, digest, raw, declaration, evaluationReport)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct benchmark report %s: %w", id, err)
	}
	return value, nil
}

func (repo *BenchmarkRepo) ListExperimentDeclarations(ctx context.Context, id uuid.UUID, limit, offset int) ([]*benchmark.Declaration, error) {
	return repo.listDeclarations(ctx, `SELECT id FROM passive_benchmark_declarations WHERE experiment_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, id, limit, offset)
}

func (repo *BenchmarkRepo) ListInstrumentDeclarations(ctx context.Context, id uuid.UUID, limit, offset int) ([]*benchmark.Declaration, error) {
	return repo.listDeclarations(ctx, `SELECT id FROM passive_benchmark_declarations WHERE benchmark_instrument_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, id, limit, offset)
}

func (repo *BenchmarkRepo) ListEvaluationReports(ctx context.Context, id uuid.UUID, limit, offset int) ([]*benchmark.Report, error) {
	return repo.listReports(ctx, `SELECT id FROM benchmark_opportunity_cost_reports WHERE evaluation_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, id, limit, offset)
}

func (repo *BenchmarkRepo) ListDeclarationReports(ctx context.Context, id uuid.UUID, limit, offset int) ([]*benchmark.Report, error) {
	return repo.listReports(ctx, `SELECT id FROM benchmark_opportunity_cost_reports WHERE declaration_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, id, limit, offset)
}

func (repo *BenchmarkRepo) loadBenchmarkObservations(ctx context.Context, id uuid.UUID) ([]benchmark.ObservationInput, error) {
	rows, err := repo.pool.Query(ctx, `SELECT observed_at,benchmark_value,cash_return,evidence_id,evidence_sha256 FROM passive_benchmark_observations WHERE declaration_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []benchmark.ObservationInput
	for rows.Next() {
		var value benchmark.ObservationInput
		if err = rows.Scan(&value.ObservedAt, &value.Value, &value.CashReturn, &value.EvidenceID, &value.EvidenceSHA256); err != nil {
			return nil, err
		}
		value.ObservedAt = value.ObservedAt.UTC()
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *BenchmarkRepo) listDeclarations(ctx context.Context, query string, id uuid.UUID, limit, offset int) ([]*benchmark.Declaration, error) {
	if id == uuid.Nil || limit < 1 || limit > 1000 || offset < 0 {
		return nil, fmt.Errorf("postgres: benchmark declaration list boundary is invalid")
	}
	rows, err := repo.pool.Query(ctx, query, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []*benchmark.Declaration
	for rows.Next() {
		var valueID uuid.UUID
		if err = rows.Scan(&valueID); err != nil {
			return nil, err
		}
		value, loadErr := repo.GetDeclaration(ctx, valueID)
		if loadErr != nil {
			return nil, loadErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *BenchmarkRepo) listReports(ctx context.Context, query string, id uuid.UUID, limit, offset int) ([]*benchmark.Report, error) {
	if id == uuid.Nil || limit < 1 || limit > 1000 || offset < 0 {
		return nil, fmt.Errorf("postgres: benchmark report list boundary is invalid")
	}
	rows, err := repo.pool.Query(ctx, query, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []*benchmark.Report
	for rows.Next() {
		var valueID uuid.UUID
		if err = rows.Scan(&valueID); err != nil {
			return nil, err
		}
		value, loadErr := repo.GetReport(ctx, valueID)
		if loadErr != nil {
			return nil, loadErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *BenchmarkRepo) stage(name string) error {
	if repo.afterStage == nil {
		return nil
	}
	return repo.afterStage(name)
}
