package update

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// --- the stick -------------------------------------------------------------

func other(slot string) string {
	if slot == "a" {
		return "b"
	}
	return "a"
}

var slotFiles = []string{"vmlinuz", "initrd.img", "filesystem.squashfs"}

// entry is the boot entry of a slot as it lies on the stick.
type entry struct {
	path    string
	content string
	// left is the number of tries the loader still gives it; nil for an
	// entry that is marked good.
	left *int
}

var entryName = regexp.MustCompile(`^titanos-([ab])(?:\+([0-9]+)(?:-[0-9]+)?)?\.conf$`)

// readEntries returns the boot entries by slot.
func (m *Manager) readEntries() map[string]entry {
	entries := map[string]entry{}
	dir := filepath.Join(m.bootDir, "loader/entries")
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		match := entryName.FindStringSubmatch(file.Name())
		if match == nil {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			continue
		}
		e := entry{path: filepath.Join(dir, file.Name()), content: string(content)}
		if match[2] != "" {
			var left int
			_, _ = fmt.Sscan(match[2], &left) // a value that cannot be read leaves the zero value, which the code handles
			e.left = &left
		}
		entries[match[1]] = e
	}
	return entries
}

var sortKeyLine = regexp.MustCompile(`(?m)^sort-key[ \t]+(\S+)[ \t]*$`)

func sortKey(content string) string {
	if match := sortKeyLine.FindStringSubmatch(content); match != nil {
		return match[1]
	}
	return ""
}

func withSortKey(content, key string) string {
	if sortKeyLine.MatchString(content) {
		return sortKeyLine.ReplaceAllString(content, "sort-key "+key)
	}
	return strings.TrimRight(content, "\n") + "\nsort-key " + key + "\n"
}

// nextSlot tells which slot the boot loader starts next: the first entry
// in its order that has tries left.
func nextSlot(entries map[string]entry) string {
	best := ""
	for slot, e := range entries {
		if e.left != nil && *e.left == 0 {
			continue // given up on
		}
		if best == "" || sortKey(e.content) < sortKey(entries[best].content) || sortKey(e.content) == sortKey(entries[best].content) && slot < best {
			best = slot
		}
	}
	return best
}

// writeFile replaces a file on the stick: written next to it, flushed,
// then renamed over it.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// putEntry writes the boot entry of a slot, with a counter if it has to
// prove itself, and removes the other forms of its name.
func (m *Manager) putEntry(slot, content string, counted bool) error {
	dir := filepath.Join(m.bootDir, "loader/entries")
	name := "titanos-" + slot + ".conf"
	if counted {
		name = fmt.Sprintf("titanos-%s+%d.conf", slot, tries)
	}
	if err := writeFile(filepath.Join(dir, name), []byte(content)); err != nil {
		return err
	}
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		if match := entryName.FindStringSubmatch(file.Name()); match != nil && match[1] == slot && file.Name() != name {
			os.Remove(filepath.Join(dir, file.Name()))
		}
	}
	return syncDir(dir)
}

// clearSlot makes a slot unbootable and removes its system files.
func (m *Manager) clearSlot(slot string) {
	// What is in the slot can no longer be started while it is replaced.
	m.removeEntry(slot)
	dir := filepath.Join(m.bootDir, "titanos", slot)
	for _, name := range append(slices.Clone(slotFiles), "version") {
		os.Remove(filepath.Join(dir, name))
	}
}

func (m *Manager) removeEntry(slot string) {
	dir := filepath.Join(m.bootDir, "loader/entries")
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		if match := entryName.FindStringSubmatch(file.Name()); match != nil && match[1] == slot {
			os.Remove(filepath.Join(dir, file.Name()))
		}
	}
	_ = syncDir(dir) // the data are written; the sync only hurries them to disk
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	// FAT has no directory entries of its own to flush; an error here is
	// no reason to fail.
	_ = d.Sync() // the data are written; the sync only hurries them to disk
	return nil
}

// unpinLoader removes a fixed default from loader.conf: with one, the
// loader would start that entry even after giving up on it.
func (m *Manager) unpinLoader() error {
	path := filepath.Join(m.bootDir, "loader/loader.conf")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // no file, no default
	}
	var kept []string
	changed := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "default") {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	if !changed {
		return nil
	}
	return writeFile(path, []byte(strings.Join(kept, "\n")))
}

func (m *Manager) slotVersion(slot string) string {
	data, err := os.ReadFile(filepath.Join(m.bootDir, "titanos", slot, "version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *Manager) slotComplete(slot string) bool {
	for _, name := range slotFiles {
		if info, err := os.Stat(filepath.Join(m.bootDir, "titanos", slot, name)); err != nil || info.Size() == 0 {
			return false
		}
	}
	return true
}

// slots returns what lies in the two slots. The stick sleeps when nobody
// uses it, and a page that asks every few seconds must not keep it awake:
// what was read is kept for a while, unless something is going on.
func (m *Manager) slots(fresh bool) []Slot {
	m.mu.Lock()
	cached, age := m.slotCache, m.now().Sub(m.slotTime)
	m.mu.Unlock()
	if cached != nil && !fresh && age < 10*time.Minute {
		return cached
	}
	entries := m.readEntries()
	next := nextSlot(entries)
	var list []Slot
	for _, name := range []string{"a", "b"} {
		e, hasEntry := entries[name]
		slot := Slot{Name: name, Version: m.slotVersion(name), Running: name == m.slot, Complete: hasEntry && m.slotComplete(name), Next: name == next, TriesLeft: e.left}
		if slot.Running && slot.Version == "" {
			slot.Version = m.version
		}
		list = append(list, slot)
	}
	m.mu.Lock()
	m.slotCache, m.slotTime = list, m.now()
	m.mu.Unlock()
	return list
}

// forget drops what is remembered about the slots, after they changed.
func (m *Manager) forget() {
	m.mu.Lock()
	m.slotCache = nil
	m.mu.Unlock()
}
