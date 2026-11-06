package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Speech and pictures for the KI-Agent (PLAN 49, decisions of the owner of
// 5.11.2026): speech to text with a Whisper service on the Ollama computer
// (OpenAI-compatible /v1/audio/transcriptions, e.g. Speaches), switched on
// and off in the settings; pictures with the main model when it sees them,
// otherwise with a model of its own. Both are off until set up.

type agentVoice struct {
	Enabled bool   `json:"enabled,omitempty"`
	URL     string `json:"url,omitempty"`
	Model   string `json:"model,omitempty"`
}

type agentVision struct {
	Enabled bool   `json:"enabled,omitempty"`
	Model   string `json:"model,omitempty"`
}

const (
	maxSpeech = 10 << 20
	maxImage  = 20 << 20
)

func checkAgentMedia(req *agentSettings) error {
	v := &req.Voice
	v.URL, v.Model = strings.TrimSpace(v.URL), strings.TrimSpace(v.Model)
	if v.URL != "" {
		u, err := url.Parse(v.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("the Whisper address is like http://192.168.1.5:8000")
		}
		v.URL = strings.TrimRight(u.String(), "/")
	}
	if v.Enabled && v.URL == "" {
		return errors.New("speech input needs the address of a Whisper service")
	}
	if len(v.Model) > 120 || len(req.Vision.Model) > 200 {
		return errors.New("model name too long")
	}
	if req.Vision.Enabled && strings.TrimSpace(req.Vision.Model) == "" {
		return errors.New("choose the model for pictures")
	}
	return nil
}

// agentImageModel is the model that looks at pictures: the chosen picture
// model, else the main model if it sees pictures, else none.
func agentImageModel(settings agentSettings, models []agentModelView) string {
	if settings.Vision.Enabled && settings.Vision.Model != "" {
		return settings.Vision.Model
	}
	for _, m := range models {
		if m.Name == settings.Model && slices.Contains(m.Capabilities, "vision") {
			return m.Name
		}
	}
	return ""
}

// imageModel finds the picture model at run time.
func (b *agentToolbox) imageModel(ctx context.Context, client *agent.Ollama, main string) string {
	if b.settings.Vision.Enabled && b.settings.Vision.Model != "" {
		return b.settings.Vision.Model
	}
	if main == "" {
		main = b.settings.Model
	}
	if info, err := client.Show(ctx, main); err == nil && slices.Contains(info.Capabilities, "vision") {
		return main
	}
	return ""
}

var imageExt = []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".heic"}

// imageTool lets the agent look at a picture: one attached to the chat or
// one on the NAS (with the account's rights and the folder rules).
func (b *agentToolbox) imageTool() *agentTool {
	return &agentTool{name: "image_view", area: agent.AreaAgent, kind: agent.KindAgent, untrusted: true,
		description: "Look at a picture and answer a question about it: one attached to this chat (name) or one on the NAS (root and path). Use it for photos, screenshots of error messages, type plates, receipts.",
		params: schema(map[string]any{
			"name":     str("File name of a picture attached to this chat."),
			"root":     str("Or: root of the file manager, e.g. share/Fotos."),
			"path":     str("With root: path of the picture."),
			"question": str("What to look for, e.g. \"Was steht in der Fehlermeldung?\" or \"Beschreibe das Foto\"."),
		}, "question"),
		own: b.imageView}
}

