package api

import (
	"errors"
	"net"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/files"
)

// Gruppe „Dokumente und Fristen“ (PLAN 54-D): Dokumenten-Gedächtnis,
// Fristenwächter, Papierkram-Autopilot. Every part is off by default:
// owners and administrators switch it on for the NAS (agentDocsNAS), then
// every account switches it on for itself and chooses its folders, its
// deadline mode and its mailbox (agentDocsMine, agentPaperMine).
//
//	agent_docs_index.go     reading the folders at night, the index, search
//	agent_docs_deadline.go  deadlines as suggestions or reminders
//	agent_docs_paper.go     the inbox folder, the mailbox, filing, overview
//
// The index and the paperwork log live in .titanos/docs/<account> on the
// agent's volume (root, 0700), never on the stick and never in RAM only.

// agentDocsNAS: whether the functions are there at all.
type agentDocsNAS struct {
	Enabled   bool `json:"enabled,omitempty"`
	Deadlines bool `json:"deadlines,omitempty"`
	Paper     bool `json:"paper,omitempty"`
	// At is when the folders are read each night (default 03:00).
	At string `json:"at,omitempty"`
}

func (d agentDocsNAS) view() agentDocsNAS { return d }

// agentDocsMine: an account's document memory and deadline watcher.
type agentDocsMine struct {
	On bool `json:"on,omitempty"`
	// Folders, when Custom: chosen folders of shares the account can read.
	// Without, the account's own KI-Agent/<konto>/Dokumente.
	Custom  bool             `json:"custom,omitempty"`
	Folders []agentFolderRef `json:"folders,omitempty"`
	// Deadlines: the account lets the watcher find deadlines.
	Deadlines bool `json:"deadlines,omitempty"`
	// Mode: "suggest" (default, Erst vorschlagen) or "remind" (Gleich als
	// Erinnerung).
	Mode string `json:"mode,omitempty"`
	// Lead: days before a deadline to remind (default 14 and 2).
	Lead []int `json:"lead,omitempty"`
	// NoScans: scans and photos are not given to the picture model.
	NoScans bool `json:"noScans,omitempty"`
}

func (d agentDocsMine) view() agentDocsMine { return d }

// agentPaperMine: an account's paperwork autopilot and its mailbox.
type agentPaperMine struct {
	On bool `json:"on,omitempty"`
	// Filing: "auto" (default, selbst ablegen) or "ask" (jedes Mal
	// vorschlagen).
	Filing string `json:"filing,omitempty"`
	// Scheme of folders below Target: "sender" (default), "type", "year".
	Scheme string `json:"scheme,omitempty"`
	// Target: where filed documents go (default KI-Agent Dokumente/Ablage).
	Target agentFolderRef `json:"target"`
	Mail   agentPaperMail `json:"mail"`
}

// agentPaperMail is the account's own mailbox for bills.
type agentPaperMail struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Security string `json:"security,omitempty"` // tls (default) or starttls
	User     string `json:"user,omitempty"`
	// Password is a secret: never sent to the page, kept when empty.
	Password    string `json:"password,omitempty"`
	PasswordSet bool   `json:"passwordSet,omitempty"`
	// Mode: "both" (default, Anhänge und Mailtext) or "attachments".
	Mode string `json:"mode,omitempty"`
}

func (p agentPaperMine) view() agentPaperMine {
	p.Mail.PasswordSet = p.Mail.Password != ""
	p.Mail.Password = ""
	return p
}

const (
	docsDefaultFolder = "Dokumente"
	paperInbox        = "Eingang"
	paperDefaultDir   = "Dokumente/Ablage"
	docsMaxFolders    = 20
)

func (s *Server) checkDocsNAS(d *agentDocsNAS, old agentDocsNAS) error {
	d.At = strings.TrimSpace(d.At)
	if d.At != "" && !clockRe.MatchString(d.At) {
		return errors.New("the time of the night run is like 03:00")
	}
	return nil
}

func (d agentDocsNAS) at() string {
	if d.At == "" {
		return "03:00"
	}
	return d.At
}

// cleanFolderPath makes a relative folder path below a root; hidden and
// private places are refused.
func cleanFolderPath(p string) (string, error) {
	p = strings.Trim(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "/")
	if p == "" {
		return "", nil
	}
	c := path.Clean(p)
	if c != p || c == "." || strings.HasPrefix(c, "../") || c == ".." || hiddenPath(c) || len(c) > 400 {
		return "", errors.New("invalid folder path")
	}
	for _, part := range strings.Split(c, "/") {
		if strings.HasPrefix(part, ".") {
			return "", errors.New("hidden folders are not read")
		}
	}
	return c, nil
}

