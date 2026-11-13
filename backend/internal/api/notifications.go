package api

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/system"
)

// notifyArea is the notifications, where they go and the heat alarms; an
// area with narrow dependencies (PLAN 8a, see fansArea). monitor names the
// temperature sensors; nil lists none.
type notifyArea struct {
	notify  *notify.Manager
	monitor *system.Monitor
}

func (a notifyArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/notifications", admin(a.handleNotifications))
	mux.HandleFunc("POST /api/v1/notifications/dismiss", admin(a.handleDismissNotifications))
	mux.HandleFunc("GET /api/v1/notifications/settings", admin(a.handleNotifySettings))
	mux.HandleFunc("PUT /api/v1/notifications/settings", admin(a.handleNotifySettingsChange))
	mux.HandleFunc("POST /api/v1/notifications/test", admin(a.handleNotifyTest))
	mux.HandleFunc("GET /api/v1/notifications/temperatures", admin(a.handleTemperatureAlarms))
	mux.HandleFunc("PUT /api/v1/notifications/temperatures", admin(a.handleTemperatureAlarmsChange))
}

// handleNotifications lists what the bell of this account shows: every
// message but those meant for other accounts (a camera's recipients).
func (a notifyArea) handleNotifications(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"messages": a.notify.MessagesFor(currentUser(r).Name)})
}

func (a notifyArea) handleDismissNotifications(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.IDs) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_request", "too many notification IDs")
		return
	}
	for _, id := range req.IDs {
		if id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid notification ID")
			return
		}
	}
	a.notify.Dismiss(currentUser(r).Name, req.IDs)
	a.handleNotifications(w, r)
}

// handleNotifySettings shows where messages go. Passwords and tokens are
// left out; secretSet says which of them are stored.
func (a notifyArea) handleNotifySettings(w http.ResponseWriter, r *http.Request) {
	settings, set := a.notify.Settings()
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "secretSet": set})
}

func (a notifyArea) handleNotifySettingsChange(w http.ResponseWriter, r *http.Request) {
	// ClearSecrets names the channels whose stored password or token is
	// to be dropped; an empty one in the settings keeps what is stored.
	var req struct {
		notify.Settings
		ClearSecrets []string `json:"clearSecrets"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.notify.SetSettings(req.Settings, req.ClearSecrets...); err != nil {
		switch {
		case errors.Is(err, notify.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid_notify_settings", err.Error())
		case errors.Is(err, notify.ErrNotLoaded):
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		}
		return
	}
	slog.Info("notification settings changed", "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleNotifySettings(w, r)
}

// handleNotifyTest sends a test message through one channel with the
// stored settings and says what the other side answered.
func (a notifyArea) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	var req struct{ Channel string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.notify.Test(req.Channel); err != nil {
		if errors.Is(err, notify.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_notify_settings", err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "notify_test_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// accountNotifyArea is where the NAS reaches one's own account; an area
// with narrow dependencies (PLAN 8a, see fansArea).
type accountNotifyArea struct {
	notify *notify.Manager
}

func (a accountNotifyArea) register(mux *routeMux, user func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/account/notifications", user(a.handleAccountNotify))
	mux.HandleFunc("PUT /api/v1/account/notifications", user(a.handleAccountNotifyChange))
	mux.HandleFunc("POST /api/v1/account/notifications/test", user(a.handleAccountNotifyTest))
}

// handleAccountNotify shows where the NAS reaches one's own account.
func (a accountNotifyArea) handleAccountNotify(w http.ResponseWriter, r *http.Request) {
	settings, set, err := a.notify.PersonalSettings(currentUser(r).Name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	general, _ := a.notify.Settings()
	writeJSON(w, http.StatusOK, map[string]any{"delivery": a.notify.Deliveries(currentUser(r).Name), "settings": settings, "secretSet": set, "mailServer": general.Email.Host != ""})
}

func (a accountNotifyArea) handleAccountNotifyChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		notify.Personal
		ClearSecrets []string `json:"clearSecrets"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.notify.SetPersonal(currentUser(r).Name, req.Personal, req.ClearSecrets...); err != nil {
		if errors.Is(err, notify.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_notify_settings", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		return
	}
	a.handleAccountNotify(w, r)
}

func (a accountNotifyArea) handleAccountNotifyTest(w http.ResponseWriter, r *http.Request) {
	var req struct{ Channel string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.notify.TestPersonal(currentUser(r).Name, req.Channel); err != nil {
		if errors.Is(err, notify.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_notify_settings", err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "notify_test_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// temperatureAlarm is one sensor of Settings › Temperatures › Heat
// notifications: from which temperature it notifies (nil: never, a chip
// without a limit of its own) and whether it is muted.
type temperatureAlarm struct {
	ID    string   `json:"id"`
	Limit *float64 `json:"limit"`
	// Own: the limit comes from the chip's critical value, else from the
	// default for its kind of chip.
	Own   bool `json:"own"`
	Muted bool `json:"muted"`
	// Shutdown: this sensor can switch the NAS off on heat (PLAN 75-9).
	Shutdown bool `json:"shutdown"`
}

// handleTemperatureAlarms lists the sensors with their limits and the
// muted ones, also those that are not there now.
func (a notifyArea) handleTemperatureAlarms(w http.ResponseWriter, r *http.Request) {
	muted := a.notify.MutedTemperatures()
	sensors := []temperatureAlarm{}
	if a.monitor != nil {
		for _, t := range a.monitor.Info().Temperatures {
			alarm := temperatureAlarm{ID: t.ID, Muted: slices.Contains(muted, t.ID)}
			_, alarm.Shutdown = notify.ShutsDownOnHeat(t.Chip, t.CriticalC)
			if limit, own, ok := notify.AlarmLimitSource(t.Chip, t.CriticalC); ok {
				alarm.Limit, alarm.Own = &limit, own
			}
			sensors = append(sensors, alarm)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sensors": sensors, "muted": muted, "heatShutdown": a.notify.HeatShutdown()})
}

func (a notifyArea) handleTemperatureAlarmsChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Muted []string `json:"muted"`
		// HeatShutdown is optional: an older page sends only the mutes.
		HeatShutdown *bool `json:"heatShutdown"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	err := a.notify.SetMutedTemperatures(req.Muted)
	if err == nil && req.HeatShutdown != nil {
		err = a.notify.SetHeatShutdown(*req.HeatShutdown)
	}
	if err != nil {
		switch {
		case errors.Is(err, notify.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid_notify_settings", err.Error())
		case errors.Is(err, notify.ErrNotLoaded):
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "config_write_failed", err.Error())
		}
		return
	}
	slog.Info("temperature notifications changed", "muted", len(req.Muted), "heatShutdown", req.HeatShutdown != nil && *req.HeatShutdown, "by", currentUser(r).Name, "ip", remoteIP(r))
	a.handleTemperatureAlarms(w, r)
}
