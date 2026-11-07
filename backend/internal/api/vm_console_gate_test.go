package api

import (
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/appbackup"
)

// The VM screen follows the rules of the other consoles
// (docs/rules/ACCOUNTS-ACCESS.md): like the serial console it does not
// open while an application backup owns the applications.
func TestVMScreenWaitsForApplicationBackups(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	gate := &appbackup.Manager{}
	ts.server.opts.ApplicationBackups = gate
	gate.Gate.Lock()
	defer gate.Gate.Unlock()
	for _, path := range []string{"/vms/web/serial", "/vms/web/screen"} {
		if res, body := ts.request("GET", "/api/v1"+path, nil); res.StatusCode != http.StatusLocked || errorCode(t, body) != "application_backup_running" {
			t.Fatalf("GET %s: %d %s", path, res.StatusCode, body)
		}
	}
}
