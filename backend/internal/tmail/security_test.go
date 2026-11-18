package tmail

// Regression tests of the security review of 1.05 (scenarios of its
// proofs of concept). Each failed with the code before the fixes.

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/ra5on/TitanOS/backend/internal/tmail/tmailtest"
)

// scripted is a server of the test speaking a protocol by hand over TLS;
// answer gets each command line and writes the reply.
func scripted(t *testing.T, greeting string, answer func(w *bufio.Writer, tag, cmd, line string) bool) (string, *x509.CertPool, *atomic.Int32) {
	t.Helper()
	cert, pool := tmailtest.Certificate(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	conns := &atomic.Int32{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				w := bufio.NewWriterSize(conn, 1<<20)
				fmt.Fprint(w, greeting)
				w.Flush()
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					tag, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
					cmd := strings.ToUpper(strings.Fields(rest + " x")[0])
					if !answer(w, tag, cmd, line) {
						w.Flush()
						return
					}
					if w.Flush() != nil {
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String(), pool, conns
}

// hugeIMAP answers every command after LOGIN with a literal of size bytes.
func hugeIMAP(t *testing.T, size int64) (string, *x509.CertPool, *atomic.Int32, *atomic.Int64) {
	sent := &atomic.Int64{}
	chunk := []byte(strings.Repeat("A", 1<<20))
	addr, pool, conns := scripted(t, "* OK [CAPABILITY IMAP4rev1] evil\r\n", func(w *bufio.Writer, tag, cmd, _ string) bool {
		switch cmd {
		case "LOGIN":
			fmt.Fprintf(w, "%s OK logged in\r\n", tag)
		case "LOGOUT":
			fmt.Fprintf(w, "* BYE\r\n%s OK\r\n", tag)
			return false
		case "CAPABILITY":
			fmt.Fprintf(w, "* CAPABILITY IMAP4rev1\r\n%s OK\r\n", tag)
		default:
			fmt.Fprintf(w, "* LIST () \"/\" {%d}\r\n", size)
			for left := size; left > 0; {
				n := min(left, int64(len(chunk)))
				if _, err := w.Write(chunk[:n]); err != nil {
					return false
				}
				sent.Add(n)
				left -= n
			}
			fmt.Fprintf(w, "\r\n%s OK done\r\n", tag)
		}
		return true
	})
	return addr, pool, conns, sent
}

func hostPort(t *testing.T, addr string) (string, int) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(port)
	return host, p
}

// peakHeap runs work and reports the highest heap seen meanwhile.
func peakHeap(work func()) uint64 {
	var peak atomic.Uint64
	done := make(chan struct{})
	go func() {
		var ms runtime.MemStats
		for {
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > peak.Load() {
				peak.Store(ms.HeapAlloc)
			}
			select {
			case <-done:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	runtime.GC()
	work()
	close(done)
	return peak.Load()
}

// HIGH: a hostile server cannot make titand keep an endless answer.
func TestHugeLiteralIsCutOff(t *testing.T) {
	addr, pool, _, sent := hugeIMAP(t, 512<<20)
	e := newEnv(t)
	e.m.RootCAs = pool
	in := e.input("x")
	in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	var folderErr error
	peak := peakHeap(func() { _, folderErr = e.m.Folders(t.Context(), "alex", view.ID) })
	if !errors.Is(folderErr, ErrTooLarge) {
		t.Fatalf("folders: %v", folderErr)
	}
	if peak > 96<<20 || sent.Load() > 96<<20 {
		t.Fatalf("peak heap %d MiB, server sent %d MiB", peak>>20, sent.Load()>>20)
	}
}

// HIGH: the permanent connection is cut off as well and tried again only
// after the pause, never at once.
func TestWatcherTooLargeBacksOff(t *testing.T) {
	addr, pool, conns, _ := hugeIMAP(t, 64<<20)
	e := newEnv(t)
	e.m.RootCAs = pool
	e.m.backoffMin, e.m.backoffMax = 2*time.Second, 4*time.Second
	in := e.input("x")
	in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
	in.Notify = true
	if _, err := e.m.AddMailbox("alex", in); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go e.m.Run(ctx)
	waitFor(t, "too large", func() bool {
		st := e.m.Mailboxes("alex")[0].Status
		return st.State == StateUnreachable && st.Code == "mail_too_large"
	})
	time.Sleep(time.Second)
	if n := conns.Load(); n != 1 {
		t.Fatalf("%d connections within the pause", n)
	}
}

// HIGH: an SMTP server with an endless answer of many lines.
func TestSMTPAnswerBounded(t *testing.T) {
	addr, pool, _ := scripted(t, "220 evil ESMTP\r\n", func(w *bufio.Writer, _, cmd, line string) bool {
		if strings.HasPrefix(strings.ToUpper(line), "EHLO") {
			for range 200000 {
				w.WriteString("250-X\r\n")
			}
			w.WriteString("250 AUTH PLAIN\r\n")
		}
		return true
	})
	e := newEnv(t)
	e.m.RootCAs = pool
	in := e.input(testPassword)
	in.SMTP.Host, in.SMTP.Port = hostPort(t, addr)
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Send(t.Context(), "alex", view.ID, Outgoing{To: []Address{{Addr: "bob@example.net"}}, Text: "x"}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("send: %v", err)
	}
}

// HIGH: lists of UIDs are never expanded whole, and a mail with very many
// parts does not make the list too large.
func TestUIDSetsBounded(t *testing.T) {
	set := imap.UIDSet{{Start: 1, Stop: 4000000000}}
	if n := uidCount(set); n != 1<<31-1 {
		t.Fatalf("count %d", n)
	}
	if got := uidsDesc(set, 0, 3); fmt.Sprint(got) != "[4000000000 3999999999 3999999998]" {
		t.Fatalf("desc %v", got)
	}
	if got := uidsDesc(set, 10, 3); fmt.Sprint(got) != "[9 8 7]" {
		t.Fatalf("before 10: %v", got)
	}
	if got := uidsFrom(set, 3999999999, 5); fmt.Sprint(got) != "[3999999999 4000000000]" {
		t.Fatalf("from %v", got)
	}
	e := newEnv(t)
	id := e.addMailbox()
	var b strings.Builder
	b.WriteString("From: x@example.net\r\nSubject: many\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n")
	for range 50000 {
		b.WriteString("--b\r\nContent-Type: text/plain\r\n\r\nx\r\n")
	}
	b.WriteString("--b--\r\n")
	for range 8 {
		e.deliver("INBOX", b.String())
	}
	page, err := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "INBOX"})
	if err != nil || len(page.Messages) != 8 || !page.Messages[0].Attachments {
		t.Fatalf("list: %d %v", len(page.Messages), err)
	}
}

// MEDIUM: a stored mailbox whose port changes again and again is no port
// scanner: new targets are limited per account.
func TestNewTargetsLimited(t *testing.T) {
	e := newEnv(t)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := l.Addr().(*net.TCPAddr).Port
	l.Close()
	in := e.input("whatever")
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	// One login to the real server first: its address is known then.
	if _, err := e.m.CheckSettings(t.Context(), "alex", "", e.input(testPassword)); err != nil {
		t.Fatal(err)
	}
	limited := false
	for i := range NewTargetLimit + 3 {
		in.IMAP.Port = closed + 1 + i
		in.Password = "guess-" + strconv.Itoa(i)
		if _, err := e.m.UpdateMailbox("alex", view.ID, in); err != nil {
			t.Fatal(err)
		}
		if _, err := e.m.Folders(t.Context(), "alex", view.ID); errors.Is(err, ErrRateLimited) {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("no limit for new targets")
	}
	// A server the account logged in to before is not limited.
	ok := e.input(testPassword)
	if _, err := e.m.UpdateMailbox("alex", view.ID, ok); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Folders(t.Context(), "alex", view.ID); err != nil {
		t.Fatalf("known server: %v", err)
	}
}

// MEDIUM: with IPv6 the home network has public addresses; a confined
// account reaches neither them nor the NAS itself, on any port.
func TestConfinedNotOwnNetworks(t *testing.T) {
	ownGUA := netip.MustParseAddr("2a02:8108:1234:5600::10")
	own := []netip.Addr{netip.MustParseAddr("192.168.1.10"), ownGUA}
	ports := []int{443, 80, 8750, 8751, 8752}
	for _, target := range []netip.AddrPort{
		netip.AddrPortFrom(ownGUA, 22), netip.AddrPortFrom(ownGUA, 445), netip.AddrPortFrom(ownGUA, 8123),
		netip.MustParseAddrPort("[2a02:8108:1234:5600::1]:443"), netip.MustParseAddrPort("[2a02:8108:1234:56aa::5]:993"),
		netip.MustParseAddrPort("[2002:c0a8:010a::1]:993"), netip.MustParseAddrPort("[64:ff9b:1::c0a8:10a]:993"),
	} {
		if err := CheckTargetFor(target, own, ports, true); !errors.Is(err, ErrConfined) {
			t.Errorf("%s reached: %v", target, err)
		}
	}
	if err := CheckTargetFor(netip.MustParseAddrPort("[2a02:8108:1234:5600::1]:993"), own, ports, false); err != nil {
		t.Errorf("administrator: %v", err)
	}
	if err := CheckTargetFor(netip.MustParseAddrPort("[2a00:1450:4001::1]:993"), own, ports, true); err != nil {
		t.Errorf("internet: %v", err)
	}
}

// LOW: a file without a creation time is not given to an account that has
// one.
func TestZeroCreatedNotInherited(t *testing.T) {
	e := newEnv(t)
	if err := e.store.WriteJSON(userPath("bob"), userFile{Owner: owner{Name: "bob"}, Mailboxes: []Mailbox{{ID: "abc", Name: "old bob", Email: "bob@example.org"}}}); err != nil {
		t.Fatal(err)
	}
	e.accounts["bob"] = Account{Name: "bob", Created: time.Now().UTC()}
	if got := e.newManager().Mailboxes("bob"); len(got) != 0 {
		t.Fatalf("inherited: %v", got)
	}
}

// LOW: a password sealed for an account does not open for a later
// account of the same name.
func TestPasswordBoundToCreation(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	e.m.mu.Lock()
	f := e.m.users["alex"]
	e.m.mu.Unlock()
	if !strings.HasPrefix(f.Mailboxes[0].Password, "v2:") {
		t.Fatalf("form %q", f.Mailboxes[0].Password[:3])
	}
	mb, _ := e.m.mailbox("alex", id)
	e.accounts["alex"] = Account{Name: "alex", Created: e.accounts["alex"].Created.Add(time.Second), Admin: true}
	if _, err := e.m.password("alex", mb); err == nil {
		t.Fatal("opened for another account of the name")
	}
}

// LOW: a demoted administrator keeps no connection into the home network.
func TestDemotionEndsHomeNetworkConnections(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go e.m.Run(ctx)
	in := e.input(testPassword)
	in.Notify = true
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "watcher", func() bool { return e.m.Mailboxes("alex")[0].Status.State == StateOK })
	if _, err := e.m.Folders(t.Context(), "alex", view.ID); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	a := e.accounts["alex"]
	a.Admin = false
	e.accounts["alex"] = a
	e.mu.Unlock()
	e.m.RoleChanged("alex")
	waitFor(t, "refused watcher", func() bool {
		st := e.m.Mailboxes("alex")[0].Status
		return st.State == StateUnreachable && st.Code == "mail_host_forbidden"
	})
	if _, err := e.m.Folders(t.Context(), "alex", view.ID); !errors.Is(err, ErrConfined) {
		t.Fatalf("web UI connection after demotion: %v", err)
	}
	e.deliver("INBOX", plainMail("x@example.net", "after demotion", "hi"))
	time.Sleep(500 * time.Millisecond)
	if len(e.noticeList()) != 0 {
		t.Fatal("notice after demotion")
	}
}

// LOW: taking the grant closes the connections of the web UI.
func TestGrantTakenClosesConnections(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	if _, err := e.m.Folders(t.Context(), "alex", id); err != nil {
		t.Fatal(err)
	}
	s := e.m.conns.get(poolKey{"alex", id, laneUI})
	c := s.client()
	if c == nil {
		t.Fatal("no connection")
	}
	if err := e.m.SetAllowed("alex", false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Closed():
	case <-time.After(2 * time.Second):
		t.Fatal("connection still open")
	}
}

// LOW: no new key while passwords are stored; own codes.
func TestKeyMissingOrDamaged(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	keyPath := filepath.Join(e.store.Dir(), keyFile)
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	m := e.newManager()
	if _, err := m.Folders(t.Context(), "alex", id); !errors.Is(err, ErrKeyMissing) || Code(err) != "mail_key_missing" {
		t.Fatalf("folders: %v", err)
	}
	if _, err := m.AddMailbox("alex", e.input("new-password")); !errors.Is(err, ErrKeyMissing) {
		t.Fatalf("new password: %v", err)
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a new key was made silently")
	}
	if err := os.WriteFile(keyPath, []byte("zz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.newManager().Folders(t.Context(), "alex", id); !errors.Is(err, ErrKeyDamaged) || Code(err) != "mail_key_damaged" {
		t.Fatalf("damaged: %v", err)
	}
}

// LOW: invisible and direction characters are gone, names fit the disk.
func TestInvisibleCharactersAndLongNames(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	long := strings.Repeat("ä", 200) + ".pdf"
	e.deliver("INBOX", "From: \"Bo\u200bb\" <bob@example.net>\nSubject: Rechnung\u202efdp.exe\u2066\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=b\n\n--b\nContent-Type: text/plain\n\nx\n--b\nContent-Type: application/octet-stream\nContent-Disposition: attachment; filename=\"a\u202egnp.exe\"\n\nx\n--b\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=\""+long+"\"\n\nx\n--b--\n")
	page, err := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if s := page.Messages[0]; s.Subject != "Rechnungfdp.exe" || s.From.Name != "Bob" {
		t.Fatalf("summary %q %q", s.Subject, s.From.Name)
	}
	msg, err := e.m.Read(t.Context(), "alex", id, "INBOX", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Attachments[0].Name != "agnp.exe" || len(msg.Attachments[1].Name) > 255 || !strings.HasSuffix(msg.Attachments[1].Name, ".pdf") {
		t.Fatalf("names %q %d", msg.Attachments[0].Name, len(msg.Attachments[1].Name))
	}
}

// LOW: at most maxStreams event streams per account; a new one ends the
// oldest.
func TestEventStreamsPerAccount(t *testing.T) {
	e := newEnv(t)
	first, _ := e.m.Subscribe("alex")
	for range maxStreams {
		e.m.Subscribe("alex")
	}
	select {
	case _, open := <-first:
		if open {
			t.Fatal("event instead of the end")
		}
	default:
		t.Fatal("the oldest stream is still open")
	}
}

// LOW: an empty INBOX of a server without UIDNEXT does not loop.
func TestEmptyInboxWithoutUIDNext(t *testing.T) {
	var zero atomic.Int32
	addr, pool, conns := scripted(t, "* OK [CAPABILITY IMAP4rev1 IDLE] fake\r\n", func(w *bufio.Writer, tag, cmd, line string) bool {
		switch cmd {
		case "LOGIN":
			fmt.Fprintf(w, "%s OK\r\n", tag)
		case "SELECT":
			fmt.Fprintf(w, "* 0 EXISTS\r\n* OK [UIDVALIDITY 7] ok\r\n%s OK [READ-WRITE] selected\r\n", tag)
		case "UID":
			// "UID *" or "0:*" on an empty INBOX: refused, as Dovecot does.
			if strings.Contains(line, " 0:*") || strings.Contains(line, "UID *") {
				zero.Add(1)
				fmt.Fprintf(w, "%s BAD no UID 0\r\n", tag)
			} else {
				fmt.Fprintf(w, "* SEARCH\r\n%s OK\r\n", tag)
			}
		case "IDLE":
			fmt.Fprint(w, "+ idling\r\n")
		case "DONE":
		default:
			fmt.Fprintf(w, "%s OK\r\n", tag)
		}
		return true
	})
	e := newEnv(t)
	e.m.RootCAs = pool
	in := e.input("x")
	in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
	in.Notify = true
	if _, err := e.m.AddMailbox("alex", in); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go e.m.Run(ctx)
	waitFor(t, "IDLE", func() bool { return e.m.Mailboxes("alex")[0].Status.State == StateOK })
	time.Sleep(300 * time.Millisecond)
	if zero.Load() != 0 || conns.Load() != 1 {
		t.Fatalf("searches from 0: %d, connections: %d", zero.Load(), conns.Load())
	}
}

// LOW: the bell never names an address.
func TestNoticeLabel(t *testing.T) {
	e := newEnv(t)
	e.m.noticeWindow = 10 * time.Millisecond
	e.m.notice("alex", Mailbox{ID: "a", Name: "alice@example.org", Email: "alice@example.org"}, 1, "Bob", "Hi")
	e.m.notice("alex", Mailbox{ID: "b", Name: "Privat", Email: "alice@example.org"}, 1, "Bob", "Hi")
	waitFor(t, "notices", func() bool { return len(e.noticeList()) == 2 })
	labels := map[string]string{}
	for _, n := range e.noticeList() {
		labels[n.Mailbox] = n.Label
	}
	if labels["a"] != "" || labels["b"] != "Privat" {
		t.Fatalf("labels %v", labels)
	}
}

// MEDIUM: at most HeavyPerAccount heavy requests, and none without RAM.
func TestHeavyRequestsLimited(t *testing.T) {
	e := newEnv(t)
	var releases []func()
	for range HeavyPerAccount {
		r, err := e.m.Begin(t.Context(), "alex", 1)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, r)
	}
	if _, err := e.m.Begin(t.Context(), "alex", 1); !errors.Is(err, ErrBusy) || Code(err) != "mail_busy" {
		t.Fatalf("third: %v", err)
	}
	if r, err := e.m.Begin(t.Context(), "bob", 1); err != nil {
		t.Fatalf("another account: %v", err)
	} else {
		r()
	}
	releases[0]()
	// Without room in the budget: a text-only mail still goes, a large
	// one waits a moment and is refused.
	reserveWait = 300 * time.Millisecond
	defer func() { reserveWait = 5 * time.Second }()
	e.m.Reserve = func(int64) (func(), bool) { return nil, false }
	small, err := e.m.Begin(t.Context(), "alex", 64<<10)
	if err != nil {
		t.Fatalf("text only without RAM: %v", err)
	}
	small()
	start := time.Now()
	if _, err := e.m.Begin(t.Context(), "alex", 32<<20); !errors.Is(err, ErrBusy) || time.Since(start) < 250*time.Millisecond {
		t.Fatalf("without RAM: %v after %v", err, time.Since(start))
	}
	// Room that comes while waiting is taken.
	tries := 0
	e.m.Reserve = func(int64) (func(), bool) { tries++; return func() {}, tries > 1 }
	if r, err := e.m.Begin(t.Context(), "alex", 32<<20); err != nil {
		t.Fatalf("room after a moment: %v", err)
	} else {
		r()
	}
}

// LOW (follow-up review): the servers of stored mailboxes are no new
// targets after a start, the same server counts once, and a check of
// the settings (IMAP and SMTP) is one attempt.
func TestNewTargetLimitSparesStoredMailboxes(t *testing.T) {
	e := newEnv(t)
	var ids []string
	for range 12 {
		ids = append(ids, e.addMailbox())
	}
	fresh := e.newManager()
	for _, id := range ids {
		if _, err := fresh.Folders(t.Context(), "alex", id); err != nil {
			t.Fatalf("stored mailbox after a start: %v", err)
		}
	}
	// A new account: twelve tries of one unknown server count once.
	e.accounts["carl"] = Account{Name: "carl", Created: time.Now().Add(-time.Hour), Admin: true}
	if err := e.m.SetAllowed("carl", true); err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := l.Addr().(*net.TCPAddr).Port
	l.Close()
	in := e.input("x")
	in.IMAP.Port = closed
	view, err := e.m.AddMailbox("carl", in)
	if err != nil {
		t.Fatal(err)
	}
	for range 12 {
		if _, err := e.m.Folders(t.Context(), "carl", view.ID); errors.Is(err, ErrRateLimited) {
			t.Fatal("one server counted more than once")
		}
	}
	// Nine checks against other servers (IMAP and SMTP each) are nine
	// attempts: with the one above, ten.
	for i := range NewTargetLimit - 1 {
		check := e.input("x")
		check.IMAP.Port, check.SMTP.Port = closed+100+i, closed+200+i
		if res, err := e.m.CheckSettings(t.Context(), "carl", "", check); err != nil || res.SMTP.Code == "mail_rate_limited" || res.IMAP.Code == "mail_rate_limited" {
			t.Fatalf("check %d: %+v %v", i+1, res, err)
		}
	}
	in.IMAP.Port = closed + 300
	if _, err := e.m.UpdateMailbox("carl", view.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Folders(t.Context(), "carl", view.ID); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("eleventh attempt: %v", err)
	}
}

// The sanitiser against a list of known tricks (from the review).
func TestSanitizerTricks(t *testing.T) {
	payloads := []string{
		`<svg><script>alert(1)</script></svg>`, `<math><mi xlink:href="javascript:alert(1)">x</mi></math>`,
		`<img src=x onerror=alert(1)>`, `<iframe srcdoc="<script>alert(1)</script>"></iframe>`, `<meta http-equiv="refresh" content="0;url=https://evil.example">`,
		`<form action="https://evil.example"><input name=p><button>go</button></form>`, `<a href="data:text/html,<script>alert(1)</script>">x</a>`,
		`<a href="&#106;avascript:alert(1)">x</a>`, `<a href="java&#x09;script:alert(1)">x</a>`, `<a href=" javascript:alert(1)">x</a>`,
		`<div style="background:url(https://evil.example/t.png)">x</div>`, `<div style="background-image:url(https://evil.example/t.png)">x</div>`,
		`<div style="width:expression(alert(1))">x</div>`, `<div style="border:1px solid url(https://e.example/x)">x</div>`,
		`<div style="font-family:'a';background:url(//e.example)">x</div>`, `<style>@import 'https://e.example/x.css';</style>`,
		`<base href="https://evil.example/">`, `<link rel=stylesheet href=https://e.example/x.css>`, `<object data="x.swf"></object>`, `<embed src="x">`,
		`<img srcset="https://e.example/a.png 1x">`, `<img src="//e.example/a.png">`, `<a href="https://ok.example" ping="https://e.example">x</a>`,
		`<noscript><p title="</noscript><img src=x onerror=alert(1)>">`, `<table background="https://e.example/bg.png"><tr><td>x</td></tr></table>`,
		`<!--<img src="--><img src=x onerror=alert(1)//">`, `<p style="position:fixed;top:0;left:0">overlay</p>`, `<video poster="https://e.example/p.png"></video>`,
		`<a href="https://ok.example" target="_self">x</a>`, `<img src="cid:a&quot; onerror=&quot;alert(1)">`, `<template><script>alert(1)</script></template>`,
		`<select><style></select><img src=x onerror=alert(1)></style>`, `<font face="x&quot;;background:url(https://e.example)">x</font>`,
	}
	bad := regexp.MustCompile(`(?i)<script|<svg|<math|<iframe|<form|<input|<button|<object|<embed|<link|<base|<style|<video|<template|\son[a-z]+=|javascript:|srcdoc|srcset|ping=|url\(|expression\(|position|e\.example|evil\.example|refresh`)
	for _, p := range payloads {
		doc, _ := SanitizeHTML(p, map[string]string{"a": "data:image/png;base64,AA"}, false)
		body := doc[strings.Index(doc, "</head>"):]
		if m := bad.FindString(body); m != "" {
			t.Errorf("%q survived in %s", m, body)
		}
	}
}

// Interface: every link names its real target, a deceiving text is marked.
func TestLinkTargets(t *testing.T) {
	doc, _ := SanitizeHTML(`<a href="https://evil.example/login" title="https://bank.example">www.bank.example</a> <a href="https://www.shop.example/x">shop.example</a> <a href="https://a.example/">Hier</a>`, nil, false)
	if !strings.Contains(doc, `title="https://evil.example/login"`) || strings.Contains(doc, `title="https://bank.example"`) {
		t.Fatalf("title: %s", doc)
	}
	if strings.Count(doc, `data-tmail-mismatch="1"`) != 1 || !strings.Contains(doc, "⚠ evil.example") {
		t.Fatalf("mark: %s", doc)
	}
}

// Interface: an answer marks the mail it answers.
func TestAnswerMarksAnswered(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	e.deliver("INBOX", "From: bob@example.net\nSubject: Frage\nMessage-ID: <frage-1@example.net>\n\nWie?\n")
	e.deliver("INBOX", "From: bob@example.net\nSubject: Zweite\nMessage-ID: <frage-2@example.net>\n\nUnd?\n")
	if _, err := e.m.Send(t.Context(), "alex", id, Outgoing{To: []Address{{Addr: "bob@example.net"}}, Text: "So.", InReplyTo: "<frage-1@example.net>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Send(t.Context(), "alex", id, Outgoing{To: []Address{{Addr: "bob@example.net"}}, Text: "So.", Answers: &MailRef{Folder: "INBOX", UID: 2}}); err != nil {
		t.Fatal(err)
	}
	page, _ := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "INBOX"})
	for _, s := range page.Messages {
		if !s.Flags.Answered {
			t.Errorf("%d not answered", s.UID)
		}
	}
}

// Only texts that look like web addresses are compared, by registrable
// domain; file names, versions and mail addresses are no targets.
func TestLinkMismatchOnlyForWebAddresses(t *testing.T) {
	for _, c := range []struct {
		text, href string
		mark       bool
	}{
		{"www.sparkasse.de", "https://evil.example/login", true},
		{"sparkasse.de", "https://sparkasse.evil.example/", true},
		{"https://paypal.com/signin", "https://paypal.com.evil.example/", true},
		{"amazon.co.uk", "https://amazon.example/", true},
		{"bob@bank.de", "mailto:bob@evil.example", true},
		{"www.sparkasse.de", "https://banking.sparkasse.de/x", false},
		{"sparkasse.de", "https://www.sparkasse.de/", false},
		{"amazon.co.uk", "https://www.amazon.co.uk/", false},
		{"Rechnung.pdf", "https://cdn.example/r.pdf", false},
		{"bild.jpg", "https://cdn.example/b.jpg", false},
		{"v1.2", "https://github.example/release", false},
		{"Version 2.0.1", "https://x.example/", false},
		{"bob@bank.de", "https://evil.example/", false},
		{"Hier klicken", "https://evil.example/", false},
		{"bob@bank.de", "mailto:bob@bank.de", false},
	} {
		doc, _ := SanitizeHTML(`<a href="`+c.href+`">`+c.text+`</a>`, nil, false)
		if got := strings.Contains(doc, `data-tmail-mismatch="1"`); got != c.mark {
			t.Errorf("%q → %s: marked %v, want %v", c.text, c.href, got, c.mark)
		}
	}
}

// partFloodIMAP serves one mail whose structure claims a part of 4 GB.
// flood: the fetch of the part gets an endless untagged line instead of
// the literal; else the part comes normally and every later command
// (LIST) gets the endless line.
func partFloodIMAP(t *testing.T, flood bool) (string, *x509.CertPool, *atomic.Int64) {
	sent := &atomic.Int64{}
	endless := func(w *bufio.Writer) bool {
		w.WriteString("* OK ")
		chunk := []byte(strings.Repeat("A", 1<<20))
		for range 256 {
			if _, err := w.Write(chunk); err != nil {
				return false
			}
			sent.Add(int64(len(chunk)))
		}
		return false
	}
	addr, pool, _ := scripted(t, "* OK [CAPABILITY IMAP4rev1] evil\r\n", func(w *bufio.Writer, tag, cmd, line string) bool {
		upper := strings.ToUpper(line)
		switch {
		case cmd == "LOGIN":
			fmt.Fprintf(w, "%s OK\r\n", tag)
		case cmd == "CAPABILITY":
			fmt.Fprintf(w, "* CAPABILITY IMAP4rev1\r\n%s OK\r\n", tag)
		case cmd == "SELECT":
			fmt.Fprintf(w, "* 1 EXISTS\r\n* OK [UIDVALIDITY 1] x\r\n* OK [UIDNEXT 2] x\r\n%s OK [READ-WRITE] done\r\n", tag)
		case strings.Contains(upper, "BODYSTRUCTURE"):
			fmt.Fprintf(w, "* 1 FETCH (UID 1 BODYSTRUCTURE (\"application\" \"octet-stream\" (\"name\" \"big.bin\") NIL NIL \"base64\" 4000000000 NIL (\"attachment\" (\"filename\" \"big.bin\")) NIL NIL))\r\n%s OK\r\n", tag)
		case strings.Contains(upper, "BODY.PEEK[1]"):
			if flood {
				return endless(w)
			}
			fmt.Fprintf(w, "* 1 FETCH (UID 1 BODY[1] {4}\r\nQUFB)\r\n%s OK\r\n", tag)
		case cmd == "LIST":
			return endless(w)
		default:
			fmt.Fprintf(w, "%s OK\r\n", tag)
		}
		return true
	})
	return addr, pool, sent
}

// HIGH (follow-up review): the size a server claims for a part is no
// budget, and a raised budget does not stay on the connection.
func TestPartBudgetNotFromServer(t *testing.T) {
	for _, flood := range []bool{true, false} {
		addr, pool, sent := partFloodIMAP(t, flood)
		e := newEnv(t)
		e.m.RootCAs = pool
		in := e.input("x")
		in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
		view, err := e.m.AddMailbox("alex", in)
		if err != nil {
			t.Fatal(err)
		}
		var partErr, listErr error
		peak := peakHeap(func() {
			partErr = e.m.Part(t.Context(), "alex", view.ID, "INBOX", 1, "1", func(PartInfo) io.Writer { return io.Discard })
			if !flood {
				_, listErr = e.m.Folders(t.Context(), "alex", view.ID)
			}
		})
		if flood && !errors.Is(partErr, ErrTooLarge) {
			t.Errorf("flood instead of the part: %v", partErr)
		}
		if !flood && (partErr != nil || !errors.Is(listErr, ErrTooLarge)) {
			t.Errorf("flood after the part: part %v, list %v", partErr, listErr)
		}
		if peak > 96<<20 {
			t.Errorf("flood %v: peak heap %d MiB (server sent %d MiB)", flood, peak>>20, sent.Load()>>20)
		}
	}
}

// LOW (follow-up review): look-alike, invisible and hidden tricks of link
// texts and targets.
func TestLinkMarkTricks(t *testing.T) {
	for _, c := range []struct {
		name, html string
		mark       bool
	}{
		{"plain", `<a href="https://evil.example/login">www.sparkasse.de</a>`, true},
		{"cyrillic a in text", `<a href="https://evil.example/login">www.sp` + "а" + `rkasse.de</a>`, true},
		{"zero width in text", `<a href="https://evil.example/login">www.spar` + "\u200b" + `kasse.de</a>`, true},
		{"words around", `<a href="https://evil.example/login">Login: www.sparkasse.de</a>`, true},
		{"trailing dot", `<a href="https://evil.example/login">sparkasse.de.</a>`, true},
		{"idn href", `<a href="https://www.sp` + "а" + `rkasse.de/login">www.sparkasse.de</a>`, true},
		{"punycode href", `<a href="https://www.xn--sprkasse-2fg.de/login">www.sparkasse.de</a>`, true},
		{"bidi href", `<a href="https://moc.` + "\u202e" + `evil.example/">www.paypal.com</a>`, true},
		{"font-size 0", `<p style="font-size:0"><a style="font-size:14px" href="https://evil.example/">www.sparkasse.de</a></p>`, true},
		{"blogspot", `<a href="https://evil.blogspot.com/">bank.blogspot.com</a>`, true},
		{"ip host", `<a href="http://192.0.2.7/">www.sparkasse.de</a>`, true},
		{"same site", `<a href="https://banking.sparkasse.de/">Login bei www.sparkasse.de</a>`, false},
		{"umlaut domain", `<a href="https://www.m` + "ü" + `nchen.de/">www.m` + "ü" + `nchen.de</a>`, false},
		{"file name", `<a href="https://cdn.example/r.pdf">Rechnung.pdf</a>`, false},
	} {
		doc, _ := SanitizeHTML(c.html, nil, false)
		body := doc[strings.Index(doc, "<body>"):]
		if got := strings.Contains(body, `data-tmail-mismatch="1"`); got != c.mark {
			t.Errorf("%s: marked %v, want %v: %s", c.name, got, c.mark, body)
		}
		for _, m := range regexp.MustCompile(`title="[^"]*"|data-tmail-target="[^"]*"`).FindAllString(body, -1) {
			if hasInvisible(m) {
				t.Errorf("%s: invisible characters in %q", c.name, m)
			}
		}
		if c.mark && !strings.Contains(body, "font-size:14px !important") {
			t.Errorf("%s: the mark can be hidden: %s", c.name, body)
		}
	}
	doc, _ := SanitizeHTML(`<a href="https://www.sp`+"а"+`rkasse.de/x">x</a>`, nil, false)
	if !strings.Contains(doc, `href="https://www.xn--sprkasse-26g.de/x"`) || !strings.Contains(doc, `title="https://www.xn--sprkasse-26g.de/x"`) {
		t.Errorf("punycode target: %s", doc)
	}
}

// evilList answers LIST with a mailbox name of size bytes and FETCH with
// a structure of size bytes of small parts; parts come with 8 bytes each.
func evilAnswers(t *testing.T, listSize int, structure string) (string, *x509.CertPool) {
	chunk := strings.Repeat("A", 1<<20)
	addr, pool, _ := scripted(t, "* OK [CAPABILITY IMAP4rev1 UIDPLUS MOVE] evil\r\n", func(w *bufio.Writer, tag, cmd, line string) bool {
		upper := strings.ToUpper(line)
		switch {
		case cmd == "LOGIN":
			fmt.Fprintf(w, "%s OK\r\n", tag)
		case cmd == "SELECT":
			fmt.Fprintf(w, "* 1 EXISTS\r\n* OK [UIDVALIDITY 1] v\r\n* OK [UIDNEXT 2] n\r\n%s OK [READ-WRITE] done\r\n", tag)
		case cmd == "LIST":
			fmt.Fprintf(w, "* LIST () \"/\" {%d}\r\n", listSize)
			for left := listSize; left > 0; left -= min(left, len(chunk)) {
				w.WriteString(chunk[:min(left, len(chunk))])
			}
			fmt.Fprintf(w, "\r\n%s OK done\r\n", tag)
		case strings.Contains(upper, "BODYSTRUCTURE"):
			// Written as it is: the test's server must not copy it (its
			// memory counts in the same process).
			w.WriteString("* 1 FETCH (UID 1 FLAGS (\\Seen) BODYSTRUCTURE ")
			w.WriteString(structure)
			fmt.Fprintf(w, ")\r\n%s OK done\r\n", tag)
		case strings.Contains(upper, "BODY.PEEK["):
			i := strings.Index(upper, "BODY.PEEK[")
			sec := upper[i+10 : i+10+strings.Index(upper[i+10:], "]")]
			fmt.Fprintf(w, "* 1 FETCH (UID 1 BODY[%s] {12}\r\nQUFBQUFBQUE=)\r\n%s OK done\r\n", sec, tag)
		default:
			fmt.Fprintf(w, "%s OK\r\n", tag)
		}
		return true
	})
	return addr, pool
}

func evilMailboxes(t *testing.T, e *testEnv, addr string, pool *x509.CertPool, n int) []string {
	e.m.RootCAs = pool
	var ids []string
	for i := range n {
		in := e.input("x")
		in.Name = "evil" + strconv.Itoa(i)
		in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
		view, err := e.m.AddMailbox("alex", in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, view.ID)
	}
	return ids
}

// HIGH (follow-up review N1): sizes in the structure make no room in
// advance; what is kept holds its real length.
func TestClaimedSizesMakeNoRoom(t *testing.T) {
	var parts strings.Builder
	for range 100 {
		parts.WriteString(`("application" "octet-stream" ("name" "a.bin") NIL NIL "base64" 34000000)`)
	}
	addr, pool := evilAnswers(t, 10, "("+parts.String()+` "mixed")`)
	e := newEnv(t)
	id := evilMailboxes(t, e, addr, pool, 1)[0]
	var held int
	var kept [][]byte
	peak := peakHeap(func() {
		for p := 1; p <= 100; p++ {
			data, _, err := e.m.PartBytes(t.Context(), "alex", id, "INBOX", 1, strconv.Itoa(p), MaxAttachments)
			if err != nil {
				t.Fatalf("part %d: %v", p, err)
			}
			held += cap(data)
			kept = append(kept, data)
		}
	})
	runtime.KeepAlive(kept)
	if held > 1<<20 || peak > 96<<20 {
		t.Fatalf("held %d KiB, peak %d MiB", held>>10, peak>>20)
	}
}

// MEDIUM (follow-up review N2): many mailboxes of one account cannot add
// their budgets up; commands wait for the account's turn.
func TestParallelCommandsOfAnAccountBounded(t *testing.T) {
	addr, pool := evilAnswers(t, budgetCommand-(512<<10), "")
	e := newEnv(t)
	ids := evilMailboxes(t, e, addr, pool, MaxMailboxes)
	var running, most atomic.Int32
	e.m.Reserve = func(int64) (func(), bool) {
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		return func() { running.Add(-1) }, true
	}
	peak := peakHeap(func() {
		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = e.m.Folders(t.Context(), "alex", id)
			}()
		}
		wg.Wait()
	})
	if peak > 160<<20 {
		t.Fatalf("peak heap %d MiB", peak>>20)
	}
	if most.Load() > CommandsPerAccount {
		t.Fatalf("%d commands of one account at once", most.Load())
	}
}

// MEDIUM (follow-up review N2): structures of 15 MiB, read in parallel
// by many mailboxes of an account, stay bounded.
func TestLargeStructureBounded(t *testing.T) {
	part := `("text" "plain" NIL NIL NIL "7bit" 1 1)`
	var b strings.Builder
	b.WriteString("(")
	for range (15 << 20) / len(part) { // the size of the review's proof
		b.WriteString(part)
	}
	b.WriteString(` "mixed")`)
	addr, pool := evilAnswers(t, 10, b.String())
	e := newEnv(t)
	ids := evilMailboxes(t, e, addr, pool, 8)
	// Sampled every millisecond: decoding a structure is brief.
	runtime.GC()
	var start runtime.MemStats
	runtime.ReadMemStats(&start)
	base := start.HeapAlloc
	var peak atomic.Uint64
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		var ms runtime.MemStats
		for {
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > peak.Load() {
				peak.Store(ms.HeapAlloc)
			}
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.m.Read(t.Context(), "alex", id, "INBOX", 1, false)
		}()
	}
	wg.Wait()
	close(stop)
	<-sampled
	grown := peak.Load() - min(peak.Load(), base)
	t.Logf("peak %d MiB above the start", grown>>20)
	if grown > 128<<20 {
		t.Fatalf("heap grew by %d MiB", grown>>20)
	}
}

