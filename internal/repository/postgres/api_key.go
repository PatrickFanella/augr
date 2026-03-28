package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// APIKeyRepo implements repository.APIKeyRepository using PostgreSQL.
type APIKeyRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that APIKeyRepo satisfies APIKeyRepository.
var _ repository.APIKeyRepository = (*APIKeyRepo)(nil)

// NewAPIKeyRepo returns an APIKeyRepo backed by the given connection pool.
func NewAPIKeyRepo(pool *pgxpool.Pool) *APIKeyRepo {
	return &APIKeyRepo{pool: pool}
}

// Create inserts a new hashed API key record and populates generated metadata.
func (r *APIKeyRepo) Create(ctx context.Context, key *domain.APIKey) error {
	if err := key.Validate(); err != nil {
		return fmt.Errorf("postgres: validate api key: %w", err)
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO api_keys (name, key_prefix, key_hash, rate_limit_per_minute, expires_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, last_used_at, created_at, updated_at`,
		key.Name,
		key.KeyPrefix,
		key.KeyHash,
		key.RateLimitPerMinute,
		key.ExpiresAt,
		key.RevokedAt,
	)

	if err := row.Scan(&key.ID, &key.LastUsedAt, &key.CreatedAt, &key.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create api key: %w", err)
	}

	return nil
}

// GetByPrefix retrieves an API key record by its stored public prefix.
func (r *APIKeyRepo) GetByPrefix(ctx context.Context, prefix string) (*domain.APIKey, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, key_prefix, key_hash, rate_limit_per_minute, last_used_at, expires_at, revoked_at, created_at, updated_at
		 FROM api_keys
		 WHERE key_prefix = $1`,
		prefix,
	)

	key, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get api key %s: %w", prefix, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get api key: %w", err)
	}

	return key, nil
}

// List returns API key records in reverse creation order.
func (r *APIKeyRepo) List(ctx context.Context, limit, offset int) ([]domain.APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, key_prefix, key_hash, rate_limit_per_minute, last_used_at, expires_at, revoked_at, created_at, updated_at
		 FROM api_keys
		 ORDER BY created_at DESC
		 LIMIT $1 OFFSET $2`,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list api keys: %w", err)
	}
	defer rows.Close()

	var keys []domain.APIKey
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list api keys scan: %w", err)
		}
		keys = append(keys, *key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list api keys rows: %w", err)
	}

	return keys, nil
}

// Revoke marks an API key as revoked.
func (r *APIKeyRepo) Revoke(ctx context.Context, id uuid.UUID, revokedAt time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys
		 SET revoked_at = $1, updated_at = NOW()
		 WHERE id = $2`,
		revokedAt,
		id,
	)
	if err != nil {
		return fmt.Errorf("postgres: revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: revoke api key %s: %w", id, ErrNotFound)
	}
	return nil
}

// TouchLastUsed updates the last-used timestamp for an API key.
func (r *APIKeyRepo) TouchLastUsed(ctx context.Context, id uuid.UUID, lastUsedAt time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys
		 SET last_used_at = $1, updated_at = NOW()
		 WHERE id = $2`,
		lastUsedAt,
		id,
	)
	if err != nil {
		return fmt.Errorf("postgres: touch api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: touch api key %s: %w", id, ErrNotFound)
	}
	return nil
}

func scanAPIKey(sc scanner) (*domain.APIKey, error) {
	var key domain.APIKey
	err := sc.Scan(
		&key.ID,
		&key.Name,
		&key.KeyPrefix,
		&key.KeyHash,
		&key.RateLimitPerMinute,
		&key.LastUsedAt,
		&key.ExpiresAt,
		&key.RevokedAt,
		&key.CreatedAt,
		&key.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &key, nil
}
