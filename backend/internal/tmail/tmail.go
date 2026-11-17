// Package tmail is the mail program of TitanOS ("T-Mail", docs/TMAIL.md):
// mailboxes of an account at IMAP/SMTP providers, reading with limits,
// sanitised HTML, sending, an address book, rules and messages on new
// mail.
//
// Every mailbox belongs to the account that made it and is seen by no one
// else. The settings live in /config/tmail/<account>.json, written only
// when the person saves; passwords in it are encrypted with the key in
// /config/tmail/key (AES-256-GCM). Who may use T-Mail at all is the app
// grant in /config/tmail/access.json, off for every account until an
// owner or administrator gives it.
//
// No mail is kept: titand fetches what is shown over IMAP when it is
// asked and keeps only small things in RAM (connections, the UIDNEXT of
// each INBOX for messages, the address book while no volume is there).
package tmail

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Files of /config.
const (
	dir        = "tmail"
	accessFile = dir + "/access.json"
	keyFile    = dir + "/key"
)

// Security of a connection: implicit TLS or STARTTLS, never plain.
const (
	SecurityTLS      = "tls"
	SecurityStartTLS = "starttls"
)

// States of a mailbox (Status.State).
const (
	StateOK          = "ok"
	StateConnecting  = "connecting"
	StateAuthFailed  = "auth_failed"
	StateUnreachable = "unreachable"
	StateOff         = "off"
)

// Limits of the first version (docs/TMAIL.md).
const (
	MaxMailboxes   = 20
	MaxSignatures  = 50
	MaxRules       = 200
	MaxRecipients  = 100
	MaxAttachments = 25 << 20
	maxPassword    = 1024
	maxSignature   = 16 << 10
)

// Errors the API turns into codes. Messages never hold a password.
var (
	ErrDisabled      = errors.New("tmail: T-Mail is not enabled for this account")
	ErrAuthFailed    = errors.New("tmail: the server refused the login")
	ErrUnreachable   = errors.New("tmail: the server is not reachable")
	ErrTLS           = errors.New("tmail: no TLS or the certificate is not valid")
	ErrHostForbidden = errors.New("tmail: this address may not be used")
	ErrTimeout       = errors.New("tmail: the server did not answer in time")
	ErrTooLarge      = errors.New("tmail: too large")
	ErrNotFound      = errors.New("tmail: not found")
	ErrFolderExists  = errors.New("tmail: the folder exists already")
	ErrSendFailed    = errors.New("tmail: the mail could not be sent")
	ErrInvalid       = errors.New("tmail: invalid")
	ErrUnreadable    = errors.New("tmail: the settings could not be read from /config")
	ErrNoPassword    = errors.New("tmail: no password is stored")
)

// Server is where IMAP or SMTP is reached.
type Server struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
}

// Mailbox is one mailbox of an account as it is stored. Password is the
// encrypted password (sealed), never the plain one.
type Mailbox struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	// SenderName is the name in From ("" only the address).
	SenderName string `json:"senderName,omitempty"`
	User       string `json:"user"`
	IMAP       Server `json:"imap"`
	SMTP       Server `json:"smtp"`
	Notify     bool   `json:"notify"`
	// Signature is the ID of the signature chosen by default ("" none).
	Signature string `json:"signature,omitempty"`
	Password  string `json:"password,omitempty"`
}

// Signature is a text put below a written mail.
type Signature struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

// Rule acts on new mail in the INBOX of one mailbox (Account), in order.
type Rule struct {
	ID      string   `json:"id"`
	Account string   `json:"account"`
	Enabled bool     `json:"enabled"`
	When    RuleWhen `json:"when"`
	Then    RuleThen `json:"then"`
}

// RuleWhen are parts of the sender, the recipients and the subject that
// must all be found (case does not matter); empty ones do not count.
type RuleWhen struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
}

// RuleThen is what happens to a matching mail.
type RuleThen struct {
	MoveTo   string `json:"moveTo"`
	MarkRead bool   `json:"markRead"`
	Flag     bool   `json:"flag"`
}

// owner names the account a file of /config belongs to; Created tells it
// from an earlier account of the same name.
type owner struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

// userFile is /config/tmail/<account>.json.
type userFile struct {
	Owner      owner       `json:"owner"`
	Mailboxes  []Mailbox   `json:"accounts"`
	Signatures []Signature `json:"signatures"`
	Rules      []Rule      `json:"rules"`
	// PushContent: push messages name sender and subject (default on).
	PushContent *bool `json:"pushContent,omitempty"`
}

// Account is what T-Mail needs to know of a TitanOS account right now.
type Account struct {
	Name     string
	Created  time.Time
	Disabled bool
	// Admin: owner or administrator (they have a bell).
	Admin bool
}

// Status is the state of a mailbox's connection.
type Status struct {
	State string    `json:"state"`
	Code  string    `json:"code"`
	Since time.Time `json:"since"`
}

// Notice is new mail to tell an account about (one or several mails of
// one mailbox in a short time).
type Notice struct {
	User        string
	Mailbox     string
	MailboxName string
	Count       int
	// From and Subject of the newest mail.
	From, Subject string
	// Content: the account allows sender and subject in push messages.
	Content bool
}

