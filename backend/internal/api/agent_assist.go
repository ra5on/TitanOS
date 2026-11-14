package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Assist (PLAN 44, Runde 4; PLAN 91): Home Assistant talks to the KI-Agent
// through its built-in Ollama integration, so no integration of our own is
// needed. TitanOS answers the few Ollama routes that integration uses under
// /agent-ollama, only when Assist is switched on. Home Assistant shows who
// it is with the key made here, entered there as the API key of the
// integration (Authorization: Bearer). An installation from before PLAN 91
// has no key: until one is made, the address of the Home Assistant set up
// under Settings › Home Assistant counts, as before.
//
// The integration checks certificates, and TitanOS's own one is self
// signed: /agent-ollama is therefore also answered on the plain HTTP port
// (cmd/titand), and over HTTPS under a name with a valid certificate
// (Tailscale, Let's Encrypt).
//
// A question runs in the profile "Home Assistant" (agent.AssistProfile):
// its own chats, memory, instruction and reminders, apart from every
// account; owners and administrators look after it (?profile=assist). Tools
// run with the rights of the first owner, limited by the general rights of
// the agent and, on top, by what Home Assistant may ask (assist rights
// below). An answer that takes long is cut short and the result follows as
// a message.

const (
	assistPrefix = "/agent-ollama"
	assistModel  = "titanos-agent:latest"
	assistWait   = 45 * time.Second
	// assistChatGap: questions closer together continue the same chat.
	assistChatGap = 10 * time.Minute
)

// AssistPath reports whether a request path belongs to Assist; titand
// answers those on the plain HTTP port instead of redirecting to HTTPS.
func AssistPath(path string) bool { return strings.HasPrefix(path, assistPrefix+"/") }

// What Home Assistant may use of an area.
const (
	assistDeny   = "deny"
	assistRead   = "read"
	assistChange = "change"
	// Parts of the agent's own area (agent.AreaAgent), set apart here.
	assistAreaMemory = "memory"
	assistAreaPlan   = "plan"
)

// assistAreas are the areas that can be set, in the order of the page.
var assistAreas = []string{
	toolcatalog.AreaHome, toolcatalog.AreaSystem, toolcatalog.AreaStorage, toolcatalog.AreaBackups, toolcatalog.AreaDocker,
	toolcatalog.AreaVMs, toolcatalog.AreaFiles, toolcatalog.AreaShares, toolcatalog.AreaAccounts, toolcatalog.AreaNetwork,
	toolcatalog.AreaUpdates, toolcatalog.AreaPlugins, agent.AreaWeb, assistAreaMemory, assistAreaPlan, agent.AreaCode,
}

// assistDefault is what an area allows until it is set (owner 14.11.2026:
// "nur Home Assistant und Lesen"): devices in Home Assistant, reading the
// state of the NAS, reminders; no files, no internet, no changes.
func assistDefault(area string) string {
	switch area {
	case toolcatalog.AreaHome, assistAreaPlan:
		return assistChange
	case toolcatalog.AreaSystem, toolcatalog.AreaStorage, toolcatalog.AreaBackups, toolcatalog.AreaDocker, assistAreaMemory:
		return assistRead
	}
	return assistDeny
}

// assistLevel is what a question from Home Assistant may use of an area.
func (h agentHASettings) assistLevel(area string) string {
	if l, ok := h.AssistAreas[area]; ok {
		return l
	}
	return assistDefault(area)
}

// assistMode narrows the mode the general rights give a tool for a run in
// the profile of Home Assistant: never more than those rights allow.
func (h agentHASettings) assistMode(tool, area, kind, mode string) string {
	if area == agent.AreaAgent {
		switch tool {
		case "memory_save", "skill_save":
			area, kind = assistAreaMemory, agent.KindChange
		case "schedule_create":
			area, kind = assistAreaPlan, agent.KindChange
		case "schedule_list":
			area, kind = assistAreaPlan, agent.KindRead
		default:
			return mode // asking back, named locations
		}
	}
	if strings.HasPrefix(tool, "note_") || strings.HasPrefix(tool, "notes_") {
		area = assistAreaMemory
	}
	switch h.assistLevel(area) {
	case assistChange:
		return mode
	case assistRead:
		if kind == agent.KindRead {
			return mode
		}
	}
	return agent.ModeDeny
}

