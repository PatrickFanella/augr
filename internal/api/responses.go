package api

import (
	"encoding/json"
	"net/http"
)

// Standard error codes used in API error responses.
const (
	ErrCodeBadRequest       = "ERR_BAD_REQUEST"
	ErrCodeNotFound         = "ERR_NOT_FOUND"
	ErrCodeInternal         = "ERR_INTERNAL"
	ErrCodeValidation       = "ERR_VALIDATION"
	ErrCodeMethodNotAllowed = "ERR_METHOD_NOT_ALLOWED"
	ErrCodeUnauthorized     = "ERR_UNAUTHORIZED"
	ErrCodeRateLimited      = "ERR_RATE_LIMITED"
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

// respondList writes a paginated JSON list response. Total is omitted because
// the repository layer does not return a count of all matching rows.
func respondList(w http.ResponseWriter, data any, limit, offset int) {
	respondJSON(w, http.StatusOK, ListResponse{
		Data:   data,
		Limit:  limit,
		Offset: offset,
	})
}
