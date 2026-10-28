package api

import (
	"errors"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/healthreport"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// Settings › Notifications › Health report (docs/HEALTH-REPORT.md).

func (s *Server) healthReportAnswer(w http.ResponseWriter, preview *healthreport.Report) {
	r := s.opts.HealthReport
	if preview == nil {
		p := r.Preview()
		preview = &p
	}
	answer := map[string]any{"settings": r.Settings(), "preview": preview}
	if next := r.Next(); !next.IsZero() {
		answer["next"] = next
	}
	writeJSON(w, http.StatusOK, answer)
}

func (s *Server) handleHealthReport(w http.ResponseWriter, r *http.Request) {
	s.healthReportAnswer(w, nil)
}

func (s *Server) handleHealthReportSet(w http.ResponseWriter, r *http.Request) {
	var req healthreport.Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.HealthReport.Set(req); err != nil {
		if errors.Is(err, healthreport.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_health_report", err.Error())
			return
		}
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	s.healthReportAnswer(w, nil)
}

func (s *Server) handleHealthReportSend(w http.ResponseWriter, r *http.Request) {
	report, err := s.opts.HealthReport.SendNow()
	switch {
	case errors.Is(err, notify.ErrNoChannel):
		writeError(w, http.StatusConflict, "no_notify_channel", err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, "notify_test_failed", err.Error())
	default:
		s.healthReportAnswer(w, &report)
	}
}