// assistSeen is the last request of Home Assistant, kept in memory only.
type assistSeen struct {
	At   time.Time `json:"at"`
	From string    `json:"from"`
	OK   bool      `json:"ok"`
	// Reason when refused: "off", "key", "address".
	Reason   string `json:"reason,omitempty"`
	Question string `json:"question,omitempty"`
	Millis   int64  `json:"millis,omitempty"`
}

var assistLast struct {
	sync.Mutex
	seen *assistSeen
}

func assistNote(seen assistSeen) {
	assistLast.Lock()
	defer assistLast.Unlock()
	// A refused try does not hide the question answered just before for long
	// lists; the page shows the newest either way.
	assistLast.seen = &seen
}

// assistAllowed reports whether a request may use Assist, and why not.
func (s *Server) assistAllowed(r *http.Request) (bool, string) {
	settings := s.agentSettings()
	if !settings.Enabled || !settings.HA.Assist {
		return false, "off"
	}
	if key := settings.HA.AssistKey; key != "" {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if ok && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(key)) == 1 {
			return true, ""
		}
		return false, "key"
	}
	// Without a key (set up before PLAN 91): the address of a Home
	// Assistant set up as a connection.
	if s.haHub() == nil {
		return false, "address"
	}
	remote := net.ParseIP(remoteIP(r))
	if remote == nil {
		return false, "address"
	}
	for _, conn := range s.haHub().All() {
		if ha, _ := conn.Settings(); s.assistFrom(r, ha.URL, remote) {
			return true, ""
		}
	}
	return false, "address"
}

