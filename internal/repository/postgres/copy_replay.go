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

	"github.com/PatrickFanella/get-rich-quick/internal/copyreplay"
	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type CopyReplayRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ copyreplay.Store = (*CopyReplayRepo)(nil)

func NewCopyReplayRepo(pool *pgxpool.Pool) *CopyReplayRepo { return &CopyReplayRepo{pool: pool} }

type copyReplayEnvelope struct {
	Schema            string            `json:"schema"`
	State             string            `json:"state"`
	ManifestID        string            `json:"manifest_id"`
	ManifestSHA256    string            `json:"manifest_sha256"`
	ManifestCutoff    string            `json:"manifest_cutoff"`
	SelectionCutoff   string            `json:"selection_cutoff"`
	TopN              int               `json:"top_n"`
	CandidateManagers []json.RawMessage `json:"candidate_managers"`
	Filings           []json.RawMessage `json:"filings"`
	Managers          []json.RawMessage `json:"managers"`
	Decisions         []json.RawMessage `json:"decisions"`
	Steps             []json.RawMessage `json:"steps"`
}

type replayCandidateRow struct {
	ManagerID              string `json:"manager_id"`
	PartitionContentSHA256 string `json:"partition_content_sha256"`
	SourceKey              string `json:"source_key"`
	ContentSHA256          string `json:"content_sha256"`
	AvailableAt            string `json:"available_at"`
	Eligible               bool   `json:"eligible"`
	Score                  string `json:"score"`
}

type replayFilingRow struct {
	ManagerID              string `json:"manager_id"`
	PartitionContentSHA256 string `json:"partition_content_sha256"`
	SourceKey              string `json:"source_key"`
	ContentSHA256          string `json:"content_sha256"`
	AvailableAt            string `json:"available_at"`
	ReportPeriod           string `json:"report_period"`
	PublishedAt            string `json:"published_at"`
	AmendmentNumber        int    `json:"amendment_number"`
	SupersedesKey          string `json:"supersedes_key"`
}

type replayManagerRow struct {
	ManagerID string `json:"manager_id"`
	Rank      int    `json:"rank"`
	Score     string `json:"score"`
}

type replayDecisionRow struct {
	Sequence          int    `json:"sequence"`
	DecisionAt        string `json:"decision_at"`
	ManagerID         string `json:"manager_id"`
	Status            string `json:"status"`
	FilingSourceKey   string `json:"filing_source_key"`
	FilingContentSHA  string `json:"filing_content_sha256"`
	FilingAvailableAt string `json:"filing_available_at"`
	ReportPeriod      string `json:"report_period"`
	AmendmentNumber   int    `json:"amendment_number"`
}

type replayStepRow struct {
	Sequence               int             `json:"sequence"`
	DecisionSequence       int             `json:"decision_sequence"`
	PartitionContentSHA256 string          `json:"partition_content_sha256"`
	ObservationSourceKey   string          `json:"observation_source_key"`
	ObservationContentSHA  string          `json:"observation_content_sha256"`
	AvailableAt            string          `json:"available_at"`
	Decision               json.RawMessage `json:"decision"`
}

