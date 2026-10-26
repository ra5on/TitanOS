package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func diskIdentity(d Disk) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", d.Name, d.Model, d.Serial, d.SizeBytes))))
}
func diskUnavailable(d Disk) string {
	switch {
	case d.Role == RoleStick:
		return "stick"
	case d.Role == RoleMember:
		return "volume"
	case d.Mounted:
		return "mounted"
	case d.Testing:
		return "testing"
	case d.SizeBytes < minDiskBytes:
		return "small"
	}
	return ""
}

// Plan every release before stopping anything. An unmounted foreign md array
// can only be stopped when ALL its present disks were explicitly selected.
// LVM/crypt/device-mapper, mounted filesystems and unknown holders are refused.
func (m *Manager) oldArrays(devices []blockDevice, selected []string) ([]string, error) {
	selectedSet := map[string]bool{}
	for _, name := range selected {
		selectedSet[name] = true
	}
	arrays := map[string]bool{}
	owners := map[string]map[string]bool{}
	disksOf := map[string]string{}
	var problem error
	for _, disk := range devices {
		if !isDisk(disk) {
			continue
		}
		name := strings.TrimPrefix(disk.Name, "/dev/")
		disk.walk(func(node blockDevice) {
			if node.Type == "disk" || node.Type == "part" {
				disksOf[filepath.Base(node.Name)] = name
			}
			if isArray(node) {
				for _, child := range node.Children {
					if selectedSet[name] && isArray(child) {
						problem = fmt.Errorf("%w: nested array %s must be released manually", ErrDiskUnavailable, node.Name)
					}
				}
				if owners[node.Name] == nil {
					owners[node.Name] = map[string]bool{}
				}
				owners[node.Name][name] = true
				if selectedSet[name] {
					arrays[node.Name] = true
				}
			}
			if !selectedSet[name] {
				return
			}
			if node.Mountpoint != "" {
				problem = fmt.Errorf("%w: %s is mounted at %s; unmount it before erasing", ErrDiskUnavailable, node.Name, node.Mountpoint)
			}
			if node.Type != "disk" && node.Type != "part" && !isArray(node) {
				problem = fmt.Errorf("%w: %s is used by %s (%s)", ErrDiskUnavailable, disk.Name, node.Name, node.Type)
			}
		})
	}
	if problem != nil {
		return nil, problem
	}
	for array := range arrays {
		slaves, err := os.ReadDir(filepath.Join(m.sysBlock, filepath.Base(array), "slaves"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, slave := range slaves {
			if !selectedSet[disksOf[slave.Name()]] {
				return nil, fmt.Errorf("%w: old array %s also uses %s; select all of its disks to erase it", ErrDiskUnavailable, array, slave.Name())
			}
		}
		for disk := range owners[array] {
			if !selectedSet[disk] {
				return nil, fmt.Errorf("%w: old array %s also uses %s; select all of its disks to erase it", ErrDiskUnavailable, array, disk)
			}
		}
	}
	// sysfs covers consumers which are absent from lsblk's tree (and races).
	for _, disk := range devices {
		if !selectedSet[strings.TrimPrefix(disk.Name, "/dev/")] {
			continue
		}
		disk.walk(func(node blockDevice) {
			if problem != nil {
				return
			}
			holderPath := filepath.Join(m.sysBlock, filepath.Base(node.Name), "holders")
			if node.Type == "part" {
				holderPath = filepath.Join(m.sysBlock, filepath.Base(disk.Name), filepath.Base(node.Name), "holders")
			}
			holders, err := os.ReadDir(holderPath)
			if err != nil && !os.IsNotExist(err) {
				problem = err
				return
			}
			for _, holder := range holders {
				if !arrays["/dev/"+holder.Name()] {
					problem = fmt.Errorf("%w: %s is still used by %s", ErrDiskUnavailable, node.Name, holder.Name())
				}
			}
		})
	}
	if problem != nil {
		return nil, problem
	}
	result := []string{}
	for array := range arrays {
		result = append(result, array)
	}
	sort.Strings(result)
	return result, nil
}
func (m *Manager) releaseOldArrays(ctx context.Context, devices []blockDevice, selected []string) ([]blockDevice, error) {
	arrays, err := m.oldArrays(devices, selected)
	if err != nil {
		return nil, err
	}
	if len(arrays) == 0 {
		return devices, nil
	}
	for _, array := range arrays {
		if _, err := m.runner.Run(ctx, "", "mdadm", "--stop", array); err != nil {
			return nil, fmt.Errorf("release old array %s: %w", array, err)
		}
	}
	current, err := m.blockDevices(ctx)
	if err != nil {
		return nil, err
	}
	status := m.status(current, m.arrays(ctx))
	for _, name := range selected {
		before, _ := findDevice(devices, name)
		after, found := findDevice(current, name)
		if !found || before.Serial != after.Serial || before.Size != after.Size || before.Model != after.Model {
			return nil, fmt.Errorf("%w: identity of %s changed", ErrDiskUnavailable, name)
		}
		if _, err := m.usable(status, name); err != nil {
			return nil, err
		}
		remaining, err := m.oldArrays(current, []string{name})
		if err != nil {
			return nil, err
		}
		if len(remaining) > 0 {
			return nil, fmt.Errorf("%w: old array still active on %s", ErrDiskUnavailable, name)
		}
	}
	return current, nil
}

// checkIdentity refuses a disk that is gone or is no longer the one the
// caller picked. An empty identity (older clients) checks only presence.
func checkIdentity(devices []blockDevice, name, identity string) error {
	device, found := findDevice(devices, name)
	if !found {
		return fmt.Errorf("%w: %s not found", ErrDiskUnavailable, name)
	}
	if identity != "" && diskIdentity(classify(device, nil)) != identity {
		return fmt.Errorf("%w: %s changed since selection; refresh the disk list", ErrDiskUnavailable, name)
	}
	return nil
}
func validateDiskIdentities(req CreateRequest, status Status) error {
	if len(req.Identities) == 0 {
		return nil
	} // Older API clients still get all live safety checks.
	for _, name := range req.Disks {
		index := slices.IndexFunc(status.Disks, func(d Disk) bool { return d.Name == name })
		if index < 0 || req.Identities[name] != diskIdentity(status.Disks[index]) {
			return fmt.Errorf("%w: %s changed since selection; refresh the disk list", ErrDiskUnavailable, name)
		}
	}
	return nil
}
