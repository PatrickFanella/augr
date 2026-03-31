package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestUserRepoIntegration_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newUserIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewUserRepo(pool)
	user := &domain.User{
		Username: "alice",
		Password: "correct horse battery staple",
	}

	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if user.ID == uuid.Nil {
		t.Fatal("expected Create() to populate ID")
	}
	if user.CreatedAt.IsZero() || user.UpdatedAt.IsZero() {
		t.Fatal("expected Create() to populate timestamps")
	}
	if user.Password != "" {
		t.Fatal("expected Create() to clear plaintext password")
	}
	if user.PasswordHash == "" {
		t.Fatal("expected Create() to populate PasswordHash")
	}
	if user.PasswordHash == "correct horse battery staple" {
		t.Fatal("expected Create() to hash password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("correct horse battery staple")); err != nil {
		t.Fatalf("expected PasswordHash to match original password: %v", err)
	}

	gotByUsername, err := repo.GetByUsername(ctx, user.Username)
	if err != nil {
		t.Fatalf("GetByUsername() error = %v", err)
	}
	if gotByUsername.ID != user.ID {
		t.Fatalf("GetByUsername() ID = %s, want %s", gotByUsername.ID, user.ID)
	}
	if gotByUsername.PasswordHash != user.PasswordHash {
		t.Fatal("GetByUsername() returned unexpected password hash")
	}

	gotByID, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if gotByID.Username != user.Username {
		t.Fatalf("GetByID() Username = %q, want %q", gotByID.Username, user.Username)
	}
	if gotByID.PasswordHash != user.PasswordHash {
		t.Fatal("GetByID() returned unexpected password hash")
	}
}

func TestUserRepoIntegration_CreateDuplicateUsername(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newUserIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewUserRepo(pool)
	first := &domain.User{
		Username: "alice",
		Password: "secret-1",
	}
	second := &domain.User{
		Username: "alice",
		Password: "secret-2",
	}

	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create() first user error = %v", err)
	}

	err := repo.Create(ctx, second)
	if err == nil {
		t.Fatal("expected duplicate username error, got nil")
	}
}

func TestUserRepoIntegration_NotFound(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newUserIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewUserRepo(pool)

	_, err := repo.GetByUsername(ctx, "missing")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetByUsername() error = %v, want ErrNotFound", err)
	}

	_, err = repo.GetByID(ctx, uuid.New())
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func newUserIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to create admin pool: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}

	schemaName := "integration_user_" + uuid.NewString()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = `"` + schemaName + `",public`

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}

	if _, err := pool.Exec(ctx, `CREATE TABLE users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to apply test schema DDL: %v", err)
	}

	cleanup := func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
	}

	return pool, cleanup
}
