package fileindex

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ioCounter reads how many requests the block devices under a mount point
// have completed, from /proc and /sys only: reading it never touches a disk.
// For an md array the member partitions are counted, for other filesystems
// the device they are mounted from. ok is false when that is unknown.
type ioCounter struct {
	proc, sys string
}

func (c ioCounter) read(mountPoint string) (uint64, bool) {
	data, err := os.ReadFile(filepath.Join(c.proc, "self/mountinfo"))
	if err != nil {
		return 0, false
	}
	source := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || unescape(fields[4]) != mountPoint {
			continue
		}
		for i, field := range fields {
			if field == "-" && i+2 < len(fields) {
				source = fields[i+2]
			}
		}
	}
	if !strings.HasPrefix(source, "/dev/") {
		return 0, false
	}
	if resolved, err := filepath.EvalSymlinks(source); err == nil {
		source = resolved
	}
	name := filepath.Base(source)
	devices := []string{name}
	if slaves, err := os.ReadDir(filepath.Join(c.sys, "class/block", name, "slaves")); err == nil && len(slaves) > 0 {
		devices = devices[:0]
		for _, slave := range slaves {
			devices = append(devices, slave.Name())
		}
	}
	var total uint64
	for _, device := range devices {
		stat, err := os.ReadFile(filepath.Join(c.sys, "class/block", device, "stat"))
		if err != nil {
			return 0, false
		}
		fields := strings.Fields(string(stat))
		if len(fields) < 8 {
			return 0, false
		}
		// Completed reads and writes, plus discards and flushes where known.
		for _, i := range []int{0, 4, 11, 15} {
			if i < len(fields) {
				n, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					return 0, false
				}
				total += n
			}
		}
	}
	return total, true
}

// unescape undoes the octal escapes of /proc/self/mountinfo (space = \040).
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				out.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		out.WriteByte(s[i])
	}
	return out.String()
}
