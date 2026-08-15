package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestLedgerRepoPostsBalancedTransaction(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)

	effectiveAt := time.Date(2026, 8, 14, 20, 0, 0, 0, time.UTC)
	transaction, err := ledger.NewTransaction(ledger.TransactionInput{
		AccountID:      uuid.MustParse("00000000-0000-4000-8000-000000000064"),
		EventType:      "operator.adjustment",
		IdempotencyKey: "operator-adjustment:" + uuid.NewString(),
		OriginType:     "operator",
		OriginID:       uuid.NewString(),
		EffectiveAt:    effectiveAt,
		Metadata:       json.RawMessage(`{"reason":"repository tracer"}`),
		Postings: []ledger.PostingInput{
			{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(10)},
			{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-10)},
		},
	})
	if err != nil {
		t.Fatalf("ledger.NewTransaction() error = %v", err)
	}

	created, err := repo.PostTransaction(ctx, transaction)
	if err != nil {
		t.Fatalf("PostTransaction() error = %v", err)
	}
	if created.ID != transaction.ID || len(created.Postings) != 2 {
		t.Fatalf("created transaction = %s with %d postings, want %s with 2", created.ID, len(created.Postings), transaction.ID)
	}

	got, err := repo.GetByID(ctx, transaction.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.EventType != transaction.EventType || !got.EffectiveAt.Equal(effectiveAt) {
		t.Fatalf("GetByID() event/time = %q/%s, want %q/%s", got.EventType, got.EffectiveAt, transaction.EventType, effectiveAt)
	}
	if len(got.Postings) != 2 || !got.Postings[0].Amount.Add(got.Postings[1].Amount).IsZero() {
		t.Fatalf("GetByID() postings = %+v, want two balanced lines", got.Postings)
	}
	if string(got.Metadata) != `{"reason": "repository tracer"}` {
		t.Fatalf("GetByID() metadata = %s", got.Metadata)
	}
}

func TestLedgerRepoReplaysIdenticalTransaction(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	originID := uuid.NewString()
	idempotencyKey := "replay:" + originID
	effectiveAt := time.Date(2026, 8, 15, 12, 0, 0, 123456789, time.UTC)

	newTransaction := func() *ledger.Transaction {
		transaction, err := ledger.NewTransaction(ledger.TransactionInput{
			AccountID:      accountID,
			EventType:      "operator.replay_test",
			IdempotencyKey: idempotencyKey,
			OriginType:     "operator",
			OriginID:       originID,
			EffectiveAt:    effectiveAt,
			Metadata:       json.RawMessage(`{"b":2,"a":1}`),
			Postings: []ledger.PostingInput{
				{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(25)},
				{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-25)},
			},
		})
		if err != nil {
			t.Fatalf("ledger.NewTransaction() error = %v", err)
		}
		return transaction
	}

	created, err := repo.PostTransaction(ctx, newTransaction())
	if err != nil {
		t.Fatalf("PostTransaction(first) error = %v", err)
	}
	replayed, err := repo.PostTransaction(ctx, newTransaction())
	if err != nil {
		t.Fatalf("PostTransaction(retry) error = %v", err)
	}
	if replayed.ID != created.ID {
		t.Fatalf("replayed ID = %s, want original %s", replayed.ID, created.ID)
	}
}

