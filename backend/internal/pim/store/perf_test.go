package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/pimtest"
)

// perfStore is a store with 2000 contacts (every second with a picture,
// nine in ten with a birthday) and 5 calendars with 3000 events, 200 of
// them series (pimtest), on a temp folder; delay slows every open of the
// volume (a disk that spins up).
func perfStore(t *testing.T) (*env, *Store, *pimtest.Delay) {
	t.Helper()
	e := newEnv(t)
	s := e.store()
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	s.Local = func() *time.Location { return berlin }
	delay := &pimtest.Delay{}
	volume := s.Volume
	s.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		delay.Wait()
		return volume(feature, create)
	}
	import_ := func(kind Kind, col string, list []pimtest.Item) {
		items := make([]Item, len(list))
		for i, it := range list {
			items[i] = Item{Name: it.Name, Data: it.Data}
		}
		res, err := s.Import(context.Background(), kind, "alex", col, items)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res {
			if r.Err != nil {
				t.Fatalf("%s: %v", r.Name, r.Err)
			}
		}
	}
	import_(Contacts, DefaultBook, pimtest.Contacts(2000))
	for i := range 5 {
		if _, err := s.CreateCollection(Calendar, "alex", pimtest.Calendar(i), Meta{Name: pimtest.Calendar(i)}); err != nil {
			t.Fatal(err)
		}
	}
	for col, items := range pimtest.Calendars(5, 3000, 200) {
		import_(Calendar, col, items)
	}
	// A fresh store over the same folder: what titand has after a start.
	s2 := e.store()
	s2.Local = s.Local
	s2.Volume = s.Volume
	return e, s2, delay
}

// TestPerfStore measures the store (docs/validation/performance-113.md):
// TITANOS_PERF=1 go test ./internal/pim/store -run TestPerfStore -v
func TestPerfStore(t *testing.T) {
	pimtest.Enabled(t)
	e, s, delay := perfStore(t)
	month := [2]time.Time{time.Date(2026, 11, 1, 0, 0, 0, 0, s.local()), time.Date(2026, 12, 1, 0, 0, 0, 0, s.local())}
	week := [2]time.Time{time.Date(2026, 11, 16, 0, 0, 0, 0, s.local()), time.Date(2026, 11, 23, 0, 0, 0, 0, s.local())}
	query := func(w [2]time.Time) func() {
		return func() {
			for i := range 5 {
				if _, _, err := s.Query("alex", pimtest.Calendar(i), w[0], w[1], 0); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// The first read loads every state.json.
	start := time.Now()
	if _, err := s.Objects(Contacts, "alex", DefaultBook); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Collections(Calendar, "alex"); err != nil {
		t.Fatal(err)
	}
	t.Logf("%-58s %s", "first load of both state.json (cold RAM)", time.Since(start))
	query(month)() // the series parsed once

	pimtest.Bench(t, "Objects(contacts) 2000", func() { _, _ = s.Objects(Contacts, "alex", DefaultBook) })
	pimtest.Bench(t, "Query 5 calendars, one month", query(month))
	pimtest.Bench(t, "Query 5 calendars, one week", query(week))
	col, _ := s.GetCollection(Contacts, "alex", DefaultBook)
	opens := e.opens.Load()
	pimtest.Bench(t, "ChangesLimit(contacts, current token)", func() {
		if _, err := s.ChangesLimit(Contacts, "alex", DefaultBook, col.SyncToken, 0); err != nil {
			t.Fatal(err)
		}
	})
	pimtest.Bench(t, "GetCollection + Collections(calendar)", func() {
		_, _ = s.GetCollection(Contacts, "alex", DefaultBook)
		_, _ = s.Collections(Calendar, "alex")
	})
	if n := e.opens.Load() - opens; n != 0 {
		t.Errorf("an unchanged ctag/sync-token opened the volume %d times", n)
	}
	pimtest.Bench(t, "Get(contact with picture)", func() {
		if _, err := s.Get(Contacts, "alex", DefaultBook, "perf-contact-0000.vcf"); err != nil {
			t.Fatal(err)
		}
	})
	if err := s.SavePhoto("alex", "perf-contact-0000.vcf", mustStat(t, s, "perf-contact-0000.vcf"), pimtest.Photo()); err != nil {
		t.Fatal(err)
	}
	pimtest.Bench(t, "Photo(contact)", func() {
		if _, err := s.Photo("alex", "perf-contact-0000.vcf"); err != nil {
			t.Fatal(err)
		}
	})
	names := make([]string, 0, 200)
	for i := range 200 {
		names = append(names, fmt.Sprintf("perf-contact-%04d.vcf", i))
	}
	pimtest.Bench(t, "200 × Get (a multiget of 200)", func() {
		for _, n := range names {
			if _, err := s.Get(Contacts, "alex", DefaultBook, n); err != nil {
				t.Fatal(err)
			}
		}
	})

	data := pimtest.Contacts(3)[2].Data
	n := 0
	slowPut := func() {
		n++
		if _, err := s.Put(Contacts, "alex", DefaultBook, "perf-contact-0002.vcf", append(append([]byte(nil), data[:len(data)-len("END:VCARD\r\n")]...), fmt.Sprintf("NOTE:%d\r\nEND:VCARD\r\n", n)...), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	pimtest.Bench(t, "Put(contact) into a book of 2000 (file, fsync, state.json)", slowPut)
	pimtest.Blocked(t, "Objects(contacts) while Puts run (no delay)", 10, slowPut, func() { _, _ = s.Objects(Contacts, "alex", DefaultBook) })

	// A slow disk (200 ms per open of the volume) under one request; the
	// others read only RAM and should not wait for it.
	delay.Set(200 * time.Millisecond)
	defer delay.Set(0)
	list := func() { _, _ = s.Objects(Contacts, "alex", DefaultBook) }
	ctag := func() { _, _ = s.GetCollection(Contacts, "alex", DefaultBook) }
	cals := func() { _, _ = s.Collections(Contacts, "alex") }
	slowGet := func() { _, _ = s.Get(Contacts, "alex", DefaultBook, "perf-contact-0001.vcf") }
	slowPhoto := func() { _, _ = s.Photo("alex", "perf-contact-0000.vcf") }
	pimtest.Blocked(t, "Objects(contacts) while a Get waits for the disk", 10, slowGet, list)
	pimtest.Blocked(t, "GetCollection (ctag) while a Get waits for the disk", 10, slowGet, ctag)
	pimtest.Blocked(t, "Objects(contacts) while a Photo waits for the disk", 10, slowPhoto, list)
	pimtest.Blocked(t, "Objects(contacts) while a Put waits for the disk", 10, slowPut, list)
	pimtest.Blocked(t, "GetCollection (ctag) while a Put waits for the disk", 10, slowPut, ctag)
	pimtest.Blocked(t, "Collections(contacts) while a Put waits for the disk", 10, slowPut, cals)
}

func mustStat(t *testing.T, s *Store, name string) string {
	t.Helper()
	info, err := s.Stat(Contacts, "alex", DefaultBook, name)
	if err != nil {
		t.Fatal(err)
	}
	return info.ETag
}
