package fileindex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/office"
)

func TestTokens(t *testing.T) {
	got := Tokens("Rechnung Nr. 2026-17: Größe STRAßE, rechnung a ÄRGER")
	want := []string{"rechnung", "nr", "2026", "17", "größe", "straße", "ärger"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	if len(Tokens(strings.Repeat("x", 100))) != 0 {
		t.Fatal("overlong word kept")
	}
}

type fixture struct {
	m    *Manager
	base string
	vol  string
}

func newFixture(t *testing.T, settings Settings) fixture {
	t.Helper()
	base := t.TempDir()
	vol := filepath.Join(base, "data")
	os.Mkdir(vol, 0700)
	m := New(config.Open(t.TempDir()))
	m.MountRoot, m.Mounted = base, func() []string { return []string{"data"} }
	m.ShareVolume = func(name string) (string, bool) { return "data", name == "docs" || name == "other" }
	m.pause = 0
	m.extract.pdf = func(_ context.Context, data []byte, first, last int) (office.PDFReading, error) {
		if first > 1 {
			return office.PDFReading{}, errors.New("no such page")
		}
		return office.PDFReading{Pages: []office.PDFPage{{Number: 1, Text: string(data[5:])}}}, nil
	}
	if err := m.Set(settings); err != nil {
		t.Fatal(err)
	}
	return fixture{m, base, vol}
}

func (f fixture) write(t *testing.T, rel, text string) {
	t.Helper()
	full := filepath.Join(f.vol, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(full), 0700)
	if err := os.WriteFile(full, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) search(t *testing.T, prefixes []string, q string) []string {
	t.Helper()
	hits, _, err := f.m.Search(context.Background(), "data", prefixes, q, 100)
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	var out []string
	for _, hit := range hits {
		out = append(out, hit.Path)
	}
	return out
}

func TestIndexAddUpdateDelete(t *testing.T) {
	f := newFixture(t, Settings{Enabled: true, Volumes: []string{"data"}})
	f.write(t, "docs/a.txt", "Rechnung Strom 2026")
	f.write(t, "docs/b.md", "Einkaufsliste: Milch, Brot")
	f.write(t, "other/c.pdf", "%PDF-Vertrag Wohnung")
	f.write(t, "docs/.hidden/x.txt", "Rechnung geheim")
	f.write(t, "docs/photo.jpg", "Rechnung")
	f.write(t, ".titanos/secret.txt", "Rechnung intern")
	f.m.pass(context.Background(), "data", []string{""}, false)
	st := f.m.Status().Volumes[0]
	if st.LastError != "" || st.Files != 3 || !st.Known {
		t.Fatalf("status %+v", st)
	}
	if got := f.search(t, []string{""}, "rechn"); !slices.Equal(got, []string{"docs/a.txt"}) {
		t.Fatalf("prefix search: %v", got)
	}
	if got := f.search(t, []string{""}, "vertrag wohnung"); !slices.Equal(got, []string{"other/c.pdf"}) {
		t.Fatalf("pdf: %v", got)
	}
	if got := f.search(t, []string{""}, "vertrag milch"); len(got) != 0 {
		t.Fatalf("words must all match: %v", got)
	}
	// File names are searchable too.
	if got := f.search(t, []string{""}, "b md"); !slices.Equal(got, []string{"docs/b.md"}) {
		t.Fatalf("name: %v", got)
	}
	// Changed content replaces the old words.
	f.write(t, "docs/a.txt", "Gutschrift Wasser")
	future := time.Now().Add(time.Minute)
	os.Chtimes(filepath.Join(f.vol, "docs/a.txt"), future, future)
	f.m.pass(context.Background(), "data", []string{""}, false)
	if got := f.search(t, []string{""}, "rechnung"); len(got) != 0 {
		t.Fatalf("old words kept: %v", got)
	}
	if got := f.search(t, []string{""}, "gutschrift"); !slices.Equal(got, []string{"docs/a.txt"}) {
		t.Fatalf("new words: %v", got)
	}
	if st := f.m.Status().Volumes[0]; st.Updated != 1 {
		t.Fatalf("unchanged files were read again: %+v", st)
	}
	// Deleted files are forgotten.
	os.Remove(filepath.Join(f.vol, "other/c.pdf"))
	f.m.pass(context.Background(), "data", []string{""}, false)
	if got := f.search(t, []string{""}, "vertrag"); len(got) != 0 {
		t.Fatalf("deleted file found: %v", got)
	}
	if st := f.m.Status().Volumes[0]; st.Files != 2 {
		t.Fatalf("status after delete %+v", st)
	}
}

// Hits are limited to the folders the caller may see; the database itself
// knows nothing about accounts.
func TestSearchFiltersByFolder(t *testing.T) {
	f := newFixture(t, Settings{Enabled: true, Shares: []string{"docs", "other"}})
	f.write(t, "docs/a.txt", "Projekt Titan")
	f.write(t, "other/b.txt", "Projekt Titan")
	f.write(t, "docsextra/c.txt", "Projekt Titan")
	f.m.pass(context.Background(), "data", f.m.scope(f.m.Settings())["data"], false)
	if got := f.search(t, []string{"docs"}, "titan"); !slices.Equal(got, []string{"docs/a.txt"}) {
		t.Fatalf("docs: %v", got)
	}
	if got := f.search(t, []string{"other"}, "projekt"); !slices.Equal(got, []string{"other/b.txt"}) {
		t.Fatalf("other: %v", got)
	}
	// A folder outside the chosen shares is not indexed and not searchable.
	if _, _, err := f.m.Search(context.Background(), "data", []string{"docsextra"}, "titan", 10); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unindexed share: %v", err)
	}
	if got := f.search(t, []string{""}, "titan"); !slices.Equal(got, []string{"docs/a.txt", "other/b.txt"}) {
		t.Fatalf("volume: %v", got)
	}
	// Taking a share out of the scope forgets it with the next pass.
	f.m.Set(Settings{Enabled: true, Shares: []string{"docs"}})
	f.m.pass(context.Background(), "data", f.m.scope(f.m.Settings())["data"], false)
	if got := f.search(t, []string{""}, "titan"); !slices.Equal(got, []string{"docs/a.txt"}) {
		t.Fatalf("after scope change: %v", got)
	}
	if f.m.Covers("data", "other") || !f.m.Covers("data", "docs/sub") || !f.m.Covers("data", "") {
		t.Fatal("covers")
	}
	if _, _, err := f.m.Search(context.Background(), "data", []string{"docs"}, "!!", 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty query: %v", err)
	}
	f.m.Set(Settings{})
	if _, _, err := f.m.Search(context.Background(), "data", []string{""}, "titan", 10); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("switched off: %v", err)
	}
}

