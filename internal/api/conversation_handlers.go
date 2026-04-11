package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/service"
)

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	if s.conversations == nil {
		respondError(w, http.StatusNotImplemented, "conversations not configured", ErrCodeNotImplemented)
		return
	}
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	filter := repository.ConversationFilter{}
	if !ParseEnumParam(w, q, "agent_role", &filter.AgentRole) {
		return
	}
	if v := q.Get("pipeline_run_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.PipelineRunID = &id
		}
	}

	conversations, err := s.conversations.ListConversations(r.Context(), filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list conversations", ErrCodeInternal)
		return
	}
	total, err := s.conversations.CountConversations(r.Context(), filter)
	if err != nil {
		s.logger.Warn("count conversations", "error", err.Error())
	}
	respondListWithTotal(w, conversations, total, limit, offset)
}

func (s *Server) handleGetConversationMessages(w http.ResponseWriter, r *http.Request) {
	if s.conversations == nil {
		respondError(w, http.StatusNotImplemented, "conversations not configured", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	conv, err := s.conversations.GetConversation(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "conversation not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get conversation", ErrCodeInternal)
		return
	}

	limit, offset := parsePagination(r)
	messages, err := s.conversations.GetMessages(r.Context(), id, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get messages", ErrCodeInternal)
		return
	}

	// Inject agent pipeline decisions as synthetic assistant messages at the
	// start of the conversation so the agent's analysis appears as if they
	// were a participant in the chat.
	if offset == 0 && s.decisions != nil {
		decisions, decErr := s.decisions.GetByRun(r.Context(), conv.PipelineRunID, repository.AgentDecisionFilter{
			AgentRole: conv.AgentRole,
		}, 20, 0)
		if decErr == nil && len(decisions) > 0 {
			synthetic := make([]domain.ConversationMessage, 0, len(decisions)+len(messages))
			for _, dec := range decisions {
				content := dec.OutputText
				if dec.Phase != "" {
					content = fmt.Sprintf("[%s] %s", dec.Phase, content)
				}
				synthetic = append(synthetic, domain.ConversationMessage{
					ID:             dec.ID,
					ConversationID: id,
					Role:           domain.ConversationMessageRoleAssistant,
					Content:        content,
					CreatedAt:      dec.CreatedAt,
				})
			}
			synthetic = append(synthetic, messages...)
			messages = synthetic
		}
	}

	respondList(w, messages, limit, offset)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	if s.conversations == nil {
		respondError(w, http.StatusNotImplemented, "conversations not configured", ErrCodeNotImplemented)
		return
	}
	var body struct {
		PipelineRunID uuid.UUID        `json:"pipeline_run_id"`
		AgentRole     domain.AgentRole `json:"agent_role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if body.PipelineRunID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "pipeline_run_id is required", ErrCodeValidation)
		return
	}
	if body.AgentRole == "" {
		respondError(w, http.StatusBadRequest, "agent_role is required", ErrCodeValidation)
		return
	}

	run, err := s.findRunByID(r.Context(), body.PipelineRunID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to verify pipeline run", ErrCodeInternal)
		return
	}
	if run == nil {
		respondError(w, http.StatusBadRequest, "pipeline_run_id does not reference an existing run", ErrCodeValidation)
		return
	}

	roleLabel := strings.ReplaceAll(string(body.AgentRole), "_", " ")
	title := fmt.Sprintf("Chat with %s \u2014 %s", titleCase(roleLabel), run.Ticker)

	conv := &domain.Conversation{
		PipelineRunID: body.PipelineRunID,
		AgentRole:     body.AgentRole,
		Title:         title,
	}
	if err := s.conversations.CreateConversation(r.Context(), conv); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create conversation", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusCreated, conv)
}

func (s *Server) handleCreateConversationMessage(w http.ResponseWriter, r *http.Request) {
	if s.conversations == nil {
		respondError(w, http.StatusNotImplemented, "conversations not configured", ErrCodeNotImplemented)
		return
	}
	convID, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}

	msg, err := s.conversationSvc.CreateMessage(r.Context(), convID, body.Content)
	if err != nil {
		if svcErr, ok := err.(*service.ServiceError); ok {
			code := ErrCodeInternal
			switch svcErr.Status {
			case 400:
				code = ErrCodeBadRequest
			case 404:
				code = ErrCodeNotFound
			case 501:
				code = ErrCodeNotImplemented
			}
			respondError(w, svcErr.Status, svcErr.Message, code)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to create message", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusCreated, msg)
}
