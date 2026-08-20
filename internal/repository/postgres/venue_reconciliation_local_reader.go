package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/venuerecon"
)

// VenueReconciliationLocalReader owns the complete repeatable-read, read-only
// transaction used to rebuild a checkpoint and collect its lifecycle evidence.
type VenueReconciliationLocalReader struct{ pool *pgxpool.Pool }

var _ venuerecon.LocalEvidenceReader = (*VenueReconciliationLocalReader)(nil)

func NewVenueReconciliationLocalReader(pool *pgxpool.Pool) *VenueReconciliationLocalReader {
	return &VenueReconciliationLocalReader{pool: pool}
}

func (reader *VenueReconciliationLocalReader) ReadLocalEvidenceInRepeatableRead(
	ctx context.Context,
	request venuerecon.LocalSnapshotRequest,
) (venuerecon.LocalSnapshotInput, error) {
	if reader == nil || reader.pool == nil || request.AccountID == uuid.Nil || request.CheckpointID == uuid.Nil {
		return venuerecon.LocalSnapshotInput{}, fmt.Errorf("postgres: local reconciliation reader and scope are required")
	}
	tx, err := reader.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return venuerecon.LocalSnapshotInput{}, fmt.Errorf("postgres: begin local reconciliation read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	checkpoint, err := scanProjectionCheckpoint(tx.QueryRow(ctx, projectionCheckpointSelectSQL+`
		WHERE id=$1 AND projection_version IS NOT NULL`, request.CheckpointID))
	if errors.Is(err, pgx.ErrNoRows) {
		return venuerecon.LocalSnapshotInput{}, repository.ErrNotFound
	}
	if err != nil {
		return venuerecon.LocalSnapshotInput{}, fmt.Errorf("postgres: load reconciliation checkpoint: %w", err)
	}
	if checkpoint.AccountID != request.AccountID || !checkpoint.AsOf.Equal(request.HorizonEnd) {
		return venuerecon.LocalSnapshotInput{}, fmt.Errorf("postgres: reconciliation checkpoint scope mismatch")
	}
	projectionRequest := ledger.ProjectionRequest{
		AccountID: request.AccountID, AsOf: checkpoint.AsOf, MarkSource: checkpoint.MarkSource,
		MarkNamespace: checkpoint.MarkNamespace, MaxMarkAge: checkpoint.MaxMarkAge,
	}
	projectionInput, err := loadProjectionInput(ctx, tx, projectionRequest)
	if err != nil {
		return venuerecon.LocalSnapshotInput{}, err
	}
	projection, err := ledger.BuildPortfolioProjection(projectionInput)
	if err != nil {
		return venuerecon.LocalSnapshotInput{}, fmt.Errorf("postgres: rebuild reconciliation checkpoint: %w", err)
	}
	rebuilt := projection.Checkpoint()
	rebuilt.AttestationKeyID = checkpoint.AttestationKeyID
	rebuilt.AttestationHMAC = append([]byte(nil), checkpoint.AttestationHMAC...)
	if !sameProjectionCheckpoint(rebuilt, checkpoint) {
		return venuerecon.LocalSnapshotInput{}, fmt.Errorf("postgres: reconciliation checkpoint no longer rebuilds exactly")
	}

	transactionIDs := make([]uuid.UUID, 0, len(projectionInput.Transactions))
	for _, transaction := range projectionInput.Transactions {
		transactionIDs = append(transactionIDs, transaction.ID)
	}
	fills, err := loadVenueReconciliationLocalFills(ctx, tx, request)
	if err != nil {
		return venuerecon.LocalSnapshotInput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return venuerecon.LocalSnapshotInput{}, fmt.Errorf("postgres: commit local reconciliation read: %w", err)
	}
	return venuerecon.LocalSnapshotInput{
		AccountID: request.AccountID, Provider: request.Provider, Namespace: request.Namespace,
		HorizonStart: request.HorizonStart, HorizonEnd: request.HorizonEnd, Checkpoint: checkpoint,
		TransactionIDs: transactionIDs, Fills: fills,
	}, nil
}

func loadVenueReconciliationLocalFills(
	ctx context.Context,
	tx pgx.Tx,
	request venuerecon.LocalSnapshotRequest,
) ([]venuerecon.LocalFill, error) {
	rows, err := tx.Query(ctx, `SELECT
		f.id,f.intent_id,f.order_id,f.account_id,f.instrument_id,f.venue_contract_id,f.normalization_id,f.ledger_transaction_id,
		f.side,f.quantity::TEXT,f.price::TEXT,e.source_event_id,COALESCE(NULLIF(e.source_revision,''),v.raw_sha256),e.observation_class,e.observation_discriminator,
		e.original_fill_id,e.original_source_event_id,b.external_order_id,o.client_order_id,
		COALESCE(n.cost_amount,0)::TEXT,n.cash_currency,e.source_at
	FROM execution_lifecycle_events e
	JOIN execution_fills f ON f.id=CASE WHEN e.observation_class='ordinary' THEN e.fill_id ELSE e.original_fill_id END
	JOIN venue_observations v ON v.account_id=e.account_id AND v.provider=e.source AND
		v.source_namespace=e.source_namespace AND v.source_event_id=e.source_event_id
	JOIN execution_orders o ON o.id=f.order_id
	LEFT JOIN execution_order_bindings b ON b.order_id=f.order_id
	JOIN economic_event_normalizations n ON n.id=f.normalization_id
	WHERE e.account_id=$1 AND e.source=$2 AND e.source_namespace=$3 AND e.source_at >= $4 AND e.source_at <= $5
	  AND ((e.observation_class='ordinary' AND e.kind IN ('fill_acknowledged','fill_recorded')) OR e.kind IN ('fill_correction_observed','fill_bust_observed'))
	ORDER BY e.source_at,e.ingest_sequence,e.id`, request.AccountID, request.Provider, request.Namespace, request.HorizonStart, request.HorizonEnd)
	if err != nil {
		return nil, fmt.Errorf("postgres: load local reconciliation fills: %w", err)
	}
	defer rows.Close()
	result := make([]venuerecon.LocalFill, 0)
	for rows.Next() {
		var fill venuerecon.LocalFill
		var quantity, price, fee string
		var originalFillID *uuid.UUID
		if err := rows.Scan(
			&fill.FillID, &fill.IntentID, &fill.OrderID, &fill.AccountID, &fill.InstrumentID, &fill.VenueContractID,
			&fill.NormalizationID, &fill.LedgerTransactionID, &fill.Side, &quantity, &price, &fill.SourceID,
			&fill.SourceRevision, &fill.ObservationClass, &fill.ObservationDiscriminator, &originalFillID,
			&fill.OriginalSourceID, &fill.ExternalOrderID, &fill.ClientOrderID, &fee, &fill.Currency, &fill.SourceAt,
		); err != nil {
			return nil, err
		}
		fill.Provider = venue.Provider(request.Provider)
		fill.Namespace = request.Namespace
		if originalFillID != nil {
			fill.OriginalFillID = *originalFillID
		}
		if fill.Quantity, err = decimal.NewFromString(quantity); err != nil {
			return nil, err
		}
		if fill.Price, err = decimal.NewFromString(price); err != nil {
			return nil, err
		}
		if fill.Fee, err = decimal.NewFromString(fee); err != nil {
			return nil, err
		}
		fill.SourceAt = fill.SourceAt.UTC().Truncate(time.Microsecond)
		if fill.ObservationClass == "" {
			fill.ObservationClass = lifecycle.ObservationOrdinary
		}
		result = append(result, fill)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
