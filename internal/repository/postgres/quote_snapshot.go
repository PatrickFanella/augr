package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// QuoteSnapshotRepo persists immutable exact quote/depth observations.
type QuoteSnapshotRepo struct{ pool *pgxpool.Pool }

var _ repository.QuoteSnapshotRepository = (*QuoteSnapshotRepo)(nil)

// NewQuoteSnapshotRepo returns a quote snapshot repository backed by pool.
func NewQuoteSnapshotRepo(pool *pgxpool.Pool) *QuoteSnapshotRepo {
	return &QuoteSnapshotRepo{pool: pool}
}

// RecordQuoteSnapshot atomically inserts one observation and every declared
// depth level. Deferred database checks validate the complete book at commit.
func (repo *QuoteSnapshotRepo) RecordQuoteSnapshot(ctx context.Context, snapshot *marketdata.QuoteSnapshot) (*marketdata.QuoteSnapshot, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("postgres: record quote snapshot: snapshot is required")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: record quote snapshot: %w", err)
	}
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: record quote snapshot: repository pool is required")
	}

	databaseTransaction, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin quote snapshot transaction: %w", err)
	}
	defer func() { _ = databaseTransaction.Rollback(ctx) }()

	bidDepthCount, askDepthCount := quoteDepthCounts(snapshot.Depth)
	var persistedID uuid.UUID
	err = databaseTransaction.QueryRow(ctx, `INSERT INTO quote_snapshots (
		id, instrument_id, venue_contract_id, provider, venue, source,
		observation_namespace, observation_id, source_revision, source_sequence,
		exchange_at, received_at, available_at, bid, bid_size, ask, ask_size,
		last, mark, market_status, session_status, bid_depth_count,
		ask_depth_count, metadata, created_at
	) VALUES (
		$1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13,$14,$15,
		$16,$17,$18,$19,NULLIF($20,''),NULLIF($21,''),$22,$23,$24,$25
	)
	ON CONFLICT (
		instrument_id, provider, venue, observation_namespace, observation_id, source_revision
	) DO NOTHING
	RETURNING id`,
		snapshot.ID,
		snapshot.InstrumentID,
		nullableQuoteUUID(snapshot.VenueContractID),
		snapshot.Provider,
		snapshot.Venue,
		snapshot.Source,
		snapshot.ObservationNamespace,
		snapshot.ObservationID,
		snapshot.SourceRevision,
		nullableQuoteInt64(snapshot.SourceSequence),
		nullableQuoteTime(snapshot.ExchangeAt),
		snapshot.ReceivedAt,
		nullableQuoteTime(snapshot.AvailableAt),
		nullableQuoteDecimal(snapshot.Bid),
		nullableQuoteDecimal(snapshot.BidSize),
		nullableQuoteDecimal(snapshot.Ask),
		nullableQuoteDecimal(snapshot.AskSize),
		nullableQuoteDecimal(snapshot.Last),
		nullableQuoteDecimal(snapshot.Mark),
		snapshot.MarketStatus,
		snapshot.SessionStatus,
		bidDepthCount,
		askDepthCount,
		jsonForStorage(snapshot.Metadata),
		snapshot.CreatedAt,
	).Scan(&persistedID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := databaseTransaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("postgres: commit replayed quote snapshot: %w", err)
		}
		existing, loadErr := repo.getQuoteSnapshotBySourceIdentity(ctx, snapshot)
		if loadErr != nil {
			return nil, fmt.Errorf("postgres: load replayed quote snapshot: %w", loadErr)
		}
		if !sameQuoteSnapshotPayload(existing, snapshot) {
			return nil, fmt.Errorf(
				"postgres: quote observation identity %q reused with mismatched payload: %w",
				snapshot.ObservationID,
				repository.ErrIdempotencyConflict,
			)
		}
		return existing, nil
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return nil, fmt.Errorf("postgres: quote snapshot identity conflicts with an existing observation: %v: %w", err, repository.ErrIdempotencyConflict)
		}
		return nil, fmt.Errorf("postgres: insert quote snapshot %s: %w", snapshot.ID, err)
	}

	for _, level := range snapshot.Depth {
		if _, err := databaseTransaction.Exec(ctx, `INSERT INTO quote_depth_levels (
			quote_snapshot_id, side, level_index, price, size
		) VALUES ($1,$2,$3,$4,$5)`,
			persistedID,
			level.Side,
			level.Level,
			level.Price.String(),
			level.Size.String(),
		); err != nil {
			return nil, fmt.Errorf("postgres: insert %s quote depth level %d: %w", level.Side, level.Level, err)
		}
	}
	if err := databaseTransaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit quote snapshot %s: %w", snapshot.ID, err)
	}
	return repo.GetQuoteSnapshotByID(ctx, persistedID)
}

