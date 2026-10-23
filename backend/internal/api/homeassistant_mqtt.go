package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// HASnapshot gathers the values Home Assistant gets by MQTT, from what titand
// keeps in memory. Disks are not asked anything: sleeping ones stay asleep.
func (s *Server) HASnapshot(ctx context.Context) homeassistant.Snapshot {
	info := s.opts.Monitor.Info()
	snap := homeassistant.Snapshot{Host: info.Hostname, Version: s.opts.Version}
	sys := &homeassistant.SystemValues{CPUPercent: info.CPU.UsagePercent, UptimeSeconds: uint64(max(info.UptimeSeconds, 0))}
	if info.Memory.TotalBytes > 0 {
		sys.MemoryPercent = float64(info.Memory.UsedBytes) / float64(info.Memory.TotalBytes) * 100
	}
	for _, t := range info.Temperatures {
		if t.Chip == "coretemp" && t.Label == "Package id 0" && t.Celsius != nil || t.Chip == "k10temp" && t.Label == "Tctl" && t.Celsius != nil {
			sys.CPUTemperature = t.Celsius
			break
		}
	}
	if s.opts.Update != nil {
		sys.UpdateAvailable = s.opts.Update.Status().Available != nil
	}
	snap.System = sys
	if status, err := s.opts.Storage.Status(ctx); err == nil {
		for _, v := range status.Volumes {
			values := homeassistant.VolumeValues{Name: v.Name, State: v.State}
			if v.Mounted && v.SizeBytes > 0 {
				used := float64(v.UsedBytes) / float64(v.SizeBytes) * 100
				free := v.SizeBytes - min(v.UsedBytes, v.SizeBytes)
				values.UsedPercent, values.FreeBytes = &used, &free
			}
			snap.Volumes = append(snap.Volumes, values)
		}
		for _, d := range status.Disks {
			if d.Serial == "" {
				continue // without a serial number there is no name that lasts
			}
			values := homeassistant.DiskValues{ID: d.Serial, Name: strings.TrimSpace(d.Model + " " + d.Serial)}
			if d.Smart != nil {
				healthy := d.Smart.Healthy
				values.Healthy = &healthy
				if d.Smart.TemperatureC > 0 {
					temp := float64(d.Smart.TemperatureC)
					values.Temperature = &temp
				}
			}
			snap.Disks = append(snap.Disks, values)
		}
	}
	if s.opts.Docker != nil {
		for _, c := range s.opts.Docker.Status(ctx).Containers {
			snap.Containers = append(snap.Containers, homeassistant.RunValues{Name: c.Name, Running: c.State == "running"})
		}
	}
	if s.opts.VMs != nil {
		for _, v := range s.opts.VMs.List(s.opts.Storage.Mounted()) {
			snap.VMs = append(snap.VMs, homeassistant.RunValues{Name: v.Name, Running: v.State == "running"})
		}
	}
	if s.opts.UPS != nil {
		if u := s.opts.UPS.Status(); u.Mode != "off" {
			state := "online"
			switch {
			case !u.Connected:
				state = "unreachable"
			case u.LowBattery:
				state = "low_battery"
			case u.OnBattery:
				state = "on_battery"
			}
			values := &homeassistant.UPSValues{Status: state}
			if u.Charge >= 0 {
				charge := float64(u.Charge)
				values.Charge = &charge
			}
			snap.UPS = values
		}
	}
	if s.opts.Backup != nil {
		for _, b := range s.opts.Backup.CachedViews() {
			snap.Backups = append(snap.Backups, homeassistant.BackupValues{Known: b.StateKnown, Name: b.Job.ID, Running: b.State.Running, Percent: b.State.Percent, LastOK: b.State.LastOK, Next: b.Next, Problem: b.State.LastError != "" || b.State.Warning != ""})
		}
	}
	settings, _ := s.opts.HomeAssistant.Settings()
	if user, ok := s.opts.Users.Get(settings.Account); ok && user.Role.CanAdminister() {
		work := &homeassistant.WorkValues{Busy: s.opts.Update != nil && s.opts.Update.Working != nil && s.opts.Update.Working() || s.storageBusy(ctx)}
		if s.opts.Files != nil {
			for _, job := range s.opts.Files.Jobs(user.Name) {
				if job.State == files.JobRunning {
					work.Copies++
				}
			}
		}
		s.agentMu.Lock()
		for key := range s.agentJobs {
			if strings.HasPrefix(key, user.Name+"/") {
				work.Agents++
			}
		}
		s.agentMu.Unlock()
		snap.Work = work
	}
	return snap
}

// HACommand carries out a button or switch pressed in Home Assistant, as
// the account chosen in the settings and through the same routes as the web
// interface, so that its rights and checks apply.
func (s *Server) HACommand(ctx context.Context, kind, name, action string) error {
	settings, _ := s.opts.HomeAssistant.Settings()
	user, ok := s.opts.Users.Get(settings.Account)
	if !ok || !user.Role.CanAdminister() {
		return fmt.Errorf("no owner or administrator account chosen for Home Assistant")
	}
	var method, route string
	var body any
	switch kind {
	case "backup":
		if action != "run" || s.opts.Backup == nil {
			return fmt.Errorf("unknown backup command")
		}
		method, route = http.MethodPost, "/api/v1/backups/"+url.PathEscape(name)+"/run"
	case "container":
		id := ""
		for _, c := range s.opts.Docker.Status(ctx).Containers {
			if c.Name == name {
				id = c.ID
			}
		}
		if id == "" {
			return fmt.Errorf("container %s not found", name)
		}
		method, route = http.MethodPost, "/api/v1/docker/containers/"+url.PathEscape(id)+"/"+action
	case "vm":
		method, route = http.MethodPost, "/api/v1/vms/"+url.PathEscape(name)+"/"+action
	case "system":
		switch name {
		case "updates":
			method, route = http.MethodPost, "/api/v1/update/check"
		case "power":
			// Never in the middle of copies, backups, disk tests or a rebuild.
			if s.opts.Update != nil && s.opts.Update.Working != nil && s.opts.Update.Working() || s.storageBusy(ctx) {
				s.opts.Notify.Notify("warning", "ha_power_refused", action)
				return fmt.Errorf("the NAS is busy; %s refused", action)
			}
			method, route, body = http.MethodPost, "/api/v1/system/power", map[string]string{"action": action}
		}
	}
	if route == "" {
		return fmt.Errorf("unknown command")
	}
	code, answer := s.internalAs(ctx, user, method, route, body)
	if len(answer) > 300 {
		answer = answer[:300]
	}
	slog.Info("command from Home Assistant", "kind", kind, "name", name, "action", action, "as", user.Name, "status", code)
	if code < 200 || code > 299 {
		return fmt.Errorf("%d: %s", code, answer)
	}
	return nil
}

// storageBusy says whether an array is being rebuilt or checked.
func (s *Server) storageBusy(ctx context.Context) bool {
	status, err := s.opts.Storage.Status(ctx)
	if err != nil {
		return true
	}
	for _, v := range status.Volumes {
		if v.Sync != nil {
			return true
		}
	}
	return false
}

// internalAs calls a route of the web interface in-process as an account.
func (s *Server) internalAs(ctx context.Context, user auth.User, method, route string, body any) (int, string) {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, agentIdentity{}, user.Name), method, route, bytes.NewReader(data))
	if err != nil {
		return http.StatusInternalServerError, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}
