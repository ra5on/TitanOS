package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/docindex"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/imapmini"
	"github.com/ra5on/TitanOS/backend/internal/toolcatalog"
)

// Papierkram-Autopilot (PLAN 54-D items 3, 4, 6, 7): what lands in
// KI-Agent/<konto>/Eingang (scanner over SMB, dragged in) or comes by the
// account's own mailbox is read, recognised (sender, date, sum, due date,
// type), named "2026-11-03 Stadtwerke Abschlag.pdf" and filed, or kept for
// "Zuordnen" when unsure. Every step is in the log with "Rückgängig". The
// mailbox is only read: mails are marked \Seen, never moved or deleted.

const (
	paperFile       = "paper.json"
	maxPaperLog     = 16 << 20
	maxPaperEntries = 3000
	maxPaperSeen    = 3000
	paperMailBatch  = 20
	maxPaperSkip    = 500
	paperSettle     = time.Minute // a scanner may still be writing
)

type paperEntry struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
	// State: filed, ask (Zuordnen), waiting (scan without picture model).
	State string `json:"state"`
	// Origin: inbox (Eingang), mail (attachment), mailtext.
	Origin   string        `json:"origin"`
	Original string        `json:"original"` // path in the KI-Agent folder, Eingang/…
	Root     string        `json:"root"`     // where it is now
	Path     string        `json:"path"`
	Info     docindex.Info `json:"info"`
	Paid     bool          `json:"paid,omitempty"`
	Subject  string        `json:"subject,omitempty"`
	Error    string        `json:"error,omitempty"`
	// Offered: the due date went to the deadline watcher once.
	Offered bool `json:"offered,omitempty"`
}

type paperHint struct {
	Origin   string    `json:"origin"`
	FromName string    `json:"fromName,omitempty"`
	From     string    `json:"from,omitempty"`
	Subject  string    `json:"subject,omitempty"`
	Date     time.Time `json:"date,omitzero"`
}

type paperLog struct {
	Entries []paperEntry `json:"entries"`
	Seen    []string     `json:"seen,omitempty"`
	// Skip: mails left alone (too large, unreadable), as uidvalidity:uid.
	Skip      []string             `json:"skip,omitempty"`
	Hints     map[string]paperHint `json:"hints,omitempty"`
	MailAt    time.Time            `json:"mailAt,omitzero"`
	MailError string               `json:"mailError,omitempty"`
}

func (s *Server) paperLoad(user string) (paperLog, error) {
	var lg paperLog
	err := s.docsRead(user, paperFile, &lg, maxPaperLog)
	if lg.Hints == nil {
		lg.Hints = map[string]paperHint{}
	}
	return lg, err
}

func (s *Server) paperSave(user string, lg *paperLog) error {
	if len(lg.Entries) > maxPaperEntries {
		lg.Entries = lg.Entries[len(lg.Entries)-maxPaperEntries:]
	}
	if len(lg.Seen) > maxPaperSeen {
		lg.Seen = lg.Seen[len(lg.Seen)-maxPaperSeen:]
	}
	return s.docsWrite(user, paperFile, lg)
}

var paperExts = []string{".pdf", ".jpg", ".jpeg", ".png", ".webp", ".md", ".txt", ".docx"}

// paperIMAP lets tests reach their fake server (certificate, address).
var paperIMAP = func(c *imapmini.Config) {}

// paperInboxRoot is the account's KI-Agent folder, with the inbox folder
// made when it is missing.
func (s *Server) paperInboxRoot(u auth.User) (files.Root, bool) {
	root, ok := s.docsShareRoot(u, "share/"+agentShareName)
	if !ok || !root.Writable {
		return files.Root{}, false
	}
	return root, true
}

