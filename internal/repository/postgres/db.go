package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool and provides a shared connection pool for
// all PostgreSQL repository implementations.
type DB struct {
	Pool *pgxpool.Pool
}

// NewDB creates a connection pool using the provided connection string and
// returns a DB handle. The caller is responsible for calling Close when the
// pool is no longer needed.
func NewDB(ctx context.Context, connString string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("postgres: create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close releases all connections held by the pool.
func (db *DB) Close() {
	db.Pool.Close()
}

func (db *DB) GetProviderCooldown(ctx context.Context, provider string) (time.Time, error) {
	var until time.Time
	err := db.Pool.QueryRow(ctx, `SELECT retry_after_until FROM provider_rate_limit_cooldowns WHERE provider = $1`, provider).Scan(&until)
	if err != nil {
		if err == pgx.ErrNoRows {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return until, nil
}

func (db *DB) SetProviderCooldown(ctx context.Context, provider string, until time.Time) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO provider_rate_limit_cooldowns (provider, retry_after_until, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (provider) DO UPDATE SET retry_after_until = GREATEST(provider_rate_limit_cooldowns.retry_after_until, EXCLUDED.retry_after_until), updated_at = NOW()`, provider, until)
	return err
}

func (db *DB) CompareAndClearProviderCooldown(ctx context.Context, provider string, observed time.Time) (bool, error) {
	ct, err := db.Pool.Exec(ctx, `DELETE FROM provider_rate_limit_cooldowns WHERE provider = $1 AND retry_after_until = $2 AND retry_after_until <= NOW()`, provider, observed)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}
