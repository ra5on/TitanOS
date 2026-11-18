package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

// contactsMailServer is a test server with T-Mail (granted to alex and
// bea) and the store of calendars and contacts, each on a volume of its
// own; pimGone takes the volume of the contacts away.
func contactsMailServer(t *testing.T) (ts *testServer, pim *pimstore.Store, mailDir string, pimGone *atomic.Bool) {
	t.Helper()
	mailDir, pimDir := t.TempDir(), t.TempDir()
	pimGone = &atomic.Bool{}
	pim = pimstore.New()
	pim.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		if pimGone.Load() {
			return nil, nil, pimstore.ErrUnavailable
		}
		path := filepath.Join(pimDir, ".titanos", feature)
		if create {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return nil, nil, err
			}
		}
		root, err := os.OpenRoot(path)
		return root, func() {}, err
	}
	pim.Current = func(string) string {
		if pimGone.Load() {
			return ""
		}
		return "data"
	}
	ts = newTestServerWith(t, func(o *Options) {
		users := o.Users
		m := tmail.New(o.Store)
		m.Account = func(name string) (tmail.Account, bool) {
			u, ok := users.Get(name)
			return tmail.Account{Name: u.Name, Created: u.Created, Disabled: u.Disabled, Admin: u.Role.CanAdminister()}, ok
		}
		m.Volume = func(bool) (*os.Root, func(), error) {
			root, err := os.OpenRoot(mailDir)
			if err != nil {
				return nil, nil, err
			}
			return root, func() { root.Close() }, nil
		}
		if err := m.Load(); err != nil {
			t.Fatal(err)
		}
		o.Mail = m
		o.PIM = pim
		pim.Account = func(name string) (time.Time, bool) {
			u, ok := users.Get(name)
			return u.Created, ok
		}
	})
	ts.setup()
	ts.guest("bea")
	ts.grantMail("alex")
	ts.grantMail("bea")
	return ts, pim, mailDir, pimGone
}

