// Package protect holds the parts of the KI-Agent's protection functions
// (PLAN 54-S) that need no server: the ransomware detector and its event
// source (fanotify), the lookup of the writing SMB client, the in-place
// restore from a snapshot, the state of the emergency access and the
// emergency handbook. The api package wires them to shares, storage and
// notifications.
package protect

import (
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

// Op is what happened to a file.
type Op int

const (
	// OpWrite: a file opened for writing was closed (FAN_CLOSE_WRITE).
	OpWrite Op = iota + 1
	// OpRename: a file got a new name (FAN_RENAME); From is the old path.
	OpRename
	// OpCreate: a new file appeared (FAN_CREATE).
	OpCreate
)

// Event is one change of a file seen on a data volume.
type Event struct {
	At   time.Time
	Op   Op
	Path string // absolute
	From string // absolute, for OpRename
	PID  int
}

// Kind is how suspicious an event is.
type Kind int

const (
	KindNone Kind = iota
	// KindKnownRename: renamed to an extension known from ransomware.
	KindKnownRename
	// KindUnknownRename: renamed to an extension no normal program uses.
	KindUnknownRename
	// KindRewrite: rewritten with content that looks encrypted.
	KindRewrite
	// KindNote: a file named like a ransom note.
	KindNote
)

// Thresholds decide when activity on a share is an attack. Counts are per
// share and client within Window.
type Thresholds struct {
	Window time.Duration
	// Known renames to a ransomware extension.
	Known int
	// Renames to unknown extensions.
	Renames int
	// Rewrites with encrypted-looking content.
	Rewrites int
	// Ransom notes in different folders.
	Notes int
	// Quiet is how long nothing suspicious must happen before an incident
	// counts as stopped.
	Quiet time.Duration
}

// DefaultThresholds are the values ab Werk.
func DefaultThresholds() Thresholds {
	return Thresholds{Window: time.Minute, Known: 5, Renames: 30, Rewrites: 30, Notes: 3, Quiet: 2 * time.Minute}
}

// Affected is a file touched during an incident: Was is where it was (the
// path to restore), Now where it is (a renamed .locked copy, or Was).
// Both are relative to the share.
type Affected struct {
	Was  string `json:"was"`
	Now  string `json:"now"`
	Kind Kind   `json:"kind"`
}

// MaxAffected bounds the files kept per incident.
const MaxAffected = 5000

// Incident is an attack in progress (or one that stopped).
type Incident struct {
	Share  string
	Client string
	Reason string // known-extension, unknown-extension, rewrite, notes
	First  time.Time
	Last   time.Time
	Count  int
	Files  []Affected
	// More is how many files were seen beyond MaxAffected.
	More int
}

type mark struct {
	at   time.Time
	kind Kind
	dir  string
	file Affected
}

// Detector counts suspicious events per share and client and reports an
// incident once a threshold is crossed. It is safe for concurrent use.
type Detector struct {
	mu     sync.Mutex
	T      Thresholds
	marks  map[string][]mark // share|client
	active map[string]*Incident
	muted  map[string]time.Time // share|client or share| → until
}

// NewDetector returns a detector with the thresholds.
func NewDetector(t Thresholds) *Detector {
	return &Detector{T: t, marks: map[string][]mark{}, active: map[string]*Incident{}, muted: map[string]time.Time{}}
}

// SetThresholds changes the thresholds (the settings changed).
func (d *Detector) SetThresholds(t Thresholds) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.T = t
}

// Window is the time window of the thresholds.
func (d *Detector) Window() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.T.Window
}

func key(share, client string) string { return share + "|" + client }

// Mute ignores a client on a share (client "" for every client) until a
// time: after a false alarm, and while titand restores files itself.
func (d *Detector) Mute(share, client string, until time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.muted[key(share, client)] = until
	if client == "" {
		for k := range d.marks {
			if strings.HasPrefix(k, share+"|") {
				delete(d.marks, k)
			}
		}
	} else {
		delete(d.marks, key(share, client))
	}
}

