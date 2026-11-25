package store

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// ArchiveSource identifies a collection inside the current account. Its
// native .ics or .vcf objects carry recurrence exceptions, contact photos,
// group membership and the relationships between reminders.
type ArchiveSource struct {
	Kind Kind   `json:"kind"`
	ID   string `json:"id"`
}
type ArchiveCollection struct {
	ArchiveSource
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
	Order       int    `json:"order,omitempty"`
	Count       int    `json:"count"`
	Bytes       int64  `json:"bytes"`
	Tag         string `json:"tag"`
}
type ArchiveManifest struct {
	Format      int                 `json:"format"`
	Account     string              `json:"account"`
	Collections []ArchiveCollection `json:"collections"`
}

func (s *Store) ArchiveCollections(account string, kind Kind) ([]ArchiveCollection, error) {
	acc, err := s.accountFor(kind, account)
	if err != nil {
		return nil, err
	}
	var result []ArchiveCollection
	for _, c := range acc.collections() {
		c.mu.Lock()
		if !c.deleted {
			v := info(kind, c)
			var size int64
			for _, e := range c.st.Index {
				size += e.Size
			}
			result = append(result, ArchiveCollection{ArchiveSource: ArchiveSource{kind, v.ID}, Name: v.Name, Color: v.Color, Description: v.Description, Order: v.Order, Count: v.Count, Bytes: size, Tag: v.CTag})
		}
		c.mu.Unlock()
	}
	slices.SortFunc(result, func(a, b ArchiveCollection) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}

var ErrArchive = errors.New("pim: invalid personal backup")
var ErrArchiveChanged = errors.New("pim: collection changed while being backed up; retry")

func tarObject(tw *tar.Writer, name string, data []byte) error {
	err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg})
	if err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

func (s *Store) archiveCollection(ctx context.Context, tw *tar.Writer, account string, c ArchiveCollection) error {
	items, tag, err := s.ObjectsTag(c.Kind, account, c.ID)
	if err != nil {
		return err
	}
	if tag != c.Tag {
		return ErrArchiveChanged
	}
	reader, err := s.Reader(c.Kind, account, c.ID)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		obj, err := reader.Get(item.Name)
		if err != nil {
			return err
		}
		if obj.ETag != item.ETag {
			return ErrArchiveChanged
		}
		if err := tarObject(tw, string(c.Kind)+"/"+c.ID+"/"+encodeName(item.Name), obj.Data); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) WriteArchive(ctx context.Context, out io.Writer, account string, sources []ArchiveSource) error {
	if !ValidAccount(account) || len(sources) == 0 || len(sources) > 3*MaxCollections {
		return ErrArchive
	}
	byKind := map[Kind][]ArchiveCollection{}
	seen := map[ArchiveSource]bool{}
	manifest := ArchiveManifest{Format: 1, Account: account}
	for _, source := range sources {
		if !source.Kind.valid() || !ValidCollection(source.ID) || seen[source] {
			return ErrArchive
		}
		seen[source] = true
		if _, ok := byKind[source.Kind]; !ok {
			list, err := s.ArchiveCollections(account, source.Kind)
			if err != nil {
				return err
			}
			byKind[source.Kind] = list
		}
		list := byKind[source.Kind]
		i := slices.IndexFunc(list, func(v ArchiveCollection) bool { return v.ID == source.ID })
		if i < 0 {
			return ErrNotFound
		}
		manifest.Collections = append(manifest.Collections, list[i])
	}
	tw := tar.NewWriter(out)
	header, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err = tarObject(tw, "TitanOS.json", header); err != nil {
		return err
	}
	for _, c := range manifest.Collections {
		if err = s.archiveCollection(ctx, tw, account, c); err != nil {
			return err
		}
	}
	for _, c := range manifest.Collections {
		cur, err := s.GetCollection(c.Kind, account, c.ID)
		if err != nil {
			return err
		}
		if cur.CTag != c.Tag {
			return ErrArchiveChanged
		}
	}
	if err = tarObject(tw, "complete", []byte("TitanOS-PIM-1\n")); err != nil {
		return err
	}
	return tw.Close()
}

