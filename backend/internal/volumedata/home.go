package volumedata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/diskwake"
	"github.com/ra5on/TitanOS/backend/internal/naming"
)

// Home says where TitanOS keeps its own data (.titanos/<feature>) while no
// volume was chosen for a feature, and brings along what an earlier default
// volume holds.
//
// Until 0.76 the default was the first mounted volume by name, also when
// its disks were asleep. Choose now prefers a volume that never sleeps
// (storage.SystemDataVolume). So that nothing is lost or split over two
// volumes, the feature's folder is moved from the volume used before to the
// chosen one: from the first mounted volume by name (the old rule, once per
// start and pair of volumes) and from the volume this start used so far
// (every time the choice changes: when disk sleep is set differently or a
// volume is unlocked later). Afterwards no other volume is touched, so a
// sleeping volume is not woken again.
//
// A move never runs in the caller: Volume starts it in the background, with
// a time limit, and answers "" (no volume yet) until it is over, the same
// answer writers get while no volume is mounted. Writers take the feature
// with Hold or Use around their writes; a move waits for them and they get
// no volume while one waits or runs, so nothing is appended to a file that
// is being moved away.
type Home struct {
	MountRoot string
	// Mounted returns the names of the mounted volumes.
	Mounted func() []string
	// Choose returns the default volume, "" while there is none (yet).
	Choose func() string
	// Context ends moves when titand stops; nil never ends them early.
	// Once it is done no move is started any more.
	Context context.Context
	// MoveLimit bounds one move; zero means 15 minutes.
	MoveLimit time.Duration
	// OnMoved is told after the folder of a feature was moved away from a
	// volume (also partly), so that its writer forgets what it knew there.
	OnMoved func(feature, from, to string)
	// Override returns the volume chosen for one feature, "" while it
	// follows the default volume (PLAN 79-8). A chosen volume that is not
	// mounted means "no volume yet", never another one. Nil: no feature
	// has a choice of its own.
	Override func(feature string) string
	// Pending returns the volumes that still hold data of a feature from
	// before its volume was changed (kept in /config, so that a move cut
	// short by a restart is taken up again). Settled is told once such a
	// volume was gone through without an error. Both may be nil.
	Pending func(feature string) []string
	Settled func(feature, from string)
	// RetryAfter is how long after a move that did not get through (the
	// time limit, an error) it is tried again; zero means five minutes.
	// Until then the feature works on its volume, and what could not be
	// moved stays where it was.
	RetryAfter time.Duration
	// OnMissing is told, once, when a chosen volume has not been mounted
	// for MissingAfter (zero: 15 minutes): everything kept there waits in
	// memory and nothing falls back to another volume, so someone must
	// know. OnBack is told when such a volume is there again.
	OnMissing    func(volume string)
	OnBack       func(volume string)
	MissingAfter time.Duration
	// now is the clock; nil means time.Now.
	now func() time.Time
	// missing are the chosen volumes that are not mounted.
	missing map[string]*missingVolume
	// NoLegacy names features that never lived on the first volume by
	// default (the monitoring history): for them the rule until 0.76 does
	// not apply.
	NoLegacy []string

	mu       sync.Mutex
	features map[string]*homeFeature
	// work counts the background moves; Wait waits for them (tests).
	work sync.WaitGroup
}

type missingVolume struct {
	since time.Time
	told  bool
}

func (h *Home) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// seen notes whether a volume a feature belongs on is mounted, and tells
// OnMissing and OnBack. Only a chosen volume can be missing: the automatic
// choice is always one of the mounted ones.
func (h *Home) seen(volume string, mounted bool) {
	h.mu.Lock()
	entry := h.missing[volume]
	var tell func(string)
	switch {
	case mounted && entry != nil:
		if entry.told {
			tell = h.OnBack
		}
		delete(h.missing, volume)
	case !mounted && entry == nil:
		if h.missing == nil {
			h.missing = map[string]*missingVolume{}
		}
		h.missing[volume] = &missingVolume{since: h.clock()}
	case !mounted:
		after := h.MissingAfter
		if after <= 0 {
			after = 15 * time.Minute
		}
		if !entry.told && h.clock().Sub(entry.since) >= after {
			entry.told = true
			tell = h.OnMissing
		}
	}
	h.mu.Unlock()
	if tell != nil {
		tell(volume)
	}
}

