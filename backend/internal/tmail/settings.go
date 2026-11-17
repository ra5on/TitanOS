package tmail

import (
	"encoding/json"
	"fmt"
	"net"
	"net/mail"
	"slices"
	"strings"
	"unicode/utf8"
)

// MailboxView is a mailbox as the web UI sees it: never the password,
// only whether one is stored.
type MailboxView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	SenderName  string `json:"senderName"`
	User        string `json:"user"`
	IMAP        Server `json:"imap"`
	SMTP        Server `json:"smtp"`
	PasswordSet bool   `json:"passwordSet"`
	Notify      bool   `json:"notify"`
	Signature   string `json:"signature"`
	Status      Status `json:"status"`
	Unread      int    `json:"unread"`
}

// MailboxInput is a mailbox as the web UI sends it. An empty password
// keeps the stored one.
type MailboxInput struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	SenderName string `json:"senderName"`
	User       string `json:"user"`
	IMAP       Server `json:"imap"`
	SMTP       Server `json:"smtp"`
	Notify     bool   `json:"notify"`
	Signature  string `json:"signature"`
	Password   string `json:"password"`
}

// SignatureView is a signature with the mailboxes that choose it by
// default (the same as MailboxView.Signature, seen from the signature).
type SignatureView struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Text    string      `json:"text"`
	Default DefaultList `json:"default"`
}

// DefaultList names mailboxes. It reads a list of IDs, one ID, or an
// object whose values are IDs (or whose keys are IDs set to true), the
// forms the first contract left open ("default:{accountId}").
type DefaultList []string

