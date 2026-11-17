package tmail

import (
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/tmail/tmailtest"
)

// Protocol tests run against the servers of tmailtest in the process.

const (
	testUser     = tmailtest.User
	testPassword = tmailtest.Password
)

type testEnv struct {
	t        *testing.T
	m        *Manager
	store    *config.Store
	srv      *tmailtest.Servers
	mu       sync.Mutex
	notices  []Notice
	volume   string
	accounts map[string]Account
}

// newEnv starts the servers and a manager for the account "alex" with
// the grant.
func newEnv(t *testing.T) *testEnv {
	t.Helper()
	e := &testEnv{t: t, accounts: map[string]Account{}, srv: tmailtest.Start(t)}
	e.store = config.Open(t.TempDir())
	e.volume = t.TempDir()
	e.accounts["alex"] = Account{Name: "alex", Created: time.Now().Add(-time.Hour).UTC(), Admin: true}
	e.m = e.newManager()
	if err := e.m.SetAllowed("alex", true); err != nil {
		t.Fatal(err)
	}
	return e
}

// newManager is a manager as titand makes it at a start, with the test's
// trust, servers on loopback and short pauses.
func (e *testEnv) newManager() *Manager {
	m := New(e.store)
	m.Account = func(name string) (Account, bool) {
		e.mu.Lock()
		defer e.mu.Unlock()
		a, ok := e.accounts[name]
		return a, ok
	}
	m.RootCAs = e.srv.Pool
	m.Targets = func(netip.AddrPort) error { return nil }
	m.noticeWindow = 200 * time.Millisecond
	m.backoffMin, m.backoffMax = 50*time.Millisecond, 200*time.Millisecond
	m.Notify = func(n Notice) {
		e.mu.Lock()
		e.notices = append(e.notices, n)
		e.mu.Unlock()
	}
	m.Volume = func(bool) (*os.Root, func(), error) {
		root, err := os.OpenRoot(e.volume)
		if err != nil {
			return nil, nil, err
		}
		return root, func() { root.Close() }, nil
	}
	if err := m.Load(); err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *testEnv) input(password string) MailboxInput {
	ih, ip, _ := net.SplitHostPort(e.srv.IMAPAddr)
	sh, sp, _ := net.SplitHostPort(e.srv.SMTPAddr)
	iport, _ := strconv.Atoi(ip)
	sport, _ := strconv.Atoi(sp)
	return MailboxInput{Name: "Privat", Email: testUser, SenderName: "Alice", User: testUser, Password: password,
		IMAP: Server{Host: ih, Port: iport, Security: SecurityTLS}, SMTP: Server{Host: sh, Port: sport, Security: SecurityTLS}}
}

func (e *testEnv) addMailbox() string {
	e.t.Helper()
	view, err := e.m.AddMailbox("alex", e.input(testPassword))
	if err != nil {
		e.t.Fatal(err)
	}
	return view.ID
}

func (e *testEnv) deliver(folder, raw string) { e.srv.Deliver(folder, raw) }

func (e *testEnv) count(folder string) int { return e.srv.Count(folder) }

func (e *testEnv) noticeList() []Notice {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Notice(nil), e.notices...)
}

func (e *testEnv) sentMails() []tmailtest.Received { return e.srv.Sent() }

// waitFor polls a condition for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
