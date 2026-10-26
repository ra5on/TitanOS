package update

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type baseOutput struct{ bytes.Buffer }

func (b *baseOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256 {
		return 0, fmt.Errorf("invalid system base version")
	}
	return b.Buffer.Write(p)
}

// PluginReleaseProtection runs only for an explicit cleanup preview/action.
// Slot titles can name an app release: read the real basis from each complete
// squashfs, including old/repacked slots without additional metadata. Failure
// blocks cleanup rather than guessing which fallback plugins may be deleted.
func (m *Manager) PluginReleaseProtection() ([]string, error) {
	if !m.work.TryLock() {
		return nil, fmt.Errorf("an update is changing boot references")
	}
	defer m.work.Unlock()
	status := m.Status()
	versions := []string{status.Version, status.Base, status.App.Current, status.App.Previous}
	if status.Pending != nil {
		versions = append(versions, status.Pending.Version, status.Pending.From)
	}
	if m.Disabled {
		return versions, nil
	}
	for _, slot := range m.slots(true) {
		if !slot.Complete {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "unsquashfs", "-cat", "-no-progress", filepath.Join(m.bootDir, "titanos", slot.Name, "filesystem.squashfs"), "usr/share/titanos/base-version")
		var out baseOutput
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		err := cmd.Run()
		cancel()
		base := strings.TrimSpace(out.String())
		_, valid := parseVersion(base)
		if err != nil || !valid {
			return nil, fmt.Errorf("cannot verify system base of boot slot %s; plugin cleanup remains blocked", slot.Name)
		}
		versions = append(versions, slot.Version, base)
	}
	return versions, nil
}
