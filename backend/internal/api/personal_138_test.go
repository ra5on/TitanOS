package api

import (
	"context"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

// 1.38 (owner 28.11.2026): a question to the local model runs without an
// approval for a new account; the answer is ready after one step.
func TestPersonalQuestionNeedsNoApproval(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	chat := s.personal.tools["chat.ask"]
	chat.Prepare = func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
		return pa.Preview{Title: "Antwort", Text: "**fett**"}, nil
	}
	s.personal.tools["chat.ask"] = chat
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{RequestID: "q1", Tool: "chat.ask", Prompt: "Wie spät ist es?"})
	workOne(t, s)
	st := personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil)
	for _, got := range st.Jobs {
		if got.ID == j.ID {
			if got.Status != "done" || got.Result == nil || got.Result.Text != "**fett**" {
				t.Fatalf("question waited or failed: %+v", got)
			}
			return
		}
	}
	t.Fatal("job missing")
}

// A failure the account can act on keeps its reason instead of "tool_failed".
func TestPersonalModelFailureKeepsItsReason(t *testing.T) {
	for _, code := range []personalFailure{errModelOffline, errModelMissing, errModelTimeout, errModelEmpty, errModelFailed} {
		t.Run(string(code), func(t *testing.T) {
			ts := personalFixture(t)
			s := ts.server
			chat := s.personal.tools["chat.ask"]
			chat.Prepare = func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
				return pa.Preview{}, code
			}
			s.personal.tools["chat.ask"] = chat
			j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{RequestID: "f-" + string(code), Tool: "chat.ask", Prompt: "Hallo"})
			workOne(t, s)
			st := personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil)
			for _, got := range st.Jobs {
				if got.ID == j.ID && (got.Status != "failed" || got.Error != string(code)) {
					t.Fatalf("want failed/%s, got %s/%s", code, got.Status, got.Error)
				}
			}
		})
	}
}

func TestPersonalThinkPrefersOffThenLow(t *testing.T) {
	if got := personalThink(nil); got != nil {
		t.Fatalf("no thinking info: %v", got)
	}
	if got := personalThink(&agent.Thinking{Values: []any{true, false}}); got != false {
		t.Fatalf("switchable: %v", got)
	}
	if got := personalThink(&agent.Thinking{Values: []any{"low", "medium", "high"}}); got != "low" {
		t.Fatalf("levels: %v", got)
	}
	if got := personalThink(&agent.Thinking{Values: []any{true}}); got != nil {
		t.Fatalf("always on: %v", got)
	}
}

func TestPersonalChatRenameAndDelete(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	chat := s.personal.tools["chat.ask"]
	chat.Prepare = func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
		return pa.Preview{Title: "Antwort", Text: "ok"}, nil
	}
	s.personal.tools["chat.ask"] = chat
	long := "Bitte fasse mir die wichtigsten Punkte der letzten Sicherung zusammen und nenne Fehlerdetails"
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{RequestID: "c1", Tool: "chat.ask", Prompt: long})
	workOne(t, s)
	st := personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil)
	if len(st.Chats) != 1 || st.Chats[0].Title != "Bitte fasse mir die wichtigsten Punkte der letzten Sicherung zusammen und nenne" {
		t.Fatalf("title cut between words: %+v", st.Chats)
	}
	id := j.Request.ChatID
	got := personalHTTP[pa.Chat](t, ts, ts.client, "PUT", "/chats/"+id, map[string]string{"title": "  Sicherung\n prüfen "})
	if got.Title != "Sicherung prüfen" {
		t.Fatalf("rename: %+v", got)
	}
	if r, _ := ts.request("PUT", personalBase+"/chats/"+id, map[string]string{"title": " "}); r.StatusCode != 400 {
		t.Fatalf("empty title: %d", r.StatusCode)
	}
	personalHTTP[map[string]bool](t, ts, ts.client, "DELETE", "/chats/"+id, nil)
	st = personalHTTP[pa.State](t, ts, ts.client, "GET", "/state", nil)
	if len(st.Chats) != 0 || len(st.Jobs) != 0 {
		t.Fatalf("chat and its jobs gone: %+v %+v", st.Chats, st.Jobs)
	}
	if r, _ := ts.request("DELETE", personalBase+"/chats/"+id, nil); r.StatusCode != 404 {
		t.Fatalf("second delete: %d", r.StatusCode)
	}
}
