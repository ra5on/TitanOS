package api

import (
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

// 1.38 (owner 28.11.2026, Durchsicht §124): a chat can be renamed and deleted.
// Deleting takes its jobs with it; a job that is just writing to the outside
// (phase "executing") keeps the chat until it has finished. The record of an
// irreversible effect (`Effects`) stays, so a deleted job is never repeated.

const personalChatTitleMax = 120

// personalChatTitle cleans a name the account typed: one line, no control
// characters, at most personalChatTitleMax characters; "" when nothing is left.
func personalChatTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > personalChatTitleMax {
		return ""
	}
	return s
}

// personalTitleFrom names a new chat after its first question: the words that
// fit into 80 characters, cut between two words, never inside one.
func personalTitleFrom(prompt string) string {
	words := strings.Fields(prompt)
	title := ""
	for _, w := range words {
		next := strings.TrimSpace(title + " " + w)
		if utf8.RuneCountInString(next) > 80 {
			break
		}
		title = next
	}
	if title == "" && len(words) > 0 {
		title = personalLimitText(words[0], 80)
	}
	return title
}

func (s *Server) personalRenameChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if !readPersonalJSON(w, r, &in) {
		return
	}
	title := personalChatTitle(in.Title)
	if title == "" {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	id := r.PathValue("id")
	var chat pa.Chat
	err := s.personalTxn(currentUser(r), true, func(a *personalAccount) error {
		at := slices.IndexFunc(a.Chats, func(c pa.Chat) bool { return c.ID == id })
		if at < 0 {
			return errPersonalMissing
		}
		a.Chats[at].Title = title
		chat = a.Chats[at]
		return nil
	})
	personalRespond(w, chat, err)
}

func (s *Server) personalDeleteChat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.personalTxn(currentUser(r), true, func(a *personalAccount) error {
		at := slices.IndexFunc(a.Chats, func(c pa.Chat) bool { return c.ID == id })
		if at < 0 {
			return errPersonalMissing
		}
		for _, j := range a.Jobs {
			if j.Request.ChatID == id && j.Phase == "executing" {
				return errPersonalConflict
			}
		}
		a.Jobs = slices.DeleteFunc(a.Jobs, func(j pa.Job) bool {
			if j.Request.ChatID != id {
				return false
			}
			delete(a.Plans, j.ID)
			return true
		})
		a.Chats = slices.Delete(a.Chats, at, at+1)
		return nil
	})
	personalRespond(w, map[string]bool{"deleted": err == nil}, err)
}