// docsShareRoot is a share the account can read now (the brief: only
// shares, never volumes, USB or the stick).
func (s *Server) docsShareRoot(user auth.User, id string) (files.Root, bool) {
	if s.opts.Shares == nil || s.opts.Storage == nil {
		return files.Root{}, false
	}
	for _, r := range s.fileRoots(user) {
		if r.ID == id && r.Kind == "share" {
			return r, true
		}
	}
	return files.Root{}, false
}

func (s *Server) docsUser(name string) (auth.User, bool) {
	if s.opts.Users == nil {
		return auth.User{}, false
	}
	return s.opts.Users.Get(name)
}

func (s *Server) checkDocsMine(user string, d *agentDocsMine, old agentDocsMine) error {
	u, ok := s.docsUser(user)
	if !ok {
		return errors.New("unknown account")
	}
	switch d.Mode {
	case "", "suggest", "remind":
	default:
		return errors.New("deadline mode is suggest or remind")
	}
	if len(d.Lead) > 3 {
		return errors.New("at most three reminders before a deadline")
	}
	for _, l := range d.Lead {
		if l < 0 || l > 365 {
			return errors.New("remind 0 to 365 days before")
		}
	}
	slices.Sort(d.Lead)
	slices.Reverse(d.Lead)
	d.Lead = slices.Compact(d.Lead)
	if len(d.Folders) > docsMaxFolders {
		return errors.New("at most 20 folders")
	}
	seen := map[string]bool{}
	out := d.Folders[:0:0]
	for _, f := range d.Folders {
		p, err := cleanFolderPath(f.Path)
		if err != nil {
			return err
		}
		// Rights now, not as the page claims (another account's folder,
		// a share without access, a volume as a whole: refused).
		if _, ok := s.docsShareRoot(u, f.Root); !ok {
			return errors.New("no read access to " + f.Root)
		}
		key := f.Root + "/" + p
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, agentFolderRef{Root: f.Root, Path: p})
	}
	d.Folders = out
	if !d.Custom {
		d.Folders = nil
	}
	return nil
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

func (s *Server) checkPaperMine(user string, p *agentPaperMine, old agentPaperMine) error {
	u, ok := s.docsUser(user)
	if !ok {
		return errors.New("unknown account")
	}
	switch p.Filing {
	case "", "auto", "ask":
	default:
		return errors.New("filing is auto or ask")
	}
	switch p.Scheme {
	case "", "sender", "type", "year":
	default:
		return errors.New("folder scheme is sender, type or year")
	}
	if p.Target.Root != "" || p.Target.Path != "" {
		dir, err := cleanFolderPath(p.Target.Path)
		if err != nil {
			return err
		}
		root, ok := s.docsShareRoot(u, p.Target.Root)
		if !ok || !root.Writable {
			return errors.New("no write access to " + p.Target.Root)
		}
		if p.Target.Root == "share/"+agentShareName && (dir == paperInbox || strings.HasPrefix(dir, paperInbox+"/")) {
			return errors.New("the target cannot be the inbox folder")
		}
		p.Target.Path = dir
	}
	m := &p.Mail
	m.Host, m.User = strings.TrimSpace(m.Host), strings.TrimSpace(m.User)
	m.PasswordSet = false
	if m.Password == "" && m.Host == old.Mail.Host && m.User == old.Mail.User {
		m.Password = old.Mail.Password
	}
	switch m.Security {
	case "", "tls", "starttls":
	default:
		return errors.New("mailbox security is tls or starttls")
	}
	switch m.Mode {
	case "", "both", "attachments":
	default:
		return errors.New("mailbox mode is both or attachments")
	}
	if m.Port < 0 || m.Port > 65535 || len(m.User) > 200 || len(m.Password) > 500 || strings.ContainsAny(m.User+m.Password, "\r\n") {
		return errors.New("invalid mailbox settings")
	}
	if m.Host != "" && (!hostRe.MatchString(m.Host) && net.ParseIP(m.Host) == nil) {
		return errors.New("the mail server is a host name like imap.example.org")
	}
	if m.Enabled && (m.Host == "" || m.User == "" || m.Password == "") {
		return errors.New("the mailbox needs server, user and password")
	}
	return nil
}

func (p agentPaperMine) filing() string {
	if p.Filing == "" {
		return "auto"
	}
	return p.Filing
}

func (p agentPaperMine) scheme() string {
	if p.Scheme == "" {
		return "sender"
	}
	return p.Scheme
}

