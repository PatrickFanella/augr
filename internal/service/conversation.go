package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	agentconv "github.com/PatrickFanella/get-rich-quick/internal/agent/conversation"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// ConversationService encapsulates the multi-step logic for creating a
// conversation message: saving the user turn, building LLM context from
// pipeline decisions/snapshots/memories, calling the LLM, and persisting the
// assistant response.
type ConversationService struct {
	conversations repository.ConversationRepository
	decisions     repository.AgentDecisionRepository
	snapshots     repository.PipelineRunSnapshotRepository
	memories      repository.MemoryRepository
	llmProvider   llm.Provider
	logger        *slog.Logger
}

func NewConversationService(
	conversations repository.ConversationRepository,
	decisions repository.AgentDecisionRepository,
	snapshots repository.PipelineRunSnapshotRepository,
	memories repository.MemoryRepository,
	llmProvider llm.Provider,
	logger *slog.Logger,
) *ConversationService {
	return &ConversationService{
		conversations: conversations,
		decisions:     decisions,
		snapshots:     snapshots,
		memories:      memories,
		llmProvider:   llmProvider,
		logger:        logger,
	}
}

// CreateMessage saves the user message, generates an LLM response using
// pipeline context, persists the assistant message, and returns it.
func (svc *ConversationService) CreateMessage(ctx context.Context, convID uuid.UUID, userContent string) (*domain.ConversationMessage, error) {
	if strings.TrimSpace(userContent) == "" {
		return nil, &ServiceError{Status: 400, Message: "content is required"}
	}

	conv, err := svc.conversations.GetConversation(ctx, convID)
	if err != nil {
		if isNotFound(err) {
			return nil, &ServiceError{Status: 404, Message: "conversation not found"}
		}
		return nil, &ServiceError{Status: 500, Message: "failed to get conversation"}
	}

	userMsg := &domain.ConversationMessage{
		ConversationID: convID,
		Role:           domain.ConversationMessageRoleUser,
		Content:        userContent,
	}
	if err := svc.conversations.AddMessage(ctx, convID, userMsg); err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to save message"}
	}

	if svc.llmProvider == nil {
		return nil, &ServiceError{Status: 501, Message: "LLM provider not configured"}
	}

	history, err := svc.conversations.GetMessages(ctx, convID, 100, 0)
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to load history"}
	}

	var llmMessages []llm.Message
	var systemPrompt string

	if svc.decisions != nil && svc.snapshots != nil && svc.memories != nil {
		cb := agentconv.NewContextBuilder(svc.decisions, svc.snapshots, svc.memories, 0)
		builtCtx, buildErr := cb.BuildContext(ctx, agentconv.ContextInput{
			RunID:               conv.PipelineRunID,
			AgentRole:           conv.AgentRole,
			ConversationHistory: history,
		})
		if buildErr != nil {
			svc.logger.Warn("failed to build conversation context, using simple prompt", "error", buildErr)
			systemPrompt = fmt.Sprintf("You are a %s trading agent. Answer questions about your decisions.", conv.AgentRole)
			llmMessages = historyToLLMMessages(history)
		} else {
			systemPrompt = builtCtx.SystemPrompt
			llmMessages = builtCtx.Messages
		}
	} else {
		systemPrompt = fmt.Sprintf("You are a %s trading agent. Answer questions about your decisions.", conv.AgentRole)
		llmMessages = historyToLLMMessages(history)
	}

	messages := append([]llm.Message{{Role: "system", Content: systemPrompt}}, llmMessages...)

	resp, err := svc.llmProvider.Complete(ctx, llm.CompletionRequest{Messages: messages})
	if err != nil {
		return nil, &ServiceError{Status: 500, Message: "LLM completion failed"}
	}

	assistantMsg := &domain.ConversationMessage{
		ConversationID: convID,
		Role:           domain.ConversationMessageRoleAssistant,
		Content:        resp.Content,
	}
	if err := svc.conversations.AddMessage(ctx, convID, assistantMsg); err != nil {
		return nil, &ServiceError{Status: 500, Message: "failed to save response"}
	}

	return assistantMsg, nil
}

func historyToLLMMessages(history []domain.ConversationMessage) []llm.Message {
	msgs := make([]llm.Message, 0, len(history))
	for _, m := range history {
		msgs = append(msgs, llm.Message{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	return msgs
}
