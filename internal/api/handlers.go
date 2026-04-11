package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	defaultLimit = 50
	maxLimit     = 100
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// parsePagination extracts limit/offset query params with sane defaults.
func parsePagination(r *http.Request) (limit, offset int) {
	limit = defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// parseUUID extracts a UUID from a chi URL parameter.
func parseUUID(r *http.Request, param string) (uuid.UUID, error) {
	raw := chi.URLParam(r, param)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errors.New("invalid id: " + raw)
	}
	return id, nil
}

// isNotFound checks whether err wraps repository.ErrNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, repository.ErrNotFound)
}

// isUniqueConstraintViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505). Works with pgconn.PgError wrapped by pgx.
func isUniqueConstraintViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// actorOf extracts the authenticated subject name from the request context.
// Returns an empty string when the request is unauthenticated.
func actorOf(r *http.Request) string {
	if p, ok := PrincipalFromContext(r.Context()); ok {
		return p.Subject
	}
	return ""
}

// writeAuditLog persists an audit log entry on a best-effort basis.
// Errors are logged but not propagated to avoid blocking the calling handler.
func (s *Server) writeAuditLog(ctx context.Context, actor, eventType, entityType string, entityID *uuid.UUID, details any) {
	if s.auditLog == nil {
		return
	}
	var raw json.RawMessage
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			raw = b
		}
	}
	entry := &domain.AuditLogEntry{
		ID:         uuid.New(),
		EventType:  eventType,
		EntityType: entityType,
		EntityID:   entityID,
		Actor:      actor,
		Details:    raw,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.auditLog.Create(ctx, entry); err != nil {
		s.logger.Warn("audit log write failed",
			slog.String("event_type", eventType),
			slog.String("error", err.Error()),
		)
	}
}

// titleCase capitalises the first letter of each whitespace-delimited word.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.settings.Get(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get settings", ErrCodeInternal)
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}

	settings, err := s.settings.Update(r.Context(), body)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeValidation)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "settings.updated", "settings", nil, nil)
	respondJSON(w, http.StatusOK, settings)
}
