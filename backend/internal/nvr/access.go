package nvr

import (
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"sort"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// The rights on a camera.
const (
	// RightLive is watching the live picture.
	RightLive = "live"
	// RightRecordings is watching recordings and events.
	RightRecordings = "recordings"
)

// CameraAccess are the normal accounts with a tick for one camera.
// Owners and administrators are never listed: they may always.
type CameraAccess struct {
	Live       []string `json:"live"`
	Recordings []string `json:"recordings"`
}

// accessSettings are the content of nvr/camera-access.json.
type accessSettings struct {
	Cameras map[string]CameraAccess `json:"cameras"`
}

// Account is what the rights need to know of an account right now (as in
// package appaccess).
type Account struct {
	Name     string
	Admin    bool // owner or administrator
	Disabled bool
}

// Access decides which account may see which camera, modelled on
// appaccess: owners and administrators see every camera, a normal account
// only the ones ticked for it, separately for live and recordings. A new
// account and a new camera start with nothing ticked.
//
// The rights live in /config/nvr/camera-access.json, written only when
// they change and read once at the start.
type Access struct {
	store *config.Store
	// Lookup returns the account as it is now; false if it does not exist.
	Lookup func(name string) (Account, bool)

	mu      sync.RWMutex
	cameras map[string]CameraAccess
}

// NewAccess returns rights with nothing stored; Load reads them.
func NewAccess(store *config.Store) *Access {
	return &Access{store: store, cameras: map[string]CameraAccess{}}
}

// Load reads the rights from /config. A missing file means none.
func (a *Access) Load() error {
	var s accessSettings
	err := a.store.ReadJSON(accessFile, &s)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if s.Cameras == nil {
		s.Cameras = map[string]CameraAccess{}
	}
	a.mu.Lock()
	a.cameras = s.Cameras
	a.mu.Unlock()
	return nil
}

func (a *Access) lookup(name string) (Account, bool) {
	if a.Lookup == nil || name == "" {
		return Account{}, false
	}
	return a.Lookup(name)
}

// Allows tells whether an account may use a right on a camera now: it
// exists, is not locked, and is an owner or administrator or has the tick.
func (a *Access) Allows(user, camera, right string) bool {
	account, ok := a.lookup(user)
	if !ok || account.Disabled {
		return false
	}
	if account.Admin {
		return true
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	entry := a.cameras[camera]
	switch right {
	case RightLive:
		return slices.Contains(entry.Live, user)
	case RightRecordings:
		return slices.Contains(entry.Recordings, user)
	}
	return false
}

// Sees tells whether an account may see a camera at all (either right).
func (a *Access) Sees(user, camera string) bool {
	return a.Allows(user, camera, RightLive) || a.Allows(user, camera, RightRecordings)
}

// UserRights are the ticks of one account: camera → rights.
type UserRights map[string]struct{ Live, Recordings bool }

// User returns the ticks of an account for the given cameras.
func (a *Access) User(user string, cameras []string) UserRights {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := UserRights{}
	for _, id := range cameras {
		entry := a.cameras[id]
		out[id] = struct{ Live, Recordings bool }{slices.Contains(entry.Live, user), slices.Contains(entry.Recordings, user)}
	}
	return out
}

// SetUser changes the ticks of one normal account; cameras left out stay
// as they are. cameras are the cameras that exist: others are refused.
// The file is only written when something changed.
func (a *Access) SetUser(user string, rights UserRights, cameras []string) error {
	account, ok := a.lookup(user)
	if !ok {
		return invalid("no such account")
	}
	if account.Admin {
		return invalid("owners and administrators always see every camera")
	}
	for id := range rights {
		if !slices.Contains(cameras, id) {
			return invalid("no camera %q", id)
		}
	}
	return a.change(func(next map[string]CameraAccess) {
		for id, want := range rights {
			entry := next[id]
			entry.Live = tick(entry.Live, user, want.Live)
			entry.Recordings = tick(entry.Recordings, user, want.Recordings)
			next[id] = entry
		}
	})
}

// ForgetUser removes a deleted account from every camera.
func (a *Access) ForgetUser(user string) error {
	return a.change(func(next map[string]CameraAccess) {
		for id, entry := range next {
			entry.Live = tick(entry.Live, user, false)
			entry.Recordings = tick(entry.Recordings, user, false)
			next[id] = entry
		}
	})
}

// ForgetCamera removes the rights of a deleted camera.
func (a *Access) ForgetCamera(camera string) error {
	return a.change(func(next map[string]CameraAccess) { delete(next, camera) })
}

// change applies edit to a copy of the rights and stores the result if it
// differs. Entries without any tick are dropped.
func (a *Access) change(edit func(map[string]CameraAccess)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	next := make(map[string]CameraAccess, len(a.cameras))
	for id, entry := range a.cameras {
		next[id] = CameraAccess{Live: slices.Clone(entry.Live), Recordings: slices.Clone(entry.Recordings)}
	}
	edit(next)
	for id, entry := range next {
		if len(entry.Live) == 0 && len(entry.Recordings) == 0 {
			delete(next, id)
			continue
		}
		// Never null in the file.
		if entry.Live == nil {
			entry.Live = []string{}
		}
		if entry.Recordings == nil {
			entry.Recordings = []string{}
		}
		next[id] = entry
	}
	if reflect.DeepEqual(next, a.cameras) {
		return nil
	}
	if err := a.store.WriteJSON(accessFile, accessSettings{Cameras: next}); err != nil {
		return err
	}
	a.cameras = next
	return nil
}

// tick returns list with user in it or not, sorted.
func tick(list []string, user string, want bool) []string {
	out := slices.DeleteFunc(slices.Clone(list), func(u string) bool { return u == user })
	if want {
		out = append(out, user)
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}
