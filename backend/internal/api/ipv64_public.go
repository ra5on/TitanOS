package api

import (
	"context"
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

// "Public through IPv64" in the app editor: a Docker app published
// through the CDN (reverse proxy) of IPv64.net. It is what stayed of the
// account area of IPv64 (removed with PLAN 79-2).

// ipv64Wait is how often a background change of IPv64 is looked at.
var ipv64Wait = 3 * time.Second

// handleIPv64AppDomains lists the domains of the account an app can be
// published under.
func (a ipv64Area) handleIPv64AppDomains(w http.ResponseWriter, r *http.Request) {
	a.ipv64Run(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		domains, err := c.Domains(ctx)
		return 0, map[string]any{"domains": domains}, err
	})
}

// reservedPort tells whether a port belongs to titand (web UI, HTTPS names
// of apps) or another service of the NAS that must never be published.
func (a ipv64Area) reservedPort(port int) bool {
	reserved := []int{22, 80, 443, 8750, 8752}
	reserved = append(reserved, a.reserved...)
	if a.appProxy != nil {
		reserved = append(reserved, a.appProxy.Reserved...)
		if a.appProxy.HTTPSPort > 0 {
			reserved = append(reserved, a.appProxy.HTTPSPort)
		}
	}
	return slices.Contains(reserved, port)
}

