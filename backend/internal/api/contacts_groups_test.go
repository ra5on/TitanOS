package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// TestContactGroupsCardDAVRoundTrip: a group as the iPhone sends it over
// CardDAV shows in Kontakte with its member, the web page adds a contact,
// renames and deletes it, and the iPhone reads back the same card with
// only the changed lines (members, FN/N, REV) new (1.13).
func TestContactGroupsCardDAVRoundTrip(t *testing.T) {
	ts := newDAVTestServer(t)
	device := ts.newDevice(ts.client, "iPhone")
	fixture := func(name string) string {
		b, err := os.ReadFile("../davsync/testdata/ios26/fixtures/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	dav := func(method, path, body string, header map[string]string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, ts.url+path, strings.NewReader(body))
		req.SetBasicAuth("alex", device.Password)
		for k, v := range header {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res.StatusCode, string(b)
	}
	const book = "/carddav/alex/addressbooks/kontakte/"
	vcard := map[string]string{"Content-Type": "text/vcard; charset=utf-8", "If-None-Match": "*"}
	erika := "A5B7C9D1-E3F5-4789-9ABC-DEF012345678.vcf"
	group := "B1C2D3E4-F5A6-4B7C-8D9E-0F1A2B3C4D5E.vcf"
	if code, body := dav("PUT", book+erika, fixture("erika.vcf"), vcard); code != http.StatusCreated {
		t.Fatalf("PUT erika: %d %s", code, body)
	}
	original := fixture("group-family.vcf")
	if code, body := dav("PUT", book+group, original, vcard); code != http.StatusCreated {
		t.Fatalf("PUT group: %d %s", code, body)
	}

	web := func(method, path string, body any, want int, v any) {
		t.Helper()
		res, data := ts.do(ts.client, method, path, body)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, data)
		}
		if v != nil {
			if err := json.Unmarshal(data, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	var over contactsOverview
	web("GET", "/api/v1/contacts/overview", nil, http.StatusOK, &over)
	if len(over.Groups) != 1 || over.Groups[0].Name != "Familie" || !slices.Equal(over.Groups[0].Members, []string{erika}) || over.Groups[0].ID != group {
		t.Fatalf("overview %+v", over)
	}
	if len(over.Books) != 1 || over.Books[0].Count != 1 {
		t.Fatalf("books %+v", over.Books)
	}
	// The group is no contact of the list.
	var list []cardweb.Item
	web("GET", "/api/v1/contacts", nil, http.StatusOK, &list)
	if len(list) != 1 || list[0].ID != erika || list[0].ETag == "" {
		t.Fatalf("list %+v", list)
	}

	// A new contact of the web page joins the group.
	var made contactWritten
	web("POST", "/api/v1/contacts", map[string]any{"contact": map[string]any{"first": "Jonas", "last": "Neu"}}, http.StatusCreated, &made)
	if made.Item == nil || made.Item.Name != "Jonas Neu" {
		t.Fatalf("no row with the write: %+v", made)
	}
	var g groupView
	web("PUT", "/api/v1/contacts/groups/"+url.PathEscape(group), map[string]any{"etag": over.Groups[0].ETag, "add": []string{made.ID}}, http.StatusOK, &g)
	if !slices.Equal(g.Members, []string{erika, made.ID}) {
		t.Fatalf("members %+v", g)
	}
	// Stale etag: 409 with the group as it is now.
	res, data := ts.do(ts.client, "PUT", "/api/v1/contacts/groups/"+url.PathEscape(group), map[string]any{"etag": over.Groups[0].ETag, "remove": []string{erika}})
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(data), `"current"`) {
		t.Fatalf("stale: %d %s", res.StatusCode, data)
	}

	code, got := dav("GET", book+group, "", nil)
	if code != http.StatusOK {
		t.Fatalf("GET group: %d", code)
	}
	gotLines := strings.Split(got, "\r\n")
	for _, l := range strings.Split(original, "\r\n") {
		if strings.HasPrefix(l, "REV:") {
			continue
		}
		if !slices.Contains(gotLines, l) {
			t.Errorf("line %q of the iPhone changed:\n%s", l, got)
		}
	}
	uid := strings.TrimSuffix(made.ID, ".vcf")
	if !slices.Contains(gotLines, "X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:"+uid) {
		t.Fatalf("new member missing:\n%s", got)
	}

	// Rename, then a group of its own, then delete: the contacts stay.
	name := "Familie Muster"
	web("PUT", "/api/v1/contacts/groups/"+url.PathEscape(group), map[string]any{"etag": g.ETag, "name": name}, http.StatusOK, &g)
	if _, got = dav("GET", book+group, "", nil); !strings.Contains(got, "FN:Familie Muster\r\n") || !strings.Contains(got, "N:Familie Muster;;;;\r\n") {
		t.Fatalf("renamed:\n%s", got)
	}
	var club groupView
	web("POST", "/api/v1/contacts/groups", map[string]any{"name": "Verein", "members": []string{made.ID}}, http.StatusCreated, &club)
	if code, got := dav("GET", book+club.ID, "", nil); code != http.StatusOK || !strings.Contains(got, "X-ADDRESSBOOKSERVER-KIND:group\r\n") {
		t.Fatalf("new group over CardDAV: %d %s", code, got)
	}
	web("POST", "/api/v1/contacts/groups", map[string]any{"name": "x", "members": []string{"gibtsnicht.vcf"}}, http.StatusNotFound, nil)
	web("POST", "/api/v1/contacts/groups", map[string]any{"name": "  "}, http.StatusBadRequest, nil)
	web("DELETE", "/api/v1/contacts/groups/"+url.PathEscape(group)+"?etag="+url.QueryEscape(g.ETag), nil, http.StatusNoContent, nil)
	if code, _ := dav("GET", book+group, "", nil); code != http.StatusNotFound {
		t.Fatalf("group still there: %d", code)
	}
	if code, _ := dav("GET", book+erika, "", nil); code != http.StatusOK {
		t.Fatalf("member gone with its group: %d", code)
	}
	// A contact is no group, a group no contact.
	web("DELETE", "/api/v1/contacts/groups/"+url.PathEscape(erika)+"?etag=x", nil, http.StatusNotFound, nil)
	web("GET", "/api/v1/contacts/kontakte/"+url.PathEscape(club.ID), nil, http.StatusNotFound, nil)
}

// TestContactDuplicatesMerge: two cards of one person are found, merged
// field by field into the kept one (what TitanOS does not show stays),
// the other's groups name the kept one and the other is gone.
func TestContactDuplicatesMerge(t *testing.T) {
	e := newConEnv(t)
	keepETag := e.seed("ben.vcf", iosContact)
	dropCard := crlf(`
BEGIN:VCARD
VERSION:3.0
N:Muster;Benjamin;;;
FN:Benjamin Muster
EMAIL;type=INTERNET:ben@example.com
TEL;type=WORK:+49 221 555 0142
X-SOCIALPROFILE;type=twitter:x-apple:benm
X-APPLE-PHONETIC-FIRST:Benjamin
BDAY:1986-11-20
UID:DROP-UID
END:VCARD
`)
	dropETag := e.seed("benjamin.vcf", dropCard)
	e.seed("other.vcf", crlf(`
BEGIN:VCARD
VERSION:3.0
N:Andere;Anna;;;
FN:Anna Andere
UID:OTHER
END:VCARD
`))
	e.seed("grp.vcf", crlf(`
BEGIN:VCARD
VERSION:3.0
N:Freunde;;;;
FN:Freunde
X-ADDRESSBOOKSERVER-KIND:group
X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:DROP-UID
UID:GRP
END:VCARD
`))
	var dups []duplicateView
	e.do(e.alex, "GET", "/api/v1/contacts/duplicates", nil, http.StatusOK, &dups)
	if len(dups) != 1 || !slices.Equal(dups[0].IDs, []string{"ben.vcf", "benjamin.vcf"}) || dups[0].Matches[0].Kind != cardweb.MatchEmail {
		t.Fatalf("duplicates %+v", dups)
	}
	var over contactsOverview
	e.do(e.alex, "GET", "/api/v1/contacts/overview", nil, http.StatusOK, &over)
	if over.Duplicates != 1 || len(over.Groups) != 1 {
		t.Fatalf("overview %+v", over)
	}
	// bea sees nothing of alex.
	var none []duplicateView
	e.do(e.bea, "GET", "/api/v1/contacts/duplicates", nil, http.StatusOK, &none)
	if len(none) != 0 {
		t.Fatalf("bea: %+v", none)
	}

	body := map[string]any{
		"keep": map[string]string{"id": "ben.vcf", "etag": keepETag},
		"drop": map[string]string{"id": "benjamin.vcf", "etag": dropETag},
		"contact": map[string]any{
			"first": "Ben", "last": "Muster", "org": "Beispiel GmbH",
			"phones":    []map[string]string{{"type": "mobile", "value": "+49 151 7654321"}, {"type": "work", "value": "+49 221 555 0142"}},
			"emails":    []map[string]string{{"type": "other", "value": "ben@example.com"}},
			"birthday":  "1986-11-20",
			"notes":     "Spielt mittwochs Fußball",
			"addresses": []map[string]string{{"type": "home", "street": "Lindenallee 7", "city": "Köln", "postcode": "50667", "country": "Deutschland"}},
		},
	}
	// A stale etag changes nothing.
	stale := map[string]any{"keep": body["keep"], "drop": map[string]string{"id": "benjamin.vcf", "etag": "x"}, "contact": body["contact"]}
	e.do(e.alex, "POST", "/api/v1/contacts/merge", stale, http.StatusConflict, nil)
	var res mergeResult
	e.do(e.alex, "POST", "/api/v1/contacts/merge", body, http.StatusOK, &res)
	if res.ID != "ben.vcf" || res.Groups != 1 || res.Item == nil || res.Item.Name != "Ben Muster" {
		t.Fatalf("merge %+v", res)
	}
	merged := e.raw("alex", "ben.vcf")
	for _, want := range []string{"UID:A5B7C9D1-E3F5-4789-9ABC-DEF012345678", "IMPP;", "X-APPLE-PHONETIC-LAST:Muster",
		"X-SOCIALPROFILE;type=twitter:x-apple:benm", "+49 221 555 0142", "BDAY", "item2.X-ABADR:de"} {
		if !strings.Contains(merged, want) {
			t.Errorf("merged card lacks %q:\n%s", want, merged)
		}
	}
	if strings.Contains(merged, "DROP-UID") || strings.Contains(merged, "FN:Benjamin") || strings.Contains(merged, "N:Muster;Benjamin") {
		t.Errorf("merged card has the other's name or UID:\n%s", merged)
	}
	if _, err := e.pim.Get(pimstore.Contacts, "alex", pimstore.DefaultBook, "benjamin.vcf"); err == nil {
		t.Fatal("the other contact is still there")
	}
	grp := e.raw("alex", "grp.vcf")
	if strings.Contains(grp, "DROP-UID") || !strings.Contains(grp, "urn:uuid:A5B7C9D1-E3F5-4789-9ABC-DEF012345678") {
		t.Fatalf("group not moved:\n%s", grp)
	}
	e.do(e.alex, "GET", "/api/v1/contacts/duplicates", nil, http.StatusOK, &dups)
	if len(dups) != 0 {
		t.Fatalf("still duplicates %+v", dups)
	}
	// A group is never merged, nor a contact with itself.
	e.do(e.alex, "POST", "/api/v1/contacts/merge", map[string]any{
		"keep": map[string]string{"id": "ben.vcf", "etag": res.ETag}, "drop": map[string]string{"id": "grp.vcf", "etag": "x"},
	}, http.StatusNotFound, nil)
	e.do(e.alex, "POST", "/api/v1/contacts/merge", map[string]any{
		"keep": map[string]string{"id": "ben.vcf", "etag": res.ETag}, "drop": map[string]string{"id": "ben.vcf", "etag": res.ETag},
	}, http.StatusBadRequest, nil)
}

// TestContactSearchOneRequest: the search of the page answers contacts
// and T-Mail's suggestions together; without a query no suggestions.
func TestContactSearchOneRequest(t *testing.T) {
	e := newConEnv(t)
	e.seed("ben.vcf", iosContact)
	var out contactSearch
	e.do(e.alex, "GET", "/api/v1/contacts/search?q="+url.QueryEscape("musterm"), nil, http.StatusOK, &out)
	if len(out.Contacts) != 0 || out.Suggestions == nil {
		t.Fatalf("%+v", out)
	}
	e.do(e.alex, "GET", "/api/v1/contacts/search?q="+url.QueryEscape("0151 76"), nil, http.StatusOK, &out)
	if len(out.Contacts) != 1 || out.Contacts[0].ID != "ben.vcf" {
		t.Fatalf("%+v", out)
	}
	e.do(e.alex, "GET", "/api/v1/contacts/search", nil, http.StatusOK, &out)
	if len(out.Contacts) != 1 || len(out.Suggestions) != 0 {
		t.Fatalf("%+v", out)
	}
}

// TestContactWriteKeepsList: a write answers its row and the list's ETag
// now; the list asked with it is 304 (the page keeps the row it put in
// place, the live event of its own write costs no list).
func TestContactWriteKeepsList(t *testing.T) {
	e := newConEnv(t)
	e.seed("ben.vcf", iosContact)
	var made contactWritten
	e.do(e.alex, "POST", "/api/v1/contacts", map[string]any{"contact": map[string]any{"first": "Eva", "last": "Neu"}}, http.StatusCreated, &made)
	if made.Item == nil || made.ListETag == "" {
		t.Fatalf("%+v", made)
	}
	ask := func(tag string) int {
		req, _ := http.NewRequest("GET", e.ts.url+"/api/v1/contacts", nil)
		req.Header.Set("If-None-Match", tag)
		res, err := e.alex.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := ask(made.ListETag); code != http.StatusNotModified {
		t.Fatalf("list after the write: %d", code)
	}
	var g groupView
	e.do(e.alex, "POST", "/api/v1/contacts/groups", map[string]any{"name": "Team", "members": []string{made.ID}}, http.StatusCreated, &g)
	if g.ListETag == "" || g.ListETag == made.ListETag || ask(g.ListETag) != http.StatusNotModified {
		t.Fatalf("after the group: %q", g.ListETag)
	}
	res, _ := e.ts.do(e.alex, "DELETE", "/api/v1/contacts/kontakte/"+url.PathEscape(made.ID)+"?etag="+url.QueryEscape(made.ETag), nil)
	if res.StatusCode != http.StatusNoContent || ask(res.Header.Get("X-List-ETag")) != http.StatusNotModified {
		t.Fatalf("delete: %d %q", res.StatusCode, res.Header.Get("X-List-ETag"))
	}
}

// TestContactWriteCTag: a write names the book's ctag after it, one more
// than before (the page knows then that nothing else changed).
func TestContactWriteCTag(t *testing.T) {
	e := newConEnv(t)
	e.seed("ben.vcf", iosContact)
	before, err := e.pim.GetCollection(pimstore.Contacts, "alex", pimstore.DefaultBook)
	if err != nil {
		t.Fatal(err)
	}
	var made contactWritten
	e.do(e.alex, "POST", "/api/v1/contacts", map[string]any{"contact": map[string]any{"first": "Eva"}}, http.StatusCreated, &made)
	after, _ := e.pim.GetCollection(pimstore.Contacts, "alex", pimstore.DefaultBook)
	if made.CTag == "" || made.CTag != after.CTag || made.CTag == before.CTag {
		t.Fatalf("ctag %q, before %q, after %q", made.CTag, before.CTag, after.CTag)
	}
}
