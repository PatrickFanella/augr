package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// PolymarketAccountRepo implements repository.PolymarketAccountRepository.
type PolymarketAccountRepo struct {
	pool *pgxpool.Pool
}

var _ repository.PolymarketAccountRepository = (*PolymarketAccountRepo)(nil)

// NewPolymarketAccountRepo returns a PolymarketAccountRepo backed by the given pool.
func NewPolymarketAccountRepo(pool *pgxpool.Pool) *PolymarketAccountRepo {
	return &PolymarketAccountRepo{pool: pool}
}

// UpsertAccount inserts or updates a Polymarket account profile.
func (r *PolymarketAccountRepo) UpsertAccount(ctx context.Context, acc *domain.PolymarketAccount) error {
	statsJSON, _ := json.Marshal(acc.CategoryStats)

	_, err := r.pool.Exec(ctx, `
		INSERT INTO polymarket_accounts (
			address, display_name, first_seen, last_active,
			total_trades, total_volume, markets_entered, markets_won, markets_lost,
			win_rate, category_stats, avg_position, max_position,
			avg_entry_hours_before_resolution, early_entry_rate, tags, tracked, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,NOW())
		ON CONFLICT (address) DO UPDATE SET
			display_name       = EXCLUDED.display_name,
			last_active        = EXCLUDED.last_active,
			total_trades       = EXCLUDED.total_trades,
			total_volume       = EXCLUDED.total_volume,
			markets_entered    = EXCLUDED.markets_entered,
			markets_won        = EXCLUDED.markets_won,
			markets_lost       = EXCLUDED.markets_lost,
			win_rate           = EXCLUDED.win_rate,
			category_stats     = EXCLUDED.category_stats,
			avg_position       = EXCLUDED.avg_position,
			max_position       = EXCLUDED.max_position,
			avg_entry_hours_before_resolution = EXCLUDED.avg_entry_hours_before_resolution,
			early_entry_rate   = EXCLUDED.early_entry_rate,
			tags               = EXCLUDED.tags,
			tracked            = EXCLUDED.tracked,
			updated_at         = NOW()`,
		acc.Address,
		acc.DisplayName,
		acc.FirstSeen,
		acc.LastActive,
		acc.TotalTrades,
		acc.TotalVolume,
		acc.MarketsEntered,
		acc.MarketsWon,
		acc.MarketsLost,
		acc.WinRate,
		json.RawMessage(statsJSON),
		acc.AvgPosition,
		acc.MaxPosition,
		acc.AvgEntryHoursBeforeResolution,
		acc.EarlyEntryRate,
		acc.Tags,
		acc.Tracked,
	)
	if err != nil {
		return fmt.Errorf("postgres: upsert polymarket account: %w", err)
	}
	return nil
}

// GetAccount returns a single account by wallet address.
func (r *PolymarketAccountRepo) GetAccount(ctx context.Context, address string) (*domain.PolymarketAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT address, display_name, first_seen, last_active,
		       total_trades, total_volume, markets_entered, markets_won, markets_lost,
		       win_rate, category_stats, avg_position, max_position,
		       avg_entry_hours_before_resolution, early_entry_rate, tags, tracked, updated_at
		FROM polymarket_accounts
		WHERE address = $1`, address)

	acc, err := scanAccount(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get polymarket account: %w", err)
	}
	return acc, nil
}

// ListTrackedAccounts returns tracked accounts with win_rate >= minWinRate.
func (r *PolymarketAccountRepo) ListTrackedAccounts(ctx context.Context, minWinRate float64, limit int) ([]domain.PolymarketAccount, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT address, display_name, first_seen, last_active,
		       total_trades, total_volume, markets_entered, markets_won, markets_lost,
		       win_rate, category_stats, avg_position, max_position,
		       avg_entry_hours_before_resolution, early_entry_rate, tags, tracked, updated_at
		FROM polymarket_accounts
		WHERE tracked = true AND win_rate >= $1
		ORDER BY win_rate DESC
		LIMIT $2`, minWinRate, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: list tracked accounts: %w", err)
	}
	defer rows.Close()

	var accounts []domain.PolymarketAccount
	for rows.Next() {
		acc, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan account: %w", err)
		}
		accounts = append(accounts, *acc)
	}
	return accounts, rows.Err()
}

