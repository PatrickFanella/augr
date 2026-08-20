package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/venuerecon"
)

// VenueReconciliationRepo persists append-only reconciliation evidence only.
type VenueReconciliationRepo struct{ pool *pgxpool.Pool }

var _ repository.VenueReconciliationRepository = (*VenueReconciliationRepo)(nil)

func NewVenueReconciliationRepo(pool *pgxpool.Pool) *VenueReconciliationRepo {
	return &VenueReconciliationRepo{pool: pool}
}

func (repo *VenueReconciliationRepo) RegisterVenueReconciliationPolicy(ctx context.Context, artifact *venuerecon.PolicyArtifact) (*venuerecon.PolicyArtifact, error) {
	if repo == nil || repo.pool == nil || artifact == nil {
		return nil, fmt.Errorf("postgres: register venue reconciliation policy: repository, pool, and artifact are required")
	}
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate venue reconciliation policy: %w", err)
	}
	command, err := repo.pool.Exec(ctx, `INSERT INTO venue_reconciliation_policy_artifacts (
		id,schema_name,policy_version,sha256,canonical_bytes,canonical_json,created_at
	) VALUES ($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6) ON CONFLICT (policy_version) DO NOTHING`,
		artifact.ID, artifact.Schema, artifact.Version, artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("postgres: insert venue reconciliation policy: %w", err)
	}
	if command.RowsAffected() == 0 {
		existing, loadErr := repo.getPolicy(ctx, artifact.Version)
		if loadErr != nil {
			return nil, loadErr
		}
		if !venuerecon.SamePolicyArtifactPayload(existing, artifact) {
			return nil, fmt.Errorf("postgres: venue reconciliation policy changed on retry: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	return repo.getPolicy(ctx, artifact.Version)
}

func (repo *VenueReconciliationRepo) getPolicy(ctx context.Context, version string) (*venuerecon.PolicyArtifact, error) {
	var artifact venuerecon.PolicyArtifact
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT id,schema_name,policy_version,sha256,canonical_bytes,created_at
		FROM venue_reconciliation_policy_artifacts WHERE policy_version=$1`, version).Scan(
		&artifact.ID, &artifact.Schema, &artifact.Version, &artifact.SHA256, &raw, &artifact.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get venue reconciliation policy: %w", err)
	}
	artifact.CanonicalBytes = append(json.RawMessage(nil), raw...)
	artifact.CreatedAt = artifact.CreatedAt.UTC()
	if _, err := venuerecon.PolicyFromArtifact(artifact); err != nil {
		return nil, fmt.Errorf("postgres: reconstruct venue reconciliation policy: %w", err)
	}
	return &artifact, nil
}

func (repo *VenueReconciliationRepo) RecordVenueProviderSnapshot(ctx context.Context, snapshot *venuerecon.StableProviderSnapshot, createdAt time.Time) error {
	if repo == nil || repo.pool == nil || snapshot == nil || !validReconCreatedAt(createdAt) {
		return fmt.Errorf("postgres: provider snapshot repository, evidence, and UTC microsecond creation time are required")
	}
	if err := venuerecon.ValidateStableProviderSnapshot(snapshot); err != nil {
		return fmt.Errorf("postgres: validate provider snapshot: %w", err)
	}
	firstCapture, secondCapture := snapshot.Captures()
	capture := firstCapture
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO venue_provider_snapshots (
		id,schema_name,provider,account_external_id,namespace,currency,horizon_start,horizon_end,state_sha256,sha256,
		canonical_bytes,canonical_json,state_bytes,state_json,first_capture_id,second_capture_id,
		first_capture_start,first_capture_end,second_capture_start,second_capture_end,page_count,position_count,fill_count,created_at
	) VALUES ($1,'venue-provider-stable-snapshot-v1',$2,$3,$4,$5,$6,$7,$8,$9,$10,convert_from($10,'UTF8')::JSONB,
		$11,convert_from($11,'UTF8')::JSONB,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21) ON CONFLICT (id) DO NOTHING`,
		snapshot.ID(), capture.Provider(), capture.AccountID(), capture.Namespace(), capture.Currency(), capture.HorizonStart(), capture.HorizonEnd(),
		capture.Digest(), snapshot.Digest(), []byte(snapshot.CanonicalBytes()), []byte(capture.CanonicalBytes()),
		firstCapture.ID(), secondCapture.ID(), firstCapture.CaptureStart(), firstCapture.CaptureEnd(), secondCapture.CaptureStart(), secondCapture.CaptureEnd(),
		len(capture.Pages()), len(capture.Positions()), len(capture.Fills()), createdAt)
	if err != nil {
		return fmt.Errorf("postgres: insert provider snapshot: %w", err)
	}
	if command.RowsAffected() == 0 {
		return repo.verifyStoredBytes(ctx, "venue_provider_snapshots", snapshot.ID(), snapshot.Digest(), snapshot.CanonicalBytes())
	}
	for index, page := range capture.Pages() {
		if _, err := tx.Exec(ctx, `INSERT INTO venue_provider_snapshot_pages(snapshot_id,account_external_id,provider,namespace,horizon_start,horizon_end,sequence,cursor,next_cursor,terminal,sha256,raw_bytes)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, snapshot.ID(), capture.AccountID(), capture.Provider(), capture.Namespace(), capture.HorizonStart(), capture.HorizonEnd(), index, page.Cursor, page.NextCursor, page.Terminal, reconSHA256(page.Raw), []byte(page.Raw)); err != nil {
			return fmt.Errorf("postgres: insert provider snapshot page: %w", err)
		}
	}
	for _, position := range capture.Positions() {
		if _, err := tx.Exec(ctx, `INSERT INTO venue_provider_snapshot_positions(snapshot_id,account_external_id,provider,namespace,horizon_start,horizon_end,instrument_id,venue_contract_id,contract_id,quantity,currency,source_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, snapshot.ID(), capture.AccountID(), capture.Provider(), capture.Namespace(), capture.HorizonStart(), capture.HorizonEnd(), position.InstrumentID, position.VenueContract, position.ContractID,
			position.Quantity, position.Currency, position.SourceAt); err != nil {
			return fmt.Errorf("postgres: insert provider position: %w", err)
		}
	}
	for sequence, fill := range capture.Fills() {
		evidence, _ := json.Marshal(fill)
		if _, err := tx.Exec(ctx, `INSERT INTO venue_provider_snapshot_fills(snapshot_id,account_external_id,provider,namespace,horizon_start,horizon_end,sequence,comparison_key,source_id,original_source_id,observation_class,observation_discriminator,evidence)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, snapshot.ID(), capture.AccountID(), capture.Provider(), capture.Namespace(), capture.HorizonStart(), capture.HorizonEnd(), sequence, providerFillKey(fill), fill.SourceID, fill.OriginalSourceID,
			fill.ObservationClass, fill.ObservationDiscriminator, string(evidence)); err != nil {
			return fmt.Errorf("postgres: insert provider fill: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit provider snapshot: %w", err)
	}
	return nil
}

func (repo *VenueReconciliationRepo) RecordVenueLocalSnapshot(ctx context.Context, snapshot *venuerecon.LocalSnapshot, createdAt time.Time) error {
	if repo == nil || repo.pool == nil || snapshot == nil || !validReconCreatedAt(createdAt) {
		return fmt.Errorf("postgres: local snapshot repository, evidence, and creation time are required")
	}
	if err := venuerecon.ValidateLocalSnapshot(snapshot); err != nil {
		return err
	}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO venue_local_snapshots(id,schema_name,account_id,provider,namespace,horizon_start,horizon_end,checkpoint_id,sha256,
		canonical_bytes,canonical_json,transaction_count,position_count,fill_count,issue_count,created_at)
		VALUES($1,'venue-local-snapshot-v1',$2,$3,$4,$5,$6,$7,$8,$9,convert_from($9,'UTF8')::JSONB,$10,$11,$12,$13,$14) ON CONFLICT(id) DO NOTHING`,
		snapshot.ID(), snapshot.AccountID(), snapshot.Provider(), snapshot.Namespace(), snapshot.HorizonStart(), snapshot.HorizonEnd(), snapshot.CheckpointID(), snapshot.Digest(),
		[]byte(snapshot.CanonicalBytes()), len(snapshot.TransactionIDs()), len(snapshot.Positions()), len(snapshot.Fills()), len(snapshot.Issues()), createdAt)
	if err != nil {
		return fmt.Errorf("postgres: insert local snapshot: %w", err)
	}
	if command.RowsAffected() == 0 {
		return repo.verifyStoredBytes(ctx, "venue_local_snapshots", snapshot.ID(), snapshot.Digest(), snapshot.CanonicalBytes())
	}
	for _, id := range snapshot.TransactionIDs() {
		if _, err := tx.Exec(ctx, `INSERT INTO venue_local_snapshot_transactions VALUES($1,$2,$3,$4,$5,$6,$7)`, snapshot.ID(), snapshot.AccountID(), snapshot.Provider(), snapshot.Namespace(), snapshot.HorizonStart(), snapshot.HorizonEnd(), id); err != nil {
			return err
		}
	}
	for _, position := range snapshot.Positions() {
		if _, err := tx.Exec(ctx, `INSERT INTO venue_local_snapshot_positions VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, snapshot.ID(), snapshot.AccountID(), snapshot.Provider(), snapshot.Namespace(), snapshot.HorizonStart(), snapshot.HorizonEnd(), position.InstrumentID, position.Quantity); err != nil {
			return err
		}
	}
	for sequence, fill := range snapshot.Fills() {
		evidence, _ := json.Marshal(fill)
		if _, err := tx.Exec(ctx, `INSERT INTO venue_local_snapshot_fills(snapshot_id,account_id,provider,namespace,horizon_start,horizon_end,sequence,comparison_key,fill_id,ledger_transaction_id,evidence) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, snapshot.ID(), snapshot.AccountID(), snapshot.Provider(), snapshot.Namespace(), snapshot.HorizonStart(), snapshot.HorizonEnd(), sequence, localFillKey(fill), fill.FillID, fill.LedgerTransactionID, string(evidence)); err != nil {
			return err
		}
	}
	for _, issue := range snapshot.Issues() {
		evidence, _ := json.Marshal(issue)
		key := reconSHA256([]byte(string(issue.Reason) + "\x00" + issue.SourceID + "\x00" + issue.LedgerTransactionID))
		if _, err := tx.Exec(ctx, `INSERT INTO venue_local_snapshot_issues VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, snapshot.ID(), snapshot.AccountID(), snapshot.Provider(), snapshot.Namespace(), snapshot.HorizonStart(), snapshot.HorizonEnd(), key, issue.Reason, string(evidence)); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit local snapshot: %w", err)
	}
	return nil
}

func (repo *VenueReconciliationRepo) RecordVenueReconciliationRun(ctx context.Context, run *venuerecon.Run, createdAt time.Time) (*venuerecon.Run, error) {
	if repo == nil || repo.pool == nil || run == nil || !validReconCreatedAt(createdAt) {
		return nil, fmt.Errorf("postgres: reconciliation run repository, evidence, and creation time are required")
	}
	if err := venuerecon.ValidatePersistableRun(run); err != nil {
		return nil, err
	}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO venue_reconciliation_runs(id,schema_name,policy_version,provider_snapshot_id,local_snapshot_id,
		clean,result_count,incident_count,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,convert_from($10,'UTF8')::JSONB,$11) ON CONFLICT(id) DO NOTHING`,
		run.ID, run.Schema, run.PolicyVersion, nullableUUID(run.ProviderSnapshotID), run.LocalSnapshotID, run.Clean, len(run.Results), len(run.Incidents),
		run.SHA256, []byte(run.CanonicalBytes), createdAt)
	if err != nil {
		return nil, fmt.Errorf("postgres: insert reconciliation run: %w", err)
	}
	if command.RowsAffected() == 0 {
		return repo.GetVenueReconciliationRun(ctx, run.ID)
	}
	for _, result := range run.Results {
		if _, err := tx.Exec(ctx, `INSERT INTO venue_reconciliation_results(run_id,policy_version,provider_snapshot_id,local_snapshot_id,id,result_key,kind,status,reason,severity,provider_value,local_value,delta)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, run.ID, run.PolicyVersion, nullableUUID(run.ProviderSnapshotID), run.LocalSnapshotID, result.ID, result.Key, result.Kind, result.Status, result.Reason,
			result.Severity, result.ProviderValue, result.LocalValue, result.Delta); err != nil {
			return nil, err
		}
	}
	for _, incident := range run.Incidents {
		if _, err := tx.Exec(ctx, `INSERT INTO venue_reconciliation_incidents(run_id,policy_version,provider_snapshot_id,local_snapshot_id,id,result_id,incident_key,reason,severity)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, run.ID, run.PolicyVersion, nullableUUID(run.ProviderSnapshotID), run.LocalSnapshotID, incident.ID, incident.ResultID, incident.Key, incident.Reason, incident.Severity); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit reconciliation run: %w", err)
	}
	return repo.GetVenueReconciliationRun(ctx, run.ID)
}

func (repo *VenueReconciliationRepo) GetVenueReconciliationRun(ctx context.Context, id uuid.UUID) (*venuerecon.Run, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: reconciliation run repository and ID are required")
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM venue_reconciliation_runs WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get reconciliation run: %w", err)
	}
	return venuerecon.RunFromCanonical(id, digest, raw)
}

func (repo *VenueReconciliationRepo) verifyStoredBytes(ctx context.Context, table string, id uuid.UUID, digest string, raw []byte) error {
	if table != "venue_provider_snapshots" && table != "venue_local_snapshots" {
		return fmt.Errorf("postgres: unsupported reconciliation evidence table")
	}
	var storedDigest string
	var storedRaw []byte
	query := fmt.Sprintf("SELECT sha256,canonical_bytes FROM %s WHERE id=$1", table)
	if err := repo.pool.QueryRow(ctx, query, id).Scan(&storedDigest, &storedRaw); err != nil {
		return err
	}
	if storedDigest != digest || string(storedRaw) != string(raw) {
		return repository.ErrIdempotencyConflict
	}
	return nil
}

func validReconCreatedAt(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}

func nullableUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func providerFillKey(fill venuerecon.ProviderFill) string {
	if fill.ObservationClass == lifecycle.ObservationOrdinary {
		return reconSHA256([]byte("ordinary\x00" + fill.SourceID))
	}
	return reconSHA256([]byte("revision\x00" + fill.OriginalSourceID + "\x00" + string(fill.ObservationClass) + "\x00" + fill.ObservationDiscriminator))
}

func localFillKey(fill venuerecon.LocalFillEvidence) string {
	if fill.ObservationClass == lifecycle.ObservationOrdinary {
		return reconSHA256([]byte("ordinary\x00" + fill.SourceID))
	}
	return reconSHA256([]byte("revision\x00" + fill.OriginalSourceID + "\x00" + string(fill.ObservationClass) + "\x00" + fill.ObservationDiscriminator))
}

func reconSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