func TestSettingsValidation(t *testing.T) {
	f := newFixture(t, Settings{})
	for _, bad := range []Settings{{Time: "25:00"}, {Volumes: []string{"../x"}}, {Shares: []string{"a/b"}}, {Shares: []string{".x"}}} {
		if err := f.m.Set(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v accepted", bad)
		}
	}
	if err := f.m.Set(Settings{Enabled: true, Volumes: []string{"data", "data"}, Time: "03:15", OCR: true}); err != nil {
		t.Fatal(err)
	}
	again := New(f.m.store)
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if s := again.Settings(); !s.Enabled || !slices.Equal(s.Volumes, []string{"data"}) || s.Time != "03:15" || !s.OCR {
		t.Fatalf("%+v", s)
	}
	fresh := New(config.Open(t.TempDir()))
	if err := fresh.Load(); err != nil || fresh.Settings().Enabled {
		t.Fatal("index must be off by default")
	}
}

// Passes start only when other I/O shows the volume is in use, not again
// within the minimum gap, and never because of their own I/O.
func TestPassesFollowVolumeActivity(t *testing.T) {
	f := newFixture(t, Settings{Enabled: true, Volumes: []string{"data"}})
	f.write(t, "a.txt", "hallo welt")
	proc, sys := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(proc, "self"), 0700)
	os.WriteFile(filepath.Join(proc, "self/mountinfo"), []byte("40 1 9:127 / "+f.vol+" rw,noatime shared:1 - ext4 /dev/md127 rw\n"), 0600)
	os.MkdirAll(filepath.Join(sys, "class/block/md127/slaves/sda1"), 0700)
	os.MkdirAll(filepath.Join(sys, "class/block/sda1"), 0700)
	stat := filepath.Join(sys, "class/block/sda1/stat")
	setIO := func(n string) {
		os.WriteFile(stat, []byte(n+" 0 0 0 5 0 0 0 0 0 0 0 0 0 0 0 0\n"), 0600)
	}
	setIO("100")
	f.m.io = ioCounter{proc: proc, sys: sys}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	f.m.now = func() time.Time { return now }
	ran := func() bool { return f.m.Status().Volumes[0].Known }
	f.m.tick(context.Background())
	if ran() {
		t.Fatal("pass without activity")
	}
	now = now.Add(time.Minute)
	f.m.tick(context.Background())
	if ran() {
		t.Fatal("pass on an idle volume")
	}
	setIO("200")
	now = now.Add(time.Minute)
	f.m.tick(context.Background())
	if !ran() {
		t.Fatal("no pass on an active volume")
	}
	f.m.mu.Lock()
	delete(f.m.status, "data")
	f.m.mu.Unlock()
	setIO("300")
	now = now.Add(5 * time.Minute)
	f.m.tick(context.Background())
	if ran() {
		t.Fatal("pass again within the minimum gap")
	}
	if n, ok := f.m.io.read(f.vol); !ok || n != 305 {
		t.Fatalf("io counter %d %v", n, ok)
	}
	// A manual request runs at once, also on an idle volume.
	if err := f.m.Start("data"); err != nil {
		t.Fatal(err)
	}
	f.m.tick(context.Background())
	if !ran() {
		t.Fatal("manual pass did not run")
	}
	if err := f.m.Start("elsewhere"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("pass of a volume outside the scope")
	}
}

