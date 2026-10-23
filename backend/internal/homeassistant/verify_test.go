package homeassistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestVerifyPostconditionsAndPartialResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("verification mutated state")
		}
		if r.URL.Path == "/api/states/light.one" {
			w.Write([]byte(`{"state":"on","attributes":{"brightness":128}}`))
		} else {
			w.Write([]byte(`{"state":"unavailable"}`))
		}
	}))
	defer srv.Close()
	m := NewManager(config.Open(t.TempDir()))
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.SetSettings(Settings{Enabled: true, URL: srv.URL, Token: "test", Domains: []string{"light"}}); err != nil {
		t.Fatal(err)
	}
	observations := m.Verify(context.Background(), "light", "turn_on", []string{"light.one", "light.two"}, map[string]any{"brightness_pct": 50.0}, 30*time.Millisecond)
	if observations[0].Status != "verified" || observations[1].Status == "verified" {
		t.Fatalf("%+v", observations)
	}
	if len(Desired("script", "turn_on", nil)) != 0 || len(Desired("light", "toggle", nil)) != 0 {
		t.Fatal("unverifiable service guessed")
	}
	want := Desired("climate", "set_temperature", map[string]any{"temperature": 21.0})
	if !matchesState(State{State: "heat", Attributes: map[string]any{"temperature": 21.0, "current_temperature": 16.0}}, want) {
		t.Fatal("setpoint confused with room temperature")
	}
}

func TestEntityPoliciesOnlyNarrowAccess(t *testing.T) {
	m, _, _ := newManager(t, Settings{Enabled: true, Token: "good-token", Domains: []string{"light"}, Policies: map[string]EntityPolicy{
		"light.kitchen": {ReadOnly: true}, "light.office": {Services: []string{"turn_off"}}, "light.none": {Services: []string{}}, "switch.hidden": {Aliases: []string{"heater"}},
	}})
	if m.CanCall("light.kitchen", "turn_on") || m.CanCall("light.office", "turn_on") || m.CanCall("light.none", "turn_off") || m.Allowed("switch.hidden") {
		t.Fatal("policy widened access")
	}
	if !m.CanCall("light.office", "turn_off") || !m.Allowed("light.kitchen") {
		t.Fatal("allowed access lost")
	}
	s, _ := m.Settings()
	p := s.Policies["light.office"]
	p.Services[0] = "turn_on"
	if m.CanCall("light.office", "turn_on") {
		t.Fatal("settings alias manager memory")
	}
	if _, err := m.Call(context.Background(), "light", "turn_on", []string{"light.kitchen"}, nil); err == nil {
		t.Fatal("mutation ignored policy")
	}
}
