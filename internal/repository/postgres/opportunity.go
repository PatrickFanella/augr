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
		strategy_id, pipeline_run_id, market_type, ticker, side, prediction_side, signal, status, score, confidence,
		edge_pct, expected_return_pct, max_loss_pct, entry_price, liquidity_usd, market_cap_usd, spread_pct, proposed_notional,
		selected_notional, reason, reject_reason, evidence, expires_at, dedupe_key
	)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`
	if upsert {
		query += ` ON CONFLICT (dedupe_key) DO UPDATE SET
			strategy_id = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.strategy_id ELSE portfolio_opportunities.strategy_id END,
			pipeline_run_id = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.pipeline_run_id ELSE portfolio_opportunities.pipeline_run_id END,
			market_type = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.market_type ELSE portfolio_opportunities.market_type END,
			ticker = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.ticker ELSE portfolio_opportunities.ticker END,
			side = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.side ELSE portfolio_opportunities.side END,
			prediction_side = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.prediction_side ELSE portfolio_opportunities.prediction_side END,
			signal = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.signal ELSE portfolio_opportunities.signal END,
			status = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.status ELSE portfolio_opportunities.status END,
			score = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.score ELSE portfolio_opportunities.score END,
			confidence = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.confidence ELSE portfolio_opportunities.confidence END,
			edge_pct = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.edge_pct ELSE portfolio_opportunities.edge_pct END,
			expected_return_pct = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.expected_return_pct ELSE portfolio_opportunities.expected_return_pct END,
			max_loss_pct = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.max_loss_pct ELSE portfolio_opportunities.max_loss_pct END,
			entry_price = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.entry_price ELSE portfolio_opportunities.entry_price END,
			liquidity_usd = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.liquidity_usd ELSE portfolio_opportunities.liquidity_usd END,
			market_cap_usd = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.market_cap_usd ELSE portfolio_opportunities.market_cap_usd END,
			spread_pct = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.spread_pct ELSE portfolio_opportunities.spread_pct END,
			proposed_notional = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.proposed_notional ELSE portfolio_opportunities.proposed_notional END,
			selected_notional = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.selected_notional ELSE portfolio_opportunities.selected_notional END,
			reason = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.reason ELSE portfolio_opportunities.reason END,
			reject_reason = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.reject_reason ELSE portfolio_opportunities.reject_reason END,
			evidence = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.evidence ELSE portfolio_opportunities.evidence END,
			expires_at = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN EXCLUDED.expires_at ELSE portfolio_opportunities.expires_at END,
			updated_at = CASE WHEN portfolio_opportunities.status IN ('queued', 'selected') THEN NOW() ELSE portfolio_opportunities.updated_at END`
	}
	query += ` RETURNING id, created_at, updated_at`

	row := r.pool.QueryRow(ctx, query,
		opportunity.StrategyID,
		opportunity.PipelineRunID,
		opportunity.MarketType,
		opportunity.Ticker,
		opportunity.Side,
		opportunity.PredictionSide,
		opportunity.Signal,
		opportunity.Status,
		opportunity.Score,
		opportunity.Confidence,
		opportunity.EdgePct,
		opportunity.ExpectedReturnPct,
		opportunity.MaxLossPct,
		opportunity.EntryPrice,
		opportunity.LiquidityUSD,
		opportunity.MarketCapUSD,
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

const opportunitySelectSQL = `SELECT id, strategy_id, pipeline_run_id, market_type, ticker, side, prediction_side, signal,
	status, score::double precision, confidence::double precision, edge_pct::double precision,
	expected_return_pct::double precision, max_loss_pct::double precision, entry_price::double precision, liquidity_usd::double precision,
	market_cap_usd::double precision, spread_pct::double precision, proposed_notional::double precision, selected_notional::double precision,
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
		&opportunity.PredictionSide,
		&opportunity.Signal,
		&opportunity.Status,
		&score,
		&opportunity.Confidence,
		&opportunity.EdgePct,
		&opportunity.ExpectedReturnPct,
		&opportunity.MaxLossPct,
		&opportunity.EntryPrice,
		&opportunity.LiquidityUSD,
		&opportunity.MarketCapUSD,
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
