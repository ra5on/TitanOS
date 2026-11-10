package api

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/protect"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

// Notfallzugang (PLAN 54-S.4): a registered person asks; owners and
// administrators are told at once and may decline or grant now; without an
// answer the access is granted after the waiting time. A grant gives read
// access ("ro") on the chosen shares where the person has none yet –
// never write access, never more than they have – and the handbook; after
// 30 days (or when an owner ends it, the person is removed or the function
// is switched off) exactly what the grant added is taken back. Every step
// goes into the activity log.

func (s *Server) emergencyRecord(user, action, target string) {
	env := s.agentEnv()
	env.activity.Record(activity.Entry{At: time.Now().UTC(), User: user, Via: activity.ViaSystem, Action: action, Target: target, OK: true})
}

func (s *Server) emergencyWaitingCount() int {
	st, err := s.protect().emergency.State()
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range st.Requests {
		if r.State == protect.StateWaiting {
			n++
		}
	}
	return n
}

func (s *Server) emergencyLatest(account string) (protect.Request, bool) {
	st, err := s.protect().emergency.State()
	if err != nil {
		return protect.Request{}, false
	}
	for i := len(st.Requests) - 1; i >= 0; i-- {
		if st.Requests[i].Account == account {
			return st.Requests[i], true
		}
	}
	return protect.Request{}, false
}

// emergencyAsk files a request of an account (via: web or agent).
func (s *Server) emergencyAsk(user auth.User, reason, code, via string) (protect.Request, error) {
	env := s.agentEnv()
	e := s.agentSettings().Features.Emergency
	if !e.Enabled {
		return protect.Request{}, errors.New("the emergency access is not switched on")
	}
	if user.Disabled {
		return protect.Request{}, protect.ErrNotRegistered
	}
	if code != "" && !e.Code {
		return protect.Request{}, protect.ErrBadCode
	}
	ps := s.protect()
	now := ps.now()
	req, err := ps.emergency.Ask(e.person(user.Name), user.Name, reason, code, now)
	if err != nil {
		return req, err
	}
	s.emergencyRecord(user.Name, "emergency access requested", fmt.Sprintf("%s (%s)", user.Name, via))
	slog.Warn("emergency access requested", "account", user.Name, "due", req.Due, "code", req.Code)
	if req.Code {
		return s.emergencyGrant(req, "", now)
	}
	if env.webPush != nil {
		body := fmt.Sprintf("Grund: „%s“. Ohne Antwort wird der Zugang am %s freigeschaltet.", req.Reason, req.Due.In(s.agentLocation()).Format("02.01. um 15:04"))
		for _, name := range s.notifyAudience() {
			if name == user.Name {
				continue
			}
			env.webPush.SendTo(name, webpush.Message{Title: "🔑 " + user.Name + " beantragt den Notfallzugang", Body: body, URL: "/agent?feature=emergency", Tag: "emergency-" + req.ID})
		}
	}
	return req, nil
}

// emergencyGrant gives the read access of a request and keeps what it added.
func (s *Server) emergencyGrant(req protect.Request, by string, now time.Time) (protect.Request, error) {
	env := s.agentEnv()
	ps := s.protect()
	ps.accessMu.Lock()
	var added []string
	var failed error
	for _, name := range req.Shares {
		sh, ok := s.shareByName(name)
		if !ok {
			continue // removed since: nothing to give
		}
		if _, has := sh.Access[req.Account]; has {
			continue // the person has access already; never more
		}
		sh.Access = maps.Clone(sh.Access)
		if sh.Access == nil {
			sh.Access = map[string]string{}
		}
		sh.Access[req.Account] = shares.ReadOnly
		if err := s.updateShare(sh); err != nil {
			failed = err
			break
		}
		added = append(added, name)
	}
	ps.accessMu.Unlock()
	if failed != nil {
		s.emergencyRevoke(req.Account, added)
		return req, fmt.Errorf("access not granted: %w", failed)
	}
	out, err := ps.emergency.Grant(req.ID, by, added, now)
	if err != nil {
		s.emergencyRevoke(req.Account, added)
		return out, err
	}
	how := "after the waiting time"
	if by != "" {
		how = "by " + by
	} else if out.Code {
		how = "with the one-time code"
	}
	s.emergencyRecord(req.Account, "emergency access granted", fmt.Sprintf("%s: %v (%s)", req.Account, added, how))
	slog.Warn("emergency access granted", "account", req.Account, "shares", added, "how", how, "ends", out.Ends)
	if env.webPush != nil {
		env.webPush.SendTo(req.Account, webpush.Message{Title: "🔑 Notfallzugang freigeschaltet", Body: "Du kannst jetzt lesend auf die Freigaben und das Notfallhandbuch zugreifen.", URL: "/agent?feature=emergency", Tag: "emergency-" + req.ID})
		for _, name := range s.notifyAudience() {
			env.webPush.SendTo(name, webpush.Message{Title: "🔑 Notfallzugang für " + req.Account + " freigeschaltet", Body: "Er endet von selbst nach 30 Tagen.", URL: "/agent?feature=emergency", Tag: "emergency-" + req.ID})
		}
	}
	return out, nil
}