func (s *Server) paperStartRun(u auth.User, m agentFeaturesMine, mail bool, at time.Time) bool {
	rt := s.docsRT(u.Name)
	rt.mu.Lock()
	if rt.paperRun {
		rt.mu.Unlock()
		return false
	}
	rt.paperRun = true
	rt.mu.Unlock()
	go func() {
		// A bad document or mail must never take titand down.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("paperwork run failed", "account", u.Name, "panic", r)
			}
		}()
		defer func() {
			rt.mu.Lock()
			rt.paperRun = false
			rt.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := s.paperRun(ctx, u, m, mail, at); err != nil {
			slog.Warn("paperwork not done", "account", u.Name, "err", err)
		}
	}()
	return true
}

// paperRun fetches new mail (when mail), then looks at every file in the
// inbox folder. The disk of the KI-Agent folder must be awake.
func (s *Server) paperRun(ctx context.Context, u auth.User, m agentFeaturesMine, mail bool, now time.Time) error {
	env := s.agentEnv()
	root, ok := s.paperInboxRoot(u)
	if !ok {
		return errors.New("the share KI-Agent is not there")
	}
	if s.agentAsleep(ctx, u, root.ID) {
		return nil
	}
	if err := env.files.Mkdir(root, paperInbox, u.UID); err != nil && !errors.Is(err, files.ErrExists) {
		return err
	}
	rt := s.docsRT(u.Name)
	rt.paperMu.Lock()
	defer rt.paperMu.Unlock()
	lg, err := s.paperLoad(u.Name)
	if err != nil {
		return err
	}
	// Written only when something changed: a run every ten minutes must
	// not keep the disk from its standby (PLAN 53).
	before := paperPrint(lg)
	if mail && m.Paper.Mail.Enabled {
		if err := s.paperFetchMail(ctx, u, m.Paper, root, &lg, now); err != nil {
			lg.MailError = err.Error()
		} else {
			lg.MailError = ""
		}
		rt.mu.Lock()
		rt.mailAt = now // in RAM only
		rt.mu.Unlock()
		if p := paperPrint(lg); p != before {
			if err := s.paperSave(u.Name, &lg); err != nil {
				return err
			}
			before = p
		}
	}
	entries, err := env.files.List(root, paperInbox)
	if err != nil {
		return err
	}
	nas := s.agentSettings().Features.Docs
	scans := docsMaxScans
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		p := path.Join(paperInbox, e.Name)
		if e.Dir || strings.HasPrefix(e.Name, ".") || strings.HasPrefix(e.Name, "~$") || !slices.Contains(paperExts, strings.ToLower(path.Ext(e.Name))) || e.Size > docsMaxFile {
			continue
		}
		hint, fromMail := lg.Hints[p]
		if !fromMail && now.Sub(e.Modified) < paperSettle {
			continue
		}
		i := slices.IndexFunc(lg.Entries, func(x paperEntry) bool { return x.Root == root.ID && x.Path == p && x.State != "filed" })
		if i >= 0 && lg.Entries[i].State == "ask" {
			continue // waits for the person
		}
		entry := paperEntry{ID: agent.NewID(), At: now, Origin: "inbox", Original: p, Root: root.ID, Path: p}
		if fromMail {
			entry.Origin, entry.Subject = hint.Origin, hint.Subject
		}
		if i >= 0 {
			entry = lg.Entries[i]
		}
		data, err := s.docsReadFile(root, p)
		if err != nil {
			continue
		}
		pages, _, err := s.docsExtract(ctx, e.Name, data, !s.agentBusy(), &scans)
		if errors.Is(err, errDocsWait) {
			entry.State = "waiting"
			lg.Entries = paperPut(lg.Entries, entry)
			continue
		}
		var text strings.Builder
		for _, pg := range pages {
			text.WriteString(pg.Text + "\n")
		}
		entry.Info = docindex.Recognize(text.String(), docindex.Hint{Name: e.Name, FromName: hint.FromName, From: hint.From, Subject: hint.Subject, Date: firstTime(hint.Date, e.Modified)}, now)
		delete(lg.Hints, p)
		entry.State, entry.Error = "ask", ""
		if m.Paper.filing() == "auto" && entry.Info.Sure {
			if err := s.paperFile(u, root, &entry, data, m.Paper); err != nil {
				entry.Error = err.Error()
			}
		}
		if entry.State == "filed" {
			entry.Offered = s.paperDue(u, m, nas, entry) || entry.Offered
		}
		lg.Entries = paperPut(lg.Entries, entry)
	}
	if paperPrint(lg) == before {
		return nil
	}
	return s.paperSave(u.Name, &lg)
}

