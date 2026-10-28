package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func loginSettings(change func(map[string]any)) map[string]any {
	v := map[string]any{
		"theme": "hell", "accent": "gruen", "customAccent": "#e5b92e", "style": "glass", "glass": 40, "blur": 12,
		"background": map[string]any{"source": "builtin", "id": "nordlicht", "fit": "cover", "x": 40, "y": 60, "zoom": 120, "dim": 20},
		"position":   "left", "width": "wide",
		"texts":    map[string]any{"de": map[string]string{"title": "Willkommen", "notice": "Nur für <b>Familie</b>"}},
		"hideHost": true, "hideLogo": false, "clock": true,
	}
	if change != nil {
		change(v)
	}
	return v
}

func testPNG(t *testing.T, w, h int, alpha uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 90, alpha})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func loginForm(t *testing.T, ts *testServer, client *http.Client, settings any, img []byte) (*http.Response, []byte) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	data, _ := json.Marshal(settings)
	_ = form.WriteField("settings", string(data))
	if img != nil {
		part, _ := form.CreateFormFile("image", "mine.png")
		_, _ = part.Write(img)
	}
	_ = form.Close()
	req, _ := http.NewRequest("PUT", ts.url+"/api/v1/login-appearance", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res, out
}

func TestLoginAppearanceDefaultsAndPublicFields(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	anonymous := ts.newClient()
	res, body := ts.do(anonymous, "GET", "/api/v1/login-appearance", nil)
	if res.StatusCode != 200 {
		t.Fatalf("public get: %d %s", res.StatusCode, body)
	}
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	if got["configured"] != false || got["theme"] != "inherit" || got["image"] != nil {
		t.Fatalf("defaults: %s", body)
	}
	if res, body := ts.request("PUT", "/api/v1/login-appearance", loginSettings(nil)); res.StatusCode != 200 {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	_, body = ts.do(anonymous, "GET", "/api/v1/login-appearance", nil)
	got = map[string]any{}
	_ = json.Unmarshal(body, &got)
	allowed := map[string]bool{"configured": true, "theme": true, "accent": true, "customAccent": true, "style": true, "glass": true, "blur": true, "background": true, "image": true, "position": true, "width": true, "texts": true, "hideHost": true, "hideLogo": true, "clock": true}
	for key := range got {
		if !allowed[key] {
			t.Errorf("public answer has %q", key)
		}
	}
	for _, secret := range []string{"alex", "version", "hostname", "ip"} {
		if strings.Contains(strings.ToLower(string(body)), `"`+secret) {
			t.Errorf("public answer mentions %s: %s", secret, body)
		}
	}
	if got["customAccent"] != "#E5B92E" || got["configured"] != true {
		t.Fatalf("stored: %s", body)
	}
	// The text is stored as given and only ever rendered as text.
	if !strings.Contains(string(body), "Nur für \\u003cb\\u003eFamilie") {
		t.Fatalf("notice: %s", body)
	}
	// hideHost also keeps the name out of the public session.
	_, body = ts.do(anonymous, "GET", "/api/v1/session", nil)
	if session(t, body).Hostname != "" {
		t.Fatal("hostname shown although hidden")
	}
	_, body = ts.request("GET", "/api/v1/session", nil)
	if host, _ := os.Hostname(); session(t, body).Hostname != host {
		t.Fatal("logged-in session lost the hostname")
	}
	// A restart reads the file again.
	fresh := New(ts.server.opts)
	value, exists := fresh.loginAppearance()
	if !exists || value.Theme != "hell" || value.Texts["de"].Title != "Willkommen" {
		t.Fatalf("reload: %+v", value)
	}
	// Back to the default look.
	if res, _ := ts.request("DELETE", "/api/v1/login-appearance", nil); res.StatusCode != 204 {
		t.Fatal("delete")
	}
	if _, err := ts.store.Read(loginAppearanceFile); err == nil {
		t.Fatal("file left")
	}
}

func TestLoginAppearanceValidation(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	bad := map[string]func(map[string]any){
		"theme":  func(v map[string]any) { v["theme"] = "eigen" },
		"accent": func(v map[string]any) { v["accent"] = "red" },
		"hex":    func(v map[string]any) { v["customAccent"] = "red;x" },
		"style":  func(v map[string]any) { v["style"] = "neon" },
		"glass":  func(v map[string]any) { v["glass"] = 95 },
		"blur":   func(v map[string]any) { v["blur"] = -1 },
		"motif":  func(v map[string]any) { v["background"].(map[string]any)["id"] = "../../users/alex/background" },
		"url":    func(v map[string]any) { v["background"].(map[string]any)["source"] = "url" },
		"id on theme": func(v map[string]any) {
			v["background"] = map[string]any{"source": "theme", "id": "x", "fit": "cover", "x": 1, "y": 1, "zoom": 100, "dim": 0}
		},
		"fit":      func(v map[string]any) { v["background"].(map[string]any)["fit"] = "fill" },
		"zoom":     func(v map[string]any) { v["background"].(map[string]any)["zoom"] = 250 },
		"dim":      func(v map[string]any) { v["background"].(map[string]any)["dim"] = 90 },
		"position": func(v map[string]any) { v["position"] = "top" },
		"width":    func(v map[string]any) { v["width"] = "huge" },
		"locale":   func(v map[string]any) { v["texts"] = map[string]any{"fr": map[string]string{"title": "x"}} },
		"long title": func(v map[string]any) {
			v["texts"] = map[string]any{"de": map[string]string{"title": strings.Repeat("a", 61)}}
		},
		"long notice": func(v map[string]any) {
			v["texts"] = map[string]any{"en": map[string]string{"notice": strings.Repeat("ä", 301)}}
		},
		"newline": func(v map[string]any) { v["texts"] = map[string]any{"en": map[string]string{"notice": "a\nb"}} },
		"unknown": func(v map[string]any) { v["script"] = "<script>" },
		"custom none": func(v map[string]any) {
			v["background"].(map[string]any)["source"] = "custom"
			delete(v["background"].(map[string]any), "id")
		},
	}
	for name, change := range bad {
		if res, body := ts.request("PUT", "/api/v1/login-appearance", loginSettings(change)); res.StatusCode != 400 {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
	if _, err := ts.store.Read(loginAppearanceFile); err == nil {
		t.Fatal("refused settings were written")
	}
	ok := loginSettings(func(v map[string]any) {
		v["texts"] = map[string]any{"en": map[string]string{"notice": strings.Repeat("ä", 300)}, "de": map[string]string{"title": "  "}}
	})
	if res, body := ts.request("PUT", "/api/v1/login-appearance", ok); res.StatusCode != 200 {
		t.Fatalf("limits: %d %s", res.StatusCode, body)
	}
	value, _ := ts.server.loginAppearance()
	if _, kept := value.Texts["de"]; kept || len([]rune(value.Texts["en"].Notice)) != 300 {
		t.Fatalf("texts: %+v", value.Texts)
	}
}

func TestLoginAppearanceNeedsAdministrator(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	anonymous := ts.newClient()
	if res, _ := ts.do(anonymous, "PUT", "/api/v1/login-appearance", loginSettings(nil)); res.StatusCode != 401 {
		t.Fatalf("anonymous save: %d", res.StatusCode)
	}
	if res, _ := ts.do(anonymous, "DELETE", "/api/v1/login-appearance", nil); res.StatusCode != 401 {
		t.Fatal("anonymous delete")
	}
	if _, err := ts.users.Create("sam", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	user := ts.newClient()
	if res, _ := ts.do(user, "POST", "/api/v1/login", credentials{Name: "sam", Password: testPassword}); res.StatusCode != 200 {
		t.Fatal("login")
	}
	if res, _ := ts.do(user, "PUT", "/api/v1/login-appearance", loginSettings(nil)); res.StatusCode != 403 {
		t.Fatalf("user save: %d", res.StatusCode)
	}
	if res, _ := loginForm(t, ts, user, loginSettings(nil), testPNG(t, 32, 32, 255)); res.StatusCode != 403 {
		t.Fatal("user upload")
	}
	if _, err := ts.users.Create("kim", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	admin := ts.newClient()
	ts.do(admin, "POST", "/api/v1/login", credentials{Name: "kim", Password: testPassword})
	if res, body := ts.do(admin, "PUT", "/api/v1/login-appearance", loginSettings(nil)); res.StatusCode != 200 {
		t.Fatalf("admin save: %d %s", res.StatusCode, body)
	}
}

func TestLoginAppearanceImage(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	custom := loginSettings(func(v map[string]any) {
		v["background"] = map[string]any{"source": "custom", "fit": "cover", "x": 50, "y": 50, "zoom": 100, "dim": 40}
	})
	// Not an image, too large, too small, a fake digest.
	if res, _ := loginForm(t, ts, ts.client, custom, []byte("<svg onload=alert(1)>")); res.StatusCode != 415 {
		t.Fatal("svg accepted")
	}
	if res, _ := loginForm(t, ts, ts.client, custom, append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, maxLoginImageBytes)...)); res.StatusCode != 413 {
		t.Fatal("size limit")
	}
	if res, _ := loginForm(t, ts, ts.client, custom, testPNG(t, 8, 8, 255)); res.StatusCode != 415 {
		t.Fatal("tiny image accepted")
	}
	if res, _ := loginForm(t, ts, ts.client, custom, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR broken")); res.StatusCode != 415 {
		t.Fatal("broken png accepted")
	}

	res, body := loginForm(t, ts, ts.client, custom, testPNG(t, 64, 40, 255))
	if res.StatusCode != 200 {
		t.Fatalf("upload: %d %s", res.StatusCode, body)
	}
	var saved loginAppearance
	_ = json.Unmarshal(body, &saved)
	if saved.Image == nil || saved.Image.Width != 64 || saved.Image.Height != 40 {
		t.Fatalf("meta: %s", body)
	}
	anonymous := ts.newClient()
	res, data := ts.do(anonymous, "GET", "/api/v1/login-appearance/background/"+saved.Image.ID, nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("serve: %d %v", res.StatusCode, res.Header)
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil || digest(data) != saved.Image.ID {
		t.Fatal("served image is not the re-encoded one")
	}
	// Only the configured image: no other files of the stick.
	for _, id := range []string{strings.Repeat("a", 64), "..%2F..%2Fusers%2Falex%2Faccount.json"} {
		if res, _ := ts.do(anonymous, "GET", "/api/v1/login-appearance/background/"+id, nil); res.StatusCode != 404 {
			t.Fatalf("other id %s served", id)
		}
	}
	// Saving again without an upload keeps the image; a transparent PNG stays PNG.
	if res, body := ts.request("PUT", "/api/v1/login-appearance", custom); res.StatusCode != 200 || !strings.Contains(string(body), saved.Image.ID) {
		t.Fatalf("keep: %d %s", res.StatusCode, body)
	}
	res, body = loginForm(t, ts, ts.client, custom, testPNG(t, 4000, 20, 100))
	if res.StatusCode != 200 {
		t.Fatalf("second upload: %d %s", res.StatusCode, body)
	}
	var second loginAppearance
	_ = json.Unmarshal(body, &second)
	if second.Image.Width != maxLoginImageEdge || second.Image.ID == saved.Image.ID {
		t.Fatalf("scaled: %+v", second.Image)
	}
	res, _ = ts.do(anonymous, "GET", "/api/v1/login-appearance/background/"+second.Image.ID, nil)
	if res.Header.Get("Content-Type") != "image/png" {
		t.Fatal("transparent image not PNG")
	}
	if res, _ := ts.do(anonymous, "GET", "/api/v1/login-appearance/background/"+saved.Image.ID, nil); res.StatusCode != 404 {
		t.Fatal("old image still served")
	}
	files, _ := filepath.Glob(filepath.Join(ts.store.Dir(), "system", "login-background-*"))
	if len(files) != 1 {
		t.Fatalf("stored images: %v", files)
	}
	// The image cannot be removed while it is in use; afterwards it can.
	inUse := loginSettings(func(v map[string]any) {
		v["background"] = map[string]any{"source": "custom", "fit": "cover", "x": 50, "y": 50, "zoom": 100, "dim": 40}
		v["removeImage"] = true
	})
	if res, _ := ts.request("PUT", "/api/v1/login-appearance", inUse); res.StatusCode != 400 {
		t.Fatal("removed image in use")
	}
	if res, _ := ts.request("PUT", "/api/v1/login-appearance", loginSettings(func(v map[string]any) { v["removeImage"] = true })); res.StatusCode != 200 {
		t.Fatal("remove")
	}
	files, _ = filepath.Glob(filepath.Join(ts.store.Dir(), "system", "login-background-*"))
	if len(files) != 0 {
		t.Fatalf("image left: %v", files)
	}
}

func TestLoginAppearanceDamagedFile(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	_ = ts.store.Write(loginAppearanceFile, []byte(`{"theme":"<script>","position":"left"}`))
	value, exists := New(ts.server.opts).loginAppearance()
	if exists || value.Theme != "inherit" || value.Position != "center" {
		t.Fatalf("damaged file used: %+v", value)
	}
}
