package postgres

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/venuerecon"
)

type venueReconResolver struct{ contract instrument.VenueContract }

func (resolver venueReconResolver) ResolveVenueContract(context.Context, venue.Provider, string, time.Time) (instrument.VenueContract, error) {
	return resolver.contract, nil
}

type venueReconFixture struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	repo     *VenueReconciliationRepo
	policy   *venuerecon.PolicyArtifact
	provider *venuerecon.StableProviderSnapshot
	local    *venuerecon.LocalSnapshot
	run      *venuerecon.Run
	created  time.Time
}

func newVenueReconFixture(t *testing.T) venueReconFixture {
	t.Helper()
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	pool := pools.owner
	for _, migration := range []string{
		"000070_accounting_dual_run.up.sql", "000071_common_execution_lifecycle.up.sql",
		"000072_simulation_policy_artifacts.up.sql", "000073_venue_adapter_observations.up.sql",
		"000074_capital_margin_profiles.up.sql", "000075_venue_reconciliation.up.sql",
	} {
		if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, migration)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	economic := newEconomicLedgerFixture(t, ctx, pool, "venue-reconciliation")
	ledgerRepo := NewLedgerRepo(pool)
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, economic.source); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.ApplyEconomicNormalization(ctx, economic.normalization); err != nil {
		t.Fatal(err)
	}
	projectionRepo := NewProjectionRepo(pools.writer, pools.attestor)
	markAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	mark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: economic.instrument.ID, Price: decimal.NewFromInt(12), PriceCurrency: "USD",
		Source: "test-source", SourceNamespace: "marks/reconciliation", SourceObservationID: "mark-1",
		SourceRevision: "v1", EffectiveAt: markAt, ObservedAt: markAt.Add(time.Second), Metadata: json.RawMessage(`{"source":"test"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectionRepo.RecordMarkObservation(ctx, mark); err != nil {
		t.Fatal(err)
	}
	asOf := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	projection, err := projectionRepo.RebuildPortfolioProjection(ctx, ledger.ProjectionRequest{
		AccountID: economic.account.ID, AsOf: asOf, MarkSource: "test-source", MarkNamespace: "marks/reconciliation", MaxMarkAge: 48 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := projectionRepo.GetProjectionCheckpointByID(ctx, projection.CheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT id FROM ledger_transactions WHERE account_id=$1 AND effective_at <= $2 ORDER BY effective_at,id`, economic.account.ID, asOf)
	if err != nil {
		t.Fatal(err)
	}
	var transactionIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		transactionIDs = append(transactionIDs, id)
	}
	rows.Close()
	horizonStart := economic.normalization.EffectiveAt.Add(-time.Hour)
	local, err := venuerecon.NewLocalSnapshot(venuerecon.LocalSnapshotInput{
		AccountID: economic.account.ID, Provider: venue.ProviderAlpaca, Namespace: "alpaca/account-activities/FILL",
		HorizonStart: horizonStart, HorizonEnd: asOf, Checkpoint: checkpoint, TransactionIDs: transactionIDs,
		Issues: []venuerecon.LocalSnapshotIssue{{
			Reason: venuerecon.ReasonLocalFillIncomplete, AccountID: economic.account.ID,
			Provider: venue.ProviderAlpaca, Namespace: "alpaca/account-activities/FILL", SourceID: economic.source.SourceEventID,
			SourceAt: economic.normalization.EffectiveAt, VenueContractID: economic.contract.ID,
			LedgerTransactionID: economic.normalization.Transaction.ID, EvidenceID: economic.source.ID,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	providerContract := *economic.contract
	providerContract.Venue = string(venue.ProviderAlpaca)
	providerContract.ContractID = "AAPL"
	providerInput := venuerecon.CaptureInput{
		Provider: venue.ProviderAlpaca, Namespace: "alpaca/account-activities/FILL", AccountID: economic.account.ID.String(), Currency: "USD",
		HorizonStart: horizonStart, HorizonEnd: asOf, ProviderAsOf: asOf,
		CaptureStart: asOf.Add(time.Minute), CaptureEnd: asOf.Add(2 * time.Minute),
		Cash: projection.Totals.Cash.String(), Equity: projection.Totals.Equity.String(),
		Positions: []venuerecon.PositionInput{{ContractID: "AAPL", Quantity: projection.Positions[0].Quantity.String(), Currency: "USD", SourceAt: asOf}},
	}
	providerInput.Pages = venueReconRawPages(t, providerInput)
	first, err := venuerecon.NormalizeAlpacaCapture(ctx, providerInput, venueReconResolver{contract: providerContract})
	if err != nil {
		t.Fatal(err)
	}
	providerInput.CaptureStart = providerInput.CaptureStart.Add(time.Minute)
	providerInput.CaptureEnd = providerInput.CaptureEnd.Add(time.Minute)
	second, err := venuerecon.NormalizeAlpacaCapture(ctx, providerInput, venueReconResolver{contract: providerContract})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := venuerecon.AdmitStableProviderSnapshot(first, second)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := venuerecon.NewPolicy(venuerecon.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	created := asOf.Add(5 * time.Minute)
	artifact, err := policy.NewArtifact(created)
	if err != nil {
		t.Fatal(err)
	}
	run, err := venuerecon.Compare(venuerecon.CompareInput{Policy: policy, Provider: admission, Local: local, EquityBasisEquivalent: true})
	if err != nil {
		t.Fatal(err)
	}
	return venueReconFixture{
		ctx: ctx, pool: pool, repo: NewVenueReconciliationRepo(pool), policy: artifact,
		provider: admission.Snapshot, local: local, run: run, created: created,
	}
}

func venueReconRawPages(t *testing.T, input venuerecon.CaptureInput) []venuerecon.RawPage {
	t.Helper()
	header := map[string]any{
		"account_id": input.AccountID, "currency": input.Currency,
		"provider_as_of": input.ProviderAsOf.Format("2006-01-02T15:04:05.000000Z"), "cash": input.Cash, "equity": input.Equity,
	}
	first := mapsClone(header)
	positions := make([]map[string]any, 0, len(input.Positions))
	for _, row := range input.Positions {
		positions = append(positions, map[string]any{"contract_id": row.ContractID, "quantity": row.Quantity, "currency": row.Currency, "source_at": row.SourceAt.Format("2006-01-02T15:04:05.000000Z")})
	}
	first["positions"], first["fills"] = positions, []any{}
	second := mapsClone(header)
	second["positions"], second["fills"] = []any{}, []any{}
	firstRaw, _ := json.Marshal(first)
	secondRaw, _ := json.Marshal(second)
	return []venuerecon.RawPage{{Raw: firstRaw, NextCursor: "next"}, {Cursor: "next", Terminal: true, Raw: secondRaw}}
}

func mapsClone(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func TestVenueReconciliationRepoPersistsReloadsAndConverges(t *testing.T) {
	fixture := newVenueReconFixture(t)
	registered, err := fixture.repo.RegisterVenueReconciliationPolicy(fixture.ctx, fixture.policy)
	if err != nil || registered.Version != fixture.policy.Version {
		t.Fatalf("register policy = %+v, %v", registered, err)
	}
	if err := fixture.repo.RecordVenueProviderSnapshot(fixture.ctx, fixture.provider, fixture.created); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.RecordVenueLocalSnapshot(fixture.ctx, fixture.local, fixture.created); err != nil {
		t.Fatal(err)
	}
	persisted, err := fixture.repo.RecordVenueReconciliationRun(fixture.ctx, fixture.run, fixture.created)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ID != fixture.run.ID || string(persisted.CanonicalBytes) != string(fixture.run.CanonicalBytes) {
		t.Fatal("persisted run differs")
	}
	if _, err := fixture.repo.RegisterVenueReconciliationPolicy(fixture.ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.RecordVenueProviderSnapshot(fixture.ctx, fixture.provider, fixture.created); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.RecordVenueLocalSnapshot(fixture.ctx, fixture.local, fixture.created); err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.repo.GetVenueReconciliationRun(fixture.ctx, fixture.run.ID)
	if err != nil || loaded.ID != fixture.run.ID || loaded.SHA256 != fixture.run.SHA256 {
		t.Fatalf("loaded run = %+v, %v", loaded, err)
	}
}

func TestVenueReconciliationRepoEightWritersConvergeWithoutDuplicateIncidents(t *testing.T) {
	fixture := newVenueReconFixture(t)
	if _, err := fixture.repo.RegisterVenueReconciliationPolicy(fixture.ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.RecordVenueProviderSnapshot(fixture.ctx, fixture.provider, fixture.created); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.RecordVenueLocalSnapshot(fixture.ctx, fixture.local, fixture.created); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.repo.RecordVenueReconciliationRun(fixture.ctx, fixture.run, fixture.created)
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Error(err)
		}
	}
	var runs, results, incidents int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT (SELECT count(*) FROM venue_reconciliation_runs WHERE id=$1),
		(SELECT count(*) FROM venue_reconciliation_results WHERE run_id=$1),(SELECT count(*) FROM venue_reconciliation_incidents WHERE run_id=$1)`, fixture.run.ID).Scan(&runs, &results, &incidents); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || results != len(fixture.run.Results) || incidents != len(fixture.run.Incidents) {
		t.Fatalf("counts=%d/%d/%d", runs, results, incidents)
	}
}

func TestVenueReconciliationRepoRejectsMutationAndIncompleteGraph(t *testing.T) {
	fixture := newVenueReconFixture(t)
	if _, err := fixture.repo.RegisterVenueReconciliationPolicy(fixture.ctx, fixture.policy); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE venue_reconciliation_policy_artifacts SET created_at=created_at WHERE id=$1`, fixture.policy.ID); err == nil {
		t.Fatal("policy update succeeded")
	}
	tx, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	state := fixture.provider.Capture()
	firstCapture, secondCapture := fixture.provider.Captures()
	_, err = tx.Exec(fixture.ctx, `INSERT INTO venue_provider_snapshots(id,schema_name,provider,account_external_id,namespace,currency,horizon_start,horizon_end,
		state_sha256,sha256,canonical_bytes,canonical_json,state_bytes,state_json,first_capture_id,second_capture_id,
		first_capture_start,first_capture_end,second_capture_start,second_capture_end,page_count,position_count,fill_count,created_at)
		VALUES($1,'venue-provider-stable-snapshot-v1',$2,$3,$4,$5,$6,$7,$8,$9,$10,convert_from($10,'UTF8')::JSONB,$11,convert_from($11,'UTF8')::JSONB,
		$12,$13,$14,$15,$16,$17,1,1,0,$18)`,
		fixture.provider.ID(), state.Provider(), state.AccountID(), state.Namespace(), state.Currency(), state.HorizonStart(), state.HorizonEnd(),
		state.Digest(), fixture.provider.Digest(), []byte(fixture.provider.CanonicalBytes()), []byte(state.CanonicalBytes()),
		firstCapture.ID(), secondCapture.ID(), firstCapture.CaptureStart(), firstCapture.CaptureEnd(), secondCapture.CaptureStart(), secondCapture.CaptureEnd(), fixture.created)
	if err == nil {
		err = tx.Commit(fixture.ctx)
	} else {
		_ = tx.Rollback(fixture.ctx)
	}
	if err == nil {
		t.Fatal("incomplete provider graph committed")
	}
	var count int
	if scanErr := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM venue_provider_snapshots`).Scan(&count); scanErr != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, scanErr)
	}
	if err := fixture.repo.RecordVenueProviderSnapshot(fixture.ctx, fixture.provider, fixture.created); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `DELETE FROM venue_provider_snapshot_pages WHERE snapshot_id=$1`, fixture.provider.ID()); err == nil {
		t.Fatal("provider page deletion succeeded")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO venue_provider_snapshot_pages(snapshot_id,sequence,cursor,next_cursor,terminal,sha256,raw_bytes)
		VALUES($1,99,'forged','','true',encode(digest($2::BYTEA,'sha256'),'hex'),$2)`, fixture.provider.ID(), []byte(`{"forged":true}`)); err == nil {
		t.Fatal("extra provider page committed")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO venue_provider_snapshot_pages(snapshot_id,sequence,cursor,next_cursor,terminal,sha256,raw_bytes)
		VALUES($1,0,'','','true',encode(digest($2::BYTEA,'sha256'),'hex'),$2)`, uuid.New(), []byte(`{}`)); err == nil {
		t.Fatal("orphan provider page committed")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `DELETE FROM venue_provider_snapshots WHERE id=$1`, fixture.provider.ID()); err == nil {
		t.Fatal("provider snapshot deletion succeeded")
	}
}
