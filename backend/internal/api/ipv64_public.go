package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
)

// Public access through IPv64 (CDN with reverse proxy and portmapper, a
// Docker app published through it) and the Cloud Router with the tunnel
// of the NAS.

// ipv64Wait is how often a background change of IPv64 is looked at.
var ipv64Wait = 3 * time.Second

// --- CDN ---

func (s *Server) handleIPv64CDNs(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		list, err := c.CDNs(ctx)
		return 0, map[string]any{"cdn": list, "published": s.opts.IPv64.Settings().Published}, err
	})
}

func (s *Server) handleIPv64CDNCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		var req struct {
			Domain string `json:"domain"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		noteTarget(r, req.Domain)
		cdn, err := c.CreateCDN(ctx, req.Domain)
		return http.StatusCreated, cdn, err
	})(w, r)
}

func (s *Server) handleIPv64CDN(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		cdn, err := c.CDN(ctx, token)
		if err != nil {
			return 0, nil, err
		}
		records, err := c.CDNRecords(ctx, token)
		if err != nil {
			return 0, nil, err
		}
		access, err := c.AccessList(ctx, token)
		return 0, map[string]any{"cdn": cdn, "records": records, "accessList": access}, err
	})(w, r)
}

func (s *Server) handleIPv64CDNChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		var in dnsprovider.CDNInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		if in.BackendPort != nil && s.reservedPort(*in.BackendPort) {
			return 0, nil, invalidIPv64("the ports of the TitanOS web UI are never published")
		}
		noteTarget(r, token)
		cdn, err := c.ChangeCDN(ctx, token, in)
		return 0, cdn, err
	})(w, r)
}

func (s *Server) handleIPv64CDNDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, token)
		cdn, err := c.DeleteCDN(ctx, token)
		return http.StatusAccepted, cdn, err
	})(w, r)
}

func (s *Server) handleIPv64CDNAction(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		var req struct {
			Action string `json:"action"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		noteTarget(r, token, req.Action)
		cdn, status, err := c.CDNAction(ctx, token, req.Action)
		return status, cdn, err
	})(w, r)
}

func (s *Server) handleIPv64CDNRecord(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		record, err := pathRef(r, "record")
		if err != nil {
			return 0, nil, err
		}
		var req struct {
			// Mode is full, other_family_only or "" (direct again).
			Mode string `json:"mode"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		noteTarget(r, record, req.Mode)
		rec, err := c.CDNRecord(ctx, token, record, req.Mode)
		return http.StatusAccepted, rec, err
	})(w, r)
}

func (s *Server) handleIPv64MappingCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		var in dnsprovider.MappingInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		if s.reservedPort(in.TargetPort) {
			return 0, nil, invalidIPv64("the ports of the TitanOS web UI are never published")
		}
		noteTarget(r, token, in.Name)
		m, err := c.CreateMapping(ctx, token, in)
		return http.StatusCreated, m, err
	})(w, r)
}

func (s *Server) handleIPv64MappingDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		mapping, err := pathRef(r, "mapping")
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, token, mapping)
		return 0, nil, c.DeleteMapping(ctx, token, mapping)
	})(w, r)
}

func (s *Server) handleIPv64AccessAdd(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		var req struct {
			Address string `json:"address"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		noteTarget(r, token, req.Address)
		e, err := c.AddAccess(ctx, token, strings.TrimSpace(req.Address))
		return http.StatusCreated, e, err
	})(w, r)
}

func (s *Server) handleIPv64AccessRemove(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		id, err := pathInt(r, "id")
		if err != nil {
			return 0, nil, err
		}
		noteTarget(r, token)
		return 0, nil, c.RemoveAccess(ctx, token, id)
	})(w, r)
}

// --- a Docker app published through the CDN ---

// reservedPort tells whether a port belongs to titand (web UI, HTTPS names
// of apps) or another service of the NAS that must never be published.
func (s *Server) reservedPort(port int) bool {
	reserved := []int{22, 80, 443, 8750}
	reserved = append(reserved, s.opts.IPv64Reserved...)
	if s.opts.AppProxy != nil {
		reserved = append(reserved, s.opts.AppProxy.Reserved...)
		if s.opts.AppProxy.HTTPSPort > 0 {
			reserved = append(reserved, s.opts.AppProxy.HTTPSPort)
		}
	}
	return slices.Contains(reserved, port)
}

