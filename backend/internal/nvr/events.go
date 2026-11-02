package nvr

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Event is something a camera saw. Motion events come from the motion
// check; Labels name what an object detector found in the picture (person,
// vehicle, package, animal, plate) when one is installed.
type Event struct {
	ID       string    `json:"id"` // "2026-11-02/14-05-00"
	Camera   string    `json:"camera"`
	Kind     string    `json:"kind"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end,omitempty"`
	Labels   []string  `json:"labels,omitempty"`
	Snapshot bool      `json:"snapshot"`
}

// Detector recognises objects in a still picture. TitanOS ships none in the
// base image: the models and runtimes come as signed plugins (Plan 26).
type Detector interface {
	// Detect returns labels such as "person", "vehicle", "package",
	// "animal" or "plate".
	Detect(jpeg []byte) ([]string, error)
}

func eventDir(root, camera string) string { return filepath.Join(root, camera, "events") }

func eventPath(root string, e Event) string {
	return filepath.Join(eventDir(root, e.Camera), filepath.FromSlash(e.ID))
}

// eventID names an event by its start time in UTC, like the recordings.
func eventID(t time.Time) string { return t.UTC().Format("2006-01-02/15-04-05") }

// WriteEvent stores (or replaces) the description of an event.
func WriteEvent(root string, e Event) error {
	p := eventPath(root, e) + ".json"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// WriteSnapshot stores the picture of an event.
func WriteSnapshot(root string, e Event, jpeg []byte) error {
	p := eventPath(root, e) + ".jpg"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, jpeg, 0o644)
}

// SnapshotPath is where the picture of an event lies.
func SnapshotPath(root, camera, day, file string) string {
	return filepath.Join(eventDir(root, camera), day, file)
}

// Events lists a camera's events, newest first, at most limit.
func Events(root, camera string, limit int) []Event {
	var out []Event
	_ = filepath.WalkDir(eventDir(root, camera), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var e Event
		if json.Unmarshal(data, &e) == nil && e.Camera == camera {
			out = append(out, e)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// pruneEvents removes events older than keep days; the folder of the
// camera's recordings is handled by Prune.
func pruneEvents(root, camera string, keep int, now time.Time) int {
	removed := 0
	base := eventDir(root, camera)
	days, err := os.ReadDir(base)
	if err != nil {
		return 0
	}
	for _, day := range days {
		if !day.IsDir() {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02", day.Name(), time.UTC)
		if err != nil || now.Sub(t.AddDate(0, 0, 1)) <= time.Duration(keep)*24*time.Hour {
			continue
		}
		if os.RemoveAll(filepath.Join(base, day.Name())) == nil {
			removed++
		}
	}
	return removed
}
