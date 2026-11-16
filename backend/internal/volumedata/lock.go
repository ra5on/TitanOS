package volumedata

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// ErrHeld: a sweep holds the folder right now (TryExclusive); the work
// that asked can be tried again shortly.
var ErrHeld = errors.New("volumedata: leftovers in the folder are being removed; try again shortly")

// Share takes a shared lock (flock) on the folder of root until the
// returned function is called. It never waits: while a sweep holds the
// folder it returns ErrHeld, so that a caller holding other locks is not
// held up by the walk of a sweep. Work that leaves files of its own in a
// private folder holds one while it runs; a sweep of what interrupted work
// left takes the folder exclusively (TryExclusive) and so never removes
// what running work writes. The lock belongs to its own descriptor, so it
// also keeps work of the same process apart.
func Share(root *os.Root) (func(), error) {
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrHeld
		}
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
