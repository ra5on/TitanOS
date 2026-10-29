package appproxy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSLabel(t *testing.T) {
	for in, want := range map[string]string{
		"immich": "immich",
		"My_App": "my-app",
		"_x__y-": "x-y",
		"___":    "app",
		"a.b c":  "a-b-c",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef-zz": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde",
	} {
		if got := DNSLabel(in); got != want || !labelRE.MatchString(got) {
			t.Errorf("DNSLabel(%q) = %q, want %q", in, got, want)
		}
	}
	taken := map[string]bool{"immich": true, "immich-2": true}
	if got := freeLabel("immich", func(n string) bool { return taken[n] }); got != "immich-3" {
		t.Fatalf("free label %q", got)
	}
}

func TestPublishTailnetOneClick(t *testing.T) {
	m, fake, mount := newTailnetManager(t)
	ctx := context.Background()
	var probed []string
	m.Tailnet.probe = func(_ context.Context, ip, name string) (int, error) {
		probed = append(probed, ip, name)
		return 200, nil
	}

	// No key stored and none given: nothing changes, the page asks for one.
	if _, err := m.PublishTailnet("my_app", 8080, ""); !errors.Is(err, ErrTailnetNoKey) {
		t.Fatalf("without key: %v", err)
	}
	if len(m.Settings().Apps) != 0 {
		t.Fatal("route stored without a key")
	}
	if _, err := m.PublishTailnet("my_app", 8080, "not-a-key"); !errors.Is(err, ErrTailnetKey) {
		t.Fatalf("bad key: %v", err)
	}
	// A name of another route is taken: the app gets the next free one.
	m.SetRoute("other", Route{Enabled: true, Name: "my-app", Port: 9000})
	if _, err := m.PublishTailnet("my_app", 0, "tskey-auth-abc"); !errors.Is(err, ErrNoPort) {
		t.Fatalf("no port: %v", err)
	}
	route, err := m.PublishTailnet("my_app", 8080, "tskey-auth-abc")
	if err != nil {
		t.Fatal(err)
	}
	stored := m.Settings().Apps["my_app"]
	if route.Name != "my-app-2" || stored.TailscaleName != "my-app" || !stored.Tailscale || !stored.Enabled || !stored.LocalOnly || !stored.TailnetOnly || stored.Port != 8080 {
		t.Fatalf("route %+v", stored)
	}
	if !m.TailnetInfo().KeySet {
		t.Fatal("key not stored")
	}
	// Not running yet: no link, no check.
	if m.TailnetURL("my_app") != "" {
		t.Fatal("link before the device runs")
	}
	if _, err := m.CheckTailnet(ctx, "my_app"); !errors.Is(err, ErrTailnetNotReady) {
		t.Fatalf("check too early: %v", err)
	}

	m.reconcileTailnet(ctx)
	n := m.tailnetNode("app:my_app")
	if n == nil || n.State != "running" {
		t.Fatalf("node %+v", n)
	}
	if got := fake.called("tailscale --socket=" + m.Tailnet.socket("app-my_app") + " up"); len(got) != 1 || !strings.Contains(got[0], "--hostname=my-app") {
		t.Fatalf("up: %v", got)
	}
	if m.TailnetURL("my_app") != n.URL {
		t.Fatalf("link %q, want %q", m.TailnetURL("my_app"), n.URL)
	}
	check, err := m.CheckTailnet(ctx, "my_app")
	if err != nil || !check.Reachable || check.Status != 200 || check.URL != n.URL {
		t.Fatalf("check %+v %v", check, err)
	}
	// IPv4 first, with the ts.net name for the certificate.
	if len(probed) != 2 || probed[0] != "100.101.102.103" || probed[1] != n.dns {
		t.Fatalf("probed %v", probed)
	}
	m.Tailnet.probe = func(context.Context, string, string) (int, error) {
		return 0, errors.New("tls: failed to verify certificate")
	}
	if check, err := m.CheckTailnet(ctx, "my_app"); err != nil || check.Reachable || check.Error == "" {
		t.Fatalf("failing check %+v %v", check, err)
	}

	// A second click changes nothing.
	if _, err := m.PublishTailnet("my_app", 1234, ""); err != nil {
		t.Fatal(err)
	}
	if again := m.Settings().Apps["my_app"]; again.Port != 8080 || again.Name != "my-app-2" || again.TailscaleName != "my-app" {
		t.Fatalf("second click: %+v", again)
	}

	// Out of the tailnet: the route it made is off, the device logs out
	// and its state is gone.
	if err := m.UnpublishTailnet("my_app"); err != nil {
		t.Fatal(err)
	}
	if off := m.Settings().Apps["my_app"]; off.Enabled || off.Tailscale || off.TailscaleName != "" || off.TailnetOnly {
		t.Fatalf("after removing: %+v", off)
	}
	m.reconcileTailnet(ctx)
	if len(fake.called("tailscale --socket="+m.Tailnet.socket("app-my_app")+" logout")) != 1 || len(fake.called("systemctl stop titanos-ts-app-my_app")) == 0 {
		t.Fatal("no logout")
	}
	if _, err := os.Stat(filepath.Join(mount, "data", tailnetState, "app-my_app")); !os.IsNotExist(err) {
		t.Fatal("state kept")
	}
	if m.TailnetURL("my_app") != "" {
		t.Fatal("link kept")
	}
	if err := m.UnpublishTailnet("my_app"); err != nil {
		t.Fatal("second remove:", err)
	}
	// Published again, the switched-off route comes back on.
	if _, err := m.PublishTailnet("my_app", 0, ""); err != nil {
		t.Fatal(err)
	}
	if back := m.Settings().Apps["my_app"]; !back.Enabled || !back.Tailscale || !back.TailnetOnly {
		t.Fatalf("again: %+v", back)
	}
}

