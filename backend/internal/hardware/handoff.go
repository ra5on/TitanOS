package hardware

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ErrInUse means a program of the NAS still has the graphics device open.
// Taking the device from its driver underneath it can hang the whole NAS.
var ErrInUse = errors.New("hardware: the device is in use on the NAS")

// Users returns the programs of the NAS that have a DRM device of one of the
// given addresses open, as "name (pid)". The kernel names the PCI address of
// every DRM client in its fdinfo (drm-pdev), whatever node it opened: card,
// render node, inside or outside a container.
func (m *Manager) Users(addresses []string) ([]string, error) {
	pids, err := os.ReadDir(m.proc)
	if err != nil {
		return nil, err
	}
	self := strconv.Itoa(os.Getpid())
	var users []string
	for _, pid := range pids {
		if _, err := strconv.ParseUint(pid.Name(), 10, 32); err != nil || pid.Name() == self {
			continue
		}
		folder := filepath.Join(m.proc, pid.Name(), "fdinfo")
		files, err := os.ReadDir(folder)
		if errors.Is(err, os.ErrPermission) {
			return nil, err
		}
		for _, file := range files {
			data, err := os.ReadFile(filepath.Join(folder, file.Name()))
			if err != nil || len(data) > 65536 {
				continue
			}
			if _, client := parseDRMClient(string(data)); client.address == "" || !slices.Contains(addresses, client.address) {
				continue
			}
			name := m.read(m.proc, pid.Name(), "comm")
			if name == "" {
				name = "?"
			}
			users = append(users, fmt.Sprintf("%s (%s)", name, pid.Name()))
			break
		}
	}
	sort.Strings(users)
	return users, nil
}

// framebufferConsoles returns the virtual consoles drawn by fbcon, the text
// console on the firmware's display.
func (m *Manager) framebufferConsoles() []string {
	consoles, _ := filepath.Glob(filepath.Join(m.sys, "class/vtconsole/vtcon*"))
	var found []string
	for _, console := range consoles {
		if strings.Contains(m.read(console, "name"), "frame buffer") {
			found = append(found, console)
		}
	}
	return found
}

// PrepareHandoff makes sure the PCI devices can be taken from the NAS
// without bringing it down: no program may still use them, and the text
// console leaves the display the firmware started on. It returns whether it
// detached the console.
func (m *Manager) PrepareHandoff(pci []string) (bool, error) {
	status := m.Status()
	var graphics []string
	primary := false
	for _, device := range status.PCI {
		if !slices.Contains(pci, device.Address) || device.Kind == "npu" || device.Driver == "vfio-pci" {
			continue
		}
		graphics = append(graphics, device.Address)
		primary = primary || device.Primary
	}
	if len(graphics) == 0 {
		return false, nil
	}
	users, err := m.Users(graphics)
	if err != nil {
		return false, fmt.Errorf("%w: cannot tell who uses %s: %v", ErrInUse, strings.Join(graphics, ", "), err)
	}
	if len(users) > 0 {
		return false, fmt.Errorf("%w: %s", ErrInUse, strings.Join(users, ", "))
	}
	if !primary {
		return false, nil
	}
	detached := false
	for _, console := range m.framebufferConsoles() {
		if m.read(console, "bind") != "1" {
			continue
		}
		if err := os.WriteFile(filepath.Join(console, "bind"), []byte("0"), 0); err != nil {
			m.RestoreConsole()
			return false, fmt.Errorf("%w: detach the console from %s: %v", ErrInUse, strings.Join(graphics, ", "), err)
		}
		detached = true
	}
	if detached {
		// Only what TitanOS took is given back later.
		if err := os.MkdirAll(m.state, 0o755); err == nil {
			_ = os.WriteFile(m.consoleNote(), nil, 0o644) // marks what was taken, best effort: without it the console is simply not given back
		}
	}
	return detached, nil
}

// consoleNote marks that TitanOS detached the text console.
func (m *Manager) consoleNote() string {
	return filepath.Join(m.state, "console-detached")
}

// RestoreConsole gives the text console back to the display once no VM has
// the device it lives on. It does nothing while one still does, and nothing
// at all if TitanOS did not detach the console itself. Callers keep it apart
// from a VM start (see titand's main), which detaches it again.
func (m *Manager) RestoreConsole() {
	if _, err := os.Stat(m.consoleNote()); err != nil {
		return
	}
	for _, device := range m.Status().PCI {
		if device.Primary && device.Driver == "vfio-pci" {
			return
		}
	}
	for _, console := range m.framebufferConsoles() {
		if m.read(console, "bind") == "0" {
			_ = os.WriteFile(filepath.Join(console, "bind"), []byte("1"), 0) // giving the console back as far as it goes; nothing else is left to try
		}
	}
	os.Remove(m.consoleNote())
}
