package assistant

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Automations are account-owned durable definitions and dispatch records on a
// data volume. They never fall back to RAM or write the boot stick on a tick.
type Trigger struct {
	Hysteresis float64   `json:"hysteresis,omitempty"`
	Kind       string    `json:"kind"` // once, interval, daily, weekly, event
	At         time.Time `json:"at,omitempty"`
	Minutes    int       `json:"minutes,omitempty"`
	Time       string    `json:"time,omitempty"`
	Timezone   string    `json:"timezone,omitempty"`
	Weekday    int       `json:"weekday,omitempty"`
	Event      string    `json:"event,omitempty"`  // notification, usb, volume, files, homeassistant
	Target     string    `json:"target,omitempty"` // notification kind, USB UUID, volume name, entity ID
	Root       string    `json:"root,omitempty"`
	Path       string    `json:"path,omitempty"`
	Percent    int       `json:"percent,omitempty"`
	// From, To, Above and Below narrow a Home Assistant state change.
	From  string   `json:"from,omitempty"`
	To    string   `json:"to,omitempty"`
	Above *float64 `json:"above,omitempty"`
	Below *float64 `json:"below,omitempty"`
}
type Conditions struct {
	States    []StateCondition `json:"states,omitempty"`
	AnyState  bool             `json:"anyState,omitempty"`
	Window    *TimeWindow      `json:"window,omitempty"`
	Volume    string           `json:"volume,omitempty"`
	NoRebuild bool             `json:"noRebuild,omitempty"`
}
type AutomationRun struct {
	Instruction string    `json:"instruction,omitempty"`
	Execution   string    `json:"execution,omitempty"`
	Notify      string    `json:"notify,omitempty"`
	Name        string    `json:"name,omitempty"`
	Chat        string    `json:"chat"`
	Scheduled   time.Time `json:"scheduled"`
	State       string    `json:"state"`
	Detail      string    `json:"detail,omitempty"`
}
type EventCursor struct {
	Mark    string `json:"mark"`
	Ready   bool   `json:"ready"`
	Latched bool   `json:"latched"`
}

type Automation struct {
	ExtraTriggers   []Trigger              `json:"extraTriggers,omitempty"`
	ExtraEvents     map[string]EventCursor `json:"extraEvents,omitempty"`
	Latched         bool                   `json:"latched,omitempty"`
	CooldownSeconds int                    `json:"cooldownSeconds,omitempty"`
	Versions        []AutomationDefinition `json:"versions,omitempty"`
	ID              string                 `json:"id"`
	Revision        int                    `json:"revision"`
	Name            string                 `json:"name"`
	Prompt          string                 `json:"prompt"`
	Enabled         bool                   `json:"enabled"`
	Trigger         Trigger                `json:"trigger"`
	Conditions      Conditions             `json:"conditions"`
	Execution       string                 `json:"execution"` // inherit, confirm, direct
	Notify          string                 `json:"notify"`    // errors, always, never
	CatchUp         string                 `json:"catchUp"`   // latest or skip
	Overlap         string                 `json:"overlap"`   // skip or queue
	Next            time.Time              `json:"next,omitempty"`
	LastEvent       string                 `json:"lastEvent,omitempty"`
	EventReady      bool                   `json:"eventReady,omitempty"`
	LastStart       time.Time              `json:"lastStart,omitempty"`
	Pending         bool                   `json:"pending,omitempty"`
	Detail          string                 `json:"detail,omitempty"`
	Runs            []AutomationRun        `json:"runs"`
}