func TestDailyTime(t *testing.T) {
	f := newFixture(t, Settings{Enabled: true, Volumes: []string{"data"}, Time: "03:00"})
	f.m.io = ioCounter{proc: t.TempDir(), sys: t.TempDir()} // activity unknown
	f.m.Timezone = func() string { return "UTC" }
	now := time.Date(2026, 5, 1, 2, 59, 0, 0, time.UTC)
	f.m.now = func() time.Time { return now }
	ran := func() bool { return f.m.Status().Volumes[0].Known }
	f.m.tick(context.Background())
	if ran() {
		t.Fatal("early")
	}
	now = now.Add(2 * time.Minute)
	f.m.tick(context.Background())
	if !ran() {
		t.Fatal("daily pass missing")
	}
	f.m.mu.Lock()
	delete(f.m.status, "data")
	f.m.mu.Unlock()
	now = now.Add(10 * time.Minute)
	f.m.tick(context.Background())
	if ran() {
		t.Fatal("daily pass twice")
	}
}

func TestRemoveIndex(t *testing.T) {
	f := newFixture(t, Settings{Enabled: true, Volumes: []string{"data"}})
	f.write(t, "a.txt", "hallo")
	f.m.pass(context.Background(), "data", []string{""}, false)
	if err := f.m.Remove("data"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.vol, ".titanos/fileindex", dbName)); !os.IsNotExist(err) {
		t.Fatal("index file kept")
	}
	if _, _, err := f.m.Search(context.Background(), "data", []string{""}, "hallo", 10); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("search without index: %v", err)
	}
	// Searching never creates an index.
	if _, err := os.Stat(filepath.Join(f.vol, ".titanos/fileindex", dbName)); !os.IsNotExist(err) {
		t.Fatal("search created the index")
	}
}
