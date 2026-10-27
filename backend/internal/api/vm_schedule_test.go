package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/vm"
)

// The VM list carries the stored schedule and the next scheduled times; a
// schedule change goes through PUT /vms/{name} like the rest of the form.
func TestVMScheduleInTheList(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	stored := vm.VM{Name: "office", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Volume: "data",
		PowerSchedule: vm.PowerSchedule{Enabled: true, StopTimeout: 300, Rules: []vm.PowerRule{
			{Action: "start", Days: []int{0, 1, 2, 3, 4, 5, 6}, Time: "07:00"},
			{Action: "stop", Days: []int{0, 1, 2, 3, 4, 5, 6}, Time: "22:00"}}}}
	if err := ts.store.WriteJSON("vms/office.json", stored); err != nil {
		t.Fatal(err)
	}
	if err := ts.server.opts.VMs.Load(); err != nil {
		t.Fatal(err)
	}
	res, body := ts.request("GET", "/api/v1/vms", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var list struct {
		VMs []struct {
			PowerSchedule vm.PowerSchedule `json:"powerSchedule"`
			Schedule      *vm.PowerView    `json:"schedule"`
		} `json:"vms"`
	}
	if err := json.Unmarshal(body, &list); err != nil || len(list.VMs) != 1 {
		t.Fatalf("%v %s", err, body)
	}
	got := list.VMs[0]
	if !got.PowerSchedule.Enabled || len(got.PowerSchedule.Rules) != 2 || got.Schedule == nil || got.Schedule.NextStart == nil || got.Schedule.NextStop == nil {
		t.Fatalf("schedule: %s", body)
	}
	if d := time.Until(*got.Schedule.NextStart); d <= 0 || d > 24*time.Hour {
		t.Fatalf("next start %v", got.Schedule.NextStart)
	}
	// Development mode changes nothing, also not the schedule.
	change := map[string]any{"memoryMiB": 1024, "vcpus": 1, "network": "user", "powerSchedule": map[string]any{"enabled": false, "rules": []any{}}}
	if res, body := ts.request("PUT", "/api/v1/vms/office", change); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Fatalf("change in development mode: %d %s", res.StatusCode, body)
	}
}
