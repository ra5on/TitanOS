package api

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// pimCache keeps the prepared address book of each account (1.13): the
// sorted rows of the list with their folded search texts and the parsed
// birthdays (cardweb.ListIndex). The pages ask for the list and the
// birthdays on every view; the book changes rarely. An entry is kept
// under the ctag of the book read in the same moment as its entries
// (Store.ObjectsTag) and the volume, so every change – from the web
// page, an iPhone over CardDAV, the agent or an import – makes it stale
// by itself; no write has to remember to drop it. In RAM only.
type pimCache struct {
	mu    sync.Mutex
	books map[string]*bookIndex // account + "\x00" + book
	order []string              // oldest first, for the bound
}

type bookIndex struct {
	key string // volume + "\x00" + ctag
	ix  *cardweb.ListIndex
}

// maxCachedBooks bounds the cache: a few hundred KB per book of 2000
// contacts.
const maxCachedBooks = 32

func newPIMCache() *pimCache { return &pimCache{books: map[string]*bookIndex{}} }

// volumeOf names the volume the contacts are on now (RAM only).
func volumeOf(store *pimstore.Store) string {
	if store.Current == nil {
		return "."
	}
	return store.Current(string(pimstore.Contacts))
}

// book returns the prepared rows of an address book and its ctag. A nil
// cache (the agent's toolbox) prepares them every time.
func (c *pimCache) book(store *pimstore.Store, user, book string) (*cardweb.ListIndex, string, error) {
	if c == nil {
		objects, ctag, err := store.ObjectsTag(pimstore.Contacts, user, book)
		if err != nil {
			return nil, "", err
		}
		return cardweb.NewListIndex(book, objects), ctag, nil
	}
	col, err := store.GetCollection(pimstore.Contacts, user, book)
	if err != nil {
		return nil, "", err
	}
	name, volume := user+"\x00"+book, volumeOf(store)
	c.mu.Lock()
	if b := c.books[name]; b != nil && b.key == volume+"\x00"+col.CTag {
		c.mu.Unlock()
		return b.ix, col.CTag, nil
	}
	c.mu.Unlock()
	objects, ctag, err := store.ObjectsTag(pimstore.Contacts, user, book)
	if err != nil {
		return nil, "", err
	}
	ix := cardweb.NewListIndex(book, objects)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.books[name]; !ok {
		c.order = append(c.order, name)
		for len(c.order) > maxCachedBooks {
			delete(c.books, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.books[name] = &bookIndex{key: volume + "\x00" + ctag, ix: ix}
	return ix, ctag, nil
}

// forget drops what the cache keeps of an account (deleted).
func (c *pimCache) forget(user string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.order[:0]
	for _, name := range c.order {
		if strings.HasPrefix(name, user+"\x00") {
			delete(c.books, name)
			continue
		}
		kept = append(kept, name)
	}
	c.order = kept
}

// pimETag is the ETag of an answer made of these parts (ctags, the
// window, the query …): the same parts, the same answer. "p1" names the
// form of the answers; a change of it changes every tag.
func pimETag(parts ...string) string {
	sum := sha256.Sum256([]byte("p1\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}
