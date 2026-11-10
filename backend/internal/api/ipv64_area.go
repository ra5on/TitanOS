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

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
)

// Settings › Dynamic DNS › IPv64 (until 28 Oct 2026 an area of its own): everything API v2 of IPv64.net offers that TitanOS
// manages (see docs/IPV64.md). Routes mirror the paths of IPv64 under
// /api/v1/ipv64; the key is the one of this area or of dynamic DNS/Let's
// Encrypt and never leaves titand. Only owners and administrators; every
// change additionally only from a web session (not from MCP,
// Home Assistant or an API token), and none of these routes is in the
// MCP catalog.

// ipv64State keeps what the routes remember in RAM.
type ipv64State struct {
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

func (st *ipv64State) rememberChecks(list []dnsprovider.IPv64Check) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.checks == nil {
		st.checks = map[string]string{}
	}
	for _, c := range list {
		if c.Token() != "" {
			st.checks[c.ID] = c.Token()
		}
	}
}

func (st *ipv64State) token(id string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.checks[id]
}

// testAllowed keeps test messages to one per 30 seconds and target.
func (st *ipv64State) testAllowed(key string, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.tests == nil {
		st.tests = map[string]time.Time{}
	}
	if now.Sub(st.tests[key]) < 30*time.Second {
		return false
	}
	st.tests[key] = now
	return true
}

// ipv64Area is IPv64.net: the account, its domains, healthchecks and the
// publishing of apps; an area with narrow dependencies (PLAN 8a, see
// fansArea). ddns and letsEncrypt hold the stored keys and host names,
// ipv64 the account area, firewall and appProxy what publishing an app
// touches, docker the ports of an app, reserved the ports of titand that
// are never published, state what the routes remember in RAM (state of the
// server; tests replace its look at Docker).
type ipv64Area struct {
	ddns        *ddns.Manager
	letsEncrypt *letsencrypt.Manager
	ipv64       *ipv64.Manager
	firewall    *firewall.Manager
	appProxy    *appproxy.Manager
	docker      *docker.Manager
	reserved    []int
	state       *ipv64State
}

// ipv64Area is the area of the server. It reads the managers when it is
// asked for, so the agent's list of addresses (agent_addresses.go) sees
// the same ones.
func (s *Server) ipv64Area() ipv64Area {
	return ipv64Area{ddns: s.opts.DDNS, letsEncrypt: s.opts.LetsEncrypt, ipv64: s.opts.IPv64, firewall: s.opts.Firewall,
		appProxy: s.opts.AppProxy, docker: s.opts.Docker, reserved: s.opts.IPv64Reserved, state: &s.ipv64}
}

func (a ipv64Area) register(mux *routeMux, admin, browserOnly func(http.HandlerFunc) http.HandlerFunc) {
	// IPv64.net: POST everywhere, so that a typed-in API key stays out of addresses.
	mux.HandleFunc("POST /api/v1/network/ipv64/account", admin(a.handleIPv64Account))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains", admin(a.handleIPv64Domains))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains/create", admin(a.handleIPv64DomainCreate))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains/change", admin(a.handleIPv64DomainChange))
	mux.HandleFunc("POST /api/v1/network/ipv64/records", admin(a.handleIPv64Records))
	mux.HandleFunc("POST /api/v1/network/ipv64/records/add", admin(a.handleIPv64RecordAdd))
	mux.HandleFunc("POST /api/v1/network/ipv64/records/delete", admin(a.handleIPv64RecordDelete))
	mux.HandleFunc("POST /api/v1/network/ipv64/healthchecks", admin(a.handleIPv64Healthchecks))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/on", admin(a.handleIPv64HeartbeatOn))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/off", admin(a.handleIPv64HeartbeatOff))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/status", admin(a.handleIPv64HeartbeatStatus))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/test", admin(a.handleIPv64HeartbeatTest))
	a.ipv64Routes(mux, admin, browserOnly)
}

