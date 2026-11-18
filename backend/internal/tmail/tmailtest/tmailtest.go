// Package tmailtest runs mail servers in the process for the tests of
// T-Mail: go-imap's imapmemserver behind TLS and a go-smtp server, both
// with a certificate made for the test that only the test trusts. Only
// tests import it; nothing leaves the host.
package tmailtest

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
)

// The login of the test's mailbox.
const (
	User     = "alice@example.org"
	Password = "imap-secret-4711"
)

// Received is a mail the SMTP server took.
type Received struct {
	From string
	To   []string
	Data []byte
}

// Servers are an IMAP and an SMTP server with implicit TLS on 127.0.0.1.
type Servers struct {
	t        testing.TB
	IMAPAddr string
	SMTPAddr string
	// Pool trusts the servers' certificate.
	Pool *x509.CertPool
	// Mailbox is the IMAP account (folders INBOX, Sent, Drafts, Trash,
	// Archiv).
	Mailbox *imapmemserver.User
	// IMAPConns counts the connections the IMAP server accepted.
	IMAPConns atomic.Int32

	mu   sync.Mutex
	sent []Received
}

// Start starts both servers until the test ends.
func Start(t testing.TB) *Servers {
	t.Helper()
	s := &Servers{t: t}
	cert, pool := Certificate(t)
	s.Pool = pool
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	mem := imapmemserver.New()
	s.Mailbox = imapmemserver.NewUser(User, Password)
	for _, name := range []string{"INBOX", "Sent", "Drafts", "Trash", "Archiv"} {
		if err := s.Mailbox.Create(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	mem.AddUser(s.Mailbox)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:      imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}, imap.CapUIDPlus: {}},
		TLSConfig: tlsCfg,
		Logger:    discard{},
	})
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	s.IMAPAddr = ln.Addr().String()
	go func() { _ = srv.Serve(counting{ln, &s.IMAPConns}) }()
	t.Cleanup(func() { _ = srv.Close() })

	ss := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) { return &session{s: s}, nil }))
	ss.Domain = "localhost"
	ss.TLSConfig = tlsCfg
	ss.ErrorLog = discard{}
	sl, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	s.SMTPAddr = sl.Addr().String()
	go func() { _ = ss.Serve(sl) }()
	t.Cleanup(func() { _ = ss.Close() })
	return s
}

// Deliver puts a mail into a folder, as a provider would.
func (s *Servers) Deliver(folder, raw string) {
	s.t.Helper()
	s.DeliverAt(folder, raw, time.Time{})
}

// DeliverAt is Deliver with the date of arrival (INTERNALDATE); zero is now.
func (s *Servers) DeliverAt(folder, raw string, at time.Time) {
	s.t.Helper()
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\n", "\r\n")
	if _, err := s.Mailbox.Append(folder, literal{bytes.NewReader([]byte(raw)), int64(len(raw))}, &imap.AppendOptions{Time: at}); err != nil {
		s.t.Fatal(err)
	}
}

// Count is the number of mails in a folder.
func (s *Servers) Count(folder string) int {
	s.t.Helper()
	st, err := s.Mailbox.Status(folder, &imap.StatusOptions{NumMessages: true})
	if err != nil {
		s.t.Fatal(err)
	}
	return int(*st.NumMessages)
}

// Sent returns the mails the SMTP server took.
func (s *Servers) Sent() []Received {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Received(nil), s.sent...)
}

// Certificate makes a certificate for 127.0.0.1 and localhost and a pool
// that trusts it.
func Certificate(t testing.TB) (tls.Certificate, *x509.CertPool) {
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

type counting struct {
	net.Listener
	n *atomic.Int32
}

func (l counting) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.n.Add(1)
	}
	return c, err
}

type literal struct {
	*bytes.Reader
	size int64
}

func (l literal) Size() int64 { return l.size }

type discard struct{}

func (discard) Printf(string, ...any) {}
func (discard) Println(...any)        {}

// session takes mail after a PLAIN login with the test's password.
type session struct {
	s      *Servers
	authed bool
	from   string
	to     []string
}

func (x *session) AuthMechanisms() []string { return []string{sasl.Plain} }

func (x *session) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, user, pass string) error {
		if user != User || pass != Password {
			return smtp.ErrAuthFailed
		}
		x.authed = true
		return nil
	}), nil
}

func (x *session) Mail(from string, _ *smtp.MailOptions) error {
	if !x.authed {
		return smtp.ErrAuthRequired
	}
	x.from = from
	return nil
}

func (x *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	x.to = append(x.to, to)
	return nil
}

func (x *session) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	x.s.mu.Lock()
	x.s.sent = append(x.s.sent, Received{From: x.from, To: x.to, Data: data})
	x.s.mu.Unlock()
	return nil
}

func (x *session) Reset()        { x.from, x.to = "", nil }
func (x *session) Logout() error { return nil }
