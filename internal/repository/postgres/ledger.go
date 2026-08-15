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

	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// LedgerRepo persists append-only balanced transactions.
type LedgerRepo struct{ pool *pgxpool.Pool }

var _ repository.LedgerRepository = (*LedgerRepo)(nil)

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

	var persistedID uuid.UUID
	err = databaseTransaction.QueryRow(ctx, `INSERT INTO ledger_transactions (
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		reference_type, reference_id, effective_at, observed_at, metadata,
		posting_count, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,$10,$11,$12,$13)
	ON CONFLICT (account_id, idempotency_key) DO NOTHING
	RETURNING id`,
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

	for _, posting := range transaction.Postings {
		if _, err := databaseTransaction.Exec(ctx, `INSERT INTO ledger_postings (
			id, transaction_id, idempotency_key, ledger_account, unit_kind,
			unit, amount, metadata, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			posting.ID,
			persistedID,
			posting.IdempotencyKey,
			posting.LedgerAccount,
			posting.UnitKind,
			posting.Unit,
			posting.Amount.StringFixed(12),
			jsonForStorage(posting.Metadata),
			posting.CreatedAt.UTC(),
		); err != nil {
			return nil, fmt.Errorf("postgres: insert ledger posting %q: %w", posting.IdempotencyKey, err)
		}
	}

	if err := databaseTransaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit ledger transaction %s: %w", transaction.ID, err)
	}
	return repo.GetByID(ctx, persistedID)
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
