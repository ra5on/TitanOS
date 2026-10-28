package ddns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
)

type fakeUpdater struct {
	mu    sync.Mutex
	calls []string
	fail  error
}

func (f *fakeUpdater) SetAddresses(_ context.Context, host string, v4, v6 netip.Addr) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.calls = append(f.calls, fmt.Sprintf("%s %v %v", host, v4, v6))
	return nil
}

func (f *fakeUpdater) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

type world struct {
	m       *Manager
	up      *fakeUpdater
	v4      netip.Addr
	v6      netip.Addr
	dns     map[string][]netip.Addr
	events  []string
	lookups int
}

func newWorld(t *testing.T) *world {
	w := &world{up: &fakeUpdater{}, v4: netip.MustParseAddr("203.0.113.10"), v6: netip.MustParseAddr("2001:db8:1::10"), dns: map[string][]netip.Addr{}}
	w.m = New(config.Open(t.TempDir()))
	w.m.lookupIPv4 = func(context.Context, string) (netip.Addr, error) {
		w.lookups++
		if !w.v4.IsValid() {
			return netip.Addr{}, errors.New("no answer")
		}
		return w.v4, nil
	}
	w.m.ipv6 = func(string) (netip.Addr, error) {
		if !w.v6.IsValid() {
			return netip.Addr{}, errors.New("none")
		}
		return w.v6, nil
	}
	w.m.resolve = func(_ context.Context, host string) ([]netip.Addr, error) {
		if addrs, ok := w.dns[host]; ok {
			return addrs, nil
		}
		return nil, errors.New("no such host")
	}
	w.m.newUpdater = func(dnsprovider.Config) (dnsprovider.Updater, error) { return w.up, nil }
	w.m.OnEvent = func(_, kind, subject string) { w.events = append(w.events, kind+" "+subject) }
	w.m.Activity = func(action, target string, ok bool) { w.events = append(w.events, action) }
	if err := w.m.Load(); err != nil {
		t.Fatal(err)
	}
	return w
}

func settings() Settings {
	return Settings{Enabled: true, Hostnames: []string{"nas.example.org"}, IPv4: true, IPv6: true,
		Provider: dnsprovider.Config{Type: dnsprovider.Cloudflare, Token: "secret"}}
}

func TestUpdatesOnlyOnChange(t *testing.T) {
	w := newWorld(t)
	if err := w.m.Set(settings(), nil); err != nil {
		t.Fatal(err)
	}
	w.m.check(context.Background(), false)
	if got := w.up.take(); !slices.Equal(got, []string{"nas.example.org 203.0.113.10 2001:db8:1::10"}) {
		t.Fatal(got)
	}
	st := w.m.Status()
	if st.PublishedIPv4 != "203.0.113.10" || st.PublishedIPv6 != "2001:db8:1::10" || st.LastUpdate == nil || st.LastError != "" {
		t.Fatalf("%+v", st)
	}
	w.m.check(context.Background(), false)
	if got := w.up.take(); len(got) != 0 {
		t.Fatal("nothing changed, nothing sent", got)
	}
	// Only the IPv4 address changes: only the A record is sent.
	w.v4 = netip.MustParseAddr("198.51.100.20")
	w.m.check(context.Background(), false)
	if got := w.up.take(); !slices.Equal(got, []string{"nas.example.org 198.51.100.20 invalid IP"}) {
		t.Fatal(got)
	}
	// "Update now" sends both again.
	w.m.check(context.Background(), true)
	if got := w.up.take(); len(got) != 1 || !strings.Contains(got[0], "2001:db8:1::10") {
		t.Fatal(got)
	}
	if st := w.m.Status(); st.Settings.Provider.Token != "" || !st.Secrets["token"] {
		t.Fatal("token in status")
	}
}

