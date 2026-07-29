package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type KalshiWatchedMarketsRepo struct{ pool *pgxpool.Pool }

type KalshiMarketSnapshotsRepo struct{ pool *pgxpool.Pool }

type KalshiDiscoveryRunRepo struct{ pool *pgxpool.Pool }

var (
	_ repository.KalshiWatchedMarketsRepository  = (*KalshiWatchedMarketsRepo)(nil)
	_ repository.KalshiMarketSnapshotsRepository = (*KalshiMarketSnapshotsRepo)(nil)
	_ repository.KalshiDiscoveryRunRepository    = (*KalshiDiscoveryRunRepo)(nil)
)

func NewKalshiWatchedMarketsRepo(pool *pgxpool.Pool) *KalshiWatchedMarketsRepo {
	return &KalshiWatchedMarketsRepo{pool: pool}
}

func NewKalshiMarketSnapshotsRepo(pool *pgxpool.Pool) *KalshiMarketSnapshotsRepo {
	return &KalshiMarketSnapshotsRepo{pool: pool}
}

func NewKalshiDiscoveryRunRepo(pool *pgxpool.Pool) *KalshiDiscoveryRunRepo {
	return &KalshiDiscoveryRunRepo{pool: pool}
}

func (r *KalshiWatchedMarketsRepo) Upsert(ctx context.Context, market *domain.KalshiWatchedMarket) error {
	if market == nil {
		return fmt.Errorf("postgres: upsert kalshi watched market: nil market")
	}
	if market.AddedAt.IsZero() {
		market.AddedAt = time.Now().UTC()
	}
	row := r.pool.QueryRow(ctx, `INSERT INTO kalshi_watched_markets
		(ticker, event_ticker, title, category, status, close_time, added_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (ticker) DO UPDATE SET
			event_ticker = EXCLUDED.event_ticker,
			title = EXCLUDED.title,
			category = EXCLUDED.category,
			status = EXCLUDED.status,
			close_time = EXCLUDED.close_time,
			updated_at = NOW()
		RETURNING enabled, added_at, updated_at`,
		market.Ticker, market.EventTicker, market.Title, market.Category, market.Status, market.CloseTime, market.AddedAt,
	)
	if err := row.Scan(&market.Enabled, &market.AddedAt, &market.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: upsert kalshi watched market: %w", err)
	}
	return nil
}

