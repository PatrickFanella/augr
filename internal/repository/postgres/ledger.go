package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// LedgerRepo persists append-only balanced transactions.
type LedgerRepo struct{ pool *pgxpool.Pool }

var (
	_ repository.LedgerRepository        = (*LedgerRepo)(nil)
	_ repository.EconomicEventRepository = (*LedgerRepo)(nil)
)

func NewLedgerRepo(pool *pgxpool.Pool) *LedgerRepo {
	return &LedgerRepo{pool: pool}
}

// PostTransaction writes the transaction and all posting lines atomically.
// PostgreSQL rechecks unit balance with a deferred constraint before commit.
func (repo *LedgerRepo) PostTransaction(ctx context.Context, transaction *ledger.Transaction) (*ledger.Transaction, error) {
	if transaction == nil {
		return nil, fmt.Errorf("postgres: post ledger transaction: transaction is required")
	}
	if err := transaction.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: post ledger transaction: %w", err)
	}

	databaseTransaction, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin ledger transaction: %w", err)
	}
	defer func() { _ = databaseTransaction.Rollback(ctx) }()

	persistedID, err := insertLedgerTransactionRow(ctx, databaseTransaction, transaction, true)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := databaseTransaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("postgres: commit replayed ledger transaction: %w", err)
		}
		existing, err := repo.getByIdempotencyKey(ctx, transaction.AccountID, transaction.IdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("postgres: load replayed ledger transaction: %w", err)
		}
		if !sameLedgerTransactionPayload(existing, transaction) {
			return nil, fmt.Errorf("postgres: ledger idempotency key %q reused with mismatched payload: %w", transaction.IdempotencyKey, repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return nil, fmt.Errorf("postgres: ledger transaction identity conflicts with an existing event: %v: %w", err, repository.ErrIdempotencyConflict)
		}
		return nil, fmt.Errorf("postgres: insert ledger transaction %s: %w", transaction.ID, err)
	}

	if err := insertLedgerPostingRows(ctx, databaseTransaction, persistedID, transaction.Postings); err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}

	if err := databaseTransaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit ledger transaction %s: %w", transaction.ID, err)
	}
	return repo.GetByID(ctx, persistedID)
}

// RecordEconomicSourceEvent durably appends exact raw evidence before any
// normalization attempt. An identical retry returns the original row; revision
// or wire-evidence changes under the same source identity conflict.
func (repo *LedgerRepo) RecordEconomicSourceEvent(ctx context.Context, event *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("postgres: record economic source event: event is required")
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: record economic source event: %w", err)
	}

	var persistedID uuid.UUID
	err := repo.pool.QueryRow(ctx, `INSERT INTO economic_source_events (
		id, account_id, source, source_namespace, source_event_id, source_revision,
		observed_at, raw_payload, payload_sha256, payload, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::JSONB,$11)
	ON CONFLICT DO NOTHING
	RETURNING id`,
		event.ID,
		event.AccountID,
		event.Source,
		event.SourceNamespace,
		event.SourceEventID,
		event.SourceRevision,
		event.ObservedAt,
		[]byte(event.RawPayload),
		event.PayloadSHA256,
		string(event.RawPayload),
		event.CreatedAt,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := repo.getEconomicSourceEventByIdentity(
			ctx,
			event.AccountID,
			event.Source,
			event.SourceNamespace,
			event.SourceEventID,
		)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed economic source event: %w", loadErr)
		}
		if !ledger.SameEconomicSourceEventPayload(existing, event) {
			return nil, fmt.Errorf("postgres: economic source identity reused with mismatched evidence: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		return nil, economicEventWriteError("record economic source event", err)
	}
	return repo.GetEconomicSourceEventByID(ctx, persistedID)
}

