package api

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/ra5on/TitanOS/backend/internal/shares"
)

type shareResponse struct {
	shares.Share
	Path string `json:"path"`
	// Available is false while the share's volume is not mounted.
	Available bool `json:"available"`
}

var shareErrors = []struct {
	err    error
	status int
	code   string
}{
	{shares.ErrNotLoaded, http.StatusServiceUnavailable, "config_unavailable"},
	{shares.ErrInvalidName, http.StatusBadRequest, "invalid_share_name"},
	{shares.ErrExists, http.StatusConflict, "share_exists"},
	{shares.ErrNotFound, http.StatusNotFound, "share_not_found"},
	{shares.ErrUnknownVolume, http.StatusBadRequest, "unknown_volume"},
	{shares.ErrUnknownUser, http.StatusBadRequest, "unknown_user"},
	{shares.ErrInvalidAccess, http.StatusBadRequest, "invalid_access"},
	{shares.ErrInvalidClient, http.StatusBadRequest, "invalid_nfs_client"},
	{shares.ErrInvalidOption, http.StatusBadRequest, "invalid_option"},
	{shares.ErrVolumeChange, http.StatusConflict, "volume_change"},
}

func writeShareError(w http.ResponseWriter, err error) {
	for _, known := range shareErrors {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, err.Error())
			return
		}
	}
	slog.Error("share change failed", "err", err)
	writeError(w, http.StatusInternalServerError, "config_write_failed", "the share could not be saved to /config")
}

func (s *Server) userNames() []string {
	var names []string
	for _, user := range s.opts.Users.List() {
		names = append(names, user.Name)
	}
	return names
}

// handleListShares returns all shares to administrators and to everyone
// else the ones they have access to.
func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	mounted := s.opts.Storage.Mounted()
	list := []shareResponse{}
	for _, share := range s.opts.Shares.List() {
		if _, has := share.Access[user.Name]; !has && !user.Role.CanAdminister() {
			continue
		}
		if !user.Role.CanAdminister() {
			// Who else has access is not this account's business.
			share.Access = map[string]string{user.Name: share.Access[user.Name]}
			share.NFS.Clients = nil
		}
		list = append(list, shareResponse{Share: share, Path: s.opts.Shares.Path(share), Available: slices.Contains(mounted, share.Volume)})
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	var share shares.Share
	if !readJSON(w, r, &share) {
		return
	}
	// Samba does not offer a share that no one may open. Unless the form
	// says otherwise, whoever creates it can read and write.
	if len(share.Access) == 0 && !share.Guest {
		share.Access = map[string]string{currentUser(r).Name: shares.ReadWrite}
	}
	if err := s.opts.Shares.Create(share, s.opts.Storage.Names(), s.userNames()); err != nil {
		writeShareError(w, err)
		return
	}
	slog.Info("share created", "share", share.Name, "volume", share.Volume, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleUpdateShare(w http.ResponseWriter, r *http.Request) {
	var share shares.Share
	if !readJSON(w, r, &share) {
		return
	}
	name := r.PathValue("name")
	if err := s.opts.Shares.Update(name, share, s.opts.Storage.Names(), s.userNames()); err != nil {
		writeShareError(w, err)
		return
	}
	slog.Info("share changed", "share", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteShare stops sharing a folder. Its files stay on the volume.
func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.opts.Shares.Delete(name); err != nil {
		writeShareError(w, err)
		return
	}
	slog.Info("share deleted (files kept)", "share", name, "by", currentUser(r).Name, "ip", remoteIP(r))
	w.WriteHeader(http.StatusNoContent)
}
