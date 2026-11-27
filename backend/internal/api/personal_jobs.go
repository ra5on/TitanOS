package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

func personalDigest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *Server) personalStepRequest(j *pa.Job, p *personalPlan, tool string) pa.Request {
	req := j.Request
	req.Tool = tool
	if p != nil {
		req.Origin = p.Origin
		req.MailTriggered = p.MailTriggered
	}
	if p != nil && p.StepArgs[tool] != nil {
		req.Args = p.StepArgs[tool]
	}
	if p != nil && len(p.Steps) > 1 {
		req.Sources = nil
		for _, src := range j.Request.Sources {
			if slices.Contains(s.personal.tools[tool].SourceKinds, src.Kind) {
				req.Sources = append(req.Sources, src)
			}
		}
	}
	// The workflow never turns source text into a prompt, tool, destination or arbitrary argument.
	if p != nil && p.Index > 0 && p.Prior != "" {
		req.PreviousText = p.Prior
	}
	return req
}
func (s *Server) personalNewJob(u auth.User, r pa.Request, steps []string, ask bool) (pa.Job, error) {
	var j pa.Job
	if err := validatePersonalRequest(r); err != nil {
		return j, err
	}
	err := s.personalTxn(u, true, func(a *personalAccount) error {
		var err error
		j, err = s.personalEnqueue(u, a, r, steps, ask)
		return err
	})
	return j, err
}
func (s *Server) personalEnqueue(u auth.User, a *personalAccount, r pa.Request, steps []string, ask bool) (pa.Job, error) {
	for _, old := range a.Jobs {
		if old.Request.RequestID != r.RequestID {
			continue
		}
		copy := r
		if copy.ChatID == "" {
			copy.ChatID = old.Request.ChatID
		}
		if personalDigest(copy) != personalDigest(old.Request) || !slices.Equal(a.Plans[old.ID].Steps, steps) {
			return pa.Job{}, errPersonalConflict
		}
		return old, nil
	}
	if len(steps) == 0 || r.Tool != steps[0] {
		return pa.Job{}, errPersonalInvalid
	}
	for _, id := range steps {
		t, ok := s.personal.tools[id]
		if !ok || t.Prepare == nil {
			return pa.Job{}, errPersonalMissing
		}
		if personalCap(t, a).Effective == pa.Never {
			return pa.Job{}, errPersonalDenied
		}
	}
	stamp, err := s.personalGrantStamp(u)
	if err != nil {
		return pa.Job{}, err
	}
	now := time.Now().UTC()
	if r.ChatID == "" {
		r.ChatID = personalID()
		title := personalLimitText(strings.TrimSpace(r.Prompt), 80)
		if title == "" {
			title = s.personal.tools[r.Tool].Title
		}
		a.Chats = append(a.Chats, pa.Chat{ID: r.ChatID, Title: title, Created: now})
	} else if !slices.ContainsFunc(a.Chats, func(c pa.Chat) bool { return c.ID == r.ChatID }) {
		return pa.Job{}, errPersonalMissing
	}
	j := pa.Job{TotalSteps: len(steps), Audit: []pa.Audit{{Time: now, State: "planned", Tool: steps[0]}}, ID: personalID(), Request: r, Status: "planned", Phase: "queued", Created: now, Updated: now}
	a.Jobs = append(a.Jobs, j)
	a.Plans[j.ID] = &personalPlan{Origin: r.Origin, Steps: slices.Clone(steps), Ask: ask, GrantStamp: stamp}
	return j, nil
}

