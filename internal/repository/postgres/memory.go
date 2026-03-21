package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// MemoryRepo implements repository.MemoryRepository using PostgreSQL.
type MemoryRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that MemoryRepo satisfies MemoryRepository.
var _ repository.MemoryRepository = (*MemoryRepo)(nil)

// NewMemoryRepo returns a MemoryRepo backed by the given connection pool.
func NewMemoryRepo(pool *pgxpool.Pool) *MemoryRepo {
	return &MemoryRepo{pool: pool}
}

// Create inserts a new agent memory and populates the generated ID and
// CreatedAt on the provided struct. The situation_tsv column is populated
// automatically by the database trigger.
func (r *MemoryRepo) Create(ctx context.Context, memory *domain.AgentMemory) error {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO agent_memories (
			agent_role, situation, recommendation, outcome,
			pipeline_run_id, relevance_score
		)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		memory.AgentRole,
		memory.Situation,
		memory.Recommendation,
		nilIfEmpty(memory.Outcome),
		memory.PipelineRunID,
		memory.RelevanceScore,
	)

	if err := row.Scan(&memory.ID, &memory.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create agent memory: %w", err)
	}

	return nil
}

// Search performs full-text search over stored memories using the provided
// query and filters. When a non-empty query is supplied, results are ranked
// by ts_rank against the situation_tsv column. An empty query returns all
// memories matching the filter, ordered by created_at DESC.
func (r *MemoryRepo) Search(ctx context.Context, query string, filter repository.MemorySearchFilter, limit, offset int) ([]domain.AgentMemory, error) {
	trimmedQuery := strings.TrimSpace(query)
	sqlQuery, args := buildSearchQuery(trimmedQuery, filter, limit, offset)

	rows, err := r.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: search agent memories: %w", err)
	}
	defer rows.Close()

	var memories []domain.AgentMemory
	for rows.Next() {
		m, err := scanAgentMemory(rows, trimmedQuery != "")
		if err != nil {
			return nil, fmt.Errorf("postgres: search agent memories scan: %w", err)
		}
		memories = append(memories, *m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: search agent memories rows: %w", err)
	}

	return memories, nil
}

// Delete removes an agent memory by its ID. It returns ErrNotFound when no row
// matches.
func (r *MemoryRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM agent_memories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres: delete agent memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: delete agent memory %s: %w", id, ErrNotFound)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanAgentMemory scans a single row into an AgentMemory. When withRank is
// true, an extra ts_rank float64 column is expected and consumed (used only
// for ordering). RelevanceScore always reflects the stored column value.
func scanAgentMemory(sc scanner, withRank bool) (*domain.AgentMemory, error) {
	var (
		m              domain.AgentMemory
		outcome        *string
		pipelineRunID  *uuid.UUID
		relevanceScore *float64
	)

	var err error
	if withRank {
		var rank float64 // consumed for ordering; not mapped onto the domain struct
		err = sc.Scan(
			&m.ID,
			&m.AgentRole,
			&m.Situation,
			&m.Recommendation,
			&outcome,
			&pipelineRunID,
			&relevanceScore,
			&m.CreatedAt,
			&rank,
		)
	} else {
		err = sc.Scan(
			&m.ID,
			&m.AgentRole,
			&m.Situation,
			&m.Recommendation,
			&outcome,
			&pipelineRunID,
			&relevanceScore,
			&m.CreatedAt,
		)
	}

	if err != nil {
		return nil, err
	}

	if outcome != nil {
		m.Outcome = *outcome
	}
	m.PipelineRunID = pipelineRunID
	if relevanceScore != nil {
		m.RelevanceScore = relevanceScore
	}

	return &m, nil
}

// buildSearchQuery constructs the SELECT query and arguments for Search with
// dynamic WHERE conditions and optional full-text ranking. All values are
// parameterized.
func buildSearchQuery(query string, filter repository.MemorySearchFilter, limit, offset int) (string, []any) {
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

	hasFTS := query != ""

	// Full-text search condition.
	var rankExpr string
	if hasFTS {
		p := nextArg(query)
		conditions = append(conditions, "situation_tsv @@ plainto_tsquery('english', "+p+")")
		rankExpr = "ts_rank(situation_tsv, plainto_tsquery('english', " + p + "))"
	}

	if filter.AgentRole != "" {
		conditions = append(conditions, "agent_role = "+nextArg(filter.AgentRole))
	}

	if filter.PipelineRunID != nil {
		conditions = append(conditions, "pipeline_run_id = "+nextArg(*filter.PipelineRunID))
	}

	if filter.MinRelevanceScore != nil {
		conditions = append(conditions, "relevance_score >= "+nextArg(*filter.MinRelevanceScore))
	}

	if filter.CreatedAfter != nil {
		conditions = append(conditions, "created_at >= "+nextArg(*filter.CreatedAfter))
	}

	if filter.CreatedBefore != nil {
		conditions = append(conditions, "created_at < "+nextArg(*filter.CreatedBefore))
	}

	// Build SELECT clause.
	selectCols := `id, agent_role, situation, recommendation, outcome,
		 pipeline_run_id, relevance_score, created_at`

	if hasFTS {
		selectCols += ", " + rankExpr + " AS rank"
	}

	base := "SELECT " + selectCols + " FROM agent_memories"

	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Order: by rank (descending) if FTS, otherwise by created_at (descending).
	if hasFTS {
		base += " ORDER BY rank DESC, created_at DESC"
	} else {
		base += " ORDER BY created_at DESC"
	}

	base += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))

	return base, args
}

// nilIfEmpty returns nil when s is empty, otherwise a pointer to s.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
