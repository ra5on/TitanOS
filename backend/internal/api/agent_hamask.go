package api

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/diagnose"
)

// What the agent reads of Home Assistant beyond states (1.10, security
// review of 17 Nov 2026, M1–M3, B1): the configuration of an automation
// or script, a line of the logbook. Both name devices that may not be
// released to the agent – in entity fields, but also in templates, keys of
// scenes, comma lists and services – and may hold secrets (the id of a
// webhook, a token in data, a login in an address). Rather too much is
// masked than too little: a device that is not released is never named,
// a secret never leaves the NAS.

const (
	// haHiddenMark stands where a device that is not released was named.
	haHiddenMark = "(not released)"
	// haSecretMark stands where a secret was.
	haSecretMark = "(secret removed)"
)

// haEntityDomains are the domains entities can have in Home Assistant;
// broader than what can be released, since a config may name any of them.
var haEntityDomains = []string{
	"air_quality", "alarm_control_panel", "assist_satellite", "automation", "binary_sensor", "button",
	"calendar", "camera", "climate", "conversation", "counter", "cover", "date", "datetime",
	"device_tracker", "event", "fan", "geo_location", "group", "humidifier", "image", "image_processing",
	"input_boolean", "input_button", "input_datetime", "input_number", "input_select", "input_text",
	"lawn_mower", "light", "lock", "media_player", "notify", "number", "person", "plant", "remote",
	"scene", "schedule", "script", "select", "sensor", "siren", "stt", "sun", "switch", "tag", "text",
	"time", "timer", "todo", "tts", "update", "vacuum", "valve", "wake_word", "water_heater", "weather",
	"zone",
}

// haEntityToken finds entity ids inside any text: "lock.haustuer" in
// "{{ is_state('lock.haustuer','on') }}", in "light.a, lock.b" or in
// states.lock.haustuer.state.
var haEntityToken = regexp.MustCompile(`\b(?:` + strings.Join(haEntityDomains, "|") + `)\.[a-z0-9_]+`)

// haSecretKey are keys whose value is a secret, whatever its type: the id
// of a webhook (anyone who knows it can trigger the automation), tokens,
// passwords, keys, the code of an alarm panel or a lock, headers.
var haSecretKey = regexp.MustCompile(`(?i)(passw|passphrase|secret|token|api_?key|apikey|private_?key|webhook|credential|auth|cookie|session|^headers?$|^code$|^pin$|^key$|^psk$)`)

// haPlaceKeys name devices, areas, floors and labels by id; they are never
// shown (a device has no release of its own to check against).
var haPlaceKeys = map[string]bool{"device_id": true, "area_id": true, "floor_id": true, "label_id": true}

// haMasker masks one config or text against the release of a context.
type haMasker struct {
	allowed func(entity string) bool
	hidden  bool
	secrets bool
}

func newHAMasker(ctx context.Context, allowed func(context.Context, string) bool) *haMasker {
	return &haMasker{allowed: func(e string) bool { return allowed(ctx, e) }}
}

// text masks the entity ids of a text that are not released.
func (m *haMasker) text(s string) string {
	return haEntityToken.ReplaceAllStringFunc(s, func(id string) string {
		if m.allowed(id) {
			return id
		}
		m.hidden = true
		return haHiddenMark
	})
}

// value returns v with devices that are not released and secrets masked;
// key is the key v stands under ("" at the top or in a list of a list).
func (m *haMasker) value(key string, v any) any {
	if key != "" && haSecretKey.MatchString(key) {
		// A reference to secrets.yaml says only which secret, not what.
		if s, ok := v.(string); ok && strings.HasPrefix(strings.TrimSpace(s), "!secret ") {
			return s
		}
		if v == nil || v == "" {
			return v
		}
		m.secrets = true
		return haSecretMark
	}
	if haPlaceKeys[key] {
		if v == nil || v == "" {
			return v
		}
		m.hidden = true
		return haHiddenMark
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		n := 0
		for k, vv := range x {
			nk := m.text(k)
			if nk != k {
				// Keys of a scene: several may be masked, keep them apart.
				n++
				nk = fmt.Sprintf("%s %d", haHiddenMark, n)
			}
			out[nk] = m.value(k, vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = m.value(key, vv)
		}
		return out
	case string:
		switch key {
		case "service", "action":
			// light.turn_on is a service, not a device; but notify.annas_handy
			// and script.gute_nacht name one.
			domain, _, _ := strings.Cut(x, ".")
			if domain == "notify" || domain == "script" {
				if !m.allowed(x) {
					m.hidden = true
					return haHiddenMark
				}
			}
			return m.secret(x)
		}
		return m.secret(m.text(x))
	}
	return v
}

// secret takes secrets out of free text (a token in a message, a login in
// an address).
func (m *haMasker) secret(s string) string {
	out := diagnose.Secrets(s)
	if out != s {
		m.secrets = true
	}
	return out
}

// why says what was masked, for the result.
func (m *haMasker) why() string {
	var parts []string
	if m.hidden {
		parts = append(parts, "Devices, areas and services that are not released to the agent are shown as "+haHiddenMark+"; the person can release them under "+whereHA+".")
	}
	if m.secrets {
		parts = append(parts, "Secrets (webhook ids, tokens, passwords, codes, logins in addresses) are shown as "+haSecretMark+" or <removed>; Home Assistant keeps them.")
	}
	return strings.Join(parts, " ")
}

// haHasMarks reports whether a config holds stand-ins of a masked or cut
// result anywhere (keys or values): it cannot be saved like that.
func haHasMarks(v any, marks ...string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			if haHasMarks(k, marks...) || haHasMarks(vv, marks...) {
				return true
			}
		}
	case []any:
		for _, vv := range x {
			if haHasMarks(vv, marks...) {
				return true
			}
		}
	case string:
		for _, mark := range marks {
			if strings.Contains(x, mark) {
				return true
			}
		}
	}
	return false
}
