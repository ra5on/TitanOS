package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// Tests of the security review of 1.10 (Entwuerfe/109-agent-fehler/
// SICHERHEIT-110.md): Home Assistant (M1–M3, B1).

// haSecretConfig is an automation as the editor of Home Assistant stores
// it: a webhook, a token in data, a login in an address, a reference to
// secrets.yaml, and devices that are not released in every place a config
// can name them.
const haSecretConfig = `{"id":"1","alias":"Licht aus",
"triggers":[{"trigger":"webhook","webhook_id":"geheim-abc123"},{"trigger":"state","entity_id":"binary_sensor.fenster_kueche, light.flur"},
 {"trigger":"device","device_id":"a1b2c3d4e5","domain":"lock","type":"unlocked"}],
"conditions":[{"condition":"template","value_template":"{{ is_state('lock.haustuer','unlocked') and states.sensor.anna_handy_ort.state == 'home' }}"}],
"actions":[{"action":"light.turn_on","target":{"entity_id":"light.flur","area_id":"schlafzimmer"}},
 {"action":"notify.mobile_app_annas_iphone","data":{"message":"Tür {{ states('lock.haustuer') }}","token":"tok-123456","code":1234}},
 {"action":"scene.apply","data":{"entities":{"lock.haustuer":"locked","light.flur":"off"}}},
 {"action":"rest_command.kamera","data":{"url":"http://admin:s3cret@cam.local/snap","password":"!secret cam_pw"}}]}`

func haSecretHouse(t *testing.T, saved *string) *testServer {
	t.Helper()
	ha := haHouse(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/states/automation.licht_aus":
			w.Write([]byte(`{"entity_id":"automation.licht_aus","state":"on","attributes":{"friendly_name":"Licht aus","id":"1"}}`))
		case (r.URL.Path == "/api/config/automation/config/1" || r.URL.Path == "/api/config/script/config/1") && r.Method == http.MethodGet:
			w.Write([]byte(haSecretConfig))
		case strings.HasPrefix(r.URL.Path, "/api/config/") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			*saved = string(body)
			w.Write([]byte(`{"result":"ok"}`))
		case strings.HasPrefix(r.URL.Path, "/api/config/"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/reload"):
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})
	return haAgentServer(t, ha)
}

// The secrets and hidden devices of the review, none of which may reach
// the model.
var haLeaks = []string{"geheim-abc123", "tok-123456", "1234", "s3cret", "admin:", "lock.haustuer", "anna_handy", "annas_iphone", "fenster_kueche", "a1b2c3d4e5", "schlafzimmer"}

func haLeaked(text string) string {
	for _, l := range haLeaks {
		if strings.Contains(text, l) {
			return l
		}
	}
	return ""
}

// M1, M2: ha_config_read masks secrets and every naming of a device that
// is not released – templates, keys of a scene, comma lists, notify,
// device_id, area_id – and keeps what is released and the services.
func TestAgentHAConfigMasksSecretsAndHiddenDevices(t *testing.T) {
	var saved string
	ts := haSecretHouse(t, &saved)
	settings := ts.server.agentSettings()
	settings.HA.Release = homeassistant.Release{Domains: []string{"automation", "light"}}
	result, text, err := runAgentTool(t, ts, settings, "ha_config_read", map[string]any{"entity": "automation.licht_aus"})
	if err != nil {
		t.Fatal(err)
	}
	if l := haLeaked(text); l != "" {
		t.Fatalf("%q reaches the model: %s", l, text)
	}
	for _, keep := range []string{`"light.turn_on"`, `light.flur`, `!secret cam_pw`, `"scene.apply"`, `cam.local`, haHiddenMark, haSecretMark} {
		if !strings.Contains(text, keep) {
			t.Fatalf("%s missing: %s", keep, text)
		}
	}
	if result["hidden"] != true || !strings.Contains(result["why"].(string), "Secrets") || !strings.Contains(result["note"].(string), agent.CutMark) {
		t.Fatalf("hints: %v", result)
	}
	// Everything released ("Alles"): devices stay, secrets still go.
	settings.HA.Release = homeassistant.Release{All: true}
	_, text, _ = runAgentTool(t, ts, settings, "ha_config_read", map[string]any{"entity": "automation.licht_aus"})
	if !strings.Contains(text, "lock.haustuer") || strings.Contains(text, "geheim-abc123") || strings.Contains(text, "s3cret") {
		t.Fatalf("all released: %s", text)
	}
}

