package tmail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// The permanent connection of a mailbox: at most one, to its INBOX, and
// only while messages or rules are on for it (docs/TMAIL.md "Anbieter
// schonen"). It waits with IDLE; new mail goes through the rules first,
// then becomes a message if it is still in the INBOX. A refused login
// stops it until the settings change; other errors are tried again after
// a growing pause (30 s … 15 min). It is no work of the NAS: an app update
// or a stop of titand closes it at once.

type watcher struct {
	user, id string
	config   string
	cancel   context.CancelFunc
	done     chan struct{}
}

func (w *watcher) stop() { w.cancel() }

type contextHolder struct {
	mu  sync.Mutex
	ctx context.Context
}

func (h *contextHolder) get() context.Context {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ctx
}

// Run keeps the permanent connections until ctx ends, closes connections
// of the web UI after their rest and follows changes of accounts (locked,
// grant taken) every half minute, from RAM only.
func (m *Manager) Run(ctx context.Context) {
	m.ctx.mu.Lock()
	m.ctx.ctx = ctx
	m.ctx.mu.Unlock()
	m.SyncAll()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-tick.C:
			m.conns.reap(m.clock(), m.sessionIdle)
			m.SyncAll()
		}
	}
}

// stopAll ends every connection without waiting for the servers.
func (m *Manager) stopAll() {
	m.watch.mu.Lock()
	m.watch.init()
	running := m.watch.running
	m.watch.running = map[string]*watcher{}
	m.watch.mu.Unlock()
	for _, w := range running {
		w.stop()
	}
	m.conns.closeAll()
}

// SyncAll brings the permanent connections of every account in line.
func (m *Manager) SyncAll() {
	m.mu.Lock()
	users := make([]string, 0, len(m.users))
	for name := range m.users {
		users = append(users, name)
	}
	m.mu.Unlock()
	m.watch.mu.Lock()
	m.watch.init()
	for k := range m.watch.running {
		user, _, _ := strings.Cut(k, "\x00")
		if !slices.Contains(users, user) {
			users = append(users, user)
		}
	}
	m.watch.mu.Unlock()
	for _, user := range users {
		m.Sync(user)
	}
}

// watchConfig names what a watcher depends on; another value restarts it.
func watchConfig(mb Mailbox, rules []Rule) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%v\x00%v", fingerprint(mb), mb.Notify, rules)
	return hex.EncodeToString(h.Sum(nil))
}

// Sync starts and stops the permanent connections of an account after a
// change: only with the grant, a password, no refused login, and messages
// or rules on.
func (m *Manager) Sync(user string) {
	ctx := m.ctx.get()
	want := map[string]Mailbox{}
	configs := map[string]string{}
	if m.Allowed(user) {
		m.mu.Lock()
		f := m.fileLocked(user)
		var mailboxes []Mailbox
		if f != nil {
			mailboxes = slices.Clone(f.Mailboxes)
		}
		m.mu.Unlock()
		for _, mb := range mailboxes {
			rules := m.rulesOf(user, mb.ID)
			if mb.Password == "" || (!mb.Notify && len(rules) == 0) || m.authFailed(user, mb) {
				continue
			}
			k := key(user, mb.ID)
			want[k], configs[k] = mb, watchConfig(mb, rules)
		}
	}
	m.watch.mu.Lock()
	m.watch.init()
	var stop []*watcher
	for k, w := range m.watch.running {
		if w.user != user {
			continue
		}
		if _, ok := want[k]; !ok || configs[k] != w.config || ctx == nil {
			stop = append(stop, w)
			delete(m.watch.running, k)
		}
	}
	if ctx != nil && ctx.Err() == nil {
		for k, mb := range want {
			if _, ok := m.watch.running[k]; ok {
				continue
			}
			wctx, cancel := context.WithCancel(ctx)
			w := &watcher{user: user, id: mb.ID, config: configs[k], cancel: cancel, done: make(chan struct{})}
			m.watch.running[k] = w
			delete(m.watch.status, k)
			go func() {
				defer close(w.done)
				m.runWatcher(wctx, w)
			}()
		}
	}
	m.watch.mu.Unlock()
	for _, w := range stop {
		w.stop()
	}
}

// runWatcher connects again and again until ctx ends or the login fails.
func (m *Manager) runWatcher(ctx context.Context, w *watcher) {
	delay := m.backoffMin
	for ctx.Err() == nil {
		mb, err := m.mailbox(w.user, w.id)
		if err != nil {
			return
		}
		m.setStatus(w.user, w.id, StateConnecting, "")
		started := m.clock()
		err = m.idleSession(ctx, w.user, mb)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrAuthFailed) {
			slog.Info("T-Mail: login refused, not tried again until the settings change", "account", w.user, "mailbox", w.id)
			m.setStatus(w.user, w.id, StateAuthFailed, "mail_auth_failed")
			m.setAuthFailed(w.user, mb)
			return
		}
		slog.Info("T-Mail: connection of a mailbox lost", "account", w.user, "mailbox", w.id, "err", safeText(fmt.Sprint(err)))
		m.setStatus(w.user, w.id, StateUnreachable, Code(err))
		if m.clock().Sub(started) > 5*time.Minute {
			delay = m.backoffMin
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, m.backoffMax)
	}
}

