package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/pim/personalbackup"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

func TestPersonalBackupPrivacyAndRecovery(t *testing.T) {
	pim, _, _ := pimTestStore(t)
	ts := newTestServerWith(t, func(o *Options) {
		o.PIM = pim
		pim.Account = func(name string) (time.Time, bool) { u, ok := o.Users.Get(name); return u.Created, ok }
	})
	ts.setup()
	user := ts.signIn("bea", auth.RoleUser)
	const api = "/api/v1/account/personal-backup"
	source := store.ArchiveSource{Kind: store.Calendar, ID: store.DefaultCalendar}
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	res, body := ts.do(user, "PUT", api, personalbackup.Settings{Recipient: key.Recipient().String(), Sources: []store.ArchiveSource{source}})
	if res.StatusCode != 204 {
		t.Fatalf("enable: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("GET", "/api/v1/backups/personal-sources", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), "bea") || bytes.Contains(body, []byte(source.ID)) {
		t.Fatalf("operator sources: %d %s", res.StatusCode, body)
	}
	res, _ = ts.request("GET", api+"/archive?account=bea", nil)
	if res.StatusCode != 400 {
		t.Fatalf("owner accessed other account: %d", res.StatusCode)
	}
	u, _ := ts.users.Get("bea")
	for _, route := range []struct{ method, path string }{{"GET", api}, {"GET", api + "/archive"}, {"PUT", api}, {"POST", api + "/key"}, {"POST", api + "/restore"}} {
		code, _ := ts.server.internalAs(context.Background(), u, route.method, route.path, nil)
		if code != 403 {
			t.Fatalf("non-browser access to %s: %d", route.path, code)
		}
	}
	res, encrypted := ts.do(user, "GET", api+"/archive", nil)
	if res.StatusCode != 200 || !bytes.HasPrefix(encrypted, []byte("age-encryption.org/v1")) {
		t.Fatalf("download: %d", res.StatusCode)
	}
	var input bytes.Buffer
	form := multipart.NewWriter(&input)
	if err := form.WriteField("identity", key.String()); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("file", "backup.age")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(encrypted); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", ts.url+api+"/restore", &input)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Origin", ts.url)
	res, err = user.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	restored, _ := io.ReadAll(res.Body)
	var report store.ArchiveReport
	if res.StatusCode != 200 || json.Unmarshal(restored, &report) != nil || report.Account != "bea" || len(report.Collections) != 1 {
		t.Fatalf("test restore: %d %s", res.StatusCode, restored)
	}
}
