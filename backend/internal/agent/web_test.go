package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestWebAddresses(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "10.1.2.3", "192.168.178.1", "172.20.0.1", "100.100.1.1", "169.254.169.254", "::1", "fd7a:115c:a1e0::1", "fe80::1", "::ffff:192.168.1.1", "0.0.0.0", "224.0.0.1"} {
		if Allowed(netip.MustParseAddr(addr)) {
			t.Errorf("%s allowed", addr)
		}
	}
	for _, addr := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if !Allowed(netip.MustParseAddr(addr)) {
			t.Errorf("%s blocked", addr)
		}
	}
	w := NewWeb()
	// A public name that points into the home network.
	w.Resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("192.168.1.10")}, nil
	}
	if _, err := w.Read(context.Background(), "1", "http://evil.example/"); err == nil || !strings.Contains(err.Error(), "home network") {
		t.Fatalf("private address read: %v", err)
	}
	if _, err := w.Read(context.Background(), "1", "http://10.0.0.1/"); err == nil {
		t.Fatal("private IP read")
	}
	if _, err := w.Read(context.Background(), "1", "http://example.com:8080/"); err == nil {
		t.Fatal("other port read")
	}
	if _, err := w.Read(context.Background(), "1", "file:///etc/passwd"); err == nil {
		t.Fatal("file read")
	}
}

func TestWebSearchAndRead(t *testing.T) {
	var redirectTo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html/":
			r.ParseForm()
			if r.Form.Get("q") != "jellyfin port" {
				t.Errorf("query %q", r.Form.Get("q"))
			}
			w.Write([]byte(`<div class="result"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fjellyfin.org%2Fdocs&amp;rut=x">Jellyfin <b>Docs</b></a>
<a class="result__snippet" href="x">Port <b>8096</b> is the default.</a></div>
<div class="result"><a class="result__a" href="https://duckduckgo.com/y.js?ad=1">Ad</a></div>`))
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><title>Ports &amp; more</title><script>evil()</script></head><body><nav>menu</nav><p>Jellyfin listens on 8096.</p></body></html>`))
		case "/jump":
			http.Redirect(w, r, redirectTo, http.StatusFound)
		}
	}))
	defer srv.Close()
	old := SearchURL
	SearchURL = srv.URL + "/html/"
	defer func() { SearchURL = old }()
	w := NewWeb()
	allowServer := func(a netip.Addr) bool { return a.IsLoopback() }
	w.Check = allowServer
	w.Ports = append(w.Ports, srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	results, err := w.Search(context.Background(), "1", "jellyfin port")
	if err != nil || len(results) != 1 || results[0].URL != "https://jellyfin.org/docs" || results[0].Title != "Jellyfin Docs" || results[0].Snippet != "Port 8096 is the default." {
		t.Fatalf("results: %+v %v", results, err)
	}
	page, err := w.Read(context.Background(), "1", srv.URL+"/page")
	if err != nil || page.Title != "Ports & more" || strings.Contains(page.Text, "evil") || strings.Contains(page.Text, "menu") || !strings.Contains(page.Text, "8096") {
		t.Fatalf("page: %+v %v", page, err)
	}
	// A redirect into a blocked network fails like a direct address.
	w.Check = func(a netip.Addr) bool { return a.IsLoopback() && a.Is4() }
	redirectTo = "http://[::1]:" + srv.URL[strings.LastIndex(srv.URL, ":")+1:] + "/page"
	if _, err := w.Read(context.Background(), "1", srv.URL+"/jump"); err == nil {
		t.Fatal("redirect into a blocked network followed")
	}
}
