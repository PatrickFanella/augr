package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// AllocationDecisionRepo implements repository.AllocationDecisionRepository using PostgreSQL.
type AllocationDecisionRepo struct {
	pool *pgxpool.Pool
}

var _ repository.AllocationDecisionRepository = (*AllocationDecisionRepo)(nil)

// NewAllocationDecisionRepo returns a repository backed by the given pool.
func NewAllocationDecisionRepo(pool *pgxpool.Pool) *AllocationDecisionRepo {
	return &AllocationDecisionRepo{pool: pool}
}

// Create inserts a new allocation decision.
func (r *AllocationDecisionRepo) Create(ctx context.Context, decision *domain.AllocationDecision) error {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO allocation_decisions (
			opportunity_id, strategy_id, mode, action, score, notional_usd, quantity, reasons, created_order_id
		)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at`,
		decision.OpportunityID,
		decision.StrategyID,
		decision.Mode,
		decision.Action,
		decision.Score,
		decision.NotionalUSD,
		decision.Quantity,
		stringSliceOrEmpty(decision.Reasons),
		decision.CreatedOrderID,
	)
	if err := row.Scan(&decision.ID, &decision.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create allocation decision: %w", err)
	}
	return nil
}

// List returns allocation decisions matching the filter.
func (r *AllocationDecisionRepo) List(ctx context.Context, filter repository.AllocationDecisionFilter, limit, offset int) ([]domain.AllocationDecision, error) {
	query, args := buildAllocationDecisionListQuery(filter, limit, offset)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list allocation decisions: %w", err)
	}
	defer rows.Close()

	var decisions []domain.AllocationDecision
	for rows.Next() {
		decision, err := scanAllocationDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list allocation decisions scan: %w", err)
		}
		decisions = append(decisions, *decision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list allocation decisions rows: %w", err)
	}
	return decisions, nil
}

// Count returns the total number of allocation decisions matching the filter.
func (r *AllocationDecisionRepo) Count(ctx context.Context, filter repository.AllocationDecisionFilter) (int, error) {
	query, args := buildAllocationDecisionCountQuery(filter)
	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count allocation decisions: %w", err)
	}
	return total, nil
}

const allocationDecisionSelectSQL = `SELECT id, opportunity_id, strategy_id, mode, action,
	score::double precision, notional_usd::double precision, quantity::double precision,
	reasons, created_order_id, created_at
	FROM allocation_decisions`

func scanAllocationDecision(sc scanner) (*domain.AllocationDecision, error) {
	var decision domain.AllocationDecision
	if err := sc.Scan(
		&decision.ID,
		&decision.OpportunityID,
		&decision.StrategyID,
		&decision.Mode,
		&decision.Action,
		&decision.Score,
		&decision.NotionalUSD,
		&decision.Quantity,
		&decision.Reasons,
		&decision.CreatedOrderID,
		&decision.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &decision, nil
}

func buildAllocationDecisionCountQuery(filter repository.AllocationDecisionFilter) (string, []any) {
	query, args := buildAllocationDecisionQuery("SELECT COUNT(*) FROM allocation_decisions", filter, 0, 0, false)
	return query, args
}

func buildAllocationDecisionListQuery(filter repository.AllocationDecisionFilter, limit, offset int) (string, []any) {
	return buildAllocationDecisionQuery(allocationDecisionSelectSQL, filter, limit, offset, true)
}

func buildAllocationDecisionQuery(base string, filter repository.AllocationDecisionFilter, limit, offset int, includePagination bool) (string, []any) {
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

	if filter.Mode != "" {
		conditions = append(conditions, "mode = "+nextArg(filter.Mode))
	}
	if filter.Action != "" {
		conditions = append(conditions, "action = "+nextArg(filter.Action))
	}
	if filter.StrategyID != nil {
		conditions = append(conditions, "strategy_id = "+nextArg(*filter.StrategyID))
	}
	if filter.OpportunityID != nil {
		conditions = append(conditions, "opportunity_id = "+nextArg(*filter.OpportunityID))
	}
	if filter.CreatedAfter != nil {
		conditions = append(conditions, "created_at >= "+nextArg(*filter.CreatedAfter))
	}

	query := base
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	if includePagination {
		query += " ORDER BY created_at DESC, id DESC"
		query += fmt.Sprintf(" LIMIT %s OFFSET %s", nextArg(limit), nextArg(offset))
	}
	return query, args
}
