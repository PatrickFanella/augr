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

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestQuoteSnapshotRepoPersistsExactObservationAndDepth(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	value, contract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	exchangeAt := time.Date(2026, 8, 15, 16, 0, 0, 123456000, time.UTC)
	receivedAt := exchangeAt.Add(25 * time.Millisecond)
	availableAt := receivedAt.Add(time.Millisecond)
	sequence := int64(42)
	bid := decimal.RequireFromString("100.12")
	bidSize := decimal.NewFromInt(12)
	ask := decimal.RequireFromString("100.38")
	askSize := decimal.NewFromInt(10)
	last := decimal.RequireFromString("100.25")
	mark := decimal.RequireFromString("100.250000000001")

	snapshot, err := marketdata.NewQuoteSnapshot(marketdata.QuoteSnapshotInput{
		InstrumentID:         value.ID,
		VenueContractID:      &contract.ID,
		Provider:             "test-provider",
		Venue:                "test-venue",
		Source:               "test-feed",
		ObservationNamespace: "quotes/test/one",
		ObservationID:        "observation-42",
		SourceRevision:       "revision-a",
		SourceSequence:       &sequence,
		ExchangeAt:           &exchangeAt,
		ReceivedAt:           receivedAt,
		AvailableAt:          &availableAt,
		Bid:                  &bid,
		BidSize:              &bidSize,
		Ask:                  &ask,
		AskSize:              &askSize,
		Last:                 &last,
		Mark:                 &mark,
		MarketStatus:         "open",
		SessionStatus:        "regular",
		Bids: []marketdata.DepthLevelInput{
			{Price: bid, Size: bidSize},
			{Price: decimal.RequireFromString("100.00"), Size: decimal.NewFromInt(20)},
		},
		Asks: []marketdata.DepthLevelInput{
			{Price: ask, Size: askSize},
			{Price: decimal.RequireFromString("100.50"), Size: decimal.NewFromInt(30)},
		},
		Metadata: json.RawMessage(`{"clock":"exchange"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	persisted, err := repo.RecordQuoteSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("RecordQuoteSnapshot() error = %v", err)
	}
	if persisted.ID != snapshot.ID || persisted.IngestSequence <= 0 || persisted.InstrumentID != value.ID ||
		persisted.VenueContractID == nil || *persisted.VenueContractID != contract.ID ||
		persisted.SourceSequence == nil || *persisted.SourceSequence != sequence ||
		persisted.Bid == nil || !persisted.Bid.Equal(bid) || persisted.Mark == nil || !persisted.Mark.Equal(mark) ||
		len(persisted.Depth) != 4 || persisted.Depth[0].Side != marketdata.DepthSideBid ||
		persisted.Depth[2].Side != marketdata.DepthSideAsk {
		t.Fatalf("persisted snapshot = %+v", persisted)
	}
	if err := persisted.ValidateAgainstVenueContract(*contract); err != nil {
		t.Fatalf("persisted exact venue mechanics = %v", err)
	}
}

func TestQuoteSnapshotRepoRetainsNonExecutableVenueMechanicsEvidence(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	value, contract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	tests := []struct {
		name   string
		modify func(*marketdata.QuoteSnapshotInput)
		want   marketdata.AssessmentCode
	}{
		{name: "valid exact multiples", modify: func(_ *marketdata.QuoteSnapshotInput) {}},
		{name: "off tick top of book", modify: func(input *marketdata.QuoteSnapshotInput) {
			bid := decimal.RequireFromString("100.125")
			input.Bid = &bid
			input.Bids[0].Price = bid
		}, want: marketdata.AssessmentInvalidPriceTick},
		{name: "off tick depth", modify: func(input *marketdata.QuoteSnapshotInput) {
			input.Bids = append(input.Bids, marketdata.DepthLevelInput{
				Price: decimal.RequireFromString("100.115"),
				Size:  decimal.NewFromInt(1),
			})
		}, want: marketdata.AssessmentInvalidPriceTick},
		{name: "off lot depth size", modify: func(input *marketdata.QuoteSnapshotInput) {
			input.Bids = append(input.Bids, marketdata.DepthLevelInput{
				Price: decimal.RequireFromString("100.11"),
				Size:  decimal.RequireFromString("1.5"),
			})
		}, want: marketdata.AssessmentInvalidLotSize},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "mechanics-"+strings.ReplaceAll(test.name, " ", "-"))
			test.modify(&input)
			snapshot, err := marketdata.NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := repo.RecordQuoteSnapshot(ctx, snapshot)
			if err != nil {
				t.Fatalf("RecordQuoteSnapshot() must retain attributable evidence: %v", err)
			}
			loaded, err := repo.GetQuoteSnapshotByID(ctx, persisted.ID)
			if err != nil {
				t.Fatal(err)
			}
			mechanicsErr := loaded.ValidateAgainstVenueContract(*contract)
			if test.want == "" {
				if mechanicsErr != nil {
					t.Fatalf("valid venue mechanics = %v", mechanicsErr)
				}
				return
			}
			var assessmentErr *marketdata.AssessmentError
			if !errors.As(mechanicsErr, &assessmentErr) || assessmentErr.Code != test.want {
				t.Fatalf("venue mechanics error = %v, want %s", mechanicsErr, test.want)
			}
		})
	}
}

func TestQuoteSnapshotRepoReplaysIdenticalObservation(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	value, contract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	input := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "replay")
	input.Metadata = json.RawMessage(`{"provider":"test","sequence":42}`)
	first, err := marketdata.NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.RecordQuoteSnapshot(ctx, first)
	if err != nil {
		t.Fatal(err)
	}

	input.Metadata = json.RawMessage(`{"sequence":42,"provider":"test"}`)
	retry, err := marketdata.NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.RecordQuoteSnapshot(ctx, retry)
	if err != nil {
		t.Fatalf("RecordQuoteSnapshot(retry) error = %v", err)
	}
	if replayed.ID != persisted.ID || replayed.IngestSequence != persisted.IngestSequence {
		t.Fatalf("replayed identity = %s/%d, want %s/%d", replayed.ID, replayed.IngestSequence, persisted.ID, persisted.IngestSequence)
	}
}

func TestQuoteSnapshotRepoRejectsMismatchedObservationReuse(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	value, contract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	input := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "conflict")
	first, err := marketdata.NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordQuoteSnapshot(ctx, first); err != nil {
		t.Fatal(err)
	}

	input.Source = "different-feed"
	conflict, err := marketdata.NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordQuoteSnapshot(ctx, conflict); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("RecordQuoteSnapshot(conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestQuoteSnapshotRepoAllowsSameIDForDistinctInstrumentsNamespacesOrRevisions(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	firstInstrument, firstContract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	secondInstrument, secondContract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	inputs := []marketdata.QuoteSnapshotInput{
		quoteSnapshotRepositoryFixtureInput(firstInstrument.ID, firstContract.ID, "shared"),
		quoteSnapshotRepositoryFixtureInput(firstInstrument.ID, firstContract.ID, "shared"),
		quoteSnapshotRepositoryFixtureInput(firstInstrument.ID, firstContract.ID, "shared"),
		quoteSnapshotRepositoryFixtureInput(secondInstrument.ID, secondContract.ID, "shared"),
	}
	inputs[1].ObservationNamespace = "quotes/test/two"
	inputs[2].SourceRevision = "correction-1"

	ids := make(map[uuid.UUID]struct{}, len(inputs))
	for index := range inputs {
		snapshot, err := marketdata.NewQuoteSnapshot(inputs[index])
		if err != nil {
			t.Fatal(err)
		}
		persisted, err := repo.RecordQuoteSnapshot(ctx, snapshot)
		if err != nil {
			t.Fatalf("RecordQuoteSnapshot(scope %d) error = %v", index, err)
		}
		ids[persisted.ID] = struct{}{}
	}
	if len(ids) != len(inputs) {
		t.Fatalf("distinct persisted IDs = %d, want %d", len(ids), len(inputs))
	}
}

func TestQuoteSnapshotRepoValidatesVenueContract(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*marketdata.QuoteSnapshotInput, *instrument.Instrument, *instrument.VenueContract, *instrument.Instrument)
	}{
		{name: "wrong instrument", modify: func(input *marketdata.QuoteSnapshotInput, _ *instrument.Instrument, _ *instrument.VenueContract, other *instrument.Instrument) {
			input.InstrumentID = other.ID
		}},
		{name: "wrong venue", modify: func(input *marketdata.QuoteSnapshotInput, _ *instrument.Instrument, _ *instrument.VenueContract, _ *instrument.Instrument) {
			input.Venue = "other-venue"
		}},
		{name: "expired", modify: func(input *marketdata.QuoteSnapshotInput, _ *instrument.Instrument, contract *instrument.VenueContract, _ *instrument.Instrument) {
			exchangeAt := contract.ValidTo.Add(time.Microsecond)
			receivedAt := exchangeAt.Add(time.Millisecond)
			availableAt := receivedAt.Add(time.Millisecond)
			input.ExchangeAt, input.ReceivedAt, input.AvailableAt = &exchangeAt, receivedAt, &availableAt
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			pool := newQuoteSnapshotIntegrationPool(t, ctx)
			value, contract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
			other, _ := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
			input := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "contract-"+test.name)
			test.modify(&input, value, contract, other)
			snapshot, err := marketdata.NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewQuoteSnapshotRepo(pool).RecordQuoteSnapshot(ctx, snapshot); err == nil {
				t.Fatal("RecordQuoteSnapshot() accepted invalid venue contract")
			} else if errors.Is(err, repository.ErrIdempotencyConflict) {
				t.Fatalf("venue contract error was misclassified as idempotency conflict: %v", err)
			}
		})
	}
}

func TestQuoteSnapshotRepoLatestAtUsesAvailabilityNotExchangeOrImportTime(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	value, contract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	base := time.Date(2026, 8, 15, 2, 0, 0, 0, time.UTC)

	firstInput := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "available-first")
	firstExchange := base
	firstReceive := base.Add(time.Second)
	firstAvailable := base.Add(2 * time.Second)
	firstInput.ExchangeAt, firstInput.ReceivedAt, firstInput.AvailableAt = &firstExchange, firstReceive, &firstAvailable
	first, err := marketdata.NewQuoteSnapshot(firstInput)
	if err != nil {
		t.Fatal(err)
	}
	first, err = repo.RecordQuoteSnapshot(ctx, first)
	if err != nil {
		t.Fatal(err)
	}

	laterInput := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "available-later")
	laterExchange := base.Add(-time.Hour)
	laterReceive := base.Add(3 * time.Second)
	laterAvailable := base.Add(4 * time.Second)
	laterInput.ExchangeAt, laterInput.ReceivedAt, laterInput.AvailableAt = &laterExchange, laterReceive, &laterAvailable
	later, err := marketdata.NewQuoteSnapshot(laterInput)
	if err != nil {
		t.Fatal(err)
	}
	later, err = repo.RecordQuoteSnapshot(ctx, later)
	if err != nil {
		t.Fatal(err)
	}

	unknownInput := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "unknown-availability")
	unknownInput.AvailableAt = nil
	unknown, err := marketdata.NewQuoteSnapshot(unknownInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordQuoteSnapshot(ctx, unknown); err != nil {
		t.Fatal(err)
	}

	selector, err := marketdata.NewQuoteSelector(value.ID, "test-provider", "test-venue", "quotes/test/one", base.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := repo.LatestQuoteSnapshotAt(ctx, selector)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != first.ID {
		t.Fatalf("selected before later availability = %s, want %s", selected.ID, first.ID)
	}
	selector.AsOf = base.Add(5 * time.Second)
	selected, err = repo.LatestQuoteSnapshotAt(ctx, selector)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != later.ID {
		t.Fatalf("selected after later availability = %s, want %s", selected.ID, later.ID)
	}
}

func TestQuoteSnapshotRepoLatestAtDeterministicallyOrdersSameTimeObservations(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	value, contract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	availableAt := time.Date(2026, 8, 15, 3, 0, 1, 0, time.UTC)
	var expected *marketdata.QuoteSnapshot
	for _, sequence := range []int64{1, 2} {
		input := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "sequence-"+decimal.NewFromInt(sequence).String())
		exchangeAt := availableAt.Add(-2 * time.Second)
		receivedAt := availableAt.Add(-time.Second)
		input.ExchangeAt, input.ReceivedAt, input.AvailableAt = &exchangeAt, receivedAt, &availableAt
		input.SourceSequence = &sequence
		snapshot, err := marketdata.NewQuoteSnapshot(input)
		if err != nil {
			t.Fatal(err)
		}
		persisted, err := repo.RecordQuoteSnapshot(ctx, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if sequence == 2 {
			expected = persisted
		}
	}
	selector, err := marketdata.NewQuoteSelector(value.ID, "test-provider", "test-venue", "quotes/test/one", availableAt)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := repo.LatestQuoteSnapshotAt(ctx, selector)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != expected.ID {
		t.Fatalf("same-time selected = %s, want highest source sequence %s", selected.ID, expected.ID)
	}
}

func TestQuoteSnapshotRepoLoadsMissingFieldsAsNilNotZero(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	value, _ := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	input := marketdata.QuoteSnapshotInput{
		InstrumentID:         value.ID,
		Provider:             "test-provider",
		Venue:                "test-venue",
		ObservationNamespace: "quotes/test/missing",
		ObservationID:        "missing",
		ReceivedAt:           time.Date(2026, 8, 15, 4, 0, 0, 0, time.UTC),
	}
	snapshot, err := marketdata.NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.RecordQuoteSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Bid != nil || persisted.Ask != nil || persisted.AvailableAt != nil || persisted.VenueContractID != nil {
		t.Fatalf("missing fields became values: %+v", persisted)
	}

	zero := decimal.Zero
	input.ObservationID = "zero"
	input.Bid, input.Ask = &zero, &zero
	withZero, err := marketdata.NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	withZero, err = repo.RecordQuoteSnapshot(ctx, withZero)
	if err != nil {
		t.Fatal(err)
	}
	if withZero.Bid == nil || withZero.Ask == nil || !withZero.Bid.IsZero() || !withZero.Ask.IsZero() {
		t.Fatalf("present zero fields became missing: %+v", withZero)
	}
}

func TestQuoteSnapshotRepoConcurrentIdenticalAppendCreatesOneSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	value, contract := createQuoteSnapshotInstrumentFixture(t, ctx, pool)
	repo := NewQuoteSnapshotRepo(pool)
	input := quoteSnapshotRepositoryFixtureInput(value.ID, contract.ID, "concurrent")
	const writers = 8
	ids := make(chan uuid.UUID, writers)
	errorsSeen := make(chan error, writers)
	var wait sync.WaitGroup
	wait.Add(writers)
	for range writers {
		go func() {
			defer wait.Done()
			snapshot, err := marketdata.NewQuoteSnapshot(input)
			if err != nil {
				errorsSeen <- err
				return
			}
			persisted, err := repo.RecordQuoteSnapshot(ctx, snapshot)
			if err != nil {
				errorsSeen <- err
				return
			}
			ids <- persisted.ID
		}()
	}
	wait.Wait()
	close(ids)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("concurrent RecordQuoteSnapshot() error = %v", err)
	}
	var expected uuid.UUID
	for id := range ids {
		if expected == uuid.Nil {
			expected = id
		} else if id != expected {
			t.Fatalf("concurrent persisted IDs = %s and %s", expected, id)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM quote_snapshots
		WHERE instrument_id = $1 AND observation_id = 'concurrent'`, value.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent snapshot count = %d, want 1", count)
	}
}

func TestQuoteSnapshotRepoRejectsUnknownInstrument(t *testing.T) {
	ctx := context.Background()
	pool := newQuoteSnapshotIntegrationPool(t, ctx)
	input := marketdata.QuoteSnapshotInput{
		InstrumentID:         uuid.New(),
		Provider:             "test-provider",
		Venue:                "test-venue",
		ObservationNamespace: "quotes/test/unknown",
		ObservationID:        "unknown-instrument",
		ReceivedAt:           time.Date(2026, 8, 15, 5, 0, 0, 0, time.UTC),
	}
	snapshot, err := marketdata.NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewQuoteSnapshotRepo(pool).RecordQuoteSnapshot(ctx, snapshot); err == nil {
		t.Fatal("RecordQuoteSnapshot() accepted unknown instrument")
	} else if errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("foreign-key error was misclassified as idempotency conflict: %v", err)
	}
}

func quoteSnapshotRepositoryFixtureInput(instrumentID, venueContractID uuid.UUID, observationID string) marketdata.QuoteSnapshotInput {
	exchangeAt := time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
	receivedAt := exchangeAt.Add(25 * time.Millisecond)
	availableAt := receivedAt.Add(time.Millisecond)
	sequence := int64(42)
	bid := decimal.RequireFromString("100.12")
	ask := decimal.RequireFromString("100.38")
	return marketdata.QuoteSnapshotInput{
		InstrumentID:         instrumentID,
		VenueContractID:      &venueContractID,
		Provider:             "test-provider",
		Venue:                "test-venue",
		Source:               "test-feed",
		ObservationNamespace: "quotes/test/one",
		ObservationID:        observationID,
		SourceSequence:       &sequence,
		ExchangeAt:           &exchangeAt,
		ReceivedAt:           receivedAt,
		AvailableAt:          &availableAt,
		Bid:                  &bid,
		Ask:                  &ask,
		MarketStatus:         "open",
		SessionStatus:        "regular",
		Bids:                 []marketdata.DepthLevelInput{{Price: bid, Size: decimal.NewFromInt(5)}},
		Asks:                 []marketdata.DepthLevelInput{{Price: ask, Size: decimal.NewFromInt(6)}},
		Metadata:             json.RawMessage(`{"source":"fixture"}`),
	}
}

func createQuoteSnapshotInstrumentFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*instrument.Instrument, *instrument.VenueContract) {
	t.Helper()
	repo := NewInstrumentRepo(pool)
	value, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey:      "figi:test:" + uuid.NewString(),
		AssetClass:       instrument.AssetClassEquity,
		PrimaryVenue:     "test-venue",
		Currency:         "USD",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical,
		Status:           instrument.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repo.CreateInstrument(ctx, value)
	if err != nil {
		t.Fatal(err)
	}
	validFrom := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	validTo := validFrom.Add(48 * time.Hour)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID:     value.ID,
		Venue:            "test-venue",
		ContractID:       "TEST-CONTRACT-" + uuid.NewString(),
		Currency:         "USD",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical,
		ValidFrom:        validFrom,
		ValidTo:          &validTo,
	})
	if err != nil {
		t.Fatal(err)
	}
	contract, err = repo.RegisterVenueContract(ctx, contract)
	if err != nil {
		t.Fatal(err)
	}
	return value, contract
}

func newQuoteSnapshotIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping quote snapshot repository integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping quote snapshot repository integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "integration_quote_snapshot_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create quote snapshot integration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop quote snapshot integration schema: %v", err)
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
		if !entry.IsDir() && strings.HasSuffix(name, ".up.sql") && name <= "000067_quote_depth_snapshots.up.sql" {
			migrationNames = append(migrationNames, name)
		}
	}
	sort.Strings(migrationNames)
	if len(migrationNames) == 0 || migrationNames[len(migrationNames)-1] != "000067_quote_depth_snapshots.up.sql" {
		t.Fatal("migration 67 was not found")
	}
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
