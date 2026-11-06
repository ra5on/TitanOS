package imapmini

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// FakeServer is a tiny IMAP server for tests: one INBOX, fixed mails.
type FakeServer struct {
	Addr     string
	Pool     *x509.CertPool
	User     string
	Password string
	StartTLS bool

	mu       sync.Mutex
	Mails    map[uint32][]byte
	Seen     map[uint32]bool
	Commands []string
	ln       net.Listener
	cert     tls.Certificate
}

func newFake(t *testing.T, starttls bool) *FakeServer {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mail.test"}, DNSNames: []string{"mail.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &FakeServer{Addr: ln.Addr().String(), Pool: pool, User: "alex@mail.test", Password: `ge"heim\1`, StartTLS: starttls,
		Mails: map[uint32][]byte{}, Seen: map[uint32]bool{}, ln: ln, cert: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}}
	t.Cleanup(func() { ln.Close() })
	go f.serve()
	return f
}

func (f *FakeServer) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(c)
	}
}

func (f *FakeServer) handle(raw net.Conn) {
	defer raw.Close()
	tc := &tls.Config{Certificates: []tls.Certificate{f.cert}}
	conn := net.Conn(raw)
	if !f.StartTLS {
		conn = tls.Server(raw, tc)
	}
	r := bufio.NewReader(conn)
	fmt.Fprint(conn, "* OK fake IMAP ready\r\n")
	authed := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		tag, rest, _ := strings.Cut(line, " ")
		f.mu.Lock()
		if strings.HasPrefix(rest, "LOGIN") {
			f.Commands = append(f.Commands, "LOGIN ***")
		} else {
			f.Commands = append(f.Commands, rest)
		}
		f.mu.Unlock()
		switch {
		case rest == "STARTTLS" && f.StartTLS:
			fmt.Fprintf(conn, "%s OK begin TLS\r\n", tag)
			conn = tls.Server(raw, tc)
			r = bufio.NewReader(conn)
		case strings.HasPrefix(rest, "LOGIN "):
			want := "LOGIN " + quote(f.User) + " " + quote(f.Password)
			if rest == want {
				authed = true
				fmt.Fprintf(conn, "%s OK logged in\r\n", tag)
			} else {
				fmt.Fprintf(conn, "%s NO [AUTHENTICATIONFAILED] wrong\r\n", tag)
			}
		case !authed:
			fmt.Fprintf(conn, "%s BAD login first\r\n", tag)
		case rest == "SELECT INBOX":
			fmt.Fprintf(conn, "* %d EXISTS\r\n%s OK [READ-WRITE] selected\r\n", len(f.Mails), tag)
		case rest == "UID SEARCH UNSEEN":
			f.mu.Lock()
			var ids []string
			for uid := range f.Mails {
				if !f.Seen[uid] {
					ids = append(ids, fmt.Sprint(uid))
				}
			}
			f.mu.Unlock()
			fmt.Fprintf(conn, "* SEARCH %s\r\n%s OK done\r\n", strings.Join(ids, " "), tag)
		case strings.HasPrefix(rest, "UID FETCH "):
			var uid uint32
			fmt.Sscanf(rest, "UID FETCH %d BODY.PEEK[]", &uid)
			f.mu.Lock()
			body := f.Mails[uid]
			f.mu.Unlock()
			fmt.Fprintf(conn, "* 1 FETCH (UID %d BODY[] {%d}\r\n", uid, len(body))
			conn.Write(body)
			fmt.Fprintf(conn, ")\r\n%s OK fetched\r\n", tag)
		case strings.HasPrefix(rest, "UID STORE "):
			var uid uint32
			fmt.Sscanf(rest, "UID STORE %d", &uid)
			f.mu.Lock()
			f.Seen[uid] = true
			f.mu.Unlock()
			fmt.Fprintf(conn, "%s OK stored\r\n", tag)
		case rest == "LOGOUT":
			fmt.Fprintf(conn, "* BYE\r\n%s OK bye\r\n", tag)
			return
		default:
			fmt.Fprintf(conn, "%s BAD unknown\r\n", tag)
		}
	}
}

func (f *FakeServer) config(security string) Config {
	return Config{Host: "mail.test", Port: 993, Security: security, User: f.User, Password: f.Password, TLS: &tls.Config{RootCAs: f.Pool},
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, f.Addr)
		}}
}

