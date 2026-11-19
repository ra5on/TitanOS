package appphotos

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// storeOn is a store on a volume "tank" below a temporary mount root;
// mounted false is the NAS without its system data volume.
func storeOn(t *testing.T) (*Store, string, *bool) {
	t.Helper()
	mnt := t.TempDir()
	os.Mkdir(filepath.Join(mnt, "tank"), 0755)
	mounted := true
	s := &Store{Volume: func(create bool) (*os.Root, func(), error) {
		if !mounted {
			return nil, nil, volumedata.ErrUnavailable
		}
		open := volumedata.OpenExisting
		if create {
			open = volumedata.Open
		}
		root, err := open(mnt, "tank", Feature, []string{"tank"})
		return root, func() {}, err
	}}
	return s, filepath.Join(mnt, "tank", ".titanos", Feature), &mounted
}

func TestRecordAndLookup(t *testing.T) {
	s, dir, _ := storeOn(t)
	// Nothing recorded yet: an empty index, and no folder made by reading.
	if got, err := s.Lookup("alex"); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("reading made the folder")
	}
	a := Entry{SHA256: sum("a"), Size: 1, Root: "share/Fotos", Path: "Handy/2026/11/IMG_1.jpg", ModTime: 1}
	if err := s.Record("alex", "dev1", a); err != nil {
		t.Fatal(err)
	}
	// The same picture from a second phone into another folder, and again
	// from the first phone with a new time.
	b := a
	b.Path = "Handy 2/IMG_1.jpg"
	s.Record("alex", "dev2", b)
	a.ModTime = 2
	s.Record("alex", "dev1", a)
	s.Record("anna", "dev3", Entry{SHA256: sum("c"), Size: 1, Root: "share/Fotos", Path: "c.jpg"})
	got, err := s.Lookup("alex")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[sum("a")]) != 2 {
		t.Fatalf("lookup: %+v", got)
	}
	for _, e := range got[sum("a")] {
		if e.Device == "dev1" && (e.ModTime != 2 || e.Path != a.Path) || e.Device == "dev2" && e.Path != b.Path {
			t.Fatalf("entry %+v", e)
		}
	}
	// Private: root's folders, 0700, the log 0600.
	for _, p := range []string{dir, filepath.Join(dir, "alex"), filepath.Join(dir, "alex", "dev1")} {
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0700 {
			t.Fatalf("%s: %v %v", p, st.Mode(), err)
		}
	}
	if st, err := os.Stat(filepath.Join(dir, "alex", "dev1", indexFile)); err != nil || st.Mode().Perm() != 0600 {
		t.Fatal(st, err)
	}
}

func TestRefusals(t *testing.T) {
	s, dir, mounted := storeOn(t)
	good := Entry{SHA256: sum("a"), Size: 1, Root: "share/Fotos", Path: "a.jpg"}
	for _, bad := range []struct {
		account, device string
		e               Entry
	}{
		{"../x", "dev", good}, {"alex", "../dev", good}, {"alex", "a/b", good}, {"Alex", "dev", good},
		{"alex", "dev", Entry{SHA256: "xyz", Size: 1, Root: "r", Path: "a"}},
		{"alex", "dev", Entry{SHA256: strings.ToUpper(sum("a")), Size: 1, Root: "r", Path: "a"}},
		{"alex", "dev", Entry{SHA256: sum("a"), Size: 1, Root: "r", Path: "../a"}},
		{"alex", "dev", Entry{SHA256: sum("a"), Size: 1, Root: "r", Path: "/a"}},
		{"alex", "dev", Entry{SHA256: sum("a"), Size: -1, Root: "r", Path: "a"}},
	} {
		if err := s.Record(bad.account, bad.device, bad.e); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	// Without the volume: an error, never an empty index.
	*mounted = false
	if _, err := s.Lookup("alex"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := s.Record("alex", "dev", good); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := s.Ready(); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	var none *Store
	if _, err := none.Lookup("alex"); !errors.Is(err, ErrUnavailable) || none.Forget("alex") != nil {
		t.Fatal("nil store")
	}
	*mounted = true
	// A folder others may write to is not trusted.
	if err := s.Record("alex", "dev", good); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(dir, "alex"), 0777)
	if _, err := s.Lookup("alex"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("open folder: %v", err)
	}
}

// A line cut short by a crash is skipped; many repeats are written anew.
func TestLogSurvivesAndCompacts(t *testing.T) {
	s, dir, _ := storeOn(t)
	e := Entry{SHA256: sum("a"), Size: 1, Root: "share/Fotos", Path: "a.jpg"}
	if err := s.Record("alex", "dev", e); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "alex", "dev", indexFile)
	f, _ := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"sha256":"` + sum("b") + `","size":`)
	f.Close()
	if got, err := s.Lookup("alex"); err != nil || len(got) != 1 {
		t.Fatalf("cut line: %v %v", got, err)
	}
	for i := range 1200 {
		e.ModTime = int64(i)
		if err := s.Record("alex", "dev", e); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.Lookup("alex"); err != nil || len(got[sum("a")]) != 1 || got[sum("a")][0].ModTime != 1199 {
		t.Fatalf("repeats: %v %v", got, err)
	}
	data, _ := os.ReadFile(log)
	if lines := strings.Count(string(data), "\n"); lines != 1 {
		t.Fatalf("not compacted: %d lines", lines)
	}
	if got, err := s.Lookup("alex"); err != nil || got[sum("a")][0].ModTime != 1199 {
		t.Fatalf("after compacting: %v %v", got, err)
	}
}

func TestForget(t *testing.T) {
	s, dir, _ := storeOn(t)
	for i := range 3 {
		s.Record("alex", fmt.Sprintf("dev%d", i), Entry{SHA256: sum("a"), Size: 1, Root: "share/Fotos", Path: "a.jpg"})
	}
	s.Record("anna", "dev", Entry{SHA256: sum("a"), Size: 1, Root: "share/Fotos", Path: "a.jpg"})
	if err := s.Forget("alex"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alex")); !os.IsNotExist(err) {
		t.Fatal("alex kept")
	}
	if got, _ := s.Lookup("anna"); len(got) != 1 {
		t.Fatal("anna lost")
	}
	if err := s.Forget("nobody"); err != nil {
		t.Fatal(err)
	}
}
