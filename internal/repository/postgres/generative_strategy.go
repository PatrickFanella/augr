package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/generativestrategy"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type GenerativeStrategyRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ generativestrategy.Store = (*GenerativeStrategyRepo)(nil)

func NewGenerativeStrategyRepo(pool *pgxpool.Pool) *GenerativeStrategyRepo {
	return &GenerativeStrategyRepo{pool: pool}
}

type generatedSpecEnvelope struct {
	Schema       string            `json:"schema"`
	FamilyID     string            `json:"family_id"`
	FamilySHA256 string            `json:"family_sha256"`
	SpecKey      string            `json:"spec_key"`
	Inputs       []json.RawMessage `json:"inputs"`
	Universe     struct {
		Instruments []string `json:"instruments"`
	} `json:"universe"`
	ProhibitedBehaviors []string          `json:"prohibited_behaviors"`
	PropertyTests       []string          `json:"property_tests"`
	ExampleTests        []json.RawMessage `json:"example_tests"`
}

type generatedReceiptEnvelope struct {
	Schema           string `json:"schema"`
	State            string `json:"state"`
	FamilyID         string `json:"family_id"`
	FamilySHA256     string `json:"family_sha256"`
	SpecID           string `json:"spec_id"`
	SpecSHA256       string `json:"spec_sha256"`
	VersionID        string `json:"version_id"`
	VersionSHA256    string `json:"version_sha256"`
	CompilerKind     string `json:"compiler_kind"`
	CompilerVersion  string `json:"compiler_version"`
	SourceCommit     string `json:"source_commit"`
	SourceTreeSHA256 string `json:"source_tree_sha256"`
	ConfigSchema     string `json:"config_schema"`
	DecisionContract string `json:"decision_contract"`
	ConfigSHA256     string `json:"config_sha256"`
}

type generatedNormalizedRow struct {
	kind     string
	sequence int
	raw      json.RawMessage
}

func generatedRows(envelope generatedSpecEnvelope) ([]generatedNormalizedRow, error) {
	rows := make([]generatedNormalizedRow, 0, len(envelope.Inputs)+len(envelope.Universe.Instruments)+len(envelope.ProhibitedBehaviors)+len(envelope.PropertyTests)+len(envelope.ExampleTests))
	for sequence, raw := range envelope.Inputs {
		rows = append(rows, generatedNormalizedRow{"input", sequence, raw})
	}
	for sequence, value := range envelope.Universe.Instruments {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, generatedNormalizedRow{"instrument", sequence, raw})
	}
	for sequence, value := range envelope.ProhibitedBehaviors {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, generatedNormalizedRow{"prohibition", sequence, raw})
	}
	for sequence, value := range envelope.PropertyTests {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, generatedNormalizedRow{"property", sequence, raw})
	}
	for sequence, raw := range envelope.ExampleTests {
		rows = append(rows, generatedNormalizedRow{"example", sequence, raw})
	}
	return rows, nil
}

