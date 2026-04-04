package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func (s *Server) handleGetOptionsChain(w http.ResponseWriter, r *http.Request) {
	if s.optionsProvider == nil {
		respondError(w, http.StatusNotImplemented, "options data provider not configured", ErrCodeNotImplemented)
		return
	}

	underlying := strings.TrimSpace(chi.URLParam(r, "underlying"))
	if underlying == "" {
		respondError(w, http.StatusBadRequest, "underlying ticker is required", ErrCodeBadRequest)
		return
	}

	var expiry time.Time
	if v := r.URL.Query().Get("expiry"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid expiry format, expected YYYY-MM-DD", ErrCodeValidation)
			return
		}
		expiry = parsed
	}

	var optionType domain.OptionType
	if v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type"))); v != "" {
		switch v {
		case "call":
			optionType = domain.OptionTypeCall
		case "put":
			optionType = domain.OptionTypePut
		default:
			respondError(w, http.StatusBadRequest, "type must be 'call' or 'put'", ErrCodeValidation)
			return
		}
	}

	snapshots, err := s.optionsProvider.GetOptionsChain(r.Context(), underlying, expiry, optionType)
	if err != nil {
		s.logger.Error("options chain request failed", "underlying", underlying, "error", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch options chain", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, snapshots)
}

func (s *Server) handleGetOptionsContractBars(w http.ResponseWriter, r *http.Request) {
	if s.optionsProvider == nil {
		respondError(w, http.StatusNotImplemented, "options data provider not configured", ErrCodeNotImplemented)
		return
	}

	symbol := strings.TrimSpace(chi.URLParam(r, "symbol"))
	if symbol == "" {
		respondError(w, http.StatusBadRequest, "contract symbol is required", ErrCodeBadRequest)
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		respondError(w, http.StatusBadRequest, "'from' and 'to' query params are required (YYYY-MM-DD)", ErrCodeValidation)
		return
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid 'from' format, expected YYYY-MM-DD", ErrCodeValidation)
		return
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid 'to' format, expected YYYY-MM-DD", ErrCodeValidation)
		return
	}

	timeframe := data.Timeframe1d
	if v := strings.TrimSpace(r.URL.Query().Get("timeframe")); v != "" {
		timeframe = data.Timeframe(v)
	}

	bars, err := s.optionsProvider.GetOptionsOHLCV(r.Context(), symbol, timeframe, from, to)
	if err != nil {
		s.logger.Error("options contract bars request failed", "symbol", symbol, "error", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch contract bars", ErrCodeInternal)
		return
	}

	respondJSON(w, http.StatusOK, bars)
}
