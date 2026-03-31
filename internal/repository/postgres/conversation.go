package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// ConversationRepo implements repository.ConversationRepository using PostgreSQL.
type ConversationRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that ConversationRepo satisfies ConversationRepository.
var _ repository.ConversationRepository = (*ConversationRepo)(nil)

// NewConversationRepo returns a ConversationRepo backed by the given connection pool.
func NewConversationRepo(pool *pgxpool.Pool) *ConversationRepo {
	return &ConversationRepo{pool: pool}
}

// CreateConversation inserts a new conversation and populates generated fields on
// the provided struct.
func (r *ConversationRepo) CreateConversation(ctx context.Context, conv *domain.Conversation) error {
	var row scanner
	if conv.ID == uuid.Nil {
		row = r.pool.QueryRow(ctx,
			`INSERT INTO conversations (pipeline_run_id, agent_role, title)
			 VALUES ($1, $2, $3)
			 RETURNING id, created_at, updated_at`,
			conv.PipelineRunID,
			conv.AgentRole,
			nullString(conv.Title),
		)
	} else {
		row = r.pool.QueryRow(ctx,
			`INSERT INTO conversations (id, pipeline_run_id, agent_role, title)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id, created_at, updated_at`,
			conv.ID,
			conv.PipelineRunID,
			conv.AgentRole,
			nullString(conv.Title),
		)
	}

	if err := row.Scan(&conv.ID, &conv.CreatedAt, &conv.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create conversation: %w", err)
	}

	return nil
}

// GetConversation retrieves a conversation by ID. It returns ErrNotFound when
// no row matches.
func (r *ConversationRepo) GetConversation(ctx context.Context, id uuid.UUID) (*domain.Conversation, error) {
	row := r.pool.QueryRow(ctx, conversationSelectSQL+` WHERE id = $1`, id)

	conv, err := scanConversation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get conversation %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get conversation: %w", err)
	}

	return conv, nil
}

// ListConversations returns conversations matching the provided filter with pagination.
func (r *ConversationRepo) ListConversations(ctx context.Context, filter repository.ConversationFilter, limit, offset int) ([]domain.Conversation, error) {
	query, args := buildConversationListQuery(filter, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list conversations: %w", err)
	}
	defer rows.Close()

	var conversations []domain.Conversation
	for rows.Next() {
		conv, err := scanConversation(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list conversations scan: %w", err)
		}
		conversations = append(conversations, *conv)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list conversations rows: %w", err)
	}

	return conversations, nil
}

// AddMessage inserts a new message for the given conversation and populates the
// generated fields on the provided struct.
func (r *ConversationRepo) AddMessage(ctx context.Context, convID uuid.UUID, msg *domain.ConversationMessage) error {
	var row scanner
	if msg.ID == uuid.Nil {
		row = r.pool.QueryRow(ctx,
			`INSERT INTO conversation_messages (conversation_id, role, content)
			 VALUES ($1, $2, $3)
			 RETURNING id, created_at`,
			convID,
			msg.Role,
			msg.Content,
		)
	} else {
		row = r.pool.QueryRow(ctx,
			`INSERT INTO conversation_messages (id, conversation_id, role, content)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id, created_at`,
			msg.ID,
			convID,
			msg.Role,
			msg.Content,
		)
	}

	if err := row.Scan(&msg.ID, &msg.CreatedAt); err != nil {
		return fmt.Errorf("postgres: add conversation message: %w", err)
	}

	msg.ConversationID = convID
	return nil
}

// GetMessages retrieves conversation messages in chronological order with pagination.
func (r *ConversationRepo) GetMessages(ctx context.Context, convID uuid.UUID, limit, offset int) ([]domain.ConversationMessage, error) {
	rows, err := r.pool.Query(ctx,
		messageSelectSQL+` WHERE conversation_id = $1 ORDER BY created_at, id LIMIT $2 OFFSET $3`,
		convID,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: get conversation messages: %w", err)
	}
	defer rows.Close()

	var messages []domain.ConversationMessage
	for rows.Next() {
		msg, err := scanConversationMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: get conversation messages scan: %w", err)
		}
		messages = append(messages, *msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: get conversation messages rows: %w", err)
	}

	return messages, nil
}

const conversationSelectSQL = `SELECT id, pipeline_run_id, agent_role, title, created_at, updated_at
	FROM conversations`

const messageSelectSQL = `SELECT id, conversation_id, role, content, created_at
	FROM conversation_messages`

func scanConversation(sc scanner) (*domain.Conversation, error) {
	var (
		conv  domain.Conversation
		title *string
	)

	if err := sc.Scan(
		&conv.ID,
		&conv.PipelineRunID,
		&conv.AgentRole,
		&title,
		&conv.CreatedAt,
		&conv.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if title != nil {
		conv.Title = *title
	}

	return &conv, nil
}

func scanConversationMessage(sc scanner) (*domain.ConversationMessage, error) {
	var msg domain.ConversationMessage

	if err := sc.Scan(
		&msg.ID,
		&msg.ConversationID,
		&msg.Role,
		&msg.Content,
		&msg.CreatedAt,
	); err != nil {
		return nil, err
	}

	return &msg, nil
}

func buildConversationListQuery(filter repository.ConversationFilter, limit, offset int) (string, []any) {
	qb := NewQueryBuilder()

	if filter.PipelineRunID != nil {
		qb.AddCondition("pipeline_run_id", "=", *filter.PipelineRunID)
	}
	if filter.AgentRole != "" {
		qb.AddCondition("agent_role", "=", filter.AgentRole)
	}

	query := conversationSelectSQL + qb.WhereClause() + " ORDER BY created_at DESC, id DESC"
	return qb.Pagination(query, limit, offset), qb.Args()
}
