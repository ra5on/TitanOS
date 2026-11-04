package files

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrLinkOutside: a symbolic link whose target lies outside the root.
var ErrLinkOutside = errors.New("files: the link points outside this place")

// followLink resolves symbolic links anywhere in name, also those with an
// absolute target such as /mnt/<volume>/appdata/app/config.yaml or a linked
// folder in the middle of the path, which os.Root refuses. The target must
// stay inside the root: for a share that is the share itself, so a link
// cannot reach other shares or the system. The answer is a path relative to
// the root; the caller still opens it through os.Root, so a link swapped in
// afterwards cannot lead out either. A path without links comes back unchanged.
func (r Root) followLink(name string) (string, error) {
	base, err := filepath.EvalSymlinks(r.Dir)
	if err != nil {
		return name, nil
	}
	real, err := filepath.EvalSymlinks(filepath.Join(r.Dir, filepath.FromSlash(name)))
	if err != nil {
		// Every caller needs the file or folder to exist; a link into
		// nothing is simply not there.
		return "", mapError(err)
	}
	if real == filepath.Join(base, filepath.FromSlash(name)) {
		return name, nil
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrLinkOutside
	}
	rel = filepath.ToSlash(rel)
	if r.secret(rel) {
		return "", ErrLinkOutside
	}
	return rel, nil
}
