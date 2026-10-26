package storage

import (
	"context"
	"regexp"
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

// These commands read kernel progress/status records, never file contents.
func (m *Manager) refreshBtrfs(ctx context.Context) {
	if m.ReadOnly || !m.actions.TryLock() {
		return
	}
	defer m.actions.Unlock()
	states := map[string]btrfsProgress{}
	for _, volume := range m.configured() {
		if volume.Filesystem != "btrfs" || !m.readMounts()[m.MountPoint(volume.Name)] {
			continue
		}
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		scrub, scrubErr := m.runner.Run(probe, "", "btrfs", "scrub", "status", "-R", m.MountPoint(volume.Name))
		replace, replaceErr := m.runner.Run(probe, "", "btrfs", "replace", "status", "-1", m.MountPoint(volume.Name))
		show, showErr := m.runner.Run(probe, "", "btrfs", "filesystem", "show", "--raw", m.MountPoint(volume.Name))
		cancel()
		state := parseBtrfsProgress(scrub, replace)
		m.mu.RLock()
		previous := m.btrfsProgress[volume.Name]
		m.mu.RUnlock()
		state.Missing = btrfsMissing(show)
		if showErr != nil {
			state.Missing = previous.Missing // unknown: keep what was seen
		}
		// An observation failure must not let the standby worker treat a
		// previously busy mirror as idle.
		if state.State == "" && (scrubErr != nil && previous.State == VolumeChecking || replaceErr != nil && previous.State == VolumeRebuilding) {
			state = previous
		}
		states[volume.Name] = state
	}
	m.mu.Lock()
	m.btrfsProgress = states
	m.mu.Unlock()
}
