package files

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Apps in appdata often link their config with an absolute path, e.g.
// /mnt/data/appdata/app/config.yaml. Those links must open, download and
// save like the file itself; links out of the place must stay closed.
func linkFixture(t *testing.T) (*Manager, Root, string) {
	t.Helper()
	volume := t.TempDir()
	outside := t.TempDir()
	app := filepath.Join(volume, "appdata", "app")
	if err := os.MkdirAll(filepath.Join(app, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(app, "conf", "real.yaml"), "a: 1\n")
	write(filepath.Join(outside, "secret.yaml"), "nope: 1\n")
	link := func(target, name string) {
		if err := os.Symlink(target, filepath.Join(app, name)); err != nil {
			t.Fatal(err)
		}
	}
	link(filepath.Join(app, "conf", "real.yaml"), "abs.yaml")
	link("conf/real.yaml", "rel.yaml")
	link(filepath.Join(outside, "secret.yaml"), "out.yaml")
	link("../../../../../../../../etc/hostname", "up.yaml")
	link(filepath.Join(app, "missing.yaml"), "dangling.yaml")
	return NewManager(os.Getgid()), Root{ID: "volume/data", Kind: "volume", Dir: volume, Writable: true}, app
}

func TestOpenFollowsLinksInsideTheRoot(t *testing.T) {
	m, root, _ := linkFixture(t)
	for _, name := range []string{"abs.yaml", "rel.yaml"} {
		f, info, err := m.Open(root, "appdata/app/"+name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		data, _ := io.ReadAll(f)
		f.Close()
		if string(data) != "a: 1\n" || info.Name() != "real.yaml" {
			t.Fatalf("%s: %q %s", name, data, info.Name())
		}
	}
}

func TestLinksOutOfTheRootStayClosed(t *testing.T) {
	m, root, _ := linkFixture(t)
	for _, name := range []string{"out.yaml", "up.yaml"} {
		if _, _, err := m.Open(root, "appdata/app/"+name); !errors.Is(err, ErrLinkOutside) {
			t.Fatalf("open %s: %v", name, err)
		}
		if _, err := m.ReadText(root, "appdata/app/"+name); !errors.Is(err, ErrLinkOutside) {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := m.SaveTextVersion(root, "appdata/app/"+name, "x: 1\n", ""); !errors.Is(err, ErrLinkOutside) {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	if _, _, err := m.Open(root, "appdata/app/dangling.yaml"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dangling: %v", err)
	}
}

// A share is its own place: a link to another folder of the same volume
// would reach data the account may have no right to.
func TestShareLinksStayInTheShare(t *testing.T) {
	m, volume, app := linkFixture(t)
	share := Root{ID: "share/app", Kind: "share", Dir: filepath.Join(volume.Dir, "appdata", "app", "conf"), Writable: true}
	if err := os.Symlink(filepath.Join(app, "abs.yaml"), filepath.Join(share.Dir, "via.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Open(share, "via.yaml"); err != nil {
		t.Fatalf("link inside the share: %v", err)
	}
	if err := os.Symlink(filepath.Join(app, "rel.yaml"), filepath.Join(share.Dir, "..", "x")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(volume.Dir, "appdata", "app", "x"), filepath.Join(share.Dir, "side.yaml")); err != nil {
		t.Fatal(err)
	}
	// side.yaml → ../x → rel.yaml → conf/real.yaml ends inside the share again: allowed.
	if _, _, err := m.Open(share, "side.yaml"); err != nil {
		t.Fatalf("chain back into the share: %v", err)
	}
	other := filepath.Join(volume.Dir, "appdata", "other.yaml")
	if err := os.WriteFile(other, []byte("o: 1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(share.Dir, "other.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Open(share, "other.yaml"); !errors.Is(err, ErrLinkOutside) {
		t.Fatalf("link to another folder of the volume: %v", err)
	}
}

func TestEditingALinkKeepsTheLink(t *testing.T) {
	m, root, app := linkFixture(t)
	text, err := m.ReadText(root, "appdata/app/abs.yaml")
	if err != nil || text.Content != "a: 1\n" {
		t.Fatalf("read: %q %v", text.Content, err)
	}
	if _, err := m.SaveTextVersion(root, "appdata/app/abs.yaml", "a: 2\n", text.Version); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Lstat(filepath.Join(app, "abs.yaml"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v %v", info.Mode(), err)
	}
	data, _ := os.ReadFile(filepath.Join(app, "conf", "real.yaml"))
	if string(data) != "a: 2\n" {
		t.Fatalf("target not saved: %q", data)
	}
	if _, err := os.Stat(filepath.Join(app, "conf", "real.yaml.prev")); err != nil {
		t.Fatalf("previous version beside the target: %v", err)
	}
}
