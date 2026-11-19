package api

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Import and export of an address book as one .vcf file (TECHNIK 7.2).

// importPerCard is the RAM of a card beyond its bytes during an import
// (index entry, the parse that checks it).
const importPerCard = 4 << 10

type contactFailure struct {
	// Index is the card's place in the file, from 1 (1.15: "Karte 3").
	Index  int    `json:"index,omitempty"`
	UID    string `json:"uid,omitempty"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

type contactImportResult struct {
	Added   int              `json:"added"`
	Updated int              `json:"updated"`
	Skipped int              `json:"skipped"`
	Failed  []contactFailure `json:"failed"`
}

// readImportFile reads the field "file" of a multipart body of at most
// 20 MB; it answers errors itself with the codes of kind.
func readImportFile(w http.ResponseWriter, r *http.Request, invalid, tooLarge string) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImport+(64<<10))
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, invalid, "a multipart body with the field file is required")
		return nil, false
	}
	var data []byte
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, tooLarge, "the file is larger than 20 MB")
			return nil, false
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, invalid, "the multipart body could not be read")
			return nil, false
		}
		if part.FormName() != "file" || data != nil {
			continue
		}
		data, err = io.ReadAll(io.LimitReader(part, maxImport+1))
		if errors.As(err, &maxErr) || len(data) > maxImport {
			writeError(w, http.StatusRequestEntityTooLarge, tooLarge, "the file is larger than 20 MB")
			return nil, false
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, invalid, "the file could not be read")
			return nil, false
		}
	}
	if data == nil {
		writeError(w, http.StatusBadRequest, invalid, "the field file is missing")
		return nil, false
	}
	return data, true
}

// handleImportContacts imports a .vcf file (multipart field "file", at
// most 20 MB) into an address book: one file per card with the bytes as
// they came (a card without UID gets one), all written with one
// state.json (store.Import). Duplicates: by UID, a card without UID by FN
// and first e-mail address; onDuplicate=skip (default) leaves them,
// replace writes the card into the file the contact has (equal bytes are
// skipped). A card twice in the file counts once.
func (a pimDataArea) handleImportContacts(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	book := r.PathValue("id")
	user := currentUser(r).Name
	if _, err := store.GetCollection(pimstore.Contacts, user, book); err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	onDuplicate := r.URL.Query().Get("onDuplicate")
	if onDuplicate == "" {
		onDuplicate = "skip"
	}
	if onDuplicate != "skip" && onDuplicate != "replace" {
		writeError(w, http.StatusBadRequest, "contacts_invalid", "onDuplicate is skip or replace")
		return
	}
	size := int64(maxImport)
	if r.ContentLength > 0 && r.ContentLength < size {
		size = r.ContentLength
	}
	release, ok := a.reserveRAM(w, pimstore.Contacts, importFactor*size)
	if !ok {
		return
	}
	defer release()
	data, ok := readImportFile(w, r, "contacts_import_invalid", "contacts_too_large")
	if !ok {
		return
	}
	pieces := cardweb.SplitVCF(data)
	data = nil
	if len(pieces) == 0 {
		writeError(w, http.StatusBadRequest, "contacts_import_invalid", "the file holds no vCard")
		return
	}
	releaseCards, ok := a.reserveRAM(w, pimstore.Contacts, importPerCard*int64(len(pieces)))
	if !ok {
		return
	}
	defer releaseCards()
	existing, err := store.Objects(pimstore.Contacts, user, book)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	byUID := map[string]pimstore.ObjectInfo{}
	byKey := map[string]pimstore.ObjectInfo{}
	taken := map[string]bool{}
	for _, o := range existing {
		taken[o.Name] = true
		if o.UID != "" {
			byUID[o.UID] = o
		}
		email := ""
		if len(o.Emails) > 0 {
			email = o.Emails[0]
		}
		if k := cardweb.Key(o.FN, email); k != "" {
			if _, dup := byKey[k]; !dup {
				byKey[k] = o
			}
		}
	}
	out := contactImportResult{Failed: []contactFailure{}}
	seen := map[string]bool{}
	var items []pimstore.Item
	var who []contactFailure
	for at, p := range pieces {
		key := "uid:" + p.UID
		if p.UID == "" {
			key = "key:" + cardweb.Key(p.FN, p.Email)
		}
		if key != "key:" && seen[key] {
			out.Skipped++
			continue
		}
		seen[key] = true
		cardData := p.Data
		old, dup := byUID[p.UID]
		if p.UID == "" {
			old, dup = byKey[cardweb.Key(p.FN, p.Email)]
			if dup && old.UID != "" {
				cardData = cardweb.WithUID(cardData, old.UID)
			}
		}
		switch {
		case dup && (onDuplicate == "skip" || pimstore.ETag(cardData) == old.ETag):
			out.Skipped++
			continue
		case dup:
			items = append(items, pimstore.Item{Name: old.Name, Data: cardData})
		default:
			uid := p.UID
			if uid == "" {
				uid = calweb.NewUID()
				cardData = cardweb.WithUID(cardData, uid)
			}
			name := importName(pimstore.Contacts, uid, taken)
			taken[name] = true
			items = append(items, pimstore.Item{Name: name, Data: cardData})
		}
		who = append(who, contactFailure{Index: at + 1, UID: p.UID, Name: p.FN})
	}
	pieces = nil
	if len(items) > 0 {
		results, err := store.Import(r.Context(), pimstore.Contacts, user, book, items)
		if err != nil {
			contactsFail(w, r, book, err, "contact_not_found")
			return
		}
		for i, res := range results {
			switch {
			case res.Err != nil:
				f := who[i]
				f.Reason = importReason(res.Err)
				out.Failed = append(out.Failed, f)
			case res.Created:
				out.Added++
			default:
				out.Updated++
			}
		}
	}
	if len(out.Failed) > maxImportFailures {
		out.Failed = out.Failed[:maxImportFailures]
	}
	if len(out.Failed) > 0 {
		slog.Info("Contacts: import with failed entries", "account", user, "book", book, "failed", len(out.Failed))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleExportContacts writes an address book as one .vcf download: every
// card as stored (groups included, so a re-import keeps them), never
// shown inline (nosniff, sandbox).
func (a pimDataArea) handleExportContacts(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	book := r.PathValue("id")
	user := currentUser(r).Name
	if _, err := store.GetCollection(pimstore.Contacts, user, book); err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	objects, err := store.Objects(pimstore.Contacts, user, book)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	// One open of the folder for all entries (1.13), each read without
	// the collection's lock.
	rd, err := store.Reader(pimstore.Contacts, user, book)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	defer rd.Close()
	exportHeaders := func() {
		h := w.Header()
		h.Set("Content-Type", "text/vcard; charset=utf-8")
		h.Set("Content-Disposition", `attachment; filename="`+book+`.vcf"`)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "sandbox")
		h.Set("Cache-Control", "no-store")
	}
	started := false
	for _, o := range objects {
		obj, err := rd.Get(o.Name)
		if errors.Is(err, pimstore.ErrNotFound) {
			continue // deleted meanwhile
		}
		if err != nil && !started {
			contactsFail(w, r, book, err, "contact_not_found")
			return
		}
		if err != nil {
			// The answer has begun: a cut file must not look complete.
			slog.Error("Contacts: export cut short", "account", user, "book", book, "code", "read_failed")
			panic(http.ErrAbortHandler)
		}
		if !started {
			exportHeaders()
			started = true
		}
		data := obj.Data
		if !bytes.HasSuffix(data, []byte("\n")) {
			data = append(append([]byte{}, data...), '\r', '\n')
		}
		if _, err := w.Write(data); err != nil {
			return
		}
	}
	if !started {
		exportHeaders()
		w.WriteHeader(http.StatusOK)
	}
}
