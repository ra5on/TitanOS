package api

import (
	"fmt"
	"math"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// Only use values already sampled by the NAS. No SMART, repository, history or
// device query is allowed in these conversions.
func haMetric(key, name string, value any, unit, class, stateClass string) homeassistant.MetricValue {
	m := homeassistant.MetricValue{Key: key, Name: name, Unit: unit, Class: class, StateClass: stateClass}
	m.Binary = key == "on_battery" || key == "shutdown" || class == "problem" || class == "running" || class == "connectivity" || class == "update"
	var text string
	switch v := value.(type) {
	case nil:
		return m
	case bool:
		m.Binary = true
		if v {
			text = "ON"
		} else {
			text = "OFF"
		}
	case *time.Time:
		if v == nil || v.IsZero() {
			return m
		}
		text = v.UTC().Format(time.RFC3339)
	case *float64:
		if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
			return m
		}
		text = fmt.Sprintf("%.2f", *v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return m
		}
		text = fmt.Sprintf("%.2f", v)
	default:
		text = fmt.Sprint(v)
	}
	m.Value = &text
	return m
}
func (s *Server) haAdditional(snap *homeassistant.Snapshot) {
	info := s.opts.Monitor.Info()
	add := func(group, id, name string, metrics ...homeassistant.MetricValue) {
		snap.Telemetry = append(snap.Telemetry, homeassistant.DeviceValues{Group: group, ID: id, Name: name, Metrics: metrics})
	}
	metric := haMetric
	add("system", "nas", "NAS",
		metric("stick", "Boot stick present", info.Stick.Present, "", "", ""))
	if info.Image != nil {
		add("system", "image", "System",
			metric("slot", "Slot", info.Image.Slot, "", "", ""),
			metric("overlay", "Overlay used", info.Image.OverlayUsedBytes, "B", "data_size", "measurement"),
			metric("overlay_total", "Overlay total", info.Image.OverlayTotalBytes, "B", "data_size", "measurement"))
	}
	if s.opts.Update != nil {
		p := s.opts.Update.CurrentProgress()
		add("system", "update", "Update",
			metric("phase", "Phase", p.Phase, "", "", ""),
			metric("progress", "Progress", p.Percent, "%", "", "measurement"))
	}
	for _, n := range info.Networks {
		add("network", n.MAC+"/"+n.Interface, n.Interface,
			metric("state", "Link state", n.State, "", "", ""),
			metric("receive", "Receive", n.RxBytesPerSecond, "B/s", "data_rate", "measurement"),
			metric("send", "Send", n.TxBytesPerSecond, "B/s", "data_rate", "measurement"),
			metric("speed", "Link speed", n.SpeedMbps, "Mbit/s", "data_rate", "measurement"))
	}
	if s.opts.Accelerators != nil {
		for _, d := range s.opts.Accelerators.Snapshot() {
			group := "igpu"
			if d.Kind == "npu" {
				group = "npu"
			} else if d.Vendor == "NVIDIA" {
				group = "gpu"
			}
			metrics := []homeassistant.MetricValue{metric("state", "State", d.State, "", "", ""),
				metric("driver", "Driver", d.Driver, "", "", ""),
				metric("usage", "Usage", d.UsagePercent, "%", "", "measurement"),
				metric("temperature", "Temperature", d.TemperatureC, "°C", "temperature", "measurement"),
				metric("power", "Power", d.PowerWatts, "W", "power", "measurement"),
				metric("memory_used", "Memory used", d.MemoryUsedBytes, "B", "data_size", "measurement"),
				metric("memory_total", "Memory total", d.MemoryTotalBytes, "B", "data_size", "measurement"),
				metric("clock", "Clock", d.FrequencyMHz, "MHz", "frequency", "measurement")}
			for _, e := range d.Engines {
				metrics = append(metrics,
					metric("engine_"+e.Name, e.Name, e.UsagePercent, "%", "", "measurement"))
			}
			add(group, d.Address, d.Name, metrics...)
		}
	}
	if s.opts.Maintenance != nil {
		status := s.opts.Maintenance.Status()
		if status.Error != "" || status.Settings.Enabled && !status.Available {
			snap.UnavailableGroups = append(snap.UnavailableGroups, "maintenance")
		}
		for _, v := range status.Jobs {
			var next *time.Time = v.Next
			add("maintenance", v.Job.ID, v.Job.Name,
				metric("running", "Running", v.State.Running, "", "running", ""),
				metric("blocked", "Blocked", v.State.Blocked, "", "problem", ""),
				metric("problem", "Problem", v.State.Error != "", "", "problem", ""),
				metric("wait", "Wait reason", v.Wait, "", "", ""),
				metric("phase", "Phase", v.State.Phase, "", "", ""),
				metric("last_success", "Last success", v.State.LastOK, "", "timestamp", ""),
				metric("next", "Next run", next, "", "timestamp", ""))
		}
	}
	if s.opts.Monitoring != nil {
		for _, v := range s.opts.Monitoring.CachedTrends(time.Now()) {
			add("capacity", v.Volume, v.Volume,
				metric("days_90", "Days to 90 percent", v.DaysTo90, "d", "duration", "measurement"),
				metric("days_full", "Days to full", v.DaysToFull, "d", "duration", "measurement"))
		}
	}
	if s.opts.Backup != nil {
		mounted := s.opts.Storage.Mounted()
		for _, v := range s.opts.Backup.CachedViews() {
			var check, probe *time.Time
			var phase any
			if v.StateKnown {
				check = v.State.LastCheck
				phase = v.State.Phase
			}
			if v.StateKnown && v.State.Probe != nil {
				probe = &v.State.Probe.At
			}
			var problem, overdue any
			if v.StateKnown {
				overdue = s.opts.Backup.Overdue(v)
				problem = v.State.CheckError != "" || (v.State.Probe != nil && v.State.Probe.Error != "")
			}
			metrics := []homeassistant.MetricValue{metric("last_check", "Last archive check", check, "", "timestamp", ""),
				metric("last_probe", "Last restore probe", probe, "", "timestamp", ""),
				metric("verification_problem", "Verification problem", problem, "", "problem", ""),
				metric("state_known", "State known", v.StateKnown, "", "", ""),
				metric("phase", "Phase", phase, "", "", ""),
				metric("manual", "Manual only", !v.Job.Enabled, "", "", ""),
				metric("overdue", "Overdue", overdue, "", "problem", "")}
			if v.Job.Destination.Type == "local" {
				available := false
				for _, name := range mounted {
					available = available || name == v.Job.Destination.Volume
				}
				metrics = append(metrics,
					metric("target", "Target available", available, "", "connectivity", ""))
			}
			add("backups", v.Job.ID, v.Job.ID, metrics...)
		}
	}
	if s.opts.ApplicationBackups != nil {
		op, active := s.opts.ApplicationBackups.Status()
		add("work", "application_recovery", "Application backup",
			metric("recovery", "Recovery required", op != nil && !active, "", "problem", ""))
	}
}
