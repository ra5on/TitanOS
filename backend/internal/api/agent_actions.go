package api

// Rights per kind of action, drafts and the activity log of the agent's
// writes to mail, calendar, contacts and reminders (1.13, owner 18 Nov
// 2026, Q27–Q29; agent/actions.go decides). The administrator's values
// stand in agent.json (policy.actionRights, the NAS-wide cap); every
// account may be stricter for itself in its private file actions.json
// (GET/PUT /api/v1/agent/rights/me), never through /agent/prefs, which
// replaces its whole object.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// agentActionsFile is the account's own action rights.
const agentActionsFile = "actions.json"

// agentOwnActions reads the account's own action rights; none (or an
// unreadable file) is no narrowing beyond the NAS.
func (s *Server) agentOwnActions(user string) map[string]string {
	if user == "" || user == agent.AssistProfile {
		return nil
	}
	var own map[string]string
	if err := s.agentStore().ReadPrivate(user, agentActionsFile, &own); err != nil {
		return nil
	}
	return own
}

// actionFor is the kind of action of a call, "" for a tool without one.
func (t *agentTool) actionFor(args json.RawMessage) string {
	if t.actionOf != nil {
		if k := t.actionOf(args); k != "" {
			return k
		}
	}
	return t.action
}

// actionMode is the mode of an action in this chat: the NAS cap, the
// account's own value and the rules of the chat (agent.EffectiveAction).
// Questions from Home Assistant never write mail, events, contacts or
// reminders.
func (b *agentToolbox) actionMode(chat *agent.Chat, name, key string) (string, error) {
	if b.user.Name == agent.AssistProfile {
		return "", fmt.Errorf("%s is not open to questions from Home Assistant", name)
	}
	read := chat != nil && (chat.Tainted || chat.PIMRead || chat.OutsideSkill)
	mode := agent.EffectiveAction(b.settings.Policy, b.own, key, read)
	if mode == agent.ModeDeny {
		return "", fmt.Errorf("the person has not allowed %s for the agent (Einstellungen › KI-Agent › Rechte)", name)
	}
	return mode, nil
}

// mailOpen reports whether mail is open to the agent for this account:
// mail.read on the NAS and for the account.
func (b *agentToolbox) mailOpen() bool {
	return agent.EffectiveAction(b.settings.Policy, b.own, agent.ActMailRead, false) == agent.ModeAllow
}

// logChange writes a change the agent made to mail, the calendar, the
// contacts or the reminders into the activity log ("per KI-Agent"),
// with a short target and never the content (Q27).
func (b *agentToolbox) logChange(t *agentTool, p agent.Prepared, err error) {
	if p.Draft || t.kind == agent.KindRead || t.kind == agent.KindAgent || !agent.ReadsPersonal(t.area) {
		return
	}
	env := b.s.agentEnv()
	if env.activity == nil {
		return
	}
	target := ""
	if t.logged != nil {
		target = t.logged(p.Args)
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusBadRequest
	}
	env.activity.Record(activity.Entry{At: time.Now().UTC(), User: b.session.user.Name, Via: activity.ViaAgent, Action: t.name, Target: target, Status: status, OK: err == nil})
}

// draftText is what the model reads after a draft: nothing happened yet.
func draftOutcome(d *agent.Draft, what string) (agent.Outcome, error) {
	data, err := json.Marshal(map[string]any{
		"draft": true, "saved": false, "what": what,
		"note": "Nothing was written, saved or sent – it is not in the drafts folder either: the person's right for this is \"Nur Vorschläge\". They see a card under your answer in this chat; with \"Öffnen und bearbeiten\" they finish it in the program. Say exactly that in one sentence; do not call the tool again and do not look for it.",
	})
	return agent.Outcome{Text: string(data), Draft: d}, err
}

// ---------- routes ----------

// agentActionsView is what the pages show: the modes that hold for the
// account (without the rules of a chat), the NAS cap and its own values.
func (s *Server) agentActionsView(user string) map[string]any {
	settings := s.agentSettings()
	own := s.agentOwnActions(user)
	nas := map[string]string{}
	for _, k := range agent.ActionKeys {
		nas[k] = settings.Policy.ActionMode(k)
	}
	if own == nil {
		own = map[string]string{}
	}
	return map[string]any{"effective": agent.EffectiveActions(settings.Policy, own), "nas": nas, "own": own, "keys": agent.ActionKeys}
}

func (s *Server) handleAgentRightsMine(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user.Name == agent.AssistProfile {
		writeError(w, http.StatusNotFound, "not_found", "not for the profile of Home Assistant")
		return
	}
	writeJSON(w, http.StatusOK, s.agentActionsView(user.Name))
}

// handleAgentRightsMineSet stores the account's own values. Any valid
// value may be stored; only what is stricter than the NAS counts, so a
// cap the administrator tightens later holds at once.
func (s *Server) handleAgentRightsMineSet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user.Name == agent.AssistProfile {
		writeError(w, http.StatusNotFound, "not_found", "not for the profile of Home Assistant")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var in struct {
		Own map[string]string `json:"own"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := agent.CheckActions(in.Own); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_agent_rights", err.Error())
		return
	}
	store := s.agentStore()
	var err error
	if len(in.Own) == 0 {
		err = store.RemovePrivate(user.Name, agentActionsFile)
	} else {
		err = store.WritePrivate(user.Name, agentActionsFile, in.Own)
	}
	if err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.agentActionsView(user.Name))
}

// handleAgentDraft hands a draft of the account's own chat to a program
// (?agentDraft=<chat>:<entry>): a step made in the mode "Nur Vorschläge",
// or the step that waits for approval, as the editor would show it.
// Nothing is written here.
func (s *Server) handleAgentDraft(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, entry := r.PathValue("id"), r.PathValue("entry")
	if !agent.ValidID(id) || entry == "" || len(entry) > 64 {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	chat, err := s.agentStore().Chat(user.Name, id)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if at := slices.IndexFunc(chat.Entries, func(e agent.Entry) bool { return e.ID == entry && e.Draft != nil }); at >= 0 {
		writeJSON(w, http.StatusOK, chat.Entries[at].Draft)
		return
	}
	if p := chat.Pending; p != nil && p.ID == entry {
		d, err := s.agentPendingDraft(r.Context(), user, chat)
		if err != nil {
			writeError(w, http.StatusConflict, "no_draft", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, d)
		return
	}
	writeError(w, http.StatusNotFound, "no_draft", "no such draft in this chat")
}

// agentPendingDraft is the draft of the step a chat waits with, made by
// the tool's draft (nothing is written).
func (s *Server) agentPendingDraft(ctx context.Context, user auth.User, chat *agent.Chat) (*agent.Draft, error) {
	p := chat.Pending
	if p == nil {
		return nil, errors.New("no step waits")
	}
	box := s.newAgentToolbox(user, s.agentSettings())
	t, ok := box.tools[p.Tool]
	if !ok || t.draft == nil {
		return nil, errors.New("this step has no draft")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := t.draft(ctx, chat, p.Args)
	if err == nil && out.Draft == nil {
		err = errors.New("no draft")
	}
	return out.Draft, err
}
