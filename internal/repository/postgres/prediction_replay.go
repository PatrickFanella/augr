package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type PredictionReplayRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ predictionreplay.Store = (*PredictionReplayRepo)(nil)

func NewPredictionReplayRepo(pool *pgxpool.Pool) *PredictionReplayRepo {
	return &PredictionReplayRepo{pool: pool}
}

type predictionEnvelope struct {
	Schema         string            `json:"schema"`
	State          string            `json:"state"`
	ManifestID     string            `json:"manifest_id"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	ManifestCutoff string            `json:"manifest_cutoff"`
	Books          []json.RawMessage `json:"books"`
	Fees           []json.RawMessage `json:"fees"`
	Replays        []json.RawMessage `json:"replays"`
}

type predictionEvidenceRow struct {
	PartitionContentSHA256 string `json:"partition_content_sha256"`
	SourceKey              string `json:"source_key"`
	ContentSHA256          string `json:"content_sha256"`
	AvailableAt            string `json:"available_at"`
}

type predictionLevelRow struct {
	Side     string `json:"side"`
	Sequence int    `json:"sequence"`
	Price    string `json:"price"`
	Size     string `json:"size"`
}

type predictionBookRow struct {
	predictionEvidenceRow
	MarketID     string               `json:"market_id"`
	OutcomeID    string               `json:"outcome_id"`
	Venue        string               `json:"venue"`
	ExchangeAt   string               `json:"exchange_at"`
	Revision     int                  `json:"revision"`
	CorrectionOf string               `json:"correction_of"`
	Levels       []predictionLevelRow `json:"levels"`
}

type predictionFeeRow struct {
	predictionEvidenceRow
	InstrumentID  string `json:"instrument_id"`
	Venue         string `json:"venue"`
	Role          string `json:"role"`
	EffectiveFrom string `json:"effective_from"`
	EffectiveTo   string `json:"effective_to"`
	Formula       string `json:"formula"`
	Rate          string `json:"rate"`
	Scale         int32  `json:"scale"`
	Rounding      string `json:"rounding"`
}

type predictionFillRow struct {
	Sequence int    `json:"sequence"`
	Level    int    `json:"level"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
	Gross    string `json:"gross"`
}

type predictionReplayRow struct {
	Sequence         int                 `json:"sequence"`
	DecisionAt       string              `json:"decision_at"`
	MarketID         string              `json:"market_id"`
	OutcomeID        string              `json:"outcome_id"`
	Side             string              `json:"side"`
	Role             string              `json:"role"`
	Quantity         string              `json:"quantity"`
	LimitPrice       string              `json:"limit_price"`
	Status           string              `json:"status"`
	BookSourceKey    string              `json:"book_source_key"`
	FeeSourceKey     string              `json:"fee_source_key"`
	FilledQuantity   string              `json:"filled_quantity"`
	ResidualQuantity string              `json:"residual_quantity"`
	WeightedPrice    string              `json:"weighted_price"`
	GrossCash        string              `json:"gross_cash"`
	Fee              string              `json:"fee"`
	NetCash          string              `json:"net_cash"`
	Fills            []predictionFillRow `json:"fills"`
}

