package httpfetch

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func TestRedirectRules(t *testing.T) {
	check := Redirects(3)
	req := func(raw string) *http.Request {
		u, _ := url.Parse(raw)
		return &http.Request{URL: u}
	}
	if err := check(req("http://a/b"), []*http.Request{req("https://a/")}); !errors.Is(err, ErrRedirect) {
		t.Errorf("https to http: %v", err)
	}
	if err := check(req("https://a/b"), []*http.Request{req("https://a/"), req("http://b/")}); err != nil {
		t.Errorf("upgrade: %v", err)
	}
	if err := check(req("ftp://a/b"), []*http.Request{req("http://a/")}); !errors.Is(err, ErrRedirect) {
		t.Errorf("ftp: %v", err)
	}
	if err := check(req("https://u:p@a/b"), []*http.Request{req("https://a/")}); !errors.Is(err, ErrRedirect) {
		t.Errorf("user info: %v", err)
	}
	if err := check(req("https://a/b"), []*http.Request{req("https://a/"), req("https://a/"), req("https://a/")}); !errors.Is(err, ErrRedirect) {
		t.Errorf("too many: %v", err)
	}
}

func TestValidURLAndFilename(t *testing.T) {
	for raw, want := range map[string]bool{"https://example.org/a.iso": true, "http://10.0.0.2:8080/x": true, "ftp://a/b": false, "/relative": false, "https://u@a/": false, "javascript:alert(1)": false} {
		if _, ok := ValidURL(raw); ok != want {
			t.Errorf("ValidURL(%q) = %v", raw, ok)
		}
	}
	u, _ := url.Parse("https://example.org/dl/debian%2012.iso?x=1")
	if got := URLFilename(u); got != "debian 12.iso" {
		t.Errorf("URLFilename = %q", got)
	}
	r := &Response{Response: &http.Response{Header: http.Header{"Content-Disposition": {`attachment; filename="..\evil/name.iso"`}}, Request: &http.Request{URL: u}}}
	if got := r.Filename(); got != "name.iso" {
		t.Errorf("Filename = %q", got)
	}
}

func TestOpenResumesAndBlocksAddresses(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 1000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"x"`)
		http.ServeContent(w, r, "f", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()
	client := NewClient(Policy{})
	res, err := Open(t.Context(), client, server.URL, nil, Resume{Offset: 100, ETag: `"x"`})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.Offset != 100 || res.Total != int64(len(data)) || !bytes.Equal(got, data[100:]) {
		t.Fatalf("offset %d total %d", res.Offset, res.Total)
	}
	// A weak or missing validator starts over.
	res, err = Open(t.Context(), client, server.URL, nil, Resume{Offset: 100, ETag: `W/"x"`})
	if err != nil || res.Offset != 0 {
		t.Fatalf("weak validator: %+v %v", res, err)
	}
	res.Body.Close()
	// A different ETag: the server sends the whole file.
	res, err = Open(t.Context(), client, server.URL, nil, Resume{Offset: 100, ETag: `"old"`})
	if err != nil || res.Offset != 0 {
		t.Fatalf("changed source: %+v %v", res, err)
	}
	res.Body.Close()
	blocked := NewClient(Policy{Allow: func(netip.AddrPort) bool { return false }})
	if _, err = Open(t.Context(), blocked, server.URL, nil, Resume{}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("blocked address: %v", err)
	}
}
