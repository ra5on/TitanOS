package api

// Reminders (VTODO) for the agent (1.13): tasks_list, tasks_create,
// tasks_update, tasks_complete and tasks_delete over pim/tasks.Service,
// the same functions as the web API (no HTTP in between). They belong to
// the area calendar (Kalender shows the reminders), with the rights
// task.create, task.update, task.complete and task.delete. Ids are
// task:<list>/<file>; another account's reminders are reached by no id.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/pim/tasks"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

const agentMaxTasks = 100

func (b *agentToolbox) tasksService() tasks.Service { return tasks.Service{Store: b.s.opts.PIM} }

func taskID(list, name string) string { return "task:" + list + "/" + name }

func parseTaskID(id string) (string, string, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(id), "task:")
	list, name, ok2 := strings.Cut(rest, "/")
	if !ok || !ok2 || !pimstore.ValidCollection(list) || name == "" || strings.Contains(name, "/") {
		return "", "", toolcatalog.Errorf("id %q is not a reminder id from tasks_list (task:<list>/<file>)", id)
	}
	return list, name, nil
}

func (b *agentToolbox) taskTools() []*agentTool {
	cal := agent.AreaCalendar
	id := str("Id from tasks_list, e.g. task:erinnerungen--titanos/abc.ics. Never make one up.")
	etag := str("Leave out; TitanOS fills it in.")
	fields := map[string]any{
		"title":    map[string]any{"type": "string", "maxLength": tasks.MaxTitle, "description": "Title."},
		"notes":    map[string]any{"type": "string", "maxLength": tasks.MaxNotes, "description": "Notes (\"\" clears them)."},
		"due":      map[string]any{"type": []string{"string", "null"}, "description": "Due: 2026-11-20 (a day) or 2026-11-20T09:00 (zone of the NAS); null removes it."},
		"priority": map[string]any{"type": "integer", "minimum": 0, "maximum": 9, "description": "1 high, 5 medium, 9 low, 0 none."},
		"list":     str("List id from tasks_list (default the first; on update: move there)."),
	}
	with := func(more map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range fields {
			out[k] = v
		}
		for k, v := range more {
			out[k] = v
		}
		return out
	}
	return []*agentTool{
		{name: "tasks_list", area: cal, kind: agent.KindRead, untrusted: true,
			description: "The person's reminders (Erinnerungen) with their lists: open ones by default, sorted by due date.",
			params: schema(map[string]any{"list": str("Only this list id."), "status": enum("open (default), done or all.", "open", "done", "all"),
				"dueBefore": str("Only due before this day YYYY-MM-DD (exclusive).")}),
			own: b.tasksList, prepare: b.pimCheckArgs},
		{name: "tasks_create", area: cal, kind: agent.KindChange, action: agent.ActTaskCreate,
			description: "Add a reminder (a to-do, not an event at a time).",
			params:      schema(fields, "title"), own: b.tasksCreate, prepare: b.tasksPrepare, draft: b.tasksDraft("tasks_create"), logged: func(json.RawMessage) string { return "" }},
		{name: "tasks_update", area: cal, kind: agent.KindChange, action: agent.ActTaskUpdate,
			description: "Change a reminder: only the fields given change.",
			params:      schema(with(map[string]any{"id": id, "etag": etag}), "id"), own: b.tasksUpdate, prepare: b.tasksPrepare, draft: b.tasksDraft("tasks_update"), logged: taskLogged},
		{name: "tasks_complete", area: cal, kind: agent.KindChange, action: agent.ActTaskComplete,
			description: "Tick a reminder off (done:false opens it again).",
			params:      schema(map[string]any{"id": id, "etag": etag, "done": map[string]any{"type": "boolean", "description": "Default true."}}, "id"),
			own:         b.tasksComplete, prepare: b.tasksPrepare, draft: b.tasksDraft("tasks_complete"), logged: taskLogged},
		{name: "tasks_delete", area: cal, kind: agent.KindChange, action: agent.ActTaskDelete,
			description: "Delete a reminder, only when asked to. Always asks the person.",
			params:      schema(map[string]any{"id": id, "etag": etag}, "id"), own: b.tasksDelete, prepare: b.tasksPrepare, logged: taskLogged},
	}
}

