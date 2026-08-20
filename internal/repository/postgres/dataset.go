package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// DatasetRepo persists exact research-input evidence. It has no fetch,
// scheduling, current-pointer, or experiment activation operation.
type DatasetRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ repository.DatasetRepository = (*DatasetRepo)(nil)

func NewDatasetRepo(pool *pgxpool.Pool) *DatasetRepo { return &DatasetRepo{pool: pool} }

func (repo *DatasetRepo) RegisterDatasetPolicy(ctx context.Context, artifact *dataset.PolicyArtifact) (*dataset.PolicyArtifact, error) {
	if repo == nil || repo.pool == nil || artifact == nil {
		return nil, fmt.Errorf("postgres: dataset policy repository, pool, and artifact are required")
	}
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("postgres: validate dataset policy: %w", err)
	}
	command, err := repo.pool.Exec(ctx, `INSERT INTO dataset_quality_policy_artifacts(
		id,schema_name,policy_version,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,convert_from($5,'UTF8')::JSONB,$6) ON CONFLICT(policy_version) DO NOTHING`,
		artifact.ID, artifact.Schema, artifact.Version, artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("postgres: insert dataset policy: %w", err)
	}
	existing, err := repo.getDatasetPolicy(ctx, artifact.Version)
	if err != nil {
		return nil, err
	}
	if command.RowsAffected() == 0 && !sameDatasetPolicyArtifact(existing, artifact) {
		return nil, fmt.Errorf("postgres: dataset policy changed on retry: %w", repository.ErrIdempotencyConflict)
	}
	return existing, nil
}