// appPorts are the TCP ports the containers of an app publish on the NAS.
func (s *Server) appPorts(ctx context.Context, app string) ([]int, bool) {
	if s.ipv64.appPorts != nil {
		return s.ipv64.appPorts(app)
	}
	if s.opts.Docker == nil {
		return nil, false
	}
	status := s.opts.Docker.Status(ctx)
	found := false
	var ports []int
	for _, c := range status.Containers {
		if c.App != app && !(c.App == "" && c.Name == app) {
			continue
		}
		found = true
		for _, p := range c.Ports {
			if p.Host > 0 && (p.Protocol == "" || p.Protocol == "tcp") && !slices.Contains(ports, p.Host) {
				ports = append(ports, p.Host)
			}
		}
	}
	slices.Sort(ports)
	return ports, found
}

func (s *Server) handleIPv64App(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	ports, _ := s.appPorts(r.Context(), app)
	var usable []int
	for _, p := range ports {
		if !s.reservedPort(p) {
			usable = append(usable, p)
		}
	}
	entry, published := s.opts.IPv64.PublishedApp(app)
	out := map[string]any{"ports": append([]int{}, usable...), "firewall": s.opts.Firewall.Enabled(), "zones": s.opts.Firewall.AppZones(app)}
	if published {
		out["published"] = entry
	}
	_, source := s.opts.IPv64.Key()
	out["keySource"] = source
	writeJSON(w, http.StatusOK, out)
}

func isCode(err error, codes ...string) bool {
	var apiErr *dnsprovider.APIError
	return errors.As(err, &apiErr) && slices.Contains(codes, apiErr.Code)
}

