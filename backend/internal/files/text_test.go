package files

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stickRoot(t *testing.T) (*Manager, Root) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	for _, d := range []string{"network", "users/alex", "apps/blog", "tls"} {
		os.MkdirAll(filepath.Join(dir, d), 0o755)
	}
	write := func(name, content string) { os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644) }
	write("network/network.json", `{"name":"nas"}`)
	write("users/alex/account.json", `{"hash":"secret"}`)
	write("apps/blog/compose.yaml", "services: {}\n")
	write("apps/blog/.env", "DB_PASSWORD=secret\n")
	write("tls/key.pem", "private")
	m := NewManager(os.Getgid())
	return m, Root{ID: "stick", Name: "TITANOS", Kind: "stick", Dir: dir, Editable: true, Hide: []string{"users", "tls", "ssh", "notify"}}
}

func TestStickHidesSecrets(t *testing.T) {
	m, root := stickRoot(t)
	top, err := m.List(root, "")
	if err != nil || names(top) != "apps,network" {
		t.Errorf("top level: %q, %v", names(top), err)
	}
	if _, err := m.List(root, "users"); !errors.Is(err, ErrNotFound) {
		t.Errorf("listing users: %v", err)
	}
	if blog, _ := m.List(root, "apps/blog"); names(blog) != "compose.yaml" {
		t.Errorf(".env shown: %q", names(blog))
	}
	for _, p := range []string{"users/alex/account.json", "tls/key.pem", "apps/blog/.env"} {
		if _, _, err := m.Open(root, p); !errors.Is(err, ErrNotFound) {
			t.Errorf("Open(%s): %v", p, err)
		}
	}
	var out strings.Builder
	if err := m.Zip(&out, root, []string{"apps"}); err != nil || strings.Contains(out.String(), "DB_PASSWORD") {
		t.Errorf("zip: %v", err)
	}
	if err := m.Zip(&out, root, []string{"users"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("zip of users: %v", err)
	}
	if _, err := m.StartCopy(root, []string{"network"}, Root{Dir: t.TempDir(), Writable: true}, "", false, "", "alex", 1); !errors.Is(err, ErrProtected) {
		t.Errorf("copy from the stick: %v", err)
	}
}

func TestSaveText(t *testing.T) {
	m, root := stickRoot(t)
	file := filepath.Join(root.Dir, "network/network.json")
	if err := m.SaveText(root, "network/network.json", `{"name":"neu"}`); err != nil {
		t.Fatal(err)
	}
	if read(t, file) != `{"name":"neu"}` || read(t, file+".prev") != `{"name":"nas"}` {
		t.Error("content or backup copy")
	}
	for name, tc := range map[string]struct {
		path, content string
		want          error
	}{
		"broken json": {"network/network.json", `{"name":`, ErrInvalidJSON},
		"new file":    {"network/other.json", `{}`, ErrNotFound},
		"secret":      {"users/alex/account.json", `{}`, ErrNotFound},
		"env":         {"apps/blog/.env", "X=1", ErrNotFound},
		"backup copy": {"network/network.json.prev", "x", ErrInvalid},
		"leaves":      {"../x", "x", ErrInvalid},
		"binary":      {"apps/blog/compose.yaml", "a\x00b", ErrInvalid},
		"too big":     {"apps/blog/compose.yaml", strings.Repeat("a", maxText+1), ErrInvalid},
		"a folder":    {"apps", "x", ErrInvalid},
	} {
		if err := m.SaveText(root, tc.path, tc.content); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	if read(t, file) != `{"name":"neu"}` {
		t.Error("a refused save changed the file")
	}
	root.Editable = false
	if err := m.SaveText(root, "apps/blog/compose.yaml", "x"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("not editable: %v", err)
	}
}

func TestTextEditingWritableRootsAndConflicts(t *testing.T) {
	m, root := stickRoot(t)
	root.Kind = "share"
	root.Editable = false
	root.Writable = true
	name := "apps/blog/compose.yaml"
	opened, err := m.ReadText(root, name)
	if err != nil {
		t.Fatal(err)
	}
	next, err := m.SaveTextVersion(root, name, "services: {web: {}}\n", opened.Version)
	if err != nil {
		t.Fatal(err)
	}
	if next.Version == opened.Version {
		t.Fatal("version did not change")
	}
	if _, err = m.SaveTextVersion(root, name, "stale\n", opened.Version); !errors.Is(err, ErrTextConflict) {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root.Dir, name)); got != next.Content {
		t.Fatal(got)
	}
	root.Writable = false
	if _, err = m.ReadText(root, name); !errors.Is(err, ErrReadOnly) {
		t.Fatal(err)
	}
	root.Writable = true
	m.Protected = func(string) bool { return true }
	if _, err = m.SaveTextVersion(root, name, "x", next.Version); !errors.Is(err, ErrProtected) {
		t.Fatal(err)
	}
}
func TestTextRejectsBinaryAndPreservesOriginalOnFailure(t *testing.T) {
	m, root := stickRoot(t)
	name := "apps/blog/test.txt"
	target := filepath.Join(root.Dir, name)
	for _, data := range [][]byte{{0xff}, {'a', 0, 'b'}, []byte(strings.Repeat("x", maxText+1))} {
		os.WriteFile(target, data, 0600)
		if _, err := m.ReadText(root, name); !errors.Is(err, ErrNotText) {
			t.Fatal(err)
		}
	}
	os.WriteFile(target, []byte("original"), 0644)
	opened, err := m.ReadText(root, name)
	if err != nil {
		t.Fatal(err)
	}
	m.syncFile = func(*os.File) error { return errors.New("disk unavailable") }
	if _, err = m.SaveTextVersion(root, name, "replacement", opened.Version); err == nil {
		t.Fatal("ignored write failure")
	}
	if read(t, target) != "original" {
		t.Fatal("changed original")
	}
}
func TestTextBackupDoesNotFollowSymlink(t *testing.T) {
	m, root := stickRoot(t)
	name := "apps/blog/compose.yaml"
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("untouched"), 0600)
	if err := os.Symlink(outside, filepath.Join(root.Dir, name+".prev")); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveText(root, name, "services: {} # changed\n"); err != nil {
		t.Fatal(err)
	}
	if read(t, outside) != "untouched" {
		t.Fatal("backup followed symlink")
	}
}

func TestTextPreservesUserMetadata(t *testing.T) {
	m, root := stickRoot(t)
	name := "apps/blog/compose.yaml"
	f, err := os.Open(filepath.Join(root.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	err = unix.Fsetxattr(int(f.Fd()), "user.test", []byte("keep"), 0)
	f.Close()
	if errors.Is(err, unix.ENOTSUP) {
		t.Skip("xattrs unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SaveText(root, name, "services: {} # edited\n"); err != nil {
		t.Fatal(err)
	}
	f, err = os.Open(filepath.Join(root.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data := make([]byte, 32)
	n, err := unix.Fgetxattr(int(f.Fd()), "user.test", data)
	if err != nil || string(data[:n]) != "keep" {
		t.Fatal(n, err)
	}
}
