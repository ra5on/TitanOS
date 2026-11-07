package storage

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type btrfsProgress struct {
	State  string
	Sync   *Sync
	Errors uint64
	// Missing is the number of devices the mounted filesystem misses.
	Missing int
}

var btrfsPercent = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)% done`)
var btrfsRunning = regexp.MustCompile(`(?mi)^status:\s*running`)
var btrfsErrors = regexp.MustCompile(`uncorrectable_errors:\s*(\d+)`)

func parseBtrfsProgress(scrub, replace string) btrfsProgress {
	var result btrfsProgress
	if match := btrfsErrors.FindStringSubmatch(scrub); match != nil {
		result.Errors, _ = strconv.ParseUint(match[1], 10, 64)
	}
	if btrfsRunning.MatchString(scrub) || strings.Contains(scrub, "running for") {
		result.State = VolumeChecking
		result.Sync = &Sync{RemainingSeconds: -1}
	}
	if match := btrfsPercent.FindStringSubmatch(replace); match != nil {
		percent, _ := strconv.ParseFloat(match[1], 64)
		result.State = VolumeRebuilding
		result.Sync = &Sync{Percent: min(100, percent), RemainingSeconds: -1}
	}
	return result
}

// How often refreshBtrfs asks btrfs: often while something is going on
// (scrub, replace, balance, a missing device), else rarely. Four commands
// per volume every five seconds cost more than an idle NAS should spend
// (owner decision 7.11.2026).
const (
	btrfsBusyInterval = 5 * time.Second
	btrfsIdleInterval = 30 * time.Second
)

// btrfsDue reports whether refreshBtrfs should ask btrfs now: something
// runs or is missing on a volume, a Btrfs volume was mounted since the
// last look, or the idle interval has passed. Called with actions held.
func (m *Manager) btrfsDue(mounted []string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.btrfsProbed.IsZero() || m.now().Sub(m.btrfsProbed) >= btrfsIdleInterval {
		return true
	}
	if len(mounted) != len(m.btrfsProgress) {
		return true
	}
	for _, name := range mounted {
		state, ok := m.btrfsProgress[name]
		if !ok || state.State != "" || state.Missing > 0 {
			return true
		}
	}
	return false
}

// These commands read kernel progress/status records, never file contents.
// The caller (Run) comes every btrfsBusyInterval; btrfsDue decides.
func (m *Manager) refreshBtrfs(ctx context.Context) {
	if m.ReadOnly || !m.actions.TryLock() {
		return
	}
	defer m.actions.Unlock()
	mounts := m.readMounts() // once per round, not once per volume
	var mounted []string
	volumes := m.configured()
	for _, volume := range volumes {
		if volume.Filesystem == "btrfs" && mounts[m.MountPoint(volume.Name)] {
			mounted = append(mounted, volume.Name)
		}
	}
	if !m.btrfsDue(mounted) {
		return
	}
	states := map[string]btrfsProgress{}
	for _, volume := range volumes {
		if !slices.Contains(mounted, volume.Name) {
			continue
		}
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		scrub, scrubErr := m.runner.Run(probe, "", "btrfs", "scrub", "status", "-R", m.MountPoint(volume.Name))
		replace, replaceErr := m.runner.Run(probe, "", "btrfs", "replace", "status", "-1", m.MountPoint(volume.Name))
		show, showErr := m.runner.Run(probe, "", "btrfs", "filesystem", "show", "--raw", m.MountPoint(volume.Name))
		balance, balanceErr := m.runner.Run(probe, "", "btrfs", "balance", "status", m.MountPoint(volume.Name))
		cancel()
		state := parseBtrfsProgress(scrub, replace)
		if running, percent := parseBtrfsBalance(balance); running {
			state.State, state.Sync = VolumeReshaping, &Sync{Percent: percent, RemainingSeconds: -1}
		}
		m.mu.RLock()
		previous := m.btrfsProgress[volume.Name]
		m.mu.RUnlock()
		state.Missing = btrfsMissing(show)
		if showErr != nil {
			state.Missing = previous.Missing // unknown: keep what was seen
		}
		// An observation failure must not let the standby worker treat a
		// previously busy mirror as idle.
		if state.State == "" && (scrubErr != nil && previous.State == VolumeChecking || replaceErr != nil && previous.State == VolumeRebuilding || balanceErr != nil && previous.State == VolumeReshaping) {
			state = previous
		}
		states[volume.Name] = state
	}
	m.mu.Lock()
	m.btrfsProgress = states
	m.btrfsProbed = m.now()
	m.mu.Unlock()
}
