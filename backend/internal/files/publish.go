package files

import (
	"os"
	"path"

	"golang.org/x/sys/unix"
)

// publishNewFile atomically publishes a completed upload without overwriting an
// existing name. Unlike hard links, RENAME_NOREPLACE also works on FAT/exFAT.
// Resolve both parent directories through os.Root, then use basenames only so
// raw syscalls cannot follow a path outside the authorised root.
func publishNewFile(root *os.Root, from, to string) error {
	src, err := root.Open(path.Dir(from))
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := root.Open(path.Dir(to))
	if err != nil {
		return err
	}
	defer dst.Close()
	return unix.Renameat2(int(src.Fd()), path.Base(from), int(dst.Fd()), path.Base(to), unix.RENAME_NOREPLACE)
}