func (b *agentToolbox) imageView(ctx context.Context, chat *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var req struct{ Name, Root, Path, Question string }
	if err := json.Unmarshal(args, &req); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	client, err := b.s.agentOllama()
	if err != nil {
		return agent.Outcome{}, err
	}
	model := b.imageModel(ctx, client, chatModel(chat))
	if model == "" {
		return agent.Outcome{}, toolcatalog.Errorf("no model that sees pictures is set up; tell the person: Einstellungen › KI-Agent › Bilder (a main model that sees pictures like gemma4, or a model for pictures)")
	}
	var data []byte
	name := req.Name
	switch {
	case req.Name != "":
		if chat == nil {
			return agent.Outcome{}, toolcatalog.Errorf("no chat")
		}
		if data, err = b.s.agentStore().Attachment(b.user.Name, chat.ID, req.Name); err != nil {
			return agent.Outcome{}, toolcatalog.Errorf("no attachment %q in this chat", req.Name)
		}
	case req.Root != "" && req.Path != "":
		if b.settings.Policy.Mode("file_read", toolcatalog.AreaFiles, agent.KindRead) == agent.ModeDeny {
			return agent.Outcome{}, toolcatalog.Errorf("reading files is not allowed for the agent")
		}
		name = req.Path
		if data, err = b.readNASFile(req.Root, req.Path, maxImage); err != nil {
			return agent.Outcome{}, err
		}
	default:
		return agent.Outcome{}, toolcatalog.Errorf("give the name of an attachment or root and path")
	}
	if !slices.Contains(imageExt, strings.ToLower(path.Ext(name))) {
		return agent.Outcome{}, toolcatalog.Errorf("%q is no picture (jpg, png, webp, gif, bmp, heic)", name)
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		question = "Beschreibe das Bild genau, mit allem Text, der darauf steht."
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	text, err := client.Look(ctx, model, question+"\nAntworte auf Deutsch, sachlich. Gib Text im Bild wörtlich wieder.", data, b.settings.KeepAlive, sameModelContext(b.settings, model))
	if err != nil {
		return agent.Outcome{}, err
	}
	return agent.Outcome{Text: fmt.Sprintf("[Bild %s, angesehen mit %s]\n%s", name, model, text)}, nil
}

func chatModel(chat *agent.Chat) string {
	if chat == nil {
		return ""
	}
	return chat.Model
}

// readNASFile reads a file of a root the account sees, at most limit bytes.
func (b *agentToolbox) readNASFile(rootID, p string, limit int64) ([]byte, error) {
	for _, root := range b.s.fileRoots(b.user) {
		if root.ID != rootID {
			continue
		}
		f, info, err := b.s.opts.Files.Open(root, p)
		if err != nil {
			return nil, toolcatalog.Errorf("cannot open %s/%s", rootID, p)
		}
		defer f.Close()
		if info.Size() > limit {
			return nil, toolcatalog.Errorf("the file is larger than %d MB", limit>>20)
		}
		return io.ReadAll(io.LimitReader(f, limit))
	}
	return nil, toolcatalog.Errorf("no root %q for this account", rootID)
}

// handleAgentTranscribe turns a voice message into text with the Whisper
// service. The audio is passed on and not kept.
func (s *Server) handleAgentTranscribe(w http.ResponseWriter, r *http.Request) {
	settings := s.agentSettings()
	if !settings.Voice.Enabled || settings.Voice.URL == "" {
		writeError(w, http.StatusConflict, "voice_off", "speech input is switched off")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxSpeech+1))
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "no_audio", "no audio")
		return
	}
	if len(data) > maxSpeech {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "at most 10 MB")
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang != "de" && lang != "en" {
		lang = ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	text, err := whisperTranscribe(ctx, settings.Voice, data, r.Header.Get("Content-Type"), lang)
	if err != nil {
		writeError(w, http.StatusBadGateway, "voice_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text})
}

// whisperClient never follows redirects and never uses a proxy.
var whisperClient = &http.Client{
	Transport:     &http.Transport{Proxy: nil, ResponseHeaderTimeout: 90 * time.Second},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func whisperTranscribe(ctx context.Context, v agentVoice, audio []byte, contentType, lang string) (string, error) {
	ext := map[string]string{"audio/webm": "webm", "audio/ogg": "ogg", "audio/mp4": "m4a", "audio/aac": "aac", "audio/mpeg": "mp3", "audio/wav": "wav", "audio/x-wav": "wav"}
	base, _, _ := strings.Cut(contentType, ";")
	name := "speech." + ext[strings.TrimSpace(base)]
	if strings.HasSuffix(name, ".") {
		name += "webm"
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	model := v.Model
	if model == "" {
		model = "Systran/faster-whisper-large-v3"
	}
	_ = form.WriteField("model", model)
	_ = form.WriteField("response_format", "json")
	if lang != "" {
		_ = form.WriteField("language", lang)
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.URL+"/v1/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	res, err := whisperClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper not reachable: %v", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whisper answered HTTP %d", res.StatusCode)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", errors.New("whisper sent no text")
	}
	return strings.TrimSpace(out.Text), nil
}

// handleAgentVoiceCheck tests a Whisper address before it is saved: it
// lists the models of the service.
func (s *Server) handleAgentVoiceCheck(w http.ResponseWriter, r *http.Request) {
	var req agentVoice
	if !readJSON(w, r, &req) {
		return
	}
	probe := agentSettings{Voice: agentVoice{URL: req.URL}}
	if err := checkAgentMedia(&probe); err != nil || probe.Voice.URL == "" {
		writeError(w, http.StatusBadRequest, "invalid_address", "the Whisper address is like http://192.168.1.5:8000")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.Voice.URL+"/v1/models", nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_address", err.Error())
		return
	}
	res, err := whisperClient.Do(hreq)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "error": err.Error()})
		return
	}
	defer res.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK || json.Unmarshal(data, &out) != nil {
		writeJSON(w, http.StatusOK, map[string]any{"reachable": false, "error": fmt.Sprintf("HTTP %d", res.StatusCode)})
		return
	}
	models := []string{}
	for _, m := range out.Data {
		models = append(models, m.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"reachable": true, "models": models})
}

// sameModelContext is the context to send with a request to model: the
// configured one when it is the agent's model (one load in Ollama), else
// what Ollama has set for that model.
func sameModelContext(settings agentSettings, model string) int {
	if strings.TrimSuffix(model, ":latest") == strings.TrimSuffix(settings.Model, ":latest") {
		return settings.Context
	}
	return 0
}
