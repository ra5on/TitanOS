package viewer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppKeyPinReadsTheAppsOwnKey(t *testing.T) {
	app := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer app.Close()
	sum := sha256.Sum256(app.Certificate().RawSubjectPublicKeyInfo)
	want := base64.StdEncoding.EncodeToString(sum[:])
	if got := appKeyPin(context.Background(), strings.TrimPrefix(app.URL, "https://")); got != want {
		t.Fatalf("pin %q, want %q", got, want)
	}
	// Plain HTTP or nothing listening: no pin.
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer plain.Close()
	if got := appKeyPin(context.Background(), strings.TrimPrefix(plain.URL, "http://")); got != "" {
		t.Errorf("plain HTTP gave %q", got)
	}
	plain.Close()
	if got := appKeyPin(context.Background(), strings.TrimPrefix(plain.URL, "http://")); got != "" {
		t.Errorf("closed port gave %q", got)
	}
}
