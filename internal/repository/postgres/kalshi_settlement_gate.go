package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type KalshiSettlementGateRepo struct{ pool *pgxpool.Pool }

var _ repository.KalshiSettlementGateRepository = (*KalshiSettlementGateRepo)(nil)

func NewKalshiSettlementGateRepo(pool *pgxpool.Pool) *KalshiSettlementGateRepo {
	return &KalshiSettlementGateRepo{pool: pool}
}

func (r *KalshiSettlementGateRepo) Get(ctx context.Context, jobName string) (*domain.KalshiSettlementGateState, error) {
	row := r.pool.QueryRow(ctx, `SELECT job_name, consecutive_successes, threshold, eligible, projection_fingerprint, last_outcome, last_error, fetched, resolved, would_settle_markets, would_settle_decisions, last_run_at, updated_at FROM kalshi_settlement_gate WHERE job_name = $1`, jobName)
	state := &domain.KalshiSettlementGateState{}
	if err := row.Scan(&state.JobName, &state.ConsecutiveSuccesses, &state.Threshold, &state.Eligible, &state.ProjectionFingerprint, &state.LastOutcome, &state.LastError, &state.Fetched, &state.Resolved, &state.WouldSettleMarkets, &state.WouldSettleDecisions, &state.LastRunAt, &state.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get kalshi settlement gate: %w", err)
	}
	return state, nil
}

func (r *KalshiSettlementGateRepo) RecordSuccess(ctx context.Context, jobName string, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions int, projectionFingerprint string, lastRunAt time.Time) (*domain.KalshiSettlementGateState, error) {
	return r.upsert(ctx, jobName, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, projectionFingerprint, lastRunAt, "success", "", true)
}

func (r *KalshiSettlementGateRepo) RecordFailure(ctx context.Context, jobName string, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions int, lastRunAt time.Time, lastError string) (*domain.KalshiSettlementGateState, error) {
	return r.upsert(ctx, jobName, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, "", lastRunAt, "failure", lastError, false)
}

func (r *KalshiSettlementGateRepo) upsert(ctx context.Context, jobName string, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions int, projectionFingerprint string, lastRunAt time.Time, lastOutcome, lastError string, success bool) (*domain.KalshiSettlementGateState, error) {
	consecutive := 0
	eligible := false
	if success {
		consecutive = 1
		eligible = threshold > 0 && consecutive >= threshold
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO kalshi_settlement_gate (job_name, consecutive_successes, threshold, eligible, projection_fingerprint, last_outcome, last_error, fetched, resolved, would_settle_markets, would_settle_decisions, last_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (job_name) DO UPDATE SET
			consecutive_successes = CASE WHEN $13 AND $5 <> '' AND kalshi_settlement_gate.projection_fingerprint = $5 AND kalshi_settlement_gate.last_outcome = 'success' THEN kalshi_settlement_gate.consecutive_successes + 1 WHEN $13 THEN 1 ELSE 0 END,
			threshold = $3,
			eligible = CASE WHEN $13 AND $5 <> '' AND kalshi_settlement_gate.projection_fingerprint = $5 AND kalshi_settlement_gate.last_outcome = 'success' THEN CASE WHEN $3 > 0 AND kalshi_settlement_gate.consecutive_successes + 1 >= $3 THEN true ELSE false END WHEN $13 THEN false ELSE false END,
			projection_fingerprint = CASE WHEN $13 THEN $5 ELSE '' END,
			last_outcome = $6,
			last_error = $7,
			fetched = $8,
			resolved = $9,
			would_settle_markets = $10,
			would_settle_decisions = $11,
			last_run_at = $12,
			updated_at = NOW()`, jobName, consecutive, threshold, eligible, projectionFingerprint, lastOutcome, lastError, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, lastRunAt, success)
	if err != nil {
		return nil, fmt.Errorf("postgres: upsert kalshi settlement gate: %w", err)
	}
	return r.Get(ctx, jobName)
}
