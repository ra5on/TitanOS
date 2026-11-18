package tmail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
)

// Lanes (1.13, owner 18 Nov 2026 "zweite IMAP-Verbindung"): a mailbox
// has up to two connections of the web UI. The page's lane opens mails,
// lists folders' mail, sets flags, moves and sends; the background lane
// counts the folders (STATUS) and searches all folders. So a search of up
// to a minute or the counts of a hundred folders never hold up opening a
// mail. With the permanent IDLE connection (watch.go) a mailbox has at
// most three connections to its provider. A provider that refuses the
// second one (iCloud and Gmail limit them) is remembered for
// singleFor: its background work then waits on the one connection, as
// before 1.13; the refusal never counts as a wrong password.

type lane int

const (
	laneUI lane = iota
	laneBackground
)

// singleFor is how long a refused second connection is not tried again.
const singleFor = 30 * time.Minute

// errSecondRefused: the provider did not take a second connection.
var errSecondRefused = errors.New("tmail: second connection refused")

type laneCtxKey struct{}

// background marks work for the background lane.
func background(ctx context.Context) context.Context {
	return context.WithValue(ctx, laneCtxKey{}, laneBackground)
}

func laneOf(ctx context.Context) lane {
	if l, ok := ctx.Value(laneCtxKey{}).(lane); ok {
		return l
	}
	return laneUI
}

// withBackground runs work on the background lane, or on the page's when
// that has no connection yet or the provider allows one connection only.
func (m *Manager) withBackground(ctx context.Context, user, id string, limit time.Duration, work func(c *imapclient.Client, s *session) error) error {
	// The second connection only next to a working first one: a mailbox
	// whose login, address or certificate is wrong fails on the page's
	// connection with the errors and pauses it always had.
	if !m.conns.isSingle(user, id, time.Now()) && m.conns.live(poolKey{user, id, laneUI}) {
		err := m.withLane(ctx, user, id, laneBackground, limit, work)
		if !errors.Is(err, errSecondRefused) {
			return err
		}
		slog.Info("T-Mail: the provider refused a second connection; the mailbox uses one", "user", user, "mailbox", id, "err", errors.Unwrap(err))
		m.conns.setSingle(user, id, time.Now().Add(singleFor))
	}
	return m.withLane(ctx, user, id, laneUI, limit, work)
}

// live tells whether a lane holds an open connection now.
func (p *connPool) live(k poolKey) bool {
	p.mu.Lock()
	s := p.sessions[k]
	p.mu.Unlock()
	if s == nil {
		return false
	}
	c := s.client()
	if c == nil {
		return false
	}
	select {
	case <-c.Closed():
		return false
	default:
		return true
	}
}

func (p *connPool) isSingle(user, id string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	until, ok := p.single[key(user, id)]
	if ok && now.After(until) {
		delete(p.single, key(user, id))
		return false
	}
	return ok
}

func (p *connPool) setSingle(user, id string, until time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.single == nil {
		p.single = map[string]time.Time{}
	}
	p.single[key(user, id)] = until
	// The background lane's session holds no connection; dropped.
	delete(p.sessions, poolKey{user, id, laneBackground})
}

// The folder list with its counts is kept a short while (folderTTL) for
// a mailbox in which nothing happened since: no command of the page, no
// event (new mail, flags, status). A page that asks again after every
// event of another window gets it without asking the provider.

// folderTTL is how long a list of folders with counts stays.
const folderTTL = 15 * time.Second

// maxKeptFolders bounds the list of one mailbox that is kept (bytes of
// its names): a hundred folders take some 10 KB.
const maxKeptFolders = 256 << 10

type folderEntry struct {
	list []Folder
	gen  uint64
	fp   string // the mailbox's settings it was read with
	at   time.Time
}

type folderCache struct {
	m map[string]folderEntry
}

// cachedFolders returns the kept folders of a mailbox if nothing happened
// since (generation gen) and they are young enough.
func (m *Manager) cachedFolders(user, id string, gen uint64, now time.Time) ([]Folder, bool) {
	fp, ok := m.folderPrint(user, id)
	if !ok {
		return nil, false
	}
	m.conns.mu.Lock()
	defer m.conns.mu.Unlock()
	e, ok := m.conns.folders.m[key(user, id)]
	if !ok || e.gen != gen || e.fp != fp || now.Sub(e.at) >= folderTTL || now.Before(e.at) {
		return nil, false
	}
	return slices.Clone(e.list), true
}

func (m *Manager) keepFolders(user, id string, list []Folder, gen uint64, now time.Time) {
	size := 0
	for _, f := range list {
		size += len(f.Path) + len(f.Name) + 64
	}
	if size > maxKeptFolders {
		return // what a hostile server sends is not kept beyond its request
	}
	fp, ok := m.folderPrint(user, id)
	if !ok {
		return
	}
	m.conns.mu.Lock()
	defer m.conns.mu.Unlock()
	if m.conns.folders.m == nil {
		m.conns.folders.m = map[string]folderEntry{}
	}
	m.conns.folders.m[key(user, id)] = folderEntry{list: slices.Clone(list), gen: gen, fp: fp, at: now}
}

// folderPrint names what kept counts depend on: the mailbox's settings
// and the account's role (as the connection's fingerprint); ok is false
// for a mailbox that is gone or an account without T-Mail.
func (m *Manager) folderPrint(user, id string) (string, bool) {
	if !m.Allowed(user) {
		return "", false
	}
	mb, err := m.mailbox(user, id)
	if err != nil || m.authFailed(user, mb) {
		return "", false
	}
	return fingerprint(mb) + fmt.Sprint(m.confined(user)), true
}

// touch counts something that happened in a mailbox (a command of the
// page or an event): kept folder counts are stale.
func (h *hub) touch(user, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.touchLocked(key(user, id))
}

func (h *hub) touchLocked(k string) {
	if h.gens == nil {
		h.gens = map[string]uint64{}
	}
	h.gens[k]++
}

// generation is the count of touch for a mailbox.
func (h *hub) generation(user, id string) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.gens[key(user, id)]
}
