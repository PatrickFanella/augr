package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "username and password are required", ErrCodeValidation)
		return
	}

	user, err := s.users.GetByUsername(r.Context(), req.Username)
	if err != nil {
		if isNotFound(err) {
			verifyPasswordAgainstDummyHash(req.Password)
			respondError(w, http.StatusUnauthorized, "invalid username or password", ErrCodeUnauthorized)
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to authenticate user", ErrCodeInternal)
		return
	}

	if err := verifyPassword(user.PasswordHash, req.Password); err != nil {
		respondError(w, http.StatusUnauthorized, "invalid username or password", ErrCodeUnauthorized)
		return
	}

	tokenPair, err := s.auth.GenerateTokenPair(user.Username)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate auth tokens", ErrCodeInternal)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	respondJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt.UTC(),
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "username and password are required", ErrCodeValidation)
		return
	}

	user := &domain.User{
		Username: req.Username,
		Password: req.Password,
	}
	if err := s.users.Create(r.Context(), user); err != nil {
		if isUniqueConstraintViolation(err) {
			respondError(w, http.StatusConflict, "username already taken", ErrCodeConflict)
			return
		}
		s.logger.Error("register: create user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create user", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), user.Username, "user.registered", "user", &user.ID, nil)

	tokenPair, err := s.auth.GenerateTokenPair(user.Username)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate auth tokens", ErrCodeInternal)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	respondJSON(w, http.StatusCreated, LoginResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt.UTC(),
	})
}

func (s *Server) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if strings.TrimSpace(body.RefreshToken) == "" {
		respondError(w, http.StatusBadRequest, "refresh_token is required", ErrCodeValidation)
		return
	}

	tokenPair, err := s.auth.RefreshTokenPair(body.RefreshToken)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "invalid or expired refresh token", ErrCodeUnauthorized)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	respondJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt.UTC(),
	})
}

func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "not authenticated", ErrCodeUnauthorized)
		return
	}

	user, err := s.users.GetByUsername(r.Context(), principal.Subject)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "user not found", ErrCodeNotFound)
			return
		}
		s.logger.Error("get current user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to fetch user", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, user)
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "not authenticated", ErrCodeUnauthorized)
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	if strings.TrimSpace(req.CurrentPassword) == "" || strings.TrimSpace(req.NewPassword) == "" {
		respondError(w, http.StatusBadRequest, "current_password and new_password are required", ErrCodeValidation)
		return
	}
	if len(req.NewPassword) < 8 {
		respondError(w, http.StatusBadRequest, "new_password must be at least 8 characters", ErrCodeValidation)
		return
	}

	user, err := s.users.GetByUsername(r.Context(), principal.Subject)
	if err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "user not found", ErrCodeNotFound)
			return
		}
		s.logger.Error("update me: get user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to fetch user", ErrCodeInternal)
		return
	}

	if err := verifyPassword(user.PasswordHash, req.CurrentPassword); err != nil {
		respondError(w, http.StatusUnauthorized, "current password is incorrect", ErrCodeUnauthorized)
		return
	}

	newHash, err := hashPassword(req.NewPassword)
	if err != nil {
		s.logger.Error("update me: hash password", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to update password", ErrCodeInternal)
		return
	}

	if err := s.users.UpdatePasswordHash(r.Context(), user.ID, newHash); err != nil {
		s.logger.Error("update me: update password hash", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to update password", ErrCodeInternal)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	keys, err := s.auth.ListAPIKeys(r.Context(), limit, offset)
	if err != nil {
		s.logger.Error("list api keys", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to list api keys", ErrCodeInternal)
		return
	}
	total, err := s.auth.CountAPIKeys(r.Context())
	if err != nil {
		s.logger.Warn("count api keys", "error", err.Error())
	}
	respondListWithTotal(w, keys, total, limit, offset)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body", ErrCodeBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required", ErrCodeValidation)
		return
	}

	plaintext, key, err := s.auth.CreateAPIKey(r.Context(), req.Name, req.ExpiresAt)
	if err != nil {
		s.logger.Error("create api key", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create api key", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "api_key.created", "api_key", &key.ID,
		map[string]string{"name": key.Name})

	respondJSON(w, http.StatusCreated, struct {
		Key      string         `json:"key"`
		Metadata *domain.APIKey `json:"metadata"`
	}{Key: plaintext, Metadata: key})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid api key id", ErrCodeBadRequest)
		return
	}

	if err := s.auth.RevokeAPIKey(r.Context(), id); err != nil {
		if isNotFound(err) {
			respondError(w, http.StatusNotFound, "api key not found", ErrCodeNotFound)
			return
		}
		s.logger.Error("revoke api key", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to revoke api key", ErrCodeInternal)
		return
	}
	s.writeAuditLog(r.Context(), actorOf(r), "api_key.revoked", "api_key", &id, nil)

	w.WriteHeader(http.StatusNoContent)
}
