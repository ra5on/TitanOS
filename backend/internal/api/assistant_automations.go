package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

func (s *Server) validateAutomation(u auth.User, a *assistant.Automation) error {
	if err := a.Validate(time.Now()); err != nil {
		return err
	}
	for _, trigger := range a.ExtraTriggers {
		copy := *a
		copy.ExtraTriggers = nil
		copy.Trigger = trigger
		if err := s.validateAutomation(u, &copy); err != nil {
			return err
		}
	}
	// Disabling a definition must work even after its volume disappeared or
	// access was revoked. Dispatch always validates an enabled copy below.
	if !a.Enabled {
		return nil
	}
	if (a.Conditions.Volume != "" || a.Conditions.NoRebuild) && s.opts.Assistant.Settings().Access(assistant.AreaStorage) == assistant.AccessOff {
		return assistant.ErrOff
	}
	if a.Conditions.Volume != "" || a.Conditions.NoRebuild || a.Trigger.Kind == "event" && a.Trigger.Event != "files" {
		if !u.Role.CanAdminister() {
			return assistant.ErrInvalid
		}
	}
	if len(a.Conditions.States) > 0 {
		if !u.Role.CanAdminister() || s.opts.Assistant.Settings().Access(assistant.AreaHome) == assistant.AccessOff || s.opts.HomeAssistant == nil {
			return homeassistant.ErrNotAllowed
		}
		for _, condition := range a.Conditions.States {
			if !s.opts.HomeAssistant.Allowed(condition.Entity) {
				return homeassistant.ErrNotAllowed
			}
		}
	}
	if a.Trigger.Kind == "event" {
		area := assistant.EventAreas[a.Trigger.Event]
		if s.opts.Assistant.Settings().Access(area) == assistant.AccessOff {
			return assistant.ErrOff
		}
		if a.Trigger.Event == "homeassistant" && (s.opts.HomeAssistant == nil || !s.opts.HomeAssistant.Allowed(a.Trigger.Target)) {
			return homeassistant.ErrNotAllowed
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
		"cooldownSeconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 604800},
		"conditions":      schema(map[string]any{"volume": str("Required mounted volume name"), "noRebuild": map[string]any{"type": "boolean"}, "anyState": map[string]any{"type": "boolean"}, "states": map[string]any{"type": "array", "maxItems": 20, "items": schema(map[string]any{"entity": str("Released entity ID"), "operator": enum("Comparison", "eq", "neq", "above", "below", "available"), "value": str("Expected state or number"), "forSeconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 604800, "description": "Require an unchanged state for this duration"}}, "entity", "operator")}, "window": schema(map[string]any{"start": str("HH:MM inclusive"), "end": str("HH:MM exclusive"), "timezone": str("IANA timezone"), "weekdays": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 0, "maximum": 6}}}, "start", "end", "timezone")}),
		"trigger":         schema(map[string]any{"kind": enum("Trigger type", "once", "interval", "daily", "weekly", "event"), "at": str("RFC3339 date for once"), "minutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 525600}, "time": str("HH:MM for daily/weekly"), "timezone": str("IANA timezone, e.g. Europe/Berlin"), "weekday": map[string]any{"type": "integer", "minimum": 0, "maximum": 6, "description": "Sunday=0"}, "event": enum("Event type", "notification", "usb", "volume", "files", "homeassistant"), "target": str("Notification kind, USB filesystem UUID, volume name or Home Assistant entity ID (released ones from ha_entities)"), "from": str("Home Assistant: only changes from this state"), "to": str("Home Assistant: only changes to this state, e.g. not_home"), "above": map[string]any{"type": "number", "description": "Home Assistant: numeric state rises above this"}, "below": map[string]any{"type": "number", "description": "Home Assistant: numeric state falls below this"}, "root": str("Exact accessible root for folder changes"), "path": str("Relative folder (non-recursive)"), "percent": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, "kind"),
	}, "name", "prompt", "enabled", "trigger")
	properties := definition["properties"].(map[string]any)
	triggerSchema := properties["trigger"].(map[string]any)
	triggerSchema["properties"].(map[string]any)["hysteresis"] = map[string]any{"type": "number", "minimum": 0, "maximum": 1e9, "description": "Rearm distance for a single numeric HA threshold"}
	properties["extraTriggers"] = map[string]any{"type": "array", "maxItems": 10, "items": triggerSchema, "description": "Additional event triggers (kind must be event); any trigger can start the task"}
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
func (s *Server) automationEvent(ctx context.Context, u auth.User, t assistant.Trigger, last string, latch ...*bool) (string, bool, error) {
	switch t.Event {
	case "homeassistant":
		ha := s.opts.HomeAssistant
		if ha == nil || !ha.Active() {
			return "", false, fmt.Errorf("Home Assistant is not connected")
		}
		if !ha.Allowed(t.Target) {
			return "", false, homeassistant.ErrNotAllowed
		}
		// Recorded transitions since the last look, so that a short change in
		// between still counts. Marks only grow, also across restarts.
		since, newest := ha.Transitions(t.Target, last)
		if newest < last {
			newest = last
		}
		matched := false
		for _, tr := range since {
			if t.Hysteresis > 0 && len(latch) > 0 {
				value, e := strconv.ParseFloat(tr.To, 64)
				_, beforeErr := strconv.ParseFloat(tr.From, 64)
				if e != nil || math.IsNaN(value) || math.IsInf(value, 0) {
					continue
				}
				beyond := t.Above != nil && value > *t.Above || t.Below != nil && value < *t.Below
				if beforeErr != nil {
					*latch[0] = beyond
					continue
				}
				reset := t.Above != nil && value <= *t.Above-t.Hysteresis || t.Below != nil && value >= *t.Below+t.Hysteresis
				if reset {
					*latch[0] = false
				}
				if !*latch[0] && homeassistant.Match(tr, t.From, t.To, t.Above, t.Below) {
					matched = true
					*latch[0] = true
				}
			} else if homeassistant.Match(tr, t.From, t.To, t.Above, t.Below) {
				matched = true
			}
		}
		if matched {
			return newest, true, nil
		}
		return newest, false, nil
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
	if w := a.Conditions.Window; w != nil && !w.Contains(time.Now()) {
		return fmt.Errorf("outside_time_window")
	}
	if len(a.Conditions.States) > 0 {
		matched := 0
		for _, c := range a.Conditions.States {
			if s.opts.HomeAssistant == nil || !s.opts.HomeAssistant.Allowed(c.Entity) {
				return homeassistant.ErrNotAllowed
			}
			st, err := s.opts.HomeAssistant.State(ctx, c.Entity)
			if err != nil {
				return err
			}
			valid := st.State != "unknown" && st.State != "unavailable"
			ok := false
			switch c.Operator {
			case "eq":
				ok = valid && st.State == c.Value
			case "neq":
				ok = valid && st.State != c.Value
			case "available":
				ok = valid
			case "above", "below":
				n, e := strconv.ParseFloat(st.State, 64)
				v, ve := strconv.ParseFloat(c.Value, 64)
				ok = valid && e == nil && ve == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && !math.IsNaN(v) && !math.IsInf(v, 0) && (c.Operator == "above" && n > v || c.Operator == "below" && n < v)
			}
			if c.ForSeconds > 0 {
				ok = ok && !st.LastChanged.IsZero() && time.Since(st.LastChanged) >= time.Duration(c.ForSeconds)*time.Second
			}
			if ok {
				matched++
			}
		}
		if matched == 0 || !a.Conditions.AnyState && matched != len(a.Conditions.States) {
			return fmt.Errorf("home_conditions_pending")
		}
	}
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
					c = &assistant.Chat{ID: run.Chat, Title: run.Name, Model: s.opts.Assistant.Settings().PlanningModel, Created: now, Updated: now, Automation: a.ID, Execution: run.Execution, Notify: run.Notify, Run: &assistant.Run{State: "queued", Started: now}, Messages: []assistant.Entry{{Role: "user", Content: run.Instruction, Time: now}}}
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
				area := assistant.EventAreas[a.Trigger.Event]
				if s.opts.Assistant.Settings().Access(area) == assistant.AccessOff || !u.Role.CanAdminister() && a.Trigger.Event != "files" {
					a.Detail = "event_access_denied"
					continue
				}
				// Explicit event monitoring only, with one-minute cooldown. Fingerprints
				// survive restarts. Preserve the baseline on read errors.
				fingerprint, match, e := s.automationEvent(ctx, u, a.Trigger, a.LastEvent, &a.Latched)
				if e != nil {
					a.Detail = e.Error()
				} else {
					// High-water marks: older entries never count again.
					highWater := a.Trigger.Event == "notification" || a.Trigger.Event == "homeassistant"
					changed := fingerprint != a.LastEvent
					if highWater {
						changed = fingerprint > a.LastEvent
					}
					if a.EventReady && changed && match {
						a.Pending = true
						due = true
					}
					if !highWater || fingerprint > a.LastEvent {
						a.LastEvent = fingerprint
					}
					a.EventReady = true
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
			if a.Enabled {
				if a.ExtraEvents == nil {
					a.ExtraEvents = map[string]assistant.EventCursor{}
				}
				for index, trigger := range a.ExtraTriggers {
					key := strconv.Itoa(index)
					cursor := a.ExtraEvents[key]
					copy := *a
					copy.ExtraTriggers = nil
					copy.Trigger = trigger
					if err := s.validateAutomation(u, &copy); err != nil {
						a.Detail = err.Error()
						continue
					}
					mark, match, err := s.automationEvent(ctx, u, trigger, cursor.Mark, &cursor.Latched)
					if err != nil {
						a.Detail = err.Error()
						continue
					}
					high := trigger.Event == "homeassistant" || trigger.Event == "notification"
					changed := mark != cursor.Mark
					if high {
						changed = mark > cursor.Mark
					}
					if cursor.Ready && changed && match {
						a.Pending = true
						due = true
					}
					if !high || mark > cursor.Mark {
						cursor.Mark = mark
					}
					cursor.Ready = true
					a.ExtraEvents[key] = cursor
				}
			}
			if (a.Trigger.Kind == "event" || len(a.ExtraTriggers) > 0) && now.Sub(a.LastStart) < time.Duration(max(60, a.CooldownSeconds))*time.Second {
				if due {
					a.Pending = true
				}
				continue
			}
			if !due {
				continue
			}
			if active {
				a.Pending = a.Overlap == "queue"
				a.Detail = "previous_run_open"
				continue
			}
			validation := *a
			validation.Enabled = true
			if err := s.validateAutomation(u, &validation); err != nil {
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

// watchHomeAssistant tells the Home Assistant watcher which entities enabled
// automations wait for; their state changes are then recorded.
func (s *Server) watchHomeAssistant(users []auth.User) {
	if s.opts.HomeAssistant == nil {
		return
	}
	var entities []string
	for _, u := range users {
		if !s.assistantAllowed(u) || !u.Role.CanAdminister() {
			continue
		}
		list, err := s.automations.List(u.Name)
		if err != nil {
			continue
		}
		for _, a := range list {
			if a.Enabled {
				for _, trigger := range append([]assistant.Trigger{a.Trigger}, a.ExtraTriggers...) {
					if trigger.Kind == "event" && trigger.Event == "homeassistant" && s.opts.HomeAssistant.Allowed(trigger.Target) {
						entities = append(entities, trigger.Target)
					}
				}
			}
		}
	}
	s.opts.HomeAssistant.SetWatched(entities)
}

func (s *Server) handleAutomationPreview(w http.ResponseWriter, r *http.Request) {
	var a assistant.Automation
	if !readJSON(w, r, &a) {
		return
	}
	a.Enabled = true
	if err := s.validateAutomation(currentUser(r), &a); err != nil {
		writeAssistantError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	reason := ""
	if err := s.automationConditions(ctx, a); err != nil {
		reason = err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]any{"conditionsMet": reason == "", "reason": reason, "next": a.Trigger.NextAfter(time.Now()), "observedAt": time.Now(), "executed": false})
}
