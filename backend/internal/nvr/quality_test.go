package nvr

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReadAndWriteQualityOverONVIF(t *testing.T) {
	c := &fakeCamera{user: "admin", password: "pw", paths: map[string]bool{}}
	startFakeCamera(t, c)
	p := loopbackProber(c)
	port := p.ONVIFPorts[0]

	list, o, err := p.ReadQuality(context.Background(), "127.0.0.1", port, "admin", "pw")
	if err != nil || o != OutcomeOK || len(list) != 1 {
		t.Fatalf("read: %+v %v %v", list, o, err)
	}
	q := list[0]
	if q.Token != "enc_main" || q.Codec != "H264" || q.Width != 2560 || q.FPS != 25 || q.BitrateKb != 6144 {
		t.Fatalf("setting: %+v", q)
	}
	if len(q.Resolutions) != 2 || q.FPSMax != 25 || q.BitrateMin != 256 || q.BitrateMax != 8192 {
		t.Fatalf("options: %+v", q)
	}

	next, _, err := p.WriteQuality(context.Background(), "127.0.0.1", port, "admin", "pw", QualityChange{Token: "enc_main", Width: 1280, Height: 720, BitrateKb: 2048})
	if err != nil || next.Width != 1280 || next.BitrateKb != 2048 || next.FPS != 25 {
		t.Fatalf("write: %+v %v", next, err)
	}
	for _, want := range []string{`token="enc_main"`, "<Width>1280</Width>", "<BitrateLimit>2048</BitrateLimit>", "<FrameRateLimit>25</FrameRateLimit>", "<H264Profile>High</H264Profile>", "<ForcePersistence>true", "<IPv4Address>239.0.0.1</IPv4Address>", "<SessionTimeout xmlns=\"http://www.onvif.org/ver10/schema\">PT30S</SessionTimeout>"} {
		if !strings.Contains(c.setEnc, want) {
			t.Errorf("request lacks %s:\n%s", want, c.setEnc)
		}
	}

	// Values outside what the camera offers never reach it.
	c.setEnc = ""
	for name, bad := range map[string]QualityChange{
		"resolution": {Token: "enc_main", Width: 1920, Height: 1080},
		"bit rate":   {Token: "enc_main", BitrateKb: 99999},
		"frame rate": {Token: "enc_main", FPS: 60},
		"stream":     {Token: "nope", BitrateKb: 1000},
	} {
		if _, _, err := p.WriteQuality(context.Background(), "127.0.0.1", port, "admin", "pw", bad); !errors.Is(err, ErrOutsideOptions) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if c.setEnc != "" {
		t.Fatal("an invalid change was sent to the camera")
	}
}

func TestQualityNeedsONVIFAndHomeNetwork(t *testing.T) {
	if _, _, err := (&Prober{}).ReadQuality(context.Background(), "192.168.1.5", 0, "a", "b"); !errors.Is(err, ErrQualityUnsupported) {
		t.Fatalf("no ONVIF port: %v", err)
	}
	if _, _, err := (&Prober{}).ReadQuality(context.Background(), "8.8.8.8", 80, "a", "b"); err == nil {
		t.Fatal("outside address accepted")
	}
}
