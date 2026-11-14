package nvr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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
	ID   string `json:"id"`
	Name string `json:"name"`
	// RetainDays is the longest the camera kept anything (0.80 reads only
	// this); RecordDays and EventDays are its two days since 0.81 (PLAN
	// 88), 0 when they equal RetainDays (as Record stores them).
	RetainDays int       `json:"retainDays"`
	RecordDays int       `json:"recordDays,omitempty"`
	EventDays  int       `json:"eventDays,omitempty"`
	DeletedAt  time.Time `json:"deletedAt"`
}

// record is what the stub keeps, as a camera that records nothing.
func (d DeletedCamera) record() Record {
	return Record{Mode: RecordOff, RetainDays: d.RetainDays, RecordDays: d.RecordDays, EventDays: d.EventDays}
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
	inRange := func(days int) bool { return days >= 0 && days <= d.RetainDays }
	return ValidCameraID(d.ID) && validName(d.Name) && d.RetainDays >= 1 && d.RetainDays <= maxRetainDays &&
		inRange(d.RecordDays) && inRange(d.EventDays) && !d.DeletedAt.IsZero()
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

// PurgeDeletedCamera deletes the recordings of a deleted camera now
// instead of when its days are over (owner 13.11.2026: "the old
// recordings of removed cameras cannot be deleted"). Frigate must run:
//
//  1. DELETE /api/recordings/start/0/end/<now>?cameras=<id> deletes every
//     recording of the camera, file and entry (frigate/api/record.py).
//     Frigate's deletion of a camera alone would leave the files: they lie
//     under recordings/<day>/<hour>/<camera>, it removes recordings/<camera>
//     (frigate/util/camera_cleanup.py).
//  2. DELETE /api/cameras/<id>?delete_exports=false deletes the rest:
//     events with their pictures and clips (also kept ones), previews,
//     the timeline, and the stub in Frigate's running configuration.
//     Exports stay: they belong to the accounts that made them and are
//     never deleted by the NVR itself.
//  3. The stub leaves the list on the stick; config.yml without it
//     restarts Frigate once, as removing a camera does.
//
// A failure in 1 or 2 leaves the stub, so the button stays and can be
// pressed again; what was deleted before stays deleted.
//
// The cameras' lock is held only to check and to take the stub off, not
// while Frigate deletes (minutes for weeks of recordings): adding,
// changing and removing cameras go on meanwhile; a camera added with the
// ID being purged is refused until the purge is over (it would take the
// recordings back that are being deleted).
func (m *Manager) PurgeDeletedCamera(ctx context.Context, id string) error {
	base, err := m.beginPurge(id)
	if err != nil {
		return err
	}
	defer m.endPurge(id)
	return m.purge(ctx, base, id)
}

// StartPurge checks like PurgeDeletedCamera and then deletes in the
// background (owner 13.11.2026: weeks of recordings take minutes – a
// closed window or a phone going to sleep must not stop it halfway). The
// camera is listed with purging: true until it is gone; a failure is told
// to the administrators (nvr_purge_failed) and leaves the stub.
func (m *Manager) StartPurge(id string) error {
	base, err := m.beginPurge(id)
	if err != nil {
		return err
	}
	name := id
	if d, ok := m.DeletedCamera(id); ok {
		name = d.Name
	}
	go func() {
		defer m.endPurge(id)
		ctx, cancel := context.WithTimeout(context.Background(), purgeLimit)
		defer cancel()
		if err := m.purge(ctx, base, id); err != nil {
			slog.Warn("NVR: recordings of a removed camera not deleted", "camera", id, "err", err)
			m.notify("warning", "nvr_purge_failed", name)
		}
	}()
	return nil
}

// purgeLimit bounds a purge in the background.
const purgeLimit = time.Hour

// ErrPurgeRunning: the recordings of this removed camera are being deleted.
var ErrPurgeRunning = errors.New("nvr: the recordings of this camera are being deleted")

// beginPurge checks that id is a removed camera with recordings, Frigate
// runs and no purge of it runs, and marks it as being purged.
func (m *Manager) beginPurge(id string) (string, error) {
	m.camerasMu.Lock()
	defer m.camerasMu.Unlock()
	if _, ok := m.DeletedCamera(id); !ok {
		return "", ErrNotFound
	}
	base, err := m.base()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rt.purging[id] {
		return "", ErrPurgeRunning
	}
	if m.rt.purging == nil {
		m.rt.purging = map[string]bool{}
	}
	m.rt.purging[id] = true
	return base, nil
}

func (m *Manager) endPurge(id string) {
	m.mu.Lock()
	delete(m.rt.purging, id)
	m.mu.Unlock()
}

// Purging tells whether the recordings of a removed camera are being deleted.
func (m *Manager) Purging(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rt.purging[id]
}

// purge does the deleting of PurgeDeletedCamera.
func (m *Manager) purge(ctx context.Context, base, id string) error {
	// In steps of purgeStep from the oldest recording on, so that no single
	// answer of Frigate takes long (it deletes file by file).
	from, end := m.purgeFrom(ctx, id), m.now().Add(time.Hour).Unix()
	only := url.Values{"cameras": {id}}
	for start := from; start < end; start += int64(purgeStep / time.Second) {
		path := "/api/recordings/start/" + strconv.FormatInt(start, 10) + "/end/" + strconv.FormatInt(min(start+int64(purgeStep/time.Second), end), 10)
		if err := m.frigate.call(ctx, http.MethodDelete, base, path, only, nil, nil); err != nil {
			return fmt.Errorf("nvr: recordings of the removed camera not deleted: %w", err)
		}
	}
	err := m.frigate.callLong(ctx, http.MethodDelete, base, "/api/cameras/"+url.PathEscape(id), url.Values{"delete_exports": {"false"}}, nil, nil)
	// 404: Frigate runs without the stub (it was removed already).
	if err != nil && frigateStatus(err) != http.StatusNotFound {
		return fmt.Errorf("nvr: events of the removed camera not deleted: %w", err)
	}
	m.camerasMu.Lock()
	defer m.camerasMu.Unlock()
	if err := m.storeDeletedLocked(func(list []DeletedCamera) []DeletedCamera {
		return slices.DeleteFunc(list, func(d DeletedCamera) bool { return d.ID == id })
	}); err != nil {
		return fmt.Errorf("nvr: removed camera not taken off the stick: %w", err)
	}
	slog.Info("NVR: recordings of a removed camera deleted", "camera", id)
	return nil
}

// purgeStep is the stretch of recordings one call of PurgeDeletedCamera
// deletes: six hours of a camera recording all the time are about 2000
// files.
const purgeStep = 6 * time.Hour

// purgeFrom is where the recordings of a deleted camera begin: the oldest
// hour Frigate lists, at the latest its days before it was deleted (and
// two more for recordings older than its days that Frigate had not
// cleaned yet).
func (m *Manager) purgeFrom(ctx context.Context, id string) int64 {
	d, _ := m.DeletedCamera(id)
	from := d.DeletedAt.Add(-time.Duration(d.RetainDays+2) * 24 * time.Hour).Unix()
	if days, err := m.RecordingsSummary(ctx, id); err == nil {
		for _, day := range days {
			for _, h := range day.Hours {
				from = min(from, h.Start)
			}
		}
	}
	return from - 3600
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
