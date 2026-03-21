package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// AuditLogRepo implements repository.AuditLogRepository using PostgreSQL.
type AuditLogRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that AuditLogRepo satisfies AuditLogRepository.
var _ repository.AuditLogRepository = (*AuditLogRepo)(nil)

// NewAuditLogRepo returns an AuditLogRepo backed by the given connection pool.
func NewAuditLogRepo(pool *pgxpool.Pool) *AuditLogRepo {
	return &AuditLogRepo{pool: pool}
}

const auditLogSelectSQL = `SELECT id, event_type, entity_type, entity_id, actor, details, created_at FROM audit_log`

// Create inserts a new audit log entry and populates the generated ID and
// CreatedAt on the provided struct.
func (r *AuditLogRepo) Create(ctx context.Context, entry *domain.AuditLogEntry) error {
	details, err := marshalDetails(entry.Details)
	if err != nil {
		return err
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO audit_log (event_type, entity_type, entity_id, actor, details)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		entry.EventType,
		nullString(entry.EntityType),
		entry.EntityID,
		nullString(entry.Actor),
		details,
	)

	if err := row.Scan(&entry.ID, &entry.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create audit log entry: %w", err)
	}

	return nil
}

// Query returns audit log entries that match the provided filter, ordered by
// created_at descending. Use limit and offset for pagination.
func (r *AuditLogRepo) Query(ctx context.Context, filter repository.AuditLogFilter, limit, offset int) ([]domain.AuditLogEntry, error) {
	query, args := buildAuditLogQuery(filter, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: query audit log: %w", err)
	}
	defer rows.Close()

	var entries []domain.AuditLogEntry
	for rows.Next() {
		entry, err := scanAuditLogEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan audit log entry: %w", err)
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate audit log rows: %w", err)
	}

	return entries, nil
}

// buildAuditLogQuery constructs the SELECT query and arguments for Query.
func buildAuditLogQuery(filter repository.AuditLogFilter, limit, offset int) (string, []any) {
	var (
		conditions []string
		args       []any
		argIdx     int
	)

	nextArg := func(v any) string {
		argIdx++
		args = append(args, v)
		return fmt.Sprintf("$%d", argIdx)
	}

	if filter.EventType != "" {
		conditions = append(conditions, "event_type = "+nextArg(filter.EventType))
	}

	if filter.EntityType != "" {
		conditions = append(conditions, "entity_type = "+nextArg(filter.EntityType))
	}

	if filter.EntityID != nil {
		conditions = append(conditions, "entity_id = "+nextArg(filter.EntityID))
	}

	if filter.Actor != "" {
		conditions = append(conditions, "actor = "+nextArg(filter.Actor))
	}

	if filter.CreatedAfter != nil {
		conditions = append(conditions, "created_at >= "+nextArg(*filter.CreatedAfter))
	}

	if filter.CreatedBefore != nil {
		conditions = append(conditions, "created_at <= "+nextArg(*filter.CreatedBefore))
	}

	query := auditLogSelectSQL
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY created_at DESC, id DESC"
	query += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return query, args
}

// scanAuditLogEntry scans a single row into an AuditLogEntry.
func scanAuditLogEntry(sc scanner) (*domain.AuditLogEntry, error) {
	var (
		entry      domain.AuditLogEntry
		entityType *string
		actor      *string
		detailsRaw []byte
	)

	err := sc.Scan(
		&entry.ID,
		&entry.EventType,
		&entityType,
		&entry.EntityID,
		&actor,
		&detailsRaw,
		&entry.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if entityType != nil {
		entry.EntityType = *entityType
	}

	if actor != nil {
		entry.Actor = *actor
	}

	if detailsRaw != nil {
		entry.Details = json.RawMessage(detailsRaw)
	}

	return &entry, nil
}

// marshalDetails ensures the details JSONB value is valid JSON.
// A nil or empty value is stored as SQL NULL.
func marshalDetails(data json.RawMessage) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	if !json.Valid(data) {
		return nil, fmt.Errorf("postgres: audit log entry details is not valid JSON")
	}

	return data, nil
}
