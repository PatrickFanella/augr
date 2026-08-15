package postgres

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	projectionRebuildAttempts      = 3
	projectionCheckpointHMACDomain = "augr-projection-checkpoint-hmac-v1"
)

// ProjectionRepo owns canonical marks and immutable full-rebuild portfolio
// checkpoints. It is deliberately separate from legacy position repositories.
type ProjectionRepo struct {
	pool     *pgxpool.Pool
	attestor ProjectionCheckpointAttestor
}

// ProjectionCheckpointAttestor is a versioned HMAC capability supplied by an
// external secret provider. Its secret is never persisted or logged by Go.
type ProjectionCheckpointAttestor struct {
	KeyID  string
	Secret []byte
}

var _ repository.ProjectionRepository = (*ProjectionRepo)(nil)

func NewProjectionRepo(pool *pgxpool.Pool, attestor ProjectionCheckpointAttestor) *ProjectionRepo {
	attestor.Secret = append([]byte(nil), attestor.Secret...)
	return &ProjectionRepo{pool: pool, attestor: attestor}
}

// RecordMarkObservation appends one canonical mark. Revision, price, time, or
// metadata changes under an existing source observation identity conflict.
func (repo *ProjectionRepo) RecordMarkObservation(ctx context.Context, mark *ledger.MarkObservation) (*ledger.MarkObservation, error) {
	if mark == nil {
		return nil, fmt.Errorf("postgres: record mark observation: mark is required")
	}
	if err := mark.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: record mark observation: %w", err)
	}
	var persistedID uuid.UUID
	err := repo.pool.QueryRow(ctx, `INSERT INTO mark_observations (
		id, unit_kind, unit, price, price_currency, source,
		source_observation_id, effective_at, observed_at, metadata, created_at,
		instrument_id, source_namespace, source_revision
	) VALUES ($1,'instrument',$2,$3,$4,$5,$6,$7,$8,$9::JSONB,$10,$11,$12,$13)
	ON CONFLICT DO NOTHING
	RETURNING id`,
		mark.ID,
		mark.InstrumentID.String(),
		mark.Price.String(),
		mark.PriceCurrency,
		mark.Source,
		mark.SourceObservationID,
		mark.EffectiveAt,
		mark.ObservedAt,
		jsonForStorage(mark.Metadata),
		mark.CreatedAt,
		mark.InstrumentID,
		mark.SourceNamespace,
		mark.SourceRevision,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.getMarkObservationByIdentity(
			ctx, mark.InstrumentID, mark.PriceCurrency, mark.Source, mark.SourceNamespace, mark.SourceObservationID,
		)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed mark observation: %w", loadErr)
		}
		if !ledger.SameMarkObservation(existing, mark) {
			return nil, fmt.Errorf("postgres: canonical mark source identity reused with changed evidence: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return nil, fmt.Errorf("postgres: canonical mark identity conflict: %v: %w", err, repository.ErrIdempotencyConflict)
		}
		return nil, fmt.Errorf("postgres: insert canonical mark: %w", err)
	}
	return repo.GetMarkObservationByID(ctx, persistedID)
}