func (d *Detector) mutedLocked(share, client string, at time.Time) bool {
	for _, k := range []string{key(share, client), key(share, "")} {
		if until, ok := d.muted[k]; ok {
			if at.Before(until) {
				return true
			}
			delete(d.muted, k)
		}
	}
	return false
}

// Active reports whether an incident runs on a share.
func (d *Detector) Active(share string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.active[share]
	return ok
}

// Observe adds a classified event. It returns a copy of the incident when
// this event started one, nil otherwise. Events of a share with an active
// incident are added to it whatever the client.
func (d *Detector) Observe(share, client string, kind Kind, file Affected, at time.Time) *Incident {
	if kind == KindNone || share == "" {
		return nil
	}
	file.Kind = kind
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mutedLocked(share, client, at) {
		return nil
	}
	if inc, ok := d.active[share]; ok {
		inc.add(file, at)
		return nil
	}
	k := key(share, client)
	recent := slices.DeleteFunc(d.marks[k], func(m mark) bool { return at.Sub(m.at) > d.T.Window })
	recent = append(recent, mark{at: at, kind: kind, dir: path.Dir(file.Now), file: file})
	d.marks[k] = recent
	reason := d.judge(recent)
	if reason == "" {
		return nil
	}
	delete(d.marks, k)
	inc := &Incident{Share: share, Client: client, Reason: reason, First: recent[0].at, Last: at}
	d.active[share] = inc
	for _, m := range recent {
		inc.add(m.file, m.at)
	}
	out := inc.copy()
	return &out
}

// judge decides from the marks of a window whether it is an attack.
func (d *Detector) judge(marks []mark) string {
	known, unknown, rewrites := 0, 0, 0
	notes := map[string]bool{}
	for _, m := range marks {
		switch m.kind {
		case KindKnownRename:
			known++
		case KindUnknownRename:
			unknown++
		case KindRewrite:
			rewrites++
		case KindNote:
			notes[m.dir] = true
		}
	}
	switch {
	case d.T.Known > 0 && known >= d.T.Known:
		return "known-extension"
	case d.T.Renames > 0 && known+unknown >= d.T.Renames:
		return "unknown-extension"
	case d.T.Rewrites > 0 && rewrites >= d.T.Rewrites:
		return "rewrite"
	case d.T.Notes > 0 && len(notes) >= d.T.Notes:
		return "notes"
	}
	return ""
}

func (inc *Incident) add(file Affected, at time.Time) {
	inc.Last = at
	inc.Count++
	if i := slices.IndexFunc(inc.Files, func(f Affected) bool { return f.Now == file.Was }); i >= 0 {
		// Rewritten, then renamed: the original stays where it was.
		inc.Files[i].Now = file.Now
		if file.Kind != KindNone {
			inc.Files[i].Kind = file.Kind
		}
		return
	}
	if len(inc.Files) >= MaxAffected {
		inc.More++
		return
	}
	inc.Files = append(inc.Files, file)
}

func (inc *Incident) copy() Incident {
	out := *inc
	out.Files = slices.Clone(inc.Files)
	return out
}

// Snapshot returns a copy of the active incident of a share.
func (d *Detector) Snapshot(share string) (Incident, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	inc, ok := d.active[share]
	if !ok {
		return Incident{}, false
	}
	return inc.copy(), true
}

// Tick ends the incidents without suspicious events for Quiet and returns
// them.
func (d *Detector) Tick(now time.Time) []Incident {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []Incident
	for share, inc := range d.active {
		if now.Sub(inc.Last) >= d.T.Quiet {
			out = append(out, inc.copy())
			delete(d.active, share)
		}
	}
	slices.SortFunc(out, func(a, b Incident) int { return a.First.Compare(b.First) })
	return out
}

// Forget drops the incident of a share (false alarm).
func (d *Detector) Forget(share string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.active, share)
}

// ---------- classification ----------

