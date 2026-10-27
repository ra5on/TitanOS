package storage

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/quota"
)

// States of share quotas on a volume.
const (
	QuotasUnsupported = "unsupported" // Btrfs: limits exist only per subvolume
	QuotasOff         = "off"
	QuotasPending     = "pending" // switched on, takes effect at the next mount
	QuotasActive      = "active"
)

// SetShareQuotas switches project quotas for the shares of a volume on or
// off. They can only start when the volume is mounted, so the change takes
// effect at the next start of the NAS.
func (m *Manager) SetShareQuotas(name string, on bool) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	volumes := m.configured()
	for i := range volumes {
		if volumes[i].Name != name {
			continue
		}
		if volumes[i].Filesystem != "ext4" && volumes[i].Filesystem != "xfs" {
			return fmt.Errorf("%w: share quotas need ext4 or XFS", ErrNotPossible)
		}
		volumes[i].ShareQuotas = on
		return m.saveVolumes(volumes)
	}
	return ErrNotFound
}

// quotaMountOptions prepares a volume for project quotas before it is
// mounted. A failure never keeps the volume from mounting: the data comes
// first, the quotas stay pending and the reason is shown.
func (m *Manager) quotaMountOptions(ctx context.Context, volume VolumeConfig) string {
	m.setQuotaProblem(volume.Name, "")
	if !volume.ShareQuotas {
		return ""
	}
	switch volume.Filesystem {
	case "xfs":
		// XFS checks the quotas itself while mounting.
		return ",prjquota"
	case "ext4":
		device := "UUID=" + volume.FilesystemUUID
		out, err := m.runner.Run(ctx, "", "tune2fs", "-l", device)
		if err == nil && ext4HasProjectQuota(out) {
			return ""
		}
		// Adds the quota inodes and counts what is already there. tune2fs
		// refuses a file system that was not cleanly unmounted; it is then
		// left alone until a check has run.
		if _, err := m.runner.Run(ctx, "", "tune2fs", "-O", "quota,project", "-Q", "prjquota", device); err != nil {
			slog.Warn("project quotas not switched on", "volume", volume.Name, "err", err)
			m.setQuotaProblem(volume.Name, err.Error())
		}
	}
	return ""
}

func ext4HasProjectQuota(tune2fs string) bool {
	for line := range strings.Lines(tune2fs) {
		if rest, ok := strings.CutPrefix(line, "Filesystem features:"); ok {
			features := strings.Fields(rest)
			has := func(f string) bool {
				for _, v := range features {
					if v == f {
						return true
					}
				}
				return false
			}
			return has("quota") && has("project")
		}
	}
	return false
}

func (m *Manager) setQuotaProblem(volume, problem string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.quotaProblems == nil {
		m.quotaProblems = map[string]string{}
	}
	if problem == "" {
		delete(m.quotaProblems, volume)
	} else {
		m.quotaProblems[volume] = problem
	}
}

// quotaState fills in the share quota state of a volume for the status.
func (m *Manager) quotaState(cfg VolumeConfig, volume *Volume) {
	switch {
	case cfg.Filesystem != "ext4" && cfg.Filesystem != "xfs":
		volume.ShareQuotas = QuotasUnsupported
		return
	case !cfg.ShareQuotas:
		volume.ShareQuotas = QuotasOff
		return
	}
	volume.ShareQuotas = QuotasPending
	m.mu.RLock()
	volume.ShareQuotasError = m.quotaProblems[cfg.Name]
	m.mu.RUnlock()
	if volume.Mounted {
		if active, _ := m.quotaActive(volume.MountPoint); active {
			volume.ShareQuotas = QuotasActive
		}
	}
}

// ShareQuotas tells the shares where a volume is, whether its kernel
// enforces project quotas and whether TitanOS should use them there.
func (m *Manager) ShareQuotas(name string) (mountPoint string, active, wanted bool) {
	cfg, ok := m.find(name)
	if !ok || !containsMounted(m.Mounted(), name) {
		return "", false, false
	}
	mountPoint = m.MountPoint(name)
	active, _ = m.quotaActive(mountPoint)
	return mountPoint, active, cfg.ShareQuotas
}

func (m *Manager) quotaActive(mountPoint string) (bool, error) {
	if m.quotaProbe != nil {
		return m.quotaProbe(mountPoint)
	}
	return quota.Active(mountPoint)
}
