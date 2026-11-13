package nvr

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// camera078 is a camera as 0.78 wrote it to /config/nvr/cameras.json:
// without streams, recipients, pause or window.
const camera078 = `{"id":"einfahrt","name":"Einfahrt","enabled":true,
 "source":{"kind":"rtsp","host":"192.168.1.61","port":554,"user":"viewer","password":"geheim",
  "mainURL":"rtsp://192.168.1.61:554/Streaming/Channels/101","subURL":"rtsp://192.168.1.61:554/Streaming/Channels/102","onvifPort":0},
 "detect":{"enabled":true,"objects":["person","car"],"plates":false,"faces":false},
 "record":{"mode":"events","retainDays":14,"preSeconds":5,"postSeconds":5},
 "audio":{"enabled":true,"twoWay":false,"codec":"PCMA"},
 "ptz":false,
 "notify":{"titan":true,"push":true,"email":false,"homeAssistant":false,"agent":false,"kinds":["person","offline"]},
 "order":1}`

// TestCamera078KeepsItsBehaviour: a camera of 0.78 records the main
// stream, detects on the sub stream, shows the sub stream in small tiles,
// keeps its streams open, tells the administrators the old ways with the
// old pause of 60 s and reports at any time (PLAN 82).
func TestCamera078KeepsItsBehaviour(t *testing.T) {
	var c Camera
	if err := json.Unmarshal([]byte(camera078), &c); err != nil {
		t.Fatal(err)
	}
	c, err := ValidateCamera(c)
	if err != nil {
		t.Fatalf("a camera of 0.78 is refused: %v", err)
	}
	if c.recordStream() != StreamMain || c.detectStream() != StreamSub {
		t.Errorf("record %s, detect %s; want main, sub", c.recordStream(), c.detectStream())
	}
	if c.liveStream(true) != StreamSub || c.liveStream(false) != StreamMain {
		t.Errorf("live small %s, large %s; want sub, main", c.liveStream(true), c.liveStream(false))
	}
	if c.Streams.NoPreload {
		t.Error("a camera of 0.78 must keep its streams open")
	}
	if c.recordCodec() != "PCMA" {
		t.Errorf("record codec %q, want the main stream's PCMA", c.recordCodec())
	}
	if !c.Notify.legacyWays() || !c.Notify.Titan || !c.Notify.Push {
		t.Errorf("notify %+v: a camera of 0.78 keeps its ways", c.Notify)
	}
	if c.Notify.pause() != 60*time.Second {
		t.Errorf("pause %v, want 60 s", c.Notify.pause())
	}
	if !c.Notify.Window.Open(time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)) {
		t.Error("a camera of 0.78 reports at any time")
	}
	// Stored again by this version and read back: still the old ways.
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var again Camera
	if err := json.Unmarshal(data, &again); err != nil {
		t.Fatal(err)
	}
	if !again.Notify.legacyWays() {
		t.Errorf("stored again, the camera lost its old ways: %s", data)
	}
}

