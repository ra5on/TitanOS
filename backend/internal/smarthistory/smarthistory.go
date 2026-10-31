// Package smarthistory keeps a history of the most important SMART values of
// each disk (PLAN 7e): once a day, from the values titand already holds in
// RAM (storage.SmartReports runs no smartctl, sleeping disks stay asleep).
// The history lies on a data volume in .titanos/smart/, one small JSON file
// per disk named after model and serial number, never on the boot stick and
// never in RAM only. Without a mounted volume nothing is kept.
package smarthistory

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	// Feature is the folder below .titanos on the volume.
	Feature = "smart"
	// MaxSamples is how many days are kept per disk (about two years).
	MaxSamples = 750
	// fileLimit is the largest history file read.
	fileLimit = 1 << 20
	// every is how often titand looks whether today's values are kept.
	every = time.Hour
	// firstAfter gives the volumes time to mount and SMART its first read.
	firstAfter = 3 * time.Minute
)

// Sample holds the values of one day. Missing values were not reported by
// the disk (an NVMe SSD has no reallocated sectors, a HDD no wear).
type Sample struct {
	// Day is the local date, YYYY-MM-DD.
	Day string `json:"day"`
	// ReadAt is when SMART was read; a disk asleep all day keeps older values.
	ReadAt         time.Time `json:"readAt"`
	TemperatureC   *int      `json:"temperatureC,omitempty"`
	PowerOnHours   *int64    `json:"powerOnHours,omitempty"`
	Reallocated    *uint64   `json:"reallocatedSectors,omitempty"`
	Pending        *uint64   `json:"pendingSectors,omitempty"`
	Uncorrectable  *uint64   `json:"uncorrectableSectors,omitempty"`
	CRCErrors      *uint64   `json:"crcErrors,omitempty"`
	MediaErrors    *uint64   `json:"mediaErrors,omitempty"`
	PercentageUsed *int      `json:"percentageUsed,omitempty"`
	AvailableSpare *int      `json:"availableSpare,omitempty"`
}

// file is what lies on the volume per disk.
type file struct {
	Model   string   `json:"model"`
	Serial  string   `json:"serial"`
	Samples []Sample `json:"samples"`
}

// History is the answer of the API for one disk.
type History struct {
	Disk string `json:"disk"`
	// Volume is where the history lies; empty without a mounted volume.
	Volume  string   `json:"volume"`
	Samples []Sample `json:"samples"`
}

// Recorder keeps the history.
type Recorder struct {
	// Reports returns the SMART state of every disk from the RAM cache.
	Reports func(ctx context.Context) ([]storage.SmartReport, error)
	// Mounted returns the names of the mounted volumes.
	Mounted   func() []string
	MountRoot string
	// Disabled keeps disks untouched (development mode).
	Disabled bool
	now      func() time.Time

	mu sync.Mutex
	// kept is the last day kept per volume and disk, to spare reading the
	// files every hour.
	kept map[string]string
}

// New returns a recorder for volumes below mountRoot.
func New(mountRoot string) *Recorder {
	return &Recorder{MountRoot: mountRoot, now: time.Now, kept: map[string]string{}}
}

// Key names the file of a disk: model and serial number, lower case, only
// letters, digits, dot, dash and underscore. Without a serial number a disk
// cannot be told from another one later, and gets no history.
func Key(model, serial string) string {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return ""
	}
	clean := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(strings.TrimSpace(s)) {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
				b.WriteRune(r)
			default:
				b.WriteByte('_')
			}
		}
		return b.String()
	}
	key := clean(model) + "_" + clean(serial)
	key = strings.TrimLeft(key, "._-")
	if len(key) > 120 {
		key = key[len(key)-120:]
	}
	if key == "" {
		return ""
	}
	return key
}

// volume is where the history lies: the first mounted volume by name, the
// same rule as for the kept journal.
func (r *Recorder) volume() (string, []string) {
	if r.Mounted == nil {
		return "", nil
	}
	mounted := r.Mounted()
	for _, v := range slices.Sorted(slices.Values(mounted)) {
		if naming.ValidVolume(v) {
			return v, mounted
		}
	}
	return "", mounted
}

