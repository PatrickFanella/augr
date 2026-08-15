package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestInstrumentRepoCreatesAndLoadsExactMechanics(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)

	want, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey:      "figi:BBG000B9XRY4",
		AssetClass:       instrument.AssetClassEquity,
		PrimaryVenue:     "NASDAQ",
		Currency:         "usd",
		TickSize:         decimal.RequireFromString("0.00000001"),
		LotSize:          decimal.RequireFromString("0.125"),
		Multiplier:       decimal.RequireFromString("1.5"),
		SettlementMethod: instrument.SettlementPhysical,
		Status:           instrument.StatusActive,
		Metadata:         json.RawMessage(`{"figi":"BBG000B9XRY4"}`),
	})
	if err != nil {
		t.Fatalf("NewInstrument() error = %v", err)
	}
	created, err := repo.CreateInstrument(ctx, want)
	if err != nil {
		t.Fatalf("CreateInstrument() error = %v", err)
	}
	got, err := repo.GetInstrumentByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetInstrumentByID() error = %v", err)
	}
	if got.ID != want.ID || got.IdentityKey != want.IdentityKey ||
		!got.TickSize.Equal(want.TickSize) || !got.LotSize.Equal(want.LotSize) ||
		!got.Multiplier.Equal(want.Multiplier) || !jsonBytesEqual(got.Metadata, want.Metadata) {
		t.Fatalf("loaded instrument = %+v, want exact mechanics from %+v", got, want)
	}
}

func TestInstrumentRepoResolvesAliasByHistoricalTime(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)
	first := createInstrumentFixture(t, ctx, repo, "figi:test:first")
	second := createInstrumentFixture(t, ctx, repo, "figi:test:second")

	t0 := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	t2 := t1.Add(24 * time.Hour)
	for _, input := range []instrument.AliasEventInput{
		{InstrumentID: first.ID, Provider: "test", AliasType: instrument.AliasTicker, AliasValue: "XYZ", Action: instrument.AliasAssigned, EffectiveAt: t0, Source: "test"},
		{InstrumentID: first.ID, Provider: "test", AliasType: instrument.AliasTicker, AliasValue: "XYZ", Action: instrument.AliasRetired, EffectiveAt: t1, Source: "test"},
		{InstrumentID: second.ID, Provider: "test", AliasType: instrument.AliasTicker, AliasValue: "XYZ", Action: instrument.AliasAssigned, EffectiveAt: t2, Source: "test"},
	} {
		event, err := instrument.NewAliasEvent(input)
		if err != nil {
			t.Fatalf("NewAliasEvent() error = %v", err)
		}
		if _, err := repo.AppendAliasEvent(ctx, event); err != nil {
			t.Fatalf("AppendAliasEvent(%s) error = %v", event.Action, err)
		}
	}

	atT0, err := repo.ResolveAlias(ctx, " TEST ", instrument.AliasTicker, "xyz", t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("ResolveAlias(at t0) error = %v", err)
	}
	between, betweenErr := repo.ResolveAlias(ctx, "test", instrument.AliasTicker, "XYZ", t1.Add(time.Hour))
	atT2, err := repo.ResolveAlias(ctx, "test", instrument.AliasTicker, "XYZ", t2.Add(time.Hour))
	if err != nil {
		t.Fatalf("ResolveAlias(at t2) error = %v", err)
	}
	if atT0.ID != first.ID || between != nil || !errors.Is(betweenErr, repository.ErrNotFound) || atT2.ID != second.ID {
		t.Fatalf("historical identity changed: %s / %v %v / %s", atT0.ID, between, betweenErr, atT2.ID)
	}
}