// emergencyRevoke takes back the read access a grant added, where it is
// still what the grant set.
func (s *Server) emergencyRevoke(account string, added []string) {
	ps := s.protect()
	ps.accessMu.Lock()
	defer ps.accessMu.Unlock()
	for _, name := range added {
		sh, ok := s.shareByName(name)
		if !ok || sh.Access[account] != shares.ReadOnly {
			continue
		}
		sh.Access = maps.Clone(sh.Access)
		delete(sh.Access, account)
		if err := s.updateShare(sh); err != nil {
			slog.Error("emergency access not taken back", "account", account, "share", name, "err", err)
		}
	}
}

func (s *Server) emergencyEnd(req protect.Request, by, why string, now time.Time) error {
	s.emergencyRevoke(req.Account, req.Added)
	if _, err := s.protect().emergency.End(req.ID, by, now); err != nil {
		return err
	}
	s.emergencyRecord(req.Account, "emergency access ended", fmt.Sprintf("%s (%s)", req.Account, why))
	slog.Info("emergency access ended", "account", req.Account, "why", why)
	return nil
}

// emergencyTick grants what waited long enough and ends what ran out. The
// state is in RAM once read, so this reads nothing from the volume.
func (s *Server) emergencyTick(e agentEmergency, now time.Time) {
	if !e.Enabled {
		return
	}
	s.emergencyReconcile(e, now)
	s.emergencyDetach()
	grant, end, err := s.protect().emergency.Due(now)
	if err != nil {
		return
	}
	for _, r := range grant {
		if _, err := s.emergencyGrant(r, "", now); err != nil {
			slog.Error("emergency access not granted", "account", r.Account, "err", err)
		}
	}
	for _, r := range end {
		_ = s.emergencyEnd(r, "", "30 days over", now)
	}
}

// emergencyDetach notices shares whose entry for a granted person is no
// longer the "ro" the grant set (an administrator changed or removed it):
// from then on that entry is the administrator's, and the end of the grant
// does not touch it. Runs every minute from RAM (share list, state).
func (s *Server) emergencyDetach() {
	ps := s.protect()
	st, err := ps.emergency.State()
	if err != nil {
		return
	}
	for _, r := range st.Requests {
		if r.State != protect.StateGranted || len(r.Added) == 0 {
			continue
		}
		var changed []string
		for _, name := range r.Added {
			sh, ok := s.shareByName(name)
			if !ok || sh.Access[r.Account] != shares.ReadOnly {
				changed = append(changed, name)
			}
		}
		if len(changed) > 0 {
			if _, err := ps.emergency.Detach(r.ID, changed); err == nil {
				s.emergencyRecord(r.Account, "emergency access: share changed by an administrator, kept at the end", fmt.Sprintf("%s: %v", r.Account, changed))
			}
		}
	}
}

// emergencyReconcile ends requests and grants that no longer have a
// registered person (or when the function was switched off).
func (s *Server) emergencyReconcile(e agentEmergency, now time.Time) {
	env := s.agentEnv()
	ps := s.protect()
	st, err := ps.emergency.State()
	if err != nil {
		return
	}
	for _, r := range st.Requests {
		if !r.Open() {
			continue
		}
		p := e.person(r.Account)
		user, ok := env.users.Get(r.Account)
		gone := !e.Enabled || p == nil || !ok || user.Disabled
		if !gone {
			continue
		}
		if r.State == protect.StateWaiting {
			_, _ = ps.emergency.Cancel(r.ID, "", now)
			s.emergencyRecord(r.Account, "emergency access request canceled", r.Account)
		} else {
			_ = s.emergencyEnd(r, "", "no longer registered", now)
		}
	}
	var keep []string
	for _, p := range e.Persons {
		keep = append(keep, p.Account)
	}
	if !e.Code {
		keep = nil
	}
	_ = ps.emergency.DropCodes(keep)
}

// ---------- routes ----------

type emergencyPersonView struct {
	protect.Person
	CodeSet bool `json:"codeSet"`
}

