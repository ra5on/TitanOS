package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/hwcheck"
)

func TestHardwareReportRoutes(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, _ := ts.request("GET", "/api/v1/hardware/report", nil); res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("without a checker: %d", res.StatusCode)
	}
	if missing := ts.server.cpuMissing("services:\n  db:\n    image: mongo:7\n"); len(missing) != 0 {
		t.Errorf("without a checker: %v", missing)
	}

	root := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(root, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}
	// A CPU without AVX, one disk known as SMR.
	write("proc/cpuinfo", "processor : 0\nvendor_id : GenuineIntel\nmodel name : Old\nflags : fpu lm cmov cx8 fxsr mmx syscall sse sse2 vmx\n")
	write("proc/sys/kernel/osrelease", "6.12.113+deb13-amd64")
	write("sys/devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda/device/model", "WDC WD40EFAX-68J")
	write("sys/devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda/queue/rotational", "1")
	os.MkdirAll(filepath.Join(root, "sys/block"), 0o755)
	os.Symlink(filepath.Join(root, "sys/devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda"), filepath.Join(root, "sys/block/sda"))
	ts.server.opts.HWCheck = &hwcheck.Checker{Sys: filepath.Join(root, "sys"), Proc: filepath.Join(root, "proc"), Dev: filepath.Join(root, "dev")}

	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, path := range []string{"/api/v1/hardware/report", "/api/v1/hardware/disks"} {
		if res, _ := ts.do(guest, "GET", path, nil); res.StatusCode != http.StatusForbidden {
			t.Errorf("guest %s: %d", path, res.StatusCode)
		}
	}

	res, body := ts.request("GET", "/api/v1/hardware/report", nil)
	var report hwcheck.Report
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &report) != nil || report.CPU.Model != "Old" || len(report.Disks) != 1 || report.Disks[0].SMR != "known_model" {
		t.Fatalf("report: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/hardware/report?format=text", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Disposition"), ".txt") || !strings.Contains(string(body), "TitanOS hardware report") {
		t.Errorf("text: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/hardware/report?format=json", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Disposition"), ".json") || !strings.Contains(string(body), `"version": "1.2.3"`) {
		t.Errorf("json: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/hardware/disks", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"smr":"known_model"`) {
		t.Errorf("disks: %d %s", res.StatusCode, body)
	}
	if missing := ts.server.cpuMissing("services:\n  db:\n    image: mongo:7\n"); !slices.Equal(missing, []string{"avx"}) {
		t.Errorf("cpu missing: %v", missing)
	}
}
