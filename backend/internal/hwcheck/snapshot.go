package hwcheck

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A snapshot is the part of sysfs, procfs and debugfs the hardware page
// reads, plus the kernel log of this start and the module aliases of the
// running kernel, as a tar.gz. Unpacked, it is a Checker's root: a reported
// machine becomes a test case (testdata/snapshots). It holds no serial
// numbers, MAC or IP addresses and no host name.

// snapshotAttributes are the device attributes that are copied; others
// (config space, ROMs, serials) never are.
var snapshotAttributes = map[string]bool{
	"vendor": true, "device": true, "class": true, "revision": true, "modalias": true,
	"subsystem_vendor": true, "subsystem_device": true, "idVendor": true, "idProduct": true,
	"bcdDevice": true, "bDeviceClass": true, "bInterfaceClass": true, "bInterfaceNumber": true,
	"product": true, "manufacturer": true, "speed": true, "type": true, "carrier": true,
	"size": true, "removable": true, "model": true, "name": true, "boot_vga": true,
}

var dmiAttributes = []string{"board_vendor", "board_name", "sys_vendor", "product_name", "bios_vendor", "bios_version", "bios_date"}

type snapshot struct {
	sys   string
	files map[string][]byte
	links map[string]string
	dirs  map[string]bool
}

func (s *snapshot) file(name string, data []byte) {
	s.files[filepath.ToSlash(name)] = data
}

// copyFile copies a small file of the real system, if it is there.
func (s *snapshot) copyFile(real, name string) {
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	data, err := readLimited(real, 64*1024)
	if err == nil {
		s.file(name, data)
	}
}

func readLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit))
}

// inSys maps a path of the real sysfs into the snapshot.
func (s *snapshot) inSys(real string) (string, bool) {
	rel, err := filepath.Rel(s.sys, real)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.Join("sys", rel), true
}

// link records a symlink of sysfs (bus/pci/devices/X, driver, device) with
// its target, and the device behind it.
func (s *snapshot) link(real string) {
	target, err := filepath.EvalSymlinks(real)
	if err != nil {
		return
	}
	from, ok := s.inSys(real)
	to, ok2 := s.inSys(target)
	if !ok || !ok2 {
		return
	}
	relative, err := filepath.Rel(filepath.Dir(from), to)
	if err != nil {
		return
	}
	s.links[filepath.ToSlash(from)] = filepath.ToSlash(relative)
	s.dirs[filepath.ToSlash(to)] = true
}

// device copies the attributes of a device and of the devices above it,
// and its driver link.
func (s *snapshot) device(real string) {
	real, err := filepath.EvalSymlinks(real)
	if err != nil {
		return
	}
	devices := filepath.Join(s.sys, "devices")
	for dir := real; strings.HasPrefix(dir, devices+string(filepath.Separator)); dir = filepath.Dir(dir) {
		name, _ := s.inSys(dir)
		if s.dirs[filepath.ToSlash(name)+"/."] {
			break // done before, with its parents
		}
		s.dirs[filepath.ToSlash(name)+"/."] = true
		s.dirs[filepath.ToSlash(name)] = true
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if snapshotAttributes[entry.Name()] {
				s.copyFile(filepath.Join(dir, entry.Name()), filepath.Join(name, entry.Name()))
			}
		}
		if _, err := os.Lstat(filepath.Join(dir, "driver")); err == nil {
			s.link(filepath.Join(dir, "driver"))
		}
		// The DRM nodes of a GPU (card0, renderD128).
		if nodes, err := os.ReadDir(filepath.Join(dir, "drm")); err == nil {
			for _, node := range nodes {
				s.dirs[filepath.ToSlash(filepath.Join(name, "drm", node.Name()))] = true
			}
		}
	}
}