// TestSettings078 are settings of 0.78: messages on, the decoder chosen
// by TitanOS.
func TestSettings078(t *testing.T) {
	var s Settings
	if err := json.Unmarshal([]byte(`{"enabled":true,"volume":"daten","detector":{"kind":"auto"},"storage":{"limitGB":0},"privacyNoticeDismissed":true,"rtspOut":false}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.NotifyOff {
		t.Error("settings of 0.78 must keep the messages on")
	}
	if err := ValidateSettings(s, []string{"daten"}); err != nil {
		t.Errorf("settings of 0.78 refused: %v", err)
	}
	for _, decoder := range []string{"", "auto", "cpu", "0000:00:02.0", "0000:03:00.0"} {
		s.Decoder = decoder
		if err := ValidateSettings(s, []string{"daten"}); err != nil {
			t.Errorf("decoder %q refused: %v", decoder, err)
		}
	}
	for _, decoder := range []string{"/dev/dri/renderD128", "nvidia", "0000:00:02", "igpu"} {
		s.Decoder = decoder
		if err := ValidateSettings(s, []string{"daten"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("decoder %q taken", decoder)
		}
	}
}

// TestEmptyRecipientsStayNobody: an empty list is nobody, also after
// it was stored and read back; it never turns into the old ways.
func TestEmptyRecipientsStayNobody(t *testing.T) {
	c := testCamera("tor", "Tor", func(c *Camera) {
		c.Notify.Recipients = []Recipient{}
		c.Notify.Titan, c.Notify.Push = true, true
	})
	if c.Notify.legacyWays() || c.Notify.Titan || c.Notify.Push {
		t.Fatalf("notify %+v: no recipients is nobody", c.Notify)
	}
	data, _ := json.Marshal(c)
	var again Camera
	if err := json.Unmarshal(data, &again); err != nil {
		t.Fatal(err)
	}
	again, err := ValidateCamera(again)
	if err != nil || again.Notify.legacyWays() {
		t.Fatalf("stored and read back: %v, recipients %#v (%s)", err, again.Notify.Recipients, data)
	}
}

// TestRecipientsKeepTheWayBack: the ways of 0.78 say whether some
// recipient has a way, so that 0.78 still tells when it runs again.
func TestRecipientsKeepTheWayBack(t *testing.T) {
	c := testCamera("tor", "Tor", func(c *Camera) {
		c.Notify.Recipients = []Recipient{{User: "ben", Push: true}, {User: "anna", Bell: true, Email: true}}
	})
	n := c.Notify
	if !n.Titan || !n.Push || !n.Email {
		t.Errorf("ways %+v: some recipient has each", n)
	}
	if n.Recipients[0].User != "anna" {
		t.Errorf("recipients are kept sorted: %+v", n.Recipients)
	}
	for _, bad := range [][]Recipient{
		{{User: "anna"}, {User: "anna"}},
		{{User: "Anna"}},
		{{User: ""}},
		{{User: "../x"}},
	} {
		c.Notify.Recipients = bad
		if _, err := ValidateCamera(c); !errors.Is(err, ErrInvalid) {
			t.Errorf("recipients %+v taken", bad)
		}
	}
}

func TestPauseChoices(t *testing.T) {
	for _, seconds := range []int{0, 60, 300, 900, 1800, 3600} {
		c := testCamera("tor", "Tor", func(c *Camera) { c.Notify.PauseSeconds = &seconds })
		if c.Notify.pause() != time.Duration(seconds)*time.Second {
			t.Errorf("pause %d: %v", seconds, c.Notify.pause())
		}
	}
	c := testCamera("tor", "Tor", nil)
	for _, seconds := range []int{-1, 1, 59, 120, 7200} {
		c.Notify.PauseSeconds = &seconds
		if _, err := ValidateCamera(c); !errors.Is(err, ErrInvalid) {
			t.Errorf("pause %d taken", seconds)
		}
	}
}

func TestStreamChoices(t *testing.T) {
	c := testCamera("tor", "Tor", func(c *Camera) {
		c.Streams = StreamUse{Record: StreamSub, Detect: StreamMain, Live: StreamMain, NoPreload: true}
		c.Audio.Codec, c.Audio.SubCodec = "PCMA", "MPEG4-GENERIC"
	})
	if c.recordStream() != StreamSub || c.detectStream() != StreamMain || c.liveStream(true) != StreamMain {
		t.Errorf("record %s detect %s live %s", c.recordStream(), c.detectStream(), c.liveStream(true))
	}
	if c.recordCodec() != "MPEG4-GENERIC" {
		t.Errorf("record codec %q: the sub stream is recorded", c.recordCodec())
	}
	c.Streams = StreamUse{Live: StreamSub}
	if c.liveStream(false) != StreamSub {
		t.Error("live always on the sub stream")
	}
	// Without a sub stream everything is the main stream; asking for the
	// sub stream to record or show is refused.
	single := testCamera("hof", "Hof", func(c *Camera) { c.Source.SubURL = "" })
	if single.detectStream() != StreamMain || single.liveStream(true) != StreamMain {
		t.Error("a camera with one stream uses it for everything")
	}
	for _, use := range []StreamUse{{Record: StreamSub}, {Live: StreamSub}, {Record: "low"}, {Detect: "auto"}, {Live: "small"}} {
		single.Streams = use
		if _, err := ValidateCamera(single); !errors.Is(err, ErrInvalid) {
			t.Errorf("streams %+v taken for a camera with one stream", use)
		}
	}
}

func TestWindow(t *testing.T) {
	at := func(day time.Weekday, hour, minute int) time.Time {
		// 2026-11-16 is a Monday.
		return time.Date(2026, 11, 16+(int(day)+6)%7, hour, minute, 0, 0, time.UTC)
	}
	night := Window{On: true, Days: []int{1, 2, 3, 4, 5}, From: "22:00", To: "06:00"}
	cases := []struct {
		w    Window
		t    time.Time
		open bool
	}{
		{night, at(time.Monday, 23, 0), true},
		{night, at(time.Tuesday, 5, 59), true},   // the night that began on Monday
		{night, at(time.Monday, 5, 0), false},    // the night that began on Sunday: Sunday is not chosen
		{night, at(time.Saturday, 3, 0), true},   // began on Friday
		{night, at(time.Saturday, 23, 0), false}, // Saturday not chosen
		{night, at(time.Monday, 12, 0), false},
		{night, at(time.Tuesday, 6, 0), false},
		{Window{On: true, Days: []int{6, 7}, From: "00:00", To: "00:00"}, at(time.Sunday, 13, 0), true},
		{Window{On: true, Days: []int{6, 7}, From: "00:00", To: "00:00"}, at(time.Monday, 13, 0), false},
		{Window{On: true, Days: []int{1}, From: "08:00", To: "17:30"}, at(time.Monday, 17, 29), true},
		{Window{On: true, Days: []int{1}, From: "08:00", To: "17:30"}, at(time.Monday, 17, 30), false},
		{Window{On: false, Days: []int{1}, From: "08:00", To: "09:00"}, at(time.Friday, 20, 0), true},
	}
	for i, tc := range cases {
		if got := tc.w.Open(tc.t); got != tc.open {
			t.Errorf("case %d (%s %s): open %v, want %v", i, tc.t.Weekday(), tc.t.Format("15:04"), got, tc.open)
		}
	}
	c := testCamera("tor", "Tor", nil)
	for _, bad := range []Window{
		{On: true, Days: []int{}, From: "22:00", To: "06:00"},
		{On: true, Days: []int{8}, From: "22:00", To: "06:00"},
		{On: true, Days: []int{1}, From: "24:00", To: "06:00"},
		{On: true, Days: []int{1}, From: "7:00", To: "08:00"},
	} {
		c.Notify.Window = bad
		if _, err := ValidateCamera(c); !errors.Is(err, ErrInvalid) {
			t.Errorf("window %+v taken", bad)
		}
	}
}
