package media

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHasExternalData(t *testing.T) {
	for name, tc := range map[string]struct {
		info string
		want bool
	}{
		"plain":            {`{"virtual-size": 1, "filename": "/proc/self/fd/3"}`, false},
		"data file":        {`{"format-specific": {"data": {"data-file": "/srv/x.raw"}}}`, true},
		"empty data file":  {`{"format-specific": {"data": {"data-file": ""}}}`, false},
		"other filename":   {`{"children": [{"info": {"filename": "/etc/shadow"}}]}`, true},
		"nested own":       {`{"children": [{"info": {"filename": "/proc/self/fd/3"}}]}`, false},
		"list of nothings": {`[1, "a", null]`, false},
	} {
		var v any
		if err := json.Unmarshal([]byte(tc.info), &v); err != nil {
			t.Fatal(err)
		}
		if got := hasExternalData(v); got != tc.want {
			t.Errorf("%s: %t", name, got)
		}
	}
}

func TestZipDirectory(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{"a.iso", "readme.txt", "dir/b.txt"} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte("content of " + name))
	}
	w.SetComment("a comment the end record carries")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	entries, dirSize, err := zipDirectory(bytes.NewReader(data), int64(len(data)))
	if err != nil || entries != 3 || dirSize == 0 || dirSize > uint64(len(data)) {
		t.Fatalf("entries %d, directory %d, %v", entries, dirSize, err)
	}
	if _, _, err := zipDirectory(bytes.NewReader([]byte("not a zip at all, just text")), 27); !errors.Is(err, ErrInvalid) {
		t.Fatalf("not a zip: %v", err)
	}
	// An end record that announces ZIP64 without a locator keeps the 32-bit values.
	z64 := bytes.Clone(data)
	end := bytes.LastIndex(z64, []byte{0x50, 0x4b, 0x05, 0x06})
	copy(z64[end+16:], []byte{0xff, 0xff, 0xff, 0xff})
	if entries, _, err := zipDirectory(bytes.NewReader(z64), int64(len(z64))); err != nil || entries != 3 {
		t.Fatalf("ZIP64 marker without locator: %d %v", entries, err)
	}
}

func TestValidateISO(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, at int64, signature string) *os.File {
		path := filepath.Join(dir, name)
		data := make([]byte, 40*2048)
		if signature != "" {
			copy(data[at:], signature)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	ctx := context.Background()
	if err := validateImage(ctx, write("iso9660.iso", 16*2048+1, "CD001"), "iso9660.iso"); err != nil {
		t.Fatalf("ISO9660: %v", err)
	}
	if err := validateImage(ctx, write("udf.ISO", 20*2048+1, "NSR03"), "udf.ISO"); err != nil {
		t.Fatalf("UDF: %v", err)
	}
	if err := validateImage(ctx, write("empty.iso", 0, ""), "empty.iso"); err == nil {
		t.Fatal("ISO without descriptor accepted")
	}
	if err := validateImage(ctx, write("late.iso", 33*2048+1, "CD001"), "late.iso"); err == nil {
		t.Fatal("descriptor outside sectors 16-31 accepted")
	}
	if err := validateImage(ctx, write("x.exe", 0, ""), "x.exe"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown type: %v", err)
	}
}

func TestCappedBuffer(t *testing.T) {
	var b cappedBuffer
	if _, err := b.Write(make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte{1}); err == nil {
		t.Fatal("write over 64 KiB accepted")
	}
}
