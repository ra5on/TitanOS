package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

var errPersonalDuplicate = errors.New("another job already attempted this native action")

func (s *Server) personalClaim() (string, error) {
	rt := &s.personal
	rt.tick.Lock()
	defer rt.tick.Unlock()
	rt.mu.Lock()
	for id, l := range rt.leases {
		if l.Until.Before(time.Now()) && !rt.processing[id] {
			delete(rt.leases, id)
		}
	}
	rt.mu.Unlock()
	us := s.opts.Users.List()
	if len(us) == 0 {
		return "", nil
	}
	for i := range us {
		idx := (i + rt.cursor) % len(us)
		u := us[idx]
		if u.Disabled {
			continue
		}
		leaseID := ""
		var recovered string
		var notifyRecovery bool
		rt.mu.Lock()
		next, known := rt.known[personalKey(u)]
		rt.mu.Unlock()
		if known && (next.IsZero() || time.Until(next) > 0) {
			continue
		}
		err := s.personalTxn(u, false, func(a *personalAccount) error {
			if a.Settings.Paused {
				return nil
			}
			for i := range a.Jobs {
				j := &a.Jobs[i]
				p := a.Plans[j.ID]
				if j.Status != "planned" && j.Status != "running" || j.Request.RunAt.After(time.Now()) || p == nil {
					continue
				}
				if p.Claim != "" {
					if l, ok := rt.leases[p.Claim]; ok {
						if rt.processing[p.Claim] || l.Until.After(time.Now()) {
							continue
						}
						delete(rt.leases, p.Claim)
					}
				}
				if j.Phase == "executing" {
					recovered = j.ID
					return nil
				}
				// Reserve in memory under the same account lock. After a daemon restart a read
				// can be prepared again. A persisted executing marker is handled separately.
				for key, l := range rt.leases {
					if l.Owner != u.Name || l.Job != j.ID {
						continue
					}
					if rt.processing[key] || l.Until.After(time.Now()) {
						return nil
					}
					delete(rt.leases, key)
				}
				leaseID = personalID()
				rt.leases[leaseID] = personalLease{Owner: u.Name, Created: u.Created, Job: j.ID, Until: time.Now().Add(20 * time.Second)}
				return nil
			}
			return nil
		})
		if err != nil {
			rt.mu.Lock()
			rt.known[personalKey(u)] = time.Now().Add(15 * time.Second)
			rt.mu.Unlock()
			continue
		}
		if recovered != "" {
			err = s.personalTxn(u, true, func(a *personalAccount) error {
				j, err := a.find(recovered)
				if err != nil {
					return err
				}
				if j.Phase != "executing" {
					return errPersonalConflict
				}
				j.Status = "failed"
				j.Phase = "uncertain"
				j.Result = nil
				j.Error = "effect_unconfirmed"
				notifyRecovery = a.Settings.Notifications.Blocker
				personalAudit(j, a.Plans[j.ID], "uncertain")
				j.Updated = time.Now().UTC()
				a.Plans[j.ID].Claim = ""
				return nil
			})
			if err == nil && notifyRecovery {
				s.personalNotice(u, recovered, "failed")
			}
			continue
		}
		if leaseID != "" {
			rt.cursor = (idx + 1) % len(us)
			return leaseID, nil
		}
	}
	return "", nil
}

func (s *Server) personalNotice(u auth.User, id, status string) {
	current, ok := s.opts.Users.Get(u.Name)
	if !ok || current.Disabled || personalKey(current) != personalKey(u) {
		return
	}
	body := "Ein Auftrag hat ein Ergebnis."
	if status == "waiting" {
		body = "Ein Auftrag benötigt deine Freigabe."
	} else if status == "failed" {
		body = "Ein Auftrag benötigt deine Aufmerksamkeit."
	}
	link := "/agent?job=" + id
	if s.opts.Notify != nil && u.Role.CanAdminister() {
		s.opts.Notify.NotifyTo(notify.Info, "personal_agent_"+status, "", []notify.Target{{User: u.Name, Bell: true}}, false, notify.About{Ref: "agent", Link: link})
	}
	if s.opts.WebPush != nil {
		s.opts.WebPush.SendTo(u.Name, webpush.Message{Title: "Agent", Body: body, URL: link, Tag: "personal-agent-" + id})
	}
}
func (s *Server) personalLeaseOwner(token string) (personalLease, auth.User, error) {
	rt := &s.personal
	rt.mu.Lock()
	l, ok := rt.leases[token]
	rt.mu.Unlock()
	if !ok || time.Now().After(l.Until) {
		return l, auth.User{}, errPersonalConflict
	}
	u, ok := s.opts.Users.Get(l.Owner)
	if !ok || u.Disabled || !u.Created.Equal(l.Created) {
		return l, u, errPersonalDenied
	}
	return l, u, nil
}

