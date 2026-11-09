package vm

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Restoring, recovering and deleting snapshots (split from snapshots.go,
// PLAN 75-12).

func (m *Manager) RestoreSnapshot(ctx context.Context, name, id, confirm string, memory bool, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	release := true
	defer func() {
		if release {
			lock.Unlock()
		}
	}()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if confirm != v.Name && confirm != v.Label() {
		return ErrNotConfirmed
	}
	if m.pid(name) != 0 {
		return ErrRunning
	}
	if !m.available(v, mounted) {
		return ErrUnknownVolume
	}
	if m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	s, err := m.readSnapshot(v, id)
	if err != nil {
		return err
	}
	if memory && !s.Memory {
		return ErrInvalid
	}
	if memory {
		runtime, err := m.snapshotRuntime(ctx, s.VM)
		if err != nil {
			return err
		}
		if runtime != s.Runtime {
			return ErrSnapshotRuntime
		}
	}
	needs := map[uint64]int64{}
	paths := map[uint64]string{}
	for _, f := range s.Files {
		src := filepath.Join(m.snapshotRoot(v), id, f.File)
		if err = m.snapshotPath(src, false); err != nil {
			return err
		}
		size, allocated, e := snapshotTreeSize(src)
		if e != nil {
			return e
		}
		st, e := os.Stat(src)
		if e != nil {
			return e
		}
		if st.IsDir() != f.Directory || size != f.Size {
			return ErrBadImage
		}
		if other := m.usedBy(f.Target, v.Name); other != "" {
			return ErrBadImage
		}
		parent := filepath.Dir(f.Target)
		if err = m.snapshotPath(parent, false); err != nil {
			return err
		}
		parentInfo, e := os.Stat(parent)
		if e != nil {
			return e
		}
		dev := parentInfo.Sys().(*syscall.Stat_t).Dev
		needs[dev] += allocated
		paths[dev] = parent
	}
	if memory {
		p := filepath.Join(m.snapshotRoot(v), id, "memory.save")
		if err = m.snapshotPath(p, false); err != nil {
			return err
		}
		if _, err = m.runner.Run(ctx, "", "virsh", "--quiet", "save-image-dumpxml", p); err != nil {
			return err
		}
	}
	budget := map[string]int64{}
	for dev, amount := range needs {
		budget[paths[dev]] = amount + (256 << 20)
	}
	releaseSpace, err := m.reserveSnapshotSpace(budget)
	if err != nil {
		return err
	}
	release = false
	ctx = context.WithoutCancel(ctx)
	m.snapshotJob(name, func() error {
		defer releaseSpace()
		return m.restoreSnapshot(ctx, v, s, memory)
	}, lock)
	return nil
}
func snapshotStaging(f SnapshotFile, id, suffix string) string {
	return f.Target + ".titanos-snapshot-" + id + suffix
}
func (m *Manager) restoreSnapshot(ctx context.Context, v VM, s Snapshot, memory bool) (result error) {
	root := m.snapshotRoot(v)
	store := config.Open(root)
	op := snapshotOperation{Kind: "restore", Phase: "staging", ID: s.ID, Original: v, Files: s.Files, Memory: memory, Runtime: s.Runtime}
	for _, f := range op.Files {
		_, err := os.Lstat(f.Target)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		op.Existing = append(op.Existing, err == nil)
		for _, suffix := range []string{".new", ".old"} {
			if _, err = os.Lstat(snapshotStaging(f, op.ID, suffix)); !errors.Is(err, fs.ErrNotExist) {
				return ErrSnapshotRecovery
			}
		}
	}
	if err := store.WriteJSON("operation.json", op); err != nil {
		return err
	}
	defer func() {
		if result != nil && op.Phase != "committed" && op.Phase != "resume-started" {
			result = errors.Join(result, m.rollbackSnapshot(v, &op))
		}
	}()
	for _, f := range op.Files {
		if err := copySnapshotTree(filepath.Join(root, s.ID, f.File), snapshotStaging(f, s.ID, ".new")); err != nil {
			return err
		}
	}
	op.Phase = "switching"
	if err := store.WriteJSON("operation.json", op); err != nil {
		return err
	}
	for i, f := range op.Files {
		if op.Existing[i] {
			if err := os.Rename(f.Target, snapshotStaging(f, s.ID, ".old")); err != nil {
				return err
			}
			if err := syncSnapshotDir(filepath.Dir(f.Target)); err != nil {
				return err
			}
		}
		if err := os.Rename(snapshotStaging(f, s.ID, ".new"), f.Target); err != nil {
			return err
		}
		if err := syncSnapshotDir(filepath.Dir(f.Target)); err != nil {
			return err
		}
	}
	restored := s.VM
	restored.SnapshotSchedule = v.SnapshotSchedule
	restored.PowerSchedule = v.PowerSchedule
	if err := m.persistVM(restored); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[v.Name] = restored
	m.mu.Unlock()
	op.Phase = "committed"
	if err := store.WriteJSON("operation.json", op); err != nil {
		op.Phase = "switching"
		return err
	}
	if memory {
		if err := m.resumeSnapshot(ctx, store, &op, filepath.Join(root, s.ID)); err != nil {
			return err
		}
	}
	for _, f := range op.Files {
		if err := os.RemoveAll(snapshotStaging(f, s.ID, ".old")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := syncSnapshotDir(filepath.Dir(f.Target)); err != nil {
			return err
		}
	}
	return store.Remove("operation.json")
}
func (m *Manager) rollbackSnapshot(v VM, op *snapshotOperation) error {
	if m.pid(v.Name) != 0 {
		return ErrRunning
	}
	for i, f := range op.Files {
		old, next := snapshotStaging(f, op.ID, ".old"), snapshotStaging(f, op.ID, ".new")
		if _, err := os.Lstat(old); err == nil {
			if err = os.RemoveAll(f.Target); err != nil {
				return err
			}
			if err = os.Rename(old, f.Target); err != nil {
				return err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		} else if !op.Existing[i] && op.Phase == "switching" {
			if err = os.RemoveAll(f.Target); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if err := os.RemoveAll(next); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := syncSnapshotDir(filepath.Dir(f.Target)); err != nil {
			return err
		}
	}
	if err := m.persistVM(op.Original); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[v.Name] = op.Original
	m.mu.Unlock()
	return config.Open(m.snapshotRoot(v)).Remove("operation.json")
}

// Memory is never replayed after a resume may already have executed. The owner
// can explicitly discard RAM in that ambiguous case and cold-start later.
func (m *Manager) RecoverSnapshot(ctx context.Context, name, confirm string, discardMemory bool, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if confirm != v.Name && confirm != v.Label() {
		return ErrNotConfirmed
	}
	if !m.available(v, mounted) {
		return ErrUnknownVolume
	}
	root := m.snapshotRoot(v)
	if err = m.snapshotPath(root, false); err != nil {
		return err
	}
	store := config.Open(root)
	var op snapshotOperation
	if err = store.ReadJSON("operation.json", &op); err != nil {
		return err
	}
	if !snapshotID.MatchString(op.ID) || op.Original.UUID != v.UUID || op.Original.Name != v.Name {
		return ErrInvalid
	}
	dir := filepath.Join(root, op.ID)
	if op.Kind == "restore" {
		s, err := m.readSnapshot(v, op.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(s.Files, op.Files) || len(op.Existing) != len(op.Files) {
			return ErrInvalid
		}
		if op.Phase == "staging" || op.Phase == "switching" {
			return m.rollbackSnapshot(v, &op)
		}
		if op.Phase != "committed" && op.Phase != "resume-started" {
			return ErrInvalid
		}
	} else if op.Kind != "create" {
		return ErrInvalid
	}
	if op.Memory && m.pid(v.Name) == 0 && !discardMemory {
		if op.Phase == "resume-started" {
			return ErrSnapshotRecovery
		}
		runtime, e := m.snapshotRuntime(ctx, op.Original)
		if e != nil {
			return e
		}
		if runtime != op.Runtime {
			return ErrSnapshotRuntime
		}
		p := filepath.Join(dir, "memory.save")
		if err = m.snapshotPath(p, false); err != nil {
			return err
		}
		if _, err = m.runner.Run(ctx, "", "virsh", "--quiet", "save-image-dumpxml", p); err != nil {
			return err
		}
		if err = m.resumeSnapshot(ctx, store, &op, dir); err != nil {
			return err
		}
	}
	if op.Kind == "restore" {
		for _, f := range op.Files {
			if err = os.RemoveAll(snapshotStaging(f, op.ID, ".old")); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	if err = store.Remove("operation.json"); err != nil {
		return err
	}
	if op.Kind == "create" {
		if _, e := os.Stat(filepath.Join(dir, "snapshot.json")); errors.Is(e, fs.ErrNotExist) {
			os.RemoveAll(dir)
		}
	}
	m.mu.Lock()
	delete(m.snapshotJobs, name)
	m.mu.Unlock()
	return nil
}
func (m *Manager) DeleteSnapshot(name, id, confirm string, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if !m.available(v, mounted) {
		return ErrUnknownVolume
	}
	if m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	s, err := m.readSnapshot(v, id)
	if err != nil {
		if !snapshotID.MatchString(id) {
			return ErrNotFound
		}
		dir := filepath.Join(m.snapshotRoot(v), id)
		if e := m.snapshotPath(dir, false); e != nil {
			return e
		}
		st, e := os.Lstat(dir)
		if e != nil {
			return e
		}
		if !st.IsDir() {
			return ErrInvalid
		}
		s.Name = id
	}
	if confirm != s.Name {
		return ErrNotConfirmed
	}
	return config.Open(m.snapshotRoot(v)).RemoveAll(id)
}
