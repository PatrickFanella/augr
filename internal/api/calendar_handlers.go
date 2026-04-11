package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func (s *Server) handleGetEarningsCalendar(w http.ResponseWriter, r *http.Request) {
	if s.eventsProvider == nil {
		respondError(w, http.StatusNotImplemented, "events provider not configured", ErrCodeNotImplemented)
		return
	}

	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	events, err := s.eventsProvider.GetEarningsCalendar(r.Context(), from, to)
	if err != nil {
		s.logger.Error("earnings calendar request failed", "error", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch earnings calendar", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, events)
}

func (s *Server) handleGetEconomicCalendar(w http.ResponseWriter, r *http.Request) {
	if s.eventsProvider == nil {
		respondError(w, http.StatusNotImplemented, "events provider not configured", ErrCodeNotImplemented)
		return
	}

	events, err := s.eventsProvider.GetEconomicCalendar(r.Context())
	if err != nil {
		if isEconomicCalendarAccessDenied(err) {
			s.logger.Warn("economic calendar provider access denied; returning degraded empty response", "error", err)
			respondJSON(w, http.StatusOK, []domain.EconomicEvent{})
			return
		}
		s.logger.Error("economic calendar request failed", "error", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch economic calendar", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, events)
}

func isEconomicCalendarAccessDenied(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "status=403") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "don't have access to this resource")
}

func (s *Server) handleGetFilings(w http.ResponseWriter, r *http.Request) {
	if s.eventsProvider == nil {
		respondError(w, http.StatusNotImplemented, "events provider not configured", ErrCodeNotImplemented)
		return
	}

	ticker := strings.TrimSpace(r.URL.Query().Get("ticker"))
	form := strings.TrimSpace(r.URL.Query().Get("form"))

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30)
	to := now

	if v := r.URL.Query().Get("from"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid 'from' format, expected YYYY-MM-DD", ErrCodeValidation)
			return
		}
		from = parsed
	}
	if v := r.URL.Query().Get("to"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid 'to' format, expected YYYY-MM-DD", ErrCodeValidation)
			return
		}
		to = parsed
	}

	filings, err := s.eventsProvider.GetFilings(r.Context(), ticker, form, from, to)
	if err != nil {
		s.logger.Error("filings request failed", "ticker", ticker, "form", form, "error", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch filings", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, filings)
}

func (s *Server) handleGetIPOCalendar(w http.ResponseWriter, r *http.Request) {
	if s.eventsProvider == nil {
		respondError(w, http.StatusNotImplemented, "events provider not configured", ErrCodeNotImplemented)
		return
	}

	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	events, err := s.eventsProvider.GetIPOCalendar(r.Context(), from, to)
	if err != nil {
		s.logger.Error("ipo calendar request failed", "error", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch IPO calendar", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, events)
}

// parseDateRange extracts from/to query params with a default of today -> today+30 days.
// Returns false if a parse error was written to the response.
func parseDateRange(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	now := time.Now().UTC()
	from = now
	to = now.AddDate(0, 0, 30)

	if v := r.URL.Query().Get("from"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid 'from' format, expected YYYY-MM-DD", ErrCodeValidation)
			return time.Time{}, time.Time{}, false
		}
		from = parsed
	}
	if v := r.URL.Query().Get("to"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid 'to' format, expected YYYY-MM-DD", ErrCodeValidation)
			return time.Time{}, time.Time{}, false
		}
		to = parsed
	}

	return from, to, true
}
