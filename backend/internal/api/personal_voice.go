package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/speech"
)

const personalVoiceID = "speech.transcribe"

func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		return []personalTool{{ID: personalVoiceID, Title: "Sprache lokal in Text umwandeln", App: "agent",
			Available: func(auth.User) error {
				if s.opts.Speech == nil || !s.opts.Speech.Settings().Enabled || s.opts.Speech.Status().Whisper.Phase != "running" {
					return speech.ErrNotRunning
				}
				return nil
			},
			Check: func(context.Context, auth.User, pa.Request) error {
				return errors.New("use the microphone to dictate into the composer")
			},
		}}
	})
}

// Dictation is an interactive input method. The user confirms this recording;
// audio is never attached to a job, persisted, or sent to a remote recognizer.
func (s *Server) personalVoice(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authArea().authenticate(r)
	allowed := func() bool {
		current, exists := s.opts.Users.Get(u.Name)
		if !ok || !exists || !current.Created.Equal(u.Created) || current.Disabled {
			return false
		}
		c, err := s.personalEffective(current, personalVoiceID)
		return err == nil && c.Effective != pa.Never && (c.Effective != pa.Ask || r.Header.Get("X-TitanOS-Use-Recording") == "yes")
	}
	if !allowed() {
		writeError(w, http.StatusForbidden, "forbidden", "dictation permission required")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxSpeechAudio+1))
	if err != nil || len(data) > maxSpeechAudio {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "audio is too large")
		return
	}
	audio, err := speech.ParseWAV(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_audio", "invalid WAV")
		return
	}
	if !allowed() {
		writeError(w, http.StatusForbidden, "forbidden", "dictation permission revoked")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !allowed() {
					cancel()
					return
				}
			}
		}
	}()
	lang := r.URL.Query().Get("lang")
	if len(lang) != 2 {
		lang = ""
	}
	text, err := s.opts.Speech.Hear(ctx, audio, lang)
	if !allowed() {
		writeError(w, http.StatusForbidden, "forbidden", "dictation permission revoked")
		return
	}
	if err != nil {
		speechError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}
