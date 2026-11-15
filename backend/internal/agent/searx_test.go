package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The own search: results of a SearXNG become titles, addresses and
// snippets; a switched-off JSON format and other answers are errors, and
// nothing that is not a web address comes through.
func TestSearxSearch(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.String()
		switch r.URL.Query().Get("q") {
		case "aus":
			w.WriteHeader(http.StatusForbidden)
		case "html":
			w.Write([]byte("<html>"))
		default:
			w.Write([]byte(`{"results":[{"title":" TitanOS ","url":"https://example.org/a","content":"Ein  NAS\n im RAM"},{"title":"x","url":"javascript:alert(1)","content":""},{"title":"","url":"https://example.org/b"}]}`))
		}
	}))
	defer srv.Close()
	got, err := SearxSearch(context.Background(), srv.URL+"/", "titanos nas")
	if err != nil || len(got) != 1 || got[0].Title != "TitanOS" || got[0].URL != "https://example.org/a" || got[0].Snippet != "Ein NAS im RAM" {
		t.Fatalf("%+v %v", got, err)
	}
	if !strings.HasPrefix(asked, "/search?") || !strings.Contains(asked, "format=json") {
		t.Fatalf("asked %q", asked)
	}
	if _, err := SearxSearch(context.Background(), srv.URL, "aus"); err == nil || !strings.Contains(err.Error(), "JSON format") {
		t.Fatalf("forbidden: %v", err)
	}
	if _, err := SearxSearch(context.Background(), srv.URL, "html"); err == nil {
		t.Fatal("HTML accepted")
	}
	if _, err := SearxSearch(context.Background(), srv.URL, ""); err == nil {
		t.Fatal("empty search accepted")
	}
}

// Against a real SearXNG: TITANOS_SEARX=http://127.0.0.1:8888 go test ./internal/agent -run TestSearxReal -v
func TestSearxReal(t *testing.T) {
	address := os.Getenv("TITANOS_SEARX")
	if address == "" {
		t.Skip("TITANOS_SEARX not set")
	}
	got, err := SearxSearch(context.Background(), address, "searxng metasearch")
	if err != nil || len(got) == 0 {
		t.Fatalf("%d results, %v", len(got), err)
	}
	t.Logf("%d results, first: %s – %s", len(got), got[0].Title, got[0].URL)
}
