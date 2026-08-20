package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/evidenceprogram"
)

var ErrShadowCampaignConflict = errors.New("postgres: shadow campaign conflict")

type ShadowCampaignRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

func NewShadowCampaignRepo(pool *pgxpool.Pool) *ShadowCampaignRepo {
	return &ShadowCampaignRepo{pool: pool}
}

func (r *ShadowCampaignRepo) RegisterCampaign(ctx context.Context, campaign *evidenceprogram.ShadowCampaign) error {
	if r == nil || r.pool == nil || campaign == nil {
		return fmt.Errorf("postgres: shadow campaign is required")
	}
	record := campaign.Record()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO shadow_campaigns(id,schema_name,campaign_key,started_at,target_days,benchmark_id,benchmark_sha256,candidate_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(id) DO NOTHING`, record.ID, evidenceprogram.ShadowCampaignSchemaV1, record.Key, record.StartedAt, evidenceprogram.ShadowTargetDays, record.Benchmark.ID, record.Benchmark.SHA256, len(record.Candidates), record.SHA256, []byte(record.CanonicalBytes), record.CanonicalBytes)
	if err != nil {
		return fmt.Errorf("postgres: persist shadow campaign: %w", err)
	}
	if command.RowsAffected() == 0 {
		return r.compareCampaign(ctx, tx, record.ID, record.SHA256, record.CanonicalBytes)
	}
	if err = r.stage("campaign"); err != nil {
		return err
	}
	for sequence, candidate := range record.Candidates {
		raw, _ := json.Marshal(candidate)
		if _, err = tx.Exec(ctx, `INSERT INTO shadow_campaign_candidates(campaign_id,sequence,candidate_key,version_id,version_sha256,canonical_row) VALUES($1,$2,$3,$4,$5,$6)`, record.ID, sequence, candidate.Key, candidate.VersionID, candidate.SHA256, raw); err != nil {
			return fmt.Errorf("postgres: persist shadow candidate: %w", err)
		}
	}
	if err = r.stage("candidates"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ShadowCampaignRepo) RegisterDay(ctx context.Context, day *evidenceprogram.ShadowDay) error {
	if r == nil || r.pool == nil || day == nil {
		return fmt.Errorf("postgres: shadow day is required")
	}
	record := day.Record()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO shadow_campaign_days(id,schema_name,campaign_id,campaign_sha256,sequence,observed_at,source_kind,source_id,source_sha256,candidate_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(id) DO NOTHING`, record.ID, evidenceprogram.ShadowDaySchemaV1, record.CampaignID, record.CampaignSHA256, record.Sequence, record.ObservedAt, record.Source.Kind, record.Source.ID, record.Source.SHA256, len(record.Candidates), record.SHA256, []byte(record.CanonicalBytes), record.CanonicalBytes)
	if err != nil {
		return fmt.Errorf("postgres: persist shadow day: %w", err)
	}
	if command.RowsAffected() == 0 {
		return r.compareDay(ctx, tx, record.ID, record.SHA256, record.CanonicalBytes)
	}
	if err = r.stage("day"); err != nil {
		return err
	}
	for sequence, candidate := range record.Candidates {
		raw, _ := json.Marshal(candidate)
		if _, err = tx.Exec(ctx, `INSERT INTO shadow_campaign_day_candidates(day_id,sequence,candidate_key,critical_defects,executable_samples,simulated_fills,slippage_known,slippage_divergence,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, record.ID, sequence, candidate.Key, candidate.CriticalDefects, candidate.ExecutableSamples, candidate.SimulatedFills, candidate.SlippageKnown, candidate.SlippageDivergence, raw); err != nil {
			return fmt.Errorf("postgres: persist shadow day candidate: %w", err)
		}
	}
	if err = r.stage("day_candidates"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ShadowCampaignRepo) GetCampaign(ctx context.Context, id uuid.UUID) (*evidenceprogram.ShadowCampaign, error) {
	var digest string
	var raw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM shadow_campaigns WHERE id=$1`, id).Scan(&digest, &raw); err != nil {
		return nil, err
	}
	return evidenceprogram.ShadowCampaignFromCanonical(id, digest, raw)
}

func (r *ShadowCampaignRepo) ListDays(ctx context.Context, campaign *evidenceprogram.ShadowCampaign) ([]*evidenceprogram.ShadowDay, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,sha256,canonical_bytes FROM shadow_campaign_days WHERE campaign_id=$1 ORDER BY sequence`, campaign.ID())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []*evidenceprogram.ShadowDay{}
	for rows.Next() {
		var id uuid.UUID
		var digest string
		var raw []byte
		if err = rows.Scan(&id, &digest, &raw); err != nil {
			return nil, err
		}
		day, decodeErr := evidenceprogram.ShadowDayFromCanonical(id, digest, raw, campaign)
		if decodeErr != nil {
			return nil, decodeErr
		}
		values = append(values, day)
	}
	return values, rows.Err()
}

func (r *ShadowCampaignRepo) compareCampaign(ctx context.Context, tx pgx.Tx, id uuid.UUID, digest string, raw []byte) error {
	var gotDigest string
	var gotRaw []byte
	if err := tx.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM shadow_campaigns WHERE id=$1`, id).Scan(&gotDigest, &gotRaw); err != nil {
		return err
	}
	if gotDigest != digest || !bytes.Equal(gotRaw, raw) {
		return ErrShadowCampaignConflict
	}
	return tx.Commit(ctx)
}

func (r *ShadowCampaignRepo) compareDay(ctx context.Context, tx pgx.Tx, id uuid.UUID, digest string, raw []byte) error {
	var gotDigest string
	var gotRaw []byte
	if err := tx.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM shadow_campaign_days WHERE id=$1`, id).Scan(&gotDigest, &gotRaw); err != nil {
		return err
	}
	if gotDigest != digest || !bytes.Equal(gotRaw, raw) {
		return ErrShadowCampaignConflict
	}
	return tx.Commit(ctx)
}

func (r *ShadowCampaignRepo) stage(name string) error {
	if r.afterStage != nil {
		return r.afterStage(name)
	}
	return nil
}
