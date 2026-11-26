package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

const globalPersonalGrants = "system/personal-agent-resource-epoch.json"

func personalGrantFile(name string) string {
	if name == "" {
		return globalPersonalGrants
	}
	return "users/" + name + "/personal-agent-epoch.json"
}

// The revision lives on the config store, so revoking an app while the volume holding the
// approval is disconnected also invalidates the approval after remount or reboot.
func (s *Server) personalGrantStamp(u auth.User) (string, error) {
	stamps := []string{string(u.Role)}
	for _, name := range []string{"", u.Name} {
		var stamp string
		if err := s.opts.Store.ReadJSON(personalGrantFile(name), &stamp); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		stamps = append(stamps, stamp)
	}
	return personalDigest(stamps), nil
}
func (s *Server) personalEntitlementsChanged(name string) {
	if name != "" && auth.ValidateName(name) != nil {
		return
	}
	if err := s.opts.Store.WriteJSON(personalGrantFile(name), personalID()); err != nil {
		slog.Warn("Agent grant revision could not be saved", "error", err)
	}
	s.cancelPersonalRuns(name, nil)
}
func (s *Server) cancelPersonalRuns(name string, affected map[string]bool) {
	s.personal.mu.Lock()
	for token, l := range s.personal.leases {
		if (name == "" || l.Owner == name) && (affected == nil || affected[l.Job]) {
			if cancel := s.personal.active[token]; cancel != nil {
				cancel()
			}
		}
	}
	s.personal.mu.Unlock()
}

// A denied approval attempt consumes the previous token even when the upstream permission
// provider has no revision of its own.
func (s *Server) personalRejectApproval(u auth.User, id string) {
	_ = s.personalTxn(u, true, func(a *personalAccount) error {
		j, err := a.find(id)
		if err != nil {
			return err
		}
		if j.Status != "waiting" {
			return errPersonalConflict
		}
		p := a.Plans[id]
		p.Approval = ""
		p.ReadOK = false
		j.Token = ""
		j.Preview = nil
		j.Status = "failed"
		j.Phase = "complete"
		j.Error = "permission_revoked"
		j.Updated = time.Now().UTC()
		personalAudit(j, p, "denied")
		return nil
	})
}
