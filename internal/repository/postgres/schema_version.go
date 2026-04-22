package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RequiredSchemaVersion is the minimum schema version this runtime requires.
const RequiredSchemaVersion = 32

type schemaVersionState string

const (
	schemaVersionBehind schemaVersionState = "behind"
	schemaVersionMatch  schemaVersionState = "match"
	schemaVersionAhead  schemaVersionState = "ahead"
)

type schemaVersionRow interface {
	Scan(dest ...any) error
}

type schemaVersionQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) schemaVersionRow
}

type poolSchemaVersionQuerier struct {
	pool *pgxpool.Pool
}

func (q poolSchemaVersionQuerier) QueryRow(ctx context.Context, sql string, args ...any) schemaVersionRow {
	return q.pool.QueryRow(ctx, sql, args...)
}

// CurrentSchemaVersion returns the latest applied schema version recorded in
// schema_migrations.
func CurrentSchemaVersion(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	return currentSchemaVersion(ctx, poolSchemaVersionQuerier{pool: pool})
}

func currentSchemaVersion(ctx context.Context, querier schemaVersionQuerier) (int, error) {
	var version int
	if err := querier.QueryRow(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version); err != nil {
		if err == pgx.ErrNoRows {
			return 0, fmt.Errorf("postgres: read schema version: schema_migrations is empty: %w", err)
		}
		return 0, fmt.Errorf("postgres: read schema version: %w", err)
	}

	return version, nil
}

// CompareSchemaVersion compares current against required.
func CompareSchemaVersion(current, required int) schemaVersionState {
	switch {
	case current < required:
		return schemaVersionBehind
	case current > required:
		return schemaVersionAhead
	default:
		return schemaVersionMatch
	}
}
