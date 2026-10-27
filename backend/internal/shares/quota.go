package shares

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/quota"
)

// Bounds of a share quota. Below the minimum, the space the file system
// itself needs for a few folders would already count against it.
const (
	MinQuota = 16 << 20
	MaxQuota = 1 << 60
	// firstQuotaID keeps share projects apart from IDs set up by hand.
	firstQuotaID = 10000
)

// States of the quota of a share.
const (
	QuotaNone     = ""
	QuotaPending  = "pending"  // the volume has no active project quotas yet
	QuotaApplying = "applying" // existing files are being counted
	QuotaActive   = "active"
	QuotaError    = "error"
)

// QuotaStatus is what a share holds against its limit.
type QuotaStatus struct {
	State string `json:"state"`
	Used  uint64 `json:"used"`
	Limit uint64 `json:"limit"`
	Error string `json:"error,omitempty"`
}

// VolumeQuotas tells whether the kernel enforces project quotas on a volume
// (active) and whether TitanOS should use them there (wanted).
type VolumeQuotas func(volume string) (mountPoint string, active, wanted bool)

// quotaKernel is the part of package quota the shares use; tests replace it.
type quotaKernel interface {
	Project(dir string) (uint32, bool, error)
	Assign(dir string, id uint32) error
	Get(mountPoint string, id uint32) (quota.Usage, error)
	SetLimit(mountPoint string, id uint32, bytes uint64) error
}

type kernelQuota struct{}

func (kernelQuota) Project(dir string) (uint32, bool, error) { return quota.Project(dir) }
func (kernelQuota) Assign(dir string, id uint32) error       { return quota.Assign(dir, id) }
func (kernelQuota) Get(mountPoint string, id uint32) (quota.Usage, error) {
	return quota.Get(mountPoint, id)
}
func (kernelQuota) SetLimit(mountPoint string, id uint32, bytes uint64) error {
	return quota.SetLimit(mountPoint, id, bytes)
}

type quotaState struct {
	mu       sync.Mutex
	status   map[string]QuotaStatus
	assigned map[string]bool // background Assign running, by share
	// failed holds why counting the files of a share failed, until a retry.
	failed map[string]assignFailure
	// released are projects of deleted shares whose limit is still to go.
	released []releasedQuota
	// applied are the limits set in the kernel since titand started.
	applied map[quotaKey]uint64
	where   map[string]quotaKey
	usage   map[string]usageRead
	kernel  quotaKernel
}

type quotaKey struct {
	point string
	id    uint32
}

type usageRead struct {
	used uint64
	at   time.Time
}

type assignFailure struct {
	err error
	at  time.Time
}

type releasedQuota struct {
	volume string
	id     uint32
}

// assignRetry is how long a failed count waits before it is tried again.
const assignRetry = 10 * time.Minute

const quotaIDFile = "shares/quota-ids.json"

// assignQuotaID gives a share with a quota its own project, keeping the
// one it has. IDs are never given twice: files of a deleted share keep
// theirs and would otherwise count for a new one.
func (m *Manager) assignQuotaID(share *Share) error {
	if share.Quota == 0 || share.QuotaID != 0 {
		return nil
	}
	var last struct {
		Next uint32 `json:"next"`
	}
	if err := m.store.ReadJSON(quotaIDFile, &last); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	next := max(last.Next, firstQuotaID)
	for _, s := range m.shares {
		if s.QuotaID >= next {
			next = s.QuotaID + 1
		}
	}
	last.Next = next + 1
	if err := m.store.WriteJSON(quotaIDFile, last); err != nil {
		return err
	}
	share.QuotaID = next
	return nil
}

