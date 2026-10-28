package plugins

import (
	"bytes"
	"context"
	"time"

	"golang.org/x/sys/unix"
)

// listenUevents calls event for every kernel uevent (the same the udev
// daemon gets) until ctx is done. Only ACTION and SUBSYSTEM are read.
func listenUevents(ctx context.Context, event func(action, subsystem string)) error {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: 1}); err != nil {
		return err
	}
	// Wake up now and then to notice ctx.
	timeout := unix.NsecToTimeval(time.Second.Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &timeout); err != nil {
		return err
	}
	buffer := make([]byte, 16*1024)
	for ctx.Err() == nil {
		n, _, err := unix.Recvfrom(fd, buffer, 0)
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		action, subsystem := parseUevent(buffer[:n])
		if action != "" {
			event(action, subsystem)
		}
	}
	return nil
}

// parseUevent reads "add@/devices/...\0ACTION=add\0SUBSYSTEM=pci\0...".
func parseUevent(message []byte) (action, subsystem string) {
	for _, field := range bytes.Split(message, []byte{0}) {
		if value, ok := bytes.CutPrefix(field, []byte("ACTION=")); ok {
			action = string(value)
		} else if value, ok := bytes.CutPrefix(field, []byte("SUBSYSTEM=")); ok {
			subsystem = string(value)
		}
	}
	return action, subsystem
}
