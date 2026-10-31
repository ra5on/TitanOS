package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// --- installing -------------------------------------------------------------

func (m *Manager) setProgress(change func(*Progress)) {
	m.mu.Lock()
	change(&m.progress)
	m.mu.Unlock()
}

// Install starts installing the release that Check found. It returns at
// once; Status tells how far it is.
func (m *Manager) Install(ctx context.Context) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	manifest, pending, phase, appTrial := m.available, m.pending, m.progress.Phase, m.appTrial
	raw, signature := m.availableRaw, m.availableSig
	m.mu.Unlock()
	switch {
	case phase == "download" || phase == "switch" || phase == "restart" || pending != nil || appTrial:
		return ErrBusy
	case manifest == nil:
		return ErrNothing
	}
	if m.appKind(manifest) {
		return m.installApp(ctx, *manifest, raw, signature)
	}
	if manifest.MinVersion != "" {
		minimum, _ := parseVersion(manifest.MinVersion)
		if running, ok := parseVersion(m.version); ok && running.compare(minimum) < 0 {
			return fmt.Errorf("%w: update to %s first", ErrTooOld, manifest.MinVersion)
		}
	}
	if err := m.unconfirmed(); err != nil {
		return err
	}
	target := other(m.slot)
	staged, err := m.room(target, manifest.Size())
	if err != nil {
		return err
	}
	if !m.work.TryLock() {
		return ErrBusy
	}
	// What was read above may have changed while another action (a step
	// back, a cancelled switch, an app update) held the lock: look again.
	m.mu.Lock()
	again := m.available
	busy := m.pending != nil || m.appTrial || m.progress.Phase == "download" || m.progress.Phase == "switch" || m.progress.Phase == "restart"
	m.mu.Unlock()
	if busy || again == nil || again.Version != manifest.Version {
		m.work.Unlock()
		if again == nil {
			return ErrNothing
		}
		return ErrBusy
	}
	if err := m.unconfirmed(); err != nil {
		m.work.Unlock()
		return err
	}
	m.setProgress(func(p *Progress) { *p = Progress{Phase: "download", FallbackRemoved: !staged} })
	go func() {
		defer m.work.Unlock()
		err := m.install(context.WithoutCancel(ctx), *manifest, target, staged)
		m.forget()
		m.setProgress(func(p *Progress) {
			if err != nil {
				*p = Progress{Phase: "failed", Error: err.Error(), FallbackRemoved: !staged}
			} else {
				*p = Progress{Phase: "done", Percent: 100}
			}
		})
		if err != nil {
			slog.Error("update not installed", "version", manifest.Version, "err", err)
		}
	}()
	return nil
}

// unconfirmed refuses an update while the running system is itself still
// on trial.
func (m *Manager) unconfirmed() error {
	if e, ok := m.readEntries()[m.slot]; ok && e.left != nil {
		return ErrUnconfirmed
	}
	return nil
}

// stagingDir is where a new system is loaded while the fallback slot stays
// intact; on the stick, next to the slots.
func (m *Manager) stagingDir(target string) string {
	return filepath.Join(m.bootDir, "titanos", ".incoming-"+target)
}

// room checks that the stick holds the new system next to the running
// one. staged says whether it also fits next to the fallback: then it is
// loaded beside it and the fallback is only replaced once everything is
// there and checked. Otherwise the fallback has to go first (slots are
// large, sticks often small).
func (m *Manager) room(target string, size int64) (bool, error) {
	free, err := m.stickFree()
	if err != nil {
		return false, err
	}
	need := size + (8 << 20)
	free += dirSize(m.stagingDir(target)) // left over from a broken attempt
	if free >= need {
		return true, nil
	}
	for _, name := range slotFiles {
		if info, err := os.Stat(filepath.Join(m.bootDir, "titanos", target, name)); err == nil {
			free += info.Size() // goes away first
		}
	}
	free += m.stickFor(target).Size()
	if free < need {
		return false, fmt.Errorf("%w: %d MiB needed, %d MiB free", ErrNoSpace, size>>20, free>>20)
	}
	return false, nil
}

func (m *Manager) stickFree() (int64, error) {
	if m.freeSpace != nil {
		return m.freeSpace()
	}
	var room unix.Statfs_t
	if err := unix.Statfs(m.bootDir, &room); err != nil {
		return 0, err
	}
	return int64(room.Bavail) * int64(room.Bsize), nil
}

func dirSize(dir string) int64 {
	var total int64
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}