func (repo *DatasetRepo) getDatasetPolicy(ctx context.Context, version string) (*dataset.PolicyArtifact, error) {
	var artifact dataset.PolicyArtifact
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT id,schema_name,policy_version,sha256,canonical_bytes,created_at
		FROM dataset_quality_policy_artifacts WHERE policy_version=$1`, version).Scan(
		&artifact.ID, &artifact.Schema, &artifact.Version, &artifact.SHA256, &raw, &artifact.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get dataset policy: %w", err)
	}
	artifact.CanonicalBytes = append(json.RawMessage(nil), raw...)
	artifact.CreatedAt = artifact.CreatedAt.UTC()
	if _, err := dataset.PolicyFromArtifact(artifact); err != nil {
		return nil, fmt.Errorf("postgres: reconstruct dataset policy: %w", err)
	}
	return &artifact, nil
}

func (repo *DatasetRepo) RecordDatasetManifest(ctx context.Context, manifest *dataset.Manifest, createdAt time.Time) (*dataset.Manifest, error) {
	if repo == nil || repo.pool == nil || manifest == nil || !validDatasetCreatedAt(createdAt) {
		return nil, fmt.Errorf("postgres: dataset manifest repository, evidence, and UTC microsecond creation time are required")
	}
	validated, err := dataset.ManifestFromCanonical(manifest.ID(), manifest.Digest(), manifest.CanonicalBytes())
	if err != nil {
		return nil, fmt.Errorf("postgres: validate dataset manifest: %w", err)
	}
	partitions := validated.Partitions()
	observationCount := 0
	for _, partition := range partitions {
		observationCount += len(partition.Observations)
	}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO dataset_manifests(
		id,schema_name,decision_cutoff,partition_count,observation_count,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::JSONB,$8) ON CONFLICT(id) DO NOTHING`,
		validated.ID(), dataset.ManifestSchemaV1, validated.DecisionCutoff(), len(partitions), observationCount,
		validated.Digest(), []byte(validated.CanonicalBytes()), createdAt)
	if err != nil {
		return nil, fmt.Errorf("postgres: insert dataset manifest: %w", err)
	}
	if command.RowsAffected() == 0 {
		if err := verifyDatasetEnvelope(ctx, tx, "dataset_manifests", validated.ID(), validated.Digest(), validated.CanonicalBytes()); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return repo.GetDatasetManifest(ctx, validated.ID())
	}
	if err := repo.stage("manifest_parent"); err != nil {
		return nil, err
	}
	for _, partition := range partitions {
		partitionBytes, marshalErr := json.Marshal(partition)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_manifest_partitions(
			manifest_id,manifest_decision_cutoff,sequence,kind,provider,source_name,namespace,request_sha256,content_sha256,media_type,
			effective_start,effective_end,observed_start,observed_end,available_start,available_end,symbology_version,adjustment_policy,
			timezone_name,calendar_name,revision,supersedes_content_sha256,row_count,license_name,retention_policy,canonical_bytes,canonical_json
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,convert_from($26,'UTF8')::JSONB)`,
			validated.ID(), validated.DecisionCutoff(), partition.Sequence, partition.Kind, partition.Provider, partition.Source,
			partition.Namespace, partition.RequestSHA256, partition.ContentSHA256, partition.MediaType, partition.EffectiveStart,
			partition.EffectiveEnd, partition.ObservedStart, partition.ObservedEnd, partition.AvailableStart, partition.AvailableEnd,
			partition.SymbologyVersion, partition.AdjustmentPolicy, partition.Timezone, partition.Calendar, partition.Revision,
			partition.SupersedesContentSHA256, partition.RowCount, partition.License, partition.RetentionPolicy, partitionBytes); err != nil {
			return nil, fmt.Errorf("postgres: insert dataset partition: %w", err)
		}
	}
	if err := repo.stage("manifest_partitions"); err != nil {
		return nil, err
	}
	for _, partition := range partitions {
		for _, observation := range partition.Observations {
			observationBytes, marshalErr := json.Marshal(observation)
			if marshalErr != nil {
				return nil, marshalErr
			}
			if _, err := tx.Exec(ctx, `INSERT INTO dataset_manifest_observations(
				manifest_id,manifest_decision_cutoff,partition_sequence,partition_content_sha256,sequence,source_key,instrument_id,
				effective_at,published_at,observed_at,available_at,revision,correction_of,content_sha256,bid,ask,volume,depth,canonical_bytes,canonical_json
			) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,convert_from($19,'UTF8')::JSONB)`,
				validated.ID(), validated.DecisionCutoff(), partition.Sequence, partition.ContentSHA256, observation.Sequence,
				observation.SourceKey, nullableDatasetString(observation.InstrumentID), observation.EffectiveAt,
				nullableDatasetString(observation.PublishedAt), observation.ObservedAt, observation.AvailableAt, observation.Revision,
				observation.CorrectionOf, observation.ContentSHA256, observation.Bid, observation.Ask, observation.Volume,
				observation.Depth, observationBytes); err != nil {
				return nil, fmt.Errorf("postgres: insert dataset observation: %w", err)
			}
		}
	}
	if err := repo.stage("manifest_observations"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit dataset manifest: %w", err)
	}
	return repo.GetDatasetManifest(ctx, validated.ID())
}

func (repo *DatasetRepo) GetDatasetManifest(ctx context.Context, id uuid.UUID) (*dataset.Manifest, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: dataset manifest repository and ID are required")
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM dataset_manifests WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get dataset manifest: %w", err)
	}
	manifest, err := dataset.ManifestFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct dataset manifest: %w", err)
	}
	if err := repo.verifyManifestChildren(ctx, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (repo *DatasetRepo) RecordDatasetQualityResult(ctx context.Context, result *dataset.QualityResult, createdAt time.Time) (*dataset.QualityResult, error) {
	if repo == nil || repo.pool == nil || result == nil || !validDatasetCreatedAt(createdAt) {
		return nil, fmt.Errorf("postgres: dataset quality repository, evidence, and UTC microsecond creation time are required")
	}
	validated, err := dataset.QualityResultFromCanonical(result.ID(), result.Digest(), result.CanonicalBytes())
	if err != nil {
		return nil, fmt.Errorf("postgres: validate dataset quality result: %w", err)
	}
	checks, findings := validated.Checks(), validated.Findings()
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO dataset_quality_results(
		id,schema_name,policy_version,manifest_id,quarantined,check_count,finding_count,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,convert_from($9,'UTF8')::JSONB,$10) ON CONFLICT(id) DO NOTHING`,
		validated.ID(), dataset.QualityResultSchemaV1, validated.PolicyVersion(), validated.ManifestID(), validated.Quarantined(),
		len(checks), len(findings), validated.Digest(), []byte(validated.CanonicalBytes()), createdAt)
	if err != nil {
		return nil, fmt.Errorf("postgres: insert dataset quality result: %w", err)
	}
	if command.RowsAffected() == 0 {
		if err := verifyDatasetEnvelope(ctx, tx, "dataset_quality_results", validated.ID(), validated.Digest(), validated.CanonicalBytes()); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return repo.GetDatasetQualityResult(ctx, validated.ID())
	}
	if err := repo.stage("quality_parent"); err != nil {
		return nil, err
	}
	for sequence, check := range checks {
		checkBytes, marshalErr := json.Marshal(check)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_quality_checks(
			result_id,policy_version,manifest_id,sequence,check_key,partition_content_sha256,kind,check_code,required,status,severity,evidence_sha256,canonical_bytes,canonical_json
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,convert_from($13,'UTF8')::JSONB)`, validated.ID(),
			validated.PolicyVersion(), validated.ManifestID(), sequence, check.Key, check.PartitionContentSHA256, check.Kind,
			check.Check, check.Required, check.Status, check.Severity, check.EvidenceSHA256, checkBytes); err != nil {
			return nil, fmt.Errorf("postgres: insert dataset quality check: %w", err)
		}
	}
	if err := repo.stage("quality_checks"); err != nil {
		return nil, err
	}
	for sequence, finding := range findings {
		findingBytes, marshalErr := json.Marshal(finding)
		if marshalErr != nil {
			return nil, marshalErr
		}
		evidenceBytes, marshalErr := json.Marshal(finding.Evidence)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_quality_findings(
			result_id,policy_version,manifest_id,sequence,finding_key,partition_content_sha256,check_code,finding_code,severity,evidence,canonical_bytes,canonical_json
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,convert_from($11,'UTF8')::JSONB)`, validated.ID(),
			validated.PolicyVersion(), validated.ManifestID(), sequence, finding.Key, finding.PartitionContentSHA256,
			finding.Check, finding.Code, finding.Severity, string(evidenceBytes), findingBytes); err != nil {
			return nil, fmt.Errorf("postgres: insert dataset quality finding: %w", err)
		}
	}
	if err := repo.stage("quality_findings"); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit dataset quality result: %w", err)
	}
	return repo.GetDatasetQualityResult(ctx, validated.ID())
}