// InsertTrades bulk-inserts trade records, ignoring duplicates.
func (r *PolymarketAccountRepo) InsertTrades(ctx context.Context, trades []domain.PolymarketAccountTrade) error {
	if len(trades) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, t := range trades {
		_, err := tx.Exec(ctx, `
			INSERT INTO polymarket_account_trades
				(account_address, market_slug, side, action, price, size_usdc, timestamp, outcome, pnl)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT DO NOTHING`,
			t.AccountAddress, t.MarketSlug, t.Side, t.Action,
			t.Price, t.SizeUSDC, t.Timestamp, nilStr(t.Outcome), t.PnL,
		)
		if err != nil {
			return fmt.Errorf("postgres: insert trade for %s: %w", t.AccountAddress, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit trades: %w", err)
	}
	return nil
}

// ListTradesByAccount returns trades for a given address within [from, to].
func (r *PolymarketAccountRepo) ListTradesByAccount(ctx context.Context, address string, from, to time.Time, limit int) ([]domain.PolymarketAccountTrade, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, account_address, market_slug, side, action, price, size_usdc,
		       timestamp, COALESCE(outcome,''), pnl, created_at
		FROM polymarket_account_trades
		WHERE account_address = $1 AND timestamp BETWEEN $2 AND $3
		ORDER BY timestamp DESC
		LIMIT $4`, address, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: list trades by account: %w", err)
	}
	defer rows.Close()

	var trades []domain.PolymarketAccountTrade
	for rows.Next() {
		var t domain.PolymarketAccountTrade
		if err := rows.Scan(
			&t.ID, &t.AccountAddress, &t.MarketSlug, &t.Side, &t.Action,
			&t.Price, &t.SizeUSDC, &t.Timestamp, &t.Outcome, &t.PnL, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("postgres: scan trade: %w", err)
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}

// MarkTracked auto-flags accounts with high win rate as tracked.
func (r *PolymarketAccountRepo) MarkTracked(ctx context.Context, minWinRate float64, minResolved int) (int64, error) {
	result, err := r.pool.Exec(ctx, `
		UPDATE polymarket_accounts
		SET tracked = true, updated_at = NOW()
		WHERE tracked = false
		  AND win_rate >= $1
		  AND (markets_won + markets_lost) >= $2`,
		minWinRate, minResolved)
	if err != nil {
		return 0, fmt.Errorf("postgres: mark tracked: %w", err)
	}
	return result.RowsAffected(), nil
}

// scanAccount scans one row into a PolymarketAccount.
type accountScanner interface {
	Scan(dest ...any) error
}

func scanAccount(row accountScanner) (*domain.PolymarketAccount, error) {
	var acc domain.PolymarketAccount
	var statsJSON []byte
	if err := row.Scan(
		&acc.Address,
		&acc.DisplayName,
		&acc.FirstSeen,
		&acc.LastActive,
		&acc.TotalTrades,
		&acc.TotalVolume,
		&acc.MarketsEntered,
		&acc.MarketsWon,
		&acc.MarketsLost,
		&acc.WinRate,
		&statsJSON,
		&acc.AvgPosition,
		&acc.MaxPosition,
		&acc.AvgEntryHoursBeforeResolution,
		&acc.EarlyEntryRate,
		&acc.Tags,
		&acc.Tracked,
		&acc.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(statsJSON) > 0 {
		_ = json.Unmarshal(statsJSON, &acc.CategoryStats)
	}
	return &acc, nil
}

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
