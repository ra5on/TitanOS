package api

import (
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/docindex"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Fristenwächter (PLAN 54-D item 2): deadlines found while reading become
// suggestions of kind "deadline" in the agent's Vorschläge (default, "Erst
// vorschlagen") or reminders right away ("Gleich als Erinnerung"). Taking a
// suggestion makes reminders the given days before, each with a message.

const maxProposed = 4000

var deadlineWord = map[string]string{
	docindex.KindCancel:   "Kündigung",
	docindex.KindWarranty: "Garantie",
	docindex.KindPayment:  "Zahlung",
	docindex.KindDue:      "Frist",
}

// docsPlace names a folder for people: "KI-Agent › Dokumente".
func docsPlace(root, p string) string {
	name := strings.TrimPrefix(root, "share/")
	dir := path.Dir(p)
	if dir == "." || dir == "" {
		return name
	}
	return name + " › " + strings.ReplaceAll(dir, "/", " › ")
}

// deadlineItem is the suggestion of one deadline of a document.
func deadlineItem(root, p string, page int, d docindex.Deadline, sender, paperID string) agentInboxItem {
	date := d.Date.Format("02.01.2006")
	base := strings.TrimSuffix(path.Base(p), path.Ext(p))
	title := ""
	switch {
	case d.Kind == docindex.KindPayment && d.Cents > 0 && sender != "":
		title = fmt.Sprintf("%s: %s fällig am %s", sender, docindex.FormatEuro(d.Cents), date)
	case d.Kind == docindex.KindPayment:
		title = fmt.Sprintf("Zahlung fällig am %s · %s", date, base)
	default:
		title = fmt.Sprintf("%s bis %s · %s", deadlineWord[d.Kind], date, base)
	}
	body := fmt.Sprintf("Aus „%s“ (%s)", path.Base(p), docsPlace(root, p))
	if page > 0 {
		body += fmt.Sprintf(", Seite %d", page)
	}
	if d.Snippet != "" {
		body += ": „" + d.Snippet + "“"
	}
	data := map[string]string{"root": root, "path": p, "page": strconv.Itoa(page), "date": d.Date.Format("2006-01-02"), "deadline": d.Kind}
	if d.Cents != 0 {
		data["cents"] = strconv.FormatInt(d.Cents, 10)
	}
	if d.Kind == docindex.KindPayment {
		data["debit"] = map[bool]string{true: "1", false: "0"}[d.Debit]
	}
	if paperID != "" {
		data["paper"] = paperID
	}
	return agentInboxItem{Kind: "deadline", Title: title, Body: body, Data: data}
}

// docsDeadlinesFound offers (or plans) the deadlines of changed documents.
func (s *Server) docsDeadlinesFound(u auth.User, mine agentDocsMine, st *docsState, changed []docindex.Doc, now time.Time) {
	for _, doc := range changed {
		for _, d := range docindex.Deadlines(doc.Pages, now) {
			s.docsOffer(u, mine, st, deadlineItem(doc.Root, doc.Path, d.Page, d, "", ""))
		}
	}
}

// docsOffer puts one deadline into Vorschläge, or plans it right away.
func (s *Server) docsOffer(u auth.User, mine agentDocsMine, st *docsState, item agentInboxItem) {
	key := item.Data["root"] + "|" + item.Data["path"] + "|" + item.Data["deadline"] + "|" + item.Data["date"]
	if slices.Contains(st.Proposed, key) {
		return
	}
	st.Proposed = append(st.Proposed, key)
	if len(st.Proposed) > maxProposed {
		st.Proposed = st.Proposed[len(st.Proposed)-maxProposed:]
	}
	if mine.Mode == "remind" {
		if err := s.docsRemind(u.Name, item, mine.lead(), time.Now().In(s.agentLocation())); err != nil {
			slog.Warn("deadline not planned", "account", u.Name, "err", err)
		}
		return
	}
	s.addAgentInbox(u.Name, item)
}

// docsAcceptDeadline is "Übernehmen" of a deadline suggestion (from
// applyAgentInbox). On failure the suggestion comes back.
func (s *Server) docsAcceptDeadline(user string, item agentInboxItem) error {
	m, err := s.agentFeaturesOf(user)
	if err != nil {
		return err
	}
	if err := s.docsRemind(user, item, m.Docs.lead(), time.Now().In(s.agentLocation())); err != nil {
		s.addAgentInbox(user, item)
		return err
	}
	return nil
}

// docsRemind plans reminders lead days before the date at 09:00 (those
// already past are left out); with none left, one on the day itself.
func (s *Server) docsRemind(user string, item agentInboxItem, lead []int, now time.Time) error {
	loc := s.agentLocation()
	date, err := time.ParseInLocation("2006-01-02", item.Data["date"], loc)
	if err != nil {
		return errors.New("the deadline has no date")
	}
	at := func(days int) time.Time {
		d := date.AddDate(0, 0, -days)
		return time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc)
	}
	var times []time.Time
	var leads []int
	for _, l := range lead {
		if t := at(l); t.After(now) {
			times, leads = append(times, t), append(leads, l)
		}
	}
	if len(times) == 0 {
		if t := at(0); t.After(now) {
			times, leads = []time.Time{t}, []int{0}
		} else if date.AddDate(0, 0, 1).After(now) {
			// Today is the day and 09:00 has passed: in an hour.
			times, leads = []time.Time{now.Add(time.Hour).Truncate(time.Minute)}, []int{0}
		} else {
			return toolcatalog.Errorf("the deadline %s has passed already", date.Format("02.01.2006"))
		}
	}
	store := s.agentStore()
	list, err := store.Schedules(user)
	if err != nil {
		return err
	}
	if len(list)+len(times) > maxSchedules {
		return toolcatalog.Errorf("there are already %d planned tasks; delete some under Geplant", len(list))
	}
	name := "Frist: " + item.Title
	if r := []rune(name); len(r) > 80 {
		name = string(r[:79]) + "…"
	}
	for i, t := range times {
		task := item.Title
		switch {
		case leads[i] == 1:
			task += " – morgen."
		case leads[i] > 1:
			task += fmt.Sprintf(" – noch %d Tage.", leads[i])
		default:
			task += " – heute."
		}
		if item.Body != "" {
			task += "\n" + item.Body
		}
		if r := []rune(task); len(r) > 4000 {
			task = string(r[:3999]) + "…"
		}
		sc := agent.Schedule{ID: agent.NewID(), Name: name, Task: task, Kind: "reminder", Every: "once", Date: t.Format("2006-01-02"), At: t.Format("15:04"), Enabled: true, Created: now}
		if err := checkSchedule(&sc); err != nil {
			return err
		}
		list = append(list, sc)
	}
	return store.SaveSchedules(user, list)
}
