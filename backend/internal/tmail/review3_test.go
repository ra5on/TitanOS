package tmail

// Regression tests of the third review of 1.05 (low findings and notes,
// fixed in 1.06). The scenarios follow the reviewers' proofs of concept.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// stallWriter is a browser that stops reading: Write blocks until the
// test ends, then fails.
type stallWriter struct {
	started chan struct{}
	once    sync.Once
	release <-chan struct{}
}

func (w *stallWriter) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return 0, errors.New("the browser went away")
}

// LOW (third review): stalled downloads of one account hold neither a
// command's turn nor the floor of another account, even with TitanOS's
// budget of RAM used up; downloads have turns of their own.
func TestStalledDownloadsBlockNoOne(t *testing.T) {
	body := strings.Repeat("QUFB", 1<<18) // 1 MiB base64
	addr, pool, _ := scripted(t, "* OK [CAPABILITY IMAP4rev1] downloads\r\n", func(w *bufio.Writer, tag, cmd, line string) bool {
		upper := strings.ToUpper(line)
		switch {
		case cmd == "SELECT":
			fmt.Fprintf(w, "* 1 EXISTS\r\n* OK [UIDVALIDITY 1] v\r\n* OK [UIDNEXT 2] n\r\n%s OK [READ-WRITE] done\r\n", tag)
		case strings.Contains(upper, "BODYSTRUCTURE"):
			fmt.Fprintf(w, "* 1 FETCH (UID 1 BODYSTRUCTURE (\"application\" \"octet-stream\" (\"name\" \"a.bin\") NIL NIL \"base64\" %d))\r\n%s OK done\r\n", len(body), tag)
		case strings.Contains(upper, "BODY.PEEK["):
			fmt.Fprintf(w, "* 1 FETCH (UID 1 BODY[1] {%d}\r\n%s)\r\n%s OK done\r\n", len(body), body, tag)
		default:
			fmt.Fprintf(w, "%s OK\r\n", tag)
		}
		return true
	})
	e := newEnv(t)
	ids := evilMailboxes(t, e, addr, pool, DownloadsPerAccount+1)
	e.accounts["bob"] = Account{Name: "bob", Created: time.Now().Add(-time.Hour).UTC(), Admin: true}
	if err := e.m.SetAllowed("bob", true); err != nil {
		t.Fatal(err)
	}
	in := e.input("x")
	in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
	bob, err := e.m.AddMailbox("bob", in)
	if err != nil {
		t.Fatal(err)
	}
	reserveWait = 300 * time.Millisecond
	t.Cleanup(func() { reserveWait = 5 * time.Second })
	// The budget of RAM is used up (a big copy, VMs): only the floor runs.
	e.m.Reserve = func(int64) (func(), bool) { return nil, false }

	release := make(chan struct{})
	var running sync.WaitGroup
	t.Cleanup(func() { close(release); running.Wait() })
	stall := func(id string) {
		t.Helper()
		w := &stallWriter{started: make(chan struct{}), release: release}
		running.Go(func() {
			_ = e.m.Part(t.Context(), "alex", id, "INBOX", 1, "1", func(PartInfo) io.Writer { return w })
		})
		select {
		case <-w.started:
		case <-time.After(5 * time.Second):
			t.Fatal("the download did not start")
		}
	}
	stall(ids[0])
	stall(ids[1])

	// Another account goes on at once.
	start := time.Now()
	if _, err := e.m.Folders(t.Context(), "bob", bob.ID); err != nil {
		t.Fatalf("another account while two downloads stall: %v after %v", err, time.Since(start))
	}
	// So does the account itself, on another mailbox.
	if _, err := e.m.Folders(t.Context(), "alex", ids[2]); err != nil {
		t.Fatalf("the account itself on another mailbox: %v", err)
	}
	// Downloads have their own limit per account.
	stall(ids[2])
	stall(ids[3])
	if err := e.m.Part(t.Context(), "alex", ids[4], "INBOX", 1, "1", func(PartInfo) io.Writer { return io.Discard }); !errors.Is(err, ErrBusy) {
		t.Fatalf("a fifth download: %v", err)
	}
	if _, err := e.m.Folders(t.Context(), "bob", bob.ID); err != nil {
		t.Fatalf("another account while four downloads stall: %v", err)
	}
	// A request for a mailbox whose download stalls waits for the
	// connection, not with a turn of the account: it ends with the
	// request and leaves the account's commands free.
	short, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	var waited atomic.Bool
	running.Go(func() {
		_, err := e.m.Folders(short, "alex", ids[0])
		waited.Store(errors.Is(err, ErrTimeout))
	})
	time.Sleep(50 * time.Millisecond)
	if _, err := e.m.Folders(t.Context(), "alex", ids[4]); err != nil {
		t.Fatalf("a command while another waits for its mailbox: %v", err)
	}
	waitFor(t, "the waiting request to end with its context", waited.Load)
}

