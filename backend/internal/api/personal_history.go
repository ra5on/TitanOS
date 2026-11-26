package api

import (
	"slices"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

type personalTurn struct{ Prompt, Answer string }

// History is always scoped to one canonical source set. An adapter must separately obtain
// permission to disclose history or memory to an external model.
func (s *Server) personalChatContext(u auth.User, r pa.Request) ([]personalTurn, []string, error) {
	var turns []personalTurn
	var memory []string
	if r.Tool != "chat.ask" {
		return nil, nil, errPersonalDenied
	}
	err := s.personalTxn(u, false, func(a *personalAccount) error {
		scope := personalDigest(r.Sources)
		for i := len(a.Jobs) - 1; i >= 0 && len(turns) < 8; i-- {
			j := a.Jobs[i]
			if r.ChatID == "" || j.Request.ChatID != r.ChatID || j.Request.Tool != "chat.ask" || j.Request.RequestID == r.RequestID || j.Status != "done" || j.Result == nil || personalDigest(j.Request.Sources) != scope {
				continue
			}
			turns = append(turns, personalTurn{Prompt: personalLimitText(j.Request.Prompt, 1024), Answer: personalLimitText(j.Result.Text, 2048)})
		}
		slices.Reverse(turns)
		available := 8192
		for _, v := range a.Memory {
			if len(v) > available {
				break
			}
			memory = append(memory, v)
			available -= len(v)
		}
		return nil
	})
	return turns, memory, err
}