const invoice = "From: =?utf-8?q?Stadtwerke_M=C3=BCnster?= <rechnung@stadtwerke.test>\r\n" +
	"To: alex@mail.test\r\nSubject: =?utf-8?q?Ihre_Abschlagsrechnung_November?=\r\n" +
	"Date: Tue, 03 Nov 2026 08:15:00 +0100\r\nMessage-ID: <abc123@stadtwerke.test>\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"XX\"\r\n\r\n" +
	"--XX\r\nContent-Type: multipart/alternative; boundary=\"YY\"\r\n\r\n" +
	"--YY\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"Abschlag 86,00 =E2=82=AC, f=C3=A4llig am 15.11.2026.\r\n" +
	"--YY\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Abschlag <b>86,00 &euro;</b></p>\r\n--YY--\r\n" +
	"--XX\r\nContent-Type: application/pdf; name=\"../../Rechnung.pdf\"\r\nContent-Disposition: attachment; filename=\"../../Rechnung.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\nJSVFT0YK\r\n--XX--\r\n"

func TestFetchUnseenOverTLS(t *testing.T) {
	for _, sec := range []string{TLS, StartTLS} {
		t.Run(sec, func(t *testing.T) {
			f := newFake(t, sec == StartTLS)
			f.Mails[7] = []byte(invoice)
			f.Mails[8] = []byte("From: a@b.test\r\nSubject: alt\r\n\r\nschon gelesen\r\n")
			f.Seen[8] = true
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, err := Dial(ctx, f.config(sec))
			if err != nil {
				t.Fatal(err)
			}
			if err := c.SelectInbox(); err != nil {
				t.Fatal(err)
			}
			uids, err := c.Unseen()
			if err != nil || len(uids) != 1 || uids[0] != 7 {
				t.Fatalf("unseen %v %v", uids, err)
			}
			raw, err := c.Fetch(7)
			if err != nil || string(raw) != invoice {
				t.Fatalf("fetch: %v %q", err, raw)
			}
			if f.Seen[7] {
				t.Fatal("PEEK marked read")
			}
			if err := c.MarkSeen(7); err != nil || !f.Seen[7] {
				t.Fatal("store")
			}
			c.Close()
			for _, cmd := range f.Commands {
				if strings.Contains(cmd, "EXPUNGE") || strings.Contains(cmd, "Deleted") || strings.Contains(cmd, "MOVE") || strings.Contains(cmd, "COPY") {
					t.Fatalf("changed the mailbox: %s", cmd)
				}
			}
		})
	}
}

func TestLoginRefusedKeepsPasswordOut(t *testing.T) {
	f := newFake(t, false)
	cfg := f.config(TLS)
	cfg.Password = "falsch-sehr-geheim"
	_, err := Dial(context.Background(), cfg)
	if err != ErrAuth || strings.Contains(err.Error(), "geheim") {
		t.Fatalf("%v", err)
	}
}

func TestCertificateIsChecked(t *testing.T) {
	f := newFake(t, false)
	cfg := f.config(TLS)
	cfg.TLS = &tls.Config{InsecureSkipVerify: true} // must not switch the check off
	if _, err := Dial(context.Background(), cfg); err == nil {
		t.Fatal("unknown certificate accepted")
	}
	cfg.Security = "none"
	if _, err := Dial(context.Background(), cfg); err == nil {
		t.Fatal("plain connection allowed")
	}
}

func TestParseMail(t *testing.T) {
	m, err := Parse([]byte(invoice))
	if err != nil {
		t.Fatal(err)
	}
	if m.FromName != "Stadtwerke Münster" || m.From != "rechnung@stadtwerke.test" || m.Subject != "Ihre Abschlagsrechnung November" || m.MessageID != "abc123@stadtwerke.test" {
		t.Fatalf("header: %+v", m)
	}
	if !strings.Contains(m.Text, "86,00 €, fällig am 15.11.2026") {
		t.Fatalf("text: %q", m.Text)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Name != "Rechnung.pdf" || !strings.HasPrefix(string(m.Attachments[0].Data), "%PDF-1.4") {
		t.Fatalf("attachments: %+v", m.Attachments)
	}
	// HTML only, Latin-1.
	m, _ = Parse([]byte("From: x@y.test\r\nContent-Type: text/html; charset=iso-8859-1\r\n\r\n<p>Betrag f\xe4llig</p><script>x()</script>"))
	if m.Text != "Betrag fällig" {
		t.Fatalf("html: %q", m.Text)
	}
	for in, want := range map[string]string{"../x.pdf": "x.pdf", ".bashrc": "bashrc", "": "Anhang.pdf", "a\x01b.pdf": "a_b.pdf", `C:\evil\y.png`: "y.png"} {
		if got := SafeFileName(in, "application/pdf"); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
