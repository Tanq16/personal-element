package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"encoding/json/v2"

	"element-agent/internal/matrix"
)

func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(value, "Bearer ")
	if !ok {
		return ""
	}
	return token
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.MarshalWrite(w, payload); err != nil {
		log.Error().Err(err).Msg("failed to write response")
	}
}

func writeMatrixError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"errcode": code, "error": message})
}

func (s *Server) handleTransaction(w http.ResponseWriter, r *http.Request) {
	txnID := r.PathValue("txnId")
	if !s.seen.add(txnID) {
		log.Debug().Str("txn", txnID).Msg("duplicate transaction")
		writeJSON(w, http.StatusOK, struct{}{})
		return
	}

	var txn matrix.Transaction
	if err := json.UnmarshalRead(r.Body, &txn); err != nil {
		log.Error().Err(err).Str("txn", txnID).Msg("failed to decode transaction")
		writeMatrixError(w, http.StatusBadRequest, "M_NOT_JSON", "malformed transaction")
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), dispatchBudget)
	defer cancel()
	for _, event := range txn.Events {
		s.handleEvent(ctx, event)
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleQueryUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	for _, name := range s.state.Names() {
		if s.mx.UserID(name) == userID {
			writeJSON(w, http.StatusOK, struct{}{})
			return
		}
	}
	writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "no such agent")
}
