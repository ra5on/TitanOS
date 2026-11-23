package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appphotos"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

func photoSum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type photoAnswers struct {
	Items []photoAnswer `json:"items"`
}

// photoServer is a test NAS with the share Docs on the volume tank and
// the index of the photo backup on the same volume; *mounted false takes
// the system data volume away.
func photoServer(t *testing.T) (*testServer, string, *bool) {
	t.Helper()
	mounted := true
	var mnt string
	ts := newTestServerWith(t, func(o *Options) {
		o.AppPhotos = &appphotos.Store{Volume: func(create bool) (*os.Root, func(), error) {
			if !mounted || mnt == "" {
				return nil, nil, volumedata.ErrUnavailable
			}
			open := volumedata.OpenExisting
			if create {
				open = volumedata.Open
			}
			root, err := open(mnt, "tank", appphotos.Feature, []string{"tank"})
			return root, func() {}, err
		}}
	})
	ts.setup()
	dir := ts.withShare()
	mnt = filepath.Dir(filepath.Dir(dir))
	return ts, dir, &mounted
}

// uploadPhoto runs the upload of the file manager from the app and ends it
// with the photo finish.
func (ts *testServer) uploadPhoto(phone *http.Client, dir, name, content, claimed string) (*http.Response, []byte) {
	ts.t.Helper()
	res, body := ts.do(phone, "POST", "/api/v1/files/uploads", map[string]any{"root": "share/Docs", "dir": dir, "name": name, "size": len(content), "conflict": "rename"})
	var start struct{ ID string }
	if res.StatusCode != http.StatusCreated || json.Unmarshal(body, &start) != nil {
		ts.t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(phone, "PATCH", "/api/v1/files/uploads/"+start.ID+"?offset=0", []byte(content)); res.StatusCode != http.StatusOK {
		ts.t.Fatalf("chunk: %d %s", res.StatusCode, body)
	}
	return ts.do(phone, "POST", "/api/v1/app/photos/uploads/"+start.ID+"/finish", map[string]string{"sha256": claimed})
}

func (ts *testServer) answers(res *http.Response, body []byte) []photoAnswer {
	ts.t.Helper()
	var got photoAnswers
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &got) != nil {
		ts.t.Fatalf("answer: %d %s", res.StatusCode, body)
	}
	return got.Items
}

