package davsync

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/devices"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

func TestPath(t *testing.T) {
	for p, want := range map[string]bool{
		"/caldav": true, "/caldav/": true, "/caldav/alex/": true, "/caldav/alex/calendars/privat/a.ics": true,
		"/carddav": true, "/carddav/alex/addressbooks/kontakte/": true,
		"/.well-known/caldav": true, "/.well-known/carddav": true,
		"/caldavx": false, "/carddavx/": false, "/calendar": false, "/dav/": false, "/": false, "": false,
		"/caldav/../api/v1/system": false, "/caldav/./alex/": false, "/caldav//alex/": false, "/caldav/alex/..": false,
		"/.well-known/caldav/": false, "/.well-known/caldav/../../dav/": false, "/.well-known/caldavx": false,
		"/api/v1/caldav": false, "/x/caldav/": false,
	} {
		if got := Path(p); got != want {
			t.Errorf("Path(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestParsePath(t *testing.T) {
	for p, want := range map[string]bool{
		"/caldav/%61lex/":                                              true,  // decoded once: alex
		"/caldav/%2561lex/":                                            false, // "%61lex" is no account name
		"/caldav/alex/calendars/privat/a%2Fb.ics":                      false,
		"/caldav/alex/calendars/privat/..":                             false,
		"/caldav/alex/calendars/privat/a.ics/":                         false,
		"/caldav/alex/calendars/privat/.a.ics":                         false,
		"/caldav/alex/calendars/privat/a.vcf":                          false,
		"/caldav/alex/calendars/privat/a%00.ics":                       false,
		"/caldav/alex/calendars/Privat/":                               false,
		"/caldav/alex/addressbooks/":                                   false,
		"/carddav/alex/calendars/":                                     false,
		"/caldav/alex/calendars/privat/a.ics/x":                        false,
		"/caldav/alex/calendars/privat/a%20%C3%A4.ics":                 true,
		"/caldav/alex/calendars/5E1C3A9B-1D2E-4F3A-8B7C-6D5E4F3A2B1C/": true,
	} {
		if _, got := parsePath(p); got != want {
			t.Errorf("parsePath(%q) = %v, want %v", p, got, want)
		}
	}
	tg, _ := parsePath("/caldav/alex/calendars/privat/a%20%C3%A4.ics")
	if tg.name != "a ä.ics" || tg.href() != "/caldav/alex/calendars/privat/a%20%C3%A4.ics" {
		t.Errorf("%+v %s", tg, tg.href())
	}
}

func (e *env) do(method, path, auth string, header map[string]string, body io.Reader) (*http.Response, []byte) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, body)
	if err != nil {
		e.t.Fatal(err)
	}
	if auth != "" {
		req.SetBasicAuth(strings.Replace(auth, "account", "alex", 1), e.passwords[auth])
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := e.client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, b
}

const propfindName = `<?xml version="1.0"?><A:propfind xmlns:A="DAV:"><A:prop><A:displayname/></A:prop></A:propfind>`

func TestBodyLimits(t *testing.T) {
	e := newEnv(t)
	big := propfindName + strings.Repeat(" ", MaxBody)
	if res, _ := e.do("PROPFIND", "/caldav/alex/calendars/", "alex", nil, strings.NewReader(big)); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("large body: %d", res.StatusCode)
	}
	// Without Content-Length (chunked): cut at the limit.
	if res, _ := e.do("PROPFIND", "/caldav/alex/calendars/", "alex", nil, io.MultiReader(strings.NewReader(big))); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("large chunked body: %d", res.StatusCode)
	}
	deep := `<A:propfind xmlns:A="DAV:"><A:prop>` + strings.Repeat("<A:x>", maxXMLDepth) + strings.Repeat("</A:x>", maxXMLDepth) + `</A:prop></A:propfind>`
	if res, _ := e.do("PROPFIND", "/caldav/alex/calendars/", "alex", nil, strings.NewReader(deep)); res.StatusCode != http.StatusBadRequest {
		t.Errorf("deep body: %d", res.StatusCode)
	}
	many := `<A:propfind xmlns:A="DAV:"><A:prop>` + strings.Repeat("<A:x/>", maxXMLNodes) + `</A:prop></A:propfind>`
	if res, _ := e.do("PROPFIND", "/caldav/alex/calendars/", "alex", nil, strings.NewReader(many)); res.StatusCode != http.StatusBadRequest {
		t.Errorf("many elements: %d", res.StatusCode)
	}
	// Entities are never expanded: a DOCTYPE is refused, an entity
	// without one is an error of the strict parser.
	laughs := `<?xml version="1.0"?><!DOCTYPE l [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;&a;&a;">]>` +
		`<A:propfind xmlns:A="DAV:"><A:prop><A:displayname>&b;</A:displayname></A:prop></A:propfind>`
	for _, body := range []string{laughs, `<A:propfind xmlns:A="DAV:"><A:prop><A:displayname>&b;</A:displayname></A:prop></A:propfind>`,
		`<!DOCTYPE x SYSTEM "file:///etc/passwd"><A:propfind xmlns:A="DAV:"/>`} {
		res, b := e.do("PROPFIND", "/caldav/alex/calendars/", "alex", nil, strings.NewReader(body))
		if res.StatusCode != http.StatusBadRequest || bytes.Contains(b, []byte("aaaa")) || bytes.Contains(b, []byte("root:")) {
			t.Errorf("entities: %d %s", res.StatusCode, b)
		}
	}
	if _, err := parseXML([]byte(laughs)); err == nil {
		t.Error("DOCTYPE parsed")
	}
}

