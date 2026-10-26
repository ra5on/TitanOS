package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"golang.org/x/sys/unix"
)

type CloneRequest struct {
	Name   string `json:"name"`
	Volume string `json:"volume"`
}
type CloneOperation struct {
	ID      string    `json:"id"`
	Source  string    `json:"source"`
	VM      VM        `json:"vm"`
	Phase   string    `json:"phase"`
	Error   string    `json:"error"`
	Created time.Time `json:"created"`
	// Conflict records a target folder that blocked publication. The journal
	// keeps Phase "publishing" so the previous app version can still finish
	// (retrying the no-replace rename) or discard it; CloneOperations reports
	// such an operation as phase "conflict".
	Conflict string `json:"conflict,omitempty"`
}

func (m *Manager) Clone(ctx context.Context, name string, req CloneRequest, mounted []string) error {
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
	if !m.createMu.TryLock() {
		return ErrSnapshotBusy
	}
	defer func() {
		if release {
			m.createMu.Unlock()
		}
	}()
	source, err := m.get(name)
	if err != nil {
		return err
	}
	if !m.available(source, mounted) || !slices.Contains(mounted, req.Volume) {
		return ErrUnknownVolume
	}
	if m.pid(name) != 0 {
		return ErrRunning
	}
	if m.pendingSnapshot(source) {
		return ErrSnapshotRecovery
	}
	if m.snapshotReason(source) != "" {
		return ErrSnapshotUnsupported
	}
	if _, err := m.get(req.Name); err == nil {
		return ErrExists
	}
	var clone VM
	b, _ := json.Marshal(source)
	if err := json.Unmarshal(b, &clone); err != nil {
		return err
	}
	clone.Name = req.Name
	clone.DisplayName = ""
	clone.Volume = req.Volume
	clone.Directory = ""
	clone.Autostart = false
	clone.CPUPin = nil
	clone.Exclusive = false
	clone.SnapshotSchedule = SnapshotSchedule{}
	clone.Created = time.Now().UTC()
	clone.UUID = newUUID()
	clone.MAC = newMAC()
	for i := range clone.Disks {
		clone.Disks[i].File = fmt.Sprintf("vdisk%d.%s", i+1, clone.Disks[i].Format)
		clone.Disks[i].Volume = req.Volume
		clone.Disks[i].Path = ""
		clone.Disks[i].Source = ""
	}
	if err := m.prepare(ctx, &clone, mounted); err != nil {
		return err
	}
	if err := m.checkDirectory(clone); err != nil {
		return err
	}
	// checkDirectory accepts an existing legacy folder (Directory == ""), but
	// a clone is published without overwriting: a leftover folder (e.g. of a
	// VM deleted with its disks kept) would only fail after the whole copy.
	target := m.dir(clone.Volume, clone.folder())
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%w: %s already exists; remove or rename it, or choose another name", ErrBadFolder, target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	files, err := m.snapshotFiles(source)
	if err != nil {
		return err
	}
	var needed int64 = 256 << 20
	for _, f := range files {
		_, allocated, err := snapshotTreeSize(f.Target)
		if err != nil {
			return err
		}
		needed += allocated
	}
	for _, d := range source.Disks {
		if m.usedBy(m.diskPath(source, d), source.Name) != "" {
			return ErrBadImage
		}
		if _, err := m.imageInfo(ctx, m.diskPath(source, d)); err != nil {
			return err
		}
	}
	releaseSpace, err := m.reserveSnapshotSpace(map[string]int64{filepath.Join(m.mountRoot, req.Volume): needed})
	if err != nil {
		return err
	}
	root, err := volumedata.Open(m.mountRoot, req.Volume, "vmclones", mounted)
	if err != nil {
		releaseSpace()
		return err
	}
	op := CloneOperation{ID: newUUID(), Source: name, VM: clone, Phase: "copying", Created: time.Now().UTC()}
	if err := root.Mkdir(op.ID, 0700); err != nil {
		root.Close()
		releaseSpace()
		return err
	}
	if err := root.Mkdir(op.ID+"/data", 0700); err != nil {
		root.Close()
		releaseSpace()
		return err
	}
	if err := volumedata.WriteJSON(root, op.ID+"/operation.json", op); err != nil {
		root.Close()
		releaseSpace()
		return err
	}
	release = false
	m.operationJob(name, "clone", func() (result error) {
		defer m.createMu.Unlock()
		defer root.Close()
		defer releaseSpace()
		defer func() {
			if result != nil {
				op.Error = result.Error()
				_ = volumedata.WriteJSON(root, op.ID+"/operation.json", op)
			}
		}()
		stage := filepath.Join(m.mountRoot, req.Volume, ".titanos", "vmclones", op.ID, "data")
		for i, f := range files {
			target := ""
			if i < len(source.Disks) {
				target = clone.Disks[i].File
			} else {
				target = filepath.Base(f.Target)
			}
			before, err := os.Stat(f.Target)
			if err != nil {
				return err
			}
			if err := copySnapshotTree(f.Target, filepath.Join(stage, target)); err != nil {
				return err
			}
			after, err := os.Stat(f.Target)
			if err != nil {
				return err
			}
			if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				return errors.New("source changed while cloning")
			}
			if i < len(source.Disks) {
				if _, err := m.runner.Run(context.WithoutCancel(ctx), "", "qemu-img", "compare", "-f", source.Disks[i].Format, "-F", clone.Disks[i].Format, f.Target, filepath.Join(stage, target)); err != nil {
					return err
				}
			}
		}
		if err := m.share(stage, 0770); err != nil {
			return err
		}
		if err := syncSnapshotDir(stage); err != nil {
			return err
		}
		markerRoot, err := os.OpenRoot(stage)
		if err != nil {
			return err
		}
		err = volumedata.WriteJSON(markerRoot, ".titanos-clone.json", op)
		markerRoot.Close()
		if err != nil {
			return err
		}
		op.Phase = "verified"
		if err := volumedata.WriteJSON(root, op.ID+"/operation.json", op); err != nil {
			return err
		}
		return m.publishClone(root, &op)
	}, lock)
	return nil
}
func (m *Manager) publishClone(root *os.Root, op *CloneOperation) error {
	v := op.VM
	if err := m.validClone(v, v.Volume); err != nil {
		return err
	}
	parent := filepath.Join(m.mountRoot, v.Volume, "vms")
	if err := os.Mkdir(parent, 0770); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := m.snapshotPath(parent, false); err != nil {
		return err
	}
	if err := m.share(parent, 0770); err != nil {
		return err
	}
	stage := filepath.Join(m.mountRoot, v.Volume, ".titanos", "vmclones", op.ID, "data")
	final := m.dir(v.Volume, v.folder())
	if op.Phase == "verified" {
		op.Phase = "publishing"
		if err := volumedata.WriteJSON(root, op.ID+"/operation.json", op); err != nil {
			return err
		}
	}
	op.Conflict = ""
	if _, err := os.Lstat(stage); err == nil {
		if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE); err != nil {
			if errors.Is(err, unix.EEXIST) || errors.Is(err, unix.ENOTEMPTY) {
				// Someone else's folder: never touch it. The staged copy
				// stays and can be finished later or discarded.
				op.Conflict = final
				err = fmt.Errorf("%w: %s already exists; remove or rename it and finish, or discard the staged clone", ErrBadFolder, final)
				op.Error = err.Error()
				return errors.Join(err, volumedata.WriteJSON(root, op.ID+"/operation.json", op))
			}
			return err
		}
		if err := syncSnapshotDir(parent); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := m.snapshotPath(final, false); err != nil {
		return err
	}
	// The marker binds a previously published directory to this recovery operation.
	marker := filepath.Join(final, ".titanos-clone.json")
	if _, err := os.Stat(marker); errors.Is(err, fs.ErrNotExist) {
		return errors.New("clone publication needs its verified marker")
	}
	var saved CloneOperation
	finalRoot, err := os.OpenRoot(final)
	if err != nil {
		return err
	}
	err = volumedata.ReadJSON(finalRoot, ".titanos-clone.json", &saved, 1<<20)
	finalRoot.Close()
	if err != nil || saved.ID != op.ID || saved.VM.UUID != v.UUID {
		return errors.New("clone directory identity mismatch")
	}
	for _, disk := range v.Disks {
		p := m.diskPath(v, disk)
		if err := m.snapshotPath(p, false); err != nil {
			return err
		}
		if _, err := m.imageInfo(context.Background(), p); err != nil {
			return err
		}
	}
	m.mu.Lock()
	_, exists := m.vms[v.Name]
	loaded := m.loaded
	m.mu.Unlock()
	if !loaded {
		return ErrInvalid
	}
	if exists {
		current, _ := m.get(v.Name)
		if current.UUID != v.UUID {
			return ErrExists
		}
	} else {
		if err := m.persistVM(v); err != nil {
			return err
		}
		m.mu.Lock()
		m.vms[v.Name] = v
		m.mu.Unlock()
	}
	if err := root.RemoveAll(op.ID); err != nil {
		return err
	}
	m.mu.Lock()
	if job := m.snapshotJobs[op.Source]; job != nil && job.Operation == "clone" && job.State == "failed" {
		delete(m.snapshotJobs, op.Source)
	}
	m.mu.Unlock()
	return nil
}
func (m *Manager) CloneOperations(volume string, mounted []string) ([]CloneOperation, error) {
	root, err := volumedata.Open(m.mountRoot, volume, "vmclones", mounted)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(101)
	if err != nil && !errors.Is(err, io.EOF) && len(entries) == 0 {
		return nil, err
	}
	if len(entries) > 100 {
		return nil, errors.New("too many clone operations")
	}
	out := []CloneOperation{}
	for _, e := range entries {
		if !e.IsDir() || !snapshotID.MatchString(e.Name()) {
			continue
		}
		var op CloneOperation
		if err := volumedata.ReadJSON(root, e.Name()+"/operation.json", &op, 1<<20); err != nil {
			return nil, err
		}
		if op.Phase == "publishing" && op.Conflict != "" {
			op.Phase = "conflict" // reported only; the journal stays "publishing"
		}
		out = append(out, op)
	}
	return out, nil
}
func (m *Manager) RecoverClone(volume, id, action, confirm string, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !snapshotID.MatchString(id) || confirm != id {
		return ErrNotConfirmed
	}
	if !m.createMu.TryLock() {
		return ErrSnapshotBusy
	}
	defer m.createMu.Unlock()
	root, err := volumedata.Open(m.mountRoot, volume, "vmclones", mounted)
	if err != nil {
		return err
	}
	defer root.Close()
	var op CloneOperation
	if err := volumedata.ReadJSON(root, id+"/operation.json", &op, 1<<20); err != nil {
		return err
	}
	if op.ID != id || op.VM.Volume != volume || !nameRE.MatchString(op.VM.Name) || op.VM.Directory != "" {
		return ErrInvalid
	}
	if action == "discard" {
		switch op.Phase {
		case "copying", "verified":
			return root.RemoveAll(id)
		case "publishing":
			// Until the rename succeeded the staged copy is the only copy and
			// nothing was published; afterwards the folder is the clone.
			info, err := root.Lstat(id + "/data")
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return ErrInvalid
				}
				return err
			}
			if !info.IsDir() {
				return ErrInvalid
			}
			return root.RemoveAll(id)
		}
		return ErrInvalid
	}
	if action == "finish" && (op.Phase == "verified" || op.Phase == "publishing") {
		if err := m.validClone(op.VM, volume); err != nil {
			return err
		}
		return m.publishClone(root, &op)
	}
	return ErrInvalid
}

func (m *Manager) validClone(v VM, volume string) error {
	mac, err := net.ParseMAC(v.MAC)
	if err != nil || len(mac) != 6 || !snapshotID.MatchString(v.UUID) || v.Volume != volume || v.Directory != "" || v.Autostart || v.Exclusive || len(v.CPUPin) > 0 || len(v.PCI) > 0 || len(v.USB) > 0 || len(v.Folders) > 0 || v.AutoGPU || v.ISO != "" || len(v.Disks) == 0 {
		return ErrInvalid
	}
	for i, d := range v.Disks {
		if d.Path != "" || d.Source != "" || d.Volume != volume || (d.Format != "raw" && d.Format != "qcow2") || d.File != fmt.Sprintf("vdisk%d.%s", i+1, d.Format) {
			return ErrInvalid
		}
	}
	return m.prepare(context.Background(), &v, []string{volume})
}
