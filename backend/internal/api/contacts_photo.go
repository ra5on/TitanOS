package api

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/photo"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Pictures of contacts (TECHNIK 5.5): in the card as PHOTO (base64, as the
// devices keep it), for the page a re-encoded JPEG of at most 512 px in
// photos/ next to the book (a cache, made again from the card when it is
// missing). A picture chosen in TitanOS (uploaded, or a file of the NAS
// read with the account's file rights) goes into the card shrunk to 512 px
// and 256 KiB.

// maxContactPhoto bounds an uploaded picture or a file of the NAS.
const maxContactPhoto = 20 << 20

// handleContactPhoto serves the picture of a contact for the page: a JPEG
// TitanOS made, never shown as anything else (nosniff, sandbox).
func (a pimDataArea) handleContactPhoto(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	book, name := r.PathValue("book"), r.PathValue("id")
	user := currentUser(r).Name
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
	noPhoto := func() {
		writeError(w, http.StatusNotFound, "contact_photo_not_found", contactsMessages["contact_photo_not_found"])
	}
	if !info.HasPhoto {
		noPhoto()
		return
	}
	jpeg, err := store.Photo(user, name)
	if errors.Is(err, pimstore.ErrNotFound) {
		obj, gerr := store.Get(pimstore.Contacts, user, book, name)
		if gerr != nil {
			contactsFail(w, r, book, gerr, "contact_not_found")
			return
		}
		pic, ok := cardweb.Photo(obj.Data)
		if !ok {
			noPhoto() // by reference (never fetched) or not readable
			return
		}
		if jpeg, err = photo.Reencode(pic, photo.Contact(photo.DevicePixels)); err != nil {
			noPhoto()
			return
		}
		if book == pimstore.DefaultBook {
			if serr := store.SavePhoto(user, name, obj.ETag, jpeg); serr != nil && !errors.Is(serr, pimstore.ErrPrecondition) {
				slog.Warn("Contacts: picture not kept", "account", user, "err", serr)
			}
		}
	} else if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, no-cache")
	h.Set("ETag", `"`+info.ETag+`"`)
	_, _ = w.Write(jpeg)
}

// readContactPhoto reads the picture of a PUT: a multipart upload (field
// "file") or JSON {root, path} of a file of the NAS, read with the rights
// of the account (the roots it may open; missing and foreign are the same
// 404). It answers errors itself.
func (a pimDataArea) readContactPhoto(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	tooLarge := func() {
		writeError(w, http.StatusRequestEntityTooLarge, "contacts_too_large", "the picture is larger than 20 MB")
	}
	switch ct {
	case "application/json":
		var q struct {
			Root string `json:"root"`
			Path string `json:"path"`
		}
		if !readJSON(w, r, &q) {
			return nil, false
		}
		if q.Root == "" || q.Path == "" || a.files == nil || a.roots == nil {
			writeError(w, http.StatusBadRequest, "contacts_photo_invalid", "root and path name the picture")
			return nil, false
		}
		var root files.Root
		found := false
		for _, rt := range a.roots(r) {
			if rt.ID == q.Root {
				root, found = rt, true
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "file_not_found", "no such share")
			return nil, false
		}
		f, info, err := a.files.Open(root, q.Path)
		if err != nil {
			writeFileError(w, err)
			return nil, false
		}
		defer f.Close()
		if !info.Mode().IsRegular() {
			writeError(w, http.StatusBadRequest, "contacts_photo_invalid", "only a file can be a picture")
			return nil, false
		}
		if info.Size() > maxContactPhoto {
			tooLarge()
			return nil, false
		}
		data, err := io.ReadAll(io.LimitReader(f, maxContactPhoto+1))
		if err != nil {
			writeFileError(w, err)
			return nil, false
		}
		if len(data) > maxContactPhoto {
			tooLarge()
			return nil, false
		}
		return data, true
	case "multipart/form-data":
		r.Body = http.MaxBytesReader(w, r.Body, maxContactPhoto+(64<<10))
		mr, err := r.MultipartReader()
		if err != nil {
			writeError(w, http.StatusBadRequest, "contacts_photo_invalid", "a multipart body with the field file is required")
			return nil, false
		}
		for {
			part, err := mr.NextPart()
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				tooLarge()
				return nil, false
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, "contacts_photo_invalid", "the field file is missing")
				return nil, false
			}
			if part.FormName() != "file" {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(part, maxContactPhoto+1))
			if errors.As(err, &maxErr) || len(data) > maxContactPhoto {
				tooLarge()
				return nil, false
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, "contacts_photo_invalid", "the picture could not be read")
				return nil, false
			}
			return data, true
		}
	}
	writeError(w, http.StatusBadRequest, "contacts_photo_invalid", "send the picture as multipart (field file) or JSON {root, path}")
	return nil, false
}

