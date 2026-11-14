package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// PLAN 91: on the plain HTTP port only Assist is answered, everything else
// still goes to HTTPS.
func TestAssistOverHTTP(t *testing.T) {
	site := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := assistOverHTTP(site, redirectToHTTPS(":443"))
	for path, want := range map[string]int{
		"/agent-ollama/api/tags": http.StatusTeapot,
		"/agent-ollama/api/chat": http.StatusTeapot,
		"/agent-ollama":          http.StatusPermanentRedirect,
		"/agent-ollamax/api":     http.StatusPermanentRedirect,
		"/api/v1/agent/status":   http.StatusPermanentRedirect,
		"/":                      http.StatusPermanentRedirect,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "http://nas.local"+path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
}
