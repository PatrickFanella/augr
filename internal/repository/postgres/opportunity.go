package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// OpportunityRepo implements repository.OpportunityRepository using PostgreSQL.
type OpportunityRepo struct {
	pool *pgxpool.Pool
}

var _ repository.OpportunityRepository = (*OpportunityRepo)(nil)

// NewOpportunityRepo returns a repository backed by the given pool.
func NewOpportunityRepo(pool *pgxpool.Pool) *OpportunityRepo { return &OpportunityRepo{pool: pool} }

// Create inserts a new opportunity.
func (r *OpportunityRepo) Create(ctx context.Context, opportunity *domain.Opportunity) error {
	return r.save(ctx, opportunity, false)
}

// UpsertQueuedByDedupeKey inserts or refreshes a queued opportunity by dedupe key.
func (r *OpportunityRepo) UpsertQueuedByDedupeKey(ctx context.Context, opportunity *domain.Opportunity) error {
	opportunity.Status = domain.OpportunityStatusQueued
	return r.save(ctx, opportunity, true)
}

// Get retrieves an opportunity by ID.
func (r *OpportunityRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Opportunity, error) {
	row := r.pool.QueryRow(ctx, opportunitySelectSQL+` WHERE id = $1`, id)
	opportunity, err := scanOpportunity(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get opportunity %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get opportunity: %w", err)
	}
	return opportunity, nil
}

// List returns opportunities matching the provided filter.
func (r *OpportunityRepo) List(ctx context.Context, filter repository.OpportunityFilter, limit, offset int) ([]domain.Opportunity, error) {
	query, args := buildOpportunityListQuery(filter, limit, offset)
	return r.list(ctx, query, args, "list opportunities")
}

// Count returns the number of opportunities matching the filter.
func (r *OpportunityRepo) Count(ctx context.Context, filter repository.OpportunityFilter) (int, error) {
	query, args := buildOpportunityCountQuery(filter)
	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("postgres: count opportunities: %w", err)
	}
	return total, nil
}

// UpdateStatus updates the status and reject reason for an opportunity.
func (r *OpportunityRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.OpportunityStatus, rejectReason string) error {
	row := r.pool.QueryRow(ctx,
		`UPDATE portfolio_opportunities
		 SET status = $1, reject_reason = $2, updated_at = NOW()
		 WHERE id = $3
		 RETURNING id`,
		status,
		rejectReason,
		id,
	)

	var updatedID uuid.UUID
	if err := row.Scan(&updatedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update opportunity %s: %w", id, ErrNotFound)
		}
		return fmt.Errorf("postgres: update opportunity: %w", err)
	}
	return nil
}

func (r *OpportunityRepo) save(ctx context.Context, opportunity *domain.Opportunity, upsert bool) error {
	evidence, err := marshalOpportunityJSON(opportunity.Evidence)
	if err != nil {
		return err
	}

	query := `INSERT INTO portfolio_opportunities (
		strategy_id, pipeline_run_id, market_type, ticker, side, signal, status, score, confidence,
		edge_pct, expected_return_pct, max_loss_pct, liquidity_usd, spread_pct, proposed_notional,
		selected_notional, reason, reject_reason, evidence, expires_at, dedupe_key
	)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`
	if upsert {
		query += ` ON CONFLICT (dedupe_key) DO UPDATE SET
			strategy_id = EXCLUDED.strategy_id,
			pipeline_run_id = EXCLUDED.pipeline_run_id,
			market_type = EXCLUDED.market_type,
			ticker = EXCLUDED.ticker,
			side = EXCLUDED.side,
			signal = EXCLUDED.signal,
			status = EXCLUDED.status,
			score = EXCLUDED.score,
			confidence = EXCLUDED.confidence,
			edge_pct = EXCLUDED.edge_pct,
			expected_return_pct = EXCLUDED.expected_return_pct,
			max_loss_pct = EXCLUDED.max_loss_pct,
			liquidity_usd = EXCLUDED.liquidity_usd,
			spread_pct = EXCLUDED.spread_pct,
			proposed_notional = EXCLUDED.proposed_notional,
			selected_notional = EXCLUDED.selected_notional,
			reason = EXCLUDED.reason,
			reject_reason = EXCLUDED.reject_reason,
			evidence = EXCLUDED.evidence,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()`
	}
	query += ` RETURNING id, created_at, updated_at`

	row := r.pool.QueryRow(ctx, query,
		opportunity.StrategyID,
		opportunity.PipelineRunID,
		opportunity.MarketType,
		opportunity.Ticker,
		opportunity.Side,
		opportunity.Signal,
		opportunity.Status,
		opportunity.Score,
		opportunity.Confidence,
		opportunity.EdgePct,
		opportunity.ExpectedReturnPct,
		opportunity.MaxLossPct,
		opportunity.LiquidityUSD,
		opportunity.SpreadPct,
		opportunity.ProposedNotional,
		opportunity.SelectedNotional,
		opportunity.Reason,
		opportunity.RejectReason,
		evidence,
		opportunity.ExpiresAt,
		opportunity.DedupeKey,
	)

	if err := row.Scan(&opportunity.ID, &opportunity.CreatedAt, &opportunity.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: save opportunity: %w", err)
	}
	return nil
}

