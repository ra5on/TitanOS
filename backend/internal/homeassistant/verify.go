package homeassistant

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

type Observation struct {
	Entity     string         `json:"entity"`
	Status     string         `json:"status"`
	State      string         `json:"state,omitempty"`
	Expected   map[string]any `json:"expected,omitempty"`
	ObservedAt time.Time      `json:"observedAt"`
	Error      string         `json:"error,omitempty"`
}

// Desired describes only postconditions that the action actually promises.
// Scenes, scripts, toggles and restarts cannot be inferred from a final state.
func Desired(domain, service string, data map[string]any) map[string]any {
	want := map[string]any{}
	switch service {
	case "turn_on", "turn_off":
		if domain == "light" || domain == "switch" || domain == "fan" || domain == "input_boolean" || domain == "humidifier" {
			want["state"] = map[string]string{"turn_on": "on", "turn_off": "off"}[service]
		}
	case "lock":
		if domain == "lock" {
			want["state"] = "locked"
		}
	case "unlock":
		if domain == "lock" {
			want["state"] = "unlocked"
		}
	case "open_cover":
		if domain == "cover" {
			want["state"] = "open"
		}
	case "close_cover":
		if domain == "cover" {
			want["state"] = "closed"
		}
	case "set_cover_position":
		if domain == "cover" {
			if v, ok := data["position"]; ok {
				want["current_position"] = v
			}
		}
	case "set_temperature":
		if domain == "climate" || domain == "water_heater" {
			for _, key := range []string{"temperature", "target_temp_low", "target_temp_high"} {
				if v, ok := data[key]; ok {
					want[key] = v
				}
			}
		}
	case "set_hvac_mode":
		if domain == "climate" {
			if v, ok := data["hvac_mode"]; ok {
				want["state"] = v
			}
		}
	case "set_percentage":
		if domain == "fan" {
			if v, ok := data["percentage"]; ok {
				want["percentage"] = v
			}
		}
	}
	if domain == "light" && service == "turn_on" {
		for _, key := range []string{"brightness", "color_temp_kelvin"} {
			if v, ok := data[key]; ok {
				want[key] = v
			}
		}
		if v, ok := numeric(data["brightness_pct"]); ok {
			want["brightness"] = math.Round(v * 255 / 100)
		}
	}
	return want
}

func numeric(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(n), true
	case json.Number:
		x, err := n.Float64()
		return x, err == nil
	}
	return 0, false
}

func matchesState(st State, want map[string]any) bool {
	if st.State == "unknown" || st.State == "unavailable" {
		return false
	}
	for key, expected := range want {
		actual := st.Attributes[key]
		if key == "state" {
			actual = st.State
		}
		if n, ok := numeric(expected); ok {
			v, valid := numeric(actual)
			tolerance := 0.01
			if key == "brightness" {
				tolerance = 1
			}
			if !valid || math.Abs(n-v) > tolerance {
				return false
			}
		} else {
			a, _ := json.Marshal(actual)
			b, _ := json.Marshal(expected)
			if string(a) != string(b) {
				return false
			}
		}
	}
	return len(want) > 0
}

// Verify only observes. It never retries a mutation, including on timeout.
// Its deadline is shared across all targets, not multiplied by their number.
func (m *Manager) Verify(ctx context.Context, domain, service string, entities []string, data map[string]any, duration time.Duration) []Observation {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	want := Desired(domain, service, data)
	out := make([]Observation, len(entities))
	for i, id := range entities {
		out[i] = Observation{Entity: id, Status: "not_verified", Expected: want}
	}
	for {
		pending := false
		for i := range out {
			o := &out[i]
			if o.Status == "verified" {
				continue
			}
			o.ObservedAt = time.Now()
			if !m.Allowed(o.Entity) {
				o.Status, o.Error = "not_verified", "entity access revoked"
				continue
			}
			st, err := m.State(ctx, o.Entity)
			if err != nil {
				o.Status, o.Error = "not_verified", err.Error()
				continue
			}
			o.State, o.Error = st.State, ""
			if len(want) == 0 {
				o.Status = "not_verifiable"
				continue
			}
			o.Status = "not_verified"
			if matchesState(st, want) {
				o.Status = "verified"
			} else {
				pending = true
			}
		}
		if !pending || ctx.Err() != nil {
			return out
		}
		select {
		case <-ctx.Done():
			return out
		case <-time.After(500 * time.Millisecond):
		}
	}
}
