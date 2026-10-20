package files

import (
	"encoding/json"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const trashAttribute = "user.titanos.trash"

type trashMetadata struct {
	Original string    `json:"original"`
	Deleted  time.Time `json:"deleted"`
}

// Metadata travels with a deleted directory so children retain the deletion
// time without recursively changing their modification times. Unsupported
// filesystems and older/Samba entries keep the existing timestamp fallback.
func rememberTrash(r *os.Root, target, original string, now time.Time) {
	f, err := r.OpenFile(target, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return
	}
	data, _ := json.Marshal(trashMetadata{Original: original, Deleted: now})
	_ = unix.Fsetxattr(int(f.Fd()), trashAttribute, data, 0)
}

func trashDetails(r *os.Root, relative string, fallback time.Time) trashMetadata {
	_, original, _ := strings.Cut(relative, "/")
	result := trashMetadata{Original: original, Deleted: fallback}
	best := time.Time{}
	// Walk from this entry towards the account directory. A newly deleted
	// parent takes precedence over attributes left by an earlier restoration.
	for current := relative; strings.Contains(current, "/"); current = path.Dir(current) {
		f, err := r.OpenFile(path.Join(RecycleDir, current), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		data := make([]byte, 8192)
		n, err := unix.Fgetxattr(int(f.Fd()), trashAttribute, data)
		f.Close()
		if err != nil {
			continue
		}
		var saved trashMetadata
		if json.Unmarshal(data[:n], &saved) != nil || saved.Deleted.IsZero() || !saved.Deleted.After(best) {
			continue
		}
		cleaned, err := clean(saved.Original)
		if err != nil || cleaned == "." {
			continue
		}
		best = saved.Deleted
		result.Deleted = saved.Deleted
		suffix := strings.TrimPrefix(relative, current)
		result.Original = path.Join(cleaned, strings.TrimPrefix(suffix, "/"))
	}
	return result
}
