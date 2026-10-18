package files

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"strings"
)

// Atomic replacement must keep existing share ACLs and user metadata. FAT and
// exFAT do not have these attributes; their mount-wide permissions stay intact.
func copyTextMetadata(source, target *os.File) error {
	size, err := unix.Flistxattr(int(source.Fd()), nil)
	if errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		return err
	}
	names := make([]byte, size)
	size, err = unix.Flistxattr(int(source.Fd()), names)
	if err != nil {
		return err
	}
	for _, name := range strings.Split(string(names[:size]), "\x00") {
		if !strings.HasPrefix(name, "user.") && name != "system.posix_acl_access" {
			continue
		}
		size, err := unix.Fgetxattr(int(source.Fd()), name, nil)
		if err != nil {
			return err
		}
		value := make([]byte, size)
		size, err = unix.Fgetxattr(int(source.Fd()), name, value)
		if err != nil {
			return err
		}
		if err = unix.Fsetxattr(int(target.Fd()), name, value[:size], 0); err != nil {
			return err
		}
	}
	return nil
}
