package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"filippo.io/age"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/pim/personalbackup"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

func (s *Server) registerPersonalBackup(mux *routeMux) {
	g := s.guards()
	web := func(fn http.HandlerFunc) http.HandlerFunc { return g.user(g.clusterLocal(g.browserOnly(fn))) }
	mux.HandleFunc("GET /api/v1/account/personal-backup", web(s.personalBackupSettings))
	mux.HandleFunc("PUT /api/v1/account/personal-backup", web(s.setPersonalBackup))
	mux.HandleFunc("POST /api/v1/account/personal-backup/key", web(s.personalBackupKey))
	mux.HandleFunc("GET /api/v1/account/personal-backup/archive", web(s.downloadPersonalBackup))
	mux.HandleFunc("POST /api/v1/account/personal-backup/restore", web(s.restorePersonalBackup))
	mux.HandleFunc("GET /api/v1/backups/personal-sources", g.admin(g.clusterLocal(g.browserOnly(s.personalBackupSources))))
}
func (s *Server) personalBackups() personalbackup.Manager {
	return personalbackup.Manager{Config: s.opts.Store, PIM: s.opts.PIM}
}
func backupPersonalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrRestoreBusy):
		writeError(w, 409, "personal_backup_busy", "another recovery is running")
	case errors.Is(err, store.ErrArchiveChanged):
		writeError(w, 409, "personal_backup_changed", "the collection changed during backup")
	case errors.Is(err, store.ErrArchive), errors.Is(err, store.ErrInvalidCollection), errors.Is(err, store.ErrPrecondition), errors.Is(err, store.ErrInvalidData):
		writeError(w, 400, "personal_backup_invalid", "check the archive, recovery key and selected collections")
	default:
		writeError(w, 503, "personal_backup_unavailable", "the personal data or backup settings are unavailable")
	}
}
func (s *Server) personalBackupSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	m := s.personalBackups()
	account := currentUser(r).Name
	settings, err := m.Settings(account)
	if err != nil {
		backupPersonalError(w, err)
		return
	}
	var sources []store.ArchiveCollection
	var unavailable []store.Kind
	for _, kind := range []store.Kind{store.Calendar, store.Contacts, store.Tasks} {
		list, err := m.PIM.ArchiveCollections(account, kind)
		if err != nil {
			unavailable = append(unavailable, kind)
			continue
		}
		sources = append(sources, list...)
	}
	writeJSON(w, 200, map[string]any{"settings": settings, "collections": sources, "unavailable": unavailable})
}
func (s *Server) setPersonalBackup(w http.ResponseWriter, r *http.Request) {
	var request personalbackup.Settings
	if !readJSON(w, r, &request) {
		return
	}
	if err := s.personalBackups().Set(currentUser(r).Name, request); err != nil {
		backupPersonalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) personalBackupKey(w http.ResponseWriter, r *http.Request) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		backupPersonalError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]string{"identity": id.String(), "recipient": id.Recipient().String()})
}

// Operators see the opted-in account and key fingerprint, never the names,
// contents or sizes of another person's collections or their private key.
func (s *Server) personalBackupSources(w http.ResponseWriter, r *http.Request) {
	out, err := s.optedInPersonalSources()
	if err != nil {
		backupPersonalError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) optedInPersonalSources() ([]backup.PersonalSource, error) {
	out := []backup.PersonalSource{}
	m := s.personalBackups()
	if m.PIM != nil {
		for _, u := range s.opts.Users.List() {
			if u.Disabled {
				continue
			}
			settings, err := m.Settings(u.Name)
			if err != nil {
				return nil, err
			}
			if id := settings.ID(); id != "" {
				out = append(out, backup.PersonalSource{Account: u.Name, ID: id})
			}
		}
	}
	return out, nil
}
func (s *Server) downloadPersonalBackup(w http.ResponseWriter, r *http.Request) {
	m := s.personalBackups()
	account := currentUser(r).Name
	settings, err := m.Settings(account)
	if err != nil {
		backupPersonalError(w, err)
		return
	}
	if settings.ID() == "" {
		backupPersonalError(w, store.ErrPrecondition)
		return
	}
	file, release, err := m.PIM.ArchiveScratch(settings.Sources[0].Kind)
	if err != nil {
		backupPersonalError(w, err)
		return
	}
	defer release()
	// Withhold every byte until the collection versions and the account's
	// grant have been checked again and the encryption stream has closed.
	if err = m.Write(r.Context(), file, account, settings.ID(), ""); err != nil {
		backupPersonalError(w, err)
		return
	}
	size, err := file.Seek(0, io.SeekCurrent)
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		backupPersonalError(w, err)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="TitanOS-`+account+`.tar.age"`)
	if _, err = io.Copy(w, file); err != nil {
		// Do not finish a partial download as a successful file.
		panic(http.ErrAbortHandler)
	}
}
func (s *Server) restorePersonalBackup(w http.ResponseWriter, r *http.Request) {
	m := s.personalBackups()
	if m.PIM == nil {
		backupPersonalError(w, store.ErrUnavailable)
		return
	}
	if s.opts.MemoryPolicy != nil {
		release, ok := s.opts.MemoryPolicy.TryReserve(32 << 20)
		if !ok {
			backupPersonalError(w, store.ErrRestoreBusy)
			return
		}
		defer release()
	}
	form, err := r.MultipartReader()
	if err != nil {
		backupPersonalError(w, store.ErrArchive)
		return
	}
	part, err := form.NextPart()
	if err != nil || part.FormName() != "identity" {
		backupPersonalError(w, store.ErrArchive)
		return
	}
	key, err := io.ReadAll(io.LimitReader(part, 257))
	if err != nil || len(key) > 256 {
		backupPersonalError(w, store.ErrArchive)
		return
	}
	id, err := age.ParseX25519Identity(strings.TrimSpace(string(key)))
	if err != nil {
		backupPersonalError(w, store.ErrArchive)
		return
	}
	file, err := form.NextPart()
	if err != nil || file.FormName() != "file" {
		backupPersonalError(w, store.ErrArchive)
		return
	}
	plain, err := age.Decrypt(file, id)
	if err != nil {
		backupPersonalError(w, store.ErrArchive)
		return
	}
	apply := r.URL.Query().Get("apply") == "1"
	report, err := m.PIM.RestoreArchive(r.Context(), plain, currentUser(r).Name, apply)
	if err != nil {
		if apply && len(report.Collections) > 0 {
			writeJSON(w, 409, map[string]any{"partial": true, "report": report})
			return
		}
		if errors.Is(err, store.ErrUnavailable) || errors.Is(err, store.ErrRestoreBusy) {
			backupPersonalError(w, err)
			return
		}
		backupPersonalError(w, store.ErrArchive)
		return
	}
	writeJSON(w, 200, report)
}
