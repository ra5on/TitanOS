package photo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"runtime"
	"testing"
)

// noise makes a picture that compresses badly (a large JPEG).
func noise(w, h int, seed uint64) image.Image {
	r := rand.New(rand.NewPCG(seed, seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.UintN(256))
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image, q int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// bombPNG is the header of a PNG that claims w×h pixels and holds none.
func bombPNG(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(kind string, data []byte) {
		_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
		b.WriteString(kind)
		b.Write(data)
		crc := crc32.NewIEEE()
		crc.Write([]byte(kind))
		crc.Write(data)
		_ = binary.Write(&b, binary.BigEndian, crc.Sum32())
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8 bit RGBA: 4 bytes per pixel
	chunk("IHDR", ihdr)
	chunk("IDAT", []byte{0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01})
	chunk("IEND", nil)
	return b.Bytes()
}

// A picture far larger than its file claims (a decompression bomb) is
// refused from the header: nothing of its size is allocated.
func TestBombRefusedBeforeDecoding(t *testing.T) {
	bomb := bombPNG(30000, 30000) // 900 million pixels, 3.6 GB as RGBA
	if len(bomb) > 100 {
		t.Fatalf("bomb of %d bytes", len(bomb))
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Reencode(bomb, Options{Edge: 512})
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrSize) {
		t.Fatalf("bomb: %v", err)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Fatalf("the bomb allocated %d bytes", grown)
	}
	// Just over the bound is refused, just under is decoded (and fails
	// here only because the file holds no pixels).
	if _, err := Reencode(bombPNG(10001, 5000), Options{MaxPixels: 50_000_000}); !errors.Is(err, ErrSize) {
		t.Fatalf("over the bound: %v", err)
	}
	if _, err := Reencode(bombPNG(100, 100), Options{}); !errors.Is(err, ErrDecode) {
		t.Fatalf("a small header without pixels: %v", err)
	}
	if _, err := Reencode([]byte("GIF89a not a picture"), Options{}); !errors.Is(err, ErrFormat) {
		t.Fatalf("gif: %v", err)
	}
	if _, err := Reencode(encodeJPEG(t, noise(8, 8, 1), 90), Options{MinEdge: 16}); !errors.Is(err, ErrSize) {
		t.Fatalf("too small: %v", err)
	}
}

// Contacts: the ratio stays, at most Edge, the JPEG within MaxBytes.
func TestReencodeFitsAndBounds(t *testing.T) {
	big := encodeJPEG(t, noise(1200, 800, 2), 100)
	out, err := Reencode(big, Options{Edge: 512, MaxBytes: 256 << 10})
	if err != nil {
		t.Fatal(err)
	}
	c, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || format != "jpeg" || c.Width != 512 || c.Height != 341 || len(out) > 256<<10 {
		t.Fatalf("%s %dx%d, %d bytes, %v", format, c.Width, c.Height, len(out), err)
	}
	// Noise needs a lower quality or a smaller edge to fit 40 KiB.
	small, err := Reencode(big, Options{Edge: 512, MaxBytes: 40 << 10})
	if err != nil || len(small) > 40<<10 {
		t.Fatalf("40 KiB: %d bytes, %v", len(small), err)
	}
	// A small picture is not enlarged; PNG comes out as JPEG.
	var p bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	_ = png.Encode(&p, img)
	out, err = Reencode(p.Bytes(), Options{Edge: 512})
	if err != nil {
		t.Fatal(err)
	}
	if c, format, _ := image.DecodeConfig(bytes.NewReader(out)); format != "jpeg" || c.Width != 40 || c.Height != 30 {
		t.Fatalf("small: %s %dx%d", format, c.Width, c.Height)
	}
	// Account pictures: the middle square.
	out, err = Reencode(big, Options{Edge: 256, Square: true})
	if c, _, _ := image.DecodeConfig(bytes.NewReader(out)); err != nil || c.Width != 256 || c.Height != 256 {
		t.Fatalf("square: %dx%d %v", c.Width, c.Height, err)
	}
}