func (r *PredictionReplayRepo) RegisterRecorder(ctx context.Context, value *predictionreplay.Recorder) (*predictionreplay.Recorder, error) {
	if r == nil || r.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: prediction recorder is required")
	}
	var envelope predictionEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	levelCount, fillCount := 0, 0
	for _, raw := range envelope.Books {
		var row predictionBookRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, fmt.Errorf("postgres: decode prediction book")
		}
		levelCount += len(row.Levels)
	}
	for _, raw := range envelope.Replays {
		var row predictionReplayRow
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, fmt.Errorf("postgres: decode prediction replay")
		}
		fillCount += len(row.Fills)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO prediction_book_fee_recorders(id,schema_name,state,manifest_id,manifest_sha256,manifest_cutoff,book_count,level_count,fee_count,replay_count,fill_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,convert_from($13,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.ManifestID, envelope.ManifestSHA256, parsePredictionTime(envelope.ManifestCutoff), len(envelope.Books), levelCount, len(envelope.Fees), len(envelope.Replays), fillCount, value.Digest(), value.CanonicalBytes())
	if err != nil {
		return nil, predictionWriteError("insert parent", err)
	}
	if err = r.stage("parent"); err != nil {
		return nil, err
	}
	for sequence, raw := range envelope.Books {
		var row predictionBookRow
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO prediction_recorded_books(recorder_id,sequence,market_id,outcome_id,venue,partition_content_sha256,source_key,content_sha256,exchange_at,available_at,revision,correction_of,level_count,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb) ON CONFLICT(recorder_id,sequence) DO NOTHING`, value.ID(), sequence, row.MarketID, row.OutcomeID, row.Venue, row.PartitionContentSHA256, row.SourceKey, row.ContentSHA256, parsePredictionTime(row.ExchangeAt), parsePredictionTime(row.AvailableAt), row.Revision, row.CorrectionOf, len(row.Levels), string(raw))
		if err != nil {
			return nil, predictionWriteError("insert book", err)
		}
		if err = r.stage("book"); err != nil {
			return nil, err
		}
		for nestedSequence, level := range row.Levels {
			levelRaw, _ := json.Marshal(level)
			_, err = tx.Exec(ctx, `INSERT INTO prediction_recorded_book_levels(recorder_id,book_sequence,sequence,side,level,price,size,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT(recorder_id,book_sequence,sequence) DO NOTHING`, value.ID(), sequence, nestedSequence, level.Side, level.Sequence, level.Price, level.Size, string(levelRaw))
			if err != nil {
				return nil, predictionWriteError("insert level", err)
			}
			if err = r.stage("level"); err != nil {
				return nil, err
			}
		}
	}
	for sequence, raw := range envelope.Fees {
		var row predictionFeeRow
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		var effectiveTo any
		if row.EffectiveTo != "" {
			effectiveTo = parsePredictionTime(row.EffectiveTo)
		}
		_, err = tx.Exec(ctx, `INSERT INTO prediction_recorded_fee_policies(recorder_id,sequence,instrument_id,venue,role,partition_content_sha256,source_key,content_sha256,available_at,effective_from,effective_to,formula,rate,scale,rounding,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb) ON CONFLICT(recorder_id,sequence) DO NOTHING`, value.ID(), sequence, row.InstrumentID, row.Venue, row.Role, row.PartitionContentSHA256, row.SourceKey, row.ContentSHA256, parsePredictionTime(row.AvailableAt), parsePredictionTime(row.EffectiveFrom), effectiveTo, row.Formula, row.Rate, row.Scale, row.Rounding, string(raw))
		if err != nil {
			return nil, predictionWriteError("insert fee", err)
		}
		if err = r.stage("fee"); err != nil {
			return nil, err
		}
	}
	for _, raw := range envelope.Replays {
		var row predictionReplayRow
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO prediction_recorded_replays(recorder_id,sequence,decision_at,market_id,outcome_id,side,role,quantity,limit_price,status,book_source_key,fee_source_key,filled_quantity,residual_quantity,weighted_price,gross_cash,fee,net_cash,fill_count,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20::jsonb) ON CONFLICT(recorder_id,sequence) DO NOTHING`, value.ID(), row.Sequence, parsePredictionTime(row.DecisionAt), row.MarketID, row.OutcomeID, row.Side, row.Role, row.Quantity, row.LimitPrice, row.Status, row.BookSourceKey, row.FeeSourceKey, row.FilledQuantity, row.ResidualQuantity, row.WeightedPrice, row.GrossCash, row.Fee, row.NetCash, len(row.Fills), string(raw))
		if err != nil {
			return nil, predictionWriteError("insert replay", err)
		}
		if err = r.stage("replay"); err != nil {
			return nil, err
		}
		for _, fill := range row.Fills {
			fillRaw, _ := json.Marshal(fill)
			_, err = tx.Exec(ctx, `INSERT INTO prediction_recorded_fills(recorder_id,replay_sequence,sequence,book_level,price,quantity,gross,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT(recorder_id,replay_sequence,sequence) DO NOTHING`, value.ID(), row.Sequence, fill.Sequence, fill.Level, fill.Price, fill.Quantity, fill.Gross, string(fillRaw))
			if err != nil {
				return nil, predictionWriteError("insert fill", err)
			}
			if err = r.stage("fill"); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, predictionWriteError("commit", err)
	}
	manifest, err := NewDatasetRepo(r.pool).GetDatasetManifest(ctx, value.ManifestID())
	if err != nil {
		return nil, err
	}
	loaded, err := r.GetRecorder(ctx, value.ID(), manifest)
	if err != nil {
		return nil, err
	}
	if loaded.Digest() != value.Digest() || !bytes.Equal(loaded.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: prediction recorder conflict: %w", repository.ErrIdempotencyConflict)
	}
	return loaded, nil
}

func (r *PredictionReplayRepo) GetRecorder(ctx context.Context, id uuid.UUID, manifest *dataset.Manifest) (*predictionreplay.Recorder, error) {
	if r == nil || r.pool == nil || id == uuid.Nil || manifest == nil {
		return nil, fmt.Errorf("postgres: prediction recorder identity and manifest are required")
	}
	var digest string
	var raw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM prediction_book_fee_recorders WHERE id=$1`, id).Scan(&digest, &raw); errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var envelope predictionEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("postgres: decode prediction recorder")
	}
	for table, values := range map[string][]json.RawMessage{"prediction_recorded_books": envelope.Books, "prediction_recorded_fee_policies": envelope.Fees, "prediction_recorded_replays": envelope.Replays} {
		rows, err := r.pool.Query(ctx, `SELECT canonical_row FROM `+table+` WHERE recorder_id=$1 ORDER BY sequence`, id)
		if err != nil {
			return nil, err
		}
		index := 0
		for rows.Next() {
			var normalized []byte
			if rows.Scan(&normalized) != nil || index >= len(values) || !jsonEqual(normalized, values[index]) {
				rows.Close()
				return nil, fmt.Errorf("postgres: normalized prediction recorder %s does not reconstruct", id)
			}
			index++
		}
		rows.Close()
		if index != len(values) {
			return nil, fmt.Errorf("postgres: normalized prediction recorder %s does not reconstruct", id)
		}
	}
	value, err := predictionreplay.FromCanonical(id, digest, raw, manifest)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct prediction recorder %s: %w", id, err)
	}
	return value, nil
}

func (r *PredictionReplayRepo) stage(value string) error {
	if r.afterStage != nil {
		return r.afterStage(value)
	}
	return nil
}

func parsePredictionTime(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", value)
	return parsed
}

func predictionWriteError(action string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "does not reconstruct") || strings.Contains(err.Error(), "absent from manifest") || strings.Contains(err.Error(), "foreign key")) {
		return fmt.Errorf("postgres: prediction recorder %s conflict: %w", action, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: prediction recorder %s: %w", action, err)
}
