package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OptionsScanResult is a row in the options_scan_results table.
type OptionsScanResult struct {
	Ticker       string    `json:"ticker"`
	ScanDate     time.Time `json:"scan_date"`
	ClosePrice   float64   `json:"close_price"`
	ADV          float64   `json:"adv"`
	IVRank       float64   `json:"iv_rank"`
	IVPercentile float64   `json:"iv_percentile"`
	ATMIV        float64   `json:"atm_iv"`
	PutCallRatio float64   `json:"put_call_ratio"`
	VolumeRatio  float64   `json:"volume_ratio"`
	ChainDepth   int       `json:"chain_depth"`
	ATMOI        float64   `json:"atm_oi"`
	Score        float64   `json:"score"`
}

// IVHistoryRecord is a row in the iv_history table.
type IVHistoryRecord struct {
	Ticker       string    `json:"ticker"`
	Date         time.Time `json:"date"`
	ATMIV        float64   `json:"atm_iv"`
	IVRank       float64   `json:"iv_rank"`
	IVPercentile float64   `json:"iv_percentile"`
}

// OptionsScanRepo persists options scan results and IV history.
type OptionsScanRepo struct {
	pool *pgxpool.Pool
}

// NewOptionsScanRepo returns a new OptionsScanRepo.
func NewOptionsScanRepo(pool *pgxpool.Pool) *OptionsScanRepo {
	return &OptionsScanRepo{pool: pool}
}

// UpsertScanResult inserts or updates a scan result for a ticker+date.
func (r *OptionsScanRepo) UpsertScanResult(ctx context.Context, res *OptionsScanResult) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO options_scan_results
			(ticker, scan_date, close_price, adv, iv_rank, iv_percentile, atm_iv, put_call_ratio, volume_ratio, chain_depth, atm_oi, score)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (ticker, scan_date) DO UPDATE SET
			close_price = EXCLUDED.close_price, adv = EXCLUDED.adv,
			iv_rank = EXCLUDED.iv_rank, iv_percentile = EXCLUDED.iv_percentile,
			atm_iv = EXCLUDED.atm_iv, put_call_ratio = EXCLUDED.put_call_ratio,
			volume_ratio = EXCLUDED.volume_ratio, chain_depth = EXCLUDED.chain_depth,
			atm_oi = EXCLUDED.atm_oi, score = EXCLUDED.score`,
		res.Ticker, res.ScanDate, res.ClosePrice, res.ADV,
		res.IVRank, res.IVPercentile, res.ATMIV, res.PutCallRatio,
		res.VolumeRatio, res.ChainDepth, res.ATMOI, res.Score,
	)
	if err != nil {
		return fmt.Errorf("postgres: upsert options scan result: %w", err)
	}
	return nil
}

// ListLatestScan returns the most recent scan results for a given date.
func (r *OptionsScanRepo) ListLatestScan(ctx context.Context, date time.Time, limit int) ([]OptionsScanResult, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT ticker, scan_date, close_price, adv, iv_rank, iv_percentile, atm_iv, put_call_ratio, volume_ratio, chain_depth, atm_oi, score
		 FROM options_scan_results
		 WHERE scan_date = $1
		 ORDER BY score DESC
		 LIMIT $2`,
		date, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list options scan results: %w", err)
	}
	defer rows.Close()

	var results []OptionsScanResult
	for rows.Next() {
		var res OptionsScanResult
		if err := rows.Scan(&res.Ticker, &res.ScanDate, &res.ClosePrice, &res.ADV,
			&res.IVRank, &res.IVPercentile, &res.ATMIV, &res.PutCallRatio,
			&res.VolumeRatio, &res.ChainDepth, &res.ATMOI, &res.Score); err != nil {
			return nil, fmt.Errorf("postgres: scan options scan result: %w", err)
		}
		results = append(results, res)
	}
	return results, rows.Err()
}

// UpsertIVHistory records daily ATM IV for a ticker.
func (r *OptionsScanRepo) UpsertIVHistory(ctx context.Context, rec *IVHistoryRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO iv_history (ticker, date, atm_iv, iv_rank, iv_percentile)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (ticker, date) DO UPDATE SET
			atm_iv = EXCLUDED.atm_iv, iv_rank = EXCLUDED.iv_rank, iv_percentile = EXCLUDED.iv_percentile`,
		rec.Ticker, rec.Date, rec.ATMIV, rec.IVRank, rec.IVPercentile,
	)
	if err != nil {
		return fmt.Errorf("postgres: upsert iv history: %w", err)
	}
	return nil
}

// GetIVHistory returns IV history for a ticker sorted by date descending.
func (r *OptionsScanRepo) GetIVHistory(ctx context.Context, ticker string, lookbackDays int) ([]IVHistoryRecord, error) {
	if lookbackDays <= 0 {
		lookbackDays = 252
	}
	rows, err := r.pool.Query(ctx,
		`SELECT ticker, date, atm_iv, iv_rank, iv_percentile
		 FROM iv_history
		 WHERE ticker = $1 AND date >= NOW() - ($2 || ' days')::interval
		 ORDER BY date DESC`,
		ticker, fmt.Sprintf("%d", lookbackDays),
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: get iv history: %w", err)
	}
	defer rows.Close()

	var records []IVHistoryRecord
	for rows.Next() {
		var rec IVHistoryRecord
		if err := rows.Scan(&rec.Ticker, &rec.Date, &rec.ATMIV, &rec.IVRank, &rec.IVPercentile); err != nil {
			return nil, fmt.Errorf("postgres: scan iv history: %w", err)
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}
