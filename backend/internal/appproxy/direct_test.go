package appproxy

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// PLAN 75-14: a direct window takes the app's name under the suffix the
// web UI is reached by; reached by an IP or a tunnel's own name: none.
func TestDirectHost(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.SetRoute("thunderbird", Route{Enabled: true, Name: "mail", Port: 5800, Scheme: "https"}); err != nil {
		t.Fatal(err)
	}
	for ui, want := range map[string]string{
		"nas.local":         "mail.nas.local",
		"nas.local:443":     "mail.nas.local",
		"immich.nas.local":  "mail.nas.local",
		"192.168.1.20":      "",
		"titan.example.org": "",
		"evilnas.local":     "",
	} {
		got, ok := m.DirectHost("thunderbird", ui)
		if got != want || ok != (want != "") {
			t.Errorf("ui %q: %q %v, want %q", ui, got, ok, want)
		}
	}
	if _, ok := m.DirectHost("none", "nas.local"); ok {
		t.Error("direct host for an app without a name")
	}
	if got := m.FrameSources(); len(got) != 1 || got[0] != "https://*.nas.local" {
		t.Errorf("frame sources %v", got)
	}
}

// The enter page of a direct window tells only the web UI's origins that
// the app was reached, by a script with a nonce, then goes on.
func TestDirectEnterPage(t *testing.T) {
	f := newGateFixture(t)
	w := f.do(EnterPath+"?t="+url.QueryEscape(directToken(f, "alex"))+"&frame=1", true, nil)
	body := w.Body.String()
	csp := w.Header().Get("Content-Security-Policy")
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 1 {
		t.Fatalf("enter: %d %v", w.Code, w.Result().Cookies())
	}
	if !strings.Contains(body, "titanos-direct") || !strings.Contains(body, "location.replace('/')") || strings.Contains(body, `"*"`) {
		t.Fatalf("page %s", body)
	}
	if !strings.Contains(csp, "script-src 'nonce-") || !strings.Contains(csp, "frame-ancestors 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("csp %q", csp)
	}
	// Without frame=1 the enter address stays a redirect.
	w = f.do(EnterPath+"?t="+url.QueryEscape(directToken(f, "alex")), true, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("plain enter: %d", w.Code)
	}
}

func directToken(f *gateFixture, user string) string {
	token, _ := f.access.EnterToken(user, "immich", auth.SessionID(f.logins[user]))
	return token
}