func (repo *ProjectionRepo) GetMarkObservationByID(ctx context.Context, id uuid.UUID) (*ledger.MarkObservation, error) {
	mark, err := scanProjectionMark(repo.pool.QueryRow(ctx, projectionMarkSelectSQL+` WHERE id = $1 AND instrument_id IS NOT NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get canonical mark %s: %w", id, err)
	}
	return mark, nil
}

func (repo *ProjectionRepo) getMarkObservationByIdentity(
	ctx context.Context,
	instrumentID uuid.UUID,
	currency, source, namespace, observationID string,
) (*ledger.MarkObservation, error) {
	mark, err := scanProjectionMark(repo.pool.QueryRow(ctx, projectionMarkSelectSQL+`
		WHERE instrument_id=$1 AND price_currency=$2 AND source=$3
		  AND source_namespace=$4 AND source_observation_id=$5`,
		instrumentID, currency, source, namespace, observationID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return mark, err
}

const projectionMarkSelectSQL = `SELECT
	id, instrument_id, price::TEXT, price_currency, source, source_namespace,
	source_observation_id, source_revision, effective_at, observed_at, metadata, created_at
FROM mark_observations`

func scanProjectionMark(row accountRow) (*ledger.MarkObservation, error) {
	var mark ledger.MarkObservation
	var price string
	var metadata []byte
	if err := row.Scan(
		&mark.ID,
		&mark.InstrumentID,
		&price,
		&mark.PriceCurrency,
		&mark.Source,
		&mark.SourceNamespace,
		&mark.SourceObservationID,
		&mark.SourceRevision,
		&mark.EffectiveAt,
		&mark.ObservedAt,
		&metadata,
		&mark.CreatedAt,
	); err != nil {
		return nil, err
	}
	parsedPrice, err := decimal.NewFromString(price)
	if err != nil {
		return nil, fmt.Errorf("parse canonical mark price %q: %w", price, err)
	}
	mark.Price = parsedPrice
	mark.Metadata = append(json.RawMessage(nil), metadata...)
	mark.EffectiveAt = mark.EffectiveAt.UTC()
	mark.ObservedAt = mark.ObservedAt.UTC()
	mark.CreatedAt = mark.CreatedAt.UTC()
	if err := mark.Validate(); err != nil {
		return nil, fmt.Errorf("validate loaded canonical mark: %w", err)
	}
	return &mark, nil
}

// RebuildPortfolioProjection loads and checkpoints one complete bitemporal
// snapshot in a repeatable-read transaction. Serialization retries always
// rebuild from zero; checkpoints are never used as a cursor.
func (repo *ProjectionRepo) RebuildPortfolioProjection(ctx context.Context, request ledger.ProjectionRequest) (*ledger.PortfolioProjection, error) {
	request.AsOf = request.AsOf.UTC().Truncate(time.Microsecond)
	request.MarkSource = strings.ToLower(strings.TrimSpace(request.MarkSource))
	request.MarkNamespace = strings.TrimSpace(request.MarkNamespace)
	request.MaxMarkAge = request.MaxMarkAge.Truncate(time.Microsecond)
	var lastError error
	for attempt := 0; attempt < projectionRebuildAttempts; attempt++ {
		projection, err := repo.rebuildPortfolioProjectionOnce(ctx, request)
		if err == nil {
			return projection, nil
		}
		lastError = err
		if !isProjectionRetryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("postgres: rebuild portfolio projection exhausted serialization retries: %w", lastError)
}

func (repo *ProjectionRepo) rebuildPortfolioProjectionOnce(ctx context.Context, request ledger.ProjectionRequest) (*ledger.PortfolioProjection, error) {
	databaseTransaction, err := repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin repeatable-read projection rebuild: %w", err)
	}
	defer func() { _ = databaseTransaction.Rollback(ctx) }()
	if err := requireProjectionWriterBoundary(ctx, databaseTransaction); err != nil {
		return nil, err
	}

	input, err := loadProjectionInput(ctx, databaseTransaction, request)
	if err != nil {
		return nil, err
	}
	projection, err := ledger.BuildPortfolioProjection(input)
	if err != nil {
		return nil, fmt.Errorf("postgres: build portfolio projection: %w", err)
	}
	checkpoint := projection.Checkpoint()
	if err := repo.attestProjectionCheckpoint(checkpoint); err != nil {
		return nil, err
	}
	if err := checkpoint.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate portfolio checkpoint before persistence: %w", err)
	}

	var persistedID uuid.UUID
	err = databaseTransaction.QueryRow(ctx,
		`SELECT persisted_id FROM persist_canonical_projection_checkpoint($1,$2,$3)`,
		checkpoint.PayloadBytes,
		checkpoint.AttestationKeyID,
		checkpoint.AttestationHMAC,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		if rollbackErr := databaseTransaction.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return nil, fmt.Errorf("postgres: roll back replayed projection checkpoint: %w", rollbackErr)
		}
		existing, loadErr := repo.getProjectionCheckpointByIdentity(ctx, checkpoint)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed projection checkpoint: %w", loadErr)
		}
		if !sameProjectionCheckpoint(existing, checkpoint) {
			return nil, fmt.Errorf("postgres: projection checkpoint identity reused with changed payload: %w", repository.ErrIdempotencyConflict)
		}
		return projection, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: insert portfolio projection checkpoint: %w", err)
	}
	if persistedID != checkpoint.ID {
		return nil, fmt.Errorf("postgres: persisted projection checkpoint ID %s, want %s", persistedID, checkpoint.ID)
	}
	if err := databaseTransaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit portfolio projection checkpoint: %w", err)
	}
	return projection, nil
}

func (repo *ProjectionRepo) attestProjectionCheckpoint(checkpoint *ledger.ProjectionCheckpoint) error {
	if checkpoint == nil {
		return fmt.Errorf("postgres: attest projection checkpoint: checkpoint is required")
	}
	keyID := repo.attestor.KeyID
	if keyID == "" || keyID != strings.TrimSpace(keyID) || keyID != strings.ToLower(keyID) || len(keyID) > 128 {
		return fmt.Errorf("postgres: attest projection checkpoint: normalized attestation key ID is required")
	}
	for index, character := range keyID {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '.' || character == '_' || character == '-') {
			continue
		}
		return fmt.Errorf("postgres: attest projection checkpoint: attestation key ID is invalid")
	}
	if len(repo.attestor.Secret) != sha256.Size {
		return fmt.Errorf("postgres: attest projection checkpoint: 32-byte attestation secret is required")
	}
	mac := hmac.New(sha256.New, repo.attestor.Secret)
	_, _ = mac.Write([]byte(projectionCheckpointHMACDomain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(keyID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(checkpoint.PayloadBytes)
	checkpoint.AttestationKeyID = keyID
	checkpoint.AttestationHMAC = mac.Sum(nil)
	return nil
}

func requireProjectionWriterBoundary(ctx context.Context, tx pgx.Tx) error {
	var canInsertDirectly, canReadSigningKeys, canExecuteControlledWrite bool
	if err := tx.QueryRow(ctx, `SELECT
		has_table_privilege(current_user, 'projection_checkpoints', 'INSERT'),
		has_table_privilege(current_user, 'projection_checkpoint_signing_keys', 'SELECT'),
		has_function_privilege(current_user, 'persist_canonical_projection_checkpoint(bytea,text,bytea)', 'EXECUTE')`,
	).Scan(&canInsertDirectly, &canReadSigningKeys, &canExecuteControlledWrite); err != nil {
		return fmt.Errorf("postgres: inspect projection checkpoint writer privileges: %w", err)
	}
	if canInsertDirectly || canReadSigningKeys || !canExecuteControlledWrite {
		return fmt.Errorf(
			"postgres: unsafe checkpoint writer privileges: require controlled function EXECUTE, no direct checkpoint INSERT, and no signing-key SELECT",
		)
	}
	return nil
}

func loadProjectionInput(ctx context.Context, tx pgx.Tx, request ledger.ProjectionRequest) (ledger.ProjectionInput, error) {
	var currency string
	if err := tx.QueryRow(ctx, `SELECT base_currency FROM accounts WHERE id=$1`, request.AccountID).Scan(&currency); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ledger.ProjectionInput{}, repository.ErrNotFound
		}
		return ledger.ProjectionInput{}, fmt.Errorf("postgres: load projection account: %w", err)
	}
	transactions, err := loadProjectionTransactions(ctx, tx, request)
	if err != nil {
		return ledger.ProjectionInput{}, err
	}
	mechanics, err := loadProjectionMechanics(ctx, tx, request)
	if err != nil {
		return ledger.ProjectionInput{}, err
	}
	marks, err := loadProjectionMarks(ctx, tx, request, currency)
	if err != nil {
		return ledger.ProjectionInput{}, err
	}
	return ledger.ProjectionInput{
		Request: request, BaseCurrency: currency, Transactions: transactions, Mechanics: mechanics, Marks: marks,
	}, nil
}

func loadProjectionTransactions(ctx context.Context, tx pgx.Tx, request ledger.ProjectionRequest) ([]*ledger.Transaction, error) {
	rows, err := tx.Query(ctx, `SELECT
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		COALESCE(reference_type, ''), COALESCE(reference_id, ''),
		effective_at, observed_at, metadata, created_at
	FROM ledger_transactions
	WHERE account_id=$1 AND effective_at <= $2 AND observed_at <= $2
	ORDER BY effective_at, observed_at, id`, request.AccountID, request.AsOf)
	if err != nil {
		return nil, fmt.Errorf("postgres: load projection ledger transactions: %w", err)
	}
	defer rows.Close()
	transactions := make([]*ledger.Transaction, 0)
	for rows.Next() {
		transaction, err := scanLedgerTransaction(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan projection ledger transaction: %w", err)
		}
		transaction.EffectiveAt = transaction.EffectiveAt.UTC()
		transaction.ObservedAt = transaction.ObservedAt.UTC()
		transaction.CreatedAt = transaction.CreatedAt.UTC()
		transactions = append(transactions, transaction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: load projection ledger transactions: %w", err)
	}
	for _, transaction := range transactions {
		postingRows, err := tx.Query(ctx, `SELECT
			id, transaction_id, idempotency_key, ledger_account, unit_kind,
			unit, amount::TEXT, metadata, created_at
		FROM ledger_postings WHERE transaction_id=$1
		ORDER BY idempotency_key, id`, transaction.ID)
		if err != nil {
			return nil, fmt.Errorf("postgres: load projection postings for %s: %w", transaction.ID, err)
		}
		for postingRows.Next() {
			posting, err := scanLedgerPosting(postingRows)
			if err != nil {
				postingRows.Close()
				return nil, fmt.Errorf("postgres: scan projection posting for %s: %w", transaction.ID, err)
			}
			posting.CreatedAt = posting.CreatedAt.UTC()
			transaction.Postings = append(transaction.Postings, *posting)
		}
		if err := postingRows.Err(); err != nil {
			postingRows.Close()
			return nil, fmt.Errorf("postgres: load projection postings for %s: %w", transaction.ID, err)
		}
		postingRows.Close()
	}
	return transactions, nil
}

func loadProjectionMechanics(ctx context.Context, tx pgx.Tx, request ledger.ProjectionRequest) ([]ledger.ProjectionMechanics, error) {
	rows, err := tx.Query(ctx, `SELECT
		n.id, n.source_event_id, n.ledger_transaction_id, n.event_type, n.normalizer_version,
		n.execution_origin_type, n.execution_origin_id, n.reference_type, n.reference_id,
		COALESCE(n.venue, ''), n.instrument_id, n.secondary_instrument_id,
		n.venue_contract_id, n.option_terms_id, n.cash_currency,
		n.quantity::TEXT, n.price::TEXT, COALESCE(n.cost_kind, ''), COALESCE(n.cost_currency, ''),
		n.cost_amount::TEXT, n.position_quantity::TEXT, n.settlement_price::TEXT,
		COALESCE(ot.contract_type, ''), ot.strike_price::TEXT, ot.deliverable_quantity::TEXT,
		COALESCE(vc.multiplier::TEXT, '0'), COALESCE(si.multiplier::TEXT, '0')
	FROM economic_event_normalizations AS n
	JOIN ledger_transactions AS lt ON lt.id=n.ledger_transaction_id
	LEFT JOIN venue_contracts AS vc ON vc.id=n.venue_contract_id
	LEFT JOIN instruments AS si ON si.id=n.secondary_instrument_id
	LEFT JOIN option_contract_terms AS ot ON ot.id=n.option_terms_id
	WHERE lt.account_id=$1 AND lt.effective_at <= $2 AND lt.observed_at <= $2
	ORDER BY lt.effective_at, lt.observed_at, lt.id`, request.AccountID, request.AsOf)
	if err != nil {
		return nil, fmt.Errorf("postgres: load projection mechanics: %w", err)
	}
	defer rows.Close()
	result := make([]ledger.ProjectionMechanics, 0)
	for rows.Next() {
		var mechanics ledger.ProjectionMechanics
		var instrumentID, secondaryInstrumentID, venueContractID, optionTermsID *uuid.UUID
		var quantity, price, costAmount, positionQuantity, settlementPrice *string
		var strikePrice, deliverableQuantity *string
		var primaryMultiplier, secondaryMultiplier string
		if err := rows.Scan(
			&mechanics.NormalizationID,
			&mechanics.SourceEventID,
			&mechanics.TransactionID,
			&mechanics.EventType,
			&mechanics.NormalizerVersion,
			&mechanics.ExecutionOriginType,
			&mechanics.ExecutionOriginID,
			&mechanics.ReferenceType,
			&mechanics.ReferenceID,
			&mechanics.Venue,
			&instrumentID,
			&secondaryInstrumentID,
			&venueContractID,
			&optionTermsID,
			&mechanics.CashCurrency,
			&quantity,
			&price,
			&mechanics.CostKind,
			&mechanics.CostCurrency,
			&costAmount,
			&positionQuantity,
			&settlementPrice,
			&mechanics.OptionContractType,
			&strikePrice,
			&deliverableQuantity,
			&primaryMultiplier,
			&secondaryMultiplier,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan projection mechanics: %w", err)
		}
		if instrumentID != nil {
			mechanics.InstrumentID = *instrumentID
		}
		if secondaryInstrumentID != nil {
			mechanics.SecondaryInstrumentID = *secondaryInstrumentID
		}
		if venueContractID != nil {
			mechanics.VenueContractID = *venueContractID
		}
		if optionTermsID != nil {
			mechanics.OptionTermsID = *optionTermsID
		}
		var err error
		if mechanics.Quantity, err = parseOptionalEconomicDecimal(quantity); err != nil {
			return nil, fmt.Errorf("postgres: parse projection quantity: %w", err)
		}
		if mechanics.Price, err = parseOptionalEconomicDecimal(price); err != nil {
			return nil, fmt.Errorf("postgres: parse projection price: %w", err)
		}
		if mechanics.CostAmount, err = parseOptionalEconomicDecimal(costAmount); err != nil {
			return nil, fmt.Errorf("postgres: parse projection cost: %w", err)
		}
		if mechanics.PositionQuantity, err = parseOptionalEconomicDecimal(positionQuantity); err != nil {
			return nil, fmt.Errorf("postgres: parse projection position quantity: %w", err)
		}
		if mechanics.SettlementPrice, err = parseOptionalEconomicDecimal(settlementPrice); err != nil {
			return nil, fmt.Errorf("postgres: parse projection settlement price: %w", err)
		}
		if mechanics.StrikePrice, err = parseOptionalEconomicDecimal(strikePrice); err != nil {
			return nil, fmt.Errorf("postgres: parse projection strike price: %w", err)
		}
		if mechanics.DeliverableQuantity, err = parseOptionalEconomicDecimal(deliverableQuantity); err != nil {
			return nil, fmt.Errorf("postgres: parse projection deliverable quantity: %w", err)
		}
		mechanics.PrimaryMultiplier, err = decimal.NewFromString(primaryMultiplier)
		if err != nil {
			return nil, fmt.Errorf("postgres: parse primary projection multiplier: %w", err)
		}
		mechanics.SecondaryMultiplier, err = decimal.NewFromString(secondaryMultiplier)
		if err != nil {
			return nil, fmt.Errorf("postgres: parse secondary projection multiplier: %w", err)
		}
		result = append(result, mechanics)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: load projection mechanics: %w", err)
	}
	return result, nil
}

func loadProjectionMarks(ctx context.Context, tx pgx.Tx, request ledger.ProjectionRequest, currency string) ([]*ledger.MarkObservation, error) {
	rows, err := tx.Query(ctx, projectionMarkSelectSQL+`
		WHERE instrument_id IS NOT NULL AND price_currency=$1 AND source=$2 AND source_namespace=$3
		  AND effective_at <= $4 AND observed_at <= $4 AND effective_at >= $5
		ORDER BY instrument_id, effective_at DESC, observed_at DESC, id DESC`,
		currency, request.MarkSource, request.MarkNamespace, request.AsOf, request.AsOf.Add(-request.MaxMarkAge),
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: load projection marks: %w", err)
	}
	defer rows.Close()
	result := make([]*ledger.MarkObservation, 0)
	for rows.Next() {
		mark, err := scanProjectionMark(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan projection mark: %w", err)
		}
		result = append(result, mark)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: load projection marks: %w", err)
	}
	return result, nil
}

func (repo *ProjectionRepo) GetProjectionCheckpointByID(ctx context.Context, id uuid.UUID) (*ledger.ProjectionCheckpoint, error) {
	checkpoint, err := scanProjectionCheckpoint(repo.pool.QueryRow(ctx, projectionCheckpointSelectSQL+`
		WHERE id=$1 AND projection_version IS NOT NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get projection checkpoint %s: %w", id, err)
	}
	return checkpoint, nil
}

func (repo *ProjectionRepo) getProjectionCheckpointByIdentity(ctx context.Context, expected *ledger.ProjectionCheckpoint) (*ledger.ProjectionCheckpoint, error) {
	checkpoint, err := scanProjectionCheckpoint(repo.pool.QueryRow(ctx, projectionCheckpointSelectSQL+`
		WHERE account_id=$1 AND projection_type=$2 AND projection_version=$3
		  AND as_of=$4 AND input_checksum=$5`,
		expected.AccountID, expected.ProjectionType, expected.ProjectionVersion, expected.AsOf, expected.InputChecksum,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return checkpoint, err
}

const projectionCheckpointSelectSQL = `SELECT
	id, account_id, projection_type, through_transaction_id,
	projection_version, as_of, fifo_method, base_currency,
	mark_source, mark_namespace, max_mark_age_microseconds,
	transaction_count, mark_count, lot_count, match_count, position_count,
	input_checksum, checksum, payload_bytes, created_at
	, attestation_key_id, attestation_hmac
FROM projection_checkpoints`

func scanProjectionCheckpoint(row accountRow) (*ledger.ProjectionCheckpoint, error) {
	var checkpoint ledger.ProjectionCheckpoint
	var maxMarkAgeMicroseconds int64
	if err := row.Scan(
		&checkpoint.ID,
		&checkpoint.AccountID,
		&checkpoint.ProjectionType,
		&checkpoint.ThroughTransactionID,
		&checkpoint.ProjectionVersion,
		&checkpoint.AsOf,
		&checkpoint.FIFO,
		&checkpoint.BaseCurrency,
		&checkpoint.MarkSource,
		&checkpoint.MarkNamespace,
		&maxMarkAgeMicroseconds,
		&checkpoint.TransactionCount,
		&checkpoint.MarkCount,
		&checkpoint.LotCount,
		&checkpoint.MatchCount,
		&checkpoint.PositionCount,
		&checkpoint.InputChecksum,
		&checkpoint.OutputChecksum,
		&checkpoint.PayloadBytes,
		&checkpoint.CreatedAt,
		&checkpoint.AttestationKeyID,
		&checkpoint.AttestationHMAC,
	); err != nil {
		return nil, err
	}
	checkpoint.AsOf = checkpoint.AsOf.UTC()
	checkpoint.CreatedAt = checkpoint.CreatedAt.UTC()
	checkpoint.MaxMarkAge = time.Duration(maxMarkAgeMicroseconds) * time.Microsecond
	checkpoint.PayloadBytes = append([]byte(nil), checkpoint.PayloadBytes...)
	checkpoint.AttestationHMAC = append([]byte(nil), checkpoint.AttestationHMAC...)
	if err := checkpoint.Validate(); err != nil {
		return nil, fmt.Errorf("validate loaded projection checkpoint: %w", err)
	}
	return &checkpoint, nil
}

func sameProjectionCheckpoint(left, right *ledger.ProjectionCheckpoint) bool {
	return left != nil && right != nil && left.ID == right.ID && left.AccountID == right.AccountID &&
		left.ProjectionType == right.ProjectionType && left.ThroughTransactionID == right.ThroughTransactionID &&
		left.ProjectionVersion == right.ProjectionVersion && left.AsOf.Equal(right.AsOf) && left.FIFO == right.FIFO &&
		left.BaseCurrency == right.BaseCurrency && left.MarkSource == right.MarkSource &&
		left.MarkNamespace == right.MarkNamespace && left.MaxMarkAge == right.MaxMarkAge &&
		left.TransactionCount == right.TransactionCount && left.MarkCount == right.MarkCount &&
		left.LotCount == right.LotCount && left.MatchCount == right.MatchCount && left.PositionCount == right.PositionCount &&
		left.InputChecksum == right.InputChecksum && left.OutputChecksum == right.OutputChecksum &&
		bytes.Equal(left.PayloadBytes, right.PayloadBytes) && left.AttestationKeyID == right.AttestationKeyID &&
		bytes.Equal(left.AttestationHMAC, right.AttestationHMAC)
}

func isProjectionRetryable(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && (postgresError.Code == "40001" || postgresError.Code == "40P01")
}
