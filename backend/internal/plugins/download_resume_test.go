package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type rangeServer struct {
	mu     sync.Mutex
	ranges []string
}

func downloadFixture(t *testing.T, handler func(*rangeServer, http.ResponseWriter, *http.Request)) (*Manager, *rangeServer, []byte, Manifest, string) {
	t.Helper()
	m, _ := newTestManager(t)
	data := bytes.Repeat([]byte("0123456789abcdef"), 64<<10) // 1 MiB
	sum := sha256.Sum256(data)
	s := &rangeServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.ranges = append(s.ranges, r.Header.Get("Range"))
		s.mu.Unlock()
		handler(s, w, r)
	}))
	t.Cleanup(server.Close)
	m.client = server.Client()
	return m, s, data, Manifest{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}, server.URL + "/sdk"
}

func serveRange(data []byte) func(*rangeServer, http.ResponseWriter, *http.Request) {
	return func(_ *rangeServer, w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "sdk", time.Time{}, bytes.NewReader(data))
	}
}

func TestDownloadResumesPartialWithRange(t *testing.T) {
	var data []byte
	m, s, data, manifest, url := downloadFixture(t, func(s *rangeServer, w http.ResponseWriter, r *http.Request) {
		serveRange(data)(s, w, r)
	})
	dest := filepath.Join(t.TempDir(), "sdk.part")
	os.WriteFile(dest, data[:300000], 0600)
	if err := m.downloadImage(context.Background(), url, dest, manifest); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Fatal("resumed file differs")
	}
	if len(s.ranges) != 1 || s.ranges[0] != "bytes=300000-" {
		t.Fatalf("expected one range request, got %q", s.ranges)
	}
	// A server that ignores Range still yields the verified image.
	os.WriteFile(dest, data[:1000], 0600)
	ignoring := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer ignoring.Close()
	m.client = ignoring.Client()
	if err := m.downloadImage(context.Background(), ignoring.URL, dest, manifest); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Fatal("full fallback differs")
	}
}

func TestDownloadResumedHashMismatchRestarts(t *testing.T) {
	var data []byte
	m, s, data, manifest, url := downloadFixture(t, func(s *rangeServer, w http.ResponseWriter, r *http.Request) {
		serveRange(data)(s, w, r)
	})
	dest := filepath.Join(t.TempDir(), "sdk.part")
	os.WriteFile(dest, bytes.Repeat([]byte("x"), 4096), 0600) // corrupt prefix
	if err := m.downloadImage(context.Background(), url, dest, manifest); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Fatal("restart did not produce the verified image")
	}
	if len(s.ranges) != 2 || s.ranges[0] != "bytes=4096-" || s.ranges[1] != "" {
		t.Fatalf("expected range then full request, got %q", s.ranges)
	}
	// A complete but wrong file from a fresh start is rejected and removed.
	manifest.SHA256 = hex.EncodeToString(make([]byte, 32))
	os.Remove(dest)
	if err := m.downloadImage(context.Background(), url, dest, manifest); !errors.Is(err, ErrInvalid) {
		t.Fatal("hash mismatch accepted", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("mismatching image kept")
	}
}

func TestDownloadStallTimeoutKeepsPartForResume(t *testing.T) {
	old := downloadStall
	downloadStall = 150 * time.Millisecond
	t.Cleanup(func() { downloadStall = old })
	var data []byte
	stall := true
	m, s, data, manifest, url := downloadFixture(t, func(s *rangeServer, w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		stalling := stall
		s.mu.Unlock()
		if !stalling {
			serveRange(data)(s, w, r)
			return
		}
		w.Header().Set("Content-Length", "1048576")
		w.Write(data[:200000])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	dest := filepath.Join(t.TempDir(), "sdk.part")
	start := time.Now()
	err := m.downloadImage(context.Background(), url, dest, manifest)
	if !errors.Is(err, ErrDownload) || time.Since(start) > 5*time.Second {
		t.Fatal("stall not detected", err)
	}
	if st, err := os.Stat(dest); err != nil || st.Size() != 200000 {
		t.Fatal("partial download discarded", err)
	}
	s.mu.Lock()
	stall = false
	s.mu.Unlock()
	if err := m.downloadImage(context.Background(), url, dest, manifest); err != nil {
		t.Fatal(err)
	}
	if s.ranges[len(s.ranges)-1] != "bytes=200000-" {
		t.Fatalf("did not resume: %q", s.ranges)
	}
}

func TestDownloadSlowButSteadyHasNoTotalDeadline(t *testing.T) {
	old := downloadStall
	downloadStall = 200 * time.Millisecond
	t.Cleanup(func() { downloadStall = old })
	var data []byte
	m, _, data, manifest, url := downloadFixture(t, func(_ *rangeServer, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		chunk := len(data) / 8
		for i := 0; i < len(data); i += chunk {
			w.Write(data[i : i+chunk])
			w.(http.Flusher).Flush()
			time.Sleep(100 * time.Millisecond) // total 800 ms > stall timeout
		}
	})
	dest := filepath.Join(t.TempDir(), "sdk.part")
	if err := m.downloadImage(context.Background(), url, dest, manifest); err != nil {
		t.Fatal(err)
	}
}
