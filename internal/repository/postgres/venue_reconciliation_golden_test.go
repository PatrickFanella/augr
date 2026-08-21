package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/alpaca"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/venuerecon"
)

type venueReconciliationGoldenFixture struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	provider venue.Provider
	account  *domain.Account
	contract *instrument.VenueContract
	baseTime time.Time
	request  venuerecon.LocalSnapshotRequest
	policy   *venuerecon.Policy
	artifact *venuerecon.PolicyArtifact
	local    *venuerecon.LocalSnapshot
	stable   *venuerecon.StableProviderSnapshot
	run      *venuerecon.Run
	alpaca   *alpacaVenueAdapterFixture
	filled   *lifecycle.Aggregate
}

func TestVenueReconciliationGoldenAlpacaAndKalshi(t *testing.T) {
	for _, provider := range []venue.Provider{venue.ProviderAlpaca, venue.ProviderKalshi} {
		t.Run(string(provider), func(t *testing.T) {
			fixture := newVenueReconciliationGoldenFixture(t, provider)
			if !fixture.run.Clean || len(fixture.run.Incidents) != 0 {
				t.Fatalf("golden run = clean:%v incidents:%+v", fixture.run.Clean, fixture.run.Incidents)
			}
			persistVenueReconciliationGolden(t, fixture, NewVenueReconciliationRepo(fixture.pool))
			loaded, err := NewVenueReconciliationRepo(fixture.pool).GetVenueReconciliationRun(fixture.ctx, fixture.run.ID)
			if err != nil || loaded.ID != fixture.run.ID || loaded.SHA256 != fixture.run.SHA256 {
				t.Fatalf("golden reload = %+v, %v", loaded, err)
			}
			freshLocal, err := venuerecon.NewLocalSource(NewVenueReconciliationLocalReader(fixture.pool)).Capture(fixture.ctx, fixture.request)
			if err != nil || freshLocal.ID() != fixture.local.ID() {
				t.Fatalf("fresh local snapshot = %v/%v", snapshotID(freshLocal), err)
			}
			var observations, fills, normalizations, transactions, checkpoints int
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
				(SELECT count(*) FROM venue_observations WHERE account_id=$1),
				(SELECT count(*) FROM execution_fills WHERE account_id=$1),
				(SELECT count(*) FROM economic_event_normalizations n JOIN execution_fills f ON f.normalization_id=n.id WHERE f.account_id=$1),
				(SELECT count(*) FROM ledger_transactions WHERE account_id=$1),
				(SELECT count(*) FROM projection_checkpoints WHERE account_id=$1)`, fixture.account.ID).
				Scan(&observations, &fills, &normalizations, &transactions, &checkpoints); err != nil {
				t.Fatal(err)
			}
			if observations < 1 || fills != 1 || normalizations != 1 || transactions != 2 || checkpoints != 1 {
				t.Fatalf("golden source graph=%d/%d/%d/%d/%d", observations, fills, normalizations, transactions, checkpoints)
			}
		})
	}
}

func TestVenueReconciliationGoldenOrphanObservationRemainsNonClean(t *testing.T) {
	fixture := newVenueReconciliationGoldenFixture(t, venue.ProviderAlpaca)
	var existingID uuid.UUID
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT id FROM venue_observations
		WHERE account_id=$1 AND kind='fill' ORDER BY source_at,id LIMIT 1`, fixture.account.ID).Scan(&existingID); err != nil {
		t.Fatal(err)
	}
	existing, err := NewVenueAdapterRepo(fixture.pool).GetVenueObservationByID(fixture.ctx, existingID)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := venue.NewObservation(venue.ObservationInput{
		AccountID: existing.AccountID, IntentID: existing.IntentID, OrderID: existing.OrderID,
		BindingID: existing.BindingID, VenueContractID: existing.VenueContractID,
		Provider: existing.Provider, Venue: existing.Venue, PolicyVersion: existing.PolicyVersion,
		Kind: existing.Kind, ProviderState: existing.ProviderState, MappedOutcome: existing.MappedOutcome,
		ExternalOrderID: existing.ExternalOrderID, ClientOrderID: existing.ClientOrderID,
		ProviderContractID: existing.ProviderContractID, CanonicalOutcome: existing.CanonicalOutcome,
		ProviderBookSide: existing.ProviderBookSide, ProviderAction: existing.ProviderAction,
		ProviderPrice: existing.ProviderPrice, IdentityKind: existing.IdentityKind,
		SourceNamespace: existing.SourceNamespace, SourceEventID: existing.SourceEventID + "-orphan",
		SourceRevision: existing.SourceRevision, SourceAt: existing.SourceAt.Add(time.Second),
		ReceivedAt: existing.ReceivedAt.Add(time.Second), RawPayload: existing.RawPayload,
		CreatedAt: existing.CreatedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVenueAdapterRepo(fixture.pool).RecordVenueObservation(fixture.ctx, orphan); err != nil {
		t.Fatal(err)
	}

	local, err := venuerecon.NewLocalSource(NewVenueReconciliationLocalReader(fixture.pool)).Capture(fixture.ctx, fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if issues := local.Issues(); len(issues) != 1 || issues[0].Reason != venuerecon.ReasonLocalFillIncomplete ||
		issues[0].EvidenceID != orphan.ID.String() || issues[0].LedgerTransactionID != "" {
		t.Fatalf("orphan local issues = %+v", issues)
	}
	run, err := venuerecon.Compare(venuerecon.CompareInput{
		Policy: fixture.policy, Provider: &venuerecon.SnapshotAdmission{Snapshot: fixture.stable},
		Local: local, EquityBasisEquivalent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Clean || len(run.Incidents) != 1 || run.Incidents[0].Reason != venuerecon.ReasonLocalFillIncomplete {
		t.Fatalf("orphan reconciliation run = clean:%v incidents:%+v", run.Clean, run.Incidents)
	}
}

func TestVenueReconciliationRejectsForgedChildScope(t *testing.T) {
	fixture := newVenueReconciliationGoldenFixture(t, venue.ProviderAlpaca)
	persistVenueReconciliationGolden(t, fixture, NewVenueReconciliationRepo(fixture.pool))
	tx, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(fixture.ctx) }()
	if _, err := tx.Exec(fixture.ctx, `INSERT INTO venue_provider_snapshot_pages(
		snapshot_id,account_external_id,provider,namespace,horizon_start,horizon_end,
		sequence,cursor,next_cursor,terminal,sha256,raw_bytes
	) SELECT id,account_external_id || '-forged',provider,namespace,horizon_start,horizon_end,
		99,'forged','',true,encode(digest('{}'::BYTEA,'sha256'),'hex'),'{}'::BYTEA
		FROM venue_provider_snapshots WHERE id=$1`, fixture.stable.ID()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(fixture.ctx); err == nil || !strings.Contains(err.Error(), "graph counts do not reconstruct") {
		t.Fatalf("forged child scope commit error = %v", err)
	}
}

func TestVenueReconciliationGoldenIndependentPerturbations(t *testing.T) {
	fixture := newVenueReconciliationGoldenFixture(t, venue.ProviderAlpaca)
	var baselineTransactions int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM ledger_transactions WHERE account_id=$1`, fixture.account.ID).Scan(&baselineTransactions); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]struct {
		mutate func(*venuerecon.CaptureInput)
		reason venuerecon.ReasonCode
	}{
		"cash": {func(input *venuerecon.CaptureInput) {
			input.Cash = decimal.RequireFromString(input.Cash).Add(decimal.RequireFromString("0.01")).String()
		}, venuerecon.ReasonCashMismatch},
		"position": {func(input *venuerecon.CaptureInput) {
			input.Positions[0].Quantity = decimal.RequireFromString(input.Positions[0].Quantity).Add(decimal.NewFromInt(1)).String()
		}, venuerecon.ReasonPositionQuantityMismatch},
		"fill": {func(input *venuerecon.CaptureInput) {
			input.Fills[0].Price = decimal.RequireFromString(input.Fills[0].Price).Add(decimal.RequireFromString("0.01")).String()
		}, venuerecon.ReasonFillPriceMismatch},
	} {
		t.Run(name, func(t *testing.T) {
			stable := buildGoldenProviderSnapshot(t, fixture, mutate.mutate)
			run, err := venuerecon.Compare(venuerecon.CompareInput{
				Policy: fixture.policy, Provider: &venuerecon.SnapshotAdmission{Snapshot: stable}, Local: fixture.local, EquityBasisEquivalent: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if run.Clean || len(run.Incidents) != 1 || run.Incidents[0].Reason != mutate.reason || run.Incidents[0].Severity != venuerecon.SeverityCritical {
				t.Fatalf("%s perturbation incidents = %+v", name, run.Incidents)
			}
			var transactions int
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM ledger_transactions WHERE account_id=$1`, fixture.account.ID).Scan(&transactions); err != nil || transactions != baselineTransactions {
				t.Fatalf("%s perturbation changed ledger count=%d err=%v", name, transactions, err)
			}
		})
	}
}

func TestVenueReconciliationRestartConvergesAfterEveryPersistedStage(t *testing.T) {
	for stage := range 4 {
		t.Run(fmt.Sprintf("after-stage-%d", stage+1), func(t *testing.T) {
			fixture := newVenueReconciliationGoldenFixture(t, venue.ProviderAlpaca)
			first := NewVenueReconciliationRepo(fixture.pool)
			if stage >= 0 {
				if _, err := first.RegisterVenueReconciliationPolicy(fixture.ctx, fixture.artifact); err != nil {
					t.Fatal(err)
				}
			}
			if stage >= 1 {
				if err := first.RecordVenueProviderSnapshot(fixture.ctx, fixture.stable, fixture.artifact.CreatedAt); err != nil {
					t.Fatal(err)
				}
			}
			if stage >= 2 {
				if err := first.RecordVenueLocalSnapshot(fixture.ctx, fixture.local, fixture.artifact.CreatedAt); err != nil {
					t.Fatal(err)
				}
			}
			if stage >= 3 {
				if _, err := first.RecordVenueReconciliationRun(fixture.ctx, fixture.run, fixture.artifact.CreatedAt); err != nil {
					t.Fatal(err)
				}
			}
			fresh := NewVenueReconciliationRepo(fixture.pool)
			persistVenueReconciliationGolden(t, fixture, fresh)
			loaded, err := fresh.GetVenueReconciliationRun(fixture.ctx, fixture.run.ID)
			if err != nil || loaded.ID != fixture.run.ID || loaded.SHA256 != fixture.run.SHA256 {
				t.Fatalf("restart run = %+v, %v", loaded, err)
			}
			var policies, providers, locals, runs, incidents int
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
				(SELECT count(*) FROM venue_reconciliation_policy_artifacts),
				(SELECT count(*) FROM venue_provider_snapshots),(SELECT count(*) FROM venue_local_snapshots),
				(SELECT count(*) FROM venue_reconciliation_runs),(SELECT count(*) FROM venue_reconciliation_incidents)`).
				Scan(&policies, &providers, &locals, &runs, &incidents); err != nil {
				t.Fatal(err)
			}
			if policies != 1 || providers != 1 || locals != 1 || runs != 1 || incidents != 0 {
				t.Fatalf("restart counts=%d/%d/%d/%d/%d", policies, providers, locals, runs, incidents)
			}
		})
	}
}

func TestVenueReconciliationGoldenCorrectionBustAndUnstableRemainNonClean(t *testing.T) {
	for _, activityType := range []string{"trade_correct", "trade_bust"} {
		t.Run(activityType, func(t *testing.T) {
			fixture := newVenueReconciliationGoldenFixture(t, venue.ProviderAlpaca)
			original := fixture.local.Fills()[0]
			activity := alpaca.FillActivity{
				ID: "revision-" + activityType + "-" + fixture.account.ID.String(), ActivityType: activityType,
				OrderID: original.ExternalOrderID, ClientOrderID: original.ClientOrderID, Quantity: original.Quantity,
				Price: original.Price, Side: string(original.Side), Symbol: fixture.contract.ContractID,
				TransactionTime:    fixture.baseTime.Add(6 * time.Second).Format(time.RFC3339Nano),
				CumulativeQuantity: original.Quantity, LeavesQuantity: "0", OriginalActivityID: original.SourceID,
			}
			raw, err := json.Marshal(activity)
			if err != nil {
				t.Fatal(err)
			}
			result, err := alpaca.PlanFillActivityResult(fixture.alpaca.adapterContext(fixture.filled), []alpaca.FillActivityFact{{Activity: activity, RawPayload: raw}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := venue.PersistResult(fixture.ctx, newPostgresVenueResultStore(fixture.pool), fixture.account.ID, result); err != nil {
				t.Fatal(err)
			}
			local, err := venuerecon.NewLocalSource(NewVenueReconciliationLocalReader(fixture.pool)).Capture(fixture.ctx, fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			if len(local.Fills()) != 2 || local.Fills()[0].FillID != local.Fills()[1].FillID {
				t.Fatalf("revision local lineage = %+v", local.Fills())
			}
			fixture.local = local
			stable := buildGoldenProviderSnapshot(t, fixture, nil)
			run, err := venuerecon.Compare(venuerecon.CompareInput{
				Policy: fixture.policy, Provider: &venuerecon.SnapshotAdmission{Snapshot: stable}, Local: local, EquityBasisEquivalent: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := venuerecon.ReasonCorrectionPending
			if activityType == "trade_bust" {
				want = venuerecon.ReasonBustPending
			}
			if run.Clean || len(run.Incidents) != 1 || run.Incidents[0].Reason != want {
				t.Fatalf("revision run incidents = %+v", run.Incidents)
			}
			fixture.stable, fixture.run = stable, run
			persistVenueReconciliationGolden(t, fixture, NewVenueReconciliationRepo(fixture.pool))
			var fills, distinctFills, transactions int
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
				(SELECT count(*) FROM venue_local_snapshot_fills),(SELECT count(DISTINCT fill_id) FROM venue_local_snapshot_fills),
				(SELECT count(*) FROM ledger_transactions WHERE account_id=$1)`, fixture.account.ID).Scan(&fills, &distinctFills, &transactions); err != nil {
				t.Fatal(err)
			}
			if fills != 2 || distinctFills != 1 || transactions != 2 {
				t.Fatalf("revision persistence=%d/%d transactions=%d", fills, distinctFills, transactions)
			}
		})
	}

	t.Run("unstable", func(t *testing.T) {
		fixture := newVenueReconciliationGoldenFixture(t, venue.ProviderAlpaca)
		changed := buildGoldenProviderSnapshot(t, fixture, func(input *venuerecon.CaptureInput) {
			input.Cash = decimal.RequireFromString(input.Cash).Add(decimal.RequireFromString("0.01")).String()
		})
		first, _ := fixture.stable.Captures()
		second, _ := changed.Captures()
		admission, err := venuerecon.AdmitStableProviderSnapshot(first, second)
		if err != nil || admission.Snapshot != nil || admission.Reason != venuerecon.ReasonSnapshotUnstable {
			t.Fatalf("unstable admission = %+v, %v", admission, err)
		}
		run, err := venuerecon.Compare(venuerecon.CompareInput{Policy: fixture.policy, Provider: admission, Local: fixture.local, EquityBasisEquivalent: true})
		if err != nil || run.Clean || len(run.Incidents) != 1 || run.Incidents[0].Reason != venuerecon.ReasonSnapshotUnstable {
			t.Fatalf("unstable run = %+v, %v", run, err)
		}
	})
}

func TestVenueReconciliationPersistentRehearsal(t *testing.T) {
	databaseURL := os.Getenv("AUGR_VENUE_RECONCILIATION_REHEARSAL_DB_URL")
	if databaseURL == "" {
		t.Skip("set AUGR_VENUE_RECONCILIATION_REHEARSAL_DB_URL only for an explicitly disposable schema-75 database")
	}
	if os.Getenv("DB_URL") != databaseURL || os.Getenv("AUGR_RETAIN_PROJECTION_SCHEMA") == "" {
		t.Fatal("DB_URL must equal the rehearsal URL and AUGR_RETAIN_PROJECTION_SCHEMA must name the retained schema")
	}
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	if os.Getenv("AUGR_RETAIN_PROJECTION_SCHEMA_PREMIGRATED") != "true" {
		applyVenueReconciliationMigrations(t, ctx, pools.owner)
	}
	repo := NewVenueReconciliationRepo(pools.owner)

	alpacaFixture := newVenueReconciliationGoldenFixtureWithPools(t, venue.ProviderAlpaca, pools)
	persistVenueReconciliationGolden(t, alpacaFixture, repo)
	kalshiFixture := newVenueReconciliationGoldenFixtureWithPools(t, venue.ProviderKalshi, pools)
	persistVenueReconciliationGolden(t, kalshiFixture, repo)
	driftSnapshot := buildGoldenProviderSnapshot(t, alpacaFixture, func(input *venuerecon.CaptureInput) {
		input.Cash = decimal.RequireFromString(input.Cash).Add(decimal.RequireFromString("0.01")).String()
	})
	driftRun, err := venuerecon.Compare(venuerecon.CompareInput{
		Policy: alpacaFixture.policy, Provider: &venuerecon.SnapshotAdmission{Snapshot: driftSnapshot},
		Local: alpacaFixture.local, EquityBasisEquivalent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if driftRun.Clean || len(driftRun.Incidents) != 1 || driftRun.Incidents[0].Reason != venuerecon.ReasonCashMismatch {
		t.Fatalf("retained drift run = %+v", driftRun)
	}
	driftFixture := alpacaFixture
	driftFixture.stable, driftFixture.run = driftSnapshot, driftRun
	persistVenueReconciliationGolden(t, driftFixture, repo)

	for _, fixture := range []venueReconciliationGoldenFixture{alpacaFixture, kalshiFixture, driftFixture} {
		freshLocal, captureErr := venuerecon.NewLocalSource(NewVenueReconciliationLocalReader(pools.owner)).Capture(ctx, fixture.request)
		if captureErr != nil || freshLocal.ID() != fixture.local.ID() {
			t.Fatalf("retained local recompute = %v/%v", snapshotID(freshLocal), captureErr)
		}
		freshProvider := buildGoldenProviderSnapshot(t, fixture, func(input *venuerecon.CaptureInput) {
			if fixture.run.ID == driftRun.ID {
				input.Cash = decimal.RequireFromString(input.Cash).Add(decimal.RequireFromString("0.01")).String()
			}
		})
		freshRun, compareErr := venuerecon.Compare(venuerecon.CompareInput{
			Policy: fixture.policy, Provider: &venuerecon.SnapshotAdmission{Snapshot: freshProvider},
			Local: freshLocal, EquityBasisEquivalent: true,
		})
		if compareErr != nil || freshProvider.ID() != fixture.stable.ID() || freshRun.ID != fixture.run.ID || freshRun.SHA256 != fixture.run.SHA256 {
			t.Fatalf("retained recompute = provider:%v run:%v hash:%s err:%v", freshProvider.ID(), freshRun.ID, freshRun.SHA256, compareErr)
		}
		loaded, loadErr := repo.GetVenueReconciliationRun(ctx, fixture.run.ID)
		if loadErr != nil || loaded.ID != freshRun.ID || loaded.SHA256 != freshRun.SHA256 {
			t.Fatalf("retained reload = %+v, %v", loaded, loadErr)
		}
	}

	var policies, providers, locals, runs, incidents int
	if err := pools.owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM venue_reconciliation_policy_artifacts),
		(SELECT count(*) FROM venue_provider_snapshots),(SELECT count(*) FROM venue_local_snapshots),
		(SELECT count(*) FROM venue_reconciliation_runs),(SELECT count(*) FROM venue_reconciliation_incidents)`).
		Scan(&policies, &providers, &locals, &runs, &incidents); err != nil {
		t.Fatal(err)
	}
	if policies != 1 || providers != 3 || locals != 2 || runs != 3 || incidents != 1 {
		t.Fatalf("retained graph counts=%d/%d/%d/%d/%d", policies, providers, locals, runs, incidents)
	}
	if _, err := pools.owner.Exec(ctx, repositoryMigrationSQL(t, "000075_venue_reconciliation.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 75") {
		t.Fatalf("nonempty schema-75 rollback error = %v", err)
	}
}

func newVenueReconciliationGoldenFixture(t *testing.T, provider venue.Provider) venueReconciliationGoldenFixture {
	t.Helper()
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	applyVenueReconciliationMigrations(t, ctx, pools.owner)
	return newVenueReconciliationGoldenFixtureWithPools(t, provider, pools)
}

func applyVenueReconciliationMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, migration := range []string{
		"000070_accounting_dual_run.up.sql", "000071_common_execution_lifecycle.up.sql",
		"000072_simulation_policy_artifacts.up.sql", "000073_venue_adapter_observations.up.sql",
		"000074_capital_margin_profiles.up.sql", "000075_venue_reconciliation.up.sql",
	} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migration)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
}

func newVenueReconciliationGoldenFixtureWithPools(
	t *testing.T,
	provider venue.Provider,
	pools projectionIntegrationPools,
) venueReconciliationGoldenFixture {
	t.Helper()
	ctx := context.Background()
	var account *domain.Account
	var contract *instrument.VenueContract
	var baseTime time.Time
	var alpacaAdapter *alpacaVenueAdapterFixture
	var filledAggregate *lifecycle.Aggregate
	if provider == venue.ProviderAlpaca {
		adapter := newAlpacaVenueAdapterFixtureWithPool(t, ctx, pools.owner, "reconciliation-golden")
		result := adapter.planFills(t, []alpaca.FillActivityFact{adapter.fillFact(t, "golden-fill", "8", "10.25", "8", "0")})
		filled, persistErr := venue.PersistResult(ctx, newPostgresVenueResultStore(pools.owner), adapter.account.ID, result)
		if persistErr != nil {
			t.Fatal(persistErr)
		}
		if filled == nil {
			t.Fatal("persisted Alpaca fill returned a nil aggregate")
		}
		account, contract, baseTime = adapter.account, adapter.contract, adapter.baseTime
		alpacaAdapter, filledAggregate = &adapter, filled
	} else {
		adapter := newVenueAdapterRepositoryFixtureWithPool(t, ctx, pools.owner, "reconciliation-golden")
		policy, err := venue.ReviewedPolicy(venue.ProviderKalshi)
		if err != nil {
			t.Fatal(err)
		}
		adapterContext := kalshi.CommonLifecycleContext{
			Policy: policy, Aggregate: adapter.aggregate, Account: adapter.base.account, Instrument: adapter.base.instrument,
			VenueContract: adapter.base.contract, Route: kalshi.CommonRouteFacts{}, ReceivedAt: adapter.base.baseTime.Add(20 * time.Second),
		}
		fact := kalshiPostgresFillFact(t, adapterContext, "golden-order-"+adapter.aggregate.Order.ID.String(), "golden-fill-"+adapter.aggregate.Order.ID.String(), adapter.aggregate.Order.Quantity, adapter.base.baseTime.Add(10*time.Second))
		result, err := kalshi.PlanFillResults(adapterContext, []kalshi.CommonFillFact{fact})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := venue.PersistResult(ctx, newPostgresVenueResultStore(pools.owner), adapter.base.account.ID, result); err != nil {
			t.Fatal(err)
		}
		account, contract, baseTime = adapter.base.account, adapter.base.contract, adapter.base.baseTime
	}
	markAt := baseTime.Add(30 * time.Second)
	mark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: contract.InstrumentID, Price: decimal.RequireFromString("10.25"), PriceCurrency: "USD",
		Source: "reconciliation-golden", SourceNamespace: "marks/reconciliation-golden", SourceObservationID: "mark-" + account.ID.String(),
		SourceRevision: "v1", EffectiveAt: markAt, ObservedAt: markAt.Add(time.Second), Metadata: json.RawMessage(`{"fixture":"reconciliation-golden"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	projectionRepo := NewProjectionRepo(pools.writer, pools.attestor)
	if _, err := projectionRepo.RecordMarkObservation(ctx, mark); err != nil {
		t.Fatal(err)
	}
	asOf := baseTime.Add(2 * time.Minute)
	projection, err := projectionRepo.RebuildPortfolioProjection(ctx, ledger.ProjectionRequest{
		AccountID: account.ID, AsOf: asOf, MarkSource: "reconciliation-golden", MarkNamespace: "marks/reconciliation-golden", MaxMarkAge: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := venuerecon.LocalSnapshotRequest{
		AccountID: account.ID, Provider: provider, Namespace: reconciliationNamespace(provider),
		HorizonStart: baseTime.Add(-time.Minute), HorizonEnd: asOf, CheckpointID: projection.CheckpointID,
	}
	local, err := venuerecon.NewLocalSource(NewVenueReconciliationLocalReader(pools.owner)).Capture(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := venuerecon.NewPolicy(venuerecon.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(asOf.Add(5 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	fixture := venueReconciliationGoldenFixture{
		ctx: ctx, pool: pools.owner, provider: provider, account: account, contract: contract, baseTime: baseTime,
		request: request, policy: policy, artifact: artifact, local: local, alpaca: alpacaAdapter, filled: filledAggregate,
	}
	fixture.stable = buildGoldenProviderSnapshot(t, fixture, nil)
	fixture.run, err = venuerecon.Compare(venuerecon.CompareInput{
		Policy: policy, Provider: &venuerecon.SnapshotAdmission{Snapshot: fixture.stable}, Local: local, EquityBasisEquivalent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func buildGoldenProviderSnapshot(t *testing.T, fixture venueReconciliationGoldenFixture, mutate func(*venuerecon.CaptureInput)) *venuerecon.StableProviderSnapshot {
	t.Helper()
	input := venuerecon.CaptureInput{
		Provider: fixture.provider, Namespace: reconciliationNamespace(fixture.provider), AccountID: fixture.account.ID.String(), Currency: "USD",
		HorizonStart: fixture.request.HorizonStart, HorizonEnd: fixture.request.HorizonEnd, ProviderAsOf: fixture.request.HorizonEnd,
		CaptureStart: fixture.request.HorizonEnd.Add(time.Minute), CaptureEnd: fixture.request.HorizonEnd.Add(2 * time.Minute),
		Cash: fixture.local.Cash().String(), Equity: fixture.local.Equity().String(),
	}
	for _, position := range fixture.local.Positions() {
		input.Positions = append(input.Positions, venuerecon.PositionInput{
			ContractID: fixture.contract.ContractID, Quantity: position.Quantity, Currency: "USD", SourceAt: fixture.request.HorizonEnd,
		})
	}
	for _, fill := range fixture.local.Fills() {
		input.Fills = append(input.Fills, venuerecon.FillInput{
			SourceID: fill.SourceID, OriginalSourceID: fill.OriginalSourceID, ObservationClass: fill.ObservationClass,
			ObservationDiscriminator: fill.ObservationDiscriminator, ExternalOrderID: fill.ExternalOrderID, ClientOrderID: fill.ClientOrderID,
			ContractID: fixture.contract.ContractID, Side: fill.Side, Quantity: fill.Quantity, Price: fill.Price, Fee: fill.Fee,
			Currency: fill.Currency, SourceRevision: fill.SourceRevision, SourceAt: mustGoldenTime(t, fill.SourceAt),
		})
	}
	if mutate != nil {
		mutate(&input)
	}
	setGoldenProviderPages(t, &input)
	normalize := venuerecon.NormalizeAlpacaCapture
	if fixture.provider == venue.ProviderKalshi {
		normalize = venuerecon.NormalizeKalshiCapture
	}
	first, err := normalize(fixture.ctx, input, venueReconResolver{contract: *fixture.contract})
	if err != nil {
		t.Fatal(err)
	}
	input.CaptureStart = input.CaptureStart.Add(time.Minute)
	input.CaptureEnd = input.CaptureEnd.Add(time.Minute)
	second, err := normalize(fixture.ctx, input, venueReconResolver{contract: *fixture.contract})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := venuerecon.AdmitStableProviderSnapshot(first, second)
	if err != nil || admission.Snapshot == nil {
		t.Fatalf("stable provider snapshot = %+v, %v", admission, err)
	}
	return admission.Snapshot
}

func setGoldenProviderPages(t *testing.T, input *venuerecon.CaptureInput) {
	t.Helper()
	header := map[string]any{
		"account_id": input.AccountID, "currency": input.Currency, "provider_as_of": goldenTime(input.ProviderAsOf),
		"cash": input.Cash, "equity": input.Equity,
	}
	first, second := mapsClone(header), mapsClone(header)
	positions := make([]map[string]any, 0, len(input.Positions))
	for _, row := range input.Positions {
		positions = append(positions, map[string]any{"contract_id": row.ContractID, "quantity": row.Quantity, "currency": row.Currency, "source_at": goldenTime(row.SourceAt)})
	}
	fills := make([]map[string]any, 0, len(input.Fills))
	for _, row := range input.Fills {
		fills = append(fills, map[string]any{
			"source_id": row.SourceID, "original_source_id": row.OriginalSourceID, "observation_class": row.ObservationClass,
			"observation_discriminator": row.ObservationDiscriminator, "external_order_id": row.ExternalOrderID,
			"client_order_id": row.ClientOrderID, "contract_id": row.ContractID, "side": row.Side, "quantity": row.Quantity,
			"price": row.Price, "fee": row.Fee, "currency": row.Currency, "source_revision": row.SourceRevision, "source_at": goldenTime(row.SourceAt),
		})
	}
	first["positions"], first["fills"] = positions, []any{}
	second["positions"], second["fills"] = []any{}, fills
	firstRaw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondRaw, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	input.Pages = []venuerecon.RawPage{{Raw: firstRaw, NextCursor: "next"}, {Cursor: "next", Terminal: true, Raw: secondRaw}}
}

func persistVenueReconciliationGolden(t *testing.T, fixture venueReconciliationGoldenFixture, repo *VenueReconciliationRepo) {
	t.Helper()
	if _, err := repo.RegisterVenueReconciliationPolicy(fixture.ctx, fixture.artifact); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordVenueProviderSnapshot(fixture.ctx, fixture.stable, fixture.artifact.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordVenueLocalSnapshot(fixture.ctx, fixture.local, fixture.artifact.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordVenueReconciliationRun(fixture.ctx, fixture.run, fixture.artifact.CreatedAt); err != nil {
		t.Fatal(err)
	}
}

func reconciliationNamespace(provider venue.Provider) string {
	if provider == venue.ProviderKalshi {
		return "kalshi/portfolio/fills"
	}
	return "alpaca/account-activities/FILL"
}

func goldenTime(value time.Time) string { return value.Format("2006-01-02T15:04:05.000000Z") }

func mustGoldenTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02T15:04:05.000000Z", value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func snapshotID(snapshot *venuerecon.LocalSnapshot) uuid.UUID {
	if snapshot == nil {
		return uuid.Nil
	}
	return snapshot.ID()
}