// ApplyQuotas puts the limits of the shares into the kernel. Counting the
// existing files of a share runs in the background; everything else is a
// few system calls, so this runs with every round of Run.
func (m *Manager) ApplyQuotas(volumes VolumeQuotas, mounted []string) {
	if m.Disabled || volumes == nil {
		return
	}
	kernel := m.quotaKernel
	if kernel == nil {
		kernel = kernelQuota{}
	}
	m.quota.mu.Lock()
	released := m.quota.released
	m.quota.released = nil
	m.quota.mu.Unlock()
	var waiting []releasedQuota
	for _, r := range released {
		point, active, _ := volumes(r.volume)
		if !active || !slices.Contains(mounted, r.volume) {
			waiting = append(waiting, r)
			continue
		}
		if err := kernel.SetLimit(point, r.id, 0); err != nil {
			slog.Warn("share quota: limit of a deleted share not removed", "project", r.id, "err", err)
		}
		m.quota.mu.Lock()
		delete(m.quota.applied, quotaKey{point, r.id})
		m.quota.mu.Unlock()
	}
	if len(waiting) > 0 {
		m.quota.mu.Lock()
		m.quota.released = append(m.quota.released, waiting...)
		m.quota.mu.Unlock()
	}
	status := map[string]QuotaStatus{}
	where := map[string]quotaKey{}
	m.quota.mu.Lock()
	if m.quota.applied == nil {
		m.quota.applied = map[quotaKey]uint64{}
	}
	m.quota.mu.Unlock()
	for _, share := range m.List() {
		if share.QuotaID == 0 || !slices.Contains(mounted, share.Volume) {
			continue
		}
		point, active, wanted := volumes(share.Volume)
		limit := share.Quota
		if !wanted {
			limit = 0
		}
		if !active {
			// Limits are set again once the volume enforces quotas.
			m.quota.mu.Lock()
			delete(m.quota.applied, quotaKey{point, share.QuotaID})
			m.quota.mu.Unlock()
			if share.Quota > 0 {
				status[share.Name] = QuotaStatus{State: QuotaPending, Limit: share.Quota}
			}
			continue
		}
		dir := m.Path(share)
		if _, err := os.Lstat(dir); err != nil {
			continue
		}
		st := QuotaStatus{State: QuotaActive, Limit: limit}
		if limit > 0 {
			if id, inherit, err := kernel.Project(dir); err != nil {
				st = QuotaStatus{State: QuotaError, Limit: limit, Error: err.Error()}
			} else if id != share.QuotaID || !inherit {
				st.State = QuotaApplying
				if err := m.assignLater(kernel, share.Name, dir, share.QuotaID); err != nil {
					st = QuotaStatus{State: QuotaError, Limit: limit, Error: err.Error()}
				}
			}
		}
		key := quotaKey{point, share.QuotaID}
		m.quota.mu.Lock()
		applied, known := m.quota.applied[key]
		m.quota.mu.Unlock()
		if !known || applied != limit {
			if err := kernel.SetLimit(point, share.QuotaID, limit); err != nil {
				st = QuotaStatus{State: QuotaError, Limit: limit, Error: err.Error()}
			} else {
				m.quota.mu.Lock()
				m.quota.applied[key] = limit
				delete(m.quota.usage, share.Name)
				m.quota.mu.Unlock()
			}
		}
		if share.Quota > 0 && wanted {
			status[share.Name] = st
			where[share.Name] = key
		}
	}
	m.quota.mu.Lock()
	// A share still being counted keeps saying so.
	for name, busy := range m.quota.assigned {
		if busy {
			if st, ok := status[name]; ok && st.State == QuotaActive {
				st.State = QuotaApplying
				status[name] = st
			}
		}
	}
	m.quota.status = status
	m.quota.where = where
	m.quota.kernel = kernel
	m.quota.mu.Unlock()
}

// assignLater counts the files of a share in the background. It returns
// the error of the last attempt while that waits for its retry.
func (m *Manager) assignLater(kernel quotaKernel, name, dir string, id uint32) error {
	m.quota.mu.Lock()
	defer m.quota.mu.Unlock()
	if m.quota.assigned == nil {
		m.quota.assigned = map[string]bool{}
		m.quota.failed = map[string]assignFailure{}
	}
	if m.quota.assigned[name] {
		return nil
	}
	if f, ok := m.quota.failed[name]; ok && time.Since(f.at) < assignRetry {
		return f.err
	}
	m.quota.assigned[name] = true
	go func() {
		err := kernel.Assign(dir, id)
		m.quota.mu.Lock()
		delete(m.quota.assigned, name)
		if err != nil {
			slog.Warn("share quota: counting files failed", "share", name, "err", err)
			m.quota.failed[name] = assignFailure{err, time.Now()}
		} else {
			slog.Info("share quota applied", "share", name, "project", id)
			delete(m.quota.failed, name)
		}
		m.quota.mu.Unlock()
		m.Kick()
	}()
	return nil
}

// usageFresh is how long a read usage is shown before it is read again.
// Usage is read only when asked for, so sleeping disks are left alone.
const usageFresh = 30 * time.Second

// Quota returns what a share holds against its limit, if it has one.
func (m *Manager) Quota(name string) (QuotaStatus, bool) {
	m.quota.mu.Lock()
	st, ok := m.quota.status[name]
	key, enforced := m.quota.where[name]
	cached, fresh := m.quota.usage[name]
	kernel := m.quota.kernel
	m.quota.mu.Unlock()
	if !ok || !enforced || st.State == QuotaError {
		return st, ok
	}
	if fresh && time.Since(cached.at) < usageFresh {
		st.Used = cached.used
		return st, true
	}
	usage, err := kernel.Get(key.point, key.id)
	if err != nil {
		st.State, st.Error = QuotaError, err.Error()
		return st, true
	}
	st.Used = usage.Used
	m.quota.mu.Lock()
	if m.quota.usage == nil {
		m.quota.usage = map[string]usageRead{}
	}
	m.quota.usage[name] = usageRead{usage.Used, time.Now()}
	m.quota.mu.Unlock()
	return st, true
}