// The body has 60 s (BodyTimeout); one that stalls gets 408.
func TestBodyTimeout(t *testing.T) {
	e := newEnv(t)
	e.h.bodyTimeout = 200 * time.Millisecond
	r, w := io.Pipe()
	defer w.Close()
	go func() { _, _ = w.Write([]byte(`<A:propfind xmlns:A="DAV:">`)) }()
	start := time.Now()
	res, _ := e.do("PROPFIND", "/caldav/alex/calendars/", "alex", nil, r)
	if res.StatusCode != http.StatusRequestTimeout || time.Since(start) > 10*time.Second {
		t.Errorf("stalled body: %d after %v", res.StatusCode, time.Since(start))
	}
}

func TestMultigetLimit(t *testing.T) {
	e := newEnv(t)
	body := `<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/></D:prop>` +
		strings.Repeat("<D:href>/caldav/alex/calendars/privat/a.ics</D:href>", MaxHrefs+1) + `</C:calendar-multiget>`
	if res, _ := e.do("REPORT", "/caldav/alex/calendars/privat/", "alex", nil, strings.NewReader(body)); res.StatusCode != http.StatusInsufficientStorage {
		t.Errorf("1001 hrefs: %d", res.StatusCode)
	}
	body = `<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/></D:prop>` +
		strings.Repeat("<D:href>/caldav/alex/calendars/privat/a.ics</D:href>", MaxHrefs) + `</C:calendar-multiget>`
	if res, _ := e.do("REPORT", "/caldav/alex/calendars/privat/", "alex", nil, strings.NewReader(body)); res.StatusCode != http.StatusMultiStatus {
		t.Errorf("1000 hrefs: %d", res.StatusCode)
	}
}

// Go's own client keeps PROPFIND and its body on a 308 (not on a 301,
// which is why the sequences follow by hand, as iOS does).
func TestWellKnown308WithGoClient(t *testing.T) {
	e := newEnv(t)
	e.h.Redirect = http.StatusPermanentRedirect
	req, _ := http.NewRequest("PROPFIND", e.srv.URL+WellKnownCalDAV, strings.NewReader(`<A:propfind xmlns:A="DAV:"><A:prop><A:current-user-principal/></A:prop></A:propfind>`))
	req.SetBasicAuth("alex", e.passwords["alex"])
	req.Header.Set("Depth", "0")
	res, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus || !strings.Contains(string(b), "<d:href>/caldav/alex/</d:href>") {
		t.Errorf("308: %d %s", res.StatusCode, b)
	}
}