// Snapshot writes the snapshot as tar.gz. version is the TitanOS version.
func (c *Checker) Snapshot(ctx context.Context, w io.Writer, version string) error {
	s := &snapshot{sys: c.Sys, files: map[string][]byte{}, links: map[string]string{}, dirs: map[string]bool{}}
	for _, bus := range []string{"pci", "usb"} {
		entries, _ := os.ReadDir(filepath.Join(c.Sys, "bus", bus, "devices"))
		for _, entry := range entries {
			path := filepath.Join(c.Sys, "bus", bus, "devices", entry.Name())
			s.link(path)
			s.device(path)
		}
	}
	nets, _ := os.ReadDir(filepath.Join(c.Sys, "class/net"))
	for _, entry := range nets {
		dir := filepath.Join(c.Sys, "class/net", entry.Name())
		name := filepath.Join("sys/class/net", entry.Name())
		s.dirs[filepath.ToSlash(name)] = true
		for _, attribute := range []string{"type", "carrier", "speed"} {
			s.copyFile(filepath.Join(dir, attribute), filepath.Join(name, attribute))
		}
		if _, err := os.Lstat(filepath.Join(dir, "device")); err == nil {
			s.link(filepath.Join(dir, "device"))
			s.device(filepath.Join(dir, "device"))
		}
	}
	blocks, _ := os.ReadDir(filepath.Join(c.Sys, "block"))
	for _, entry := range blocks {
		if !diskName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(c.Sys, "block", entry.Name())
		s.link(path)
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		name, _ := s.inSys(real)
		for _, attribute := range []string{"size", "removable", "queue/rotational", "queue/zoned"} {
			s.copyFile(filepath.Join(real, attribute), filepath.Join(name, attribute))
		}
		if _, err := os.Lstat(filepath.Join(real, "device")); err == nil {
			s.link(filepath.Join(real, "device"))
			s.device(filepath.Join(real, "device"))
		}
	}
	for _, attribute := range dmiAttributes {
		s.copyFile(filepath.Join(c.Sys, "class/dmi/id", attribute), filepath.Join("sys/class/dmi/id", attribute))
	}
	chips, _ := filepath.Glob(filepath.Join(c.Sys, "class/hwmon/hwmon*"))
	for _, chip := range chips {
		name := filepath.Join("sys/class/hwmon", filepath.Base(chip))
		s.copyFile(filepath.Join(chip, "name"), filepath.Join(name, "name"))
		inputs, _ := filepath.Glob(filepath.Join(chip, "*_input"))
		for _, input := range inputs {
			if base := filepath.Base(input); strings.HasPrefix(base, "temp") || strings.HasPrefix(base, "fan") {
				s.copyFile(input, filepath.Join(name, base))
			}
		}
	}
	for _, marker := range []string{"firmware/efi", "firmware/acpi/tables/DMAR", "firmware/acpi/tables/IVRS"} {
		if _, err := os.Stat(filepath.Join(c.Sys, marker)); err == nil {
			s.dirs[filepath.ToSlash(filepath.Join("sys", marker))] = true
		}
	}
	if groups, err := os.ReadDir(filepath.Join(c.Sys, "kernel/iommu_groups")); err == nil {
		for _, group := range groups {
			s.dirs["sys/kernel/iommu_groups/"+group.Name()] = true
		}
	}
	if _, err := os.Stat(filepath.Join(c.Dev, "kvm")); err == nil {
		s.file("dev/kvm", nil)
	}
	s.copyFile(filepath.Join(c.Sys, "module/i915/parameters/enable_guc"), "sys/module/i915/parameters/enable_guc")
	for _, name := range []string{"cpuinfo", "meminfo", "cmdline", "sys/kernel/osrelease"} {
		s.copyFile(filepath.Join(c.Proc, name), filepath.Join("proc", name))
	}
	if c.Debug != "" {
		ucs, _ := filepath.Glob(filepath.Join(c.Debug, "dri/*/gt*/uc"))
		for _, uc := range ucs {
			rel, _ := filepath.Rel(c.Debug, uc)
			for _, name := range []string{"huc_info", "guc_info", "usage"} {
				s.copyFile(filepath.Join(uc, name), filepath.Join("debug", rel, name))
			}
		}
	}
	release := c.kernelRelease()
	for _, name := range []string{"modules.alias", "modules.builtin.alias"} {
		if release == "" || c.Modules == "" {
			break
		}
		// Only the PCI and USB lines: what moduleAliases reads.
		if data, err := readLimited(filepath.Join(c.Modules, release, name), 32<<20); err == nil {
			var lines []string
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "alias pci:") || strings.HasPrefix(line, "alias usb:") {
					lines = append(lines, line)
				}
			}
			s.file(filepath.Join("modules", release, name), []byte(strings.Join(lines, "\n")+"\n"))
		}
	}
	// Which drivers are built into the kernel (names only).
	if data, err := readLimited(filepath.Join(c.Modules, release, "modules.builtin"), 4<<20); err == nil && release != "" {
		s.file(filepath.Join("modules", release, "modules.builtin"), data)
	}
	for _, dir := range c.Modprobe {
		confs, _ := filepath.Glob(filepath.Join(dir, "*.conf"))
		// /etc/modprobe.d and /usr/lib/modprobe.d both end in modprobe.d.
		from := strings.Trim(strings.ReplaceAll(filepath.Dir(dir), string(filepath.Separator), "-"), "-")
		for _, conf := range confs {
			s.copyFile(conf, filepath.Join("modprobe.d", from, filepath.Base(conf)))
		}
	}
	s.file("kernel.log", []byte(scrub(c.kernelLog(ctx))))
	meta, _ := json.MarshalIndent(map[string]string{"version": version, "kernel": release, "taken": time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	s.file("snapshot.json", append(meta, '\n'))
	return s.write(w)
}

var (
	macAddress  = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b`)
	ipv4Address = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// At least four groups, or "::": a PCI address (0000:00:0e.0) has three.
	ipv6Address = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){3,7}[0-9a-f]{1,4}\b|\b(?:[0-9a-f]{1,4}:)+:(?:[0-9a-f]{1,4}(?::[0-9a-f]{1,4})*)?|::(?:[0-9a-f]{1,4}:)*[0-9a-f]{1,4}\b`)
	serialLine  = regexp.MustCompile(`(?i)serial ?(number|no)?\s*[:=]`)
)

