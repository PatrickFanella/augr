package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestMarshalPipelineRunSnapshotPayload_ValidJSON(t *testing.T) {
	input := json.RawMessage(`{"bars":[{"close":100.5}]}`)

	got, err := marshalPipelineRunSnapshotPayload(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != `{"bars":[{"close":100.5}]}` {
		t.Errorf("expected payload pass-through, got %s", got)
	}
}

func TestMarshalPipelineRunSnapshotPayload_Required(t *testing.T) {
	_, err := marshalPipelineRunSnapshotPayload(nil)
	if err == nil {
		t.Fatal("expected error for nil payload, got nil")
	}
}

func TestMarshalPipelineRunSnapshotPayload_InvalidJSON(t *testing.T) {
	_, err := marshalPipelineRunSnapshotPayload(json.RawMessage(`{not valid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestPipelineRunSnapshotRepoIntegration_CreatePersistsSnapshot(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newPipelineRunSnapshotIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPipelineRunSnapshotRepo(pool)
	runID := uuid.New()
	snapshot := &domain.PipelineRunSnapshot{
		PipelineRunID: runID,
		DataType:      "market",
		Payload:       json.RawMessage(`{"ticker":"AAPL","bars":[{"close":189.12}]}`),
	}

	if err := repo.Create(ctx, snapshot); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if snapshot.ID == uuid.Nil {
		t.Fatal("expected Create() to populate ID")
	}
	if snapshot.CreatedAt.IsZero() {
		t.Fatal("expected Create() to populate CreatedAt")
	}

	var (
		gotRunID    uuid.UUID
		gotDataType string
		gotPayload  []byte
		gotCreated  time.Time
	)
	if err := pool.QueryRow(ctx, `
		SELECT pipeline_run_id, data_type, payload, created_at
		FROM pipeline_run_snapshots
		WHERE id = $1
	`, snapshot.ID).Scan(&gotRunID, &gotDataType, &gotPayload, &gotCreated); err != nil {
		t.Fatalf("failed to query persisted snapshot: %v", err)
	}

	if gotRunID != snapshot.PipelineRunID {
		t.Fatalf("PipelineRunID: want %s, got %s", snapshot.PipelineRunID, gotRunID)
	}
	if gotDataType != snapshot.DataType {
		t.Fatalf("DataType: want %q, got %q", snapshot.DataType, gotDataType)
	}
	if !jsonBytesEqual(gotPayload, snapshot.Payload) {
		t.Fatalf("Payload: want %s, got %s", snapshot.Payload, gotPayload)
	}
	if !gotCreated.Equal(snapshot.CreatedAt) {
		t.Fatalf("CreatedAt: want %v, got %v", snapshot.CreatedAt, gotCreated)
	}
}

func TestPipelineRunSnapshotRepoIntegration_GetByRun(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newPipelineRunSnapshotIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPipelineRunSnapshotRepo(pool)
	runID := uuid.New()
	otherRunID := uuid.New()
	createdAt := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)

	firstID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	secondID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	for _, row := range []struct {
		id            uuid.UUID
		pipelineRunID uuid.UUID
		dataType      string
		payload       string
		createdAt     time.Time
	}{
		{
			id:            secondID,
			pipelineRunID: runID,
			dataType:      "news",
			payload:       `{"headline":"Later by id"}`,
			createdAt:     createdAt,
		},
		{
			id:            firstID,
			pipelineRunID: runID,
			dataType:      "market",
			payload:       `{"ticker":"AAPL"}`,
			createdAt:     createdAt,
		},
		{
			id:            uuid.New(),
			pipelineRunID: otherRunID,
			dataType:      "social",
			payload:       `{"sentiment":"bullish"}`,
			createdAt:     createdAt.Add(time.Minute),
		},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO pipeline_run_snapshots (id, pipeline_run_id, data_type, payload, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, row.id, row.pipelineRunID, row.dataType, json.RawMessage(row.payload), row.createdAt); err != nil {
			t.Fatalf("failed to seed snapshot %s: %v", row.id, err)
		}
	}

	got, err := repo.GetByRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(got))
	}

	if got[0].ID != firstID || got[1].ID != secondID {
		t.Fatalf("expected snapshots ordered by created_at, id; got IDs %s then %s", got[0].ID, got[1].ID)
	}

	if got[0].DataType != "market" || !jsonBytesEqual(got[0].Payload, json.RawMessage(`{"ticker":"AAPL"}`)) {
		t.Fatalf("unexpected first snapshot: %+v", got[0])
	}
	if got[1].DataType != "news" || !jsonBytesEqual(got[1].Payload, json.RawMessage(`{"headline":"Later by id"}`)) {
		t.Fatalf("unexpected second snapshot: %+v", got[1])
	}
}

func newPipelineRunSnapshotIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_pipeline_run_snapshot_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	sanitizedSchemaName := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+sanitizedSchemaName); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+sanitizedSchemaName+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+sanitizedSchemaName+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}

	ddl := []string{
		`CREATE TABLE pipeline_run_snapshots (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			pipeline_run_id UUID        NOT NULL,
			data_type       TEXT        NOT NULL CHECK (data_type IN ('market', 'news', 'fundamentals', 'social')),
			payload         JSONB       NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX idx_pipeline_run_snapshots_pipeline_run_id ON pipeline_run_snapshots (pipeline_run_id)`,
	}

	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+sanitizedSchemaName+` CASCADE`)
			adminPool.Close()
			t.Fatalf("failed to apply test schema DDL: %v", err)
		}
	}

	cleanup := func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+sanitizedSchemaName+` CASCADE`)
		adminPool.Close()
	}

	return pool, cleanup
}
