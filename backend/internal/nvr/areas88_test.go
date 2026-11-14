package nvr

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

// PLAN 88: zones and masks are checked on the server: points, ranges,
// names, how many.
func TestZonesAndMasksAreChecked(t *testing.T) {
	square := []Point{{0.1, 0.1}, {0.4, 0.1}, {0.4, 0.4}, {0.1, 0.4}}
	ok := func(edit func(c *Camera)) (Camera, error) {
		return ValidateCamera(testCamera("hof", "Hof", nil).withEdit(edit))
	}
	got, err := ok(func(c *Camera) {
		c.Zones = []Zone{{Name: "Haustür vorn", Points: []Point{{0.12345, 0.1}, {0.4, 0.1}, {0.4, 0.4}, {0.1, 0.4}, {0.1, 0.4}}}}
	})
	if err != nil {
		t.Fatal(err)
	}
	z := got.Zones[0]
	if z.ID != "haustuer_vorn" || len(z.Points) != 4 || z.Points[0][0] != 0.123 {
		t.Errorf("stored zone %+v", z)
	}
	for name, edit := range map[string]func(c *Camera){
		"two points":      func(c *Camera) { c.Zones = []Zone{{Name: "A", Points: square[:2]}} },
		"too many points": func(c *Camera) { c.Zones = []Zone{{Name: "A", Points: slices.Repeat(square, 9)}} },
		"outside":         func(c *Camera) { c.Zones = []Zone{{Name: "A", Points: []Point{{0, 0}, {1.2, 0}, {1, 1}}}} },
		"negative":        func(c *Camera) { c.Masks.Motion = []Mask{{Points: []Point{{-0.1, 0}, {1, 0}, {1, 1}}}} },
		"not a number":    func(c *Camera) { c.Masks.Objects = []Mask{{Points: []Point{{math.NaN(), 0}, {1, 0}, {1, 1}}}} },
		"a line":          func(c *Camera) { c.Zones = []Zone{{Name: "A", Points: []Point{{0, 0}, {0.5, 0.5}, {1, 1}}}} },
		"no name":         func(c *Camera) { c.Zones = []Zone{{Name: "", Points: square}} },
		"long name":       func(c *Camera) { c.Zones = []Zone{{Name: strings.Repeat("x", 41), Points: square}} },
		"control":         func(c *Camera) { c.Zones = []Zone{{Name: "A\nB", Points: square}} },
		"same name":       func(c *Camera) { c.Zones = []Zone{{Name: "Weg", Points: square}, {Name: "weg", Points: square}} },
		"bad id":          func(c *Camera) { c.Zones = []Zone{{ID: "Weg-1", Name: "Weg", Points: square}} },
		"same id": func(c *Camera) {
			c.Zones = []Zone{{ID: "a", Name: "A", Points: square}, {ID: "a", Name: "B", Points: square}}
		},
		"too many zones": func(c *Camera) {
			for i := range MaxZones + 1 {
				c.Zones = append(c.Zones, Zone{Name: "Z" + string(rune('a'+i)), Points: square})
			}
		},
		"too many masks": func(c *Camera) { c.Masks.Motion = slices.Repeat([]Mask{{Points: square}}, MaxMasks+1) },
		"only, no zone":  func(c *Camera) { c.ZonesOnly = true },
		"messages to none": func(c *Camera) {
			c.Zones = []Zone{{ID: "a", Name: "A", Points: square}}
			c.Notify.Zones = []string{"b"}
		},
	} {
		if _, err := ok(edit); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A zone ID is never a camera ID in Frigate.
	if zoneKey("einfahrt") == "einfahrt" || ValidCameraID(zoneKey("einfahrt")) || zoneOfKey("z-einfahrt") != "einfahrt" || zoneOfKey("einfahrt") != "" {
		t.Error("zone keys")
	}
}

func (c Camera) withEdit(edit func(*Camera)) Camera {
	edit(&c)
	return c
}

// Frigate reads a number above "1.0" (as text) as a pixel: every number
// is written in its shortest form with at most 3 places.
func TestCoordinatesAreWrittenAsFrigateReadsThem(t *testing.T) {
	for v, want := range map[float64]string{0: "0", 1: "1", 0.5: "0.5", 0.1234: "0.123", 0.9996: "1", 0.00001: "0", 0.3: "0.3", 1.0 / 3: "0.333"} {
		got := formatCoordinate(v)
		if got != want || got > "1.0" || strings.ContainsAny(got, "eE") {
			t.Errorf("%v → %q, want %q", v, got, want)
		}
	}
}

// Zones, masks and "events only in zones" in config.yml (the golden
// file extras.yml, checked with Frigate's own parser in the image).
func TestZonesInConfig(t *testing.T) {
	tc := renderCases()["extras"]
	out, err := Render(tc.settings, tc.cameras, tc.deleted, tc.hw)
	if err != nil {
		t.Fatal(err)
	}
	cfg := parse(t, out)
	cam := dig(t, cfg, "cameras", "einfahrt")
	if got := dig(t, cam, "zones", "z-einfahrt", "coordinates"); got != "0,0.5,0.6,0.45,1,1,0,1" {
		t.Errorf("zone %v", got)
	}
	if got := dig(t, cam, "zones", "z-tor", "friendly_name"); got != "Tor & Weg" {
		t.Errorf("name %v", got)
	}
	for _, path := range [][]string{{"review", "alerts", "required_zones"}, {"review", "detections", "required_zones"}, {"snapshots", "required_zones"}} {
		if got := dig(t, cam, path...); len(got.([]any)) != 2 {
			t.Errorf("%v: %v", path, got)
		}
	}
	if got := dig(t, cam, "motion", "mask", "mask_1", "coordinates"); got != "0,0,0.3,0,0.3,0.06,0,0.06" {
		t.Errorf("motion mask %v", got)
	}
	if got := dig(t, cam, "objects", "mask", "mask_1", "coordinates"); got != "0.5,0,1,0,1,0.2" {
		t.Errorf("object mask %v", got)
	}
	// Zones without "only": no required zones; no masks: no keys.
	garden := dig(t, cfg, "cameras", "garten").(map[string]any)
	if _, ok := garden["review"]; ok {
		t.Error("garten has required zones")
	}
	if _, ok := dig(t, garden, "snapshots").(map[string]any)["required_zones"]; ok {
		t.Error("garten snapshots need zones")
	}
	plain := renderCases()["npu"]
	out, _ = Render(plain.settings, plain.cameras, nil, plain.hw)
	for _, key := range []string{"zones:", "mask:", "required_zones", "review:"} {
		if strings.Contains(string(out), key) {
			t.Errorf("a camera of 0.80 renders %s", key)
		}
	}
}

// A camera file of 0.80 reads as before (no zones, no masks), and one of
// 0.81 is still a camera 0.80 reads (its fields are only added).
func TestCameraFilesOf080And081(t *testing.T) {
	old := `{"id":"hof","name":"Hof","enabled":true,"source":{"kind":"rtsp","host":"192.168.1.5","port":554,"user":"","mainURL":"rtsp://192.168.1.5/main","subURL":"","onvifPort":0},
	"streams":{},"detect":{"enabled":true,"objects":["person"],"plates":false,"faces":false},"record":{"mode":"events","retainDays":14,"preSeconds":5,"postSeconds":5},
	"audio":{"enabled":false,"twoWay":false},"ptz":false,"notify":{"titan":true,"push":false,"email":false,"homeAssistant":false,"agent":false,"kinds":["person","offline"],"recipients":null,"window":{"on":false,"days":[],"from":"","to":""}},"order":1}`
	var c Camera
	if err := json.Unmarshal([]byte(old), &c); err != nil {
		t.Fatal(err)
	}
	valid, err := ValidateCamera(c)
	if err != nil || valid.Zones != nil || valid.ZonesOnly || len(valid.Masks.Motion) != 0 || valid.Notify.FaceUnknown || valid.Notify.Zones != nil {
		t.Fatalf("0.80 camera: %+v %v", valid, err)
	}
	data, _ := json.Marshal(valid)
	for _, key := range []string{`"zones"`, `"masks"`, `"zonesOnly"`, `"faceUnknown"`, `"recordDays"`, `"eventDays"`} {
		if strings.Contains(string(data), key) {
			t.Errorf("a 0.80 camera is written with %s", key)
		}
	}
	// 0.81 with everything: the kinds and days 0.80 checks stay in its range.
	tc := renderCases()["extras"].cameras[0]
	data, _ = json.Marshal(tc)
	var as080 struct {
		Record struct {
			RetainDays int `json:"retainDays"`
		} `json:"record"`
		Notify struct {
			Kinds []string `json:"kinds"`
		} `json:"notify"`
	}
	if err := json.Unmarshal(data, &as080); err != nil {
		t.Fatal(err)
	}
	for _, kind := range as080.Notify.Kinds {
		if !slices.Contains(append(slices.Clone(allowedObjects), "plate", "face", "offline"), kind) {
			t.Errorf("0.80 would refuse kind %q", kind)
		}
	}
	if as080.Record.RetainDays != 30 {
		t.Errorf("retainDays %d", as080.Record.RetainDays)
	}
}

// Events show the names of the zones the object entered; a camera that
// shows only events in its zones lists no others, also across pages.
func TestEventsInZones(t *testing.T) {
	r := seeded(t)
	ctx := context.Background()
	c, _ := r.m.Camera("haustuer")
	c.Zones = []Zone{{ID: "weg", Name: "Weg zur Tür", Points: []Point{{0, 0}, {1, 0}, {1, 1}}}}
	if _, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{}); err != nil {
		t.Fatal(err)
	}
	// Half of the events entered the zone, one a zone that is gone.
	r.frigate.Set(func(f *DevFrigate) {
		n := 0
		for i, e := range f.data.events {
			if e.Camera != "haustuer" {
				continue
			}
			switch n % 3 {
			case 0:
				f.data.events[i].Zones = []string{"z-weg"}
			case 1:
				f.data.events[i].Zones = []string{"z-alt"}
			}
			n++
		}
	})
	all, _, err := r.m.Events(ctx, EventFilter{Cameras: []string{"haustuer"}, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	inZone := 0
	for _, e := range all {
		if slices.Equal(e.Zones, []string{"Weg zur Tür"}) {
			inZone++
		} else if len(e.Zones) != 0 {
			t.Errorf("zones %v", e.Zones)
		}
	}
	if inZone == 0 || inZone == len(all) {
		t.Fatalf("%d of %d in the zone", inZone, len(all))
	}
	c.ZonesOnly = true
	if _, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{}); err != nil {
		t.Fatal(err)
	}
	only, _, _ := r.m.Events(ctx, EventFilter{Cameras: []string{"haustuer"}, Limit: 200})
	if len(only) != inZone {
		t.Errorf("only in zones: %d, want %d", len(only), inZone)
	}
	// Pages of one event: none is lost, none twice, filtered ones skipped.
	var paged []string
	before := 0.0
	for range 50 {
		page, next, err := r.m.Events(ctx, EventFilter{Cameras: []string{"haustuer"}, Limit: 1, Before: before})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			paged = append(paged, e.ID)
		}
		if before = next; next == 0 {
			break
		}
	}
	if len(paged) != inZone || len(slices.Compact(slices.Clone(paged))) != inZone {
		t.Errorf("paged %v, want %d", paged, inZone)
	}
}

