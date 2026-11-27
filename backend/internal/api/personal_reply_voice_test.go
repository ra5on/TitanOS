package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/speech"
)

type replyVoiceResponse struct {
	code             int
	audioType, cache string
	body             []byte
	err              error
}

func playReply(ctx context.Context, client *http.Client, url, body string, confirm bool) replyVoiceResponse {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return replyVoiceResponse{err: err}
	}
	if confirm {
		req.Header.Set("X-TitanOS-Play-Reply", "yes")
	}
	res, err := client.Do(req)
	if err != nil {
		return replyVoiceResponse{err: err}
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return replyVoiceResponse{res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Cache-Control"), b, err}
}

func TestPersonalReplyVoiceIsBoundToOneFinishedLocalAnswer(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	u, _ := ts.users.Get("alex")
	const answer = "Eine ausschließlich lokal gespeicherte Antwort."
	ctx := t.Context()
	chat := s.personal.tools["chat.ask"]
	chat.Prepare = func(context.Context, auth.User, pa.Request) (pa.Preview, error) {
		return pa.Preview{Title: "Agent", Text: answer}, nil
	}
	s.personal.tools["chat.ask"] = chat
	rights := func(path string, speechLevel, chatLevel pa.Level) {
		t.Helper()
		personalHTTP[map[string]any](t, ts, ts.client, "PUT", path, map[string]any{"rights": map[string]pa.Level{personalReplyVoiceID: speechLevel, "chat.ask": chatLevel}})
	}
	rights("/limits/alex", pa.Auto, pa.Auto)
	rights("/settings", pa.Ask, pa.Auto)
	j := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{RequestID: "voice-original", Tool: "chat.ask", Prompt: "Nur der Auftrag, nicht die vorzulesende Antwort."})
	path := personalBase + "/jobs/" + j.ID + "/speech"
	r := playReply(ctx, ts.client, ts.url+path, "", true)
	if r.code != 409 {
		t.Fatalf("unfinished: %+v", r)
	}
	workOne(t, s)
	r = playReply(ctx, ts.client, ts.url+path, "", true)
	if r.code != 503 || !bytes.Contains(r.body, []byte("speech_not_running")) {
		t.Fatalf("native route without Piper: %+v", r)
	}
	voice := s.personal.tools[personalReplyVoiceID]
	voice.Available = func(auth.User) error { return nil }
	s.personal.tools[personalReplyVoiceID] = voice

	started := make(chan string, 1)
	stopped := make(chan struct{}, 1)
	var block atomic.Bool
	var changedAfterSynthesis atomic.Bool
	var calls atomic.Int32
	audio := speech.Audio{Rate: 16000, Width: 2, Channels: 1, PCM: []byte{0, 0, 1, 0}}
	fn := func(ctx context.Context, text string) (speech.Audio, error) {
		calls.Add(1)
		if text != answer {
			t.Errorf("synthesis used other text: %q", text)
		}
		if block.Load() {
			started <- text
			<-ctx.Done()
			stopped <- struct{}{}
			// Simulate a native provider that returns a late success after cancellation.
		}
		if changedAfterSynthesis.Swap(false) {
			err := s.personalTxn(u, true, func(a *personalAccount) error {
				job, err := a.find(j.ID)
				if err == nil {
					job.Result.Text = "Inzwischen ersetzte Antwort"
				}
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}
		return audio, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/personal-agent/jobs/{id}/speech", s.personalBrowser(s.personalReplyVoice(fn)))
	h := httptest.NewServer(mux)
	defer h.Close()
	url := h.URL + path
	check := func(got replyVoiceResponse, code int) {
		t.Helper()
		if got.err != nil || got.code != code || (code != 200 && strings.Contains(got.audioType, "wav")) {
			t.Fatalf("want %d: %+v", code, got)
		}
	}
	check(playReply(ctx, ts.client, url, "", false), 403)
	check(playReply(ctx, ts.client, url, `{"text":"speak something else"}`, true), 400)
	check(playReply(ctx, ts.client, url+"?voice=remote", "", true), 400)
	if res, _ := ts.request("POST", personalBase+"/jobs", pa.Request{Tool: personalReplyVoiceID, RequestID: "voice-scheduled"}); res.StatusCode != 403 {
		t.Fatalf("queued synthesis: %d", res.StatusCode)
	}
	guest, err := ts.users.Create("bea", testPassword, auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	client := ts.newClient()
	if res, _ := ts.do(client, "POST", "/api/v1/login", credentials{Name: guest.Name, Password: testPassword}); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	rights("/limits/bea", pa.Ask, pa.Ask)
	check(playReply(ctx, client, url, "", true), 404)
	other := personalHTTP[pa.Job](t, ts, ts.client, "POST", "/jobs", pa.Request{Tool: "agent.example", RequestID: "not-a-chat"})
	workOne(t, s)
	check(playReply(ctx, ts.client, h.URL+personalBase+"/jobs/"+other.ID+"/speech", "", true), 409)
	if calls.Load() != 0 {
		t.Fatal("native provider received an unauthorized request")
	}
	r = playReply(ctx, ts.client, url, "", true)
	check(r, 200)
	if r.audioType != "audio/wav" || r.cache != "no-store" || !bytes.Equal(r.body, audio.WAV()) {
		t.Fatalf("unexpected audio metadata or PCM: %+v", r)
	}

	rights("/settings", pa.Auto, pa.Auto)
	check(playReply(ctx, ts.client, url, "", false), 200)
	rights("/settings", pa.Auto, pa.Never)
	check(playReply(ctx, ts.client, url, "", false), 403)
	rights("/settings", pa.Auto, pa.Auto)
	changedAfterSynthesis.Store(true)
	check(playReply(ctx, ts.client, url, "", true), 403)
	if err := s.personalTxn(u, true, func(a *personalAccount) error {
		job, err := a.find(j.ID)
		if err == nil {
			job.Result.Text = answer
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	for _, reason := range []string{"personal-revoke-and-restore", "admin-revoke", "resource-epoch", "client-stop", "session-ended"} {
		requestCtx, cancel := context.WithCancel(ctx)
		done := make(chan replyVoiceResponse, 1)
		go func() { done <- playReply(requestCtx, ts.client, url, "", true) }()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal(reason, "did not start")
		}
		switch reason {
		case "personal-revoke-and-restore":
			rights("/settings", pa.Never, pa.Auto)
			rights("/settings", pa.Auto, pa.Auto)
		case "admin-revoke":
			rights("/limits/alex", pa.Auto, pa.Never)
		case "resource-epoch":
			s.personalEntitlementsChanged(u.Name)
		case "client-stop":
			cancel()
		case "session-ended":
			if res, _ := ts.request("POST", "/api/v1/logout", nil); res.StatusCode != 204 {
				t.Fatal(res.StatusCode)
			}
		}
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Fatal(reason, "did not stop local synth")
		}
		select {
		case got := <-done:
			if reason == "client-stop" {
				if got.err == nil || !strings.Contains(got.err.Error(), "canceled") {
					t.Fatal("browser cancellation", got)
				}
			} else {
				check(got, 403)
			}
		case <-time.After(3 * time.Second):
			t.Fatal(reason, "handler did not finish")
		}
		cancel()
		if reason == "admin-revoke" {
			rights("/limits/alex", pa.Auto, pa.Auto)
		}
	}
}
