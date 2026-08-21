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

	"github.com/PatrickFanella/get-rich-quick/internal/capacity"
	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type CapacityRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ capacity.Store = (*CapacityRepo)(nil)

func NewCapacityRepo(pool *pgxpool.Pool) *CapacityRepo { return &CapacityRepo{pool: pool} }

type (
	capacityContractEnvelope struct {
		Schema             string              `json:"schema"`
		State              string              `json:"state"`
		Family             capacity.FamilyKind `json:"family"`
		EvaluationID       string              `json:"evaluation_id"`
		EvaluationSHA256   string              `json:"evaluation_sha256"`
		SourceReportID     string              `json:"source_report_id"`
		SourceReportSHA256 string              `json:"source_report_sha256"`
		EvaluationStart    string              `json:"evaluation_start"`
		EvaluationEnd      string              `json:"evaluation_end"`
		AfterCostReturn    string              `json:"after_cost_return"`
		CapacityAvailable  bool                `json:"capacity_available"`
		UnavailableReason  string              `json:"unavailable_reason"`
		CapitalPerUnit     string              `json:"capital_per_unit"`
		MaximumUnits       int                 `json:"maximum_units"`
	}
	capacityComparisonEnvelope struct {
		Schema               string            `json:"schema"`
		State                string            `json:"state"`
		CapitalPolicyVersion string            `json:"capital_policy_version"`
		Families             []json.RawMessage `json:"families"`
	}
	capacityFamilyEnvelope struct {
		Family                 capacity.FamilyKind `json:"family"`
		ContractID             string              `json:"contract_id"`
		ContractSHA256         string              `json:"contract_sha256"`
		AfterCostReturn        string              `json:"after_cost_return"`
		MinimumViableTier      string              `json:"minimum_viable_tier"`
		MinimumViableAvailable bool                `json:"minimum_viable_available"`
		Tiers                  []json.RawMessage   `json:"tiers"`
	}
	capacityTierEnvelope struct {
		Ordinal           int    `json:"ordinal"`
		Tier              string `json:"tier"`
		Viable            bool   `json:"viable"`
		Reason            string `json:"reason"`
		Units             int    `json:"units"`
		ExecutableCapital string `json:"executable_capital"`
		UnusedCapital     string `json:"unused_capital"`
		Saturated         bool   `json:"saturated"`
	}
)

