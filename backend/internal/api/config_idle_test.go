package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

var routeParameter = regexp.MustCompile(`\{[^}]*\}`)

// The stick is written only when someone changes something (PLAN 79-12):
// reading the state of the NAS, as the web UI does all the time, and
// idling must never write to /config. Every GET route is asked twice; the
// first round may create what is made on first need (keys), the second
// round and a pause after it must leave the stick alone.
func TestReadingAndIdlingNeverWriteTheStick(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	sweep := func() {
		for _, pattern := range ts.server.patterns {
			method, path, ok := strings.Cut(pattern, " ")
			if !ok || method != http.MethodGet {
				continue
			}
			path = routeParameter.ReplaceAllString(path, "x")
			// Streams never end by themselves: a short look is enough.
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.url+path, nil)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			if res, err := ts.client.Do(req); err == nil {
				res.Body.Close()
			}
			cancel()
		}
	}
	start := ts.store.Writes()
	sweep()
	ts.server.sessions.Flush()
	before := ts.store.Writes()
	t.Logf("first round wrote %d times", before-start)
	sweep()
	time.Sleep(1500 * time.Millisecond)
	ts.server.sessions.Flush()
	if after := ts.store.Writes(); after != before {
		t.Fatalf("/config was written %d times while the NAS was only read and idle; the journal line \"configuration on the stick changed\" names the files", after-before)
	}
}
