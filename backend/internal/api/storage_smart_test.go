package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// smartSystem answers lsblk with two disks and smartctl from the storage
// test data; everything else succeeds silently.
type smartSystem struct {
	smartctl []string
}

func (f *smartSystem) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	switch name {
	case "lsblk":
		return `{"blockdevices":[
 {"name":"/dev/sda","kname":"/dev/sda","type":"disk","size":4000787030016,"rota":true,"tran":"sata","model":"WDC WD40EFZX","serial":"WD-1"},
 {"name":"/dev/sdb","kname":"/dev/sdb","type":"disk","size":4000787030016,"rota":true,"tran":"sata","model":"WDC WD40EFZX","serial":"WD-2"}]}`, nil
	case "smartctl":
		f.smartctl = append(f.smartctl, strings.Join(args, " "))
		file := "smart-sata-hdd.json"
		if args[len(args)-1] == "/dev/sdb" {
			file = "smart-standby.json"
		}
		data, err := os.ReadFile("../storage/testdata/" + file)
		return string(data), err
	}
	return "", nil
}

func TestSmartAPI(t *testing.T) {
	ts := newTestServer(t)
	system := &smartSystem{}
	ts.disks.UseRunner(system)
	routes := [][2]string{{"GET", "/api/v1/storage/smart"}, {"POST", "/api/v1/storage/smart/refresh"}, {"GET", "/api/v1/storage/smart/sda"}}
	for _, route := range routes {
		if res, _ := ts.request(route[0], route[1], nil); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%v without login: %d", route, res.StatusCode)
		}
	}
	ts.setup()

	// The list reads no SMART itself: before a refresh nothing is known.
	res, body := ts.request("GET", "/api/v1/storage/smart", nil)
	var list struct {
		Disks []storage.SmartReport `json:"disks"`
	}
	if json.Unmarshal(body, &list) != nil || res.StatusCode != http.StatusOK || len(list.Disks) != 2 || list.Disks[0].State != storage.SmartUnknown || len(system.smartctl) != 0 {
		t.Fatalf("list before refresh: %d %s %v", res.StatusCode, body, system.smartctl)
	}

	res, body = ts.request("POST", "/api/v1/storage/smart/refresh", nil)
	if json.Unmarshal(body, &list) != nil || res.StatusCode != http.StatusOK || len(list.Disks) != 2 {
		t.Fatalf("refresh: %d %s", res.StatusCode, body)
	}
	if len(system.smartctl) != 2 || !slices.ContainsFunc(system.smartctl, func(c string) bool { return strings.Contains(c, "--nocheck=standby") }) {
		t.Fatalf("smartctl calls: %v", system.smartctl)
	}
	for _, call := range system.smartctl {
		if !strings.Contains(call, "--nocheck=standby") {
			t.Fatalf("SMART query may wake a disk: %s", call)
		}
	}
	sda, sdb := list.Disks[0], list.Disks[1]
	if sda.Disk != "sda" || sda.State != storage.SmartWarning || sda.Values == nil || sda.Values.Attributes != nil || sdb.Disk != "sdb" || !sdb.Sleeping || sdb.State != storage.SmartUnknown {
		t.Fatalf("reports: %+v %+v", sda, sdb)
	}
	// A second refresh right away uses the cache.
	ts.request("POST", "/api/v1/storage/smart/refresh", nil)
	if len(system.smartctl) != 2 {
		t.Fatalf("refresh within 30 seconds ran smartctl again: %v", system.smartctl)
	}

	res, body = ts.request("GET", "/api/v1/storage/smart/sda", nil)
	var detail storage.SmartReport
	if json.Unmarshal(body, &detail) != nil || res.StatusCode != http.StatusOK || detail.Values == nil || len(detail.Values.Attributes) != 12 || !slices.Contains(detail.Warnings, storage.SmartWarnReallocated) {
		t.Fatalf("detail: %d %s", res.StatusCode, body)
	}
	for _, disk := range []string{"sdq", "sda1", "%2E%2E"} {
		if res, body := ts.request("GET", "/api/v1/storage/smart/"+disk, nil); res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "disk_not_found") {
			t.Fatalf("unknown disk %q: %d %s", disk, res.StatusCode, body)
		}
	}

	if _, err := ts.users.Create("guest", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "guest", Password: testPassword})
	for _, route := range routes {
		if res, _ := ts.do(guest, route[0], route[1], nil); res.StatusCode != http.StatusForbidden {
			t.Fatalf("plain user %v: %d", route, res.StatusCode)
		}
	}
	if _, err := ts.users.Create("helper", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	admin := ts.newClient()
	ts.do(admin, "POST", "/api/v1/login", credentials{Name: "helper", Password: testPassword})
	if res, _ := ts.do(admin, "GET", "/api/v1/storage/smart/sda", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("administrator: %d", res.StatusCode)
	}
}