func TestLedgerRepoRejectsIdempotencyPayloadConflict(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	originID := uuid.NewString()
	idempotencyKey := "conflict:" + originID

	newTransaction := func(amount int64) *ledger.Transaction {
		transaction, err := ledger.NewTransaction(ledger.TransactionInput{
			AccountID:      accountID,
			EventType:      "operator.conflict_test",
			IdempotencyKey: idempotencyKey,
			OriginType:     "operator",
			OriginID:       originID,
			EffectiveAt:    time.Date(2026, 8, 15, 13, 0, 0, 0, time.UTC),
			Postings: []ledger.PostingInput{
				{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(amount)},
				{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-amount)},
			},
		})
		if err != nil {
			t.Fatalf("ledger.NewTransaction() error = %v", err)
		}
		return transaction
	}

	if _, err := repo.PostTransaction(ctx, newTransaction(25)); err != nil {
		t.Fatalf("PostTransaction(first) error = %v", err)
	}
	if _, err := repo.PostTransaction(ctx, newTransaction(26)); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("PostTransaction(conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestLedgerRepoRejectsMetadataConflictBeyondFloatPrecision(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	originID := uuid.NewString()

	newTransaction := func(metadata json.RawMessage) *ledger.Transaction {
		transaction, err := ledger.NewTransaction(ledger.TransactionInput{
			AccountID:      accountID,
			EventType:      "operator.metadata_precision_test",
			IdempotencyKey: "metadata-precision:" + originID,
			OriginType:     "operator",
			OriginID:       originID,
			EffectiveAt:    time.Date(2026, 8, 15, 13, 30, 0, 0, time.UTC),
			Metadata:       metadata,
			Postings: []ledger.PostingInput{
				{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(1)},
				{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-1)},
			},
		})
		if err != nil {
			t.Fatalf("ledger.NewTransaction() error = %v", err)
		}
		return transaction
	}

	if _, err := repo.PostTransaction(ctx, newTransaction(json.RawMessage(`{"sequence":9007199254740992}`))); err != nil {
		t.Fatalf("PostTransaction(first) error = %v", err)
	}
	if _, err := repo.PostTransaction(ctx, newTransaction(json.RawMessage(`{"sequence":9007199254740993}`))); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("PostTransaction(metadata conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestLedgerRepoRejectsDuplicateOriginWithDifferentKey(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	originID := uuid.NewString()

	newTransaction := func(idempotencyKey string) *ledger.Transaction {
		transaction, err := ledger.NewTransaction(ledger.TransactionInput{
			AccountID:      accountID,
			EventType:      "operator.origin_test",
			IdempotencyKey: idempotencyKey,
			OriginType:     "operator",
			OriginID:       originID,
			EffectiveAt:    time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC),
			Postings: []ledger.PostingInput{
				{IdempotencyKey: "cash", LedgerAccount: "asset:cash", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(25)},
				{IdempotencyKey: "equity", LedgerAccount: "equity:adjustment", UnitKind: ledger.UnitKindCurrency, Unit: "USD", Amount: decimal.NewFromInt(-25)},
			},
		})
		if err != nil {
			t.Fatalf("ledger.NewTransaction() error = %v", err)
		}
		return transaction
	}

	if _, err := repo.PostTransaction(ctx, newTransaction("origin-first:"+originID)); err != nil {
		t.Fatalf("PostTransaction(first) error = %v", err)
	}
	if _, err := repo.PostTransaction(ctx, newTransaction("origin-second:"+originID)); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("PostTransaction(duplicate origin) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestEconomicEventRepoPersistsRawThenAppliesExactLedger(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	fixture := newEconomicLedgerFixture(t, ctx, pool, "persist")

	persistedSource, err := repo.RecordEconomicSourceEvent(ctx, fixture.source)
	if err != nil {
		t.Fatalf("RecordEconomicSourceEvent() error = %v", err)
	}
	if persistedSource.ID != fixture.source.ID || string(persistedSource.RawPayload) != string(fixture.source.RawPayload) {
		t.Fatalf("persisted source = %+v", persistedSource)
	}
	persisted, err := repo.ApplyEconomicNormalization(ctx, fixture.normalization)
	if err != nil {
		t.Fatalf("ApplyEconomicNormalization() error = %v", err)
	}
	if persisted.SourceEvent.ID != fixture.source.ID || persisted.Transaction.ID != fixture.normalization.Transaction.ID ||
		len(persisted.Transaction.Postings) != 4 || persisted.Instrument == nil ||
		persisted.Instrument.ID != fixture.instrument.ID || persisted.VenueContract == nil ||
		persisted.VenueContract.ID != fixture.contract.ID {
		t.Fatalf("persisted normalization = %+v", persisted)
	}
	if err := persisted.Validate(); err != nil {
		t.Fatalf("loaded normalization Validate() error = %v", err)
	}
}

