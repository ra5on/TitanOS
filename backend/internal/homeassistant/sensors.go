package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Sensors of the NAS for Home Assistant, by MQTT discovery: the NAS
// publishes, Home Assistant needs no access to it. Values come from what
// titand keeps in memory anyway; publishing reads neither /config nor disks.

// Snapshot is what the NAS reports, gathered by the caller.
type BackupValues struct {
	Known        bool
	Name         string
	Running      bool
	Percent      float64
	LastOK, Next *time.Time
	Problem      bool
}
type WorkValues struct {
	Busy   bool
	Copies int
	Agents int
}

type Snapshot struct {
	Backups []BackupValues
	Work    *WorkValues
	Host    string
	Version string
	System  *SystemValues
	Volumes []VolumeValues
	Disks   []DiskValues
	// Containers and VMs: name and whether it runs.
	Containers []RunValues
	VMs        []RunValues
	UPS        *UPSValues
}

type SystemValues struct {
	CPUPercent      float64
	MemoryPercent   float64
	CPUTemperature  *float64
	UptimeSeconds   uint64
	UpdateAvailable bool
}

type VolumeValues struct {
	Name        string
	State       string
	UsedPercent *float64
	FreeBytes   *uint64
}

type DiskValues struct {
	// ID is stable across restarts (serial number), never a device name.
	ID          string
	Name        string
	Temperature *float64
	Healthy     *bool
}

type RunValues struct {
	Name    string
	Running bool
}

type UPSValues struct {
	Status string
	Charge *float64
}

// Source gathers a snapshot.
type Source func(ctx context.Context) Snapshot

// Commander carries out a button or switch pressed in Home Assistant:
// kind is container, vm or system, name what it is about, action what to do.
type Commander func(ctx context.Context, kind, name, action string) error

type entity struct {
	component string // sensor, binary_sensor, switch, button
	key       string
	config    map[string]any
	state     string
	// command, for switches and buttons: kind and name for the Commander.
	kind, name string
}

var keyChars = regexp.MustCompile(`[^a-z0-9_]+`)

func key(parts ...string) string {
	return strings.Trim(keyChars.ReplaceAllString(strings.ToLower(strings.Join(parts, "_")), "_"), "_")
}

func number(v float64) string { return fmt.Sprintf("%.1f", v) }