// Messages: an object that entered no zone of a camera that shows only
// events in zones is not told – until it enters one (the check comes
// before "once"); messages limited to zones wait for one of them.
func TestMessagesInZones(t *testing.T) {
	r := newNoticeRig(t)
	square := []Point{{0, 0}, {0.5, 0}, {0.5, 0.5}, {0, 0.5}}
	r.m.mu.Lock()
	r.m.cameras[0].Zones = []Zone{{ID: "weg", Name: "Weg", Points: square}, {ID: "beet", Name: "Beet", Points: square}}
	r.m.cameras[0].ZonesOnly = true
	r.m.mu.Unlock()
	r.set("haustuer", func(n *Notify) {
		n.Recipients = []Recipient{{User: "alex", Bell: true, Added: created.Add(time.Hour)}}
	})
	e := Event{ID: "1731412345.1-zone", Camera: "haustuer", Label: "person", Start: float64(r.now.Unix())}
	route := func(keys ...string) int {
		e.Zones = keys
		r.m.placeEvent(&e)
		r.m.routeEvent(e, "update")
		return len(r.take())
	}
	if n := route(); n != 0 {
		t.Errorf("outside the zones: %d", n)
	}
	if n := route("z-beet"); n != 1 {
		t.Errorf("entered a zone: %d", n)
	}
	if n := route("z-beet"); n != 0 {
		t.Errorf("twice: %d", n)
	}
	// Only "Weg" tells.
	r.set("haustuer", func(n *Notify) { n.Zones = []string{"weg"} })
	r.advance(time.Hour)
	e.ID = "1731412345.2-zone"
	if n := route("z-beet"); n != 0 {
		t.Errorf("other zone: %d", n)
	}
	if n := route("z-beet", "z-weg"); n != 1 {
		t.Errorf("chosen zone: %d", n)
	}
	// Without "only", messages limited to zones still wait for one.
	r.m.mu.Lock()
	r.m.cameras[0].ZonesOnly = false
	r.m.mu.Unlock()
	r.advance(time.Hour)
	e.ID = "1731412345.3-zone"
	if n := route(); n != 0 {
		t.Errorf("limited, no zone: %d", n)
	}
	r.set("haustuer", func(n *Notify) { n.Zones = nil })
	if n := route(); n != 1 {
		t.Errorf("whole picture: %d", n)
	}
}

