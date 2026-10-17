package files

import (
	"errors"
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
		"broken json": {"network/network.json", `{"name":`, ErrInvalid},
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