// UnmarshalJSON accepts every form of DefaultList.
func (d *DefaultList) UnmarshalJSON(data []byte) error {
	var list []string
	if json.Unmarshal(data, &list) == nil {
		*d = list
		return nil
	}
	var one string
	if json.Unmarshal(data, &one) == nil {
		*d = nil
		if one != "" {
			*d = DefaultList{one}
		}
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	*d = nil
	for k, v := range object {
		switch v := v.(type) {
		case string:
			if v != "" {
				*d = append(*d, v)
			}
		case bool:
			if v {
				*d = append(*d, k)
			}
		}
	}
	return nil
}

// Settings are GET/PUT /api/v1/mail/settings.
type Settings struct {
	Signatures  []SignatureView `json:"signatures"`
	Rules       []Rule          `json:"rules"`
	PushContent bool            `json:"pushContent"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

// defaultPort is the usual port of a protocol and security.
func defaultPort(smtp bool, security string) int {
	switch {
	case smtp && security == SecurityTLS:
		return 465
	case smtp:
		return 587
	case security == SecurityTLS:
		return 993
	}
	return 143
}

func cleanServer(s Server, smtp bool) (Server, error) {
	s.Host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s.Host)), ".")
	if s.Security == "" {
		s.Security = SecurityTLS
	}
	if s.Security != SecurityTLS && s.Security != SecurityStartTLS {
		return s, invalid("security must be tls or starttls")
	}
	if s.Port == 0 {
		s.Port = defaultPort(smtp, s.Security)
	}
	if s.Port < 1 || s.Port > 65535 {
		return s, invalid("port")
	}
	if !validHost(s.Host) {
		return s, invalid("host")
	}
	return s, nil
}

// validHost accepts a DNS name or an IP address, nothing with a scheme,
// a port or spaces.
func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}

func shortText(s string, limit int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= limit && !strings.ContainsAny(s, "\r\n\x00")
}

func cleanInput(in MailboxInput) (MailboxInput, error) {
	var err error
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)
	in.User = strings.TrimSpace(in.User)
	in.SenderName = strings.TrimSpace(stripInvisible(in.SenderName))
	in.Name = strings.TrimSpace(stripInvisible(in.Name))
	if !shortText(in.SenderName, 128) {
		return in, invalid("senderName")
	}
	addr, perr := mail.ParseAddress(in.Email)
	if perr != nil || addr.Address != in.Email || !strings.Contains(in.Email, "@") {
		return in, invalid("email")
	}
	if in.Name == "" {
		in.Name = in.Email
	}
	if !shortText(in.Name, 64) {
		return in, invalid("name")
	}
	if in.User == "" {
		in.User = in.Email
	}
	if !shortText(in.User, 256) {
		return in, invalid("user")
	}
	if len(in.Password) > maxPassword || strings.ContainsAny(in.Password, "\r\n\x00") {
		return in, invalid("password")
	}
	if in.IMAP, err = cleanServer(in.IMAP, false); err != nil {
		return in, err
	}
	if in.SMTP, err = cleanServer(in.SMTP, true); err != nil {
		return in, err
	}
	return in, nil
}

// file returns a copy of an account's settings to change, or an error if
// T-Mail may not be used or the file was not readable.
func (m *Manager) editLocked(user string) (*userFile, error) {
	account, ok := m.lookup(user)
	if !ok || account.Disabled || !m.grantedLocked(account) {
		return nil, ErrDisabled
	}
	if m.unreadable[user] {
		return nil, ErrUnreadable
	}
	return cloneFile(m.fileLocked(user)), nil
}

func (m *Manager) viewLocked(user string, mb Mailbox) MailboxView {
	return MailboxView{ID: mb.ID, Name: mb.Name, Email: mb.Email, SenderName: mb.SenderName, User: mb.User, IMAP: mb.IMAP, SMTP: mb.SMTP,
		PasswordSet: mb.Password != "", Notify: mb.Notify, Signature: mb.Signature,
		Status: m.statusOf(user, mb), Unread: m.unreadOf(user, mb.ID)}
}

// Mailboxes lists the mailboxes of an account.
func (m *Manager) Mailboxes(user string) []MailboxView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []MailboxView{}
	if f := m.fileLocked(user); f != nil {
		for _, mb := range f.Mailboxes {
			out = append(out, m.viewLocked(user, mb))
		}
	}
	return out
}

// AddMailbox stores a new mailbox.
func (m *Manager) AddMailbox(user string, in MailboxInput) (MailboxView, error) {
	in, err := cleanInput(in)
	if err != nil {
		return MailboxView{}, err
	}
	m.mu.Lock()
	f, err := m.editLocked(user)
	if err != nil {
		m.mu.Unlock()
		return MailboxView{}, err
	}
	if len(f.Mailboxes) >= MaxMailboxes {
		m.mu.Unlock()
		return MailboxView{}, invalid("at most %d mailboxes", MaxMailboxes)
	}
	mb := Mailbox{ID: newID(), Name: in.Name, Email: in.Email, SenderName: in.SenderName, User: in.User, IMAP: in.IMAP, SMTP: in.SMTP, Notify: in.Notify}
	if in.Signature != "" && slices.ContainsFunc(f.Signatures, func(s Signature) bool { return s.ID == in.Signature }) {
		mb.Signature = in.Signature
	}
	if in.Password != "" {
		if mb.Password, err = m.sealLocked(user, mb.ID, in.Password); err != nil {
			m.mu.Unlock()
			return MailboxView{}, err
		}
	}
	f.Mailboxes = append(f.Mailboxes, mb)
	if err := m.saveLocked(user, f); err != nil {
		m.mu.Unlock()
		return MailboxView{}, err
	}
	view := m.viewLocked(user, mb)
	m.mu.Unlock()
	m.Sync(user)
	return view, nil
}

// UpdateMailbox changes a mailbox; an empty password keeps the stored one
// (and its ciphertext, so that nothing is written for nothing).
func (m *Manager) UpdateMailbox(user, id string, in MailboxInput) (MailboxView, error) {
	in, err := cleanInput(in)
	if err != nil {
		return MailboxView{}, err
	}
	m.mu.Lock()
	f, err := m.editLocked(user)
	if err != nil {
		m.mu.Unlock()
		return MailboxView{}, err
	}
	i := slices.IndexFunc(f.Mailboxes, func(mb Mailbox) bool { return mb.ID == id })
	if i < 0 {
		m.mu.Unlock()
		return MailboxView{}, ErrNotFound
	}
	mb := f.Mailboxes[i]
	if in.Password == "" && mb.Password != "" && !sameHosts(mb, in) {
		m.mu.Unlock()
		return MailboxView{}, ErrPasswordRequired
	}
	mb.Name, mb.Email, mb.SenderName, mb.User, mb.IMAP, mb.SMTP, mb.Notify = in.Name, in.Email, in.SenderName, in.User, in.IMAP, in.SMTP, in.Notify
	mb.Signature = ""
	if in.Signature != "" && slices.ContainsFunc(f.Signatures, func(s Signature) bool { return s.ID == in.Signature }) {
		mb.Signature = in.Signature
	}
	if in.Password != "" {
		if mb.Password, err = m.sealLocked(user, mb.ID, in.Password); err != nil {
			m.mu.Unlock()
			return MailboxView{}, err
		}
	}
	f.Mailboxes[i] = mb
	if err := m.saveLocked(user, f); err != nil {
		m.mu.Unlock()
		return MailboxView{}, err
	}
	view := m.viewLocked(user, mb)
	m.mu.Unlock()
	// A new password, user or host ends the stop after a failed login.
	m.Sync(user)
	return view, nil
}

// RemoveMailbox removes a mailbox from TitanOS (nothing at the provider),
// with its rules.
func (m *Manager) RemoveMailbox(user, id string) error {
	m.mu.Lock()
	f, err := m.editLocked(user)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	i := slices.IndexFunc(f.Mailboxes, func(mb Mailbox) bool { return mb.ID == id })
	if i < 0 {
		m.mu.Unlock()
		return ErrNotFound
	}
	f.Mailboxes = slices.Delete(f.Mailboxes, i, i+1)
	f.Rules = slices.DeleteFunc(f.Rules, func(r Rule) bool { return r.Account == id })
	if err := m.saveLocked(user, f); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	m.conns.close(user, id)
	m.Sync(user)
	return nil
}

// Settings returns signatures, rules and the push setting of an account.
func (m *Manager) Settings(user string) Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Settings{Signatures: []SignatureView{}, Rules: []Rule{}, PushContent: true}
	f := m.fileLocked(user)
	if f == nil {
		return out
	}
	for _, s := range f.Signatures {
		view := SignatureView{ID: s.ID, Name: s.Name, Text: s.Text, Default: DefaultList{}}
		for _, mb := range f.Mailboxes {
			if mb.Signature == s.ID {
				view.Default = append(view.Default, mb.ID)
			}
		}
		out.Signatures = append(out.Signatures, view)
	}
	out.Rules = append(out.Rules, f.Rules...)
	if f.PushContent != nil {
		out.PushContent = *f.PushContent
	}
	return out
}

// SetSettings stores signatures, rules and the push setting. A signature
// or rule without an ID gets one. Default names the mailboxes that choose
// a signature by default; a mailbox named by none chooses none.
func (m *Manager) SetSettings(user string, s Settings) (Settings, error) {
	m.mu.Lock()
	f, err := m.editLocked(user)
	if err != nil {
		m.mu.Unlock()
		return Settings{}, err
	}
	if len(s.Signatures) > MaxSignatures || len(s.Rules) > MaxRules {
		m.mu.Unlock()
		return Settings{}, invalid("too many signatures or rules")
	}
	mailboxes := map[string]bool{}
	for _, mb := range f.Mailboxes {
		mailboxes[mb.ID] = true
	}
	defaults := map[string]string{}
	seen := map[string]bool{}
	f.Signatures = []Signature{}
	for _, sig := range s.Signatures {
		if sig.ID == "" {
			sig.ID = newID()
		}
		sig.Name = strings.TrimSpace(sig.Name)
		if !idRE.MatchString(sig.ID) || seen[sig.ID] || !shortText(sig.Name, 64) || sig.Name == "" ||
			len(sig.Text) > maxSignature || !utf8.ValidString(sig.Text) || strings.ContainsRune(sig.Text, 0) {
			m.mu.Unlock()
			return Settings{}, invalid("signature %q", sig.Name)
		}
		seen[sig.ID] = true
		for _, id := range sig.Default {
			if mailboxes[id] {
				defaults[id] = sig.ID
			}
		}
		f.Signatures = append(f.Signatures, Signature{ID: sig.ID, Name: sig.Name, Text: strings.ReplaceAll(sig.Text, "\r\n", "\n")})
	}
	for i := range f.Mailboxes {
		f.Mailboxes[i].Signature = defaults[f.Mailboxes[i].ID]
	}
	f.Rules = []Rule{}
	seen = map[string]bool{}
	for _, r := range s.Rules {
		if r.ID == "" {
			r.ID = newID()
		}
		r.When.From, r.When.To, r.When.Subject = strings.TrimSpace(r.When.From), strings.TrimSpace(r.When.To), strings.TrimSpace(r.When.Subject)
		r.Then.MoveTo = strings.TrimSpace(r.Then.MoveTo)
		switch {
		case !idRE.MatchString(r.ID) || seen[r.ID]:
			err = invalid("rule ID")
		case !mailboxes[r.Account]:
			err = invalid("rule %s: no such mailbox", r.ID)
		case !shortText(r.When.From, 256) || !shortText(r.When.To, 256) || !shortText(r.When.Subject, 256) || !shortText(r.Then.MoveTo, 512):
			err = invalid("rule %s", r.ID)
		case r.When.From == "" && r.When.To == "" && r.When.Subject == "":
			err = invalid("rule %s: no condition", r.ID)
		case r.Then.MoveTo == "" && !r.Then.MarkRead && !r.Then.Flag:
			err = invalid("rule %s: no action", r.ID)
		}
		if err != nil {
			m.mu.Unlock()
			return Settings{}, err
		}
		seen[r.ID] = true
		f.Rules = append(f.Rules, r)
	}
	push := s.PushContent
	f.PushContent = &push
	if err := m.saveLocked(user, f); err != nil {
		m.mu.Unlock()
		return Settings{}, err
	}
	m.mu.Unlock()
	m.Sync(user)
	return m.Settings(user), nil
}

// signatureText returns the text of a signature of an account.
func (m *Manager) signatureText(user, id string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fileLocked(user)
	if f == nil || id == "" {
		return "", false
	}
	for _, s := range f.Signatures {
		if s.ID == id {
			return s.Text, true
		}
	}
	return "", false
}

// rulesOf returns the enabled rules of a mailbox, in order.
func (m *Manager) rulesOf(user, id string) []Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fileLocked(user)
	if f == nil {
		return nil
	}
	var out []Rule
	for _, r := range f.Rules {
		if r.Enabled && r.Account == id {
			out = append(out, r)
		}
	}
	return out
}

// stripInvisible drops format characters (Unicode Cf, bidi controls).
func stripInvisible(s string) string {
	return strings.Map(func(r rune) rune {
		if invisible(r) {
			return -1
		}
		return r
	}, s)
}

// MailboxCount is the number of mailboxes of an account (for the question
// before it is deleted); none of an earlier account of the name.
func (m *Manager) MailboxCount(user string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f := m.fileLocked(user); f != nil {
		return len(f.Mailboxes)
	}
	return 0
}
