package nvr

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// noticeRig is a manager with cameras in RAM, accounts the test changes, a
// clock it moves and the notices it routed (PLAN 82-3).
type noticeRig struct {
	t        *testing.T
	m        *Manager
	mu       sync.Mutex
	accounts map[string]Account
	now      time.Time
	routed   []Notice
}

// created is when the test's accounts were made: before any right or
// recipient the tests give.
var created = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func newNoticeRig(t *testing.T) *noticeRig {
	t.Helper()
	r := &noticeRig{t: t, accounts: map[string]Account{
		"alex": {Name: "alex", Admin: true, Created: created},
		"olga": {Name: "olga", Admin: true, Created: created},
		"sam":  {Name: "sam", Created: created},
		"carl": {Name: "carl", Created: created},
		"mara": {Name: "mara", Created: created},
	}}
	// Tuesday, 17 November 2026, 12:00 in Berlin.
	r.now = time.Date(2026, 11, 17, 11, 0, 0, 0, time.UTC)
	m := New(config.Open(t.TempDir()))
	m.Access.Lookup = func(name string) (Account, bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		a, ok := r.accounts[name]
		return a, ok
	}
	if err := m.Access.Load(); err != nil {
		t.Fatal(err)
	}
	m.Now = func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }
	m.Timezone = func() string { return "Europe/Berlin" }
	m.Route = func(n Notice) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.routed = append(r.routed, n)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	m.cameras = []Camera{
		testCamera("haustuer", "Haustür", func(c *Camera) {
			c.Detect.Objects = []string{"person", "car"}
			c.Notify = Notify{Kinds: []string{"person", "car", "plate", "face", "offline"}, Recipients: []Recipient{}}
		}),
		testCamera("garten", "Garten", func(c *Camera) {
			c.Notify = Notify{Titan: true, Push: true, Kinds: []string{"person", "offline"}}
		}),
	}
	// sam may watch the front door live; carl may see nothing.
	if err := m.Access.SetUser("sam", UserRights{"haustuer": {Live: true}}, []string{"haustuer", "garten"}); err != nil {
		t.Fatal(err)
	}
	r.m = m
	return r
}

// set changes a camera in RAM as it is stored (not through UpdateCamera:
// these tests are about sending).
func (r *noticeRig) set(id string, edit func(n *Notify)) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	for i := range r.m.cameras {
		if r.m.cameras[i].ID == id {
			edit(&r.m.cameras[i].Notify)
		}
	}
}

func (r *noticeRig) advance(d time.Duration) {
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
}

func (r *noticeRig) at(t time.Time) {
	r.mu.Lock()
	r.now = t
	r.mu.Unlock()
}

// take returns what was routed since the last call.
func (r *noticeRig) take() []Notice {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.routed
	r.routed = nil
	return out
}

var eventNo int

// event lets Frigate report a new event at a camera.
func (r *noticeRig) event(camera, label, subLabel, plate string) {
	eventNo++
	r.m.routeEvent(Event{ID: "1731412345." + strconv.Itoa(eventNo) + "-test", Camera: camera, Label: label, SubLabel: subLabel, Plate: plate})
}

func (r *noticeRig) offline(camera string) {
	c, _ := r.m.Camera(camera)
	r.m.report(c, KindOffline, "nvr_camera_offline", "warning", c.Name, "", r.m.now())
}

func users(targets []Target) []string {
	out := []string{}
	for _, t := range targets {
		out = append(out, t.User)
	}
	return out
}

