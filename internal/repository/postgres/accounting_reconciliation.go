package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/accountingrecon"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type AccountingReconciliationRepo struct{ pool *pgxpool.Pool }

var _ repository.AccountingReconciliationRepository = (*AccountingReconciliationRepo)(nil)

func NewAccountingReconciliationRepo(pool *pgxpool.Pool) *AccountingReconciliationRepo {
	return &AccountingReconciliationRepo{pool: pool}
}

func (repo *AccountingReconciliationRepo) RecordAccountingRun(ctx context.Context, run *accountingrecon.Run) (*accountingrecon.Run, error) {
	if repo == nil || repo.pool == nil || run == nil {
		return nil, fmt.Errorf("postgres: record accounting reconciliation: dependencies and run are required")
	}
	if err := run.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: record accounting reconciliation: %w", err)
	}
	if err := validateAccountingAttestationShape(run); err != nil {
		return nil, fmt.Errorf("postgres: record accounting reconciliation: %w", err)
	}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin accounting reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var persistedID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO accounting_reconciliation_runs (
		id, account_id, comparison_version, policy_version, as_of, generated_at,
		generator, projection_version, mark_source, mark_namespace,
		max_mark_age_microseconds, capture_fence_id, capture_epoch,
		legacy_snapshot_id, legacy_snapshot_checksum, legacy_snapshot_bytes,
		ledger_snapshot_id, ledger_snapshot_checksum, ledger_snapshot_bytes,
		payload, payload_bytes, checksum, result_count, equal_count,
		explained_count, unexplained_count, not_comparable_count, synthetic,
		attestation_type, attestation_key_id, attestation
	) VALUES (
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
		$19,$20::JSONB,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31
	) ON CONFLICT DO NOTHING RETURNING id`,
		run.ID, run.AccountID, run.Version, run.PolicyVersion, run.AsOf, run.GeneratedAt,
		run.Generator, run.ProjectionVersion, run.MarkSource, run.MarkNamespace,
		run.MaxMarkAge.Microseconds(), run.CaptureFenceID, run.CaptureEpoch,
		run.Legacy.ID, run.Legacy.Checksum, run.Legacy.PayloadBytes,
		run.Ledger.ID, run.Ledger.Checksum, run.Ledger.PayloadBytes,
		json.RawMessage(run.PayloadBytes), run.PayloadBytes, run.Checksum, len(run.Results),
		run.EqualCount, run.ExplainedCount, run.UnexplainedCount, run.NotComparableCount,
		run.Synthetic, nullAccountingString(run.AttestationType), nullAccountingString(run.AttestationKeyID), nullAccountingBytes(run.Attestation),
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("postgres: commit replayed accounting reconciliation: %w", commitErr)
		}
		existing, loadErr := repo.GetAccountingRunByID(ctx, run.ID)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: accounting reconciliation identity conflict: %w", repository.ErrIdempotencyConflict)
		}
		if !sameAccountingRun(existing, run) {
			return nil, fmt.Errorf("postgres: accounting reconciliation changed replay: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: insert accounting reconciliation run: %w", err)
	}
	if persistedID != run.ID {
		return nil, fmt.Errorf("postgres: accounting reconciliation persisted ID %s, want %s", persistedID, run.ID)
	}

	for _, result := range run.Results {
		var explanation any
		if result.Explanation != nil {
			encoded, marshalErr := json.Marshal(result.Explanation)
			if marshalErr != nil {
				return nil, fmt.Errorf("postgres: marshal accounting explanation %q: %w", result.FactKey, marshalErr)
			}
			explanation = json.RawMessage(encoded)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO accounting_reconciliation_results (
			id, run_id, fact_key, legacy_value, ledger_value, delta,
			status, reason_code, explanation
		) VALUES ($1,$2,$3,$4::NUMERIC,$5::NUMERIC,$6::NUMERIC,$7,$8,$9::JSONB)`,
			result.ID, run.ID, result.FactKey, decimalSQLArgument(result.LegacyValue),
			decimalSQLArgument(result.LedgerValue), decimalSQLArgument(result.Delta),
			result.Status, result.ReasonCode, explanation,
		); err != nil {
			return nil, fmt.Errorf("postgres: insert accounting reconciliation result %q: %w", result.FactKey, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit accounting reconciliation: %w", err)
	}
	return repo.GetAccountingRunByID(ctx, run.ID)
}

