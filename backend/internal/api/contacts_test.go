package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	vcardcheck "github.com/ra5on/TitanOS/backend/internal/pim/card"
	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// conEnv: T-Mail (granted to alex and bea) and the contacts on a volume of
// their own (contactsMailServer); alex signed in, bea a plain account.
type conEnv struct {
	t        *testing.T
	ts       *testServer
	pim      *pimstore.Store
	mailDir  string
	gone     *atomic.Bool
	alex     *http.Client
	bea      *http.Client
	location *time.Location
}

func newConEnv(t *testing.T) *conEnv {
	t.Helper()
	ts, pim, mailDir, gone := contactsMailServer(t)
	pim.Local = func() *time.Location { return calBerlin }
	bea := ts.newClient()
	if res, _ := ts.do(bea, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatal("bea")
	}
	return &conEnv{t: t, ts: ts, pim: pim, mailDir: mailDir, gone: gone, alex: ts.client, bea: bea, location: calBerlin}
}

func (e *conEnv) do(client *http.Client, method, path string, body any, want int, v any) []byte {
	e.t.Helper()
	res, data := e.ts.do(client, method, path, body)
	if res.StatusCode != want {
		e.t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, data)
	}
	if v != nil {
		if err := json.Unmarshal(data, v); err != nil {
			e.t.Fatalf("%s %s: %v %s", method, path, err, data)
		}
	}
	return data
}

// seed stores a card of alex as a device would.
func (e *conEnv) seed(name string, data []byte) string {
	e.t.Helper()
	res, err := e.pim.Put(pimstore.Contacts, "alex", pimstore.DefaultBook, name, data, pimstore.PutOptions{})
	if err != nil {
		e.t.Fatalf("seed %s: %v", name, err)
	}
	return res.ETag
}

func (e *conEnv) raw(account, name string) string {
	e.t.Helper()
	obj, err := e.pim.Get(pimstore.Contacts, account, pimstore.DefaultBook, name)
	if err != nil {
		e.t.Fatalf("get %s: %v", name, err)
	}
	return strings.ReplaceAll(string(obj.Data), "\r\n ", "")
}

func (e *conEnv) list(client *http.Client, q string) []cardweb.Item {
	e.t.Helper()
	var out []cardweb.Item
	e.do(client, "GET", "/api/v1/contacts?q="+url.QueryEscape(q), nil, http.StatusOK, &out)
	return out
}

func itemNames(items []cardweb.Item) string {
	var s []string
	for _, it := range items {
		s = append(s, it.Name)
	}
	return strings.Join(s, ",")
}

// iosContact is a card as iOS 26 writes it.
var iosContact = crlf(`
BEGIN:VCARD
VERSION:3.0
PRODID:-//Apple Inc.//iPhone OS 26.1//EN
N:Muster;Ben;;;
FN:Ben Muster
ORG:Beispiel GmbH;
item1.EMAIL;type=INTERNET;type=pref:ben@example.com
item1.X-ABLabel:_$!<Other>!$_
TEL;type=CELL;type=VOICE;type=pref:+49 151 7654321
item2.ADR;type=HOME;type=pref:;;Lindenallee 7;Köln;;50667;Deutschland
item2.X-ABADR:de
IMPP;X-SERVICE-TYPE=Skype;type=HOME;type=pref:skype:ben.muster
X-APPLE-PHONETIC-LAST:Muster
NOTE:Spielt mittwochs Fußball
REV:2026-11-10T07:45:12Z
UID:A5B7C9D1-E3F5-4789-9ABC-DEF012345678
END:VCARD
`)

var groupCard = crlf(`
BEGIN:VCARD
VERSION:3.0
N:Familie;;;;
FN:Familie
X-ADDRESSBOOKSERVER-KIND:group
X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:A5B7C9D1-E3F5-4789-9ABC-DEF012345678
UID:B1C2D3E4-F5A6-4B7C-8D9E-0F1A2B3C4D5E
END:VCARD
`)

// contactRoutes are the routes of Kontakte with method (books included).
func contactRoutes(ts *testServer) [][2]string {
	var out [][2]string
	for _, rt := range pimDataRoutes(ts) {
		if strings.HasPrefix(rt[1], "/api/v1/contacts") {
			out = append(out, rt)
		}
	}
	return out
}

func contactPath(p string) string {
	return strings.NewReplacer("{book}", "kontakte", "{id}", "x.vcf", "books/x.vcf", "books/kontakte").Replace(p)
}