// GetQuoteSnapshotByID loads one observation and its canonically ordered book.
func (repo *QuoteSnapshotRepo) GetQuoteSnapshotByID(ctx context.Context, id uuid.UUID) (*marketdata.QuoteSnapshot, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: get quote snapshot: repository pool is required")
	}
	snapshot, err := scanQuoteSnapshot(repo.pool.QueryRow(ctx, quoteSnapshotSelectSQL+` WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get quote snapshot %s: %w", id, err)
	}
	if err := repo.loadQuoteDepth(ctx, snapshot); err != nil {
		return nil, err
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate loaded quote snapshot %s: %w", id, err)
	}
	return snapshot, nil
}

// LatestQuoteSnapshotAt selects only a row that was decision-available in the
// requested source namespace at selector.AsOf.
func (repo *QuoteSnapshotRepo) LatestQuoteSnapshotAt(ctx context.Context, selector marketdata.QuoteSelector) (*marketdata.QuoteSnapshot, error) {
	if err := selector.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: latest quote snapshot: %w", err)
	}
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: latest quote snapshot: repository pool is required")
	}
	var id uuid.UUID
	if err := repo.pool.QueryRow(ctx, `SELECT id
		FROM quote_snapshots
		WHERE instrument_id = $1
		  AND provider = $2
		  AND venue = $3
		  AND observation_namespace = $4
		  AND available_at IS NOT NULL
		  AND available_at <= $5
		ORDER BY available_at DESC, source_sequence DESC NULLS LAST, ingest_sequence DESC
		LIMIT 1`,
		selector.InstrumentID,
		selector.Provider,
		selector.Venue,
		selector.ObservationNamespace,
		selector.AsOf,
	).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: latest quote snapshot: %w", err)
	}
	return repo.GetQuoteSnapshotByID(ctx, id)
}

const quoteSnapshotSelectSQL = `SELECT
	id, ingest_sequence, instrument_id, venue_contract_id,
	provider, venue, COALESCE(source, ''), observation_namespace, observation_id,
	source_revision, source_sequence, exchange_at, received_at, available_at,
	bid::TEXT, bid_size::TEXT, ask::TEXT, ask_size::TEXT, last::TEXT, mark::TEXT,
	COALESCE(market_status, ''), COALESCE(session_status, ''), metadata, created_at
FROM quote_snapshots`

func scanQuoteSnapshot(row accountRow) (*marketdata.QuoteSnapshot, error) {
	var snapshot marketdata.QuoteSnapshot
	var bid, bidSize, ask, askSize, last, mark *string
	var metadata []byte
	if err := row.Scan(
		&snapshot.ID,
		&snapshot.IngestSequence,
		&snapshot.InstrumentID,
		&snapshot.VenueContractID,
		&snapshot.Provider,
		&snapshot.Venue,
		&snapshot.Source,
		&snapshot.ObservationNamespace,
		&snapshot.ObservationID,
		&snapshot.SourceRevision,
		&snapshot.SourceSequence,
		&snapshot.ExchangeAt,
		&snapshot.ReceivedAt,
		&snapshot.AvailableAt,
		&bid,
		&bidSize,
		&ask,
		&askSize,
		&last,
		&mark,
		&snapshot.MarketStatus,
		&snapshot.SessionStatus,
		&metadata,
		&snapshot.CreatedAt,
	); err != nil {
		return nil, err
	}
	var err error
	if snapshot.Bid, err = parseOptionalQuoteDecimal(bid); err != nil {
		return nil, fmt.Errorf("parse quote bid: %w", err)
	}
	if snapshot.BidSize, err = parseOptionalQuoteDecimal(bidSize); err != nil {
		return nil, fmt.Errorf("parse quote bid size: %w", err)
	}
	if snapshot.Ask, err = parseOptionalQuoteDecimal(ask); err != nil {
		return nil, fmt.Errorf("parse quote ask: %w", err)
	}
	if snapshot.AskSize, err = parseOptionalQuoteDecimal(askSize); err != nil {
		return nil, fmt.Errorf("parse quote ask size: %w", err)
	}
	if snapshot.Last, err = parseOptionalQuoteDecimal(last); err != nil {
		return nil, fmt.Errorf("parse quote last: %w", err)
	}
	if snapshot.Mark, err = parseOptionalQuoteDecimal(mark); err != nil {
		return nil, fmt.Errorf("parse quote mark: %w", err)
	}
	snapshot.ExchangeAt = normalizeLoadedQuoteTime(snapshot.ExchangeAt)
	snapshot.AvailableAt = normalizeLoadedQuoteTime(snapshot.AvailableAt)
	snapshot.ReceivedAt = snapshot.ReceivedAt.UTC().Truncate(time.Microsecond)
	snapshot.CreatedAt = snapshot.CreatedAt.UTC().Truncate(time.Microsecond)
	snapshot.Metadata = append(json.RawMessage(nil), metadata...)
	return &snapshot, nil
}

func (repo *QuoteSnapshotRepo) loadQuoteDepth(ctx context.Context, snapshot *marketdata.QuoteSnapshot) error {
	rows, err := repo.pool.Query(ctx, `SELECT side, level_index, price::TEXT, size::TEXT
		FROM quote_depth_levels
		WHERE quote_snapshot_id = $1
		ORDER BY CASE side WHEN 'bid' THEN 0 ELSE 1 END, level_index`, snapshot.ID)
	if err != nil {
		return fmt.Errorf("postgres: list quote depth for snapshot %s: %w", snapshot.ID, err)
	}
	defer rows.Close()

	snapshot.Depth = make([]marketdata.DepthLevel, 0)
	for rows.Next() {
		var level marketdata.DepthLevel
		var price, size string
		if err := rows.Scan(&level.Side, &level.Level, &price, &size); err != nil {
			return fmt.Errorf("postgres: scan quote depth for snapshot %s: %w", snapshot.ID, err)
		}
		level.Price, err = decimal.NewFromString(price)
		if err != nil {
			return fmt.Errorf("postgres: parse quote depth price %q: %w", price, err)
		}
		level.Size, err = decimal.NewFromString(size)
		if err != nil {
			return fmt.Errorf("postgres: parse quote depth size %q: %w", size, err)
		}
		snapshot.Depth = append(snapshot.Depth, level)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres: list quote depth for snapshot %s: %w", snapshot.ID, err)
	}
	return nil
}

func (repo *QuoteSnapshotRepo) getQuoteSnapshotBySourceIdentity(ctx context.Context, candidate *marketdata.QuoteSnapshot) (*marketdata.QuoteSnapshot, error) {
	var id uuid.UUID
	if err := repo.pool.QueryRow(ctx, `SELECT id
		FROM quote_snapshots
		WHERE instrument_id = $1
		  AND provider = $2
		  AND venue = $3
		  AND observation_namespace = $4
		  AND observation_id = $5
		  AND source_revision = $6`,
		candidate.InstrumentID,
		candidate.Provider,
		candidate.Venue,
		candidate.ObservationNamespace,
		candidate.ObservationID,
		candidate.SourceRevision,
	).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return repo.GetQuoteSnapshotByID(ctx, id)
}

func sameQuoteSnapshotPayload(left, right *marketdata.QuoteSnapshot) bool {
	if left == nil || right == nil {
		return false
	}
	if left.InstrumentID != right.InstrumentID ||
		!sameOptionalUUID(left.VenueContractID, right.VenueContractID) ||
		left.Provider != right.Provider ||
		left.Venue != right.Venue ||
		left.Source != right.Source ||
		left.ObservationNamespace != right.ObservationNamespace ||
		left.ObservationID != right.ObservationID ||
		left.SourceRevision != right.SourceRevision ||
		!sameOptionalInt64(left.SourceSequence, right.SourceSequence) ||
		!sameOptionalTime(left.ExchangeAt, right.ExchangeAt) ||
		!left.ReceivedAt.Equal(right.ReceivedAt) ||
		!sameOptionalTime(left.AvailableAt, right.AvailableAt) ||
		!sameOptionalDecimal(left.Bid, right.Bid) ||
		!sameOptionalDecimal(left.BidSize, right.BidSize) ||
		!sameOptionalDecimal(left.Ask, right.Ask) ||
		!sameOptionalDecimal(left.AskSize, right.AskSize) ||
		!sameOptionalDecimal(left.Last, right.Last) ||
		!sameOptionalDecimal(left.Mark, right.Mark) ||
		left.MarketStatus != right.MarketStatus ||
		left.SessionStatus != right.SessionStatus ||
		!jsonObjectsEqual(left.Metadata, right.Metadata) ||
		len(left.Depth) != len(right.Depth) {
		return false
	}
	for index := range left.Depth {
		leftLevel := left.Depth[index]
		rightLevel := right.Depth[index]
		if leftLevel.Side != rightLevel.Side || leftLevel.Level != rightLevel.Level ||
			!leftLevel.Price.Equal(rightLevel.Price) || !leftLevel.Size.Equal(rightLevel.Size) {
			return false
		}
	}
	return true
}

func quoteDepthCounts(depth []marketdata.DepthLevel) (int, int) {
	var bids, asks int
	for _, level := range depth {
		switch level.Side {
		case marketdata.DepthSideBid:
			bids++
		case marketdata.DepthSideAsk:
			asks++
		}
	}
	return bids, asks
}

func parseOptionalQuoteDecimal(value *string) (*decimal.Decimal, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := decimal.NewFromString(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func normalizeLoadedQuoteTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}

func nullableQuoteDecimal(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func nullableQuoteTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableQuoteUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableQuoteInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func sameOptionalDecimal(left, right *decimal.Decimal) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func sameOptionalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
