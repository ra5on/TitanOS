package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/speech"
)

// Speech (PLAN 92, package speech, docs/SPEECH.md): speech to text and
// text to speech on the NAS for Home Assistant and the KI-Agent. The page
// Settings › Sprache belongs to owners and administrators; the KI-Agent
// uses the services for every account that may use the agent.

func (s *Server) registerSpeech(mux *routeMux) {
	if s.opts.Speech == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/speech", s.requireAdmin(s.handleSpeech))
	mux.HandleFunc("PUT /api/v1/speech/settings", s.requireAdmin(s.handleSpeechSettings))
	mux.HandleFunc("POST /api/v1/speech/download", s.requireAdmin(s.handleSpeechDownload))
	mux.HandleFunc("DELETE /api/v1/speech/download", s.requireAdmin(s.handleSpeechCancel))
	mux.HandleFunc("DELETE /api/v1/speech/{kind}/{id}", s.requireAdmin(s.handleSpeechRemove))
	mux.HandleFunc("POST /api/v1/speech/say", s.requireAdmin(s.handleSpeechSay))
	mux.HandleFunc("POST /api/v1/speech/hear", s.requireAdmin(s.handleSpeechHear))
	// The KI-Agent: reading aloud and the microphone (agent.go, requireAgent).
	mux.HandleFunc("GET /api/v1/agent/speech", s.requireAgent(s.handleAgentSpeech))
	mux.HandleFunc("POST /api/v1/agent/speech/say", s.requireAgent(s.handleAgentSpeechSay))
	mux.HandleFunc("POST /api/v1/agent/speech/hear", s.requireAgent(s.handleAgentSpeechHear))
}

// maxSpeechText is what is spoken in one go; maxSpeechAudio what is heard.
const (
	maxSpeechText  = 4000
	maxSpeechAudio = 12 << 20
)

type speechDevice struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Available bool   `json:"available"`
	// Reason when it is not: "driver" or "vm".
	Reason           string   `json:"reason,omitempty"`
	MemoryUsedBytes  *float64 `json:"memoryUsedBytes,omitempty"`
	MemoryTotalBytes *float64 `json:"memoryTotalBytes,omitempty"`
}

type speechModelView struct {
	speech.WhisperModel
	Installed bool `json:"installed"`
}

type speechVoiceView struct {
	speech.Voice
	Installed bool `json:"installed"`
}

func (s *Server) speechDevices(ctx context.Context) []speechDevice {
	out := []speechDevice{}
	m := s.opts.Speech
	if m.Devices == nil {
		return out
	}
	env := s.agentEnv()
	for _, d := range m.Devices(ctx) {
		if d.Kind != "nvidia" {
			continue
		}
		dev := speechDevice{ID: d.ID, Title: d.Title, Available: d.Available, Reason: d.Reason}
		if !d.Available && dev.Reason == "" {
			dev.Reason = "driver"
		}
		if env.accelerators != nil {
			for _, a := range env.accelerators.Snapshot() {
				if a.Address == d.ID {
					dev.MemoryUsedBytes, dev.MemoryTotalBytes = a.MemoryUsedBytes, a.MemoryTotalBytes
				}
			}
		}
		out = append(out, dev)
	}
	return out
}

func (s *Server) handleSpeech(w http.ResponseWriter, r *http.Request) {
	m := s.opts.Speech
	settings := m.Settings()
	models, voices := m.Installed()
	out := map[string]any{"settings": settings, "status": m.Status(), "jobs": m.Jobs(), "devices": s.speechDevices(r.Context())}
	list := []speechModelView{}
	for _, model := range speech.WhisperModels {
		list = append(list, speechModelView{WhisperModel: model, Installed: slices.Contains(models, model.ID)})
	}
	out["models"] = list
	// The catalog of voices needs a volume to keep its copy and, the first
	// time, the internet; without both the page still shows what is there.
	voiceList := []speechVoiceView{}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	catalog, err := m.Voices(ctx)
	cancel()
	if err != nil {
		out["voicesError"] = err.Error()
		for _, id := range voices {
			voiceList = append(voiceList, speechVoiceView{Voice: speech.Voice{ID: id, Name: id}, Installed: true})
		}
	}
	for _, v := range catalog {
		voiceList = append(voiceList, speechVoiceView{Voice: v, Installed: slices.Contains(voices, v.ID)})
	}
	out["voices"] = voiceList
	volumes := []string{}
	if m.Mounted != nil {
		volumes = m.Mounted()
	}
	out["volumes"] = volumes
	// Where Home Assistant finds the services: the NAS in the home
	// network, and its name in the tailnet when there is one.
	hosts := []map[string]string{}
	if ip := appproxy.PrimaryIPv4(); ip != "" {
		hosts = append(hosts, map[string]string{"kind": "lan", "host": ip})
	}
	if ts := s.agentEnv().tailscale; ts != nil {
		if name := ts.Status().Name; name != "" {
			hosts = append(hosts, map[string]string{"kind": "tailscale", "host": strings.TrimSuffix(name, ".")})
		}
	}
	out["hosts"] = hosts
	writeJSON(w, http.StatusOK, out)
}

