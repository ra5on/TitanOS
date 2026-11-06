package protect

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
)

// RestoreResult says what an in-place restore did.
type RestoreResult struct {
	Restored int      `json:"restored"`
	Recycled int      `json:"recycled"`
	Missing  int      `json:"missing"`
	Failed   []string `json:"failed,omitempty"`
}

// RecycleFolder is where the guard puts the encrypted files of a share
// (inside the share's recycle bin, which titand empties after its days).
const RecycleFolder = ".recycle/titanos-guard"

func safeRel(rel string) bool {
	if rel == "" || rel == "." || strings.HasPrefix(rel, "/") || path.Clean(rel) != rel || strings.ContainsAny(rel, "\x00\\") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." {
			return false
		}
	}
	return !Hidden(rel)
}

// Restore puts the affected files of a share back from a snapshot of it:
// what an attack left (the renamed or rewritten file) goes to the recycle
// bin, never deleted, and the file of the snapshot takes its old place.
// Files the snapshot does not have (made during the attack) stay as they
// are. Both folders are opened as roots, so no link leads outside.
func Restore(shareDir, snapshotDir string, files []Affected, stamp string) RestoreResult {
	var res RestoreResult
	share, err := os.OpenRoot(shareDir)
	if err != nil {
		res.Failed = append(res.Failed, "share: "+err.Error())
		return res
	}
	defer share.Close()
	snap, err := os.OpenRoot(snapshotDir)
	if err != nil {
		res.Failed = append(res.Failed, "snapshot: "+err.Error())
		return res
	}
	defer snap.Close()
	bin := RecycleFolder + "/" + stamp
	for _, f := range files {
		if !safeRel(f.Was) || f.Now != "" && !safeRel(f.Now) {
			res.Failed = append(res.Failed, f.Was)
			continue
		}
		info, err := snap.Lstat(f.Was)
		if err != nil || !info.Mode().IsRegular() {
			res.Missing++
			// The attacked copy stays: there is nothing to put in its place.
			continue
		}
		now := f.Now
		if now == "" {
			now = f.Was
		}
		if _, err := share.Lstat(now); err == nil {
			if err := moveToBin(share, now, bin); err != nil {
				res.Failed = append(res.Failed, now)
				continue
			}
			res.Recycled++
		}
		if now != f.Was {
			// A file that also stands under the old name (rewritten, then
			// renamed back by the attack) goes to the bin too.
			if _, err := share.Lstat(f.Was); err == nil {
				if err := moveToBin(share, f.Was, bin); err != nil {
					res.Failed = append(res.Failed, f.Was)
					continue
				}
				res.Recycled++
			}
		}
		if err := copyBack(snap, share, f.Was, info); err != nil {
			res.Failed = append(res.Failed, f.Was)
			continue
		}
		res.Restored++
	}
	return res
}

func moveToBin(root *os.Root, rel, bin string) error {
	target := bin + "/" + rel
	if err := root.MkdirAll(path.Dir(target), 0o770); err != nil {
		return err
	}
	if _, err := root.Lstat(target); err == nil {
		target += ".1"
	}
	return root.Rename(rel, target)
}

func copyBack(snap, share *os.Root, rel string, info fs.FileInfo) error {
	src, err := snap.Open(rel)
	if err != nil {
		return err
	}
	defer src.Close()
	if dir := path.Dir(rel); dir != "." {
		if err := share.MkdirAll(dir, 0o770); err != nil {
			return err
		}
	}
	tmp := rel + ".titanos-restore"
	dst, err := share.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		share.Remove(tmp)
		return err
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		_ = share.Lchown(tmp, int(st.Uid), int(st.Gid))
	}
	_ = share.Chtimes(tmp, info.ModTime(), info.ModTime())
	if err := share.Rename(tmp, rel); err != nil {
		share.Remove(tmp)
		return err
	}
	return nil
}

// ErrNoSnapshot means no snapshot from before the attack exists.
var ErrNoSnapshot = errors.New("no snapshot from before the attack")