func (r *KalshiWatchedMarketsRepo) SetEnabled(ctx context.Context, ticker string, enabled bool) error {
	tag, err := r.pool.Exec(ctx, `UPDATE kalshi_watched_markets SET enabled = $2, updated_at = NOW() WHERE ticker = $1`, ticker, enabled)
	if err != nil {
		return fmt.Errorf("postgres: set kalshi watched market enabled: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (r *KalshiWatchedMarketsRepo) ListEnabled(ctx context.Context) ([]domain.KalshiWatchedMarket, error) {
	rows, err := r.pool.Query(ctx, `SELECT ticker, event_ticker, title, category, status, close_time, enabled, added_at, updated_at
		FROM kalshi_watched_markets
		WHERE enabled = true
		ORDER BY added_at DESC, ticker DESC`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list enabled kalshi watched markets: %w", err)
	}
	defer rows.Close()

	var out []domain.KalshiWatchedMarket
	for rows.Next() {
		market, err := scanKalshiWatchedMarket(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan kalshi watched market: %w", err)
		}
		out = append(out, *market)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list enabled kalshi watched markets rows: %w", err)
	}
	return out, nil
}

func (r *KalshiMarketSnapshotsRepo) Create(ctx context.Context, snapshot *domain.KalshiMarketSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("postgres: create kalshi market snapshot: nil snapshot")
	}
	if snapshot.ID == uuid.Nil {
		snapshot.ID = uuid.New()
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	raw := snapshot.Raw
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	row := r.pool.QueryRow(ctx, `INSERT INTO kalshi_market_snapshots
		(id, ticker, title, status, yes_bid, yes_ask, no_bid, no_ask, volume, open_interest, close_time, raw, captured_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING captured_at`,
		snapshot.ID, snapshot.Ticker, snapshot.Title, snapshot.Status, snapshot.YesBid, snapshot.YesAsk, snapshot.NoBid, snapshot.NoAsk, snapshot.Volume, snapshot.OpenInterest, snapshot.CloseTime, raw, snapshot.CapturedAt,
	)
	if err := row.Scan(&snapshot.CapturedAt); err != nil {
		return fmt.Errorf("postgres: create kalshi market snapshot: %w", err)
	}
	snapshot.Raw = raw
	return nil
}

func (r *KalshiMarketSnapshotsRepo) ListLatestByTicker(ctx context.Context, ticker string, limit int) ([]domain.KalshiMarketSnapshot, error) {
	query, args := buildKalshiSnapshotListQuery(`WHERE ticker = $1`, ticker, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list kalshi snapshots by ticker: %w", err)
	}
	defer rows.Close()

	var out []domain.KalshiMarketSnapshot
	for rows.Next() {
		snapshot, err := scanKalshiMarketSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan kalshi market snapshot: %w", err)
		}
		out = append(out, *snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list kalshi snapshots by ticker rows: %w", err)
	}
	return out, nil
}

func (r *KalshiMarketSnapshotsRepo) ListRecent(ctx context.Context, limit int) ([]domain.KalshiMarketSnapshot, error) {
	query, args := buildKalshiSnapshotListQuery("", "", limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list recent kalshi snapshots: %w", err)
	}
	defer rows.Close()

	var out []domain.KalshiMarketSnapshot
	for rows.Next() {
		snapshot, err := scanKalshiMarketSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan kalshi market snapshot: %w", err)
		}
		out = append(out, *snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list recent kalshi snapshots rows: %w", err)
	}
	return out, nil
}

func (r *KalshiDiscoveryRunRepo) Create(ctx context.Context, run *domain.KalshiDiscoveryRun) error {
	if run == nil {
		return fmt.Errorf("postgres: create kalshi discovery run: nil run")
	}
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	errs, summary, err := marshalKalshiDiscoveryRunJSON(*run)
	if err != nil {
		return err
	}
	if run.Status == "" {
		run.Status = domain.KalshiDiscoveryStatusRunning
	}
	if _, err := r.pool.Exec(ctx, `UPDATE kalshi_discovery_runs
		SET status = 'failed',
		    errors = errors || '["abandoned discovery run recovered before next start"]'::jsonb,
		    finished_at = COALESCE(finished_at, NOW()),
		    updated_at = NOW()
		WHERE status = 'running' AND updated_at < NOW() - INTERVAL '2 hours'`); err != nil {
		return fmt.Errorf("postgres: reconcile stale kalshi discovery runs: %w", err)
	}
	row := r.pool.QueryRow(ctx, `INSERT INTO kalshi_discovery_runs
		(id, status, fetched, screened, proposed, deployed, errors, summary, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING started_at, updated_at`,
		run.ID, run.Status, run.Result.Fetched, run.Result.Screened, run.Result.Proposed, run.Result.Deployed, errs, summary, run.StartedAt,
	)
	if err := row.Scan(&run.StartedAt, &run.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create kalshi discovery run: %w", err)
	}
	return nil
}

func (r *KalshiDiscoveryRunRepo) GetActive(ctx context.Context) (*domain.KalshiDiscoveryRun, error) {
	run, err := scanKalshiDiscoveryRun(r.pool.QueryRow(ctx, kalshiDiscoverySelectSQL+` WHERE status = 'running' ORDER BY updated_at DESC LIMIT 1`))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get active kalshi discovery run: %w", err)
	}
	return run, nil
}

func (r *KalshiDiscoveryRunRepo) Finish(ctx context.Context, run *domain.KalshiDiscoveryRun) error {
	if run == nil {
		return fmt.Errorf("postgres: finish kalshi discovery run: nil run")
	}
	if run.FinishedAt == nil {
		now := time.Now().UTC()
		run.FinishedAt = &now
	}
	errs, summary, err := marshalKalshiDiscoveryRunJSON(*run)
	if err != nil {
		return err
	}
	if run.Status == "" {
		run.Status = domain.KalshiDiscoveryStatusCompleted
	}
	row := r.pool.QueryRow(ctx, `UPDATE kalshi_discovery_runs SET
		status = $2, fetched = $3, screened = $4, proposed = $5, deployed = $6, errors = $7, summary = $8, finished_at = $9, updated_at = NOW()
		WHERE id = $1 RETURNING updated_at`,
		run.ID, run.Status, run.Result.Fetched, run.Result.Screened, run.Result.Proposed, run.Result.Deployed, errs, summary, run.FinishedAt,
	)
	if err := row.Scan(&run.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: finish kalshi discovery run %s: %w", run.ID, repository.ErrNotFound)
		}
		return fmt.Errorf("postgres: finish kalshi discovery run: %w", err)
	}
	return nil
}