// entities turns a snapshot into the entities of the enabled groups.
func entities(snap Snapshot, q MQTT, base string) []entity {
	var list []entity
	add := func(component, k, name string, state string, extra map[string]any) {
		cfg := map[string]any{"name": name}
		for n, v := range extra {
			cfg[n] = v
		}
		list = append(list, entity{component: component, key: k, config: cfg, state: state})
	}
	sensor := func(k, name, state, unit, class string) {
		extra := map[string]any{"state_class": "measurement"}
		if unit != "" {
			extra["unit_of_measurement"] = unit
		}
		if class != "" {
			extra["device_class"] = class
		}
		add("sensor", k, name, state, extra)
	}
	onOff := func(v bool) string { return map[bool]string{true: "ON", false: "OFF"}[v] }
	if s := snap.System; s != nil && slices.Contains(q.Groups, "system") {
		sensor("cpu", "CPU", number(s.CPUPercent), "%", "")
		sensor("memory", "RAM", number(s.MemoryPercent), "%", "")
		if s.CPUTemperature != nil {
			sensor("cpu_temperature", "CPU temperature", number(*s.CPUTemperature), "°C", "temperature")
		}
		add("sensor", "uptime", "Uptime", fmt.Sprint(s.UptimeSeconds/60), map[string]any{"unit_of_measurement": "min", "device_class": "duration", "state_class": "total_increasing"})
		add("binary_sensor", "update", "Update available", onOff(s.UpdateAvailable), map[string]any{"device_class": "update"})
		add("sensor", "version", "Version", snap.Version, map[string]any{"entity_category": "diagnostic"})
	}
	if slices.Contains(q.Groups, "volumes") {
		for _, v := range snap.Volumes {
			k := key("volume", v.Name)
			add("sensor", k+"_state", "Volume "+v.Name, v.State, nil)
			if v.UsedPercent != nil {
				sensor(k+"_used", "Volume "+v.Name+" used", number(*v.UsedPercent), "%", "")
			}
			if v.FreeBytes != nil {
				sensor(k+"_free", "Volume "+v.Name+" free", number(float64(*v.FreeBytes)/1e9), "GB", "data_size")
			}
		}
	}
	if slices.Contains(q.Groups, "disks") {
		for _, d := range snap.Disks {
			k := key("disk", d.ID)
			if d.Temperature != nil {
				sensor(k+"_temperature", "Disk "+d.Name+" temperature", number(*d.Temperature), "°C", "temperature")
			}
			if d.Healthy != nil {
				add("binary_sensor", k+"_problem", "Disk "+d.Name+" problem", onOff(!*d.Healthy), map[string]any{"device_class": "problem"})
			}
		}
	}
	if slices.Contains(q.Groups, "backups") {
		for _, b := range snap.Backups {
			k := key("backup", b.Name)
			if b.Known {
				add("binary_sensor", k+"_running", "Backup "+b.Name+" running", onOff(b.Running), map[string]any{"device_class": "running"})
				add("binary_sensor", k+"_problem", "Backup "+b.Name+" problem", onOff(b.Problem), map[string]any{"device_class": "problem"})
			}
			if b.Running {
				sensor(k+"_progress", "Backup "+b.Name+" progress", number(b.Percent), "%", "")
			}
			if b.LastOK != nil {
				add("sensor", k+"_last_success", "Backup "+b.Name+" last success", b.LastOK.UTC().Format(time.RFC3339), map[string]any{"device_class": "timestamp"})
			}
			if b.Next != nil {
				add("sensor", k+"_next", "Backup "+b.Name+" next run", b.Next.UTC().Format(time.RFC3339), map[string]any{"device_class": "timestamp"})
			}
		}
	}
	if w := snap.Work; w != nil && slices.Contains(q.Groups, "work") {
		add("binary_sensor", "work_busy", "NAS busy", onOff(w.Busy), nil)
		sensor("work_copies", "Account file transfers", fmt.Sprint(w.Copies), "", "")
		sensor("work_agents", "Account active assistant tasks", fmt.Sprint(w.Agents), "", "")
	}
	commandTopic := func(kind, name string) string { return base + "/cmd/" + kind + "/" + key(name) }
	for _, group := range []struct {
		name  string
		kind  string
		items []RunValues
		on    string
		off   string
	}{{"containers", "container", snap.Containers, "start", "stop"}, {"vms", "vm", snap.VMs, "start", "shutdown"}} {
		sensors, commands := slices.Contains(q.Groups, group.name), slices.Contains(q.Commands, group.name)
		for _, item := range group.items {
			k := key(group.kind, item.Name)
			label := map[string]string{"container": "Container ", "vm": "VM "}[group.kind] + item.Name
			if commands {
				list = append(list, entity{component: "switch", key: k, kind: group.kind, name: item.Name, state: onOff(item.Running),
					config: map[string]any{"name": label, "command_topic": commandTopic(group.kind, item.Name), "payload_on": group.on, "payload_off": group.off, "state_on": "ON", "state_off": "OFF"}})
				if group.kind == "container" {
					list = append(list, entity{component: "button", key: k + "_restart", kind: group.kind, name: item.Name,
						config: map[string]any{"name": label + " restart", "command_topic": commandTopic(group.kind, item.Name), "payload_press": "restart"}})
				}
			} else if sensors {
				add("binary_sensor", k, label, onOff(item.Running), map[string]any{"device_class": "running"})
			}
		}
	}
	if u := snap.UPS; u != nil && slices.Contains(q.Groups, "ups") {
		add("sensor", "ups_status", "UPS status", u.Status, nil)
		if u.Charge != nil {
			sensor("ups_charge", "UPS charge", number(*u.Charge), "%", "battery")
		}
	}
	if slices.Contains(q.Commands, "backups") {
		for _, b := range snap.Backups {
			list = append(list, entity{component: "button", key: key("backup", b.Name) + "_run", kind: "backup", name: b.Name, config: map[string]any{"name": "Run backup " + b.Name, "command_topic": commandTopic("backup", b.Name), "payload_press": "run"}})
		}
	}
	if slices.Contains(q.Commands, "updates") {
		list = append(list, entity{component: "button", key: "check_updates", kind: "system", name: "updates",
			config: map[string]any{"name": "Check for updates", "command_topic": base + "/cmd/system/updates", "payload_press": "check"}})
	}
	if slices.Contains(q.Commands, "power") {
		for _, a := range []string{"reboot", "poweroff"} {
			list = append(list, entity{component: "button", key: a, kind: "system", name: "power",
				config: map[string]any{"name": map[string]string{"reboot": "Restart NAS", "poweroff": "Shut down NAS"}[a], "command_topic": base + "/cmd/system/power", "payload_press": a, "device_class": map[string]string{"reboot": "restart", "poweroff": ""}[a]}})
		}
	}
	return list
}

// mqttInterval is how often states are published.
var mqttInterval = 30 * time.Second

// RunMQTT publishes sensors while MQTT is switched on, until ctx ends.
func (m *Manager) RunMQTT(ctx context.Context, source Source, commands Commander) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		changed := m.Changed()
		m.mu.Lock()
		q := m.settings.MQTT
		account := m.settings.Account
		m.mu.Unlock()
		if !q.Enabled || q.Host == "" {
			m.setMQTTStatus(false, "")
			select {
			case <-ctx.Done():
			case <-changed:
			}
			continue
		}
		if len(q.Commands) > 0 && account == "" {
			q.Commands = nil // no account to act for: no buttons
		}
		started := time.Now()
		err := m.mqttSession(ctx, q.ID, q, source, commands, changed)
		if ctx.Err() != nil {
			return
		}
		if err == nil { // settings changed
			backoff = 5 * time.Second
			continue
		}
		m.setMQTTStatus(false, err.Error())
		slog.Warn("MQTT connection to Home Assistant lost", "err", err, "retry", backoff)
		if time.Since(started) > time.Minute {
			backoff = 5 * time.Second
		}
		select {
		case <-ctx.Done():
		case <-changed:
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Minute)
	}
}