func (repo *DatasetRepo) GetDatasetQualityResult(ctx context.Context, id uuid.UUID) (*dataset.QualityResult, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: dataset quality repository and ID are required")
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM dataset_quality_results WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get dataset quality result: %w", err)
	}
	result, err := dataset.QualityResultFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct dataset quality result: %w", err)
	}
	if err := repo.verifyQualityChildren(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (repo *DatasetRepo) verifyManifestChildren(ctx context.Context, manifest *dataset.Manifest) error {
	rows, err := repo.pool.Query(ctx, `SELECT canonical_bytes FROM dataset_manifest_partitions WHERE manifest_id=$1 ORDER BY sequence`, manifest.ID())
	if err != nil {
		return err
	}
	defer rows.Close()
	partitions := manifest.Partitions()
	index := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if index >= len(partitions) {
			return fmt.Errorf("postgres: dataset manifest relational graph differs from canonical evidence")
		}
		want, _ := json.Marshal(partitions[index])
		if !bytes.Equal(raw, want) {
			return fmt.Errorf("postgres: dataset manifest partition differs from canonical evidence")
		}
		index++
	}
	if err := rows.Err(); err != nil || index != len(partitions) {
		return fmt.Errorf("postgres: dataset manifest partition graph is incomplete: %w", err)
	}
	for _, partition := range partitions {
		observationRows, err := repo.pool.Query(ctx, `SELECT canonical_bytes FROM dataset_manifest_observations
			WHERE manifest_id=$1 AND partition_sequence=$2 ORDER BY sequence`, manifest.ID(), partition.Sequence)
		if err != nil {
			return err
		}
		observationIndex := 0
		for observationRows.Next() {
			var raw []byte
			if err := observationRows.Scan(&raw); err != nil {
				observationRows.Close()
				return err
			}
			if observationIndex >= len(partition.Observations) {
				observationRows.Close()
				return fmt.Errorf("postgres: dataset observation graph differs from canonical evidence")
			}
			want, _ := json.Marshal(partition.Observations[observationIndex])
			if !bytes.Equal(raw, want) {
				observationRows.Close()
				return fmt.Errorf("postgres: dataset observation differs from canonical evidence")
			}
			observationIndex++
		}
		rowErr := observationRows.Err()
		observationRows.Close()
		if rowErr != nil || observationIndex != len(partition.Observations) {
			return fmt.Errorf("postgres: dataset observation graph is incomplete: %w", rowErr)
		}
	}
	return nil
}