func (r *KalshiDiscoveryRunRepo) ListLatest(ctx context.Context, limit int) ([]domain.KalshiDiscoveryRun, error) {
	query, args := buildKalshiDiscoveryListLatestQuery(limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list kalshi discovery runs: %w", err)
	}
	defer rows.Close()

	var runs []domain.KalshiDiscoveryRun
	for rows.Next() {
		run, err := scanKalshiDiscoveryRun(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan kalshi discovery run: %w", err)
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list kalshi discovery runs rows: %w", err)
	}
	return runs, nil
}

const kalshiDiscoverySelectSQL = `SELECT id, status, fetched, screened, proposed, deployed, errors, summary, started_at, finished_at, updated_at FROM kalshi_discovery_runs`

func buildKalshiDiscoveryListLatestQuery(limit int) (string, []any) {
	if limit <= 0 {
		limit = 20
	}
	return kalshiDiscoverySelectSQL + ` ORDER BY started_at DESC, id DESC LIMIT $1`, []any{limit}
}

func buildKalshiSnapshotListQuery(whereClause, ticker string, limit int) (string, []any) {
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT id, ticker, title, status, yes_bid, yes_ask, no_bid, no_ask, volume, open_interest, close_time, raw, captured_at FROM kalshi_market_snapshots`
	args := make([]any, 0, 2)
	if whereClause != "" {
		query += ` ` + whereClause
		args = append(args, ticker)
	}
	query += ` ORDER BY captured_at DESC, id DESC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	return query, args
}

func marshalKalshiDiscoveryRunJSON(run domain.KalshiDiscoveryRun) ([]byte, []byte, error) {
	errs := run.Result.Errors
	if errs == nil {
		errs = []string{}
	}
	if len(run.Result.Summary) == 0 {
		run.Result.Summary = json.RawMessage(`{}`)
	}
	errorsJSON, err := json.Marshal(errs)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: marshal kalshi discovery errors: %w", err)
	}
	summaryJSON, err := json.Marshal(run.Result.Summary)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: marshal kalshi discovery summary: %w", err)
	}
	return errorsJSON, summaryJSON, nil
}

func scanKalshiWatchedMarket(sc scanner) (*domain.KalshiWatchedMarket, error) {
	var market domain.KalshiWatchedMarket
	if err := sc.Scan(&market.Ticker, &market.EventTicker, &market.Title, &market.Category, &market.Status, &market.CloseTime, &market.Enabled, &market.AddedAt, &market.UpdatedAt); err != nil {
		return nil, err
	}
	return &market, nil
}

func scanKalshiMarketSnapshot(sc scanner) (*domain.KalshiMarketSnapshot, error) {
	var snapshot domain.KalshiMarketSnapshot
	if err := sc.Scan(&snapshot.ID, &snapshot.Ticker, &snapshot.Title, &snapshot.Status, &snapshot.YesBid, &snapshot.YesAsk, &snapshot.NoBid, &snapshot.NoAsk, &snapshot.Volume, &snapshot.OpenInterest, &snapshot.CloseTime, &snapshot.Raw, &snapshot.CapturedAt); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func scanKalshiDiscoveryRun(sc scanner) (*domain.KalshiDiscoveryRun, error) {
	var run domain.KalshiDiscoveryRun
	var errorsJSON, summaryJSON []byte
	if err := sc.Scan(&run.ID, &run.Status, &run.Result.Fetched, &run.Result.Screened, &run.Result.Proposed, &run.Result.Deployed, &errorsJSON, &summaryJSON, &run.StartedAt, &run.FinishedAt, &run.UpdatedAt); err != nil {
		return nil, err
	}
	if len(errorsJSON) == 0 {
		errorsJSON = []byte(`[]`)
	}
	if len(summaryJSON) == 0 {
		summaryJSON = []byte(`{}`)
	}
	if err := json.Unmarshal(errorsJSON, &run.Result.Errors); err != nil {
		return nil, fmt.Errorf("postgres: unmarshal kalshi discovery errors: %w", err)
	}
	run.Result.Summary = json.RawMessage(summaryJSON)
	return &run, nil
}