func readArchiveEntry(tr *tar.Reader, expected string, limit int64) ([]byte, error) {
	h, err := tr.Next()
	if err != nil {
		return nil, err
	}
	if h.Typeflag != tar.TypeReg || h.Name != expected || h.Size > limit || h.Size < 0 {
		return nil, ErrArchive
	}
	return io.ReadAll(tr)
}

// ReadArchive calls begin/item in manifest order. Callers stage the data in a
// private collection until the entire archive (including age's final tag) has
// been checked. It never extracts a member using its archive path.
func ReadArchive(ctx context.Context, r io.Reader, begin func(ArchiveCollection) error, item func(ArchiveSource, Item) error) (ArchiveManifest, error) {
	var manifest ArchiveManifest
	tr := tar.NewReader(r)
	header, err := readArchiveEntry(tr, "TitanOS.json", 1<<20)
	if err != nil {
		return manifest, err
	}
	if json.Unmarshal(header, &manifest) != nil || manifest.Format != 1 || !ValidAccount(manifest.Account) || len(manifest.Collections) < 1 || len(manifest.Collections) > 3*MaxCollections {
		return manifest, ErrArchive
	}
	seen := map[ArchiveSource]bool{}
	for _, c := range manifest.Collections {
		meta := MetaPatch{Name: &c.Name, Color: &c.Color, Description: &c.Description, Order: &c.Order}
		if !c.Kind.valid() || !ValidCollection(c.ID) || (c.Kind == Contacts && c.ID != DefaultBook) || seen[c.ArchiveSource] || !meta.valid() || c.Count < 0 || c.Count > MaxEntries || c.Bytes < 0 || c.Bytes > int64(c.Count)*maxSize(c.Kind) {
			return manifest, ErrArchive
		}
		seen[c.ArchiveSource] = true
		if err := begin(c); err != nil {
			return manifest, err
		}
		prefix := string(c.Kind) + "/" + c.ID + "/"
		var total int64
		names := map[string]bool{}
		for range c.Count {
			if err := ctx.Err(); err != nil {
				return manifest, err
			}
			h, err := tr.Next()
			if err != nil {
				return manifest, err
			}
			if h.Typeflag != tar.TypeReg || len(h.Name) <= len(prefix) || h.Name[:len(prefix)] != prefix || h.Size < 0 || h.Size > maxSize(c.Kind) || h.Size > c.Bytes-total {
				return manifest, ErrArchive
			}
			name, ok := decodeName(c.Kind, h.Name[len(prefix):])
			if !ok || names[name] {
				return manifest, ErrArchive
			}
			names[name] = true
			total += h.Size
			data, err := io.ReadAll(tr)
			if err != nil {
				return manifest, err
			}
			if err = item(c.ArchiveSource, Item{Name: name, Data: data}); err != nil {
				return manifest, err
			}
		}
		if total != c.Bytes {
			return manifest, ErrArchive
		}
	}
	end, err := readArchiveEntry(tr, "complete", 16)
	if err != nil || string(end) != "TitanOS-PIM-1\n" {
		return manifest, ErrArchive
	}
	if _, err = tr.Next(); !errors.Is(err, io.EOF) {
		return manifest, ErrArchive
	}
	// Force authentication of the final encrypted chunk. Extra plaintext
	// following the TAR would otherwise be silently ignored.
	var extra [1]byte
	if n, err := io.ReadFull(r, extra[:]); n != 0 || !errors.Is(err, io.EOF) {
		return manifest, fmt.Errorf("%w: trailing data or incomplete stream", ErrArchive)
	}
	return manifest, nil
}

// ArchiveScratch holds a mounted personal-data volume until the validated
// encrypted download has been served. It never spills onto the boot stick.
func (s *Store) ArchiveScratch(kind Kind) (*os.File, func(), error) {
	root, release, err := s.open(kind, true)
	if err != nil {
		return nil, nil, err
	}
	f, err := volumedata.UnlinkedTemp(root)
	if err != nil {
		release()
		return nil, nil, err
	}
	return f, func() { f.Close(); release() }, nil
}