func taskLogged(args json.RawMessage) string { return argString(args, "id") }

func (b *agentToolbox) tasksReady() error {
	if !b.pimMounted(pimstore.Tasks) {
		return toolcatalog.Errorf("the reminders are on a data volume that is not mounted right now; nothing was changed")
	}
	return nil
}

// tasksList lists the lists and the reminders.
func (b *agentToolbox) tasksList(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var a struct{ List, Status, DueBefore string }
	if err := json.Unmarshal(args, &a); err != nil {
		return agent.Outcome{}, toolcatalog.Errorf("invalid arguments")
	}
	out := map[string]any{"lists": []tasks.List{}, "reminders": []map[string]any{}}
	if !b.pimMounted(pimstore.Tasks) {
		out["hidden"], out["why"] = true, "the reminders are on a data volume that is not mounted right now; this is no sign that there are none"
		return pimJSON(out)
	}
	svc := b.tasksService()
	lists, err := svc.Lists(b.pimAccount(), "de")
	if err != nil {
		return agent.Outcome{}, pimError(err, pimstore.Calendar)
	}
	f := tasks.Filter{List: a.List, Status: a.Status}
	if a.DueBefore != "" {
		t, err := time.ParseInLocation(time.DateOnly, a.DueBefore, b.s.opts.PIM.Zone())
		if err != nil {
			return agent.Outcome{}, toolcatalog.Errorf("dueBefore is a day YYYY-MM-DD")
		}
		f.DueBefore = t
	}
	items, err := svc.Tasks(b.pimAccount(), f)
	if err != nil {
		return agent.Outcome{}, taskError(err)
	}
	rows := []map[string]any{}
	for i, it := range items {
		if i >= agentMaxTasks {
			out["truncated"] = true
			break
		}
		rows = append(rows, map[string]any{"id": taskID(it.List, it.ID), "list": it.List, "title": it.Title, "due": it.Due, "done": it.Done, "priority": it.Priority, "recurring": it.Recurring})
	}
	out["lists"], out["reminders"] = lists, rows
	return pimJSON(out)
}

func taskError(err error) error {
	switch {
	case errors.Is(err, tasks.ErrInvalid):
		return toolcatalog.Errorf("%v", err)
	case pimGone(err):
		return toolcatalog.Errorf("the reminders are on a data volume that is not mounted right now; nothing was changed")
	case errors.Is(err, pimstore.ErrNotFound), errors.Is(err, pimstore.ErrInvalidName), errors.Is(err, pimstore.ErrInvalidCollection):
		return toolcatalog.Errorf("there is no such reminder or list; take the id from tasks_list")
	case errors.Is(err, pimstore.ErrPrecondition):
		return toolcatalog.Errorf("the reminder was changed meanwhile; read it again and ask the person once more")
	}
	return pimError(err, pimstore.Calendar)
}

// tasksPrepare checks a call, reads the reminder it changes (its etag
// goes into the arguments, the card names its title) and writes the
// summary.
func (b *agentToolbox) tasksPrepare(_ context.Context, chat *agent.Chat, name string, args json.RawMessage, p *agent.Prepared) (string, error) {
	if err := toolcatalog.ValidateArguments(b.tools[name].params, args); err != nil {
		return "", err
	}
	if err := b.tasksReady(); err != nil {
		return "", err
	}
	en := pimEnglish(chat)
	if name == "tasks_create" {
		p.Summary = pick(en, "Erinnerung anlegen: ", "Add a reminder: ") + argString(args, "title")
		if due := argString(args, "due"); due != "" {
			p.Summary += pick(en, ", fällig ", ", due ") + due
		}
		return "", nil
	}
	list, file, err := parseTaskID(argString(args, "id"))
	if err != nil {
		return "", err
	}
	task, err := b.tasksService().Get(b.pimAccount(), list, file)
	if err != nil {
		return "", taskError(err)
	}
	withETag(p, task.ETag)
	switch name {
	case "tasks_update":
		p.Summary = pick(en, "Erinnerung ändern: ", "Change a reminder: ") + task.Title
	case "tasks_complete":
		if v, ok := rawFields(args)["done"].(bool); ok && !v {
			p.Summary = pick(en, "Erinnerung wieder öffnen: ", "Open a reminder again: ") + task.Title
		} else {
			p.Summary = pick(en, "Erinnerung abhaken: ", "Tick off a reminder: ") + task.Title
		}
	case "tasks_delete":
		p.Summary = pick(en, "Erinnerung löschen: ", "Delete a reminder: ") + task.Title
		return agent.ModeAsk, nil
	}
	return "", nil
}

