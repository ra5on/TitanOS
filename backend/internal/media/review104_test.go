package media

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// N-102-38: a download whose server stops sending without closing the
// connection ends after downloadStall with a message that says so,
// instead of staying "downloading" until someone cancels it.
func TestStalledDownloadEnds(t *testing.T) {
	stall := downloadStall
	downloadStall = 200 * time.Millisecond
	t.Cleanup(func() { downloadStall = stall })
	m := testManager(t)
	image := testISO(2 << 20)
	quit := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		w.Write(image[:64<<10])
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-quit:
		}
	}))
	defer server.Close()
	defer close(quit)
	j, err := m.Create(Request{Volume: "data", Name: "stall.iso", URL: server.URL + "/stall.iso"})
	if err != nil {
		t.Fatal(err)
	}
	j = waitJob(t, m, j.ID, "failed", "done", "review")
	if j.Phase != "failed" || !strings.Contains(j.Error, "no data") {
		t.Fatalf("stalled download: %+v", j)
	}
}
