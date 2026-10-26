package auth

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// The lock inode stays outside account directories, including during account
// deletion. Its descriptor is closed after each operation, so it cannot keep
// the configuration stick mounted while the NAS is idle.
func (u *Users) lockAccountFiles() (func(), error) {
	path := filepath.Join(u.store.Dir(), ".accounts.lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("auth: account lock is not a regular file")
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return func() { file.Close() }, nil
}