func (repo *DatasetRepo) verifyQualityChildren(ctx context.Context, result *dataset.QualityResult) error {
	checks, findings := result.Checks(), result.Findings()
	if err := verifyDatasetChildBytes(ctx, repo.pool, `SELECT canonical_bytes FROM dataset_quality_checks WHERE result_id=$1 ORDER BY sequence`, result.ID(), checks); err != nil {
		return fmt.Errorf("postgres: dataset quality check graph: %w", err)
	}
	if err := verifyDatasetChildBytes(ctx, repo.pool, `SELECT canonical_bytes FROM dataset_quality_findings WHERE result_id=$1 ORDER BY sequence`, result.ID(), findings); err != nil {
		return fmt.Errorf("postgres: dataset quality finding graph: %w", err)
	}
	return nil
}

func verifyDatasetChildBytes[T any](ctx context.Context, pool *pgxpool.Pool, query string, id uuid.UUID, want []T) error {
	rows, err := pool.Query(ctx, query, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if index >= len(want) {
			return fmt.Errorf("relational graph has extra rows")
		}
		expected, _ := json.Marshal(want[index])
		if !bytes.Equal(raw, expected) {
			return fmt.Errorf("relational row differs from canonical evidence")
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if index != len(want) {
		return fmt.Errorf("relational graph is incomplete")
	}
	return nil
}

func verifyDatasetEnvelope(ctx context.Context, tx pgx.Tx, table string, id uuid.UUID, digest string, raw []byte) error {
	if table != "dataset_manifests" && table != "dataset_quality_results" {
		return fmt.Errorf("postgres: unsupported dataset evidence table")
	}
	var storedDigest string
	var storedRaw []byte
	if err := tx.QueryRow(ctx, fmt.Sprintf("SELECT sha256,canonical_bytes FROM %s WHERE id=$1", table), id).Scan(&storedDigest, &storedRaw); err != nil {
		return err
	}
	if storedDigest != digest || !bytes.Equal(storedRaw, raw) {
		return repository.ErrIdempotencyConflict
	}
	return nil
}

func (repo *DatasetRepo) stage(name string) error {
	if repo.afterStage == nil {
		return nil
	}
	if err := repo.afterStage(name); err != nil {
		return fmt.Errorf("postgres: dataset persistence stopped after %s: %w", name, err)
	}
	return nil
}

func sameDatasetPolicyArtifact(left, right *dataset.PolicyArtifact) bool {
	return left != nil && right != nil && left.ID == right.ID && left.Schema == right.Schema && left.Version == right.Version &&
		left.SHA256 == right.SHA256 && bytes.Equal(left.CanonicalBytes, right.CanonicalBytes) && left.CreatedAt.Equal(right.CreatedAt)
}

func validDatasetCreatedAt(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}

func nullableDatasetString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
