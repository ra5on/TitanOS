package thumbs

import (
	"bytes"
	"testing"
	"time"
)

func TestReadPhotoGPS(t *testing.T) {
	ph := ReadPhoto(bytes.NewReader(ExifJPEGForTest("iPhone 15", "2026:10:12 15:42:00", 55.5578, 8.0833)))
	if ph.Camera != "iPhone 15" || !ph.Taken.Equal(time.Date(2026, 10, 12, 15, 42, 0, 0, time.UTC)) {
		t.Fatalf("camera/date: %+v", ph)
	}
	if !ph.HasGPS || ph.Lat < 55.55 || ph.Lat > 55.56 || ph.Lon < 8.08 || ph.Lon > 8.09 {
		t.Fatalf("gps: %+v", ph)
	}
	south := ReadPhoto(bytes.NewReader(ExifJPEGForTest("X", "2025:12:24 18:00:00", -33.86, -70.65)))
	if !south.HasGPS || south.Lat > -33.85 || south.Lon > -70.64 {
		t.Fatalf("south/west: %+v", south)
	}
	if ReadPhoto(bytes.NewReader([]byte{0xFF, 0xD8, 0xFF, 0xD9})).HasGPS {
		t.Fatal("gps from nothing")
	}
}