func TestAppPhotoBackup(t *testing.T) {
	ts, dir, _ := photoServer(t)
	app := newTestApp(t)
	user, device := ts.pairApp(app, "Pixel 9 von Alex")
	phone, _, _ := ts.appSession(app, user, device)
	for _, d := range []string{"Handy", "Handy/2026", "Handy/2026/11"} {
		if res, body := ts.do(phone, "POST", "/api/v1/files/mkdir", map[string]string{"root": "share/Docs", "path": d}); res.StatusCode != http.StatusCreated {
			t.Fatalf("mkdir %s: %d %s", d, res.StatusCode, body)
		}
	}
	// Year/month made again: file_exists, which the app takes as done.
	if res, body := ts.do(phone, "POST", "/api/v1/files/mkdir", map[string]string{"root": "share/Docs", "path": "Handy/2026"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "file_exists" {
		t.Fatalf("mkdir again: %d %s", res.StatusCode, body)
	}

	// The app claims another sum than the bytes have: nothing published.
	res, body := ts.uploadPhoto(phone, "Handy/2026/11", "IMG_1.jpg", "picture one", photoSum("something else"))
	if res.StatusCode != http.StatusConflict || errorCode(t, body) != "upload_hash_mismatch" {
		t.Fatalf("wrong sum: %d %s", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "Handy/2026/11/IMG_1.jpg")); !os.IsNotExist(err) {
		t.Fatal("published with the wrong sum")
	}
	res, body = ts.uploadPhoto(phone, "Handy/2026/11", "IMG_1.jpg", "picture one", photoSum("picture one"))
	var done struct {
		Path, SHA256, Root string
		Recorded           bool
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &done) != nil || done.Path != "Handy/2026/11/IMG_1.jpg" || done.SHA256 != photoSum("picture one") || !done.Recorded {
		t.Fatalf("finish: %d %s", res.StatusCode, body)
	}
	// Same name, other content: "(2)", as the file manager does.
	res, body = ts.uploadPhoto(phone, "Handy/2026/11", "IMG_1.jpg", "picture two", photoSum("picture two"))
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"path":"Handy/2026/11/IMG_1 (2).jpg"`) {
		t.Fatalf("second: %d %s", res.StatusCode, body)
	}

	// Check: by content, renamed on the phone too, in the folder and below.
	check := func(client *http.Client, root, dir string, items ...photoItem) (*http.Response, []byte) {
		return ts.do(client, "POST", "/api/v1/app/photos/check", map[string]any{"root": root, "dir": dir, "items": items})
	}
	got := ts.answers(check(phone, "share/Docs", "Handy",
		photoItem{SHA256: photoSum("picture one"), Size: 11},
		photoItem{SHA256: photoSum("picture two"), Size: 11, Path: "renamed.jpg"},
		photoItem{SHA256: photoSum("new"), Size: 3}))
	if got[0].State != photoSaved || got[0].Path != "Handy/2026/11/IMG_1.jpg" || got[1].State != photoSaved || got[2].State != photoMissing {
		t.Fatalf("check: %+v", got)
	}
	// Another target does not count.
	if got := ts.answers(check(phone, "share/Docs", "Anderswo", photoItem{SHA256: photoSum("picture one"), Size: 11})); got[0].State != photoMissing {
		t.Fatalf("other folder: %+v", got)
	}
	// A picture already in the target (copied there by SMB) whose path the
	// app names: read once, same content, saved and kept in the index.
	os.WriteFile(filepath.Join(dir, "Handy/2026/11/IMG_9.jpg"), []byte("by smb"), 0644)
	got = ts.answers(check(phone, "share/Docs", "Handy",
		photoItem{SHA256: photoSum("by smb"), Size: 6, Path: "2026/11/IMG_9.jpg"},
		photoItem{SHA256: photoSum("not this"), Size: 6, Path: "2026/11/IMG_9.jpg"},
		photoItem{SHA256: photoSum("x"), Size: 1, Path: "../../escape"}))
	if got[0].State != photoSaved || got[1].State != photoMissing || got[2].State != photoMissing {
		t.Fatalf("probe: %+v", got)
	}

	// Verify before "Speicher freigeben": confirmed only with the content.
	verify := func(items ...photoItem) []photoAnswer {
		return ts.answers(ts.do(phone, "POST", "/api/v1/app/photos/verify", map[string]any{"items": items}))
	}
	got = verify(photoItem{SHA256: photoSum("picture one"), Size: 11}, photoItem{SHA256: photoSum("by smb"), Size: 6}, photoItem{SHA256: photoSum("new"), Size: 3})
	if got[0].State != photoSaved || got[1].State != photoSaved || got[2].State != photoMissing {
		t.Fatalf("verify: %+v", got)
	}
	// A new time with the same content is read again and still confirmed;
	// changed content is not.
	one := filepath.Join(dir, "Handy/2026/11/IMG_1.jpg")
	later := time.Now().Add(time.Hour)
	os.Chtimes(one, later, later)
	if got := verify(photoItem{SHA256: photoSum("picture one"), Size: 11}); got[0].State != photoSaved {
		t.Fatalf("touched: %+v", got)
	}
	os.WriteFile(one, []byte("picture 1!!"), 0644)
	if got := verify(photoItem{SHA256: photoSum("picture one"), Size: 11}); got[0].State != photoMissing {
		t.Fatalf("changed: %+v", got)
	}
	os.Remove(filepath.Join(dir, "Handy/2026/11/IMG_1 (2).jpg"))
	if got := verify(photoItem{SHA256: photoSum("picture two"), Size: 11}); got[0].State != photoMissing {
		t.Fatalf("deleted: %+v", got)
	}

	// The index lies on the system data volume, private.
	log := filepath.Join(filepath.Dir(dir), ".titanos", "apps", "alex", device, "photos.jsonl")
	if st, err := os.Stat(log); err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("index: %v %v", st, err)
	}

	// A phone paired anew (new device) still finds what the first one saved.
	app2 := newTestApp(t)
	_, device2 := ts.pairApp(app2, "Pixel 10 von Alex")
	phone2, _, _ := ts.appSession(app2, user, device2)
	if got := ts.answers(check(phone2, "share/Docs", "Handy", photoItem{SHA256: photoSum("by smb"), Size: 6})); got[0].State != photoSaved {
		t.Fatalf("new device: %+v", got)
	}

	// Rights count now: read only, then none.
	ts.request("PUT", "/api/v1/shares/Docs", map[string]any{"name": "Docs", "volume": "tank", "smb": true, "access": map[string]string{"alex": "ro"}})
	res, body = ts.do(phone, "POST", "/api/v1/files/uploads", map[string]any{"root": "share/Docs", "dir": "Handy", "name": "x.jpg", "size": 1})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("upload read only: %d %s", res.StatusCode, body)
	}
	ts.request("PUT", "/api/v1/shares/Docs", map[string]any{"name": "Docs", "volume": "tank", "smb": true, "access": map[string]string{}})
	if res, body := check(phone, "share/Docs", "Handy", photoItem{SHA256: photoSum("by smb"), Size: 6}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("check without rights: %d %s", res.StatusCode, body)
	}
	// Without rights (or with the volume not mounted) nothing can be said:
	// "unsure", never "missing" (24.11.2026), and never "saved".
	if got := verify(photoItem{SHA256: photoSum("by smb"), Size: 6}); got[0].State != photoUnsure {
		t.Fatalf("verify without rights: %+v", got)
	}
}

func TestAppPhotoRefusals(t *testing.T) {
	ts, _, mounted := photoServer(t)
	item := []photoItem{{SHA256: photoSum("a"), Size: 1}}
	// The browser is not the app.
	for _, path := range []string{"/api/v1/app/photos/check", "/api/v1/app/photos/verify", "/api/v1/app/photos/uploads/abc/finish"} {
		if res, body := ts.request("POST", path, map[string]any{"root": "share/Docs", "items": item, "sha256": photoSum("a")}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "app_session_required" {
			t.Fatalf("browser %s: %d %s", path, res.StatusCode, body)
		}
	}
	// Without a login nothing.
	if res, _ := ts.do(ts.newClient(), "POST", "/api/v1/app/photos/check", map[string]any{}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
	app := newTestApp(t)
	user, device := ts.pairApp(app, "Pixel 9 von Alex")
	phone, _, _ := ts.appSession(app, user, device)
	many := make([]photoItem, photoItemsMax+1)
	for i := range many {
		many[i] = photoItem{SHA256: photoSum(fmt.Sprint(i)), Size: 1}
	}
	for _, bad := range []struct {
		body any
		code string
	}{
		{map[string]any{"root": "share/Docs", "items": many}, "photos_too_many"},
		{map[string]any{"root": "share/Docs", "items": []photoItem{{SHA256: "ABC", Size: 1}}}, "invalid_checksum"},
		{map[string]any{"root": "share/Docs", "dir": "../x", "items": item}, "invalid_path"},
		{map[string]any{"root": "share/Nope", "items": item}, "file_not_found"},
	} {
		if res, body := ts.do(phone, "POST", "/api/v1/app/photos/check", bad.body); errorCode(t, body) != bad.code {
			t.Fatalf("%s: %d %s", bad.code, res.StatusCode, body)
		}
	}
	// Without the system data volume: 503, never an empty answer; nothing
	// is finished that could not be kept.
	*mounted = false
	if res, body := ts.do(phone, "POST", "/api/v1/app/photos/check", map[string]any{"root": "share/Docs", "items": item}); res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "photos_unavailable" {
		t.Fatalf("no volume: %d %s", res.StatusCode, body)
	}
	res, body := ts.uploadPhoto(phone, "", "a.jpg", "a", photoSum("a"))
	if res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "photos_unavailable" {
		t.Fatalf("finish without volume: %d %s", res.StatusCode, body)
	}
}
