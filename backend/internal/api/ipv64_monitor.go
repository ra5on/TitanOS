package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
)

// Monitoring and failover at IPv64: healthchecks of every type, their
// events, incidents and statistics, integrations, status pages, offline
// pages, DNS failover policies and the one-click maintenance page.

// --- healthchecks ---

func (s *Server) handleIPv64Checks(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		checks, err := c.Checks(ctx)
		if err != nil {
			return 0, nil, err
		}
		s.ipv64.rememberChecks(checks)
		integrations, err := c.FullIntegrations(ctx)
		return 0, map[string]any{"healthchecks": checks, "integrations": integrations, "nasCheck": s.nasCheckID()}, err
	})
}

func (s *Server) checkParam(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (string, string, error) {
	id, err := pathRef(r, "id")
	if err != nil {
		return "", "", err
	}
	token, err := s.checkToken(ctx, c, id)
	return id, token, err
}

func (s *Server) handleIPv64Check(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		_, token, err := s.checkParam(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		check, err := c.Check(ctx, token)
		return 0, check, err
	})(w, r)
}

func (s *Server) handleIPv64CheckCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		var in dnsprovider.HealthcheckInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		if in.Name != nil {
			noteTarget(r, *in.Name)
		}
		check, err := c.CreateCheck(ctx, in)
		if err == nil {
			s.ipv64.rememberChecks([]dnsprovider.IPv64Check{check})
		}
		return http.StatusCreated, check, err
	})(w, r)
}

func (s *Server) handleIPv64CheckChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		_, token, err := s.checkParam(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		var in dnsprovider.HealthcheckInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		check, err := c.ChangeCheck(ctx, token, in)
		noteTarget(r, check.Name)
		return 0, check, err
	})(w, r)
}

func (s *Server) handleIPv64CheckDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, token, err := s.checkParam(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, id)
		if err := c.DeleteHealthcheck(ctx, token); err != nil {
			return 0, nil, err
		}
		// The check of "watch reachability": the NAS stops pinging it.
		if s.opts.DDNS != nil && s.opts.DDNS.HeartbeatToken() == token {
			if err := s.opts.DDNS.StopHeartbeat(true); err != nil {
				slog.Error("heartbeat not stopped after its check was deleted", "err", err)
			}
		}
		return 0, nil, nil
	})(w, r)
}

func (s *Server) handleIPv64CheckAction(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, token, err := s.checkParam(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		action := r.PathValue("action")
		noteTarget(r, id, action)
		if action == "test-notification" {
			if !s.ipv64.testAllowed("check:"+id, time.Now()) {
				return 0, nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusTooManyRequests, Message: "one test message every 30 seconds", RetryAfter: 30 * time.Second}
			}
			return 0, map[string]bool{"sent": true}, c.TestNotification(ctx, token)
		}
		check, err := c.CheckAction(ctx, token, action)
		if err != nil {
			return 0, nil, err
		}
		s.ipv64.rememberChecks([]dnsprovider.IPv64Check{check})
		followed := false
		if action == "rotate-token" && s.opts.DDNS != nil {
			if followed, err = s.opts.DDNS.ReplaceTokens("", "", token, check.Token()); err != nil {
				slog.Error("new healthcheck token not stored", "err", err)
			}
		}
		return 0, map[string]any{"healthcheck": check, "nasUpdated": followed}, nil
	})(w, r)
}

func (s *Server) handleIPv64CheckEvents(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		_, token, err := s.checkParam(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		page, err := c.Events(ctx, token, 50, max(before, 0))
		return 0, page, err
	})(w, r)
}

func (s *Server) handleIPv64CheckIncidents(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		_, token, err := s.checkParam(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		list, err := c.Incidents(ctx, token, 20)
		return 0, map[string]any{"incidents": list}, err
	})(w, r)
}

func (s *Server) handleIPv64CheckStats(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		_, token, err := s.checkParam(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		rangeName := r.URL.Query().Get("range")
		if rangeName == "" {
			rangeName = "24h"
		}
		stats, err := c.Stats(ctx, token, rangeName)
		return 0, stats, err
	})(w, r)
}

// --- integrations ---

func (s *Server) handleIPv64Integrations(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		list, err := c.FullIntegrations(ctx)
		return 0, map[string]any{"integrations": list, "types": dnsprovider.IPv64IntegrationTypes}, err
	})
}

func (s *Server) handleIPv64IntegrationCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		var in dnsprovider.IntegrationInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		noteTarget(r, in.Name, in.Type)
		i, err := c.CreateIntegration(ctx, in)
		return http.StatusCreated, i, err
	})(w, r)
}

