package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	store "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/tmail"
)

// Produce a minimal calendar request. Other attendees, notes, alarms, links
// and attachments from the original appointment never leave the calendar.
func inviteICS(v calweb.View, original []byte, from, to string, local *time.Location, cancel, priorInvite bool) ([]byte, error) {
	if v.Recurring {
		return nil, errors.New("select a single, non-recurring appointment")
	}
	cal, err := ical.NewDecoder(bytes.NewReader(original)).Decode()
	if err != nil {
		return nil, errPersonalSource
	}
	sequence := 0
	known := priorInvite
	for _, item := range cal.Children {
		if item.Name != ical.CompEvent {
			continue
		}
		if owner := item.Props.Get("ORGANIZER"); owner != nil && !strings.EqualFold(owner.Value, "mailto:"+from) {
			return nil, errors.New("the selected mailbox is not the organizer")
		}
		for _, attendee := range item.Props.Values("ATTENDEE") {
			if strings.EqualFold(attendee.Value, "mailto:"+to) {
				known = true
			}
		}
		if seq := item.Props.Get("SEQUENCE"); seq != nil {
			v, err := strconv.Atoi(seq.Value)
			if err != nil || v < 0 || v > 1000000 {
				return nil, errPersonalSource
			}
			sequence = v
		}
	}
	if cancel && !known {
		return nil, errors.New("the recipient has no verified invitation to this appointment")
	}

	// Rebuild the content via the same calendar code as the native editor.
	// A fixed stamp from the source, rather than the current clock, binds the
	// same invitation across both sides of approval.
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, item := range cal.Children {
		if item.Name == ical.CompEvent {
			if d := item.Props.Get("DTSTAMP"); d != nil {
				if dt, parseErr := time.Parse("20060102T150405Z", d.Value); parseErr == nil {
					stamp = dt
				}
			}
		}
	}
	data, err := calweb.Build(calweb.Input{Title: &v.Title, Start: v.Start, End: v.End, AllDay: v.AllDay, Timezone: v.Timezone, Location: &v.Location}, v.UID, stamp, local)
	if err != nil {
		return nil, err
	}
	clean, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
	if err != nil {
		return nil, err
	}
	method := "REQUEST"
	if cancel {
		method = "CANCEL"
		sequence++
	}
	clean.Props.SetText("METHOD", method)
	for _, ev := range clean.Children {
		if ev.Name == ical.CompEvent {
			ev.Props.SetURI("ORGANIZER", &url.URL{Scheme: "mailto", Opaque: from})
			attendee := ical.NewProp("ATTENDEE")
			attendee.Value = "mailto:" + to
			if cancel {
				ev.Props.SetText("STATUS", "CANCELLED")
			} else {
				attendee.Params.Set("RSVP", "TRUE")
			}
			ev.Props.SetText("SEQUENCE", strconv.Itoa(sequence))
			ev.Props.Set(attendee)
		}
	}
	var b bytes.Buffer
	err = ical.NewEncoder(&b).Encode(clean)
	return b.Bytes(), err
}

