package main

import (
	"os"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appphotos"

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
	// The mark that an account kept data (pim.json, written once per
	// account and feature; it goes with users/<name>): a folder missing on
	// the current volume is 503, never an empty collection.
	w.pim.ReadMark = func(account string) ([]byte, error) {
		return w.store.Read(pimMarkFile(account))
	}
	w.pim.WriteMark = func(account string, data []byte) error {
		return w.store.Write(pimMarkFile(account), data)
	}
	// Where such a folder went: looked for on the other mounted volumes
	// once per mount set (the journal names it).
	w.pim.Mounted = w.volumes.Mounted
	w.pim.Peek = func(volume, feature string) (*os.Root, error) {
		return volumedata.OpenExisting("/mnt", volume, feature, w.volumes.Mounted())
	}
	return nil
}

func pimMarkFile(account string) string { return "users/" + account + "/pim.json" }

// wireAppPhotos builds the index of the Android app's photo backup
// (PLAN 116): .titanos/apps on the system data volume, made only to write;
// without a volume every call is 503, never an empty index.
func (w *titand) wireAppPhotos() error {
	w.appPhotos = &appphotos.Store{Volume: func(create bool) (*os.Root, func(), error) {
		volume, release := w.systemData.Use(appphotos.Feature)
		if volume == "" {
			release()
			return nil, nil, volumedata.ErrUnavailable
		}
		open := volumedata.OpenExisting
		if create {
			open = volumedata.Open
		}
		root, err := open("/mnt", volume, appphotos.Feature, w.volumes.Mounted())
		if err != nil {
			release()
			return nil, nil, err
		}
		return root, release, nil
	}}
	return nil
}