// untilReady repeats call while IPv64 is still busy with the CDN (409:
// server not assigned yet, proxy not on yet), at most 90 seconds.
func untilReady(ctx context.Context, call func() error) error {
	deadline := time.Now().Add(90 * time.Second)
	for {
		err := call()
		if err == nil || !isCode(err, "cdn_server_pending", "proxy_disabled", "busy", "no_records") || time.Now().After(deadline) {
			return err
		}
		t := time.NewTimer(ipv64Wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// handleIPv64Publish makes an app reachable from the internet: A/AAAA for
// the name (created with the addresses of dynamic DNS, or the one the
// request comes from), the domain in the CDN, the records routed through
// it, the reverse proxy on with HTTPS to the app's port, and, if the
// firewall is on and the owner allows it, the app's ports open from the
// internet zone.
func (s *Server) handleIPv64Publish(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	var req struct {
		Domain   string `json:"domain"`
		Prefix   string `json:"prefix"`
		Port     int    `json:"port"`
		Scheme   string `json:"scheme"`
		Firewall bool   `json:"firewall"`
		// Confirm is the click in the warning dialog.
		Confirm bool `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Domain, req.Prefix = strings.ToLower(strings.TrimSpace(req.Domain)), strings.ToLower(strings.TrimSpace(req.Prefix))
	if req.Scheme == "" {
		req.Scheme = "http"
	}
	switch {
	case !req.Confirm:
		writeError(w, http.StatusBadRequest, "ipv64_confirm_required", "publishing an app needs the confirmation")
		return
	case !dnsprovider.ValidHost(req.Domain) || (req.Prefix != "" && !dnsprovider.ValidHost(req.Prefix+"."+req.Domain)) || strings.Contains(req.Prefix, "."):
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "domain or prefix")
		return
	case req.Scheme != "http" && req.Scheme != "https":
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "scheme")
		return
	case s.reservedPort(req.Port):
		writeError(w, http.StatusBadRequest, "ipv64_reserved_port", "the ports of the TitanOS web UI are never published")
		return
	}
	if _, ok := s.opts.IPv64.PublishedApp(app); ok {
		writeError(w, http.StatusConflict, "ipv64_exists", "the app is already published")
		return
	}
	ports, found := s.appPorts(r.Context(), app)
	if !found || !slices.Contains(ports, req.Port) {
		writeError(w, http.StatusBadRequest, "ipv64_port_not_published", "the app publishes no such TCP port on the NAS")
		return
	}
	noteTarget(r, app, req.Domain)
	s.ipv64Run(w, r, 3*time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		entry := ipv64.Published{App: app, Port: req.Port, Domain: req.Domain, Prefix: req.Prefix, At: time.Now().UTC(), Records: []string{}}
		names := func() ([]dnsprovider.IPv64FullRecord, error) {
			all, err := c.FullRecords(ctx, req.Domain)
			var out []dnsprovider.IPv64FullRecord
			for _, rec := range all {
				if rec.Prefix == req.Prefix && (rec.Type == "A" || rec.Type == "AAAA") {
					out = append(out, rec)
				}
			}
			return out, err
		}
		records, err := names()
		if err != nil {
			return 0, nil, err
		}
		if len(records) == 0 {
			v4, v6 := s.ddnsAddresses()
			if _, err := c.UpdatePrefix(ctx, req.Domain, req.Prefix, v4, v6); err != nil {
				return 0, nil, err
			}
			if records, err = names(); err != nil {
				return 0, nil, err
			}
		}
		cdns, err := c.CDNs(ctx)
		if err != nil {
			return 0, nil, err
		}
		for _, cdn := range cdns {
			if cdn.Domain != nil && *cdn.Domain == req.Domain {
				entry.CDN = cdn.ID
				detail, err := c.CDN(ctx, cdn.ID)
				if err != nil {
					return 0, nil, err
				}
				if detail.Proxy.State == "on" && detail.Proxy.Backend.Port != 0 && detail.Proxy.Backend.Port != req.Port {
					return 0, nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusConflict, Code: "cdn_in_use", Message: "the CDN of this domain already sends to another port; a domain has one backend port"}
				}
			}
		}
		if entry.CDN == "" {
			cdn, err := c.CreateCDN(ctx, req.Domain)
			if err != nil {
				return 0, nil, err
			}
			entry.CDN, entry.CreatedCDN = cdn.ID, true
		}
		// "Fertig ist die Änderung, wenn bei CDN-Domains die Liste pending
		// leer ist": wait for the server, looking every few seconds.
		if err := waitForServer(ctx, c, entry.CDN); err != nil {
			s.unpublish(context.WithoutCancel(ctx), c, entry)
			return 0, nil, err
		}
		for _, rec := range records {
			ref := rec.Token
			if ref == "" {
				continue
			}
			err := untilReady(ctx, func() error {
				_, err := c.CDNRecord(ctx, entry.CDN, ref, "full")
				return err
			})
			if err != nil && !isCode(err, "already_enabled") {
				s.unpublish(context.WithoutCancel(ctx), c, entry)
				return 0, nil, err
			}
			entry.Records = append(entry.Records, ref)
		}
		if err := untilReady(ctx, func() error {
			_, _, err := c.CDNAction(ctx, entry.CDN, "proxy/enable")
			if isCode(err, "invalid_state") {
				return nil
			}
			return err
		}); err != nil {
			s.unpublish(context.WithoutCancel(ctx), c, entry)
			return 0, nil, err
		}
		frontend, on := "https_redirect", true
		in := dnsprovider.CDNInput{Frontend: &frontend, BackendScheme: &req.Scheme, BackendPort: &req.Port, Websocket: &on}
		var detail dnsprovider.IPv64CDN
		if err := untilReady(ctx, func() error {
			var err error
			detail, err = c.ChangeCDN(ctx, entry.CDN, in)
			return err
		}); err != nil {
			s.unpublish(context.WithoutCancel(ctx), c, entry)
			return 0, nil, err
		}
		entry.Pending = detail.Pending
		if req.Firewall && s.opts.Firewall.Enabled() {
			before := s.opts.Firewall.AppZones(app)
			zones := before
			zones.Internet = true
			changed, err := s.opts.Firewall.SetAppZones(ctx, app, zones)
			if err != nil {
				slog.Error("firewall zones of a published app not changed", "app", app, "err", err)
			} else if changed {
				entry.Firewall = true
				entry.Before = &ipv64.AppZones{Home: before.Home, VPN: before.VPN, Internet: before.Internet}
			}
		}
		if err := s.opts.IPv64.AddPublished(entry); err != nil {
			return 0, nil, err
		}
		slog.Info("app published through IPv64", "app", app, "domain", req.Domain, "port", req.Port, "by", currentUser(r).Name)
		return http.StatusCreated, entry, nil
	})
}

func (s *Server) ddnsAddresses() (v4, v6 netip.Addr) {
	if s.opts.DDNS == nil {
		return
	}
	return s.opts.DDNS.Addresses()
}

// unpublish routes the records back, switches off and removes what was
// made for the app; errors are logged, the caller reports its own.
func (s *Server) unpublish(ctx context.Context, c *dnsprovider.IPv64, entry ipv64.Published) error {
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil && !isCode(err, "not_enabled", "not_found", "cdn_not_found", "record_not_found") {
			firstErr = err
		}
	}
	for _, rec := range entry.Records {
		_, err := c.CDNRecord(ctx, entry.CDN, rec, "")
		keep(err)
	}
	if entry.CreatedCDN && entry.CDN != "" {
		_, _, err := c.CDNAction(ctx, entry.CDN, "proxy/disable")
		if !isCode(err, "invalid_state", "proxy_disabled") {
			keep(err)
		}
		keep(untilRecordsOff(ctx, func() error {
			_, err := c.DeleteCDN(ctx, entry.CDN)
			return err
		}))
	}
	if entry.Firewall && entry.Before != nil {
		if _, err := s.opts.Firewall.SetAppZones(ctx, entry.App, firewall.Zones{Home: entry.Before.Home, VPN: entry.Before.VPN, Internet: entry.Before.Internet}); err != nil {
			keep(err)
		}
	}
	if firstErr != nil {
		slog.Warn("unpublishing through IPv64 incomplete", "app", entry.App, "err", firstErr)
	}
	return firstErr
}

// waitForServer waits until IPv64 has assigned a CDN server (at most 90
// seconds).
func waitForServer(ctx context.Context, c *dnsprovider.IPv64, token string) error {
	deadline := time.Now().Add(90 * time.Second)
	for {
		cdn, err := c.CDN(ctx, token)
		if err != nil {
			return err
		}
		if !slices.Contains(cdn.Pending, "server_assignment") {
			return nil
		}
		if time.Now().After(deadline) {
			return &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusConflict, Code: "cdn_server_pending", Message: "IPv64 has not assigned a CDN server yet; try again in a minute"}
		}
		t := time.NewTimer(ipv64Wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// untilRecordsOff repeats the removal of a CDN domain while its records
// are still being switched back (409 records_enabled), up to a minute.
func untilRecordsOff(ctx context.Context, call func() error) error {
	deadline := time.Now().Add(time.Minute)
	for {
		err := call()
		if err == nil || !isCode(err, "records_enabled", "busy", "cdn_deleting") || time.Now().After(deadline) {
			if isCode(err, "cdn_deleting") {
				return nil
			}
			return err
		}
		t := time.NewTimer(ipv64Wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

func (s *Server) handleIPv64Unpublish(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	entry, ok := s.opts.IPv64.PublishedApp(app)
	if !ok {
		writeError(w, http.StatusNotFound, "ipv64_not_found", "the app is not published")
		return
	}
	noteTarget(r, app, entry.Domain)
	s.ipv64Run(w, r, 3*time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		if err := s.unpublish(ctx, c, entry); err != nil {
			return 0, nil, err
		}
		return 0, nil, s.opts.IPv64.RemovePublished(app)
	})
}

// --- Cloud Router ---

func (s *Server) handleIPv64Routers(w http.ResponseWriter, r *http.Request) {
	s.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		list, err := c.Routers(ctx)
		return 0, map[string]any{"routers": list}, err
	})
}

func (s *Server) handleIPv64RouterCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		var req struct {
			Name string `json:"name"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		noteTarget(r, req.Name)
		router, err := c.CreateRouter(ctx, req.Name)
		return http.StatusAccepted, router, err
	})(w, r)
}

func (s *Server) handleIPv64Router(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		router, err := c.Router(ctx, token)
		if err != nil {
			return 0, nil, err
		}
		routes, err := c.Routes(ctx, token)
		if err != nil {
			return 0, nil, err
		}
		rules, err := c.RouterRules(ctx, token)
		if err != nil {
			return 0, nil, err
		}
		ips, err := c.PublicIPs(ctx, token)
		return 0, map[string]any{"router": router, "routes": routes, "rules": rules, "publicIPs": ips}, err
	})(w, r)
}