// dynamicDNS answers when dynamic DNS is not available.
func (a ipv64Area) dynamicDNS(w http.ResponseWriter) bool {
	return ddnsArea{ddns: a.ddns}.dynamicDNS(w)
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
func (a ipv64Area) ipv64Run(w http.ResponseWriter, r *http.Request, timeout time.Duration, fn func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error)) {
	client, err := a.ipv64.Client()
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

func (a ipv64Area) ipv64Handle(fn func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.ipv64Run(w, r, 45*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) { return fn(ctx, c, r) })
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
func (a ipv64Area) checkToken(ctx context.Context, c *dnsprovider.IPv64, id string) (string, error) {
	if token := a.state.token(id); token != "" {
		return token, nil
	}
	list, err := c.Checks(ctx)
	if err != nil {
		return "", err
	}
	a.state.rememberChecks(list)
	if token := a.state.token(id); token != "" {
		return token, nil
	}
	return "", &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusNotFound, Message: "no such healthcheck"}
}

func (a ipv64Area) nasCheckID() string {
	if a.ddns == nil {
		return ""
	}
	if token := a.ddns.HeartbeatToken(); token != "" {
		return dnsprovider.HealthcheckID(token)
	}
	return ""
}

// --- settings ---

func (a ipv64Area) handleIPv64Status(w http.ResponseWriter, r *http.Request) {
	st := a.ipv64.Status()
	hostname, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{
		"status": st, "firewall": a.firewall.Enabled(), "nasCheck": a.nasCheckID(), "hostname": hostname,
		"zones": dnsprovider.IPv64Zones, "recordTypes": dnsprovider.IPv64RecordTypes,
	})
}

func (a ipv64Area) handleIPv64Key(w http.ResponseWriter, r *http.Request) {
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
		if _, err := dnsprovider.NewIPv64(req.APIKey, "", a.ipv64.Options).Me(ctx); err != nil {
			ipv64AreaFailure(w, err)
			return
		}
	}
	if err := a.ipv64.SetKey(req.APIKey, req.Clear); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	noteTarget(r, map[bool]string{true: "clear", false: "set"}[req.Clear])
	a.handleIPv64Status(w, r)
}

func (a ipv64Area) handleIPv64Notify(w http.ResponseWriter, r *http.Request) {
	var req ipv64.Notify
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.ipv64.SetNotify(req); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	a.handleIPv64Status(w, r)
}

func (a ipv64Area) handleIPv64Blocklist(w http.ResponseWriter, r *http.Request) {
	var req ipv64.Blocklist
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.ipv64.SetBlocklist(r.Context(), req); err != nil {
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the settings could not be saved to /config")
		return
	}
	noteTarget(r, map[bool]string{true: "on", false: "off"}[req.Enabled])
	a.handleIPv64Status(w, r)
}

func (a ipv64Area) handleIPv64BlocklistRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	if err := a.ipv64.RefreshBlocklist(ctx); err != nil {
		var apiErr *dnsprovider.APIError
		if errors.As(err, &apiErr) || errors.Is(err, ipv64.ErrNoKey) {
			ipv64AreaFailure(w, err)
			return
		}
		writeError(w, http.StatusBadGateway, "ipv64_blocklist", err.Error())
		return
	}
	a.handleIPv64Status(w, r)
}

// --- overview ---

func (a ipv64Area) handleIPv64Me(w http.ResponseWriter, r *http.Request) {
	a.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		me, err := c.Me(ctx)
		_, source := a.ipv64.Key()
		return 0, map[string]any{"me": me, "keySource": source}, err
	})
}

func (a ipv64Area) handleIPv64Logs(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit == 0 {
		limit = 50
	}
	a.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		page, err := c.Logs(ctx, limit, max(before, 0))
		return 0, page, err
	})
}