// learnedBook writes T-Mail's address book of an account (learned from
// sent mail) before it is first read.
func learnedBook(t *testing.T, ts *testServer, mailDir, account string, addrs ...string) {
	t.Helper()
	u, _ := ts.users.Get(account)
	var list []map[string]any
	for i, a := range addrs {
		list = append(list, map[string]any{"name": "Gelernt " + a, "addr": a, "count": len(addrs) - i, "last": "2026-11-01T10:00:00Z"})
	}
	data, _ := json.Marshal(map[string]any{"owner": map[string]any{"name": u.Name, "created": u.Created}, "contacts": list})
	if err := os.MkdirAll(filepath.Join(mailDir, account), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mailDir, account, "contacts.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

type suggestion struct {
	Name, Addr, Source string
}

func suggestions(t *testing.T, ts *testServer, client *http.Client, q string) []suggestion {
	t.Helper()
	res, body := ts.do(client, "GET", "/api/v1/mail/contacts?q="+q, nil)
	var out []suggestion
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil {
		t.Fatalf("contacts %q: %d %s", q, res.StatusCode, body)
	}
	return out
}

func vcard(uid, fn string, extra ...string) []byte {
	lines := append([]string{"BEGIN:VCARD", "VERSION:3.0", "UID:" + uid, "FN:" + fn, "N:" + fn + ";;;;"}, extra...)
	return []byte(strings.Join(append(lines, "END:VCARD", ""), "\r\n"))
}

// TECHNIK 7.4: matching contacts first, then the learned addresses; an
// address once, as contact; at most 20; another account sees none of the
// contacts; the contacts never go into T-Mail's contacts.json; without
// the volume of the contacts the learned ones still come.
func TestMailContactsFirst(t *testing.T) {
	ts, pim, mailDir, pimGone := contactsMailServer(t)
	learnedBook(t, ts, mailDir, "alex", "carol@example.net", "dave@example.net", "erika@firma.example")
	learnedBook(t, ts, mailDir, "bea", "dave@example.net")
	put := func(name string, data []byte) {
		t.Helper()
		if _, err := pim.Put(pimstore.Contacts, "alex", pimstore.DefaultBook, name, data, pimstore.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	put("carol.vcf", vcard("carol", "Carol Contact", "EMAIL:Carol@Example.net"))
	put("erika.vcf", vcard("erika", "Erika Mustermann", "EMAIL:erika@example.org", "EMAIL:erika@firma.example"))
	put("group.vcf", vcard("group", "Carol und Erika", "X-ADDRESSBOOKSERVER-KIND:group", "EMAIL:alle@example.org"))

	got := suggestions(t, ts, ts.client, "car")
	if len(got) != 1 || got[0] != (suggestion{"Carol Contact", "Carol@Example.net", "contact"}) {
		t.Fatalf("car: %+v", got)
	}
	got = suggestions(t, ts, ts.client, "")
	want := []suggestion{
		{"Carol Contact", "Carol@Example.net", "contact"},
		{"Erika Mustermann", "erika@example.org", "contact"},
		{"Erika Mustermann", "erika@firma.example", "contact"},
		{"Gelernt dave@example.net", "dave@example.net", "learned"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("all:\n got %+v\nwant %+v", got, want)
	}
	// Bea has T-Mail and no contacts: only her learned ones, none of alex's.
	bea := ts.newClient()
	ts.do(bea, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	if got := suggestions(t, ts, bea, ""); len(got) != 1 || got[0].Source != "learned" || got[0].Addr != "dave@example.net" {
		t.Fatalf("bea: %+v", got)
	}
	if got := suggestions(t, ts, bea, "erika"); len(got) != 0 {
		t.Fatalf("bea sees alex's contacts: %+v", got)
	}
	// At most 20, contacts first.
	for i := range 25 {
		put(fmt.Sprintf("p%02d.vcf", i), vcard(fmt.Sprintf("p%02d", i), fmt.Sprintf("Person %02d", i), fmt.Sprintf("EMAIL:person%02d@example.org", i)))
	}
	if got := suggestions(t, ts, ts.client, ""); len(got) != 20 || got[19].Source != "contact" {
		t.Fatalf("limit: %d %+v", len(got), got)
	}
	// Nothing of the contacts in T-Mail's address book.
	for _, account := range []string{"alex", "bea"} {
		book, _ := os.ReadFile(filepath.Join(mailDir, account, "contacts.json"))
		if strings.Contains(string(book), "erika@example.org") || strings.Contains(string(book), "Contact") || strings.Contains(string(book), "source") {
			t.Fatalf("contacts.json of %s: %s", account, book)
		}
	}
	// Without the volume of the contacts: the learned ones, no error.
	pimGone.Store(true)
	got = suggestions(t, ts, ts.client, "")
	if len(got) != 3 || got[0].Source != "learned" || got[0].Addr != "carol@example.net" {
		t.Fatalf("no volume: %+v", got)
	}
}

// 1.12: the sender pictures name the account's own contacts with a
// picture by address (lower case), nothing of another account, nothing
// without the volume; the picture itself answers 503 then, and the list
// shows initials.
func TestMailAvatars(t *testing.T) {
	ts, pim, _, pimGone := contactsMailServer(t)
	put := func(name string, data []byte) {
		t.Helper()
		if _, err := pim.Put(pimstore.Contacts, "alex", pimstore.DefaultBook, name, data, pimstore.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	photo := "PHOTO;ENCODING=b;TYPE=JPEG:/9j/4AAQSkZJRg=="
	put("anna.vcf", vcard("anna", "Anna Bild", "EMAIL:Anna@Example.org", "EMAIL:anna@firma.example", photo))
	put("ben.vcf", vcard("ben", "Ben Ohne", "EMAIL:ben@example.org"))
	put("group.vcf", vcard("group", "Gruppe", "X-ADDRESSBOOKSERVER-KIND:group", "EMAIL:alle@example.org", photo))
	avatars := func(client *http.Client) map[string]string {
		t.Helper()
		res, body := ts.do(client, "GET", "/api/v1/mail/avatars", nil)
		var out struct {
			Book   string            `json:"book"`
			Photos map[string]string `json:"photos"`
		}
		if res.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil || out.Book != "kontakte" || out.Photos == nil {
			t.Fatalf("avatars: %d %s", res.StatusCode, body)
		}
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("cache: %q", res.Header.Get("Cache-Control"))
		}
		return out.Photos
	}
	if got := avatars(ts.client); fmt.Sprint(got) != "map[anna@example.org:anna.vcf anna@firma.example:anna.vcf]" {
		t.Fatalf("alex: %v", got)
	}
	bea := ts.newClient()
	ts.do(bea, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	if got := avatars(bea); len(got) != 0 {
		t.Fatalf("bea sees alex's pictures: %v", got)
	}
	pimGone.Store(true)
	if got := avatars(ts.client); len(got) != 0 {
		t.Fatalf("without the volume: %v", got)
	}
	if res, _ := ts.do(ts.client, "GET", "/api/v1/contacts/kontakte/anna.vcf/photo", nil); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("picture without the volume: %d", res.StatusCode)
	}
}
