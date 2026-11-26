package api

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

var personalIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)
var personalColorPattern = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)

func levelValid(l pa.Level) bool { return l == pa.Never || l == pa.Ask || l == pa.Auto }
func lowerLevel(a, b pa.Level) pa.Level {
	if a == pa.Never || b == pa.Never || !levelValid(a) || !levelValid(b) {
		return pa.Never
	}
	if a == pa.Ask || b == pa.Ask {
		return pa.Ask
	}
	return pa.Auto
}
func personalCap(t personalTool, a *personalAccount) pa.Capability {
	ceiling := t.Default
	if !levelValid(ceiling) {
		ceiling = pa.Never
	}
	if limit, ok := a.Limits[t.ID]; ok {
		ceiling = limit
	}
	pref, ok := a.Settings.Rights[t.ID]
	if !ok {
		pref = pa.Ask
		if t.ID == "agent.example" {
			pref = pa.Auto
		}
	}
	c := pa.Capability{ID: t.ID, Title: t.Title, App: t.App, Sensitive: t.Sensitive, Ceiling: ceiling, Effective: lowerLevel(ceiling, pref)}
	switch {
	case ceiling == pa.Never:
		c.Reason = "admin"
	case pref == pa.Never:
		c.Reason = "personal"
	case c.Effective == pa.Ask && ceiling == pa.Ask:
		c.Reason = "admin_ask"
	case c.Effective == pa.Ask:
		c.Reason = "personal_ask"
	case t.Sensitive:
		c.Reason = "approval_required"
	}
	return c
}
func (s *Server) personalEffective(u auth.User, id string) (pa.Capability, error) {
	t, ok := s.personal.tools[id]
	if !ok {
		return pa.Capability{}, errPersonalMissing
	}
	var c pa.Capability
	err := s.personalTxn(u, false, func(a *personalAccount) error { c = personalCap(t, a); return nil })
	if err != nil {
		return c, err
	}
	u, ok = s.opts.Users.Get(u.Name)
	if !ok || u.Disabled {
		return c, errPersonalDenied
	}
	if t.Available != nil {
		if err := t.Available(u); err != nil {
			c.Effective = pa.Never
			c.Reason = "app"
		}
	}
	return c, nil
}
func (s *Server) personalCaps(u auth.User, a *personalAccount) []pa.Capability {
	cs := make([]pa.Capability, 0, len(s.personal.tools))
	for _, t := range s.personal.tools {
		c := personalCap(t, a)
		if t.Available != nil && t.Available(u) != nil {
			c.Effective = pa.Never
			c.Reason = "app"
		}
		cs = append(cs, c)
	}
	slices.SortFunc(cs, func(a, b pa.Capability) int { return cmp.Compare(a.ID, b.ID) })
	return cs
}
func (s *Server) personalCheck(ctx context.Context, u auth.User, req pa.Request) (pa.Level, error) {
	current, ok := s.opts.Users.Get(u.Name)
	if !ok || current.Disabled || personalKey(current) != personalKey(u) {
		return pa.Never, errPersonalDenied
	}
	c, err := s.personalEffective(current, req.Tool)
	if err != nil {
		return pa.Never, err
	}
	if c.Effective == pa.Never {
		return pa.Never, errPersonalDenied
	}
	t := s.personal.tools[req.Tool]
	if t.Check != nil {
		if err := t.Check(ctx, current, req); err != nil {
			return pa.Never, fmt.Errorf("%w: %w", errPersonalDenied, err)
		}
	}
	return c.Effective, nil
}

