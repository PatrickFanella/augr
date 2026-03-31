package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildConversationListQuery_NoFilters(t *testing.T) {
	query, args := buildConversationListQuery(repository.ConversationFilter{}, 10, 5)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}
	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}
	if args[1] != 5 {
		t.Errorf("expected offset=5, got %v", args[1])
	}

	assertContains(t, query, "FROM conversations")
	assertContains(t, query, "ORDER BY created_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildConversationListQuery_WithFilters(t *testing.T) {
	runID := uuid.New()
	query, args := buildConversationListQuery(repository.ConversationFilter{
		PipelineRunID: &runID,
		AgentRole:     domain.AgentRoleTrader,
	}, 20, 0)

	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "pipeline_run_id = $1")
	assertContains(t, query, "agent_role = $2")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
}

func TestConversationRepoIntegration_CreateAndGetConversation(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newConversationIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewConversationRepo(pool)
	conv := &domain.Conversation{
		PipelineRunID: uuid.New(),
		AgentRole:     domain.AgentRoleTrader,
		Title:         "Trader thread",
	}

	if err := repo.CreateConversation(ctx, conv); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	if conv.ID == uuid.Nil {
		t.Fatal("expected CreateConversation() to populate ID")
	}
	if conv.CreatedAt.IsZero() {
		t.Fatal("expected CreateConversation() to populate CreatedAt")
	}
	if conv.UpdatedAt.IsZero() {
		t.Fatal("expected CreateConversation() to populate UpdatedAt")
	}

	got, err := repo.GetConversation(ctx, conv.ID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}

	if got.ID != conv.ID {
		t.Errorf("expected ID %s, got %s", conv.ID, got.ID)
	}
	if got.PipelineRunID != conv.PipelineRunID {
		t.Errorf("expected PipelineRunID %s, got %s", conv.PipelineRunID, got.PipelineRunID)
	}
	if got.AgentRole != conv.AgentRole {
		t.Errorf("expected AgentRole %q, got %q", conv.AgentRole, got.AgentRole)
	}
	if got.Title != conv.Title {
		t.Errorf("expected Title %q, got %q", conv.Title, got.Title)
	}
}

