package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/cardweb"
	store "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/pim/tasks"
)

type personalPIM struct {
	area pimDataArea
	kind store.Kind
	app  string
}

func (p personalPIM) available(_ auth.User) error {
	st := p.area.store
	if st == nil || st.Current != nil && st.Current(string(p.kind)) == "" {
		return errors.New("app data volume unavailable")
	}
	return nil
}
func (p personalPIM) check(_ context.Context, u auth.User, r pa.Request) error {
	if err := p.available(u); err != nil {
		return err
	}
	if strings.HasSuffix(r.Tool, ".create") {
		if len(r.Sources) != 0 {
			return errors.New("create requires explicit fields and a target collection")
		}
		return nil
	}
	if len(r.Sources) != 1 || r.Sources[0].Kind != p.app {
		return errPersonalSource
	}
	ref := r.Sources[0].Ref
	if !store.ValidCollection(ref["collection"]) || !store.ValidName(p.kind, ref["object"]) {
		return errPersonalSource
	}
	return nil
}

func (p personalPIM) read(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
	if err := p.check(ctx, u, r); err != nil {
		return pa.Preview{}, err
	}
	src := r.Sources[0]
	ref := src.Ref
	col, id := ref["collection"], ref["object"]
	var object any
	var title, text, etag string
	var err error
	switch p.kind {
	case store.Calendar:
		var e calweb.View
		e, _, err = p.area.openEvent(p.area.store, u.Name, col, id, ref["occurrence"])
		title, etag = e.Title, e.ETag
		text = strings.Join([]string{e.Title, e.Start + " – " + e.End, e.Location}, "\n")
		object = e
	case store.Contacts:
		var c cardweb.Contact
		c, _, err = p.area.openContact(p.area.store, u.Name, col, id)
		title, etag = c.Name, c.ETag
		lines := []string{c.Name, c.Org}
		for _, v := range c.Emails {
			lines = append(lines, v.Value)
		}
		for _, v := range c.Phones {
			lines = append(lines, v.Value)
		}
		text = strings.Join(lines, "\n")
		object = c
	case store.Tasks:
		var t tasks.Task
		t, err = (tasks.Service{Store: p.area.store}).Get(u.Name, col, id)
		title, etag = t.Title, t.ETag
		text = strings.TrimSpace(t.Title + " " + t.Due)
		object = t
	}
	if err != nil || ref["etag"] != "" && ref["etag"] != etag {
		return pa.Preview{}, errPersonalSource
	}
	src.Label = title
	return pa.Preview{Title: title, Text: text, Data: jsonPIM(object), Sources: []pa.Source{src}}, nil
}

type pimCreateFields struct {
	Collection string `json:"collection"`
	Title      string `json:"title"`
	Start      string `json:"start,omitempty"`
	End        string `json:"end,omitempty"`
	AllDay     bool   `json:"allDay,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
	Location   string `json:"location,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Due        string `json:"due,omitempty"`
	Email      string `json:"email,omitempty"`
}

