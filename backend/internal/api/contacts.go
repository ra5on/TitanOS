package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/photo"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The contacts of Kontakte (TECHNIK 7.2): one address book per account
// (kontakte), the cards as the devices keep them. Every route: a web
// session of this node (pimDataArea.register), the account's own data
// only, foreign ids 404, without the volume 503 contacts_unavailable
// (never an empty list). Group vCards (iOS groups) are hidden: not in the
// list, 404 here – they stay on the disk and under /carddav/, so the
// membership on the devices is never touched.

// contactsStore is the store when the contacts can be read now; else it
// answers 503 contacts_unavailable itself. From RAM only.
func (a pimDataArea) contactsStore(w http.ResponseWriter) (*pimstore.Store, bool) {
	if a.store == nil || (a.store.Current != nil && a.store.Current(string(pimstore.Contacts)) == "") {
		writePIMUnavailable(w, pimstore.Contacts)
		return nil, false
	}
	return a.store, true
}

// contactsCode maps an error of the store or of cardweb to status and code.
func contactsCode(err error, notFound string) (int, string) {
	var uc *pimstore.UIDConflictError
	switch {
	case errors.Is(err, pimstore.ErrUnavailable), errors.Is(err, volumedata.ErrHeld):
		return http.StatusServiceUnavailable, "contacts_unavailable"
	case errors.Is(err, pimstore.ErrNotFound), errors.Is(err, pimstore.ErrInvalidCollection), errors.Is(err, pimstore.ErrInvalidName):
		return http.StatusNotFound, notFound
	case errors.Is(err, pimstore.ErrPrecondition):
		return http.StatusConflict, "contacts_conflict"
	case errors.As(err, &uc):
		return http.StatusConflict, "contacts_uid_conflict"
	case errors.Is(err, pimstore.ErrCollectionFull):
		return http.StatusInsufficientStorage, "contacts_full"
	case errors.Is(err, pimstore.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "contacts_too_large"
	case errors.Is(err, cardweb.ErrInvalid), errors.Is(err, pimstore.ErrInvalidData):
		return http.StatusBadRequest, "contacts_invalid"
	case errors.Is(err, photo.ErrFormat), errors.Is(err, photo.ErrSize), errors.Is(err, photo.ErrDecode), errors.Is(err, photo.ErrEncode):
		return http.StatusBadRequest, "contacts_photo_invalid"
	}
	return http.StatusInternalServerError, "internal"
}

var contactsMessages = map[string]string{
	"contact_not_found":            "no such contact or address book",
	"contact_photo_not_found":      "the contact has no picture TitanOS can show",
	"contact_suggestion_not_found": "no such address learned by T-Mail",
	"contacts_conflict":            "the contact was changed meanwhile",
	"contact_exists":               "a contact has this address already",
	"contacts_uid_conflict":        "the contact may not change its UID",
	"contacts_full":                "the address book is full",
	"contacts_too_large":           "the contact or file is too large",
	"contacts_invalid":             "the contact is not valid",
	"contacts_photo_invalid":       "the picture must be JPEG, PNG or WebP and not too large",
	"internal":                     "the contacts could not be read or written",
}

// contactsFail answers an error. The journal gets account, book and code,
// never names or addresses; only failures of the server itself (500).
func contactsFail(w http.ResponseWriter, r *http.Request, book string, err error, notFound string) {
	status, code := contactsCode(err, notFound)
	if status == http.StatusServiceUnavailable {
		writePIMUnavailable(w, pimstore.Contacts)
		return
	}
	if status == http.StatusInternalServerError {
		slog.Error("Contacts: request failed", "account", currentUser(r).Name, "book", book, "code", code, "err", err)
	}
	writeError(w, status, code, contactsMessages[code])
}

// errNoContact is a group vCard or a contact of no book: 404.
var errNoContact = pimstore.ErrNotFound

// openContact reads a contact (never a group) with its etag.
func (a pimDataArea) openContact(store *pimstore.Store, user, book, name string) (cardweb.Contact, pimstore.Object, error) {
	if _, err := store.GetCollection(pimstore.Contacts, user, book); err != nil {
		return cardweb.Contact{}, pimstore.Object{}, err
	}
	info, err := store.Stat(pimstore.Contacts, user, book, name)
	if err != nil {
		return cardweb.Contact{}, pimstore.Object{}, err
	}
	if info.Group {
		return cardweb.Contact{}, pimstore.Object{}, errNoContact
	}
	obj, err := store.Get(pimstore.Contacts, user, book, name)
	if err != nil {
		return cardweb.Contact{}, pimstore.Object{}, err
	}
	c, err := cardweb.Open(obj.Data)
	if err != nil {
		return cardweb.Contact{}, obj, err
	}
	c.ID, c.Book, c.ETag = name, book, obj.ETag
	if !obj.MTime.IsZero() {
		c.Modified = obj.MTime.In(store.Zone()).Format(time.RFC3339)
	}
	return c, obj, nil
}

// handleContacts lists the contacts (groups never), searched with q, of
// one book or all (there is one). The rows come prepared from the cache
// (pim_cache.go). The answer has an ETag of the books' ctags, q, the week
// (birthdaySoon) and the zone; a page that sends it back as If-None-Match
// gets 304 without a body and keeps the list it has (1.13). The list
// stays no-store: the browser keeps no copy of its own.
func (a pimDataArea) handleContacts(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	user := currentUser(r).Name
	q, book := r.URL.Query().Get("q"), r.URL.Query().Get("book")
	var books []string
	if book != "" {
		if _, err := store.GetCollection(pimstore.Contacts, user, book); err != nil {
			contactsFail(w, r, book, err, "contact_not_found")
			return
		}
		books = []string{book}
	} else {
		cols, err := store.Collections(pimstore.Contacts, user)
		if err != nil {
			contactsFail(w, r, "", err, "contact_not_found")
			return
		}
		for _, c := range cols {
			books = append(books, c.ID)
		}
	}
	local, now := store.Zone(), a.now()
	monday, _ := cardweb.WeekOf(now, local)
	parts := []string{"contacts", user, volumeOf(store), q, monday.Format("2006-01-02"), local.String()}
	indexes := make([]*cardweb.ListIndex, 0, len(books))
	for _, b := range books {
		ix, ctag, err := a.cache.book(store, user, b)
		if err != nil {
			contactsFail(w, r, b, err, "contact_not_found")
			return
		}
		indexes = append(indexes, ix)
		parts = append(parts, b, ctag)
	}
	etag := pimETag(parts...)
	w.Header().Set("ETag", `"`+etag+`"`)
	if notModified(r, etag) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	out := []cardweb.Item{}
	for _, ix := range indexes {
		out = append(out, ix.List(q, now, local)...)
	}
	writeJSONList(w, r, out)
}

func (a pimDataArea) handleContact(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	book, name := r.PathValue("book"), r.PathValue("id")
	c, _, err := a.openContact(store, currentUser(r).Name, book, name)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// contactWritten is the answer of a write: where the contact is now.
type contactWritten struct {
	ID       string `json:"id"`
	Book     string `json:"book"`
	ETag     string `json:"etag"`
	HasPhoto *bool  `json:"hasPhoto,omitempty"`
}

type createContactRequest struct {
	Book    string        `json:"book"`
	Contact cardweb.Input `json:"contact"`
}

// maxContactBody bounds the JSON of a contact (notes of 32 KiB, 100
// numbers, addresses and e-mail addresses).
const maxContactBody = 512 << 10

// handleCreateContact makes a new vCard 3.0 (UID random, file <uid>.vcf).
func (a pimDataArea) handleCreateContact(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	var q createContactRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxContactBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	if q.Book == "" {
		q.Book = pimstore.DefaultBook
	}
	out, err := a.createContact(store, currentUser(r).Name, q.Book, q.Contact)
	if err != nil {
		contactsFail(w, r, q.Book, err, "contact_not_found")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a pimDataArea) createContact(store *pimstore.Store, user, book string, in cardweb.Input) (contactWritten, error) {
	if _, err := store.GetCollection(pimstore.Contacts, user, book); err != nil {
		return contactWritten{}, err
	}
	uid := calweb.NewUID()
	data, err := cardweb.Build(in, uid, a.now())
	if err != nil {
		return contactWritten{}, err
	}
	name := uid + ".vcf"
	res, err := store.Put(pimstore.Contacts, user, book, name, data, pimstore.PutOptions{IfNoneMatch: true})
	if err != nil {
		return contactWritten{}, err
	}
	return contactWritten{ID: name, Book: book, ETag: res.ETag}, nil
}

// contactConflict answers 409 contacts_conflict with the contact as it is
// now (null when it is gone).
func (a pimDataArea) contactConflict(w http.ResponseWriter, store *pimstore.Store, user, book, name string) {
	body := map[string]any{"error": apiError{Code: "contacts_conflict", Message: contactsMessages["contacts_conflict"]}, "current": nil}
	if c, _, err := a.openContact(store, user, book, name); err == nil {
		body["current"] = c
	}
	writeJSON(w, http.StatusConflict, body)
}

type updateContactRequest struct {
	ETag    string        `json:"etag"`
	Contact cardweb.Input `json:"contact"`
}

// handleUpdateContact merges the fields of the page into the card: what
// the page does not know stays (cardweb.Edit). The etag is required.
func (a pimDataArea) handleUpdateContact(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	book, name := r.PathValue("book"), r.PathValue("id")
	user := currentUser(r).Name
	var q updateContactRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxContactBody)
	if !readJSONBody(w, r, &q) {
		return
	}
	_, obj, err := a.openContact(store, user, book, name)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	if strings.TrimSpace(q.ETag) == "" {
		writeError(w, http.StatusBadRequest, "contacts_invalid", "etag is required")
		return
	}
	if !sameETag(q.ETag, obj.ETag) {
		a.contactConflict(w, store, user, book, name)
		return
	}
	data, changed, err := cardweb.Edit(obj.Data, q.Contact, a.now())
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	if !changed {
		writeJSON(w, http.StatusOK, contactWritten{ID: name, Book: book, ETag: obj.ETag})
		return
	}
	res, err := store.Put(pimstore.Contacts, user, book, name, data, pimstore.PutOptions{IfMatch: obj.ETag})
	if errors.Is(err, pimstore.ErrPrecondition) {
		a.contactConflict(w, store, user, book, name)
		return
	}
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	writeJSON(w, http.StatusOK, contactWritten{ID: name, Book: book, ETag: res.ETag})
}

// handleDeleteContact removes a contact (etag required); 204.
func (a pimDataArea) handleDeleteContact(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	book, name := r.PathValue("book"), r.PathValue("id")
	user := currentUser(r).Name
	etag := r.URL.Query().Get("etag")
	if _, err := store.GetCollection(pimstore.Contacts, user, book); err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	info, err := store.Stat(pimstore.Contacts, user, book, name)
	if err == nil && info.Group {
		err = errNoContact
	}
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	if strings.TrimSpace(etag) == "" {
		writeError(w, http.StatusBadRequest, "contacts_invalid", "etag is required")
		return
	}
	if !sameETag(etag, info.ETag) {
		a.contactConflict(w, store, user, book, name)
		return
	}
	err = store.DeleteObject(pimstore.Contacts, user, book, name, info.ETag)
	if errors.Is(err, pimstore.ErrPrecondition) {
		a.contactConflict(w, store, user, book, name)
		return
	}
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