func TestConversationRepoIntegration_GetConversationNotFound(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newConversationIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewConversationRepo(pool)

	_, err := repo.GetConversation(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected GetConversation() to return error for unknown conversation")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestConversationRepoIntegration_AddMessagesAndGetMessagesChronological(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newConversationIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewConversationRepo(pool)
	conv := createTestConversation(t, ctx, repo, uuid.New(), domain.AgentRoleMarketAnalyst, "Analysis thread")

	first := &domain.ConversationMessage{
		ID:      mustParseConversationUUID(t, "00000000-0000-0000-0000-000000000001"),
		Role:    domain.ConversationMessageRoleUser,
		Content: "What changed?",
	}
	if err := repo.AddMessage(ctx, conv.ID, first); err != nil {
		t.Fatalf("AddMessage(first) error = %v", err)
	}

	second := &domain.ConversationMessage{
		ID:      mustParseConversationUUID(t, "00000000-0000-0000-0000-000000000002"),
		Role:    domain.ConversationMessageRoleAssistant,
		Content: "Momentum improved.",
	}
	if err := repo.AddMessage(ctx, conv.ID, second); err != nil {
		t.Fatalf("AddMessage(second) error = %v", err)
	}

	messages, err := repo.GetMessages(ctx, conv.ID, 10, 0)
	if err != nil {
		t.Fatalf("GetMessages() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].ID != first.ID || messages[1].ID != second.ID {
		t.Fatalf("expected chronological order [%s %s], got [%s %s]", first.ID, second.ID, messages[0].ID, messages[1].ID)
	}
	if first.ConversationID != conv.ID || second.ConversationID != conv.ID {
		t.Fatal("expected AddMessage() to populate ConversationID")
	}
}

func TestConversationRepoIntegration_ListConversationsFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newConversationIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewConversationRepo(pool)
	runID := uuid.New()
	otherRunID := uuid.New()

	conv1 := createTestConversationWithID(t, ctx, repo, mustParseConversationUUID(t, "00000000-0000-0000-0000-000000000001"), runID, domain.AgentRoleTrader, "First")
	conv2 := createTestConversationWithID(t, ctx, repo, mustParseConversationUUID(t, "00000000-0000-0000-0000-000000000002"), runID, domain.AgentRoleTrader, "Second")
	conv3 := createTestConversationWithID(t, ctx, repo, mustParseConversationUUID(t, "00000000-0000-0000-0000-000000000003"), otherRunID, domain.AgentRoleMarketAnalyst, "Third")

	byRun, err := repo.ListConversations(ctx, repository.ConversationFilter{
		PipelineRunID: &runID,
	}, 10, 0)
	if err != nil {
		t.Fatalf("ListConversations() by pipeline_run_id error = %v", err)
	}
	if len(byRun) != 2 {
		t.Fatalf("expected 2 conversations for run, got %d", len(byRun))
	}
	for _, conv := range byRun {
		if conv.PipelineRunID != runID {
			t.Fatalf("expected run-scoped results to have pipeline_run_id=%s, got %s", runID, conv.PipelineRunID)
		}
	}

	byRole, err := repo.ListConversations(ctx, repository.ConversationFilter{
		AgentRole: domain.AgentRoleMarketAnalyst,
	}, 10, 0)
	if err != nil {
		t.Fatalf("ListConversations() by agent_role error = %v", err)
	}
	if len(byRole) != 1 {
		t.Fatalf("expected 1 conversation for market analyst role, got %d", len(byRole))
	}
	if byRole[0].ID != conv3.ID {
		t.Fatalf("expected market analyst conversation %s, got %s", conv3.ID, byRole[0].ID)
	}

	page, err := repo.ListConversations(ctx, repository.ConversationFilter{}, 2, 1)
	if err != nil {
		t.Fatalf("ListConversations() pagination error = %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 conversations on paginated page, got %d", len(page))
	}
	if page[0].ID != conv2.ID || page[1].ID != conv1.ID {
		t.Fatalf("expected paginated results [%s %s], got [%s %s]", conv2.ID, conv1.ID, page[0].ID, page[1].ID)
	}
}

func TestConversationRepoIntegration_MessagePagination(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newConversationIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewConversationRepo(pool)
	conv := createTestConversation(t, ctx, repo, uuid.New(), domain.AgentRoleTrader, "Paginated messages")

	first := &domain.ConversationMessage{
		ID:      mustParseConversationUUID(t, "00000000-0000-0000-0000-000000000011"),
		Role:    domain.ConversationMessageRoleUser,
		Content: "First",
	}
	if err := repo.AddMessage(ctx, conv.ID, first); err != nil {
		t.Fatalf("AddMessage(first) error = %v", err)
	}
	second := &domain.ConversationMessage{
		ID:      mustParseConversationUUID(t, "00000000-0000-0000-0000-000000000012"),
		Role:    domain.ConversationMessageRoleAssistant,
		Content: "Second",
	}
	if err := repo.AddMessage(ctx, conv.ID, second); err != nil {
		t.Fatalf("AddMessage(second) error = %v", err)
	}
	third := &domain.ConversationMessage{
		ID:      mustParseConversationUUID(t, "00000000-0000-0000-0000-000000000013"),
		Role:    domain.ConversationMessageRoleUser,
		Content: "Third",
	}
	if err := repo.AddMessage(ctx, conv.ID, third); err != nil {
		t.Fatalf("AddMessage(third) error = %v", err)
	}

	page, err := repo.GetMessages(ctx, conv.ID, 2, 1)
	if err != nil {
		t.Fatalf("GetMessages() pagination error = %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 messages on page, got %d", len(page))
	}
	if page[0].ID != second.ID || page[1].ID != third.ID {
		t.Fatalf("expected paginated message results [%s %s], got [%s %s]", second.ID, third.ID, page[0].ID, page[1].ID)
	}
}

func createTestConversation(t *testing.T, ctx context.Context, repo *ConversationRepo, runID uuid.UUID, role domain.AgentRole, title string) *domain.Conversation {
	t.Helper()

	return createTestConversationWithID(t, ctx, repo, uuid.Nil, runID, role, title)
}

func createTestConversationWithID(t *testing.T, ctx context.Context, repo *ConversationRepo, id, runID uuid.UUID, role domain.AgentRole, title string) *domain.Conversation {
	t.Helper()

	conv := &domain.Conversation{
		ID:            id,
		PipelineRunID: runID,
		AgentRole:     role,
		Title:         title,
	}
	if err := repo.CreateConversation(ctx, conv); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	return conv
}

func mustParseConversationUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()

	id, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("failed to parse uuid %q: %v", value, err)
	}

	return id
}

func newConversationIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_conversation_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
		`CREATE TABLE conversations (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			pipeline_run_id UUID        NOT NULL,
			agent_role      TEXT        NOT NULL,
			title           TEXT,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE conversation_messages (
			id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			conversation_id UUID        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
			role            TEXT        NOT NULL CHECK (role IN ('user', 'assistant')),
			content         TEXT        NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE OR REPLACE FUNCTION prevent_conversation_message_created_at_update() RETURNS trigger AS $$
		BEGIN
			IF NEW.created_at IS DISTINCT FROM OLD.created_at THEN
				RAISE EXCEPTION 'conversation_messages.created_at is immutable';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER trg_conversation_messages_created_at_immutable
			BEFORE UPDATE OF created_at ON conversation_messages
			FOR EACH ROW EXECUTE FUNCTION prevent_conversation_message_created_at_update()`,
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
