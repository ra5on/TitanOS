package nvr

import (
	"errors"
	"slices"
	"testing"
)

func TestValidateSettings(t *testing.T) {
	mounted := []string{"tank"}
	ok := Settings{Enabled: true, Volume: "tank", Detector: DetectorSetting{Kind: DetectorAuto}, Storage: StorageSetting{LimitGB: 200}}
	if err := ValidateSettings(ok, mounted); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSettings(Defaults(), nil); err != nil {
		t.Fatalf("the defaults (off, no volume) must be valid: %v", err)
	}
	for name, edit := range map[string]func(*Settings){
		"unknown detector":   func(s *Settings) { s.Detector.Kind = "gpu" },
		"negative limit":     func(s *Settings) { s.Storage.LimitGB = -1 },
		"huge limit":         func(s *Settings) { s.Storage.LimitGB = MaxLimitGB + 1 },
		"volume not mounted": func(s *Settings) { s.Volume = "other" },
		"volume is a path":   func(s *Settings) { s.Volume = "../tank" },
		"on without volume":  func(s *Settings) { s.Volume = "" },
	} {
		s := ok
		edit(&s)
		if err := ValidateSettings(s, mounted); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestValidateCamera(t *testing.T) {
	good := testCamera("haustuer", "Haustür", nil)
	if good.Source.Host != "192.168.1.61" || good.Source.Port != 554 {
		t.Errorf("host and port of an RTSP camera come from its main stream: %+v", good.Source)
	}
	onvif := good
	onvif.Source.Kind, onvif.Source.Host, onvif.Source.ONVIFPort, onvif.PTZ = "onvif", "192.168.1.61", 0, true
	if c, err := ValidateCamera(onvif); err != nil || c.Source.ONVIFPort != 80 {
		t.Errorf("onvif: %v %+v", err, c.Source)
	}
	for name, edit := range map[string]func(*Camera){
		"id upper case":            func(c *Camera) { c.ID = "Haus" },
		"id reserved":              func(c *Camera) { c.ID = "discover" },
		"id is a stream name":      func(c *Camera) { c.ID = "haus_sub" },
		"id twoway":                func(c *Camera) { c.ID = "haus_twoway" },
		"id with dash":             func(c *Camera) { c.ID = "haus-tuer" },
		"id too long":              func(c *Camera) { c.ID = "a2345678901234567890123456789012x" },
		"empty name":               func(c *Camera) { c.Name = "" },
		"name with newline":        func(c *Camera) { c.Name = "a\nb" },
		"name with outer space":    func(c *Camera) { c.Name = " a" },
		"http stream":              func(c *Camera) { c.Source.MainURL = "http://192.168.1.61/video" },
		"exec source":              func(c *Camera) { c.Source.MainURL = "exec:ffmpeg -i x" },
		"credentials in address":   func(c *Camera) { c.Source.MainURL = "rtsp://u:p@192.168.1.61/x" },
		"fragment":                 func(c *Camera) { c.Source.MainURL = "rtsp://192.168.1.61/x#backchannel=1" },
		"brace":                    func(c *Camera) { c.Source.MainURL = "rtsp://192.168.1.61/{FRIGATE_RTSP_PASSWORD}" },
		"space":                    func(c *Camera) { c.Source.MainURL = "rtsp://192.168.1.61/a b" },
		"this host":                func(c *Camera) { c.Source.MainURL = "rtsp://127.0.0.1:8554/x" },
		"localhost":                func(c *Camera) { c.Source.MainURL = "rtsp://localhost/x" },
		"bad port":                 func(c *Camera) { c.Source.MainURL = "rtsp://192.168.1.61:99999/x" },
		"bad sub stream":           func(c *Camera) { c.Source.SubURL = "ftp://192.168.1.61/x" },
		"password without user":    func(c *Camera) { c.Source.User = "" },
		"newline in password":      func(c *Camera) { c.Source.Password = "a\nb" },
		"unknown kind":             func(c *Camera) { c.Source.Kind = "usb" },
		"ptz without onvif":        func(c *Camera) { c.PTZ = true },
		"onvif without host":       func(c *Camera) { c.Source.Kind, c.Source.Host = "onvif", "" },
		"unknown object":           func(c *Camera) { c.Detect.Objects = []string{"dragon"} },
		"detection without object": func(c *Camera) { c.Detect.Objects = nil },
		"plates without detection": func(c *Camera) { c.Detect.Enabled, c.Detect.Plates, c.Record.Mode = false, true, RecordMotion },
		"events without detection": func(c *Camera) { c.Detect.Enabled = false },
		"unknown mode":             func(c *Camera) { c.Record.Mode = "always" },
		"no days":                  func(c *Camera) { c.Record.RetainDays = 0 },
		"too many days":            func(c *Camera) { c.Record.RetainDays = 366 },
		"pre too long":             func(c *Camera) { c.Record.PreSeconds = 61 },
		"negative post":            func(c *Camera) { c.Record.PostSeconds = -1 },
		"two-way without sound":    func(c *Camera) { c.Audio.TwoWay = true },
		"unknown notify kind":      func(c *Camera) { c.Notify.Kinds = []string{"everything"} },
		"negative order":           func(c *Camera) { c.Order = -1 },
	} {
		c := good
		edit(&c)
		if _, err := ValidateCamera(c); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	// Lists come back in a fixed order and without doubles.
	c := good
	c.Detect.Objects = []string{"dog", "person", "dog"}
	c.Notify.Kinds = []string{"offline", "plate", "person"}
	c, err := ValidateCamera(c)
	if err != nil || !slices.Equal(c.Detect.Objects, []string{"person", "dog"}) || !slices.Equal(c.Notify.Kinds, []string{"person", "plate", "offline"}) {
		t.Errorf("%v %v %v", err, c.Detect.Objects, c.Notify.Kinds)
	}
}

func TestCameraOutsideTheHomeNetworkNeedsConfirmation(t *testing.T) {
	c := testCamera("a", "A", nil)
	c.Source.MainURL, c.Source.SubURL = "rtsp://203.0.113.9/live", ""
	if _, err := ValidateCamera(c); !errors.Is(err, ErrTargetNotAllowed) {
		t.Fatalf("a public address without confirmation: %v", err)
	}
	c.Source.AllowPublic = true
	if _, err := ValidateCamera(c); err != nil {
		t.Fatalf("confirmed: %v", err)
	}
	// A host name is resolved by Frigate; Tailscale counts as home.
	c.Source.AllowPublic = false
	for _, address := range []string{"rtsp://kamera.fritz.box/live", "rtsp://100.100.1.2/live", "rtsp://[fd00::5]/live"} {
		c.Source.MainURL = address
		if _, err := ValidateCamera(c); err != nil {
			t.Errorf("%s: %v", address, err)
		}
	}
}

func TestSlugID(t *testing.T) {
	for name, want := range map[string]string{
		"Haustür vorn": "haustuer_vorn",
		"  Garten!! ":  "garten",
		"2. Stock":     "cam_2_stock",
		"":             "cam",
		"Straße (Süd)": "strasse_sued",
		"Kamera_sub":   "kamera_sub_2",
		"discover":     "discover_2",
		"Ein sehr langer Name für eine Kamera im Garten": "ein_sehr_langer_name_fue",
	} {
		got := SlugID(name, nil)
		if got != want || !ValidCameraID(got) {
			t.Errorf("SlugID(%q) = %q, want %q", name, got, want)
		}
	}
	if got := SlugID("Garten", []string{"garten", "garten_2"}); got != "garten_3" {
		t.Errorf("taken: %q", got)
	}
}

func TestSplitCredentials(t *testing.T) {
	clean, user, password := SplitCredentials("rtsp://admin:p%40ss@192.168.1.5:554/live?x=1")
	if clean != "rtsp://192.168.1.5:554/live?x=1" || user != "admin" || password != "p@ss" {
		t.Errorf("%q %q %q", clean, user, password)
	}
	if clean, user, _ := SplitCredentials("rtsp://192.168.1.5/live"); clean != "rtsp://192.168.1.5/live" || user != "" {
		t.Errorf("%q %q", clean, user)
	}
}

func TestViewHidesThePassword(t *testing.T) {
	c := testCamera("a", "A", nil)
	view := c.View(CameraState{})
	if !view.Source.HasPassword || view.Source.User != "viewer" {
		t.Errorf("%+v", view.Source)
	}
}
