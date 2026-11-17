package main

import (
	"os"
	"sync"
	"time"

	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// pimFeatures are the folders of T-Kalender and Kontakte below .titanos on
// the system data volume (TECHNIK 5.1): they follow the default volume,
// move along with it (Watch) and never lived on the first volume by name
// (NoLegacy).
var pimFeatures = pimstore.Features

// wirePIM builds the store of calendars and address books (TECHNIK 5):
// the folders are made only to write; reading and the removal of a
// deleted account's data make nothing. Without a volume every call is
// ErrUnavailable (503), never an empty collection.
func (w *titand) wirePIM() error {
	w.pim = pimstore.New()
	w.pim.Account = func(name string) (time.Time, bool) {
		u, ok := w.users.Get(name)
		return u.Created, ok
	}
	// The NAS zone (Settings › Time zone) for floating times and dates;
	// the index keeps those as wall clock, so a new zone needs no rebuild.
	var zoneMu sync.Mutex
	var zoneName string
	var zone *time.Location
	w.pim.Local = func() *time.Location {
		name := w.settings.CurrentTimezone()
		zoneMu.Lock()
		defer zoneMu.Unlock()
		if zone == nil || name != zoneName {
			zoneName, zone = name, time.Local
			if loc, err := time.LoadLocation(name); err == nil {
				zone = loc
			}
		}
		return zone
	}
	// RAM only: which volume the folders live on now (no disk access, no
	// note for disk wake-ups). The state in RAM belongs to that volume.
	w.pim.Current = w.systemData.Volume
	w.pim.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		volume, release := w.systemData.Use(feature)
		if volume == "" {
			release()
			return nil, nil, volumedata.ErrUnavailable
		}
		open := volumedata.OpenExisting
		if create {
			open = volumedata.Open
		}
		root, err := open("/mnt", volume, feature, w.volumes.Mounted())
		if err != nil {
			release()
			return nil, nil, err
		}
		// The store closes the root; release ends the hold against a move.
		return root, release, nil
	}
	return nil
}
