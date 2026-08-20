package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type StrategyCatalogRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ repository.StrategyCatalogRepository = (*StrategyCatalogRepo)(nil)

func NewStrategyCatalogRepo(pool *pgxpool.Pool) *StrategyCatalogRepo {
	return &StrategyCatalogRepo{pool: pool}
}

func (repo *StrategyCatalogRepo) RegisterStrategyFamily(ctx context.Context, family *strategycatalog.Family) (*strategycatalog.Family, error) {
	if family == nil {
		return nil, fmt.Errorf("postgres: strategy family is required")
	}
	evidence, err := strategycatalog.NewInitialLifecycleEvidence(strategycatalog.EntityFamily, family.ID(), family.Digest())
	if err != nil {
		return nil, err
	}
	assetClasses, err := json.Marshal(family.AssetClasses())
	if err != nil {
		return nil, err
	}
	err = repo.writeGraph(ctx, "family_parent", func(tx pgx.Tx, createdAt time.Time) error {
		result, err := tx.Exec(ctx, `INSERT INTO strategy_families(
			id,schema_name,slug,name,thesis,asset_classes,sha256,canonical_bytes,canonical_json,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,convert_from($8,'UTF8')::jsonb,$9) ON CONFLICT(id) DO NOTHING`,
			family.ID(), strategycatalog.FamilySchemaV1, family.Slug(), family.Name(), family.Thesis(), string(assetClasses),
			family.Digest(), family.CanonicalBytes(), createdAt)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			stored, loadErr := getStrategyFamilyTx(ctx, tx, family.ID())
			if loadErr != nil || !bytes.Equal(stored.CanonicalBytes(), family.CanonicalBytes()) {
				return fmt.Errorf("postgres: strategy family changed on retry: %w", repository.ErrIdempotencyConflict)
			}
			return nil
		}
		return insertStrategyLifecycle(ctx, tx, evidence, createdAt)
	})
	if err != nil {
		return nil, err
	}
	return repo.GetStrategyFamily(ctx, family.ID())
}

func (repo *StrategyCatalogRepo) GetStrategyFamily(ctx context.Context, id uuid.UUID) (*strategycatalog.Family, error) {
	return getStrategyFamilyQuery(ctx, repo.pool, id)
}