func (s *Server) handleIPv64IntegrationChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathInt(r, "id")
		if err != nil {
			return 0, nil, err
		}
		var req struct {
			Name    *string `json:"name"`
			Default *bool   `json:"default"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		i, err := c.ChangeIntegration(ctx, int(id), req.Name, req.Default)
		noteTarget(r, i.Name)
		return 0, i, err
	})(w, r)
}

func (s *Server) handleIPv64IntegrationDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathInt(r, "id")
		if err != nil || id == 0 {
			return 0, nil, invalidIPv64("the account e-mail cannot be deleted")
		}
		noteTarget(r, strconv.FormatInt(id, 10))
		return 0, nil, c.DeleteIntegration(ctx, int(id))
	})(w, r)
}

func (s *Server) handleIPv64IntegrationTest(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathInt(r, "id")
		if err != nil {
			return 0, nil, err
		}
		if !s.ipv64.testAllowed("integration:"+strconv.FormatInt(id, 10), time.Now()) {
			return 0, nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusTooManyRequests, Message: "one test message every 30 seconds", RetryAfter: 30 * time.Second}
		}
		noteTarget(r, strconv.FormatInt(id, 10))
		return 0, map[string]bool{"sent": true}, c.TestIntegration(ctx, int(id))
	})(w, r)
}

// --- status pages ---

// statusPageBody reads a status page with healthcheck IDs and turns them
// into tokens.
func (s *Server) statusPageBody(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (dnsprovider.StatusPageInput, error) {
	var in dnsprovider.StatusPageInput
	if err := decode(r, &in); err != nil {
		return in, err
	}
	if in.Healthchecks != nil {
		tokens := make([]string, 0, len(*in.Healthchecks))
		for _, id := range *in.Healthchecks {
			if !dnsprovider.ValidIPv64Ref(id) {
				return in, invalidIPv64("healthcheck")
			}
			token, err := s.checkToken(ctx, c, id)
			if err != nil {
				return in, err
			}
			tokens = append(tokens, token)
		}
		in.Healthchecks = &tokens
	}
	return in, nil
}

func (s *Server) handleIPv64StatusPages(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		list, err := c.StatusPages(ctx)
		return 0, map[string]any{"pages": list}, err
	})
}

func (s *Server) handleIPv64StatusPageCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		in, err := s.statusPageBody(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		page, err := c.CreateStatusPage(ctx, in)
		noteTarget(r, page.Slug)
		return http.StatusCreated, page, err
	})(w, r)
}

func (s *Server) handleIPv64StatusPageChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathInt(r, "id")
		if err != nil {
			return 0, nil, err
		}
		in, err := s.statusPageBody(ctx, c, r)
		if err != nil {
			return 0, nil, err
		}
		page, err := c.ChangeStatusPage(ctx, int(id), in)
		noteTarget(r, page.Slug)
		return 0, page, err
	})(w, r)
}

func (s *Server) handleIPv64StatusPageDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathInt(r, "id")
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, strconv.FormatInt(id, 10))
		return 0, nil, c.DeleteStatusPage(ctx, int(id))
	})(w, r)
}

// --- offline pages ---

func (s *Server) handleIPv64OfflinePages(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		list, err := c.OfflinePages(ctx)
		return 0, map[string]any{"pages": list}, err
	})
}

func (s *Server) handleIPv64OfflinePageCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		var in dnsprovider.OfflinePageInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		page, err := c.CreateOfflinePage(ctx, in)
		noteTarget(r, page.Domain)
		return http.StatusCreated, page, err
	})(w, r)
}

func (s *Server) handleIPv64OfflinePageChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		var in dnsprovider.OfflinePageInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		page, err := c.ChangeOfflinePage(ctx, id, in)
		noteTarget(r, page.Domain)
		return 0, page, err
	})(w, r)
}

func (s *Server) handleIPv64OfflinePageDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, id)
		return 0, nil, c.DeleteOfflinePage(ctx, id)
	})(w, r)
}

// handleIPv64OfflineIcon takes the logo as the raw body (Content-Type of
// the image) and sends it to IPv64 as multipart.
func (s *Server) handleIPv64OfflineIcon(w http.ResponseWriter, r *http.Request) {
	id, err := pathRef(r, "id")
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, dnsprovider.MaxIconBytes+1))
	if err != nil || len(data) > dnsprovider.MaxIconBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the logo is at most 1 MB")
		return
	}
	if _, err := dnsprovider.CheckIcon(data); err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	noteTarget(r, id)
	s.ipv64Run(w, r, 45*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		page, err := c.UploadIcon(ctx, id, data)
		return 0, page, err
	})
}

func (s *Server) handleIPv64OfflineIconDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, id)
		return 0, nil, c.DeleteIcon(ctx, id)
	})(w, r)
}

// --- failover ---

func (s *Server) handleIPv64Policies(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		list, err := c.Policies(ctx)
		return 0, map[string]any{"policies": list, "maintenance": s.opts.IPv64.Settings().Maintenance}, err
	})
}

func (s *Server) handleIPv64PolicyCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		var in dnsprovider.PolicyInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		p, err := c.CreatePolicy(ctx, in)
		noteTarget(r, p.Name)
		return http.StatusCreated, p, err
	})(w, r)
}

func (s *Server) handleIPv64PolicyChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		var in dnsprovider.PolicyInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		p, err := c.ChangePolicy(ctx, id, in)
		noteTarget(r, p.Name)
		return 0, p, err
	})(w, r)
}

func (s *Server) handleIPv64PolicyDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, id)
		return 0, nil, c.DeletePolicy(ctx, id)
	})(w, r)
}

func (s *Server) handleIPv64PolicyAction(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		action := r.PathValue("action")
		p, err := c.PolicyAction(ctx, id, action)
		noteTarget(r, p.Name, action)
		status := 0
		if action == "failover" || action == "fallback" {
			status = http.StatusAccepted
		}
		return status, p, err
	})(w, r)
}

func (s *Server) handleIPv64MonitorAdd(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		var req struct {
			Healthcheck string `json:"healthcheck"`
			Negate      bool   `json:"negate"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		if !dnsprovider.ValidIPv64Ref(req.Healthcheck) {
			return 0, nil, invalidIPv64("healthcheck")
		}
		token, err := s.checkToken(ctx, c, req.Healthcheck)
		if err != nil {
			return 0, nil, err
		}
		p, err := c.AddMonitor(ctx, id, token, req.Negate)
		noteTarget(r, p.Name)
		return http.StatusCreated, p, err
	})(w, r)
}