// LOW (third review): a server stored before a start is known only at
// the first address its name resolves to; a name whose DNS changes from
// try to try counts as a new target each time, also within a minute.
func TestKnownServerOnlyAtItsFirstAddress(t *testing.T) {
	e := newEnv(t)
	in := e.input(testPassword)
	in.IMAP.Host, in.IMAP.Port = "mail.example", 993
	if _, err := e.m.AddMailbox("alex", in); err != nil {
		t.Fatal(err)
	}
	fresh := e.newManager()
	stored := Server{Host: "mail.example", Port: 993}
	at := func(i int) netip.AddrPort {
		return netip.AddrPortFrom(netip.AddrFrom4([4]byte{203, 0, 113, byte(i)}), 993)
	}
	if !fresh.mayTry(t.Context(), "alex", stored, at(1)) {
		t.Fatal("the first address of a stored server")
	}
	// The name now points elsewhere again and again.
	for i := 2; i < 2+NewTargetLimit; i++ {
		if !fresh.mayTry(t.Context(), "alex", stored, at(i)) {
			t.Fatalf("attempt %d refused too early", i-1)
		}
	}
	if fresh.mayTry(t.Context(), "alex", stored, at(2+NewTargetLimit)) {
		t.Fatal("a stored name reached more new addresses than the limit")
	}
	// Its first address stays known, and a repeated address counts once.
	if !fresh.mayTry(t.Context(), "alex", stored, at(1)) || !fresh.mayTry(t.Context(), "alex", stored, at(2)) {
		t.Fatal("the first address or a counted one refused")
	}
	// The same for a name nobody stored: one attempt per address.
	other := Server{Host: "rebind.example", Port: 143}
	for i := range NewTargetLimit {
		if !fresh.mayTry(t.Context(), "carl", other, netip.AddrPortFrom(netip.AddrFrom4([4]byte{198, 51, 100, byte(i)}), 143)) {
			t.Fatalf("attempt %d refused too early", i+1)
		}
	}
	if fresh.mayTry(t.Context(), "carl", other, netip.AddrPortFrom(netip.AddrFrom4([4]byte{198, 51, 100, 200}), 143)) {
		t.Fatal("one name reached more addresses than the limit")
	}
}

// INFO (third review): the mark keeps its own spacing, font, indent and
// case, whatever the sender sets around the link.
func TestLinkMarkKeepsItsLook(t *testing.T) {
	doc, _ := SanitizeHTML(`<p style="letter-spacing:-20px;font-family:Webdings;text-indent:-9999px;text-transform:lowercase;word-spacing:-50px">`+
		`<a href="https://evil.example/">www.sparkasse.de</a></p>`, nil, false)
	start := strings.Index(doc, "<span")
	if start < 0 {
		t.Fatalf("no mark: %s", doc)
	}
	mark := doc[start : start+strings.Index(doc[start:], ">")]
	for _, want := range []string{"letter-spacing:normal !important", "word-spacing:normal !important", "font-family:sans-serif !important",
		"text-indent:0 !important", "text-transform:none !important", "font-size:14px !important", "color:#b00020 !important"} {
		if !strings.Contains(mark, want) {
			t.Errorf("mark without %q: %s", want, mark)
		}
	}
}

