package nvr

import (
	"context"
	"errors"
	"testing"
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