func taskInput(args json.RawMessage) (tasks.Input, error) {
	var in tasks.Input
	if err := json.Unmarshal(args, &in); err != nil {
		return in, toolcatalog.Errorf("invalid arguments")
	}
	in.Lang = "de"
	return in, nil
}

func (b *agentToolbox) tasksCreate(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	if err := b.tasksReady(); err != nil {
		return agent.Outcome{}, err
	}
	in, err := taskInput(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	t, err := b.tasksService().Create(b.pimAccount(), in)
	if err != nil {
		return agent.Outcome{}, taskError(err)
	}
	return pimJSON(map[string]any{"id": taskID(t.List, t.ID), "title": t.Title, "due": t.Due})
}

func (b *agentToolbox) tasksUpdate(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	list, file, err := parseTaskID(argString(args, "id"))
	if err != nil {
		return agent.Outcome{}, err
	}
	in, err := taskInput(args)
	if err != nil {
		return agent.Outcome{}, err
	}
	if in.List == "" {
		in.List = list
	}
	t, changed, err := b.tasksService().Update(b.pimAccount(), list, file, argString(args, "etag"), in)
	if err != nil {
		return agent.Outcome{}, taskError(err)
	}
	return pimJSON(map[string]any{"id": taskID(t.List, t.ID), "title": t.Title, "due": t.Due, "changed": changed})
}

func (b *agentToolbox) tasksComplete(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	list, file, err := parseTaskID(argString(args, "id"))
	if err != nil {
		return agent.Outcome{}, err
	}
	done := true
	if v, ok := rawFields(args)["done"].(bool); ok {
		done = v
	}
	d, err := b.tasksService().SetDone(b.pimAccount(), list, file, argString(args, "etag"), done)
	if err != nil {
		return agent.Outcome{}, taskError(err)
	}
	return pimJSON(map[string]any{"id": taskID(d.Task.List, d.Task.ID), "title": d.Task.Title, "done": d.Task.Done, "next": d.Copy != nil})
}

func (b *agentToolbox) tasksDelete(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	list, file, err := parseTaskID(argString(args, "id"))
	if err != nil {
		return agent.Outcome{}, err
	}
	if err := b.tasksService().Delete(b.pimAccount(), list, file, argString(args, "etag")); err != nil {
		return agent.Outcome{}, taskError(err)
	}
	return pimJSON(map[string]any{"deleted": true})
}

// tasksDraft is the reminder for the list of Kalender: title, notes, due,
// priority, list; for a change or ticking off also the list and id.
func (b *agentToolbox) tasksDraft(tool string) func(context.Context, *agent.Chat, json.RawMessage) (agent.Outcome, error) {
	return func(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
		if err := toolcatalog.ValidateArguments(b.tools[tool].params, args); err != nil {
			return agent.Outcome{}, err
		}
		fields := rawFields(args)
		kind := "task"
		if tool != "tasks_create" {
			list, file, err := parseTaskID(argString(args, "id"))
			if err != nil {
				return agent.Outcome{}, err
			}
			task, err := b.tasksService().Get(b.pimAccount(), list, file)
			if err != nil {
				return agent.Outcome{}, taskError(err)
			}
			fields["list"], fields["id"] = list, file
			if _, ok := fields["title"]; !ok {
				fields["title"] = task.Title
			}
			if tool == "tasks_complete" {
				kind = "complete"
				if _, ok := fields["done"]; !ok {
					fields["done"] = true
				}
			}
		}
		return draftOutcome(&agent.Draft{App: "tasks", Kind: kind, Fields: fields}, "reminder")
	}
}