func (repo *StrategyCatalogRepo) RegisterStrategyVersion(ctx context.Context, version *strategycatalog.Version) (*strategycatalog.Version, error) {
	if version == nil {
		return nil, fmt.Errorf("postgres: strategy version is required")
	}
	evidence, err := strategycatalog.NewInitialLifecycleEvidence(strategycatalog.EntityVersion, version.ID(), version.Digest())
	if err != nil {
		return nil, err
	}
	err = repo.writeGraph(ctx, "version_parent", func(tx pgx.Tx, createdAt time.Time) error {
		var familyExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM strategy_families WHERE id=$1)`, version.FamilyID()).Scan(&familyExists); err != nil || !familyExists {
			return fmt.Errorf("postgres: strategy version family is missing")
		}
		result, err := tx.Exec(ctx, `INSERT INTO strategy_versions(
			id,schema_name,family_id,compiler_kind,compiler_version,source_commit,source_tree_sha256,config_schema,config_bytes,config,
			decision_contract,required_kind_count,sha256,canonical_bytes,canonical_json,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,convert_from($9,'UTF8')::jsonb,$10,$11,$12,$13,convert_from($13,'UTF8')::jsonb,$14)
		ON CONFLICT(id) DO NOTHING`, version.ID(), strategycatalog.VersionSchemaV1, version.FamilyID(), version.CompilerKind(),
			version.CompilerVersion(), version.SourceCommit(), version.SourceTreeSHA256(), version.ConfigSchema(), version.Config(),
			version.DecisionContract(), len(version.RequiredDatasetKinds()), version.Digest(), version.CanonicalBytes(), createdAt)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			stored, loadErr := getStrategyVersionTx(ctx, tx, version.ID())
			if loadErr != nil || !bytes.Equal(stored.CanonicalBytes(), version.CanonicalBytes()) {
				return fmt.Errorf("postgres: strategy version changed on retry: %w", repository.ErrIdempotencyConflict)
			}
			return nil
		}
		for sequence, kind := range version.RequiredDatasetKinds() {
			if _, err := tx.Exec(ctx, `INSERT INTO strategy_version_dataset_kinds(version_id,family_id,sequence,kind) VALUES($1,$2,$3,$4)`,
				version.ID(), version.FamilyID(), sequence, kind); err != nil {
				return err
			}
		}
		if err := repo.stage("version_kinds"); err != nil {
			return err
		}
		return insertStrategyLifecycle(ctx, tx, evidence, createdAt)
	})
	if err != nil {
		return nil, err
	}
	return repo.GetStrategyVersion(ctx, version.ID())
}

func (repo *StrategyCatalogRepo) GetStrategyVersion(ctx context.Context, id uuid.UUID) (*strategycatalog.Version, error) {
	return getStrategyVersionQuery(ctx, repo.pool, id)
}

func (repo *StrategyCatalogRepo) DeclareResearchExperiment(ctx context.Context, experiment *strategycatalog.Experiment) (*strategycatalog.Experiment, error) {
	if experiment == nil {
		return nil, fmt.Errorf("postgres: research experiment is required")
	}
	evidence, err := strategycatalog.NewInitialLifecycleEvidence(strategycatalog.EntityExperiment, experiment.ID(), experiment.Digest())
	if err != nil {
		return nil, err
	}
	err = repo.writeGraph(ctx, "experiment_parent", func(tx pgx.Tx, createdAt time.Time) error {
		if err := admitResearchExperiment(ctx, tx, experiment); err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `INSERT INTO research_experiments(
			id,schema_name,state,version_id,account_id,capital_binding_id,manifest_id,quality_result_id,simulation_policy_version,
			capital_policy_version,mode,evaluation_start,evaluation_end,seed,dataset_quarantined,sha256,canonical_bytes,canonical_json,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,convert_from($17,'UTF8')::jsonb,$18)
		ON CONFLICT(id) DO NOTHING`, experiment.ID(), strategycatalog.ExperimentSchemaV1, experiment.State(), experiment.VersionID(),
			experiment.AccountID(), experiment.CapitalBindingID(), experiment.ManifestID(), experiment.QualityResultID(),
			experiment.SimulationPolicyVersion(), experiment.CapitalPolicyVersion(), experiment.Mode(), experiment.EvaluationStart(),
			experiment.EvaluationEnd(), experiment.Seed(), experiment.DatasetQuarantined(), experiment.Digest(), experiment.CanonicalBytes(), createdAt)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			stored, loadErr := getResearchExperimentTx(ctx, tx, experiment.ID())
			if loadErr != nil || !bytes.Equal(stored.CanonicalBytes(), experiment.CanonicalBytes()) {
				return fmt.Errorf("postgres: research experiment changed on retry: %w", repository.ErrIdempotencyConflict)
			}
			return nil
		}
		return insertStrategyLifecycle(ctx, tx, evidence, createdAt)
	})
	if err != nil {
		return nil, err
	}
	return repo.GetResearchExperiment(ctx, experiment.ID())
}

func (repo *StrategyCatalogRepo) GetResearchExperiment(ctx context.Context, id uuid.UUID) (*strategycatalog.Experiment, error) {
	return getResearchExperimentQuery(ctx, repo.pool, id)
}

func (repo *StrategyCatalogRepo) ProposeStrategyDeployment(ctx context.Context, deployment *strategycatalog.Deployment) (*strategycatalog.Deployment, error) {
	if deployment == nil {
		return nil, fmt.Errorf("postgres: strategy deployment is required")
	}
	evidence, err := strategycatalog.NewInitialLifecycleEvidence(strategycatalog.EntityDeployment, deployment.ID(), deployment.Digest())
	if err != nil {
		return nil, err
	}
	err = repo.writeGraph(ctx, "deployment_parent", func(tx pgx.Tx, createdAt time.Time) error {
		var admitted bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM strategy_versions v
			JOIN account_capital_policy_bindings b ON b.id=$1 AND b.account_id=$2
			WHERE v.id=$3 AND $4::numeric<=b.starting_capital AND b.environment=$5 AND
			((b.environment='paper_scored' AND b.evidence_class='promotion_evidence') OR
			 (b.environment='paper_stress' AND b.evidence_class='synthetic_stress')))`, deployment.CapitalBindingID(), deployment.AccountID(),
			deployment.VersionID(), deployment.Budget(), deployment.Mode()).Scan(&admitted); err != nil || !admitted {
			return fmt.Errorf("postgres: strategy deployment evidence is not admissible")
		}
		result, err := tx.Exec(ctx, `INSERT INTO strategy_deployments(
			id,schema_name,state,activation_authority,version_id,account_id,capital_binding_id,budget,schedule_cron,timezone_name,
			risk_policy_version,mode,sha256,canonical_bytes,canonical_json,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,convert_from($14,'UTF8')::jsonb,$15) ON CONFLICT(id) DO NOTHING`,
			deployment.ID(), strategycatalog.DeploymentSchemaV1, deployment.State(), deployment.ActivationAuthority(), deployment.VersionID(),
			deployment.AccountID(), deployment.CapitalBindingID(), deployment.Budget(), deployment.ScheduleCron(), deployment.Timezone(),
			deployment.RiskPolicyVersion(), deployment.Mode(), deployment.Digest(), deployment.CanonicalBytes(), createdAt)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			stored, loadErr := getStrategyDeploymentTx(ctx, tx, deployment.ID())
			if loadErr != nil || !bytes.Equal(stored.CanonicalBytes(), deployment.CanonicalBytes()) {
				return fmt.Errorf("postgres: strategy deployment changed on retry: %w", repository.ErrIdempotencyConflict)
			}
			return nil
		}
		return insertStrategyLifecycle(ctx, tx, evidence, createdAt)
	})
	if err != nil {
		return nil, err
	}
	return repo.GetStrategyDeployment(ctx, deployment.ID())
}

