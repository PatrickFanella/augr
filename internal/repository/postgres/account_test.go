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

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestAccountRepoCreatesAccountWithOpeningCapital(t *testing.T) {
	ctx := context.Background()
	pool := newAccountIntegrationPool(t, ctx)
	repo := NewAccountRepo(pool)

	account, err := domain.NewAccount(domain.AccountInput{
		Name:                  "Five hundred dollar scored account",
		Environment:           domain.AccountEnvironmentPaperScored,
		Venue:                 "internal",
		BaseCurrency:          "USD",
		StorageNamespace:      "paper_scored/" + uuid.NewString(),
		StartingCapital:       decimal.NewFromInt(500),
		BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile:         domain.MarginProfileCash,
		CreatedBy:             "repository-test",
		CreationMetadata:      json.RawMessage(`{"purpose":"small-capital test"}`),
	})
	if err != nil {
		t.Fatalf("NewAccount() error = %v", err)
	}
	if err := repo.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.GetByID(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ID != account.ID || !got.StartingCapital.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("GetByID() = %+v, want account %s with 500 starting capital", got, account.ID)
	}
	if string(got.CreationMetadata) != `{"purpose": "small-capital test"}` {
		t.Fatalf("CreationMetadata = %s, want persisted object", got.CreationMetadata)
	}

	flows, err := repo.ListCapitalFlows(ctx, account.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListCapitalFlows() error = %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("opening flows = %d, want 1", len(flows))
	}
	opening := flows[0]
	if opening.Type != domain.CapitalFlowTypeDeposit ||
		opening.Source != domain.CapitalFlowSourceAccountOpening ||
		opening.IdempotencyKey != "account-opening:"+account.ID.String() ||
		!opening.Amount.Equal(account.StartingCapital) {
		t.Fatalf("opening flow = %+v, want matching opening deposit", opening)
	}
}