func (repo *AccountingReconciliationRepo) GetAccountingRunByID(ctx context.Context, id uuid.UUID) (*accountingrecon.Run, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: get accounting reconciliation: repository and ID are required")
	}
	return repo.loadAccountingRun(ctx, repo.pool.QueryRow(ctx, accountingRunSelectSQL+` WHERE id=$1`, id))
}

func (repo *AccountingReconciliationRepo) ListAccountingRuns(ctx context.Context, accountID uuid.UUID, limit, offset int) ([]*accountingrecon.Run, error) {
	if repo == nil || repo.pool == nil || accountID == uuid.Nil || limit <= 0 || offset < 0 {
		return nil, fmt.Errorf("postgres: list accounting reconciliations: valid repository, account, limit, and offset are required")
	}
	rows, err := repo.pool.Query(ctx, `SELECT id FROM accounting_reconciliation_runs
		WHERE account_id=$1 ORDER BY as_of DESC, id DESC LIMIT $2 OFFSET $3`, accountID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list accounting reconciliations: %w", err)
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres: scan accounting reconciliation ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list accounting reconciliation IDs: %w", err)
	}
	out := make([]*accountingrecon.Run, 0, len(ids))
	for _, id := range ids {
		run, err := repo.GetAccountingRunByID(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

const accountingRunSelectSQL = `SELECT
	id, account_id, comparison_version, policy_version, as_of, generated_at,
	generator, projection_version, mark_source, mark_namespace,
	max_mark_age_microseconds, capture_fence_id, capture_epoch,
	legacy_snapshot_id, legacy_snapshot_checksum, legacy_snapshot_bytes,
	ledger_snapshot_id, ledger_snapshot_checksum, ledger_snapshot_bytes,
	payload_bytes, checksum, result_count, equal_count, explained_count,
	unexplained_count, not_comparable_count, synthetic,
	COALESCE(attestation_type,''), COALESCE(attestation_key_id,''), attestation
FROM accounting_reconciliation_runs`

func (repo *AccountingReconciliationRepo) loadAccountingRun(ctx context.Context, row accountRow) (*accountingrecon.Run, error) {
	var (
		id, accountID, legacyID, ledgerID                   uuid.UUID
		version, policy, generator, projectionVersion       string
		markSource, markNamespace, fenceID                  string
		legacyChecksum, ledgerChecksum, checksum            string
		asOf, generatedAt                                   sql.NullTime
		maxMarkAgeMicros, captureEpoch                      int64
		legacyBytes, ledgerBytes, payloadBytes, attestation []byte
		resultCount, equalCount, explainedCount             int
		unexplainedCount, notComparableCount                int
		synthetic                                           bool
		attestationType, attestationKeyID                   string
	)
	if err := row.Scan(
		&id, &accountID, &version, &policy, &asOf, &generatedAt,
		&generator, &projectionVersion, &markSource, &markNamespace,
		&maxMarkAgeMicros, &fenceID, &captureEpoch,
		&legacyID, &legacyChecksum, &legacyBytes,
		&ledgerID, &ledgerChecksum, &ledgerBytes,
		&payloadBytes, &checksum, &resultCount, &equalCount, &explainedCount,
		&unexplainedCount, &notComparableCount, &synthetic,
		&attestationType, &attestationKeyID, &attestation,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: scan accounting reconciliation: %w", err)
	}
	run, err := accountingrecon.DecodeRun(payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("postgres: validate accounting reconciliation bytes: %w", err)
	}
	if !asOf.Valid || !generatedAt.Valid || run.ID != id || run.AccountID != accountID || run.Version != version || run.PolicyVersion != policy ||
		!run.AsOf.Equal(asOf.Time.UTC()) || !run.GeneratedAt.Equal(generatedAt.Time.UTC()) || run.Generator != generator ||
		run.ProjectionVersion != projectionVersion || run.MarkSource != markSource || run.MarkNamespace != markNamespace ||
		run.MaxMarkAge.Microseconds() != maxMarkAgeMicros || run.CaptureFenceID != fenceID || run.CaptureEpoch != uint64(captureEpoch) ||
		run.Legacy.ID != legacyID || run.Legacy.Checksum != legacyChecksum || !bytes.Equal(run.Legacy.PayloadBytes, legacyBytes) ||
		run.Ledger.ID != ledgerID || run.Ledger.Checksum != ledgerChecksum || !bytes.Equal(run.Ledger.PayloadBytes, ledgerBytes) ||
		run.Checksum != checksum || len(run.Results) != resultCount || run.EqualCount != equalCount || run.ExplainedCount != explainedCount ||
		run.UnexplainedCount != unexplainedCount || run.NotComparableCount != notComparableCount || run.Synthetic != synthetic {
		return nil, fmt.Errorf("postgres: accounting reconciliation relational envelope differs from canonical bytes")
	}
	run.AttestationType = attestationType
	run.AttestationKeyID = attestationKeyID
	run.Attestation = append([]byte(nil), attestation...)
	if err := validateAccountingAttestationShape(run); err != nil {
		return nil, fmt.Errorf("postgres: loaded accounting reconciliation attestation: %w", err)
	}
	if err := repo.validateAccountingResults(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func (repo *AccountingReconciliationRepo) validateAccountingResults(ctx context.Context, run *accountingrecon.Run) error {
	rows, err := repo.pool.Query(ctx, `SELECT id, fact_key,
		legacy_value::TEXT, ledger_value::TEXT, delta::TEXT,
		status, reason_code, explanation
		FROM accounting_reconciliation_results WHERE run_id=$1 ORDER BY fact_key`, run.ID)
	if err != nil {
		return fmt.Errorf("postgres: load accounting reconciliation results: %w", err)
	}
	defer rows.Close()
	actual := make(map[string]accountingResultRow)
	for rows.Next() {
		var value accountingResultRow
		if err := rows.Scan(&value.id, &value.factKey, &value.legacy, &value.ledger, &value.delta, &value.status, &value.reason, &value.explanation); err != nil {
			return fmt.Errorf("postgres: scan accounting reconciliation result: %w", err)
		}
		actual[value.factKey] = value
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres: load accounting reconciliation results: %w", err)
	}
	if len(actual) != len(run.Results) {
		return fmt.Errorf("postgres: accounting reconciliation relational result count differs")
	}
	for _, expected := range run.Results {
		value, ok := actual[expected.FactKey]
		if !ok || value.id != expected.ID || value.status != string(expected.Status) || value.reason != expected.ReasonCode ||
			!sameNullableDecimal(value.legacy, expected.LegacyValue) || !sameNullableDecimal(value.ledger, expected.LedgerValue) ||
			!sameNullableDecimal(value.delta, expected.Delta) || !sameExplanationJSON(value.explanation, expected.Explanation) {
			return fmt.Errorf("postgres: accounting reconciliation result %q differs from canonical bytes", expected.FactKey)
		}
	}
	return nil
}

type accountingResultRow struct {
	id          uuid.UUID
	factKey     string
	legacy      sql.NullString
	ledger      sql.NullString
	delta       sql.NullString
	status      string
	reason      string
	explanation []byte
}

func sameNullableDecimal(actual sql.NullString, expected *decimal.Decimal) bool {
	if expected == nil {
		return !actual.Valid
	}
	if !actual.Valid {
		return false
	}
	parsed, err := decimal.NewFromString(actual.String)
	return err == nil && parsed.Equal(*expected) && parsed.String() == expected.String()
}

func sameExplanationJSON(actual []byte, expected *accountingrecon.Explanation) bool {
	if expected == nil {
		return len(actual) == 0
	}
	var decoded accountingrecon.Explanation
	if err := json.Unmarshal(actual, &decoded); err != nil {
		return false
	}
	return decoded == *expected
}

func validateAccountingAttestationShape(run *accountingrecon.Run) error {
	hasType := strings.TrimSpace(run.AttestationType) != ""
	hasKey := strings.TrimSpace(run.AttestationKeyID) != ""
	hasBytes := len(run.Attestation) > 0
	if hasType != hasKey || hasKey != hasBytes || run.AttestationType != strings.TrimSpace(run.AttestationType) || run.AttestationKeyID != strings.TrimSpace(run.AttestationKeyID) {
		return fmt.Errorf("opaque accounting attestation must be wholly present or absent and normalized")
	}
	return nil
}

func sameAccountingRun(left, right *accountingrecon.Run) bool {
	return left != nil && right != nil && left.ID == right.ID && bytes.Equal(left.PayloadBytes, right.PayloadBytes) &&
		left.AttestationType == right.AttestationType && left.AttestationKeyID == right.AttestationKeyID && bytes.Equal(left.Attestation, right.Attestation)
}

func decimalSQLArgument(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func nullAccountingString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullAccountingBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
