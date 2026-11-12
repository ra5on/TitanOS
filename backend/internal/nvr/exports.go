package nvr

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Errors of exports.
var (
	// ErrExportNotFound: no such export, or one of another account.
	ErrExportNotFound = errors.New("nvr: no such export")
	// ErrExportRunning: the export is still being made.
	ErrExportRunning = errors.New("nvr: the export is still running")
	// ErrExportFailed: Frigate refused the export (nothing recorded in
	// the time asked for, its queue is full).
	ErrExportFailed = errors.New("nvr: the export could not be started")
)

// The states of an export.
const (
	ExportRunning = "running"
	ExportDone    = "done"
	ExportFailed  = "failed"
)

// Export is a piece of recording saved as one file.
type Export struct {
	ID     string `json:"id"`
	Camera string `json:"camera"`
	Name   string `json:"name"`
	// From and To are what was asked for; 0 for an export TitanOS did
	// not start.
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	Created float64 `json:"created"`
	State   string  `json:"state"`
	// SizeBytes is null while the export runs.
	SizeBytes *int64 `json:"sizeBytes"`
	// Owner is the account that made it; "" if that is not known.
	Owner string `json:"owner"`

	// file is the name of the video below /exports/.
	file string
	// thumb is the path of the preview picture below /clips/.
	thumb string
}

// exportRecord is what TitanOS keeps about an export it started
// (exports.json in .titanos/nvr on the NVR's volume): Frigate knows
// neither accounts nor the time that was asked for.
type exportRecord struct {
	Owner string `json:"owner"`
	// Camera and Name are what was asked for (a failed export is shown
	// with them after Frigate forgot it).
	Camera string  `json:"camera,omitempty"`
	Name   string  `json:"name,omitempty"`
	From   float64 `json:"from"`
	To     float64 `json:"to"`
	// Created is when the export was asked for (Frigate's own date of
	// an export is the beginning of what it shows).
	Created float64 `json:"created,omitempty"`
	// Failed: Frigate said the export failed; it is shown as failed until
	// it is deleted, without asking Frigate again.
	Failed bool `json:"failed,omitempty"`
}

const exportsFile = "exports.json"

// MaxExportSeconds is the longest export (a day).
const MaxExportSeconds = 24 * 3600

// exportRecords returns the records, read once per start from the volume.
func (m *Manager) exportRecords() (map[string]exportRecord, error) {
	m.mu.Lock()
	records, volume := m.rt.exports, m.settings.Volume
	m.mu.Unlock()
	if records != nil {
		return records, nil
	}
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	records = map[string]exportRecord{}
	if err := volumedata.ReadJSON(root, exportsFile, &records, 4<<20); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("NVR: owners of the exports not readable; exports are shown to administrators only", "err", err)
		records = map[string]exportRecord{}
	}
	if m.Dev && len(records) == 0 {
		records = devExportRecords(m.Cameras())
	}
	m.mu.Lock()
	if m.rt.exports == nil {
		m.rt.exports = records
	}
	records = m.rt.exports
	m.mu.Unlock()
	return records, nil
}

// changeExportRecords edits the records and writes them to the volume.
// One change at a time: two at once would lose one of them.
func (m *Manager) changeExportRecords(edit func(map[string]exportRecord)) error {
	m.exportsMu.Lock()
	defer m.exportsMu.Unlock()
	if _, err := m.exportRecords(); err != nil {
		return err
	}
	m.mu.Lock()
	next := map[string]exportRecord{}
	for id, record := range m.rt.exports {
		next[id] = record
	}
	volume := m.settings.Volume
	m.mu.Unlock()
	edit(next)
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := volumedata.WriteJSON(root, exportsFile, next); err != nil {
		return err
	}
	m.mu.Lock()
	if m.settings.Volume == volume {
		m.rt.exports = next
	}
	m.mu.Unlock()
	return nil
}

// owns tells whether an account made an export: its name, and the
// account was there when the export was made (a later account of the
// same name never inherits the exports of an earlier one, even if the
// NVR could not note the deletion).
func (m *Manager) owns(owner string, record exportRecord) bool {
	if owner == "" || record.Owner != owner {
		return false
	}
	if m.Access == nil {
		return true
	}
	account, ok := m.Access.lookup(owner)
	if !ok || account.Created.IsZero() {
		return true
	}
	return record.Created > 0 && record.Created >= float64(account.Created.Unix())
}

// exportName checks the name of an export.
func exportName(name string) bool {
	return validName(name) && utf8.RuneCountInString(name) <= 80
}

// frigateExport is an export as /api/exports gives it.
type frigateExport struct {
	ID         string  `json:"id"`
	Camera     string  `json:"camera"`
	Name       string  `json:"name"`
	Date       float64 `json:"date"`
	VideoPath  string  `json:"video_path"`
	ThumbPath  string  `json:"thumb_path"`
	InProgress bool    `json:"in_progress"`
}

// frigateJob is an export job (queued, running, done or failed).
type frigateJob struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Camera string   `json:"camera"`
	Name   string   `json:"name"`
	Start  *float64 `json:"start_time"`
}

