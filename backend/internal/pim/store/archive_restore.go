package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
)

var ErrRestoreBusy = errors.New("pim: another recovery is in progress")

type RestoredCollection struct {
	ArchiveCollection
	Target    string `json:"target,omitempty"`
	Added     int    `json:"added"`
	Identical int    `json:"identical"`
	Conflicts int    `json:"conflicts"`
}
type ArchiveReport struct {
	Account     string               `json:"account"`
	Collections []RestoredCollection `json:"collections"`
}

func (s *Store) restoreWorkspace(account string, fn func(*Store) error) (result error) {
	// The recovery workspace is private, excluded from normal backups, and
	// belongs to the account. Opening the real account pins its data volume.
	var ar *os.Root
	for _, kind := range []Kind{Calendar, Contacts, Tasks} {
		acc, end, err := s.beginWrite(kind, account)
		if err != nil {
			result = err
			continue
		}
		root, release, err := s.openWrite(acc)
		if err != nil {
			end()
			result = err
			continue
		}
		end()
		defer release()
		ar = root
		break
	}
	if ar == nil {
		return result
	}
	const name = ".personal-restore-stage"
	if err := ar.RemoveAll(name); err != nil {
		return err
	}
	if err := ar.Mkdir(name, 0700); err != nil {
		return err
	}
	defer func() { result = errors.Join(result, ar.RemoveAll(name)) }()
	dir, err := ar.OpenRoot(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	staged := New()
	staged.Local = s.Local
	staged.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		if create {
			if err := dir.Mkdir(feature, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
				return nil, nil, err
			}
		}
		root, err := dir.OpenRoot(feature)
		return root, nil, err
	}
	return fn(staged)
}

type archiveBatch struct {
	store      *Store
	ctx        context.Context
	account    string
	collection ArchiveCollection
	items      []Item
	size       int
}

func (b *archiveBatch) add(c ArchiveSource, it Item) error {
	if len(b.items) >= 128 || b.size+len(it.Data) > 4<<20 {
		if err := b.flush(); err != nil {
			return err
		}
	}
	b.items = append(b.items, it)
	b.size += len(it.Data)
	return nil
}
func (b *archiveBatch) flush() error {
	if len(b.items) == 0 {
		return nil
	}
	result, err := b.store.Import(b.ctx, b.collection.Kind, b.account, b.collection.ID, b.items)
	if err != nil {
		return err
	}
	for _, item := range result {
		if item.Err != nil {
			return item.Err
		}
		if !item.Created {
			return ErrArchive
		}
	}
	b.items = nil
	b.size = 0
	return nil
}

// RestoreArchive always restores into a second store and verifies every object
// first. With apply, verified calendars and lists get private copies and only
// absent contacts are added. A later retry never overwrites a person's edits.
func (s *Store) RestoreArchive(ctx context.Context, in io.Reader, account string, apply bool) (ArchiveReport, error) {
	var report ArchiveReport
	s.mu.Lock()
	if s.restoring[account] || len(s.restoring) >= 2 {
		s.mu.Unlock()
		return report, ErrRestoreBusy
	}
	if s.restoring == nil {
		s.restoring = map[string]bool{}
	}
	s.restoring[account] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.restoring, account); s.mu.Unlock() }()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.Context(account), cancel)
	defer cancel()
	defer stop()
	err := s.restoreWorkspace(account, func(staged *Store) error {
		const owner = "recovery"
		b := archiveBatch{ctx: ctx, store: staged, account: owner}
		manifest, err := ReadArchive(ctx, in, func(c ArchiveCollection) error {
			if err := b.flush(); err != nil {
				return err
			}
			b.collection = c
			if c.Kind != Contacts {
				_, err := staged.CreateCollection(c.Kind, owner, c.ID, Meta{Name: c.Name, Color: c.Color, Description: c.Description, Order: c.Order})
				if errors.Is(err, ErrExists) && c.ID == defaultID(c.Kind) {
					_, err = staged.UpdateCollection(c.Kind, owner, c.ID, MetaPatch{Name: &c.Name, Color: &c.Color, Description: &c.Description, Order: &c.Order})
				}
				return err
			}
			return nil
		}, b.add)
		if err != nil {
			return err
		}
		if err = b.flush(); err != nil {
			return err
		}
		report.Account = manifest.Account
		for _, c := range manifest.Collections {
			items, err := staged.Objects(c.Kind, owner, c.ID)
			if err != nil {
				return err
			}
			var size int64
			for _, it := range items {
				size += it.Size
			}
			if len(items) != c.Count || size != c.Bytes {
				return ErrArchive
			}
			report.Collections = append(report.Collections, RestoredCollection{ArchiveCollection: c})
		}
		if apply {
			if err := ctx.Err(); err != nil {
				return err
			}
			for i := range report.Collections {
				if err := s.importArchiveCopy(ctx, account, staged, owner, manifest.Account, &report.Collections[i]); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return report, err
}
func (s *Store) importArchiveCopy(ctx context.Context, account string, staged *Store, sourceAccount, original string, r *RestoredCollection) error {
	r.Target = DefaultBook
	if r.Kind != Contacts {
		hash := sha256.Sum256([]byte(strings.Join([]string{original, string(r.Kind), r.ID, r.Tag}, "\x00")))
		r.Target = "restore-" + hex.EncodeToString(hash[:12])
		// The ID stays the same for a retry of this snapshot. A truncated
		// name still fits the store's UTF-8 byte limit.
		name := r.Name
		for len(name) > 238 {
			rr := []rune(name)
			name = string(rr[:len(rr)-1])
		}
		_, err := s.CreateCollection(r.Kind, account, r.Target, Meta{Name: "Restore · " + name, Color: r.Color, Order: r.Order, Description: r.Description})
		if err != nil && !errors.Is(err, ErrExists) {
			return err
		}
	}
	objects, err := staged.Objects(r.Kind, sourceAccount, r.ID)
	if err != nil {
		return err
	}
	reader, err := staged.Reader(r.Kind, sourceAccount, r.ID)
	if err != nil {
		return err
	}
	defer reader.Close()
	var batch []Item
	size := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		results, err := s.importItems(ctx, r.Kind, account, r.Target, batch, true)
		if err != nil {
			return err
		}
		for _, it := range results {
			switch {
			case errors.Is(it.Err, ErrUIDConflict), errors.Is(it.Err, ErrPrecondition):
				r.Conflicts++
			case it.Err != nil:
				return it.Err
			case it.Created:
				r.Added++
			default:
				r.Identical++
			}
		}
		batch = nil
		size = 0
		return nil
	}
	for _, it := range objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		obj, err := reader.Get(it.Name)
		if err != nil {
			return err
		}
		if len(batch) >= 128 || size+len(obj.Data) > 4<<20 {
			if err := flush(); err != nil {
				return err
			}
		}
		batch = append(batch, Item{Name: it.Name, Data: obj.Data})
		size += len(obj.Data)
	}
	return flush()
}
