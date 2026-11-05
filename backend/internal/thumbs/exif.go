package thumbs

import (
	"bytes"
	"encoding/binary"
	"io"
	"time"
)

// Photo is what the start of a JPEG tells about the picture.
type Photo struct {
	// Orientation is the EXIF orientation 1–8; 0 or 1 means as stored.
	Orientation int
	// Taken is DateTimeOriginal (or DateTime), in the camera's local time.
	Taken time.Time
	// GPS position in degrees (PLAN 52); HasGPS tells whether there was one.
	Lat, Lon float64
	HasGPS   bool
	// Camera is the model of the camera (EXIF Model), if any.
	Camera string
}

// exifLimit is how much of a JPEG is read to find its EXIF block. Cameras
// put it first; a thumbnail embedded in it rarely exceeds 64 KiB.
const exifLimit = 256 << 10

// ReadPhoto reads the EXIF orientation and date of a JPEG. It never fails:
// a picture without (readable) EXIF data is simply as stored and undated.
func ReadPhoto(r io.Reader) Photo {
	data, _ := io.ReadAll(io.LimitReader(r, exifLimit))
	return parseJPEG(data)
}

func parseJPEG(data []byte) Photo {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return Photo{}
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return Photo{}
		}
		marker := data[i+1]
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // image data starts: no EXIF before it
			return Photo{}
		}
		length := int(binary.BigEndian.Uint16(data[i+2:]))
		if length < 2 || i+2+length > len(data) {
			return Photo{}
		}
		segment := data[i+4 : i+2+length]
		if marker == 0xE1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return parseTIFF(segment[6:])
		}
		i += 2 + length
	}
	return Photo{}
}

// parseTIFF reads the few tags it needs from the TIFF structure of an EXIF
// block. Every offset is checked; a broken block yields what was readable.
func parseTIFF(t []byte) Photo {
	var photo Photo
	if len(t) < 8 {
		return photo
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return photo
	}
	if order.Uint16(t[2:]) != 42 {
		return photo
	}
	var dateTime, original string
	var gpsIFD uint32
	// ascii reads a string value (inline up to 4 bytes, else at its offset).
	ascii := func(num uint32, value []byte) string {
		var raw []byte
		if num <= 4 {
			raw = value[:num]
		} else if start := order.Uint32(value); int64(start)+int64(num) <= int64(len(t)) && num <= 128 {
			raw = t[start : start+num]
		}
		return string(bytes.TrimRight(raw, "\x00 "))
	}
	// ifd walks one directory and returns the offset of the EXIF sub-IFD.
	ifd := func(offset uint32) uint32 {
		if offset < 8 || int64(offset)+2 > int64(len(t)) {
			return 0
		}
		count := int(order.Uint16(t[offset:]))
		var sub uint32
		for n := range min(count, 512) {
			at := int(offset) + 2 + n*12
			if at+12 > len(t) {
				break
			}
			tag, kind, num := order.Uint16(t[at:]), order.Uint16(t[at+2:]), order.Uint32(t[at+4:])
			value := t[at+8 : at+12]
			switch {
			case tag == 0x0112 && kind == 3 && num == 1:
				if o := int(order.Uint16(value)); o >= 1 && o <= 8 {
					photo.Orientation = o
				}
			case tag == 0x8769 && (kind == 4 || kind == 13):
				sub = order.Uint32(value)
			case tag == 0x8825 && (kind == 4 || kind == 13):
				gpsIFD = order.Uint32(value)
			case tag == 0x0110 && kind == 2 && num > 0:
				photo.Camera = ascii(num, value)
			case (tag == 0x0132 || tag == 0x9003) && kind == 2 && num >= 19 && num <= 64:
				start := order.Uint32(value)
				if int64(start)+19 <= int64(len(t)) {
					text := string(t[start : start+19])
					if tag == 0x0132 {
						dateTime = text
					} else {
						original = text
					}
				}
			}
		}
		return sub
	}
	if sub := ifd(order.Uint32(t[4:])); sub != 0 {
		ifd(sub)
	}
	if gpsIFD != 0 {
		photo.Lat, photo.Lon, photo.HasGPS = parseGPS(t, order, gpsIFD)
	}
	for _, text := range []string{original, dateTime} {
		if when, err := time.Parse("2006:01:02 15:04:05", text); err == nil && when.Year() > 1900 {
			photo.Taken = when
			break
		}
	}
	return photo
}

// parseGPS reads latitude and longitude from the GPS directory of an EXIF
// block: references N/S/E/W and three rationals (degrees, minutes, seconds).
func parseGPS(t []byte, order binary.ByteOrder, offset uint32) (float64, float64, bool) {
	if offset < 8 || int64(offset)+2 > int64(len(t)) {
		return 0, 0, false
	}
	var latRef, lonRef byte
	var lat, lon float64
	var haveLat, haveLon bool
	rationals := func(start uint32) (float64, bool) {
		if int64(start)+24 > int64(len(t)) {
			return 0, false
		}
		var v [3]float64
		for i := range 3 {
			num, den := order.Uint32(t[start+uint32(i*8):]), order.Uint32(t[start+uint32(i*8)+4:])
			if den == 0 {
				return 0, false
			}
			v[i] = float64(num) / float64(den)
		}
		return v[0] + v[1]/60 + v[2]/3600, true
	}
	count := int(order.Uint16(t[offset:]))
	for n := range min(count, 64) {
		at := int(offset) + 2 + n*12
		if at+12 > len(t) {
			break
		}
		tag, kind, num := order.Uint16(t[at:]), order.Uint16(t[at+2:]), order.Uint32(t[at+4:])
		value := t[at+8 : at+12]
		switch {
		case tag == 1 && kind == 2 && num >= 1:
			latRef = value[0]
		case tag == 3 && kind == 2 && num >= 1:
			lonRef = value[0]
		case tag == 2 && kind == 5 && num == 3:
			lat, haveLat = rationals(order.Uint32(value))
		case tag == 4 && kind == 5 && num == 3:
			lon, haveLon = rationals(order.Uint32(value))
		}
	}
	if !haveLat || !haveLon || lat > 90 || lon > 180 || (lat == 0 && lon == 0) {
		return 0, 0, false
	}
	if latRef == 'S' {
		lat = -lat
	}
	if lonRef == 'W' {
		lon = -lon
	}
	return lat, lon, true
}
