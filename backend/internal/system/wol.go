package system

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Wake-on-LAN through the ethtool interface of the kernel, without the
// ethtool program. The setting lasts until the card is reset; titand sets it
// at every start and change.

const (
	ethtoolGWOL = 0x5
	ethtoolSWOL = 0x6
	wakeMagic   = 1 << 5 // WAKE_MAGIC
)

type ethtoolWolinfo struct {
	cmd       uint32
	supported uint32
	wolopts   uint32
	sopass    [6]byte
}

type ifreqData struct {
	name [unix.IFNAMSIZ]byte
	data uintptr
}

func ethtoolWOL(name string, info *ethtoolWolinfo) error {
	if len(name) >= unix.IFNAMSIZ {
		return errors.New("interface name too long")
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var req ifreqData
	copy(req.name[:], name)
	req.data = uintptr(unsafe.Pointer(info))
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), unix.SIOCETHTOOL, uintptr(unsafe.Pointer(&req)))
	runtime.KeepAlive(info)
	if errno != 0 {
		return errno
	}
	return nil
}

// getWakeOnLAN says whether a card can wake by magic packet and whether it is on.
func getWakeOnLAN(name string) (supported, enabled bool, err error) {
	info := ethtoolWolinfo{cmd: ethtoolGWOL}
	if err := ethtoolWOL(name, &info); err != nil {
		return false, false, err
	}
	return info.supported&wakeMagic != 0, info.wolopts&wakeMagic != 0, nil
}

// setWakeOnLAN switches waking by magic packet on or off.
func setWakeOnLAN(name string, enable bool) error {
	info := ethtoolWolinfo{cmd: ethtoolGWOL}
	if err := ethtoolWOL(name, &info); err != nil {
		return err
	}
	if info.supported&wakeMagic == 0 {
		return errors.New("not supported by this network card")
	}
	opts := info.wolopts &^ wakeMagic
	if enable {
		opts |= wakeMagic
	}
	set := ethtoolWolinfo{cmd: ethtoolSWOL, wolopts: opts}
	return ethtoolWOL(name, &set)
}