func (m *Manager) setMQTTStatus(connected bool, lastError string) {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.mqttConnected, m.watch.mqttError = connected, lastError
}

// MQTTStatus says whether sensors are being published, and the last error.
func (m *Manager) MQTTStatus() (bool, string) {
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	return m.watch.mqttConnected, m.watch.mqttError
}

// mqttSession publishes until the settings change (nil) or the connection fails.
func (m *Manager) mqttSession(ctx context.Context, id string, q MQTT, source Source, commands Commander, changed <-chan struct{}) error {
	base := "titanos/" + id
	availability := base + "/status"
	client, err := dialMQTT(ctx, MQTTOptions{Host: q.Host, Port: q.Port, Username: q.Username, Password: q.Password, TLS: q.TLS,
		ClientID: "titanos-" + id, WillTopic: availability, WillPayload: "offline"})
	if err != nil {
		return err
	}
	defer client.Close()
	if len(q.Commands) > 0 {
		if err := client.Subscribe(base + "/cmd/#"); err != nil {
			return err
		}
	}
	m.setMQTTStatus(true, "")
	// published remembers the config sent per topic: unchanged configs are
	// not sent again, vanished entities get an empty one and disappear.
	published := map[string]string{}
	targets := map[string][2]string{} // command topic → kind, name
	device := func(snap Snapshot) map[string]any {
		return map[string]any{"identifiers": []string{"titanos_" + id}, "name": "TitanOS " + snap.Host, "manufacturer": "TitanOS", "sw_version": snap.Version}
	}
	publish := func() error {
		snapCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		snap := source(snapCtx)
		cancel()
		seen := map[string]bool{}
		for _, e := range entities(snap, q, base) {
			topic := q.Prefix + "/" + e.component + "/titanos_" + id + "/" + e.key + "/config"
			seen[topic] = true
			cfg := e.config
			cfg["unique_id"] = "titanos_" + id + "_" + e.key
			cfg["object_id"] = "titanos_" + e.key
			cfg["availability_topic"] = availability
			cfg["device"] = device(snap)
			if e.component != "button" {
				cfg["state_topic"] = base + "/state/" + e.key
			}
			if t, ok := cfg["command_topic"].(string); ok {
				targets[t] = [2]string{e.kind, e.name}
			}
			data, _ := json.Marshal(cfg)
			if published[topic] != string(data) {
				if err := client.Publish(topic, data, true); err != nil {
					return err
				}
				published[topic] = string(data)
			}
			if e.component != "button" {
				if err := client.Publish(base+"/state/"+e.key, []byte(e.state), true); err != nil {
					return err
				}
			}
		}
		for topic := range published {
			if !seen[topic] {
				if err := client.Publish(topic, nil, true); err != nil {
					return err
				}
				delete(published, topic)
			}
		}
		return client.Publish(availability, []byte("online"), true)
	}
	if err := publish(); err != nil {
		return err
	}
	tick := time.NewTicker(mqttInterval)
	defer tick.Stop()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			client.Publish(availability, []byte("offline"), true)
			return nil
		case <-changed:
			// Entities of groups switched off go away; the new session sends
			// what is still wanted.
			for topic := range published {
				client.Publish(topic, nil, true)
			}
			return nil
		case err := <-client.errs:
			return err
		case <-ping.C:
			if err := client.Ping(); err != nil {
				return err
			}
		case <-tick.C:
			if err := publish(); err != nil {
				return err
			}
		case msg, ok := <-client.incoming:
			if !ok {
				return <-client.errs
			}
			target, known := targets[msg.Topic]
			action := strings.TrimSpace(string(msg.Payload))
			if !known || commands == nil || !allowedCommand(q, target[0], target[1], action) {
				slog.Warn("command from Home Assistant ignored", "topic", msg.Topic)
				continue
			}
			go func() {
				cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
				defer cancel()
				if err := commands(cmdCtx, target[0], target[1], action); err != nil {
					slog.Warn("command from Home Assistant failed", "kind", target[0], "name", target[1], "action", action, "err", err)
				}
			}()
			// Publish soon so that switches show the new state.
			tick.Reset(3 * time.Second)
		}
	}
}

// allowedCommand checks a command against the groups switched on; only the
// actions TitanOS offers count.
func allowedCommand(q MQTT, kind, name, action string) bool {
	switch kind {
	case "backup":
		return slices.Contains(q.Commands, "backups") && action == "run"
	case "container":
		return slices.Contains(q.Commands, "containers") && slices.Contains([]string{"start", "stop", "restart"}, action)
	case "vm":
		return slices.Contains(q.Commands, "vms") && slices.Contains([]string{"start", "shutdown"}, action)
	case "system":
		return name == "updates" && action == "check" && slices.Contains(q.Commands, "updates") ||
			name == "power" && (action == "reboot" || action == "poweroff") && slices.Contains(q.Commands, "power")
	}
	return false
}
