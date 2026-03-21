package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// ---------------------------------------------------------------------------
// Unit tests for buildAuditLogQuery
// ---------------------------------------------------------------------------

func TestBuildAuditLogQuery_NoFilters(t *testing.T) {
	query, args := buildAuditLogQuery(repository.AuditLogFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}

	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM audit_log")
	assertContains(t, query, "ORDER BY created_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildAuditLogQuery_AllFilters(t *testing.T) {
	entityID := uuid.New()
	createdAfter := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	createdBefore := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)

	filter := repository.AuditLogFilter{
		EventType:     "order.created",
		EntityType:    "order",
		EntityID:      &entityID,
		Actor:         "system",
		CreatedAfter:  &createdAfter,
		CreatedBefore: &createdBefore,
	}

	query, args := buildAuditLogQuery(filter, 25, 50)

	// 6 filter args + limit + offset = 8
	if len(args) != 8 {
		t.Fatalf("expected 8 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "event_type = $1")
	assertContains(t, query, "entity_type = $2")
	assertContains(t, query, "entity_id = $3")
	assertContains(t, query, "actor = $4")
	assertContains(t, query, "created_at >= $5")
	assertContains(t, query, "created_at <= $6")
	assertContains(t, query, "LIMIT $7 OFFSET $8")
	assertContains(t, query, "WHERE")

	if args[0] != "order.created" {
		t.Errorf("expected event_type arg order.created, got %v", args[0])
	}

	if args[1] != "order" {
		t.Errorf("expected entity_type arg order, got %v", args[1])
	}

	if args[2] != &entityID {
		t.Errorf("expected entity_id arg %s, got %v", entityID, args[2])
	}

	if args[3] != "system" {
		t.Errorf("expected actor arg system, got %v", args[3])
	}
}

func TestBuildAuditLogQuery_PartialFilters(t *testing.T) {
	filter := repository.AuditLogFilter{
		EventType: "order.filled",
		Actor:     "broker",
	}

	query, args := buildAuditLogQuery(filter, 10, 0)

	// 2 filter args + limit + offset = 4
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "event_type = $1")
	assertContains(t, query, "actor = $2")
	assertNotContains(t, query, "entity_type =")
	assertNotContains(t, query, "entity_id =")
	assertNotContains(t, query, "created_at >=")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
}

// ---------------------------------------------------------------------------
// Unit tests for marshalDetails
// ---------------------------------------------------------------------------

func TestMarshalDetails_ValidJSON(t *testing.T) {
	input := json.RawMessage(`{"amount":100,"currency":"USD"}`)

	got, err := marshalDetails(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != `{"amount":100,"currency":"USD"}` {
		t.Errorf("expected details pass-through, got %s", got)
	}
}

func TestMarshalDetails_NilDefaultsToNull(t *testing.T) {
	got, err := marshalDetails(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != nil {
		t.Errorf("expected nil details, got %s", got)
	}
}

func TestMarshalDetails_EmptyDefaultsToNull(t *testing.T) {
	got, err := marshalDetails(json.RawMessage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != nil {
		t.Errorf("expected nil details, got %s", got)
	}
}

func TestMarshalDetails_InvalidJSON(t *testing.T) {
	_, err := marshalDetails(json.RawMessage(`{not valid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestAuditLogRepoIntegration_CreateAndQuery(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newAuditLogIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAuditLogRepo(pool)

	entityID := uuid.New()

	entry1 := &domain.AuditLogEntry{
		EventType:  "order.created",
		EntityType: "order",
		EntityID:   &entityID,
		Actor:      "system",
		Details:    json.RawMessage(`{"ticker":"AAPL","qty":10}`),
	}
	entry2 := &domain.AuditLogEntry{
		EventType:  "order.filled",
		EntityType: "order",
		EntityID:   &entityID,
		Actor:      "broker",
		Details:    json.RawMessage(`{"fill_price":185.25}`),
	}
	entry3 := &domain.AuditLogEntry{
		EventType: "pipeline.started",
		Actor:     "scheduler",
	}

	// Create entries and verify ID/CreatedAt are populated.
	for _, e := range []*domain.AuditLogEntry{entry1, entry2, entry3} {
		if err := repo.Create(ctx, e); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if e.ID == uuid.Nil {
			t.Fatal("expected Create() to populate ID")
		}
		if e.CreatedAt.IsZero() {
			t.Fatal("expected Create() to populate CreatedAt")
		}
	}

	// Query all entries.
	all, err := repo.Query(ctx, repository.AuditLogFilter{}, 100, 0)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(all))
	}

	// Filter by event_type.
	byType, err := repo.Query(ctx, repository.AuditLogFilter{EventType: "order.created"}, 100, 0)
	if err != nil {
		t.Fatalf("Query(EventType) error = %v", err)
	}
	if len(byType) != 1 {
		t.Fatalf("expected 1 entry for event_type=order.created, got %d", len(byType))
	}
	if byType[0].EventType != "order.created" {
		t.Errorf("expected event_type order.created, got %s", byType[0].EventType)
	}

	// Filter by entity_type.
	byEntity, err := repo.Query(ctx, repository.AuditLogFilter{EntityType: "order"}, 100, 0)
	if err != nil {
		t.Fatalf("Query(EntityType) error = %v", err)
	}
	if len(byEntity) != 2 {
		t.Fatalf("expected 2 entries for entity_type=order, got %d", len(byEntity))
	}

	// Filter by entity_id.
	byEntityID, err := repo.Query(ctx, repository.AuditLogFilter{EntityID: &entityID}, 100, 0)
	if err != nil {
		t.Fatalf("Query(EntityID) error = %v", err)
	}
	if len(byEntityID) != 2 {
		t.Fatalf("expected 2 entries for entity_id, got %d", len(byEntityID))
	}

	// Filter by actor.
	byActor, err := repo.Query(ctx, repository.AuditLogFilter{Actor: "scheduler"}, 100, 0)
	if err != nil {
		t.Fatalf("Query(Actor) error = %v", err)
	}
	if len(byActor) != 1 {
		t.Fatalf("expected 1 entry for actor=scheduler, got %d", len(byActor))
	}

	// Filter by date range.
	after := time.Now().Add(-5 * time.Minute)
	before := time.Now().Add(5 * time.Minute)

	byRange, err := repo.Query(ctx, repository.AuditLogFilter{CreatedAfter: &after, CreatedBefore: &before}, 100, 0)
	if err != nil {
		t.Fatalf("Query(date range) error = %v", err)
	}
	if len(byRange) != 3 {
		t.Fatalf("expected 3 entries in date range, got %d", len(byRange))
	}

	// Verify ordering (most recent first).
	if len(all) >= 2 && all[0].CreatedAt.Before(all[1].CreatedAt) {
		t.Error("expected results ordered by created_at DESC")
	}

	// Verify JSONB details round-trip.
	found := false
	for _, e := range all {
		if e.ID == entry1.ID {
			found = true
			if !jsonBytesEqual(e.Details, json.RawMessage(`{"ticker":"AAPL","qty":10}`)) {
				t.Errorf("expected details %q, got %q", `{"ticker":"AAPL","qty":10}`, string(e.Details))
			}
			if e.EntityType != "order" {
				t.Errorf("expected entity_type order, got %s", e.EntityType)
			}
			if e.EntityID == nil || *e.EntityID != entityID {
				t.Errorf("expected entity_id %s, got %v", entityID, e.EntityID)
			}
			if e.Actor != "system" {
				t.Errorf("expected actor system, got %s", e.Actor)
			}
		}
	}
	if !found {
		t.Error("entry1 not found in Query() results")
	}

	// Verify entry with no optional fields.
	foundEntry3 := false
	for _, e := range all {
		if e.ID == entry3.ID {
			foundEntry3 = true
			if e.EntityType != "" {
				t.Errorf("expected empty entity_type, got %s", e.EntityType)
			}
			if e.EntityID != nil {
				t.Errorf("expected nil entity_id, got %v", e.EntityID)
			}
			if len(e.Details) != 0 {
				t.Errorf("expected nil details, got %s", e.Details)
			}
		}
	}
	if !foundEntry3 {
		t.Error("entry3 not found in Query() results")
	}

	// Verify pagination.
	page1, err := repo.Query(ctx, repository.AuditLogFilter{}, 2, 0)
	if err != nil {
		t.Fatalf("Query(limit=2) error = %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("expected 2 entries with limit=2, got %d", len(page1))
	}

	page2, err := repo.Query(ctx, repository.AuditLogFilter{}, 2, 2)
	if err != nil {
		t.Fatalf("Query(limit=2,offset=2) error = %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("expected 1 entry with limit=2,offset=2, got %d", len(page2))
	}

	// Ensure no overlap between pages.
	if page1[0].ID == page2[0].ID || page1[1].ID == page2[0].ID {
		t.Error("pagination produced overlapping results")
	}
}

// ---------------------------------------------------------------------------
// Pool setup helper
// ---------------------------------------------------------------------------

func newAuditLogIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to create admin pool: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}

	schemaName := "integration_audit_log_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}

	ddl := []string{
		`CREATE TABLE audit_log (
			id          UUID        NOT NULL DEFAULT gen_random_uuid(),
			event_type  TEXT        NOT NULL,
			entity_type TEXT,
			entity_id   UUID,
			actor       TEXT,
			details     JSONB,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (id, created_at)
		) PARTITION BY RANGE (created_at)`,
		`CREATE TABLE audit_log_2026_q1 PARTITION OF audit_log
			FOR VALUES FROM ('2026-01-01') TO ('2026-04-01')`,
		`CREATE TABLE audit_log_default PARTITION OF audit_log DEFAULT`,
		`CREATE INDEX idx_audit_log_event_type ON audit_log (event_type)`,
		`CREATE INDEX idx_audit_log_entity ON audit_log (entity_type, entity_id)`,
		`CREATE INDEX idx_audit_log_created_at ON audit_log (created_at)`,
	}

	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
			adminPool.Close()
			t.Fatalf("failed to apply test schema DDL: %v", err)
		}
	}

	cleanup := func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
	}

	return pool, cleanup
}
