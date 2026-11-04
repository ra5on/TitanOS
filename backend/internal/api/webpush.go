package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// Notifications on this device (Web Push, docs/WEB-PUSH.md): a browser or a
// web app on the home screen subscribes and then gets what the bell shows,
// from the level set under "send from". Owners and administrators only,
// like the bell.

// pushArea needs the push subscriptions and, for the test message, the
// wording of notifications (narrow dependencies, see fansArea).
type pushArea struct {
	push   *webpush.Manager
	notify *notify.Manager
}

func (a pushArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/notifications/push", admin(a.handlePushDevices))
	mux.HandleFunc("POST /api/v1/notifications/push", admin(a.handlePushSubscribe))
	mux.HandleFunc("DELETE /api/v1/notifications/push/{id}", admin(a.handlePushRemove))
	mux.HandleFunc("POST /api/v1/notifications/push/{id}/test", admin(a.handlePushTest))
}

func (a pushArea) handlePushDevices(w http.ResponseWriter, r *http.Request) {
	key, err := a.push.PublicKey()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicKey": key, "devices": a.push.Devices(currentUser(r).Name)})
}

func (a pushArea) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string               `json:"name"`
		Subscription webpush.Subscription `json:"subscription"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	device, err := a.push.Subscribe(currentUser(r).Name, req.Name, req.Subscription)
	switch {
	case errors.Is(err, webpush.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_push_subscription", err.Error())
	case errors.Is(err, webpush.ErrTooMany):
		writeError(w, http.StatusConflict, "too_many_push_devices", err.Error())
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	default:
		writeJSON(w, http.StatusOK, device)
	}
}

func (a pushArea) handlePushRemove(w http.ResponseWriter, r *http.Request) {
	err := a.push.Remove(currentUser(r).Name, r.PathValue("id"))
	switch {
	case errors.Is(err, webpush.ErrNotFound):
		writeError(w, http.StatusNotFound, "push_device_not_found", err.Error())
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a pushArea) handlePushTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	title, text := a.notify.Wording("test", "")
	err := a.push.Test(ctx, currentUser(r).Name, r.PathValue("id"), webpush.Message{Title: title, Body: text, Tag: "test", URL: "/settings/notifications"})
	switch {
	case errors.Is(err, webpush.ErrNotFound):
		writeError(w, http.StatusNotFound, "push_device_not_found", err.Error())
	case errors.Is(err, webpush.ErrGone):
		writeError(w, http.StatusGone, "push_device_gone", err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, "notify_test_failed", err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