// INFO (follow-up review): waiting for room ends with the request.
func TestMoreEndsWithTheRequest(t *testing.T) {
	e := newEnv(t)
	e.m.Reserve = func(int64) (func(), bool) { return nil, false }
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	if _, err := e.m.More(ctx, 32<<20); !errors.Is(err, ErrBusy) || time.Since(start) > time.Second {
		t.Fatalf("%v after %v", err, time.Since(start))
	}
}

// LOW (follow-up review N4): a server is known by its address after a
// login, not by its name.
func TestKnownServerByAddress(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.CheckSettings(t.Context(), "alex", "", e.input(testPassword)); err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := l.Addr().(*net.TCPAddr).Port
	l.Close()
	in := e.input("x")
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	for i := range NewTargetLimit {
		in.IMAP.Port = closed + 1 + i
		in.Password = "p" + strconv.Itoa(i)
		e.m.UpdateMailbox("alex", view.ID, in)
		e.m.Folders(t.Context(), "alex", view.ID)
	}
	// The real server under another name: its address is known.
	ok := e.input(testPassword)
	_, port := hostPort(t, e.srv.IMAPAddr)
	ok.IMAP.Host, ok.IMAP.Port = "localhost", port
	if _, err := e.m.UpdateMailbox("alex", view.ID, ok); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Folders(t.Context(), "alex", view.ID); errors.Is(err, ErrRateLimited) {
		t.Fatalf("known address refused: %v", err)
	}
	// A known name on another port is a new target.
	ok.IMAP.Host, ok.IMAP.Port = "127.0.0.1", closed+50
	e.m.UpdateMailbox("alex", view.ID, ok)
	if _, err := e.m.Folders(t.Context(), "alex", view.ID); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("new port of a known host: %v", err)
	}
}

