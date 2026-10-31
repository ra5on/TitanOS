package update

import (
	"errors"
	"log/slog"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

// Kernel parameters that a setting of the NAS asks for (such as
// i915.enable_guc for Intel graphics) live in the "options" line of the boot
// entries on the stick: whatever loads a driver, the initramfs or udev in the
// system, reads them from the kernel command line. The entries are written
// only when a parameter changes, and every entry written later (a full
// update, a rollback) carries them too, so they survive A/B switches.

var (
	// ErrParam is a parameter name or value TitanOS does not write.
	ErrParam    = errors.New("update: invalid kernel parameter")
	paramName   = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+$`)
	paramValue  = regexp.MustCompile(`^[A-Za-z0-9_,.:-]{0,64}$`)
	optionsLine = regexp.MustCompile(`(?m)^options[ \t]+(.*?)[ \t]*$`)
)

// ParamState tells what the boot entries hold for a kernel parameter.
type ParamState struct {
	// Entries holds the value per slot: "" for an entry without the
	// parameter. Slots without an entry are missing.
	Entries map[string]string `json:"entries"`
	// Deferred is set while the entry of the running system could not be
	// written yet: it is on trial and the boot check renames it.
	Deferred bool `json:"deferred"`
}

// editOptions returns a kernel command line with every form of name taken
// out and, unless value is empty, name=value appended. All other
// parameters stay as they are, in their order.
func editOptions(options, name, value string) string {
	var kept []string
	for _, field := range strings.Fields(options) {
		key, _, _ := strings.Cut(field, "=")
		if key == name {
			continue
		}
		kept = append(kept, field)
	}
	if value != "" {
		kept = append(kept, name+"="+value)
	}
	return strings.Join(kept, " ")
}

// optionValue returns the value of name on a kernel command line: the last
// one counts, as with the kernel. ok is false if it is not there.
func optionValue(options, name string) (value string, ok bool) {
	for _, field := range strings.Fields(options) {
		key, v, _ := strings.Cut(field, "=")
		if key == name {
			value, ok = v, true
		}
	}
	return value, ok
}

// entryOptions joins the options lines of a boot entry; systemd-boot does
// the same with several of them.
func entryOptions(content string) string {
	var all []string
	for _, match := range optionsLine.FindAllStringSubmatch(content, -1) {
		all = append(all, match[1])
	}
	return strings.Join(all, " ")
}

// withParam sets name=value (or takes it out for "") in a boot entry. The
// parameter goes on the last options line and is removed from the others;
// an entry without options gets a line of its own.
func withParam(content, name, value string) string {
	matches := optionsLine.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		if value == "" {
			return content
		}
		return strings.TrimRight(content, "\n") + "\noptions  " + name + "=" + value + "\n"
	}
	var b strings.Builder
	last := 0
	for i, match := range matches {
		b.WriteString(content[last:match[2]])
		options := content[match[2]:match[3]]
		if i == len(matches)-1 {
			b.WriteString(editOptions(options, name, value))
		} else {
			b.WriteString(editOptions(options, name, ""))
		}
		last = match[3]
	}
	b.WriteString(content[last:])
	return b.String()
}

// withParams applies the parameters a setting asks for to a boot entry.
func withParams(content string, params map[string]string) string {
	for _, name := range slices.Sorted(maps.Keys(params)) {
		content = withParam(content, name, params[name])
	}
	return content
}

// params returns a copy of the parameters TitanOS manages.
func (m *Manager) params() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.kernelParams)
}

// SetKernelParam makes name=value part of every boot entry, now and for
// entries written later; an empty value takes it out. An entry is only
// written if it changes. The entry of the running system is left alone
// while it is on trial (Deferred): call again once it is confirmed.
func (m *Manager) SetKernelParam(name, value string) (ParamState, error) {
	if !paramName.MatchString(name) || !paramValue.MatchString(value) {
		return ParamState{}, ErrParam
	}
	m.mu.Lock()
	if m.kernelParams == nil {
		m.kernelParams = map[string]string{}
	}
	m.kernelParams[name] = value
	m.mu.Unlock()
	if m.Disabled {
		return ParamState{}, ErrDisabled
	}
	// An update writing the stick takes minutes; its entry gets the
	// parameter from kernelParams, the others are written once it is done.
	if !m.work.TryLock() {
		return ParamState{}, ErrBusy
	}
	defer m.work.Unlock()
	state := ParamState{Entries: map[string]string{}}
	changed := false
	for slot, e := range m.readEntries() {
		if slot == m.slot && e.left != nil {
			// titand -check-boot renames this file once the system is
			// good; writing it now could leave two entries for the slot.
			current, _ := optionValue(entryOptions(e.content), name)
			state.Entries[slot], state.Deferred = current, current != value
			continue
		}
		next := withParam(e.content, name, value)
		state.Entries[slot] = value
		if next == e.content {
			continue
		}
		if err := writeFile(e.path, []byte(next)); err != nil {
			return state, err
		}
		m.writeBootEnv(slot, next)
		changed = true
	}
	if changed {
		_ = syncDir(filepath.Join(m.bootDir, "loader/entries")) // the data are written; the sync only hurries them to disk
		unix.Sync()
		m.forget()
		slog.Info("kernel parameter written to the boot entries", "name", name, "value", value)
	}
	return state, nil
}

// KernelParam tells what the boot entries on the stick hold for name. It
// reads the stick; call it on request, not in a loop.
func (m *Manager) KernelParam(name string) ParamState {
	state := ParamState{Entries: map[string]string{}}
	if m.Disabled {
		return state
	}
	want, managed := m.params()[name]
	for slot, e := range m.readEntries() {
		value, _ := optionValue(entryOptions(e.content), name)
		state.Entries[slot] = value
		if managed && slot == m.slot && e.left != nil && value != want {
			state.Deferred = true
		}
	}
	return state
}
