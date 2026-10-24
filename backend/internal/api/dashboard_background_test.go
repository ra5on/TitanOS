package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func imageDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func imagePrefs(id string) any {
	return map[string]any{"version": 6, "visual": map[string]any{"images": []map[string]string{{"id": id, "name": "Mine.png"}}, "background": map[string]string{"source": "custom", "id": id}}}
}
func dashboardForm(t *testing.T, ts *testServer, value any, id string, image []byte) (*http.Response, []byte) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	data, _ := json.Marshal(value)
	_ = form.WriteField("preferences", string(data))
	if image != nil {
		part, _ := form.CreateFormFile("image", id)
		_, _ = part.Write(image)
	}
	_ = form.Close()
	req, _ := http.NewRequest("PUT", ts.url+"/api/v1/preferences/dashboard", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	res, err := ts.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	result, _ := io.ReadAll(res.Body)
	return res, result
}
func TestDashboardImageCommitAndAccountIsolation(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	image := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR first")
	id := imageDigest(image)
	_, _ = ts.request("PUT", "/api/v1/background", image)
	if res, body := dashboardForm(t, ts, imagePrefs(id), id, image); res.StatusCode != 204 {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/background/"+id, nil); res.StatusCode != 200 || !bytes.Equal(body, image) {
		t.Fatalf("image: %d %s", res.StatusCode, body)
	}
	reloaded := newPreferences(ts.store)
	value, err := reloaded.get("alex", "dashboard")
	if err != nil || !bytes.Contains(value, []byte(id)) {
		t.Fatalf("restart lost selection: %s %v", value, err)
	}
	if res, body := ts.request("GET", "/api/v1/background", nil); res.StatusCode != 200 || !bytes.Equal(body, image) {
		t.Fatal("legacy image changed")
	}
	if _, err := ts.users.Create("guest", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	if res, _ := ts.do(guest, "POST", "/api/v1/login", credentials{Name: "guest", Password: testPassword}); res.StatusCode != 200 {
		t.Fatal("guest login")
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/background/"+id, nil); res.StatusCode != 404 {
		t.Fatal("image leaked")
	}
	if res, _ := ts.do(guest, "PUT", "/api/v1/preferences/dashboard", imagePrefs(id)); res.StatusCode != 400 {
		t.Fatal("foreign reference accepted")
	}
}
func TestDashboardFailedCommitAndValidation(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	first := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR first")
	second := append(bytes.Clone(first), []byte(" second")...)
	a, b := imageDigest(first), imageDigest(second)
	if res, _ := dashboardForm(t, ts, imagePrefs(a), a, first); res.StatusCode != 204 {
		t.Fatal("first save")
	}
	path := filepath.Join(ts.store.Dir(), preferenceFile("alex", "dashboard"))
	old, _ := os.ReadFile(path)
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if res, _ := dashboardForm(t, ts, imagePrefs(b), b, second); res.StatusCode != 500 {
		t.Fatalf("failed write: %d", res.StatusCode)
	}
	_, prefs := ts.request("GET", "/api/v1/preferences", nil)
	if !bytes.Contains(prefs, []byte(a)) || bytes.Contains(prefs, []byte(b)) {
		t.Fatal("cache changed after failure")
	}
	if _, err := ts.store.Read(dashboardImageFile("alex", b)); !os.IsNotExist(err) {
		t.Fatal("failed save left new image")
	}
	if _, err := ts.store.Read(dashboardImageFile("alex", a)); err != nil {
		t.Fatal("old image removed")
	}
	_ = os.Remove(path)
	_ = os.Rename(path+".saved", path)
	persisted, _ := os.ReadFile(path)
	if !bytes.Equal(old, persisted) {
		t.Fatal("previous preferences changed")
	}
	if res, _ := dashboardForm(t, ts, imagePrefs(b), b, []byte("wrong")); res.StatusCode != 400 {
		t.Fatal("wrong digest accepted")
	}
	html := []byte("<html>not image</html>")
	if res, _ := dashboardForm(t, ts, imagePrefs(imageDigest(html)), imageDigest(html), html); res.StatusCode != 415 {
		t.Fatal("HTML accepted")
	}
	if res, _ := dashboardForm(t, ts, imagePrefs(b), b, bytes.Repeat([]byte{1}, maxBackgroundBytes+1)); res.StatusCode != 413 {
		t.Fatal("oversized image accepted")
	}
	for range 2 {
		if res, _ := ts.request("PUT", "/api/v1/preferences/dashboard", map[string]any{"visual": map[string]any{}}); res.StatusCode != 204 {
			t.Fatal("clear failed")
		}
	}
	if _, err := ts.store.Read(dashboardImageFile("alex", a)); !os.IsNotExist(err) {
		t.Fatal("old library not collected")
	}
}
