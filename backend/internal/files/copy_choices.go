package files

import (
	"os"
	"path"
	"strings"
)

type CopyChoice struct {
	Action        string `json:"action"`
	Name          string `json:"name,omitempty"`
	Target        string `json:"target,omitempty"`
	SourceVersion string `json:"sourceVersion,omitempty"`
	TargetVersion string `json:"targetVersion,omitempty"`
}

func validateChoices(src, dst Root, paths []string, choices map[string]CopyChoice) error {
	if len(choices) > 1000 {
		return ErrInvalid
	}
	for p, c := range choices {
		if cleanPath, e := clean(p); e != nil || cleanPath != p {
			return ErrInvalid
		}
		included := false
		for _, top := range paths {
			if p == top || strings.HasPrefix(p, top+"/") {
				included = true
			}
		}
		if !included {
			return ErrInvalid
		}
		if c.Action != "skip" && c.Action != "replace" && c.Action != "rename" && c.Action != "copy" {
			return ErrInvalid
		}
		if c.Name != "" && (!validName(c.Name) || strings.Contains(c.Name, "\\")) {
			return ErrInvalid
		}
		if c.Target != "" {
			v, e := clean(c.Target)
			if e != nil || v == "." || v != c.Target || dst.secret(v) {
				return ErrInvalid
			}
			if src.Dir == dst.Dir && (v == p || strings.HasPrefix(v, p+"/")) {
				return ErrInvalid
			}
		}
	}
	return nil
}
func choiceTarget(target string, c CopyChoice) string {
	if c.Target != "" {
		target = c.Target
	}
	if c.Name != "" {
		target = path.Join(path.Dir(target), c.Name)
	}
	return target
}
func checkChoice(r *os.Root, p, version string) error {
	info, err := r.Lstat(p)
	if version == "absent" {
		if os.IsNotExist(err) {
			return nil
		}
		return ErrTextConflict
	}
	if err != nil || entryVersion(info) != version {
		return ErrTextConflict
	}
	return nil
}

func (m *Manager) copyParents(r *os.Root, root Root, dir string, uid int) error {
	p := ""
	for _, part := range strings.Split(dir, "/") {
		if part == "." || part == "" {
			continue
		}
		p = path.Join(p, part)
		if m.protected(root, p) || root.secret(p) {
			return ErrProtected
		}
		info, err := r.Lstat(p)
		if os.IsNotExist(err) {
			if err = r.Mkdir(p, 0770); err != nil {
				return mapError(err)
			}
			m.own(r, p, uid, true)
		} else if err != nil {
			return mapError(err)
		} else if !info.IsDir() {
			return ErrInvalid
		}
	}
	return nil
}
