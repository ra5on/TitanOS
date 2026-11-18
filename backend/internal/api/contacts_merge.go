package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Kontakte 1.13 (owner 18 Nov 2026): the side bar in one request
// (address books, groups, the number of possible duplicates), the search
// with T-Mail's suggestions in one request, possible duplicates and
// merging two contacts into one. Every route: a web session of this node
// (pimDataArea.register).

type duplicateView struct {
	Book string `json:"book"`
	cardweb.DuplicateSet
}

// contactDuplicates are the possible duplicates of every book of the
// account (cardweb.Duplicates on the index, no disk). Shared with the
// later tool contacts_duplicates of the agent.
func contactDuplicates(store *pimstore.Store, user string) ([]duplicateView, error) {
	cols, err := store.Collections(pimstore.Contacts, user)
	if err != nil {
		return nil, err
	}
	out := []duplicateView{}
	for _, c := range cols {
		objects, err := store.Objects(pimstore.Contacts, user, c.ID)
		if err != nil {
			return nil, err
		}
		for _, s := range cardweb.Duplicates(objects) {
			out = append(out, duplicateView{Book: c.ID, DuplicateSet: s})
		}
	}
	return out, nil
}

type contactsOverview struct {
	Books  []bookView  `json:"books"`
	Groups []groupView `json:"groups"`
	// Duplicates is the number of sets of possible duplicates.
	Duplicates int `json:"duplicates"`
}