func speechError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, speech.ErrNoVolume):
		writeError(w, http.StatusBadRequest, "speech_volume", "choose a mounted volume for the models")
	case errors.Is(err, speech.ErrUnknown):
		writeError(w, http.StatusNotFound, "not_found", "no such model or voice")
	case errors.Is(err, speech.ErrBusy):
		writeError(w, http.StatusConflict, "speech_busy", "this is being downloaded already")
	case errors.Is(err, speech.ErrActive):
		writeError(w, http.StatusConflict, "speech_active", "the active model or voice cannot be removed")
	case errors.Is(err, speech.ErrNotRunning):
		writeError(w, http.StatusServiceUnavailable, "speech_not_running", "the speech service is not running")
	case errors.Is(err, speech.ErrNotThere):
		writeError(w, http.StatusBadRequest, "speech_not_there", "download it first")
	default:
		writeError(w, http.StatusBadGateway, "speech_failed", err.Error())
	}
}

func (s *Server) handleSpeechSettings(w http.ResponseWriter, r *http.Request) {
	var req speech.Settings
	if !readJSON(w, r, &req) {
		return
	}
	m := s.opts.Speech
	models, voices := []string{}, []string{}
	if req.Volume == m.Settings().Volume {
		models, voices = m.Installed()
	}
	// Only what is downloaded can be made active.
	if (req.WhisperModel != "" && !slices.Contains(models, req.WhisperModel)) || (req.Voice != "" && !slices.Contains(voices, req.Voice)) {
		speechError(w, speech.ErrNotThere)
		return
	}
	if err := m.SetSettings(req); err != nil {
		if errors.Is(err, speech.ErrNoVolume) {
			speechError(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_speech_settings", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m.Settings())
}

type speechItem struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (s *Server) handleSpeechDownload(w http.ResponseWriter, r *http.Request) {
	var req speechItem
	if !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	if err := s.opts.Speech.Download(ctx, req.Kind, req.ID); err != nil {
		speechError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleSpeechCancel(w http.ResponseWriter, r *http.Request) {
	var req speechItem
	if !readJSON(w, r, &req) {
		return
	}
	s.opts.Speech.Cancel(req.Kind, req.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSpeechRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Speech.Remove(r.PathValue("kind"), r.PathValue("id")); err != nil {
		speechError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) speechSay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text  string `json:"text"`
		Voice string `json:"voice"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || len(text) > maxSpeechText {
		writeError(w, http.StatusBadRequest, "invalid_text", "1 to 4000 characters")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	audio, err := s.opts.Speech.Say(ctx, text, req.Voice)
	if err != nil {
		speechError(w, err)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(audio.WAV())
}

func (s *Server) speechHear(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxSpeechAudio+1))
	if err != nil || len(data) > maxSpeechAudio {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "at most 12 MB of audio")
		return
	}
	audio, err := speech.ParseWAV(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_audio", "a WAV file with 16 bit PCM is expected")
		return
	}
	language := r.URL.Query().Get("lang")
	if len(language) != 2 {
		language = ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	text, err := s.opts.Speech.Hear(ctx, audio, language)
	if err != nil {
		speechError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *Server) handleSpeechSay(w http.ResponseWriter, r *http.Request)  { s.speechSay(w, r) }
func (s *Server) handleSpeechHear(w http.ResponseWriter, r *http.Request) { s.speechHear(w, r) }

// speechForAgent says what the KI-Agent can use right now.
func (s *Server) speechForAgent() (say, hear bool) {
	m := s.opts.Speech
	if m == nil {
		return false, false
	}
	settings := m.Settings()
	if !settings.Enabled || !settings.Agent {
		return false, false
	}
	st := m.Status()
	return st.Piper.Phase == "running", st.Whisper.Phase == "running"
}

func (s *Server) handleAgentSpeech(w http.ResponseWriter, r *http.Request) {
	say, hear := s.speechForAgent()
	writeJSON(w, http.StatusOK, map[string]bool{"say": say, "hear": hear})
}

func (s *Server) handleAgentSpeechSay(w http.ResponseWriter, r *http.Request) {
	if say, _ := s.speechForAgent(); !say {
		speechError(w, speech.ErrNotRunning)
		return
	}
	s.speechSay(w, r)
}

func (s *Server) handleAgentSpeechHear(w http.ResponseWriter, r *http.Request) {
	if _, hear := s.speechForAgent(); !hear {
		speechError(w, speech.ErrNotRunning)
		return
	}
	s.speechHear(w, r)
}
