package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
)

// Moving: the source removed after a copy, or a rename within one root
// (split from copy.go, PLAN 75-12).

// removeMoved removes the source of a cross-root move, file by file, and
// the folders that end up empty. Whatever was not copied, has changed since,
// was skipped or failed stays at the source. It reports whether rel is gone.
func (m *Manager) removeMoved(ctx context.Context, job *Job, sr, dr *os.Root, rel string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := sr.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, m.noteProblem(job, rel, mapError(err), "read")
	}
	if m.wasReported(job, rel) {
		return false, nil // the problem is listed already
	}
	entry, err := job.entry(rel)
	if err != nil {
		return false, storeErr(err)
	}
	if entry.Skip {
		return false, m.noteKept(job, rel, "kept_skipped")
	}
	switch {
	case info.IsDir():
		var inner error
		err := eachChild(sr, rel, func(child fs.DirEntry) error {
			p := path.Join(rel, child.Name())
			if hidden(child.Name()) {
				if m.removeStalePart(sr, p) {
					return nil
				}
				inner = m.noteKept(job, p, "kept_internal")
				return inner
			}
			_, inner = m.removeMoved(ctx, job, sr, dr, p)
			return inner
		})
		if inner != nil {
			return false, inner
		}
		if err != nil {
			return false, m.noteProblem(job, rel, failure("unreadable_folder", mapError(err)), "")
		}
		if job.durable != nil {
			if err := syncDir(sr, rel); err != nil {
				return false, storeErr(err)
			}
		}
		if sr.Remove(rel) != nil {
			return false, nil // something below stays; it is listed
		}
		return true, nil
	case info.Mode()&fs.ModeSymlink != 0:
		return false, m.noteKept(job, rel, "kept_link")
	case !info.Mode().IsRegular():
		return false, m.noteKept(job, rel, "kept_special")
	case !entry.Complete:
		return false, m.noteKept(job, rel, "kept_new")
	case !m.copiedUnchanged(job, rel, info, entry):
		return false, m.noteKept(job, rel, "kept_changed")
	case !m.destinationIntact(job, dr, entry):
		return false, m.noteKept(job, rel, "kept_target_changed")
	}
	if err := sr.Remove(rel); err != nil {
		return false, m.noteProblem(job, rel, failure("remove_failed", mapError(err)), "")
	}
	return true, nil
}