// M3: the text of a logbook line does not name hidden devices.
func TestAgentHALogbookMasksMessages(t *testing.T) {
	ha := haHouse(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/logbook/") {
			var lines []string
			for range 130 {
				lines = append(lines, `{"when":"2026-11-17T02:00:00+00:00","entity_id":"automation.licht_aus","name":"Licht aus","message":"triggered by state of binary_sensor.fenster_kueche"}`)
			}
			lines = append(lines, `{"when":"2026-11-17T03:00:00+00:00","entity_id":"automation.licht_aus","name":"Licht aus","message":"triggered by action lock.unlock"}`)
			w.Write([]byte("[" + strings.Join(lines, ",") + "]"))
			return
		}
		http.NotFound(w, r)
	})
	ts := haAgentServer(t, ha)
	settings := ts.server.agentSettings()
	settings.HA.Release = homeassistant.Release{Domains: []string{"automation"}}
	result, text, err := runAgentTool(t, ts, settings, "ha_logbook", map[string]any{"since": "2026-11-17T00:00", "until": "2026-11-17T07:00"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "fenster_kueche") || result["hidden"] != true || !strings.Contains(text, "triggered by state of "+haHiddenMark) {
		t.Fatalf("logbook message: %.600s", text)
	}
	// N7: at most the newest 100 lines, with the way to older ones.
	if result["shown"] != float64(haLogbookLines) || result["cut"] != true || !strings.Contains(result["next"].(string), "until=") {
		t.Fatalf("limit: %v %v %v", result["shown"], result["cut"], result["next"])
	}
	// The newest line is kept (its service looks like a device and is
	// masked too: rather too much than too little).
	if !strings.Contains(text, "T03:00:00") {
		t.Fatalf("the newest line is missing: %.300s", text)
	}
}