// Each native operation can be canceled even while waiting for a model or a remote mailbox.
func (s *Server) personalWatch(parent context.Context, token string, u auth.User, jid string, req pa.Request) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	s.personal.mu.Lock()
	s.personal.active[token] = cancel
	s.personal.mu.Unlock()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				err := s.personalTxn(u, false, func(a *personalAccount) error {
					j, err := a.find(jid)
					if err != nil {
						return err
					}
					if a.Settings.Paused || j.Status != "running" || a.Plans[jid].Claim != token {
						return errPersonalDenied
					}
					return nil
				})
				if err == nil {
					_, err = s.personalWorkflowCheck(ctx, u, jid, req)
				}
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() {
		close(stop)
		cancel()
		<-done
		s.personal.mu.Lock()
		delete(s.personal.active, token)
		s.personal.mu.Unlock()
	}
}
func (s *Server) personalDropLease(token string) {
	s.personal.mu.Lock()
	delete(s.personal.leases, token)
	s.personal.mu.Unlock()
}
func (s *Server) personalStep(parent context.Context, token string, apply bool) (bool, error) {
	l, u, err := s.personalLeaseOwner(token)
	if err != nil {
		return false, err
	}
	s.personal.mu.Lock()
	if s.personal.processing[token] {
		s.personal.mu.Unlock()
		return false, errPersonalConflict
	}
	s.personal.processing[token] = true
	s.personal.mu.Unlock()
	defer func() { s.personal.mu.Lock(); delete(s.personal.processing, token); s.personal.mu.Unlock() }()
	stamp, err := s.personalGrantStamp(u)
	if err != nil {
		s.personalDropLease(token)
		return false, err
	}
	var req pa.Request
	var approved string
	var readOK, ask, staleSource bool
	var saved pa.Preview
	err = s.personalTxn(u, true, func(a *personalAccount) error {
		j, err := a.find(l.Job)
		if err != nil {
			return err
		}
		p := a.Plans[j.ID]
		if p == nil || p.Index >= len(p.Steps) || a.Settings.Paused || j.Status != "planned" && j.Status != "running" {
			return errPersonalConflict
		}
		if p.GrantStamp != stamp {
			staleSource = p.Index > 0
			p.ReadOK = false
			p.Approval = ""
			p.GrantStamp = stamp
			if apply {
				return errPersonalDenied
			}
		}
		if apply {
			if j.Phase != "ready" || p.Claim != token || j.Preview == nil {
				return errPersonalConflict
			}
			saved = *j.Preview
		} else {
			if j.Phase == "executing" || j.Status == "running" && p.Claim == token {
				return errPersonalConflict
			}
			p.Claim = token
			j.Phase = "preparing"
		}
		j.Status = "running"
		j.Updated = time.Now().UTC()
		req = s.personalStepRequest(j, p, p.Steps[p.Index])
		approved = p.Approval
		readOK = p.ReadOK
		ask = p.Ask
		return nil
	})
	if err != nil {
		s.personalDropLease(token)
		return false, err
	}
	ctx, stop := s.personalWatch(parent, token, u, l.Job, req)
	defer stop()
	level, checkErr := s.personalWorkflowCheck(ctx, u, l.Job, req)
	t := s.personal.tools[req.Tool]
	var preview pa.Preview
	var result pa.Result
	state := "done"
	if checkErr != nil || staleSource {
		state = "denied"
	} else if !readOK && (level == pa.Ask || ask) {
		state = "permission"
		preview = pa.Preview{Title: t.Title, Text: req.Prompt, Sources: req.Sources}
	} else {
		preview, checkErr = t.Prepare(ctx, u, req)
		if checkErr != nil {
			state = "failed"
		} else if len(preview.Text) > 128<<10 || len(preview.Data) > 128<<10 {
			state = "failed"
		} else if t.Apply != nil {
			digest := personalDigest(&preview)
			switch {
			case (t.Sensitive || level == pa.Ask || ask) && approved != digest:
				state = "approval"
			case !apply:
				state = "ready"
			case personalDigest(&saved) != digest:
				state = "approval"
			default:
				// Fsync the irreversible fence before any external write. On a lost acknowledgement
				// or a process/power failure this action is never retried automatically.
				effect := ""
				if t.EffectKey != nil {
					effect = t.EffectKey(u, req, preview)
				} else if t.Sensitive {
					effect = personalDigest(preview)
				}
				if effect != "" {
					effect = personalDigest([]string{req.Tool, effect})
				}
				checkErr = s.personalFence(ctx, u, l.Job, token, req, readOK, effect)
				if checkErr == nil {
					result, checkErr = t.Apply(ctx, u, req, preview)
					if checkErr != nil {
						state = "uncertain"
						if errors.Is(checkErr, pa.ErrNoEffect) {
							state = "refused"
						}
					}
				} else {
					state = "denied"
					if errors.Is(checkErr, errPersonalDuplicate) {
						state = "duplicate"
					}
				}
			}
		} else {
			result = pa.Result{Silent: preview.Silent, Title: preview.Title, Text: preview.Text, Sources: preview.Sources, Data: preview.Data}
		}
	}
	if ctx.Err() != nil && state != "uncertain" {
		if apply && state == "done" {
			state = "uncertain"
		} else {
			state = "denied"
		}
	}
	// Recheck immediately on completion as well, even when a tool ignored cancellation.
	if _, e := s.personalWorkflowCheck(ctx, u, l.Job, req); e != nil && state == "done" {
		if apply {
			state = "uncertain"
		} else {
			state = "denied"
		}
	}
	if state == "done" && (len(result.Text) > 128<<10 || len(result.Data) > 128<<10) {
		state = "failed"
		if apply {
			state = "uncertain"
		}
	}
	again, err := s.personalFinish(u, l.Job, token, state, preview, result)
	if !again {
		s.personalDropLease(token)
	}
	return again, err
}
func (s *Server) personalFence(ctx context.Context, u auth.User, id, token string, req pa.Request, readOK bool, effect string) error {
	l, err := s.personalWorkflowCheck(ctx, u, id, req)
	if err != nil {
		return err
	}
	if l == pa.Ask && !readOK {
		return errPersonalDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.personalTxn(u, true, func(a *personalAccount) error {
		j, err := a.find(id)
		if err != nil {
			return err
		}
		p := a.Plans[id]
		stamp, err := s.personalGrantStamp(u)
		if err != nil || stamp != p.GrantStamp {
			return errPersonalDenied
		}
		if j.Phase != "ready" || j.Status != "running" || p.Claim != token || a.Settings.Paused {
			return errPersonalConflict
		}
		c := personalCap(s.personal.tools[req.Tool], a)
		if c.Effective == pa.Never || (c.Effective == pa.Ask && !p.ReadOK) {
			return errPersonalDenied
		}
		if effect != "" {
			if a.Effects[effect] != "" {
				return errPersonalDuplicate
			}
			a.Effects[effect] = id
			p.Effect = effect
		}
		j.Phase = "executing"
		personalAudit(j, p, "executing")
		return nil
	})
}
func (s *Server) personalFinish(u auth.User, id, token, state string, preview pa.Preview, result pa.Result) (bool, error) {
	again := false
	notice := ""
	err := s.personalTxn(u, true, func(a *personalAccount) error {
		j, err := a.find(id)
		if err != nil {
			return err
		}
		p := a.Plans[id]
		if p.Claim != token || j.Status != "running" {
			return errPersonalConflict
		}
		j.Updated = time.Now().UTC()
		personalAudit(j, p, state)
		j.Token = ""
		switch state {
		case "permission", "approval":
			j.Preview = &preview
			j.Status = "waiting"
			j.Phase = state
			j.Token = personalID()
			p.Approval = ""
			notice = j.Status
		case "ready":
			j.Preview = &preview
			j.Phase = "ready"
			again = true
			l := s.personal.leases[token]
			l.Until = time.Now().Add(30 * time.Second)
			s.personal.leases[token] = l
		case "done":
			j.CompletedSteps = p.Index + 1
			j.Receipts = append(j.Receipts, pa.Receipt{Step: p.Index + 1, Tool: p.Steps[p.Index], Title: result.Title, At: j.Updated})
			j.Result = &result
			j.Preview = nil
			if p.Index+1 < len(p.Steps) {
				p.Index++
				p.Prior = personalLimitText(result.Text, 8192)
				p.Approval = ""
				p.ReadOK = false
				p.Effect = ""
				j.Phase = "queued"
				j.Status = "planned"
				if p.CancelAfter {
					j.Status = "canceled"
					j.Phase = "complete"
				} else if p.PauseAfter {
					j.Status = "paused"
					p.PauseAfter = false
				}
			} else {
				j.Status = "done"
				j.Phase = "complete"
				observed := result.Text
				if p.Index > 0 && s.personal.tools[p.Steps[p.Index]].Apply != nil {
					observed = p.Prior
				}
				if !result.Silent && (p.Expected == "" || strings.TrimSpace(observed) != p.Expected) {
					notice = j.Status
				}
			}
		default:
			j.Status = "failed"
			j.Phase = "complete"
			if state != "uncertain" && state != "refused" {
				j.Preview = nil
			}
			j.Result = nil
			if state == "uncertain" {
				j.Phase = "uncertain"
				j.Error = "effect_unconfirmed"
			} else if state == "duplicate" {
				j.Error = "action_already_attempted"
			} else if state == "refused" {
				if p.Effect != "" && a.Effects[p.Effect] == id {
					delete(a.Effects, p.Effect)
					p.Effect = ""
				}
				j.Error = "effect_rejected"
			} else if state == "denied" {
				j.Error = "permission_revoked"
				p.Prior = ""
			} else {
				j.Error = "tool_failed"
			}
			notice = j.Status
		}
		if notice == "done" && !a.Settings.Notifications.Result || notice == "failed" && !a.Settings.Notifications.Blocker || notice == "waiting" && !a.Settings.Notifications.Approval {
			notice = ""
		}
		if !again {
			p.Claim = ""
		}
		return nil
	})
	if err == nil && notice != "" {
		s.personalNotice(u, id, notice)
	}
	return again, err
}