// Use the authority of the local browser request, never a URL from source content,
// arguments or forwarded proxy headers. Only fixed, authenticated file routes use it.
func personalOrigin(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if strings.ContainsAny(r.Host, "/?#@\\") {
		return ""
	}
	origin := url.URL{Scheme: scheme, Host: r.Host}
	parsed, err := url.ParseRequestURI(origin.String())
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return parsed.String()
}
func (s *Server) personalCreate(w http.ResponseWriter, r *http.Request) {
	var req pa.Request
	if !readPersonalJSON(w, r, &req) {
		return
	}
	u := currentUser(r)
	req.Origin = personalOrigin(r)
	if err := validatePersonalRequest(req); err != nil {
		personalRespond(w, nil, err)
		return
	}
	if _, err := s.personalCheck(r.Context(), u, req); err != nil {
		personalRespond(w, nil, err)
		return
	}
	j, err := s.personalNewJob(u, req, []string{req.Tool}, false)
	s.personalRespondJob(w, r, j.ID, err)
}
func (s *Server) personalGetJob(w http.ResponseWriter, r *http.Request) {
	s.personalRespondJob(w, r, r.PathValue("id"), nil)
}
func (s *Server) personalRespondJob(w http.ResponseWriter, r *http.Request, id string, inputErr error) {
	if inputErr != nil {
		personalRespond(w, nil, inputErr)
		return
	}
	var j *pa.Job
	var state *personalAccount
	u := currentUser(r)
	err := s.personalTxn(u, false, func(a *personalAccount) error {
		var err error
		j, err = a.find(id)
		state = a
		return err
	})
	if err == nil {
		s.personalRedact(r.Context(), u, state, j)
	}
	personalRespond(w, j, err)
}
func (s *Server) personalAct(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Args  json.RawMessage `json:"args,omitempty"`
		Token string          `json:"token"`
	}
	action := r.PathValue("action")
	if (action == "approve" || action == "revise") && !readPersonalJSON(w, r, &req) {
		return
	}
	u := currentUser(r)
	id := r.PathValue("id")
	if action == "approve" || action == "revise" {
		stamp, stampErr := s.personalGrantStamp(u)
		if stampErr != nil {
			personalRespond(w, nil, stampErr)
			return
		}
		var actionReq pa.Request
		err := s.personalTxn(u, false, func(a *personalAccount) error {
			j, err := a.find(id)
			if err != nil {
				return err
			}
			p := a.Plans[id]
			if j.Status != "waiting" {
				return errPersonalConflict
			}
			if p.GrantStamp != stamp {
				return errPersonalDenied
			}
			actionReq = s.personalStepRequest(j, p, p.Steps[p.Index])
			return nil
		})
		if err == nil {
			_, err = s.personalWorkflowCheck(r.Context(), u, id, actionReq)
		}
		if err != nil {
			if errors.Is(err, errPersonalDenied) {
				s.personalRejectApproval(u, id)
			}
			personalRespond(w, nil, err)
			return
		}
	}
	err := s.personalTxn(u, true, func(a *personalAccount) error {
		j, err := a.find(id)
		if err != nil {
			return err
		}
		p := a.Plans[id]
		switch action {
		case "seen":
			j.Seen = true
			return nil
		case "cancel":
			if j.Status == "canceled" {
				return nil
			}
			if j.Status == "done" || j.Status == "failed" {
				return errPersonalConflict
			}
			if j.Phase == "executing" {
				p.CancelAfter = true
				return nil
			}
			j.Status = "canceled"
			j.Phase = "complete"
			j.Preview = nil
			j.Token = ""
			p.Claim = ""
			p.Approval = ""
			p.ReadOK = false
		case "pause":
			if j.Status == "paused" {
				return nil
			}
			if j.Status == "done" || j.Status == "failed" || j.Status == "canceled" {
				return errPersonalConflict
			}
			if j.Phase == "executing" {
				p.PauseAfter = true
				return nil
			}
			j.Status = "paused"
			j.Phase = "queued"
			j.Token = ""
			j.Preview = nil
			p.ReadOK = false
			p.Approval = ""
			p.Claim = ""
		case "resume":
			if j.Status != "paused" {
				return errPersonalConflict
			}
			j.Status = "planned"
			j.Phase = "queued"
			p.PauseAfter = false
		case "revise":
			if j.Status != "waiting" || j.Phase != "approval" || req.Token == "" || req.Token != j.Token || len(req.Args) == 0 || len(req.Args) > 32768 {
				return errPersonalConflict
			}
			if p.StepArgs == nil {
				p.StepArgs = map[string]json.RawMessage{}
			}
			p.StepArgs[p.Steps[p.Index]] = req.Args
			p.Approval = ""
			j.Token = ""
			j.Preview = nil
			j.Phase = "queued"
			j.Status = "planned"
		case "approve":
			if j.Status != "waiting" || req.Token == "" || !bytes.Equal([]byte(req.Token), []byte(j.Token)) {
				return errPersonalConflict
			}
			if j.Phase == "permission" {
				p.ReadOK = true
			} else if j.Phase == "approval" && j.Preview != nil {
				p.Approval = personalDigest(j.Preview)
			} else {
				return errPersonalConflict
			}
			j.Status = "planned"
			j.Phase = "queued"
			j.Token = ""
		default:
			return errPersonalMissing
		}
		j.Updated = time.Now().UTC()
		personalAudit(j, p, action)
		return nil
	})
	s.personalRespondJob(w, r, id, err)
}

func personalAudit(j *pa.Job, p *personalPlan, state string) {
	if len(j.Audit) >= 200 {
		j.Audit = slices.Clone(j.Audit[len(j.Audit)-199:])
	}
	j.Audit = append(j.Audit, pa.Audit{Time: time.Now().UTC(), State: state, Tool: p.Steps[p.Index]})
}