// assistFrom says whether remote is the Home Assistant at address.
func (s *Server) assistFrom(r *http.Request, address string, remote net.IP) bool {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if a.IP.Equal(remote) {
			return true
		}
	}
	// Home Assistant on this NAS itself (host network, "localhost").
	if remote.IsLoopback() {
		for _, a := range addrs {
			if a.IP.IsLoopback() {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleAgentOllama(w http.ResponseWriter, r *http.Request) {
	if ok, reason := s.assistAllowed(r); !ok {
		if reason != "off" {
			assistNote(assistSeen{At: time.Now(), From: remoteIP(r), Reason: reason})
		}
		// The Ollama integration of Home Assistant reads 401 as "the key is
		// wrong"; a plain refusal would look like a broken server there.
		status := http.StatusForbidden
		if reason == "key" {
			status = http.StatusUnauthorized
		}
		writeError(w, status, "assist_not_allowed", "Assist is off, or the key of TitanOS is missing or wrong")
		return
	}
	switch strings.TrimPrefix(r.URL.Path, assistPrefix) {
	case "/api/version":
		writeJSON(w, http.StatusOK, map[string]string{"version": "0.40.0"})
	case "/api/tags":
		writeJSON(w, http.StatusOK, map[string]any{"models": []map[string]any{{
			"name": assistModel, "model": assistModel, "size": 0, "modified_at": time.Now().UTC().Format(time.RFC3339),
			"details": map[string]any{"family": "titanos", "format": "agent"},
		}}})
	case "/api/show":
		writeJSON(w, http.StatusOK, map[string]any{"capabilities": []string{"completion"}, "details": map[string]any{"family": "titanos"}, "model_info": map[string]any{}})
	case "/api/pull":
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
	case "/api/chat":
		s.assistChat(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "not found")
	}
}

func (s *Server) assistChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream *bool `json:"stream"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	question := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			question = strings.TrimSpace(req.Messages[i].Content)
			break
		}
	}
	started := time.Now()
	answer := s.assistAnswer(r.Context(), question)
	short := question
	if r := []rune(short); len(r) > 120 {
		short = string(r[:119]) + "…"
	}
	assistNote(assistSeen{At: started, From: remoteIP(r), OK: true, Question: short, Millis: time.Since(started).Milliseconds()})
	stream := req.Stream == nil || *req.Stream
	now := time.Now().UTC().Format(time.RFC3339Nano)
	msg := map[string]any{"role": "assistant", "content": answer}
	if !stream {
		writeJSON(w, http.StatusOK, map[string]any{"model": assistModel, "created_at": now, "message": msg, "done": true, "done_reason": "stop"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	enc.Encode(map[string]any{"model": assistModel, "created_at": now, "message": msg, "done": false})
	enc.Encode(map[string]any{"model": assistModel, "created_at": now, "message": map[string]any{"role": "assistant", "content": ""}, "done": true, "done_reason": "stop"})
}

// assistAnswer runs a question and waits for the answer, at most a while.
func (s *Server) assistAnswer(ctx context.Context, question string) string {
	if question == "" || len(question) > maxAgentMessage {
		return "Ich habe keine Frage verstanden."
	}
	user, ok := s.assistProfile()
	if !ok {
		return "Der KI-Agent ist für Home Assistant nicht eingerichtet."
	}
	store := s.agentStore()
	now := time.Now()
	var chat *agent.Chat
	if list, err := store.Chats(user.Name); err == nil {
		for _, info := range list {
			if info.Origin == "homeassistant" && now.Sub(info.Updated) < assistChatGap && !s.agentRunning(user.Name, info.ID) {
				chat, _ = store.Chat(user.Name, info.ID)
				break
			}
		}
	}
	if chat == nil {
		chat = &agent.Chat{ID: agent.NewID(), Title: "Home Assistant (Assist) · " + now.Format("02.01. 15:04"), Created: now, Origin: "homeassistant", State: agent.StateIdle, Entries: []agent.Entry{}}
		if p := s.assistProject(user.Name); p != "" {
			chat.Project = p
		}
	}
	chat.Pending = nil
	chat.Entries = append(chat.Entries, agent.Entry{ID: agent.NewID(), At: now, Role: "user", Content: question})
	chat.Updated = now
	if err := store.SaveChat(user.Name, chat); err != nil {
		return "Der KI-Agent kann gerade nicht speichern."
	}
	done := make(chan struct{})
	if err := s.agentStart(user, chat, func(rn *agent.Runner, runCtx context.Context) error {
		defer close(done)
		return rn.Run(runCtx, chat)
	}); err != nil {
		return fmt.Sprintf("Der KI-Agent ist nicht bereit: %v", err)
	}
	select {
	case <-done:
	case <-time.After(assistWait):
		return "Ich kümmere mich darum und schicke dir das Ergebnis als Mitteilung."
	case <-ctx.Done():
		return ""
	}
	fresh, err := store.Chat(user.Name, chat.ID)
	if err != nil {
		return "Fertig."
	}
	if fresh.State == agent.StateWaiting && fresh.Pending != nil {
		return "Dafür brauche ich deine Freigabe in TitanOS: " + fresh.Pending.Summary
	}
	for i := len(fresh.Entries) - 1; i >= 0; i-- {
		if e := fresh.Entries[i]; e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
			return plainSpeech(e.Content)
		}
	}
	return "Fertig."
}

// assistActor is the account whose rights a run in the profile of Home
// Assistant has: the one chosen before PLAN 91 if it still administers,
// else the first owner.
func (s *Server) assistActor() (auth.User, bool) {
	env := s.agentEnv()
	if env.users == nil {
		return auth.User{}, false
	}
	for _, name := range []string{s.agentSettings().HA.AssistAs, s.firstOwner()} {
		if u, ok := env.users.Get(name); ok && name != "" && !u.Disabled && u.Role.CanAdminister() && s.agentAllowed(u) {
			return u, true
		}
	}
	return auth.User{}, false
}

// assistProfile is the profile of Home Assistant as the "account" a run
// and the store work with: its name is agent.AssistProfile, its role that
// of the acting account. It cannot log in and is no account.
func (s *Server) assistProfile() (auth.User, bool) {
	actor, ok := s.assistActor()
	if !ok {
		return auth.User{}, false
	}
	return auth.User{Name: agent.AssistProfile, Role: actor.Role}, true
}

// agentUserNamed is the account of that name, or the profile of Home
// Assistant for agent.AssistProfile.
func (s *Server) agentUserNamed(name string) (auth.User, bool) {
	if name == agent.AssistProfile {
		return s.assistProfile()
	}
	return s.agentEnv().users.Get(name)
}

// assistProject is the project named "Home Assistant", if the account has one.
func (s *Server) assistProject(user string) string {
	list, err := s.agentStore().Projects(user)
	if err != nil {
		return ""
	}
	for _, p := range list {
		if strings.EqualFold(p.Name, "Home Assistant") {
			return p.ID
		}
	}
	return ""
}

// plainSpeech removes Markdown so a voice reads the text well.
func plainSpeech(s string) string {
	r := strings.NewReplacer("**", "", "__", "", "`", "", "### ", "", "## ", "", "# ", "")
	s = r.Replace(s)
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.Trim(line, "-|: ") == "" {
			continue
		}
		if strings.HasPrefix(line, "|") {
			// A table row is read as a list: "a, b."
			var cells []string
			for _, c := range strings.Split(line, "|") {
				if c = strings.TrimSpace(c); c != "" {
					cells = append(cells, c)
				}
			}
			out = append(out, strings.Join(cells, ", ")+".")
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimLeft(line, "-*")))
	}
	return strings.Join(out, " ")
}

