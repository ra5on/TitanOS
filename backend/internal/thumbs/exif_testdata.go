package thumbs

import (
	"bytes"
	"encoding/binary"
)

// ExifJPEGForTest builds a small JPEG with EXIF: camera model, date taken
// and GPS, for tests (PLAN 52). titand itself does not use it.
func ExifJPEGForTest(model, taken string, lat, lon float64) []byte {
	le := binary.LittleEndian
	var t bytes.Buffer
	t.WriteString("II")
	binary.Write(&t, le, uint16(42))
	binary.Write(&t, le, uint32(8))
	// Layout: IFD0 at 8 with 3 entries (Model, ExifIFD, GPSIFD).
	ifd0 := 8
	ifd0Size := 2 + 3*12 + 4
	exif := ifd0 + ifd0Size
	exifSize := 2 + 1*12 + 4
	gps := exif + exifSize
	gpsSize := 2 + 4*12 + 4
	data := gps + gpsSize
	modelOff := data
	takenOff := modelOff + len(model) + 1
	latOff := takenOff + 20
	lonOff := latOff + 24
	entry := func(tag, kind uint16, num uint32, value uint32) {
		binary.Write(&t, le, tag)
		binary.Write(&t, le, kind)
		binary.Write(&t, le, num)
		binary.Write(&t, le, value)
	}
	binary.Write(&t, le, uint16(3))
	entry(0x0110, 2, uint32(len(model)+1), uint32(modelOff))
	entry(0x8769, 4, 1, uint32(exif))
	entry(0x8825, 4, 1, uint32(gps))
	binary.Write(&t, le, uint32(0))
	binary.Write(&t, le, uint16(1))
	entry(0x9003, 2, 20, uint32(takenOff))
	binary.Write(&t, le, uint32(0))
	ref := func(c byte) uint32 { return uint32(c) }
	latRef, lonRef := byte('N'), byte('E')
	if lat < 0 {
		latRef, lat = 'S', -lat
	}
	if lon < 0 {
		lonRef, lon = 'W', -lon
	}
	binary.Write(&t, le, uint16(4))
	entry(1, 2, 2, ref(latRef))
	entry(2, 5, 3, uint32(latOff))
	entry(3, 2, 2, ref(lonRef))
	entry(4, 5, 3, uint32(lonOff))
	binary.Write(&t, le, uint32(0))
	t.WriteString(model + "\x00")
	t.WriteString(taken + "\x00")
	rat := func(v float64) {
		deg := uint32(v)
		minf := (v - float64(deg)) * 60
		mins := uint32(minf)
		secs := uint32((minf - float64(mins)) * 60 * 1000)
		for _, p := range [][2]uint32{{deg, 1}, {mins, 1}, {secs, 1000}} {
			binary.Write(&t, le, p[0])
			binary.Write(&t, le, p[1])
		}
	}
	rat(lat)
	rat(lon)
	seg := append([]byte("Exif\x00\x00"), t.Bytes()...)
	var j bytes.Buffer
	j.Write([]byte{0xFF, 0xD8, 0xFF, 0xE1})
	binary.Write(&j, binary.BigEndian, uint16(len(seg)+2))
	j.Write(seg)
	j.Write([]byte{0xFF, 0xD9})
	return j.Bytes()
}