// paperPrint is the log as it would be written, to see whether it changed.
func paperPrint(lg paperLog) string {
	lg.MailAt = time.Time{}
	data, _ := json.Marshal(lg)
	return string(data)
}

// paperMailAt is when the mailbox was last looked at (RAM, else the
// time an older version wrote).
func (s *Server) paperMailAt(user string, lg paperLog) time.Time {
	rt := s.docsRT(user)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.mailAt.IsZero() {
		return rt.mailAt
	}
	return lg.MailAt
}

func firstTime(a, b time.Time) time.Time {
	if !a.IsZero() {
		return a
	}
	return b
}

func paperPut(list []paperEntry, e paperEntry) []paperEntry {
	if i := slices.IndexFunc(list, func(x paperEntry) bool { return x.ID == e.ID }); i >= 0 {
		list[i] = e
		return list
	}
	return append(list, e)
}

// paperDue offers the due date of a filed bill to the deadline watcher.
func (s *Server) paperDue(u auth.User, m agentFeaturesMine, nas agentDocsNAS, e paperEntry) bool {
	if e.Offered || !nas.Deadlines || !m.Docs.Deadlines || e.Info.Due.IsZero() || e.Info.Due.Before(dayStart(time.Now().In(s.agentLocation()))) {
		return false
	}
	d := docindex.Deadline{Kind: docindex.KindPayment, Date: e.Info.Due, Cents: e.Info.Cents, Debit: e.Info.Debit}
	item := deadlineItem(e.Root, e.Path, 0, d, e.Info.Sender, e.ID)
	if e.Origin == "mailtext" && e.Subject != "" {
		item.Body = "Aus der Mail „" + e.Subject + "“"
	}
	var st docsState
	s.docsOffer(u, m.Docs, &st, item)
	return true
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

var paperTypeFolder = map[string]string{
	docindex.TypeEnergy: "Strom und Gas", docindex.TypeTelecom: "Internet und Handy", docindex.TypeInsurance: "Versicherungen",
	docindex.TypeShopping: "Einkäufe", docindex.TypeHealth: "Gesundheit", docindex.TypeHousing: "Wohnen", docindex.TypeTax: "Steuern und Ämter",
	docindex.TypeOther: "Sonstiges",
}

// paperFile names and files one document of the inbox folder.
func (s *Server) paperFile(u auth.User, src files.Root, e *paperEntry, data []byte, p agentPaperMine) error {
	t := p.target()
	dst, ok := s.docsShareRoot(u, t.Root)
	if !ok || !dst.Writable {
		return errors.New("no write access to the filing folder")
	}
	sender := docindex.SafeName(e.Info.Sender)
	if sender == "" {
		sender = "Unbekannt"
	}
	sub := sender
	switch p.scheme() {
	case "type":
		sub = path.Join(paperTypeFolder[e.Info.Type], sender)
	case "year":
		year := strconv.Itoa(e.At.Year())
		if !e.Info.Date.IsZero() {
			year = strconv.Itoa(e.Info.Date.Year())
		}
		sub = path.Join(year, sender)
	}
	dir := path.Join(t.Path, sub)
	if err := s.paperMkdirAll(dst, dir, u.UID); err != nil {
		return err
	}
	name := docindex.FileName(e.Info, path.Ext(e.Path))
	to, err := s.paperFree(dst, dir, name)
	if err != nil {
		return err
	}
	if err := s.paperMove(u, src, e.Path, dst, to, data); err != nil {
		return err
	}
	e.Root, e.Path, e.State, e.Error = dst.ID, to, "filed", ""
	return nil
}

func (s *Server) paperMkdirAll(root files.Root, dir string, uid int) error {
	env := s.agentEnv()
	if dir == "" || dir == "." {
		return nil
	}
	parts := strings.Split(dir, "/")
	for i := range parts {
		if err := env.files.Mkdir(root, strings.Join(parts[:i+1], "/"), uid); err != nil && !errors.Is(err, files.ErrExists) {
			return err
		}
	}
	return nil
}

// paperFree finds a name in dir that is not taken: "x.pdf", "x (2).pdf".
func (s *Server) paperFree(root files.Root, dir, name string) (string, error) {
	env := s.agentEnv()
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 1; n < 100; n++ {
		cand := name
		if n > 1 {
			cand = fmt.Sprintf("%s (%d)%s", base, n, ext)
		}
		p := path.Join(dir, cand)
		f, _, err := env.files.Open(root, p)
		if errors.Is(err, files.ErrNotFound) {
			return p, nil
		}
		if err == nil {
			f.Close()
		}
	}
	return "", errors.New("no free name")
}

// paperMove moves a file within a root (rename) or across roots (write the
// copy, then remove the source, into the recycle bin where there is one).
func (s *Server) paperMove(u auth.User, src files.Root, from string, dst files.Root, to string, data []byte) error {
	env := s.agentEnv()
	if src.ID == dst.ID {
		return env.files.Rename(src, from, to)
	}
	if data == nil {
		var err error
		if data, err = s.docsReadFile(src, from); err != nil {
			return err
		}
	}
	if err := env.files.WriteFile(dst, to, data, u.UID, false); err != nil {
		return err
	}
	f, info, err := env.files.Open(dst, to)
	if err != nil {
		return err
	}
	f.Close()
	if info.Size() != int64(len(data)) {
		return errors.New("the copy is not complete")
	}
	return env.files.Delete(src, []string{from}, u.Name, !src.Recycle)
}

// paperFetchMail takes new mails of the account's mailbox into the inbox
// folder: PDF and picture attachments, and the mail text as Markdown.
// Everything is written first; only then the mail is marked read.
func (s *Server) paperFetchMail(ctx context.Context, u auth.User, p agentPaperMine, root files.Root, lg *paperLog, now time.Time) error {
	m := p.Mail
	sec := m.Security
	if sec == "" {
		sec = imapmini.TLS
	}
	cfg := imapmini.Config{Host: m.Host, Port: m.port(), Security: sec, User: m.User, Password: m.Password}
	paperIMAP(&cfg)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var c *imapmini.Client
	connect := func() error {
		if c != nil {
			_ = c.Close() // best effort, the run goes on with a new connection
		}
		var err error
		if c, err = imapmini.Dial(ctx, cfg); err != nil {
			c = nil
			return err
		}
		return c.SelectInbox()
	}
	if err := connect(); err != nil {
		return err
	}
	defer func() {
		if c != nil {
			_ = c.Close() // best effort
		}
	}()
	uids, err := c.Unseen()
	if err != nil {
		return err
	}
	// Mails that are too large or cannot be read are left alone (unread,
	// for the person) and never block the ones after them.
	key := func(uid uint32) string { return c.UIDValidity + ":" + strconv.FormatUint(uint64(uid), 10) }
	uids = slices.DeleteFunc(uids, func(uid uint32) bool { return slices.Contains(lg.Skip, key(uid)) })
	if len(uids) > paperMailBatch {
		uids = uids[:paperMailBatch]
	}
	skip := func(uid uint32) {
		lg.Skip = append(lg.Skip, key(uid))
		if len(lg.Skip) > maxPaperSkip {
			lg.Skip = lg.Skip[len(lg.Skip)-maxPaperSkip:]
		}
	}
	reconnects := 0
	for _, uid := range uids {
		raw, err := s.paperFetchOne(c, uid)
		if err != nil {
			if !errors.Is(err, imapmini.ErrBroken) && !errors.Is(err, imapmini.ErrTooLarge) && !errors.Is(err, errPaperTooLarge) {
				return err // network or server: tried again next time
			}
			skip(uid)
			if errors.Is(err, errPaperTooLarge) {
				continue // asked by size: the stream is fine
			}
			// The stream is out of step: connect anew, go on with the next.
			if reconnects++; reconnects > 3 {
				return err
			}
			if err := connect(); err != nil {
				return err
			}
			continue
		}
		mail, perr := imapmini.Parse(raw)
		if perr != nil {
			skip(uid)
			continue
		}
		id := mail.MessageID
		if id == "" {
			sum := sha256.Sum256(raw)
			id = "sha256:" + hex.EncodeToString(sum[:12])
		}
		if !slices.Contains(lg.Seen, id) {
			if err := s.paperTakeMail(u, p, root, lg, mail, now); err != nil {
				return err
			}
			lg.Seen = append(lg.Seen, id)
			// Durable before the mailbox hears of it.
			if err := s.paperSave(u.Name, lg); err != nil {
				return err
			}
		}
		if err := c.MarkSeen(uid); err != nil {
			return err
		}
	}
	return nil
}

var errPaperTooLarge = errors.New("mail too large")

// paperFetchOne asks the size first, so a mail over the limit is never
// fetched at all.
func (s *Server) paperFetchOne(c *imapmini.Client, uid uint32) ([]byte, error) {
	size, err := c.Size(uid)
	if err != nil {
		return nil, err
	}
	if size > imapmini.MaxMessage {
		return nil, errPaperTooLarge
	}
	return c.Fetch(uid)
}

func (s *Server) paperTakeMail(u auth.User, p agentPaperMine, root files.Root, lg *paperLog, mail imapmini.Mail, now time.Time) error {
	env := s.agentEnv()
	date := mail.Date
	if date.IsZero() {
		date = now
	}
	prefix := date.In(s.agentLocation()).Format("2006-01-02")
	hint := paperHint{Origin: "mail", FromName: mail.FromName, From: mail.From, Subject: mail.Subject, Date: date}
	for _, a := range mail.Attachments {
		ext := strings.ToLower(path.Ext(a.Name))
		if ext != ".pdf" && !docsImage(a.Name) {
			continue
		}
		to, err := s.paperFree(root, paperInbox, docindex.SafeName(prefix+" "+a.Name))
		if err != nil {
			return err
		}
		if err := env.files.WriteFile(root, to, a.Data, u.UID, false); err != nil {
			return err
		}
		lg.Hints[to] = hint
	}
	if p.Mail.Mode != "attachments" && strings.TrimSpace(mail.Text) != "" {
		subject := docindex.SafeName(mail.Subject)
		if subject == "" {
			subject = "Mail"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n- Von: %s <%s>\n- Datum: %s\n\n%s\n", mail.Subject, mail.FromName, mail.From, date.In(s.agentLocation()).Format("02.01.2006 15:04"), mail.Text)
		to, err := s.paperFree(root, paperInbox, prefix+" "+subject+".md")
		if err != nil {
			return err
		}
		if err := env.files.WriteFile(root, to, []byte(b.String()), u.UID, false); err != nil {
			return err
		}
		h := hint
		h.Origin = "mailtext"
		lg.Hints[to] = h
	}
	return nil
}

// ---------- the page ----------

type paperInboxInfo struct {
	Folder    string    `json:"folder"`
	Waiting   int       `json:"waiting"`
	Ask       int       `json:"ask"`
	MailAt    time.Time `json:"mailAt,omitzero"`
	MailError string    `json:"mailError,omitempty"`
}

func (s *Server) paperInboxView(u auth.User, p agentPaperMine) *paperInboxInfo {
	if !s.agentSettings().Features.Docs.Paper || !p.On {
		return nil
	}
	lg, err := s.paperLoad(u.Name)
	if err != nil {
		return nil
	}
	out := &paperInboxInfo{Folder: agentShareName + " › " + paperInbox, MailAt: s.paperMailAt(u.Name, lg), MailError: lg.MailError}
	for _, e := range lg.Entries {
		switch e.State {
		case "waiting":
			out.Waiting++
		case "ask":
			out.Ask++
		}
	}
	return out
}

func (s *Server) paperOn(w http.ResponseWriter, r *http.Request) (auth.User, agentFeaturesMine, bool) {
	user := currentUser(r)
	nas := s.agentSettings().Features.Docs
	if !nas.Enabled || !nas.Paper {
		docsOff(w)
		return user, agentFeaturesMine{}, false
	}
	m, err := s.agentFeaturesOf(user.Name)
	if err != nil {
		agentStoreError(w, err)
		return user, m, false
	}
	if !m.Paper.On {
		writeError(w, http.StatusConflict, "paper_off", "the paperwork autopilot is switched off for this account")
		return user, m, false
	}
	return user, m, true
}

type paperType struct {
	Type    string   `json:"type"`
	Cents   int64    `json:"cents"`
	Count   int      `json:"count"`
	Senders []string `json:"senders"`
}

type paperView struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	State   string    `json:"state"`
	Origin  string    `json:"origin"`
	To      string    `json:"to,omitempty"`
	Root    string    `json:"root"`
	Path    string    `json:"path"`
	At      time.Time `json:"at"`
	Sender  string    `json:"sender,omitempty"`
	Type    string    `json:"type"`
	Kind    string    `json:"kind,omitempty"`
	Cents   int64     `json:"cents,omitempty"`
	Due     string    `json:"due,omitempty"`
	Doubt   []string  `json:"doubt,omitempty"`
	Paid    bool      `json:"paid,omitempty"`
	Error   string    `json:"error,omitempty"`
	Subject string    `json:"subject,omitempty"`
}

func viewOf(e paperEntry) paperView {
	v := paperView{ID: e.ID, Name: path.Base(e.Path), State: e.State, Origin: e.Origin, Root: e.Root, Path: e.Path, At: e.At, Sender: e.Info.Sender,
		Type: e.Info.Type, Kind: e.Info.Kind, Cents: e.Info.Cents, Doubt: e.Info.Doubt, Paid: e.Paid, Error: e.Error, Subject: e.Subject}
	if e.State == "filed" {
		v.To = docsPlace(e.Root, e.Path)
	}
	if !e.Info.Due.IsZero() {
		v.Due = e.Info.Due.Format("2006-01-02")
	}
	return v
}

// paperSummary is the overview of a month (2026-11).
func paperSummary(lg paperLog, month string, now time.Time) map[string]any {
	cur, err := time.ParseInLocation("2006-01", month, now.Location())
	if err != nil {
		cur = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}
	prev := cur.AddDate(0, -1, 0)
	inMonth := func(e paperEntry, m time.Time) bool {
		d := e.Info.Date
		if d.IsZero() {
			d = e.At.In(now.Location())
		}
		return e.State == "filed" && d.Year() == m.Year() && d.Month() == m.Month()
	}
	var total, prevTotal int64
	count := 0
	byType := map[string]*paperType{}
	for _, e := range lg.Entries {
		if inMonth(e, prev) {
			prevTotal += e.Info.Cents
		}
		if !inMonth(e, cur) {
			continue
		}
		total += e.Info.Cents
		count++
		t := byType[e.Info.Type]
		if t == nil {
			t = &paperType{Type: e.Info.Type, Senders: []string{}}
			byType[e.Info.Type] = t
		}
		t.Cents += e.Info.Cents
		t.Count++
		if e.Info.Sender != "" && !slices.Contains(t.Senders, e.Info.Sender) && len(t.Senders) < 3 {
			t.Senders = append(t.Senders, e.Info.Sender)
		}
	}
	types := []paperType{}
	for _, k := range docindex.Types {
		if t := byType[k]; t != nil {
			types = append(types, *t)
		}
	}
	open := []paperView{}
	today := dayStart(now)
	for _, e := range lg.Entries {
		if e.State == "filed" && !e.Paid && !e.Info.Due.IsZero() && !e.Info.Due.Before(today) && e.Info.Cents > 0 {
			open = append(open, viewOf(e))
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].Due < open[j].Due })
	recent := []paperView{}
	for i := len(lg.Entries) - 1; i >= 0 && len(recent) < 30; i-- {
		recent = append(recent, viewOf(lg.Entries[i]))
	}
	months := []string{}
	for i := 2; i >= 0; i-- {
		months = append(months, cur.AddDate(0, -i, 0).Format("2006-01"))
	}
	return map[string]any{"month": cur.Format("2006-01"), "months": months, "total": total, "count": count, "prevTotal": prevTotal,
		"types": types, "open": open, "recent": recent, "mailAt": lg.MailAt, "mailError": lg.MailError}
}

