package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackupRejectsForcedKillAndOwnsContainerLifecycle(t *testing.T) {
	for _, code := range []int{0, 137} {
		m, _, _ := newTestManager(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				w.WriteHeader(204)
				return
			}
			fmt.Fprintf(w, `{"State":{"Running":false,"OOMKilled":false,"ExitCode":%d}}`, code)
		}))
		m.api.http = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		}}}
		lease, err := m.BackupLease(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if m.hardwareMu.TryLock() {
			m.hardwareMu.Unlock()
			t.Fatal("backup did not own lifecycle")
		}
		err = lease.Stop(context.Background(), "container")
		lease.Close()
		server.Close()
		if (err == nil) != (code == 0) {
			t.Fatal(code, err)
		}
	}
}