// TestContactRouteRights: every route of Kontakte is for a web session of
// this node only – never in-process (MCP, agent), a token, another node,
// without a session or in publicRoutes – and the session reaches the
// handler (TECHNIK 8.3 questions 19/20).
func TestContactRouteRights(t *testing.T) {
	e := newConEnv(t)
	ts := e.ts
	routes := contactRoutes(ts)
	if len(routes) != 13 {
		t.Fatalf("routes: %v", routes)
	}
	for _, r := range publicRoutes {
		if strings.HasPrefix(r.Path, "/contacts") {
			t.Errorf("public route %s %s", r.Method, r.Path)
		}
	}
	owner, _ := ts.users.Get("alex")
	token := ts.mcpToken("alex", "system:read", "system:write", "files:read", "files:write")
	for _, rt := range routes {
		path := contactPath(rt[1])
		code, body := ts.server.internalAs(context.Background(), owner, rt[0], path, map[string]any{})
		if code != http.StatusForbidden || !strings.Contains(body, "browser_session_required") {
			t.Errorf("%s %s in-process: %d %s", rt[0], path, code, body)
		}
		req, _ := http.NewRequest(rt[0], ts.url+path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with a token: %d", rt[0], path, res.StatusCode)
		}
		creq := httptest.NewRequest(rt[0], path, strings.NewReader("{}"))
		creq = creq.WithContext(cluster.WithIdentity(creq.Context(), cluster.Identity{Node: "n2", User: "alex"}))
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, creq)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cluster_local_only") {
			t.Errorf("%s %s through the cluster: %d %s", rt[0], path, rec.Code, rec.Body)
		}
		if res, _ := ts.do(ts.newClient(), rt[0], path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", rt[0], path, res.StatusCode)
		}
		if res, body := ts.do(e.alex, rt[0], path, map[string]any{}); strings.Contains(string(body), "browser_session_required") ||
			res.StatusCode == http.StatusNotFound && !strings.Contains(string(body), "_not_found") || res.StatusCode >= 500 {
			t.Errorf("%s %s from the web UI: %d %s", rt[0], path, res.StatusCode, body)
		}
	}
}

// Without the volume (or without a store) every route answers 503
// contacts_unavailable with Retry-After – never an empty list, never the
// suggestions of T-Mail alone.
func TestContactsWithoutVolume(t *testing.T) {
	e := newConEnv(t)
	e.seed("x.vcf", pimCard("x"))
	learnedBook(t, e.ts, e.mailDir, "alex", "carol@example.net")
	e.gone.Store(true)
	bare := newTestServer(t)
	bare.setup()
	for _, ts := range []*testServer{e.ts, bare} {
		for _, rt := range contactRoutes(e.ts) {
			path := contactPath(rt[1])
			res, body := ts.do(ts.client, rt[0], path, map[string]any{"addr": "carol@example.net"})
			if res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "contacts_unavailable" || res.Header.Get("Retry-After") == "" {
				t.Errorf("%s %s: %d %s", rt[0], path, res.StatusCode, body)
			}
		}
	}
}