// appPorts are the TCP ports the containers of an app publish on the NAS.
func (a ipv64Area) appPorts(ctx context.Context, app string) ([]int, bool) {
	if a.state.appPorts != nil {
		return a.state.appPorts(app)
	}
	if a.docker == nil {
		return nil, false
	}
	status := a.docker.Status(ctx)
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

func (a ipv64Area) handleIPv64App(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	ports, _ := a.appPorts(r.Context(), app)
	var usable []int
	for _, p := range ports {
		if !a.reservedPort(p) {
			usable = append(usable, p)
		}
	}
	entry, published := a.ipv64.PublishedApp(app)
	out := map[string]any{"ports": append([]int{}, usable...), "firewall": a.firewall.Enabled(), "zones": a.firewall.AppZones(app)}
	if published {
		out["published"] = entry
	}
	_, source := a.ipv64.Key()
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

// publishRequest is how an app is to be published through IPv64.
type publishRequest struct {
	Domain   string `json:"domain"`
	Prefix   string `json:"prefix"`
	Port     int    `json:"port"`
	Scheme   string `json:"scheme"`
	Firewall bool   `json:"firewall"`
	// Confirm is the click in the warning dialog.
	Confirm bool `json:"confirm"`
}

// publishRefusal is why a request to publish cannot go ahead.
type publishRefusal struct {
	status        int
	code, message string
}

// checkPublish normalizes a request and returns why it cannot be
// published, or nil. replacing allows an app that is published already
// (its publication is taken back before the new one).
func (a ipv64Area) checkPublish(ctx context.Context, app string, req *publishRequest, replacing bool) *publishRefusal {
	req.Domain, req.Prefix = strings.ToLower(strings.TrimSpace(req.Domain)), strings.ToLower(strings.TrimSpace(req.Prefix))
	if req.Scheme == "" {
		req.Scheme = "http"
	}
	switch {
	case !req.Confirm:
		return &publishRefusal{http.StatusBadRequest, "ipv64_confirm_required", "publishing an app needs the confirmation"}
	case !dnsprovider.ValidHost(req.Domain) || (req.Prefix != "" && !dnsprovider.ValidHost(req.Prefix+"."+req.Domain)) || strings.Contains(req.Prefix, "."):
		return &publishRefusal{http.StatusBadRequest, "invalid_ipv64", "domain or prefix"}
	case req.Scheme != "http" && req.Scheme != "https":
		return &publishRefusal{http.StatusBadRequest, "invalid_ipv64", "scheme"}
	case a.reservedPort(req.Port):
		return &publishRefusal{http.StatusBadRequest, "ipv64_reserved_port", "the ports of the TitanOS web UI are never published"}
	}
	if _, ok := a.ipv64.PublishedApp(app); ok && !replacing {
		return &publishRefusal{http.StatusConflict, "ipv64_exists", "the app is already published"}
	}
	ports, found := a.appPorts(ctx, app)
	if !found || !slices.Contains(ports, req.Port) {
		return &publishRefusal{http.StatusBadRequest, "ipv64_port_not_published", "the app publishes no such TCP port on the NAS"}
	}
	return nil
}

// handleIPv64Publish makes an app reachable from the internet (see
// publishApp).
func (a ipv64Area) handleIPv64Publish(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	var req publishRequest
	if !readJSON(w, r, &req) {
		return
	}
	if refusal := a.checkPublish(r.Context(), app, &req, false); refusal != nil {
		writeError(w, refusal.status, refusal.code, refusal.message)
		return
	}
	noteTarget(r, app, req.Domain)
	a.ipv64Run(w, r, 3*time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		entry, err := a.publishApp(ctx, c, app, req, currentUser(r).Name)
		if err != nil {
			return 0, nil, err
		}
		return http.StatusCreated, entry, nil
	})
}

// publishApp makes an app reachable from the internet: A/AAAA for the
// name (created with the addresses of dynamic DNS, or the one the request
// comes from), the domain in the CDN, the records routed through it, the
// reverse proxy on with HTTPS to the app's port, and, if the firewall is
// on and the owner allows it, the app's ports open from the internet
// zone. req must have passed checkPublish.
func (a ipv64Area) publishApp(ctx context.Context, c *dnsprovider.IPv64, app string, req publishRequest, by string) (ipv64.Published, error) {
	fail := func(err error) (ipv64.Published, error) { return ipv64.Published{}, err }
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
		return fail(err)
	}
	if len(records) == 0 {
		v4, v6 := a.ddnsAddresses()
		if _, err := c.UpdatePrefix(ctx, req.Domain, req.Prefix, v4, v6); err != nil {
			return fail(err)
		}
		if records, err = names(); err != nil {
			return fail(err)
		}
	}
	cdns, err := c.CDNs(ctx)
	if err != nil {
		return fail(err)
	}
	for _, cdn := range cdns {
		if cdn.Domain != nil && *cdn.Domain == req.Domain {
			entry.CDN = cdn.ID
			detail, err := c.CDN(ctx, cdn.ID)
			if err != nil {
				return fail(err)
			}
			if detail.Proxy.State == "on" && detail.Proxy.Backend.Port != 0 && detail.Proxy.Backend.Port != req.Port {
				return fail(&dnsprovider.APIError{Provider: "IPv64", Status: http.StatusConflict, Code: "cdn_in_use", Message: "the CDN of this domain already sends to another port; a domain has one backend port"})
			}
		}
	}
	if entry.CDN == "" {
		cdn, err := c.CreateCDN(ctx, req.Domain)
		if err != nil {
			return fail(err)
		}
		entry.CDN, entry.CreatedCDN = cdn.ID, true
	}
	// "Fertig ist die Änderung, wenn bei CDN-Domains die Liste pending
	// leer ist": wait for the server, looking every few seconds.
	if err := waitForServer(ctx, c, entry.CDN); err != nil {
		_ = a.unpublish(context.WithoutCancel(ctx), c, entry) // cleanup, best effort: nothing more can be done if it fails
		return fail(err)
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
			_ = a.unpublish(context.WithoutCancel(ctx), c, entry) // cleanup, best effort: nothing more can be done if it fails
			return fail(err)
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
		_ = a.unpublish(context.WithoutCancel(ctx), c, entry) // cleanup, best effort: nothing more can be done if it fails
		return fail(err)
	}
	frontend, on := "https_redirect", true
	in := dnsprovider.CDNInput{Frontend: &frontend, BackendScheme: &req.Scheme, BackendPort: &req.Port, Websocket: &on}
	var detail dnsprovider.IPv64CDN
	if err := untilReady(ctx, func() error {
		var err error
		detail, err = c.ChangeCDN(ctx, entry.CDN, in)
		return err
	}); err != nil {
		_ = a.unpublish(context.WithoutCancel(ctx), c, entry) // cleanup, best effort: nothing more can be done if it fails
		return fail(err)
	}
	entry.Pending = detail.Pending
	if req.Firewall && a.firewall.Enabled() {
		before := a.firewall.AppZones(app)
		zones := before
		zones.Internet = true
		changed, err := a.firewall.SetAppZones(ctx, app, zones)
		if err != nil {
			slog.Error("firewall zones of a published app not changed", "app", app, "err", err)
		} else if changed {
			entry.Firewall = true
			entry.Before = &ipv64.AppZones{Home: before.Home, VPN: before.VPN, Internet: before.Internet}
		}
	}
	if err := a.ipv64.AddPublished(entry); err != nil {
		return fail(err)
	}
	slog.Info("app published through IPv64", "app", app, "domain", req.Domain, "port", req.Port, "by", by)
	return entry, nil
}

func (a ipv64Area) ddnsAddresses() (v4, v6 netip.Addr) {
	if a.ddns == nil {
		return
	}
	return a.ddns.Addresses()
}

// unpublish routes the records back, switches off and removes what was
// made for the app; errors are logged, the caller reports its own.
func (a ipv64Area) unpublish(ctx context.Context, c *dnsprovider.IPv64, entry ipv64.Published) error {
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
		if _, err := a.firewall.SetAppZones(ctx, entry.App, firewall.Zones{Home: entry.Before.Home, VPN: entry.Before.VPN, Internet: entry.Before.Internet}); err != nil {
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

func (a ipv64Area) handleIPv64Unpublish(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	entry, ok := a.ipv64.PublishedApp(app)
	if !ok {
		writeError(w, http.StatusNotFound, "ipv64_not_found", "the app is not published")
		return
	}
	noteTarget(r, app, entry.Domain)
	a.ipv64Run(w, r, 3*time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error) {
		if err := a.unpublish(ctx, c, entry); err != nil {
			return 0, nil, err
		}
		return 0, nil, a.ipv64.RemovePublished(app)
	})
}