func (s *Server) invitePlan(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, string, tmail.Outgoing, error) {
	var p pa.Preview
	var out tmail.Outgoing
	native := personalPIM{s.pimDataArea(), store.Calendar, "calendar"}
	if err := native.check(ctx, u, r); err != nil {
		return p, "", out, err
	}
	mailtools := personalMailTools{s.opts.Mail, s.personalInfer}
	if err := mailtools.allowed(u); err != nil {
		return p, "", out, err
	}
	var args struct {
		Account string `json:"account"`
		To      string `json:"to"`
	}
	if json.Unmarshal(r.Args, &args) != nil {
		return p, "", out, errors.New("select sending mailbox and recipient")
	}
	recipients, err := parseAddrs([]string{args.To})
	if err != nil || len(recipients) != 1 {
		return p, "", out, errors.New("invalid recipient")
	}
	boxes := s.opts.Mail.Mailboxes(u.Name)
	at := slices.IndexFunc(boxes, func(m tmail.MailboxView) bool { return m.ID == args.Account })
	if at < 0 {
		return p, "", out, errPersonalSource
	}
	src := r.Sources[0]
	ref := src.Ref
	v, obj, err := native.area.openEvent(native.area.store, u.Name, ref["collection"], ref["object"], ref["occurrence"])
	if err != nil || ref["etag"] != "" && ref["etag"] != obj.ETag {
		return p, "", out, errPersonalSource
	}
	cancel := r.Tool == "calendar.cancelInvite"
	prior := false
	if cancel {
		err := s.personalTxn(u, false, func(ac *personalAccount) error {
			for _, j := range ac.Jobs {
				if j.Request.Tool != "calendar.invite" || j.Status != "done" || j.Result == nil || len(j.Result.Sources) != 1 {
					continue
				}
				src := j.Result.Sources[0].Ref
				var sent struct{ Account, To string }
				if src["collection"] == ref["collection"] && src["object"] == ref["object"] && json.Unmarshal(j.Request.Args, &sent) == nil && sent.Account == args.Account && strings.EqualFold(sent.To, args.To) {
					prior = true
					break
				}
			}
			return nil
		})
		if err != nil {
			return p, "", out, err
		}
	}
	ics, err := inviteICS(v, obj.Data, boxes[at].Email, recipients[0].Addr, native.area.store.Zone(), cancel, prior)
	if err != nil {
		return p, "", out, err
	}
	src.Label = v.Title
	body := strings.Join([]string{v.Title, v.Start + " – " + v.End, v.Location}, "\n")
	method := "REQUEST"
	title := "Zum Termin einladen"
	subject := v.Title
	if cancel {
		method = "CANCEL"
		title = "Einladung absagen"
		subject = "Absage: " + v.Title
	}
	out = tmail.Outgoing{To: recipients, Subject: subject, Text: body, Files: []tmail.File{{Name: "termin.ics", Type: "text/calendar; method=" + method + "; charset=utf-8", Data: ics}}}
	detail := map[string]any{"method": method, "uid": v.UID, "account": boxes[at].ID, "from": boxes[at].Email, "to": addrLines(recipients), "title": v.Title, "start": v.Start, "end": v.End, "timezone": v.Timezone, "location": v.Location, "etag": obj.ETag, "attachments": []string{"termin.ics"}}
	p = pa.Preview{Title: title, Text: strings.Join([]string{boxes[at].Email + " → " + strings.Join(addrLines(recipients), ", "), body, "termin.ics"}, "\n"), Sources: []pa.Source{src}, Data: jsonPIM(detail)}
	return p, boxes[at].ID, out, nil
}
func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		p := personalPIM{s.pimDataArea(), store.Calendar, "calendar"}
		allowed := func(u auth.User) error {
			if err := p.available(u); err != nil {
				return err
			}
			return (personalMailTools{s.opts.Mail, s.personalInfer}).allowed(u)
		}
		template := personalTool{ID: "calendar.invite", EffectKey: func(_ auth.User, _ pa.Request, p pa.Preview) string { return personalDigest(p.Data) }, SourceKinds: []string{"calendar"}, Title: "Zum Termin einladen", App: "calendar", Sensitive: true, Available: allowed, Check: func(ctx context.Context, u auth.User, r pa.Request) error {
			if err := allowed(u); err != nil {
				return err
			}
			return p.check(ctx, u, r)
		},
			Prepare: func(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
				v, _, _, err := s.invitePlan(ctx, u, r)
				return v, err
			},
			Apply: func(ctx context.Context, u auth.User, r pa.Request, approved pa.Preview) (pa.Result, error) {
				fresh, id, msg, err := s.invitePlan(ctx, u, r)
				if err != nil {
					return pa.Result{}, err
				}
				if !bytes.Equal(fresh.Data, approved.Data) {
					return pa.Result{}, fmt.Errorf("%w: invitation changed since approval", pa.ErrNoEffect)
				}
				if err = ctx.Err(); err != nil {
					return pa.Result{}, err
				}
				receipt, err := s.opts.Mail.Send(ctx, u.Name, id, msg)
				noun := "Einladung"
				if r.Tool == "calendar.cancelInvite" {
					noun = "Absage"
				}
				return sentEvidence(receipt, err, fresh.Sources, noun)
			}}
		cancellation := template
		cancellation.ID = "calendar.cancelInvite"
		cancellation.Title = "Einladung absagen"
		return []personalTool{template, cancellation}
	})
}
