package nvr

import (
	"slices"
	"strings"
	"time"
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
// The deleted cameras are kept on the stick (/config/nvr/
// deleted-cameras.json), next to the cameras: deleting a camera is a
// change of the owner's, and the list is needed whatever volume is
// mounted (a stub written into every volume's config.yml is harmless
// where the camera never recorded) and before any volume is (the ID of a
// new camera). It is written only when the owner deletes a camera or adds
// one with a stub's ID; when a stub's days are over is worked out when it
// is read, and the expired ones leave the file with the owner's next
// write of it. The loop never writes it.

const deletedFile = "nvr/deleted-cameras.json"

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

// rendered tells whether the stub is still in config.yml at now.
func (d DeletedCamera) rendered(now time.Time) bool {
	return !now.After(d.ExpiresAt().Add(pruneAfter))
}

func validDeleted(d DeletedCamera) bool {
	return ValidCameraID(d.ID) && validName(d.Name) && d.RetainDays >= 1 && d.RetainDays <= 365 && !d.DeletedAt.IsZero()
}

// deletedList is the list as read from the stick (m.mu held by the
// caller or not needed: it is replaced, never changed in place).
func (m *Manager) deletedList() []DeletedCamera {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.deleted)
}

// renderedStubs are the deleted cameras config.yml keeps at now.
func (m *Manager) renderedStubs(now time.Time) []DeletedCamera {
	return slices.DeleteFunc(m.deletedList(), func(d DeletedCamera) bool { return !d.rendered(now) })
}

// storeDeletedLocked writes the list (m.camerasMu held): the entries edit
// leaves, without those whose days and pruneAfter are over. Only the
// owner's changes call it.
func (m *Manager) storeDeletedLocked(edit func([]DeletedCamera) []DeletedCamera) error {
	now := m.now()
	next := edit(m.deletedList())
	next = slices.DeleteFunc(next, func(d DeletedCamera) bool { return !d.rendered(now) })
	slices.SortFunc(next, func(a, b DeletedCamera) int { return strings.Compare(a.ID, b.ID) })
	if next == nil {
		next = []DeletedCamera{}
	}
	if err := m.store.WriteJSON(deletedFile, next); err != nil {
		return err
	}
	m.mu.Lock()
	m.deleted = next
	m.changedLocked()
	m.mu.Unlock()
	m.wake()
	return nil
}

// DeletedCameras are the deleted cameras whose recordings are still kept,
// by ID.
func (m *Manager) DeletedCameras() []DeletedCamera {
	m.mu.Lock()
	list, cameras := slices.Clone(m.deleted), m.cameras
	m.mu.Unlock()
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
