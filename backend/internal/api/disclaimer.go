package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

// The disclaimer (DISCLAIMER.md in every TitanOS repository, owner
// decision of 23 Nov 2026): the owner confirms once that they have read it,
// when setting the NAS up or, on a NAS set up before, at the next login.
// The confirmation lives in /config/system/disclaimer.json, written only
// then; it is read once and kept in RAM.

const (
	disclaimerFile = "system/disclaimer.json"
	// disclaimerVersion counts versions of the text. A changed text that
	// must be confirmed anew raises it.
	disclaimerVersion = 1
)

type disclaimerState struct {
	Version int       `json:"version"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
}

// disclaimerStore holds the confirmation in RAM after the first read.
type disclaimerStore struct {
	mu     sync.Mutex
	loaded bool
	state  disclaimerState
}

func (d *disclaimerStore) get(store *config.Store) disclaimerState {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.loaded && store != nil {
		var st disclaimerState
		err := store.ReadJSON(disclaimerFile, &st)
		switch {
		case err == nil:
			d.state, d.loaded = st, true
		case errors.Is(err, fs.ErrNotExist):
			d.loaded = true
		default:
			// Unreadable now (the stick is away): ask again later, never
			// treat it as confirmed.
			slog.Warn("disclaimer confirmation not readable", "err", err)
		}
	}
	return d.state
}

func (d *disclaimerStore) accept(store *config.Store, by string) (disclaimerState, error) {
	st := disclaimerState{Version: disclaimerVersion, By: by, At: time.Now().UTC().Truncate(time.Second)}
	if store == nil {
		return st, errors.New("no configuration store")
	}
	if err := store.WriteJSON(disclaimerFile, st); err != nil {
		return disclaimerState{}, err
	}
	d.mu.Lock()
	d.state, d.loaded = st, true
	d.mu.Unlock()
	return st, nil
}

// disclaimerArea serves the confirmation: the stick for the file, the
// state in RAM at the server.
type disclaimerArea struct {
	store *config.Store
	state *disclaimerStore
}

func (a disclaimerArea) register(mux *routeMux, user, browserOnly func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/disclaimer", user(a.handleDisclaimer))
	mux.HandleFunc("POST /api/v1/system/disclaimer", user(browserOnly(a.handleDisclaimerAccept)))
}

func disclaimerView(st disclaimerState, user auth.User) map[string]any {
	accepted := st.Version >= disclaimerVersion
	view := map[string]any{"version": disclaimerVersion, "accepted": accepted,
		// Only the owner is asked; everybody can read the text.
		"required": !accepted && user.Role == auth.RoleOwner}
	if st.Version > 0 {
		view["by"], view["at"], view["acceptedVersion"] = st.By, st.At, st.Version
	}
	return view
}

func (a disclaimerArea) handleDisclaimer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, disclaimerView(a.state.get(a.store), currentUser(r)))
}

func (a disclaimerArea) handleDisclaimerAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version int `json:"version"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := currentUser(r)
	if user.Role != auth.RoleOwner {
		writeError(w, http.StatusForbidden, "owner_only", "only the owner confirms the disclaimer")
		return
	}
	if req.Version != disclaimerVersion {
		writeError(w, http.StatusConflict, "disclaimer_changed", "the disclaimer changed; read it again")
		return
	}
	st, err := a.state.accept(a.store, user.Name)
	if err != nil {
		slog.Error("disclaimer confirmation not stored", "err", err)
		writeError(w, http.StatusInternalServerError, "config_write_failed", "the confirmation could not be saved to /config")
		return
	}
	slog.Info("disclaimer confirmed", "user", user.Name, "version", st.Version, "ip", remoteIP(r))
	writeJSON(w, http.StatusOK, disclaimerView(st, user))
}