// idleSession is one connection: INBOX selected, new mail handled, IDLE
// until something comes.
func (m *Manager) idleSession(ctx context.Context, user string, mb Mailbox) error {
	wake := make(chan struct{}, 1)
	handler := &imapclient.UnilateralDataHandler{Mailbox: func(data *imapclient.UnilateralDataMailbox) {
		if data.NumMessages != nil {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}}
	password, err := m.password(user, mb)
	if err != nil {
		return err
	}
	c, err := m.connect(ctx, mb, password, handler)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() }) // titand stops: no goodbye to the server
	defer stop()
	limited := func(work func() error) error {
		timer := time.AfterFunc(commandTimeout, func() { _ = c.Close() })
		defer timer.Stop()
		return work()
	}
	var data *imap.SelectData
	if err := limited(func() error {
		var err error
		data, err = c.Select("INBOX", nil).Wait()
		if err != nil {
			return err
		}
		next := uint32(data.UIDNext)
		if next == 0 {
			all, err := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
			if err != nil {
				return err
			}
			for _, u := range all.AllUIDs() {
				next = max(next, uint32(u)+1)
			}
		}
		m.baselineFor(user, mb.ID, data.UIDValidity, next)
		return m.checkNew(c, user, mb)
	}); err != nil {
		return classify(err)
	}
	m.setStatus(user, mb.ID, StateOK, "")
	for {
		idle, err := c.Idle()
		if err != nil {
			return classify(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-c.Closed():
			return ErrUnreachable
		case <-wake:
		case <-time.After(m.idleRestart):
		}
		if err := limited(func() error {
			if err := idle.Close(); err != nil {
				return err
			}
			if err := idle.Wait(); err != nil {
				return err
			}
			return m.checkNew(c, user, mb)
		}); err != nil {
			return classify(err)
		}
	}
}

// baselineFor returns where new mail starts; the first connection after a
// start of titand (or a new UIDVALIDITY) sets it to UIDNEXT.
func (m *Manager) baselineFor(user, id string, validity, next uint32) baseline {
	k := key(user, id)
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	m.watch.init()
	b, ok := m.watch.baseline[k]
	if !ok || b.validity != validity {
		b = baseline{validity: validity, next: next}
		m.watch.baseline[k] = b
	}
	return b
}

func (m *Manager) advance(user, id string, uid uint32) {
	k := key(user, id)
	m.watch.mu.Lock()
	defer m.watch.mu.Unlock()
	if b, ok := m.watch.baseline[k]; ok && uid+1 > b.next {
		b.next = uid + 1
		m.watch.baseline[k] = b
	}
}

// checkNew handles the mail of the INBOX from the baseline on.
func (m *Manager) checkNew(c *imapclient.Client, user string, mb Mailbox) error {
	m.watch.mu.Lock()
	b := m.watch.baseline[key(user, mb.ID)]
	m.watch.mu.Unlock()
	data, err := c.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{{imap.UIDRange{Start: imap.UID(b.next), Stop: 0}}}}, nil).Wait()
	if err != nil {
		return err
	}
	var uids []uint32
	for _, u := range data.AllUIDs() {
		if uint32(u) >= b.next {
			uids = append(uids, uint32(u))
		}
	}
	slices.Sort(uids)
	if len(uids) > 0 {
		msgs, err := c.Fetch(uidSet(uids...), &imap.FetchOptions{UID: true, Envelope: true, Flags: true}).Collect()
		if err != nil {
			return err
		}
		slices.SortFunc(msgs, func(a, b *imapclient.FetchMessageBuffer) int { return int(int64(a.UID) - int64(b.UID)) })
		rules := m.rulesOf(user, mb.ID)
		var told []Summary
		for _, msg := range msgs {
			s := summaryOf(msg)
			if folder, uid, moved := m.applyRules(c, user, mb, rules, msg); moved {
				m.events.publish(user, Event{Type: "mail", Data: map[string]any{"account": mb.ID, "folder": folder, "uid": uid}})
			} else {
				m.events.publish(user, Event{Type: "mail", Data: map[string]any{"account": mb.ID, "folder": "INBOX", "uid": s.UID}})
				if !s.Flags.Seen {
					told = append(told, s)
				}
			}
			m.advance(user, mb.ID, s.UID)
		}
		if mb.Notify && len(told) > 0 {
			last := told[len(told)-1]
			from := last.From.Name
			if from == "" {
				from = last.From.Addr
			}
			m.notice(user, mb, len(told), from, last.Subject)
		}
	}
	unread, err := c.UIDSearch(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen, imap.FlagDeleted}}, nil).Wait()
	if err == nil {
		m.setUnread(user, mb.ID, len(unread.AllUIDs()))
	}
	return nil
}

