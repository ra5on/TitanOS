package quota

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

// The structs must match the kernel ABI byte for byte.
func TestKernelStructSizes(t *testing.T) {
	if s := unsafe.Sizeof(fsDiskQuota{}); s != 112 {
		t.Fatalf("fs_disk_quota is %d bytes", s)
	}
	if s := unsafe.Sizeof(fsxattr{}); s != 28 {
		t.Fatalf("fsxattr is %d bytes", s)
	}
	if qcmd(qXSetQLim) != 0x580402 || qcmd(qGetInfo) != 0x80000502 {
		t.Fatalf("%#x %#x", qcmd(qXSetQLim), qcmd(qGetInfo))
	}
}

// A temporary directory has no project quotas; nothing may be reported
// as active and the calls must fail cleanly instead of crashing.
func TestInactiveFileSystem(t *testing.T) {
	dir := t.TempDir()
	if active, err := Active(dir); err != nil || active {
		t.Fatalf("%v %v", active, err)
	}
	if err := SetLimit(dir, 10001, 1<<20); err == nil {
		t.Fatal("limit accepted without quotas")
	}
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0600)
	os.Symlink("/etc/passwd", filepath.Join(dir, "link"))
	// tmpfs and most test file systems have no project IDs: Assign reports
	// that instead of touching the symlink target.
	if err := Assign(dir, 10001); err == nil {
		if id, _, err := Project(dir); err != nil || id != 10001 {
			t.Fatalf("%d %v", id, err)
		}
	}
}
