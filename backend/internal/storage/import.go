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
	// Encrypted is an array with LUKS on it: preview and import need its
	// password. EncryptionUUID is the LUKS UUID.
	Encrypted      bool   `json:"encrypted,omitempty"`
	EncryptionUUID string `json:"encryptionUUID,omitempty"`
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
	// Password opens an encrypted array; never stored or logged.
	Password string `json:"password,omitempty"`
}

// ImportCandidates only describes unregistered, assembled arrays and two-device Btrfs filesystems.
// It never assembles, mounts or modifies a device as a side effect of discovery.
func (m *Manager) ImportCandidates(ctx context.Context) ([]ImportCandidate, error) {
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return nil, err
	}
	arrays := m.arrays(ctx)
	status := m.status(ctx, devices, arrays)
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
			ID, FS, UUID, LUKS string
			Disks, Identities  []string
		}{c.ID, c.Filesystem, c.FilesystemUUID, c.EncryptionUUID, c.Disks, identities})
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
		if node.FSType == "crypto_LUKS" {
			// The file system is known only once the password opened it.
			c.Encrypted, c.EncryptionUUID, c.Filesystem, c.FilesystemUUID = true, node.UUID, "", ""
		}
		if node.Mountpoint != "" || len(node.Children) > 0 {
			c.Reason = "mounted-or-partitioned"
		}
		if !c.Encrypted && (!slices.Contains([]string{"ext4", "xfs"}, c.Filesystem) || c.FilesystemUUID == "") {
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
	// LUKS directly on disks or partitions (an encrypted Btrfs member, or
	// anything else) cannot be taken over: TitanOS would have to guess
	// which containers belong together. Shown so that the reason is clear.
	for _, d := range devices {
		if !isDisk(d) {
			continue
		}
		d.walk(func(node blockDevice) {
			if node.FSType != "crypto_LUKS" || !ownedByDisk(node) || node.UUID == "" || registered[normalizeUUID(node.UUID)] || m.Owns(node.UUID) {
				return
			}
			var disks []Disk
			for _, candidate := range status.Disks {
				if candidate.Path == d.Name {
					disks = append(disks, candidate)
				}
			}
			c := ImportCandidate{ID: "luks:" + node.UUID, Encrypted: true, EncryptionUUID: node.UUID, Size: node.Size, Reason: "encrypted-partition", device: node.Name}
			fingerprint(&c, disks)
			out = append(out, c)
		})
	}
	for uuid, disks := range btrfs {
		if registered[normalizeUUID(uuid)] {
			continue
		}
		node := btrfsDevices[uuid]
		c := ImportCandidate{ID: "btrfs:" + uuid, Filesystem: "btrfs", FilesystemUUID: uuid, Level: "raid1", Size: node.Size, device: node.Name}
		if node.Type == "crypt" {
			// Opened by someone else; after a restart TitanOS could not
			// open it again.
			c.Reason = "encrypted-partition"
		}
		// The profile is read in the preview; any of single, RAID1 and
		// RAID1C3 is taken over as long as every disk is there.
		if c.Reason == "" && (len(disks) == 0 || btrfsCounts[uuid] != len(disks) || node.Mountpoint != "") {
			c.Reason = "requires-unmounted-complete-btrfs"
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

// openImport opens the LUKS container of an encrypted candidate under
// mapper and fills in the file system found inside.
func (m *Manager) openImport(ctx context.Context, c *ImportCandidate, password []byte, mapper string, readOnly bool) error {
	if len(password) == 0 {
		return ErrPasswordRequired
	}
	key := "import:" + c.ID
	if err := m.checkAttempt(key); err != nil {
		return err
	}
	err := m.luksOpen(ctx, password, c.device, mapper, readOnly)
	m.noteAttempt(key, err)
	if err != nil {
		return err
	}
	device := m.mapperPath(mapper)
	fsType, _ := m.runner.Run(ctx, "", "blkid", "--match-tag", "TYPE", "--output", "value", device)
	uuid, _ := m.runner.Run(ctx, "", "blkid", "--match-tag", "UUID", "--output", "value", device)
	c.Filesystem, c.FilesystemUUID = strings.TrimSpace(fsType), strings.TrimSpace(uuid)
	if !slices.Contains([]string{"ext4", "xfs"}, c.Filesystem) || !validUUID(c.FilesystemUUID) {
		_ = m.luksClose(ctx, mapper) // cleanup, best effort: nothing more can be done if it fails
		return fmt.Errorf("%w: unsupported-filesystem", ErrNotPossible)
	}
	c.device = device
	return nil
}

// importMapper is the temporary name of an encrypted candidate's preview.
func importMapper(c ImportCandidate) string {
	short := strings.ReplaceAll(c.EncryptionUUID, "-", "")
	return "titanos-import-" + short[:min(8, len(short))]
}

func (m *Manager) previewImport(ctx context.Context, c ImportCandidate, password []byte) (preview ImportPreview, err error) {
	if c.Encrypted {
		mapper := importMapper(c)
		if err := m.openImport(ctx, &c, password, mapper, true); err != nil {
			return ImportPreview{Candidate: c, Folders: []string{}, ReadOnly: true}, err
		}
		defer func() { _ = m.luksClose(context.WithoutCancel(ctx), mapper) }() // cleanup, best effort: nothing more can be done if it fails
	}
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
		profile := importedProfile(output)
		if profile == "" {
			return preview, fmt.Errorf("%w: only single, RAID1 and RAID1C3 profiles are supported", ErrInvalidLevel)
		}
		preview.Candidate.Level = profile
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
	password := []byte(req.Password)
	defer wipe(password)
	return m.previewImport(ctx, c, password)
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
	password := []byte(req.Password)
	defer wipe(password)
	preview, err := m.previewImport(ctx, c, password)
	if err != nil {
		return err
	}
	// Registration writes only UUID configuration. No formatting, relabeling, folder creation or account import.
	v := VolumeConfig{Name: req.Name, Filesystem: preview.Candidate.Filesystem, FilesystemUUID: preview.Candidate.FilesystemUUID, ArrayUUID: c.ArrayUUID, Level: preview.Candidate.Level, Created: time.Now().UTC()}
	if c.Encrypted {
		// Opened for good under the volume's own name and mounted from
		// there; from now on it is locked after every start.
		v.Encryption = &EncryptionConfig{UUIDs: []string{c.EncryptionUUID}}
		if err := m.openImport(ctx, &c, password, cryptName(v, ""), false); err != nil {
			return err
		}
		if c.FilesystemUUID != v.FilesystemUUID {
			_ = m.luksClose(ctx, cryptName(v, "")) // cleanup, best effort: nothing more can be done if it fails
			return fmt.Errorf("%w: device identity changed", ErrDiskUnavailable)
		}
	}
	if c.Filesystem == "btrfs" {
		v.Profile, v.Devices = preview.Candidate.Level, len(c.Disks)
	}
	if err := m.saveVolumes(append(m.configured(), v)); err != nil {
		return err
	}
	return m.mount(ctx, v)
}

// importedProfile maps `btrfs filesystem df` to a TitanOS profile, or ""
// for layouts TitanOS does not manage (RAID0, RAID5/6, RAID10, mixed).
func importedProfile(df string) string {
	profile := func(kind string) string {
		found := ""
		for line := range strings.SplitSeq(df, "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), kind+", "); ok {
				p, _, _ := strings.Cut(rest, ":")
				p = strings.ToLower(p)
				if found != "" && found != p {
					return "mixed"
				}
				found = p
			}
		}
		return found
	}
	data, meta := profile("Data"), profile("Metadata")
	switch {
	case data == "single" && (meta == "dup" || meta == "single"):
		return "single"
	case data == "raid1" && meta == "raid1", data == "raid1c3" && meta == "raid1c3":
		return data
	}
	return ""
}
