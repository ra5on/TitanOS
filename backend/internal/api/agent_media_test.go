package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 49: speech to text through the Whisper service (only when switched
// on) and pictures through the model that sees them.
func TestAgentVoiceAndPictures(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	var gotModel, gotLang string
	var gotAudio []byte
	whisper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"Systran/faster-whisper-large-v3"}]}`))
		case "/v1/audio/transcriptions":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			gotModel, gotLang = r.FormValue("model"), r.FormValue("language")
			f, _, _ := r.FormFile("file")
			gotAudio, _ = io.ReadAll(f)
			w.Write([]byte(`{"text":" Erinnere mich morgen um neun. "}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer whisper.Close()

	speak := func() (*http.Response, []byte) {
		req, _ := http.NewRequest("POST", ts.url+"/api/v1/agent/transcribe?lang=de", strings.NewReader("OggS-audio"))
		req.Header.Set("Origin", ts.url)
		req.Header.Set("Content-Type", "audio/ogg")
		res, err := ts.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res, body
	}
	if res, _ := speak(); res.StatusCode != http.StatusConflict {
		t.Fatalf("voice off: %d", res.StatusCode)
	}
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["voice"] = map[string]any{"enabled": true, "url": "ftp://x"}
	if res, _ := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad whisper address: %d", res.StatusCode)
	}
	settings["voice"] = map[string]any{"enabled": true, "url": whisper.URL + "/", "model": "large-v3-turbo"}
	settings["vision"] = map[string]any{"enabled": true, "model": "gemma4:12b"}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/agent/voice/check", map[string]any{"url": whisper.URL}); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"reachable":true`) {
		t.Fatalf("check: %d %s", res.StatusCode, body)
	}
	res, body := speak()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"text":"Erinnere mich morgen um neun."`) {
		t.Fatalf("transcribe: %d %s", res.StatusCode, body)
	}
	if gotModel != "large-v3-turbo" || gotLang != "de" || string(gotAudio) != "OggS-audio" {
		t.Fatalf("sent to whisper: %q %q %q", gotModel, gotLang, gotAudio)
	}
	_, body = ts.request("GET", "/api/v1/agent/status", nil)
	if !strings.Contains(string(body), `"voice":true`) || !strings.Contains(string(body), `"vision":"gemma4:12b"`) {
		t.Fatalf("status: %s", body)
	}

	// A photo in the chat goes to the picture model.
	res, body = ts.request("POST", "/api/v1/agent/chats", map[string]any{"draft": true})
	var draft agent.Chat
	json.Unmarshal(body, &draft)
	req, _ := http.NewRequest("POST", ts.url+"/api/v1/agent/chats/"+draft.ID+"/attachments?name="+url.QueryEscape("typenschild.jpg"), strings.NewReader("\xff\xd8\xff-jpeg"))
	req.Header.Set("Origin", ts.url)
	if up, err := ts.client.Do(req); err != nil || up.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %v %v", err, up)
	}
	fake.replies = []string{
		toolCall("image_view", map[string]any{"name": "typenschild.jpg", "question": "Welches Modell?"}),
		`{"role":"assistant","content":"Typenschild: Modell X200, Baujahr 2019."}`,
		`{"role":"assistant","content":"Es ist ein X200."}`,
	}
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+draft.ID+"/messages", map[string]any{"message": "[Anhang: typenschild.jpg]\n\nWas ist das für ein Gerät?"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("message: %d %s", res.StatusCode, body)
	}
	chat := waitChat(t, ts, ts.client, draft.ID)
	seen := ""
	for _, e := range chat.Entries {
		if e.Tool == "image_view" {
			seen = e.Result
		}
	}
	if !strings.Contains(seen, "X200") || !chat.Tainted {
		t.Fatalf("picture: %q tainted=%v", seen, chat.Tainted)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	look := false
	for _, g := range fake.got {
		if g["model"] == "gemma4:12b" && strings.Contains(fmtJSON(g["messages"]), `"images":[`) {
			look = true
		}
	}
	if !look {
		t.Fatal("the picture did not reach the picture model")
	}
}

func fmtJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
