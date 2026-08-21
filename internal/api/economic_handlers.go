package api

import "net/http"

func (s *Server) handleListEconomicAccounts(w http.ResponseWriter, r *http.Request) {
	if s.economicAccounts == nil {
		respondError(w, http.StatusNotImplemented, "economic account reads are disabled", ErrCodeNotImplemented)
		return
	}
	limit, offset := parsePagination(r)
	accounts, err := s.economicAccounts.List(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list economic accounts", ErrCodeInternal)
		return
	}
	respondList(w, accounts, limit, offset)
}

func (s *Server) handleGetEconomicAccount(w http.ResponseWriter, r *http.Request) {
	if s.economicAccounts == nil {
		respondError(w, http.StatusNotImplemented, "economic account reads are disabled", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	account, err := s.economicAccounts.GetByID(r.Context(), id)
	if err != nil {
		respondEconomicReadError(w, err, "economic account not found", "failed to get economic account")
		return
	}
	respondJSON(w, http.StatusOK, account)
}

func (s *Server) handleListEconomicCapitalFlows(w http.ResponseWriter, r *http.Request) {
	if s.economicAccounts == nil {
		respondError(w, http.StatusNotImplemented, "economic account reads are disabled", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	limit, offset := parsePagination(r)
	if _, err = s.economicAccounts.GetByID(r.Context(), id); err != nil {
		respondEconomicReadError(w, err, "economic account not found", "failed to get economic account")
		return
	}
	flows, err := s.economicAccounts.ListCapitalFlows(r.Context(), id, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list economic capital flows", ErrCodeInternal)
		return
	}
	respondList(w, flows, limit, offset)
}

func (s *Server) handleGetEconomicCapitalSummary(w http.ResponseWriter, r *http.Request) {
	if s.economicAccounts == nil {
		respondError(w, http.StatusNotImplemented, "economic account reads are disabled", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	summary, err := s.economicAccounts.GetCapitalSummary(r.Context(), id)
	if err != nil {
		respondEconomicReadError(w, err, "economic account not found", "failed to summarize economic account")
		return
	}
	respondJSON(w, http.StatusOK, summary)
}

func (s *Server) handleGetEconomicLedgerTransaction(w http.ResponseWriter, r *http.Request) {
	if s.economicLedger == nil {
		respondError(w, http.StatusNotImplemented, "economic ledger reads are disabled", ErrCodeNotImplemented)
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), ErrCodeBadRequest)
		return
	}
	transaction, err := s.economicLedger.GetByID(r.Context(), id)
	if err != nil {
		respondEconomicReadError(w, err, "ledger transaction not found", "failed to get ledger transaction")
		return
	}
	respondJSON(w, http.StatusOK, transaction)
}

func respondEconomicReadError(w http.ResponseWriter, err error, notFoundMessage, internalMessage string) {
	if isNotFound(err) {
		respondError(w, http.StatusNotFound, notFoundMessage, ErrCodeNotFound)
		return
	}
	respondError(w, http.StatusInternalServerError, internalMessage, ErrCodeInternal)
}