// handleContactsOverview is what the side bar of Kontakte shows: the
// address books with their counts, the groups and how many sets of
// possible duplicates there are.
func (a pimDataArea) handleContactsOverview(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	user := currentUser(r).Name
	cols, err := store.Collections(pimstore.Contacts, user)
	if err != nil {
		contactsFail(w, r, "", err, "contact_not_found")
		return
	}
	out := contactsOverview{Books: []bookView{}, Groups: []groupView{}}
	for _, c := range cols {
		ix, _, err := a.cache.book(store, user, c.ID)
		if err != nil {
			contactsFail(w, r, c.ID, err, "contact_not_found")
			return
		}
		out.Books = append(out.Books, bookView{ID: c.ID, Name: c.Name, Count: ix.Len()})
		groups, err := contactGroups(store, user, c.ID)
		if err != nil {
			contactsFail(w, r, c.ID, err, "contact_not_found")
			return
		}
		out.Groups = append(out.Groups, groups...)
		objects, err := store.Objects(pimstore.Contacts, user, c.ID)
		if err != nil {
			contactsFail(w, r, c.ID, err, "contact_not_found")
			return
		}
		out.Duplicates += len(cardweb.Duplicates(objects))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a pimDataArea) handleContactDuplicates(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	out, err := contactDuplicates(store, currentUser(r).Name)
	if err != nil {
		contactsFail(w, r, "", err, "contact_not_found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type contactSearch struct {
	Contacts    []cardweb.Item      `json:"contacts"`
	Suggestions []contactSuggestion `json:"suggestions"`
}

// handleContactSearch answers a search of the page in one request: the
// contacts that match (like GET /contacts?q=) and, with T-Mail's grant,
// the learned addresses that match and are in no contact.
func (a pimDataArea) handleContactSearch(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	user, q := currentUser(r).Name, r.URL.Query().Get("q")
	cols, err := store.Collections(pimstore.Contacts, user)
	if err != nil {
		contactsFail(w, r, "", err, "contact_not_found")
		return
	}
	local, now := store.Zone(), a.now()
	out := contactSearch{Contacts: []cardweb.Item{}, Suggestions: []contactSuggestion{}}
	known := map[string]bool{}
	for _, c := range cols {
		ix, _, err := a.cache.book(store, user, c.ID)
		if err != nil {
			contactsFail(w, r, c.ID, err, "contact_not_found")
			return
		}
		out.Contacts = append(out.Contacts, ix.List(q, now, local)...)
	}
	if strings.TrimSpace(q) != "" && a.mail != nil && a.mail.Allowed(user) {
		if known, err = contactAddresses(store, user); err != nil {
			contactsFail(w, r, "", err, "contact_not_found")
			return
		}
		for _, c := range a.learned(user, q, maxContactSuggestions, known) {
			out.Suggestions = append(out.Suggestions, contactSuggestion{Name: c.Name, Addr: c.Addr, Count: c.Count})
		}
	}
	writeJSONList(w, r, out)
}

// contactRow is the row of the list of one contact after a write (the
// page puts it in place instead of reading the list again); nil when it
// cannot be read now.
func (a pimDataArea) contactRow(store *pimstore.Store, user, book, name string) *cardweb.Item {
	info, err := store.Stat(pimstore.Contacts, user, book, name)
	if err != nil || info.Group {
		return nil
	}
	rows := cardweb.NewListIndex(book, []pimstore.ObjectInfo{info}).List("", a.now(), store.Zone())
	if len(rows) != 1 {
		return nil
	}
	return &rows[0]
}

type mergeSide struct {
	ID   string `json:"id"`
	ETag string `json:"etag"`
}

type mergeRequest struct {
	Book string    `json:"book"`
	Keep mergeSide `json:"keep"`
	Drop mergeSide `json:"drop"`
	// Contact: the fields as chosen in the preview (all of them).
	Contact cardweb.Input `json:"contact"`
	// Photo: "keep" (default), "drop" or "none".
	Photo string `json:"photo"`
}

type mergeResult struct {
	contactWritten
	// Groups is the number of groups whose membership moved to the kept
	// contact.
	Groups int `json:"groups"`
}

// handleMergeContacts takes one contact into another: the kept card
// gets the chosen fields and what TitanOS does not show of the other
// (cardweb.Merge), the groups of the other name the kept one, then the
// other is deleted. Both etags are required and checked first; the steps
// run in this order so that nothing is lost when one fails (the other
// contact is deleted last).
func (a pimDataArea) handleMergeContacts(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	var q mergeRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxContactBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	user, book := currentUser(r).Name, bookParam(r, q.Book)
	switch q.Photo {
	case "", "keep", "drop", "none":
	default:
		writeError(w, http.StatusBadRequest, "contacts_invalid", "photo is keep, drop or none")
		return
	}
	if q.Keep.ID == "" || q.Drop.ID == "" || q.Keep.ID == q.Drop.ID {
		writeError(w, http.StatusBadRequest, "contacts_invalid", "two different contacts are needed")
		return
	}
	if strings.TrimSpace(q.Keep.ETag) == "" || strings.TrimSpace(q.Drop.ETag) == "" {
		writeError(w, http.StatusBadRequest, "contacts_invalid", "etag is required")
		return
	}
	_, keep, err := a.openContact(store, user, book, q.Keep.ID)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	_, drop, err := a.openContact(store, user, book, q.Drop.ID)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	if !sameETag(q.Keep.ETag, keep.ETag) {
		a.contactConflict(w, store, user, book, q.Keep.ID)
		return
	}
	if !sameETag(q.Drop.ETag, drop.ETag) {
		a.contactConflict(w, store, user, book, q.Drop.ID)
		return
	}
	data, err := cardweb.Merge(keep.Data, drop.Data, q.Contact, q.Photo, a.now())
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	res, err := store.Put(pimstore.Contacts, user, book, q.Keep.ID, data, pimstore.PutOptions{IfMatch: keep.ETag})
	if errors.Is(err, pimstore.ErrPrecondition) {
		a.contactConflict(w, store, user, book, q.Keep.ID)
		return
	}
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	keepInfo, _ := store.Stat(pimstore.Contacts, user, book, q.Keep.ID)
	dropInfo, _ := store.Stat(pimstore.Contacts, user, book, q.Drop.ID)
	groups, err := a.rewriteMembership(store, user, book, dropInfo.UID, keepInfo.UID)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	if err := store.DeleteObject(pimstore.Contacts, user, book, q.Drop.ID, drop.ETag); err != nil {
		// The kept contact holds everything now; the other stays as it was.
		if errors.Is(err, pimstore.ErrPrecondition) {
			a.contactConflict(w, store, user, book, q.Drop.ID)
			return
		}
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	out := mergeResult{contactWritten: contactWritten{ID: q.Keep.ID, Book: book, ETag: res.ETag, Item: a.contactRow(store, user, book, q.Keep.ID), ListETag: a.listTagNow(store, user)}, Groups: groups}
	writeJSON(w, http.StatusOK, out)
}
