package thumbs

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// exifJPEG returns a w×h JPEG whose EXIF block says orientation and date.
func exifJPEG(t *testing.T, w, h, orientation int, taken string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w / 2 {
		for y := range h {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, img, nil); err != nil {
		t.Fatal(err)
	}
	// TIFF, big endian: IFD0 with orientation and a pointer to the EXIF IFD,
	// which holds DateTimeOriginal.
	var tiff bytes.Buffer
	be := binary.BigEndian
	tiff.WriteString("MM")
	binary.Write(&tiff, be, uint16(42))
	binary.Write(&tiff, be, uint32(8))
	binary.Write(&tiff, be, uint16(2))
	binary.Write(&tiff, be, []uint16{0x0112, 3})
	binary.Write(&tiff, be, uint32(1))
	binary.Write(&tiff, be, []uint16{uint16(orientation), 0})
	binary.Write(&tiff, be, []uint16{0x8769, 4})
	binary.Write(&tiff, be, uint32(1))
	binary.Write(&tiff, be, uint32(38))
	binary.Write(&tiff, be, uint32(0))
	// EXIF IFD at 38: one entry, the date at 56.
	binary.Write(&tiff, be, uint16(1))
	binary.Write(&tiff, be, []uint16{0x9003, 2})
	binary.Write(&tiff, be, uint32(20))
	binary.Write(&tiff, be, uint32(56))
	binary.Write(&tiff, be, uint32(0))
	tiff.WriteString(taken + "\x00")
	segment := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var out bytes.Buffer
	out.Write([]byte{0xFF, 0xD8, 0xFF, 0xE1})
	binary.Write(&out, be, uint16(len(segment)+2))
	out.Write(segment)
	out.Write(plain.Bytes()[2:])
	return out.Bytes()
}

func source(t *testing.T, dir, name string, data []byte) Source {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, data, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(full)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	info, _ := f.Stat()
	return Source{Rel: name, Name: name, File: f, Info: info}
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestOrientationAndDateFromEXIF(t *testing.T) {
	data := exifJPEG(t, 400, 200, 6, "2025:07:14 18:30:05")
	photo := ReadPhoto(bytes.NewReader(data))
	if photo.Orientation != 6 || !photo.Taken.Equal(time.Date(2025, 7, 14, 18, 30, 5, 0, time.UTC)) {
		t.Fatalf("photo %+v", photo)
	}
	m := New()
	thumb, kind, err := m.Get(context.Background(), source(t, t.TempDir(), "a.JPG", data), 100)
	if err != nil || kind != "image/jpeg" {
		t.Fatal(kind, err)
	}
	b := decode(t, thumb).Bounds()
	// 400×200 fits 160×80, turned by 90° to 80×160.
	if b.Dx() != 80 || b.Dy() != 160 {
		t.Fatalf("thumbnail %v", b)
	}
	if ReadPhoto(bytes.NewReader([]byte("not a jpeg"))) != (Photo{}) {
		t.Fatal("garbage yielded EXIF data")
	}
	// A truncated EXIF block must not panic.
	for n := 4; n < 120; n++ {
		parseJPEG(data[:n])
	}
}

func TestTransparentPictureStaysPNG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 50, 30))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	thumb, kind, err := New().Get(context.Background(), source(t, t.TempDir(), "x.png", buf.Bytes()), 160)
	if err != nil || kind != "image/png" {
		t.Fatal(kind, err)
	}
	if b := decode(t, thumb).Bounds(); b.Dx() != 50 {
		t.Fatalf("a small picture was enlarged: %v", b)
	}
}

func TestUnsupportedAndMissingHelper(t *testing.T) {
	m := New()
	m.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	dir := t.TempDir()
	if _, _, err := m.Get(context.Background(), source(t, dir, "clip.mp4", []byte("....")), 160); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("video without ffmpeg: %v", err)
	}
	if _, _, err := m.Get(context.Background(), source(t, dir, "notes.txt", []byte("hi")), 160); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("text file: %v", err)
	}
	if _, _, err := m.Get(context.Background(), source(t, dir, "broken.png", []byte("no png")), 160); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("broken picture: %v", err)
	}
	if Kind("a.HEIC") != "" || Kind("b.webp") != "image" || Kind("c.pdf") != "pdf" || Kind("d.MKV") != "video" {
		t.Fatal("kinds")
	}
	if FitSize(1) != 160 || FitSize(321) != 640 || FitSize(99999) != 1280 {
		t.Fatal("sizes")
	}
}

// The PDF helper gets the opened file on stdin, never a path from the
// request, and its PNG answer is scaled like any picture.
func TestPDFThroughHelper(t *testing.T) {
	dir := t.TempDir()
	page := image.NewRGBA(image.Rect(0, 0, 300, 600))
	var buf bytes.Buffer
	png.Encode(&buf, page)
	pngFile := filepath.Join(dir, "page.png")
	os.WriteFile(pngFile, buf.Bytes(), 0600)
	script := filepath.Join(dir, "pdftoppm")
	os.WriteFile(script, []byte("#!/bin/sh\nfor a; do last=$a; done\n[ \"$last\" = - ] || exit 3\nhead -c 5 | grep -q '%PDF-' || exit 4\ncat "+pngFile+"\n"), 0700)
	m := New()
	m.lookPath = func(name string) (string, error) {
		if name == "pdftoppm" {
			return script, nil
		}
		return "", exec.ErrNotFound
	}
	thumb, _, err := m.Get(context.Background(), source(t, dir, "doc.pdf", []byte("%PDF-1.7 ...")), 160)
	if err != nil {
		t.Fatal(err)
	}
	if b := decode(t, thumb).Bounds(); b.Dx() != 80 || b.Dy() != 160 {
		t.Fatalf("pdf thumbnail %v", b)
	}
}

