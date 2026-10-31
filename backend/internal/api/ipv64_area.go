package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
)

// Settings › Dynamic DNS › IPv64 (until 28 Oct 2026 an area of its own): everything API v2 of IPv64.net offers that TitanOS
// manages (see docs/IPV64.md). Routes mirror the paths of IPv64 under
// /api/v1/ipv64; the key is the one of this area or of dynamic DNS/Let's
// Encrypt and never leaves titand. Only owners and administrators; every
// change additionally only from a web session (not from the assistant,
// Home Assistant or an API token), and none of these routes is in the
// assistant's catalog.

// ipv64Area keeps what the routes remember in RAM.
type ipv64Area struct {
	mu sync.Mutex
	// checks maps the ID of a healthcheck to its token.
	checks map[string]string
	// keyIDs maps a hash of a stored key to the ID IPv64 gives it.
	keyIDs map[string]int64
	// tests: last test message per healthcheck or integration.
	tests map[string]time.Time
	// appPorts replaces the look at Docker in tests.
	appPorts func(app string) ([]int, bool)
}

func (a *ipv64Area) rememberChecks(list []dnsprovider.IPv64Check) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.checks == nil {
		a.checks = map[string]string{}
	}
	for _, c := range list {
		if c.Token() != "" {
			a.checks[c.ID] = c.Token()
		}
	}
}

func (a *ipv64Area) token(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.checks[id]
}

// testAllowed keeps test messages to one per 30 seconds and target.
func (a *ipv64Area) testAllowed(key string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tests == nil {
		a.tests = map[string]time.Time{}
	}
	if now.Sub(a.tests[key]) < 30*time.Second {
		return false
	}
	a.tests[key] = now
	return true
}

// browserOnly refuses requests made by the assistant, Home Assistant or
// an API token: they act in the name of an account but are no person at a
// web session. Creating keys and everything that changes the account at
// IPv64 needs one.
func (s *Server) browserOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, token := r.Context().Value(apiTokenIdentity{}).(string)
		_, agent := r.Context().Value(inProcessIdentity{}).(string)
		if token || agent {
			writeError(w, http.StatusForbidden, "browser_session_required", "this needs a web session")
			return
		}
		next(w, r)
	}
}

// ipv64AreaFailure is ipv64Failure plus the answers of the bigger API.
func ipv64AreaFailure(w http.ResponseWriter, err error) {
	var apiErr *dnsprovider.APIError
	switch {
	case errors.Is(err, ipv64.ErrNoKey):
		writeError(w, http.StatusBadRequest, "ipv64_no_key", "the API key of the IPv64 account is needed")
	case errors.Is(err, ipv64.ErrDisabled):
		writeError(w, http.StatusConflict, "dev_mode", err.Error())
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
		writeError(w, http.StatusNotFound, "ipv64_not_found", apiErr.Error())
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict:
		writeError(w, http.StatusConflict, "ipv64_conflict", apiErr.Error())
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden:
		writeError(w, http.StatusConflict, "ipv64_limit", apiErr.Error())
	case errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnprocessableEntity || apiErr.Status == http.StatusRequestEntityTooLarge || apiErr.Status == http.StatusUnsupportedMediaType):
		writeError(w, http.StatusBadRequest, "ipv64_invalid", apiErr.Error())
	default:
		ipv64Failure(w, err)
	}
}

// ipv64Run calls fn with a client and writes its answer: nil out is 204.
func (s *Server) ipv64Run(w http.ResponseWriter, r *http.Request, timeout time.Duration, fn func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error)) {
	client, err := s.opts.IPv64.Client()
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	status, out, err := fn(ctx, client)
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, status, out)
}

func (s *Server) ipv64Handle(fn func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.ipv64Run(w, r, 45*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) { return fn(ctx, c, r) })
	}
}

// pathRef returns a path value that may go into a path at IPv64.
func pathRef(r *http.Request, name string) (string, error) {
	value := r.PathValue(name)
	if !dnsprovider.ValidIPv64Ref(value) {
		return "", invalidIPv64(name)
	}
	return value, nil
}

func pathInt(r *http.Request, name string) (int64, error) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || n < 0 {
		return 0, invalidIPv64(name)
	}
	return n, nil
}

func invalidIPv64(what string) error {
	return errors.Join(dnsprovider.ErrInvalid, errors.New(what))
}

// decode reads the JSON body into v (bounded like readJSON).
func decode(r *http.Request, v any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody+1))
	if err != nil || len(data) > maxJSONBody {
		return invalidIPv64("body")
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return invalidIPv64("body is not valid JSON")
	}
	return nil
}