// Create, read, merge, conflict, delete; what the page does not show stays
// in the card; another account sees nothing; groups are hidden.
func TestContactsEdit(t *testing.T) {
	e := newConEnv(t)
	etag := e.seed("ben.vcf", iosContact)
	e.seed("familie.vcf", groupCard)

	var c cardweb.Contact
	e.do(e.alex, "GET", "/api/v1/contacts/kontakte/ben.vcf", nil, http.StatusOK, &c)
	if c.ETag != etag || c.First != "Ben" || c.Org != "Beispiel GmbH" || c.Emails[0].Type != "other" || c.Phones[0].Type != "mobile" ||
		c.Addresses[0].City != "Köln" || c.Notes != "Spielt mittwochs Fußball" || c.Modified == "" {
		t.Fatalf("contact %+v", c)
	}
	// The page sends what it shows; a changed note, a new number.
	phones := append(c.Phones, cardweb.Value{Type: "work", Value: "+49 221 555 0142"})
	var w contactWritten
	e.do(e.alex, "PUT", "/api/v1/contacts/kontakte/ben.vcf", map[string]any{"etag": c.ETag, "contact": map[string]any{
		"first": c.First, "last": c.Last, "org": c.Org, "phones": phones, "emails": c.Emails, "addresses": c.Addresses,
		"birthday": "", "notes": "Spielt dienstags Fußball"}}, http.StatusOK, &w)
	raw := e.raw("alex", "ben.vcf")
	mustHave(t, "merged", raw, "item1.EMAIL;type=INTERNET;type=pref:ben@example.com\r\n", "item1.X-ABLabel:_$!<Other>!$_\r\n",
		"IMPP;X-SERVICE-TYPE=Skype;type=HOME;type=pref:skype:ben.muster\r\n", "X-APPLE-PHONETIC-LAST:Muster\r\n",
		"item2.X-ABADR:de\r\n", "TEL;type=CELL;type=VOICE;type=pref:+49 151 7654321\r\n", "TEL;TYPE=WORK:+49 221 555 0142\r\n",
		"NOTE:Spielt dienstags Fußball\r\n", "UID:A5B7C9D1-E3F5-4789-9ABC-DEF012345678\r\n", "PRODID:-//Apple Inc.//iPhone OS 26.1//EN\r\n")
	if w.ETag == etag || w.ID != "ben.vcf" || w.Book != "kontakte" {
		t.Fatalf("written %+v", w)
	}
	// The group (membership by UID) is untouched, hidden from the page,
	// still on the disk and under /carddav/.
	if e.raw("alex", "familie.vcf") != strings.ReplaceAll(string(groupCard), "\r\n ", "") {
		t.Fatal("the group changed")
	}
	if got := itemNames(e.list(e.alex, "")); got != "Ben Muster" {
		t.Fatalf("list %s", got)
	}
	for _, rt := range [][2]string{{"GET", ""}, {"PUT", ""}, {"DELETE", "?etag=x"}, {"GET", "/photo"}, {"PUT", "/photo"}, {"DELETE", "/photo"}} {
		path := "/api/v1/contacts/kontakte/familie.vcf" + rt[1]
		if res, body := e.ts.do(e.alex, rt[0], path, map[string]any{"etag": "x", "contact": map[string]any{"first": "x"}}); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "contact_not_found" {
			t.Errorf("group %s %s: %d %s", rt[0], path, res.StatusCode, body)
		}
	}
	if objs, _ := e.pim.Objects(pimstore.Contacts, "alex", "kontakte"); len(objs) != 2 {
		t.Fatalf("on the disk: %d", len(objs))
	}
	// Unchanged input writes nothing.
	var same contactWritten
	e.do(e.alex, "PUT", "/api/v1/contacts/kontakte/ben.vcf", map[string]any{"etag": w.ETag, "contact": map[string]any{"notes": "Spielt dienstags Fußball"}}, http.StatusOK, &same)
	if same.ETag != w.ETag {
		t.Fatal("an unchanged contact was written")
	}
	// An old etag: 409 with the contact as it is now.
	res, body := e.ts.do(e.alex, "PUT", "/api/v1/contacts/kontakte/ben.vcf", map[string]any{"etag": etag, "contact": map[string]any{"notes": "alt"}})
	var conflict struct {
		Current cardweb.Contact `json:"current"`
	}
	if res.StatusCode != http.StatusConflict || errorCode(t, body) != "contacts_conflict" || json.Unmarshal(body, &conflict) != nil || conflict.Current.ETag != w.ETag {
		t.Fatalf("conflict: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.do(e.alex, "PUT", "/api/v1/contacts/kontakte/ben.vcf", map[string]any{"contact": map[string]any{"notes": "x"}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("without etag: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.do(e.alex, "PUT", "/api/v1/contacts/kontakte/ben.vcf", map[string]any{"etag": w.ETag, "contact": map[string]any{"emails": []map[string]string{{"type": "mobile", "value": "x@y"}}}}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "contacts_invalid" {
		t.Fatalf("invalid: %d %s", res.StatusCode, body)
	}

	// A new contact: vCard 3.0 under <uid>.vcf, FN of first and last name.
	var created contactWritten
	e.do(e.alex, "POST", "/api/v1/contacts", map[string]any{"contact": map[string]any{"first": "Lena", "last": "Beispiel",
		"phones": []map[string]string{{"type": "mobile", "value": "+49 172 9988776"}}, "birthday": "--12-02"}}, http.StatusCreated, &created)
	newRaw := e.raw("alex", created.ID)
	if !strings.HasSuffix(created.ID, ".vcf") || !strings.Contains(newRaw, "UID:"+strings.TrimSuffix(created.ID, ".vcf")+"\r\n") {
		t.Fatalf("created %+v\n%s", created, newRaw)
	}
	mustHave(t, "created", newRaw, "VERSION:3.0\r\n", "FN:Lena Beispiel\r\n", "N:Beispiel;Lena;;;\r\n", "BDAY;X-APPLE-OMIT-YEAR=1604:1604-12-02\r\n")
	if _, err := vcardcheck.Parse([]byte(newRaw)); err != nil {
		t.Fatal(err)
	}
	var lena cardweb.Contact
	e.do(e.alex, "GET", "/api/v1/contacts/kontakte/"+created.ID, nil, http.StatusOK, &lena)
	if lena.Birthday != "--12-02" || lena.Name != "Lena Beispiel" {
		t.Fatalf("lena %+v", lena)
	}

	// Another account: every route of alex's contact is 404, nothing leaks.
	for _, rt := range [][2]string{{"GET", "/api/v1/contacts/kontakte/ben.vcf"}, {"PUT", "/api/v1/contacts/kontakte/ben.vcf"},
		{"DELETE", "/api/v1/contacts/kontakte/ben.vcf?etag=" + w.ETag}, {"GET", "/api/v1/contacts/kontakte/ben.vcf/photo"},
		{"PUT", "/api/v1/contacts/kontakte/ben.vcf/photo"}, {"DELETE", "/api/v1/contacts/kontakte/ben.vcf/photo"}} {
		res, data := e.ts.do(e.bea, rt[0], rt[1], map[string]any{"etag": w.ETag, "contact": map[string]any{"notes": "bea"}})
		if res.StatusCode != http.StatusNotFound || strings.Contains(string(data), "Muster") {
			t.Errorf("bea %s %s: %d %s", rt[0], rt[1], res.StatusCode, data)
		}
	}
	if items := e.list(e.bea, ""); len(items) != 0 {
		t.Fatalf("bea sees %v", items)
	}
	if !strings.Contains(e.raw("alex", "ben.vcf"), "dienstags") {
		t.Fatal("bea changed alex's contact")
	}
	if res, _ := e.ts.do(e.alex, "GET", "/api/v1/contacts?book=andere", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown book: %d", res.StatusCode)
	}

	// Delete: etag required and strict.
	if res, body := e.ts.do(e.alex, "DELETE", "/api/v1/contacts/kontakte/ben.vcf?etag="+etag, nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "contacts_conflict" {
		t.Fatalf("delete with an old etag: %d %s", res.StatusCode, body)
	}
	e.do(e.alex, "DELETE", "/api/v1/contacts/kontakte/ben.vcf?etag="+w.ETag, nil, http.StatusNoContent, nil)
	if res, _ := e.ts.do(e.alex, "GET", "/api/v1/contacts/kontakte/ben.vcf", nil); res.StatusCode != http.StatusNotFound {
		t.Fatal("still there")
	}
}

// Search over name, company, numbers (digits, with and without country
// code), e-mail and postal address; sorted by last name (DIN 5007).
func TestContactsSearch(t *testing.T) {
	e := newConEnv(t)
	e.seed("ben.vcf", iosContact)
	e.seed("anna.vcf", vcard("anna", "Anna Ärger", "N:Ärger;Anna;;;", "ORG:Praxis am Markt", "TEL;TYPE=CELL:0170 1234567"))
	e.seed("eva.vcf", vcard("eva", "Eva Krüger", "N:Krüger;Eva;;;", "EMAIL:eva.krueger@mail.example"))
	e.seed("auto.vcf", vcard("auto", "Autohaus Beispiel", "N:;;;;", "ORG:Autohaus Beispiel"))
	e.seed("familie.vcf", groupCard)
	if got := itemNames(e.list(e.alex, "")); got != "Anna Ärger,Autohaus Beispiel,Eva Krüger,Ben Muster" {
		t.Fatalf("sorted: %s", got)
	}
	for q, want := range map[string]string{
		"+49 170 123": "Anna Ärger", "0151 765": "Ben Muster", "+49151": "Ben Muster", "4567": "Anna Ärger",
		"krueger@": "Eva Krüger", "lindenallee": "Ben Muster", "50667 köln": "Ben Muster", "praxis": "Anna Ärger",
		"beispiel": "Autohaus Beispiel,Ben Muster", "familie": "", "nichts": "",
	} {
		if got := itemNames(e.list(e.alex, q)); got != want {
			t.Errorf("q=%q: %q, want %q", q, got, want)
		}
	}
}

// A birthday without year (iOS: X-APPLE-OMIT-YEAR=1604) reads as --MM-DD;
// a birthday of this week is in the list.
func TestContactsBirthday(t *testing.T) {
	e := newConEnv(t)
	today := time.Now().In(calBerlin)
	e.seed("a.vcf", vcard("a", "Anna Beispiel", "BDAY;X-APPLE-OMIT-YEAR=1604:1604-"+today.Format("01-02")))
	e.seed("b.vcf", vcard("b", "Ben Muster", "BDAY:1986-"+today.AddDate(0, 0, 8).Format("01-02")))
	var c cardweb.Contact
	e.do(e.alex, "GET", "/api/v1/contacts/kontakte/a.vcf", nil, http.StatusOK, &c)
	if c.Birthday != "--"+today.Format("01-02") {
		t.Fatalf("birthday %q", c.Birthday)
	}
	items := e.list(e.alex, "")
	if items[0].BirthdaySoon == nil || items[0].BirthdaySoon.Date != today.Format("2006-01-02") || items[0].BirthdaySoon.Age != nil || items[1].BirthdaySoon != nil {
		t.Fatalf("list %+v", items)
	}
}

func contactPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 3), uint8(x ^ y), 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// putPhoto uploads a picture as multipart.
func (e *conEnv) putPhoto(client *http.Client, path string, pic []byte) (*http.Response, []byte) {
	e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "bild.png")
	_, _ = fw.Write(pic)
	_ = mw.Close()
	req, _ := http.NewRequest("PUT", e.ts.url+path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, data
}

func (e *conEnv) putPhotoJSON(client *http.Client, path string, v any) (*http.Response, []byte) {
	e.t.Helper()
	data, _ := json.Marshal(v)
	req, _ := http.NewRequest("PUT", e.ts.url+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res, out
}

// Pictures: an upload is shrunk to 512 px and 256 KiB into the card
// (PHOTO;ENCODING=b;TYPE=JPEG); the page gets a JPEG that never runs as
// anything else; a file of the NAS only with the account's rights; a
// bomb is refused; the cache is made again from the card.
func TestContactPhoto(t *testing.T) {
	e := newConEnv(t)
	etag := e.seed("ben.vcf", iosContact)
	res, body := e.putPhoto(e.alex, "/api/v1/contacts/kontakte/ben.vcf/photo?etag="+etag, contactPNG(t, 1600, 1200))
	var w contactWritten
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &w) != nil || w.HasPhoto == nil || !*w.HasPhoto || w.ETag == etag {
		t.Fatalf("upload: %d %s", res.StatusCode, body)
	}
	raw := e.raw("alex", "ben.vcf")
	mustHave(t, "photo in the card", raw, "PHOTO;ENCODING=b;TYPE=JPEG:", "IMPP;X-SERVICE-TYPE=Skype")
	pic, ok := cardweb.Photo([]byte(raw))
	if c, format, err := image.DecodeConfig(bytes.NewReader(pic)); !ok || err != nil || format != "jpeg" || c.Width != 512 || c.Height != 384 || len(pic) > 256<<10 {
		t.Fatalf("picture in the card: %s %dx%d %d bytes", format, c.Width, c.Height, len(pic))
	}
	get := func(client *http.Client, path string) (*http.Response, []byte) {
		r, err := client.Get(e.ts.url + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		return r, data
	}
	r, data := get(e.alex, "/api/v1/contacts/kontakte/ben.vcf/photo")
	h := r.Header
	if r.StatusCode != http.StatusOK || h.Get("Content-Type") != "image/jpeg" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Security-Policy") != "sandbox" || !strings.HasPrefix(h.Get("Cache-Control"), "private") || !bytes.Equal(data, pic) {
		t.Fatalf("photo route: %d %v %d bytes", r.StatusCode, h, len(data))
	}
	// 1.13: the card's ETag names the picture; asked again with it, 304
	// from RAM.
	photoTag := h.Get("ETag")
	if photoTag != `"`+w.ETag+`"` || h.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("photo etag %s (card %s), %s", photoTag, w.ETag, h.Get("Cache-Control"))
	}
	conditional := func(client *http.Client, tag, query string) *http.Response {
		req, _ := http.NewRequest("GET", e.ts.url+"/api/v1/contacts/kontakte/ben.vcf/photo"+query, nil)
		req.Header.Set("If-None-Match", tag)
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r
	}
	for _, tag := range []string{photoTag, "W/" + photoTag, `"x", ` + photoTag, "*"} {
		if r := conditional(e.alex, tag, ""); r.StatusCode != http.StatusNotModified || r.Header.Get("ETag") != photoTag {
			t.Fatalf("If-None-Match %s: %d", tag, r.StatusCode)
		}
	}
	if r := conditional(e.alex, `"old"`, ""); r.StatusCode != http.StatusOK {
		t.Fatalf("another etag: %d", r.StatusCode)
	}
	// ?v=<the card's ETag> (photoURL of the page) names one picture for good.
	if r := conditional(e.alex, `"old"`, "?v="+w.ETag); r.Header.Get("Cache-Control") != "private, max-age=604800, immutable" {
		t.Fatalf("?v=etag: %s", r.Header.Get("Cache-Control"))
	}
	if r := conditional(e.alex, `"old"`, "?v=stale"); r.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("?v=stale: %s", r.Header.Get("Cache-Control"))
	}
	// bea never gets alex's picture, not even as 304.
	if r := conditional(e.bea, photoTag, ""); r.StatusCode != http.StatusNotFound {
		t.Fatalf("bea with alex's etag: %d", r.StatusCode)
	}
	// A conflict: the old etag.
	if res, body := e.putPhoto(e.alex, "/api/v1/contacts/kontakte/ben.vcf/photo?etag="+etag, contactPNG(t, 64, 64)); res.StatusCode != http.StatusConflict || errorCode(t, body) != "contacts_conflict" {
		t.Fatalf("photo conflict: %d %s", res.StatusCode, body)
	}
	// A decompression bomb: a PNG header of 30000×30000 pixels.
	bomb := contactPNG(t, 16, 16)
	bomb[16], bomb[17], bomb[18], bomb[19] = 0, 0, 0x75, 0x30                // width 30000
	bomb[20], bomb[21], bomb[22], bomb[23] = 0, 0, 0x75, 0x30                // height 30000
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29])) // a valid header: refused by the pixel bound
	if res, body := e.putPhoto(e.alex, "/api/v1/contacts/kontakte/ben.vcf/photo", bomb); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "contacts_photo_invalid" {
		t.Fatalf("bomb: %d %s", res.StatusCode, body)
	}
	if res, body := e.putPhoto(e.alex, "/api/v1/contacts/kontakte/ben.vcf/photo", []byte("<html>no picture</html>")); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "contacts_photo_invalid" {
		t.Fatalf("html: %d %s", res.StatusCode, body)
	}

	// From the files of the NAS: alex may read the share, bea may not.
	dir := e.ts.withShare()
	if err := os.WriteFile(filepath.Join(dir, "portrait.png"), contactPNG(t, 300, 400), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, body := e.putPhotoJSON(e.alex, "/api/v1/contacts/kontakte/ben.vcf/photo", map[string]string{"root": "share/Docs", "path": "portrait.png"}); res.StatusCode != http.StatusOK {
		t.Fatalf("from the NAS: %d %s", res.StatusCode, body)
	}
	pic, _ = cardweb.Photo([]byte(e.raw("alex", "ben.vcf")))
	if c, _, _ := image.DecodeConfig(bytes.NewReader(pic)); c.Width != 300 || c.Height != 400 {
		t.Fatalf("NAS picture %dx%d", c.Width, c.Height)
	}
	var mine contactWritten
	e.do(e.bea, "POST", "/api/v1/contacts", map[string]any{"contact": map[string]any{"first": "Bea"}}, http.StatusCreated, &mine)
	if res, body := e.putPhotoJSON(e.bea, "/api/v1/contacts/kontakte/"+mine.ID+"/photo", map[string]string{"root": "share/Docs", "path": "portrait.png"}); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "file_not_found" {
		t.Fatalf("bea from alex's share: %d %s", res.StatusCode, body)
	}
	if res, body := e.putPhotoJSON(e.alex, "/api/v1/contacts/kontakte/ben.vcf/photo", map[string]string{"root": "share/Docs", "path": "../../etc/passwd"}); res.StatusCode < 400 || res.StatusCode >= 500 {
		t.Fatalf("outside the share: %d %s", res.StatusCode, body)
	}

	// A card of a device with a picture: the cache is made from the card.
	devCard := []byte(strings.Replace(string(iosContact), "END:VCARD", "PHOTO;ENCODING=b;TYPE=JPEG:"+b64(pic)+"\r\nEND:VCARD", 1))
	e.seed("dev.vcf", []byte(strings.Replace(string(devCard), "A5B7C9D1", "D5B7C9D1", 1)))
	if r, data := get(e.alex, "/api/v1/contacts/kontakte/dev.vcf/photo"); r.StatusCode != http.StatusOK || len(data) == 0 {
		t.Fatalf("rebuilt: %d", r.StatusCode)
	}
	if cached, err := e.pim.Photo("alex", "dev.vcf"); err != nil || len(cached) == 0 {
		t.Fatalf("cache: %v", err)
	}

	// Delete the picture: the card loses PHOTO, the route says so.
	var gone contactWritten
	e.do(e.alex, "DELETE", "/api/v1/contacts/kontakte/ben.vcf/photo", nil, http.StatusOK, &gone)
	if gone.HasPhoto == nil || *gone.HasPhoto || strings.Contains(e.raw("alex", "ben.vcf"), "PHOTO") {
		t.Fatalf("deleted: %+v", gone)
	}
	if r, data := get(e.alex, "/api/v1/contacts/kontakte/ben.vcf/photo"); r.StatusCode != http.StatusNotFound || errorCode(t, data) != "contact_photo_not_found" {
		t.Fatalf("no photo: %d %s", r.StatusCode, data)
	}
}

