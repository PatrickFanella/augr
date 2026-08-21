package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/costattribution"
	"github.com/PatrickFanella/get-rich-quick/internal/evidencereview"
	reviewqualification "github.com/PatrickFanella/get-rich-quick/internal/evidencereview/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	researchqualification "github.com/PatrickFanella/get-rich-quick/internal/researchworkflow/qualification"
)

func TestCostAttributionRetainedQualification(t *testing.T) {
	url := os.Getenv("COST_ATTRIBUTION_QUALIFICATION_DB_URL")
	if url == "" {
		t.Skip("set COST_ATTRIBUTION_QUALIFICATION_DB_URL to a dedicated schema-100 database with OVR-603 evidence")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM full_cost_attribution_reports`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("existing=%d/%v", count, err)
	}
	var accountID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts ORDER BY id LIMIT 1`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	reviewFixture, err := reviewqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	researchFixture, err := researchqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	fee := qualificationCostTransaction(t, accountID, "cost.fee", "ovr606-fee", start.Add(time.Hour), []ledger.PostingInput{{IdempotencyKey: "fee-expense", LedgerAccount: "expense:fees", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.RequireFromString("1.25"), Metadata: json.RawMessage(`{}`)}, {IdempotencyKey: "fee-cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.RequireFromString("-1.25"), Metadata: json.RawMessage(`{}`)}})
	rebate := qualificationCostTransaction(t, accountID, "cost.rebate", "ovr606-rebate", start.Add(2*time.Hour), []ledger.PostingInput{{IdempotencyKey: "rebate-cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.RequireFromString("0.4"), Metadata: json.RawMessage(`{}`)}, {IdempotencyKey: "rebate-income", LedgerAccount: "income:rebates", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.RequireFromString("-0.4"), Metadata: json.RawMessage(`{}`)}})
	ledgerRepo := NewLedgerRepo(pool)
	fee, err = ledgerRepo.PostTransaction(ctx, fee)
	if err != nil {
		t.Fatal(err)
	}
	rebate, err = ledgerRepo.PostTransaction(ctx, rebate)
	if err != nil {
		t.Fatal(err)
	}
	var feeSHA, rebateSHA string
	if err := pool.QueryRow(ctx, `SELECT full_cost_ledger_evidence_sha256($1),full_cost_ledger_evidence_sha256($2)`, fee.ID, rebate.ID).Scan(&feeSHA, &rebateSHA); err != nil {
		t.Fatal(err)
	}
	incompleteInput := qualificationCostInput(reviewFixture.Case, reviewFixture.Summary, researchFixture, accountID, start, end, fee.ID, feeSHA, rebate.ID, rebateSHA, true)
	incomplete, err := costattribution.NewReport(incompleteInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, failedStage := range []string{"report", "lines"} {
		repo := NewCostAttributionRepo(pool)
		repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := repo.RegisterReport(ctx, incomplete); err == nil {
			t.Fatalf("stage %s accepted", failedStage)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM full_cost_attribution_reports WHERE id=$1`, incomplete.ID()).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failedStage, count, err)
		}
	}
	type result struct {
		persisted *PersistedCostAttributionReport
		err       error
	}
	results := make(chan result, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			persisted, registerErr := NewCostAttributionRepo(pool).RegisterReport(ctx, incomplete)
			results <- result{persisted, registerErr}
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Error(result.err)
			continue
		}
		if result.persisted.ID != incomplete.ID() || result.persisted.SHA256 != incomplete.Digest() {
			t.Fatalf("result=%+v", result.persisted)
		}
	}
	changedInput := incompleteInput
	changedInput.Lines = append([]costattribution.LineInput(nil), incompleteInput.Lines...)
	changedInput.Lines[4].Explanation = "Changed unknown infrastructure reason."
	changed, err := costattribution.NewReport(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCostAttributionRepo(pool).RegisterReport(ctx, changed); !errors.Is(err, ErrCostAttributionConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	restarted, err := NewCostAttributionRepo(pool).GetReport(ctx, incomplete.ID())
	if err != nil || restarted.SHA256 != incomplete.Digest() || string(restarted.CanonicalBytes) != string(incomplete.CanonicalBytes()) {
		t.Fatalf("restart=%+v/%v", restarted, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE full_cost_attribution_reports SET currency=currency WHERE id=$1`, incomplete.ID()); err == nil {
		t.Fatal("append-only update accepted")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM full_cost_attribution_reports WHERE id=$1`, incomplete.ID()); err == nil {
		t.Fatal("append-only delete accepted")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO full_cost_attribution_lines(report_id,sequence,line_key,category,knowledge_status,amount,evidence_kind,evidence_sha256,method,method_sha256,explanation,canonical_row) VALUES($1,5,'forged_extra','infrastructure','unknown','','','','','','forged','{}')`, incomplete.ID()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("direct SQL line forgery accepted")
	}
	completeInput := qualificationCostInput(reviewFixture.HeldCase, reviewFixture.HeldSummary, researchFixture, accountID, start, end, fee.ID, feeSHA, rebate.ID, rebateSHA, false)
	forgedInput := completeInput
	forgedInput.Lines = append([]costattribution.LineInput(nil), completeInput.Lines...)
	forgedInput.Lines[2].Amount = "1.24"
	forged, err := costattribution.NewReport(forgedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCostAttributionRepo(pool).RegisterReport(ctx, forged); err == nil {
		t.Fatal("forged actual ledger fee accepted")
	}
	complete, err := costattribution.NewReport(completeInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCostAttributionRepo(pool).RegisterReport(ctx, complete); err != nil {
		t.Fatal(err)
	}
	if incomplete.Totals().Coverage != costattribution.CoverageContainsUnknowns || incomplete.Totals().UnknownCount != 1 || complete.Totals().Coverage != costattribution.CoverageWithEstimates || complete.Totals().UnknownCount != 0 {
		t.Fatalf("coverage incomplete=%+v complete=%+v", incomplete.Totals(), complete.Totals())
	}
	t.Logf("incomplete_id=%s sha256=%s net=%s", incomplete.ID(), incomplete.Digest(), incomplete.Totals().KnownNetCost)
	t.Logf("complete_id=%s sha256=%s net=%s", complete.ID(), complete.Digest(), complete.Totals().KnownNetCost)
}

func qualificationCostTransaction(t *testing.T, accountID uuid.UUID, eventType, key string, effectiveAt time.Time, postings []ledger.PostingInput) *ledger.Transaction {
	t.Helper()
	transaction, err := ledger.NewTransaction(ledger.TransactionInput{AccountID: accountID, EventType: eventType, IdempotencyKey: key, OriginType: "ovr606_qualification", OriginID: key, EffectiveAt: effectiveAt, ObservedAt: effectiveAt, Metadata: json.RawMessage(`{"synthetic":true}`), Postings: postings})
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

func qualificationCostInput(reviewCase *evidencereview.Case, summary *evidencereview.Summary, research researchqualification.Fixture, accountID uuid.UUID, start, end time.Time, feeID uuid.UUID, feeSHA string, rebateID uuid.UUID, rebateSHA string, infrastructureUnknown bool) costattribution.Input {
	lines := []costattribution.LineInput{
		{Key: "model_generation", Category: costattribution.CategoryModel, Status: costattribution.StatusActual, Amount: research.Hypothesis.ProvenanceCost(), EvidenceKind: "research_hypothesis", EvidenceID: research.Hypothesis.ID(), EvidenceSHA256: research.Hypothesis.Digest(), Explanation: "Actual retained model invocation cost."},
		{Key: "licensed_dataset", Category: costattribution.CategoryData, Status: costattribution.StatusEstimated, Amount: "2.5", EvidenceKind: "dataset_manifest", EvidenceID: research.Parents.Manifest.ID(), EvidenceSHA256: research.Parents.Manifest.Digest(), Method: "partition_rate_v1", MethodSHA256: strings.Repeat("a", 64), Explanation: "Estimated from retained partition volume and reviewed unit rate."},
		{Key: "execution_fee", Category: costattribution.CategoryFee, Status: costattribution.StatusActual, Amount: "1.25", EvidenceKind: "ledger_transaction", EvidenceID: feeID, EvidenceSHA256: feeSHA, Explanation: "Actual immutable ledger fee."},
		{Key: "venue_rebate", Category: costattribution.CategoryRebate, Status: costattribution.StatusActual, Amount: "0.4", EvidenceKind: "ledger_transaction", EvidenceID: rebateID, EvidenceSHA256: rebateSHA, Explanation: "Actual immutable ledger rebate."},
	}
	if infrastructureUnknown {
		lines = append(lines, costattribution.LineInput{Key: "shared_infrastructure", Category: costattribution.CategoryInfrastructure, Status: costattribution.StatusUnknown, Explanation: "No attributable infrastructure invoice or allocation evidence was retained."})
	} else {
		lines = append(lines, costattribution.LineInput{Key: "shared_infrastructure", Category: costattribution.CategoryInfrastructure, Status: costattribution.StatusEstimated, Amount: "0.75", EvidenceKind: "external_artifact", EvidenceID: uuid.MustParse("60600000-0000-4000-8000-000000000004"), EvidenceSHA256: strings.Repeat("d", 64), Method: "cpu_hour_allocation_v1", MethodSHA256: strings.Repeat("e", 64), Explanation: "Estimated from retained CPU hours and reviewed allocation rate."})
	}
	return costattribution.Input{Case: reviewCase, Summary: summary, Hypothesis: research.Hypothesis, Manifest: research.Parents.Manifest, AccountID: accountID, WindowStart: start, WindowEnd: end, StatementAt: end, Currency: "USD", Lines: lines}
}
