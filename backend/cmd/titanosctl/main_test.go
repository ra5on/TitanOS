package main

import (
	"bytes"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIAuthenticatesAndNeverFollowsRedirects(t *testing.T) {
	t.Setenv("TITANOS_TOKEN", "personal-test-token")
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer personal-test-token" {
			t.Error("missing token")
		}
		if r.URL.Path == "/api/v1/system" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"hostname":"nas"}`)
			return
		}
		w.Header().Set("Location", target.URL)
		w.WriteHeader(302)
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := run([]string{"--url", server.URL, "status"}, &out); err != nil || !strings.Contains(out.String(), "nas") {
		t.Fatal(err, out.String())
	}
	if err := run([]string{"--url", server.URL, "request", "GET", "/redirect"}, &out); err == nil || called {
		t.Fatal("redirect followed or accepted", err, called)
	}
}
func TestCLITrustsExplicitNASCertificateAndStreamsBinary(t *testing.T) {
	t.Setenv("TITANOS_TOKEN", "test")
	content := bytes.Repeat([]byte("raw image data\x00"), 1024)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(content)
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := run([]string{"--url", server.URL, "request", "GET", "/files/download"}, &out); err == nil {
		t.Fatal("untrusted TLS accepted")
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	path := filepath.Join(t.TempDir(), "nas.crt")
	os.WriteFile(path, cert, 0600)
	if err := run([]string{"--url", server.URL, "--ca-file", path, "request", "GET", "/files/download"}, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), content) {
		t.Fatal("binary data changed")
	}
}
