package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/naming"
)

type ImportCandidate struct {
	ID             string   `json:"id"`
	Filesystem     string   `json:"filesystem"`
	FilesystemUUID string   `json:"filesystemUUID"`
	ArrayUUID      string   `json:"arrayUUID"`
	Level          string   `json:"level"`
	Disks          []string `json:"disks"`
	Size           uint64   `json:"size"`
	Fingerprint    string   `json:"fingerprint"`
	Reason         string   `json:"reason"`
	device         string
}
type ImportPreview struct {
	Candidate ImportCandidate `json:"candidate"`
	Folders   []string        `json:"folders"`
	Truncated bool            `json:"truncated"`
	ReadOnly  bool            `json:"readOnly"`
}
type ImportRequest struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	Confirm     string `json:"confirm"`
}

// ImportCandidates only describes unregistered, assembled arrays and two-device Btrfs filesystems.
// It never assembles, mounts or modifies a device as a side effect of discovery.
func (m *Manager) ImportCandidates(ctx context.Context) ([]ImportCandidate, error) {
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return nil, err
	}
	arrays := m.arrays(ctx)
	status := m.status(devices, arrays)
	registered := map[string]bool{}
	for _, v := range m.configured() {
		registered[normalizeUUID(v.ArrayUUID)] = true
		registered[normalizeUUID(v.FilesystemUUID)] = true
	}
	nodes := map[string]blockDevice{}
	owners := map[string][]Disk{}
	btrfs := map[string][]Disk{}
	btrfsDevices := map[string]blockDevice{}
	btrfsCounts := map[string]int{}
	for _, d := range devices {
		if !isDisk(d) {
			continue
		}
		var disk Disk
		for _, candidate := range status.Disks {
			if candidate.Path == d.Name {
				disk = candidate
				break
			}
		}
		d.walk(func(node blockDevice) {
			if isArray(node) {
				nodes[filepath.Base(node.Name)] = node
				owners[filepath.Base(node.Name)] = append(owners[filepath.Base(node.Name)], disk)
			}
			if node.FSType == "btrfs" && node.UUID != "" {
				btrfsCounts[node.UUID]++
				if !slices.ContainsFunc(btrfs[node.UUID], func(d Disk) bool { return d.Path == disk.Path }) {
					btrfs[node.UUID] = append(btrfs[node.UUID], disk)
				}
				btrfsDevices[node.UUID] = node
			}
		})
	}
	// lsblk may emit an array under only one of its parents. Resolve every
	// member partition explicitly so fingerprints cover the complete set.
	for _, a := range arrays {
		members := []Disk{}
		seen := map[string]bool{}
		for _, member := range a.members {
			for _, device := range devices {
				if !isDisk(device) {
					continue
				}
				match := false
				device.walk(func(n blockDevice) {
					if filepath.Base(n.Name) == member.device {
						match = true
					}
				})
				if match && !seen[device.Name] {
					for _, d := range status.Disks {
						if d.Path == device.Name {
							members = append(members, d)
							seen[device.Name] = true
						}
					}
				}
			}
		}
		owners[a.device] = members
	}
	out := []ImportCandidate{}
	fingerprint := func(c *ImportCandidate, disks []Disk) {
		sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
		identities := []string{}
		for _, d := range disks {
			c.Disks = append(c.Disks, d.Name)
			identities = append(identities, d.Identity)
			if d.Role == RoleStick || d.Role == RoleMember || d.Mounted || d.Testing {
				c.Reason = "device-in-use"
			}
			if m.Claimed != nil && m.Claimed()[d.Name] != "" {
				c.Reason = "device-in-use"
			}
		}
		data, _ := json.Marshal(struct {
			ID, FS, UUID      string
			Disks, Identities []string
		}{c.ID, c.Filesystem, c.FilesystemUUID, c.Disks, identities})
		hash := sha256.Sum256(data)
		c.Fingerprint = hex.EncodeToString(hash[:])
	}
	for _, a := range arrays {
		if registered[a.uuid] {
			continue
		}
		node, ok := nodes[a.device]
		if !ok {
			continue
		}
		c := ImportCandidate{ID: "md:" + a.uuid, Filesystem: node.FSType, FilesystemUUID: node.UUID, ArrayUUID: a.uuid, Level: a.level, Size: node.Size, device: "/dev/" + a.device}
		if node.Mountpoint != "" || len(node.Children) > 0 {
			c.Reason = "mounted-or-partitioned"
		}
		if !slices.Contains([]string{"ext4", "xfs"}, c.Filesystem) || c.FilesystemUUID == "" {
			c.Reason = "unsupported-filesystem"
		}
		if _, ok := minDisks[c.Level]; !ok {
			c.Reason = "unsupported-raid"
		}
		if len(owners[a.device]) < a.disks {
			c.Reason = "incomplete-membership"
		}
		if a.degraded > 0 || (a.syncAction != "idle" && a.syncAction != "") {
			c.Reason = "array-not-healthy"
		}
		fingerprint(&c, owners[a.device])
		out = append(out, c)
	}
	for uuid, disks := range btrfs {
		if registered[normalizeUUID(uuid)] {
			continue
		}
		node := btrfsDevices[uuid]
		c := ImportCandidate{ID: "btrfs:" + uuid, Filesystem: "btrfs", FilesystemUUID: uuid, Level: "raid1", Size: node.Size, device: node.Name}
		if len(disks) != 2 || btrfsCounts[uuid] != 2 || node.Mountpoint != "" {
			c.Reason = "requires-unmounted-two-device-mirror"
		}
		fingerprint(&c, disks)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (m *Manager) importCandidate(ctx context.Context, req ImportRequest) (ImportCandidate, error) {
	list, err := m.ImportCandidates(ctx)
	if err != nil {
		return ImportCandidate{}, err
	}
	for _, c := range list {
		if c.ID == req.ID {
			if req.Fingerprint == "" || c.Fingerprint != req.Fingerprint {
				return c, fmt.Errorf("%w: device identity changed", ErrDiskUnavailable)
			}
			if c.Reason != "" {
				return c, fmt.Errorf("%w: %s", ErrNotPossible, c.Reason)
			}
			return c, nil
		}
	}
	return ImportCandidate{}, ErrNotFound
}
func (m *Manager) previewImport(ctx context.Context, c ImportCandidate) (preview ImportPreview, err error) {
	preview = ImportPreview{Candidate: c, Folders: []string{}, ReadOnly: true}
	point, err := os.MkdirTemp("", "titanos-import-")
	if err != nil {
		return preview, err
	}
	defer os.Remove(point)
	options := "ro,nodev,nosuid,noexec"
	switch c.Filesystem {
	case "ext4":
		options += ",noload"
	case "xfs":
		options += ",norecovery,nouuid"
	case "btrfs":
		options += ",nologreplay,subvolid=5"
	default:
		return preview, ErrInvalidFS
	}
	if _, err = m.runner.Run(ctx, "", "mount", "-t", c.Filesystem, "-o", options, c.device, point); err != nil {
		return preview, err
	}
	defer func() {
		_, unmountErr := func() (string, error) {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			return m.runner.Run(cleanup, "", "umount", point)
		}()
		err = errors.Join(err, unmountErr)
	}()
	if c.Filesystem == "btrfs" {
		output, e := m.runner.Run(ctx, "", "btrfs", "filesystem", "df", "-b", point)
		if e != nil {
			return preview, e
		}
		if !strings.Contains(output, "Data, RAID1:") || !strings.Contains(output, "Metadata, RAID1:") {
			return preview, fmt.Errorf("%w: only native RAID1 data and metadata profiles are supported", ErrInvalidLevel)
		}
	}
	root, e := os.OpenRoot(point)
	if e != nil {
		return preview, e
	}
	defer root.Close()
	dir, e := root.Open(".")
	if e != nil {
		return preview, e
	}
	defer dir.Close()
	entries, e := dir.ReadDir(201)
	if e != nil && !errors.Is(e, io.EOF) {
		return preview, e
	}
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			preview.Folders = append(preview.Folders, entry.Name())
		}
	}
	sort.Strings(preview.Folders)
	if len(entries) > 200 {
		preview.Truncated = true
	}
	return preview, nil
}
func (m *Manager) PreviewImport(ctx context.Context, req ImportRequest) (ImportPreview, error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ImportPreview{}, ErrReadOnly
	}
	c, err := m.importCandidate(ctx, req)
	if err != nil {
		return ImportPreview{}, err
	}
	return m.previewImport(ctx, c)
}
func (m *Manager) Import(ctx context.Context, req ImportRequest) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}
	m.mu.RLock()
	loaded := m.loaded
	m.mu.RUnlock()
	if !loaded {
		return errors.New("volume configuration not loaded")
	}
	if !naming.ValidVolume(req.Name) {
		return ErrInvalidName
	}
	if req.Confirm != req.Name {
		return ErrNotConfirmed
	}
	for _, v := range m.configured() {
		if strings.EqualFold(v.Name, req.Name) {
			return ErrExists
		}
	}
	c, err := m.importCandidate(ctx, req)
	if err != nil {
		return err
	}
	if _, err = m.previewImport(ctx, c); err != nil {
		return err
	}
	// Registration writes only UUID configuration. No formatting, relabeling, folder creation or account import.
	v := VolumeConfig{Name: req.Name, Filesystem: c.Filesystem, FilesystemUUID: c.FilesystemUUID, ArrayUUID: c.ArrayUUID, Level: c.Level, Created: time.Now().UTC()}
	if err := m.saveVolumes(append(m.configured(), v)); err != nil {
		return err
	}
	return m.mount(ctx, v)
}
