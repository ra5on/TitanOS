package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/diagnose"
)

// handleLogs returns lines of the journal. The log names accounts and
// addresses and is for administrators.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q := diagnose.Query{Source: query.Get("source"), Priority: 6, After: query.Get("after"), Search: query.Get("search")}
	if value := query.Get("priority"); value != "" {
		priority, err := strconv.Atoi(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "priority must be a number from 0 to 7")
			return
		}
		q.Priority = priority
	}
	q.Lines, _ = strconv.Atoi(query.Get("lines"))
	logs, err := s.opts.Diagnose.Logs(r.Context(), q)
	if err != nil {
		if errors.Is(err, diagnose.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		slog.Error("reading the journal failed", "err", err)
		writeError(w, http.StatusInternalServerError, "logs_failed", "the log could not be read")
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// handleDiagnosticBundle packs logs and the state of the system into a zip
// file for a bug report, with addresses, names and secrets taken out.
func (s *Server) handleDiagnosticBundle(w http.ResponseWriter, r *http.Request) {
	var accounts []string
	for _, user := range s.opts.Users.List() {
		accounts = append(accounts, user.Name)
	}
	slog.Info("diagnostic bundle requested", "by", currentUser(r).Name, "ip", remoteIP(r))
	// The name of the NAS is one of the things a bundle hides.
	name := "titanos-diagnose-" + time.Now().Format("20060102-150405") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if err := s.opts.Diagnose.Bundle(r.Context(), w, accounts); err != nil {
		// The answer has begun; all that is left is to cut it short.
		slog.Error("diagnostic bundle failed", "err", err)
		panic(http.ErrAbortHandler)
	}
}