// GetEconomicSourceEventByID loads exact wire evidence without substituting
// the queryable JSONB representation for the original bytes.
func (repo *LedgerRepo) GetEconomicSourceEventByID(ctx context.Context, id uuid.UUID) (*ledger.EconomicSourceEvent, error) {
	event, err := scanEconomicSourceEvent(repo.pool.QueryRow(ctx, economicSourceEventSelectSQL+` WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get economic source event %s: %w", id, err)
	}
	return event, nil
}

func (repo *LedgerRepo) getEconomicSourceEventByIdentity(
	ctx context.Context,
	accountID uuid.UUID,
	source, namespace, sourceEventID string,
) (*ledger.EconomicSourceEvent, error) {
	event, err := scanEconomicSourceEvent(repo.pool.QueryRow(ctx, economicSourceEventSelectSQL+`
		WHERE account_id = $1 AND source = $2 AND source_namespace = $3 AND source_event_id = $4`,
		accountID,
		source,
		namespace,
		sourceEventID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return event, err
}

const economicSourceEventSelectSQL = `SELECT
	id, account_id, source, source_namespace, source_event_id, source_revision,
	observed_at, raw_payload, payload_sha256, created_at
FROM economic_source_events`

func scanEconomicSourceEvent(row accountRow) (*ledger.EconomicSourceEvent, error) {
	var event ledger.EconomicSourceEvent
	var rawPayload []byte
	if err := row.Scan(
		&event.ID,
		&event.AccountID,
		&event.Source,
		&event.SourceNamespace,
		&event.SourceEventID,
		&event.SourceRevision,
		&event.ObservedAt,
		&rawPayload,
		&event.PayloadSHA256,
		&event.CreatedAt,
	); err != nil {
		return nil, err
	}
	event.RawPayload = append(json.RawMessage(nil), rawPayload...)
	event.ObservedAt = event.ObservedAt.UTC()
	event.CreatedAt = event.CreatedAt.UTC()
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("validate loaded economic source event: %w", err)
	}
	return &event, nil
}

// ApplyEconomicNormalization atomically appends the typed normalization,
// deterministic ledger parent and every posting. The raw source row must have
// committed in an earlier transaction.
func (repo *LedgerRepo) ApplyEconomicNormalization(ctx context.Context, normalization *ledger.EconomicNormalization) (*ledger.EconomicNormalization, error) {
	if normalization == nil {
		return nil, fmt.Errorf("postgres: apply economic normalization: normalization is required")
	}
	if err := normalization.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: apply economic normalization: %w", err)
	}

	databaseTransaction, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin economic normalization: %w", err)
	}
	defer func() { _ = databaseTransaction.Rollback(ctx) }()

	_, err = insertEconomicNormalizationAggregate(ctx, databaseTransaction, normalization)
	if errors.Is(err, pgx.ErrNoRows) {
		if rollbackErr := databaseTransaction.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return nil, fmt.Errorf("postgres: roll back replayed economic normalization: %w", rollbackErr)
		}
		existing, loadErr := repo.GetEconomicNormalizationBySourceEventID(ctx, normalization.SourceEvent.ID)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed economic normalization: %w", loadErr)
		}
		if !ledger.SameEconomicNormalizationPayload(existing, normalization) {
			return nil, fmt.Errorf("postgres: economic source event already has a different normalization: %w", repository.ErrIdempotencyConflict)
		}
		return existing, nil
	}
	if err != nil {
		return nil, err
	}
	if err := databaseTransaction.Commit(ctx); err != nil {
		return nil, economicEventWriteError("commit economic normalization", err)
	}
	return repo.GetEconomicNormalizationBySourceEventID(ctx, normalization.SourceEvent.ID)
}

// insertEconomicNormalizationAggregate writes one validated normalization and
// its exact ledger aggregate into the caller's transaction. The raw source
// event must already exist. Callers retain commit authority so a lifecycle fill
// can add its binding, fill, and event before any economic effect becomes
// visible.
func insertEconomicNormalizationAggregate(
	ctx context.Context,
	databaseTransaction pgx.Tx,
	normalization *ledger.EconomicNormalization,
) (uuid.UUID, error) {
	if normalization == nil {
		return uuid.Nil, fmt.Errorf("postgres: insert economic normalization: normalization is required")
	}
	if err := normalization.Validate(); err != nil {
		return uuid.Nil, fmt.Errorf("postgres: insert economic normalization: %w", err)
	}
	persistedSource, err := scanEconomicSourceEvent(databaseTransaction.QueryRow(
		ctx,
		economicSourceEventSelectSQL+` WHERE id = $1`,
		normalization.SourceEvent.ID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("postgres: apply economic normalization: source event %s: %w", normalization.SourceEvent.ID, repository.ErrNotFound)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("postgres: load economic normalization source event: %w", err)
	}
	if !ledger.SameEconomicSourceEventPayload(persistedSource, normalization.SourceEvent) {
		return uuid.Nil, fmt.Errorf("postgres: economic normalization source evidence differs from persisted raw event: %w", repository.ErrIdempotencyConflict)
	}
	if normalization.EventType == ledger.EconomicEventOptionExercise ||
		normalization.EventType == ledger.EconomicEventOptionAssignment {
		if normalization.Instrument == nil {
			return uuid.Nil, fmt.Errorf("postgres: physical economic normalization requires an option instrument")
		}
		if err := acquireEconomicOptionTermsLock(ctx, databaseTransaction, normalization.Instrument.ID); err != nil {
			return uuid.Nil, fmt.Errorf("postgres: lock physical option terms history: %w", err)
		}
	}

	var persistedID uuid.UUID
	err = databaseTransaction.QueryRow(ctx, `INSERT INTO economic_event_normalizations (
		id, source_event_id, event_type, normalizer_version,
		execution_origin_type, execution_origin_id, reference_type, reference_id,
		venue, instrument_id, secondary_instrument_id, venue_contract_id,
		option_terms_id, effective_at, cash_currency, quantity, price,
		cost_kind, cost_currency, cost_amount, position_quantity, settlement_price,
		ledger_transaction_id, created_at
	) VALUES (
		$1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11,$12,$13,$14,$15,
		$16,$17,NULLIF($18,''),NULLIF($19,''),$20,$21,$22,$23,$24
	)
	ON CONFLICT (source_event_id) DO NOTHING
	RETURNING id`,
		normalization.ID,
		normalization.SourceEvent.ID,
		normalization.EventType,
		normalization.NormalizerVersion,
		normalization.ExecutionOriginType,
		normalization.ExecutionOriginID,
		normalization.ReferenceType,
		normalization.ReferenceID,
		normalization.Venue,
		normalizationInstrumentID(normalization.Instrument),
		normalizationInstrumentID(normalization.SecondaryInstrument),
		normalizationVenueContractID(normalization.VenueContract),
		normalizationOptionTermsID(normalization.OptionTerms),
		normalization.EffectiveAt,
		normalization.CashCurrency,
		nullableDecimalPointer(normalization.Quantity),
		nullableDecimalPointer(normalization.Price),
		string(normalization.CostKind),
		normalization.CostCurrency,
		nullableDecimalPointer(normalization.CostAmount),
		nullableDecimalPointer(normalization.PositionQuantity),
		nullableDecimalPointer(normalization.SettlementPrice),
		normalization.Transaction.ID,
		normalization.CreatedAt,
	).Scan(&persistedID)
	if err != nil {
		return uuid.Nil, economicEventWriteError("insert economic normalization", err)
	}
	if err := insertLedgerAggregateStrict(ctx, databaseTransaction, normalization.Transaction); err != nil {
		return uuid.Nil, err
	}
	return persistedID, nil
}

func insertLedgerAggregateStrict(ctx context.Context, databaseTransaction pgx.Tx, transaction *ledger.Transaction) error {
	if transaction == nil {
		return fmt.Errorf("postgres: insert economic ledger aggregate: transaction is required")
	}
	if err := transaction.Validate(); err != nil {
		return fmt.Errorf("postgres: insert economic ledger aggregate: %w", err)
	}
	persistedID, err := insertLedgerTransactionRow(ctx, databaseTransaction, transaction, false)
	if err != nil {
		return economicEventWriteError("insert economic ledger transaction", err)
	}
	if persistedID != transaction.ID {
		return fmt.Errorf("postgres: insert economic ledger transaction returned %s, want %s", persistedID, transaction.ID)
	}
	if err := insertLedgerPostingRows(ctx, databaseTransaction, persistedID, transaction.Postings); err != nil {
		return economicEventWriteError("insert economic ledger postings", err)
	}
	return nil
}

// insertLedgerTransactionRow and insertLedgerPostingRows are the single SQL
// write path for both the legacy ledger repository and economic-event
// normalization. The caller controls only whether an idempotent parent replay
// may return no row; economic normalizations always use the strict path.
func insertLedgerTransactionRow(
	ctx context.Context,
	databaseTransaction pgx.Tx,
	transaction *ledger.Transaction,
	allowReplay bool,
) (uuid.UUID, error) {
	query := `INSERT INTO ledger_transactions (
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		reference_type, reference_id, effective_at, observed_at, metadata,
		posting_count, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,$10,$11,$12,$13)`
	if allowReplay {
		query += ` ON CONFLICT (account_id, idempotency_key) DO NOTHING`
	}
	query += ` RETURNING id`

	var persistedID uuid.UUID
	err := databaseTransaction.QueryRow(ctx, query,
		transaction.ID,
		transaction.AccountID,
		transaction.EventType,
		transaction.IdempotencyKey,
		transaction.OriginType,
		transaction.OriginID,
		transaction.ReferenceType,
		transaction.ReferenceID,
		transaction.EffectiveAt.UTC(),
		transaction.ObservedAt.UTC(),
		jsonForStorage(transaction.Metadata),
		len(transaction.Postings),
		transaction.CreatedAt.UTC(),
	).Scan(&persistedID)
	return persistedID, err
}

func insertLedgerPostingRows(
	ctx context.Context,
	databaseTransaction pgx.Tx,
	transactionID uuid.UUID,
	postings []ledger.Posting,
) error {
	for _, posting := range postings {
		if _, err := databaseTransaction.Exec(ctx, `INSERT INTO ledger_postings (
			id, transaction_id, idempotency_key, ledger_account, unit_kind,
			unit, amount, metadata, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			posting.ID,
			transactionID,
			posting.IdempotencyKey,
			posting.LedgerAccount,
			posting.UnitKind,
			posting.Unit,
			posting.Amount.StringFixed(12),
			jsonForStorage(posting.Metadata),
			posting.CreatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("insert ledger posting %q: %w", posting.IdempotencyKey, err)
		}
	}
	return nil
}

// GetEconomicNormalizationBySourceEventID reloads a complete typed aggregate,
// including immutable account/instrument references and ledger postings.
func (repo *LedgerRepo) GetEconomicNormalizationBySourceEventID(ctx context.Context, sourceEventID uuid.UUID) (*ledger.EconomicNormalization, error) {
	var normalizationID uuid.UUID
	if err := repo.pool.QueryRow(ctx, `SELECT id FROM economic_event_normalizations WHERE source_event_id = $1`, sourceEventID).Scan(&normalizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: find economic normalization for source event %s: %w", sourceEventID, err)
	}
	return repo.getEconomicNormalizationByID(ctx, normalizationID)
}

func (repo *LedgerRepo) getEconomicNormalizationByID(ctx context.Context, id uuid.UUID) (*ledger.EconomicNormalization, error) {
	var normalization ledger.EconomicNormalization
	var sourceEventID, ledgerTransactionID uuid.UUID
	var instrumentID, secondaryInstrumentID, venueContractID, optionTermsID *uuid.UUID
	var quantity, price, costAmount, positionQuantity, settlementPrice *string
	if err := repo.pool.QueryRow(ctx, `SELECT
		id, source_event_id, event_type, normalizer_version,
		execution_origin_type, execution_origin_id, reference_type, reference_id,
		COALESCE(venue, ''), instrument_id, secondary_instrument_id,
		venue_contract_id, option_terms_id, effective_at, cash_currency,
		quantity::TEXT, price::TEXT, COALESCE(cost_kind, ''),
		COALESCE(cost_currency, ''), cost_amount::TEXT,
		position_quantity::TEXT, settlement_price::TEXT,
		ledger_transaction_id, created_at
	FROM economic_event_normalizations WHERE id = $1`, id).Scan(
		&normalization.ID,
		&sourceEventID,
		&normalization.EventType,
		&normalization.NormalizerVersion,
		&normalization.ExecutionOriginType,
		&normalization.ExecutionOriginID,
		&normalization.ReferenceType,
		&normalization.ReferenceID,
		&normalization.Venue,
		&instrumentID,
		&secondaryInstrumentID,
		&venueContractID,
		&optionTermsID,
		&normalization.EffectiveAt,
		&normalization.CashCurrency,
		&quantity,
		&price,
		&normalization.CostKind,
		&normalization.CostCurrency,
		&costAmount,
		&positionQuantity,
		&settlementPrice,
		&ledgerTransactionID,
		&normalization.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get economic normalization %s: %w", id, err)
	}
	var err error
	if normalization.Quantity, err = parseOptionalEconomicDecimal(quantity); err != nil {
		return nil, fmt.Errorf("postgres: parse economic normalization quantity: %w", err)
	}
	if normalization.Price, err = parseOptionalEconomicDecimal(price); err != nil {
		return nil, fmt.Errorf("postgres: parse economic normalization price: %w", err)
	}
	if normalization.CostAmount, err = parseOptionalEconomicDecimal(costAmount); err != nil {
		return nil, fmt.Errorf("postgres: parse economic normalization cost: %w", err)
	}
	if normalization.PositionQuantity, err = parseOptionalEconomicDecimal(positionQuantity); err != nil {
		return nil, fmt.Errorf("postgres: parse economic normalization position: %w", err)
	}
	if normalization.SettlementPrice, err = parseOptionalEconomicDecimal(settlementPrice); err != nil {
		return nil, fmt.Errorf("postgres: parse economic normalization settlement price: %w", err)
	}
	normalization.EffectiveAt = normalization.EffectiveAt.UTC()
	normalization.CreatedAt = normalization.CreatedAt.UTC()
	if normalization.SourceEvent, err = repo.GetEconomicSourceEventByID(ctx, sourceEventID); err != nil {
		return nil, err
	}
	if normalization.Account, err = NewAccountRepo(repo.pool).GetByID(ctx, normalization.SourceEvent.AccountID); err != nil {
		return nil, err
	}
	instrumentRepo := NewInstrumentRepo(repo.pool)
	if instrumentID != nil {
		if normalization.Instrument, err = instrumentRepo.GetInstrumentByID(ctx, *instrumentID); err != nil {
			return nil, err
		}
	}
	if secondaryInstrumentID != nil {
		if normalization.SecondaryInstrument, err = instrumentRepo.GetInstrumentByID(ctx, *secondaryInstrumentID); err != nil {
			return nil, err
		}
	}
	if venueContractID != nil {
		if normalization.VenueContract, err = instrumentRepo.getVenueContractByID(ctx, *venueContractID); err != nil {
			return nil, err
		}
	}
	if optionTermsID != nil {
		if normalization.OptionTerms, err = instrumentRepo.GetOptionContractTermsByID(ctx, *optionTermsID); err != nil {
			return nil, err
		}
	}
	if normalization.Transaction, err = repo.GetByID(ctx, ledgerTransactionID); err != nil {
		return nil, err
	}
	if err := normalization.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded economic normalization %s: %w", id, err)
	}
	return &normalization, nil
}

