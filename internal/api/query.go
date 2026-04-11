package api

import (
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// ParseEnumParam extracts a string query param, casts it to T, validates it,
// and writes a 400 response on failure. Returns true if the param is absent
// (no-op) or valid; returns false after writing an error.
//
// T must be a named string type with an IsValid() method.
//
// Usage:
//
//	if !ParseEnumParam(w, q, "market_type", &filter.MarketType) { return }
func ParseEnumParam[T interface {
	~string
	IsValid() bool
}](w http.ResponseWriter, q url.Values, key string, dst *T) bool {
	raw := q.Get(key)
	if raw == "" {
		return true
	}
	val := T(raw)
	if !val.IsValid() {
		respondError(w, http.StatusBadRequest, "invalid "+key, ErrCodeBadRequest)
		return false
	}
	*dst = val
	return true
}

// ParseUUIDParam extracts a UUID query param and writes a 400 on parse
// failure. Returns true if the param is absent (no-op) or valid.
func ParseUUIDParam(w http.ResponseWriter, q url.Values, key string, dst **uuid.UUID) bool {
	raw := q.Get(key)
	if raw == "" {
		return true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid "+key, ErrCodeBadRequest)
		return false
	}
	*dst = &id
	return true
}

// ParseTimeParam extracts a time query param using the given layout and writes
// a 400 on parse failure. Returns true if the param is absent (no-op) or valid.
func ParseTimeParam(w http.ResponseWriter, q url.Values, key, layout string, dst **time.Time) bool {
	raw := q.Get(key)
	if raw == "" {
		return true
	}
	t, err := time.Parse(layout, raw)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid "+key, ErrCodeBadRequest)
		return false
	}
	*dst = &t
	return true
}