// B1: ha_automation_save checks the release of the automation it changes,
// gives the previous version masked, puts secrets back and refuses
// stand-ins, cut configs and devices that are not released.
func TestAgentHASaveKeepsTheRelease(t *testing.T) {
	var saved string
	ts := haSecretHouse(t, &saved)
	settings := ts.server.agentSettings()
	settings.Policy = agent.Policy{Preset: agent.PresetAll}
	settings.HA.Release = homeassistant.Release{Domains: []string{"automation", "light"}}
	save := func(name string, args map[string]any) (map[string]any, string, error) {
		saved = ""
		return runAgentTool(t, ts, settings, name, args)
	}
	webhook := map[string]any{"alias": "Licht aus", "triggers": []any{map[string]any{"trigger": "webhook", "webhook_id": haSecretMark}},
		"actions": []any{map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": "light.flur"}}}}
	result, text, err := save("ha_automation_save", map[string]any{"id": "1", "config": webhook})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "geheim-abc123") || strings.Contains(saved, haSecretMark) {
		t.Fatalf("secret not put back: %s", saved)
	}
	if l := haLeaked(text); l != "" || result["previous"] == nil {
		t.Fatalf("previous not masked (%q): %s", l, text)
	}
	refused := []map[string]any{
		{"alias": "x", "actions": []any{map[string]any{"action": "lock.unlock", "target": map[string]any{"entity_id": haHiddenMark}}}},
		{"alias": "x", "conditions": []any{map[string]any{"condition": "template", "value_template": "{{ is_state('lock.haustuer','on') }}"}}},
		{"alias": "x", "actions": []any{map[string]any{"action": "light.turn_on", "target": map[string]any{"area_id": "schlafzimmer"}}}},
		{"alias": "x", "actions": []any{"1 more " + agent.CutMark}},
		{"alias": "x", "actions": []any{map[string]any{"action": "notify.mobile_app_annas_iphone"}}},
	}
	for i, config := range refused {
		if _, _, err := save("ha_automation_save", map[string]any{"id": "1", "config": config}); err == nil || saved != "" {
			t.Fatalf("config %d saved: %v %s", i, err, saved)
		}
	}
	// An automation that is not released is not changed.
	settings.HA.Release = homeassistant.Release{Domains: []string{"light"}}
	if _, _, err := save("ha_automation_save", map[string]any{"id": "1", "config": webhook}); err == nil || saved != "" {
		t.Fatalf("hidden automation saved: %v", err)
	}
	// A new automation of released devices can be made.
	if _, _, err := save("ha_automation_save", map[string]any{"id": "neu", "config": map[string]any{"alias": "Neu", "actions": []any{map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": "light.flur"}}}}}); err != nil || saved == "" {
		t.Fatalf("new automation: %v", err)
	}
	// Scripts share the code: script.1 exists and is not released.
	if _, _, err := save("ha_script_save", map[string]any{"id": "1", "config": map[string]any{"alias": "x", "sequence": []any{}}}); err == nil || saved != "" {
		t.Fatalf("hidden script saved: %v", err)
	}
}

// N1–N4, N6: the activity log does not give addresses (not even through a
// search), nor names typed at a failed login; an app in front of a target
// does not hide a locked folder; a search never says whether something in
// a locked folder matched; the rules are applied by name, without the
// folder having to exist; a year at most, and it says so.
func TestAgentActivityLogSecurity(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard, Folders: []agent.FolderRule{{Root: "share/Docs", Path: "Privat", Mode: agent.ModeDeny}}})
	ts.withShare() // Privat is not made: the rule holds by its name
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	now := time.Now()
	log.Record(activity.Entry{At: now.Add(-time.Hour), User: "MeinPasswort123", Via: "web", IP: "203.0.113.7", Action: "login_failed", OK: false})
	log.Record(activity.Entry{At: now.Add(-time.Hour), User: "alex", Via: "web", IP: "203.0.113.9", Action: "login", OK: true})
	log.Record(activity.Entry{At: now.Add(-time.Hour), User: "alex", Via: "web", Action: "POST /api/v1/docker/open", Target: "jellyfin: " + filePlace("share/Docs", "Privat/film.mkv"), OK: true})
	log.Record(activity.Entry{At: now.Add(-time.Hour), User: "alex", Via: "web", Action: "POST /api/v1/files/delete", Target: filePlace("share/Docs", "Offen/alt.txt"), OK: true})
	settings := ts.server.agentSettings()
	run := func(args map[string]any) (map[string]any, string) {
		t.Helper()
		result, text, err := runAgentTool(t, ts, settings, "activity_log", args)
		if err != nil {
			t.Fatal(err)
		}
		return result, text
	}
	// N1: no search by address.
	if result, _ := run(map[string]any{"search": "203.0.113"}); result["total"] != float64(0) {
		t.Fatalf("found by address: %v", result)
	}
	// N2: a typed name is neither shown nor searchable nor a filter.
	result, text := run(map[string]any{})
	if strings.Contains(text, "MeinPasswort123") || !strings.Contains(text, "(no such account)") {
		t.Fatalf("typed name shown: %s", text)
	}
	if result, _ := run(map[string]any{"search": "MeinPass"}); result["total"] != float64(0) {
		t.Fatalf("typed name found: %v", result)
	}
	if _, _, err := runAgentTool(t, ts, settings, "activity_log", map[string]any{"account": "MeinPasswort123"}); err == nil {
		t.Fatal("unknown account as a filter")
	}
	// N3, N6: the app in front and the folder that does not exist on disk.
	if strings.Contains(text, "Privat") || result["hidden"] != float64(1) {
		t.Fatalf("locked folder behind an app: %v %s", result["hidden"], text)
	}
	// N4: a search says the same whether a locked entry matched or not.
	hit, _ := run(map[string]any{"search": "film"})
	miss, _ := run(map[string]any{"search": "nichts-passt-xyz"})
	if hit["hidden"] != true || miss["hidden"] != true || hit["why"] != miss["why"] || hit["total"] != float64(0) {
		t.Fatalf("search oracle: %v / %v", hit, miss)
	}
	// N6: more than a year back is cut, and said.
	if result, _ := run(map[string]any{"since": "500d"}); !strings.Contains(fmt.Sprint(result["note"]), "365 days") || result["since"] == now.AddDate(0, 0, -500).Format("2006-01-02 15:04") {
		t.Fatalf("since: %v %v", result["since"], result["note"])
	}
}
