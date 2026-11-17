package tmail

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrPasswordRequired: a stored password is only ever sent to the hosts it
// was stored for. Changing the IMAP or SMTP host (or using a stored
// password for a check against other hosts) needs the password again.
var ErrPasswordRequired = errors.New("tmail: enter the password again for another host")

// ErrRateLimited: more checks of the settings than CheckLimit a minute.
var ErrRateLimited = errors.New("tmail: too many checks of the settings; try again in a minute")

// CheckLimit is how many checks of the settings an account may make a
// minute: /accounts/test connects anywhere the account may, and must not
// become a tool to scan ports.
const CheckLimit = 10

// checkLimiter counts the checks per account (RAM only).
type checkLimiter struct {
	mu   sync.Mutex
	used map[string][]time.Time
}

func (l *checkLimiter) allow(user string, now time.Time, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.used == nil {
		l.used = map[string][]time.Time{}
	}
	recent := l.used[user][:0]
	for _, t := range l.used[user] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= limit {
		l.used[user] = recent
		return false
	}
	l.used[user] = append(recent, now)
	return true
}

// vagueCode hides why a connection of a confined account failed: refused,
// no answer, no TLS and "not a mail server" look the same, so that no
// answer tells open from closed ports (security review of 1.05). Other
// codes say nothing about the network and stay.
func vagueCode(err error) string {
	switch code := Code(err); code {
	case "mail_tls", "mail_timeout", "mail_unreachable":
		return "mail_unreachable"
	default:
		return code
	}
}

// VagueCode is vagueCode for the API.
func VagueCode(err error) string { return vagueCode(err) }

// Confined tells whether an account reaches only servers on the internet
// (role user); its errors are answered with VagueCode.
func (m *Manager) Confined(user string) bool { return m.confined(user) }

// Check is the answer of one protocol in a check of the settings.
type Check struct {
	OK   bool   `json:"ok"`
	Code string `json:"code"`
}

// CheckResult is POST /api/v1/mail/accounts/test.
type CheckResult struct {
	IMAP Check `json:"imap"`
	SMTP Check `json:"smtp"`
}

func sameHosts(a Mailbox, in MailboxInput) bool {
	return a.IMAP.Host == in.IMAP.Host && a.SMTP.Host == in.SMTP.Host
}

// CheckSettings logs in to IMAP and SMTP with settings the person entered
// (nothing is stored). Without a password the stored one of mailbox id
// is used, for the same hosts only. A check that goes through with the
// stored settings ends the stop after a refused login.
func (m *Manager) CheckSettings(ctx context.Context, user, id string, in MailboxInput) (CheckResult, error) {
	if !m.Allowed(user) {
		return CheckResult{}, ErrDisabled
	}
	in, err := cleanInput(in)
	if err != nil {
		return CheckResult{}, err
	}
	if !m.checks.allow(user, m.clock(), CheckLimit) {
		return CheckResult{}, ErrRateLimited
	}
	code := Code
	if m.confined(user) {
		code = vagueCode
	}
	password := in.Password
	var stored Mailbox
	if password == "" {
		if stored, err = m.mailbox(user, id); err != nil {
			return CheckResult{}, err
		}
		if !sameHosts(stored, in) {
			return CheckResult{}, ErrPasswordRequired
		}
		if password, err = m.password(user, stored); err != nil {
			return CheckResult{}, err
		}
	}
	mb := Mailbox{ID: id, Email: in.Email, User: in.User, IMAP: in.IMAP, SMTP: in.SMTP}
	var out CheckResult
	c, _, err := m.connect(ctx, user, mb, password, nil)
	if err == nil {
		_ = c.Close() // only the login was asked for
		out.IMAP.OK = true
	} else {
		out.IMAP.Code = code(err)
	}
	if err := m.smtpSend(ctx, user, mb, password, "", nil, nil); err == nil {
		out.SMTP.OK = true
	} else {
		out.SMTP.Code = code(err)
	}
	if out.IMAP.OK && out.SMTP.OK && in.Password == "" && stored.User == in.User && stored.IMAP == in.IMAP && stored.SMTP == in.SMTP {
		m.clearAuthFailed(user, id)
		m.Sync(user)
	}
	return out, nil
}

// ErrBusy: no room for another heavy request now (two per account, and
// TitanOS's budget of RAM).
var ErrBusy = errors.New("tmail: busy; try again in a moment")

// HeavyPerAccount is how many heavy requests (send, draft, attachments)
// an account may have running at once.
const HeavyPerAccount = 2

// heavyState counts the heavy requests per account.
type heavyState struct {
	mu      sync.Mutex
	running map[string]int
}

// Begin admits a heavy request that holds up to bytes in RAM: at most
// HeavyPerAccount per account and only within the budget of RAM
// (Reserve). The caller answers ErrBusy at once if it is refused.
func (m *Manager) Begin(user string, bytes int64) (func(), error) {
	h := &m.heavy
	h.mu.Lock()
	if h.running == nil {
		h.running = map[string]int{}
	}
	if h.running[user] >= HeavyPerAccount {
		h.mu.Unlock()
		return nil, ErrBusy
	}
	h.running[user]++
	h.mu.Unlock()
	leave := func() {
		h.mu.Lock()
		h.running[user]--
		if h.running[user] <= 0 {
			delete(h.running, user)
		}
		h.mu.Unlock()
	}
	release := func() {}
	if m.Reserve != nil {
		r, ok := m.Reserve(bytes)
		if !ok {
			leave()
			return nil, ErrBusy
		}
		release = r
	}
	var once sync.Once
	return func() { once.Do(func() { release(); leave() }) }, nil
}
