package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io/fs"
	"os"
	"path"
	"sync"
	"syscall"
	"time"
)

type UndoMove struct {
	CurrentRoot, OriginalRoot Root
	Current, Original         string
	stamp                     string
}
type UndoInfo struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Count   int       `json:"count"`
	Roots   []string  `json:"roots"`
	Created time.Time `json:"created"`
	Busy    bool      `json:"busy"`
}
type undoRecord struct {
	UndoInfo
	user  string
	moves []UndoMove
}
type undoStore struct {
	mu      sync.Mutex
	records []*undoRecord
}

func randomID() string { b := make([]byte, 12); rand.Read(b); return hex.EncodeToString(b) }

// treeStamp detects replacement, edits, new children and metadata changes.
// It reads directory entries and stat data, never the file contents.
func treeStamp(ctx context.Context, r *os.Root, p string, regularOnly bool) (string, error) {
	h := sha256.New()
	count := 0
	err := fs.WalkDir(r.FS(), p, func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return mapError(e)
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		count++
		if count > scanLimit {
			return ErrScanLimit
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if regularOnly && name != p && hidden(d.Name()) {
			return ErrInvalid
		}
		if regularOnly && !info.IsDir() && !info.Mode().IsRegular() {
			return ErrInvalid
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return ErrInvalid
		}
		fmt.Fprintf(h, "%s\x00%d:%d:%d:%d:%d:%d:%d:%d\n", name, stat.Dev, stat.Ino, info.Mode(), info.Size(), stat.Mtim.Sec, stat.Mtim.Nsec, stat.Ctim.Sec, stat.Ctim.Nsec)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}
func (m *Manager) RecordUndo(user, kind string, moves []UndoMove) string {
	if len(moves) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	valid := []UndoMove{}
	ids := []string{}
	seen := map[string]bool{}
	for _, move := range moves {
		if move.CurrentRoot.ID == move.OriginalRoot.ID && move.Current == move.Original {
			continue
		}
		r, e := os.OpenRoot(move.CurrentRoot.Dir)
		if e != nil {
			continue
		}
		move.stamp, e = treeStamp(ctx, r, move.Current, false)
		r.Close()
		if e != nil {
			continue
		}
		valid = append(valid, move)
		for _, id := range []string{move.CurrentRoot.ID, move.OriginalRoot.ID} {
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	if len(valid) == 0 {
		return ""
	}
	m.undo.mu.Lock()
	defer m.undo.mu.Unlock()
	m.pruneUndo()
	id := randomID()
	m.undo.records = append(m.undo.records, &undoRecord{UndoInfo: UndoInfo{ID: id, Kind: kind, Count: len(valid), Roots: ids, Created: m.now()}, user: user, moves: valid})
	// Keep at most twenty records per account, leaving other accounts untouched.
	count := 0
	for i := len(m.undo.records) - 1; i >= 0; i-- {
		record := m.undo.records[i]
		if record.user == user {
			count++
			if count > 20 && !record.Busy {
				m.undo.records = append(m.undo.records[:i], m.undo.records[i+1:]...)
			}
		}
	}
	return id
}
func (m *Manager) pruneUndo() {
	kept := m.undo.records[:0]
	for _, r := range m.undo.records {
		if r.Busy || m.now().Sub(r.Created) < 30*time.Minute {
			kept = append(kept, r)
		}
	}
	m.undo.records = kept
}
func (m *Manager) UndoList(user string) []UndoInfo {
	m.undo.mu.Lock()
	defer m.undo.mu.Unlock()
	m.pruneUndo()
	out := []UndoInfo{}
	for i := len(m.undo.records) - 1; i >= 0; i-- {
		r := m.undo.records[i]
		if r.user == user {
			info := r.UndoInfo
			info.Roots = append([]string{}, info.Roots...)
			out = append(out, info)
		}
	}
	return out
}
func renameAcross(sr, dr *os.Root, from, to string) error {
	a, e := sr.Open(path.Dir(from))
	if e != nil {
		return e
	}
	defer a.Close()
	b, e := dr.Open(path.Dir(to))
	if e != nil {
		return e
	}
	defer b.Close()
	return unix.Renameat2(int(a.Fd()), path.Base(from), int(b.Fd()), path.Base(to), unix.RENAME_NOREPLACE)
}
func (m *Manager) Undo(ctx context.Context, user, id string, roots map[string]Root, uid int) error {
	m.undo.mu.Lock()
	var record *undoRecord
	for _, r := range m.undo.records {
		if r.ID == id && r.user == user && !r.Busy && m.now().Sub(r.Created) < 30*time.Minute {
			record = r
			break
		}
	}
	if record == nil {
		m.undo.mu.Unlock()
		return ErrNoJob
	}
	record.Busy = true
	m.undo.mu.Unlock()
	defer func() {
		m.undo.mu.Lock()
		record.Busy = false
		if len(record.moves) == 0 {
			for i, r := range m.undo.records {
				if r == record {
					m.undo.records = append(m.undo.records[:i], m.undo.records[i+1:]...)
					break
				}
			}
		}
		m.undo.mu.Unlock()
	}()
	// Validate all originals before starting. Each move is checked again directly
	// before publication; partial completion leaves the remaining undo entries.
	for _, move := range record.moves {
		if err := m.checkUndo(ctx, move, roots); err != nil {
			return err
		}
	}
	for len(record.moves) > 0 {
		move := record.moves[0]
		if err := m.checkUndo(ctx, move, roots); err != nil {
			return err
		}
		sr, e := os.OpenRoot(move.CurrentRoot.Dir)
		if e != nil {
			return mapError(e)
		}
		dr, e := os.OpenRoot(move.OriginalRoot.Dir)
		if e != nil {
			sr.Close()
			return mapError(e)
		}
		e = renameAcross(sr, dr, move.Current, move.Original)
		if errors.Is(e, syscall.EXDEV) {
			e = m.undoAcrossFilesystems(ctx, sr, dr, move, uid)
		}
		sr.Close()
		dr.Close()
		if e != nil {
			return mapError(e)
		}
		m.undo.mu.Lock()
		record.moves = record.moves[1:]
		record.Count = len(record.moves)
		m.undo.mu.Unlock()
	}
	return nil
}
func (m *Manager) checkUndo(ctx context.Context, move UndoMove, roots map[string]Root) error {
	for _, saved := range []Root{move.CurrentRoot, move.OriginalRoot} {
		now, ok := roots[saved.ID]
		if !ok || !now.Writable || now.Dir != saved.Dir || len(now.Hide) > 0 {
			return ErrReadOnly
		}
	}
	if m.protected(move.CurrentRoot, move.Current) || m.protected(move.OriginalRoot, move.Original) {
		return ErrProtected
	}
	sr, e := os.OpenRoot(move.CurrentRoot.Dir)
	if e != nil {
		return mapError(e)
	}
	defer sr.Close()
	stamp, e := treeStamp(ctx, sr, move.Current, false)
	if e != nil {
		return e
	}
	if stamp != move.stamp {
		return ErrTextConflict
	}
	dr, e := os.OpenRoot(move.OriginalRoot.Dir)
	if e != nil {
		return mapError(e)
	}
	defer dr.Close()
	if _, e = dr.Lstat(move.Original); e == nil {
		return ErrExists
	} else if !os.IsNotExist(e) {
		return mapError(e)
	}
	info, e := dr.Stat(path.Dir(move.Original))
	if e != nil || !info.IsDir() {
		return ErrNotFound
	}
	return nil
}
func (m *Manager) undoAcrossFilesystems(ctx context.Context, sr, dr *os.Root, move UndoMove, uid int) error {
	if _, e := treeStamp(ctx, sr, move.Current, true); e != nil {
		return e
	}
	stage := path.Join(path.Dir(move.Original), ".titanos-undo-"+randomID()+partSuffix)
	if e := dr.Mkdir(stage, 0700); e != nil {
		return e
	}
	defer dr.RemoveAll(stage)
	job := &Job{Move: true, c: &counters{}, completed: map[string]bool{}, targets: map[string]string{}}
	target := path.Join(stage, "item")
	if e := m.copyEntry(ctx, job, sr, dr, move.Current, target, ConflictSkip, uid); e != nil {
		return e
	}
	stamp, e := treeStamp(ctx, sr, move.Current, false)
	if e != nil {
		return e
	}
	if stamp != move.stamp {
		return ErrTextConflict
	}
	if e = publishNewFile(dr, target, move.Original); e != nil {
		return e
	}
	return sr.RemoveAll(move.Current)
}

func (m *Manager) DeleteWithUndo(root Root, paths []string, user string, permanent bool) (string, error) {
	if permanent || !root.Recycle {
		return "", m.Delete(root, paths, user, permanent)
	}
	if !root.Writable || len(root.Hide) > 0 {
		return "", ErrReadOnly
	}
	moves := []UndoMove{}
	var result error
	for _, p := range paths {
		cleaned, e := clean(p)
		if e != nil || cleaned == "." {
			result = ErrInvalid
			break
		}
		if m.protected(root, cleaned) {
			result = ErrProtected
			break
		}
		r, e := os.OpenRoot(root.Dir)
		if e != nil {
			result = mapError(e)
			break
		}
		target, e := m.recyclePath(r, cleaned, user)
		r.Close()
		if e != nil {
			result = mapError(e)
			break
		}
		moves = append(moves, UndoMove{CurrentRoot: root, OriginalRoot: root, Current: target, Original: cleaned})
	}
	return m.RecordUndo(user, "trash", moves), result
}