func (repo *StrategyCatalogRepo) GetStrategyDeployment(ctx context.Context, id uuid.UUID) (*strategycatalog.Deployment, error) {
	return getStrategyDeploymentQuery(ctx, repo.pool, id)
}

func (repo *StrategyCatalogRepo) MapLegacyStrategyFamily(ctx context.Context, mapping *strategycatalog.LegacyMapping) (*strategycatalog.LegacyMapping, error) {
	if mapping == nil {
		return nil, fmt.Errorf("postgres: legacy strategy mapping is required")
	}
	evidence, err := strategycatalog.NewInitialLifecycleEvidence(strategycatalog.EntityLegacyMapping, mapping.ID(), mapping.Digest())
	if err != nil {
		return nil, err
	}
	err = repo.writeGraph(ctx, "legacy_mapping_parent", func(tx pgx.Tx, createdAt time.Time) error {
		var admitted bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM strategies s JOIN strategy_families f ON f.id=$1
			WHERE s.id=$2 AND strategy_legacy_snapshot_sha(s.id)=$3)`, mapping.FamilyID(), mapping.LegacyStrategyID(), mapping.LegacySnapshotSHA256()).Scan(&admitted); err != nil || !admitted {
			return fmt.Errorf("postgres: legacy strategy snapshot is not admissible")
		}
		result, err := tx.Exec(ctx, `INSERT INTO legacy_strategy_family_mappings(
			id,schema_name,state,legacy_strategy_id,family_id,legacy_snapshot_sha256,sha256,canonical_bytes,canonical_json,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,convert_from($8,'UTF8')::jsonb,$9) ON CONFLICT(id) DO NOTHING`, mapping.ID(),
			strategycatalog.LegacyMappingSchemaV1, mapping.State(), mapping.LegacyStrategyID(), mapping.FamilyID(), mapping.LegacySnapshotSHA256(),
			mapping.Digest(), mapping.CanonicalBytes(), createdAt)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			stored, loadErr := getLegacyStrategyFamilyMappingTx(ctx, tx, mapping.ID())
			if loadErr != nil || !bytes.Equal(stored.CanonicalBytes(), mapping.CanonicalBytes()) {
				return fmt.Errorf("postgres: legacy mapping changed on retry: %w", repository.ErrIdempotencyConflict)
			}
			return nil
		}
		return insertStrategyLifecycle(ctx, tx, evidence, createdAt)
	})
	if err != nil {
		return nil, err
	}
	return repo.GetLegacyStrategyFamilyMapping(ctx, mapping.ID())
}

func (repo *StrategyCatalogRepo) GetLegacyStrategyFamilyMapping(ctx context.Context, id uuid.UUID) (*strategycatalog.LegacyMapping, error) {
	return getLegacyStrategyFamilyMappingQuery(ctx, repo.pool, id)
}

func (repo *StrategyCatalogRepo) writeGraph(ctx context.Context, firstStage string, write func(pgx.Tx, time.Time) error) error {
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := write(tx, createdAt); err != nil {
		return err
	}
	if err := repo.stage(firstStage); err != nil {
		return err
	}
	if err := repo.stage("lifecycle"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repo *StrategyCatalogRepo) stage(name string) error {
	if repo.afterStage == nil {
		return nil
	}
	return repo.afterStage(name)
}

func admitResearchExperiment(ctx context.Context, tx pgx.Tx, experiment *strategycatalog.Experiment) error {
	var admitted bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM strategy_versions v
		JOIN dataset_quality_results q ON q.id=$1 AND q.manifest_id=$2 AND q.quarantined=$3
		JOIN dataset_manifests m ON m.id=q.manifest_id AND m.decision_cutoff>=$4
		JOIN simulation_policy_artifacts s ON s.policy_version=$5
		JOIN account_capital_policy_bindings b ON b.id=$6 AND b.account_id=$7 AND b.policy_version=$8
		WHERE v.id=$9 AND (($10='paper_scored' AND NOT q.quarantined AND b.environment='paper_scored' AND b.evidence_class='promotion_evidence') OR
		($10='paper_stress' AND b.environment='paper_stress' AND b.evidence_class='synthetic_stress')) AND
		NOT EXISTS(SELECT 1 FROM strategy_version_dataset_kinds k WHERE k.version_id=v.id AND NOT EXISTS(
			SELECT 1 FROM dataset_manifest_partitions p WHERE p.manifest_id=m.id AND p.kind=k.kind)))`,
		experiment.QualityResultID(), experiment.ManifestID(), experiment.DatasetQuarantined(), experiment.EvaluationEnd(),
		experiment.SimulationPolicyVersion(), experiment.CapitalBindingID(), experiment.AccountID(), experiment.CapitalPolicyVersion(),
		experiment.VersionID(), experiment.Mode()).Scan(&admitted)
	if err != nil {
		return err
	}
	if !admitted {
		return fmt.Errorf("postgres: research experiment evidence is not admissible")
	}
	return nil
}