// A later operation may use data produced by every previous step. Revalidate those
// resources before disclosing, transforming or writing the carried text.
func (s *Server) personalWorkflowCheck(ctx context.Context, u auth.User, id string, current pa.Request) (pa.Level, error) {
	var previous []pa.Request
	err := s.personalTxn(u, false, func(a *personalAccount) error {
		j, err := a.find(id)
		if err != nil {
			return err
		}
		p := a.Plans[id]
		if p == nil || p.Index < 0 || p.Index >= len(p.Steps) || p.Steps[p.Index] != current.Tool {
			return errPersonalConflict
		}
		for _, tool := range p.Steps[:p.Index] {
			r := s.personalStepRequest(j, p, tool)
			r.PreviousText = ""
			previous = append(previous, r)
		}
		return nil
	})
	if err != nil {
		return pa.Never, err
	}
	for _, r := range previous {
		if _, err := s.personalCheck(ctx, u, r); err != nil {
			return pa.Never, err
		}
	}
	return s.personalCheck(ctx, u, current)
}
func validatePersonalSources(sources []pa.Source) error {
	if len(sources) > 32 {
		return errPersonalInvalid
	}
	for _, src := range sources {
		if !personalIDPattern.MatchString(src.Kind) || src.ID == "" || len(src.ID) > 4096 || len(src.Label) > 512 || len(src.Ref) > 20 {
			return errPersonalInvalid
		}
		for k, v := range src.Ref {
			if !personalIDPattern.MatchString(k) || len(v) > 4096 {
				return errPersonalInvalid
			}
		}
	}
	return nil
}
func validatePersonalRequest(r pa.Request) error {
	if !personalIDPattern.MatchString(r.Tool) || !personalIDPattern.MatchString(r.RequestID) || len(r.Prompt) > 16384 || len(r.Args) > 32768 {
		return errPersonalInvalid
	}
	if r.ChatID != "" && !personalIDPattern.MatchString(r.ChatID) {
		return errPersonalInvalid
	}
	return validatePersonalSources(r.Sources)
}
func personalLimitText(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
func (s *Server) personalValidRights(rs map[string]pa.Level) bool {
	for id, l := range rs {
		if _, ok := s.personal.tools[id]; !ok || !levelValid(l) {
			return false
		}
	}
	return true
}

type personalPatch struct {
	Notifications *pa.NoticePrefs     `json:"notifications"`
	Rights        map[string]pa.Level `json:"rights"`
	Paused        *bool               `json:"paused"`
	Theme         *string             `json:"theme"`
	Motion        *string             `json:"motion"`
	DotSize       *int                `json:"dotSize"`
	Colors        map[string]string   `json:"colors"`
}

func (p personalPatch) apply(v *pa.Settings) error {
	if p.Notifications != nil {
		v.Notifications = *p.Notifications
	}
	if p.Theme != nil {
		if !slices.Contains([]string{"system", "light", "dark"}, *p.Theme) {
			return errPersonalInvalid
		}
		v.Theme = *p.Theme
	}
	if p.Motion != nil {
		if !slices.Contains([]string{"full", "subtle", "off"}, *p.Motion) {
			return errPersonalInvalid
		}
		v.Motion = *p.Motion
	}
	if p.DotSize != nil {
		if *p.DotSize < 4 || *p.DotSize > 48 {
			return errPersonalInvalid
		}
		v.DotSize = *p.DotSize
	}
	if p.Colors != nil {
		if len(p.Colors) > 32 {
			return errPersonalInvalid
		}
		for k, c := range p.Colors {
			if !personalIDPattern.MatchString(k) || !personalColorPattern.MatchString(c) {
				return errPersonalInvalid
			}
		}
		v.Colors = p.Colors
	}
	if p.Rights != nil {
		v.Rights = maps.Clone(p.Rights)
	}
	if p.Paused != nil {
		v.Paused = *p.Paused
	}
	return nil
}
func personalExample(context.Context, auth.User, pa.Request) (pa.Preview, error) {
	return pa.Preview{Title: "Agent", Text: "2 + 3 = 5"}, nil
}
func (s *Server) initPersonalTools() {
	p := &s.personal
	p.active = map[string]context.CancelFunc{}
	p.processing = map[string]bool{}
	p.watchDue = map[string]time.Time{}
	p.known = map[string]time.Time{}
	p.leases = map[string]personalLease{}
	p.tools = map[string]personalTool{"agent.example": {ID: "agent.example", Title: "2 + 3", App: "agent", Default: pa.Auto, Prepare: personalExample}}
	for _, factory := range personalToolFactories {
		for _, t := range factory(s) {
			if !personalIDPattern.MatchString(t.ID) || strings.TrimSpace(t.Title) == "" {
				panic("invalid personal Agent tool: " + t.ID)
			}
			if _, exists := p.tools[t.ID]; exists {
				panic("duplicate personal Agent tool: " + t.ID)
			}
			p.tools[t.ID] = t
		}
	}
}
