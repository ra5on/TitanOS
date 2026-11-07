package webapps

import (
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// ErrArchive is an app archive titand refuses to unpack.
var ErrArchive = errors.New("webapps: invalid app archive")

func archiveError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrArchive, fmt.Sprintf(format, args...))
}

// cleanEntry checks one name below the top folder: relative, no "." or
// ".." parts, no backslash, NUL or empty parts. A trailing slash marks a
// folder.
func cleanEntry(rel string) (string, bool) {
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" || len(rel) > 512 || strings.ContainsAny(rel, "\\\x00") || strings.HasPrefix(rel, "/") {
		return "", false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return "", false
		}
	}
	return rel, true
}

// Extract unpacks the archive of app id at version into dst, a new empty
// folder: every entry lies below exactly "<id>-web-<version>/" (the upstream
// layout), which is dropped. Only regular files and folders; no links,
// absolute names, "..", duplicates; at most MaxFiles files and MaxUnpacked
// bytes as actually read (not as the zip headers claim); index.html must
// be at the top. Files are 0644, folders 0755 (dst itself stays as made).
func Extract(zr *zip.Reader, id, version string, dst *os.Root) error {
	prefix := id + "-web-" + version + "/"
	seen := map[string]bool{}
	files := 0
	var total int64
	index := false
	for _, f := range zr.File {
		name := f.Name
		if !strings.HasPrefix(name, prefix) {
			return archiveError("entry %q outside %s", name, prefix)
		}
		if name == prefix {
			if !f.Mode().IsDir() {
				return archiveError("top entry is not a folder")
			}
			continue
		}
		rel, ok := cleanEntry(strings.TrimPrefix(name, prefix))
		if !ok {
			return archiveError("unsafe name %q", name)
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return archiveError("repeated name %q", name)
		}
		seen[key] = true
		mode := f.Mode()
		isDir := strings.HasSuffix(name, "/")
		switch {
		case isDir && mode.IsDir():
			if err := dst.MkdirAll(rel, 0o755); err != nil {
				return err
			}
			continue
		case isDir || mode.IsDir():
			return archiveError("folder %q is not marked as one", name)
		case !mode.IsRegular():
			return archiveError("%q is not a regular file", name)
		}
		files++
		if files > MaxFiles {
			return archiveError("more than %d files", MaxFiles)
		}
		if dir := path.Dir(rel); dir != "." {
			if err := dst.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
		n, err := extractFile(f, dst, rel, MaxUnpacked-total)
		if err != nil {
			return err
		}
		total += n
		if rel == "index.html" {
			index = true
		}
	}
	if !index {
		return archiveError("no index.html")
	}
	return nil
}

func extractFile(f *zip.File, dst *os.Root, rel string, budget int64) (int64, error) {
	if f.Method != zip.Store && f.Method != zip.Deflate {
		return 0, archiveError("%q: unsupported compression", f.Name)
	}
	in, err := f.Open()
	if err != nil {
		return 0, archiveError("%q: %v", f.Name, err)
	}
	defer in.Close()
	out, err := dst.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(in, budget+1))
	closeErr := out.Close()
	switch {
	case err != nil && (errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrFormat) || errors.Is(err, io.ErrUnexpectedEOF)):
		return 0, archiveError("%q: %v", f.Name, err)
	case err != nil:
		return 0, err
	case n > budget:
		return 0, archiveError("more than %d MiB unpacked", MaxUnpacked>>20)
	case closeErr != nil:
		return 0, closeErr
	}
	// Mode bits from the archive are ignored on purpose.
	return n, dst.Chmod(rel, 0o644)
}

// compressible are the types that get a gzip copy for serving.
var compressible = map[string]bool{".js": true, ".mjs": true, ".wasm": true, ".css": true, ".json": true,
	".svg": true, ".txt": true, ".md": true, ".webmanifest": true, ".ttf": true, ".otf": true, ".map": true}

// Precompress writes <file>.gz beside every compressible file of at least
// 1 KiB that has none, so that serving a 50 MB WebAssembly file costs no
// CPU (docs/WEBAPPS.md). index.html is rewritten when served and never
// has a copy.
func Precompress(dst *os.Root) error {
	var names []string
	err := fs.WalkDir(dst.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && compressible[strings.ToLower(path.Ext(name))] {
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, err := dst.Lstat(name + ".gz"); err == nil {
			continue
		}
		if err := gzipFile(dst, name); err != nil {
			return err
		}
	}
	return nil
}

func gzipFile(dst *os.Root, name string) error {
	in, err := dst.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || info.Size() < 1024 {
		return err
	}
	out, err := dst.OpenFile(name+".gz", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(out, gzip.DefaultCompression)
	_, err = io.Copy(zw, in)
	if e := zw.Close(); err == nil {
		err = e
	}
	if e := out.Close(); err == nil {
		err = e
	}
	if err != nil {
		dst.Remove(name + ".gz")
	}
	return err
}