func TestStartSeedsFromDNS(t *testing.T) {
	w := newWorld(t)
	w.dns["nas.example.org"] = []netip.Addr{w.v4, w.v6}
	if err := w.m.Set(settings(), nil); err != nil {
		t.Fatal(err)
	}
	w.m.check(context.Background(), false)
	if got := w.up.take(); len(got) != 0 {
		t.Fatal("the records already point here", got)
	}
	// After a forced update DNS caches are not asked again.
	w.dns["nas.example.org"] = []netip.Addr{netip.MustParseAddr("192.0.2.99")}
	w.m.check(context.Background(), true)
	w.up.take()
	w.m.check(context.Background(), false)
	if got := w.up.take(); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestFailureNotifiesOnceAndRetries(t *testing.T) {
	w := newWorld(t)
	w.up.fail = &dnsprovider.APIError{Provider: "Cloudflare", Status: 403, Message: "Invalid access token"}
	if err := w.m.Set(settings(), nil); err != nil {
		t.Fatal(err)
	}
	w.m.check(context.Background(), false)
	w.m.check(context.Background(), false)
	st := w.m.Status()
	if !strings.Contains(st.LastError, "Invalid access token") || st.PublishedIPv4 != "" {
		t.Fatalf("%+v", st)
	}
	count := 0
	for _, e := range w.events {
		if e == "ddns_failed nas.example.org" {
			count++
		}
	}
	if count != 1 {
		t.Fatal(w.events)
	}
	w.up.fail = nil
	w.m.check(context.Background(), false)
	if got := w.up.take(); len(got) != 1 || w.m.Status().LastError != "" {
		t.Fatal("retry after failure", got, w.m.Status().LastError)
	}
	// IPv4 unknown: IPv6 is still sent, the problem is shown.
	w.v4 = netip.Addr{}
	w.v6 = netip.MustParseAddr("2001:db8:2::1")
	w.m.check(context.Background(), false)
	if got := w.up.take(); !slices.Equal(got, []string{"nas.example.org invalid IP 2001:db8:2::1"}) || !strings.Contains(w.m.Status().LastError, "IPv4") {
		t.Fatal(got, w.m.Status().LastError)
	}
}

func TestRunAndUpdateNow(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { w.m.Run(ctx); close(done) }()
	if err := w.m.UpdateNow(); !errors.Is(err, ErrNotEnabled) {
		t.Fatal(err)
	}
	if err := w.m.Set(settings(), nil); err != nil {
		t.Fatal(err)
	}
	waitCalls := func(n int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			w.up.mu.Lock()
			got := len(w.up.calls)
			w.up.mu.Unlock()
			if got >= n {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d updates, want %d", got, n)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitCalls(1)
	if err := w.m.UpdateNow(); err != nil {
		t.Fatal(err)
	}
	waitCalls(2)
	cancel()
	<-done
	if st := w.m.Status(); st.NextCheck == nil || st.NextCheck.Sub(*st.LastCheck) > 11*time.Minute {
		t.Fatalf("%+v", st)
	}
}

func TestValidation(t *testing.T) {
	w := newWorld(t)
	bad := map[string]func(*Settings){
		"no host":        func(s *Settings) { s.Hostnames = nil },
		"local":          func(s *Settings) { s.Hostnames = []string{"nas.local"} },
		"no family":      func(s *Settings) { s.IPv4, s.IPv6 = false, false },
		"interval":       func(s *Settings) { s.Interval = 1 },
		"lookup":         func(s *Settings) { s.LookupURL = "file:///etc/passwd" },
		"interface":      func(s *Settings) { s.Interface = "eth0; rm" },
		"no token":       func(s *Settings) { s.Provider.Token = ""; s.Provider.Type = dnsprovider.DeSEC },
		"duck elsewhere": func(s *Settings) { s.Provider = dnsprovider.Config{Type: dnsprovider.DuckDNS, Token: "t"} },
		"too many": func(s *Settings) {
			for i := range 9 {
				s.Hostnames = append(s.Hostnames, fmt.Sprintf("h%d.example.org", i))
			}
		},
	}
	for name, change := range bad {
		s := settings()
		change(&s)
		if err := w.m.Set(s, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	s := settings()
	s.Provider = dnsprovider.Config{Type: dnsprovider.DynDNS2, Server: "https://dyndns.strato.com/nic/update?hostname=<domain>&myip=<ipaddr>,<ip6addr>", User: "nas.example.org", Password: "pw"}
	if err := w.m.Set(s, nil); err != nil {
		t.Fatal(err)
	}
	s.Provider.Password = ""
	if err := w.m.Set(s, nil); err != nil {
		t.Fatal("password kept", err)
	}
	if err := w.m.Set(Settings{}, nil); err != nil {
		t.Fatal("switched off without anything", err)
	}
}

func TestLookupIPv4(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, " 203.0.113.55\n")
	}))
	defer server.Close()
	addr, err := lookupIPv4(context.Background(), server.URL)
	if err != nil || addr != netip.MustParseAddr("203.0.113.55") {
		t.Fatal(addr, err)
	}
	// A private answer (a lookup service in the home network) is refused.
	w := newWorld(t)
	w.m.lookupIPv4 = func(context.Context, string) (netip.Addr, error) { return netip.MustParseAddr("192.168.1.2"), nil }
	_, _, problems := w.m.detect(context.Background(), Settings{IPv4: true})
	if len(problems) != 1 || !strings.Contains(problems[0], "not a public") {
		t.Fatal(problems)
	}
}

func TestPickIPv6(t *testing.T) {
	table := strings.Join([]string{
		"00000000000000000000000000000001 01 80 10 80       lo",
		"fe800000000000000000000000000001 02 40 20 80     eth0",
		"fd000000000000000000000000000001 02 40 00 80     eth0",
		"20010db8000000000000000000000099 02 40 00 01     eth0", // temporary
		"20010db8000000000000000000000098 02 40 00 20     eth0", // deprecated
		"20010db8000000000000000000000077 05 40 00 00  docker0",
		"20010db8000000000000000000000042 02 40 00 00     eth0",
		"20010db8000000000000000000000043 03 40 00 80      br0",
	}, "\n")
	addr, err := pickIPv6(strings.NewReader(table), "")
	if err != nil || addr != netip.MustParseAddr("2001:db8::42") {
		t.Fatal(addr, err)
	}
	addr, err = pickIPv6(strings.NewReader(table), "br0")
	if err != nil || addr != netip.MustParseAddr("2001:db8::43") {
		t.Fatal(addr, err)
	}
	if _, err := pickIPv6(strings.NewReader(table), "wlan0"); err == nil {
		t.Fatal("no address on wlan0")
	}
}

