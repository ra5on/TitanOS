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
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
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
	if m := s.opts.NVR; m != nil && m.Store.Settings().Enabled {
		armed := m.Store.Settings().Arming.Active(time.Now())
		snap.NVRArmed = &armed
		status := m.Rec.Status()
		root := m.Root()
		for _, c := range m.Store.Cameras() {
			snap.Cameras = append(snap.Cameras, homeassistant.RunValues{Name: c.Name, Running: status[c.ID].Recording})
			var used int64
			var last *string
			if root != "" {
				segs := nvr.Segments(root, c.ID)
				for _, sg := range segs {
					used += sg.Size
				}
				if n := len(segs); n > 0 {
					v := segs[n-1].Start.UTC().Format(time.RFC3339)
					last = &v
				}
			}
			motion := "OFF"
			if m.Watch.Status()[c.ID].Motion {
				motion = "ON"
			}
			gb := fmt.Sprintf("%.2f", float64(used)/1e9)
			snap.Telemetry = append(snap.Telemetry, homeassistant.DeviceValues{Group: "cameras", ID: c.Name, Name: "Camera " + c.Name, Metrics: []homeassistant.MetricValue{
				{Key: "storage", Name: "recordings", Unit: "GB", Class: "data_size", StateClass: "measurement", Value: &gb},
				{Key: "motion", Name: "motion", Class: "motion", Binary: true, Value: &motion},
				{Key: "last_recording", Name: "last recording", Class: "timestamp", Value: last},
			}})
		}
	}
	add := func(group, id, name string, metrics ...homeassistant.MetricValue) {
		snap.Telemetry = append(snap.Telemetry, homeassistant.DeviceValues{Group: group, ID: id, Name: name, Metrics: metrics})
	}
	metric := haMetric
	if status, err := s.opts.Storage.Status(ctx); err == nil {
		for _, v := range status.Volumes {
			values := homeassistant.VolumeValues{Name: v.Name, State: v.State}
			if v.Mounted && v.SizeBytes > 0 {
				used := float64(v.UsedBytes) / float64(v.SizeBytes) * 100
				free := v.SizeBytes - min(v.UsedBytes, v.SizeBytes)
				values.UsedPercent, values.FreeBytes = &used, &free
			}
			snap.Volumes = append(snap.Volumes, values)
			var progress, speed, remaining any
			if v.Sync != nil {
				progress = v.Sync.Percent
				speed = v.Sync.SpeedBytesPerSecond
				if v.Sync.RemainingSeconds >= 0 {
					remaining = v.Sync.RemainingSeconds
				}
			}
			members := 0
			for _, member := range v.Members {
				if member.State == "active" || member.State == "rebuilding" {
					members++
				}
			}
			add("volumes", v.Name, v.Name, metric("maintenance", "Maintenance active", v.Sync != nil, "", "running", ""), metric("progress", "Maintenance progress", progress, "%", "", "measurement"), metric("speed", "Maintenance speed", speed, "B/s", "data_rate", "measurement"), metric("remaining", "Remaining", remaining, "s", "duration", "measurement"), metric("missing", "Missing members", max(0, v.RaidDisks-members), "", "", "measurement"))
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
			var hours, errors, wear any
			if d.Smart != nil {
				hours = d.Smart.PowerOnHours
				errors = d.Smart.Errors
				if d.Smart.WearPercent >= 0 {
					wear = d.Smart.WearPercent
				}
			}
			add("disks", d.Serial, values.Name, metric("hours", "Power on hours", hours, "h", "duration", "measurement"), metric("errors", "Reported errors", errors, "", "", "measurement"), metric("wear", "Life used", wear, "%", "", "measurement"), metric("testing", "Test running", d.Testing, "", "running", ""))
		}
	} else {
		snap.UnavailableGroups = append(snap.UnavailableGroups, "volumes", "disks")
	}
	if s.opts.Docker != nil {
		status := s.opts.Docker.Status(ctx)
		if status.Settings.Enabled && !status.Running {
			snap.UnavailableGroups = append(snap.UnavailableGroups, "containers", "stacks")
		}
		type stack struct{ total, running int }
		stacks := map[string]stack{}
		for _, c := range status.Containers {
			snap.Containers = append(snap.Containers, homeassistant.RunValues{Name: c.Name, Running: c.State == "running"})
			var update any
			if c.UpdateState == "success" || c.UpdateState == "available" || c.UpdateState == "current" {
				update = c.UpdateAvailable
			}
			add("containers", c.Name, c.Name, metric("state", "State", c.State, "", "", ""), metric("cpu", "CPU", c.CPUPercent, "%", "", "measurement"), metric("memory", "RAM", c.MemoryBytes, "B", "data_size", "measurement"), metric("update", "Update available", update, "", "update", ""))
			if c.App != "" {
				v := stacks[c.App]
				v.total++
				if c.State == "running" {
					v.running++
				}
				stacks[c.App] = v
			}
		}
		for name, v := range stacks {
			add("stacks", name, name, metric("running", "Running services", v.running, "", "", "measurement"), metric("stopped", "Stopped services", v.total-v.running, "", "", "measurement"), metric("total", "Services", v.total, "", "", "measurement"))
		}
	}
	if s.opts.VMs != nil {
		for _, v := range s.opts.VMs.List(s.opts.Storage.Mounted()) {
			snap.VMs = append(snap.VMs, homeassistant.RunValues{Name: v.Name, Running: v.State == "running", Unavailable: !v.Available})
			var cpu, memory any
			if v.Available {
				cpu = v.CPUPercent
				memory = v.MemoryBytes
			}
			add("vms", v.Name, v.Name, metric("state", "State", v.State, "", "", ""), metric("cpu", "CPU", cpu, "%", "", "measurement"), metric("memory", "Host RAM", memory, "B", "data_size", "measurement"))
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
			if u.Connected && u.Charge >= 0 {
				charge := float64(u.Charge)
				values.Charge = &charge
			}
			snap.UPS = values
			var runtime, load, battery, low, shutdown any
			if u.Connected {
				battery, low, shutdown = u.OnBattery, u.LowBattery, u.ShuttingDown
			}
			if u.Connected && u.Runtime >= 0 {
				runtime = u.Runtime
			}
			if u.Connected && u.Load >= 0 {
				load = u.Load
			}
			add("ups", "ups", "UPS", metric("runtime", "Runtime", runtime, "s", "duration", "measurement"), metric("load", "Load", load, "%", "", "measurement"), metric("on_battery", "On battery", battery, "", "", ""), metric("low_battery", "Low battery", low, "", "problem", ""), metric("shutdown", "Shutdown initiated", shutdown, "", "", ""))
		}
	}
	if s.opts.Backup != nil {
		for _, b := range s.opts.Backup.CachedViews() {
			snap.Backups = append(snap.Backups, homeassistant.BackupValues{Known: b.StateKnown, Name: b.Job.ID, Running: b.State.Running, Percent: b.State.Percent, LastOK: b.State.LastOK, Next: b.Next, Problem: b.State.LastError != "" || b.State.Warning != ""})
		}
	}
	settings, _ := s.opts.HomeAssistant.Settings()
	if user, ok := s.activeUser(settings.Account); ok && user.Role.CanAdminister() {
		work := &homeassistant.WorkValues{Busy: s.opts.Update != nil && s.opts.Update.Working != nil && s.opts.Update.Working() || s.storageBusy(ctx)}
		if s.opts.Files != nil {
			for _, job := range s.opts.Files.Jobs(user.Name) {
				if job.State == files.JobRunning {
					work.Copies++
				}
			}
		}
		snap.Work = work
	}
	s.haAdditional(&snap)
	return snap
}

