package api

// Account pictures (PLAN 74, owner's decision of 8.11.2026): every account
// chooses the picture the login page shows in its circle – a photo of its
// own or one of the built-in pictures. Without a choice the page shows a
// part of a built-in picture picked by the name.
//
// Storage: /config/accounts/pictures.json (name → choice) and one small
// re-encoded JPEG per photo (accounts/picture-<sha256>.jpg, at most 256×256
// pixels, no metadata). Both are read once and kept in RAM; written only
// when an account changes its picture.
//
// Before a login a photo is served only under the same gate as the list of
// accounts (GET /login/accounts: the owner shows the accounts, from the home
// network unless "also from outside", never locked or hidden accounts);
// anything else is the same 404.

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sync"

	xdraw "golang.org/x/image/draw"
)

const (
	accountPicturesFile   = "accounts/pictures.json"
	accountPictureEdge    = 256
	maxAccountPictureSize = 12 << 20
)

var accountPictureID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// accountPicture is the choice of one account: a built-in picture or a photo.
type accountPicture struct {
	Builtin string `json:"builtin,omitempty"`
	Photo   string `json:"photo,omitempty"`
}

type accountPictures struct {
	mu     sync.Mutex
	loaded bool
	byName map[string]accountPicture
	photos map[string][]byte
}

func accountPictureFile(id string) string { return "accounts/picture-" + id + ".jpg" }

// pictures returns the stored choices; the file is read once.
func (s *Server) pictures() map[string]accountPicture {
	p := &s.accountPictures
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.loaded {
		p.byName = map[string]accountPicture{}
		_ = s.opts.Store.ReadJSON(accountPicturesFile, &p.byName)
		p.photos = map[string][]byte{}
		p.loaded = true
	}
	return maps.Clone(p.byName)
}

// setPicture stores the choice of an account (zero value: back to the standard).
func (s *Server) setPicture(name string, choice accountPicture, photo []byte) error {
	s.pictures()
	p := &s.accountPictures
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.byName[name]
	if photo != nil {
		if err := s.opts.Store.Write(accountPictureFile(choice.Photo), photo); err != nil {
			return err
		}
	}
	next := maps.Clone(p.byName)
	if choice == (accountPicture{}) {
		delete(next, name)
	} else {
		next[name] = choice
	}
	if err := s.opts.Store.WriteJSON(accountPicturesFile, next); err != nil {
		if photo != nil {
			_ = s.opts.Store.Remove(accountPictureFile(choice.Photo))
		}
		return err
	}
	p.byName = next
	if photo != nil {
		p.photos[choice.Photo] = photo
	}
	// The old photo goes when no account uses it any more.
	if old.Photo != "" && old.Photo != choice.Photo {
		used := false
		for _, other := range next {
			used = used || other.Photo == old.Photo
		}
		if !used {
			_ = s.opts.Store.Remove(accountPictureFile(old.Photo))
			delete(p.photos, old.Photo)
		}
	}
	return nil
}

// photo returns the bytes of a stored photo (read from the stick once).
func (s *Server) photo(id string) ([]byte, bool) {
	s.pictures()
	p := &s.accountPictures
	p.mu.Lock()
	defer p.mu.Unlock()
	if data, ok := p.photos[id]; ok {
		return data, true
	}
	data, err := s.opts.Store.Read(accountPictureFile(id))
	if err != nil {
		return nil, false
	}
	p.photos[id] = data
	return data, true
}

func (s *Server) handleAccountPicture(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.pictures()[currentUser(r).Name])
}

// handlePutAccountPicture takes a built-in picture as JSON ({"builtin": id})
// or a photo as the body (JPEG, PNG or WebP).
func (s *Server) handlePutAccountPicture(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	name := currentUser(r).Name
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAccountPictureSize))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the picture is too large")
		return
	}
	var choice accountPicture
	var photo []byte
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		if err := json.Unmarshal(data, &choice); err != nil || choice.Photo != "" || !slices.Contains(loginBackgrounds, choice.Builtin) {
			writeError(w, http.StatusBadRequest, "bad_request", "unknown picture")
			return
		}
	} else {
		photo, err = reencodeAccountPicture(data)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		choice = accountPicture{Photo: digest(photo)}
	}
	if err := s.setPicture(name, choice, photo); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	noteTarget(r, "account-picture")
	writeJSON(w, http.StatusOK, choice)
}

func (s *Server) handleDeleteAccountPicture(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebSession(w, r) {
		return
	}
	if err := s.setPicture(currentUser(r).Name, accountPicture{}, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	noteTarget(r, "account-picture")
	w.WriteHeader(http.StatusNoContent)
}

// shownAccounts are the accounts the login page may name for this request
// (the gate of GET /login/accounts).
func (s *Server) shownAccounts(r *http.Request) []string {
	look, _ := s.loginAppearance()
	if !look.ShowAccounts || !s.opts.Users.Loaded() || !(look.ShowAccountsExternal || s.fromHomeNetwork(r)) {
		return nil
	}
	var names []string
	for _, user := range s.opts.Users.List() {
		if !user.Disabled && !slices.Contains(look.HiddenAccounts, user.Name) {
			names = append(names, user.Name)
		}
	}
	slices.Sort(names)
	return names
}

// handleLoginPicture serves the photo of an account the login page names.
func (s *Server) handleLoginPicture(w http.ResponseWriter, r *http.Request) {
	// The address names the content (sha256); private: a shared cache never keeps it.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	notFound := func() { writeError(w, http.StatusNotFound, "not_found", "no such picture") }
	id := r.PathValue("id")
	if !accountPictureID.MatchString(id) {
		notFound()
		return
	}
	// Own bucket: a page with several photos must not use up the one of the account list.
	if ok, wait := s.allowLoginAccounts("picture:" + passkeySource(r)); !ok {
		writeRateLimited(w, wait, "too many requests, try again later")
		return
	}
	pictures := s.pictures()
	allowed := false
	for _, name := range s.shownAccounts(r) {
		allowed = allowed || pictures[name].Photo == id
	}
	data, ok := s.photo(id)
	if !allowed || !ok {
		notFound()
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

// handleAccountPicturePhoto serves the own photo to the logged-in account.
func (s *Server) handleAccountPicturePhoto(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data, ok := s.photo(id)
	if !accountPictureID.MatchString(id) || !ok || s.pictures()[currentUser(r).Name].Photo != id {
		writeError(w, http.StatusNotFound, "not_found", "no such picture")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// reencodeAccountPicture decodes an upload and writes a square JPEG of at
// most accountPictureEdge pixels: only pixels are kept (no location data).
func reencodeAccountPicture(data []byte) ([]byte, error) {
	if !slices.Contains(backgroundTypes, http.DetectContentType(data)) {
		return nil, errors.New("the picture must be JPEG, PNG or WebP")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 16 || config.Height < 16 || config.Width*config.Height > maxLoginImagePixels {
		return nil, errors.New("the picture could not be read or is too large")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("the picture could not be read")
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	crop.Max = crop.Min.Add(image.Pt(side, side))
	edge := min(side, accountPictureEdge)
	canvas := image.NewRGBA(image.Rect(0, 0, edge, edge))
	xdraw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.Black}, image.Point{}, xdraw.Src)
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), img, crop, xdraw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: 86}); err != nil {
		return nil, errors.New("the picture could not be saved")
	}
	return out.Bytes(), nil
}
