package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
)

// IPv64.net beyond the DNS records: account and limits, domains and their
// records, and the healthcheck that watches whether the NAS is reachable.
// The API key comes with the request (typed in, not saved yet) or is the
// one stored for dynamic DNS or Let's Encrypt; it never goes back out, nor
// do update or healthcheck tokens.

type ipv64Request struct {
	APIKey string `json:"apiKey"`
}

// ipv64Options lets tests point the client at a fake.
var ipv64Options dnsprovider.Options

// lastIPv64Test keeps test messages one per 30 seconds, as IPv64 asks.
var lastIPv64Test struct {
	sync.Mutex
	at time.Time
}

func (s *Server) ipv64Client(w http.ResponseWriter, key string) (*dnsprovider.IPv64, bool) {
	if key == "" && s.opts.DDNS != nil {
		key = s.opts.DDNS.IPv64Key()
	}
	if key == "" && s.opts.LetsEncrypt != nil {
		key = s.opts.LetsEncrypt.IPv64Key()
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "ipv64_no_key", "the API key of the IPv64 account is needed")
		return nil, false
	}
	return dnsprovider.NewIPv64(key, "", ipv64Options), true
}

func ipv64Failure(w http.ResponseWriter, err error) {
	var apiErr *dnsprovider.APIError
	switch {
	case errors.Is(err, dnsprovider.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_ipv64", err.Error())
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized:
		writeError(w, http.StatusBadGateway, "ipv64_unauthorized", apiErr.Error())
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests:
		writeError(w, http.StatusTooManyRequests, "ipv64_rate_limited", apiErr.Error())
	case errors.As(err, &apiErr):
		writeError(w, http.StatusBadGateway, "ipv64_error", apiErr.Error())
	default:
		writeError(w, http.StatusBadGateway, "ipv64_unreachable", err.Error())
	}
}

func ipv64Context(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Second)
}

func (s *Server) handleIPv64Account(w http.ResponseWriter, r *http.Request) {
	var req ipv64Request
	if !readJSON(w, r, &req) {
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	account, err := client.Account(ctx)
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) handleIPv64Domains(w http.ResponseWriter, r *http.Request) {
	var req ipv64Request
	if !readJSON(w, r, &req) {
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	domains, err := client.Domains(ctx)
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": domains, "zones": dnsprovider.IPv64Zones})
}

func (s *Server) handleIPv64DomainCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ipv64Request
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	domain, err := client.CreateDomain(ctx, req.Name)
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	noteTarget(r, domain.Name)
	slog.Info("IPv64 domain created", "domain", domain.Name, "by", currentUser(r).Name)
	writeJSON(w, http.StatusCreated, domain)
}

func (s *Server) handleIPv64DomainChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ipv64Request
		Domain string `json:"domain"`
		dnsprovider.IPv64DomainChange
	}
	if !readJSON(w, r, &req) {
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	domain, err := client.ChangeDomain(ctx, req.Domain, req.IPv64DomainChange)
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	noteTarget(r, domain.Name)
	writeJSON(w, http.StatusOK, domain)
}