// CreateExport starts an export of a camera's recordings for an account.
func (m *Manager) CreateExport(ctx context.Context, owner, camera string, from, to float64, name string) (Export, error) {
	cameraName, ok := m.recordedCamera(camera)
	if !ok {
		return Export{}, ErrNotFound
	}
	if from <= 0 || to <= from || to-from > MaxExportSeconds {
		return Export{}, invalid("from/to: at most 24 hours")
	}
	if name == "" {
		name = fmt.Sprintf("%s %s", cameraName, time.Unix(int64(from), 0).In(m.zone()).Format("2006-01-02 15.04"))
	}
	if !exportName(name) {
		return Export{}, invalid("name")
	}
	base, err := m.base()
	if err != nil {
		return Export{}, err
	}
	var started struct {
		ID string `json:"export_id"`
	}
	path := fmt.Sprintf("/api/export/%s/start/%s/end/%s", url.PathEscape(camera), formatTime(from), formatTime(to))
	if err := m.frigate.call(ctx, http.MethodPost, base, path, nil, map[string]any{"source": "recordings", "name": name}, &started); err != nil {
		if status := frigateStatus(err); status >= 400 && status < 600 && status != http.StatusNotFound {
			slog.Info("NVR: export refused by Frigate", "camera", camera, "err", err)
			return Export{}, ErrExportFailed
		}
		return Export{}, err
	}
	if !ValidEventID(started.ID) {
		return Export{}, ErrExportFailed
	}
	created := float64(m.now().Unix())
	if err := m.changeExportRecords(func(records map[string]exportRecord) {
		records[started.ID] = exportRecord{Owner: owner, Camera: camera, Name: name, From: from, To: to, Created: created}
	}); err != nil {
		slog.Warn("NVR: owner of the export not stored; it is shown to administrators only", "export", started.ID, "err", err)
	}
	return Export{ID: started.ID, Camera: camera, Name: name, From: from, To: to, Created: created, State: ExportRunning, Owner: owner}, nil
}

