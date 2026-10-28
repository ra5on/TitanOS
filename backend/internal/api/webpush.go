package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// Notifications on this device (Web Push, docs/WEB-PUSH.md): a browser or a
// web app on the home screen subscribes and then gets what the bell shows,
// from the level set under "send from". Owners and administrators only,
// like the bell.

func (s *Server) handlePushDevices(w http.ResponseWriter, r *http.Request) {
	key, err := s.opts.WebPush.PublicKey()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicKey": key, "devices": s.opts.WebPush.Devices(currentUser(r).Name)})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string               `json:"name"`
		Subscription webpush.Subscription `json:"subscription"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	device, err := s.opts.WebPush.Subscribe(currentUser(r).Name, req.Name, req.Subscription)
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

func (s *Server) handlePushRemove(w http.ResponseWriter, r *http.Request) {
	err := s.opts.WebPush.Remove(currentUser(r).Name, r.PathValue("id"))
	switch {
	case errors.Is(err, webpush.ErrNotFound):
		writeError(w, http.StatusNotFound, "push_device_not_found", err.Error())
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	title, text := s.opts.Notify.Wording("test", "")
	err := s.opts.WebPush.Test(ctx, currentUser(r).Name, r.PathValue("id"), webpush.Message{Title: title, Body: text, Tag: "test", URL: "/settings/notifications"})
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
