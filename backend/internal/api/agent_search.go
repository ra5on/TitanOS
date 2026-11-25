package api

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

type agentTextHit struct {
	Chat     string `json:"chat"`
	Entry    string `json:"entry"`
	Author   string `json:"author,omitempty"`
	Title    string `json:"title"`
	Archived bool   `json:"archived"`
	Text     string `json:"text"`
}

// Snippets keep the match in view, including when lower-casing a character
// changes its UTF-8 byte length.
func agentSnippet(content, query string) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, query)
	if idx < 0 {
		return ""
	}
	runes := []rune(content)
	match := utf8.RuneCountInString(lower[:idx])
	start := max(0, match-40)
	end := min(len(runes), match+utf8.RuneCountInString(query)+70)
	return strings.Join(strings.Fields(string(runes[start:end])), " ")
}

func (s *Server) handleAgentTextSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	term := strings.ToLower(strings.TrimSpace(q.Get("q")))
	if utf8.RuneCountInString(term) < 2 || utf8.RuneCountInString(term) > 120 {
		writeError(w, 400, "agent_search_term", "enter between 2 and 120 characters")
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	user := currentUser(r).Name
	db := s.agentStore()
	list, err := db.Chats(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	list = append(list, s.agentSharedChats(user)...)
	key := func(c agent.ChatInfo) string { return c.Author + ":" + c.ID }
	slices.SortFunc(list, func(a, b agent.ChatInfo) int { return strings.Compare(key(a), key(b)) })
	results := []agentTextHit{}
	scanned := 0
	next := ""
	nextOffset := 0
	after := q.Get("after")
search:
	for _, info := range list {
		if r.Context().Err() != nil {
			return
		}
		k := key(info)
		if k < after || q.Get("project") != "" && info.Project != q.Get("project") {
			continue
		}
		if scanned >= 16 {
			next = k
			break
		}
		scanned++
		var chat *agent.Chat
		if info.Author == "" {
			chat, err = db.Chat(user, info.ID)
		} else {
			chat, err = s.agentReadableChat(user, info.Author, info.ID)
		}
		if errors.Is(err, agent.ErrNotFound) {
			continue
		}
		if err != nil {
			agentStoreError(w, err)
			return
		}
		if q.Get("project") != "" && chat.Project != q.Get("project") {
			continue
		}
		for i, e := range chat.Entries {
			if k == after && i < offset || (e.Role != "user" && e.Role != "assistant") || e.Content == "" {
				continue
			}
			if len(results) >= 40 {
				next, nextOffset = k, i
				break search
			}
			if snippet := agentSnippet(e.Content, term); snippet != "" {
				results = append(results, agentTextHit{chat.ID, e.ID, info.Author, chat.ShownTitle(), chat.Archived, snippet})
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"hits": results, "after": next, "offset": nextOffset})
}

func (s *Server) handleAgentTemplates(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r).Name
	db := s.agentStore()
	if r.Method != "GET" {
		entry := agent.PromptTemplate{ID: r.PathValue("id")}
		if r.Method == "POST" {
			r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
			if !readJSON(w, r, &entry) {
				return
			}
		}
		if err := db.ChangePromptTemplate(user, entry, r.Method == "DELETE"); err != nil {
			if errors.Is(err, agent.ErrTemplate) {
				writeError(w, 400, "invalid_request", "check template name, length and count")
				return
			}
			agentStoreError(w, err)
			return
		}
	}
	out, err := db.PromptTemplates(user)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"templates": out})
}