func (r *GenerativeStrategyRepo) RegisterCompilation(ctx context.Context, spec *generativestrategy.Spec, version *strategycatalog.Version, receipt *generativestrategy.Receipt) (*generativestrategy.Spec, *strategycatalog.Version, *generativestrategy.Receipt, error) {
	if r == nil || r.pool == nil || spec == nil || version == nil || receipt == nil || receipt.SpecID() != spec.ID() || receipt.VersionID() != version.ID() {
		return nil, nil, nil, fmt.Errorf("postgres: generated strategy compilation is required")
	}
	var specEnvelope generatedSpecEnvelope
	var receiptEnvelope generatedReceiptEnvelope
	if err := json.Unmarshal(spec.CanonicalBytes(), &specEnvelope); err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(receipt.CanonicalBytes(), &receiptEnvelope); err != nil {
		return nil, nil, nil, err
	}
	rows, err := generatedRows(specEnvelope)
	if err != nil {
		return nil, nil, nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var familySHA, versionSHA string
	if err = tx.QueryRow(ctx, `SELECT sha256 FROM strategy_families WHERE id=$1`, spec.FamilyID()).Scan(&familySHA); err != nil || familySHA != specEnvelope.FamilySHA256 {
		return nil, nil, nil, fmt.Errorf("postgres: generated strategy family is missing or changed")
	}
	if err = tx.QueryRow(ctx, `SELECT sha256 FROM strategy_versions WHERE id=$1`, version.ID()).Scan(&versionSHA); err != nil || versionSHA != version.Digest() {
		return nil, nil, nil, fmt.Errorf("postgres: generated strategy version is missing or changed")
	}
	_, err = tx.Exec(ctx, `INSERT INTO generated_strategy_specs(id,schema_name,family_id,family_sha256,spec_key,input_count,instrument_count,prohibition_count,property_count,example_count,normalized_row_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,convert_from($13,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, spec.ID(), specEnvelope.Schema, specEnvelope.FamilyID, specEnvelope.FamilySHA256, specEnvelope.SpecKey, len(specEnvelope.Inputs), len(specEnvelope.Universe.Instruments), len(specEnvelope.ProhibitedBehaviors), len(specEnvelope.PropertyTests), len(specEnvelope.ExampleTests), len(rows), spec.Digest(), spec.CanonicalBytes())
	if err != nil {
		return nil, nil, nil, generatedStrategyWriteError("insert spec", err)
	}
	if err = r.stage("parent"); err != nil {
		return nil, nil, nil, err
	}
	for _, row := range rows {
		_, err = tx.Exec(ctx, `INSERT INTO generated_strategy_spec_rows(spec_id,kind,sequence,canonical_row) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(spec_id,kind,sequence) DO NOTHING`, spec.ID(), row.kind, row.sequence, string(row.raw))
		if err != nil {
			return nil, nil, nil, generatedStrategyWriteError("insert normalized row", err)
		}
		if err = r.stage("row"); err != nil {
			return nil, nil, nil, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO generated_strategy_compilation_receipts(id,schema_name,state,family_id,family_sha256,spec_id,spec_sha256,version_id,version_sha256,compiler_kind,compiler_version,source_commit,source_tree_sha256,config_schema,decision_contract,config_sha256,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,convert_from($18,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, receipt.ID(), receiptEnvelope.Schema, receiptEnvelope.State, receiptEnvelope.FamilyID, receiptEnvelope.FamilySHA256, receiptEnvelope.SpecID, receiptEnvelope.SpecSHA256, receiptEnvelope.VersionID, receiptEnvelope.VersionSHA256, receiptEnvelope.CompilerKind, receiptEnvelope.CompilerVersion, receiptEnvelope.SourceCommit, receiptEnvelope.SourceTreeSHA256, receiptEnvelope.ConfigSchema, receiptEnvelope.DecisionContract, receiptEnvelope.ConfigSHA256, receipt.Digest(), receipt.CanonicalBytes())
	if err != nil {
		return nil, nil, nil, generatedStrategyWriteError("insert receipt", err)
	}
	if err = r.stage("receipt"); err != nil {
		return nil, nil, nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, nil, nil, generatedStrategyWriteError("commit", err)
	}
	loadedSpec, loadedVersion, loadedReceipt, err := r.GetCompilation(ctx, spec.ID())
	if err != nil {
		return nil, nil, nil, err
	}
	if !bytes.Equal(loadedSpec.CanonicalBytes(), spec.CanonicalBytes()) || loadedVersion.Digest() != version.Digest() || !bytes.Equal(loadedReceipt.CanonicalBytes(), receipt.CanonicalBytes()) {
		return nil, nil, nil, fmt.Errorf("postgres: generated strategy conflict: %w", repository.ErrIdempotencyConflict)
	}
	return loadedSpec, loadedVersion, loadedReceipt, nil
}

func (r *GenerativeStrategyRepo) GetCompilation(ctx context.Context, specID uuid.UUID) (*generativestrategy.Spec, *strategycatalog.Version, *generativestrategy.Receipt, error) {
	if r == nil || r.pool == nil || specID == uuid.Nil {
		return nil, nil, nil, fmt.Errorf("postgres: generated strategy identity is required")
	}
	var specDigest string
	var specRaw []byte
	var familyID uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,family_id FROM generated_strategy_specs WHERE id=$1`, specID).Scan(&specDigest, &specRaw, &familyID); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, repository.ErrNotFound
	} else if err != nil {
		return nil, nil, nil, err
	}
	catalog := NewStrategyCatalogRepo(r.pool)
	family, err := catalog.GetStrategyFamily(ctx, familyID)
	if err != nil {
		return nil, nil, nil, err
	}
	spec, err := generativestrategy.SpecFromCanonical(specID, specDigest, specRaw, family)
	if err != nil {
		return nil, nil, nil, err
	}
	var envelope generatedSpecEnvelope
	if err = json.Unmarshal(specRaw, &envelope); err != nil {
		return nil, nil, nil, err
	}
	expectedRows, err := generatedRows(envelope)
	if err != nil {
		return nil, nil, nil, err
	}
	dbRows, err := r.pool.Query(ctx, `SELECT kind,sequence,canonical_row FROM generated_strategy_spec_rows WHERE spec_id=$1 ORDER BY kind,sequence`, specID)
	if err != nil {
		return nil, nil, nil, err
	}
	defer dbRows.Close()
	stored := map[string]json.RawMessage{}
	for dbRows.Next() {
		var kind string
		var sequence int
		var raw []byte
		if err = dbRows.Scan(&kind, &sequence, &raw); err != nil {
			return nil, nil, nil, err
		}
		stored[fmt.Sprintf("%s:%d", kind, sequence)] = raw
	}
	if len(stored) != len(expectedRows) {
		return nil, nil, nil, fmt.Errorf("postgres: generated strategy rows do not reconstruct")
	}
	for _, row := range expectedRows {
		if !jsonEqual(stored[fmt.Sprintf("%s:%d", row.kind, row.sequence)], row.raw) {
			return nil, nil, nil, fmt.Errorf("postgres: generated strategy row does not reconstruct")
		}
	}
	var receiptID, versionID uuid.UUID
	var receiptDigest string
	var receiptRaw []byte
	if err = r.pool.QueryRow(ctx, `SELECT id,version_id,sha256,canonical_bytes FROM generated_strategy_compilation_receipts WHERE spec_id=$1`, specID).Scan(&receiptID, &versionID, &receiptDigest, &receiptRaw); err != nil {
		return nil, nil, nil, err
	}
	version, err := catalog.GetStrategyVersion(ctx, versionID)
	if err != nil {
		return nil, nil, nil, err
	}
	receipt, err := generativestrategy.ReceiptFromCanonical(receiptID, receiptDigest, receiptRaw, spec, version)
	if err != nil {
		return nil, nil, nil, err
	}
	return spec, version, receipt, nil
}

func (r *GenerativeStrategyRepo) stage(value string) error {
	if r.afterStage != nil {
		return r.afterStage(value)
	}
	return nil
}

func generatedStrategyWriteError(action string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "does not reconstruct") || strings.Contains(err.Error(), "foreign key")) {
		return fmt.Errorf("postgres: generated strategy %s conflict: %w", action, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: generated strategy %s: %w", action, err)
}