func (s *Server) handleIPv64MonitorRemove(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		check, err := pathRef(r, "check")
		if err != nil {
			return 0, nil, err
		}
		token, err := s.checkToken(ctx, c, check)
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, id)
		return 0, nil, c.RemoveMonitor(ctx, id, token)
	})(w, r)
}

func (s *Server) handleIPv64RuleAdd(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		var in dnsprovider.RuleInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		p, err := c.AddRule(ctx, id, in)
		noteTarget(r, p.Name)
		return http.StatusCreated, p, err
	})(w, r)
}

func (s *Server) handleIPv64RuleRemove(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "id")
		if err != nil {
			return 0, nil, err
		}
		record, err := pathRef(r, "record")
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, id, record)
		return 0, nil, c.RemoveRule(ctx, id, record)
	})(w, r)
}

// --- maintenance page when this NAS fails ---

// handleIPv64MaintenanceCreate wires the NAS heartbeat (a push healthcheck
// the NAS pings; made and switched on if there is none), an offline page
// for the domain and a failover policy that points the A/AAAA records of
// the domain to that page while the heartbeat is down, and back after.
// What it made is listed and removable; on a failure on the way, what was
// made so far is removed again.
func (s *Server) handleIPv64MaintenanceCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
		Text   string `json:"text"`
		Email  string `json:"email"`
	}
	if !readJSON(w, r, &req) || !s.dynamicDNS(w) {
		return
	}
	if !dnsprovider.ValidHost(req.Domain) {
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "domain")
		return
	}
	for _, m := range s.opts.IPv64.Settings().Maintenance {
		if m.Domain == req.Domain {
			writeError(w, http.StatusConflict, "ipv64_exists", "this domain already has a maintenance page")
			return
		}
	}
	noteTarget(r, req.Domain)
	s.ipv64Run(w, r, 2*time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		records, err := c.FullRecords(ctx, req.Domain)
		if err != nil {
			return 0, nil, err
		}
		var protect []string
		for _, rec := range records {
			if rec.Prefix == "" && (rec.Type == "A" || rec.Type == "AAAA") && rec.Token != "" {
				protect = append(protect, rec.Token)
			}
		}
		if len(protect) == 0 {
			return 0, nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusConflict, Message: "the domain has no A or AAAA record to protect"}
		}
		hostname, _ := os.Hostname()
		entry := ipv64.Maintenance{Domain: req.Domain, Records: protect, At: time.Now().UTC()}
		var undo []func()
		rollback := func() {
			for i := len(undo) - 1; i >= 0; i-- {
				undo[i]()
			}
		}
		token := s.opts.DDNS.HeartbeatToken()
		entry.CheckName = s.opts.DDNS.Status().Settings.Heartbeat.Check
		if token == "" {
			name := "titanos-" + hostname
			check, err := c.CreateHealthcheck(ctx, name, 30*time.Minute, 10*time.Minute, []int{0})
			if err != nil {
				return 0, nil, err
			}
			if err := s.opts.DDNS.SetHeartbeat(check.Token(), check.Name, 30, true); err != nil {
				_ = c.DeleteHealthcheck(context.WithoutCancel(ctx), check.Token()) // cleanup, best effort: nothing more can be done if it fails
				return 0, nil, err
			}
			token, entry.CheckName, entry.CreatedNew = check.Token(), check.Name, true
			undo = append(undo, func() {
				_ = s.opts.DDNS.StopHeartbeat(true)                        // cleanup, best effort: nothing more can be done if it fails
				_ = c.DeleteHealthcheck(context.WithoutCancel(ctx), token) // cleanup, best effort: nothing more can be done if it fails
			})
		}
		entry.Check = dnsprovider.HealthcheckID(token)
		title := hostname + " – Wartung"
		text := req.Text
		if text == "" {
			text = "Dieser Dienst ist gerade nicht erreichbar und bald wieder da."
		}
		in := dnsprovider.OfflinePageInput{Name: &title, Domain: &req.Domain, Text: &text}
		if req.Email != "" {
			in.Email = &req.Email
		}
		page, err := c.CreateOfflinePage(ctx, in)
		if err != nil {
			rollback()
			return 0, nil, err
		}
		entry.OfflinePage = page.ID
		undo = append(undo, func() { _ = c.DeleteOfflinePage(context.WithoutCancel(ctx), page.ID) }) // cleanup, best effort: nothing more can be done if it fails
		name, mode, linking, on := "TitanOS "+req.Domain, "auto_failover_and_fallback", "or", true
		integrations := []int{0}
		policy, err := c.CreatePolicy(ctx, dnsprovider.PolicyInput{Name: &name, Mode: &mode, Linking: &linking, NotificationsEnabled: &on, Integrations: &integrations})
		if err != nil {
			rollback()
			return 0, nil, err
		}
		entry.Policy, entry.ID = policy.ID, policy.ID
		undo = append(undo, func() { _ = c.DeletePolicy(context.WithoutCancel(ctx), policy.ID) }) // cleanup, best effort: nothing more can be done if it fails
		if _, err := c.AddMonitor(ctx, policy.ID, token, false); err != nil {
			rollback()
			return 0, nil, err
		}
		for _, record := range protect {
			if _, err := c.AddRule(ctx, policy.ID, dnsprovider.RuleInput{Record: record, OfflinePage: page.ID}); err != nil {
				rollback()
				return 0, nil, err
			}
		}
		if err := s.opts.IPv64.AddMaintenance(entry); err != nil {
			rollback()
			return 0, nil, err
		}
		slog.Info("IPv64 maintenance page set up", "domain", req.Domain, "by", currentUser(r).Name)
		return http.StatusCreated, entry, nil
	})
}

