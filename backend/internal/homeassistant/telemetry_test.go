package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestTelemetryUnknownSelectionAndIdentity(t *testing.T) {
	zero := "0.00"
	devices := []DeviceValues{{Group: "gpu", ID: "pci:a-b", Name: "Card", Metrics: []MetricValue{{Key: "load", Value: &zero}, {Key: "temperature", Class: "temperature"}}}, {Group: "gpu", ID: "pci:a_b", Name: "Other", Metrics: []MetricValue{{Key: "load", Value: &zero}}}}
	q := MQTT{Groups: []string{"gpu"}}
	all := telemetryEntities(devices, q)
	if len(all) != 3 || all[0].key == all[2].key || !*all[0].available || *all[1].available || all[1].state != "None" {
		t.Fatalf("unknown/identity: %+v", all)
	}
	q.Devices = map[string][]string{"gpu": {devices[0].ID}}
	if len(telemetryEntities(devices, q)) != 2 {
		t.Fatal("selection ignored")
	}
	q.Devices["gpu"] = []string{}
	if len(telemetryEntities(devices, q)) != 0 {
		t.Fatal("empty selection became all")
	}
	if len(entities(Snapshot{UPS: &UPSValues{}}, MQTT{Groups: []string{"ups"}, Devices: map[string][]string{"ups": {}}}, "base")) != 0 {
		t.Fatal("legacy UPS ignored device filter")
	}
	// Older settings have no device restrictions. Unknown legacy values retain
	// discovery with unavailable state instead of claiming that a disk is healthy.
	legacy := entities(Snapshot{Disks: []DiskValues{{ID: "serial", Name: "disk"}}, Volumes: []VolumeValues{{Name: "data"}}, Backups: []BackupValues{{Name: "archive"}}, UPS: &UPSValues{}}, MQTT{Groups: []string{"disks", "volumes", "backups", "ups"}}, "base")
	for _, e := range legacy {
		if strings.HasSuffix(e.key, "_problem") || strings.HasSuffix(e.key, "_temperature") || strings.HasSuffix(e.key, "_used") || e.key == "ups_charge" {
			if e.available == nil || *e.available || e.state != "None" {
				t.Fatalf("false healthy: %+v", e)
			}
		}
	}
}
func TestDeviceSettingsDoNotAliasCaller(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	input := map[string][]string{"gpu": {"0000:01:00.0"}}
	if err := m.SetSettings(Settings{MQTT: MQTT{Devices: input}}); err != nil {
		t.Fatal(err)
	}
	input["gpu"][0] = "changed"
	input["gpu"] = []string{}
	settings, _ := m.Settings()
	if len(settings.MQTT.Devices["gpu"]) != 1 || settings.MQTT.Devices["gpu"][0] != "0000:01:00.0" {
		t.Fatal("caller mutated live settings")
	}
	settings.MQTT.Devices["gpu"][0] = "changed"
	again, _ := m.Settings()
	if again.MQTT.Devices["gpu"][0] != "0000:01:00.0" {
		t.Fatal("response aliases live settings")
	}
	if err := m.SetSettings(Settings{MQTT: MQTT{Devices: map[string][]string{"shell": {"x"}}}}); err == nil {
		t.Fatal("unknown group accepted")
	}
}
func TestMQTTUnknownRemovedAndBirth(t *testing.T) {
	b := startBroker(t)
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	if err := m.SetSettings(Settings{MQTT: MQTT{Enabled: true, Host: "127.0.0.1", Port: b.port(), ID: "telemetry", Groups: []string{"gpu"}}}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	value := "31.00"
	snapshot := Snapshot{Telemetry: []DeviceValues{{Group: "gpu", ID: "card", Name: "GPU", Metrics: []MetricValue{{Key: "temperature", Value: &value, Class: "temperature", Unit: "°C"}}}}}
	source := func(context.Context) Snapshot {
		mu.Lock()
		defer mu.Unlock()
		result := snapshot
		result.Telemetry = append([]DeviceValues{}, snapshot.Telemetry...)
		for i := range result.Telemetry {
			result.Telemetry[i].Metrics = append([]MetricValue{}, result.Telemetry[i].Metrics...)
		}
		return result
	}
	old := mqttInterval
	mqttInterval = 40 * time.Millisecond
	defer func() { mqttInterval = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.RunMQTT(ctx, source, nil)
	key := telemetryEntities(snapshot.Telemetry, MQTT{Groups: []string{"gpu"}})[0].key
	topic := "homeassistant/sensor/titanos_telemetry/" + key + "/config"
	avail := "titanos/telemetry/available/" + key
	eventually(t, "initial metric", func() bool {
		return b.get(func(b *fakeBroker) bool { return b.retained[topic] != "" && b.retained[avail] == "online" })
	})
	b.get(func(b *fakeBroker) bool {
		var cfg map[string]any
		json.Unmarshal([]byte(b.retained[topic]), &cfg)
		if cfg["expire_after"] != float64(95) || cfg["availability_mode"] != "all" {
			t.Errorf("availability %+v", cfg)
		}
		if b.retained["titanos/telemetry/state/"+key] != "" {
			t.Error("stale state retained")
		}
		return true
	})
	mu.Lock()
	snapshot.Telemetry[0].Metrics[0].Value = nil
	mu.Unlock()
	eventually(t, "missing value", func() bool {
		return b.get(func(b *fakeBroker) bool {
			return b.retained[avail] == "offline" && b.states["titanos/telemetry/state/"+key] == "None" && b.retained[topic] != ""
		})
	})
	mu.Lock()
	snapshot.Telemetry = nil
	snapshot.UnavailableGroups = []string{"gpu"}
	mu.Unlock()
	// Birth forces a complete fresh announcement. Retained discovery from a prior
	// connection stays unavailable during a source error, then is retired on removal.
	b.send("homeassistant/status", "online")
	oldcfg := `{"unique_id":"titanos_telemetry_` + key + `","name":"GPU temperature"}`
	b.send(topic, oldcfg)
	eventually(t, "unavailable discovery preserved", func() bool {
		return b.get(func(b *fakeBroker) bool { return b.retained[topic] != "" && b.retained[avail] == "offline" })
	})
	mu.Lock()
	snapshot.UnavailableGroups = nil
	mu.Unlock()
	eventually(t, "removed device", func() bool {
		return b.get(func(b *fakeBroker) bool { return b.retained[topic] == "" && b.retained[avail] == "" })
	})
	// A burst larger than the command queue must not lose retained cleanup.
	for i := range 128 {
		name := fmt.Sprintf("gpu_old_%d", i)
		b.send("homeassistant/sensor/titanos_telemetry/"+name+"/config", `{"unique_id":"titanos_telemetry_`+name+`"}`)
	}
	eventually(t, "retained discovery burst", func() bool {
		return b.get(func(b *fakeBroker) bool {
			for i := range 128 {
				v, seen := b.retained[fmt.Sprintf("homeassistant/sensor/titanos_telemetry/gpu_old_%d/config", i)]
				if !seen || v != "" {
					return false
				}
			}
			return true
		})
	})
	stale := "homeassistant/sensor/titanos_telemetry/gpu_old_load/config"
	b.send(stale, `{"unique_id":"titanos_telemetry_gpu_old_load"}`)
	eventually(t, "stale retained discovery cleared", func() bool {
		return b.get(func(b *fakeBroker) bool { _, seen := b.retained[stale]; return seen && b.retained[stale] == "" })
	})
}

func TestNormalizedNamesCannotAliasCommands(t *testing.T) {
	snap := Snapshot{Containers: []RunValues{{Name: "photos-web", Running: true}, {Name: "photos_web", Running: false}}}
	list := entities(snap, MQTT{Groups: []string{"containers"}, Commands: []string{"containers"}}, "base")
	ids, topics := map[string]bool{}, map[string]bool{}
	for _, e := range list {
		if ids[e.key] {
			t.Fatal("discovery key collision")
		}
		ids[e.key] = true
		if e.component == "switch" {
			topic := e.config["command_topic"].(string)
			if topics[topic] {
				t.Fatal("command can target two containers")
			}
			topics[topic] = true
		}
	}
	single := entities(Snapshot{Containers: snap.Containers[:1]}, MQTT{Groups: []string{"containers"}}, "base")
	if single[0].key != "container_photos_web" {
		t.Fatal("non-colliding historical ID changed")
	}
}

func TestRetainedDiscoveryRespectsExplicitSelectionDuringOutage(t *testing.T) {
	q := MQTT{Devices: map[string][]string{"gpu": {"selected"}, "containers": {"photos"}, "ups": {}}}
	devices := []DeviceValues{{Group: "gpu", ID: "selected", Metrics: []MetricValue{{Key: "temperature"}}}, {Group: "gpu", ID: "other", Metrics: []MetricValue{{Key: "temperature"}}}}
	list := telemetryEntities(devices, MQTT{Groups: []string{"gpu"}})
	if !retainedSelected(q, "gpu", list[0].key) || retainedSelected(q, "gpu", list[1].key) {
		t.Fatal("filtered device was removed or unrelated device kept")
	}
	if !retainedSelected(q, "containers", "container_photos") || retainedSelected(q, "containers", "container_photos_database") || retainedSelected(q, "ups", "ups_charge") {
		t.Fatal("legacy selection has a prefix or empty-selection leak")
	}
	if !retainedSelected(MQTT{}, "gpu", list[1].key) {
		t.Fatal("old settings stopped selecting all devices")
	}
}

func TestMQTTFilteredReconnectKeepsSelectedUnavailableDiscovery(t *testing.T) {
	b := startBroker(t)
	m := NewManager(config.Open(t.TempDir()))
	m.Load()
	q := MQTT{Enabled: true, Host: "127.0.0.1", Port: b.port(), ID: "filtered", Groups: []string{"gpu"}, Devices: map[string][]string{"gpu": {"selected"}}}
	if err := m.SetSettings(Settings{MQTT: q}); err != nil {
		t.Fatal(err)
	}
	mu := sync.Mutex{}
	unavailable := true
	source := func(context.Context) Snapshot {
		mu.Lock()
		defer mu.Unlock()
		if unavailable {
			return Snapshot{UnavailableGroups: []string{"gpu"}}
		}
		return Snapshot{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := mqttInterval
	mqttInterval = 40 * time.Millisecond
	defer func() { mqttInterval = old }()
	go m.RunMQTT(ctx, source, nil)
	eventually(t, "discovery subscription", func() bool { return b.get(func(b *fakeBroker) bool { return len(b.subs) == 2 }) })
	devices := []DeviceValues{{Group: "gpu", ID: "selected", Metrics: []MetricValue{{Key: "load"}}}, {Group: "gpu", ID: "other", Metrics: []MetricValue{{Key: "load"}}}}
	list := telemetryEntities(devices, MQTT{Groups: []string{"gpu"}})
	for _, e := range list {
		b.send("homeassistant/sensor/titanos_filtered/"+e.key+"/config", `{"unique_id":"titanos_filtered_`+e.key+`","name":"GPU load"}`)
	}
	selected := "homeassistant/sensor/titanos_filtered/" + list[0].key + "/config"
	other := "homeassistant/sensor/titanos_filtered/" + list[1].key + "/config"
	eventually(t, "filtered outage reconciliation", func() bool {
		return b.get(func(b *fakeBroker) bool {
			_, seen := b.retained[other]
			return b.retained[selected] != "" && b.retained["titanos/filtered/available/"+list[0].key] == "offline" && seen && b.retained[other] == ""
		})
	})
	mu.Lock()
	unavailable = false
	mu.Unlock()
	eventually(t, "confirmed removal after recovery", func() bool { return b.get(func(b *fakeBroker) bool { return b.retained[selected] == "" }) })
}

func TestNewMQTTGroupsRemainEditableByPreviousApp(t *testing.T) {
	store := config.Open(t.TempDir())
	m := NewManager(store)
	m.Load()
	if err := m.SetSettings(Settings{MQTT: MQTT{Groups: []string{"system", "gpu", "network"}, Devices: map[string][]string{"gpu": {"card"}}}}); err != nil {
		t.Fatal(err)
	}
	// Decode precisely the fields known to the previous app and apply its enum
	// validation. New telemetry must not make its settings form unsaveable.
	var old struct {
		MQTT struct {
			Groups []string `json:"groups"`
		} `json:"mqtt"`
	}
	if err := store.ReadJSON(settingsFile, &old); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(old.MQTT.Groups, []string{"system"}) {
		t.Fatalf("old app sees unknown group: %v", old.MQTT.Groups)
	}
	again := NewManager(store)
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	settings, _ := again.Settings()
	if !slices.Equal(settings.MQTT.Groups, []string{"gpu", "network", "system"}) || settings.MQTT.ExtraGroups != nil || !slices.Equal(settings.MQTT.Devices["gpu"], []string{"card"}) {
		t.Fatalf("new settings did not round trip: %+v", settings.MQTT)
	}
	settings.MQTT.Groups = []string{"system"}
	if err := again.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	third := NewManager(store)
	third.Load()
	final, _ := third.Settings()
	if !slices.Equal(final.MQTT.Groups, []string{"system"}) {
		t.Fatal("deselected groups reappeared from storage")
	}
}
