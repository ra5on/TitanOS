package api

import (
	"bytes"
	"encoding/json"
	"strings"

	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func (s *Server) personalBrowser(next http.HandlerFunc) http.HandlerFunc {
	g := s.guards()
	return g.user(g.clusterLocal(g.browserOnly(next)))
}
func (s *Server) registerPersonalAgent(mux *routeMux) {
	s.initPersonalTools()
	p := s.personalBrowser
	mux.HandleFunc("GET /api/v1/personal-agent/activity", p(s.personalActivity))
	mux.HandleFunc("GET /api/v1/personal-agent/state", p(s.personalState))
	mux.HandleFunc("PUT /api/v1/personal-agent/settings", p(s.personalSettings))
	mux.HandleFunc("GET /api/v1/personal-agent/memory", p(s.personalMemory))
	mux.HandleFunc("PUT /api/v1/personal-agent/memory", p(s.personalMemory))
	g := s.guards()
	admin := func(h http.HandlerFunc) http.HandlerFunc { return g.admin(g.clusterLocal(g.browserOnly(h))) }
	mux.HandleFunc("GET /api/v1/personal-agent/limits/{account}", admin(s.personalLimits))
	mux.HandleFunc("PUT /api/v1/personal-agent/limits/{account}", admin(s.personalLimits))
	mux.HandleFunc("POST /api/v1/personal-agent/jobs", p(s.personalCreate))
	mux.HandleFunc("GET /api/v1/personal-agent/jobs/{id}", p(s.personalGetJob))
	mux.HandleFunc("POST /api/v1/personal-agent/jobs/{id}/{action}", p(s.personalAct))
	mux.HandleFunc("GET /api/v1/personal-agent/skills", p(s.personalSkills))
	mux.HandleFunc("POST /api/v1/personal-agent/skills", p(s.personalSkills))
	mux.HandleFunc("DELETE /api/v1/personal-agent/skills/{id}", p(s.personalDeleteSkill))
	mux.HandleFunc("POST /api/v1/personal-agent/skills/{id}/run", p(s.personalRunSkill))
	mux.HandleFunc("GET /api/v1/personal-agent/search", p(s.personalSearch))
	s.registerPersonalAttachments(mux)
	s.registerPersonalPIMRoutes(mux)
	s.registerPersonalContextRoutes(mux)
}
func personalRespond(w http.ResponseWriter, v any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	var status int
	var code string
	switch {
	case errors.Is(err, errPersonalInvalid):
		status, code = 400, "agent_invalid"
	case errors.Is(err, errPersonalMissing):
		status, code = 404, "agent_missing"
	case errors.Is(err, errPersonalDenied):
		status, code = 403, "agent_denied"
	case errors.Is(err, errPersonalConflict):
		status, code = 409, "agent_changed"
	case errors.Is(err, errPersonalFull):
		status, code = 507, "agent_storage_full"
	default:
		status, code = 503, "agent_storage_unavailable"
	}
	writeError(w, status, code, code)
}
func (s *Server) personalState(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var a *personalAccount
	err := s.personalTxn(u, false, func(v *personalAccount) error { a = v; return nil })
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	state := pa.State{Chats: a.Chats, Jobs: a.Jobs, Settings: a.Settings, Capabilities: s.personalCaps(u, a)}
	titleVisible := map[string]bool{}
	for i := range state.Jobs {
		j := &state.Jobs[i]
		visible := s.personalRedact(r.Context(), u, a, j)
		if _, known := titleVisible[j.Request.ChatID]; !known {
			titleVisible[j.Request.ChatID] = visible
		}
	}
	for i := range state.Chats {
		if !titleVisible[state.Chats[i].ID] {
			state.Chats[i].Title = "Agent"
		}
	}
	personalRespond(w, state, nil)
}
func (s *Server) personalRedact(ctx context.Context, u auth.User, a *personalAccount, j *pa.Job) bool {
	t, ok := s.personal.tools[j.Request.Tool]
	fresh, active := s.opts.Users.Get(u.Name)
	visible := active && !fresh.Disabled && personalKey(fresh) == personalKey(u) && ok && personalCap(t, a).Effective != pa.Never
	u = fresh
	if visible && t.Available != nil {
		visible = t.Available(u) == nil
	}
	// Sensitive results from workflows require all their tools and selected sources to remain visible.
	scope := []string{j.Request.Tool}
	if plan := a.Plans[j.ID]; plan != nil {
		scope = plan.Steps
	}
	for _, tool := range scope {
		t, ok = s.personal.tools[tool]
		if !ok || personalCap(t, a).Effective == pa.Never || (t.Available != nil && t.Available(u) != nil) {
			visible = false
			break
		}
		if visible && t.Check != nil {
			req := s.personalStepRequest(j, a.Plans[j.ID], tool)
			if t.Check(ctx, u, req) != nil {
				visible = false
				break
			}
		}
	}
	if !visible {
		j.Preview = nil
		j.Result = nil
		j.Token = ""
		j.Request.Prompt = ""
		j.Request.Args = nil
		j.Request.Sources = nil
		j.Receipts = nil
	}
	return visible
}
func (s *Server) personalMemory(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Items []string `json:"items"`
	}
	set := r.Method == http.MethodPut
	if set {
		if !readPersonalJSON(w, r, &data) {
			return
		}
		if len(data.Items) > 100 {
			personalRespond(w, nil, errPersonalInvalid)
			return
		}
		for _, v := range data.Items {
			if len(v) > 2048 {
				personalRespond(w, nil, errPersonalInvalid)
				return
			}
		}
	}
	err := s.personalTxn(currentUser(r), set, func(a *personalAccount) error {
		if set {
			a.Memory = slices.Clone(data.Items)
		}
		data.Items = a.Memory
		return nil
	})
	personalRespond(w, data, err)
}
func (s *Server) personalSettings(w http.ResponseWriter, r *http.Request) {
	var patch personalPatch
	if !readPersonalJSON(w, r, &patch) {
		return
	}
	if !s.personalValidRights(patch.Rights) {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	u := currentUser(r)
	var affected map[string]bool
	err := s.personalTxn(u, true, func(a *personalAccount) error {
		before := maps.Clone(a.Settings.Rights)
		for id, requested := range patch.Rights {
			if before[id] != requested && lowerLevel(requested, personalCap(s.personal.tools[id], a).Ceiling) != requested {
				return errPersonalDenied
			}
		}

		if err := patch.apply(&a.Settings); err != nil {
			return err
		}
		if !maps.Equal(before, a.Settings.Rights) {
			affected = s.invalidatePersonalGrants(u, a, before, a.Settings.Rights)
		}
		if patch.Paused != nil && *patch.Paused {
			pausePersonalReads(a)
		}
		return nil
	})
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	if patch.Paused != nil && *patch.Paused {
		s.cancelPersonalRuns(u.Name, nil)
	} else if len(affected) > 0 {
		s.cancelPersonalRuns(u.Name, affected)
	}
	s.personalState(w, r)
}
func (s *Server) invalidatePersonalGrants(u auth.User, a *personalAccount, before, after map[string]pa.Level) map[string]bool {
	affected := map[string]bool{}
	changed := func(id string) bool { return before[id] != after[id] }
	if changed(personalReplyVoiceID) || changed("chat.ask") {
		s.stopPersonalReplyRuns(u.Name)
	}
	if changed("mail.watch") {
		clear(a.Watches)
	}

	for i := range a.Jobs {
		j := &a.Jobs[i]
		if j.Status == "done" || j.Status == "failed" || j.Status == "canceled" {
			continue
		}
		p := a.Plans[j.ID]
		if p == nil || !slices.ContainsFunc(p.Steps, changed) {
			continue
		}
		affected[j.ID] = true
		p.ReadOK = false
		p.Approval = ""
		if j.Phase == "executing" {
			p.GrantStamp = ""
			continue
		}
		j.Token = ""
		j.Preview = nil
		j.Updated = time.Now().UTC()
		j.Phase = "queued"
		if slices.ContainsFunc(p.Steps[:p.Index], changed) {
			j.Status = "failed"
			j.Phase = "complete"
			j.Error = "permission_revoked"
			j.Result = nil
			p.Prior = ""
			personalAudit(j, p, "denied")
		} else if j.Status != "paused" {
			j.Status = "planned"
		}
	}
	return affected
}
func pausePersonalReads(a *personalAccount) {
	for i := range a.Jobs {
		j := &a.Jobs[i]
		if j.Status == "running" && j.Phase == "preparing" {
			j.Status = "planned"
			j.Phase = "queued"
			j.Updated = time.Now().UTC()
		}
	}
}
func (s *Server) personalLimits(w http.ResponseWriter, r *http.Request) {
	account, ok := s.opts.Users.Get(r.PathValue("account"))
	if !ok {
		personalRespond(w, nil, errPersonalMissing)
		return
	}
	if account.Role == auth.RoleOwner && currentUser(r).Role != auth.RoleOwner {
		personalRespond(w, nil, errPersonalDenied)
		return
	}
	var body struct {
		Rights map[string]pa.Level `json:"rights"`
	}
	set := r.Method == http.MethodPut
	if set {
		if !readPersonalJSON(w, r, &body) {
			return
		}
		if body.Rights == nil || !s.personalValidRights(body.Rights) {
			personalRespond(w, nil, errPersonalInvalid)
			return
		}
	}
	var affected map[string]bool
	err := s.personalTxn(account, set, func(a *personalAccount) error {
		if set && !maps.Equal(a.Limits, body.Rights) {
			before := a.Limits
			a.Limits = maps.Clone(body.Rights)
			affected = s.invalidatePersonalGrants(account, a, before, a.Limits)
		}
		body.Rights = a.Limits
		return nil
	})
	if err == nil && len(affected) > 0 {
		s.cancelPersonalRuns(account.Name, affected)
	}
	defaults := map[string]pa.Level{}
	for id, t := range s.personal.tools {
		if levelValid(t.Default) {
			defaults[id] = t.Default
		} else {
			defaults[id] = pa.Never
		}
	}
	personalRespond(w, map[string]any{"rights": body.Rights, "defaults": defaults}, err)
}

func (s *Server) personalActivity(w http.ResponseWriter, r *http.Request) {
	type item struct {
		ID      string    `json:"id"`
		Status  string    `json:"status"`
		Title   string    `json:"title"`
		Seen    bool      `json:"seen"`
		Updated time.Time `json:"updated"`
	}
	items := []item{}
	err := s.personalTxn(currentUser(r), false, func(a *personalAccount) error {
		jobs := slices.Clone(a.Jobs)
		slices.SortStableFunc(jobs, func(l, r pa.Job) int { return r.Updated.Compare(l.Updated) })
		for _, j := range jobs[:min(len(jobs), 30)] {
			items = append(items, item{ID: j.ID, Status: j.Status, Title: "Agent", Seen: j.Seen, Updated: j.Updated})
		}
		return nil
	})
	personalRespond(w, map[string]any{"jobs": items}, err)
}

// Private workflow inputs reject unknown properties, including attempts to choose the principal.
func readPersonalJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	var raw json.RawMessage
	if !readJSON(w, r, &raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		personalRespond(w, nil, errPersonalInvalid)
		return false
	}
	return true
}
func (s *Server) personalSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len(q) > 256 || q == "" {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	type hit struct {
		ID     string `json:"id"`
		ChatID string `json:"chatId"`
		Title  string `json:"title"`
	}
	hs := []hit{}
	cs := []pa.Chat{}
	allowedChat := map[string]bool{}
	u := currentUser(r)
	var a *personalAccount
	err := s.personalTxn(u, false, func(v *personalAccount) error { a = v; return nil })
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	for i := len(a.Jobs) - 1; i >= 0; i-- {
		j := a.Jobs[i]
		visible := s.personalRedact(r.Context(), u, a, &j)
		allowedChat[j.Request.ChatID] = visible // reverse traversal ends at the title-producing first job
		if !visible {
			continue
		}
		text := j.Request.Prompt
		title := s.personal.tools[j.Request.Tool].Title
		if j.Result != nil {
			text += " " + j.Result.Text
			title = j.Result.Title
		}
		if len(hs) < 50 && strings.Contains(strings.ToLower(text+" "+title), q) {
			hs = append(hs, hit{j.ID, j.Request.ChatID, title})
		}
	}
	for _, c := range a.Chats {
		if len(cs) < 50 && allowedChat[c.ID] && strings.Contains(strings.ToLower(c.Title), q) {
			cs = append(cs, c)
		}
	}
	personalRespond(w, map[string]any{"jobs": hs, "chats": cs}, nil)
}
