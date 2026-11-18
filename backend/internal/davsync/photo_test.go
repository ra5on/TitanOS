package davsync

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

var bigPhoto = sync.OnceValue(func() []byte {
	// Noise compresses badly: 900×700 at quality 100 is well over
	// store.MaxPhoto and leaves the card under 2 MiB.
	r := rand.New(rand.NewPCG(7, 7))
	img := image.NewRGBA(image.Rect(0, 0, 900, 700))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.UintN(256))
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 100})
	return b.Bytes()
})

// bigPhotoCard is a card as iOS writes it with a picture over 512 KiB.
func bigPhotoCard(t *testing.T) []byte {
	pic := bigPhoto()
	if len(pic) <= store.MaxPhoto || len(pic)*4/3 > 2<<20-4096 {
		t.Fatalf("picture of %d bytes", len(pic))
	}
	b64 := base64.StdEncoding.EncodeToString(pic)
	var folded strings.Builder
	folded.WriteString(b64[:46])
	for rest := b64[46:]; rest != ""; {
		n := min(len(rest), 74)
		folded.WriteString("\r\n " + rest[:n])
		rest = rest[n:]
	}
	return []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nPRODID:-//Apple Inc.//iPhone OS 26.1//EN\r\nN:Bild;Berta;;;\r\nFN:Berta Bild\r\n" +
		"TEL;type=CELL;type=VOICE;type=pref:+49 170 1234567\r\nX-APPLE-SUBLOCALITY:Ehrenfeld\r\n" +
		"PHOTO;ENCODING=b;TYPE=JPEG;X-ABCROP-RECTANGLE=ABClipRect_1&0&0&700&700&abc:" + folded.String() + "\r\n" +
		"UID:C0FFEE00-0000-4000-8000-000000000001\r\nEND:VCARD\r\n")
}

// A picture of at most 512 KiB, or one that cannot be read, stays as it
// came, with the ETag.
func TestPutPhotoKept(t *testing.T) {
	small := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;ENCODING=b;TYPE=JPEG:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 1000)) + "\r\nEND:VCARD\r\n")
	if out, ok := shrinkPhoto(small); ok || !bytes.Equal(out, small) {
		t.Fatal("a small picture changed")
	}
	// Over the limit but no picture TitanOS reads (a HEIC, say).
	odd := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:A\r\nPHOTO;ENCODING=b;TYPE=HEIC:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 600<<10)) + "\r\nEND:VCARD\r\n")
	if out, ok := shrinkPhoto(odd); ok || !bytes.Equal(out, odd) {
		t.Fatal("an unreadable picture changed")
	}
	if out, ok := shrinkPhoto(bigPhotoCard(t)); !ok || len(out) > 400<<10 || !bytes.Contains(out, []byte("X-APPLE-SUBLOCALITY:Ehrenfeld\r\n")) {
		t.Fatalf("big picture: %v, %d bytes", ok, len(out))
	}
}
