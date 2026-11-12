package nvr

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Deleting a camera must not delete its recordings: Frigate expires the
// recordings of a camera that is not in config.yml with the global
// retention (record.continuous/motion.days), within the hour. So a
// deleted camera stays in config.yml as a stub until its own retention
// is over: switched off, with its retention, an input of the local
// restream that names no stream (go2rtc has none of that name, and no
// login goes anywhere). Frigate's cleanup goes through every camera of
// the config, switched on or not (frigate/record/cleanup.py), so the
// stub's recordings expire after their days like those of any camera.
//
// The deleted cameras are kept on the NVR's volume (deleted-cameras.json
// in .titanos/nvr): the recordings are there, and their end is no change
// of the owner's (nothing on the stick changes when it comes).

const deletedFile = "deleted-cameras.json"

// pruneAfter is how long a deleted camera stays in config.yml after its
// retention ended: Frigate's cleanup runs hourly and the camera's last
// segments end a little after it was deleted.
const pruneAfter = 24 * time.Hour

// DeletedCamera is a camera that was deleted while its recordings are
// kept.
type DeletedCamera struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	RetainDays int       `json:"retainDays"`
	DeletedAt  time.Time `json:"deletedAt"`
}

// ExpiresAt is when the last recordings of the camera are deleted.
func (d DeletedCamera) ExpiresAt() time.Time {
	return d.DeletedAt.Add(time.Duration(d.RetainDays) * 24 * time.Hour)
}

func validDeleted(d DeletedCamera) bool {
	return ValidCameraID(d.ID) && validName(d.Name) && d.RetainDays >= 1 && d.RetainDays <= 365 && !d.DeletedAt.IsZero()
}

// deletedFor returns the deleted cameras of a volume, read from it the
// first time (and after the NVR moved to another volume). An error leaves
// the NVR's config as it is: written without them, it would have Frigate
// delete their recordings.
func (m *Manager) deletedFor(volume string) ([]DeletedCamera, error) {
	m.deletedMu.Lock()
	defer m.deletedMu.Unlock()
	return m.deletedForLocked(volume)
}

func (m *Manager) deletedForLocked(volume string) ([]DeletedCamera, error) {
	m.mu.Lock()
	list, from := m.rt.deleted, m.rt.deletedVolume
	m.mu.Unlock()
	if from == volume && volume != "" {
		return slices.Clone(list), nil
	}
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var stored []DeletedCamera
	if err := volumedata.ReadJSON(root, deletedFile, &stored, 1<<20); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("nvr: deleted cameras not readable: %w", err)
	}
	list = slices.DeleteFunc(stored, func(d DeletedCamera) bool { return !validDeleted(d) })
	m.mu.Lock()
	m.rt.deleted, m.rt.deletedVolume = list, volume
	m.mu.Unlock()
	return slices.Clone(list), nil
}

// changeDeleted edits the deleted cameras of a volume and writes them;
// the next look writes config.yml.
func (m *Manager) changeDeleted(volume string, edit func([]DeletedCamera) []DeletedCamera) error {
	m.deletedMu.Lock()
	defer m.deletedMu.Unlock()
	list, err := m.deletedForLocked(volume)
	if err != nil {
		return err
	}
	next := edit(list)
	if slices.Equal(next, list) {
		return nil
	}
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := volumedata.WriteJSON(root, deletedFile, next); err != nil {
		return err
	}
	m.mu.Lock()
	m.rt.deleted, m.rt.deletedVolume = next, volume
	m.rt.dirty = true
	m.mu.Unlock()
	m.wake()
	return nil
}

// pruneDeleted drops the deleted cameras whose recordings are gone. It
// runs when config.yml is made anyway, so that their end restarts
// nothing by itself.
func (m *Manager) pruneDeleted(volume string) error {
	now := m.now()
	return m.changeDeleted(volume, func(list []DeletedCamera) []DeletedCamera {
		return slices.DeleteFunc(slices.Clone(list), func(d DeletedCamera) bool { return now.After(d.ExpiresAt().Add(pruneAfter)) })
	})
}

// DeletedCameras are the deleted cameras whose recordings are still kept,
// by ID; nil while they are not known (the volume was not read yet).
func (m *Manager) DeletedCameras() []DeletedCamera {
	m.mu.Lock()
	list, volume := slices.Clone(m.rt.deleted), m.settings.Volume
	from := m.rt.deletedVolume
	cameras := m.cameras
	m.mu.Unlock()
	if from != volume || volume == "" {
		if volume == "" || !slices.Contains(m.mounted(), volume) {
			return nil
		}
		var err error
		if list, err = m.deletedFor(volume); err != nil {
			return nil
		}
	}
	now := m.now()
	out := []DeletedCamera{}
	for _, d := range list {
		if now.Before(d.ExpiresAt()) && !slices.ContainsFunc(cameras, func(c Camera) bool { return c.ID == d.ID }) {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b DeletedCamera) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// DeletedCamera returns a deleted camera whose recordings are still kept.
func (m *Manager) DeletedCamera(id string) (DeletedCamera, bool) {
	for _, d := range m.DeletedCameras() {
		if d.ID == id {
			return d, true
		}
	}
	return DeletedCamera{}, false
}

// recordedCamera tells whether id has recordings to show: a camera, or a
// deleted one whose recordings are kept.
func (m *Manager) recordedCamera(id string) (string, bool) {
	if c, ok := m.Camera(id); ok {
		return c.Name, true
	}
	if d, ok := m.DeletedCamera(id); ok {
		return d.Name, true
	}
	return "", false
}

// recordedIDs are the cameras with recordings: the cameras and the
// deleted ones whose recordings are kept.
func (m *Manager) recordedIDs() []string {
	ids := m.CameraIDs()
	for _, d := range m.DeletedCameras() {
		ids = append(ids, d.ID)
	}
	return ids
}

// stubs are the deleted cameras config.yml keeps: all of the list that
// are no camera again (a camera added with the ID of a deleted one takes
// its recordings back).
func stubs(deleted []DeletedCamera, cameras []Camera) []DeletedCamera {
	out := []DeletedCamera{}
	for _, d := range deleted {
		if validDeleted(d) && !slices.ContainsFunc(cameras, func(c Camera) bool { return c.ID == d.ID }) {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b DeletedCamera) int { return strings.Compare(a.ID, b.ID) })
	return slices.CompactFunc(out, func(a, b DeletedCamera) bool { return a.ID == b.ID })
}
