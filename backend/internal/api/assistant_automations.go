package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
)

func (s *Server) validateAutomation(u auth.User, a *assistant.Automation) error {
	if err := a.Validate(time.Now()); err != nil {
		return err
	}
	if (a.Conditions.Volume != "" || a.Conditions.NoRebuild) && s.opts.Assistant.Settings().Access(assistant.AreaStorage) == assistant.AccessOff {
		return assistant.ErrOff
	}
	if a.Conditions.Volume != "" || a.Conditions.NoRebuild || a.Trigger.Kind == "event" && a.Trigger.Event != "files" {
		if !u.Role.CanAdminister() {
			return assistant.ErrInvalid
		}
	}
	if a.Trigger.Kind == "event" {
		area := map[string]string{"notification": "system", "usb": "files", "volume": "storage", "files": "files"}[a.Trigger.Event]
		if s.opts.Assistant.Settings().Access(area) == assistant.AccessOff {
			return assistant.ErrOff
		}
		if a.Trigger.Event == "files" {
			root, err := s.assistantRoot(u, a.Trigger.Root)
			if err != nil {
				return err
			}
			if hiddenPath(a.Trigger.Path) || strings.HasPrefix(a.Trigger.Path, "/") || slices.Contains(strings.Split(a.Trigger.Path, "/"), "..") {
				return assistant.ErrInvalid
			}
			if _, err = s.opts.Files.List(root, a.Trigger.Path); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Server) handleAutomations(w http.ResponseWriter, r *http.Request) {
	list, err := s.automations.List(currentUser(r).Name)
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"automations": list})
}
func (s *Server) handleAutomationSave(w http.ResponseWriter, r *http.Request) {
	var a assistant.Automation
	if !readJSON(w, r, &a) {
		return
	}
	if err := s.validateAutomation(currentUser(r), &a); err != nil {
		writeAssistantError(w, err)
		return
	}
	saved, err := s.automations.Put(currentUser(r).Name, a, time.Now())
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
func (s *Server) handleAutomationDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.automations.Delete(currentUser(r).Name, r.PathValue("automation")); err != nil {
		writeAssistantError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleAutomationRun(w http.ResponseWriter, r *http.Request) {
	err := s.automations.Update(currentUser(r).Name, func(list *[]assistant.Automation) error {
		for i := range *list {
			a := &(*list)[i]
			if a.ID == r.PathValue("automation") {
				a.Pending = true
				return nil
			}
		}
		return assistant.ErrChatNotFound
	})
	if err != nil {
		writeAssistantError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) automationTools(u auth.User) []assistant.Tool {
	definition := schema(map[string]any{
		"id": str("Existing ID for editing; omit to create."), "revision": map[string]any{"type": "integer"}, "name": str("Name"), "prompt": str("Complete user-authorized instruction. Use existing exact targets; do not create unrelated resources."), "enabled": map[string]any{"type": "boolean"},
		"execution": enum("Execution policy", "inherit", "confirm", "direct"), "notify": enum("Administrator notifications (private results remain in chat)", "errors", "always", "never"), "catchUp": enum("Missed schedule: latest within 24h, or skip", "latest", "skip"), "overlap": enum("While previous run is open", "skip", "queue"),
		"conditions": schema(map[string]any{"volume": str("Required mounted volume name"), "noRebuild": map[string]any{"type": "boolean"}}),
		"trigger":    schema(map[string]any{"kind": enum("Trigger type", "once", "interval", "daily", "weekly", "event"), "at": str("RFC3339 date for once"), "minutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 525600}, "time": str("HH:MM for daily/weekly"), "timezone": str("IANA timezone, e.g. Europe/Berlin"), "weekday": map[string]any{"type": "integer", "minimum": 0, "maximum": 6, "description": "Sunday=0"}, "event": enum("Event type", "notification", "usb", "volume", "files"), "target": str("Notification kind, USB filesystem UUID or volume name"), "root": str("Exact accessible root for folder changes"), "path": str("Relative folder (non-recursive)"), "percent": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, "kind"),
	}, "name", "prompt", "enabled", "trigger")
	return []assistant.Tool{
		reading("automation_list", "List this account's saved automations, next starts and run history. No access to other accounts.", nil, false, noArgs(func(ctx context.Context) (any, error) { return s.automations.List(u.Name) })),
		actionTool("automation_save", "Create or edit a persistent automation ONLY when the person requests recurring/scheduled/event work. Read automation_list before editing and preserve its revision. Pausing sets enabled=false; duplication omits id and revision.", map[string]any{"automation": definition}, []string{"automation"}, func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
			var q struct{ Automation assistant.Automation }
			if err := assistant.DecodeArgs(args, &q); err != nil {
				return nil, err
			}
			if err := s.validateAutomation(u, &q.Automation); err != nil {
				return nil, err
			}
			if q.Automation.ID != "" {
				list, err := s.automations.List(u.Name)
				if err != nil {
					return nil, err
				}
				found := false
				for _, a := range list {
					if a.ID == q.Automation.ID && a.Revision == q.Automation.Revision {
						found = true
					}
				}
				if !found {
					return nil, assistant.ErrNoAction
				}
			}
			return card("api.change", map[string]string{"summary": q.Automation.Name, "preview": string(args)}, "POST", "/assistant/automations", q.Automation), nil
		}),
		actionTool("automation_delete", "Delete an automation requested by the person. Running chats are preserved.", map[string]any{"id": str("ID from automation_list")}, []string{"id"}, func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
			return s.automationCard(u, args, "DELETE", "")
		}),
		actionTool("automation_run", "Request one run now. Uses the saved definition and conditions; no duplicate overlapping run.", map[string]any{"id": str("ID from automation_list")}, []string{"id"}, func(ctx context.Context, args json.RawMessage) (*assistant.Action, error) {
			return s.automationCard(u, args, "POST", "/run")
		}),
	}
}
func (s *Server) automationCard(u auth.User, args json.RawMessage, method, suffix string) (*assistant.Action, error) {
	var q struct{ ID string }
	if err := assistant.DecodeArgs(args, &q); err != nil {
		return nil, err
	}
	list, err := s.automations.List(u.Name)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.ID == q.ID {
			return card("api.change", map[string]string{"summary": a.Name}, method, "/assistant/automations/"+a.ID+suffix, nil), nil
		}
	}
	return nil, assistant.ErrNoAction
}

// Snapshot event sources without an LLM. No recursive indexing, /config polling
// or event instructions from files. Initial observations establish a baseline.
func (s *Server) automationEvent(ctx context.Context, u auth.User, t assistant.Trigger) (string, bool, error) {
	switch t.Event {
	case "notification":
		if s.opts.Notify == nil {
			return "", false, nil
		}
		// Messages returns newest first. Fixed-width timestamps/IDs allow a
		// high-water mark so dismissing a message cannot replay an older one.
		for _, m := range s.opts.Notify.Messages() {
			if m.Kind == t.Target {
				return fmt.Sprintf("%s/%020d", m.Time.UTC().Format("2006-01-02T15:04:05.000000000Z"), m.ID), true, nil
			}
		}
		return "", false, nil
	case "usb":
		if s.opts.USB == nil {
			return "", false, nil
		}
		list, err := s.opts.USB.List(ctx)
		if err != nil {
			return "", false, err
		}
		for _, d := range list {
			if d.ID == t.Target {
				return "present", true, nil
			}
		}
		return "absent", false, nil
	case "volume":
		status, err := s.opts.Storage.Status(ctx)
		if err != nil {
			return "", false, err
		}
		for _, v := range status.Volumes {
			if v.Name == t.Target && v.Mounted && v.SizeBytes > 0 {
				above := float64(v.UsedBytes)/float64(v.SizeBytes)*100 >= float64(t.Percent)
				return fmt.Sprint(above), above, nil
			}
		}
		return "", false, fmt.Errorf("volume unavailable")
	case "files":
		root, err := s.assistantRoot(u, t.Root)
		if err != nil {
			return "", false, err
		}
		entries, err := s.opts.Files.List(root, t.Path)
		if err != nil {
			return "", false, err
		}
		entries = slices.DeleteFunc(entries, func(e files.Entry) bool { return hiddenPath(e.Name) })
		if len(entries) > 5000 {
			return "", false, fmt.Errorf("folder exceeds 5000 entries; choose a narrower folder")
		}
		slices.SortFunc(entries, func(a, b files.Entry) int { return strings.Compare(a.Name, b.Name) })
		b, _ := json.Marshal(entries)
		return fmt.Sprintf("%x", sha256.Sum256(b)), true, nil
	}
	return "", false, assistant.ErrInvalid
}
func (s *Server) automationConditions(ctx context.Context, a assistant.Automation) error {
	if a.Conditions.Volume != "" && !slices.Contains(s.opts.Storage.Mounted(), a.Conditions.Volume) {
		return fmt.Errorf("required volume unavailable")
	}
	if a.Conditions.NoRebuild {
		status, err := s.opts.Storage.Status(ctx)
		if err != nil {
			return err
		}
		for _, v := range status.Volumes {
			if v.Sync != nil {
				return fmt.Errorf("waiting for storage maintenance")
			}
		}
	}
	return nil
}

func (s *Server) scanAutomations(ctx context.Context, u auth.User, now time.Time) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// First reconcile durable run reservations; a crash between reservation and
	// chat creation reuses the exact same chat ID, never starts a second action.
	list, err := s.automations.List(u.Name)
	if err != nil {
		return
	}
	for _, a := range list {
		for _, run := range a.Runs {
			if run.State == "starting" {
				release, e := s.chats.Reserve(u.Name, run.Chat)
				if e != nil {
					continue
				}
				c, e := s.chats.Get(u.Name, run.Chat)
				if errors.Is(e, assistant.ErrChatNotFound) {
					c = &assistant.Chat{ID: run.Chat, Title: run.Name, Created: now, Updated: now, Automation: a.ID, Execution: run.Execution, Notify: run.Notify, Run: &assistant.Run{State: "queued", Started: now}, Messages: []assistant.Entry{{Role: "user", Content: run.Instruction, Time: now}}}
					e = s.chats.Save(u.Name, c)
				}
				release()
				if e == nil {
					_ = s.automations.Update(u.Name, func(items *[]assistant.Automation) error {
						for i := range *items {
							for j := range (*items)[i].Runs {
								r := &(*items)[i].Runs[j]
								if r.Chat == run.Chat && r.State == "starting" {
									r.State = "queued"
								}
							}
						}
						return nil
					})
				}
			}
		}
	}
	err = s.automations.Update(u.Name, func(items *[]assistant.Automation) error {
		for i := range *items {
			a := &(*items)[i]
			active := false
			for j := range a.Runs {
				r := &a.Runs[j]
				if r.State == "done" || r.State == "stopped" || r.State == "missing" {
					continue
				}
				c, err := s.chats.Get(u.Name, r.Chat)
				if errors.Is(err, assistant.ErrChatNotFound) && r.State != "starting" {
					r.State = "missing"
					r.Detail = "Chat removed; previous actions are not repeated."
				}
				if err == nil && c.Run != nil {
					r.State = c.Run.State
					r.Detail = c.Run.Detail
				}
				if r.State != "done" && r.State != "stopped" && r.State != "missing" {
					active = true
				}
			}
			if !a.Enabled && !a.Pending {
				continue
			}
			due := a.Pending
			scheduled := now
			if a.Enabled && a.Trigger.Kind == "event" {
				area := map[string]string{"notification": "system", "usb": "files", "volume": "storage", "files": "files"}[a.Trigger.Event]
				if s.opts.Assistant.Settings().Access(area) == assistant.AccessOff || !u.Role.CanAdminister() && a.Trigger.Event != "files" {
					a.Detail = "event_access_denied"
					continue
				}
				// Explicit event monitoring only, with one-minute cooldown. Fingerprints
				// survive restarts. Preserve the baseline on read errors.
				fingerprint, match, e := s.automationEvent(ctx, u, a.Trigger)
				if e != nil {
					a.Detail = e.Error()
					continue
				}
				changed := fingerprint != a.LastEvent
				if a.Trigger.Event == "notification" {
					changed = fingerprint > a.LastEvent
				}
				if a.EventReady && changed && match {
					a.Pending = true
					due = true
				}
				if a.Trigger.Event != "notification" || fingerprint > a.LastEvent {
					a.LastEvent = fingerprint
				}
				a.EventReady = true
				if now.Sub(a.LastStart) < time.Minute {
					continue
				}
			} else if a.Enabled && !a.Next.IsZero() && !a.Next.After(now) {
				scheduled = a.Next
				late := now.Sub(a.Next)
				due = due || late < time.Minute || a.CatchUp == "latest" && late <= 24*time.Hour
				if !due {
					a.Detail = "missed_schedule"
				}
				a.Next = a.Trigger.NextAfter(now)
				if a.Trigger.Kind == "once" {
					a.Enabled = false
				}
			}
			if !due {
				continue
			}
			if active {
				a.Pending = a.Overlap == "queue"
				a.Detail = "previous_run_open"
				continue
			}
			if err := s.validateAutomation(u, a); err != nil {
				a.Detail = err.Error()
				a.Pending = true
				continue
			}
			if err := s.automationConditions(ctx, *a); err != nil {
				a.Detail = err.Error()
				a.Pending = true
				continue
			}
			a.Runs = append(a.Runs, assistant.AutomationRun{Instruction: a.Prompt, Execution: a.Execution, Notify: a.Notify, Name: a.Name, Chat: assistant.NewRunChatID(), Scheduled: scheduled, State: "starting"})
			if len(a.Runs) > 50 {
				a.Runs = a.Runs[len(a.Runs)-50:]
			}
			a.LastStart = now
			a.Pending = false
			a.Detail = ""
		}
		return nil
	})
	if err != nil {
		slog.Error("automation scheduler failed", "user", u.Name, "err", err)
	}
}