// ---------- the page: key, addresses, last request ----------

// assistInfo is what Settings › KI-Agent › Home Assistant shows of Assist.
func (s *Server) handleAgentAssist(w http.ResponseWriter, r *http.Request) {
	settings := s.agentSettings()
	type address struct {
		Kind string `json:"kind"`
		URL  string `json:"url"`
	}
	addrs := []address{}
	for _, a := range s.agentAddresses(r) {
		host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(a.URL, "https://"), "http://"), "/agent")
		switch {
		case a.Kind == "lan":
			// In the home network without a certificate: plain HTTP.
			addrs = append(addrs, address{Kind: "lan", URL: "http://" + host + assistPrefix})
		case a.HTTPS:
			addrs = append(addrs, address{Kind: a.Kind, URL: "https://" + host + assistPrefix})
		}
	}
	levels := map[string]string{}
	for _, area := range assistAreas {
		levels[area] = settings.HA.assistLevel(area)
	}
	assistLast.Lock()
	last := assistLast.seen
	assistLast.Unlock()
	out := map[string]any{"keySet": settings.HA.AssistKey != "", "addresses": addrs, "areas": assistAreas, "levels": levels, "last": last}
	if store := s.agentStore(); store != nil {
		if mem, err := store.Memories(agent.AssistProfile); err == nil {
			out["memories"] = len(mem)
		}
		if chats, err := store.Chats(agent.AssistProfile); err == nil {
			out["chats"] = len(chats)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAgentAssistKey shows the key (GET) or makes a new one (POST); the
// old one stops working at once.
func (s *Server) handleAgentAssistKey(w http.ResponseWriter, r *http.Request) {
	a := &s.agentConf
	s.agentSettings()
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "agent.json is not readable; nothing was changed")
		return
	}
	if r.Method == http.MethodPost {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "no random numbers")
			return
		}
		next := a.settings
		next.HA.AssistKey = "tos-assist-" + base64.RawURLEncoding.EncodeToString(raw)
		if err := s.agentEnv().store.WriteJSON(agentSettingsFile, next); err != nil {
			writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
			return
		}
		a.settings = next
		slog.Info("a new key for Assist was made", "by", currentUser(r).Name)
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": a.settings.HA.AssistKey})
}
