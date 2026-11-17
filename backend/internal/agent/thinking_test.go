package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The streams in testdata/thinking are written after the formats the
// servers document (Ollama /api/chat, OpenAI-compatible SSE, the Harmony
// format of gpt-oss); unmarked-export-2026-11-17.txt is the text of the
// answer in the owner's export of 17 Nov 2026 (characters the export lost
// put back as spaces).

// serve answers /api/chat and /v1/chat/completions with a file of
// testdata, as the server would stream it; ollama: the file is NDJSON.
func serve(t *testing.T, file string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile("testdata/thinking/" + file)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat":
			w.Header().Set("Content-Type", "application/x-ndjson")
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
		default:
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stream is an Ollama stream that sends text in pieces of n bytes, as
// tokens come.
func stream(text string, n int) string {
	var b strings.Builder
	for len(text) > 0 {
		cut := min(n, len(text))
		for cut < len(text) && text[cut]&0xC0 == 0x80 {
			cut++
		}
		fmt.Fprintf(&b, `{"model":"gpt-oss:20b","message":{"role":"assistant","content":%q},"done":false}`+"\n", text[:cut])
		text = text[cut:]
	}
	b.WriteString(`{"model":"gpt-oss:20b","message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":10,"eval_count":10}` + "\n")
	return b.String()
}

type seen struct{ thinking, content strings.Builder }

func (s *seen) chunk(c Chunk) {
	s.thinking.WriteString(c.Thinking)
	s.content.WriteString(c.Content)
}

func chatWith(t *testing.T, c *Ollama) (Message, *seen) {
	t.Helper()
	var got seen
	msg, _, err := c.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "wieviele automationen habe ich"}}}, got.chunk)
	if err != nil {
		t.Fatal(err)
	}
	return msg, &got
}

func TestThinkingNeverInTheAnswer(t *testing.T) {
	const answer = "Du hast 2 Automationen."
	check := func(name string, msg Message, got *seen, thought string) {
		t.Helper()
		if msg.Content != answer || !strings.Contains(msg.Thinking, thought) {
			t.Errorf("%s: content %q thinking %q", name, msg.Content, msg.Thinking)
		}
		if live := got.content.String(); strings.TrimSpace(live) != answer {
			t.Errorf("%s: streamed as text %q", name, live)
		}
		if !strings.Contains(got.thinking.String(), thought) {
			t.Errorf("%s: streamed as thinking %q", name, got.thinking.String())
		}
	}
	// Ollama with its field thinking (gpt-oss).
	c, _ := NewOllama(serve(t, "ollama-gpt-oss-thinking.ndjson").URL)
	msg, got := chatWith(t, c)
	check("ollama thinking", msg, got, "automations exist")
	// Ollama without think for a model that thinks in tags (qwen3), the
	// tags cut between pieces.
	c, _ = NewOllama(serve(t, "ollama-think-tags.ndjson").URL)
	msg, got = chatWith(t, c)
	check("think tags", msg, got, "Okay, the user wants")
	// An OpenAI-compatible server that sends Harmony as text.
	o, _ := NewOpenAI(serve(t, "openai-harmony-text.sse").URL+"/v1", "")
	msg, got = chatWith(t, o)
	check("harmony text", msg, got, "ha_entities said total 2")
	// reasoning_content.
	o, _ = NewOpenAI(serve(t, "openai-reasoning-content.sse").URL+"/v1", "")
	msg, got = chatWith(t, o)
	check("reasoning_content", msg, got, "how many automations")
}

// The answer of the export: reasoning as text with the answer glued to it.
func TestUnmarkedReasoningOfTheExport(t *testing.T) {
	data, err := os.ReadFile("testdata/thinking/unmarked-export-2026-11-17.txt")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(data))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, stream(text, 7))
	}))
	defer srv.Close()
	c, _ := NewOllama(srv.URL)
	msg, got := chatWith(t, c)
	if !strings.HasPrefix(msg.Content, "In Home Assistant gibt es aktuell keine Automations") || strings.Contains(msg.Content, "The user") || !strings.HasSuffix(msg.Content, "weiterhelfen kann.") {
		t.Fatalf("answer: %q", msg.Content)
	}
	if !strings.HasPrefix(msg.Thinking, "The user says") || !strings.HasSuffix(msg.Thinking, `lass mich wissen."`) {
		t.Fatalf("thinking: %q", msg.Thinking)
	}
	// Live, the reasoning went to the thinking line, never into the text;
	// the answer comes with the saved message.
	if live := got.content.String(); live != "" {
		t.Fatalf("streamed as text: %q", live)
	}
	if !strings.HasPrefix(got.thinking.String(), "The user says") {
		t.Fatalf("streamed as thinking: %q", got.thinking.String()[:80])
	}
}

