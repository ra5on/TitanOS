package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A save over WebDAV replaces the file only when all of it arrived: a lost
// connection, or fewer bytes than announced, leaves the old version as it
// was and no half file behind.
func TestWebDAVSaveKeepsTheOldVersionUntilComplete(t *testing.T) {
	m, root, _ := newRoot(t)
	dav := m.DAV(root, "alex", os.Getuid())
	file := filepath.Join(root.Dir, "notiz.txt")
	read := func() string { data, _ := os.ReadFile(file); return string(data) }
	leftovers := func() string {
		entries, _ := os.ReadDir(root.Dir)
		var parts []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), partSuffix) {
				parts = append(parts, e.Name())
			}
		}
		return strings.Join(parts, ",")
	}
	const flags = os.O_RDWR | os.O_CREATE | os.O_TRUNC

	// Fewer bytes than announced (the connection dropped).
	f, err := dav.OpenFile(WithDAVLength(context.Background(), 100), "/notiz.txt", flags, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("half"))
	if read() != "notiz" {
		t.Fatalf("the file changed while the new content was still arriving: %q", read())
	}
	if err := f.Close(); err == nil {
		t.Fatal("an incomplete save was accepted")
	}
	if read() != "notiz" || leftovers() != "" {
		t.Fatalf("after an incomplete save: content %q, part files %q", read(), leftovers())
	}

	// The client went away.
	ctx, cancel := context.WithCancel(context.Background())
	f, err = dav.OpenFile(ctx, "/notiz.txt", flags, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("gone"))
	cancel()
	if err := f.Close(); err == nil {
		t.Fatal("a save of a client that went away was accepted")
	}
	if read() != "notiz" || leftovers() != "" {
		t.Fatalf("after a cancelled save: content %q, part files %q", read(), leftovers())
	}

	// A complete save, of an existing and of a new file.
	for _, name := range []string{"/notiz.txt", "/neu.txt"} {
		f, err = dav.OpenFile(WithDAVLength(context.Background(), 11), name, flags, 0o666)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte("new content"))
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(filepath.Join(root.Dir, name)); string(data) != "new content" {
			t.Fatalf("%s after a complete save: %q", name, data)
		}
	}
	if leftovers() != "" {
		t.Fatalf("part files left: %q", leftovers())
	}
	// A folder is not replaced by a file.
	if _, err := dav.OpenFile(context.Background(), "/urlaub", flags, 0o666); err == nil {
		t.Fatal("a folder was opened for a save")
	}
}