// sampleOf takes the values of a report.
func sampleOf(day string, report storage.SmartReport) Sample {
	v := report.Values
	s := Sample{
		Day: day, ReadAt: *report.ReadAt, TemperatureC: v.TemperatureC, PowerOnHours: v.PowerOnHours,
		Reallocated: v.ReallocatedSectors, Pending: v.PendingSectors, Uncorrectable: v.UncorrectableSectors, CRCErrors: v.CRCErrors,
	}
	if v.NVMe != nil {
		media, used, spare := v.NVMe.MediaErrors, v.NVMe.PercentageUsed, v.NVMe.AvailableSpare
		s.MediaErrors, s.PercentageUsed, s.AvailableSpare = &media, &used, &spare
	}
	return s
}

// Record keeps today's values of every disk that has none yet. A disk whose
// values were not read again since the last sample (asleep all day) gets no
// new one.
func (r *Recorder) Record(ctx context.Context) error {
	if r.Disabled || r.Reports == nil {
		return nil
	}
	volume, mounted := r.volume()
	if volume == "" {
		return nil
	}
	reports, err := r.Reports(ctx)
	if err != nil {
		return err
	}
	day := r.now().Format(time.DateOnly)
	var errs []error
	for _, report := range reports {
		key := Key(report.Model, report.Serial)
		if key == "" || report.Values == nil || report.ReadAt == nil {
			continue
		}
		r.mu.Lock()
		done := r.kept[volume+"/"+key] == day
		r.mu.Unlock()
		if done {
			continue
		}
		if err := r.keep(volume, mounted, key, day, report); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// keep adds today's sample of one disk to its file.
func (r *Recorder) keep(volume string, mounted []string, key, day string, report storage.SmartReport) error {
	root, err := volumedata.Open(r.MountRoot, volume, Feature, mounted)
	if err != nil {
		return err
	}
	defer root.Close()
	var f file
	name := key + ".json"
	if err := volumedata.ReadJSON(root, name, &f, fileLimit); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// A damaged file is started anew rather than blocking the history.
		slog.Warn("SMART history unreadable, starting anew", "disk", report.Disk, "err", err)
		f = file{}
	}
	mark := func() {
		r.mu.Lock()
		r.kept[volume+"/"+key] = day
		r.mu.Unlock()
	}
	if n := len(f.Samples); n > 0 {
		last := f.Samples[n-1]
		if last.Day == day || !report.ReadAt.After(last.ReadAt) {
			mark()
			return nil
		}
	}
	f.Model, f.Serial = report.Model, report.Serial
	f.Samples = append(f.Samples, sampleOf(day, report))
	if len(f.Samples) > MaxSamples {
		f.Samples = slices.Clone(f.Samples[len(f.Samples)-MaxSamples:])
	}
	if err := volumedata.WriteJSON(root, name, f); err != nil {
		return err
	}
	mark()
	return nil
}

// History returns the kept values of a disk, oldest first.
func (r *Recorder) History(report storage.SmartReport) (History, error) {
	h := History{Disk: report.Disk, Samples: []Sample{}}
	key := Key(report.Model, report.Serial)
	if r.Disabled || key == "" {
		return h, nil
	}
	volume, mounted := r.volume()
	if volume == "" {
		return h, nil
	}
	h.Volume = volume
	root, err := volumedata.Open(r.MountRoot, volume, Feature, mounted)
	if err != nil {
		return h, err
	}
	defer root.Close()
	var f file
	if err := volumedata.ReadJSON(root, key+".json", &f, fileLimit); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return h, nil
		}
		return h, err
	}
	if f.Samples != nil {
		h.Samples = f.Samples
	}
	return h, nil
}

// Run keeps the history until ctx ends: first a few minutes after the
// start, then every hour (one write per disk and day at most).
func (r *Recorder) Run(ctx context.Context) {
	if r.Disabled {
		return
	}
	wait := firstAfter
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err := r.Record(ctx); err != nil {
			slog.Warn("SMART history not kept", "err", err)
		}
		wait = every
	}
}