func TestAccountRepoReplaysIdenticalCapitalFlow(t *testing.T) {
	ctx := context.Background()
	pool := newAccountIntegrationPool(t, ctx)
	repo := NewAccountRepo(pool)

	account, err := domain.NewAccount(domain.AccountInput{
		Name:                  "Capital flow replay account",
		Environment:           domain.AccountEnvironmentPaperScored,
		Venue:                 "internal",
		BaseCurrency:          "USD",
		StorageNamespace:      "paper_scored/" + uuid.NewString(),
		StartingCapital:       decimal.NewFromInt(500),
		BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile:         domain.MarginProfileCash,
		CreatedBy:             "repository-test",
	})
	if err != nil {
		t.Fatalf("NewAccount() error = %v", err)
	}
	if err := repo.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	flow, err := domain.NewCapitalFlow(domain.CapitalFlowInput{
		AccountID:         account.ID,
		Type:              domain.CapitalFlowTypeDeposit,
		Amount:            decimal.NewFromInt(4_500),
		Currency:          "USD",
		IdempotencyKey:    "tier-5000",
		Source:            domain.CapitalFlowSourceOperator,
		ExternalReference: "transfer-tier-5000",
		EffectiveAt:       time.Date(2026, 8, 15, 14, 0, 0, 123456789, time.UTC),
		ObservedAt:        time.Date(2026, 8, 15, 14, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewCapitalFlow() error = %v", err)
	}
	created, err := repo.RecordCapitalFlow(ctx, flow)
	if err != nil {
		t.Fatalf("RecordCapitalFlow(first) error = %v", err)
	}

	retry := *flow
	retry.ID = uuid.New()
	replayed, err := repo.RecordCapitalFlow(ctx, &retry)
	if err != nil {
		t.Fatalf("RecordCapitalFlow(retry) error = %v", err)
	}
	if replayed.ID != created.ID {
		t.Fatalf("replayed ID = %s, want original %s", replayed.ID, created.ID)
	}

	flows, err := repo.ListCapitalFlows(ctx, account.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListCapitalFlows() error = %v", err)
	}
	if len(flows) != 2 {
		t.Fatalf("capital flow rows = %d, want opening plus one deposit", len(flows))
	}
}

func TestAccountRepoRejectsCapitalFlowMetadataConflictBeyondFloatPrecision(t *testing.T) {
	ctx := context.Background()
	pool := newAccountIntegrationPool(t, ctx)
	repo := NewAccountRepo(pool)

	account, err := domain.NewAccount(domain.AccountInput{
		Name:                  "Capital-flow metadata precision account",
		Environment:           domain.AccountEnvironmentPaperScored,
		Venue:                 "internal",
		BaseCurrency:          "USD",
		StorageNamespace:      "paper_scored/" + uuid.NewString(),
		StartingCapital:       decimal.NewFromInt(500),
		BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile:         domain.MarginProfileCash,
		CreatedBy:             "repository-test",
	})
	if err != nil {
		t.Fatalf("NewAccount() error = %v", err)
	}
	if err := repo.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	newFlow := func(metadata json.RawMessage) *domain.CapitalFlow {
		flow, err := domain.NewCapitalFlow(domain.CapitalFlowInput{
			AccountID:      account.ID,
			Type:           domain.CapitalFlowTypeDeposit,
			Amount:         decimal.NewFromInt(1),
			Currency:       "USD",
			IdempotencyKey: "large-json-number",
			Source:         domain.CapitalFlowSourceOperator,
			Metadata:       metadata,
			EffectiveAt:    time.Date(2026, 8, 15, 15, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("NewCapitalFlow() error = %v", err)
		}
		return flow
	}

	if _, err := repo.RecordCapitalFlow(ctx, newFlow(json.RawMessage(`{"sequence":9007199254740992}`))); err != nil {
		t.Fatalf("RecordCapitalFlow(first) error = %v", err)
	}
	if _, err := repo.RecordCapitalFlow(ctx, newFlow(json.RawMessage(`{"sequence":9007199254740993}`))); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("RecordCapitalFlow(metadata conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestAccountRepoRejectsCapitalFlowIdempotencyConflict(t *testing.T) {
	ctx := context.Background()
	pool := newAccountIntegrationPool(t, ctx)
	repo := NewAccountRepo(pool)

	account, err := domain.NewAccount(domain.AccountInput{
		Name:                  "Capital flow conflict account",
		Environment:           domain.AccountEnvironmentPaperScored,
		Venue:                 "internal",
		BaseCurrency:          "USD",
		StorageNamespace:      "paper_scored/" + uuid.NewString(),
		StartingCapital:       decimal.NewFromInt(5_000),
		BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile:         domain.MarginProfileCash,
		CreatedBy:             "repository-test",
	})
	if err != nil {
		t.Fatalf("NewAccount() error = %v", err)
	}
	if err := repo.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	flow, err := domain.NewCapitalFlow(domain.CapitalFlowInput{
		AccountID:      account.ID,
		Type:           domain.CapitalFlowTypeDeposit,
		Amount:         decimal.NewFromInt(20_000),
		Currency:       "USD",
		IdempotencyKey: "tier-25000",
		Source:         domain.CapitalFlowSourceOperator,
		EffectiveAt:    time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewCapitalFlow() error = %v", err)
	}
	if _, err := repo.RecordCapitalFlow(ctx, flow); err != nil {
		t.Fatalf("RecordCapitalFlow(first) error = %v", err)
	}

	conflict := *flow
	conflict.ID = uuid.New()
	conflict.Amount = decimal.NewFromInt(95_000)
	if _, err := repo.RecordCapitalFlow(ctx, &conflict); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("RecordCapitalFlow(conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestAccountRepoCapitalTiersPreserveOpeningHistoryAndReconcile(t *testing.T) {
	ctx := context.Background()
	pool := newAccountIntegrationPool(t, ctx)
	repo := NewAccountRepo(pool)

	account, err := domain.NewAccount(domain.AccountInput{
		Name:                  "Capital tier account",
		Environment:           domain.AccountEnvironmentPaperScored,
		Venue:                 "internal",
		BaseCurrency:          "USD",
		StorageNamespace:      "paper_scored/" + uuid.NewString(),
		StartingCapital:       decimal.NewFromInt(500),
		BuyingPowerMultiplier: decimal.NewFromInt(1),
		MarginProfile:         domain.MarginProfileCash,
		CreatedBy:             "repository-test",
	})
	if err != nil {
		t.Fatalf("NewAccount() error = %v", err)
	}
	if err := repo.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	deposits := []int64{4_500, 20_000, 75_000, 900_000, 4_000_000}
	for index, amount := range deposits {
		flow, err := domain.NewCapitalFlow(domain.CapitalFlowInput{
			AccountID:      account.ID,
			Type:           domain.CapitalFlowTypeDeposit,
			Amount:         decimal.NewFromInt(amount),
			Currency:       "USD",
			IdempotencyKey: "capital-tier-" + decimal.NewFromInt(amount).String(),
			Source:         domain.CapitalFlowSourceOperator,
			EffectiveAt:    time.Date(2026, 8, 17+index, 14, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("NewCapitalFlow(%d) error = %v", amount, err)
		}
		if _, err := repo.RecordCapitalFlow(ctx, flow); err != nil {
			t.Fatalf("RecordCapitalFlow(%d) error = %v", amount, err)
		}
	}

	summary, err := repo.GetCapitalSummary(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetCapitalSummary() error = %v", err)
	}
	if !summary.StartingCapital.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("StartingCapital = %s, want original 500", summary.StartingCapital)
	}
	if !summary.Deposits.Equal(decimal.NewFromInt(5_000_000)) ||
		!summary.Withdrawals.IsZero() ||
		!summary.NetCapital.Equal(decimal.NewFromInt(5_000_000)) {
		t.Fatalf("capital summary = %+v, want reconciled 5000000", summary)
	}
	if summary.FlowCount != 6 {
		t.Fatalf("FlowCount = %d, want opening plus five tier deposits", summary.FlowCount)
	}

	reloaded, err := repo.GetByID(ctx, account.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if !reloaded.StartingCapital.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("account starting capital changed to %s", reloaded.StartingCapital)
	}
}

func newAccountIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping account integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "integration_account_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create integration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop integration schema: %v", err)
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