func (s *Server) handleIPv64RouterRename(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		noteTarget(r, req.Name)
		router, err := c.RenameRouter(ctx, token, req.Name)
		return 0, router, err
	})(w, r)
}

func (s *Server) handleIPv64RouterAdd(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		kind := r.PathValue("kind")
		noteTarget(r, token, kind)
		switch kind {
		case "gateways":
			var req struct {
				Name string `json:"name"`
			}
			if err := decode(r, &req); err != nil {
				return 0, nil, err
			}
			g, err := c.CreateGateway(ctx, token, req.Name)
			return http.StatusAccepted, g, err
		case "routes":
			var in dnsprovider.RouteInput
			if err := decode(r, &in); err != nil {
				return 0, nil, err
			}
			route, err := c.CreateRoute(ctx, token, in)
			return http.StatusAccepted, route, err
		case "firewall":
			var in dnsprovider.RouterRuleInput
			if err := decode(r, &in); err != nil {
				return 0, nil, err
			}
			rule, err := c.CreateRouterRule(ctx, token, in)
			return http.StatusAccepted, rule, err
		case "public-ips":
			ip, err := c.CreatePublicIPv6(ctx, token)
			return http.StatusAccepted, ip, err
		}
		return 0, nil, invalidIPv64("kind")
	})(w, r)
}

