package assistant

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"
)

// StateCondition refers only to an explicitly released Home Assistant entity.
// ForSeconds requires an unchanged state, not merely repeated polling results.
type StateCondition struct {
	Entity     string `json:"entity"`
	Operator   string `json:"operator"`
	Value      string `json:"value,omitempty"`
	ForSeconds int    `json:"forSeconds,omitempty"`
}

type TimeWindow struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
	Weekdays []int  `json:"weekdays,omitempty"`
}

func (c Conditions) Validate() error {
	if len(c.States) > 20 {
		return fmt.Errorf("%w: at most 20 state conditions", ErrInvalid)
	}
	for _, state := range c.States {
		if state.Operator == "above" || state.Operator == "below" {
			value, err := strconv.ParseFloat(state.Value, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("%w: numeric state condition", ErrInvalid)
			}
		}
		if !haEntity.MatchString(state.Entity) || !slices.Contains([]string{"eq", "neq", "above", "below", "available"}, state.Operator) || len(state.Value) > 255 || state.ForSeconds < 0 || state.ForSeconds > 604800 {
			return fmt.Errorf("%w: state condition", ErrInvalid)
		}
	}
	if w := c.Window; w != nil {
		if _, err := time.LoadLocation(w.Timezone); err != nil || w.Timezone == "" {
			return fmt.Errorf("%w: window timezone", ErrInvalid)
		}
		for _, clock := range []string{w.Start, w.End} {
			if _, err := time.Parse("15:04", clock); err != nil {
				return fmt.Errorf("%w: window time", ErrInvalid)
			}
		}
		if w.Start == w.End {
			return fmt.Errorf("%w: empty window", ErrInvalid)
		}
		for _, day := range w.Weekdays {
			if day < 0 || day > 6 {
				return ErrInvalid
			}
		}
	}
	return nil
}

// Contains assigns the part after midnight to the weekday the window began.
func (w TimeWindow) Contains(now time.Time) bool {
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		return false
	}
	local := now.In(loc)
	clock := local.Format("15:04")
	day := int(local.Weekday())
	inside := clock >= w.Start && clock < w.End
	if w.Start > w.End {
		inside = clock >= w.Start || clock < w.End
		if clock < w.End {
			day = (day + 6) % 7
		}
	}
	return inside && (len(w.Weekdays) == 0 || slices.Contains(w.Weekdays, day))
}

// Definition holds no run records, so revision history stays bounded.
type AutomationDefinition struct {
	ExtraTriggers   []Trigger  `json:"extraTriggers,omitempty"`
	Revision        int        `json:"revision"`
	Name            string     `json:"name"`
	Prompt          string     `json:"prompt"`
	Trigger         Trigger    `json:"trigger"`
	Conditions      Conditions `json:"conditions"`
	Execution       string     `json:"execution"`
	Notify          string     `json:"notify"`
	CatchUp         string     `json:"catchUp"`
	Overlap         string     `json:"overlap"`
	CooldownSeconds int        `json:"cooldownSeconds"`
}

func (a Automation) Definition() AutomationDefinition {
	return AutomationDefinition{a.ExtraTriggers, a.Revision, a.Name, a.Prompt, a.Trigger, a.Conditions, a.Execution, a.Notify, a.CatchUp, a.Overlap, a.CooldownSeconds}
}
