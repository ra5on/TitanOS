package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// 1.13: the reminders of events are planned in RAM and become hints of
// the account (calendar_reminders.go): due ones once, a late one dropped,
// hidden calendars and an account without hints left out, a push only
// when asked, /system names the newest for every role, the route shows
// only the account's own.
func TestCalendarReminders(t *testing.T) {
	ts, pim, _, _ := pimServer(t)
	bea := ts.signIn("bea", auth.RoleUser)
	s := ts.server
	c := s.reminders
	berlin, _ := time.LoadLocation("Europe/Berlin")
	clock := time.Date(2026, 11, 20, 9, 0, 0, 0, berlin)
	var mu sync.Mutex
	set := func(t time.Time) { mu.Lock(); clock = t; mu.Unlock() }
	var pushed []string
	c.deps.Store(&reminderDeps{
		store:  pim,
		users:  func() []string { return []string{"alex", "bea"} },
		prefs:  s.reminderPrefsOf,
		hidden: s.pimDataArea().hiddenCalendars,
		push:   func(user string, r firedReminder) { pushed = append(pushed, user+":"+r.Title) },
		now:    func() time.Time { mu.Lock(); defer mu.Unlock(); return clock },
	})

	post := func(client *http.Client, cal string, ev map[string]any) {
		t.Helper()
		if res, body := ts.do(client, "POST", "/api/v1/calendar/events", map[string]any{"calendar": cal, "event": ev}); res.StatusCode != http.StatusCreated {
			t.Fatalf("create: %d %s", res.StatusCode, body)
		}
	}
	// 10:00 with 60 and 15 minutes before; all day tomorrow at 09:00 on the day; bea's own.
	post(ts.client, "privat", map[string]any{"title": "Zahnarzt", "start": "2026-11-20T10:00", "end": "2026-11-20T11:00",
		"timezone": "Europe/Berlin", "alarms": []int{60, 15}})
	post(ts.client, "privat", map[string]any{"title": "Müll", "start": "2026-11-21", "end": "2026-11-22", "allDay": true, "alarms": []int{-540}})
	post(ts.client, "privat", map[string]any{"title": "Früher", "start": "2026-11-20T08:00", "end": "2026-11-20T08:30",
		"timezone": "Europe/Berlin", "alarms": []int{30}})
	post(bea, "privat", map[string]any{"title": "Bea", "start": "2026-11-20T09:05", "end": "2026-11-20T10:00",
		"timezone": "Europe/Berlin", "alarms": []int{5}})
	// A hidden calendar is not planned.
	if res, body := ts.do(ts.client, "POST", "/api/v1/calendar/calendars", map[string]any{"name": "Versteckt"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("calendar: %d %s", res.StatusCode, body)
	}
	post(ts.client, "versteckt", map[string]any{"title": "Geheim", "start": "2026-11-20T09:30", "end": "2026-11-20T10:00",
		"timezone": "Europe/Berlin", "alarms": []int{0}})
	if res, _ := ts.do(ts.client, "PUT", "/api/v1/preferences/calendar", map[string]any{"hidden": []string{"versteckt"}}); res.StatusCode != http.StatusNoContent {
		t.Fatal("hide")
	}

	c.replan("")
	c.planDirty()
	c.fire()
	got := func(client *http.Client) []string {
		t.Helper()
		res, body := ts.do(client, "GET", "/api/v1/calendar/reminders", nil)
		var v struct {
			Reminders []firedReminder `json:"reminders"`
		}
		if res.StatusCode != http.StatusOK || json.Unmarshal(body, &v) != nil {
			t.Fatalf("reminders: %d %s", res.StatusCode, body)
		}
		var out []string
		for _, r := range v.Reminders {
			out = append(out, r.Title+"/"+time.Duration(r.Minutes*int(time.Minute)).String())
		}
		return out
	}
	// 09:00: the hour before the dentist; "Früher" (07:30) was due too long ago.
	if g := strings.Join(got(ts.client), ","); g != "Zahnarzt/1h0m0s" {
		t.Fatalf("09:00: %s", g)
	}
	if g := got(bea); len(g) != 1 || g[0] != "Bea/5m0s" {
		t.Fatalf("bea: %v", g)
	}
	// /system names the newest for every role.
	res, body := ts.do(bea, "GET", "/api/v1/system", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"remindersLatest":"`) {
		t.Fatalf("system: %s", body)
	}
	// Planned again (a change) and fired again at the same time: nothing twice.
	c.replan("alex")
	c.planDirty()
	c.fire()
	if g := got(ts.client); len(g) != 1 {
		t.Fatalf("twice: %v", g)
	}
	// 09:45 and the next day at 09:00 (the all-day event on its day).
	set(time.Date(2026, 11, 20, 9, 45, 30, 0, berlin))
	c.fire()
	if g := strings.Join(got(ts.client), ","); g != "Zahnarzt/1h0m0s,Zahnarzt/15m0s" {
		t.Fatalf("09:45: %s", g)
	}
	// The next day: the plan slides (made again), older hints than 12 h are gone.
	set(time.Date(2026, 11, 21, 9, 0, 0, 0, berlin))
	c.replan("")
	c.planDirty()
	c.fire()
	if g := strings.Join(got(ts.client), ","); g != "Müll/-9h0m0s" {
		t.Fatalf("next day: %s", g)
	}
	if len(pushed) != 0 {
		t.Fatalf("pushed without asking: %v", pushed)
	}

	// Push asked for, hints off: the hidden calendar stays hidden (merged).
	if res, _ := ts.do(ts.client, "PUT", "/api/v1/preferences/calendar", map[string]any{"reminderPush": true, "reminderHints": false}); res.StatusCode != http.StatusNoContent {
		t.Fatal("prefs")
	}
	if h := s.pimDataArea().hiddenCalendars("alex"); len(h) != 1 || h[0] != "versteckt" {
		t.Fatalf("hidden lost: %v", h)
	}
	post(ts.client, "privat", map[string]any{"title": "Anruf", "start": "2026-11-21T10:00", "end": "2026-11-21T10:30",
		"timezone": "Europe/Berlin", "alarms": []int{10}})
	c.planDirty()
	set(time.Date(2026, 11, 21, 9, 50, 0, 0, berlin))
	c.fire()
	if len(pushed) != 1 || pushed[0] != "alex:Anruf" {
		t.Fatalf("push: %v", pushed)
	}
	if g := got(ts.client); len(g) != 1 {
		t.Fatalf("a hint without hints: %v", g)
	}
	if res, _ := ts.do(ts.client, "PUT", "/api/v1/preferences/calendar", map[string]any{"reminderPush": "ja"}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("a push that is no boolean")
	}

	// The account deleted: nothing kept.
	c.forget("alex")
	if c.latest("alex") != "" {
		t.Fatal("forgotten account keeps hints")
	}
	_ = pimstore.Calendar
}
