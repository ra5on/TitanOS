package api

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

// agentAddressesServer is a test server whose Let's Encrypt domains and
// IPv64.net names come from the stick, as on a NAS.
func agentAddressesServer(t *testing.T) *testServer {
	t.Helper()
	return newTestServerWith(t, func(o *Options) {
		write := func(store *config.Store, name string, v any) {
			if err := store.WriteJSON(name, v); err != nil {
				t.Fatal(err)
			}
		}
		write(o.Store, "tls/acme/settings.json", map[string]any{
			"enabled": true, "challenge": "dns",
			"domains": []string{"nas.example.org", "*.apps.example.org", "home.ipv64.net", "nas.example.org."},
		})
		write(o.Store, "ddns/settings.json", map[string]any{
			"enabled": true, "hostnames": []string{"home.ipv64.net", "other.ipv64.net ", "nas.example.org"},
			"provider": map[string]any{"type": "ipv64", "token": "x"},
		})
		o.LetsEncrypt = disabledLetsEncrypt(o.Store)
		o.DDNS = disabledDDNS(o.Store)
	})
}

// The addresses of the agent app (PLAN 53) stay as they were when the
// setup page of the iPhone (TECHNIK 4.2) took the same sources: order LAN,
// Tailscale, NetBird, DDNS, domain; the first kind of a host wins; the LAN
// follows the scheme of the request, DDNS is https only as a Let's Encrypt
// domain, wildcards are left out, a trailing dot goes.
func TestAgentAddressesUnchanged(t *testing.T) {
	ts := agentAddressesServer(t)
	lan := appproxy.PrimaryIPv4()
	for _, secure := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/api/v1/agent/addresses", nil)
		scheme := "http"
		if secure {
			r.TLS = &tls.ConnectionState{}
			scheme = "https"
		}
		var want []agentAddress
		if lan != "" {
			want = append(want, agentAddress{Kind: "lan", URL: scheme + "://" + lan + "/agent", HTTPS: secure})
		}
		want = append(want,
			agentAddress{Kind: "ddns", URL: "https://home.ipv64.net/agent", HTTPS: true},
			agentAddress{Kind: "ddns", URL: "http://other.ipv64.net/agent", HTTPS: false},
			agentAddress{Kind: "ddns", URL: "https://nas.example.org/agent", HTTPS: true},
		)
		if got := ts.server.agentAddresses(r); !reflect.DeepEqual(got, want) {
			t.Fatalf("secure %v:\n got %+v\nwant %+v", secure, got, want)
		}
	}
}

// Tailscale and NetBird (the managers are switched off in tests): the
// same URLs as before the shared helper.
func TestAgentAddressesTailnets(t *testing.T) {
	for _, c := range []struct {
		src  reachSources
		want []agentAddress
	}{
		{reachSources{tsName: "titan.tail1.ts.net.", tsIPs: []string{"100.64.0.1"}, tsCertOK: true, netbird: "titan.netbird.cloud"}, []agentAddress{
			{Kind: "tailscale", URL: "https://titan.tail1.ts.net/agent", HTTPS: true},
			{Kind: "netbird", URL: "http://titan.netbird.cloud/agent"},
		}},
		{reachSources{lan: "192.168.1.20", tsName: "titan.tail1.ts.net"}, []agentAddress{
			{Kind: "lan", URL: "http://192.168.1.20/agent"},
			{Kind: "tailscale", URL: "http://titan.tail1.ts.net/agent"},
		}},
		{reachSources{tsIPs: []string{"100.64.0.1", "fd7a::1"}, tsCertOK: true}, []agentAddress{
			{Kind: "tailscale", URL: "http://100.64.0.1/agent"},
		}},
		{reachSources{}, []agentAddress{}},
	} {
		if got := agentAddressesOf(reachList(c.src), false); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v:\n got %+v\nwant %+v", c.src, got, c.want)
		}
	}
}

func setupCertsFor(valid ...string) setupCerts {
	return setupCerts{letsEncrypt: func(h string) bool { return slices.Contains(valid, h) }, fallback: certSelfSigned}
}