// tailIMAP answers a part's literal and then, mode "tail", a large string
// nobody asked for in the same response, or, mode "stall", nothing at all
// after the structure.
func tailIMAP(t *testing.T, mode string) (string, *x509.CertPool) {
	addr, pool, _ := scripted(t, "* OK [CAPABILITY IMAP4rev1] evil\r\n", func(w *bufio.Writer, tag, cmd, line string) bool {
		upper := strings.ToUpper(line)
		switch {
		case cmd == "LOGIN":
			fmt.Fprintf(w, "%s OK\r\n", tag)
		case cmd == "CAPABILITY":
			fmt.Fprintf(w, "* CAPABILITY IMAP4rev1\r\n%s OK\r\n", tag)
		case cmd == "SELECT":
			fmt.Fprintf(w, "* 1 EXISTS\r\n* OK [UIDVALIDITY 1] x\r\n* OK [UIDNEXT 2] x\r\n%s OK [READ-WRITE] done\r\n", tag)
		case strings.Contains(upper, "BODYSTRUCTURE"):
			fmt.Fprintf(w, "* 1 FETCH (UID 1 BODYSTRUCTURE (\"application\" \"octet-stream\" (\"name\" \"a.bin\") NIL NIL \"base64\" 4 NIL (\"attachment\" (\"filename\" \"a.bin\")) NIL NIL))\r\n%s OK\r\n", tag)
		case strings.Contains(upper, "BODY.PEEK[1]"):
			if mode == "stall" {
				_ = w.Flush()
				time.Sleep(5 * time.Second)
				return false
			}
			fmt.Fprintf(w, "* 1 FETCH (UID 1 BODY[1] {4}\r\nQUFB BODY[2] \"%s\")\r\n%s OK\r\n", strings.Repeat("A", 2<<20), tag)
		default:
			fmt.Fprintf(w, "%s OK\r\n", tag)
		}
		return true
	})
	return addr, pool
}

