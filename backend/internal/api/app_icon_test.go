package api

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func TestAppIcon(t *testing.T) {
	ts := newTestServer(t)
	if res, _ := ts.request("PUT", "/api/v1/app-icon", map[string]any{"kind": "color", "color": "#ff0000"}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
	// Without a choice the built-in file answers (here: not in the test UI, so index.html).
	if res, body := ts.request("GET", "/icons/app-192.png", nil); res.StatusCode != 200 || res.Header.Get("Content-Type") == "image/png" {
		t.Fatalf("default: %d %s %q", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/app-icon", map[string]any{"kind": "color", "color": "red"}); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid_app_icon") {
		t.Fatalf("bad color: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/app-icon", map[string]any{"kind": "color", "color": "#1F2A44"}); res.StatusCode != 200 || !strings.Contains(string(body), `"color":"#1f2a44"`) {
		t.Fatalf("color: %d %s", res.StatusCode, body)
	}
	// The drawn symbol needs no sign-in: home screens fetch it without.
	anonymous := ts.newClient()
	for name, size := range map[string]int{"app-180.png": 180, "app-512.png": 512} {
		res, body := ts.do(anonymous, "GET", "/icons/"+name, nil)
		img, err := png.Decode(bytes.NewReader(body))
		if res.StatusCode != 200 || err != nil || img.Bounds().Dx() != size {
			t.Fatalf("%s: %d %v", name, res.StatusCode, err)
		}
		// Dark background: the logo is white, the corner keeps the color.
		if r, g, b, _ := img.At(2, 2).RGBA(); r>>8 != 0x1f || g>>8 != 0x2a || b>>8 != 0x44 {
			t.Fatalf("%s corner %d %d %d", name, r>>8, g>>8, b>>8)
		}
		if r, _, _, _ := img.At(size/2, size*11/32).RGBA(); r>>8 < 0xf0 {
			t.Fatalf("%s logo not white on a dark color", name)
		}
	}
	// An own picture: cropped to its middle square.
	wide := image.NewRGBA(image.Rect(0, 0, 300, 100))
	for x := range 300 {
		for y := range 100 {
			c := color.RGBA{R: 255, A: 255}
			if x >= 100 && x < 200 {
				c = color.RGBA{G: 255, A: 255}
			}
			wide.Set(x, y, c)
		}
	}
	var upload bytes.Buffer
	jpeg.Encode(&upload, wide, &jpeg.Options{Quality: 95})
	req, _ := http.NewRequest("PUT", ts.url+"/api/v1/app-icon", bytes.NewReader(upload.Bytes()))
	req.Header.Set("Content-Type", "image/jpeg")
	res, err := ts.client.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("upload: %v %d", err, res.StatusCode)
	}
	_, body := ts.do(anonymous, "GET", "/icons/app-192.png", nil)
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, _, _ := img.At(10, 96).RGBA(); g>>8 < 0xd0 || r>>8 > 0x30 {
		t.Fatal("not cropped to the middle")
	}
	req, _ = http.NewRequest("PUT", ts.url+"/api/v1/app-icon", strings.NewReader("<svg/>"))
	req.Header.Set("Content-Type", "image/svg+xml")
	if res, _ := ts.client.Do(req); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("svg accepted: %d", res.StatusCode)
	}
	// Back to the default: the built-in files answer again, the picture is gone.
	ts.request("PUT", "/api/v1/app-icon", map[string]any{"kind": "default"})
	if res, _ := ts.do(anonymous, "GET", "/icons/app-192.png", nil); res.Header.Get("Content-Type") == "image/png" {
		t.Fatal("drawn symbol after going back to the default")
	}
	if _, err := ts.store.Read(appIconImage); err == nil {
		t.Fatal("own picture kept")
	}
}
