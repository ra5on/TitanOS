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
	Telemetry         []DeviceValues
	UnavailableGroups []string
	Backups           []BackupValues
	Work              *WorkValues
	Host              string
	Version           string
	System            *SystemValues
	Volumes           []VolumeValues
	Disks             []DiskValues
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
	Name        string
	Running     bool
	Unavailable bool
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
	available  *bool
	group      string
}

var keyChars = regexp.MustCompile(`[^a-z0-9_]+`)

func key(parts ...string) string {
	return strings.Trim(keyChars.ReplaceAllString(strings.ToLower(strings.Join(parts, "_")), "_"), "_")
}

func number(v float64) string { return fmt.Sprintf("%.1f", v) }

// entities turns a snapshot into the entities of the enabled groups.
func entities(snap Snapshot, q MQTT, base string) []entity {
	var list []entity
	identity := legacyIdentity(snap)
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
	unknown := func(known bool) {
		list[len(list)-1].available = &known
		if !known {
			list[len(list)-1].state = "None"
		}
	}
	onOff := func(v bool) string { return map[bool]string{true: "ON", false: "OFF"}[v] }
	if s := snap.System; s != nil && slices.Contains(q.Groups, "system") {
		sensor("cpu", "CPU", number(s.CPUPercent), "%", "")
		sensor("memory", "RAM", number(s.MemoryPercent), "%", "")
		sensor("cpu_temperature", "CPU temperature", "None", "°C", "temperature")
		unknown(s.CPUTemperature != nil)
		if s.CPUTemperature != nil {
			list[len(list)-1].state = number(*s.CPUTemperature)
		}
		add("sensor", "uptime", "Uptime", fmt.Sprint(s.UptimeSeconds/60), map[string]any{"unit_of_measurement": "min", "device_class": "duration", "state_class": "measurement"})
		add("binary_sensor", "update", "Update available", onOff(s.UpdateAvailable), map[string]any{"device_class": "update"})
		add("sensor", "version", "Version", snap.Version, map[string]any{"entity_category": "diagnostic"})
	}
	if slices.Contains(q.Groups, "volumes") {
		for _, v := range snap.Volumes {
			if !SelectedDevice(q, "volumes", v.Name) {
				continue
			}
			k := identity("volume", v.Name)
			add("sensor", k+"_state", "Volume "+v.Name, v.State, nil)
			sensor(k+"_used", "Volume "+v.Name+" used", "None", "%", "")
			unknown(v.UsedPercent != nil)
			if v.UsedPercent != nil {
				list[len(list)-1].state = number(*v.UsedPercent)
			}
			sensor(k+"_free", "Volume "+v.Name+" free", "None", "GB", "data_size")
			unknown(v.FreeBytes != nil)
			if v.FreeBytes != nil {
				list[len(list)-1].state = number(float64(*v.FreeBytes) / 1e9)
			}
		}
	}
	if slices.Contains(q.Groups, "disks") {
		for _, d := range snap.Disks {
			if !SelectedDevice(q, "disks", d.ID) {
				continue
			}
			k := identity("disk", d.ID)
			sensor(k+"_temperature", "Disk "+d.Name+" temperature", "None", "°C", "temperature")
			unknown(d.Temperature != nil)
			if d.Temperature != nil {
				list[len(list)-1].state = number(*d.Temperature)
			}
			add("binary_sensor", k+"_problem", "Disk "+d.Name+" problem", "None", map[string]any{"device_class": "problem"})
			unknown(d.Healthy != nil)
			if d.Healthy != nil {
				list[len(list)-1].state = onOff(!*d.Healthy)
			}
		}
	}
	if slices.Contains(q.Groups, "backups") {
		for _, b := range snap.Backups {
			if !SelectedDevice(q, "backups", b.Name) {
				continue
			}
			k := identity("backup", b.Name)
			add("binary_sensor", k+"_running", "Backup "+b.Name+" running", onOff(b.Running), map[string]any{"device_class": "running"})
			unknown(b.Known)
			add("binary_sensor", k+"_problem", "Backup "+b.Name+" problem", onOff(b.Problem), map[string]any{"device_class": "problem"})
			unknown(b.Known)
			sensor(k+"_progress", "Backup "+b.Name+" progress", number(b.Percent), "%", "")
			unknown(b.Known && b.Running)
			add("sensor", k+"_last_success", "Backup "+b.Name+" last success", "None", map[string]any{"device_class": "timestamp"})
			unknown(b.Known && b.LastOK != nil)
			if b.Known && b.LastOK != nil {
				list[len(list)-1].state = b.LastOK.UTC().Format(time.RFC3339)
			}
			add("sensor", k+"_next", "Backup "+b.Name+" next run", "None", map[string]any{"device_class": "timestamp"})
			unknown(b.Next != nil)
			if b.Next != nil {
				list[len(list)-1].state = b.Next.UTC().Format(time.RFC3339)
			}
		}
	}
	if w := snap.Work; w != nil && slices.Contains(q.Groups, "work") {
		add("binary_sensor", "work_busy", "NAS busy", onOff(w.Busy), nil)
		sensor("work_copies", "Account file transfers", fmt.Sprint(w.Copies), "", "")
		sensor("work_agents", "Account active assistant tasks", fmt.Sprint(w.Agents), "", "")
	}
	commandTopic := func(kind, name string) string {
		return base + "/cmd/" + kind + "/" + strings.TrimPrefix(identity(kind, name), kind+"_")
	}
	for _, group := range []struct {
		name  string
		kind  string
		items []RunValues
		on    string
		off   string
	}{{"containers", "container", snap.Containers, "start", "stop"}, {"vms", "vm", snap.VMs, "start", "shutdown"}} {
		sensors, commands := slices.Contains(q.Groups, group.name), slices.Contains(q.Commands, group.name)
		for _, item := range group.items {
			if !SelectedDevice(q, group.name, item.Name) {
				continue
			}
			k := identity(group.kind, item.Name)
			label := map[string]string{"container": "Container ", "vm": "VM "}[group.kind] + item.Name
			if commands {
				list = append(list, entity{component: "switch", key: k, kind: group.kind, name: item.Name, state: onOff(item.Running),
					config: map[string]any{"name": label, "command_topic": commandTopic(group.kind, item.Name), "payload_on": group.on, "payload_off": group.off, "state_on": "ON", "state_off": "OFF"}})
				unknown(!item.Unavailable)
				if group.kind == "container" {
					list = append(list, entity{component: "button", key: k + "_restart", kind: group.kind, name: item.Name,
						config: map[string]any{"name": label + " restart", "command_topic": commandTopic(group.kind, item.Name), "payload_press": "restart"}})
				}
			} else if sensors {
				add("binary_sensor", k, label, onOff(item.Running), map[string]any{"device_class": "running"})
				unknown(!item.Unavailable)
			}
		}
	}
	if u := snap.UPS; u != nil && slices.Contains(q.Groups, "ups") && SelectedDevice(q, "ups", "ups") {
		add("sensor", "ups_status", "UPS status", u.Status, nil)
		sensor("ups_charge", "UPS charge", "None", "%", "battery")
		unknown(u.Charge != nil)
		if u.Charge != nil {
			list[len(list)-1].state = number(*u.Charge)
		}
	}
	if slices.Contains(q.Commands, "backups") {
		for _, b := range snap.Backups {
			if !SelectedDevice(q, "backups", b.Name) {
				continue
			}
			list = append(list, entity{component: "button", key: identity("backup", b.Name) + "_run", kind: "backup", name: b.Name, config: map[string]any{"name": "Run backup " + b.Name, "command_topic": commandTopic("backup", b.Name), "payload_press": "run"}})
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
	return append(list, telemetryEntities(snap.Telemetry, q)...)
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
	if err := client.Subscribe(q.Prefix + "/status"); err != nil {
		return err
	}
	if len(q.Commands) > 0 {
		if err := client.Subscribe(base + "/cmd/#"); err != nil {
			return err
		}
	}
	m.setMQTTStatus(true, "")
	// published remembers the config sent per topic: unchanged configs are
	// not sent again, vanished entities get an empty one and disappear.
	published := map[string]string{}
	publishedEntities := map[string]entity{}
	targets := map[string][2]string{} // command topic → kind, name
	device := func(snap Snapshot) map[string]any {
		return map[string]any{"identifiers": []string{"titanos_" + id}, "name": "TitanOS " + snap.Host, "manufacturer": "TitanOS", "sw_version": snap.Version}
	}
	var snapUnavailable []string
	forceDiscovery := false
	publish := func() error {
		snapCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		snap := source(snapCtx)
		snapUnavailable = append([]string{}, snap.UnavailableGroups...)
		cancel()
		seen := map[string]bool{}
		targets = map[string][2]string{}
		for _, e := range entities(snap, q, base) {
			if e.group == "" {
				switch {
				case strings.HasPrefix(e.key, "volume_"):
					e.group = "volumes"
				case strings.HasPrefix(e.key, "disk_"):
					e.group = "disks"
				case strings.HasPrefix(e.key, "container_"):
					e.group = "containers"
				case strings.HasPrefix(e.key, "vm_"):
					e.group = "vms"
				case strings.HasPrefix(e.key, "backup_"):
					e.group = "backups"
				}
			}
			if slices.Contains(snap.UnavailableGroups, e.group) {
				known := false
				e.available = &known
				e.state = "None"
			}
			topic := q.Prefix + "/" + e.component + "/titanos_" + id + "/" + e.key + "/config"
			seen[topic] = true
			if e.available == nil && e.group != "" {
				known := true
				e.available = &known
			}
			cfg := e.config
			if cfg["device_class"] == "" {
				delete(cfg, "device_class")
			}
			cfg["unique_id"] = "titanos_" + id + "_" + e.key
			cfg["default_entity_id"] = e.component + ".titanos_" + e.key
			cfg["availability_topic"] = availability
			if e.available != nil {
				delete(cfg, "availability_topic")
				cfg["availability"] = []map[string]string{{"topic": availability}, {"topic": base + "/available/" + e.key}}
				cfg["availability_mode"] = "all"
				payload := "offline"
				if *e.available {
					payload = "online"
				}
				if err := client.Publish(base+"/available/"+e.key, []byte(payload), true); err != nil {
					return err
				}
			}
			if e.component == "sensor" || e.component == "binary_sensor" {
				cfg["expire_after"] = 95
			}
			cfg["device"] = device(snap)
			if e.component != "button" {
				cfg["state_topic"] = base + "/state/" + e.key
			}
			if t, ok := cfg["command_topic"].(string); ok {
				targets[t] = [2]string{e.kind, e.name}
			}
			data, _ := json.Marshal(cfg)
			if forceDiscovery || published[topic] != string(data) {
				client.ignored.Store(topic, true)
				if err := client.Publish(topic, data, true); err != nil {
					return err
				}
				published[topic] = string(data)
				publishedEntities[topic] = e
				if e.component != "button" {
					if err := client.Publish(base+"/state/"+e.key, nil, true); err != nil {
						return err
					}
				}
			}
			if e.component != "button" {
				if err := client.Publish(base+"/state/"+e.key, []byte(e.state), false); err != nil {
					return err
				}
			}
		}
		for topic := range published {
			if !seen[topic] {
				e := publishedEntities[topic]
				if slices.Contains(snap.UnavailableGroups, e.group) {
					if forceDiscovery {
						if err := client.Publish(topic, []byte(published[topic]), true); err != nil {
							return err
						}
					}
					if err := client.Publish(base+"/available/"+e.key, []byte("offline"), true); err != nil {
						return err
					}
					continue
				}
				if err := client.Publish(topic, nil, true); err != nil {
					return err
				}
				if err := client.Publish(base+"/state/"+e.key, nil, true); err != nil {
					return err
				}
				if err := client.Publish(base+"/available/"+e.key, nil, true); err != nil {
					return err
				}
				client.ignored.Delete(topic)
				delete(published, topic)
				delete(publishedEntities, topic)
			}
		}
		forceDiscovery = false
		return client.Publish(availability, []byte("online"), true)
	}
	if err := publish(); err != nil {
		return err
	}
	// Subscribe after publishing our current configs, avoiding a queue full of
	// our own initial announcements. The client filters later live echoes.
	if err := client.Subscribe(q.Prefix + "/+/titanos_" + id + "/+/config"); err != nil {
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
			if strings.HasPrefix(msg.Topic, q.Prefix+"/") && strings.HasSuffix(msg.Topic, "/config") && len(msg.Payload) > 0 && len(msg.Payload) <= 8192 {
				if _, known := published[msg.Topic]; known {
					continue
				}
				parts := strings.Split(strings.TrimPrefix(msg.Topic, q.Prefix+"/"), "/")
				if len(parts) != 4 || parts[1] != "titanos_"+id || parts[3] != "config" {
					continue
				}
				var cfg map[string]any
				if json.Unmarshal(msg.Payload, &cfg) != nil || cfg["unique_id"] != "titanos_"+id+"_"+parts[2] {
					continue
				}
				group := "system"
				for _, candidate := range SensorGroups {
					if strings.HasPrefix(parts[2], key(candidate)+"_") {
						group = candidate
						break
					}
				}
				if strings.HasPrefix(parts[2], "volume_") {
					group = "volumes"
				}
				if strings.HasPrefix(parts[2], "disk_") {
					group = "disks"
				}
				if strings.HasPrefix(parts[2], "container_") {
					group = "containers"
				}
				if strings.HasPrefix(parts[2], "vm_") {
					group = "vms"
				}
				if strings.HasPrefix(parts[2], "backup_") {
					group = "backups"
				}
				if slices.Contains(snapUnavailable, group) && slices.Contains(q.Groups, group) && retainedSelected(q, group, parts[2]) && len(published) < 2048 {
					delete(cfg, "availability_topic")
					cfg["availability"] = []map[string]string{{"topic": availability}, {"topic": base + "/available/" + parts[2]}}
					cfg["availability_mode"] = "all"
					data, _ := json.Marshal(cfg)
					client.ignored.Store(msg.Topic, true)
					published[msg.Topic] = string(data)
					publishedEntities[msg.Topic] = entity{key: parts[2], group: group}
					if err := client.Publish(base+"/available/"+parts[2], []byte("offline"), true); err != nil {
						return err
					}
					if err := client.Publish(msg.Topic, data, true); err != nil {
						return err
					}
				} else {
					if err := client.Publish(msg.Topic, nil, true); err != nil {
						return err
					}
					if err := client.Publish(base+"/state/"+parts[2], nil, true); err != nil {
						return err
					}
					if err := client.Publish(base+"/available/"+parts[2], nil, true); err != nil {
						return err
					}
				}
				continue
			}
			if msg.Topic == q.Prefix+"/status" && string(msg.Payload) == "online" {
				forceDiscovery = true
				if err := publish(); err != nil {
					return err
				}
				continue
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
