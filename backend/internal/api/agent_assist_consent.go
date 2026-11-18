package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The consent of an account to Assist reading its calendar and contacts
// (1.11, Sicherheitsdurchsicht S-23). Questions from Home Assistant run
// with the rights of the account "Assist als" (else the first owner), and
// any administrator sets that account and assistData. So that no
// administrator reads the owner's (or another administrator's) calendar
// through Assist, assistData "read" holds only for the areas that very
// account agreed to, in its own web session.
//
// The consent lives in /config/users/<name>/assist-consent.json, apart
// from agent.json: 1.10 never reads it, and a rollback changes nothing in
// the settings it can read and save. It is bound to the account (name and
// creation time): a new account of the name has none, and it goes with the
// account (users/<name>). It is no setting of assistAs: choosing another
// account needs that account's consent.

// assistConsentFile is the consent of an account below /config.
func assistConsentFile(user string) string { return "users/" + user + "/assist-consent.json" }

// assistConsent is what an account agreed to.
type assistConsent struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	// Areas: calendar and/or contacts (agent.DataAreas).
	Areas []string  `json:"areas"`
	At    time.Time `json:"at"`
}

// assistConsents keeps what was read, so a question of Home Assistant
// does not read the stick every time.
type assistConsents struct {
	mu   sync.Mutex
	read map[string]*assistConsent // nil value: none stored
}

// assistConsentOf is the consent of an account as it is now: none for an
// earlier account of the name, none when it cannot be read.
func (s *Server) assistConsentOf(user auth.User) map[string]bool {
	out := map[string]bool{}
	if user.Name == "" || s.opts.Store == nil {
		return out
	}
	c := &s.assistConsent
	c.mu.Lock()
	defer c.mu.Unlock()
	consent, ok := c.read[user.Name]
	if !ok {
		var stored assistConsent
		err := s.opts.Store.ReadJSON(assistConsentFile(user.Name), &stored)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			// Not readable: no consent now, asked again next time.
			slog.Warn("Assist: consent not readable", "user", user.Name, "err", err)
			return out
		default:
			consent = &stored
		}
		if c.read == nil {
			c.read = map[string]*assistConsent{}
		}
		c.read[user.Name] = consent
	}
	if consent == nil || consent.Name != user.Name || !consent.Created.Equal(user.Created) {
		return out
	}
	for _, area := range consent.Areas {
		if agent.IsDataArea(area) {
			out[area] = true
		}
	}
	return out
}

// assistDataWithConsent is assistData as it holds for a run as the
// account: "read" only where the account agreed. waiting are the areas an
// administrator allowed that wait for the consent. The map is new, never
// the one of the cached settings.
func (s *Server) assistDataWithConsent(ha agentHASettings, acting auth.User) (map[string]string, []string) {
	consent := s.assistConsentOf(acting)
	out := map[string]string{}
	var waiting []string
	for _, area := range agent.DataAreas {
		if ha.AssistData[area] != assistRead {
			continue
		}
		if consent[area] {
			out[area] = assistRead
		} else {
			waiting = append(waiting, area)
		}
	}
	return out, waiting
}

// handleAgentAssistConsent gives or withdraws the consent of the account
// of the request, for itself only, in its own browser session: no API
// token, no agent, no other node of the cluster, no name in the request.
func (s *Server) handleAgentAssistConsent(w http.ResponseWriter, r *http.Request) {
	if viaCluster(r) || !webSession(r) || s.authArea().sessionToken(r) == "" {
		writeError(w, http.StatusForbidden, "web_session_required", "only the account itself, on the web interface of this NAS")
		return
	}
	var req map[string]bool
	if !readJSON(w, r, &req) {
		return
	}
	for area := range req {
		if !agent.IsDataArea(area) {
			writeError(w, http.StatusBadRequest, "invalid_consent", "calendar and contacts only")
			return
		}
	}
	user, ok := s.opts.Users.Get(currentUser(r).Name)
	if !ok || user.Disabled {
		writeError(w, http.StatusForbidden, "forbidden", "no such account")
		return
	}
	areas := []string{}
	for _, area := range agent.DataAreas {
		if req[area] {
			areas = append(areas, area)
		}
	}
	c := &s.assistConsent
	c.mu.Lock()
	var err error
	consent := &assistConsent{Name: user.Name, Created: user.Created, Areas: areas, At: time.Now().UTC()}
	if len(areas) == 0 {
		consent = nil
		if err = s.opts.Store.Remove(assistConsentFile(user.Name)); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	} else {
		err = s.opts.Store.WriteJSON(assistConsentFile(user.Name), consent)
	}
	if err == nil {
		if c.read == nil {
			c.read = map[string]*assistConsent{}
		}
		c.read[user.Name] = consent
	} else {
		// What is on the stick is not known now: read it again next time.
		delete(c.read, user.Name)
	}
	c.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
		return
	}
	slog.Info("Assist: consent to calendar and contacts", "user", user.Name, "areas", areas, "ip", remoteIP(r))
	out := map[string]bool{}
	for _, area := range agent.DataAreas {
		out[area] = slices.Contains(areas, area)
	}
	writeJSON(w, http.StatusOK, out)
}
