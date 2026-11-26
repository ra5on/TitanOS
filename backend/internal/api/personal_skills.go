package api

import (
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"

	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
)

// A recipe captures a narrow list of tool IDs; it cannot grant a right. Data passed
// between steps is plain text. This list contains no executable expressions.
func (s *Server) personalBuiltIns() []pa.Skill {
	out := []pa.Skill{}
	for _, d := range []struct {
		id, title string
		tools     []string
	}{
		{"inbox", "Posteingang", []string{"mail.summarize"}},
		{"appointments", "Termine", []string{"calendar.read"}},
		{"documents", "Dokumente", []string{"office.inspect"}},
		{"tables", "Tabellen und PDF", []string{"office.table"}},
		{"watch", "Beobachten", []string{"mail.followup"}},
		{"delivery", "Liefertermin prüfen und antworten", []string{"mail.event", "calendar.free", "mail.reply", "mail.send"}},
		{"delivery-document", "Termin als Dokument entwerfen", []string{"mail.event", "calendar.free", "office.document"}},
	} {
		available := true
		for _, tool := range d.tools {
			if t, ok := s.personal.tools[tool]; !ok || t.Prepare == nil {
				available = false
				break
			}
		}
		if available {
			out = append(out, pa.Skill{ID: d.id, Title: d.title, Steps: d.tools, Trigger: "manual", Approval: "inherit", BuiltIn: true})
		}

	}
	return out
}
func (s *Server) personalSkills(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if r.Method == http.MethodGet {
		var out []pa.Skill
		err := s.personalTxn(u, false, func(a *personalAccount) error {
			out = s.personalBuiltIns()
			for _, sk := range a.Skills {
				if sk.Trigger == "mail" {
					watch, ok := a.Watches[sk.ID]
					switch {
					case !ok:
						sk.WatchState = "revoked"
					case a.Settings.Paused:
						sk.WatchState = "paused"
					case watch.Retry:
						sk.WatchState = "retry"
					default:
						sk.WatchState = "active"
					}
				}
				out = append(out, sk)
			}
			return nil
		})
		personalRespond(w, map[string]any{"skills": out}, err)
		return
	}
	var skill pa.Skill
	if !readPersonalJSON(w, r, &skill) {
		return
	}
	skill.Title = strings.TrimSpace(skill.Title)
	if skill.PDFName != "" && (skill.Trigger != "mail" || len(skill.PDFName) > 240 || path.Base(skill.PDFName) != skill.PDFName || strings.Contains(skill.PDFName, "\\") || strings.ContainsFunc(skill.PDFName, unicode.IsControl) || !strings.EqualFold(path.Ext(skill.PDFName), ".pdf")) {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	if slices.ContainsFunc(s.personalBuiltIns(), func(x pa.Skill) bool { return x.ID == skill.ID }) {
		personalRespond(w, nil, errPersonalConflict)
		return
	}
	if skill.WatchState != "" || len(skill.Prompt) > 16384 || len(skill.Args) > 32768 || len(skill.Expected) > 2048 || len(skill.StepArgs) > 8 || skill.Sender != "" && skill.Trigger != "mail" || skill.Title == "" || len(skill.Title) > 160 || len(skill.Steps) == 0 || len(skill.Steps) > 8 || validatePersonalSources(skill.Sources) != nil ||
		!slices.Contains([]string{"manual", "schedule", "mail"}, skill.Trigger) || !slices.Contains([]string{"ask", "inherit"}, skill.Approval) ||
		(skill.Trigger == "schedule" && (skill.RunAt.IsZero() || skill.RunAt.Before(time.Now()))) || skill.BuiltIn {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	// Idempotent creation applies even if the HTTP response was lost.
	if !personalIDPattern.MatchString(skill.ID) || len(skill.ID) < 16 {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	for _, tool := range skill.Steps {
		t, ok := s.personal.tools[tool]
		if !ok || t.Prepare == nil {
			personalRespond(w, nil, errPersonalInvalid)
			return
		}
		if t.Available != nil && t.Available(u) != nil {
			personalRespond(w, nil, errPersonalDenied)
			return
		}
	}
	for id := range skill.StepArgs {
		if !slices.Contains(skill.Steps, id) {
			personalRespond(w, nil, errPersonalInvalid)
			return
		}
	}
	var baseline personalMailWatch
	if skill.Trigger == "mail" {
		if !strings.HasPrefix(skill.Steps[0], "mail.") {
			personalRespond(w, nil, errPersonalInvalid)
			return
		}
		var err error
		baseline, err = s.personalWatchBaseline(r.Context(), u, skill)
		if err != nil {
			personalRespond(w, nil, err)
			return
		}
	}
	err := s.personalTxn(u, true, func(a *personalAccount) error {
		for _, existing := range a.Skills {
			if existing.ID == skill.ID {
				if personalDigest(existing) == personalDigest(skill) {
					return nil
				}
				return errPersonalConflict
			}
		}
		if len(a.Skills) >= 200 {
			return errPersonalFull
		}
		for _, id := range skill.Steps {
			if personalCap(s.personal.tools[id], a).Effective == pa.Never {
				return errPersonalDenied
			}
		}
		if skill.Trigger == "mail" {
			a.Watches[skill.ID] = baseline
		}
		if skill.Trigger == "schedule" {
			req := pa.Request{Tool: skill.Steps[0], Prompt: skill.Prompt, Args: skill.Args, Sources: skill.Sources, RequestID: "schedule:" + skill.ID, RunAt: skill.RunAt}
			j, err := s.personalEnqueue(a, req, skill.Steps, skill.Approval == "ask")
			if err != nil {
				return err
			}
			bindPersonalSkill(a.Plans[j.ID], skill)
		}
		a.Skills = append(a.Skills, skill)
		return nil
	})
	personalRespond(w, skill, err)
}
func (s *Server) personalDeleteSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.personalTxn(currentUser(r), true, func(a *personalAccount) error {
		at := slices.IndexFunc(a.Skills, func(sk pa.Skill) bool { return sk.ID == id })
		if at < 0 {
			return errPersonalMissing
		}
		// Deleting a recipe cancels its not-yet-executed scheduled instance.
		for i := range a.Jobs {
			j := &a.Jobs[i]
			if a.Plans[j.ID].SkillID == id && j.Status != "done" && j.Status != "failed" {
				if j.Phase == "executing" {
					return errPersonalConflict
				}
				j.Status = "failed"
				j.Phase = "complete"
				j.Error = "schedule_removed"
				j.Token = ""
				a.Plans[j.ID].Claim = ""
			}
		}
		delete(a.Watches, id)
		a.Skills = slices.Delete(a.Skills, at, at+1)
		return nil
	})
	personalRespond(w, map[string]bool{"deleted": err == nil}, err)
}
func (s *Server) personalRunSkill(w http.ResponseWriter, r *http.Request) {
	var req pa.Request
	if !readPersonalJSON(w, r, &req) {
		return
	}
	var skill pa.Skill
	u := currentUser(r)
	err := s.personalTxn(u, false, func(a *personalAccount) error {
		for _, v := range append(s.personalBuiltIns(), a.Skills...) {
			if v.ID == r.PathValue("id") {
				skill = v
				return nil
			}
		}
		return errPersonalMissing
	})
	if err != nil {
		personalRespond(w, nil, err)
		return
	}
	req.Tool = skill.Steps[0]
	if len(skill.Sources) > 0 {
		if len(req.Sources) > 0 && personalDigest(req.Sources) != personalDigest(skill.Sources) {
			personalRespond(w, nil, errPersonalDenied)
			return
		}
		req.Sources = skill.Sources
	}
	if skill.Trigger == "mail" {
		personalRespond(w, nil, errPersonalInvalid)
		return
	}
	if req.Prompt == "" {
		req.Prompt = skill.Prompt
	}
	if len(skill.Args) > 0 {
		req.Args = skill.Args
	}
	if err := validatePersonalRequest(req); err != nil {
		personalRespond(w, nil, err)
		return
	}
	var j pa.Job
	err = s.personalTxn(u, true, func(a *personalAccount) error {
		if !skill.BuiltIn && !slices.ContainsFunc(a.Skills, func(v pa.Skill) bool { return personalDigest(v) == personalDigest(skill) }) {
			return errPersonalConflict
		}
		var err error
		j, err = s.personalEnqueue(a, req, skill.Steps, skill.Approval == "ask")
		if err != nil {
			return err
		}
		p := a.Plans[j.ID]
		if p.SkillID != "" && p.SkillID != skill.ID {
			return errPersonalConflict
		}
		bindPersonalSkill(p, skill)
		return nil
	})
	s.personalRespondJob(w, r, j.ID, err)
}

func bindPersonalSkill(p *personalPlan, s pa.Skill) {
	p.SkillID = s.ID
	p.StepArgs = s.StepArgs
	p.Expected = s.Expected
}
