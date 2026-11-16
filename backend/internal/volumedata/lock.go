package volumedata

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Share takes a shared lock (flock) on the folder of root until the
// returned function is called. Work that leaves files of its own in a
// private folder holds one while it runs; a sweep of what interrupted work
// left takes the folder exclusively (TryExclusive) and so never removes
// what running work writes. The lock belongs to its own descriptor, so it
// also keeps work of the same process apart.
func Share(root *os.Root) (func(), error) {
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// TryExclusive takes the folder of root exclusively if no work holds it;
// ok is false while some does.
func TryExclusive(root *os.Root) (release func(), ok bool, err error) {
	f, err := root.Open(".")
	if err != nil {
		return nil, false, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close() }, true, nil
}