// scrub removes what identifies a machine or a network from the kernel log.
func scrub(log string) string {
	var lines []string
	for _, line := range strings.Split(log, "\n") {
		if serialLine.MatchString(line) {
			continue
		}
		line = macAddress.ReplaceAllString(line, "xx:xx:xx:xx:xx:xx")
		line = ipv6Address.ReplaceAllString(line, "<ipv6>")
		line = ipv4Address.ReplaceAllString(line, "<ipv4>")
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (s *snapshot) write(w io.Writer) error {
	compressed := gzip.NewWriter(w)
	archive := tar.NewWriter(compressed)
	now := time.Now()
	// Directories first (also those only made by files and links).
	dirs := map[string]bool{}
	addParents := func(path string) {
		for dir := filepath.ToSlash(filepath.Dir(path)); dir != "." && dir != "/"; dir = filepath.ToSlash(filepath.Dir(dir)) {
			dirs[dir] = true
		}
	}
	for dir := range s.dirs {
		if strings.HasSuffix(dir, "/.") {
			continue
		}
		dirs[dir] = true
		addParents(dir)
	}
	for name := range s.files {
		addParents(name)
	}
	for name := range s.links {
		addParents(name)
	}
	sorted := func(m map[string]bool) []string {
		keys := make([]string, 0, len(m))
		for key := range m {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys
	}
	for _, dir := range sorted(dirs) {
		if err := archive.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: dir + "/", Mode: 0o755, ModTime: now}); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(s.files))
	for name := range s.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := s.files[name]
		if err := archive.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: now}); err != nil {
			return err
		}
		if _, err := archive.Write(data); err != nil {
			return err
		}
	}
	links := make([]string, 0, len(s.links))
	for name := range s.links {
		links = append(links, name)
	}
	sort.Strings(links)
	for _, name := range links {
		if err := archive.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: name, Linkname: s.links[name], ModTime: now}); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

// OpenSnapshot returns a checker for an unpacked snapshot: its kernel log
// stands in for journalctl.
func OpenSnapshot(root string) *Checker {
	log, _ := os.ReadFile(filepath.Join(root, "kernel.log"))
	var modprobe []string
	dirs, _ := filepath.Glob(filepath.Join(root, "modprobe.d", "*"))
	modprobe = append(modprobe, dirs...)
	return &Checker{Sys: filepath.Join(root, "sys"), Proc: filepath.Join(root, "proc"), Dev: filepath.Join(root, "dev"),
		PCIIDs: filepath.Join(root, "pci.ids"), Modules: filepath.Join(root, "modules"), Modprobe: modprobe,
		Debug: filepath.Join(root, "debug"), Runner: snapshotLog(log)}
}

type snapshotLog []byte

func (log snapshotLog) Run(_ context.Context, _ string, name string, _ ...string) (string, error) {
	if name != "journalctl" {
		return "", os.ErrNotExist
	}
	return string(log), nil
}

// Unpack writes a snapshot tar.gz into dir (which must exist): regular
// files, directories and relative symlinks that stay inside dir only.
func Unpack(r io.Reader, dir string) error {
	compressed, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(header.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe path in snapshot: %s", header.Name)
		}
		target := filepath.Join(dir, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(archive, 16<<20))
			if err != nil {
				return err
			}
			if err := os.WriteFile(target, data, 0o644); err != nil {
				return err
			}
		case tar.TypeSymlink:
			resolved := filepath.Join(filepath.Dir(target), header.Linkname)
			if filepath.IsAbs(header.Linkname) || !strings.HasPrefix(resolved, filepath.Clean(dir)+string(filepath.Separator)) {
				return fmt.Errorf("unsafe link in snapshot: %s -> %s", header.Name, header.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
}
