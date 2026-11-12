// Package diskwake remembers, in RAM only, which part of TitanOS touched a
// volume a moment ago. The disk sleep timer (storage.RunDiskSleep) asks it
// when a sleeping disk woke up, so that the settings can say what woke it.
//
// Noting an access is a map update under a mutex: no file, no command, no
// disk and no /config. While no disk may sleep it is a single atomic load.
// Every part of TitanOS that reads or writes a volume on its own notes it
// right before it does so.
package diskwake

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Cause codes. The interface translates them; Detail is a value (a job, an
// account, a share, a feature), never a sentence.
const (
	Unknown        = "unknown"         // nothing of TitanOS: a share, an app, a VM
	BackupJob      = "backup_job"      // data backup; detail: job
	AppBackup      = "app_backup"      // application backup
	ConfigBackup   = "config_backup"   // copy of the stick
	Journal        = "journal"         // kept system journal
	ActivityLog    = "activity_log"    //
	Notifications  = "notifications"   // bell and held personal notifications
	SmartHistory   = "smart_history"   //
	Monitoring     = "monitoring"      // monitoring history
	ShadowCopies   = "shadow_copies"   // previous versions (sync every 10 minutes)
	RecycleSweep   = "recycle_sweep"   // emptying recycle bins
	FileIndex      = "file_index"      // full-text index pass or search
	Thumbnails     = "thumbnails"      // thumbnails and previews
	SnapshotPlan   = "snapshot_plan"   // scheduled snapshot
	Maintenance    = "maintenance"     // detail: scrub, raid_check, btrfs, plan
	DiskTest       = "disk_test"       // SMART self-test or disk test
	VMDisk         = "vm_disk"         // disk handed to a VM
	DataMove       = "data_move"       // TitanOS data moved between volumes; detail: feature
	VMStart        = "vm_start"        // detail: VM
	ContainerStart = "container_start" // detail: app or container
	LXCStart       = "lxc_start"       // detail: container
	DockerData     = "docker_data"     // Docker data root on the volume
	Replication    = "replication"     // detail: job
	UsageScan      = "usage_scan"      // storage usage of a volume
	QuotaScan      = "quota_scan"      // share quotas
	FileManager    = "file_manager"    // detail: account
	WebDAV         = "webdav"          // detail: account
	SMBSession     = "smb_session"     // an SMB client is connected; detail: share
	DockerRunning  = "docker_running"  // Docker runs on the volume (its containers write there)
	Agent          = "agent"           // data of the AI agent
	FileTransfer   = "file_transfer"   // copy/move/upload jobs of the file manager
	Snapshot       = "snapshot"        // snapshot made, listed or restored on request
	PrivateData    = "private_data"    // other .titanos/<feature>; detail: feature
)

const (
	maxTargets = 64
	maxNotes   = 16
	maxDetail  = 96
)

// Cause is why a volume was touched.
type Cause struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

type note struct {
	cause Cause
	at    time.Time
}

// Recorder keeps the last accesses per volume. The zero value is off.
type Recorder struct {
	on    atomic.Bool
	mu    sync.Mutex
	notes map[string][]note
	// Now replaces time.Now in tests.
	Now func() time.Time
}

// Enable switches noting on or off. Off forgets everything: with no disk
// that may sleep nothing is kept at all.
func (r *Recorder) Enable(on bool) {
	if r.on.Swap(on) && !on {
		r.mu.Lock()
		r.notes = nil
		r.mu.Unlock()
	}
}

// Enabled tells whether accesses are kept.
func (r *Recorder) Enabled() bool { return r.on.Load() }

// Note remembers that code (with detail) is about to touch the volume.
func (r *Recorder) Note(volume, code, detail string) {
	if !r.on.Load() || volume == "" || code == "" {
		return
	}
	if len(detail) > maxDetail {
		detail = detail[:maxDetail]
		for !utf8.ValidString(detail) {
			detail = detail[:len(detail)-1]
		}
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	cause, at := Cause{Code: code, Detail: detail}, now()
	r.mu.Lock()
	defer r.mu.Unlock()
	list, known := r.notes[volume]
	if !known && len(r.notes) >= maxTargets {
		return // more volumes than a NAS has: keep what is there
	}
	if r.notes == nil {
		r.notes = map[string][]note{}
	}
	oldest := 0
	for i := range list {
		if list[i].cause == cause {
			list[i].at = at
			return
		}
		if list[i].at.Before(list[oldest].at) {
			oldest = i
		}
	}
	if len(list) >= maxNotes {
		list[oldest] = note{cause, at}
		return
	}
	r.notes[volume] = append(list, note{cause, at})
}

// Recent returns what touched the volume between from and to (inclusive),
// newest first.
func (r *Recorder) Recent(volume string, from, to time.Time) []Cause {
	r.mu.Lock()
	var found []note
	for _, n := range r.notes[volume] {
		if !n.at.Before(from) && !n.at.After(to) {
			found = append(found, n)
		}
	}
	r.mu.Unlock()
	sort.SliceStable(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	causes := make([]Cause, 0, len(found))
	for _, n := range found {
		causes = append(causes, n.cause)
	}
	return causes
}

// Default is the recorder of the running titand.
var Default = &Recorder{}

// Note notes an access in the Default recorder.
func Note(volume, code, detail string) { Default.Note(volume, code, detail) }

// MountRoot is where the volumes are mounted (NotePath).
const MountRoot = "/mnt/"

// NotePath is Note for a path below /mnt/<volume>; other paths are not
// on a volume and are ignored.
func NotePath(path, code, detail string) {
	if !Default.on.Load() {
		return
	}
	rest, ok := strings.CutPrefix(path, MountRoot)
	if !ok {
		return
	}
	volume, _, _ := strings.Cut(rest, "/")
	Default.Note(volume, code, detail)
}

// features maps the folders under .titanos to their cause.
var features = map[string]string{
	"logs": Journal, "activity": ActivityLog, "notify": Notifications, "smart": SmartHistory,
	"monitoring": Monitoring, "backups": ConfigBackup, "appbackups": AppBackup, "backup": BackupJob,
	"shadowcopies": ShadowCopies, "snaplocks": Snapshot, "snapplans": SnapshotPlan,
	"fileindex": FileIndex, "docs": FileIndex, "thumbs": Thumbnails, "media": Thumbnails,
	"maintenance": Maintenance, "replicated": Replication, "replicas": Replication,
	"agent": Agent, "transfers": FileTransfer,
}

// FeatureCause is the cause for an access to .titanos/<feature>.
func FeatureCause(feature string) Cause {
	if code, ok := features[feature]; ok {
		if code == Maintenance {
			return Cause{Code: code, Detail: "plan"}
		}
		return Cause{Code: code}
	}
	return Cause{Code: PrivateData, Detail: feature}
}

// NoteFeature notes an access to .titanos/<feature> of a volume.
func NoteFeature(volume, feature string) {
	if !Default.on.Load() {
		return
	}
	cause := FeatureCause(feature)
	Default.Note(volume, cause.Code, cause.Detail)
}