// usedKeys returns the IDs at IPv64 of the keys TitanOS keeps, by use.
func (a ipv64Area) usedKeys(ctx context.Context) map[int64][]string {
	out := map[int64][]string{}
	for use, key := range a.ipv64.Keys() {
		hash := dnsprovider.HealthcheckID(key)
		a.state.mu.Lock()
		id, ok := a.state.keyIDs[hash]
		a.state.mu.Unlock()
		if !ok {
			me, err := dnsprovider.NewIPv64(key, "", a.ipv64.Options).Me(ctx)
			if err != nil || me.Auth.KeyID == nil {
				continue
			}
			id = *me.Auth.KeyID
			a.state.mu.Lock()
			if a.state.keyIDs == nil {
				a.state.keyIDs = map[string]int64{}
			}
			a.state.keyIDs[hash] = id
			a.state.mu.Unlock()
		}
		out[id] = append(out[id], use)
		slices.Sort(out[id])
	}
	return out
}

func (a ipv64Area) handleIPv64Keys(w http.ResponseWriter, r *http.Request) {
	a.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		keys, err := c.APIKeys(ctx)
		if err != nil {
			return 0, nil, err
		}
		used := a.usedKeys(ctx)
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

func (a ipv64Area) handleIPv64KeyCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		ExpiresIn int    `json:"expiresIn"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	a.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		key, token, err := c.CreateAPIKey(ctx, req.Name, req.ExpiresIn)
		if err != nil {
			return 0, nil, err
		}
		slog.Info("IPv64 API key created", "name", key.Name, "by", currentUser(r).Name)
		// The token goes to the browser this one time and is not stored.
		return http.StatusCreated, map[string]any{"key": key, "token": token}, nil
	})
}

func (a ipv64Area) handleIPv64KeyDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	a.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		if uses := a.usedKeys(ctx)[id]; len(uses) > 0 {
			return 0, nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusConflict, Code: "key_in_use", Message: "TitanOS uses this key (" + strings.Join(uses, ", ") + ")"}
		}
		noteTarget(r, strconv.FormatInt(id, 10))
		return 0, nil, c.DeleteAPIKey(ctx, id)
	})
}

// --- domains and records ---

func (a ipv64Area) handleIPv64AreaDomains(w http.ResponseWriter, r *http.Request) {
	a.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		domains, err := c.Domains(ctx)
		hosts := []string{}
		if a.ddns != nil {
			hosts = a.ddns.Status().Settings.Hostnames
		}
		return 0, map[string]any{"domains": domains, "zones": dnsprovider.IPv64Zones, "ddnsHosts": hosts}, err
	})
}

func (a ipv64Area) handleIPv64AreaDomainCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Own  bool   `json:"own"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, req.Name)
	a.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
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

func (a ipv64Area) handleIPv64AreaDomain(w http.ResponseWriter, r *http.Request) {
	a.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		d, err := c.Domain(ctx, name)
		return 0, d, err
	})(w, r)
}

func (a ipv64Area) handleIPv64AreaDomainChange(w http.ResponseWriter, r *http.Request) {
	a.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
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

func (a ipv64Area) handleIPv64AreaDomainDelete(w http.ResponseWriter, r *http.Request) {
	a.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, name)
		return 0, nil, c.DeleteDomain(ctx, name)
	})(w, r)
}

func (a ipv64Area) handleIPv64AreaDomainRotate(w http.ResponseWriter, r *http.Request) {
	a.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
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
		if a.ddns != nil {
			if followed, err = a.ddns.ReplaceTokens(old, fresh, "", ""); err != nil {
				slog.Error("new IPv64 update token not stored", "domain", name, "err", err)
			}
		}
		return 0, map[string]any{"domain": d, "ddnsUpdated": followed}, nil
	})(w, r)
}

func (a ipv64Area) handleIPv64AreaDomainReset(w http.ResponseWriter, r *http.Request) {
	a.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		name, err := domainParam(r)
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, name)
		d, err := c.ResetDomain(ctx, name)
		return 0, d, err
	})(w, r)
}

func (a ipv64Area) handleIPv64AreaRecordAdd(w http.ResponseWriter, r *http.Request) {
	a.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
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

func (a ipv64Area) handleIPv64AreaRecordChange(w http.ResponseWriter, r *http.Request) {
	a.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
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

func (a ipv64Area) handleIPv64AreaRecordDelete(w http.ResponseWriter, r *http.Request) {
	a.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
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
