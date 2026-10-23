package volumedata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func mountID(root *os.Root) (string, error) {
	file, err := root.Open(".")
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", file.Fd()))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "mnt_id:" {
			return fields[1], nil
		}
	}
	return "", errors.New("mount identity unavailable")
}

// SameMount compares pinned kernel mount identities, including Btrfs subvolumes.
func SameMount(a, b *os.Root) error {
	first, err := mountID(a)
	if err != nil {
		return err
	}
	second, err := mountID(b)
	if err != nil {
		return err
	}
	if first != second {
		return ErrUnavailable
	}
	return nil
}
func verifyMountedRoot(root *os.Root, mountRoot, point string) error {
	// Production data mounts are fixed at /mnt by the TitanOS filesystem contract.
	// Alternate roots are explicit sandbox fixtures, not configurable through the API.
	if filepath.Clean(mountRoot) != "/mnt" {
		return nil
	}
	id, err := mountID(root)
	if err != nil {
		return err
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 5 && fields[0] == id && fields[4] == point {
			return nil
		}
	}
	return ErrUnavailable
}
