package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrLinkOutside: a symbolic link whose target lies outside the root.
var ErrLinkOutside = errors.New("files: the link points outside this place")

// followLink resolves name when it is a symbolic link, also one with an
// absolute target such as /mnt/<volume>/appdata/app/config.yaml, which
// os.Root refuses. The target must stay inside the root: for a share that is
// the share itself, so a link cannot reach other shares or the system. The
// answer is a path relative to the root; the caller still opens it through
// os.Root, so a link swapped in afterwards cannot lead out either. Anything
// that is not a link comes back unchanged.
func (r Root) followLink(name string) (string, error) {
	full := filepath.Join(r.Dir, filepath.FromSlash(name))
	info, err := os.Lstat(full)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return name, nil
	}
	base, err := filepath.EvalSymlinks(r.Dir)
	if err != nil {
		return "", mapError(err)
	}
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", mapError(err)
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrLinkOutside
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || r.secret(rel) {
		return "", ErrLinkOutside
	}
	return rel, nil
}
