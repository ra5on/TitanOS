package update

// Boards without 64-bit UEFI start the stick with GRUB (legacy BIOS:
// i386-pc behind the MBR; 32-bit UEFI: EFI/BOOT/BOOTIA32.EFI). GRUB reads
// the same entries in loader/entries as systemd-boot, but it cannot rename
// files on FAT: it counts the tries of an entry on trial in
// boot/grub/grubenv, a 1024-byte block it overwrites in place
// (boot/grub.cfg.in on the stick). titand keeps both in step:
//
//   - Started by GRUB, titand turns GRUB's count into the file name, just
//     as systemd-boot renames titanos-b+3.conf to titanos-b+2-1.conf, and
//     clears the count. A trial GRUB gave up on becomes titanos-<slot>+0-3.
//   - A new trial clears an old count; titanos_prefer names the good slot
//     with the first sort-key, the one GRUB starts without a trial.
//   - The kernel command line of a slot lies in titanos/<slot>/bootenv, so
//     that kernel parameters and a changed command line of a release reach
//     GRUB, whose own files updates never replace.
//
// Sticks without boot/grub/grubenv (made before GRUB was added) are left as
// they are. Versions before this one do not know any of it: GRUB still
// counts and falls back on its own (docs/BOOT.md).

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	grubEnvFile  = "boot/grub/grubenv"
	bootEnvName  = "bootenv"
	grubEnvBlock = 1024
	grubEnvHead  = "# GRUB Environment Block\n"

	envTrial  = "titanos_trial"
	envCount  = "titanos_count"
	envPrefer = "titanos_prefer"
	envOpts   = "titanos_options"
)

// readGrubEnv reads a GRUB environment block. Values with escapes are not
// written by TitanOS and are kept as they are.
func readGrubEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, []byte(grubEnvHead)) {
		return nil, fmt.Errorf("%s is not a GRUB environment block", path)
	}
	env := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		if name, value, ok := strings.Cut(line, "="); ok {
			env[name] = value
		}
	}
	return env, nil
}

var grubEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// grubEnv encodes values as a GRUB environment block of 1024 bytes (or
// the next multiple), padded with '#' like grub-editenv does.
func grubEnv(env map[string]string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(grubEnvHead)
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		value := env[name]
		if !grubEnvName.MatchString(name) || strings.ContainsAny(value, "\n\\") {
			return nil, fmt.Errorf("%s cannot be stored for GRUB", name)
		}
		fmt.Fprintf(&b, "%s=%s\n", name, value)
	}
	size := max(grubEnvBlock, (b.Len()+grubEnvBlock-1)/grubEnvBlock*grubEnvBlock)
	b.Write(bytes.Repeat([]byte("#"), size-b.Len()))
	return b.Bytes(), nil
}

func (m *Manager) hasGrub() bool {
	_, err := os.Stat(filepath.Join(m.bootDir, grubEnvFile))
	return err == nil
}

// BootLoader names what started this system: "systemd-boot", or for GRUB
// "grub-i386-pc" (legacy BIOS) and "grub-i386-efi" (32-bit UEFI).
func (m *Manager) BootLoader() string {
	if m.cmdline == nil {
		return "systemd-boot"
	}
	for arg := range strings.FieldsSeq(m.cmdline()) {
		if loader, ok := strings.CutPrefix(arg, "titanos.loader="); ok && strings.HasPrefix(loader, "grub-") {
			return loader
		}
	}
	return "systemd-boot"
}

var countedName = regexp.MustCompile(`^titanos-([ab])\+([0-9]+)(?:-([0-9]+))?\.conf$`)

