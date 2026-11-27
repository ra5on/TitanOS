package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/speech"
)

const personalReplyVoiceID = "speech.speak"

func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		return []personalTool{{ID: personalReplyVoiceID, Title: "Eigene Chatantwort lokal vorlesen", App: "agent",
			Available: func(auth.User) error {
				if s.opts.Speech == nil || !s.opts.Speech.Settings().Enabled {
					return speech.ErrNotRunning
				}
				_, err := s.opts.Speech.Address("piper")
				return err
			},
			Check: func(context.Context, auth.User, pa.Request) error {
				return errors.New("select a completed chat reply to read aloud")
			},
		}}
	})
}

func (s *Server) sayPersonalReply(ctx context.Context, text string) (speech.Audio, error) {
	if s.opts.Speech == nil {
		return speech.Audio{}, speech.ErrNotRunning
	}
	return s.opts.Speech.Say(ctx, text, "")
}

type personalReplyRun struct {
	owner string
	stop  context.CancelCauseFunc
}

// Caller holds personal.mu. Changing either right invalidates the entire current
// playback, including when the user immediately restores the previous level.
func (s *Server) stopPersonalReplyRuns(owner string) {
	for run := range s.personal.replyVoices {
		if owner == "" || run.owner == owner {
			run.stop(errPersonalDenied)
		}
	}
}

func (s *Server) readableReply(r *http.Request, owner auth.User) (string, error) {
	u, ok := s.loginHolds(r, owner)
	if !ok {
		return "", errPersonalDenied
	}
	tool := s.personal.tools[personalReplyVoiceID]
	var account *personalAccount
	err := s.personalTxn(u, false, func(a *personalAccount) error { account = a; return nil })
	if err != nil {
		return "", err
	}
	j, err := account.find(r.PathValue("id"))
	if err != nil {
		return "", err
	}
	plan := account.Plans[j.ID]
	if j.Request.Tool != "chat.ask" || j.Status != "done" || j.Result == nil || strings.TrimSpace(j.Result.Text) == "" ||
		plan == nil || len(plan.Steps) != 1 || plan.Steps[0] != "chat.ask" {
		return "", errPersonalConflict
	}
	level := personalCap(tool, account).Effective
	if level == pa.Never || (level == pa.Ask && r.Header.Get("X-TitanOS-Play-Reply") != "yes") ||
		!s.personalRedact(r.Context(), u, account, j) {
		return "", errPersonalDenied
	}
	if err := tool.Available(u); err != nil {
		return "", err
	}
	return j.Result.Text, nil
}

func replyVoiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, speech.ErrNotRunning):
		speechError(w, speech.ErrNotRunning)
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, 504, "speech_timeout", "the local voice timed out")
	default:
		personalRespond(w, nil, err)
	}
}

// The browser chooses a job, never the words or a speech provider. Audio only
// leaves the process after the exact stored answer and read access are rechecked.
func (s *Server) personalReplyVoice(say func(context.Context, string) (speech.Audio, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 || r.URL.RawQuery != "" {
			personalRespond(w, nil, errPersonalInvalid)
			return
		}
		u := currentUser(r)
		timed, finish := context.WithTimeout(r.Context(), 3*time.Minute)
		defer finish()
		ctx, cancel := context.WithCancelCause(timed)
		run := &personalReplyRun{u.Name, cancel}
		s.personal.mu.Lock()
		if s.personal.replyVoices == nil {
			s.personal.replyVoices = map[*personalReplyRun]struct{}{}
		}
		s.personal.replyVoices[run] = struct{}{}
		s.personal.mu.Unlock()
		defer func() {
			cancel(nil)
			s.personal.mu.Lock()
			delete(s.personal.replyVoices, run)
			s.personal.mu.Unlock()
		}()

		text, err := s.readableReply(r, u)
		if err != nil {
			replyVoiceError(w, err)
			return
		}
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if _, ok := s.loginHolds(r, u); !ok {
						cancel(errPersonalDenied)
						return
					}
					if err := s.personal.tools[personalReplyVoiceID].Available(u); err != nil {
						cancel(err)
						return
					}
				}
			}
		}()
		audio, synthErr := say(ctx, text)
		if err := context.Cause(ctx); err != nil {
			replyVoiceError(w, err)
			return
		}
		if synthErr != nil {
			if errors.Is(synthErr, speech.ErrNotRunning) {
				replyVoiceError(w, synthErr)
			} else {
				writeError(w, 502, "speech_failed", "the local voice could not finish this answer")
			}
			return
		}
		wav := audio.WAV()
		fresh, err := s.readableReply(r, u)
		if err != nil || fresh != text || ctx.Err() != nil {
			if err == nil {
				err = errPersonalDenied
			}
			replyVoiceError(w, err)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", strconv.Itoa(len(wav)))
		for len(wav) > 0 && ctx.Err() == nil {
			end := min(len(wav), 32<<10)
			if _, err := w.Write(wav[:end]); err != nil {
				return
			}
			wav = wav[end:]
		}
	}
}
