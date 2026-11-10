package api

import (
	"errors"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/healthreport"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// Settings › Notifications › Health report (docs/HEALTH-REPORT.md).

// healthReportArea is an area with narrow dependencies (PLAN 8a, see fansArea).
type healthReportArea struct {
	report *healthreport.Manager
}

func (a healthReportArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/health-report", admin(a.handleHealthReport))
	mux.HandleFunc("PUT /api/v1/health-report", admin(a.handleHealthReportSet))
	mux.HandleFunc("POST /api/v1/health-report/send", admin(a.handleHealthReportSend))
}

func (a healthReportArea) healthReportAnswer(w http.ResponseWriter, preview *healthreport.Report) {
	r := a.report
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

func (a healthReportArea) handleHealthReport(w http.ResponseWriter, r *http.Request) {
	a.healthReportAnswer(w, nil)
}

func (a healthReportArea) handleHealthReportSet(w http.ResponseWriter, r *http.Request) {
	var req healthreport.Settings
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.report.Set(req); err != nil {
		if errors.Is(err, healthreport.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_health_report", err.Error())
			return
		}
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	a.healthReportAnswer(w, nil)
}

func (a healthReportArea) handleHealthReportSend(w http.ResponseWriter, r *http.Request) {
	report, err := a.report.SendNow()
	switch {
	case errors.Is(err, notify.ErrNoChannel):
		writeError(w, http.StatusConflict, "no_notify_channel", err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, "notify_test_failed", err.Error())
	default:
		a.healthReportAnswer(w, &report)
	}
}
