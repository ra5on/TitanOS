package tmail

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The address book learns the recipients of sent mail (no CardDAV). It
// lives on the system data volume in .titanos/mail/<account>/contacts.json
// and is written when a mail is sent or an entry is forgotten. Without a
// volume it learns in RAM until titand stops, and never keeps a file
// anywhere else (docs/TMAIL.md "Datenablage").

// Contact is an entry of the address book.
type Contact struct {
	Name  string    `json:"name"`
	Addr  string    `json:"addr"`
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

const (
	maxContacts      = 2000
	maxContactsFile  = 2 << 20
	contactsFileName = "contacts.json"
	maxSuggestions   = 20
)

type contactsFile struct {
	Owner    owner     `json:"owner"`
	Contacts []Contact `json:"contacts"`
}

type contactBook struct {
	mu    sync.Mutex
	users map[string]*userContacts
}

type userContacts struct {
	// onVolume: read from the volume (or there was none to read yet).
	onVolume bool
	list     map[string]*Contact
}

// openContacts opens the account's folder below .titanos/mail; release
// ends the use of the volume.
func (m *Manager) openContacts(user string, create bool) (*os.Root, func(), error) {
	if m.Volume == nil {
		return nil, nil, volumedata.ErrUnavailable
	}
	root, release, err := m.Volume()
	if err != nil {
		return nil, nil, err
	}
	if create {
		if err := root.Mkdir(user, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			release()
			return nil, nil, err
		}
	}
	st, err := root.Lstat(user)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		release()
		if err == nil {
			err = errors.New("tmail: unsafe folder of the address book")
		}
		return nil, nil, err
	}
	sub, err := root.OpenRoot(user)
	if err != nil {
		release()
		return nil, nil, err
	}
	return sub, func() { _ = sub.Close(); release() }, nil // closing a directory handle fails for nothing we could change
}

// loadContactsLocked reads the address book of an account once a volume
// is there (m.contacts.mu held); what was learned meanwhile stays.
func (m *Manager) loadContactsLocked(user string, account Account) *userContacts {
	b := &m.contacts
	if b.users == nil {
		b.users = map[string]*userContacts{}
	}
	uc := b.users[user]
	if uc == nil {
		uc = &userContacts{list: map[string]*Contact{}}
		b.users[user] = uc
	}
	if uc.onVolume {
		return uc
	}
	root, release, err := m.openContacts(user, false)
	if errors.Is(err, fs.ErrNotExist) {
		uc.onVolume = m.Volume != nil && volumeThere(m)
		return uc
	}
	if err != nil {
		return uc
	}
	defer release()
	var f contactsFile
	if err := volumedata.ReadJSON(root, contactsFileName, &f, maxContactsFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("T-Mail address book not readable", "account", user, "err", err)
		return uc
	}
	uc.onVolume = true
	// The book of an earlier account of the same name is not this one's.
	if !sameAccount(f.Owner, account) || f.Owner.Name == "" {
		return uc
	}
	for _, c := range f.Contacts {
		c := c
		k := strings.ToLower(c.Addr)
		if have := uc.list[k]; have != nil {
			have.Count += c.Count
			if c.Last.After(have.Last) {
				have.Last = c.Last
			}
			continue
		}
		uc.list[k] = &c
	}
	return uc
}

// volumeThere tells whether the volume of the address book is there now.
func volumeThere(m *Manager) bool {
	_, release, err := m.Volume()
	if err != nil {
		return false
	}
	release()
	return true
}

// saveContactsLocked writes the address book if a volume is there.
func (m *Manager) saveContactsLocked(user string, account Account, uc *userContacts) {
	root, release, err := m.openContacts(user, true)
	if err != nil {
		return
	}
	defer release()
	f := contactsFile{Owner: owner{Name: account.Name, Created: account.Created}, Contacts: []Contact{}}
	for _, c := range uc.list {
		f.Contacts = append(f.Contacts, *c)
	}
	sort.Slice(f.Contacts, func(i, j int) bool { return f.Contacts[i].Addr < f.Contacts[j].Addr })
	if err := volumedata.WriteJSON(root, contactsFileName, f); err != nil {
		slog.Warn("T-Mail address book not saved", "account", user, "err", err)
		return
	}
	uc.onVolume = true
}

// learn notes the recipients of a sent mail.
func (m *Manager) learn(user string, list []Address) {
	account, ok := m.lookup(user)
	if !ok || len(list) == 0 {
		return
	}
	b := &m.contacts
	b.mu.Lock()
	defer b.mu.Unlock()
	uc := m.loadContactsLocked(user, account)
	now := m.clock().UTC()
	for _, a := range list {
		k := strings.ToLower(strings.TrimSpace(a.Addr))
		if k == "" {
			continue
		}
		c := uc.list[k]
		if c == nil {
			c = &Contact{Addr: strings.TrimSpace(a.Addr)}
			uc.list[k] = c
		}
		if name := strings.TrimSpace(a.Name); name != "" {
			c.Name = clean(name, 256)
		}
		c.Count++
		c.Last = now
	}
	trimContacts(uc)
	m.saveContactsLocked(user, account, uc)
}

// trimContacts drops the oldest entries above maxContacts.
func trimContacts(uc *userContacts) {
	if len(uc.list) <= maxContacts {
		return
	}
	all := make([]*Contact, 0, len(uc.list))
	for _, c := range uc.list {
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Last.After(all[j].Last) })
	for _, c := range all[maxContacts:] {
		delete(uc.list, strings.ToLower(c.Addr))
	}
}

// Contacts suggests at most 20 entries for q (part of name or address),
// the most used first.
func (m *Manager) Contacts(user, q string) []Contact {
	account, ok := m.lookup(user)
	out := []Contact{}
	if !ok {
		return out
	}
	q = strings.ToLower(strings.TrimSpace(q))
	b := &m.contacts
	b.mu.Lock()
	defer b.mu.Unlock()
	uc := m.loadContactsLocked(user, account)
	for _, c := range uc.list {
		if q == "" || strings.Contains(strings.ToLower(c.Addr), q) || strings.Contains(strings.ToLower(c.Name), q) {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Last.After(out[j].Last)
	})
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

// ForgetContact removes an entry of the address book.
func (m *Manager) ForgetContact(user, addr string) {
	account, ok := m.lookup(user)
	if !ok {
		return
	}
	b := &m.contacts
	b.mu.Lock()
	defer b.mu.Unlock()
	uc := m.loadContactsLocked(user, account)
	k := strings.ToLower(strings.TrimSpace(addr))
	if _, ok := uc.list[k]; !ok {
		return
	}
	delete(uc.list, k)
	m.saveContactsLocked(user, account, uc)
}

// forgetContacts removes the address book of an account from RAM and the
// volume; false if the volume was not there (the file is ignored by an
// account of the same name made later: it names its owner).
func (m *Manager) forgetContacts(user string) bool {
	b := &m.contacts
	b.mu.Lock()
	delete(b.users, user)
	b.mu.Unlock()
	if m.Volume == nil {
		return true
	}
	root, release, err := m.Volume()
	if err != nil {
		return false
	}
	defer release()
	if err := root.RemoveAll(user); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("T-Mail address book of a deleted account not removed", "account", user, "err", err)
		return false
	}
	return true
}