// The main switch silences every camera, new and of 0.78 alike, but not
// the states Home Assistant reads.
func TestNotifyMainSwitch(t *testing.T) {
	r := newNoticeRig(t)
	r.set("haustuer", func(n *Notify) {
		n.HomeAssistant = true
		n.Recipients = []Recipient{{User: "alex", Bell: true, Added: created.Add(time.Hour)}}
	})
	r.m.settings.NotifyOff = true
	r.event("haustuer", "person", "", "")
	r.event("garten", "person", "", "")
	r.offline("garten")
	if got := r.take(); len(got) != 0 {
		t.Fatalf("the main switch is off, yet routed %+v", got)
	}
	ha := r.m.HACameras()
	if len(ha) != 1 || !ha[0].Active[KindPerson] {
		t.Fatalf("Home Assistant lost its state: %+v", ha)
	}
	// Switched on again: the next events are reported (the silenced ones
	// did not start the pause).
	r.m.settings.NotifyOff = false
	r.event("haustuer", "person", "", "")
	r.event("garten", "person", "", "")
	if got := r.take(); len(got) != 2 {
		t.Fatalf("routed %+v", got)
	}
}

// The pause is the camera's: none, minutes, or 60 s for a camera that
// names none (0.78).
func TestNotifyPauseChoices(t *testing.T) {
	r := newNoticeRig(t)
	pause := func(seconds int) { r.set("haustuer", func(n *Notify) { n.PauseSeconds = &seconds }) }
	r.set("haustuer", func(n *Notify) {
		n.Recipients = []Recipient{{User: "alex", Push: true, Added: created.Add(time.Hour)}}
	})
	// None: every event is told.
	pause(0)
	r.event("haustuer", "person", "", "")
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 2 {
		t.Fatalf("without a pause: %d", len(got))
	}
	// Five minutes.
	pause(300)
	r.advance(time.Hour)
	r.event("haustuer", "person", "", "")
	r.advance(4 * time.Minute)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 {
		t.Fatalf("within five minutes: %d", len(got))
	}
	r.advance(61 * time.Second)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 {
		t.Fatalf("after five minutes: %d", len(got))
	}
	// Per kind: a car is not held back by the person.
	r.event("haustuer", "car", "", "")
	if got := r.take(); len(got) != 1 || got[0].Kind != KindVehicle {
		t.Fatalf("a car: %+v", got)
	}
	// Not named (a camera of 0.78): 60 seconds.
	r.set("haustuer", func(n *Notify) { n.PauseSeconds = nil })
	r.advance(time.Hour)
	r.event("haustuer", "person", "", "")
	r.advance(59 * time.Second)
	r.event("haustuer", "person", "", "")
	r.advance(2 * time.Second)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 2 {
		t.Fatalf("with 60 s: %d", len(got))
	}
}

// The window holds back events outside it, in the time zone of the NAS;
// "offline" is told at any time. An event held back does not start the
// pause.
func TestNotifyWindow(t *testing.T) {
	r := newNoticeRig(t)
	berlin, _ := time.LoadLocation("Europe/Berlin")
	r.set("haustuer", func(n *Notify) {
		n.Recipients = []Recipient{{User: "alex", Bell: true, Added: created.Add(time.Hour)}}
		// Monday to Friday, 22:00 to 06:00 (across midnight).
		n.Window = Window{On: true, Days: []int{1, 2, 3, 4, 5}, From: "22:00", To: "06:00"}
	})
	for _, tc := range []struct {
		when time.Time
		want bool
	}{
		{time.Date(2026, 11, 17, 12, 0, 0, 0, berlin), false}, // Tuesday noon
		{time.Date(2026, 11, 17, 23, 0, 0, 0, berlin), true},  // Tuesday night
		{time.Date(2026, 11, 18, 5, 0, 0, 0, berlin), true},   // Wednesday early: Tuesday's window
		{time.Date(2026, 11, 21, 3, 0, 0, 0, berlin), true},   // Saturday early: Friday's window
		{time.Date(2026, 11, 21, 23, 0, 0, 0, berlin), false}, // Saturday night
		{time.Date(2026, 11, 23, 3, 0, 0, 0, berlin), false},  // Monday early: Sunday has none
		// 21:30 UTC is 22:30 in Berlin: the zone of the NAS counts.
		{time.Date(2026, 11, 26, 21, 30, 0, 0, time.UTC), true},
	} {
		r.at(tc.when)
		r.event("haustuer", "person", "", "")
		if got := len(r.take()) == 1; got != tc.want {
			t.Errorf("%s: told %v, want %v", tc.when, got, tc.want)
		}
	}
	// Offline outside the window (Saturday noon): told.
	r.at(time.Date(2026, 11, 28, 12, 0, 0, 0, berlin))
	r.offline("haustuer")
	if got := r.take(); len(got) != 1 || got[0].Kind != KindOffline {
		t.Fatalf("offline outside the window: %+v", got)
	}
	// Held back at 21:59:30, told at 22:00:10 although that is within
	// the minute of the pause.
	r.at(time.Date(2026, 11, 30, 21, 59, 30, 0, berlin))
	r.event("haustuer", "person", "", "")
	r.advance(40 * time.Second)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 {
		t.Fatalf("the first event in the window: %d", len(got))
	}
}