// Watch asks for the volume of each feature every minute until ctx ends,
// so that a chosen volume that stays away is noticed and a move that is
// due starts also while nothing is written.
func (h *Home) Watch(ctx context.Context, features []string) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for _, feature := range features {
			h.Volume(feature)
		}
	}
}

type homeFeature struct {
	// io: writers hold it shared around their writes, a move alone.
	io sync.RWMutex

	mu      sync.Mutex
	last    string
	tried   map[string]bool
	cleaned map[string]bool
	// retry are the moves ("from\x00to") that did not get through, with
	// the time they are tried again.
	retry map[string]time.Time
	// moving is closed when the running move is over; nil without one.
	moving chan struct{}
}

// shortWait is how long Volume waits for a move it started: one with
// nothing to move is over at once and the caller gets its volume.
const shortWait = 250 * time.Millisecond

// Default is the default volume without moving anything, for features that
// read every volume anyway (copies of /config).
func (h *Home) Default() string {
	if h == nil || h.Choose == nil {
		return ""
	}
	return h.Choose()
}

func (h *Home) feature(name string) *homeFeature {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.features == nil {
		h.features = map[string]*homeFeature{}
	}
	state := h.features[name]
	if state == nil {
		state = &homeFeature{tried: map[string]bool{}, cleaned: map[string]bool{}, retry: map[string]time.Time{}}
		h.features[name] = state
	}
	return state
}

func (h *Home) context() context.Context {
	if h.Context != nil {
		return h.Context
	}
	return context.Background()
}

// Wait waits for the moves running in the background.
func (h *Home) Wait() { h.work.Wait() }

// Target is the volume a feature belongs on, without moving anything: the
// one chosen for it, else the default volume. own tells which of the two.
func (h *Home) Target(feature string) (volume string, own bool) {
	if h == nil {
		return "", false
	}
	if h.Override != nil {
		if volume := h.Override(feature); volume != "" {
			return volume, true
		}
	}
	return h.Default(), false
}

// Moving tells whether the folder of a feature is being moved right now.
func (h *Home) Moving(feature string) bool {
	state := h.feature(feature)
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.moving != nil
}