func (r *CopyReplayRepo) RegisterReplay(ctx context.Context, value *copyreplay.Replay) (*copyreplay.Replay, error) {
	if r == nil || r.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: copy 13f replay is required")
	}
	var envelope copyReplayEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO copy_13f_replays(id,schema_name,state,manifest_id,manifest_sha256,manifest_cutoff,selection_cutoff,top_n,candidate_count,filing_count,manager_count,decision_count,step_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,convert_from($15,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.ManifestID, envelope.ManifestSHA256, parseReplayTime(envelope.ManifestCutoff), parseReplayTime(envelope.SelectionCutoff), envelope.TopN, len(envelope.CandidateManagers), len(envelope.Filings), len(envelope.Managers), len(envelope.Decisions), len(envelope.Steps), value.Digest(), value.CanonicalBytes())
	if err != nil {
		return nil, copyReplayWriteError("insert parent", err)
	}
	if err = r.stage("parent"); err != nil {
		return nil, err
	}
	for sequence, raw := range envelope.CandidateManagers {
		var row replayCandidateRow
		if json.Unmarshal(raw, &row) != nil {
			return nil, fmt.Errorf("postgres: decode copy replay candidate")
		}
		_, err = tx.Exec(ctx, `INSERT INTO copy_13f_replay_candidates(replay_id,sequence,manager_id,partition_content_sha256,source_key,content_sha256,available_at,eligible,score,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb) ON CONFLICT(replay_id,sequence) DO NOTHING`, value.ID(), sequence, row.ManagerID, row.PartitionContentSHA256, row.SourceKey, row.ContentSHA256, parseReplayTime(row.AvailableAt), row.Eligible, row.Score, string(raw))
		if err != nil {
			return nil, copyReplayWriteError("insert candidate", err)
		}
		if err = r.stage("candidate"); err != nil {
			return nil, err
		}
	}
	for sequence, raw := range envelope.Filings {
		var row replayFilingRow
		if json.Unmarshal(raw, &row) != nil {
			return nil, fmt.Errorf("postgres: decode copy replay filing")
		}
		_, err = tx.Exec(ctx, `INSERT INTO copy_13f_replay_filings(replay_id,sequence,manager_id,partition_content_sha256,source_key,content_sha256,report_period,published_at,available_at,amendment_number,supersedes_key,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb) ON CONFLICT(replay_id,sequence) DO NOTHING`, value.ID(), sequence, row.ManagerID, row.PartitionContentSHA256, row.SourceKey, row.ContentSHA256, row.ReportPeriod, parseReplayTime(row.PublishedAt), parseReplayTime(row.AvailableAt), row.AmendmentNumber, row.SupersedesKey, string(raw))
		if err != nil {
			return nil, copyReplayWriteError("insert filing", err)
		}
		if err = r.stage("filing"); err != nil {
			return nil, err
		}
	}
	for sequence, raw := range envelope.Managers {
		var row replayManagerRow
		if json.Unmarshal(raw, &row) != nil {
			return nil, fmt.Errorf("postgres: decode copy replay manager")
		}
		_, err = tx.Exec(ctx, `INSERT INTO copy_13f_replay_managers(replay_id,sequence,manager_id,rank,score,canonical_row) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT(replay_id,sequence) DO NOTHING`, value.ID(), sequence, row.ManagerID, row.Rank, row.Score, string(raw))
		if err != nil {
			return nil, copyReplayWriteError("insert manager", err)
		}
		if err = r.stage("manager"); err != nil {
			return nil, err
		}
	}
	for _, raw := range envelope.Decisions {
		var row replayDecisionRow
		if json.Unmarshal(raw, &row) != nil {
			return nil, fmt.Errorf("postgres: decode copy replay decision")
		}
		var filingAvailable, reportPeriod any
		if row.FilingAvailableAt != "" {
			filingAvailable = parseReplayTime(row.FilingAvailableAt)
		}
		if row.ReportPeriod != "" {
			reportPeriod = row.ReportPeriod
		}
		_, err = tx.Exec(ctx, `INSERT INTO copy_13f_replay_decisions(replay_id,sequence,decision_at,manager_id,status,filing_source_key,filing_content_sha256,filing_available_at,report_period,amendment_number,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb) ON CONFLICT(replay_id,sequence) DO NOTHING`, value.ID(), row.Sequence, parseReplayTime(row.DecisionAt), row.ManagerID, row.Status, row.FilingSourceKey, row.FilingContentSHA, filingAvailable, reportPeriod, row.AmendmentNumber, string(raw))
		if err != nil {
			return nil, copyReplayWriteError("insert decision", err)
		}
		if err = r.stage("decision"); err != nil {
			return nil, err
		}
	}
	for _, raw := range envelope.Steps {
		var row replayStepRow
		if json.Unmarshal(raw, &row) != nil {
			return nil, fmt.Errorf("postgres: decode copy replay step")
		}
		_, err = tx.Exec(ctx, `INSERT INTO copy_13f_replay_steps(replay_id,sequence,decision_sequence,partition_content_sha256,observation_source_key,observation_content_sha256,available_at,decision,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb) ON CONFLICT(replay_id,sequence) DO NOTHING`, value.ID(), row.Sequence, row.DecisionSequence, row.PartitionContentSHA256, row.ObservationSourceKey, row.ObservationContentSHA, parseReplayTime(row.AvailableAt), row.Decision, string(raw))
		if err != nil {
			return nil, copyReplayWriteError("insert step", err)
		}
		if err = r.stage("step"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, copyReplayWriteError("commit", err)
	}
	manifest, err := NewDatasetRepo(r.pool).GetDatasetManifest(ctx, value.ManifestID())
	if err != nil {
		return nil, err
	}
	loaded, err := r.GetReplay(ctx, value.ID(), manifest)
	if err != nil {
		return nil, err
	}
	if loaded.Digest() != value.Digest() || !bytes.Equal(loaded.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: copy 13f replay conflict: %w", repository.ErrIdempotencyConflict)
	}
	return loaded, nil
}

func (r *CopyReplayRepo) GetReplay(ctx context.Context, id uuid.UUID, manifest *dataset.Manifest) (*copyreplay.Replay, error) {
	if r == nil || r.pool == nil || id == uuid.Nil || manifest == nil {
		return nil, fmt.Errorf("postgres: copy 13f replay identity and manifest are required")
	}
	var digest string
	var raw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM copy_13f_replays WHERE id=$1`, id).Scan(&digest, &raw); errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var envelope copyReplayEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, fmt.Errorf("postgres: decode copy 13f replay")
	}
	for table, values := range map[string][]json.RawMessage{"copy_13f_replay_candidates": envelope.CandidateManagers, "copy_13f_replay_filings": envelope.Filings, "copy_13f_replay_managers": envelope.Managers, "copy_13f_replay_decisions": envelope.Decisions, "copy_13f_replay_steps": envelope.Steps} {
		rows, err := r.pool.Query(ctx, `SELECT canonical_row FROM `+table+` WHERE replay_id=$1 ORDER BY sequence`, id)
		if err != nil {
			return nil, err
		}
		index := 0
		for rows.Next() {
			var normalized []byte
			if rows.Scan(&normalized) != nil || index >= len(values) || !jsonEqual(normalized, values[index]) {
				rows.Close()
				return nil, fmt.Errorf("postgres: normalized copy 13f replay %s does not reconstruct", id)
			}
			index++
		}
		rows.Close()
		if index != len(values) {
			return nil, fmt.Errorf("postgres: normalized copy 13f replay %s does not reconstruct", id)
		}
	}
	value, err := copyreplay.FromCanonical(id, digest, raw, manifest)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct copy 13f replay %s: %w", id, err)
	}
	return value, nil
}

func (r *CopyReplayRepo) stage(value string) error {
	if r.afterStage != nil {
		return r.afterStage(value)
	}
	return nil
}

func parseReplayTime(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", value)
	return parsed
}

func copyReplayWriteError(action string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "does not reconstruct") || strings.Contains(err.Error(), "absent from manifest")) {
		return fmt.Errorf("postgres: copy 13f replay %s conflict: %w", action, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: copy 13f replay %s: %w", action, err)
}
