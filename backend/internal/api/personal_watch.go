package api

import (
	"context"
	"errors"
	"maps"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

type personalMailWatch struct {
	Origin string            `json:"origin,omitempty"`
	Retry  bool              `json:"retry,omitempty"`
	Cursor tmail.InboxCursor `json:"cursor"`
	After  time.Time         `json:"after"`
	Stamp  string            `json:"stamp"`
}

func watchSource(sk pa.Skill) (pa.Source, error) {
	var inbox pa.Source
	for _, src := range sk.Sources {
		if src.Kind == "mail" || src.Kind == "mailbox" {
			if inbox.Kind != "" || src.Ref["account"] == "" || !strings.EqualFold(src.Ref["folder"], "INBOX") {
				return inbox, errPersonalInvalid
			}
			inbox = src
		}
	}
	if inbox.Kind == "" {
		return inbox, errPersonalInvalid
	}
	a, err := mail.ParseAddress(sk.Sender)
	if err != nil || a.Name != "" || a.Address != sk.Sender || len(sk.Sender) > 254 {
		return inbox, errPersonalInvalid
	}
	return inbox, nil
}
func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		return []personalTool{{ID: "mail.watch", Title: "Posteingang nach explizitem Absender beobachten", App: "mail", Default: pa.Never, Available: func(u auth.User) error {
			if s.opts.Mail == nil || !s.opts.Mail.Allowed(u.Name) {
				return errPersonalDenied
			}
			return nil
		}}}
	})
}
func (s *Server) personalWatchBaseline(ctx context.Context, u auth.User, sk pa.Skill) (personalMailWatch, error) {
	w := personalMailWatch{}
	src, err := watchSource(sk)
	if err != nil {
		return w, err
	}
	c, err := s.personalEffective(u, "mail.watch")
	if err != nil || c.Effective != pa.Auto {
		return w, errPersonalDenied
	}
	w.Stamp, err = s.personalGrantRevisions(u, u.Name)
	if err != nil {
		return w, err
	}
	w.Cursor, _, err = s.opts.Mail.AgentInbox(ctx, u.Name, src.Ref["account"], sk.Sender, tmail.InboxCursor{})
	w.After = time.Now().UTC().Add(time.Minute)
	return w, err
}
func (a *personalAccount) nextWatch() time.Time {
	if a.Settings.Paused {
		return time.Time{}
	}
	var due time.Time
	for _, w := range a.Watches {
		if due.IsZero() || w.After.Before(due) {
			due = w.After
		}
	}
	return due
}

// Called only over the service's private Unix connection. One pass advances at most one
// rule. A cursor and its jobs commit together so a restart neither skips nor duplicates mail.
func (s *Server) personalPollWatches(ctx context.Context) error {
	if !s.personal.watch.TryLock() {
		return nil
	}
	defer s.personal.watch.Unlock()
	users := s.opts.Users.List()
	for _, u := range users {
		if u.Disabled {
			continue
		}
		s.personal.mu.Lock()
		due, known := s.personal.watchDue[personalKey(u)]
		s.personal.mu.Unlock()
		if known && (due.IsZero() || due.After(time.Now())) {
			continue
		}
		var skill pa.Skill
		var watch personalMailWatch
		err := s.personalTxn(u, false, func(a *personalAccount) error {
			if a.Settings.Paused {
				return nil
			}
			for _, v := range a.Skills {
				w, ok := a.Watches[v.ID]
				if ok && !w.After.After(time.Now()) {
					skill = v
					watch = w
					return nil
				}
			}
			return nil
		})
		if err != nil {
			s.personal.mu.Lock()
			s.personal.watchDue[personalKey(u)] = time.Now().Add(15 * time.Second)
			s.personal.mu.Unlock()
			continue
		}
		if skill.ID == "" {
			continue
		}
		return s.personalPollInbox(ctx, u, skill, watch)
	}
	return nil
}
func (s *Server) personalPollInbox(ctx context.Context, u auth.User, sk pa.Skill, watch personalMailWatch) error {
	stamp, err := s.personalGrantRevisions(u, u.Name)
	if err != nil {
		return err
	}
	cap, err := s.personalEffective(u, "mail.watch")
	if err != nil {
		return err
	}
	source, err := watchSource(sk)
	if err != nil {
		return err
	}
	allowed := stamp == watch.Stamp && cap.Effective == pa.Auto && s.opts.Mail != nil
	cursor := watch.Cursor
	var hits []tmail.Summary
	if allowed {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		cursor, hits, err = s.opts.Mail.AgentInbox(ctx, u.Name, source.Ref["account"], sk.Sender, watch.Cursor)
	}
	return s.personalTxn(u, true, func(a *personalAccount) error {
		old, exists := a.Watches[sk.ID]
		if !exists || old.Cursor != watch.Cursor || a.Settings.Paused {
			return nil
		}
		fresh, e := s.personalGrantRevisions(u, u.Name)
		if e != nil {
			return e
		}
		if !allowed || stamp != fresh || personalCap(s.personal.tools["mail.watch"], a).Effective != pa.Auto {
			// A regrant requires a newly enabled recipe rather than importing mail that
			// arrived while this rule lacked permission.
			delete(a.Watches, sk.ID)
			return nil
		}
		if err != nil {
			old.Retry = true
			old.After = time.Now().UTC().Add(2 * time.Minute)
			a.Watches[sk.ID] = old
			return nil
		}
		for _, msg := range hits {
			if !strings.EqualFold(msg.From.Addr, sk.Sender) {
				continue
			}
			src := pa.Source{Kind: "mail", ID: strconv.FormatUint(uint64(msg.UID), 10), Label: "Mail", Ref: map[string]string{"folder": "INBOX", "account": source.Ref["account"], "uid": strconv.FormatUint(uint64(msg.UID), 10), "messageId": msg.MessageID, "date": msg.Date.UTC().Format(time.RFC3339Nano)}}
			sources := []pa.Source{src}
			if sk.PDFName != "" {
				pdf := pa.Source{Kind: "mail.pdf", ID: src.ID + ":pdf", Label: sk.PDFName, Ref: maps.Clone(src.Ref)}
				pdf.Ref["filename"] = sk.PDFName
				sources = append(sources, pdf)
			}
			for _, x := range sk.Sources {
				if x.Kind != "mail" && x.Kind != "mailbox" && x.Kind != "mail.pdf" {
					sources = append(sources, x)
				}
			}
			req := pa.Request{Origin: watch.Origin, Tool: sk.Steps[0], Prompt: sk.Prompt, Args: sk.Args, RequestID: "mail:" + personalDigest([]any{sk.ID, source.Ref["account"], cursor.Validity, msg.UID}), Sources: sources}
			j, e := s.personalEnqueue(u, a, req, sk.Steps, sk.Approval == "ask")
			if errors.Is(e, errPersonalDenied) {
				delete(a.Watches, sk.ID)
				return nil
			}
			if e != nil {
				return e
			}
			p := a.Plans[j.ID]
			p.Expected = sk.Expected
			p.SkillID = sk.ID
			p.StepArgs = sk.StepArgs
		}
		old.After = time.Now().UTC().Add(time.Minute)
		old.Retry = false
		old.Cursor = cursor
		a.Watches[sk.ID] = old
		return nil
	})
}