const opportunitySelectSQL = `SELECT id, strategy_id, pipeline_run_id, market_type, ticker, side, signal,
	status, score::double precision, confidence::double precision, edge_pct::double precision,
	expected_return_pct::double precision, max_loss_pct::double precision, liquidity_usd::double precision,
	spread_pct::double precision, proposed_notional::double precision, selected_notional::double precision,
	reason, reject_reason, evidence, expires_at, created_at, updated_at, dedupe_key
	FROM portfolio_opportunities`

func (r *OpportunityRepo) list(ctx context.Context, query string, args []any, op string) ([]domain.Opportunity, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: %s: %w", op, err)
	}
	defer rows.Close()

	var opportunities []domain.Opportunity
	for rows.Next() {
		opportunity, err := scanOpportunity(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: %s scan: %w", op, err)
		}
		opportunities = append(opportunities, *opportunity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: %s rows: %w", op, err)
	}
	return opportunities, nil
}

func scanOpportunity(sc scanner) (*domain.Opportunity, error) {
	var (
		opportunity   domain.Opportunity
		strategyID    uuid.UUID
		pipelineRunID *uuid.UUID
		score         *float64
		evidence      []byte
	)
	if err := sc.Scan(
		&opportunity.ID,
		&strategyID,
		&pipelineRunID,
		&opportunity.MarketType,
		&opportunity.Ticker,
		&opportunity.Side,
		&opportunity.Signal,
		&opportunity.Status,
		&score,
		&opportunity.Confidence,
		&opportunity.EdgePct,
		&opportunity.ExpectedReturnPct,
		&opportunity.MaxLossPct,
		&opportunity.LiquidityUSD,
		&opportunity.SpreadPct,
		&opportunity.ProposedNotional,
		&opportunity.SelectedNotional,
		&opportunity.Reason,
		&opportunity.RejectReason,
		&evidence,
		&opportunity.ExpiresAt,
		&opportunity.CreatedAt,
		&opportunity.UpdatedAt,
		&opportunity.DedupeKey,
	); err != nil {
		return nil, err
	}
	opportunity.StrategyID = strategyID
	opportunity.PipelineRunID = pipelineRunID
	opportunity.Score = score
	opportunity.Evidence = json.RawMessage(evidence)
	return &opportunity, nil
}

func buildOpportunityCountQuery(filter repository.OpportunityFilter) (string, []any) {
	query, args := buildOpportunityQuery("SELECT COUNT(*) FROM portfolio_opportunities", filter, 0, 0, false)
	return query, args
}

func buildOpportunityListQuery(filter repository.OpportunityFilter, limit, offset int) (string, []any) {
	return buildOpportunityQuery(opportunitySelectSQL, filter, limit, offset, true)
}

func buildOpportunityQuery(base string, filter repository.OpportunityFilter, limit, offset int, includePagination bool) (string, []any) {
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

	if filter.Status != "" {
		conditions = append(conditions, "status = "+nextArg(filter.Status))
	}
	if filter.MarketType != "" {
		conditions = append(conditions, "market_type = "+nextArg(filter.MarketType.Normalize()))
	}
	if filter.StrategyID != nil {
		conditions = append(conditions, "strategy_id = "+nextArg(*filter.StrategyID))
	}
	if filter.Ticker != "" {
		conditions = append(conditions, "ticker = "+nextArg(filter.Ticker))
	}
	if filter.ExpiresBefore != nil {
		conditions = append(conditions, "expires_at <= "+nextArg(*filter.ExpiresBefore))
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

func marshalOpportunityJSON(data json.RawMessage) ([]byte, error) {
	if len(data) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("postgres: opportunity evidence is not valid")
	}
	return data, nil
}