func (a *Automation) Validate(now time.Time) error {
	if strings.TrimSpace(a.Name) == "" || len([]rune(a.Name)) > 120 || strings.TrimSpace(a.Prompt) == "" || len(a.Prompt) > MaxMessage {
		return fmt.Errorf("%w: name and instruction required", ErrInvalid)
	}
	if a.Execution == "" {
		a.Execution = "inherit"
	}
	if a.Notify == "" {
		a.Notify = "errors"
	}
	if a.CatchUp == "" {
		a.CatchUp = "latest"
	}
	if a.Overlap == "" {
		a.Overlap = "skip"
	}
	if !slices.Contains([]string{"inherit", "confirm", "direct"}, a.Execution) || !slices.Contains([]string{"errors", "always", "never"}, a.Notify) || !slices.Contains([]string{"latest", "skip"}, a.CatchUp) || !slices.Contains([]string{"skip", "queue"}, a.Overlap) {
		return ErrInvalid
	}
	if err := a.Conditions.Validate(); err != nil {
		return err
	}
	if a.CooldownSeconds < 0 || a.CooldownSeconds > 604800 {
		return ErrInvalid
	}
	if len(a.ExtraTriggers) > 10 {
		return ErrInvalid
	}
	for _, trigger := range a.ExtraTriggers {
		if trigger.Kind != "event" {
			return ErrInvalid
		}
		copy := *a
		copy.ExtraTriggers = nil
		copy.Trigger = trigger
		if err := copy.Validate(now); err != nil {
			return err
		}
	}
	t := a.Trigger
	if math.IsNaN(t.Hysteresis) || math.IsInf(t.Hysteresis, 0) || t.Hysteresis < 0 || t.Hysteresis > 1e9 {
		return ErrInvalid
	}
	if t.Hysteresis > 0 && (t.Event != "homeassistant" || (t.Above == nil) == (t.Below == nil)) {
		return ErrInvalid
	}
	switch t.Kind {
	case "once":
		if t.At.IsZero() {
			return ErrInvalid
		}
	case "interval":
		if t.Minutes < 1 || t.Minutes > 525600 {
			return ErrInvalid
		}
	case "daily", "weekly":
		if _, err := time.LoadLocation(t.Timezone); err != nil || t.Timezone == "" {
			return fmt.Errorf("%w: timezone", ErrInvalid)
		}
		if _, err := time.Parse("15:04", t.Time); err != nil {
			return fmt.Errorf("%w: clock time", ErrInvalid)
		}
		if t.Weekday < 0 || t.Weekday > 6 {
			return ErrInvalid
		}
	case "event":
		if !slices.Contains([]string{"notification", "usb", "volume", "files", "homeassistant"}, t.Event) {
			return ErrInvalid
		}
		if t.Event == "homeassistant" && (!haEntity.MatchString(t.Target) || len(t.From) > 255 || len(t.To) > 255) {
			return fmt.Errorf("%w: Home Assistant entity", ErrInvalid)
		}
		if t.Event == "files" {
			if t.Root == "" {
				return ErrInvalid
			}
		} else if t.Target == "" {
			return ErrInvalid
		}
		if t.Event == "volume" && (t.Percent < 1 || t.Percent > 100) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

var haEntity = regexp.MustCompile(`^[a-z_]+\.[a-z0-9_]+$`)

// EventAreas says which area of the assistant an event source belongs to.
var EventAreas = map[string]string{"notification": AreaSystem, "usb": AreaFiles, "volume": AreaStorage, "files": AreaFiles, "homeassistant": AreaHome}

// NextAfter runs once per local calendar date (including the repeated autumn
// hour). A nonexistent spring clock time is skipped, never shifted silently.
func (t Trigger) NextAfter(now time.Time) time.Time {
	switch t.Kind {
	case "once":
		if t.At.After(now) {
			return t.At
		}
	case "interval":
		return now.Add(time.Duration(t.Minutes) * time.Minute)
	case "daily", "weekly":
		loc, err := time.LoadLocation(t.Timezone)
		if err != nil {
			return time.Time{}
		}
		clock, err := time.Parse("15:04", t.Time)
		if err != nil {
			return time.Time{}
		}
		local := now.In(loc)
		for day := 0; day < 9; day++ {
			date := time.Date(local.Year(), local.Month(), local.Day()+day, 12, 0, 0, 0, loc)
			if t.Kind == "weekly" && int(date.Weekday()) != t.Weekday {
				continue
			}
			next := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
			if next.Hour() != clock.Hour() || next.Minute() != clock.Minute() {
				continue
			}
			// Resolve repeated local times to the first occurrence.
			for offset := 120; offset >= 1; offset-- {
				earlier := next.Add(-time.Duration(offset) * time.Minute)
				if earlier.Year() == date.Year() && earlier.YearDay() == date.YearDay() && earlier.Hour() == clock.Hour() && earlier.Minute() == clock.Minute() {
					next = earlier
					break
				}
			}
			if next.After(now) {
				return next
			}
		}
	}
	return time.Time{}
}

type Automations struct {
	mu  sync.Mutex
	dir func() (string, bool)
}

func NewAutomations(dir func() (string, bool)) *Automations { return &Automations{dir: dir} }

// Update serializes API changes and scheduler decisions. Callbacks must not
// re-enter this store. Failure leaves the previous durable state unchanged.
func (s *Automations) Update(user string, fn func(*[]Automation) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !accountRE.MatchString(user) {
		return ErrInvalid
	}
	base, ok := s.dir()
	if !ok {
		return fmt.Errorf("%w: automations require an available persistent volume", ErrInvalid)
	}
	dir := filepath.Join(base, user, "automations")
	file := filepath.Join(dir, "definitions.json")
	list := []Automation{}
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err = json.Unmarshal(data, &list); err != nil {
			return err
		}
	}
	before, _ := json.Marshal(list)
	if err = fn(&list); err != nil {
		return err
	}
	after, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if string(before) == string(after) {
		return nil
	}
	// Do not switch to a different volume during an operation.
	current, available := s.dir()
	if !available || current != base {
		return fmt.Errorf("automation volume changed")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return writeFileAtomic(file, after)
}
func (s *Automations) List(user string) ([]Automation, error) {
	var out []Automation
	err := s.Update(user, func(list *[]Automation) error { out = *list; return nil })
	return out, err
}
func (s *Automations) Put(user string, a Automation, now time.Time) (Automation, error) {
	err := a.Validate(now)
	if err != nil {
		return a, err
	}
	err = s.Update(user, func(list *[]Automation) error {
		if a.ID == "" {
			a.Next = time.Time{}
			a.LastEvent = ""
			a.EventReady = false
			a.LastStart = time.Time{}
			a.Pending = false
			a.Detail = ""
			a.ID = newID()
			a.Revision = 1
			a.Versions = nil
			a.ExtraEvents = nil
			a.Latched = false
			a.Runs = []AutomationRun{}
			a.Next = a.Trigger.NextAfter(now)
			if a.Trigger.Kind == "once" {
				a.Next = a.Trigger.At
			}
			*list = append(*list, a)
			return nil
		}
		for i, old := range *list {
			if old.ID == a.ID {
				if old.Revision != a.Revision {
					return fmt.Errorf("%w: automation changed; reload before editing", ErrInvalid)
				}
				a.Revision++
				a.Versions = append(old.Versions, old.Definition())
				if len(a.Versions) > 5 {
					a.Versions = a.Versions[len(a.Versions)-5:]
				}
				a.Runs = old.Runs
				a.LastStart = old.LastStart
				a.Next = old.Next
				a.LastEvent = old.LastEvent
				a.EventReady = old.EventReady
				a.Pending = old.Pending
				a.Detail = old.Detail
				a.ExtraEvents = old.ExtraEvents
				a.Latched = old.Latched
				if !reflect.DeepEqual(old.Trigger, a.Trigger) || !reflect.DeepEqual(old.ExtraTriggers, a.ExtraTriggers) || !old.Enabled && a.Enabled {
					a.Next = a.Trigger.NextAfter(now)
					a.ExtraEvents = nil
					a.Latched = false
					a.LastEvent = ""
					a.EventReady = false
					a.Pending = false
				}
				if !a.Enabled {
					a.Pending = false
				}
				(*list)[i] = a
				return nil
			}
		}
		return ErrChatNotFound
	})
	return a, err
}
func (s *Automations) Delete(user, id string) error {
	return s.Update(user, func(list *[]Automation) error {
		for i, a := range *list {
			if a.ID == id {
				*list = append((*list)[:i], (*list)[i+1:]...)
				return nil
			}
		}
		return ErrChatNotFound
	})
}
func NewRunChatID() string { return newID() }