// HACommand carries out a button or switch pressed in Home Assistant, as
// the account chosen in the settings and through the same routes as the web
// interface, so that its rights and checks apply.
func (s *Server) HACommand(ctx context.Context, kind, name, action string) error {
	settings, _ := s.opts.HomeAssistant.Settings()
	user, ok := s.activeUser(settings.Account)
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
		status := s.opts.Docker.Status(ctx)
		for _, c := range status.Containers {
			if c.Name == name {
				id = c.ID
			}
		}
		if id == "" {
			return fmt.Errorf("container %s not found", name)
		}
		method, route = http.MethodPost, "/api/v1/docker/containers/"+url.PathEscape(id)+"/"+action
	case "camera":
		return s.haCamera(name, action)
	case "nvr":
		return s.haArm(action)
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
	req, err := http.NewRequestWithContext(context.WithValue(ctx, inProcessIdentity{}, user.Name), method, route, bytes.NewReader(data))
	if err != nil {
		return http.StatusInternalServerError, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// haCamera switches the recording of a camera on or off for Home Assistant.
func (s *Server) haCamera(name, action string) error {
	if s.opts.NVR == nil || action != "start" && action != "stop" {
		return fmt.Errorf("unknown camera command")
	}
	for _, c := range s.opts.NVR.Store.Cameras() {
		if c.Name != name {
			continue
		}
		c.Record = action == "start"
		if _, err := s.opts.NVR.Store.Update(c, nil); err != nil {
			return err
		}
		s.opts.NVR.Reload()
		slog.Info("command from Home Assistant", "kind", "camera", "name", name, "action", action)
		return nil
	}
	return fmt.Errorf("camera %s not found", name)
}

// haArm arms or disarms the NVR from Home Assistant (presence: "nobody home").
// It switches the mode to on or off; a schedule set on the page is replaced.
func (s *Server) haArm(action string) error {
	if s.opts.NVR == nil || action != "on" && action != "off" {
		return fmt.Errorf("unknown NVR command")
	}
	st := s.opts.NVR.Store.Settings()
	st.Arming.Mode = action
	if err := s.opts.NVR.Store.SetSettings(st); err != nil {
		return err
	}
	slog.Info("command from Home Assistant", "kind", "nvr", "name", "armed", "action", action)
	return nil
}
