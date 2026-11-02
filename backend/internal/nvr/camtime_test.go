package nvr

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSetTimeServerOverONVIF(t *testing.T) {
	c := &fakeCamera{user: "admin", password: "pw", paths: map[string]bool{}}
	startFakeCamera(t, c)
	p := loopbackProber(c)
	res, err := p.SetTimeServer(context.Background(), "127.0.0.1", p.ONVIFPorts[0], "", "admin", "pw", "192.168.1.2")
	if err != nil || res.Method != "onvif" {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(c.ntp, "192.168.1.2") || !strings.Contains(c.timeType, "NTP") {
		t.Fatalf("camera got: %q / %q", c.ntp, c.timeType)
	}
}

// digestServer is a camera web interface with digest login.
func digestServer(t *testing.T, user, password string, handle func(r *http.Request, body string) int) (*httptest.Server, *atomic.Int32) {
	var rejected atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="cam", nonce="n1", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		params := parseAuthParams(auth[len("Digest"):])
		uri := r.URL.RequestURI()
		ha1 := md5hex(user + ":cam:" + password)
		want := md5hex(ha1 + ":n1:" + params["nc"] + ":" + params["cnonce"] + ":auth:" + md5hex(r.Method+":"+uri))
		if params["response"] != want {
			rejected.Add(1)
			w.WriteHeader(401)
			return
		}
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		w.WriteHeader(handle(r, string(buf[:n])))
		fmt.Fprint(w, "<statusString>OK</statusString>")
	}))
	t.Cleanup(srv.Close)
	return srv, &rejected
}

func httpPortProber(srv *httptest.Server) *Prober {
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	var n int
	fmt.Sscan(port, &n)
	return &Prober{Allow: loopbackOnly, HTTPPort: n}
}

func TestSetTimeServerHikvision(t *testing.T) {
	var puts []string
	srv, _ := digestServer(t, "admin", "pw", func(r *http.Request, body string) int {
		puts = append(puts, r.Method+" "+r.URL.Path+" "+body)
		return 200
	})
	res, err := httpPortProber(srv).SetTimeServer(context.Background(), "127.0.0.1", 0, "hikvision", "admin", "pw", "10.0.0.2")
	if err != nil || res.Method != "hikvision" {
		t.Fatalf("%+v %v", res, err)
	}
	if len(puts) != 2 || !strings.Contains(puts[0], "ntpServers/1") || !strings.Contains(puts[0], "10.0.0.2") || !strings.Contains(puts[1], "<timeMode>NTP</timeMode>") {
		t.Fatalf("requests: %v", puts)
	}
}

func TestSetTimeServerDahuaAndWrongPassword(t *testing.T) {
	var query string
	srv, rejected := digestServer(t, "admin", "pw", func(r *http.Request, body string) int {
		query = r.URL.RawQuery
		return 200
	})
	p := httpPortProber(srv)
	if res, err := p.SetTimeServer(context.Background(), "127.0.0.1", 0, "dahua", "admin", "pw", "10.0.0.2"); err != nil || res.Method != "dahua" || !strings.Contains(query, "NTP.Address=10.0.0.2") {
		t.Fatalf("%+v %v %s", res, err, query)
	}
	res, err := p.SetTimeServer(context.Background(), "127.0.0.1", 0, "", "admin", "wrong", "10.0.0.2")
	if err == nil || res.Method != "" {
		t.Fatalf("wrong password accepted: %+v", res)
	}
	if rejected.Load() > DefaultAuthAttempts {
		t.Fatalf("tried %d times with a wrong password", rejected.Load())
	}
}

func TestSetTimeServerRefusesOutsideHomeNetwork(t *testing.T) {
	if _, err := (&Prober{}).SetTimeServer(context.Background(), "8.8.8.8", 80, "", "a", "b", "10.0.0.2"); err == nil {
		t.Fatal("outside address accepted")
	}
}
