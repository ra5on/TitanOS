package nvr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// Review PLAN 85-4: while the NVR's volume is away (a locked encrypted
// volume after a boot, a failed disk) the NVR can be switched off,
// changed otherwise and set up anew; switching it on, or moving it, still
// needs a mounted volume.
func TestSettingsWhileTheVolumeIsAway(t *testing.T) {
	r := newRig(t, true)
	r.up()
	ctx := context.Background()
	volume := r.m.Settings().Volume
	r.m.Dev = false
	r.m.Mounted = func() []string { return []string{"other"} }
	r.m.Engine = readyEngine{r.engine}
	r.look()

	// Another setting while it is on.
	s := r.m.Settings()
	s.NotifyOff = true
	if err := r.m.SetSettings(ctx, s, SetOptions{}); err != nil {
		t.Fatalf("change while the volume is away: %v", err)
	}
	// Switching it off.
	if err := r.m.Disable(ctx); err != nil {
		t.Fatalf("switch off while the volume is away: %v", err)
	}
	if got := r.m.Settings(); got.Enabled || got.Volume != volume || !got.NotifyOff {
		t.Errorf("settings: %+v", got)
	}
	// Switching it on again on the missing volume: no.
	s = r.m.Settings()
	s.Enabled = true
	if err := r.m.SetSettings(ctx, s, SetOptions{}); !errors.Is(err, ErrNoVolume) {
		t.Errorf("switch on onto the missing volume: %v", err)
	}
	// Moving to another missing volume: no; to a mounted one: yes.
	s.Volume = "gone"
	if err := r.m.SetSettings(ctx, s, SetOptions{}); !errors.Is(err, ErrNoVolume) {
		t.Errorf("move to a missing volume: %v", err)
	}
	s.Volume = "other"
	if err := r.m.SetSettings(ctx, s, SetOptions{ConfirmSleepingVolume: true}); err != nil {
		t.Errorf("move to a mounted volume: %v", err)
	}
}

// Review PLAN 85-4: export names may have 80 characters (they were held
// to a camera name's 64), and the default name of a camera with a long
// name fits.
func TestExportNames(t *testing.T) {
	long := strings.Repeat("Kamera ", 9) + "Hof" // 66 characters
	if !exportName(strings.Repeat("x", 80)) || exportName(strings.Repeat("x", 81)) {
		t.Error("80 characters")
	}
	r, first := capRig(t)
	ctx := context.Background()
	cam := r.m.Cameras()[0]
	cam.Name = strings.Repeat("Ä", 64)
	if _, err := r.m.UpdateCamera(ctx, cam.ID, cam, CameraOptions{}); err != nil {
		t.Fatalf("camera name of 64 characters: %v", err)
	}
	made, err := r.m.CreateExport(ctx, "anna", cam.ID, first, first+60, "")
	if err != nil {
		t.Fatalf("default name of a camera with 64 characters: %v", err)
	}
	if n := utf8.RuneCountInString(made.Name); n != 80 || !strings.HasPrefix(made.Name, strings.Repeat("Ä", 63)+" ") {
		t.Errorf("name %q (%d)", made.Name, n)
	}
	if _, err := r.m.CreateExport(ctx, "anna", cam.ID, first, first+60, long+" Abend"); err != nil {
		t.Errorf("an own name of %d characters: %v", utf8.RuneCountInString(long+" Abend"), err)
	}
}