func TestPublishTailnetKeepsOwnRoute(t *testing.T) {
	m, _, _ := newTailnetManager(t)
	m.SetTailnetKey("tskey-auth-x")
	m.SetRoute("immich", Route{Enabled: true, Name: "fotos", Aliases: []string{"bilder"}, Port: 2283})
	// Another device already has the app's name in the tailnet.
	m.SetRoute("old", Route{Enabled: true, Name: "old", Port: 2284, Tailscale: true, TailscaleName: "immich"})
	if _, err := m.PublishTailnet("immich", 9999, ""); err != nil {
		t.Fatal(err)
	}
	route := m.Settings().Apps["immich"]
	if route.Name != "fotos" || route.Port != 2283 || len(route.Aliases) != 1 || route.TailscaleName != "immich-2" || route.TailnetOnly || !route.Tailscale {
		t.Fatalf("own route %+v", route)
	}
	// Taken out again, the own HTTPS name stays on.
	m.UnpublishTailnet("immich")
	if route := m.Settings().Apps["immich"]; !route.Enabled || route.Tailscale || route.Name != "fotos" {
		t.Fatalf("after removing %+v", route)
	}
	// Saving the route in a form makes it the user's own.
	m.PublishTailnet("new", 7000, "")
	m.SetRoute("new", Route{Enabled: true, Name: "new", Port: 7000, Tailscale: true})
	if m.Settings().Apps["new"].TailnetOnly {
		t.Fatal("form kept the one-click mark")
	}
}

func TestPublishTailnetRefused(t *testing.T) {
	m, fake, _ := newTailnetManager(t)
	m.Tailnet.Volume = func() (string, bool) { return "", false }
	if _, err := m.PublishTailnet("immich", 2283, "tskey-auth-x"); !errors.Is(err, ErrTailnetUnavailable) {
		t.Fatalf("plugin inactive: %v", err)
	}
	if m.TailnetInfo().KeySet || len(m.Settings().Apps) != 0 || len(fake.calls) != 0 {
		t.Fatal("changed something without the plugin")
	}
	m.Tailnet.Volume = func() (string, bool) { return "data", true }
	m.SetGeneral(General{Off: ptr(true)})
	if _, err := m.PublishTailnet("immich", 2283, "tskey-auth-x"); !errors.Is(err, ErrProxyOff) {
		t.Fatalf("all names off: %v", err)
	}
	if _, err := m.PublishTailnet("../x", 2283, "tskey-auth-x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad app: %v", err)
	}
}
