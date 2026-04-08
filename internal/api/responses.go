package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// Standard error codes used in API error responses.
const (
	ErrCodeBadRequest       = "ERR_BAD_REQUEST"
	ErrCodeNotFound         = "ERR_NOT_FOUND"
	ErrCodeNotImplemented   = "ERR_NOT_IMPLEMENTED"
	ErrCodeInternal         = "ERR_INTERNAL"
	ErrCodeValidation       = "ERR_VALIDATION"
	ErrCodeMethodNotAllowed = "ERR_METHOD_NOT_ALLOWED"
	ErrCodeUnauthorized     = "ERR_UNAUTHORIZED"
	ErrCodeRateLimited      = "ERR_RATE_LIMITED"
	ErrCodeConflict         = "ERR_CONFLICT"
)

// ErrorResponse is the standard error envelope returned by the API.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// ListResponse is a generic envelope for paginated list responses.
type ListResponse struct {
	Data   any `json:"data"`
	Total  int `json:"total,omitempty"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// LoginResponse is returned after a successful authentication request.
type LoginResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// respondJSON writes a JSON response with the given status code.
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// respondError writes a standardised JSON error response.
func respondError(w http.ResponseWriter, status int, msg, code string) {
	respondJSON(w, status, ErrorResponse{Error: msg, Code: code})
}

// respondList writes a paginated JSON list response. Total is omitted when
// the repository layer does not return a count of all matching rows.
func respondList(w http.ResponseWriter, data any, limit, offset int) {
	respondJSON(w, http.StatusOK, ListResponse{
		Data:   data,
		Limit:  limit,
		Offset: offset,
	})
}

// respondListWithTotal writes a paginated JSON list response that includes the
// total count of matching rows for proper client-side pagination.
func respondListWithTotal(w http.ResponseWriter, data any, total, limit, offset int) {
	respondJSON(w, http.StatusOK, ListResponse{
		Data:   data,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}
