package files

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
)

type RenameOptions struct {
	Dir                           string
	Names                         []string
	Find, Replace, Prefix, Suffix string
	Number                        bool
	Start, Digits                 int
}
type RenameChange struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Version string `json:"version"`
	Problem string `json:"problem"`
}
type RenameResult struct {
	Done  int    `json:"done"`
	Undo  string `json:"undo"`
	Error string `json:"error"`
}

func entryVersion(info fs.FileInfo) string {
	stat, _ := info.Sys().(*syscall.Stat_t)
	if stat == nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d:%d:%v:%v", stat.Dev, stat.Ino, info.Size(), info.Mode(), stat.Mtim, stat.Ctim))))
}

func (m *Manager) RenamePreview(root Root, opt RenameOptions) ([]RenameChange, error) {
	if !root.Writable || len(root.Hide) > 0 {
		return nil, ErrReadOnly
	}
	dir, e := clean(opt.Dir)
	if e != nil || len(opt.Names) == 0 || len(opt.Names) > 1000 || opt.Start < 0 || opt.Start > 1e9 || opt.Digits < 1 || opt.Digits > 9 {
		return nil, ErrInvalid
	}
	r, e := os.OpenRoot(root.Dir)
	if e != nil {
		return nil, mapError(e)
	}
	defer r.Close()
	changes := []RenameChange{}
	targets := map[string]int{}
	sources := map[string]bool{}
	for i, name := range opt.Names {
		if !validName(name) || sources[name] {
			return nil, ErrInvalid
		}
		sources[name] = true
		from := path.Join(dir, name)
		if root.secret(from) || m.protected(root, from) {
			return nil, ErrProtected
		}
		info, e := r.Lstat(from)
		if e != nil {
			return nil, mapError(e)
		}
		ext := ""
		if !info.IsDir() {
			ext = path.Ext(name)
		}
		base := strings.TrimSuffix(name, ext)
		if opt.Find != "" {
			base = strings.ReplaceAll(base, opt.Find, opt.Replace)
		}
		numbered := ""
		if opt.Number {
			numbered = fmt.Sprintf("%0*d", opt.Digits, opt.Start+i)
		}
		newName := opt.Prefix + base + opt.Suffix + numbered + ext
		change := RenameChange{From: from, To: path.Join(dir, newName), Version: entryVersion(info)}
		if !validName(newName) {
			change.Problem = "invalid"
		} else if m.protected(root, change.To) {
			change.Problem = "protected"
		} else if from != change.To {
			if _, e = r.Lstat(change.To); e == nil {
				change.Problem = "exists"
			} else if !os.IsNotExist(e) {
				return nil, mapError(e)
			}
		}
		if previous, ok := targets[change.To]; ok {
			change.Problem = "duplicate"
			changes[previous].Problem = "duplicate"
		}
		targets[change.To] = len(changes)
		changes = append(changes, change)
	}
	return changes, nil
}
func (m *Manager) RenameBatch(ctx context.Context, root Root, changes []RenameChange, user string) (RenameResult, error) {
	result := RenameResult{}
	if !root.Writable || len(root.Hide) > 0 {
		return result, ErrReadOnly
	}
	if len(changes) == 0 || len(changes) > 1000 {
		return result, ErrInvalid
	}
	r, e := os.OpenRoot(root.Dir)
	if e != nil {
		return result, mapError(e)
	}
	defer r.Close()
	sources, targets := map[string]bool{}, map[string]bool{}
	for _, change := range changes {
		from, e := clean(change.From)
		to, e2 := clean(change.To)
		if e != nil || e2 != nil || from == "." || to == "." || path.Dir(from) != path.Dir(to) || !validName(path.Base(to)) || sources[from] || targets[to] {
			return result, ErrInvalid
		}
		sources[from] = true
		targets[to] = true
		if root.secret(from) || root.secret(to) || m.protected(root, from) || m.protected(root, to) {
			return result, ErrProtected
		}
		info, e := r.Lstat(from)
		if e != nil {
			return result, mapError(e)
		}
		if entryVersion(info) != change.Version {
			return result, ErrTextConflict
		}
		if from != to {
			if _, e = r.Lstat(to); e == nil {
				return result, ErrExists
			} else if !os.IsNotExist(e) {
				return result, mapError(e)
			}
		}
	}
	moves := []UndoMove{}
	for _, change := range changes {
		if change.From == change.To {
			continue
		}
		if e = ctx.Err(); e != nil {
			break
		}
		info, err := r.Lstat(change.From)
		if err != nil {
			e = mapError(err)
			break
		}
		if entryVersion(info) != change.Version {
			e = ErrTextConflict
			break
		}
		if e = publishNewFile(r, change.From, change.To); e != nil {
			break
		}
		result.Done++
		moves = append(moves, UndoMove{CurrentRoot: root, OriginalRoot: root, Current: change.To, Original: change.From})
	}
	result.Undo = m.RecordUndo(user, "rename", moves)
	if e != nil {
		result.Error = "changed"
	}
	return result, nil
}