func (r *CapacityRepo) RegisterContract(ctx context.Context, v *capacity.Contract) (*capacity.Contract, error) {
	if r == nil || r.pool == nil || v == nil {
		return nil, fmt.Errorf("postgres: capacity contract is required")
	}
	var e capacityContractEnvelope
	if err := json.Unmarshal(v.CanonicalBytes(), &e); err != nil {
		return nil, err
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO capacity_v1_contracts(id,schema_name,state,family,evaluation_id,evaluation_sha256,source_report_id,source_report_sha256,evaluation_start,evaluation_end,after_cost_return,capacity_available,unavailable_reason,capital_per_unit,maximum_units,sha256,canonical_bytes,canonical_json,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,convert_from($17,'UTF8')::jsonb,$18) ON CONFLICT(id) DO NOTHING`, v.ID(), e.Schema, e.State, e.Family, e.EvaluationID, e.EvaluationSHA256, e.SourceReportID, e.SourceReportSHA256, parseCapacityTime(e.EvaluationStart), parseCapacityTime(e.EvaluationEnd), e.AfterCostReturn, e.CapacityAvailable, e.UnavailableReason, e.CapitalPerUnit, e.MaximumUnits, v.Digest(), v.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert capacity contract", err)
	}
	got, err := r.GetContract(ctx, v.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != v.Digest() || !bytes.Equal(got.CanonicalBytes(), v.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: capacity contract conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (r *CapacityRepo) GetContract(ctx context.Context, id uuid.UUID) (*capacity.Contract, error) {
	if r == nil || r.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: capacity contract identity is required")
	}
	var digest string
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM capacity_v1_contracts WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v, err := capacity.ContractFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct capacity contract %s: %w", id, err)
	}
	return v, nil
}

func (r *CapacityRepo) RecordComparison(ctx context.Context, v *capacity.Comparison) (*capacity.Comparison, error) {
	if r == nil || r.pool == nil || v == nil {
		return nil, fmt.Errorf("postgres: capacity comparison is required")
	}
	var e capacityComparisonEnvelope
	if err := json.Unmarshal(v.CanonicalBytes(), &e); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO capacity_v1_comparisons(id,schema_name,state,capital_policy_version,family_count,sha256,canonical_bytes,canonical_json,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::jsonb,$8) ON CONFLICT(id) DO NOTHING`, v.ID(), e.Schema, e.State, e.CapitalPolicyVersion, len(e.Families), v.Digest(), v.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert capacity comparison", err)
	}
	if err = r.stage("capacity_comparison"); err != nil {
		return nil, err
	}
	for sequence, raw := range e.Families {
		var f capacityFamilyEnvelope
		if err = json.Unmarshal(raw, &f); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO capacity_v1_families(comparison_id,sequence,family,contract_id,contract_sha256,minimum_viable_tier,minimum_viable_available,tier_count,canonical_family) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb) ON CONFLICT(comparison_id,sequence) DO NOTHING`, v.ID(), sequence, f.Family, f.ContractID, f.ContractSHA256, f.MinimumViableTier, f.MinimumViableAvailable, len(f.Tiers), string(raw))
		if err != nil {
			return nil, evaluationWriteError("insert capacity family", err)
		}
		if err = r.stage("capacity_family"); err != nil {
			return nil, err
		}
		for _, tierRaw := range f.Tiers {
			var tier capacityTierEnvelope
			if err = json.Unmarshal(tierRaw, &tier); err != nil {
				return nil, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO capacity_v1_tiers(comparison_id,family_sequence,ordinal,tier,viable,reason,units,executable_capital,unused_capital,saturated,canonical_tier) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb) ON CONFLICT(comparison_id,family_sequence,ordinal) DO NOTHING`, v.ID(), sequence, tier.Ordinal, tier.Tier, tier.Viable, tier.Reason, tier.Units, tier.ExecutableCapital, tier.UnusedCapital, tier.Saturated, string(tierRaw))
			if err != nil {
				return nil, evaluationWriteError("insert capacity tier", err)
			}
			if err = r.stage("capacity_tier"); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit capacity comparison", err)
	}
	got, err := r.GetComparison(ctx, v.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != v.Digest() || !bytes.Equal(got.CanonicalBytes(), v.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: capacity comparison conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (r *CapacityRepo) GetComparison(ctx context.Context, id uuid.UUID) (*capacity.Comparison, error) {
	if r == nil || r.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: capacity comparison identity is required")
	}
	var digest string
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM capacity_v1_comparisons WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT contract_id,canonical_family FROM capacity_v1_families WHERE comparison_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	contracts := []*capacity.Contract{}
	expected := []json.RawMessage{}
	for rows.Next() {
		var contractID uuid.UUID
		var family []byte
		if rows.Scan(&contractID, &family) != nil {
			rows.Close()
			return nil, fmt.Errorf("postgres: capacity family scan failed")
		}
		contract, loadErr := r.GetContract(ctx, contractID)
		if loadErr != nil {
			rows.Close()
			return nil, loadErr
		}
		contracts = append(contracts, contract)
		expected = append(expected, family)
	}
	rows.Close()
	var envelope capacityComparisonEnvelope
	_ = json.Unmarshal(raw, &envelope)
	if len(expected) != len(envelope.Families) {
		return nil, fmt.Errorf("postgres: normalized capacity comparison %s does not reconstruct", id)
	}
	for i := range expected {
		if !jsonEqual(expected[i], envelope.Families[i]) {
			return nil, fmt.Errorf("postgres: normalized capacity comparison %s does not reconstruct", id)
		}
		var family capacityFamilyEnvelope
		_ = json.Unmarshal(expected[i], &family)
		tierRows, queryErr := r.pool.Query(ctx, `SELECT canonical_tier FROM capacity_v1_tiers WHERE comparison_id=$1 AND family_sequence=$2 ORDER BY ordinal`, id, i)
		if queryErr != nil {
			return nil, queryErr
		}
		index := 0
		for tierRows.Next() {
			var got []byte
			if tierRows.Scan(&got) != nil || index >= len(family.Tiers) || !jsonEqual(got, family.Tiers[index]) {
				tierRows.Close()
				return nil, fmt.Errorf("postgres: normalized capacity comparison %s does not reconstruct", id)
			}
			index++
		}
		tierRows.Close()
		if index != len(family.Tiers) {
			return nil, fmt.Errorf("postgres: normalized capacity comparison %s does not reconstruct", id)
		}
	}
	policy, _ := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	v, err := capacity.ComparisonFromCanonical(id, digest, raw, policy, contracts)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct capacity comparison %s: %w", id, err)
	}
	return v, nil
}

func (r *CapacityRepo) stage(v string) error {
	if r.afterStage != nil {
		return r.afterStage(v)
	}
	return nil
}

func parseCapacityTime(v string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", v)
	return parsed
}