func TestEconomicEventRepoReplaysRawAndRejectsRevisionConflict(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	fixture := newEconomicLedgerFixture(t, ctx, pool, "raw-replay")
	first, err := repo.RecordEconomicSourceEvent(ctx, fixture.source)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.RecordEconomicSourceEvent(ctx, fixture.source)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID {
		t.Fatalf("replayed source ID = %s, want %s", replayed.ID, first.ID)
	}

	conflict := *fixture.source
	conflict.SourceRevision = "revision-2"
	if _, err := repo.RecordEconomicSourceEvent(ctx, &conflict); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("RecordEconomicSourceEvent(revision conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestEconomicEventRepoConcurrentIdenticalRawWritesConverge(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	fixture := newEconomicLedgerFixture(t, ctx, pool, "raw-concurrent")

	const writers = 8
	results := make(chan *ledger.EconomicSourceEvent, writers)
	errorsChannel := make(chan error, writers)
	var waitGroup sync.WaitGroup
	for range writers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			persisted, err := repo.RecordEconomicSourceEvent(ctx, fixture.source)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- persisted
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("concurrent RecordEconomicSourceEvent() error = %v", err)
	}
	for result := range results {
		if !ledger.SameEconomicSourceEventPayload(result, fixture.source) {
			t.Errorf("concurrent raw result differs from source: %+v", result)
		}
	}
	var sourceCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM economic_source_events WHERE id = $1`, fixture.source.ID).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount != 1 {
		t.Fatalf("concurrent raw source count = %d, want 1", sourceCount)
	}
}

func TestEconomicEventRepoReplaysAppliedNormalizationAndRejectsChangedVersion(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	fixture := newEconomicLedgerFixture(t, ctx, pool, "apply-replay")
	if _, err := repo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	first, err := repo.ApplyEconomicNormalization(ctx, fixture.normalization)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.ApplyEconomicNormalization(ctx, fixture.normalization)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID || replayed.Transaction.ID != first.Transaction.ID {
		t.Fatalf("replayed normalization identity changed: %+v", replayed)
	}

	base := fixture.base
	base.NormalizerVersion = "economic_event_v2"
	changed, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: base, Instrument: *fixture.instrument, VenueContract: *fixture.contract,
		Side: ledger.FillSideBuy, Quantity: decimal.NewFromInt(2), Price: decimal.RequireFromString("10.25"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApplyEconomicNormalization(ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("ApplyEconomicNormalization(changed version) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestEconomicEventRepoFailedApplyRetainsRawWithoutPartialLedger(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	fixture := newEconomicLedgerFixture(t, ctx, pool, "failed-apply")
	if _, err := repo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}

	missingInstrument := *fixture.instrument
	missingInstrument.ID = uuid.New()
	missingContract := *fixture.contract
	missingContract.ID = uuid.New()
	missingContract.InstrumentID = missingInstrument.ID
	failed, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: fixture.base, Instrument: missingInstrument, VenueContract: missingContract,
		Side: ledger.FillSideBuy, Quantity: decimal.NewFromInt(2), Price: decimal.RequireFromString("10.25"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApplyEconomicNormalization(ctx, failed); err == nil {
		t.Fatal("ApplyEconomicNormalization() with unknown canonical references unexpectedly succeeded")
	}

	var rawCount, normalizationCount, transactionCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM economic_source_events WHERE id = $1),
		(SELECT COUNT(*) FROM economic_event_normalizations WHERE source_event_id = $1),
		(SELECT COUNT(*) FROM ledger_transactions WHERE origin_id = $1::TEXT)`, fixture.source.ID).Scan(
		&rawCount,
		&normalizationCount,
		&transactionCount,
	); err != nil {
		t.Fatal(err)
	}
	if rawCount != 1 || normalizationCount != 0 || transactionCount != 0 {
		t.Fatalf("raw/normalization/transaction counts = %d/%d/%d, want 1/0/0", rawCount, normalizationCount, transactionCount)
	}
}