func (p agentPaperMine) target() agentFolderRef {
	if p.Target.Root == "" {
		return agentFolderRef{Root: "share/" + agentShareName, Path: paperDefaultDir}
	}
	return p.Target
}

func (m agentPaperMail) port() int {
	if m.Port != 0 {
		return m.Port
	}
	if m.Security == "starttls" {
		return 143
	}
	return 993
}

func (d agentDocsMine) folders() []agentFolderRef {
	if !d.Custom {
		return []agentFolderRef{{Root: "share/" + agentShareName, Path: docsDefaultFolder}}
	}
	return d.Folders
}

func (d agentDocsMine) lead() []int {
	if len(d.Lead) == 0 {
		return []int{14, 2}
	}
	return d.Lead
}

// docsOn: the memory is there for the account (NAS and account switched on).
func (s *Server) docsOn(user string) (agentDocsNAS, agentFeaturesMine, bool) {
	nas := s.agentSettings().Features.Docs
	if !nas.Enabled {
		return nas, agentFeaturesMine{}, false
	}
	m, err := s.agentFeaturesOf(user)
	if err != nil {
		return nas, m, false
	}
	return nas, m, true
}

func (s *Server) registerAgentDocs(mux *routeMux) {
	mux.HandleFunc("GET /api/v1/agent/docs", s.requireAgent(s.handleDocsStatus))
	mux.HandleFunc("POST /api/v1/agent/docs/run", s.requireAgent(s.handleDocsRun))
	mux.HandleFunc("GET /api/v1/agent/docs/search", s.requireAgent(s.handleDocsSearch))
	mux.HandleFunc("GET /api/v1/agent/paper", s.requireAgent(s.handlePaper))
	mux.HandleFunc("POST /api/v1/agent/paper/run", s.requireAgent(s.handlePaperRun))
	mux.HandleFunc("POST /api/v1/agent/paper/mail/test", s.requireAgent(s.handlePaperMailTest))
	mux.HandleFunc("POST /api/v1/agent/paper/{id}/undo", s.requireAgent(s.handlePaperUndo))
	mux.HandleFunc("POST /api/v1/agent/paper/{id}/assign", s.requireAgent(s.handlePaperAssign))
	mux.HandleFunc("POST /api/v1/agent/paper/{id}/paid", s.requireAgent(s.handlePaperPaid))
}

// docsOff answers when the NAS has the function switched off.
func docsOff(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "docs_off", "this function is not switched on on this NAS")
}

// agentDocsTick: the night run of the memory and the look into the inbox
// folder and the mailbox. Nothing here wakes a disk: the agent's volume
// and the folder's volume must be awake, else the work waits.
func (s *Server) agentDocsTick(now agentNow) {
	nas := s.agentSettings().Features.Docs
	if !nas.Enabled || s.opts.Users == nil || s.opts.Storage == nil {
		return
	}
	indexDue := docsNightDue(nas.at(), now)
	paperDue := nas.Paper && now.Minute()%10 == 0
	if !indexDue && !paperDue {
		return
	}
	if s.agentVolumeAsleep() {
		return
	}
	for _, u := range s.agentShareUsers(s.agentSettings()) {
		m, err := s.agentFeaturesOf(u.Name)
		if err != nil {
			continue
		}
		if indexDue && m.Docs.On {
			s.docsStartRun(u, m.Docs, false, now)
		}
		if paperDue && m.Paper.On {
			s.paperStartRun(u, m, now.Minute()%20 == 0, now)
		}
	}
}

// docsNightDue: in the hour after the night time, every ten minutes (the
// run itself skips what is done, so a folder on a sleeping disk is tried
// again later the same night; a missed night is made up at the next one).
func docsNightDue(at string, now time.Time) bool {
	t, err := time.ParseInLocation("15:04", at, now.Location())
	if err != nil {
		return false
	}
	due := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	d := now.Sub(due)
	return d >= 0 && d < 3*time.Hour && int(d.Minutes())%10 == 0
}

func (b *agentToolbox) docsTools() []*agentTool {
	nas := b.s.agentSettings().Features.Docs
	if !nas.Enabled {
		return nil
	}
	m, err := b.s.agentFeaturesOf(b.user.Name)
	if err != nil {
		return nil
	}
	var out []*agentTool
	if m.Docs.On {
		out = append(out, b.documentsSearchTool())
	}
	if nas.Paper && m.Paper.On {
		out = append(out, b.paperSummaryTool())
	}
	return out
}