// Volume returns the volume for a feature (the one chosen for it, else the
// default volume), "" while there is none or while its folder is being
// moved there from an earlier volume. It never blocks for longer than a
// moment.
func (h *Home) Volume(feature string) string {
	volume, own := h.Target(feature)
	if volume == "" || h.Mounted == nil {
		return volume
	}
	state := h.feature(feature)
	state.mu.Lock()
	mounted := h.Mounted()
	there := slices.Contains(mounted, volume)
	h.seen(volume, there)
	if !there {
		state.mu.Unlock()
		return ""
	}
	if state.moving == nil {
		var sources []string
		// done: tried and through, or tried and waiting for its next turn.
		done := func(from string) bool {
			key := from + "\x00" + volume
			if again, waiting := state.retry[key]; waiting {
				return h.clock().Before(again)
			}
			return state.tried[key]
		}
		if state.last != "" && state.last != volume && slices.Contains(mounted, state.last) {
			sources = append(sources, state.last)
		}
		// A move that did not get through (the time limit, an error) has
		// another turn after a while, without a restart.
		for key, again := range state.retry {
			from, to, _ := strings.Cut(key, "\x00")
			if to == volume && from != volume && !h.clock().Before(again) && !slices.Contains(sources, from) && slices.Contains(mounted, from) {
				sources = append(sources, from)
			}
		}
		if h.Pending != nil {
			// Left behind when the volume was changed and the move did not
			// finish (restart, time limit).
			for _, name := range h.Pending(feature) {
				if name != volume && !slices.Contains(sources, name) && slices.Contains(mounted, name) && !done(name) {
					sources = append(sources, name)
				}
			}
		}
		// The rule until 0.76 (first volume by name) only counts for a
		// feature that follows the default: one with a volume of its own
		// never lived there by default.
		for _, name := range slices.Sorted(slices.Values(mounted)) {
			if own || slices.Contains(h.NoLegacy, feature) {
				break
			}
			if !naming.ValidVolume(name) {
				continue
			}
			if name != volume && !slices.Contains(sources, name) && !done(name) {
				sources = append(sources, name)
			}
			break
		}
		if len(sources) == 0 {
			state.last = volume
			if !state.cleaned[volume] {
				// What a move that was cut short left in the folder.
				state.cleaned[volume] = true
				h.work.Go(func() {
					if err := CleanLeftovers(h.MountRoot, volume, feature, mounted); err != nil {
						slog.Warn("leftovers of an interrupted move of system data not removed", "feature", feature, "volume", volume, "err", err)
					}
				})
			}
			state.mu.Unlock()
			return volume
		}
		if h.context().Err() != nil {
			// titand stops: no move now, and nothing is written to the new
			// place before the old data are there.
			state.mu.Unlock()
			return ""
		}
		state.moving = make(chan struct{})
		h.work.Go(func() { h.move(state, feature, sources, volume, mounted) })
	}
	moving := state.moving
	state.mu.Unlock()
	select {
	case <-moving:
	case <-time.After(shortWait):
		return ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.moving != nil || state.last != volume {
		return ""
	}
	return volume
}

// move brings the folder of a feature from sources to volume, in the
// background and bounded. Whatever happens, the feature then lives on
// volume: what could not be moved stays where it is and is named in the
// journal. A move that was cut off by its time limit or an error is taken
// up again after RetryAfter (it goes on where it stopped).
func (h *Home) move(state *homeFeature, feature string, sources []string, volume string, mounted []string) {
	limit := h.MoveLimit
	if limit <= 0 {
		limit = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(h.context(), limit)
	defer cancel()
	state.io.Lock() // writers in flight finish first; new ones get no volume
	failed := map[string]bool{}
	for _, from := range sources {
		diskwake.Note(from, diskwake.DataMove, feature)
		diskwake.Note(volume, diskwake.DataMove, feature)
		result, err := Adopt(ctx, h.MountRoot, from, volume, feature, mounted)
		if err == nil && h.Settled != nil {
			h.Settled(feature, from)
		}
		failed[from] = err != nil
		switch {
		case err != nil:
			slog.Warn("system data of an earlier default volume not moved completely; the rest stays there", "feature", feature, "from", from, "to", volume, "moved", result.Moved, "err", err)
		case len(result.Left) > 0 || len(result.Aside) > 0:
			slog.Warn("system data of an earlier default volume moved; names that exist on both volumes were put next to the current ones as <name>.from-<volume>", "feature", feature, "from", from, "to", volume, "moved", result.Moved, "aside", result.Aside, "left", result.Left)
		case result.Moved > 0:
			slog.Info("system data moved to the default volume", "feature", feature, "from", from, "to", volume, "entries", result.Moved)
		}
		if (result.Moved > 0 || len(result.Aside) > 0) && h.OnMoved != nil {
			h.OnMoved(feature, from, volume)
		}
	}
	state.io.Unlock()
	state.mu.Lock()
	again := h.RetryAfter
	if again <= 0 {
		again = 5 * time.Minute
	}
	for _, from := range sources {
		key := from + "\x00" + volume
		if failed[from] {
			// Not through: another turn later, without a restart.
			state.retry[key] = h.clock().Add(again)
			continue
		}
		delete(state.retry, key)
		state.tried[key] = true
	}
	state.last = volume
	state.cleaned[volume] = true
	close(state.moving)
	state.moving = nil
	state.mu.Unlock()
}

// Hold takes the folder of a feature for a write: no move starts until
// release is called. ok is false while a move waits or runs; the writer
// then has no volume and tries again later. It never blocks for longer
// than a moment.
func (h *Home) Hold(feature string) (release func(), ok bool) {
	// Starts a move that is due and waits a moment for one with nothing to do.
	volume := h.Volume(feature)
	state := h.feature(feature)
	if !state.io.TryRLock() {
		return func() {}, false
	}
	// The writer writes next: that is what woke the disks if they slept.
	diskwake.NoteFeature(volume, feature)
	return sync.OnceFunc(state.io.RUnlock), true
}

// Use is Hold together with the volume to write to: "" (and nothing held)
// while there is none.
func (h *Home) Use(feature string) (volume string, release func()) {
	release, ok := h.Hold(feature)
	if !ok {
		return "", release
	}
	if volume = h.Volume(feature); volume == "" {
		release()
		return "", func() {}
	}
	return volume, release
}

// OpenExisting pins .titanos/<feature> of a mounted volume if it is there
// and private; unlike Open it creates nothing. fs.ErrNotExist without the
// folder.
func OpenExisting(mountRoot, volume, feature string, mounted []string) (*os.Root, error) {
	diskwake.NoteFeature(volume, feature)
	return openExisting(mountRoot, volume, feature, mounted)
}

// openExisting pins .titanos/<feature> of a mounted volume if it is there
// and private; nothing is created. fs.ErrNotExist without the folder.
func openExisting(mountRoot, volume, feature string, mounted []string) (*os.Root, error) {
	if !naming.ValidVolume(feature) {
		return nil, ErrUnavailable
	}
	root, err := OpenVolume(mountRoot, volume, mounted)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, part := range []string{".titanos", ".titanos/" + feature} {
		st, err := root.Lstat(part)
		if err != nil {
			return nil, err
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok || sys.Uid != uint32(os.Geteuid()) || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 {
			return nil, fmt.Errorf("unsafe private data directory %s", part)
		}
	}
	private, err := root.OpenRoot(".titanos/" + feature)
	if err != nil {
		return nil, err
	}
	if err := SameMount(root, private); err != nil {
		private.Close()
		return nil, err
	}
	return private, nil
}

// adoptPrefix and adoptSuffix name what a move is still copying; such an
// entry is never complete and is removed when the next move begins or the
// folder is first used after a start.
const (
	adoptPrefix = ".adopt-"
	adoptSuffix = ".part"
)

func adoptTemp(name string) string { return adoptPrefix + name + adoptSuffix }

func isAdoptTemp(name string) bool {
	return strings.HasPrefix(name, adoptPrefix) && strings.HasSuffix(name, adoptSuffix)
}

// Adopted is what Adopt did.
type Adopted struct {
	// Moved counts the entries that are now only at the destination.
	Moved int
	// Aside are entries whose name existed at the destination with other
	// content: the destination's entry stays the current one, the moved
	// one lies next to it as "<name>.from-<volume>".
	Aside []string
	// Left are entries that stay at the source: links and special files,
	// folders holding such, and names whose "<name>.from-<volume>" exists.
	Left []string
}

// CleanLeftovers removes what an interrupted move left half copied in
// .titanos/<feature> of a volume. A folder that is not there is no error.
func CleanLeftovers(mountRoot, volume, feature string, mounted []string) error {
	root, err := openExisting(mountRoot, volume, feature, mounted)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	return cleanLeftovers(root)
}

func cleanLeftovers(root *os.Root) error {
	entries, err := readDir(root, ".")
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if isAdoptTemp(entry.Name()) {
			errs = append(errs, root.RemoveAll(entry.Name()))
		}
	}
	return errors.Join(errs...)
}

// syncDir flushes a folder of root, so that the names in it are durable.
func syncDir(root *os.Root, name string) error {
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Adopt moves the entries of .titanos/<feature> on the volume from to the
// same folder on the volume to. The volumes are different file systems, so
// every entry is copied under a temporary name, synced, given its name,
// and only once that name is durable (the folder and the folders above it
// are synced) removed at the source. A power cut at any point loses
// nothing: the entry is then at the source, or at both places with the
// same content, and the next Adopt finishes the move (files and folders
// alike); half copied entries are removed first.
//
// Nothing at the destination is overwritten. An entry whose name exists
// there with other content is put next to it as "<name>.from-<from>": the
// destination's entry stays the one in use, the other is kept and named
// (Aside). Links and special files stay at the source (Left). A source
// folder that is missing or empty is no error; the emptied one is removed.
// ctx ends the move between two entries and inside large files.
func Adopt(ctx context.Context, mountRoot, from, to, feature string, mounted []string) (result Adopted, err error) {
	if from == to {
		return result, nil
	}
	src, err := openExisting(mountRoot, from, feature, mounted)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer src.Close()
	entries, err := readDir(src, ".")
	if err != nil || len(entries) == 0 {
		return result, err
	}
	dst, err := Open(mountRoot, to, feature, mounted)
	if err != nil {
		return result, err
	}
	defer dst.Close()
	if err := cleanLeftovers(dst); err != nil {
		return result, err
	}
	// The folder itself may be new: its name and that of .titanos must be
	// on the disk before anything is removed at the source.
	volume, err := OpenVolume(mountRoot, to, mounted)
	if err != nil {
		return result, err
	}
	for _, dir := range []string{".titanos/" + feature, ".titanos", "."} {
		if err := syncDir(volume, dir); err != nil {
			volume.Close()
			return result, err
		}
	}
	volume.Close()
	// place copies one entry to the destination under name and makes it
	// durable; the source is untouched.
	place := func(entry, name string) (complete bool, err error) {
		tmp := adoptTemp(name)
		if err := dst.RemoveAll(tmp); err != nil {
			return false, err
		}
		complete, err = copyEntry(ctx, src, dst, entry, tmp)
		if err != nil || !complete {
			_ = dst.RemoveAll(tmp) // best effort: removed again when the next move begins
			return false, err
		}
		if err := dst.Rename(tmp, name); err != nil {
			_ = dst.RemoveAll(tmp) // as above
			return false, err
		}
		return true, syncDir(dst, ".")
	}
	for _, entry := range entries {
		name := entry.Name()
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if isAdoptTemp(name) {
			continue
		}
		if !entry.Type().IsRegular() && !entry.IsDir() {
			result.Left = append(result.Left, name)
			continue
		}
		target := name
		if _, err := dst.Lstat(name); err == nil {
			if same, err := sameEntry(ctx, src, dst, name, name); err != nil {
				return result, fmt.Errorf("%s: %w", name, err)
			} else if same {
				// A move that was cut off after the copy got its name: only
				// the source is still to go.
				if err := src.RemoveAll(name); err != nil {
					return result, fmt.Errorf("%s: %w", name, err)
				}
				result.Moved++
				continue
			}
			target = name + ".from-" + from
			if _, err := dst.Lstat(target); err == nil {
				if same, err := sameEntry(ctx, src, dst, name, target); err != nil {
					return result, fmt.Errorf("%s: %w", name, err)
				} else if same {
					if err := src.RemoveAll(name); err != nil {
						return result, fmt.Errorf("%s: %w", name, err)
					}
					result.Aside = append(result.Aside, name)
					continue
				}
				result.Left = append(result.Left, name)
				continue
			} else if !errors.Is(err, fs.ErrNotExist) {
				return result, err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return result, err
		}
		complete, err := place(name, target)
		if err != nil {
			return result, fmt.Errorf("%s: %w", name, err)
		}
		if !complete {
			// Links or special files inside: the folder stays whole at the source.
			result.Left = append(result.Left, name)
			continue
		}
		if err := src.RemoveAll(name); err != nil {
			return result, fmt.Errorf("%s: %w", name, err)
		}
		if target != name {
			result.Aside = append(result.Aside, name)
		} else {
			result.Moved++
		}
	}
	if len(result.Left) == 0 {
		if volume, err := OpenVolume(mountRoot, from, mounted); err == nil {
			// Only an empty folder goes; a failure leaves an empty folder behind.
			_ = volume.Remove(".titanos/" + feature)
			volume.Close()
		}
	}
	return result, nil
}

// sameEntry tells whether a of src and b of dst are the same: two regular
// files with the same bytes, or two folders with the same entries.
func sameEntry(ctx context.Context, src, dst *os.Root, a, b string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	here, err := src.Lstat(a)
	if err != nil {
		return false, err
	}
	there, err := dst.Lstat(b)
	if err != nil {
		return false, err
	}
	switch {
	case here.Mode().IsRegular() && there.Mode().IsRegular():
		return here.Size() == there.Size() && sameContent(ctx, src, dst, a, b), nil
	case here.IsDir() && there.IsDir():
		ours, err := readDir(src, a)
		if err != nil {
			return false, err
		}
		theirs, err := readDir(dst, b)
		if err != nil {
			return false, err
		}
		if len(ours) != len(theirs) {
			return false, nil
		}
		names := map[string]bool{}
		for _, entry := range theirs {
			names[entry.Name()] = true
		}
		for _, entry := range ours {
			if !names[entry.Name()] {
				return false, nil
			}
			if same, err := sameEntry(ctx, src, dst, a+"/"+entry.Name(), b+"/"+entry.Name()); err != nil || !same {
				return false, err
			}
		}
		return true, nil
	}
	return false, nil
}

// sameContent compares two files of the same size byte by byte.
func sameContent(ctx context.Context, src, dst *os.Root, from, to string) bool {
	a, err := src.Open(from)
	if err != nil {
		return false
	}
	defer a.Close()
	b, err := dst.Open(to)
	if err != nil {
		return false
	}
	defer b.Close()
	one, two := make([]byte, 64<<10), make([]byte, 64<<10)
	for ctx.Err() == nil {
		n, errA := io.ReadFull(a, one)
		k, errB := io.ReadFull(b, two)
		if n != k || !bytes.Equal(one[:n], two[:k]) {
			return false
		}
		if errA != nil || errB != nil {
			return errA != nil && errB != nil
		}
	}
	return false
}

func readDir(root *os.Root, name string) ([]fs.DirEntry, error) {
	dir, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

// copyEntry copies a regular file or a folder with everything in it from
// src/name to dst/target, keeping mode and modification time, and syncs
// what it wrote. complete is false if something inside was neither file
// nor folder and was left out.
func copyEntry(ctx context.Context, src, dst *os.Root, name, target string) (complete bool, err error) {
	info, err := src.Lstat(name)
	if err != nil {
		return false, err
	}
	switch {
	case info.Mode().IsRegular():
		in, err := src.Open(name)
		if err != nil {
			return false, err
		}
		defer in.Close()
		out, err := dst.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return false, err
		}
		buf := make([]byte, 1<<20)
		for err == nil {
			if err = ctx.Err(); err != nil {
				break
			}
			n, readErr := in.Read(buf)
			if n > 0 {
				_, err = out.Write(buf[:n])
			}
			if readErr == io.EOF {
				break
			}
			if err == nil {
				err = readErr
			}
		}
		if err == nil {
			err = out.Sync()
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return false, err
		}
	case info.IsDir():
		if err := dst.Mkdir(target, 0700); err != nil {
			return false, err
		}
		entries, err := readDir(src, name)
		if err != nil {
			return false, err
		}
		complete = true
		for _, entry := range entries {
			done, err := copyEntry(ctx, src, dst, name+"/"+entry.Name(), target+"/"+entry.Name())
			if err != nil {
				return false, err
			}
			complete = complete && done
		}
		if err := dst.Chmod(target, info.Mode().Perm()); err != nil {
			return false, err
		}
		if dir, err := dst.Open(target); err == nil {
			err = dir.Sync()
			dir.Close()
			if err != nil {
				return false, err
			}
		}
		return complete, dst.Chtimes(target, info.ModTime(), info.ModTime())
	default:
		return false, nil
	}
	return true, dst.Chtimes(target, info.ModTime(), info.ModTime())
}

// Size adds up the files in .titanos/<feature> of a mounted volume. A
// folder that is not there has size 0. It reads the folder tree, so callers
// ask first whether the disks of the volume are asleep.
func Size(ctx context.Context, mountRoot, volume, feature string, mounted []string) (int64, error) {
	root, err := openExisting(mountRoot, volume, feature, mounted)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer root.Close()
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// Asides lists what a move put next to the current entries of
// .titanos/<feature> on a volume because the same name held other content
// on both volumes ("<name>.from-<volume>", see Adopt). They are kept, not
// used, and wait for someone to look at them. It reads the folder, so
// callers ask first whether the disks of the volume are asleep.
func Asides(mountRoot, volume, feature string, mounted []string) ([]string, error) {
	root, err := openExisting(mountRoot, volume, feature, mounted)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := readDir(root, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if name, from, ok := strings.Cut(entry.Name(), ".from-"); ok && name != "" && naming.ValidVolume(from) {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}