func parseOptionalEconomicDecimal(value *string) (*decimal.Decimal, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := decimal.NewFromString(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func normalizationInstrumentID(value *instrument.Instrument) *uuid.UUID {
	if value == nil {
		return nil
	}
	id := value.ID
	return &id
}

func normalizationVenueContractID(value *instrument.VenueContract) *uuid.UUID {
	if value == nil {
		return nil
	}
	id := value.ID
	return &id
}

func normalizationOptionTermsID(value *instrument.OptionContractTerms) *uuid.UUID {
	if value == nil {
		return nil
	}
	id := value.ID
	return &id
}

func economicEventWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("postgres: %s: %v: %w", operation, err, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}

func (repo *LedgerRepo) getByIdempotencyKey(ctx context.Context, accountID uuid.UUID, idempotencyKey string) (*ledger.Transaction, error) {
	var id uuid.UUID
	if err := repo.pool.QueryRow(ctx, `SELECT id
		FROM ledger_transactions
		WHERE account_id = $1 AND idempotency_key = $2`, accountID, idempotencyKey).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return repo.GetByID(ctx, id)
}

func sameLedgerTransactionPayload(left, right *ledger.Transaction) bool {
	if left == nil || right == nil {
		return false
	}
	if left.AccountID != right.AccountID ||
		left.EventType != right.EventType ||
		left.IdempotencyKey != right.IdempotencyKey ||
		left.OriginType != right.OriginType ||
		left.OriginID != right.OriginID ||
		left.ReferenceType != right.ReferenceType ||
		left.ReferenceID != right.ReferenceID ||
		!left.EffectiveAt.Equal(right.EffectiveAt) ||
		!jsonObjectsEqual(left.Metadata, right.Metadata) ||
		len(left.Postings) != len(right.Postings) {
		return false
	}

	rightPostings := make(map[string]ledger.Posting, len(right.Postings))
	for _, posting := range right.Postings {
		rightPostings[posting.IdempotencyKey] = posting
	}
	for _, leftPosting := range left.Postings {
		rightPosting, ok := rightPostings[leftPosting.IdempotencyKey]
		if !ok ||
			leftPosting.LedgerAccount != rightPosting.LedgerAccount ||
			leftPosting.UnitKind != rightPosting.UnitKind ||
			leftPosting.Unit != rightPosting.Unit ||
			!leftPosting.Amount.Equal(rightPosting.Amount) ||
			!jsonObjectsEqual(leftPosting.Metadata, rightPosting.Metadata) {
			return false
		}
	}
	return true
}

func (repo *LedgerRepo) GetByID(ctx context.Context, id uuid.UUID) (*ledger.Transaction, error) {
	transaction, err := scanLedgerTransaction(repo.pool.QueryRow(ctx, `SELECT
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		COALESCE(reference_type, ''), COALESCE(reference_id, ''),
		effective_at, observed_at, metadata, created_at
	FROM ledger_transactions
	WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get ledger transaction %s: %w", id, err)
	}

	rows, err := repo.pool.Query(ctx, `SELECT
		id, transaction_id, idempotency_key, ledger_account, unit_kind,
		unit, amount::TEXT, metadata, created_at
	FROM ledger_postings
	WHERE transaction_id = $1
	ORDER BY idempotency_key, id`, id)
	if err != nil {
		return nil, fmt.Errorf("postgres: list ledger postings for transaction %s: %w", id, err)
	}
	defer rows.Close()

	transaction.Postings = make([]ledger.Posting, 0)
	for rows.Next() {
		posting, err := scanLedgerPosting(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan ledger posting for transaction %s: %w", id, err)
		}
		transaction.Postings = append(transaction.Postings, *posting)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list ledger postings for transaction %s: %w", id, err)
	}
	if err := transaction.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded ledger transaction %s: %w", id, err)
	}
	return transaction, nil
}

// GetByOrigin resolves the immutable transaction produced for an authoritative
// source event, then reloads and validates its balanced postings.
func (repo *LedgerRepo) GetByOrigin(ctx context.Context, accountID uuid.UUID, originType, originID string) (*ledger.Transaction, error) {
	var id uuid.UUID
	if err := repo.pool.QueryRow(ctx, `SELECT id FROM ledger_transactions WHERE account_id=$1 AND origin_type=$2 AND origin_id=$3`, accountID, originType, originID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get ledger transaction origin: %w", err)
	}
	return repo.GetByID(ctx, id)
}

func scanLedgerTransaction(row accountRow) (*ledger.Transaction, error) {
	var transaction ledger.Transaction
	var metadata []byte
	if err := row.Scan(
		&transaction.ID,
		&transaction.AccountID,
		&transaction.EventType,
		&transaction.IdempotencyKey,
		&transaction.OriginType,
		&transaction.OriginID,
		&transaction.ReferenceType,
		&transaction.ReferenceID,
		&transaction.EffectiveAt,
		&transaction.ObservedAt,
		&metadata,
		&transaction.CreatedAt,
	); err != nil {
		return nil, err
	}
	transaction.Metadata = append(json.RawMessage(nil), metadata...)
	return &transaction, nil
}

func scanLedgerPosting(row accountRow) (*ledger.Posting, error) {
	var posting ledger.Posting
	var amount string
	var metadata []byte
	if err := row.Scan(
		&posting.ID,
		&posting.TransactionID,
		&posting.IdempotencyKey,
		&posting.LedgerAccount,
		&posting.UnitKind,
		&posting.Unit,
		&amount,
		&metadata,
		&posting.CreatedAt,
	); err != nil {
		return nil, err
	}
	parsedAmount, err := decimal.NewFromString(amount)
	if err != nil {
		return nil, fmt.Errorf("parse ledger posting amount %q: %w", amount, err)
	}
	posting.Amount = parsedAmount
	posting.Metadata = append(json.RawMessage(nil), metadata...)
	return &posting, nil
}

func jsonForStorage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return value
}
