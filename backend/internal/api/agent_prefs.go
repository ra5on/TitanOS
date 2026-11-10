package api

import (
	"errors"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// PLAN 53: the agent app's settings of each account (look, gestures,
// motion, start, speech …) live on the NAS in the account's private agent
// folder, so they follow the person to every device. The page knows the
// fields; titand only keeps the object small and flat.

const maxAgentPrefs = 8 << 10

var agentPrefKey = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]{0,31}$`)

// cleanAgentPrefs keeps scalar values and one level of objects of scalars.
func cleanAgentPrefs(in map[string]any, depth int) (map[string]any, bool) {
	if len(in) > 64 {
		return nil, false
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if !agentPrefKey.MatchString(k) {
			return nil, false
		}
		switch x := v.(type) {
		case bool, float64, nil:
			out[k] = x
		case string:
			if len(x) > 200 {
				return nil, false
			}
			out[k] = x
		case map[string]any:
			if depth > 0 {
				return nil, false
			}
			sub, ok := cleanAgentPrefs(x, depth+1)
			if !ok {
				return nil, false
			}
			out[k] = sub
		default:
			return nil, false
		}
	}
	return out, true
}

func (s *Server) handleAgentPrefs(w http.ResponseWriter, r *http.Request) {
	prefs := map[string]any{}
	if err := s.agentStore().ReadPrivate(currentUser(r).Name, "prefs.json", &prefs); err != nil && !errors.Is(err, agent.ErrNotFound) {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handleAgentPrefsSet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAgentPrefs)
	var in map[string]any
	if !readJSON(w, r, &in) {
		return
	}
	prefs, ok := cleanAgentPrefs(in, 0)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_prefs", "flat settings with short values only")
		return
	}
	if err := s.agentStore().WritePrivate(currentUser(r).Name, "prefs.json", prefs); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// agentSuggestion is a hint for the start page: something that needs
// attention right now. The page makes the text in its own language.
type agentSuggestion struct {
	Kind     string    `json:"kind"`
	Subject  string    `json:"subject,omitempty"`
	Title    string    `json:"title"`
	Severity string    `json:"severity"`
	Time     time.Time `json:"time"`
}

// handleAgentSuggestions lists warnings of the last two days that the
// agent could look into. Only owners and administrators see NAS-wide
// messages, like on the notification page.
func (s *Server) handleAgentSuggestions(w http.ResponseWriter, r *http.Request) {
	env := s.agentEnv()
	list := []agentSuggestion{}
	if (currentUser(r).Role == auth.RoleOwner || currentUser(r).Role == auth.RoleAdmin) && env.notify != nil {
		since := time.Now().Add(-48 * time.Hour)
		seen := map[string]bool{}
		msgs := env.notify.Messages()
		slices.SortFunc(msgs, func(a, b notify.Message) int { return b.Time.Compare(a.Time) })
		for _, m := range msgs {
			if m.Time.Before(since) || m.Severity == "info" && m.Kind != "update_available" {
				continue
			}
			if seen[m.Kind+"\x00"+m.Subject] {
				continue
			}
			seen[m.Kind+"\x00"+m.Subject] = true
			list = append(list, agentSuggestion{Kind: m.Kind, Subject: m.Subject, Title: m.Title, Severity: m.Severity, Time: m.Time})
			if len(list) == 3 {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": list})
}