// Real answers stay whole, also English ones and ones that look a little
// like reasoning.
func TestRealAnswersAreNotCut(t *testing.T) {
	for _, text := range []string{
		"Du hast 2 Automationen: **Licht aus** und **Morgens**.",
		"Let me explain: the volume is 71 % full. Most of it is in the share Medien.",
		"Let's look at it. The container `plex` uses fmt.Println style logs; in `main.Go` nothing is wrong. The disk is fine.",
		"We need to replace the disk sda soon. SMART shows 12 reallocated sectors.\n\n| Platte | Zustand |\n|---|---|\n| sda | Warnung |",
		"The user account bob has no access to the share Fotos. Give it under Konten › bob.",
		"analysis of the logs: nothing found.",
		"<think> is a tag some models write; this answer only names it.",
		"Okay, the backup ran at 03:00 and took 12 minutes.",
		"Die Sicherung lief um 3 Uhr.Das war gut so, und es ist nichts zu tun.",
	} {
		thinking, answer, kind := SplitThinking(text)
		if answer != text || thinking != "" || kind != "" {
			t.Errorf("cut %q into %q / %q (%s)", text, thinking, answer, kind)
		}
	}
	// Harmony without its tokens, as some servers give it.
	thinking, answer, kind := SplitThinking("analysisThe user asks for the number.assistantfinalDu hast 2 Automationen.")
	if answer != "Du hast 2 Automationen." || thinking != "The user asks for the number." || kind != "harmony" {
		t.Errorf("harmony without tokens: %q / %q", thinking, answer)
	}
	// Only the closing tag (the template opened it).
	if thinking, answer, _ := SplitThinking("Okay, count them.\n</think>\n\nDu hast 2."); thinking != "Okay, count them." || answer != "Du hast 2." {
		t.Errorf("closing tag only: %q / %q", thinking, answer)
	}
	// A tool call written as text is dropped, never run from there.
	if thinking, answer, _ := SplitThinking(`<|channel|>commentary to=functions.ha_call <|constrain|>json<|message|>{"domain":"lock"}<|call|>`); answer != "" || strings.Contains(thinking, "lock") {
		t.Errorf("tool call as text: %q / %q", thinking, answer)
	}
}

// Answers saved before 1.09 with the reasoning in them are clean when read.
func TestStoredAnswerWithReasoningIsCleanWhenRead(t *testing.T) {
	data, _ := os.ReadFile("testdata/thinking/unmarked-export-2026-11-17.txt")
	dir := t.TempDir()
	s := &Store{Open: func() (*os.Root, error) { return os.OpenRoot(dir) }}
	chat := &Chat{ID: NewID(), Title: "Automationen", Entries: []Entry{
		{ID: "u", Role: "user", Content: "automationen ist was anderes wie entitäten"},
		{ID: "a", Role: "assistant", Content: string(data)},
	}}
	if err := s.write("alex", "chats/"+chat.ID+".json", chat); err != nil {
		t.Fatal(err)
	}
	read, err := s.Chat("alex", chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := read.Entries[1]
	if !strings.HasPrefix(a.Content, "In Home Assistant") || !strings.HasPrefix(a.Thinking, "The user says") {
		t.Fatalf("read back: %q / %q", a.Content[:min(80, len(a.Content))], a.Thinking[:min(80, len(a.Thinking))])
	}
	// What the model gets as history is the answer only.
	for _, m := range buildMessages("system", read, MaxResultText, nil) {
		if m.Role == "assistant" && strings.Contains(m.Content, "The user says") {
			t.Fatal("the reasoning went back to the model as an answer")
		}
	}
}
