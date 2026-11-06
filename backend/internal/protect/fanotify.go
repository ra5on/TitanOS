package protect

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Source delivers the file events of the mounted data volumes until ctx is
// done. Tests use a fake; titand uses Fanotify.
type Source interface {
	Watch(ctx context.Context, mounts []string, out chan<- Event) error
}

// ErrOverflow is reported through Fanotify.OnOverflow when the kernel
// dropped events because titand did not read fast enough.
var ErrOverflow = errors.New("fanotify: event queue overflowed")

// Fanotify watches whole file systems with Linux fanotify (titand runs as
// root): every close after writing, every rename and every new file, with
// the folder and name of the file (FAN_REPORT_DFID_NAME). The kernel keeps
// no file open for it and reads nothing from the disk; only the folder of
// an event is looked up by its handle, which is in memory right after the
// write.
type Fanotify struct {
	// OnOverflow is told when events were lost (may be nil).
	OnOverflow func()
}

const fanMask = unix.FAN_CLOSE_WRITE | unix.FAN_RENAME | unix.FAN_CREATE

// Watch marks the file systems of mounts and sends their events to out.
func (f Fanotify) Watch(ctx context.Context, mounts []string, out chan<- Event) error {
	fd, err := unix.FanotifyInit(unix.FAN_CLASS_NOTIF|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK|unix.FAN_REPORT_DFID_NAME, unix.O_RDONLY|unix.O_LARGEFILE)
	if err != nil {
		return fmt.Errorf("fanotify_init: %w", err)
	}
	defer unix.Close(fd)
	mountFds := map[[2]int32]int{}
	defer func() {
		for _, m := range mountFds {
			unix.Close(m)
		}
	}()
	marked := 0
	var lastErr error
	for _, mount := range mounts {
		var st unix.Statfs_t
		if err := unix.Statfs(mount, &st); err != nil {
			lastErr = err
			continue
		}
		if err := unix.FanotifyMark(fd, unix.FAN_MARK_ADD|unix.FAN_MARK_FILESYSTEM, fanMask, unix.AT_FDCWD, mount); err != nil {
			lastErr = fmt.Errorf("fanotify_mark %s: %w", mount, err)
			continue
		}
		fsid := [2]int32{st.Fsid.Val[0], st.Fsid.Val[1]}
		if _, ok := mountFds[fsid]; !ok {
			mfd, err := unix.Open(mount, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err != nil {
				lastErr = err
				continue
			}
			mountFds[fsid] = mfd
		}
		marked++
	}
	if marked == 0 {
		if lastErr == nil {
			lastErr = errors.New("no volume to watch")
		}
		return lastErr
	}
	buf := make([]byte, 256<<10)
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for ctx.Err() == nil {
		n, err := unix.Poll(poll, 1000)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return err
		}
		if n <= 0 {
			continue
		}
		n, err = unix.Read(fd, buf)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		now := time.Now()
		for _, raw := range ParseEvents(buf[:n]) {
			if raw.Mask&unix.FAN_Q_OVERFLOW != 0 {
				if f.OnOverflow != nil {
					f.OnOverflow()
				}
				continue
			}
			ev, ok := raw.resolve(mountFds)
			if !ok {
				continue
			}
			ev.At = now
			select {
			case out <- ev:
			case <-ctx.Done():
				return nil
			}
		}
	}
	return nil
}

// RawInfo is one file record of an event: the folder (by handle) and name.
type RawInfo struct {
	Type       uint8
	Fsid       [2]int32
	HandleType int32
	Handle     []byte
	Name       string
}

// RawEvent is an event as the kernel reports it.
type RawEvent struct {
	Mask  uint64
	PID   int32
	Infos []RawInfo
}

var metaSize = int(unsafe.Sizeof(unix.FanotifyEventMetadata{}))

// ParseEvents reads the events of a buffer filled by read(2). Broken
// records end the parsing; what came before is returned.
func ParseEvents(buf []byte) []RawEvent {
	var out []RawEvent
	le := binary.NativeEndian
	for len(buf) >= metaSize {
		evLen := int(le.Uint32(buf[0:4]))
		metaLen := int(le.Uint16(buf[6:8]))
		if evLen < metaSize || evLen > len(buf) || metaLen < metaSize || metaLen > evLen || buf[4] != unix.FANOTIFY_METADATA_VERSION {
			break
		}
		ev := RawEvent{Mask: le.Uint64(buf[8:16]), PID: int32(le.Uint32(buf[20:24]))}
		info := buf[metaLen:evLen]
		for len(info) >= 4 {
			typ, l := info[0], int(le.Uint16(info[2:4]))
			if l < 4 || l > len(info) {
				break
			}
			rec := info[:l]
			info = info[l:]
			// header(4) fsid(8) handle_bytes(4) handle_type(4) f_handle name\0
			if len(rec) < 20 {
				continue
			}
			ri := RawInfo{Type: typ, Fsid: [2]int32{int32(le.Uint32(rec[4:8])), int32(le.Uint32(rec[8:12]))}}
			hb := int(le.Uint32(rec[12:16]))
			ri.HandleType = int32(le.Uint32(rec[16:20]))
			if 20+hb > len(rec) {
				continue
			}
			ri.Handle = append([]byte(nil), rec[20:20+hb]...)
			name := rec[20+hb:]
			for i, b := range name {
				if b == 0 {
					name = name[:i]
					break
				}
			}
			ri.Name = string(name)
			ev.Infos = append(ev.Infos, ri)
		}
		out = append(out, ev)
		buf = buf[evLen:]
	}
	return out
}

func (raw RawEvent) resolve(mountFds map[[2]int32]int) (Event, bool) {
	ev := Event{PID: int(raw.PID)}
	switch {
	case raw.Mask&unix.FAN_RENAME != 0:
		ev.Op = OpRename
	case raw.Mask&unix.FAN_CLOSE_WRITE != 0:
		ev.Op = OpWrite
	case raw.Mask&unix.FAN_CREATE != 0:
		ev.Op = OpCreate
	default:
		return ev, false
	}
	if raw.Mask&unix.FAN_ONDIR != 0 {
		return ev, false
	}
	for _, info := range raw.Infos {
		dir, ok := lookupDir(mountFds, info)
		if !ok || info.Name == "" {
			continue
		}
		p := filepath.Join(dir, info.Name)
		switch info.Type {
		case unix.FAN_EVENT_INFO_TYPE_OLD_DFID_NAME:
			ev.From = p
		case unix.FAN_EVENT_INFO_TYPE_NEW_DFID_NAME, unix.FAN_EVENT_INFO_TYPE_DFID_NAME:
			ev.Path = p
		}
	}
	if ev.Path == "" || ev.Op == OpRename && ev.From == "" {
		return ev, false
	}
	return ev, true
}

func lookupDir(mountFds map[[2]int32]int, info RawInfo) (string, bool) {
	mfd, ok := mountFds[info.Fsid]
	if !ok {
		return "", false
	}
	fd, err := unix.OpenByHandleAt(mfd, unix.NewFileHandle(info.HandleType, info.Handle), unix.O_PATH|unix.O_CLOEXEC)
	if err != nil {
		return "", false
	}
	defer unix.Close(fd)
	dir, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
	if err != nil || !filepath.IsAbs(dir) {
		return "", false
	}
	return dir, true
}

// ProcessName is the command of a process, e.g. "smbd" or "nfsd".
func ProcessName(pid int) string {
	if pid <= 0 {
		return ""
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return ""
	}
	return string(trimNL(b))
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