// checkToken returns the token of a healthcheck ID, listing the checks if
// the ID is not known yet.
func (s *Server) checkToken(ctx context.Context, c *dnsprovider.IPv64, id string) (string, error) {
	if token := s.ipv64.token(id); token != "" {
		return token, nil
	}
	list, err := c.Checks(ctx)
	if err != nil {
		return "", err
	}
	s.ipv64.rememberChecks(list)
	if token := s.ipv64.token(id); token != "" {
		return token, nil
	}
	return "", &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusNotFound, Message: "no such healthcheck"}
}

func (s *Server) nasCheckID() string {
	if s.opts.DDNS == nil {
		return ""
	}
	if token := s.opts.DDNS.HeartbeatToken(); token != "" {
		return dnsprovider.HealthcheckID(token)
	}
	return ""
}

// --- settings ---

func (s *Server) handleIPv64Status(w http.ResponseWriter, r *http.Request) {
	st := s.opts.IPv64.Status()
	hostname, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{
		"status": st, "firewall": s.opts.Firewall.Enabled(), "nasCheck": s.nasCheckID(), "hostname": hostname,
		"zones": dnsprovider.IPv64Zones, "recordTypes": dnsprovider.IPv64RecordTypes,
	})
}

func (s *Server) handleIPv64Key(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"apiKey"`
		Clear  bool   `json:"clear"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.APIKey = strings.TrimSpace(req.APIKey)
	if len(req.APIKey) > 256 || strings.ContainsAny(req.APIKey, " \r\n\t") {
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "API key")
		return
	}
	if req.APIKey != "" {
		// Checked before it is stored.
		ctx, cancel := ipv64Context(r)
		defer cancel()
		if _, err := dnsprovider.NewIPv64(req.APIKey, "", s.opts.IPv64.Options).Me(ctx); err != nil {
			ipv64AreaFailure(w, err)
			return
		}
	}
	if err := s.opts.IPv64.SetKey(req.APIKey, req.Clear); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	noteTarget(r, map[bool]string{true: "clear", false: "set"}[req.Clear])
	s.handleIPv64Status(w, r)
}

func (s *Server) handleIPv64Notify(w http.ResponseWriter, r *http.Request) {
	var req ipv64.Notify
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.IPv64.SetNotify(req); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	s.handleIPv64Status(w, r)
}

func (s *Server) handleIPv64Blocklist(w http.ResponseWriter, r *http.Request) {
	var req ipv64.Blocklist
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.opts.IPv64.SetBlocklist(r.Context(), req); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	noteTarget(r, map[bool]string{true: "on", false: "off"}[req.Enabled])
	s.handleIPv64Status(w, r)
}

func (s *Server) handleIPv64BlocklistRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	if err := s.opts.IPv64.RefreshBlocklist(ctx); err != nil {
		var apiErr *dnsprovider.APIError
		if errors.As(err, &apiErr) || errors.Is(err, ipv64.ErrNoKey) {
			ipv64AreaFailure(w, err)
			return
		}
		writeError(w, http.StatusBadGateway, "ipv64_blocklist", err.Error())
		return
	}
	s.handleIPv64Status(w, r)
}

// --- overview ---

func (s *Server) handleIPv64Me(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		me, err := c.Me(ctx)
		_, source := s.opts.IPv64.Key()
		return 0, map[string]any{"me": me, "keySource": source}, err
	})
}

func (s *Server) handleIPv64Logs(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit == 0 {
		limit = 50
	}
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		page, err := c.Logs(ctx, limit, max(before, 0))
		return 0, page, err
	})
}

// usedKeys returns the IDs at IPv64 of the keys TitanOS keeps, by use.
func (s *Server) usedKeys(ctx context.Context) map[int64][]string {
	out := map[int64][]string{}
	for use, key := range s.opts.IPv64.Keys() {
		hash := dnsprovider.HealthcheckID(key)
		s.ipv64.mu.Lock()
		id, ok := s.ipv64.keyIDs[hash]
		s.ipv64.mu.Unlock()
		if !ok {
			me, err := dnsprovider.NewIPv64(key, "", s.opts.IPv64.Options).Me(ctx)
			if err != nil || me.Auth.KeyID == nil {
				continue
			}
			id = *me.Auth.KeyID
			s.ipv64.mu.Lock()
			if s.ipv64.keyIDs == nil {
				s.ipv64.keyIDs = map[string]int64{}
			}
			s.ipv64.keyIDs[hash] = id
			s.ipv64.mu.Unlock()
		}
		out[id] = append(out[id], use)
		slices.Sort(out[id])
	}
	return out
}

func (s *Server) handleIPv64Keys(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		keys, err := c.APIKeys(ctx)
		if err != nil {
			return 0, nil, err
		}
		used := s.usedKeys(ctx)
		type key struct {
			dnsprovider.IPv64APIKey
			UsedBy []string `json:"usedBy"`
		}
		out := make([]key, 0, len(keys))
		for _, k := range keys {
			out = append(out, key{IPv64APIKey: k, UsedBy: append([]string{}, used[k.ID]...)})
		}
		return 0, map[string]any{"keys": out}, nil
	})
}