func TestEconomicEventRepoConcurrentIdenticalApplyConverges(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	fixture := newEconomicLedgerFixture(t, ctx, pool, "concurrent")
	if _, err := repo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}

	const writers = 8
	results := make(chan *ledger.EconomicNormalization, writers)
	errorsChannel := make(chan error, writers)
	var waitGroup sync.WaitGroup
	for range writers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			persisted, err := repo.ApplyEconomicNormalization(ctx, fixture.normalization)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- persisted
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("concurrent ApplyEconomicNormalization() error = %v", err)
	}
	for result := range results {
		if result.ID != fixture.normalization.ID || result.Transaction.ID != fixture.normalization.Transaction.ID {
			t.Errorf("concurrent result identity = %s/%s", result.ID, result.Transaction.ID)
		}
	}
	var normalizationCount, transactionCount, postingCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM economic_event_normalizations WHERE source_event_id = $1),
		(SELECT COUNT(*) FROM ledger_transactions WHERE origin_id = $1::TEXT),
		(SELECT COUNT(*) FROM ledger_postings WHERE transaction_id = $2)`,
		fixture.source.ID,
		fixture.normalization.Transaction.ID,
	).Scan(&normalizationCount, &transactionCount, &postingCount); err != nil {
		t.Fatal(err)
	}
	if normalizationCount != 1 || transactionCount != 1 || postingCount != 4 {
		t.Fatalf("concurrent counts = normalization:%d transaction:%d postings:%d", normalizationCount, transactionCount, postingCount)
	}
}

func TestEconomicEventRepoAppliesPhysicalOptionFromPersistedTerms(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	repo := NewLedgerRepo(pool)
	fixture := newPhysicalEconomicLedgerFixture(t, ctx, pool, "physical")
	if _, err := repo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.ApplyEconomicNormalization(ctx, fixture.normalization)
	if err != nil {
		t.Fatalf("ApplyEconomicNormalization(physical) error = %v", err)
	}
	if persisted.OptionTerms == nil || persisted.OptionTerms.ID != fixture.terms.ID ||
		persisted.SecondaryInstrument == nil || persisted.SecondaryInstrument.ID != fixture.underlying.ID {
		t.Fatalf("physical references were not reloaded: %+v", persisted)
	}
	assertRepositoryPostingAmount(t, persisted.Transaction, "option-close", "-2")
	assertRepositoryPostingAmount(t, persisted.Transaction, "underlying-delivery", "200")
	assertRepositoryPostingAmount(t, persisted.Transaction, "strike-cash", "-25000")
}

func TestOptionTermsRepoRejectsRetroactiveSupersessionAndAllowsFutureTerm(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	ledgerRepo := NewLedgerRepo(pool)
	instrumentRepo := NewInstrumentRepo(pool)
	fixture := newPhysicalEconomicLedgerFixture(t, ctx, pool, "term-history")
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.ApplyEconomicNormalization(ctx, fixture.normalization); err != nil {
		t.Fatal(err)
	}

	retroactive := newChangedOptionTerms(t, fixture, "retroactive", fixture.normalization.EffectiveAt.Add(-time.Hour), fixture.source.ObservedAt.Add(-time.Minute))
	if _, err := instrumentRepo.RegisterOptionContractTerms(ctx, retroactive); err == nil ||
		!strings.Contains(err.Error(), "retroactively supersede") {
		t.Fatalf("RegisterOptionContractTerms(retroactive) error = %v, want supersession rejection", err)
	}

	futureEffective := fixture.normalization.EffectiveAt.Add(time.Hour)
	future := newChangedOptionTerms(t, fixture, "future", futureEffective, futureEffective.Add(time.Second))
	if _, err := instrumentRepo.RegisterOptionContractTerms(ctx, future); err != nil {
		t.Fatalf("RegisterOptionContractTerms(future) error = %v", err)
	}
}

func TestOptionTermsAndPhysicalNormalizationConcurrentCommitSerialize(t *testing.T) {
	ctx := context.Background()
	pool := newLedgerIntegrationPool(t, ctx)
	ledgerRepo := NewLedgerRepo(pool)
	instrumentRepo := NewInstrumentRepo(pool)
	fixture := newPhysicalEconomicLedgerFixture(t, ctx, pool, "term-race")
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	newer := newChangedOptionTerms(
		t,
		fixture,
		"concurrent",
		fixture.normalization.EffectiveAt.Add(-time.Hour),
		fixture.source.ObservedAt.Add(-time.Minute),
	)
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gate.Rollback(ctx) }()
	if err := acquireEconomicOptionTermsLock(ctx, gate, fixture.option.ID); err != nil {
		t.Fatal(err)
	}

	type operationResult struct {
		name string
		err  error
	}
	results := make(chan operationResult, 2)
	start := make(chan struct{})
	go func() {
		<-start
		_, err := ledgerRepo.ApplyEconomicNormalization(ctx, fixture.normalization)
		results <- operationResult{name: "normalization", err: err}
	}()
	go func() {
		<-start
		_, err := instrumentRepo.RegisterOptionContractTerms(ctx, newer)
		results <- operationResult{name: "terms", err: err}
	}()
	close(start)
	select {
	case result := <-results:
		t.Fatalf("concurrent operation %s completed while option-history gate was held: %v", result.name, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := gate.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	first := <-results
	second := <-results

	successes := 0
	for _, result := range []operationResult{first, second} {
		if result.err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent outcomes = %s:%v, %s:%v; want exactly one success", first.name, first.err, second.name, second.err)
	}

	var normalizationCount, termsCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM economic_event_normalizations WHERE source_event_id = $1),
		(SELECT COUNT(*) FROM option_contract_terms WHERE option_instrument_id = $2)`,
		fixture.source.ID,
		fixture.option.ID,
	).Scan(&normalizationCount, &termsCount); err != nil {
		t.Fatal(err)
	}
	if normalizationCount == 1 && termsCount == 1 {
		return
	}
	if normalizationCount == 0 && termsCount == 2 {
		return
	}
	t.Fatalf("serialized counts = normalization:%d terms:%d", normalizationCount, termsCount)
}