// INFO (third review): an ESEARCH answer without "UID" (sequence numbers)
// is not taken as an empty result: T-Mail asks again without ESEARCH, and
// a second such answer is an error.
func TestSearchWithoutUIDsNotEmpty(t *testing.T) {
	for _, broken := range []bool{false, true} {
		addr, pool, _ := scripted(t, "* OK [CAPABILITY IMAP4rev1 ESEARCH] search\r\n", func(w *bufio.Writer, tag, cmd, line string) bool {
			upper := strings.ToUpper(line)
			switch {
			case cmd == "SELECT":
				fmt.Fprintf(w, "* 3 EXISTS\r\n* OK [UIDVALIDITY 1] v\r\n* OK [UIDNEXT 8] n\r\n%s OK [READ-WRITE] done\r\n", tag)
			case strings.Contains(upper, "SEARCH RETURN"), broken && strings.Contains(upper, "SEARCH"):
				// Sequence numbers, no "UID".
				fmt.Fprintf(w, "* ESEARCH (TAG \"%s\") ALL 1:3\r\n%s OK done\r\n", tag, tag)
			case strings.Contains(upper, "SEARCH"):
				fmt.Fprintf(w, "* SEARCH 5 6 7\r\n%s OK done\r\n", tag)
			default:
				fmt.Fprintf(w, "%s OK\r\n", tag)
			}
			return true
		})
		e := newEnv(t)
		id := evilMailboxes(t, e, addr, pool, 1)[0]
		var got []uint32
		err := e.m.withIMAP(t.Context(), "alex", id, commandTimeout, func(c *imapclient.Client, s *session) error {
			if _, err := selectFolder(c, s, "INBOX"); err != nil {
				return err
			}
			data, err := uidSearch(c, &imap.SearchCriteria{}, true)
			if err != nil {
				return err
			}
			got = uidsDesc(data.All, 0, 10)
			return nil
		})
		switch {
		case broken && !errors.Is(err, errSearchWithoutUIDs):
			t.Errorf("two answers without UIDs: %v %v", got, err)
		case !broken && (err != nil || fmt.Sprint(got) != "[7 6 5]"):
			t.Errorf("fallback without ESEARCH: %v %v", got, err)
		}
	}
}

// bigHeadMail is a mail whose "To" (in front of everything else) is
// larger than 200 KiB, as a sender with a huge list of recipients sends.
func bigHeadMail(i int) string {
	var to strings.Builder
	to.WriteString("To: chef@example.org")
	for n := 0; to.Len() < 250<<10; n++ {
		fmt.Fprintf(&to, ",\r\n kunde%d@example.org", n)
	}
	return to.String() + "\r\nFrom: \"Versand\" <versand@shop.example>\r\n" +
		fmt.Sprintf("Subject: =?UTF-8?Q?Gr=C3=BC=C3=9Fe_Nr=2E_%d?=\r\nDate: Tue, 17 Nov 2026 10:00:00 +0100\r\n", i) +
		"Content-Type: text/plain; charset=utf-8\r\n\r\nHallo\r\n"
}