func TestInstrumentRepoRejectsAliasRebindWithoutRetirement(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)
	first := createInstrumentFixture(t, ctx, repo, "figi:test:rebind-first")
	second := createInstrumentFixture(t, ctx, repo, "figi:test:rebind-second")
	t0 := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)

	assigned, err := instrument.NewAliasEvent(instrument.AliasEventInput{
		InstrumentID: first.ID,
		Provider:     "test",
		AliasType:    instrument.AliasTicker,
		AliasValue:   "REUSE",
		Action:       instrument.AliasAssigned,
		EffectiveAt:  t0,
		Source:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendAliasEvent(ctx, assigned); err != nil {
		t.Fatalf("AppendAliasEvent(first assignment) error = %v", err)
	}

	rebind, err := instrument.NewAliasEvent(instrument.AliasEventInput{
		InstrumentID: second.ID,
		Provider:     "test",
		AliasType:    instrument.AliasTicker,
		AliasValue:   "REUSE",
		Action:       instrument.AliasAssigned,
		EffectiveAt:  t0.Add(time.Hour),
		Source:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendAliasEvent(ctx, rebind); err == nil {
		t.Fatal("AppendAliasEvent(rebind without retirement) unexpectedly succeeded")
	}

	resolved, err := repo.ResolveAlias(ctx, "test", instrument.AliasTicker, "REUSE", t0.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ResolveAlias() after rejected rebind error = %v", err)
	}
	if resolved.ID != first.ID {
		t.Fatalf("resolved instrument = %s, want original %s", resolved.ID, first.ID)
	}
}

func TestInstrumentRepoReplaysIdenticalAliasEvent(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)
	value := createInstrumentFixture(t, ctx, repo, "figi:test:alias-replay")
	input := instrument.AliasEventInput{
		InstrumentID: value.ID,
		Provider:     "test",
		AliasType:    instrument.AliasFIGI,
		AliasValue:   "bbg-test-replay",
		Action:       instrument.AliasAssigned,
		EffectiveAt:  time.Date(2026, time.February, 2, 0, 0, 0, 0, time.UTC),
		Source:       "test-feed",
	}
	first, err := instrument.NewAliasEvent(input)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.AppendAliasEvent(ctx, first)
	if err != nil {
		t.Fatalf("AppendAliasEvent(first) error = %v", err)
	}

	retry, err := instrument.NewAliasEvent(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.AppendAliasEvent(ctx, retry)
	if err != nil {
		t.Fatalf("AppendAliasEvent(retry) error = %v", err)
	}
	if replayed.ID != persisted.ID {
		t.Fatalf("replayed alias ID = %s, want %s", replayed.ID, persisted.ID)
	}

	input.Source = "different-feed"
	conflict, err := instrument.NewAliasEvent(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendAliasEvent(ctx, conflict); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("AppendAliasEvent(conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestInstrumentRepoRegistersNonOverlappingVenueContract(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)
	value := createInstrumentFixture(t, ctx, repo, "figi:test:venue-contract")
	validFrom := time.Date(2026, time.February, 3, 0, 0, 0, 0, time.UTC)
	validTo := validFrom.Add(24 * time.Hour)
	input := instrument.VenueContractInput{
		InstrumentID:     value.ID,
		Venue:            "test-venue",
		ContractID:       "contract-1",
		Currency:         "USD",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical,
		ValidFrom:        validFrom,
		ValidTo:          &validTo,
		Metadata:         json.RawMessage(`{"source":"venue-spec"}`),
	}
	first, err := instrument.NewVenueContract(input)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.RegisterVenueContract(ctx, first)
	if err != nil {
		t.Fatalf("RegisterVenueContract(first) error = %v", err)
	}
	if persisted.ContractID != "CONTRACT-1" || !persisted.TickSize.Equal(first.TickSize) {
		t.Fatalf("persisted venue contract = %+v", persisted)
	}

	retry, err := instrument.NewVenueContract(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.RegisterVenueContract(ctx, retry)
	if err != nil {
		t.Fatalf("RegisterVenueContract(retry) error = %v", err)
	}
	if replayed.ID != persisted.ID {
		t.Fatalf("replayed venue contract ID = %s, want %s", replayed.ID, persisted.ID)
	}

	overlapInput := input
	overlapInput.ValidFrom = validFrom.Add(time.Hour)
	overlap, err := instrument.NewVenueContract(overlapInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RegisterVenueContract(ctx, overlap); err == nil {
		t.Fatal("RegisterVenueContract(overlap) unexpectedly succeeded")
	}
}

func TestInstrumentRepoReplaysIdenticalCorporateAction(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)
	value := createInstrumentFixture(t, ctx, repo, "figi:test:split-replay")
	input := instrument.CorporateActionInput{
		InstrumentID:     value.ID,
		ActionType:       instrument.CorporateActionSplit,
		EffectiveAt:      time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		RatioNumerator:   decimal.NewFromInt(2),
		RatioDenominator: decimal.NewFromInt(1),
		Source:           "issuer-feed",
		SourceEventID:    "split-replay-1",
		Metadata:         json.RawMessage(`{"issuer":"test"}`),
	}
	first, err := instrument.NewCorporateAction(input)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.RecordCorporateAction(ctx, first)
	if err != nil {
		t.Fatalf("RecordCorporateAction(first) error = %v", err)
	}

	retry, err := instrument.NewCorporateAction(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.RecordCorporateAction(ctx, retry)
	if err != nil {
		t.Fatalf("RecordCorporateAction(retry) error = %v", err)
	}
	if replayed.ID != persisted.ID || !replayed.RatioNumerator.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("replayed action = %+v, want persisted ID %s", replayed, persisted.ID)
	}
}

func TestInstrumentRepoRejectsCorporateActionPayloadConflict(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)
	value := createInstrumentFixture(t, ctx, repo, "figi:test:split-conflict")
	input := instrument.CorporateActionInput{
		InstrumentID:     value.ID,
		ActionType:       instrument.CorporateActionSplit,
		EffectiveAt:      time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		RatioNumerator:   decimal.NewFromInt(2),
		RatioDenominator: decimal.NewFromInt(1),
		Source:           "issuer-feed",
		SourceEventID:    "split-conflict-1",
	}
	first, err := instrument.NewCorporateAction(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordCorporateAction(ctx, first); err != nil {
		t.Fatalf("RecordCorporateAction(first) error = %v", err)
	}

	input.RatioNumerator = decimal.NewFromInt(3)
	conflict, err := instrument.NewCorporateAction(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordCorporateAction(ctx, conflict); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("RecordCorporateAction(conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestOptionTermsRepoPersistsExactTermsAndReplaysIdentically(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)
	option, underlying := createPhysicalOptionTermsFixture(t, ctx, repo)
	input := instrument.OptionContractTermsInput{
		OptionInstrumentID: option.ID, UnderlyingInstrumentID: underlying.ID,
		ContractType: instrument.OptionContractCall, StrikePrice: decimal.RequireFromString("125.50"),
		StrikeCurrency: "USD", DeliverableQuantity: decimal.NewFromInt(100),
		Source: "occ-feed", SourceNamespace: "terms/test", SourceRecordID: "terms-1", SourceRevision: "v1",
		EffectiveAt: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		ObservedAt:  time.Date(2026, time.August, 1, 0, 0, 1, 0, time.UTC),
		RawPayload:  json.RawMessage(" {\n\"id\":\"terms-1\",\"strike\":\"125.50\"\n} "),
		Metadata:    json.RawMessage(`{"authority":"occ"}`),
	}
	first, err := instrument.NewOptionContractTerms(input)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.RegisterOptionContractTerms(ctx, first)
	if err != nil {
		t.Fatalf("RegisterOptionContractTerms() error = %v", err)
	}
	loaded, err := repo.GetOptionContractTermsByID(ctx, persisted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !instrument.SameOptionContractTermsPayload(persisted, loaded) ||
		!bytes.Equal(loaded.RawPayload, input.RawPayload) {
		t.Fatalf("loaded terms lost exact payload: %+v", loaded)
	}

	retry, err := instrument.NewOptionContractTerms(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.RegisterOptionContractTerms(ctx, retry)
	if err != nil {
		t.Fatalf("RegisterOptionContractTerms(retry) error = %v", err)
	}
	if replayed.ID != persisted.ID {
		t.Fatalf("replayed terms ID = %s, want %s", replayed.ID, persisted.ID)
	}
}

func TestOptionTermsRepoRejectsRevisionOrEconomicConflict(t *testing.T) {
	ctx := context.Background()
	pool := newInstrumentIntegrationPool(t, ctx)
	repo := NewInstrumentRepo(pool)
	option, underlying := createPhysicalOptionTermsFixture(t, ctx, repo)
	input := instrument.OptionContractTermsInput{
		OptionInstrumentID: option.ID, UnderlyingInstrumentID: underlying.ID,
		ContractType: instrument.OptionContractPut, StrikePrice: decimal.NewFromInt(100),
		StrikeCurrency: "USD", DeliverableQuantity: decimal.NewFromInt(100),
		Source: "occ-feed", SourceNamespace: "terms/test", SourceRecordID: "terms-conflict", SourceRevision: "v1",
		EffectiveAt: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		ObservedAt:  time.Date(2026, time.August, 1, 0, 0, 1, 0, time.UTC),
		RawPayload:  json.RawMessage(`{"id":"terms-conflict"}`),
	}
	first, err := instrument.NewOptionContractTerms(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RegisterOptionContractTerms(ctx, first); err != nil {
		t.Fatal(err)
	}

	input.SourceRevision = "v2"
	input.StrikePrice = decimal.NewFromInt(101)
	input.RawPayload = json.RawMessage(`{"id":"terms-conflict","revision":2}`)
	conflict, err := instrument.NewOptionContractTerms(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RegisterOptionContractTerms(ctx, conflict); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("RegisterOptionContractTerms(conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func createPhysicalOptionTermsFixture(t *testing.T, ctx context.Context, repo *InstrumentRepo) (*instrument.Instrument, *instrument.Instrument) {
	t.Helper()
	underlying := createInstrumentFixture(t, ctx, repo, "figi:test:option-underlying:"+uuid.NewString())
	expiration := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
	option, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey: "occ:test:" + uuid.NewString(), AssetClass: instrument.AssetClassOption,
		PrimaryVenue: "test", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: decimal.NewFromInt(1), Multiplier: decimal.NewFromInt(100), Expiration: &expiration,
		ExerciseStyle: instrument.ExerciseAmerican, SettlementMethod: instrument.SettlementPhysical,
		UnderlyingID: &underlying.ID, Status: instrument.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.CreateInstrument(ctx, option)
	if err != nil {
		t.Fatal(err)
	}
	return persisted, underlying
}

func createInstrumentFixture(t *testing.T, ctx context.Context, repo *InstrumentRepo, identityKey string) *instrument.Instrument {
	t.Helper()
	value, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey:      identityKey,
		AssetClass:       instrument.AssetClassEquity,
		PrimaryVenue:     "test",
		Currency:         "USD",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical,
		Status:           instrument.StatusActive,
	})
	if err != nil {
		t.Fatalf("NewInstrument(%q) error = %v", identityKey, err)
	}
	created, err := repo.CreateInstrument(ctx, value)
	if err != nil {
		t.Fatalf("CreateInstrument(%q) error = %v", identityKey, err)
	}
	return created
}

func newInstrumentIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping instrument repository integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping instrument repository integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "integration_instrument_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create instrument integration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop instrument integration schema: %v", err)
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
	if len(migrationNames) == 0 || migrationNames[len(migrationNames)-1] != "000068_economic_events.up.sql" {
		t.Fatal("migration 68 was not found")
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