// syncGrub brings boot/grub/grubenv in step with the entries after they
// changed. newTrial clears what GRUB counted for an earlier trial: a new
// one gets all its tries.
func (m *Manager) syncGrub(newTrial bool) {
	path := filepath.Join(m.bootDir, grubEnvFile)
	env, err := readGrubEnv(path)
	if err != nil {
		return // no GRUB on this stick
	}
	next := make(map[string]string, len(env))
	for name, value := range env {
		next[name] = value
	}
	if newTrial {
		next[envTrial], next[envCount] = "", "0"
	}
	if prefer := preferredGood(m.readEntries()); prefer != "" {
		next[envPrefer] = prefer
	}
	m.writeGrubEnv(path, env, next)
}

func (m *Manager) writeGrubEnv(path string, old, next map[string]string) {
	same := len(old) == len(next)
	for name, value := range next {
		if v, ok := old[name]; !ok || v != value {
			same = false
		}
	}
	if same {
		return
	}
	data, err := grubEnv(next)
	if err == nil {
		err = writeFile(path, data)
	}
	if err != nil {
		slog.Warn("GRUB state on the stick not updated", "err", err)
	}
}

// preferredGood is the slot GRUB starts when nothing is on trial: of the
// entries marked good, the one with the first sort-key.
func preferredGood(entries map[string]entry) string {
	best := ""
	for _, slot := range []string{"a", "b"} {
		e, ok := entries[slot]
		if !ok || e.left != nil {
			continue
		}
		if best == "" || sortKey(e.content) < sortKey(entries[best].content) {
			best = slot
		}
	}
	return best
}

// ReconcileBootLoader runs once after the start. If GRUB started this
// system, the tries GRUB counted become part of the entry names, as
// systemd-boot would have named them, and the count is cleared. Nothing
// happens on sticks without GRUB or after a start by systemd-boot.
func (m *Manager) ReconcileBootLoader() {
	if m.Disabled || !strings.HasPrefix(m.BootLoader(), "grub-") {
		return
	}
	path := filepath.Join(m.bootDir, grubEnvFile)
	env, err := readGrubEnv(path)
	if err != nil {
		return
	}
	trial := env[envTrial]
	count, _ := strconv.Atoi(env[envCount])
	match := countedName.FindStringSubmatch(trial)
	next := make(map[string]string, len(env))
	for name, value := range env {
		next[name] = value
	}
	if match != nil && count > 0 {
		dir := filepath.Join(m.bootDir, "loader/entries")
		if _, err := os.Stat(filepath.Join(dir, trial)); err == nil {
			left, _ := strconv.Atoi(match[2])
			done, _ := strconv.Atoi(match[3])
			used := min(count, left)
			name := fmt.Sprintf("titanos-%s+%d-%d.conf", match[1], left-used, done+used)
			if err := os.Rename(filepath.Join(dir, trial), filepath.Join(dir, name)); err != nil {
				slog.Warn("GRUB tries not written to the boot entry", "err", err)
				return
			}
			syncDir(dir)
			m.forget()
			slog.Info("GRUB tries written to the boot entry", "from", trial, "to", name)
		}
	}
	next[envTrial], next[envCount] = "", "0"
	if prefer := preferredGood(m.readEntries()); prefer != "" {
		next[envPrefer] = prefer
	}
	m.writeGrubEnv(path, env, next)
}

// writeBootEnv puts the kernel command line of an entry where GRUB reads
// it. Only on sticks with GRUB.
func (m *Manager) writeBootEnv(slot, content string) {
	if !m.hasGrub() {
		return
	}
	path := filepath.Join(m.bootDir, "titanos", slot, bootEnvName)
	options := entryOptions(content)
	data, err := grubEnv(map[string]string{envOpts: options})
	if err == nil && options == "" {
		err = fmt.Errorf("entry without options")
	}
	if err != nil {
		// GRUB then uses the command line it was made with.
		os.Remove(path)
		slog.Warn("kernel command line not written for GRUB", "slot", slot, "err", err)
		return
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return
	}
	if err := writeFile(path, data); err != nil {
		slog.Warn("kernel command line not written for GRUB", "slot", slot, "err", err)
	}
}