// The request decodes over the stored camera: zones and masks are drawn
// as a whole (no point of a stored polygon stays behind).
func TestStoredZonesRoundTrip(t *testing.T) {
	r := newRig(t, true)
	r.up()
	ctx := context.Background()
	c, _ := r.m.Camera("haustuer")
	c.Zones = []Zone{{Name: "Weg", Points: []Point{{0.1, 0.1}, {0.9, 0.1}, {0.5, 0.9}}}}
	c.Masks.Objects = []Mask{{Points: []Point{{0, 0}, {1, 0}, {1, 0.1}}}}
	saved, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Zones[0].ID != "weg" {
		t.Fatalf("id %q", saved.Zones[0].ID)
	}
	r.look()
	if cfg := r.config(); !strings.Contains(cfg, "z-weg:") || !strings.Contains(cfg, "0.1,0.1,0.9,0.1,0.5,0.9") {
		t.Errorf("config.yml without the zone:\n%s", cfg)
	}
	// Renamed: the ID stays (events and messages keep their zone).
	saved.Zones[0].Name = "Gartenweg"
	again, err := r.m.UpdateCamera(ctx, "haustuer", saved, CameraOptions{})
	if err != nil || again.Zones[0].ID != "weg" || again.Zones[0].Name != "Gartenweg" {
		t.Errorf("renamed: %+v %v", again.Zones, err)
	}
}