func (s *Server) handleEmergency(w http.ResponseWriter, r *http.Request) {
	e := s.agentSettings().Features.Emergency
	ps := s.protect()
	st, err := ps.emergency.State()
	if err != nil {
		agentStoreError(w, err)
		return
	}
	persons := []emergencyPersonView{}
	for _, p := range e.Persons {
		_, set := st.Codes[p.Account]
		persons = append(persons, emergencyPersonView{Person: p, CodeSet: set})
	}
	list := slices.Clone(st.Requests)
	slices.Reverse(list)
	if list == nil {
		list = []protect.Request{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": e.Enabled, "code": e.Code, "persons": persons, "requests": list, "now": ps.now().UTC()})
}

func (s *Server) handleEmergencyRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
		Code   string `json:"code"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	user := currentUser(r)
	req, err := s.emergencyAsk(user, body.Reason, body.Code, "web")
	switch {
	case errors.Is(err, protect.ErrNotRegistered):
		writeError(w, http.StatusForbidden, "emergency_not_registered", err.Error())
	case errors.Is(err, protect.ErrOpenRequest):
		writeError(w, http.StatusConflict, "emergency_open", err.Error())
	case errors.Is(err, protect.ErrBadCode):
		writeError(w, http.StatusForbidden, "emergency_bad_code", err.Error())
	case errors.Is(err, protect.ErrCodeLocked):
		writeError(w, http.StatusTooManyRequests, "emergency_code_locked", err.Error())
	case err != nil:
		writeError(w, http.StatusBadRequest, "emergency", err.Error())
	default:
		writeJSON(w, http.StatusCreated, req)
	}
}

// handleEmergencyAction: decline, grant and end are for owners and
// administrators; a person may cancel their own waiting request.
func (s *Server) handleEmergencyAction(w http.ResponseWriter, r *http.Request) {
	env := s.agentEnv()
	user := currentUser(r)
	id, action := r.PathValue("id"), r.PathValue("action")
	ps := s.protect()
	st, err := ps.emergency.State()
	if err != nil {
		agentStoreError(w, err)
		return
	}
	i := slices.IndexFunc(st.Requests, func(x protect.Request) bool { return x.ID == id })
	if i < 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such request")
		return
	}
	req := st.Requests[i]
	noteTarget(r, req.Account)
	admin := user.Role.CanAdminister()
	if !admin && !(action == "cancel" && req.Account == user.Name) {
		writeError(w, http.StatusForbidden, "forbidden", "only owners and administrators decide")
		return
	}
	if req.Account == user.Name && action != "cancel" {
		writeError(w, http.StatusForbidden, "forbidden", "nobody decides on their own request")
		return
	}
	now := ps.now()
	var out protect.Request
	switch action {
	case "decline":
		out, err = ps.emergency.Decline(id, user.Name, now)
		if err == nil {
			s.emergencyRecord(user.Name, "emergency access declined", req.Account)
			if env.webPush != nil {
				env.webPush.SendTo(req.Account, webpush.Message{Title: "🔑 Notfallzugang abgelehnt", Body: "Dein Antrag wurde abgelehnt.", URL: "/agent?feature=emergency", Tag: "emergency-" + id})
			}
		}
	case "grant":
		out, err = s.emergencyGrant(req, user.Name, now)
	case "end":
		if req.State != protect.StateGranted {
			err = protect.ErrDecided
			break
		}
		err = s.emergencyEnd(req, user.Name, "ended by "+user.Name, now)
		out, _ = s.emergencyLatest(req.Account)
	case "cancel":
		out, err = ps.emergency.Cancel(id, user.Name, now)
		if err == nil {
			s.emergencyRecord(user.Name, "emergency access request canceled", req.Account)
		}
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown action")
		return
	}
	if errors.Is(err, protect.ErrDecided) {
		writeError(w, http.StatusConflict, "emergency_decided", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "emergency", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleEmergencyCode makes a printed one-time code for a person; it is
// shown this once, only its hash is kept.
func (s *Server) handleEmergencyCode(w http.ResponseWriter, r *http.Request) {
	e := s.agentSettings().Features.Emergency
	account := r.PathValue("account")
	if !e.Enabled || !e.Code {
		writeError(w, http.StatusBadRequest, "emergency_codes_off", "one-time codes are switched off")
		return
	}
	if e.person(account) == nil {
		writeError(w, http.StatusNotFound, "not_found", "not registered")
		return
	}
	code, err := s.protect().emergency.NewCode(account)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	s.emergencyRecord(currentUser(r).Name, "emergency code made", account)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}
