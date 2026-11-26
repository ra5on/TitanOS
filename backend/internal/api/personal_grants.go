package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"time"

	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"

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
	return s.personalGrantRevisions(u, "", u.Name)
}

// Inbox observation has no file-share dependency. Its own mailbox or user revocation
// still prevents importing messages that arrived before an explicit new subscription.
func (s *Server) personalGrantRevisions(u auth.User, names ...string) (string, error) {
	stamps := []string{string(u.Role)}
	for _, name := range names {
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

// No tool callback in this closure. PIM and other native writers may already hold their
// own resource lock. The full check runs immediately before that native transaction.
func (s *Server) personalCommitContext(ctx context.Context, u auth.User, id, token string, req pa.Request) context.Context {
	consent := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, ok := s.opts.Users.Get(u.Name)
		if !ok || current.Disabled || current.Role != u.Role || personalKey(current) != personalKey(u) {
			return errPersonalDenied
		}
		stamp, err := s.personalGrantStamp(current)
		if err != nil {
			return err
		}
		return s.personalTxn(current, false, func(a *personalAccount) error {
			j, err := a.find(id)
			if err != nil {
				return err
			}
			p := a.Plans[id]
			if p == nil || p.Claim != token || p.GrantStamp != stamp || p.PauseAfter || p.CancelAfter || a.Settings.Paused || j.Phase != "executing" || j.Status != "running" || p.Steps[p.Index] != req.Tool {
				return errPersonalDenied
			}
			for _, tool := range p.Steps[:p.Index+1] {
				t, ok := s.personal.tools[tool]
				if !ok || personalCap(t, a).Effective == pa.Never {
					return errPersonalDenied
				}
			}
			return nil
		})
	}
	resources := func(ctx context.Context) error {
		if _, err := s.personalWorkflowCheck(ctx, u, id, req); err != nil {
			return err
		}
		return consent(ctx)
	}
	return pa.WithAuthority(ctx, resources, consent)
}