// Every message goes to the listed accounts that may get it now, each its
// own ways; plates and faces only to administrators, the bell only to
// them.
func TestNotifyRecipientsAtSendTime(t *testing.T) {
	r := newNoticeRig(t)
	chosen := created.Add(time.Hour)
	r.set("haustuer", func(n *Notify) {
		n.Agent = true
		n.Recipients = []Recipient{
			{User: "alex", Bell: true, Push: true, Added: chosen},
			// A bell stored for a plain account (made by hand): ignored.
			{User: "sam", Bell: true, Push: true, Email: true, Added: chosen},
			{User: "carl", Push: true, Added: chosen},  // no right on the camera
			{User: "mara", Email: true, Added: chosen}, // locked below
			{User: "dora", Push: true, Added: chosen},  // no such account
			{User: "olga", Bell: true, Added: chosen},  // made anew below
			{User: "nobody"}, // no way at all
		}
	})
	r.mu.Lock()
	r.accounts["mara"] = Account{Name: "mara", Disabled: true, Created: created}
	// olga was deleted and made again after she was chosen: an
	// administrator, so no tick guards her; the time of the choice does.
	r.accounts["olga"] = Account{Name: "olga", Admin: true, Created: chosen.Add(time.Minute)}
	r.mu.Unlock()
	r.event("haustuer", "person", "", "")
	got := r.take()
	if len(got) != 1 || got[0].Legacy || !got[0].Agent {
		t.Fatalf("%+v", got)
	}
	want := []Target{{User: "alex", Bell: true, Push: true}, {User: "sam", Push: true, Email: true}}
	if !slices.Equal(got[0].Targets, want) {
		t.Fatalf("targets %+v, want %+v", got[0].Targets, want)
	}
	// A plate and a face: administrators and accounts with the right
	// "names" on the camera only (sam watches live, nothing more).
	namesTo := func(want ...string) {
		t.Helper()
		r.advance(time.Hour)
		r.event("haustuer", "car", "Unser Auto", "K-TI 2026")
		r.event("haustuer", "person", "Anna", "")
		seen := 0
		for _, n := range r.take() {
			if n.Kind != KindPlate && n.Kind != KindFace {
				continue
			}
			seen++
			if !slices.Equal(users(n.Targets), want) {
				t.Errorf("%s went to %v, want %v", n.Kind, users(n.Targets), want)
			}
		}
		if seen != 2 {
			t.Errorf("%d plate and face messages, want 2", seen)
		}
	}
	namesTo("alex")
	cameras := []string{"haustuer", "garten"}
	// Recordings alone do not give the names.
	if err := r.m.Access.SetUser("sam", UserRights{"haustuer": {Live: true, Recordings: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	namesTo("alex")
	if err := r.m.Access.SetUser("sam", UserRights{"haustuer": {Live: true, Recordings: true, Names: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	namesTo("alex", "sam")
	if err := r.m.Access.SetUser("sam", UserRights{"haustuer": {Live: true}}, cameras); err != nil {
		t.Fatal(err)
	}
	namesTo("alex")
	// The rights could not be read at the start: nobody but administrators.
	r.m.Access.mu.Lock()
	r.m.Access.unreadable = true
	r.m.Access.mu.Unlock()
	r.advance(time.Hour)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 || !slices.Equal(users(got[0].Targets), []string{"alex"}) {
		t.Fatalf("rights unreadable: %+v", got)
	}
	r.m.Access.mu.Lock()
	r.m.Access.unreadable = false
	r.m.Access.mu.Unlock()
	// sam is deleted and made anew after his tick was given; the camera
	// even names him again after that: the tick is not the new sam's.
	r.mu.Lock()
	r.accounts["sam"] = Account{Name: "sam", Created: time.Now().Add(time.Hour)}
	r.mu.Unlock()
	r.set("haustuer", func(n *Notify) {
		n.Recipients[1].Added = time.Now().Add(2 * time.Hour)
	})
	r.advance(time.Hour)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 1 || !slices.Equal(users(got[0].Targets), []string{"alex"}) {
		t.Fatalf("the new sam inherits: %+v", got)
	}
	// Nobody left and no agent: nothing is routed at all.
	r.set("haustuer", func(n *Notify) {
		n.Agent = false
		n.Recipients = []Recipient{{User: "carl", Push: true, Added: chosen}}
	})
	r.advance(time.Hour)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 0 {
		t.Fatalf("routed to nobody: %+v", got)
	}
	// An empty list is nobody, never the old ways.
	r.set("haustuer", func(n *Notify) { n.Recipients = []Recipient{}; n.Titan = true })
	r.advance(time.Hour)
	r.event("haustuer", "person", "", "")
	if got := r.take(); len(got) != 0 {
		t.Fatalf("an empty list: %+v", got)
	}
}

// A camera of 0.78 (no list) keeps its old ways to the administrators.
func TestNotifyLegacyCamera(t *testing.T) {
	r := newNoticeRig(t)
	r.event("garten", "person", "", "")
	got := r.take()
	if len(got) != 1 || !got[0].Legacy || !got[0].Titan || !got[0].Push || got[0].Email || got[0].Targets != nil {
		t.Fatalf("%+v", got)
	}
	r.advance(59 * time.Second)
	r.event("garten", "person", "", "")
	if got := r.take(); len(got) != 0 {
		t.Fatal("the old pause of 60 s")
	}
	// Without any way, as in 0.78, nothing.
	r.set("garten", func(n *Notify) { n.Titan, n.Push = false, false })
	r.advance(time.Hour)
	r.event("garten", "person", "", "")
	if got := r.take(); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// Saving a camera refuses recipients that may not get its messages:
// unknown or locked accounts, accounts without a right on it, a bell for
// a plain account. What was stored before, or less of it, stays.
func TestCheckRecipients(t *testing.T) {
	r := newNoticeRig(t)
	ctx := context.Background()
	m := r.m
	update := func(edit func(n *Notify)) (Camera, error) {
		c, _ := m.Camera("haustuer")
		c.Source.Password = ""
		edit(&c.Notify)
		return m.UpdateCamera(ctx, "haustuer", c, CameraOptions{})
	}
	// alex with the bell; the time of the choice is the NAS's, not the request's.
	before := time.Now().UTC()
	c, err := update(func(n *Notify) {
		n.Recipients = []Recipient{{User: "alex", Bell: true, Added: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}}
	})
	if err != nil || len(c.Notify.Recipients) != 1 || c.Notify.Recipients[0].Added.Before(before) || !c.Notify.Titan {
		t.Fatalf("%+v %v", c.Notify, err)
	}
	added := c.Notify.Recipients[0].Added
	for _, tc := range []struct {
		name string
		r    Recipient
		want error
	}{
		{"unknown", Recipient{User: "dora", Push: true}, ErrRecipientUnknown},
		{"no right", Recipient{User: "carl", Push: true}, ErrRecipientNoRight},
		{"bell of sam", Recipient{User: "sam", Bell: true}, ErrRecipientBell},
		{"sam with push", Recipient{User: "sam", Push: true, Email: true}, nil},
	} {
		_, err := update(func(n *Notify) { n.Recipients = append(n.Recipients, tc.r) })
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	r.mu.Lock()
	r.accounts["mara"] = Account{Name: "mara", Disabled: true, Created: created}
	r.mu.Unlock()
	if _, err := update(func(n *Notify) { n.Recipients = append(n.Recipients, Recipient{User: "mara", Email: true}) }); !errors.Is(err, ErrRecipientUnknown) {
		t.Errorf("a locked account: %v", err)
	}
	c, _ = m.Camera("haustuer")
	if !slices.Equal(users(targetsOf(c.Notify.Recipients)), []string{"alex", "sam"}) || !c.Notify.Recipients[0].Added.Equal(added) {
		t.Fatalf("stored %+v", c.Notify.Recipients)
	}
	// sam loses his right: the camera can still be changed, sam stays
	// (and gets nothing); fewer ways are fine, more are not.
	if err := m.Access.SetUser("sam", UserRights{"haustuer": {}}, []string{"haustuer", "garten"}); err != nil {
		t.Fatal(err)
	}
	c, _ = m.Camera("haustuer")
	c.Source.Password = ""
	c.Name = "Haustür vorne"
	if _, err := m.UpdateCamera(ctx, "haustuer", c, CameraOptions{}); err != nil {
		t.Fatalf("renaming a camera whose recipient lost the right: %v", err)
	}
	if _, err := update(func(n *Notify) { n.Recipients[1].Email = false }); err != nil {
		t.Fatalf("fewer ways: %v", err)
	}
	if _, err := update(func(n *Notify) { n.Recipients[1].Email = true }); !errors.Is(err, ErrRecipientNoRight) {
		t.Fatalf("more ways without the right: %v", err)
	}
	// alex is deleted and made anew: his unchanged entry is dropped on the
	// next save, a new choice of him counts from now.
	r.mu.Lock()
	r.accounts["alex"] = Account{Name: "alex", Admin: true, Created: time.Now().UTC()}
	r.mu.Unlock()
	if c, err := update(func(*Notify) {}); err != nil || slices.Contains(users(targetsOf(c.Notify.Recipients)), "alex") || c.Notify.Titan {
		t.Fatalf("the earlier alex's entry: %+v %v", c.Notify, err)
	}
	if c, err := update(func(n *Notify) { n.Recipients = append(n.Recipients, Recipient{User: "alex", Push: true}) }); err != nil || !c.Notify.Recipients[0].Added.After(added) {
		t.Fatalf("alex chosen anew: %+v %v", c.Notify, err)
	}
	// A new camera: only administrators may be chosen (nobody has a tick on it).
	add := NewCamera()
	add.Name = "Carport"
	add.Source = Source{Kind: "rtsp", MainURL: "rtsp://192.168.1.70:554/main"}
	add.Notify.Recipients = []Recipient{{User: "olga", Bell: true, Push: true}, {User: "sam", Push: true}}
	if _, err := m.AddCamera(ctx, add, CameraOptions{}); !errors.Is(err, ErrRecipientNoRight) {
		t.Fatalf("a plain account on a new camera: %v", err)
	}
	add.Notify.Recipients = add.Notify.Recipients[:1]
	made, err := m.AddCamera(ctx, add, CameraOptions{})
	if err != nil || len(made.Notify.Recipients) != 1 || made.Notify.Recipients[0].Added.IsZero() {
		t.Fatalf("%+v %v", made.Notify, err)
	}
	// A deleted account goes off every camera.
	if err := m.ForgetRecipient("olga"); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.Camera(made.ID); len(c.Notify.Recipients) != 0 || c.Notify.Recipients == nil || c.Notify.Titan {
		t.Fatalf("olga stays: %+v", c.Notify)
	}
}

func targetsOf(list []Recipient) []Target {
	out := []Target{}
	for _, r := range list {
		out = append(out, Target{User: r.User})
	}
	return out
}
