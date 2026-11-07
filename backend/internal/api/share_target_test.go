package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// PLAN 70: a share that reaches the NAS (no service worker) is not read and
// goes to the page; other POSTs to the UI stay refused.
func TestShareTargetWithoutWorker(t *testing.T) {
	h := uiHandler(fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})
	req := httptest.NewRequest("POST", "/share-target", strings.NewReader("--x\r\n"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/share?missing=1" {
		t.Fatalf("share target: %d %q", w.Code, w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/share", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("other POST: %d", w.Code)
	}
}