func (s *Server) handleIPv64MaintenanceDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathRef(r, "id")
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	var req struct {
		// DeleteCheck also removes a healthcheck made for it.
		DeleteCheck bool `json:"deleteCheck"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	var entry *ipv64.Maintenance
	for _, m := range s.opts.IPv64.Settings().Maintenance {
		if m.ID == id {
			entry = &m
		}
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "ipv64_not_found", "no such maintenance page")
		return
	}
	noteTarget(r, entry.Domain)
	s.ipv64Run(w, r, time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		gone := func(err error) bool {
			var apiErr *dnsprovider.APIError
			return err == nil || (errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound)
		}
		if err := c.DeletePolicy(ctx, entry.Policy); !gone(err) {
			return 0, nil, err
		}
		if err := c.DeleteOfflinePage(ctx, entry.OfflinePage); !gone(err) {
			return 0, nil, err
		}
		if req.DeleteCheck && entry.CreatedNew && s.opts.DDNS != nil {
			token := s.opts.DDNS.HeartbeatToken()
			if token != "" && dnsprovider.HealthcheckID(token) == entry.Check {
				if err := c.DeleteHealthcheck(ctx, token); !gone(err) {
					return 0, nil, err
				}
				_ = s.opts.DDNS.StopHeartbeat(true) // cleanup, best effort: nothing more can be done if it fails
			}
		}
		return 0, nil, s.opts.IPv64.RemoveMaintenance(id)
	})
}