func (m *Manager) install(ctx context.Context, manifest Manifest, target string, staged bool) error {
	slog.Info("installing update", "version", manifest.Version, "slot", target)
	base := m.source() + "/releases/download/" + manifest.Tag + "/"
	// Plugins belong to the system image: a release that is an app on an
	// older image brings that image's plugins.
	release := manifest.Version
	if manifest.App != nil && manifest.App.Base != "" {
		release = manifest.App.Base
	}
	if m.PreparePlugins != nil {
		assets := map[string]string{}
		for _, plugin := range manifest.Plugins {
			assets[plugin.Name] = plugin.Asset
		}
		if err := m.PreparePlugins(ctx, release, manifest.Kernel, manifest.KernelPackage, base, assets, m.fetch,
			func(ctx context.Context, url, dest, sum string, size int64) error {
				return m.download(ctx, url, dest, File{SHA256: sum, Size: size}, func(done int64, speed uint64) {
					m.setProgress(func(p *Progress) { p.File = filepath.Base(dest); p.SpeedBytesPerSecond = speed })
				})
			}); err != nil {
			return fmt.Errorf("prepare plugins: %w", err)
		}
	}
	dir := filepath.Join(m.bootDir, "titanos", target)
	stage := m.stagingDir(target)
	os.RemoveAll(stage)
	load := dir
	if staged {
		load = stage
	} else {
		// No room for three systems: the fallback goes before the download.
		slog.Warn("the stick has no room for the new system next to the fallback; the fallback slot is emptied first", "slot", target)
		m.clearSlot(target)
		if err := m.stickFor(target).Clear(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(load, 0o755); err != nil {
		return err
	}
	total, done := manifest.Size(), int64(0)
	for _, file := range manifest.Files {
		before := done
		err := m.download(ctx, base+file.Asset, filepath.Join(load, file.Name), file, func(n int64, speed uint64) {
			m.setProgress(func(p *Progress) {
				p.File, p.Percent, p.SpeedBytesPerSecond = file.Name, 100*float64(before+n)/float64(total), speed
			})
		})
		if err != nil {
			if staged {
				os.RemoveAll(stage) // the fallback is untouched
			}
			return fmt.Errorf("%s: %w", file.Name, err)
		}
		done += file.Size
	}
	if staged {
		// Everything is there and checked: only now replace the fallback.
		m.clearSlot(target)
		if err := m.stickFor(target).Clear(); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, file := range manifest.Files {
			if err := os.Rename(filepath.Join(stage, file.Name), filepath.Join(dir, file.Name)); err != nil {
				return err
			}
		}
		if err := syncDir(dir); err != nil {
			return err
		}
		os.RemoveAll(stage)
	}
	m.setProgress(func(p *Progress) { *p = Progress{Phase: "switch", Percent: 100, FallbackRemoved: !staged} })
	if err := writeFile(filepath.Join(dir, "version"), []byte(manifest.Version+"\n")); err != nil {
		return err
	}
	content := fmt.Sprintf("title    TitanOS\nversion  %s\nsort-key %s\nlinux    /titanos/%s/vmlinuz\ninitrd   /titanos/%s/initrd.img\noptions  %s\n",
		manifest.Version, keyPreferred, target, target, strings.ReplaceAll(manifest.Cmdline, "@SLOT@", target))
	content = withParams(content, m.params())
	return m.switchTo(target, content, Pending{Kind: "update", Version: manifest.Version})
}

// switchTo makes a slot the one that starts next, on trial, with the
// running one as the fallback.
func (m *Manager) switchTo(target, content string, pending Pending) error {
	if err := m.unpinLoader(); err != nil {
		return err
	}
	// The fallback first: should the power fail in between, the stick
	// still starts what is running now.
	if running, ok := m.readEntries()[m.slot]; ok {
		if err := m.putEntry(m.slot, withSortKey(running.content, keyFallback), false); err != nil {
			return err
		}
	}
	pending.Slot, pending.From, pending.Since, pending.BootID = target, m.version, m.now(), m.bootID
	if m.Mounted != nil {
		pending.Volumes = m.Mounted()
		if m.Encrypted != nil && !knowsEncryption(pending.Version) {
			encrypted := m.Encrypted()
			pending.Volumes = slices.DeleteFunc(pending.Volumes, func(name string) bool { return slices.Contains(encrypted, name) })
		}
	}
	if err := m.store.WriteJSON(pendingFile, pending); err != nil {
		return err
	}
	if err := m.putEntry(target, withSortKey(content, keyPreferred), true); err != nil {
		_ = m.store.Remove(pendingFile) // cleanup, best effort: nothing more can be done if it fails
		return err
	}
	// Everything has to be on the stick before someone restarts.
	unix.Sync()
	m.mu.Lock()
	m.pending, m.available, m.slotCache = &pending, nil, nil
	m.mu.Unlock()
	slog.Info("next start switches the system", "kind", pending.Kind, "version", pending.Version, "slot", target)
	return nil
}

// Rollback makes the other slot the one that starts next. Like an update
// it is on trial: if that system does not come up, this one starts again.
func (m *Manager) Rollback() error {
	if m.Disabled {
		return ErrDisabled
	}
	if !m.work.TryLock() {
		return ErrBusy
	}
	defer m.work.Unlock()
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()
	if pending != nil {
		return ErrBusy
	}
	target := other(m.slot)
	entries := m.readEntries()
	e, ok := entries[target]
	if !ok || !m.slotComplete(target) || e.left != nil && *e.left == 0 {
		return ErrNothing
	}
	if running, ok := entries[m.slot]; ok && running.left != nil {
		return ErrUnconfirmed
	}
	return m.switchTo(target, withParams(e.content, m.params()), Pending{Kind: "rollback", Version: m.slotVersion(target)})
}

// Cancel takes back a pending switch: the running system stays the one
// that starts next.
func (m *Manager) Cancel() error {
	if m.Disabled {
		return ErrDisabled
	}
	if !m.work.TryLock() {
		return ErrBusy
	}
	defer m.work.Unlock()
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()
	if pending == nil || pending.BootID != m.bootID {
		return ErrNothing
	}
	entries := m.readEntries()
	if running, ok := entries[m.slot]; ok {
		if err := m.putEntry(m.slot, withSortKey(running.content, keyPreferred), false); err != nil {
			return err
		}
	}
	if target, ok := entries[pending.Slot]; ok {
		if err := m.putEntry(pending.Slot, withSortKey(target.content, keyFallback), false); err != nil {
			return err
		}
	}
	if err := m.store.Remove(pendingFile); err != nil {
		return err
	}
	unix.Sync()
	m.mu.Lock()
	m.pending, m.progress, m.slotCache = nil, Progress{}, nil
	m.mu.Unlock()
	return nil
}

// download fetches one file of a release into the slot, checking size and
// checksum. A download that broke off is taken up where it stopped.
func (m *Manager) download(ctx context.Context, url, dest string, file File, progress func(done int64, speed uint64)) error {
	part := dest + ".part"
	var last error
	wrong := 0
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * m.retryPause):
			}
		}
		done, err := m.downloadOnce(ctx, url, part, file, progress)
		if err == nil {
			return os.Rename(part, dest)
		}
		last = err
		slog.Warn("download interrupted", "file", file.Name, "have", done, "err", err)
		if errors.Is(err, errChecksum) {
			// What is there is wrong. Once more from the start, in case
			// it was damaged on the way; twice wrong is the release.
			os.Remove(part)
			if wrong++; wrong == 2 {
				break
			}
		}
	}
	os.Remove(part)
	return last
}