// ruleMatches tells whether a rule applies to a mail.
func ruleMatches(r Rule, e *imap.Envelope) bool {
	if e == nil {
		return false
	}
	text := func(list ...[]imap.Address) string {
		var parts []string
		for _, l := range list {
			for _, a := range l {
				parts = append(parts, a.Name, a.Addr())
			}
		}
		return strings.ToLower(strings.Join(parts, " "))
	}
	has := func(hay, needle string) bool {
		return needle == "" || strings.Contains(hay, strings.ToLower(needle))
	}
	return has(text(e.From, e.Sender), r.When.From) && has(text(e.To, e.Cc), r.When.To) && has(strings.ToLower(e.Subject), r.When.Subject)
}

// applyRules runs the rules on a new mail in order; a move ends them. It
// reports where the mail went if it was moved.
func (m *Manager) applyRules(c *imapclient.Client, user string, mb Mailbox, rules []Rule, msg *imapclient.FetchMessageBuffer) (string, uint32, bool) {
	uid := uint32(msg.UID)
	for _, r := range rules {
		if !ruleMatches(r, msg.Envelope) {
			continue
		}
		var add []imap.Flag
		if r.Then.MarkRead {
			add = append(add, imap.FlagSeen)
			msg.Flags = append(msg.Flags, imap.FlagSeen)
		}
		if r.Then.Flag {
			add = append(add, imap.FlagFlagged)
		}
		if len(add) > 0 {
			if err := c.Store(uidSet(uid), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: add}, nil).Close(); err != nil {
				slog.Warn("T-Mail: rule could not set flags", "account", user, "rule", r.ID, "err", safeText(err.Error()))
			}
		}
		if r.Then.MoveTo != "" && !strings.EqualFold(r.Then.MoveTo, "INBOX") {
			data, err := c.Move(uidSet(uid), r.Then.MoveTo).Wait()
			if err != nil {
				slog.Warn("T-Mail: rule could not move a mail", "account", user, "rule", r.ID, "err", safeText(err.Error()))
				continue
			}
			var dest uint32
			if data != nil && data.DestUIDs != nil {
				if set, ok := data.DestUIDs.(imap.UIDSet); ok {
					if nums, ok := set.Nums(); ok && len(nums) == 1 {
						dest = uint32(nums[0])
					}
				}
			}
			return r.Then.MoveTo, dest, true
		}
	}
	return "", 0, false
}

// noticeState collects new mail of a mailbox for a moment, so that many
// mails at once make one message.
type noticeState struct {
	mu      sync.Mutex
	pending map[string]*Notice
}

func (m *Manager) notice(user string, mb Mailbox, count int, from, subject string) {
	k := key(user, mb.ID)
	n := &m.notices
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pending == nil {
		n.pending = map[string]*Notice{}
	}
	if p := n.pending[k]; p != nil {
		p.Count += count
		p.From, p.Subject = from, subject
		return
	}
	n.pending[k] = &Notice{User: user, Mailbox: mb.ID, MailboxName: mb.Name, Count: count, From: from, Subject: subject}
	time.AfterFunc(m.noticeWindow, func() {
		n.mu.Lock()
		p := n.pending[k]
		delete(n.pending, k)
		n.mu.Unlock()
		if p == nil || m.Notify == nil || !m.Allowed(user) {
			return
		}
		p.Content = m.pushContent(user)
		m.Notify(*p)
	})
}

// Code is the error code of the API for an error of the package.
func Code(err error) string {
	for _, c := range []struct {
		err  error
		code string
	}{
		{ErrDisabled, "mail_disabled"}, {ErrAuthFailed, "mail_auth_failed"}, {ErrTLS, "mail_tls"},
		{ErrHostForbidden, "mail_host_forbidden"}, {ErrTimeout, "mail_timeout"}, {ErrTooLarge, "mail_too_large"},
		{ErrNotFound, "mail_not_found"}, {ErrFolderExists, "mail_folder_exists"}, {ErrSendFailed, "mail_send_failed"},
		{ErrPasswordRequired, "mail_password_required"}, {ErrInvalid, "mail_invalid"}, {ErrNoPassword, "mail_no_password"}, {ErrUnreadable, "mail_settings_unreadable"},
		{ErrUnreachable, "mail_unreachable"},
	} {
		if errors.Is(err, c.err) {
			return c.code
		}
	}
	return "mail_unreachable"
}
