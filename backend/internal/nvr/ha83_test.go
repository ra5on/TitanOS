package nvr

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// PLAN 83: a condition is stored in a tidy form; no rules is none.
func TestHAConditionValidate(t *testing.T) {
	if got, err := validateHA(&HACondition{Mode: HAModeAny}); err != nil || got != nil {
		t.Fatalf("no rules: %+v %v", got, err)
	}
	got, err := validateHA(&HACondition{Rules: []HARule{{Entity: "alarm_control_panel.home", States: []string{" armed_away ", "armed_night", "armed_away"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != HAModeAll || strings.Join(got.Rules[0].States, ",") != "armed_away,armed_night" {
		t.Fatalf("stored as %+v", got)
	}
	many := make([]HARule, MaxHARules+1)
	for i := range many {
		many[i] = HARule{Entity: "input_boolean.x", States: []string{"on"}}
	}
	for name, c := range map[string]HACondition{
		"mode":        {Mode: "most", Rules: []HARule{{Entity: "switch.a", States: []string{"on"}}}},
		"entity":      {Rules: []HARule{{Entity: "Alarm", States: []string{"on"}}}},
		"no state":    {Rules: []HARule{{Entity: "switch.a", States: []string{"  "}}}},
		"control":     {Rules: []HARule{{Entity: "switch.a", States: []string{"o\tn"}}}},
		"connection":  {Rules: []HARule{{Connection: "../x", Entity: "switch.a", States: []string{"on"}}}},
		"too many":    {Rules: many},
		"long entity": {Rules: []HARule{{Entity: "sensor." + strings.Repeat("a", 260), States: []string{"on"}}}},
	} {
		if _, err := validateHA(&c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// haRig is a notice rig whose Home Assistant states the test sets; a
// missing entity is not known.
type haRig struct {
	*noticeRig
	mu     sync.Mutex
	states map[string]string
}

func newHARig(t *testing.T, condition HACondition) *haRig {
	r := &haRig{noticeRig: newNoticeRig(t), states: map[string]string{}}
	r.m.HAState = func(connection, entity string) (string, bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		s, ok := r.states[connection+"/"+entity]
		return s, ok
	}
	r.noticeRig.set("haustuer", func(n *Notify) {
		n.Recipients = []Recipient{{User: "alex", Bell: true, Push: true, Added: created.Add(time.Hour)}}
		c := condition
		n.HA = &c
	})
	return r
}

func (r *haRig) state(entity, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state == "" {
		delete(r.states, "/"+entity)
		return
	}
	r.states["/"+entity] = state
}

// The condition lets messages about events through only in the chosen
// states; an unknown state never holds one back; "Kamera offline" is told
// whatever it says; a message held back does not start the pause.
func TestHAConditionDecides(t *testing.T) {
	alarm := HARule{Entity: "alarm_control_panel.home", States: []string{"armed_away", "armed_night"}}
	away := HARule{Entity: "person.alex", States: []string{"not_home"}}
	r := newHARig(t, HACondition{Mode: HAModeAll, Rules: []HARule{alarm, away}})

	r.state("alarm_control_panel.home", "disarmed")
	r.state("person.alex", "not_home")
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 0 {
		t.Fatalf("disarmed: told %+v", got)
	}
	// Armed right after: told at once, the held back event started no pause.
	r.state("alarm_control_panel.home", "armed_away")
	r.advance(5 * time.Second)
	r.event("haustuer", "person", "", "")
	got := r.take()
	if len(got) != 1 || got[0].HAUnknown || got[0].Event == "" || !strings.HasSuffix(got[0].Event, "-test") {
		t.Fatalf("armed and away: %+v", got)
	}
	// The person's state not known, the alarm armed: told, with the note.
	r.state("person.alex", "")
	r.advance(2 * time.Minute)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 || !got[0].HAUnknown {
		t.Fatalf("one state unknown: %+v", got)
	}
	// "unavailable" is not known either; a known rule that fails decides.
	r.state("person.alex", "unavailable")
	r.state("alarm_control_panel.home", "disarmed")
	r.advance(2 * time.Minute)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 0 {
		t.Fatalf("all: one rule fails, one unknown: told %+v", got)
	}
	// Offline is told whatever the condition says, without the note.
	r.offline("haustuer")
	if got := r.take(); len(got) != 1 || got[0].Kind != KindOffline || got[0].HAUnknown || got[0].Event != "" {
		t.Fatalf("offline: %+v", got)
	}

	// "any": one rule is enough.
	r.set("haustuer", func(n *Notify) { n.HA.Mode = HAModeAny })
	r.state("person.alex", "not_home")
	r.advance(2 * time.Minute)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 || got[0].HAUnknown {
		t.Fatalf("any, one holds: %+v", got)
	}
	r.state("person.alex", "home")
	r.advance(2 * time.Minute)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 0 {
		t.Fatalf("any, none holds: told %+v", got)
	}
	// Nothing known at all (Home Assistant not reachable): told, with the note.
	r.state("person.alex", "")
	r.state("alarm_control_panel.home", "")
	r.advance(2 * time.Minute)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 || !got[0].HAUnknown {
		t.Fatalf("nothing known: %+v", got)
	}
	// Without the watcher wired at all: the same.
	r.m.HAState = nil
	r.advance(2 * time.Minute)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 || !got[0].HAUnknown {
		t.Fatalf("no watcher: %+v", got)
	}
}

// The entities of all conditions are watched, by connection; a camera of
// 0.79 without a condition is stored without one; a change decoded over a
// copy of a camera does not reach the stored condition.
func TestHAConditionStoredAndWatched(t *testing.T) {
	r := newHARig(t, HACondition{Rules: []HARule{{Entity: "alarm_control_panel.home", States: []string{"armed_away"}}, {Connection: "second", Entity: "input_boolean.holiday", States: []string{"on"}}}})
	watched := r.m.HAWatched()
	if strings.Join(watched[""], ",") != "alarm_control_panel.home" || strings.Join(watched["second"], ",") != "input_boolean.holiday" {
		t.Fatalf("watched %v", watched)
	}
	c, _ := r.m.Camera("haustuer")
	if err := json.Unmarshal([]byte(`{"notify":{"ha":{"mode":"any","rules":[{"entity":"switch.x","states":["on"]}]}}}`), &c); err != nil {
		t.Fatal(err)
	}
	stored, _ := r.m.Camera("haustuer")
	if stored.Notify.HA.Mode == HAModeAny || stored.Notify.HA.Rules[0].Entity != "alarm_control_panel.home" {
		t.Fatalf("a decoded change reached the stored camera: %+v", stored.Notify.HA)
	}
	plain, _ := r.m.Camera("garten")
	data, _ := json.Marshal(plain)
	if strings.Contains(string(data), `"ha"`) {
		t.Fatalf("a camera without a condition carries one: %s", data)
	}
}
