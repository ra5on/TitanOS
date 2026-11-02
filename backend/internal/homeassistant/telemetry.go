package homeassistant

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
)

// DeviceValues contains only already sampled values. Nil values are unavailable,
// never a substitute zero. Device IDs are stable identities rather than labels.
type DeviceValues struct {
	Group   string        `json:"group"`
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Metrics []MetricValue `json:"-"`
}
type MetricValue struct {
	Key, Name, Unit, Class, StateClass string
	Value                              *string
	Binary                             bool
}

func SelectedDevice(q MQTT, group, id string) bool {
	ids, restricted := q.Devices[group]
	return !restricted || slices.Contains(ids, id)
}
func telemetryEntities(devices []DeviceValues, q MQTT) []entity {
	var out []entity
	for _, d := range devices {
		if !slices.Contains(q.Groups, d.Group) || !SelectedDevice(q, d.Group, d.ID) {
			continue
		}
		for _, metric := range d.Metrics {
			sum := sha256.Sum256([]byte(d.Group + "\x00" + d.ID))
			k := fmt.Sprintf("%s_%x_%s", key(d.Group), sum[:6], key(metric.Key))
			cfg := map[string]any{"name": d.Name + " " + metric.Name}
			component := "sensor"
			if metric.Binary {
				component = "binary_sensor"
			}
			if metric.Class != "" {
				cfg["device_class"] = metric.Class
			}
			if metric.Unit != "" {
				cfg["unit_of_measurement"] = metric.Unit
			}
			if metric.StateClass != "" {
				cfg["state_class"] = metric.StateClass
			}
			known := metric.Value != nil
			state := "None"
			if known {
				state = *metric.Value
			}
			out = append(out, entity{component: component, key: k, config: cfg, state: state, available: &known, group: d.Group})
		}
	}
	return out
}

// Preserve existing discovery IDs unless two distinct names normalize to the
// same old key. Collisions also need distinct command topics to avoid operating
// the wrong container or VM.
func legacyIdentity(snap Snapshot) func(string, string) string {
	names := map[string]map[string]bool{}
	remember := func(kind, name string) {
		k := key(kind, name)
		if names[k] == nil {
			names[k] = map[string]bool{}
		}
		names[k][name] = true
	}
	for _, v := range snap.Volumes {
		remember("volume", v.Name)
	}
	for _, v := range snap.Disks {
		remember("disk", v.ID)
	}
	for _, v := range snap.Backups {
		remember("backup", v.Name)
	}
	for _, v := range snap.Containers {
		remember("container", v.Name)
	}
	for _, v := range snap.VMs {
		remember("vm", v.Name)
	}
	return func(kind, name string) string {
		k := key(kind, name)
		if len(names[k]) > 1 {
			sum := sha256.Sum256([]byte(name))
			return fmt.Sprintf("%s_%x", k, sum[:6])
		}
		return k
	}
}

// A reconnect may begin while an inventory source is unavailable. Retained
// discovery can still be matched against an explicit selection by its stable
// identity, without treating the source failure as removal of selected devices.
func retainedSelected(q MQTT, group, entityKey string) bool {
	ids, restricted := q.Devices[group]
	if !restricted {
		return true
	}
	kinds := map[string]string{"volumes": "volume", "disks": "disk", "backups": "backup", "containers": "container", "vms": "vm"}
	suffixes := map[string][]string{"volumes": {"_state", "_used", "_free"}, "disks": {"_temperature", "_problem"}, "backups": {"_running", "_problem", "_progress", "_last_success", "_next", "_run"}, "containers": {"", "_restart"}, "vms": {""}, "ups": {"_status", "_charge"}}
	for _, id := range ids {
		sum := sha256.Sum256([]byte(group + "\x00" + id))
		if strings.HasPrefix(entityKey, fmt.Sprintf("%s_%x_", key(group), sum[:6])) {
			return true
		}
		legacy := key(kinds[group], id)
		if group == "ups" {
			legacy = "ups"
		}
		rawHash := sha256.Sum256([]byte(id))
		for _, suffix := range suffixes[group] {
			if entityKey == legacy+suffix || entityKey == fmt.Sprintf("%s_%x%s", legacy, rawHash[:6], suffix) {
				return true
			}
		}
	}
	return false
}
