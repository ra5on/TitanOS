package notify

import (
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// An array that comes up degraded only after mdadm's last-resort timer is
// first inactive ("failed"): that must not be reported while the NAS starts.
func TestNoFailedVolumeWhileStarting(t *testing.T) {
	m, _ := newManager(t)
	w := NewWatcher(m)
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return clock }
	w.settled = clock.Add(startGrace)
	look := func(state string) {
		w.Observe(storage.Status{Volumes: []storage.Volume{{Name: "tank", State: state}}})
		clock = clock.Add(30 * time.Second)
	}
	look(storage.VolumeFailed)
	look(storage.VolumeFailed)
	look(storage.VolumeDegraded)
	look(storage.VolumeDegraded)
	kinds := []string{}
	for _, message := range m.Messages() {
		kinds = append(kinds, message.Kind)
	}
	if len(kinds) != 1 || kinds[0] != "volume_degraded" {
		t.Fatalf("messages: %v", kinds)
	}
	// After the grace a failure counts again.
	clock = clock.Add(startGrace)
	look(storage.VolumeFailed)
	look(storage.VolumeFailed)
	if got := m.Messages(); len(got) != 2 || got[0].Kind != "volume_failed" && got[1].Kind != "volume_failed" {
		t.Fatalf("failure after the start not reported: %+v", got)
	}
}
