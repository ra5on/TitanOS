package tasks

import (
	"cmp"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Service is the reminders of the accounts over pim/store: the one place
// the web API and the agent's tools go through. Every write goes through
// store.Put/DeleteObject/…, so it moves ctag and sync token (the iPhone
// sees it on its next sync) and reaches the store's change signal like a
// change of an event. Account names are those of the caller (the account
// signed in, or the agent's); nothing here checks rights.
type Service struct {
	Store *store.Store
	// Now is the clock (DTSTAMP, COMPLETED); nil = time.Now.
	Now func() time.Time
}

// ErrNoStore: no store (titand -dev without PIM), answered like no volume.
var ErrNoStore = store.ErrUnavailable

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) ready() error {
	if s.Store == nil {
		return ErrNoStore
	}
	return nil
}

func (s Service) local() *time.Location { return s.Store.Zone() }

func defaultName(lang string) string {
	if n, ok := DefaultListNames[lang]; ok {
		return n
	}
	return store.DefaultTaskListName
}

// Lists are the reminder lists of an account by order: the own ones, or
// the virtual default list (named in lang) until there is one.
func (s Service) Lists(account, lang string) ([]List, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	cols, err := s.Store.Collections(store.Tasks, account)
	if err != nil {
		return nil, err
	}
	out := make([]List, 0, len(cols))
	for _, c := range cols {
		l := List{ID: c.ID, Name: c.Name, Color: c.Color, Order: c.Order, Virtual: c.Virtual}
		if l.Color == "" {
			l.Color = store.DefaultTaskColor
		}
		if c.Virtual {
			l.Name = defaultName(lang)
		}
		objs, err := s.Store.Objects(store.Tasks, account, c.ID)
		if err != nil {
			return nil, err
		}
		l.Count = len(objs)
		for _, o := range objs {
			if !o.Task.Done() && !o.Task.Cancelled() {
				l.Open++
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// CreateList makes a list; its id comes from the name like a calendar's
// (calweb.CalendarID) and is free among calendars and lists. The first own
// list makes the virtual default real as well (store.CreateCollection).
func (s Service) CreateList(account string, in ListInput) (List, error) {
	if err := s.ready(); err != nil {
		return List{}, err
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return List{}, invalid("a list needs a name")
	}
	name := strings.TrimSpace(*in.Name)
	meta := store.Meta{Name: name, Color: store.DefaultTaskColor}
	if in.Color != nil && *in.Color != "" {
		meta.Color = *in.Color
	}
	var made store.Collection
	var err error
	for range 3 { // a list or calendar of the same name meanwhile: the next id
		var cols []store.Collection
		if cols, err = s.Store.Collections(store.Tasks, account); err != nil {
			return List{}, err
		}
		meta.Order = 0
		for _, c := range cols {
			meta.Order = max(meta.Order, c.Order+1)
		}
		if in.Order != nil {
			meta.Order = *in.Order
		}
		id := calweb.CalendarID(name, func(id string) bool {
			return s.Store.Holds(store.Tasks, account, id) || s.Store.Holds(store.Calendar, account, id)
		})
		if made, err = s.Store.CreateCollection(store.Tasks, account, id, meta); !errors.Is(err, store.ErrExists) {
			break
		}
	}
	if err != nil {
		return List{}, err
	}
	return List{ID: made.ID, Name: made.Name, Color: made.Color, Order: made.Order}, nil
}

// UpdateList changes name, color or order of a list.
func (s Service) UpdateList(account, id string, in ListInput) (List, error) {
	if err := s.ready(); err != nil {
		return List{}, err
	}
	patch := store.MetaPatch{Color: in.Color, Order: in.Order}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return List{}, invalid("a list needs a name")
		}
		patch.Name = &name
	}
	c, err := s.Store.UpdateCollection(store.Tasks, account, id, patch)
	if err != nil {
		return List{}, err
	}
	return List{ID: c.ID, Name: c.Name, Color: c.Color, Order: c.Order, Count: c.Count}, nil
}

// DeleteList deletes a list with its reminders; the last one stays
// (store.ErrLastCollection).
func (s Service) DeleteList(account, id string) error {
	if err := s.ready(); err != nil {
		return err
	}
	return s.Store.DeleteCollection(store.Tasks, account, id, "")
}

// Tasks lists reminders from the index (no disk): open ones by due (none
// last), priority (none last) and title; done ones by completion, newest
// first.
func (s Service) Tasks(account string, f Filter) ([]Item, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var lists []string
	if f.List != "" {
		lists = []string{f.List}
	} else {
		cols, err := s.Store.Collections(store.Tasks, account)
		if err != nil {
			return nil, err
		}
		for _, c := range cols {
			lists = append(lists, c.ID)
		}
	}
	switch f.Status {
	case "", "open", "done", "all":
	default:
		return nil, invalid("status %q (open, done, all)", f.Status)
	}
	loc := s.local()
	out := []Item{}
	dueAt := map[string]time.Time{}
	for _, id := range lists {
		objs, err := s.Store.Objects(store.Tasks, account, id)
		if err != nil {
			return nil, err
		}
		for _, o := range objs {
			done := o.Task.Done() || o.Task.Cancelled()
			switch f.Status {
			case "", "open":
				if done {
					continue
				}
			case "done":
				if !done {
					continue
				}
			}
			at := o.Task.DueAt(loc)
			if !f.DueBefore.IsZero() && (at.IsZero() || !at.Before(f.DueBefore)) {
				continue
			}
			it := itemOf(id, o, loc)
			dueAt[id+"/"+o.Name] = at
			out = append(out, it)
		}
	}
	slices.SortStableFunc(out, func(a, b Item) int {
		if a.Done != b.Done {
			if a.Done {
				return 1
			}
			return -1
		}
		if a.Done {
			return -cmp.Compare(a.Completed, b.Completed)
		}
		da, db := dueAt[a.List+"/"+a.ID], dueAt[b.List+"/"+b.ID]
		switch {
		case da.IsZero() != db.IsZero():
			if da.IsZero() {
				return 1
			}
			return -1
		case !da.Equal(db):
			return da.Compare(db)
		}
		pa, pb := a.Priority, b.Priority
		if pa == 0 {
			pa = 10
		}
		if pb == 0 {
			pb = 10
		}
		if pa != pb {
			return pa - pb
		}
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	return out, nil
}

// Due are the open reminders of all lists with DUE in [from, to) (the
// calendar shows them on their day): at most 400 days. A series shows its
// current occurrence only (the one iOS shows; the next one comes once it
// is ticked off).
func (s Service) Due(account string, from, to time.Time) ([]Item, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if !to.After(from) || to.After(from.AddDate(0, 0, recur.MaxWindowDays)) {
		return nil, recur.ErrWindow
	}
	all, err := s.Tasks(account, Filter{DueBefore: to})
	if err != nil {
		return nil, err
	}
	loc := s.local()
	out := all[:0]
	for _, it := range all {
		at, err := dueInstant(it, loc)
		if err == nil && !at.Before(from) {
			out = append(out, it)
		}
	}
	return out, nil
}

func dueInstant(it Item, loc *time.Location) (time.Time, error) {
	if it.DueAllDay {
		return time.ParseInLocation(time.DateOnly, it.Due, loc)
	}
	return time.Parse(time.RFC3339, it.Due)
}

// Get reads one reminder.
func (s Service) Get(account, list, id string) (Task, error) {
	if err := s.ready(); err != nil {
		return Task{}, err
	}
	obj, err := s.Store.Get(store.Tasks, account, list, id)
	if err != nil {
		return Task{}, err
	}
	return Open(list, id, obj.ETag, obj.Data, s.local())
}

// target is the list of a new reminder: the one named, else the first.
// A write into the virtual default list makes it real with its name in
// in.Lang.
func (s Service) target(account string, in Input) (string, error) {
	list := in.List
	cols, err := s.Store.Collections(store.Tasks, account)
	if err != nil {
		return "", err
	}
	if list == "" && len(cols) > 0 {
		list = cols[0].ID
	}
	for _, c := range cols {
		if c.ID == list && c.Virtual && in.Lang != "" && defaultName(in.Lang) != c.Name {
			_, err := s.Store.CreateCollection(store.Tasks, account, c.ID, store.Meta{Name: defaultName(in.Lang), Color: store.DefaultTaskColor})
			if err != nil && !errors.Is(err, store.ErrExists) {
				return "", err
			}
		}
	}
	return list, nil
}

// Create writes a new reminder (file <UID>.ics, a new UID).
func (s Service) Create(account string, in Input) (Task, error) {
	if err := s.ready(); err != nil {
		return Task{}, err
	}
	uid := newUID()
	data, err := Build(in, uid, s.now(), s.local())
	if err != nil {
		return Task{}, err
	}
	list, err := s.target(account, in)
	if err != nil {
		return Task{}, err
	}
	name := uid + ".ics"
	res, err := s.Store.Put(store.Tasks, account, list, name, data, store.PutOptions{IfNoneMatch: true})
	if err != nil {
		return Task{}, err
	}
	return Open(list, name, res.ETag, data, s.local())
}

// Update changes a reminder read with etag (store.ErrPrecondition when it
// changed meanwhile); a list other than its own in in.List moves it
// there (a new file there, then the old one deleted). An input that
// changes nothing writes nothing (the ETag stays; changed false).
func (s Service) Update(account, list, id, etag string, in Input) (Task, bool, error) {
	if err := s.ready(); err != nil {
		return Task{}, false, err
	}
	if etag == "" {
		return Task{}, false, invalid("etag missing")
	}
	obj, err := s.Store.Get(store.Tasks, account, list, id)
	if err != nil {
		return Task{}, false, err
	}
	if unquote(etag) != obj.ETag {
		return Task{}, false, store.ErrPrecondition
	}
	data, changed, err := Edit(obj.Data, in, s.now(), s.local())
	if err != nil {
		return Task{}, false, err
	}
	if in.List != "" && in.List != list {
		res, err := s.Store.Put(store.Tasks, account, in.List, id, data, store.PutOptions{IfNoneMatch: true})
		if err != nil {
			return Task{}, false, err
		}
		if err := s.Store.DeleteObject(store.Tasks, account, list, id, obj.ETag); err != nil {
			// Not moved: the copy goes again, the reminder stays where it was.
			if rerr := s.Store.DeleteObject(store.Tasks, account, in.List, id, res.ETag); rerr != nil {
				slog.Warn("Reminders: copy of a reminder not moved stays in the target list", "account", account, "err", rerr)
			}
			return Task{}, false, err
		}
		t, err := Open(in.List, id, res.ETag, data, s.local())
		return t, true, err
	}
	if !changed {
		t, err := Open(list, id, obj.ETag, obj.Data, s.local())
		return t, false, err
	}
	res, err := s.Store.Put(store.Tasks, account, list, id, data, store.PutOptions{IfMatch: obj.ETag})
	if err != nil {
		return Task{}, false, err
	}
	t, err := Open(list, id, res.ETag, data, s.local())
	return t, true, err
}

// SetDone ticks a reminder read with etag off (done) or opens it again
// (SetDone on the bytes: a series moves to its next occurrence and keeps
// the done one as a copy in the same list).
func (s Service) SetDone(account, list, id, etag string, done bool) (Done, error) {
	if err := s.ready(); err != nil {
		return Done{}, err
	}
	if etag == "" {
		return Done{}, invalid("etag missing")
	}
	obj, err := s.Store.Get(store.Tasks, account, list, id)
	if err != nil {
		return Done{}, err
	}
	if unquote(etag) != obj.ETag {
		return Done{}, store.ErrPrecondition
	}
	c, err := SetDone(obj.Data, done, s.now(), s.local())
	if err != nil {
		return Done{}, err
	}
	loc := s.local()
	if c.Data == nil {
		info, err := s.Store.Stat(store.Tasks, account, list, id)
		if err != nil {
			return Done{}, err
		}
		return Done{Task: itemOf(list, info, loc)}, nil
	}
	if _, err := s.Store.Put(store.Tasks, account, list, id, c.Data, store.PutOptions{IfMatch: obj.ETag}); err != nil {
		return Done{}, err
	}
	info, err := s.Store.Stat(store.Tasks, account, list, id)
	if err != nil {
		return Done{}, err
	}
	out := Done{Task: itemOf(list, info, loc)}
	if c.Copy != nil {
		name := c.CopyUID + ".ics"
		if _, err := s.Store.Put(store.Tasks, account, list, name, c.Copy, store.PutOptions{IfNoneMatch: true}); err != nil {
			// The series moved on; only the done copy is missing.
			slog.Warn("Reminders: done copy of a series not written", "account", account, "list", list, "err", err)
		} else if ci, err := s.Store.Stat(store.Tasks, account, list, name); err == nil {
			it := itemOf(list, ci, loc)
			out.Copy = &it
		}
	}
	return out, nil
}

// Delete deletes a reminder read with etag (its sub-tasks stay).
func (s Service) Delete(account, list, id, etag string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if etag == "" {
		return invalid("etag missing")
	}
	return s.Store.DeleteObject(store.Tasks, account, list, id, etag)
}

// unquote takes an entity tag with or without its quotes.
func unquote(tag string) string {
	tag = strings.TrimSpace(tag)
	if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		return tag[1 : len(tag)-1]
	}
	return tag
}
