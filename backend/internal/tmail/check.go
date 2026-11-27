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
	// IMAP and SMTP of one check are one attempt for the limit of new
	// servers.
	ctx = context.WithValue(ctx, attemptKey{}, newID())
	leave, err := m.enter(ctx, user)
	if err != nil {
		return CheckResult{}, err
	}
	defer leave()
	var out CheckResult
	c, _, err := m.connect(ctx, user, mb, password, nil)
	if err == nil {
		_ = c.Close() // only the login was asked for
		out.IMAP.OK = true
	} else {
		out.IMAP.Code = code(err)
	}
	if err := m.smtpSend(ctx, user, mb, password, "", nil, nil, nil); err == nil {
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

// reserveFree is what a request may hold without asking the budget: a
// text-only mail or draft (security review of 1.05, follow-up).
const reserveFree = 1 << 20

// reserveWait is how long a request waits for room in the budget of RAM
// before it is answered with ErrBusy (as files.Acquire waits).
var reserveWait = 5 * time.Second

// More takes bytes of TitanOS's budget of RAM for a request, waiting a
// little for room; ErrBusy without. Small amounts need no room.
func (m *Manager) More(ctx context.Context, bytes int64) (func(), error) {
	if m.Reserve == nil || bytes < reserveFree {
		return func() {}, nil
	}
	deadline := time.NewTimer(reserveWait)
	defer deadline.Stop()
	for {
		if release, ok := m.Reserve(bytes); ok {
			return release, nil
		}
		select {
		case <-ctx.Done():
			return nil, ErrBusy
		case <-deadline.C:
			return nil, ErrBusy
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Begin admits a heavy request that holds up to bytes in RAM: at most
// HeavyPerAccount per account and only within the budget of RAM (More).
func (m *Manager) Begin(ctx context.Context, user string, bytes int64) (func(), error) {
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
	release, err := m.More(ctx, bytes)
	if err != nil {
		leave()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { release(); leave() }) }, nil
}

// Commands of T-Mail at the servers: each may hold up to its budget and
// what decoding makes of it, so they are limited per account across all
// its mailboxes and, beyond a floor, by TitanOS's budget of RAM
// (security review of 1.05, follow-up).
const (
	// CommandsPerAccount is how many IMAP commands an account may have
	// running at once, over all its mailboxes.
	CommandsPerAccount = 4
	// commandReserve is the RAM a running command may need.
	commandReserve = 6 * budgetCommand
	// commandFloor commands of each account run without asking the
	// budget, so that T-Mail stays usable while VMs take the RAM; per
	// account, so that one account cannot take the floor of the others
	// (third review of 1.05).
	commandFloor = 1
)

type gateState struct {
	mu      sync.Mutex
	perUser map[string]int
}

// enter waits (at most reserveWait, and while ctx lasts) until an account
// may run one more command; ErrBusy without room.
func (m *Manager) enter(ctx context.Context, user string) (func(), error) {
	g := &m.gate
	return waitTurn(ctx, func() (func(), bool) {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.perUser == nil {
			g.perUser = map[string]int{}
		}
		if g.perUser[user] >= CommandsPerAccount {
			return nil, false
		}
		release, ok := func() {}, true
		if g.perUser[user] >= commandFloor && m.Reserve != nil {
			release, ok = m.Reserve(commandReserve)
		}
		if !ok {
			return nil, false
		}
		g.perUser[user]++
		return func() {
			release()
			g.mu.Lock()
			if g.perUser[user]--; g.perUser[user] <= 0 {
				delete(g.perUser, user)
			}
			g.mu.Unlock()
		}, true
	})
}

// Downloads (attachments, embedded pictures) go at the pace of the
// browser, up to PartTimeout. They hold a command's turn only until the
// part's literal begins; the stream itself takes a turn of its own,
// without RAM of the budget (it is copied through, never kept), so that
// stalled downloads cannot block the commands of anyone (third review of
// 1.05).
const (
	// DownloadsPerAccount is how many downloads an account may have
	// running at once.
	DownloadsPerAccount = 4
	// downloadsTotal is how many run on the whole NAS.
	downloadsTotal = 16
)

type downloadState struct {
	mu      sync.Mutex
	perUser map[string]int
	running int
}

// download waits (like enter) for a turn to stream a part; ErrBusy
// without one.
func (m *Manager) download(ctx context.Context, user string) (func(), error) {
	d := &m.downloads
	return waitTurn(ctx, func() (func(), bool) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.perUser == nil {
			d.perUser = map[string]int{}
		}
		if d.perUser[user] >= DownloadsPerAccount || d.running >= downloadsTotal {
			return nil, false
		}
		d.perUser[user]++
		d.running++
		return func() {
			d.mu.Lock()
			d.running--
			if d.perUser[user]--; d.perUser[user] <= 0 {
				delete(d.perUser, user)
			}
			d.mu.Unlock()
		}, true
	})
}

// waitTurn asks take for a turn until it gives one, at most reserveWait
// and while ctx lasts; ErrBusy without. The release it returns works
// once, however often it is called.
func waitTurn(ctx context.Context, take func() (func(), bool)) (func(), error) {
	deadline := time.Now().Add(reserveWait)
	for {
		if release, ok := take(); ok {
			var once sync.Once
			return func() { once.Do(release) }, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, ErrBusy
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
}