func pimWriteUID(user auth.User, r pa.Request) string {
	return fmt.Sprintf("agent-%x", sha256.Sum256(jsonPIM([]string{user.Name, user.Created.String(), r.RequestID, r.Tool})))
}
func (p personalPIM) createPlan(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, string, []byte, error) {
	var in pimCreateFields
	if err := p.check(ctx, u, r); err != nil {
		return pa.Preview{}, "", nil, err
	}
	if json.Unmarshal(r.Args, &in) != nil || strings.TrimSpace(in.Title) == "" || len(in.Title) > 512 || len(in.Notes) > 4096 || !store.ValidCollection(in.Collection) {
		return pa.Preview{}, "", nil, errors.New("invalid fields or collection")
	}
	col, err := p.area.store.GetCollection(p.kind, u.Name, in.Collection)
	if err != nil {
		return pa.Preview{}, "", nil, errPersonalSource
	}
	uid := pimWriteUID(u, r)
	// The native writers need a timestamp. The final file uses the real apply
	// time. Only user-visible fields enter the approval comparison.
	stamp := p.area.now()
	var data []byte
	name := uid + ".ics"
	lines := []string{col.Name, in.Title}
	switch p.kind {
	case store.Calendar:
		data, err = calweb.Build(calweb.Input{Title: &in.Title, Start: in.Start, End: in.End, AllDay: in.AllDay, Timezone: in.Timezone, Location: &in.Location, Notes: &in.Notes}, uid, stamp, p.area.store.Zone())
		lines = append(lines, in.Start+" – "+in.End, in.Timezone, in.Location, in.Notes)
	case store.Contacts:
		name = uid + ".vcf"
		raw := map[string]any{"first": in.Title, "notes": in.Notes}
		if in.Email != "" {
			raw["emails"] = []cardweb.Value{{Type: "other", Value: in.Email}}
		}
		var card cardweb.Input
		err = json.Unmarshal(jsonPIM(raw), &card)
		if err == nil {
			data, err = cardweb.Build(card, uid, stamp)
		}
		lines = append(lines, in.Email, in.Notes)
	case store.Tasks:
		task := tasks.Input{List: in.Collection, Title: &in.Title, Notes: &in.Notes, Timezone: in.Timezone}
		if in.Due != "" {
			task.Due = tasks.Opt[string]{Set: true, Value: &in.Due}
		}
		data, err = tasks.Build(task, uid, stamp, p.area.store.Zone())
		lines = append(lines, in.Due, in.Timezone, in.Notes)
	}
	if err != nil {
		return pa.Preview{}, "", nil, errors.New("invalid fields for this app")
	}
	return pa.Preview{Title: in.Title, Text: strings.TrimSpace(strings.Join(lines, "\n")), Data: jsonPIM(in)}, name, data, nil
}
func (p personalPIM) write(ctx context.Context, u auth.User, r pa.Request, approved pa.Preview) (pa.Result, error) {
	fresh, id, data, err := p.createPlan(ctx, u, r)
	if err != nil {
		return pa.Result{}, err
	}
	if !bytes.Equal(fresh.Data, approved.Data) || fresh.Text != approved.Text {
		return pa.Result{}, errors.New("object changed since approval")
	}
	var in pimCreateFields
	if err = json.Unmarshal(fresh.Data, &in); err != nil {
		return pa.Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return pa.Result{}, err
	}
	written, err := p.area.store.Put(p.kind, u.Name, in.Collection, id, data, store.PutOptions{IfNoneMatch: true})
	if err != nil {
		return pa.Result{}, errors.New("object creation could not be verified")
	}
	current, err := p.area.store.Get(p.kind, u.Name, in.Collection, id)
	if err != nil || current.ETag != written.ETag {
		return pa.Result{}, errors.New("written object could not be verified")
	}
	link := "/calendar"
	if p.kind == store.Calendar {
		link = "/calendar/event?" + url.Values{"cal": {in.Collection}, "id": {id}}.Encode()
	}
	if p.kind == store.Contacts {
		link = "/contacts?" + url.Values{"book": {in.Collection}, "id": {id}}.Encode()
	}
	if p.kind == store.Tasks {
		link = "/calendar?" + url.Values{"task": {id}, "list": {in.Collection}}.Encode()
	}
	return pa.Result{Title: in.Title, Text: fresh.Text, Link: link, Data: jsonPIM(map[string]string{"id": id, "collection": in.Collection, "etag": written.ETag})}, nil
}
func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		var tools []personalTool
		for _, d := range []struct {
			kind              store.Kind
			app, read, create string
		}{{store.Calendar, "calendar", "Termin ansehen", "Termin anlegen"}, {store.Contacts, "contacts", "Kontakt ansehen", "Kontakt anlegen"}, {store.Tasks, "reminders", "Erinnerung ansehen", "Erinnerung anlegen"}} {
			p := personalPIM{s.pimDataArea(), d.kind, d.app}
			tools = append(tools, personalTool{ID: d.app + ".read", SourceKinds: []string{d.app}, Title: d.read, App: d.app, Default: pa.Ask, Available: p.available, Check: p.check, Prepare: p.read},
				personalTool{ID: d.app + ".create", Title: d.create, App: d.app, Default: pa.Ask, Available: p.available, Check: p.check, Prepare: func(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
					preview, _, _, err := p.createPlan(ctx, u, r)
					return preview, err
				}, Apply: p.write})
		}
		return tools
	})
}