func (s *Server) handleIPv64KeyCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		ExpiresIn int    `json:"expiresIn"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		key, token, err := c.CreateAPIKey(ctx, req.Name, req.ExpiresIn)
		if err != nil {
			return 0, nil, err
		}
		slog.Info("IPv64 API key created", "name", key.Name, "by", currentUser(r).Name)
		// The token goes to the browser this one time and is not stored.
		return http.StatusCreated, map[string]any{"key": key, "token": token}, nil
	})
}

func (s *Server) handleIPv64KeyDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		if uses := s.usedKeys(ctx)[id]; len(uses) > 0 {
			return 0, nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusConflict, Code: "key_in_use", Message: "TitanOS uses this key (" + strings.Join(uses, ", ") + ")"}
		}
		noteTarget(r, strconv.FormatInt(id, 10))
		return 0, nil, c.DeleteAPIKey(ctx, id)
	})
}

// --- domains and records ---

func (s *Server) handleIPv64AreaDomains(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		domains, err := c.Domains(ctx)
		hosts := []string{}
		if s.opts.DDNS != nil {
			hosts = s.opts.DDNS.Status().Settings.Hostnames
		}
		return 0, map[string]any{"domains": domains, "zones": dnsprovider.IPv64Zones, "ddnsHosts": hosts}, err
	})
}

func (s *Server) handleIPv64AreaDomainCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Own  bool   `json:"own"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		if req.Own {
			d, err := c.CreateOwnDomain(ctx, req.Name)
			return http.StatusCreated, d, err
		}
		d, err := c.CreateDomain(ctx, strings.ToLower(strings.TrimSpace(req.Name)))
		return http.StatusCreated, d, err
	})
}

func domainParam(r *http.Request) (string, error) {
	name := strings.ToLower(r.PathValue("domain"))
	if !dnsprovider.ValidHost(name) {
		return "", invalidIPv64("domain")
	}
	return name, nil
}

func (s *Server) handleIPv64AreaDomain(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		d, err := c.Domain(ctx, name)
		return 0, d, err
	})(w, r)
}

func (s *Server) handleIPv64AreaDomainChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		var change dnsprovider.IPv64DomainChange
		if err := decode(r, &change); err != nil {
			return 0, nil, err
		}
		if change.IPv6Prefix != nil {
			if err := dnsprovider.CheckIPv6Prefix(*change.IPv6Prefix); err != nil {
				return 0, nil, err
			}
		}
		noteTarget(r, name)
		d, err := c.ChangeDomain(ctx, name, change)
		return 0, d, err
	})(w, r)
}

func (s *Server) handleIPv64AreaDomainDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, name)
		return 0, nil, c.DeleteDomain(ctx, name)
	})(w, r)
}

func (s *Server) handleIPv64AreaDomainRotate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		old, err := c.DomainUpdateToken(ctx, name)
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, name)
		d, fresh, err := c.RotateDomainToken(ctx, name)
		if err != nil {
			return 0, nil, err
		}
		followed := false
		if s.opts.DDNS != nil {
			if followed, err = s.opts.DDNS.ReplaceTokens(old, fresh, "", ""); err != nil {
				slog.Error("new IPv64 update token not stored", "domain", name, "err", err)
			}
		}
		return 0, map[string]any{"domain": d, "ddnsUpdated": followed}, nil
	})(w, r)
}

func (s *Server) handleIPv64AreaDomainReset(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, name)
		d, err := c.ResetDomain(ctx, name)
		return 0, d, err
	})(w, r)
}

func (s *Server) handleIPv64AreaRecordAdd(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		var req struct {
			Type    string `json:"type"`
			Prefix  string `json:"prefix"`
			Content string `json:"content"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		noteTarget(r, name, req.Type)
		rec, err := c.AddRecord(ctx, name, req.Type, req.Prefix, req.Content)
		return http.StatusCreated, rec, err
	})(w, r)
}

func (s *Server) handleIPv64AreaRecordChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "record")
		if err != nil {
			return 0, nil, err
		}
		var change dnsprovider.ChangeRecordFields
		if err := decode(r, &change); err != nil {
			return 0, nil, err
		}
		current, err := c.Record(ctx, id)
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, current.FQDN, current.Type)
		rec, err := c.ChangeRecord(ctx, id, current, change)
		return 0, rec, err
	})(w, r)
}

func (s *Server) handleIPv64AreaRecordDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		id, err := pathRef(r, "record")
		if err != nil {
			return 0, nil, err
		}
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			current, err := c.Record(ctx, id)
			if err != nil {
				return 0, nil, err
			}
			n = current.ID
		}
		noteTarget(r, id)
		return 0, nil, c.DeleteRecord(ctx, n)
	})(w, r)
}