func insertStrategyLifecycle(ctx context.Context, tx pgx.Tx, evidence *strategycatalog.LifecycleEvidence, createdAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO strategy_catalog_lifecycle_events(
		id,schema_name,entity_kind,entity_id,event_kind,prior_state,next_state,evidence_sha256,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,$2,$3,$4,$5,'',$6,$7,$8,$9,convert_from($9,'UTF8')::jsonb,$10) ON CONFLICT(id) DO NOTHING`, evidence.ID(),
		strategycatalog.LifecycleEvidenceSchemaV1, evidence.EntityKind(), evidence.EntityID(), evidence.EventKind(), evidence.NextState(),
		evidence.EvidenceSHA256(), evidence.Digest(), evidence.CanonicalBytes(), createdAt)
	return err
}

type strategyCatalogQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func getStrategyFamilyQuery(ctx context.Context, query strategyCatalogQuery, id uuid.UUID) (*strategycatalog.Family, error) {
	var digest, slug, name, thesis string
	var raw, assetsRaw []byte
	err := query.QueryRow(ctx, `SELECT sha256,canonical_bytes,slug,name,thesis,asset_classes::text FROM strategy_families WHERE id=$1`, id).
		Scan(&digest, &raw, &slug, &name, &thesis, &assetsRaw)
	if err != nil {
		return nil, strategyCatalogReadError("strategy family", err)
	}
	family, err := strategycatalog.FamilyFromCanonical(id, digest, raw)
	if err != nil || family.Slug() != slug || family.Name() != name || family.Thesis() != thesis {
		return nil, fmt.Errorf("postgres: strategy family relational identity does not reconstruct")
	}
	var assets []string
	if err := json.Unmarshal(assetsRaw, &assets); err != nil || len(assets) != len(family.AssetClasses()) {
		return nil, fmt.Errorf("postgres: strategy family assets do not reconstruct")
	}
	for index, asset := range family.AssetClasses() {
		if string(asset) != assets[index] {
			return nil, fmt.Errorf("postgres: strategy family assets do not reconstruct")
		}
	}
	return family, nil
}

func getStrategyFamilyTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*strategycatalog.Family, error) {
	return getStrategyFamilyQuery(ctx, tx, id)
}

func getStrategyVersionQuery(ctx context.Context, query strategyCatalogQuery, id uuid.UUID) (*strategycatalog.Version, error) {
	var digest, compilerKind, compilerVersion, sourceCommit, sourceTree, configSchema, decisionContract string
	var familyID uuid.UUID
	var raw, config []byte
	err := query.QueryRow(ctx, `SELECT sha256,canonical_bytes,family_id,compiler_kind,compiler_version,source_commit,
		source_tree_sha256,config_schema,config_bytes,decision_contract FROM strategy_versions WHERE id=$1`, id).
		Scan(&digest, &raw, &familyID, &compilerKind, &compilerVersion, &sourceCommit, &sourceTree, &configSchema, &config, &decisionContract)
	if err != nil {
		return nil, strategyCatalogReadError("strategy version", err)
	}
	version, err := strategycatalog.VersionFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: restore strategy version: %w", err)
	}
	if version.FamilyID() != familyID || version.CompilerKind() != compilerKind || version.CompilerVersion() != compilerVersion ||
		version.SourceCommit() != sourceCommit || version.SourceTreeSHA256() != sourceTree || version.ConfigSchema() != configSchema ||
		!bytes.Equal(version.Config(), config) || version.DecisionContract() != decisionContract {
		return nil, fmt.Errorf("postgres: strategy version relational identity does not reconstruct")
	}
	rows, err := query.Query(ctx, `SELECT kind FROM strategy_version_dataset_kinds WHERE version_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	kinds := make([]dataset.Kind, 0)
	for rows.Next() {
		var kind dataset.Kind
		if err := rows.Scan(&kind); err != nil {
			return nil, err
		}
		kinds = append(kinds, kind)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	want := version.RequiredDatasetKinds()
	if len(kinds) != len(want) {
		return nil, fmt.Errorf("postgres: strategy version dataset kinds do not reconstruct")
	}
	for index := range kinds {
		if kinds[index] != want[index] {
			return nil, fmt.Errorf("postgres: strategy version dataset kinds do not reconstruct")
		}
	}
	return version, nil
}

func getStrategyVersionTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*strategycatalog.Version, error) {
	return getStrategyVersionQuery(ctx, tx, id)
}