func TestCacheOnVolume(t *testing.T) {
	base := t.TempDir()
	os.Mkdir(filepath.Join(base, "data"), 0700)
	m := New()
	m.MountRoot, m.Mounted = base, func() []string { return []string{"data"} }
	data := exifJPEG(t, 64, 64, 1, "2020:01:01 00:00:00")
	src := source(t, filepath.Join(base, "data"), "p.jpg", data)
	src.Volume = "data"
	first, _, err := m.Get(context.Background(), src, 160)
	if err != nil {
		t.Fatal(err)
	}
	key := Key("p.jpg", src.Info, 160)
	cached := filepath.Join(base, "data", ".titanos", "thumbs", cachePath(key))
	if st, err := os.Stat(cached); err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("not cached: %v", err)
	}
	// A hit does not read the file again.
	src.File.Close()
	again, _, err := m.Get(context.Background(), src, 160)
	if err != nil || !bytes.Equal(first, again) {
		t.Fatalf("cache hit: %v", err)
	}
	// Any change of the file makes a different key.
	os.Chtimes(filepath.Join(base, "data", "p.jpg"), time.Now(), time.Now().Add(time.Hour))
	changed, _ := os.Stat(filepath.Join(base, "data", "p.jpg"))
	if Key("p.jpg", changed, 160) == key || Key("q.jpg", src.Info, 160) == key || Key("p.jpg", src.Info, 320) == key {
		t.Fatal("key does not follow the file")
	}
	// Without the volume mounted nothing is read from or written below it.
	m.Mounted = func() []string { return nil }
	if _, ok := m.cached("data", key); ok {
		t.Fatal("cache of an unmounted volume used")
	}
}

func TestCacheTrimKeepsRecent(t *testing.T) {
	base := t.TempDir()
	os.Mkdir(filepath.Join(base, "data"), 0700)
	m := New()
	m.MountRoot, m.Mounted = base, func() []string { return []string{"data"} }
	m.Limit = 4000
	start := time.Now().Add(-time.Hour)
	for i := range 10 {
		key := Key("f"+string(rune('a'+i)), fakeInfo{}, 160)
		m.now = func() time.Time { return start.Add(time.Duration(i) * time.Minute) }
		m.store("data", key, bytes.Repeat([]byte{1}, 1000))
		root, _ := m.open("data")
		root.Chtimes(cachePath(key), start.Add(time.Duration(i)*time.Minute), start.Add(time.Duration(i)*time.Minute))
		root.Close()
	}
	root, _ := m.open("data")
	defer root.Close()
	m.trim("data", root)
	if used := m.used["data"]; used > m.Limit {
		t.Fatalf("cache holds %d bytes", used)
	}
	if _, ok := m.cached("data", Key("fj", fakeInfo{}, 160)); !ok {
		t.Fatal("newest thumbnail removed")
	}
	if _, ok := m.cached("data", Key("fa", fakeInfo{}, 160)); ok {
		t.Fatal("oldest thumbnail kept")
	}
	if err := m.Clear("data"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.cached("data", Key("fj", fakeInfo{}, 160)); ok {
		t.Fatal("cleared cache still answers")
	}
}

type fakeInfo struct{ os.FileInfo }

func (fakeInfo) Size() int64        { return 1 }
func (fakeInfo) ModTime() time.Time { return time.Unix(1, 0) }
func (fakeInfo) Sys() any           { return nil }

func TestVolumeRel(t *testing.T) {
	if got := VolumeRel("/mnt", "data", "/mnt/data/fotos", "a/b.jpg"); got != "fotos/a/b.jpg" {
		t.Fatal(got)
	}
	if got := VolumeRel("/mnt", "data", "/media/usb", "b.jpg"); got != "" {
		t.Fatal(got)
	}
	if VolumeRel("", "data", "/mnt/data", "x") != "" {
		t.Fatal("no mount root")
	}
}

// PLAN 70: Office documents show the picture they carry, nothing else.
func TestOfficeThumbnail(t *testing.T) {
	pic := image.NewNRGBA(image.Rect(0, 0, 120, 160))
	var pngData bytes.Buffer
	png.Encode(&pngData, pic)
	archive := func(entries map[string][]byte) []byte {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for name, data := range entries {
			w, _ := z.Create(name)
			w.Write(data)
		}
		z.Close()
		return buf.Bytes()
	}
	dir := t.TempDir()
	m := New()
	for _, tc := range []struct{ name, entry string }{
		{"Angebot.docx", "docProps/thumbnail.png"},
		{"Tabelle.ods", "Thumbnails/thumbnail.png"},
	} {
		data := archive(map[string][]byte{"[Content_Types].xml": []byte("<x/>"), tc.entry: pngData.Bytes()})
		thumb, _, err := m.Get(context.Background(), source(t, dir, tc.name, data), 160)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if b := decode(t, thumb).Bounds(); b.Dy() != 160 {
			t.Fatalf("%s: %v", tc.name, b)
		}
	}
	// Without a picture, with a broken one, or no zip at all: the icon.
	for name, data := range map[string][]byte{
		"leer.xlsx":   archive(map[string][]byte{"xl/workbook.xml": []byte("<x/>")}),
		"kaputt.pptx": archive(map[string][]byte{"docProps/thumbnail.jpeg": []byte("no jpeg")}),
		"kein.docx":   []byte("not a zip"),
	} {
		if _, _, err := m.Get(context.Background(), source(t, dir, name, data), 160); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if Kind("a.DOCX") != "office" || Kind("b.odp") != "office" || Kind("c.doc") != "" {
		t.Fatal("office kinds")
	}
}