func (s *Server) handlePaper(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.paperOn(w, r)
	if !ok {
		return
	}
	lg, err := s.paperLoad(user.Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	lg.MailAt = s.paperMailAt(user.Name, lg)
	writeJSON(w, http.StatusOK, paperSummary(lg, r.URL.Query().Get("month"), time.Now().In(s.agentLocation())))
}

func (s *Server) handlePaperRun(w http.ResponseWriter, r *http.Request) {
	user, m, ok := s.paperOn(w, r)
	if !ok {
		return
	}
	if !s.paperStartRun(user, m, true, time.Now().In(s.agentLocation())) {
		writeError(w, http.StatusConflict, "paper_running", "the inbox is being looked at already")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

func (s *Server) handlePaperMailTest(w http.ResponseWriter, r *http.Request) {
	_, m, ok := s.paperOn(w, r)
	if !ok {
		return
	}
	p := m.Paper.Mail
	if p.Host == "" || p.User == "" || p.Password == "" {
		writeError(w, http.StatusBadRequest, "mail_incomplete", "save server, user and password first")
		return
	}
	sec := p.Security
	if sec == "" {
		sec = imapmini.TLS
	}
	cfg := imapmini.Config{Host: p.Host, Port: p.port(), Security: sec, User: p.User, Password: p.Password}
	paperIMAP(&cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c, err := imapmini.Dial(ctx, cfg)
	if err == nil {
		err = c.SelectInbox()
		_ = c.Close() // best effort
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "mail_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// paperChange runs a change of one log entry under the log's lock.
func (s *Server) paperChange(w http.ResponseWriter, r *http.Request, change func(u auth.User, m agentFeaturesMine, lg *paperLog, e *paperEntry) error) {
	user, m, ok := s.paperOn(w, r)
	if !ok {
		return
	}
	rt := s.docsRT(user.Name)
	if !rt.paperMu.TryLock() {
		writeError(w, http.StatusConflict, "paper_running", "the inbox is being looked at; try again in a moment")
		return
	}
	defer rt.paperMu.Unlock()
	lg, err := s.paperLoad(user.Name)
	if err != nil {
		agentStoreError(w, err)
		return
	}
	i := slices.IndexFunc(lg.Entries, func(e paperEntry) bool { return e.ID == r.PathValue("id") })
	if i < 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such entry")
		return
	}
	e := lg.Entries[i]
	if err := change(user, m, &lg, &e); err != nil {
		writeError(w, http.StatusBadRequest, "paper_failed", err.Error())
		return
	}
	lg.Entries[i] = e
	if err := s.paperSave(user.Name, &lg); err != nil {
		agentStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(e))
}

// handlePaperUndo moves a filed document back into the inbox folder,
// where it waits for "Zuordnen".
func (s *Server) handlePaperUndo(w http.ResponseWriter, r *http.Request) {
	s.paperChange(w, r, func(u auth.User, _ agentFeaturesMine, _ *paperLog, e *paperEntry) error {
		if e.State != "filed" {
			return errors.New("this document is not filed")
		}
		inbox, ok := s.paperInboxRoot(u)
		if !ok {
			return errors.New("the share KI-Agent is not there")
		}
		from, ok := s.docsShareRoot(u, e.Root)
		if !ok {
			return errors.New("no access to " + e.Root)
		}
		to, err := s.paperFree(inbox, paperInbox, path.Base(e.Original))
		if err != nil {
			return err
		}
		if err := s.paperMove(u, from, e.Path, inbox, to, nil); err != nil {
			return err
		}
		e.Root, e.Path, e.State = inbox.ID, to, "ask"
		return nil
	})
}

// handlePaperAssign is "Zuordnen": the person names sender and type, the
// document is filed with them.
func (s *Server) handlePaperAssign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sender string `json:"sender"`
		Type   string `json:"type"`
		Kind   string `json:"kind"`
		Date   string `json:"date"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	s.paperChange(w, r, func(u auth.User, m agentFeaturesMine, _ *paperLog, e *paperEntry) error {
		if e.State != "ask" && e.State != "waiting" {
			return errors.New("this document is filed already")
		}
		sender := docindex.SafeName(req.Sender)
		if sender == "" || !slices.Contains(docindex.Types, req.Type) {
			return errors.New("give a sender and a type")
		}
		e.Info.Sender, e.Info.Type, e.Info.Doubt, e.Info.Sure = sender, req.Type, nil, true
		if k := docindex.SafeName(req.Kind); k != "" {
			e.Info.Kind = k
		}
		if req.Date != "" {
			d, err := time.ParseInLocation("2006-01-02", req.Date, s.agentLocation())
			if err != nil {
				return errors.New("the date is like 2026-11-03")
			}
			e.Info.Date = d
		}
		if e.Info.Date.IsZero() {
			e.Info.Date = dayStart(e.At.In(s.agentLocation()))
		}
		inbox, ok := s.paperInboxRoot(u)
		if !ok || e.Root != inbox.ID {
			return errors.New("the document is not in the inbox folder")
		}
		if err := s.paperFile(u, inbox, e, nil, m.Paper); err != nil {
			return err
		}
		e.Offered = s.paperDue(u, m, s.agentSettings().Features.Docs, *e) || e.Offered
		return nil
	})
}

func (s *Server) handlePaperPaid(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paid bool `json:"paid"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	s.paperChange(w, r, func(_ auth.User, _ agentFeaturesMine, _ *paperLog, e *paperEntry) error {
		e.Paid = req.Paid
		return nil
	})
}

// ---------- the agent's tool ----------

func (b *agentToolbox) paperSummaryTool() *agentTool {
	return &agentTool{name: "paperwork_summary", area: toolcatalog.AreaFiles, kind: agent.KindRead, untrusted: true,
		description: "The person's paperwork overview (Papierkram): spending of a month by type and sender, open payments with due dates, recently filed documents. Use it for \"Was habe ich im November ausgegeben?\" or \"Welche Rechnungen sind noch offen?\".",
		params:      schema(map[string]any{"month": str("Month like 2026-11; empty for the current one.")}),
		own:         b.paperSummary}
}

func (b *agentToolbox) paperSummary(_ context.Context, _ *agent.Chat, args json.RawMessage) (agent.Outcome, error) {
	var req struct {
		Month string `json:"month"`
	}
	_ = json.Unmarshal(args, &req)
	lg, err := b.s.paperLoad(b.user.Name)
	if err != nil {
		return agent.Outcome{}, err
	}
	sum := paperSummary(lg, req.Month, time.Now().In(b.s.agentLocation()))
	delete(sum, "mailError")
	if r, ok := sum["recent"].([]paperView); ok && len(r) > 10 {
		sum["recent"] = r[:10]
	}
	sum["note"] = "Sums are in cents. Values were read from documents automatically; say so when they matter."
	data, _ := json.Marshal(sum)
	return agent.Outcome{Text: string(data)}, nil
}