type economicLedgerFixture struct {
	account       *domain.Account
	instrument    *instrument.Instrument
	contract      *instrument.VenueContract
	source        *ledger.EconomicSourceEvent
	base          ledger.EconomicNormalizationBaseInput
	normalization *ledger.EconomicNormalization
}

type physicalEconomicLedgerFixture struct {
	account       *domain.Account
	option        *instrument.Instrument
	underlying    *instrument.Instrument
	contract      *instrument.VenueContract
	terms         *instrument.OptionContractTerms
	source        *ledger.EconomicSourceEvent
	normalization *ledger.EconomicNormalization
}

func newEconomicLedgerFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) economicLedgerFixture {
	t.Helper()
	account, err := NewAccountRepo(pool).GetByID(ctx, uuid.MustParse("00000000-0000-4000-8000-000000000064"))
	if err != nil {
		t.Fatal(err)
	}
	instrumentRepo := NewInstrumentRepo(pool)
	primary, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "figi:economic:" + suffix, AssetClass: instrument.AssetClassEquity,
		PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	primary, err = instrumentRepo.CreateInstrument(ctx, primary)
	if err != nil {
		t.Fatal(err)
	}
	effectiveAt := time.Date(2026, time.August, 15, 15, 0, 0, 0, time.UTC)
	validTo := effectiveAt.Add(time.Hour)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID: primary.ID, Venue: "test-venue", ContractID: "contract-" + suffix,
		Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1),
		Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical,
		ValidFrom: effectiveAt.Add(-time.Hour), ValidTo: &validTo,
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err = instrumentRepo.RegisterVenueContract(ctx, contract)
	if err != nil {
		t.Fatal(err)
	}
	source, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID: account.ID, Source: "simulator", SourceNamespace: "fills/repository",
		SourceEventID: "fill-" + suffix, SourceRevision: "v1", ObservedAt: effectiveAt.Add(time.Second),
		RawPayload: json.RawMessage(" {\n\"id\":\"fill-" + suffix + "\",\"price\":\"10.25\"\n} "),
	})
	if err != nil {
		t.Fatal(err)
	}
	base := ledger.EconomicNormalizationBaseInput{
		SourceEvent: source, Account: account, NormalizerVersion: "economic_event_v1",
		ExecutionOriginType: ledger.ExecutionOriginStrategyVersion, ExecutionOriginID: "strategy-version-1",
		ReferenceType: "fill", ReferenceID: "fill-" + suffix, EffectiveAt: effectiveAt,
	}
	normalization, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: base, Instrument: *primary, VenueContract: *contract,
		Side: ledger.FillSideBuy, Quantity: decimal.NewFromInt(2), Price: decimal.RequireFromString("10.25"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return economicLedgerFixture{
		account: account, instrument: primary, contract: contract,
		source: source, base: base, normalization: normalization,
	}
}

func newPhysicalEconomicLedgerFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) physicalEconomicLedgerFixture {
	t.Helper()
	account, err := NewAccountRepo(pool).GetByID(ctx, uuid.MustParse("00000000-0000-4000-8000-000000000064"))
	if err != nil {
		t.Fatal(err)
	}
	instrumentRepo := NewInstrumentRepo(pool)
	underlying, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "figi:physical-underlying:" + suffix, AssetClass: instrument.AssetClassEquity,
		PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical, Status: instrument.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	underlying, err = instrumentRepo.CreateInstrument(ctx, underlying)
	if err != nil {
		t.Fatal(err)
	}
	effectiveAt := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
	expiration := effectiveAt.Add(-time.Hour)
	option, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "occ:physical:" + suffix, AssetClass: instrument.AssetClassOption,
		PrimaryVenue: "test-venue", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(100), Expiration: &expiration,
		ExerciseStyle: instrument.ExerciseAmerican, SettlementMethod: instrument.SettlementPhysical,
		UnderlyingID: &underlying.ID, Status: instrument.StatusExpired,
	})
	if err != nil {
		t.Fatal(err)
	}
	option, err = instrumentRepo.CreateInstrument(ctx, option)
	if err != nil {
		t.Fatal(err)
	}
	ended := effectiveAt.Add(-time.Second)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID: option.ID, Venue: "test-venue", ContractID: "physical-" + suffix,
		Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: decimal.NewFromInt(1),
		Multiplier: decimal.NewFromInt(100), SettlementMethod: instrument.SettlementPhysical,
		ValidFrom: effectiveAt.Add(-24 * time.Hour), ValidTo: &ended,
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err = instrumentRepo.RegisterVenueContract(ctx, contract)
	if err != nil {
		t.Fatal(err)
	}
	terms, err := instrument.NewOptionContractTerms(instrument.OptionContractTermsInput{
		OptionInstrumentID: option.ID, UnderlyingInstrumentID: underlying.ID,
		ContractType: instrument.OptionContractCall, StrikePrice: decimal.NewFromInt(125), StrikeCurrency: "USD",
		DeliverableQuantity: decimal.NewFromInt(100), Source: "occ-feed", SourceNamespace: "terms/repository",
		SourceRecordID: "terms-" + suffix, SourceRevision: "v1",
		EffectiveAt: effectiveAt.Add(-24 * time.Hour), ObservedAt: effectiveAt.Add(-23 * time.Hour),
		RawPayload: json.RawMessage(`{"terms":"` + suffix + `"}`), Metadata: json.RawMessage(`{"authority":"occ"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	terms, err = instrumentRepo.RegisterOptionContractTerms(ctx, terms)
	if err != nil {
		t.Fatal(err)
	}
	source, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID: account.ID, Source: "clearing", SourceNamespace: "option/settlement",
		SourceEventID: "exercise-" + suffix, SourceRevision: "v1", ObservedAt: effectiveAt.Add(time.Second),
		RawPayload: json.RawMessage(`{"exercise":"` + suffix + `"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	base := ledger.EconomicNormalizationBaseInput{
		SourceEvent: source, Account: account, NormalizerVersion: "economic_event_v1",
		ExecutionOriginType: ledger.ExecutionOriginSettlement, ExecutionOriginID: "settlement-batch-1",
		ReferenceType: "settlement", ReferenceID: "exercise-" + suffix, EffectiveAt: effectiveAt,
	}
	normalization, err := ledger.NewPhysicalOptionEconomicNormalization(ledger.PhysicalOptionEconomicEventInput{
		Base: base, Action: ledger.PhysicalOptionExercise,
		OptionInstrument: *option, UnderlyingInstrument: *underlying,
		VenueContract: *contract, OptionTerms: *terms, PositionQuantity: decimal.NewFromInt(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	return physicalEconomicLedgerFixture{
		account: account, option: option, underlying: underlying, contract: contract,
		terms: terms, source: source, normalization: normalization,
	}
}

func newChangedOptionTerms(
	t *testing.T,
	fixture physicalEconomicLedgerFixture,
	suffix string,
	effectiveAt, observedAt time.Time,
) *instrument.OptionContractTerms {
	t.Helper()
	terms, err := instrument.NewOptionContractTerms(instrument.OptionContractTermsInput{
		OptionInstrumentID: fixture.option.ID, UnderlyingInstrumentID: fixture.underlying.ID,
		ContractType: instrument.OptionContractCall, StrikePrice: decimal.NewFromInt(125), StrikeCurrency: "USD",
		DeliverableQuantity: decimal.NewFromInt(50), Source: "occ-feed", SourceNamespace: "terms/repository",
		SourceRecordID: "terms-" + suffix, SourceRevision: "v1",
		EffectiveAt: effectiveAt, ObservedAt: observedAt,
		RawPayload: json.RawMessage(`{"terms":"` + suffix + `"}`), Metadata: json.RawMessage(`{"authority":"occ"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return terms
}

func assertRepositoryPostingAmount(t *testing.T, transaction *ledger.Transaction, key, want string) {
	t.Helper()
	for _, posting := range transaction.Postings {
		if posting.IdempotencyKey == key {
			if posting.Amount.String() != want {
				t.Fatalf("posting %q amount = %s, want %s", key, posting.Amount, want)
			}
			return
		}
	}
	t.Fatalf("posting %q not found", key)
}

func newLedgerIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping ledger integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "integration_ledger_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create ledger integration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop ledger integration schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	migrationDirectory := filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations")
	entries, err := os.ReadDir(migrationDirectory)
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}
	migrationNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasSuffix(name, ".up.sql") && name <= "000068_economic_events.up.sql" {
			migrationNames = append(migrationNames, name)
		}
	}
	sort.Strings(migrationNames)
	for _, migrationName := range migrationNames {
		migration, err := os.ReadFile(filepath.Join(migrationDirectory, migrationName))
		if err != nil {
			t.Fatalf("read %s: %v", migrationName, err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", migrationName, err)
		}
	}
	return pool
}