// RansomExtensions are extensions known from ransomware families.
var RansomExtensions = []string{
	".locked", ".lock", ".encrypted", ".enc", ".crypt", ".crypted", ".cry", ".crypto", ".ryk", ".conti",
	".lockbit", ".blackcat", ".akira", ".royal", ".play", ".phobos", ".dharma", ".wncry", ".wcry", ".wnry",
	".locky", ".zepto", ".cerber", ".petya", ".ezz", ".ecc", ".exx", ".xyz", ".zzz", ".aaa", ".micro",
	".vvv", ".ttt", ".abc", ".rrk", ".djvu", ".stop", ".medusa", ".hive", ".basta", ".clop", ".rhysida",
}

// commonExtensions are what normal programs rename files to; a rename to
// anything else counts as unknown.
var commonExtensions = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`
		.txt .md .csv .tsv .log .json .xml .yaml .yml .ini .conf .cfg .toml .html .htm .css .js .ts .go .py .sh .c .h .cpp .java .rs .php .rb .sql
		.pdf .doc .docx .dot .dotx .odt .ott .rtf .xls .xlsx .xlsm .ods .ppt .pptx .odp .pages .numbers .key .epub .mobi
		.jpg .jpeg .png .gif .bmp .tif .tiff .webp .heic .heif .avif .svg .raw .cr2 .cr3 .nef .arw .dng .orf .rw2 .psd .xcf .ai
		.mp3 .m4a .aac .flac .wav .ogg .opus .wma .mp4 .m4v .mov .mkv .avi .webm .wmv .mpg .mpeg .ts .3gp .srt .sub .vtt
		.zip .7z .rar .tar .gz .tgz .bz2 .xz .zst .iso .img .dmg .vhd .vhdx .vmdk .qcow2
		.bak .old .orig .tmp .temp .part .partial .crdownload .download .swp .lnk .url .desktop .ics .vcf .eml .msg .pst .ost
		.exe .msi .dll .apk .deb .rpm .jar .db .sqlite .sqlite3 .kdbx .gpg .asc .pem .crt .torrent .nfo .sfv .md5 .sha256 .~lock .lck`) {
		commonExtensions[e] = true
	}
}

// ext is the lower-case extension of a name.
func ext(name string) string { return strings.ToLower(path.Ext(name)) }

// ClassifyRename judges a rename from one name to another.
func ClassifyRename(from, to string) Kind {
	if IsNote(path.Base(to)) {
		return KindNote
	}
	oldExt, newExt := ext(from), ext(to)
	if newExt == oldExt {
		return KindNone
	}
	if slices.Contains(RansomExtensions, newExt) {
		return KindKnownRename
	}
	if newExt == "" || commonExtensions[newExt] {
		return KindNone
	}
	// Temporary names of editors and sync tools (x.docx.sb-1234-abc,
	// x.~tmp, long hashes) are not what ransomware uses.
	if strings.ContainsAny(newExt, "-~$#") || len(newExt) > 13 {
		return KindNone
	}
	// Office and editors save over temporary names (~$x.docx, x.docx.sb-…),
	// sync tools over .tmp names: only a rename away from a normal
	// extension to an unknown one counts.
	if oldExt != "" && !commonExtensions[oldExt] {
		return KindNone
	}
	return KindUnknownRename
}

// notePatterns are parts of names ransomware gives its notes.
var notePatterns = []string{
	"decrypt", "how_to_restore", "how-to-restore", "how_to_recover", "restore_files", "restore-my-files", "restore_my_files",
	"recover_files", "recovery_files", "ransom", "your_files", "files_encrypted", "readme_for_decrypt", "_readme.txt",
	"read_me.hta", "!!!", "readme-warning", "help_your_files", "unlock_files", "how_to_back_files",
}

// IsNote reports whether a file name looks like a ransom note.
func IsNote(name string) bool {
	n := strings.ToLower(name)
	switch ext(n) {
	case ".txt", ".html", ".htm", ".hta", ".rtf", ".url", "":
	default:
		return false
	}
	for _, p := range notePatterns {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}
