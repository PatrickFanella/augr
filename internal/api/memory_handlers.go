package api

import (
	"encoding/json"
	"net/http"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()

	query := q.Get("q")
	filter := repository.MemorySearchFilter{}
	if !ParseEnumParam(w, q, "agent_role", &filter.AgentRole) {
		return
	}

	memories, err := s.memories.Search(r.Context(), query, filter, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to search memories", ErrCodeInternal)
		return
	}
	respondList(w, memories, limit, offset)
}

func (s *Server) handleSearchMemories(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if body.Query == "" {
		respondError(w, http.StatusBadRequest, "query is required", ErrCodeValidation)
		return
	}
	limit, offset := parsePagination(r)
	memories, err := s.memories.Search(r.Context(), body.Query, repository.MemorySearchFilter{}, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to search memories", ErrCodeInternal)
		return
	}
	respondList(w, memories, limit, offset)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	if err := s.memories.Delete(r.Context(), id); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "memory not found", ErrCodeNotFound)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to delete memory", ErrCodeInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