var errChecksum = errors.New("the checksum does not match the manifest")

func (m *Manager) downloadOnce(ctx context.Context, url, part string, file File, progress func(done int64, speed uint64)) (int64, error) {
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	have := info.Size()
	if have > file.Size {
		return have, errChecksum
	}
	// What is there already counts towards the checksum.
	hash := sha256.New()
	if _, err := io.CopyN(hash, f, have); err != nil {
		return have, err
	}
	// A download that ended just before it was renamed is complete: asking
	// for more would get "range not satisfiable".
	if have == file.Size && have > 0 {
		if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
			return have, errChecksum
		}
		progress(have, 0)
		return have, f.Sync()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A connection that stands still is given up on.
	watchdog := time.AfterFunc(m.stall, cancel)
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return have, err
	}
	req.Header.Set("User-Agent", "TitanOS/"+m.version)
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	res, err := m.client.Do(req)
	if err != nil {
		return have, err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusOK && have > 0:
		// The server sends everything again.
		if err := f.Truncate(0); err != nil {
			return have, err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return have, err
		}
		have = 0
		hash.Reset()
	case res.StatusCode == http.StatusOK, res.StatusCode == http.StatusPartialContent && have > 0:
	default:
		return have, fmt.Errorf("%s: %s", url, res.Status)
	}

	buffer := make([]byte, 1<<20)
	mark, markDone := time.Now(), have
	for {
		n, readErr := res.Body.Read(buffer)
		if n > 0 {
			watchdog.Reset(m.stall)
			if have+int64(n) > file.Size {
				return have + int64(n), errChecksum // longer than the manifest says
			}
			if _, err := f.Write(buffer[:n]); err != nil {
				return have, err
			}
			hash.Write(buffer[:n])
			have += int64(n)
			if now := time.Now(); now.Sub(mark) >= time.Second {
				progress(have, uint64(float64(have-markDone)/now.Sub(mark).Seconds()))
				mark, markDone = now, have
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return have, readErr
		}
	}
	if have < file.Size {
		return have, io.ErrUnexpectedEOF
	}
	if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return have, errChecksum
	}
	progress(have, 0)
	return have, f.Sync()
}