// handlePutContactPhoto puts a picture into the card. ?etag= (optional)
// guards against a change meanwhile. The answer has the new etag.
func (a pimDataArea) handlePutContactPhoto(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	book, name := r.PathValue("book"), r.PathValue("id")
	user := currentUser(r).Name
	_, obj, err := a.openContact(store, user, book, name)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	if etag := r.URL.Query().Get("etag"); etag != "" && !sameETag(etag, obj.ETag) {
		a.contactConflict(w, store, user, book, name)
		return
	}
	size := int64(maxContactPhoto)
	if r.ContentLength > 0 && r.ContentLength < size {
		size = r.ContentLength
	}
	release, ok := a.reserveRAM(w, pimstore.Contacts, 2*size)
	if !ok {
		return
	}
	defer release()
	pic, ok := a.readContactPhoto(w, r)
	if !ok {
		return
	}
	opt := photo.Contact(photo.DefaultMaxPixels)
	config, err := photo.Config(pic, opt)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	releaseDecode, ok := a.reserveRAM(w, pimstore.Contacts, photo.DecodeBytes(config))
	if !ok {
		return
	}
	jpeg, err := photo.Reencode(pic, opt)
	releaseDecode()
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	a.writeContactPhoto(w, r, store, book, name, obj, jpeg)
}

// handleDeleteContactPhoto removes the picture from the card.
func (a pimDataArea) handleDeleteContactPhoto(w http.ResponseWriter, r *http.Request) {
	store, ok := a.contactsStore(w)
	if !ok {
		return
	}
	book, name := r.PathValue("book"), r.PathValue("id")
	user := currentUser(r).Name
	_, obj, err := a.openContact(store, user, book, name)
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	if etag := r.URL.Query().Get("etag"); etag != "" && !sameETag(etag, obj.ETag) {
		a.contactConflict(w, store, user, book, name)
		return
	}
	a.writeContactPhoto(w, r, store, book, name, obj, nil)
}

// writeContactPhoto stores the card with the picture (nil: without) and
// keeps the JPEG for the page.
func (a pimDataArea) writeContactPhoto(w http.ResponseWriter, r *http.Request, store *pimstore.Store, book, name string, obj pimstore.Object, jpeg []byte) {
	user := currentUser(r).Name
	has := jpeg != nil
	data, err := cardweb.SetPhoto(obj.Data, jpeg, a.now())
	if err != nil {
		contactsFail(w, r, book, err, "contact_not_found")
		return
	}
	etag := obj.ETag
	if !bytes.Equal(data, obj.Data) {
		res, err := store.Put(pimstore.Contacts, user, book, name, data, pimstore.PutOptions{IfMatch: obj.ETag})
		if errors.Is(err, pimstore.ErrPrecondition) {
			a.contactConflict(w, store, user, book, name)
			return
		}
		if err != nil {
			contactsFail(w, r, book, err, "contact_not_found")
			return
		}
		etag = res.ETag
	}
	if has && book == pimstore.DefaultBook {
		if err := store.SavePhoto(user, name, etag, jpeg); err != nil && !errors.Is(err, pimstore.ErrPrecondition) {
			slog.Warn("Contacts: picture not kept", "account", user, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, contactWritten{ID: name, Book: book, ETag: etag, HasPhoto: &has})
}

// reserveRAM takes RAM of the budget for the rest of a request; it
// answers 503 calendar_busy / contacts_busy itself when there is no room.
func (a pimDataArea) reserveRAM(w http.ResponseWriter, kind pimstore.Kind, bytes int64) (release func(), ok bool) {
	if a.reserve == nil {
		return func() {}, true
	}
	release, ok = a.reserve(bytes)
	if !ok {
		w.Header().Set("Retry-After", "30")
		code, what := "calendar_busy", "import"
		if kind == pimstore.Contacts {
			code, what = "contacts_busy", "request"
		}
		writeError(w, http.StatusServiceUnavailable, code, "TitanOS has no room in RAM for this "+strings.TrimSpace(what)+" now; try again in a moment")
		return nil, false
	}
	return release, true
}
