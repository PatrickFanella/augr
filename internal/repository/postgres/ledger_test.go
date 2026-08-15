package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestLedgerRepoPostsBalancedTransaction(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)

	effectiveAt := time.Date(2026, 8, 14, 20, 0, 0, 0, time.UTC)
	transaction, err := ledger.NewTransaction(ledger.TransactionInput{
		AccountID:      uuid.MustParse("00000000-0000-4000-8000-000000000064"),
		EventType:      "operator.adjustment",
		IdempotencyKey: "operator-adjustment:" + uuid.NewString(),
		OriginType:     "operator",
		OriginID:       uuid.NewString(),
		EffectiveAt:    effectiveAt,
		Metadata:       json.RawMessage(`{"reason":"repository tracer"}`),
		Postings: []ledger.PostingInput{
			{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(10)},
			{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-10)},
		},
	})
	if err != nil {
		t.Fatalf("ledger.NewTransaction() error = %v", err)
	}

	created, err := repo.PostTransaction(ctx, transaction)
	if err != nil {
		t.Fatalf("PostTransaction() error = %v", err)
	}
	if created.ID != transaction.ID || len(created.Postings) != 2 {
		t.Fatalf("created transaction = %s with %d postings, want %s with 2", created.ID, len(created.Postings), transaction.ID)
	}

	got, err := repo.GetByID(ctx, transaction.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.EventType != transaction.EventType || !got.EffectiveAt.Equal(effectiveAt) {
		t.Fatalf("GetByID() event/time = %q/%s, want %q/%s", got.EventType, got.EffectiveAt, transaction.EventType, effectiveAt)
	}
	if len(got.Postings) != 2 || !got.Postings[0].Amount.Add(got.Postings[1].Amount).IsZero() {
		t.Fatalf("GetByID() postings = %+v, want two balanced lines", got.Postings)
	}
	if string(got.Metadata) != `{"reason": "repository tracer"}` {
		t.Fatalf("GetByID() metadata = %s", got.Metadata)
	}
}

func TestLedgerRepoReplaysIdenticalTransaction(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	originID := uuid.NewString()
	idempotencyKey := "replay:" + originID
	effectiveAt := time.Date(2026, 8, 15, 12, 0, 0, 123456789, time.UTC)

	newTransaction := func() *ledger.Transaction {
		transaction, err := ledger.NewTransaction(ledger.TransactionInput{
			AccountID:      accountID,
			EventType:      "operator.replay_test",
			IdempotencyKey: idempotencyKey,
			OriginType:     "operator",
			OriginID:       originID,
			EffectiveAt:    effectiveAt,
			Metadata:       json.RawMessage(`{"b":2,"a":1}`),
			Postings: []ledger.PostingInput{
				{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(25)},
				{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-25)},
			},
		})
		if err != nil {
			t.Fatalf("ledger.NewTransaction() error = %v", err)
		}
		return transaction
	}

	created, err := repo.PostTransaction(ctx, newTransaction())
	if err != nil {
		t.Fatalf("PostTransaction(first) error = %v", err)
	}
	replayed, err := repo.PostTransaction(ctx, newTransaction())
	if err != nil {
		t.Fatalf("PostTransaction(retry) error = %v", err)
	}
	if replayed.ID != created.ID {
		t.Fatalf("replayed ID = %s, want original %s", replayed.ID, created.ID)
	}
}

func TestLedgerRepoRejectsIdempotencyPayloadConflict(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	originID := uuid.NewString()
	idempotencyKey := "conflict:" + originID

	newTransaction := func(amount int64) *ledger.Transaction {
		transaction, err := ledger.NewTransaction(ledger.TransactionInput{
			AccountID:      accountID,
			EventType:      "operator.conflict_test",
			IdempotencyKey: idempotencyKey,
			OriginType:     "operator",
			OriginID:       originID,
			EffectiveAt:    time.Date(2026, 8, 15, 13, 0, 0, 0, time.UTC),
			Postings: []ledger.PostingInput{
				{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(amount)},
				{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-amount)},
			},
		})
		if err != nil {
			t.Fatalf("ledger.NewTransaction() error = %v", err)
		}
		return transaction
	}

	if _, err := repo.PostTransaction(ctx, newTransaction(25)); err != nil {
		t.Fatalf("PostTransaction(first) error = %v", err)
	}
	if _, err := repo.PostTransaction(ctx, newTransaction(26)); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("PostTransaction(conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestLedgerRepoRejectsMetadataConflictBeyondFloatPrecision(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	originID := uuid.NewString()

	newTransaction := func(metadata json.RawMessage) *ledger.Transaction {
		transaction, err := ledger.NewTransaction(ledger.TransactionInput{
			AccountID:      accountID,
			EventType:      "operator.metadata_precision_test",
			IdempotencyKey: "metadata-precision:" + originID,
			OriginType:     "operator",
			OriginID:       originID,
			EffectiveAt:    time.Date(2026, 8, 15, 13, 30, 0, 0, time.UTC),
			Metadata:       metadata,
			Postings: []ledger.PostingInput{
				{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
				{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
			},
		})
		if err != nil {
			t.Fatalf("ledger.NewTransaction() error = %v", err)
		}
		return transaction
	}

	if _, err := repo.PostTransaction(ctx, newTransaction(json.RawMessage(`{"sequence":9007199254740992}`))); err != nil {
		t.Fatalf("PostTransaction(first) error = %v", err)
	}
	if _, err := repo.PostTransaction(ctx, newTransaction(json.RawMessage(`{"sequence":9007199254740993}`))); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("PostTransaction(metadata conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestLedgerRepoRejectsDuplicateOriginWithDifferentKey(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	originID := uuid.NewString()

	newTransaction := func(idempotencyKey string) *ledger.Transaction {
		transaction, err := ledger.NewTransaction(ledger.TransactionInput{
			AccountID:      accountID,
			EventType:      "operator.origin_test",
			IdempotencyKey: idempotencyKey,
			OriginType:     "operator",
			OriginID:       originID,
			EffectiveAt:    time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC),
			Postings: []ledger.PostingInput{
				{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(25)},
				{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-25)},
			},
		})
		if err != nil {
			t.Fatalf("ledger.NewTransaction() error = %v", err)
		}
		return transaction
	}

	if _, err := repo.PostTransaction(ctx, newTransaction("origin-first:"+originID)); err != nil {
		t.Fatalf("PostTransaction(first) error = %v", err)
	}
	if _, err := repo.PostTransaction(ctx, newTransaction("origin-second:"+originID)); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("PostTransaction(duplicate origin) error = %v, want ErrIdempotencyConflict", err)
	}
}

func newLedgerIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping ledger integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "integration_ledger_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create ledger integration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop ledger integration schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	migrationDirectory := filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations")
	for _, migrationName := range []string{
		"000064_accounts_capital_flows.up.sql",
		"000065_immutable_ledger.up.sql",
	} {
		migration, err := os.ReadFile(filepath.Join(migrationDirectory, migrationName))
		if err != nil {
			t.Fatalf("read %s: %v", migrationName, err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", migrationName, err)
		}
	}
	return pool
}