// TECHNIK 4.2: Let's Encrypt, Tailscale (recommended), home network, then
// DDNS and NetBird; a domain only with a valid certificate.
func TestSetupAddressesOrder(t *testing.T) {
	list := reachList(reachSources{
		lan: "192.168.1.20", tsName: "titan.tail1.ts.net", tsCertOK: true, netbird: "titan.netbird.cloud",
		ddns: []string{"home.ipv64.net", "plain.ipv64.net"}, domains: []string{"nas.example.org", "old.example.org", "home.ipv64.net", "*.example.org"},
	})
	got := setupAddresses(list, "alex", 443, setupCertsFor("nas.example.org", "home.ipv64.net"))
	type row struct{ kind, server, cert string }
	var rows []row
	for _, a := range got {
		rows = append(rows, row{a.Kind, a.Server, a.Certificate})
	}
	want := []row{
		{"domain", "home.ipv64.net", "valid"},
		{"domain", "nas.example.org", "valid"},
		{"tailscale", "titan.tail1.ts.net", "valid"},
		{"lan", "192.168.1.20", "selfSigned"},
		{"ddns", "plain.ipv64.net", "selfSigned"},
		{"netbird", "titan.netbird.cloud", "selfSigned"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("order:\n got %+v\nwant %+v", rows, want)
	}
	for _, a := range got {
		if a.Recommended != (a.Kind == "tailscale") {
			t.Errorf("recommended %s: %v", a.Server, a.Recommended)
		}
	}
	ts, lan := got[2], got[3]
	if ts.CalDAV != "https://titan.tail1.ts.net/caldav/alex/" || ts.CardDAV != "https://titan.tail1.ts.net/carddav/alex/" {
		t.Errorf("full addresses: %+v", ts)
	}
	if !reflect.DeepEqual(ts.Hints, []string{"tailscaleApp"}) || !reflect.DeepEqual(lan.Hints, []string{"homeOnly", "selfSignedOnce"}) {
		t.Errorf("hints: %v / %v", ts.Hints, lan.Hints)
	}
	if !reflect.DeepEqual(got[0].Hints, []string{}) {
		t.Errorf("domain hints: %v", got[0].Hints)
	}
	// Without a valid certificate the domains go; Tailscale asks for its
	// certificate; without Tailscale the domain is recommended.
	got = setupAddresses(reachList(reachSources{lan: "192.168.1.20", tsName: "titan.tail1.ts.net", domains: []string{"nas.example.org"}}), "alex", 443, setupCertsFor())
	if len(got) != 2 || got[0].Kind != "tailscale" || got[0].Certificate != "selfSigned" || !got[0].Recommended ||
		!reflect.DeepEqual(got[0].Hints, []string{"tailscaleApp", "tailscaleCertificate", "selfSignedOnce"}) {
		t.Fatalf("no certificates: %+v", got)
	}
	got = setupAddresses(reachList(reachSources{lan: "192.168.1.20", domains: []string{"nas.example.org"}}), "alex", 443, setupCertsFor("nas.example.org"))
	if len(got) != 2 || !got[0].Recommended || got[0].Kind != "domain" || got[1].Recommended {
		t.Fatalf("without Tailscale: %+v", got)
	}
}

// A port other than 443 is part of the server and of the full addresses;
// an own certificate is "own" (no warning hint); IPv6 in brackets.
func TestSetupAddressesPortAndCertificate(t *testing.T) {
	list := reachList(reachSources{lan: "192.168.1.20", tsIPs: []string{"fd7a:115c::1"}})
	certs := setupCertsFor()
	certs.fallback = certOwn
	got := setupAddresses(list, "bea", 8443, certs)
	if len(got) != 2 {
		t.Fatalf("addresses: %+v", got)
	}
	ts, lan := got[0], got[1]
	if lan.Server != "192.168.1.20:8443" || lan.CalDAV != "https://192.168.1.20:8443/caldav/bea/" || lan.CardDAV != "https://192.168.1.20:8443/carddav/bea/" {
		t.Errorf("port: %+v", lan)
	}
	if lan.Certificate != "own" || slices.Contains(lan.Hints, "selfSignedOnce") {
		t.Errorf("own certificate: %+v", lan)
	}
	if ts.Server != "[fd7a:115c::1]:8443" || ts.CalDAV != "https://[fd7a:115c::1]:8443/caldav/bea/" {
		t.Errorf("IPv6: %+v", ts)
	}
	got = setupAddresses(list, "bea", 443, setupCertsFor())
	if got[0].Server != "[fd7a:115c::1]" || got[1].Server != "192.168.1.20" || got[1].Certificate != "selfSigned" {
		t.Errorf("443: %+v", got)
	}
}

// Every account may read the setup (host names only), with the port of
// the web UI; never a password or a hash.
func TestDeviceSetupRoute(t *testing.T) {
	ts := newTestServerWith(t, func(o *Options) { o.HTTPSPort = 8443 })
	ts.setup()
	guest := ts.guest("bea")
	res, body := ts.do(guest, "GET", "/api/v1/account/devices/setup", nil)
	var got deviceSetup
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &got) != nil || got.User != "bea" || got.Port != 8443 || got.Addresses == nil {
		t.Fatalf("setup: %d %s", res.StatusCode, body)
	}
	if strings.Contains(string(body), "password") || strings.Contains(string(body), "sha256") {
		t.Fatalf("secret in %s", body)
	}
	if lan := appproxy.PrimaryIPv4(); lan != "" {
		if len(got.Addresses) == 0 || got.Addresses[0].Server != lan+":8443" || got.Addresses[0].CardDAV != "https://"+lan+":8443/carddav/bea/" {
			t.Fatalf("home network: %s", body)
		}
	}
}