// Suggestions from T-Mail: only with the grant, never an address that is
// in a contact; "Als Kontakt übernehmen" makes a contact and the address
// leaves the suggestions, the learned entry stays in T-Mail.
func TestContactSuggestions(t *testing.T) {
	e := newConEnv(t)
	learnedBook(t, e.ts, e.mailDir, "alex", "service@bank.example", "trainer@fc-sued.example", "ben@example.com")
	e.seed("ben.vcf", iosContact)
	var got []contactSuggestion
	e.do(e.alex, "GET", "/api/v1/contacts/suggestions", nil, http.StatusOK, &got)
	if len(got) != 2 || got[0].Addr != "service@bank.example" || got[0].Count != 3 || got[1].Addr != "trainer@fc-sued.example" {
		t.Fatalf("suggestions %+v", got)
	}
	e.do(e.alex, "GET", "/api/v1/contacts/suggestions?q=trainer", nil, http.StatusOK, &got)
	if len(got) != 1 {
		t.Fatalf("q: %+v", got)
	}
	// Without the grant: an empty list, from-suggestion finds nothing.
	cora := e.ts.signIn("cora", auth.RoleUser)
	learnedBook(t, e.ts, e.mailDir, "cora", "x@example.net")
	if body := e.do(cora, "GET", "/api/v1/contacts/suggestions", nil, http.StatusOK, nil); string(body) != "[]\n" && string(body) != "[]" {
		t.Fatalf("without grant: %s", body)
	}
	if res, body := e.ts.do(cora, "POST", "/api/v1/contacts/from-suggestion", map[string]string{"addr": "x@example.net"}); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "contact_suggestion_not_found" {
		t.Fatalf("without grant: %d %s", res.StatusCode, body)
	}

	var w contactWritten
	e.do(e.alex, "POST", "/api/v1/contacts/from-suggestion", map[string]string{"addr": "Trainer@FC-Sued.example"}, http.StatusCreated, &w)
	var c cardweb.Contact
	e.do(e.alex, "GET", "/api/v1/contacts/kontakte/"+w.ID, nil, http.StatusOK, &c)
	if c.First != "Gelernt" || c.Last != "trainer@fc-sued.example" || len(c.Emails) != 1 || c.Emails[0].Value != "trainer@fc-sued.example" {
		t.Fatalf("from suggestion %+v", c)
	}
	e.do(e.alex, "GET", "/api/v1/contacts/suggestions", nil, http.StatusOK, &got)
	if len(got) != 1 || got[0].Addr != "service@bank.example" {
		t.Fatalf("after: %+v", got)
	}
	if res, body := e.ts.do(e.alex, "POST", "/api/v1/contacts/from-suggestion", map[string]string{"addr": "ben@example.com"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "contact_exists" {
		t.Fatalf("again: %d %s", res.StatusCode, body)
	}
	if res, body := e.ts.do(e.alex, "POST", "/api/v1/contacts/from-suggestion", map[string]string{"addr": "nobody@example.net"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown: %d %s", res.StatusCode, body)
	}
	// T-Mail still knows the address (now also as a contact).
	if list := suggestions(t, e.ts, e.alex, "trainer"); len(list) != 1 || list[0].Source != "contact" {
		t.Fatalf("T-Mail: %+v", list)
	}
	if e.ts.server.opts.Mail.Learned("alex", "trainer", 10, nil)[0].Addr != "trainer@fc-sued.example" {
		t.Fatal("the learned entry went")
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func (e *conEnv) importVCF(client *http.Client, query string, file []byte, want int) contactImportResult {
	e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "kontakte.vcf")
	_, _ = fw.Write(file)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", e.ts.url+"/api/v1/contacts/books/kontakte/import"+query, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var out contactImportResult
	if res.StatusCode != want || (want == http.StatusOK && json.Unmarshal(data, &out) != nil) {
		e.t.Fatalf("import: %d %s", res.StatusCode, data)
	}
	return out
}

// Import: duplicates by UID, without UID by FN and e-mail; export: every
// card as stored, a download; imported again it gives the same cards.
func TestContactImportExport(t *testing.T) {
	e := newConEnv(t)
	e.seed("ben.vcf", iosContact)
	e.seed("eva.vcf", vcard("eva-uid", "Eva Krüger", "EMAIL:eva@example.org"))
	benChanged := []byte(strings.Replace(string(iosContact), "NOTE:Spielt mittwochs Fußball", "NOTE:Neu", 1))
	evaNoUID := crlf("\nBEGIN:VCARD\nVERSION:3.0\nN:Krüger;Eva;;;\nFN:eva krüger\nEMAIL:EVA@example.org\nNOTE:importiert\nEND:VCARD\n")
	fresh := crlf("\nBEGIN:VCARD\nVERSION:3.0\nN:Neu;Nina;;;\nFN:Nina Neu\nEMAIL:nina@example.org\nEND:VCARD\n")
	broken := crlf("\nBEGIN:VCARD\nVERSION:3.0\nUID:kaputt\nN:x;;;;\nEND:VCARD\n")
	file := bytes.Join([][]byte{benChanged, evaNoUID, fresh, fresh, broken, groupCard}, nil)

	out := e.importVCF(e.alex, "", file, http.StatusOK)
	if out.Added != 2 || out.Updated != 0 || out.Skipped != 3 || len(out.Failed) != 1 || out.Failed[0].Reason != "invalid" || out.Failed[0].UID != "kaputt" {
		t.Fatalf("skip: %+v", out)
	}
	if strings.Contains(e.raw("alex", "ben.vcf"), "NOTE:Neu") {
		t.Fatal("skip replaced")
	}
	out = e.importVCF(e.alex, "?onDuplicate=replace", file, http.StatusOK)
	if out.Added != 0 || out.Updated != 2 || out.Skipped != 3 || len(out.Failed) != 1 {
		t.Fatalf("replace: %+v", out)
	}
	if !strings.Contains(e.raw("alex", "ben.vcf"), "NOTE:Neu") || !strings.Contains(e.raw("alex", "eva.vcf"), "NOTE:importiert") ||
		!strings.Contains(e.raw("alex", "eva.vcf"), "UID:eva-uid") {
		t.Fatal("replace did not write into the files of the contacts")
	}
	if got := itemNames(e.list(e.alex, "")); got != "Eva Krüger,Ben Muster,Nina Neu" && got != "eva krüger,Ben Muster,Nina Neu" {
		t.Fatalf("after import: %s", got)
	}
	if res, body := e.ts.do(e.alex, "POST", "/api/v1/contacts/books/kontakte/import?onDuplicate=x", nil); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("onDuplicate: %d %s", res.StatusCode, body)
	}
	e.importVCF(e.alex, "", []byte("kein vcard"), http.StatusBadRequest)

	res, err := e.alex.Get(e.ts.url + "/api/v1/contacts/books/kontakte/export")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	h := res.Header
	if res.StatusCode != http.StatusOK || h.Get("Content-Type") != "text/vcard; charset=utf-8" || h.Get("Content-Disposition") != `attachment; filename="kontakte.vcf"` ||
		h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("export: %d %v", res.StatusCode, h)
	}
	if n := strings.Count(string(data), "BEGIN:VCARD"); n != 4 {
		t.Fatalf("%d cards exported", n)
	}
	// bea imports alex's export: the same cards.
	again := e.importVCF(e.bea, "", data, http.StatusOK)
	if again.Added != 4 || len(again.Failed) != 0 {
		t.Fatalf("re-import %+v", again)
	}
	cards := func(account string) []string {
		objs, _ := e.pim.Objects(pimstore.Contacts, account, "kontakte")
		var out []string
		for _, o := range objs {
			out = append(out, e.raw(account, o.Name))
		}
		slices.Sort(out)
		return out
	}
	if a, b := cards("alex"), cards("bea"); !slices.Equal(a, b) {
		t.Fatalf("export and import differ:\n%v\n%v", a, b)
	}
}
