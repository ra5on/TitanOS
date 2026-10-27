package fileindex

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"
)

// The index of one volume is a bbolt database in its private folder:
//
//	files:    doc id (8 bytes) → doc (JSON)
//	paths:    path relative to the volume → doc id
//	postings: term 0x00 doc id → empty
//	meta:     version, next id
//
// Paths never leave the database in another form; who may see a hit is
// decided at search time from the root the request names.
var (
	filesBucket    = []byte("files")
	pathsBucket    = []byte("paths")
	postingsBucket = []byte("postings")
	metaBucket     = []byte("meta")
	versionKey     = []byte("version")
	nextKey        = []byte("next")
)

const (
	dbName        = "index.db"
	schemaVersion = "1"
	// maxCandidates bounds the documents one search term may yield.
	maxCandidates = 100000
)

type doc struct {
	Path     string   `json:"p"`
	Size     int64    `json:"s"`
	Modified int64    `json:"m"`
	Inode    uint64   `json:"i"`
	Terms    []string `json:"t,omitempty"`
	// Failed marks a file whose text could not be read; it is not retried
	// until it changes.
	Failed bool `json:"f,omitempty"`
}

func (d doc) same(size, modified int64, inode uint64) bool {
	return d.Size == size && d.Modified == modified && d.Inode == inode
}

// handle is the one open database of a volume. bbolt locks the file, so a
// search and a running pass share it instead of opening it twice; it is
// closed when nobody uses it, so that the volume can be unmounted.
type handle struct {
	mu    sync.Mutex
	users int
	db    *bolt.DB
	root  *os.Root
}

func openDB(root *os.Root) (*bolt.DB, error) {
	db, err := bolt.Open(dbName, 0600, &bolt.Options{Timeout: 2 * time.Second, OpenFile: func(_ string, flags int, mode os.FileMode) (*os.File, error) {
		f, err := root.OpenFile(dbName, flags|syscall.O_NOFOLLOW, mode)
		if err != nil {
			return nil, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
			f.Close()
			return nil, errors.New("fileindex: unsafe index file")
		}
		return f, nil
	}})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(metaBucket)
		if err != nil {
			return err
		}
		if string(meta.Get(versionKey)) != schemaVersion {
			// An unknown layout is rebuilt rather than misread.
			for _, name := range [][]byte{filesBucket, pathsBucket, postingsBucket} {
				if tx.Bucket(name) != nil {
					if err := tx.DeleteBucket(name); err != nil {
						return err
					}
				}
			}
			if err := meta.Put(versionKey, []byte(schemaVersion)); err != nil {
				return err
			}
		}
		for _, name := range [][]byte{filesBucket, pathsBucket, postingsBucket} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func idKey(id uint64) []byte { return binary.BigEndian.AppendUint64(nil, id) }

func postingKey(term string, id uint64) []byte {
	return binary.BigEndian.AppendUint64(append([]byte(term), 0), id)
}

func getDoc(tx *bolt.Tx, id uint64) (doc, bool) {
	var d doc
	data := tx.Bucket(filesBucket).Get(idKey(id))
	if data == nil || json.Unmarshal(data, &d) != nil {
		return d, false
	}
	return d, true
}

func lookup(tx *bolt.Tx, rel string) (uint64, doc, bool) {
	raw := tx.Bucket(pathsBucket).Get([]byte(rel))
	if len(raw) != 8 {
		return 0, doc{}, false
	}
	id := binary.BigEndian.Uint64(raw)
	d, ok := getDoc(tx, id)
	return id, d, ok
}

// put stores a document under its path, replacing an older version.
func put(tx *bolt.Tx, d doc) (uint64, error) {
	if id, old, ok := lookup(tx, d.Path); ok {
		if err := remove(tx, id, old); err != nil {
			return 0, err
		}
	}
	meta := tx.Bucket(metaBucket)
	var id uint64 = 1
	if raw := meta.Get(nextKey); len(raw) == 8 {
		id = binary.BigEndian.Uint64(raw)
	}
	if err := meta.Put(nextKey, idKey(id+1)); err != nil {
		return 0, err
	}
	data, err := json.Marshal(d)
	if err != nil {
		return 0, err
	}
	if err := tx.Bucket(filesBucket).Put(idKey(id), data); err != nil {
		return 0, err
	}
	if err := tx.Bucket(pathsBucket).Put([]byte(d.Path), idKey(id)); err != nil {
		return 0, err
	}
	postings := tx.Bucket(postingsBucket)
	for _, term := range d.Terms {
		if err := postings.Put(postingKey(term, id), nil); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func remove(tx *bolt.Tx, id uint64, d doc) error {
	postings := tx.Bucket(postingsBucket)
	for _, term := range d.Terms {
		if err := postings.Delete(postingKey(term, id)); err != nil {
			return err
		}
	}
	if err := tx.Bucket(filesBucket).Delete(idKey(id)); err != nil {
		return err
	}
	return tx.Bucket(pathsBucket).Delete([]byte(d.Path))
}

// matches returns the documents with a word that starts with term.
func matches(tx *bolt.Tx, term string) map[uint64]bool {
	out := map[uint64]bool{}
	c := tx.Bucket(postingsBucket).Cursor()
	prefix := []byte(term)
	for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix) && len(out) < maxCandidates; k, _ = c.Next() {
		if len(k) >= 9 && k[len(k)-9] == 0 {
			out[binary.BigEndian.Uint64(k[len(k)-8:])] = true
		}
	}
	return out
}

// Hit is a file whose text contains all searched words.
type Hit struct {
	// Path is relative to the volume.
	Path     string
	Size     int64
	Modified time.Time
}

// query finds documents containing every term (as a word prefix) whose
// path lies under one of the prefixes ("" = the whole volume).
func query(tx *bolt.Tx, terms []string, prefixes []string, limit int) ([]Hit, bool) {
	if len(terms) == 0 {
		return nil, false
	}
	var result map[uint64]bool
	for _, term := range terms {
		found := matches(tx, term)
		if result == nil {
			result = found
		} else {
			for id := range result {
				if !found[id] {
					delete(result, id)
				}
			}
		}
		if len(result) == 0 {
			return nil, false
		}
	}
	ids := make([]uint64, 0, len(result))
	for id := range result {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var hits []Hit
	truncated := false
	for _, id := range ids {
		d, ok := getDoc(tx, id)
		if !ok || !under(d.Path, prefixes) {
			continue
		}
		if len(hits) >= limit {
			truncated = true
			break
		}
		hits = append(hits, Hit{Path: d.Path, Size: d.Size, Modified: time.Unix(0, d.Modified)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Path < hits[j].Path })
	return hits, truncated
}

func under(p string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix == "" || p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}
