package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/pimtest"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// perfPIM is pimServer with alex's 2000 contacts and 5 calendars of 3000
// events (pimtest); delay slows every open of the volume. The clock is
// the day of the measurement (birthdays of this week).
func perfPIM(t *testing.T) (*testServer, *pimstore.Store, *pimtest.Delay) {
	t.Helper()
	ts, pim, _, _ := pimServer(t)
	pim.Local = func() *time.Location { return calBerlin }
	delay := &pimtest.Delay{}
	volume := pim.Volume
	pim.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		delay.Wait()
		return volume(feature, create)
	}
	imp := func(kind pimstore.Kind, col string, list []pimtest.Item) {
		items := make([]pimstore.Item, len(list))
		for i, it := range list {
			items[i] = pimstore.Item{Name: it.Name, Data: it.Data}
		}
		res, err := pim.Import(context.Background(), kind, "alex", col, items)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res {
			if r.Err != nil {
				t.Fatalf("%s: %v", r.Name, r.Err)
			}
		}
	}
	imp(pimstore.Contacts, pimstore.DefaultBook, pimtest.Contacts(2000))
	for i := range 5 {
		if _, err := pim.CreateCollection(pimstore.Calendar, "alex", pimtest.Calendar(i), pimstore.Meta{Name: pimtest.Calendar(i)}); err != nil {
			t.Fatal(err)
		}
	}
	for col, items := range pimtest.Calendars(5, 3000, 200) {
		imp(pimstore.Calendar, col, items)
	}
	return ts, pim, delay
}

// perfGet sends a GET as alex; the status must be want (any for -1).
func perfGet(t *testing.T, ts *testServer, path string, header map[string]string, want int) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest("GET", ts.url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := ts.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if want > 0 && res.StatusCode != want {
		t.Fatalf("GET %s: %d %s", path, res.StatusCode, data)
	}
	return res, data
}

// TestPerfPIMRoutes measures the routes of Kontakte and Kalender (docs/
// validation/performance-113.md):
// TITANOS_PERF=1 go test ./internal/api -run TestPerfPIMRoutes -v
func TestPerfPIMRoutes(t *testing.T) {
	pimtest.Enabled(t)
	ts, _, delay := perfPIM(t)
	get := func(path string) func() {
		return func() { perfGet(t, ts, path, nil, http.StatusOK) }
	}
	size := func(path string) {
		_, data := perfGet(t, ts, path, nil, http.StatusOK)
		_, zipped := perfGet(t, ts, path, map[string]string{"Accept-Encoding": "gzip"}, http.StatusOK)
		t.Logf("    %s: %d KB (sent: %d KB)", path, len(data)>>10, len(zipped)>>10)
	}
	month := "/api/v1/calendar/events?from=2026-10-26&to=2026-12-07"
	week := "/api/v1/calendar/events?from=2026-11-16&to=2026-11-23"
	noBirthdays := month + "&calendars=kal1,kal2,kal3,kal4,kal5"
	onlyBirthdays := month + "&calendars=geburtstage"
	for _, p := range []string{"/api/v1/contacts", month, week} {
		size(p)
	}
	var list calendarEventList
	_, data := perfGet(t, ts, month, nil, http.StatusOK)
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	clipped, birthdays := 0, 0
	for _, v := range list.Events {
		if v.Clipped {
			clipped++
		}
		if v.Birthday != nil {
			birthdays++
		}
	}
	t.Logf("    month view: %d events, %d birthdays, %d clipped (each one more request of the page)", len(list.Events), birthdays, clipped)

	pimtest.Bench(t, "GET /contacts (2000)", get("/api/v1/contacts"))
	pimtest.Bench(t, "GET /contacts?q=nachname01", get("/api/v1/contacts?q=nachname01"))
	pimtest.Bench(t, "GET /contacts/books", get("/api/v1/contacts/books"))
	pimtest.Bench(t, "GET /calendar/calendars", get("/api/v1/calendar/calendars"))
	pimtest.Bench(t, "GET /calendar/events month (6 weeks) + birthdays", get(month))
	pimtest.Bench(t, "GET /calendar/events month, without birthdays", get(noBirthdays))
	pimtest.Bench(t, "GET /calendar/events month, only birthdays", get(onlyBirthdays))
	pimtest.Bench(t, "GET /calendar/events week + birthdays", get(week))
	tagged := func(path string) func() {
		res, _ := perfGet(t, ts, path, nil, http.StatusOK)
		h := map[string]string{"If-None-Match": res.Header.Get("ETag")}
		return func() { perfGet(t, ts, path, h, -1) }
	}
	if res, _ := perfGet(t, ts, "/api/v1/contacts", nil, http.StatusOK); res.Header.Get("ETag") != "" {
		pimtest.Bench(t, "GET /contacts with If-None-Match (unchanged)", tagged("/api/v1/contacts"))
		pimtest.Bench(t, "GET /calendar/events month with If-None-Match", tagged(month))
		pimtest.Bench(t, "GET /calendar/events month, full=1", get(month+"&full=1"))
	}
	photo := "/api/v1/contacts/kontakte/perf-contact-0000.vcf/photo"
	start := time.Now()
	res, _ := perfGet(t, ts, photo, nil, http.StatusOK)
	t.Logf("%-58s %s", "GET photo, first (made from the card)", time.Since(start))
	pimtest.Bench(t, "GET photo (kept JPEG)", get(photo))
	etag := res.Header.Get("ETag")
	pimtest.Bench(t, "GET photo with If-None-Match", func() {
		res, _ := perfGet(t, ts, photo, map[string]string{"If-None-Match": etag}, -1)
		_ = res
	})
	res, _ = perfGet(t, ts, photo, map[string]string{"If-None-Match": etag}, -1)
	t.Logf("    photo with If-None-Match: %d", res.StatusCode)
	if clipped > 0 {
		var cut []string
		for _, v := range list.Events {
			if v.Clipped && len(cut) < 40 {
				cut = append(cut, "/api/v1/calendar/events/"+v.Calendar+"/"+v.ID+"?occurrence="+v.Occurrence)
			}
		}
		pimtest.Bench(t, "the page's fillClipped: one GET per clipped event", func() {
			for _, p := range cut {
				perfGet(t, ts, p, nil, http.StatusOK)
			}
		})
	}

	// A slow disk (200 ms per open) under one request.
	delay.Set(200 * time.Millisecond)
	slowPhoto := func() {
		perfGet(t, ts, "/api/v1/contacts/kontakte/perf-contact-0000.vcf/photo", map[string]string{"Cache-Control": "no-cache"}, -1)
	}
	slowOpen := func() { perfGet(t, ts, "/api/v1/contacts/kontakte/perf-contact-0001.vcf", nil, 0) }
	pimtest.Blocked(t, "GET /contacts while a photo waits for the disk", 8, slowPhoto, get("/api/v1/contacts"))
	pimtest.Blocked(t, "GET /contacts while a contact waits for the disk", 8, slowOpen, get("/api/v1/contacts"))
	pimtest.Blocked(t, "GET /calendar/events week while a contact waits", 8, slowOpen, get(week))
	delay.Set(0)
	_ = strings.TrimSpace
}
