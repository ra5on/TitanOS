package tmail

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Protocol tests run against servers in the process: go-imap's
// imapmemserver behind TLS and a go-smtp server, both with a certificate
// made for the test that only the test trusts. Nothing leaves the host.

const (
	testUser     = "alice@example.org"
	testPassword = "imap-secret-4711"
)

// testCert makes a certificate for 127.0.0.1 and a pool that trusts it.
func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "tmail test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}, pool
}

// countingListener counts the connections a server accepted.
type countingListener struct {
	net.Listener
	n *atomic.Int32
}

func (l countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.n.Add(1)
	}
	return c, err
}

// received is a mail the SMTP server took.
type received struct {
	from string
	to   []string
	data []byte
}

type testEnv struct {
	t        *testing.T
	m        *Manager
	store    *config.Store
	pool     *x509.CertPool
	cert     tls.Certificate
	imapUser *imapmemserver.User
	imapAddr string
	smtpAddr string
	imapConn atomic.Int32
	mu       sync.Mutex
	sent     []received
	notices  []Notice
	volume   string
	accounts map[string]Account
}

// newEnv starts an IMAP and an SMTP server with implicit TLS and a
// manager for the account "alex" with the grant.
func newEnv(t *testing.T) *testEnv {
	t.Helper()
	e := &testEnv{t: t, accounts: map[string]Account{}}
	e.cert, e.pool = testCert(t)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{e.cert}}

	mem := imapmemserver.New()
	e.imapUser = imapmemserver.NewUser(testUser, testPassword)
	for _, name := range []string{"INBOX", "Sent", "Drafts", "Trash", "Archiv"} {
		if err := e.imapUser.Create(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	mem.AddUser(e.imapUser)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:      imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}, imap.CapUIDPlus: {}},
		TLSConfig: tlsCfg,
		Logger:    discardLogger{},
	})
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	e.imapAddr = ln.Addr().String()
	go func() { _ = srv.Serve(countingListener{ln, &e.imapConn}) }()
	t.Cleanup(func() { _ = srv.Close() })

	be := smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) { return &smtpSession{e: e}, nil })
	ss := smtp.NewServer(be)
	ss.Domain = "localhost"
	ss.TLSConfig = tlsCfg
	ss.ErrorLog = discardLogger{}
	sl, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	e.smtpAddr = sl.Addr().String()
	go func() { _ = ss.Serve(sl) }()
	t.Cleanup(func() { _ = ss.Close() })

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
	m.RootCAs = e.pool
	m.targets = func(netip.AddrPort) error { return nil }
	m.noticeWindow = 200 * time.Millisecond
	m.backoffMin, m.backoffMax = 50*time.Millisecond, 200*time.Millisecond
	m.Notify = func(n Notice) {
		e.mu.Lock()
		e.notices = append(e.notices, n)
		e.mu.Unlock()
	}
	m.Volume = func() (*os.Root, func(), error) {
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
	ih, ip, _ := net.SplitHostPort(e.imapAddr)
	sh, sp, _ := net.SplitHostPort(e.smtpAddr)
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

// deliver puts a mail into a folder of the IMAP server, as a provider
// would.
func (e *testEnv) deliver(folder, raw string) {
	e.t.Helper()
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\n", "\r\n")
	if _, err := e.imapUser.Append(folder, literal{bytes.NewReader([]byte(raw)), int64(len(raw))}, &imap.AppendOptions{}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) count(folder string) int {
	e.t.Helper()
	st, err := e.imapUser.Status(folder, &imap.StatusOptions{NumMessages: true})
	if err != nil {
		e.t.Fatal(err)
	}
	return int(*st.NumMessages)
}

func (e *testEnv) noticeList() []Notice {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Notice(nil), e.notices...)
}

func (e *testEnv) sentMails() []received {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]received(nil), e.sent...)
}

type literal struct {
	*bytes.Reader
	size int64
}

func (l literal) Size() int64 { return l.size }

type discardLogger struct{}

func (discardLogger) Printf(string, ...any) {}
func (discardLogger) Println(...any)        {}

// smtpSession takes mail after a PLAIN login.
type smtpSession struct {
	e      *testEnv
	authed bool
	from   string
	to     []string
}

func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *smtpSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, user, pass string) error {
		if user != testUser || pass != testPassword {
			return smtp.ErrAuthFailed
		}
		s.authed = true
		return nil
	}), nil
}

func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error {
	if !s.authed {
		return smtp.ErrAuthRequired
	}
	s.from = from
	return nil
}

func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.e.mu.Lock()
	s.e.sent = append(s.e.sent, received{from: s.from, to: s.to, data: data})
	s.e.mu.Unlock()
	return nil
}

func (s *smtpSession) Reset()        { s.from, s.to = "", nil }
func (s *smtpSession) Logout() error { return nil }

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
