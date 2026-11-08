package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func pictureTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func loginPicture(ts *testServer, id, remote string) int {
	req := httptest.NewRequest("GET", "/api/v1/login/pictures/"+id, nil)
	req.RemoteAddr = remote
	w := httptest.NewRecorder()
	ts.server.ServeHTTP(w, req)
	return w.Code
}

// PLAN 74: an account picks a built-in picture or a photo of its own; the
// login page gets the photo only where it may name the account.
func TestAccountPicture(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.users.Create("bea", testPassword, auth.RoleUser)
	home := netip.MustParsePrefix("192.168.1.0/24")
	ts.server.loginAccounts.homeNetwork = home.Contains

	// A built-in picture.
	res, body := ts.request("PUT", "/api/v1/account/picture", map[string]string{"builtin": "nordlicht"})
	if res.StatusCode != 200 {
		t.Fatalf("builtin: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/account/picture", map[string]string{"builtin": "../etc"}); res.StatusCode != 400 {
		t.Errorf("unknown builtin: %d", res.StatusCode)
	}
	// A photo: square, at most 256 px, JPEG.
	res, body = ts.request("PUT", "/api/v1/account/picture", pictureTestPNG(t, 640, 400))
	if res.StatusCode != 200 {
		t.Fatalf("photo: %d %s", res.StatusCode, body)
	}
	var choice accountPicture
	_ = json.Unmarshal(body, &choice)
	if !accountPictureID.MatchString(choice.Photo) || choice.Builtin != "" {
		t.Fatalf("choice: %+v", choice)
	}
	stored, err := ts.store.Read(accountPictureFile(choice.Photo))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(stored))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 {
		t.Errorf("stored photo: %v %+v", err, cfg)
	}
	if res, _ := ts.request("PUT", "/api/v1/account/picture", []byte("not a picture")); res.StatusCode != 400 {
		t.Errorf("garbage: %d", res.StatusCode)
	}
	// The own account sees its photo.
	if res, _ := ts.request("GET", "/api/v1/account/picture/"+choice.Photo, nil); res.StatusCode != 200 {
		t.Errorf("own photo: %d", res.StatusCode)
	}

	// The login page: picture in the list, photo from the home network only.
	_, answer := askAccountsRaw(t, ts, "192.168.1.20:5000")
	if answer["alex"] == nil || answer["alex"].Photo != choice.Photo || answer["bea"] != nil {
		t.Fatalf("list: %+v", answer)
	}
	if code := loginPicture(ts, choice.Photo, "192.168.1.20:5000"); code != 200 {
		t.Errorf("home network: %d", code)
	}
	if code := loginPicture(ts, choice.Photo, "203.0.113.9:5000"); code != 404 {
		t.Errorf("outside: %d", code)
	}
	if code := loginPicture(ts, "users.json", "192.168.1.20:5000"); code != 404 {
		t.Errorf("bad id: %d", code)
	}
	// A locked account's photo is not served.
	if _, err := ts.users.SetDisabled("alex", true, "bea"); err == nil {
		if code := loginPicture(ts, choice.Photo, "192.168.1.20:5000"); code != 404 {
			t.Errorf("locked account: %d", code)
		}
		_, _ = ts.users.SetDisabled("alex", false, "bea")
	}

	// Back to the standard: the photo file goes.
	if res, _ := ts.request("DELETE", "/api/v1/account/picture", nil); res.StatusCode != 204 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if _, err := ts.store.Read(accountPictureFile(choice.Photo)); err == nil {
		t.Error("the photo stayed on the stick")
	}
	if code := loginPicture(ts, choice.Photo, "192.168.1.20:5000"); code != 404 {
		t.Errorf("after delete: %d", code)
	}
}

func askAccountsRaw(t *testing.T, ts *testServer, remote string) (int, map[string]*accountPicture) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/login/accounts", nil)
	req.RemoteAddr = remote
	w := httptest.NewRecorder()
	ts.server.ServeHTTP(w, req)
	var answer struct {
		Accounts []loginAccount `json:"accounts"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &answer)
	out := map[string]*accountPicture{}
	for _, a := range answer.Accounts {
		out[a.Name] = a.Picture
	}
	if w.Code != http.StatusOK {
		t.Fatalf("accounts: %d", w.Code)
	}
	return w.Code, out
}
