package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/capacity"
	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

type capacityRepositoryFixture struct {
	base       benchmarkFixture
	contracts  []*capacity.Contract
	comparison *capacity.Comparison
	repo       *CapacityRepo
}

func newCapacityRepositoryFixture(t *testing.T) capacityRepositoryFixture {
	t.Helper()
	base := newBenchmarkFixture(t)
	ctx := context.Background()
	pool := base.evaluation.experiment.strategy.pool
	for _, migration := range []string{"000083_quality_filtered_wheel_v1.up.sql", "000084_momentum_quality_baseline.up.sql", "000085_etf_time_series_trend.up.sql", "000086_defined_risk_options.up.sql", "000087_capital_tier_candidate_comparison.up.sql"} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migration)); err != nil {
			t.Fatal(err)
		}
	}
	families := []capacity.FamilyKind{capacity.FamilyPassive, capacity.FamilyWheel, capacity.FamilyMomentum, capacity.FamilyTrend, capacity.FamilyDefinedRisk}
	contracts := make([]*capacity.Contract, 5)
	for i, family := range families {
		contracts[i] = capacityTestContract(t, family, family == capacity.FamilyDefinedRisk)
	}
	policy, _ := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	comparison, err := capacity.NewComparison(policy, contracts)
	if err != nil {
		t.Fatal(err)
	}
	return capacityRepositoryFixture{base, contracts, comparison, NewCapacityRepo(pool)}
}

func capacityTestContract(t *testing.T, family capacity.FamilyKind, available bool) *capacity.Contract {
	t.Helper()
	type canonical struct {
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
	reason, perUnit, maximumUnits := "source_capacity_not_observed", "0", 0
	if available {
		reason, perUnit, maximumUnits = "", "122", 10
	}
	value := canonical{capacity.ContractSchemaV1, "completed", family, uuid.NewSHA1(uuid.NameSpaceOID, []byte("eval/"+family)).String(), strings.Repeat("a", 64), uuid.NewSHA1(uuid.NameSpaceOID, []byte("source/"+family)).String(), strings.Repeat("b", 64), "2026-08-20T15:00:00.000000Z", "2026-08-21T15:00:00.000000Z", "0.01", available, reason, perUnit, maximumUnits}
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	id := economicid.DeterministicUUID("family-capacity-contract", capacity.ContractSchemaV1+"@sha256:"+digest)
	contract, err := capacity.ContractFromCanonical(id, digest, raw)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func TestCapacityRepositoryRoundTripEightWritersAndRestart(t *testing.T) {
	fixture := newCapacityRepositoryFixture(t)
	ctx := context.Background()
	for _, contract := range fixture.contracts {
		if _, err := fixture.repo.RegisterContract(ctx, contract); err != nil {
			t.Fatal(err)
		}
	}
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RecordComparison(ctx, fixture.comparison)
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	loaded, err := NewCapacityRepo(fixture.base.evaluation.experiment.strategy.pool).GetComparison(ctx, fixture.comparison.ID())
	if err != nil || loaded.Digest() != fixture.comparison.Digest() {
		t.Fatalf("capacity reload=%v/%v", loaded, err)
	}
	var contracts, comparisons, families, tiers int
	err = fixture.base.evaluation.experiment.strategy.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM capacity_v1_contracts),(SELECT count(*) FROM capacity_v1_comparisons),(SELECT count(*) FROM capacity_v1_families),(SELECT count(*) FROM capacity_v1_tiers)`).Scan(&contracts, &comparisons, &families, &tiers)
	if err != nil || contracts != 5 || comparisons != 1 || families != 5 || tiers != 30 {
		t.Fatalf("counts=%d/%d/%d/%d err=%v", contracts, comparisons, families, tiers, err)
	}
}

func TestCapacityRepositoryAtomicStagesForgeryAppendOnlyAndRollback(t *testing.T) {
	fixture := newCapacityRepositoryFixture(t)
	ctx := context.Background()
	pool := fixture.base.evaluation.experiment.strategy.pool
	for _, contract := range fixture.contracts {
		if _, err := fixture.repo.RegisterContract(ctx, contract); err != nil {
			t.Fatal(err)
		}
	}
	for _, failed := range []string{"capacity_comparison", "capacity_family", "capacity_tier"} {
		fixture.repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := fixture.repo.RecordComparison(ctx, fixture.comparison); err == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM capacity_v1_comparisons`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, count, err)
		}
	}
	fixture.repo.afterStage = nil
	if _, err := fixture.repo.RecordComparison(ctx, fixture.comparison); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE capacity_v1_tiers SET units=999 WHERE comparison_id=$1`, fixture.comparison.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE capacity_v1_tiers DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE capacity_v1_tiers SET canonical_tier=jsonb_set(canonical_tier,'{units}','999') WHERE comparison_id=$1 AND family_sequence=0 AND ordinal=0`, fixture.comparison.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE capacity_v1_tiers ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.GetComparison(ctx, fixture.comparison.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000087_capital_tier_candidate_comparison.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back") {
		t.Fatalf("rollback=%v", err)
	}
}

func TestCapacityMigrationEmptyRollbackAndReapply(t *testing.T) {
	base := newBenchmarkFixture(t)
	ctx := context.Background()
	for _, migration := range []string{"000083_quality_filtered_wheel_v1.up.sql", "000084_momentum_quality_baseline.up.sql", "000085_etf_time_series_trend.up.sql", "000086_defined_risk_options.up.sql", "000087_capital_tier_candidate_comparison.up.sql", "000087_capital_tier_candidate_comparison.down.sql", "000087_capital_tier_candidate_comparison.up.sql"} {
		if _, err := base.evaluation.experiment.strategy.pool.Exec(ctx, repositoryMigrationSQL(t, migration)); err != nil {
			t.Fatalf("%s: %v", migration, err)
		}
	}
}