// LOW (review of 1.06, A): after the literal of a download only a small
// budget remains; a large string nobody asked for ends the connection.
func TestDownloadTailHasASmallBudget(t *testing.T) {
	addr, pool := tailIMAP(t, "tail")
	e := newEnv(t)
	e.m.RootCAs = pool
	in := e.input("x")
	in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	err = e.m.Part(t.Context(), "alex", view.ID, "INBOX", 1, "1", func(PartInfo) io.Writer { return io.Discard })
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("a large tail after the literal was read: %v", err)
	}
}

// LOW (review of 1.06, B): until its literal begins a download has the
// time of a command, not the browser's ten minutes.
func TestDownloadHeadHasTheTimeOfACommand(t *testing.T) {
	old := partHeadTimeout
	partHeadTimeout = 500 * time.Millisecond
	t.Cleanup(func() { partHeadTimeout = old })
	addr, pool := tailIMAP(t, "stall")
	e := newEnv(t)
	e.m.RootCAs = pool
	in := e.input("x")
	in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = e.m.Part(t.Context(), "alex", view.ID, "INBOX", 1, "1", func(PartInfo) io.Writer { return io.Discard })
	if !errors.Is(err, ErrTimeout) || time.Since(start) > 4*time.Second {
		t.Errorf("a stalled download before its literal: %v after %v", err, time.Since(start))
	}
}

// INFO (review of 1.06, C): a bdo or dir of the sender does not mirror the
// warning.
func TestLinkMarkIsNotMirrored(t *testing.T) {
	doc, _ := SanitizeHTML(`<bdo dir="rtl"><a href="https://ed.knab-evil.example/x">www.bank.de</a></bdo>`, nil, false)
	if !strings.Contains(doc, "direction:ltr !important") || !strings.Contains(doc, "unicode-bidi:isolate !important") {
		t.Errorf("the mark can be mirrored: %s", doc)
	}
}