// Exports lists the exports of an account (owner "" with all: every
// export, for administrators), newest first.
func (m *Manager) Exports(ctx context.Context, owner string, all bool) ([]Export, error) {
	base, err := m.base()
	if err != nil {
		return nil, err
	}
	records, err := m.exportRecords()
	if err != nil {
		return nil, err
	}
	var done []frigateExport
	if err := m.frigate.call(ctx, http.MethodGet, base, "/api/exports", nil, nil, &done); err != nil {
		return nil, err
	}
	var jobs []frigateJob
	// Without the list of jobs, queued exports are missing until they start.
	_ = m.frigate.call(ctx, http.MethodGet, base, "/api/jobs/export", nil, nil, &jobs)
	out := []Export{}
	known := map[string]bool{}
	add := func(e Export) {
		record := records[e.ID]
		e.Owner, e.From, e.To = record.Owner, record.From, record.To
		if record.Created > 0 {
			e.Created = record.Created
		}
		known[e.ID] = true
		if all || m.owns(owner, record) {
			out = append(out, e)
		}
	}
	for _, e := range done {
		if !ValidEventID(e.ID) {
			continue
		}
		export := Export{ID: e.ID, Camera: e.Camera, Name: e.Name, Created: e.Date, State: ExportDone, file: path.Base(e.VideoPath), thumb: e.ThumbPath}
		if e.InProgress {
			export.State = ExportRunning
		} else {
			export.SizeBytes = m.exportSize(ctx, base, export)
		}
		add(export)
	}
	for _, j := range jobs {
		if known[j.ID] || !ValidEventID(j.ID) {
			continue
		}
		add(Export{ID: j.ID, Camera: j.Camera, Name: j.Name, Created: float64(m.now().Unix()), State: ExportRunning})
	}
	// Exports TitanOS started that Frigate lists nowhere: failed (Frigate
	// forgets a failed job after a while: the record keeps it, and
	// Frigate is asked once), or gone behind its back (the record goes).
	var gone []string
	failed := map[string]Export{}
	for id, record := range records {
		if known[id] || !(all || m.owns(owner, record)) {
			continue
		}
		created := record.Created
		if created == 0 {
			created = record.To
		}
		export := Export{ID: id, Camera: record.Camera, Name: record.Name, From: record.From, To: record.To, Created: created, State: ExportFailed, Owner: record.Owner}
		if !record.Failed {
			var job frigateJob
			if err := m.frigate.call(ctx, http.MethodGet, base, "/api/jobs/export/"+url.PathEscape(id), nil, nil, &job); err != nil {
				if frigateStatus(err) == http.StatusNotFound {
					gone = append(gone, id)
				}
				continue
			}
			export.State = ExportRunning
			if export.Camera == "" && ValidCameraID(job.Camera) {
				export.Camera = job.Camera
			}
			if export.Name == "" {
				export.Name = clip(job.Name, 200)
			}
			if job.Status == "failed" || job.Status == "cancelled" {
				export.State = ExportFailed
				failed[id] = export
			}
		}
		out = append(out, export)
	}
	if len(failed)+len(gone) > 0 {
		if err := m.changeExportRecords(func(records map[string]exportRecord) {
			for id, export := range failed {
				if record, ok := records[id]; ok {
					record.Failed, record.Camera, record.Name = true, export.Camera, export.Name
					records[id] = record
				}
			}
			for _, id := range gone {
				delete(records, id)
			}
		}); err != nil {
			slog.Warn("NVR: records of failed or vanished exports not updated", "err", err)
		}
	}
	slices.SortFunc(out, func(a, b Export) int {
		switch {
		case a.Created > b.Created:
			return -1
		case a.Created < b.Created:
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// exportSize asks for the size of a finished export (kept: it no longer
// changes).
func (m *Manager) exportSize(ctx context.Context, base string, e Export) *int64 {
	m.mu.Lock()
	size, ok := m.rt.exportSizes[e.ID]
	m.mu.Unlock()
	if ok {
		return &size
	}
	if e.file == "" || e.file == "." || e.file == "/" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, base+"/exports/"+url.PathEscape(e.file), nil)
	if err != nil {
		return nil
	}
	res, err := m.frigate.http.Do(req)
	if err != nil {
		return nil
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ContentLength < 0 {
		return nil
	}
	size = res.ContentLength
	m.mu.Lock()
	if m.rt.exportSizes == nil {
		m.rt.exportSizes = map[string]int64{}
	}
	m.rt.exportSizes[e.ID] = size
	m.mu.Unlock()
	return &size
}

// Export returns one export if the account may see it.
func (m *Manager) Export(ctx context.Context, id, owner string, all bool) (Export, error) {
	if !ValidEventID(id) {
		return Export{}, ErrExportNotFound
	}
	list, err := m.Exports(ctx, owner, all)
	if err != nil {
		return Export{}, err
	}
	for _, e := range list {
		if e.ID == id {
			return e, nil
		}
	}
	return Export{}, ErrExportNotFound
}

// ExportMedia are Frigate's paths of the video and the preview picture
// of a finished export.
func (e Export) ExportMedia() (video, thumbnail string, err error) {
	if e.State == ExportRunning {
		return "", "", ErrExportRunning
	}
	if e.State != ExportDone || e.file == "" || e.file == "." || strings.ContainsAny(e.file, "/\\") {
		return "", "", ErrExportNotFound
	}
	video = "/exports/" + url.PathEscape(e.file)
	// thumb_path is a file below Frigate's clips folder.
	if _, rest, ok := strings.Cut(e.thumb, "/clips/"); ok && !strings.Contains(rest, "..") {
		thumbnail = "/clips/" + (&url.URL{Path: rest}).EscapedPath()
	}
	return video, thumbnail, nil
}

// RenameExport gives an export another name.
func (m *Manager) RenameExport(ctx context.Context, id, owner string, all bool, name string) (Export, error) {
	if !exportName(name) {
		return Export{}, invalid("name")
	}
	e, err := m.Export(ctx, id, owner, all)
	if err != nil {
		return e, err
	}
	if e.State != ExportDone {
		return e, ErrExportRunning
	}
	base, err := m.base()
	if err != nil {
		return e, err
	}
	if err := m.frigate.call(ctx, http.MethodPatch, base, "/api/export/"+url.PathEscape(id)+"/rename", nil, map[string]any{"name": name}, nil); err != nil {
		return e, err
	}
	e.Name = name
	return e, nil
}

// DeleteExport removes an export and its file.
func (m *Manager) DeleteExport(ctx context.Context, id, owner string, all bool) error {
	e, err := m.Export(ctx, id, owner, all)
	if err != nil {
		return err
	}
	if e.State == ExportRunning {
		return ErrExportRunning
	}
	base, err := m.base()
	if err != nil {
		return err
	}
	if e.State == ExportDone {
		if err := m.frigate.call(ctx, http.MethodPost, base, "/api/exports/delete", nil, map[string]any{"ids": []string{id}}, nil); err != nil && frigateStatus(err) != http.StatusNotFound {
			return err
		}
	}
	m.mu.Lock()
	delete(m.rt.exportSizes, id)
	m.mu.Unlock()
	return m.changeExportRecords(func(records map[string]exportRecord) { delete(records, id) })
}

// ForgetExportOwner makes the exports of a deleted account ownerless:
// administrators still see them. A new account of the same name never
// sees them anyway (it was made after them, see owns); this tidies the
// records where the volume can be reached, switched on or not.
func (m *Manager) ForgetExportOwner(owner string) {
	m.mu.Lock()
	volume := m.settings.Volume
	m.mu.Unlock()
	if volume == "" || !slices.Contains(m.mounted(), volume) {
		return
	}
	err := m.changeExportRecords(func(records map[string]exportRecord) {
		for id, record := range records {
			if record.Owner == owner {
				record.Owner = ""
				records[id] = record
			}
		}
	})
	if err != nil {
		slog.Warn("NVR: exports of the deleted account keep its name", "err", err)
	}
}