func (s *Server) handleIPv64Records(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ipv64Request
		Domain string `json:"domain"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	records, err := client.Records(ctx, req.Domain, "", "")
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records, "types": dnsprovider.IPv64RecordTypes})
}

func (s *Server) handleIPv64RecordAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ipv64Request
		Domain  string `json:"domain"`
		Type    string `json:"type"`
		Prefix  string `json:"prefix"`
		Content string `json:"content"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	record, err := client.AddRecord(ctx, req.Domain, req.Type, req.Prefix, req.Content)
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	noteTarget(r, req.Domain, req.Type)
	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) handleIPv64RecordDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ipv64Request
		Domain string `json:"domain"`
		ID     int64  `json:"id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.ID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "no record")
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	if err := client.DeleteRecord(ctx, req.ID); err != nil {
		ipv64Failure(w, err)
		return
	}
	noteTarget(r, req.Domain)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleIPv64Healthchecks(w http.ResponseWriter, r *http.Request) {
	var req ipv64Request
	if !readJSON(w, r, &req) {
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	checks, err := client.Healthchecks(ctx)
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	integrations, err := client.Integrations(ctx)
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	current := ""
	if s.opts.DDNS != nil {
		if token := s.opts.DDNS.HeartbeatToken(); token != "" {
			current = dnsprovider.HealthcheckID(token)
		}
	}
	hostname, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{"healthchecks": checks, "integrations": integrations, "current": current, "suggestedName": "titanos-" + hostname})
}

// handleIPv64HeartbeatOn creates a push healthcheck (or takes an existing
// one) and switches the pings on. Nothing happens at IPv64 before this.
func (s *Server) handleIPv64HeartbeatOn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ipv64Request
		// ID picks an existing check; empty creates one named Name.
		ID           string `json:"id"`
		Name         string `json:"name"`
		Period       int    `json:"periodMinutes"`
		Grace        int    `json:"graceMinutes"`
		Integrations []int  `json:"integrations"`
		AutoPause    bool   `json:"autoPause"`
	}
	if !readJSON(w, r, &req) || !s.dynamicDNS(w) {
		return
	}
	if req.Period == 0 {
		req.Period = 30
	}
	if req.Grace == 0 {
		req.Grace = 10
	}
	if req.Period < 20 || req.Period > 1440 || req.Grace < 1 || req.Grace > 1440 {
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "period 20–1440 minutes, grace 1–1440 minutes")
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	var check dnsprovider.IPv64Healthcheck
	if req.ID != "" {
		checks, err := client.Healthchecks(ctx)
		if err != nil {
			ipv64Failure(w, err)
			return
		}
		found := false
		for _, c := range checks {
			if c.ID == req.ID {
				check, found = c, true
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "ipv64_no_healthcheck", "no such healthcheck")
			return
		}
		if check.Period > 0 {
			req.Period = max(check.Period/60, 20)
		}
	} else {
		if req.Integrations == nil {
			req.Integrations = []int{0}
		}
		created, err := client.CreateHealthcheck(ctx, req.Name, time.Duration(req.Period)*time.Minute, time.Duration(req.Grace)*time.Minute, req.Integrations)
		if err != nil {
			ipv64Failure(w, err)
			return
		}
		check = created
	}
	if err := s.opts.DDNS.SetHeartbeat(check.Token(), check.Name, req.Period, req.AutoPause); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	noteTarget(r, check.Name)
	slog.Info("IPv64 healthcheck switched on", "check", check.Name, "by", currentUser(r).Name)
	writeJSON(w, http.StatusOK, s.opts.DDNS.Status())
}

// handleIPv64HeartbeatOff stops the pings and pauses or deletes the check
// at IPv64, as the dialog asked.
func (s *Server) handleIPv64HeartbeatOff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ipv64Request
		// Action is "pause", "delete" or "" (only stop pinging).
		Action string `json:"action"`
	}
	if !readJSON(w, r, &req) || !s.dynamicDNS(w) {
		return
	}
	if req.Action != "" && req.Action != "pause" && req.Action != "delete" {
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "action")
		return
	}
	token := s.opts.DDNS.HeartbeatToken()
	if req.Action != "" && token != "" {
		client, ok := s.ipv64Client(w, req.APIKey)
		if !ok {
			return
		}
		ctx, cancel := ipv64Context(r)
		defer cancel()
		var err error
		if req.Action == "delete" {
			err = client.DeleteHealthcheck(ctx, token)
		} else {
			err = client.PauseHealthcheck(ctx, token)
		}
		var apiErr *dnsprovider.APIError
		if err != nil && !(errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound) {
			ipv64Failure(w, err)
			return
		}
	}
	if err := s.opts.DDNS.StopHeartbeat(req.Action == "delete"); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	noteTarget(r, req.Action)
	writeJSON(w, http.StatusOK, s.opts.DDNS.Status())
}

func (s *Server) handleIPv64HeartbeatStatus(w http.ResponseWriter, r *http.Request) {
	var req ipv64Request
	if !readJSON(w, r, &req) || !s.dynamicDNS(w) {
		return
	}
	token := s.opts.DDNS.HeartbeatToken()
	if token == "" {
		writeError(w, http.StatusConflict, "ipv64_no_healthcheck", "no healthcheck chosen")
		return
	}
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	check, err := client.Healthcheck(ctx, token)
	if err != nil {
		ipv64Failure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, check)
}

func (s *Server) handleIPv64HeartbeatTest(w http.ResponseWriter, r *http.Request) {
	var req ipv64Request
	if !readJSON(w, r, &req) || !s.dynamicDNS(w) {
		return
	}
	token := s.opts.DDNS.HeartbeatToken()
	if token == "" {
		writeError(w, http.StatusConflict, "ipv64_no_healthcheck", "no healthcheck chosen")
		return
	}
	lastIPv64Test.Lock()
	wait := 30*time.Second - time.Since(lastIPv64Test.at)
	if wait > 0 {
		lastIPv64Test.Unlock()
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusTooManyRequests, "ipv64_rate_limited", "one test message every 30 seconds")
		return
	}
	lastIPv64Test.at = time.Now()
	lastIPv64Test.Unlock()
	client, ok := s.ipv64Client(w, req.APIKey)
	if !ok {
		return
	}
	ctx, cancel := ipv64Context(r)
	defer cancel()
	if err := client.TestNotification(ctx, token); err != nil {
		ipv64Failure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}