func (s *Server) handleIPv64RouterItem(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		id, err := pathInt(r, "id")
		if err != nil {
			return 0, nil, err
		}
		var disabled *bool
		if r.Method == http.MethodPatch {
			var req struct {
				Disabled bool `json:"disabled"`
			}
			if err := decode(r, &req); err != nil {
				return 0, nil, err
			}
			disabled = &req.Disabled
		}
		noteTarget(r, token, r.PathValue("kind"))
		data, err := c.RouterItem(ctx, token, r.PathValue("kind"), id, disabled)
		if err != nil {
			return 0, nil, err
		}
		if len(data) == 0 {
			return 0, nil, nil
		}
		return http.StatusAccepted, json.RawMessage(data), nil
	})(w, r)
}

func (s *Server) handleIPv64GatewayChange(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		var in dnsprovider.GatewayInput
		if err := decode(r, &in); err != nil {
			return 0, nil, err
		}
		noteTarget(r, token)
		g, err := c.ChangeGateway(ctx, token, in)
		return 0, g, err
	})(w, r)
}

func (s *Server) handleIPv64GatewayAction(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		action := r.PathValue("action")
		if r.Method == http.MethodDelete {
			action = "delete"
			if wg := s.opts.IPv64.Settings().WireGuard; wg.Enabled && wg.Gateway == token {
				return 0, nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusConflict, Code: "nas_connected", Message: "this NAS is connected to the gateway; disconnect it first"}
			}
		}
		noteTarget(r, token, action)
		g, err := c.GatewayAction(ctx, token, action)
		return http.StatusAccepted, g, err
	})(w, r)
}

func (s *Server) handleIPv64PeerCreate(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := decode(r, &req); err != nil {
			return 0, nil, err
		}
		noteTarget(r, req.Name)
		p, err := c.CreatePeer(ctx, token, req.Name)
		return http.StatusAccepted, p, err
	})(w, r)
}

func (s *Server) handleIPv64PeerDelete(w http.ResponseWriter, r *http.Request) {
	s.ipv64Handle(func(ctx context.Context, c *dnsprovider.IPv64, r *http.Request) (int, any, error) {
		token, err := pathRef(r, "token")
		if err != nil {
			return 0, nil, err
		}
		if wg := s.opts.IPv64.Settings().WireGuard; wg.Enabled && wg.Peer == token {
			return 0, nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusConflict, Code: "nas_connected", Message: "this is the peer of this NAS; disconnect it instead"}
		}
		noteTarget(r, token)
		return 0, nil, c.DeletePeer(ctx, token)
	})(w, r)
}

// --- the tunnel of this NAS ---

func (s *Server) handleIPv64Tunnel(w http.ResponseWriter, r *http.Request) {
	st := s.opts.IPv64.Status()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tunnel := st.Tunnel
	if st.Settings.WireGuard.Enabled {
		tunnel.LastHandshake = s.opts.IPv64.Handshake(ctx)
	}
	writeJSON(w, http.StatusOK, map[string]any{"wireguard": st.Settings.WireGuard, "tunnel": tunnel, "readOnly": st.ReadOnly})
}

func (s *Server) handleIPv64TunnelConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Router  string `json:"router"`
		Gateway string `json:"gateway"`
		Name    string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !dnsprovider.ValidIPv64Ref(req.Router) || !dnsprovider.ValidIPv64Ref(req.Gateway) || strings.TrimSpace(req.Name) == "" || len(req.Name) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "router, gateway or name")
		return
	}
	noteTarget(r, req.Name)
	s.ipv64Run(w, r, 5*time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		if err := s.opts.IPv64.Connect(ctx, c, req.Router, req.Gateway, req.Name); err != nil {
			return 0, nil, err
		}
		slog.Info("NAS connected to an IPv64 Cloud Router", "gateway", req.Gateway, "by", currentUser(r).Name)
		return 0, s.opts.IPv64.Status().Tunnel, nil
	})
}

func (s *Server) handleIPv64TunnelDisconnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeletePeer bool `json:"deletePeer"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	noteTarget(r, map[bool]string{true: "delete peer", false: "keep peer"}[req.DeletePeer])
	client, _ := s.opts.IPv64.Client()
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := s.opts.IPv64.Disconnect(ctx, client, req.DeletePeer); err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