// LOW (third review): a sender with very large heads (25 mails, each with
// a head over 200 KiB) does not hold up the messages of a mailbox: the
// watcher asks for a few header fields, each group cut, so the round fits
// and no pause comes up at all. The rules still see To and Subject.
func TestLargeHeadsDoNotHoldUpMessages(t *testing.T) {
	e := newEnv(t)
	e.m.backoffMin, e.m.backoffMax = time.Minute, time.Minute // any pause fails the test
	in := e.input(testPassword)
	in.Notify = true
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.SetSettings("alex", Settings{PushContent: true, Rules: []Rule{{Account: view.ID, Enabled: true,
		When: RuleWhen{To: "chef@example.org", Subject: "grüße"}, Then: RuleThen{Flag: true}}}}); err != nil {
		t.Fatal(err)
	}
	// The watcher learns where new mail starts, stops, and finds all 25 at
	// once when it comes back (as after a pause of the connection).
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { e.m.Run(ctx); close(done) }()
	waitFor(t, "IDLE", func() bool { return e.m.Mailboxes("alex")[0].Status.State == StateOK })
	cancel()
	<-done
	for i := range 25 {
		e.deliver("INBOX", bigHeadMail(i))
	}
	ctx, cancel = context.WithCancel(t.Context())
	done = make(chan struct{})
	go func() { e.m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	told := func() (n int, subject string) {
		for _, notice := range e.noticeList() {
			n, subject = n+notice.Count, notice.Subject
		}
		return n, subject
	}
	waitFor(t, "messages of all 25 mails", func() bool { n, _ := told(); return n == 25 })
	if _, subject := told(); subject != "Grüße Nr. 24" || e.noticeList()[0].From != "Versand" {
		t.Fatalf("notices: %+v", e.noticeList())
	}
	// The messages can go out before the session reports itself OK (under
	// -race in CI the status was still "connecting" here). A pause would
	// last a minute (backoff above), far longer than waitFor waits.
	waitFor(t, "status OK without a pause", func() bool { return e.m.Mailboxes("alex")[0].Status.State == StateOK })
	page, err := e.m.List(t.Context(), "alex", view.ID, ListQuery{Folder: "INBOX", Flagged: true})
	if err != nil || page.Total != 25 {
		t.Fatalf("flagged by the rule on To and Subject: %d %v", page.Total, err)
	}
}

// LOW (third review): a server that ignores the cut and sends huge heads
// anyway makes a round too large; the next round is halved and the
// watcher connects again after the shortest pause, which does not grow.
func TestTooLargeRoundsHalveWithoutGrowingPause(t *testing.T) {
	pad := strings.Repeat("x", 600<<10)
	var mu sync.Mutex
	var logins []time.Time
	addr, pool, _ := scripted(t, "* OK [CAPABILITY IMAP4rev1] heads\r\n", func(w *bufio.Writer, tag, cmd, line string) bool {
		upper := strings.ToUpper(line)
		switch {
		case cmd == "LOGIN":
			mu.Lock()
			logins = append(logins, time.Now())
			mu.Unlock()
			fmt.Fprintf(w, "%s OK\r\n", tag)
		case cmd == "SELECT":
			fmt.Fprintf(w, "* 0 EXISTS\r\n* OK [UIDVALIDITY 1] v\r\n* OK [UIDNEXT 1] n\r\n%s OK [READ-WRITE] done\r\n", tag)
		case cmd == "IDLE":
			fmt.Fprint(w, "+ idling\r\n")
		case strings.Contains(upper, "UID SEARCH UID"):
			w.WriteString("* SEARCH")
			for u := 1; u <= 25; u++ {
				fmt.Fprintf(w, " %d", u)
			}
			fmt.Fprintf(w, "\r\n%s OK done\r\n", tag)
		case strings.Contains(upper, "SEARCH"):
			fmt.Fprintf(w, "* SEARCH\r\n%s OK done\r\n", tag)
		case strings.Contains(upper, "UID FETCH"):
			set := strings.Fields(line)[3]
			for _, r := range strings.Split(set, ",") {
				lo, hi, _ := strings.Cut(r, ":")
				a, _ := strconv.Atoi(lo)
				b := a
				if hi != "" {
					b, _ = strconv.Atoi(hi)
				}
				for u := a; u <= b; u++ {
					head := fmt.Sprintf("From: a@example.org\r\nSubject: Mail %d\r\nX-Pad: %s\r\n\r\n", u, pad)
					fmt.Fprintf(w, "* %d FETCH (UID %d FLAGS () BODY[HEADER.FIELDS (FROM SENDER SUBJECT DATE)] {%d}\r\n%s)\r\n", u, u, len(head), head)
				}
			}
			fmt.Fprintf(w, "%s OK done\r\n", tag)
		default:
			fmt.Fprintf(w, "%s OK\r\n", tag)
		}
		return true
	})
	e := newEnv(t)
	e.m.RootCAs = pool
	e.m.backoffMin, e.m.backoffMax = time.Second, time.Hour
	in := e.input("x")
	in.IMAP.Host, in.IMAP.Port = hostPort(t, addr)
	in.Notify = true
	if _, err := e.m.AddMailbox("alex", in); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { e.m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	told := func() int {
		n := 0
		for _, notice := range e.noticeList() {
			n += notice.Count
		}
		return n
	}
	for deadline := time.Now().Add(30 * time.Second); told() != 25; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d of 25 mails told", told())
		}
	}
	// 20 → 10 → 5 mails a round, later 10 → 5 again: three rounds too
	// large, each followed by the shortest pause. Measured between the
	// logins, not against a clock of the whole test: with a growing pause
	// the second gap would be at least two pauses.
	mu.Lock()
	defer mu.Unlock()
	if len(logins) < 3 {
		t.Fatalf("%d connections: no round was too large", len(logins))
	}
	for i := 1; i < len(logins); i++ {
		if gap := logins[i].Sub(logins[i-1]); gap >= 2*e.m.backoffMin {
			t.Fatalf("pause %d grew: %v", i, gap)
		}
	}
}

