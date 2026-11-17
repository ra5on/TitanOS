package api

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Chat titles from the model (1.07, owner's decision of 17 Nov 2026): a
// new chat is named by the whole first question; after the first answer
// the model writes a short title in the person's language, e.g. "Apps, VMs
// und Container: Zustand und Fehler". It is one small request of its own,
// to the same model and provider as the chat (with a provider on the
// internet it is a request there, like every other request of the agent).
// A title the person gave is never replaced; when the request fails the
// question stays. Nothing is cut off with "…".

const titlePrompt = `Write a short title for this chat between a person and the assistant of their NAS. At most 6 words, in the language of the person's question, no quotation marks, no emoji, no punctuation at the end. Answer only with the title.`

// agentTitles are the chats whose title is being written (user/chat).
type agentTitles struct {
	mu   sync.Mutex
	busy map[string]bool
}

// agentTitleDue reports whether the model should write the chat's title:
// a chat of the person (not Home Assistant's, not a planned task's), not
// named by the person, not titled yet, with an answer to name it by.
func agentTitleDue(chat *agent.Chat) bool {
	if chat == nil || chat.TitleOwn || chat.TitleMade || chat.Helper || chat.Origin == "homeassistant" || chat.Schedule != "" {
		return false
	}
	_, answer := titleSource(chat)
	return answer != ""
}

// titleSource is the first question and the first answer of a chat.
func titleSource(chat *agent.Chat) (question, answer string) {
	for _, e := range chat.Entries {
		switch {
		case e.Role == "user" && question == "":
			question = strings.TrimSpace(e.Content)
		case e.Role == "assistant" && question != "" && strings.TrimSpace(e.Content) != "":
			return question, strings.TrimSpace(e.Content)
		}
	}
	return question, ""
}

func (s *Server) agentTitling(user, chat string) bool {
	s.agentTitles.mu.Lock()
	defer s.agentTitles.mu.Unlock()
	return s.agentTitles.busy[runKey(user, chat)]
}

// agentRetitle lets the model write the title of a chat in the background.
func (s *Server) agentRetitle(user, id string) {
	if s.agentTitleMark(user, id) {
		s.agentTitleRun(user, id)
	}
}

// agentTitleMark notes that the chat's title is being written (the page
// sees "titling" and asks again); false when it already is.
func (s *Server) agentTitleMark(user, id string) bool {
	key := runKey(user, id)
	s.agentTitles.mu.Lock()
	defer s.agentTitles.mu.Unlock()
	if s.agentTitles.busy[key] {
		return false
	}
	if s.agentTitles.busy == nil {
		s.agentTitles.busy = map[string]bool{}
	}
	s.agentTitles.busy[key] = true
	return true
}

// agentTitleRun writes the title of a marked chat in the background.
func (s *Server) agentTitleRun(user, id string) {
	key := runKey(user, id)
	s.agentRT.work.Add(1)
	go func() {
		defer s.agentRT.work.Done()
		defer func() {
			s.agentTitles.mu.Lock()
			delete(s.agentTitles.busy, key)
			s.agentTitles.mu.Unlock()
		}()
		s.agentWriteTitle(user, id)
	}()
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func (s *Server) agentWriteTitle(user, id string) {
	store := s.agentStore()
	chat, err := store.Chat(user, id)
	if err != nil || !agentTitleDue(chat) {
		return
	}
	question, answer := titleSource(chat)
	settings := s.agentSettings()
	if !settings.Enabled {
		return
	}
	client, err := s.agentOllama()
	if err != nil {
		return
	}
	model := chat.OwnModel()
	if model == "" {
		model = settings.Model
	}
	if model == "" {
		return
	}
	ctx, cancel := context.WithTimeout(s.agentBase(), 90*time.Second)
	defer cancel()
	question = strings.TrimSpace(fileRef.ReplaceAllString(question, ""))
	msg, _, err := client.Chat(ctx, agent.ChatRequest{Model: model, KeepAlive: settings.KeepAlive, Context: settings.Context, Messages: []agent.Message{
		{Role: "system", Content: titlePrompt},
		{Role: "user", Content: "Question: " + clip(question, 800) + "\n\nAnswer: " + clip(answer, 800)},
	}}, nil)
	if err != nil {
		slog.Info("KI-Agent: no title from the model", "chat", id, "err", err)
		return
	}
	title := cleanTitle(msg.Content)
	if title == "" {
		return
	}
	// Written while no run of the chat goes on: holding the lock of the
	// runs, no run can start between reading and saving.
	s.agentRT.mu.Lock()
	defer s.agentRT.mu.Unlock()
	if _, busy := s.agentRT.runs[runKey(user, id)]; busy {
		return
	}
	chat, err = store.Chat(user, id)
	if err != nil || !agentTitleDue(chat) {
		return
	}
	chat.Title, chat.TitleMade = title, true
	if err := store.SaveChat(user, chat); err != nil {
		slog.Warn("KI-Agent: the title could not be saved", "chat", id, "err", err)
	}
}

// cleanTitle makes the model's answer a title, or "" when it is none: one
// line, without quotes, markup and a full stop at the end, at most 10
// words and 80 characters (a longer answer is no title and is not cut).
func cleanTitle(text string) string {
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	if low := strings.ToLower(line); strings.HasPrefix(low, "title:") || strings.HasPrefix(low, "titel:") {
		line = strings.TrimSpace(line[6:])
	}
	line = strings.Trim(line, "\"'„“”‚‘’«»*#`_ ")
	line = strings.TrimRightFunc(line, func(r rune) bool { return strings.ContainsRune(".!?:;,…", r) || unicode.IsSpace(r) })
	line = strings.Join(strings.Fields(line), " ")
	if line == "" || strings.Contains(line, "…") || len([]rune(line)) > 80 || len(strings.Fields(line)) > 10 {
		return ""
	}
	return line
}
