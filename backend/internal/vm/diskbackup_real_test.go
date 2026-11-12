package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// TestRealLibvirtDiskBackup runs the backup of a running VM's disks
// against real libvirt and QEMU. It only runs where it is asked to
// (TITANOS_REAL_LIBVIRT=1, as root, in a disposable TitanOS guest:
// scripts/test-vm-diskbackup-qemu.py); never on a NAS in use. It makes one
// small VM without an operating system in TITANOS_REAL_LIBVIRT_DIR and
// removes it again.
func TestRealLibvirtDiskBackup(t *testing.T) {
	dir := os.Getenv("TITANOS_REAL_LIBVIRT_DIR")
	if os.Getenv("TITANOS_REAL_LIBVIRT") != "1" || dir == "" {
		t.Skip("needs real libvirt in a disposable guest (TITANOS_REAL_LIBVIRT=1, TITANOS_REAL_LIBVIRT_DIR)")
	}
	const name = "dbtest"
	ctx := context.Background()
	sh := func(command string, args ...string) string {
		t.Helper()
		out, err := exec.Command(command, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	folder := filepath.Join(dir, "real", "vms", name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, filepath.Join(dir, "real"), filepath.Join(dir, "real", "vms"), folder} {
		os.Chmod(d, 0o755)
	}
	base := filepath.Join(folder, "vdisk1.qcow2")
	sh("qemu-img", "create", "-q", "-f", "qcow2", base, "200M")
	// QEMU runs as its own user: it writes the image and makes the overlay
	// next to it (on a NAS the folder of a VM belongs to its group).
	os.Chmod(folder, 0o777)
	os.Chmod(base, 0o666)
	xml := fmt.Sprintf(`<domain type='qemu'><name>%s</name><uuid>11111111-2222-3333-4444-555555555555</uuid>
<memory unit='MiB'>128</memory><vcpu>1</vcpu><os><type arch='x86_64' machine='q35'>hvm</type></os>
<devices><emulator>/usr/bin/qemu-system-x86_64</emulator>
<disk type='file' device='disk'><driver name='qemu' type='qcow2' error_policy='stop'/><source file='%s'/><target dev='vda' bus='virtio'/></disk>
</devices></domain>`, name, base)
	domain := filepath.Join(dir, "domain.xml")
	os.WriteFile(domain, []byte(xml), 0o644)
	start := func() {
		t.Helper()
		sh("virsh", "--quiet", "create", "--file", domain)
	}
	exec.Command("virsh", "--quiet", "destroy", name).Run()
	start()
	t.Cleanup(func() { exec.Command("virsh", "--quiet", "destroy", name).Run() })

	manager := func() *Manager {
		// The group the image files are shared with QEMU through.
		m := NewManager(config.Open(filepath.Join(dir, "config")), lookupGroup("users", 0))
		m.mountRoot = dir
		m.Mounted = func() []string { return []string{"real"} }
		m.vms[name] = VM{Name: name, UUID: "11111111-2222-3333-4444-555555555555", Volume: "real", Template: "linux",
			Disks: []Disk{{File: "vdisk1.qcow2", Format: "qcow2", SizeBytes: 200 << 20, Volume: "real"}}}
		return m
	}
	source := func() string {
		t.Helper()
		for _, line := range strings.Split(sh("virsh", "--quiet", "domblklist", name), "\n") {
			if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "vda" {
				return strings.Join(fields[1:], " ")
			}
		}
		t.Fatal("no vda in domblklist")
		return ""
	}
	normal := func(m *Manager, what string) {
		t.Helper()
		if got := source(); got != base {
			t.Fatalf("%s: the VM runs on %s, not on its image", what, got)
		}
		if left, _ := filepath.Glob(base + ".*" + OverlaySuffix); len(left) != 0 {
			t.Fatalf("%s: overlay left: %v", what, left)
		}
		if _, err := os.Stat(m.diskBackupRecordPath(m.vms[name])); !os.IsNotExist(err) {
			t.Fatalf("%s: record left", what)
		}
		if state := strings.TrimSpace(sh("virsh", "--quiet", "domstate", name)); state != "running" {
			t.Fatalf("%s: VM is %q", what, state)
		}
		if out := sh("virsh", "blockjob", name, "vda", "--info"); !strings.Contains(out, "No current block job") && strings.TrimSpace(out) != "" {
			t.Fatalf("%s: block job left: %s", what, out)
		}
		sh("qemu-img", "check", "-U", "-q", "-f", "qcow2", base)
		t.Logf("PASS %s", what)
	}

	// 1. snapshot, copy, blockcommit with pivot.
	m := manager()
	b, err := m.BeginDiskBackup(ctx, name, DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := source(); !strings.HasSuffix(got, OverlaySuffix) {
		t.Fatalf("after the snapshot the VM runs on %s", got)
	}
	// The image stands still and can be read while the VM runs.
	sh("qemu-img", "convert", "-U", "-f", "qcow2", "-O", "qcow2", base, filepath.Join(dir, "copy.qcow2"))
	sh("qemu-img", "check", "-q", "-f", "qcow2", filepath.Join(dir, "copy.qcow2"))
	if err := b.End(); err != nil {
		t.Fatal(err)
	}
	normal(m, "snapshot, copy, commit and pivot (quiesced="+fmt.Sprint(b.Quiesced)+")")

	// 2. cancel around the moment the snapshot is taken.
	for _, delay := range []time.Duration{0, 5 * time.Millisecond, 30 * time.Millisecond, 100 * time.Millisecond, 300 * time.Millisecond} {
		run, cancel := context.WithCancel(ctx)
		go func() { time.Sleep(delay); cancel() }()
		b, err := m.BeginDiskBackup(run, name, DiskBackupOptions{Snapshot: true})
		if err == nil {
			err = b.End()
		} else if code(err) == DiskUndoFailed {
			t.Fatalf("cancel after %v left the VM undecided: %v", delay, err)
		}
		cancel()
		normal(m, fmt.Sprintf("cancel %v after the start (result: %v)", delay, err))
	}

	// 3. titand dies while the overlay is committed back: the block job
	// stays in QEMU; a new titand finishes it.
	b, err = m.BeginDiskBackup(ctx, name, DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	abandon(m, b)
	sh("virsh", "--quiet", "blockcommit", name, "vda", "--active") // as cut off: no --wait, no --pivot
	t.Logf("block job left behind: %s", strings.TrimSpace(sh("virsh", "blockjob", name, "vda", "--info")))
	m = manager()
	if err := m.RecoverDiskBackups(ctx, false); err != nil {
		t.Fatal(err)
	}
	normal(m, "titand restarted with a block job left in QEMU")

	// 4. titand dies while the VM runs on the overlay, nothing started yet.
	b, err = m.BeginDiskBackup(ctx, name, DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	abandon(m, b)
	m = manager()
	if err := m.RecoverDiskBackups(ctx, false); err != nil {
		t.Fatal(err)
	}
	normal(m, "titand restarted with the VM on the overlay")

	// 5. pause and resume, also after titand died while it was paused.
	b, err = m.BeginDiskBackup(ctx, name, DiskBackupOptions{Pause: true})
	if err != nil || b.Way != DiskWayPause {
		t.Fatalf("pause: %v", err)
	}
	if state := strings.TrimSpace(sh("virsh", "--quiet", "domstate", name)); state != "paused" {
		t.Fatalf("VM is %q while it should be paused", state)
	}
	if err := b.End(); err != nil {
		t.Fatal(err)
	}
	normal(m, "pause, copy, resume")
	b, err = m.BeginDiskBackup(ctx, name, DiskBackupOptions{Pause: true})
	if err != nil {
		t.Fatal(err)
	}
	abandon(m, b)
	m = manager()
	if err := m.RecoverDiskBackups(ctx, false); err != nil {
		t.Fatal(err)
	}
	normal(m, "titand restarted with the VM paused")

	// 6. the VM stops while it runs on the overlay (the NAS went down):
	// the overlay is committed into the image before anything else.
	b, err = m.BeginDiskBackup(ctx, name, DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	abandon(m, b)
	sh("virsh", "--quiet", "destroy", name)
	m = manager()
	if err := m.RecoverDiskBackups(ctx, false); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(base + ".*" + OverlaySuffix); len(left) != 0 {
		t.Fatalf("overlay of the stopped VM left: %v", left)
	}
	if info := sh("qemu-img", "info", "-f", "qcow2", base); strings.Contains(info, "backing file") {
		t.Fatalf("the image has a backing file now: %s", info)
	}
	sh("qemu-img", "check", "-q", "-f", "qcow2", base)
	start()
	normal(m, "VM stopped on the overlay, committed offline, started again")
}