// Without a store (no PIM, titand -dev) discovery works, data is 503.
func TestNoStore(t *testing.T) {
	e := newEnv(t)
	e.h.Store = nil
	if res, _ := e.do("PROPFIND", "/caldav/", "alex", map[string]string{"Depth": "0"}, nil); res.StatusCode != http.StatusMultiStatus {
		t.Errorf("root: %d", res.StatusCode)
	}
	for _, p := range []string{"/caldav/alex/calendars/", "/carddav/alex/addressbooks/kontakte/"} {
		if res, _ := e.do("PROPFIND", p, "alex", map[string]string{"Depth": "1"}, nil); res.StatusCode != http.StatusServiceUnavailable || res.Header.Get("Retry-After") != "300" {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
}

// A forgotten account (deleted, made anew) ends its running requests.
func TestForgetUserEndsRequests(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := e.h.accountContext(t.Context(), "alex")
	defer cancel()
	other, cancelOther := e.h.accountContext(t.Context(), "bea")
	defer cancelOther()
	if err := e.store.ForgetUser("alex"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("request of the forgotten account goes on")
	}
	if other.Err() != nil {
		t.Error("another account's request ended")
	}
}

type syncedCall struct {
	account, device, kind string
	volumeOpen            bool
}

type recordingDevices struct {
	*devices.Manager
	mu    sync.Mutex
	calls []syncedCall
}

func (r *recordingDevices) Synced(account, device, kind, addr string, volumeOpen bool) {
	r.mu.Lock()
	r.calls = append(r.calls, syncedCall{account, device, kind, volumeOpen})
	r.mu.Unlock()
	r.Manager.Synced(account, device, kind, addr, volumeOpen)
}

// "Last synced" after an answer that succeeded; volumeOpen only where the
// volume was read anyway (GET, a REPORT with calendar-data), not for
// answers from RAM.
func TestSynced(t *testing.T) {
	e := newEnv(t)
	rec := &recordingDevices{Manager: e.devices}
	e.h.Devices = rec
	e.put(store.Calendar, "alex", "privat", "a.ics", fixture(t, "dentist.ics"))
	e.put(store.Contacts, "alex", "kontakte", "c.vcf", fixture(t, "erika.vcf"))
	multiget := `<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:prop><D:getetag/></D:prop><D:href>/caldav/alex/calendars/privat/a.ics</D:href></C:calendar-multiget>`
	for _, c := range []struct {
		method, path, body string
		want               *syncedCall
	}{
		{"PROPFIND", "/caldav/alex/calendars/", "", &syncedCall{"alex", "", "caldav", false}},
		{"HEAD", "/caldav/alex/calendars/privat/a.ics", "", &syncedCall{"alex", "", "caldav", false}},
		{"GET", "/caldav/alex/calendars/privat/a.ics", "", &syncedCall{"alex", "", "caldav", true}},
		{"GET", "/carddav/alex/addressbooks/kontakte/c.vcf", "", &syncedCall{"alex", "", "carddav", true}},
		// A multiget only of ETags is answered from RAM; with the data it reads the volume.
		{"REPORT", "/caldav/alex/calendars/privat/", multiget, &syncedCall{"alex", "", "caldav", false}},
		{"REPORT", "/caldav/alex/calendars/privat/", strings.Replace(multiget, "<D:getetag/>", "<D:getetag/><C:calendar-data/>", 1), &syncedCall{"alex", "", "caldav", true}},
		{"REPORT", "/caldav/alex/calendars/privat/", `<D:sync-collection xmlns:D="DAV:"><D:sync-token/><D:sync-level>1</D:sync-level><D:prop><D:getetag/></D:prop></D:sync-collection>`, &syncedCall{"alex", "", "caldav", false}},
		{"GET", "/caldav/alex/calendars/privat/missing.ics", "", nil},
		{"PROPFIND", "/caldav/bea/", "", nil},
	} {
		rec.mu.Lock()
		rec.calls = nil
		rec.mu.Unlock()
		e.do(c.method, c.path, "alex", map[string]string{"Depth": "1"}, strings.NewReader(c.body))
		rec.mu.Lock()
		calls := rec.calls
		rec.mu.Unlock()
		if c.want == nil {
			if len(calls) != 0 {
				t.Errorf("%s %s: %v", c.method, c.path, calls)
			}
			continue
		}
		if len(calls) != 1 || calls[0].account != c.want.account || calls[0].kind != c.want.kind || calls[0].volumeOpen != c.want.volumeOpen || calls[0].device == "" {
			t.Errorf("%s %s: %+v, want %+v", c.method, c.path, calls, *c.want)
		}
	}
}

// The journal never holds titles, addresses or passwords.
func TestJournalNamesNoContent(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	}), &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)
	e := newEnv(t)
	e.put(store.Calendar, "alex", "privat", "Zahnarzt.ics", fixture(t, "dentist.ics"))
	e.put(store.Contacts, "alex", "kontakte", "erika.vcf", fixture(t, "erika.vcf"))
	e.do("GET", "/caldav/alex/calendars/privat/Zahnarzt.ics", "alex", nil, nil)
	e.do("GET", "/caldav/alex/calendars/privat/Zahnarzt.ics", "account", nil, nil)
	e.do("GET", "/carddav/alex/addressbooks/kontakte/erika.vcf", "bea", nil, nil)
	e.setVolume("")
	e.do("GET", "/caldav/alex/calendars/privat/Zahnarzt.ics", "alex", nil, nil)
	e.setVolume("disk2")
	e.do("PROPFIND", "/carddav/alex/addressbooks/kontakte/", "alex", map[string]string{"Depth": "1"}, nil)
	mu.Lock()
	out := buf.String()
	mu.Unlock()
	for _, secret := range []string{"Kontrolle", "Erika", "erika@example.com", "Musterstraße", e.passwords["alex"], e.passwords["account"], "Zahnarzt"} {
		if strings.Contains(out, secret) {
			t.Errorf("journal has %q:\n%s", secret, out)
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// A request with credentials that fail is counted by pim/devices; one
// without is not – OPTIONS needs none at all.
func TestOptionsNeedsNoSignIn(t *testing.T) {
	e := newEnv(t)
	res, _ := e.do("OPTIONS", "/caldav/alex/", "", nil, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("DAV") == "" || res.Header.Get("Allow") == "" || res.Header.Get("WWW-Authenticate") != "" {
		t.Errorf("OPTIONS: %d %v", res.StatusCode, res.Header)
	}
	if res, _ := e.do("OPTIONS", "/caldav/alex/x/", "", nil, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("OPTIONS of no path: %d", res.StatusCode)
	}
}
