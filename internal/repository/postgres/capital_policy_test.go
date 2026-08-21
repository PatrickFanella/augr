package postgres

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

func TestCapitalPolicyRepoRegistersLoadsAndReplaysExactArtifact(t *testing.T) {
	ctx, pool := newCapitalPolicyIntegrationPool(t)
	repo := NewCapitalPolicyRepo(pool)
	artifact := newCapitalPolicyArtifact(t)
	persisted, err := repo.RegisterCapitalPolicy(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.RegisterCapitalPolicy(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetCapitalPolicyByVersion(ctx, artifact.Version)
	if err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]*capital.PolicyArtifact{"persisted": persisted, "replayed": replayed, "loaded": loaded} {
		if !capital.SamePolicyArtifactPayload(candidate, artifact) || candidate.CreatedAt != artifact.CreatedAt {
			t.Fatalf("%s artifact = %+v", name, candidate)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM capital_margin_policy_artifacts`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("artifact count = %d/%v", count, err)
	}
}

func TestCapitalPolicyRepoRejectsChangedArtifactIdentity(t *testing.T) {
	ctx, pool := newCapitalPolicyIntegrationPool(t)
	repo := NewCapitalPolicyRepo(pool)
	artifact := newCapitalPolicyArtifact(t)
	if _, err := repo.RegisterCapitalPolicy(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	changed := *artifact
	changed.CanonicalBytes = append(append([]byte(nil), artifact.CanonicalBytes...), ' ')
	if _, err := repo.RegisterCapitalPolicy(ctx, &changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed artifact error = %v", err)
	}
}

func TestCapitalPolicyRepoBindsReloadsAndReplaysEveryTierPlusStress(t *testing.T) {
	ctx, pool := newCapitalPolicyIntegrationPool(t)
	repo := NewCapitalPolicyRepo(pool)
	accountRepo := NewAccountRepo(pool)
	artifact := newCapitalPolicyArtifact(t)
	if _, err := repo.RegisterCapitalPolicy(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	policy, err := capital.PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	for index, tier := range policy.Tiers() {
		account := newCapitalPolicyAccount(t, domain.AccountEnvironmentPaperScored, tier, domain.MarginProfileRegT, decimal.NewFromInt(2), index)
		if err := accountRepo.Create(ctx, account); err != nil {
			t.Fatal(err)
		}
		binding, err := capital.NewBinding(*account, policy, tier, account.MarginProfile, capitalPolicyTestTime())
		if err != nil {
			t.Fatal(err)
		}
		persisted, err := repo.BindCapitalPolicy(ctx, binding)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := repo.BindCapitalPolicy(ctx, binding)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := repo.GetCapitalBinding(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		for name, candidate := range map[string]*capital.Binding{"persisted": persisted, "replayed": replayed, "loaded": loaded} {
			if !capital.SameBindingPayload(candidate, binding) || candidate.CreatedAt != binding.CreatedAt {
				t.Fatalf("%s binding = %+v", name, candidate)
			}
		}
	}
	stressTier := decimal.NewFromInt(5_000_000)
	stress := newCapitalPolicyAccount(t, domain.AccountEnvironmentPaperStress, stressTier, domain.MarginProfileStressUnlimited, decimal.Zero, 6)
	if err := accountRepo.Create(ctx, stress); err != nil {
		t.Fatal(err)
	}
	stressBinding, err := capital.NewBinding(*stress, policy, stressTier, stress.MarginProfile, capitalPolicyTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BindCapitalPolicy(ctx, stressBinding); err != nil {
		t.Fatal(err)
	}
	var bindings, accounts, openingFlows int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM account_capital_policy_bindings),
		(SELECT COUNT(*) FROM accounts WHERE created_by = 'capital-policy-test'),
		(SELECT COUNT(*) FROM capital_flows f JOIN accounts a ON a.id=f.account_id WHERE a.created_by='capital-policy-test' AND f.source='account_opening')`).Scan(
		&bindings, &accounts, &openingFlows,
	); err != nil {
		t.Fatal(err)
	}
	if bindings != 7 || accounts != 7 || openingFlows != 7 {
		t.Fatalf("retained graph counts = bindings:%d accounts:%d openings:%d", bindings, accounts, openingFlows)
	}
}

func TestCapitalPolicyRepoConcurrentArtifactAndBindingReplayConverges(t *testing.T) {
	ctx, pool := newCapitalPolicyIntegrationPool(t)
	repo := NewCapitalPolicyRepo(pool)
	artifact := newCapitalPolicyArtifact(t)
	policy, err := capital.PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	account := newCapitalPolicyAccount(t, domain.AccountEnvironmentPaperScored, decimal.NewFromInt(100_000), domain.MarginProfileRegT, decimal.NewFromInt(2), 0)
	if err := NewAccountRepo(pool).Create(ctx, account); err != nil {
		t.Fatal(err)
	}
	binding, err := capital.NewBinding(*account, policy, account.StartingCapital, account.MarginProfile, capitalPolicyTestTime())
	if err != nil {
		t.Fatal(err)
	}
	const writers = 8
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wait sync.WaitGroup
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			if _, err := repo.RegisterCapitalPolicy(ctx, artifact); err != nil {
				errs <- err
				return
			}
			loaded, err := repo.BindCapitalPolicy(ctx, binding)
			if err != nil {
				errs <- err
				return
			}
			if !capital.SameBindingPayload(loaded, binding) {
				errs <- errors.New("binding payload changed")
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var artifacts, bindings int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM capital_margin_policy_artifacts),
		(SELECT COUNT(*) FROM account_capital_policy_bindings)`).Scan(&artifacts, &bindings); err != nil {
		t.Fatal(err)
	}
	if artifacts != 1 || bindings != 1 {
		t.Fatalf("concurrent counts = %d/%d", artifacts, bindings)
	}
}

func TestCapitalPolicyDatabaseRejectsMutationAndChangedBindingReplay(t *testing.T) {
	ctx, pool := newCapitalPolicyIntegrationPool(t)
	repo := NewCapitalPolicyRepo(pool)
	artifact := newCapitalPolicyArtifact(t)
	if _, err := repo.RegisterCapitalPolicy(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	policy, _ := capital.PolicyFromArtifact(*artifact)
	account := newCapitalPolicyAccount(t, domain.AccountEnvironmentPaperScored, decimal.NewFromInt(100_000), domain.MarginProfileRegT, decimal.NewFromInt(2), 0)
	if err := NewAccountRepo(pool).Create(ctx, account); err != nil {
		t.Fatal(err)
	}
	binding, err := capital.NewBinding(*account, policy, account.StartingCapital, account.MarginProfile, capitalPolicyTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BindCapitalPolicy(ctx, binding); err != nil {
		t.Fatal(err)
	}
	changed := *binding
	changed.Tier = decimal.NewFromInt(25_000)
	if _, err := repo.BindCapitalPolicy(ctx, &changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed binding error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE account_capital_policy_bindings SET tier = 25000 WHERE id = $1`, binding.ID); err == nil {
		t.Fatal("binding update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM capital_margin_policy_artifacts WHERE id = $1`, artifact.ID); err == nil {
		t.Fatal("artifact delete unexpectedly succeeded")
	}
}

func TestCapitalGoldenReplayPersistsReloadsAndReplaysWithoutDuplication(t *testing.T) {
	ctx, pool := newCapitalPolicyIntegrationPool(t)
	fixture := newPostgresSimulationVenueFixtureWithPool(t, ctx, pool, lifecycle.TimeInForceDay, 6*time.Hour)
	capitalRepo := NewCapitalPolicyRepo(pool)
	artifact := newCapitalPolicyArtifact(t)
	if _, err := capitalRepo.RegisterCapitalPolicy(ctx, artifact); err != nil {
		t.Fatal(err)
	}
	policy, err := capital.PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := capital.NewBinding(
		*fixture.execution.account, policy, fixture.execution.account.StartingCapital,
		fixture.execution.account.MarginProfile, capitalPolicyTestTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capitalRepo.BindCapitalPolicy(ctx, binding); err != nil {
		t.Fatal(err)
	}

	snapshot := fixture.snapshot(t, "capital-golden-restart", fixture.routed.Order.RoutedAt.Add(time.Second),
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.18"), Size: decimal.NewFromInt(8)}},
		[]marketdata.DepthLevelInput{{Price: decimal.RequireFromString("10.20"), Size: decimal.NewFromInt(8)}},
	)
	result, err := fixture.venue.Evaluate(fixture.request(fixture.routed, snapshot, *snapshot.AvailableAt))
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := simulation.PersistResult(ctx, fixture.persistence(), fixture.accountID(), result)
	if err != nil {
		t.Fatal(err)
	}
	before, err := simulation.NewOutcome(simulation.OutcomeInput{
		Account: *fixture.execution.account, VenueContract: *fixture.execution.contract,
		Aggregate: persisted, Fills: result.Fills,
	})
	if err != nil {
		t.Fatal(err)
	}

	freshCapital := NewCapitalPolicyRepo(pool)
	reloadedArtifact, err := freshCapital.GetCapitalPolicyByVersion(ctx, artifact.Version)
	if err != nil {
		t.Fatal(err)
	}
	reloadedPolicy, err := capital.PolicyFromArtifact(*reloadedArtifact)
	if err != nil {
		t.Fatal(err)
	}
	reloadedBinding, err := freshCapital.GetCapitalBinding(ctx, fixture.accountID())
	if err != nil {
		t.Fatal(err)
	}
	if reloadedPolicy.Version() != policy.Version() || !capital.SameBindingPayload(reloadedBinding, binding) {
		t.Fatalf("reloaded capital context = %q/%+v", reloadedPolicy.Version(), reloadedBinding)
	}
	freshLifecycle := NewExecutionLifecycleRepo(pool)
	reloadedLifecycle, err := freshLifecycle.GetExecutionLifecycle(ctx, fixture.accountID(), persisted.Intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedLifecycle.State != lifecycle.StateFilled || len(reloadedLifecycle.Fills) != 1 {
		t.Fatalf("reloaded lifecycle = state:%s fills:%d", reloadedLifecycle.State, len(reloadedLifecycle.Fills))
	}
	after, err := simulation.NewOutcome(simulation.OutcomeInput{
		Account: *fixture.execution.account, VenueContract: *fixture.execution.contract,
		Aggregate: reloadedLifecycle, Fills: result.Fills,
	})
	if err != nil {
		t.Fatal(err)
	}
	if before.Hash() != after.Hash() || !bytes.Equal(before.CanonicalBytes(), after.CanonicalBytes()) {
		t.Fatalf("restart outcome changed = %s/%s", before.Hash(), after.Hash())
	}
	if replayed, err := simulation.PersistResult(ctx, &postgresSimulationPersistence{
		ledger: NewLedgerRepo(pool), lifecycle: freshLifecycle,
	}, fixture.accountID(), result); err != nil || replayed.State != lifecycle.StateFilled {
		t.Fatalf("restart replay = %+v/%v", replayed, err)
	}

	var artifacts, bindings, orders, fills, normalizations, transactions int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM capital_margin_policy_artifacts),
		(SELECT COUNT(*) FROM account_capital_policy_bindings),
		(SELECT COUNT(*) FROM execution_orders WHERE intent_id=$1),
		(SELECT COUNT(*) FROM execution_fills WHERE intent_id=$1),
		(SELECT COUNT(*) FROM economic_event_normalizations n JOIN economic_source_events e ON e.id=n.source_event_id WHERE e.account_id=$2 AND n.reference_type='execution_fill'),
		(SELECT COUNT(*) FROM ledger_transactions WHERE account_id=$2 AND reference_type='execution_fill')`,
		persisted.Intent.ID, fixture.accountID(),
	).Scan(&artifacts, &bindings, &orders, &fills, &normalizations, &transactions); err != nil {
		t.Fatal(err)
	}
	if artifacts != 1 || bindings != 1 || orders != 1 || fills != 1 || normalizations != 1 || transactions != 1 {
		t.Fatalf("restart graph = artifacts:%d bindings:%d orders:%d fills:%d normalizations:%d transactions:%d",
			artifacts, bindings, orders, fills, normalizations, transactions)
	}
}

func newCapitalPolicyIntegrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool := newExecutionLifecycleIntegrationPool(t, ctx)
	for _, migrationName := range []string{
		"000072_simulation_policy_artifacts.up.sql",
		"000073_venue_adapter_observations.up.sql",
		"000074_capital_margin_profiles.up.sql",
	} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migrationName)); err != nil {
			t.Fatalf("apply %s: %v", migrationName, err)
		}
	}
	return ctx, pool
}

func newCapitalPolicyArtifact(t *testing.T) *capital.PolicyArtifact {
	t.Helper()
	policy, err := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(capitalPolicyTestTime())
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func newCapitalPolicyAccount(
	t *testing.T,
	environment domain.AccountEnvironment,
	tier decimal.Decimal,
	profile domain.MarginProfile,
	multiplier decimal.Decimal,
	ordinal int,
) *domain.Account {
	t.Helper()
	account, err := domain.NewAccount(domain.AccountInput{
		Name: "capital policy " + tier.String(), Environment: environment, Venue: "internal",
		BaseCurrency: "USD", StorageNamespace: string(environment) + "/capital-policy-" + uuid.NewString(),
		StartingCapital: tier, BuyingPowerMultiplier: multiplier, MarginProfile: profile,
		CreatedBy: "capital-policy-test", CreationMetadata: []byte(`{"ordinal":` + decimal.NewFromInt(int64(ordinal)).String() + `}`),
		CreatedAt: capitalPolicyTestTime().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func capitalPolicyTestTime() time.Time {
	return time.Date(2026, 8, 20, 16, 0, 0, 123456000, time.UTC)
}
