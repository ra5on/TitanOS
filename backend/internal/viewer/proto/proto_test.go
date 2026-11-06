package proto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseClientValid(t *testing.T) {
	for _, in := range []string{
		`{"t":"size","width":1280,"height":800,"scale":2}`,
		`{"t":"size","width":1280,"height":800}`,
		`{"t":"mouse","x":100,"y":50,"buttons":1}`,
		`{"t":"wheel","dx":0,"dy":-120}`,
		`{"t":"key","code":"KeyA","key":"a","down":true,"mods":{"shift":false,"ctrl":false,"alt":false,"meta":false}}`,
		`{"t":"text","text":"Hallo ä€😀"}`,
		`{"t":"clipboard","text":"x"}`,
		`{"t":"keyframe"}`,
		`{"t":"quality","mode":"auto","fps":60}`,
		`{"t":"upload","id":1,"name":"a.pdf","size":123}`,
		`{"t":"upload","id":2,"name":"big","size":3221225472}`, // too large: the agent answers too_large
	} {
		if _, err := ParseClient([]byte(in)); err != nil {
			t.Errorf("%s: %v", in, err)
		}
	}
	m, _ := ParseClient([]byte(`{"t":"size","width":10,"height":10}`))
	if m.Scale != 1 {
		t.Fatal("default scale")
	}
}

func TestParseClientInvalid(t *testing.T) {
	for _, in := range []string{
		``, `null`, `[]`, `{"t":"nope"}`, `{"t":"size","width":0,"height":800}`,
		`{"t":"size","width":100000,"height":800}`, `{"t":"size","width":100,"height":80,"scale":20}`,
		`{"t":"mouse","x":-5,"y":0}`, `{"t":"mouse","x":1,"y":1,"buttons":64}`,
		`{"t":"wheel","dy":1e9}`, `{"t":"key"}`, `{"t":"key","code":"` + strings.Repeat("A", 65) + `"}`,
		`{"t":"text","text":"` + strings.Repeat("a", MaxText+1) + `"}`,
		`{"t":"clipboard","text":"` + strings.Repeat("a", MaxClipboard+1) + `"}`,
		`{"t":"quality","mode":"ultra"}`, `{"t":"quality","fps":120}`,
		`{"t":"upload","id":0,"name":"a","size":1}`, `{"t":"upload","id":1,"name":"","size":1}`,
		`{"t":"upload","id":1,"name":"a","size":-1}`, "{\"t\":\"text\",\"text\":\"\xff\"}",
	} {
		if _, err := ParseClient([]byte(in)); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %.80s", in)
		}
	}
}

func TestBinaryToAgent(t *testing.T) {
	ok := append([]byte{TypeUpload, 0, 0, 0, 7}, make([]byte, MaxUploadChunk)...)
	id, p, err := ParseUploadChunk(ok)
	if err != nil || id != 7 || len(p) != MaxUploadChunk {
		t.Fatal(id, len(p), err)
	}
	for _, bad := range [][]byte{nil, {TypeUpload, 0, 0, 0, 1}, append([]byte{TypeVideo, 0, 0, 0, 1}, 1), append(ok, 0)} {
		if CheckBinaryToAgent(bad) == nil {
			t.Errorf("accepted len %d", len(bad))
		}
	}
}

func TestScreen(t *testing.T) {
	cases := []struct {
		w, h  int
		scale float64
		pw    int
		ph    int
		s     float64
	}{
		{1280, 800, 1, 1280, 800, 1},
		{1281, 801, 1, 1280, 800, 1},
		{1280, 800, 2, 2560, 1600, 2},
		{1280, 800, 1.1, 1280, 800, 1},
		{1000, 700, 1.25, 1250, 874, 1.25},
		{2560, 1440, 2, 3840, 2160, 1.5},
		{100, 100, 1, 320, 240, 1},
		{5000, 3000, 1, 3840, 2160, 1},
		{800, 600, 0, 800, 600, 1},
		{800, 600, 9, 2400, 1800, 3},
	}
	for _, c := range cases {
		pw, ph, s := Screen(c.w, c.h, c.scale)
		if pw != c.pw || ph != c.ph || s != c.s {
			t.Errorf("Screen(%d,%d,%v) = %d,%d,%v want %d,%d,%v", c.w, c.h, c.scale, pw, ph, s, c.pw, c.ph, c.s)
		}
		if pw%2 != 0 || ph%2 != 0 {
			t.Errorf("odd size %dx%d", pw, ph)
		}
	}
}

func TestHeaders(t *testing.T) {
	v := VideoHeader(true, 123456789)
	if len(v) != 10 || v[0] != TypeVideo || v[1] != VideoFlagKey || binary.BigEndian.Uint64(v[2:]) != 123456789 {
		t.Fatal(v)
	}
	if VideoHeader(false, 0)[1] != 0 {
		t.Fatal("key flag")
	}
	a := AudioHeader(42)
	if len(a) != 9 || a[0] != TypeAudio || binary.BigEndian.Uint64(a[1:]) != 42 {
		t.Fatal(a)
	}
}

func TestChromiumPolicy(t *testing.T) {
	b, err := ChromiumPolicy("http://172.18.0.2:8080")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckPolicy(b, "http://172.18.0.2:8080"); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"PasswordManagerEnabled": false, "SavingBrowserHistoryDisabled": true, "DownloadRestrictions": 1.0,
		"AllowFileSelectionDialogs": true, "DeveloperToolsAvailability": 2.0, "DownloadDirectory": "/profile/Downloads",
		"PromptForDownloadLocation": false, "BrowserSignin": 0.0, "SyncDisabled": true,
	} {
		if p[k] != want {
			t.Errorf("%s = %v, want %v", k, p[k], want)
		}
	}
	block := strings.Join(toStrings(p["URLBlocklist"]), " ")
	for _, s := range []string{"*", "file://*", "chrome://*", "devtools://*", "view-source:*", "javascript://*"} {
		if !strings.Contains(" "+block+" ", " "+s+" ") {
			t.Errorf("URLBlocklist lacks %q", s)
		}
	}
	if CheckPolicy(b, "http://172.18.0.3:8080") == nil {
		t.Fatal("policy accepted for another origin")
	}
	for _, bad := range []string{"", "http://x/path", "file:///etc", "javascript:alert(1)", "http://a,b", "http://u:p@h", "http://*"} {
		if _, err := ChromiumPolicy(bad); err == nil {
			t.Errorf("accepted origin %q", bad)
		}
	}
}

func TestCheckPolicyRejects(t *testing.T) {
	good, _ := ChromiumPolicy("https://app:8443")
	for _, bad := range []string{
		`{}`, `not json`,
		`{"URLBlocklist":["*"],"URLAllowlist":["https://app:8443","about:blank"],"DeveloperToolsAvailability":2}`,
		strings.Replace(string(good), `"about:blank"`, `"about:blank","*"`, 1),
		strings.Replace(string(good), `"DeveloperToolsAvailability": 2`, `"DeveloperToolsAvailability": 0`, 1),
	} {
		if CheckPolicy([]byte(bad), "https://app:8443") == nil {
			t.Errorf("accepted %.60s", bad)
		}
	}
}

func TestAppOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"http://172.18.0.2:8080": "http://172.18.0.2:8080", "https://app:8443/path?x=1": "https://app:8443",
		"http://[fd00::1]:80/": "http://[fd00::1]:80",
	} {
		if got, err := AppOrigin(in); err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