// INFO (third review): a digest whose structure is too large to read is
// shown as the beginning of its body, with a note (rawText), instead of
// mail_too_large.
func TestDigestShownAsRawText(t *testing.T) {
	part := `("message" "rfc822" NIL NIL NIL "7bit" 100 ("Tue, 17 Nov 2026 10:00:00 +0100" "x" NIL NIL NIL NIL NIL NIL NIL NIL) ("text" "plain" NIL NIL NIL "7bit" 1 1) 3)`
	structure := "(" + strings.Repeat(part, (budgetStructure+(256<<10))/len(part)) + ` "digest")`
	head := "From: \"Liste\" <liste@example.org>\r\nSubject: =?UTF-8?Q?Tages=C3=BCbersicht?=\r\nDate: Tue, 17 Nov 2026 10:00:00 +0100\r\n\r\n"
	text := "--b\r\nContent-Type: message/rfc822\r\n\r\nSubject: Eins\r\n\r\nErster Beitrag\x07\u202e\r\n--b--\r\n"
	var stored atomic.Bool
	addr, pool, _ := scripted(t, "* OK [CAPABILITY IMAP4rev1] digest\r\n", func(w *bufio.Writer, tag, cmd, line string) bool {
		upper := strings.ToUpper(line)
		switch {
		case cmd == "SELECT":
			fmt.Fprintf(w, "* 1 EXISTS\r\n* OK [UIDVALIDITY 1] v\r\n* OK [UIDNEXT 2] n\r\n%s OK [READ-WRITE] done\r\n", tag)
		case strings.Contains(upper, "BODYSTRUCTURE"):
			w.WriteString("* 1 FETCH (UID 1 FLAGS () BODYSTRUCTURE ")
			w.WriteString(structure)
			fmt.Fprintf(w, ")\r\n%s OK done\r\n", tag)
		case strings.Contains(upper, "BODY.PEEK[TEXT]"):
			fmt.Fprintf(w, "* 1 FETCH (UID 1 FLAGS () BODY[HEADER.FIELDS (FROM SENDER SUBJECT DATE)] {%d}\r\n%s BODY[TEXT]<0> {%d}\r\n%s)\r\n%s OK done\r\n",
				len(head), head, len(text), text, tag)
		case strings.Contains(upper, "STORE"):
			stored.Store(true)
			fmt.Fprintf(w, "%s OK\r\n", tag)
		default:
			fmt.Fprintf(w, "%s OK\r\n", tag)
		}
		return true
	})
	e := newEnv(t)
	id := evilMailboxes(t, e, addr, pool, 1)[0]
	msg, err := e.m.Read(t.Context(), "alex", id, "INBOX", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if !msg.RawText || !msg.Truncated || msg.HTML != "" || len(msg.Attachments) != 0 || msg.Subject != "Tagesübersicht" || msg.From.Addr != "liste@example.org" {
		t.Fatalf("message: %+v", msg)
	}
	if !strings.Contains(msg.Text, "Erster Beitrag\n--b--") || strings.ContainsAny(msg.Text, "\r\x07\u202e") {
		t.Fatalf("text: %q", msg.Text)
	}
	if !stored.Load() || !msg.Flags.Seen {
		t.Fatal("not marked as read")
	}
}