func syncDir(r *os.Root, rel string) error {
	dir, err := r.Open(rel)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// moveRename moves within one root by renaming. A folder that exists at the
// target is merged into, entry by entry; names that exist follow the
// conflict rule. Problems are recorded; only errors that end the job return.
func (m *Manager) moveRename(ctx context.Context, job *Job, r *os.Root, dst Root, rel, target, conflict string, uid int) error {
	if err := m.copyCheckpoint(ctx, job, 0); err != nil {
		return err
	}
	err := m.moveRenameOne(ctx, job, r, dst, rel, target, conflict, uid)
	if err == nil || fatalCopy(err) {
		return err
	}
	return m.noteProblem(job, rel, err, "")
}

func (m *Manager) moveRenameOne(ctx context.Context, job *Job, r *os.Root, dst Root, rel, target, conflict string, uid int) error {
	entry, err := job.entry(rel)
	if err != nil {
		return storeErr(err)
	}
	info, err := r.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) && (entry.Target != "" || entry.Complete) {
		return nil // moved in an earlier run
	}
	if err != nil {
		return mapError(err)
	}
	choice, explicit := job.choices[rel]
	action := conflict
	if explicit {
		if choice.Action == ConflictSkip {
			m.countTree(job, r, rel)
			return m.noteKept(job, rel, "kept_skipped")
		}
		if choice.SourceVersion != "" {
			if err := checkChoice(r, rel, choice.SourceVersion); err != nil {
				return failure("changed", ErrSourceChanged)
			}
		}
		target = choiceTarget(target, choice)
		if choice.TargetVersion != "" && entry.Target == "" {
			if err := checkChoice(r, target, choice.TargetVersion); err != nil {
				return failure("changed_target", ErrSourceChanged)
			}
		}
		action = choice.Action
		if action == "copy" || choice.Name != "" {
			action = "exact"
		}
	}
	if m.protected(dst, target) || dst.secret(target) {
		return ErrProtected
	}
	if target == rel || strings.HasPrefix(target, rel+"/") {
		return ErrIntoItself
	}
	m.setCurrent(job, rel)
	existing, err := r.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return m.renameEntry(ctx, job, r, dst, rel, target, false, conflict, uid)
	case err != nil:
		return mapError(err)
	case os.SameFile(info, existing):
		return nil
	case info.IsDir() && existing.IsDir() && action != "exact":
		// Merge into the folder that is there.
		if entry.Target != target {
			if err := storeErr(m.transferTarget(job, rel, target)); err != nil {
				return err
			}
		}
		var inner error
		err := eachChild(r, rel, func(child fs.DirEntry) error {
			p := path.Join(rel, child.Name())
			if hidden(child.Name()) {
				if m.removeStalePart(r, p) {
					return nil
				}
				inner = m.noteKept(job, p, "kept_internal")
				return inner
			}
			inner = m.moveRename(ctx, job, r, dst, p, path.Join(target, child.Name()), action, uid)
			return inner
		})
		if inner != nil {
			return inner
		}
		if err != nil {
			return failure("unreadable_folder", mapError(err))
		}
		if r.Remove(rel) == nil && job.durable != nil {
			if err := syncTransferParent(r, rel); err != nil {
				return storeErr(err)
			}
		}
		return nil
	}
	// The name exists.
	switch action {
	case "exact":
		return ErrExists
	case ConflictSkip:
		m.countTree(job, r, rel)
		return m.noteKept(job, rel, "kept_skipped")
	case ConflictReplace, ConflictMissing:
		if info.IsDir() || existing.IsDir() || !info.Mode().IsRegular() {
			return failure("type_mismatch", ErrExists)
		}
		if action == ConflictMissing && sameContent(info, existing) {
			// The target looks the same; the source stays (owner, 4.11.2026).
			job.c.count.Add(1)
			job.c.copied.Add(info.Size())
			job.c.committed.Add(info.Size())
			m.jobs.mu.Lock()
			job.Identical++
			m.jobs.mu.Unlock()
			return m.noteKept(job, rel, "kept_identical")
		}
		return m.renameEntry(ctx, job, r, dst, rel, target, true, conflict, uid)
	}
	name, err := freeName(r, target)
	if err != nil {
		return err
	}
	return m.renameEntry(ctx, job, r, dst, rel, name, false, conflict, uid)
}

// renameEntry renames one entry within the root. Across a mount point inside
// the root it copies and removes instead.
func (m *Manager) renameEntry(ctx context.Context, job *Job, r *os.Root, dst Root, rel, target string, replace bool, conflict string, uid int) error {
	if err := m.copyCheckpoint(ctx, job, 0); err != nil {
		return err
	}
	if err := storeErr(m.saveTransfer(job, "rename "+rel+" to "+target)); err != nil {
		return err
	}
	var err error
	if replace {
		var recycled bool
		if recycled, err = m.retire(job, r, target); err == nil {
			if recycled {
				err = renameNoReplace(r, rel, target)
			} else {
				err = r.Rename(rel, target)
			}
		}
	} else {
		err = renameNoReplace(r, rel, target)
	}
	if err != nil {
		empty := ""
		if e := storeErr(m.appendTransfer(job, transferEvent{Pending: &empty})); e != nil {
			return e
		}
		if errors.Is(err, syscall.EXDEV) {
			if err := m.copyEntry(ctx, job, r, r, rel, target, conflict, uid); err != nil {
				return err
			}
			_, err := m.removeMoved(ctx, job, r, r, rel)
			return err
		}
		return mapError(err)
	}
	if job.durable != nil {
		if err := syncTransferParent(r, rel); err != nil {
			return storeErr(err)
		}
		if err := syncTransferParent(r, target); err != nil {
			return storeErr(err)
		}
	}
	m.countTree(job, r, target)
	if !job.paged() {
		job.targets[rel] = target
	}
	return storeErr(m.finishTransfer(job, r, rel, target, ""))
}
