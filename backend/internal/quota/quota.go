// Package quota sets project quotas on directories of ext4 and XFS volumes:
// every file below a directory carries its project ID, and the kernel stops
// writes once the project reaches its limit. It talks to the kernel directly
// (quotactl_fd and the fsxattr ioctls), so no quota tools are needed.
package quota

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ErrInactive means the file system has no project quotas switched on.
var ErrInactive = errors.New("project quotas are not active on this volume")

const (
	prjQuota    = 2
	subCmdShift = 8
	qGetInfo    = 0x800005
	qXGetQuota  = 'X'<<8 + 3
	qXSetQLim   = 'X'<<8 + 4

	fsDQuotVersion = 1
	fsProjQuota    = 2
	fsDQBSoft      = 1 << 2
	fsDQBHard      = 1 << 3

	fsIocFSGetXattr  = 0x801c581f
	fsIocFSSetXattr  = 0x401c5820
	fsXFlagProjInher = 0x200

	// basicBlock is the unit of fs_disk_quota block counts.
	basicBlock = 512
)

// fsDiskQuota is struct fs_disk_quota from linux/dqblk_xfs.h.
type fsDiskQuota struct {
	Version      int8
	Flags        int8
	FieldMask    uint16
	ID           uint32
	BlkHardLimit uint64
	BlkSoftLimit uint64
	InoHardLimit uint64
	InoSoftLimit uint64
	BCount       uint64
	ICount       uint64
	ITimer       int32
	BTimer       int32
	IWarns       uint16
	BWarns       uint16
	ITimerHi     int8
	BTimerHi     int8
	RtbTimerHi   int8
	Padding2     int8
	RtbHardLimit uint64
	RtbSoftLimit uint64
	RtbCount     uint64
	RtbTimer     int32
	RtbWarns     uint16
	Padding3     int16
	Padding4     [8]byte
}

// fsxattr is struct fsxattr from linux/fs.h.
type fsxattr struct {
	XFlags     uint32
	ExtSize    uint32
	NExtents   uint32
	ProjID     uint32
	CowExtSize uint32
	Pad        [8]byte
}

func qcmd(cmd int) uintptr { return uintptr(cmd<<subCmdShift | prjQuota) }

// quotactl runs quotactl_fd on the file system of the open directory.
func quotactl(dir *os.File, cmd int, id uint32, addr unsafe.Pointer) error {
	_, _, errno := unix.Syscall6(unix.SYS_QUOTACTL_FD, dir.Fd(), qcmd(cmd), uintptr(id), uintptr(addr), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func openDir(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Active says whether the file system holding mountPoint enforces project quotas.
func Active(mountPoint string) (bool, error) {
	dir, err := openDir(mountPoint)
	if err != nil {
		return false, err
	}
	defer dir.Close()
	var info [24]byte
	switch err := quotactl(dir, qGetInfo, 0, unsafe.Pointer(&info)); {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.ESRCH), errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL), errors.Is(err, unix.ENOTSUP):
		return false, nil
	default:
		return false, err
	}
}

// Usage is what a project holds and may hold; Limit 0 means no limit.
type Usage struct {
	Used  uint64 `json:"used"`
	Limit uint64 `json:"limit"`
	Files uint64 `json:"files"`
}

// Get reads the usage and limit of a project.
func Get(mountPoint string, id uint32) (Usage, error) {
	dir, err := openDir(mountPoint)
	if err != nil {
		return Usage{}, err
	}
	defer dir.Close()
	var q fsDiskQuota
	if err := quotactl(dir, qXGetQuota, id, unsafe.Pointer(&q)); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return Usage{}, ErrInactive
		}
		// A project without any file and limit is not there yet.
		if errors.Is(err, unix.ENOENT) {
			return Usage{}, nil
		}
		return Usage{}, err
	}
	return Usage{Used: q.BCount * basicBlock, Limit: q.BlkHardLimit * basicBlock, Files: q.ICount}, nil
}

// SetLimit sets the hard and soft block limit of a project; 0 removes it.
func SetLimit(mountPoint string, id uint32, bytes uint64) error {
	dir, err := openDir(mountPoint)
	if err != nil {
		return err
	}
	defer dir.Close()
	blocks := (bytes + basicBlock - 1) / basicBlock
	q := fsDiskQuota{Version: fsDQuotVersion, Flags: fsProjQuota, FieldMask: fsDQBHard | fsDQBSoft, ID: id, BlkHardLimit: blocks, BlkSoftLimit: blocks}
	if err := quotactl(dir, qXSetQLim, id, unsafe.Pointer(&q)); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return ErrInactive
		}
		return fmt.Errorf("set project quota %d: %w", id, err)
	}
	return nil
}

func getXattr(f *os.File) (fsxattr, error) {
	var x fsxattr
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), fsIocFSGetXattr, uintptr(unsafe.Pointer(&x)))
	if errno != 0 {
		return x, errno
	}
	return x, nil
}

func setXattr(f *os.File, x fsxattr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), fsIocFSSetXattr, uintptr(unsafe.Pointer(&x)))
	if errno != 0 {
		return errno
	}
	return nil
}

// Project returns the project ID of a directory and whether new files
// inherit it.
func Project(path string) (uint32, bool, error) {
	f, err := openDir(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	x, err := getXattr(f)
	return x.ProjID, x.XFlags&fsXFlagProjInher != 0, err
}

// Assign gives the directory and everything below it the project ID, so
// existing files count and new ones inherit it. It stays on the file
// system of dir, skips symlinks and special files and stops at the first
// error; running it again continues where it stopped.
func Assign(dir string, id uint32) error {
	var top syscall.Stat_t
	if err := syscall.Lstat(dir, &top); err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return nil
		}
		var st syscall.Stat_t
		if err := syscall.Lstat(path, &st); err != nil {
			return err
		}
		if st.Dev != top.Dev {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		flags := os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
		if d.IsDir() {
			flags |= syscall.O_DIRECTORY
		}
		f, err := os.OpenFile(path, flags, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		x, err := getXattr(f)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		want := x
		want.ProjID = id
		if d.IsDir() {
			want.XFlags |= fsXFlagProjInher
		}
		if want == x {
			return nil
		}
		if err := setXattr(f, want); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	})
}