// Manager keeps the settings of every account in RAM and the connections.
type Manager struct {
	store *config.Store
	// Account returns the account as it is now; false if there is none.
	Account func(name string) (Account, bool)
	// Notify tells an account about new mail (bell and push); nil tells
	// nobody.
	Notify func(Notice)
	// Volume opens .titanos/mail on the system data volume for the
	// address book; release ends the use. create makes the folder (only
	// to write); without it a missing folder is fs.ErrNotExist. Another
	// error: no volume now.
	Volume func(create bool) (root *os.Root, release func(), err error)
	// OwnPorts are the ports titand listens on; the NAS's own addresses
	// are refused on them (OwnAddrs, nil: the addresses of the
	// interfaces).
	OwnPorts []int
	OwnAddrs func() []netip.Addr
	// RootCAs are the certificates trusted for TLS; nil is the system's.
	RootCAs *x509.CertPool

	// Targets checks an address after DNS; nil is CheckTarget with
	// OwnAddrs and OwnPorts. Only tests set it, to reach their servers on
	// loopback.
	Targets func(netip.AddrPort) error
	// now is the clock (tests).
	now func() time.Time
	// noticeWindow collects new mail of a mailbox into one message.
	noticeWindow time.Duration
	// idleRestart is how long one IDLE lasts before it is renewed.
	idleRestart time.Duration
	// backoff are the pauses after failed connections.
	backoffMin, backoffMax time.Duration
	// sessionIdle closes a connection of the web UI after this rest.
	sessionIdle time.Duration

	mu         sync.Mutex
	users      map[string]*userFile
	unreadable map[string]bool
	access     accessState
	key        []byte

	conns    connPool
	watch    watchState
	events   hub
	contacts contactBook
	notices  noticeState
	checks   checkLimiter
	ctx      contextHolder
}

// New returns a manager; Load reads /config.
func New(store *config.Store) *Manager {
	return &Manager{store: store, users: map[string]*userFile{}, unreadable: map[string]bool{},
		noticeWindow: 10 * time.Second, idleRestart: 25 * time.Minute,
		backoffMin: 30 * time.Second, backoffMax: 15 * time.Minute, sessionIdle: 2 * time.Minute,
		access: accessState{given: map[string]time.Time{}}}
}

func (m *Manager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

var (
	accountRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	idRE      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

func userPath(name string) string { return dir + "/" + name + ".json" }

// Load reads the grants and the settings of every account once, at the
// start. Nothing is written.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	if err := m.loadAccessLocked(); err != nil {
		firstErr = err
	}
	names, err := m.store.List(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(firstErr, err)
	}
	for _, file := range names {
		name, ok := strings.CutSuffix(file, ".json")
		if !ok || name == "access" || !accountRE.MatchString(name) {
			continue
		}
		var f userFile
		if err := m.store.ReadJSON(userPath(name), &f); err != nil {
			slog.Warn("T-Mail settings of an account not readable", "account", name, "err", err)
			m.unreadable[name] = true
			continue
		}
		m.users[name] = &f
	}
	return firstErr
}

// fileLocked returns the settings of an account (m.mu held): nil while it
// has none, and none of an earlier account of the same name.
func (m *Manager) fileLocked(user string) *userFile {
	f := m.users[user]
	if f == nil {
		return nil
	}
	if account, ok := m.lookup(user); !ok || !sameAccount(f.Owner, account) {
		return nil
	}
	return f
}

func sameAccount(o owner, a Account) bool {
	return o.Name == a.Name && (o.Created.IsZero() || a.Created.IsZero() || o.Created.Equal(a.Created))
}

func (m *Manager) lookup(name string) (Account, bool) {
	if m.Account == nil || name == "" {
		return Account{}, false
	}
	return m.Account(name)
}

// saveLocked writes the settings of an account (m.mu held), the only
// write to the stick apart from the key and the grants.
func (m *Manager) saveLocked(user string, f *userFile) error {
	if m.unreadable[user] {
		return ErrUnreadable
	}
	account, ok := m.lookup(user)
	if !ok {
		return ErrDisabled
	}
	f.Owner = owner{Name: account.Name, Created: account.Created}
	if f.Mailboxes == nil {
		f.Mailboxes = []Mailbox{}
	}
	if f.Signatures == nil {
		f.Signatures = []Signature{}
	}
	if f.Rules == nil {
		f.Rules = []Rule{}
	}
	if err := m.store.WriteJSON(userPath(user), f); err != nil {
		return err
	}
	m.users[user] = f
	return nil
}

// cloneFile copies settings so that a change can fail without a trace.
func cloneFile(f *userFile) *userFile {
	if f == nil {
		return &userFile{}
	}
	out := *f
	out.Mailboxes = slices.Clone(f.Mailboxes)
	out.Signatures = slices.Clone(f.Signatures)
	out.Rules = slices.Clone(f.Rules)
	if f.PushContent != nil {
		v := *f.PushContent
		out.PushContent = &v
	}
	return &out
}

// newID returns a random ID for a mailbox, signature or rule.
func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b) // crypto/rand does not fail on Linux
	return hex.EncodeToString(b)
}

// mailbox returns a copy of a mailbox of an account.
func (m *Manager) mailbox(user, id string) (Mailbox, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fileLocked(user)
	if f == nil {
		return Mailbox{}, ErrNotFound
	}
	for _, mb := range f.Mailboxes {
		if mb.ID == id {
			return mb, nil
		}
	}
	return Mailbox{}, ErrNotFound
}

// pushContent tells whether push messages of an account may name sender
// and subject.
func (m *Manager) pushContent(user string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fileLocked(user)
	return f == nil || f.PushContent == nil || *f.PushContent
}