func getResearchExperimentQuery(ctx context.Context, query strategyCatalogQuery, id uuid.UUID) (*strategycatalog.Experiment, error) {
	var digest, state, simulationVersion, capitalVersion string
	var raw []byte
	var versionID, accountID, bindingID, manifestID, qualityID uuid.UUID
	var mode strategycatalog.ExperimentMode
	var start, end time.Time
	var seed int64
	var quarantined bool
	err := query.QueryRow(ctx, `SELECT sha256,canonical_bytes,state,version_id,account_id,capital_binding_id,manifest_id,quality_result_id,
		simulation_policy_version,capital_policy_version,mode,evaluation_start,evaluation_end,seed,dataset_quarantined
		FROM research_experiments WHERE id=$1`, id).Scan(&digest, &raw, &state, &versionID, &accountID, &bindingID, &manifestID,
		&qualityID, &simulationVersion, &capitalVersion, &mode, &start, &end, &seed, &quarantined)
	if err != nil {
		return nil, strategyCatalogReadError("research experiment", err)
	}
	experiment, err := strategycatalog.ExperimentFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: restore research experiment: %w", err)
	}
	if experiment.State() != state || experiment.VersionID() != versionID || experiment.AccountID() != accountID ||
		experiment.CapitalBindingID() != bindingID || experiment.ManifestID() != manifestID || experiment.QualityResultID() != qualityID ||
		experiment.SimulationPolicyVersion() != simulationVersion || experiment.CapitalPolicyVersion() != capitalVersion ||
		experiment.Mode() != mode || !experiment.EvaluationStart().Equal(start) || !experiment.EvaluationEnd().Equal(end) ||
		experiment.Seed() != seed || experiment.DatasetQuarantined() != quarantined {
		return nil, fmt.Errorf("postgres: research experiment relational identity does not reconstruct")
	}
	return experiment, nil
}

func getResearchExperimentTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*strategycatalog.Experiment, error) {
	return getResearchExperimentQuery(ctx, tx, id)
}

func getStrategyDeploymentQuery(ctx context.Context, query strategyCatalogQuery, id uuid.UUID) (*strategycatalog.Deployment, error) {
	var digest, state, authority, budget, cron, timezoneName, riskVersion string
	var raw []byte
	var versionID, accountID, bindingID uuid.UUID
	var mode strategycatalog.ExperimentMode
	err := query.QueryRow(ctx, `SELECT sha256,canonical_bytes,state,activation_authority,version_id,account_id,capital_binding_id,
		trim_scale(budget)::text,schedule_cron,timezone_name,risk_policy_version,mode FROM strategy_deployments WHERE id=$1`, id).
		Scan(&digest, &raw, &state, &authority, &versionID, &accountID, &bindingID, &budget, &cron, &timezoneName, &riskVersion, &mode)
	if err != nil {
		return nil, strategyCatalogReadError("strategy deployment", err)
	}
	deployment, err := strategycatalog.DeploymentFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: restore strategy deployment: %w", err)
	}
	if deployment.State() != state || deployment.ActivationAuthority() != authority || deployment.VersionID() != versionID ||
		deployment.AccountID() != accountID || deployment.CapitalBindingID() != bindingID || deployment.Budget() != budget ||
		deployment.ScheduleCron() != cron || deployment.Timezone() != timezoneName || deployment.RiskPolicyVersion() != riskVersion ||
		deployment.Mode() != mode {
		return nil, fmt.Errorf("postgres: strategy deployment relational identity does not reconstruct")
	}
	return deployment, nil
}

func getStrategyDeploymentTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*strategycatalog.Deployment, error) {
	return getStrategyDeploymentQuery(ctx, tx, id)
}

func getLegacyStrategyFamilyMappingQuery(ctx context.Context, query strategyCatalogQuery, id uuid.UUID) (*strategycatalog.LegacyMapping, error) {
	var digest, state, snapshot string
	var raw []byte
	var legacyID, familyID uuid.UUID
	err := query.QueryRow(ctx, `SELECT sha256,canonical_bytes,state,legacy_strategy_id,family_id,legacy_snapshot_sha256
		FROM legacy_strategy_family_mappings WHERE id=$1`, id).Scan(&digest, &raw, &state, &legacyID, &familyID, &snapshot)
	if err != nil {
		return nil, strategyCatalogReadError("legacy strategy mapping", err)
	}
	mapping, err := strategycatalog.LegacyMappingFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: restore legacy strategy mapping: %w", err)
	}
	if mapping.State() != state || mapping.LegacyStrategyID() != legacyID || mapping.FamilyID() != familyID || mapping.LegacySnapshotSHA256() != snapshot {
		return nil, fmt.Errorf("postgres: legacy strategy mapping relational identity does not reconstruct")
	}
	return mapping, nil
}

func getLegacyStrategyFamilyMappingTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*strategycatalog.LegacyMapping, error) {
	return getLegacyStrategyFamilyMappingQuery(ctx, tx, id)
}

func strategyCatalogReadError(entity string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("postgres: %s: %w", entity, repository.ErrNotFound)
	}
	return fmt.Errorf("postgres: read %s: %w", entity, err)
}
