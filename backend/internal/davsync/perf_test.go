package davsync

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/pimtest"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// perfEnv is newEnv with alex's 2000 contacts and 5 calendars of 3000
// events (pimtest); delay slows every open of the volume.
func perfEnv(t *testing.T) (*env, *pimtest.Delay) {
	t.Helper()
	e := newEnv(t)
	delay := &pimtest.Delay{}
	volume := e.store.Volume
	e.store.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		delay.Wait()
		return volume(feature, create)
	}
	imp := func(kind store.Kind, col string, list []pimtest.Item) {
		items := make([]store.Item, len(list))
		for i, it := range list {
			items[i] = store.Item{Name: it.Name, Data: it.Data}
		}
		res, err := e.store.Import(context.Background(), kind, "alex", col, items)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res {
			if r.Err != nil {
				t.Fatalf("%s: %v", r.Name, r.Err)
			}
		}
	}
	imp(store.Contacts, store.DefaultBook, pimtest.Contacts(2000))
	for i := range 5 {
		if _, err := e.store.CreateCollection(store.Calendar, "alex", pimtest.Calendar(i), store.Meta{Name: pimtest.Calendar(i)}); err != nil {
			t.Fatal(err)
		}
	}
	for col, items := range pimtest.Calendars(5, 3000, 200) {
		imp(store.Calendar, col, items)
	}
	return e, delay
}

var syncTokenRE = regexp.MustCompile(`<[^>]*sync-token>([^<]+)</`)

// TestPerfDAV measures what an iPhone asks (docs/validation/
// performance-113.md): TITANOS_PERF=1 go test ./internal/davsync -run TestPerfDAV -v
func TestPerfDAV(t *testing.T) {
	pimtest.Enabled(t)
	e, delay := perfEnv(t)
	book := "/carddav/alex/addressbooks/kontakte/"
	cal := "/caldav/alex/calendars/kal1/"
	send := func(method, path, depth, body string) []byte {
		h := map[string]string{"Content-Type": "application/xml"}
		if depth != "" {
			h["Depth"] = depth
		}
		res, data := e.do(method, path, "alex", h, strings.NewReader(body))
		if res.StatusCode != http.StatusMultiStatus {
			t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, data)
		}
		return data
	}
	const ctag = `<A:propfind xmlns:A="DAV:" xmlns:C="http://calendarserver.org/ns/"><A:prop><C:getctag/><A:sync-token/></A:prop></A:propfind>`
	const etags = `<A:propfind xmlns:A="DAV:"><A:prop><A:getetag/><A:getcontenttype/></A:prop></A:propfind>`
	const home = `<A:propfind xmlns:A="DAV:" xmlns:C="http://calendarserver.org/ns/" xmlns:I="http://apple.com/ns/ical/"><A:prop><A:displayname/><A:resourcetype/><C:getctag/><A:sync-token/><I:calendar-color/></A:prop></A:propfind>`
	sync := func(token string) string {
		return `<A:sync-collection xmlns:A="DAV:"><A:sync-token>` + token + `</A:sync-token><A:sync-level>1</A:sync-level><A:prop><A:getetag/></A:prop></A:sync-collection>`
	}
	multiget := func(root, ns, data string, hrefs []string) string {
		var b strings.Builder
		fmt.Fprintf(&b, `<B:%s xmlns:A="DAV:" xmlns:B="%s"><A:prop><A:getetag/><B:%s/></A:prop>`, root, ns, data)
		for _, h := range hrefs {
			b.WriteString("<A:href>" + h + "</A:href>")
		}
		b.WriteString("</B:" + root + ">")
		return b.String()
	}
	var cards, events []string
	for i := range 200 {
		cards = append(cards, fmt.Sprintf("%sperf-contact-%04d.vcf", book, i))
		events = append(events, fmt.Sprintf("%sperf-event-%05d.ics", cal, i*5))
	}
	cardGet := multiget("addressbook-multiget", "urn:ietf:params:xml:ns:carddav", "address-data", cards)
	calGet := multiget("calendar-multiget", "urn:ietf:params:xml:ns:caldav", "calendar-data", events)

	first := send("PROPFIND", book, "0", ctag)
	token := syncTokenRE.FindSubmatch(first)
	if token == nil {
		t.Fatalf("no sync-token: %s", first)
	}
	opens := e.opens.Load()
	pimtest.Bench(t, "PROPFIND Depth 0 getctag+sync-token (book)", func() { send("PROPFIND", book, "0", ctag) })
	pimtest.Bench(t, "PROPFIND Depth 1 calendar home (5 calendars)", func() { send("PROPFIND", "/caldav/alex/calendars/", "1", home) })
	pimtest.Bench(t, "REPORT sync-collection, token unchanged (book)", func() { send("REPORT", book, "", sync(string(token[1]))) })
	if n := e.opens.Load() - opens; n != 0 {
		t.Errorf("unchanged ctag/sync-token opened the volume %d times", n)
	}
	t.Logf("unchanged ctag, home set and sync-token: %d opens of the volume", e.opens.Load()-opens)
	var size int
	pimtest.Bench(t, "PROPFIND Depth 1 book (2000 getetag)", func() { size = len(send("PROPFIND", book, "1", etags)) })
	t.Logf("    answer %d KB", size>>10)
	pimtest.Bench(t, "REPORT sync-collection, empty token (book, 2000)", func() { size = len(send("REPORT", book, "", sync(""))) })
	t.Logf("    answer %d KB", size>>10)
	pimtest.Bench(t, "REPORT addressbook-multiget 200 with address-data", func() { size = len(send("REPORT", book, "1", cardGet)) })
	t.Logf("    answer %d KB", size>>10)
	pimtest.Bench(t, "REPORT calendar-multiget 200 with calendar-data", func() { size = len(send("REPORT", cal, "1", calGet)) })
	t.Logf("    answer %d KB", size>>10)

	// A slow disk under a multiget (5 ms per open): what a second request
	// of the iPhone or the web page waits.
	delay.Set(5 * time.Millisecond)
	pimtest.Bench(t, "REPORT addressbook-multiget 200, 5 ms per open", func() { send("REPORT", book, "1", cardGet) })
	delay.Set(200 * time.Millisecond)
	slow := func() {
		send("REPORT", book, "1", multiget("addressbook-multiget", "urn:ietf:params:xml:ns:carddav", "address-data", cards[:3]))
	}
	pimtest.Blocked(t, "PROPFIND getctag (book) while a multiget waits for the disk", 8, slow, func() { send("PROPFIND", book, "0", ctag) })
	pimtest.Blocked(t, "PROPFIND Depth 1 (book) while a multiget waits for the disk", 8, slow, func() { send("PROPFIND", book, "1", etags) })
	delay.Set(0)
}
