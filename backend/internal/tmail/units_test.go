package tmail

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

// nested builds a structure n levels deep.
func nested(n int) imap.BodyStructure {
	var bs imap.BodyStructure = &imap.BodyStructureSinglePart{Type: "text", Subtype: "plain", Encoding: "7bit", Size: 10}
	for range n {
		bs = &imap.BodyStructureMultiPart{Subtype: "mixed", Children: []imap.BodyStructure{bs}}
	}
	return bs
}

func TestMIMELimits(t *testing.T) {
	if p := walkStructure(nested(MaxDepth - 1)); p.truncated || p.text == nil {
		t.Fatalf("depth %d: %+v", MaxDepth-1, p)
	}
	if p := walkStructure(nested(100)); !p.truncated || p.text != nil {
		t.Fatalf("depth 100: %+v", p)
	}
	wide := &imap.BodyStructureMultiPart{Subtype: "mixed"}
	for i := range 5000 {
		wide.Children = append(wide.Children, &imap.BodyStructureSinglePart{Type: "application", Subtype: "octet-stream", Encoding: "base64", Size: 4,
			Extended: &imap.BodyStructureSinglePartExt{Disposition: &imap.BodyStructureDisposition{Value: "attachment", Params: map[string]string{"filename": fmt.Sprint(i)}}}})
	}
	p := walkStructure(wide)
	if !p.truncated || len(p.attachments) >= MaxParts || p.parts > MaxParts {
		t.Fatalf("5000 parts: truncated %v, %d attachments", p.truncated, len(p.attachments))
	}
	// A text larger than the limit is cut, never read whole.
	long := strings.Repeat("ä", MaxText)
	text, cut := decodeText(partInfo{encoding: "8bit", params: map[string]string{"charset": "utf-8"}}, []byte(long), false, MaxText)
	if !cut || len(text) > MaxText || !strings.HasPrefix(long, text) {
		t.Fatalf("long text: cut %v, %d bytes", cut, len(text))
	}
	if got := encodedLimit(partInfo{encoding: "base64"}, MaxText); got < MaxText*4/3 || got > MaxText*2 {
		t.Fatalf("encoded limit %d", got)
	}
	// Names of attachments cannot carry paths or control characters.
	for in, want := range map[string]string{"../../etc/passwd": "passwd", "a\x00b\r\n.pdf": "a_b__.pdf", "": "attachment", `C:\x\y.doc`: "y.doc"} {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	if _, ok := parsePart("1.2.3"); !ok {
		t.Error("1.2.3")
	}
	for _, bad := range []string{"", "0", "1..2", "1.2.", "HEADER", "1.MIME", "1.2.3.4.5.6.7.8.9.10.11"} {
		if _, ok := parsePart(bad); ok {
			t.Errorf("part %q accepted", bad)
		}
	}
}

// A hostile mail through the whole way: deep nesting and many parts are
// cut off, the head and the list still come.
func TestHostileMailIsShownWithinLimits(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	var b strings.Builder
	b.WriteString("From: x@example.net\nSubject: deep\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=b0\n\n")
	for i := range 30 {
		fmt.Fprintf(&b, "--b%d\nContent-Type: multipart/mixed; boundary=b%d\n\n", i, i+1)
	}
	b.WriteString("--b30\nContent-Type: text/plain\n\ntief\n--b30--\n")
	for i := 29; i >= 0; i-- {
		fmt.Fprintf(&b, "--b%d--\n", i)
	}
	e.deliver("INBOX", b.String())
	b.Reset()
	b.WriteString("From: x@example.net\nSubject: wide\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=w\n\n--w\nContent-Type: text/plain\n\nbreit\n")
	for i := range 400 {
		fmt.Fprintf(&b, "--w\nContent-Type: application/octet-stream\nContent-Disposition: attachment; filename=f%d.bin\n\nx\n", i)
	}
	b.WriteString("--w--\n")
	e.deliver("INBOX", b.String())
	deep, err := e.m.Read(t.Context(), "alex", id, "INBOX", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if !deep.Truncated || deep.Text != "" || deep.Subject != "deep" {
		t.Fatalf("deep: truncated %v, text %q", deep.Truncated, deep.Text)
	}
	wide, err := e.m.Read(t.Context(), "alex", id, "INBOX", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if !wide.Truncated || len(wide.Attachments) >= MaxParts || !strings.Contains(wide.Text, "breit") {
		t.Fatalf("wide: truncated %v, %d attachments, text %q", wide.Truncated, len(wide.Attachments), wide.Text)
	}
}

func TestSanitizeHTML(t *testing.T) {
	doc, external := SanitizeHTML(`<p>x</p><a href="JaVaScRiPt:alert(1)" onmouseover="x()">a</a><a href="mailto:bob@example.net">m</a>`+
		`<img src="http://plain.example/x.png"><img src="data:image/png;base64,AAAA"><img src="cid:missing"><object data="x"></object>`+
		`<table background="https://t.example/bg.png" bgcolor="#fff"><tr><td style="width:10px;behavior:url(x.htc)">c</td></tr></table>`, nil, true)
	for _, bad := range []string{"javascript", "JaVaScRiPt", "onmouseover", "plain.example", "data:image/png;base64,AAAA", "<object", "background=", "behavior", "x.htc"} {
		if strings.Contains(doc, bad) {
			t.Errorf("%q left in %s", bad, doc)
		}
	}
	if !external || !strings.Contains(doc, `href="mailto:bob@example.net"`) || !strings.Contains(doc, `bgcolor="#fff"`) || !strings.Contains(doc, "width: 10px") {
		t.Errorf("cleaned: %s", doc)
	}
	if !strings.HasPrefix(doc, "<!doctype html>") || !strings.Contains(doc, "Content-Security-Policy") {
		t.Errorf("document: %s", doc)
	}
}

func TestForbiddenTargets(t *testing.T) {
	own := []netip.Addr{netip.MustParseAddr("192.168.1.10"), netip.MustParseAddr("fd00::10")}
	ports := []int{443, 80, 8750}
	for _, target := range []string{"127.0.0.1:993", "127.8.9.1:993", "[::1]:993", "169.254.169.254:80", "[fe80::1]:993", "0.0.0.0:993",
		"224.0.0.1:993", "192.168.1.10:443", "192.168.1.10:80", "[fd00::10]:8750", "[::ffff:127.0.0.1]:993"} {
		if err := CheckTarget(netip.MustParseAddrPort(target), own, ports); !errors.Is(err, ErrHostForbidden) {
			t.Errorf("%s allowed", target)
		}
	}
	for _, target := range []string{"192.168.1.10:993", "192.168.1.20:443", "10.0.0.5:143", "203.0.113.7:993", "[2001:db8::1]:465"} {
		if err := CheckTarget(netip.MustParseAddrPort(target), own, ports); err != nil {
			t.Errorf("%s refused: %v", target, err)
		}
	}
	// The real dialer refuses loopback, also behind a name.
	e := newEnv(t)
	e.m.targets = nil
	for _, host := range []string{"127.0.0.1", "localhost"} {
		in := e.input(testPassword)
		in.IMAP.Host = host
		view, err := e.m.AddMailbox("alex", in)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.m.Folders(t.Context(), "alex", view.ID); !errors.Is(err, ErrHostForbidden) {
			t.Errorf("%s: %v", host, err)
		}
	}
	if e.imapConn.Load() != 0 {
		t.Fatal("a refused target was reached")
	}
	if _, err := e.m.AddMailbox("alex", MailboxInput{Email: testUser, IMAP: Server{Host: "https://x.example/"}, SMTP: Server{Host: "x.example"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("host with scheme: %v", err)
	}
}

// Passwords lie on the stick only encrypted and never come back.
func TestPasswordsEncrypted(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	views, _ := json.Marshal(e.m.Mailboxes("alex"))
	if strings.Contains(string(views), testPassword) || !strings.Contains(string(views), `"passwordSet":true`) {
		t.Fatalf("view: %s", views)
	}
	walk := func() map[string][]byte {
		files := map[string][]byte{}
		_ = filepath.Walk(e.store.Dir(), func(path string, info os.FileInfo, err error) error {
			if err == nil && info.Mode().IsRegular() {
				data, _ := os.ReadFile(path)
				files[path] = data
				if strings.Contains(string(data), testPassword) {
					t.Errorf("plain password in %s", path)
				}
			}
			return nil
		})
		return files
	}
	before := walk()
	info, err := os.Stat(filepath.Join(e.store.Dir(), keyFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", info, err)
	}
	// A save without a password writes the same ciphertext.
	in := e.input("")
	in.Name = "Privat"
	if _, err := e.m.UpdateMailbox("alex", id, in); err != nil {
		t.Fatal(err)
	}
	after := walk()
	if string(before[filepath.Join(e.store.Dir(), userPath("alex"))]) != string(after[filepath.Join(e.store.Dir(), userPath("alex"))]) {
		t.Fatal("an unchanged password was written again")
	}
	// The ciphertext belongs to its mailbox: copied to another, it does
	// not open.
	other, err := e.m.AddMailbox("alex", e.input(""))
	if err != nil {
		t.Fatal(err)
	}
	e.m.mu.Lock()
	f := e.m.users["alex"]
	sealed := f.Mailboxes[0].Password
	f.Mailboxes[1].Password = sealed
	e.m.mu.Unlock()
	mb, _ := e.m.mailbox("alex", other.ID)
	if _, err := e.m.password("alex", mb); err == nil {
		t.Fatal("a password opened in another mailbox")
	}
	// A new start reads them back.
	fresh := e.newManager()
	mb, _ = fresh.mailbox("alex", id)
	if pw, err := fresh.password("alex", mb); err != nil || pw != testPassword {
		t.Fatalf("after a start: %q %v", pw, err)
	}
}

func TestGrantIsOffAndBelongsToTheAccount(t *testing.T) {
	e := newEnv(t)
	e.accounts["bob"] = Account{Name: "bob", Created: time.Now().Add(-time.Hour)}
	if e.m.Allowed("bob") {
		t.Fatal("grant on for a new account")
	}
	if _, err := e.m.AddMailbox("bob", e.input(testPassword)); !errors.Is(err, ErrDisabled) {
		t.Fatalf("without grant: %v", err)
	}
	if err := e.m.SetAllowed("bob", true); err != nil || !e.m.Allowed("bob") {
		t.Fatal(err)
	}
	// Locked: no T-Mail.
	e.accounts["bob"] = Account{Name: "bob", Created: e.accounts["bob"].Created, Disabled: true}
	if e.m.Allowed("bob") || !e.m.Granted("bob") {
		t.Fatal("locked account")
	}
	// An account made later under the name does not inherit the grant,
	// also when the deletion could not be noted.
	e.accounts["bob"] = Account{Name: "bob", Created: time.Now().Add(time.Minute)}
	if e.m.Allowed("bob") || e.m.Granted("bob") {
		t.Fatal("grant inherited")
	}
}

func TestForgetUser(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	if _, err := e.m.Send(t.Context(), "alex", id, Outgoing{To: []Address{{Addr: "bob@example.net"}}, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.volume, "alex", contactsFileName)); err != nil {
		t.Fatalf("address book not on the volume: %v", err)
	}
	events, _ := e.m.Subscribe("alex")
	if err := e.m.ForgetUser("alex"); err != nil {
		t.Fatal(err)
	}
	if _, open := <-events; open {
		t.Fatal("event stream still open")
	}
	for _, path := range []string{filepath.Join(e.store.Dir(), userPath("alex")), filepath.Join(e.volume, "alex")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left: %v", path, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(e.store.Dir(), accessFile))
	if strings.Contains(string(data), "alex") {
		t.Errorf("grant left: %s", data)
	}
	if e.m.Allowed("alex") || len(e.m.Mailboxes("alex")) != 0 || len(e.m.Contacts("alex", "")) != 0 {
		t.Fatal("something of the account is left")
	}
}

// Settings of an earlier account of the same name are not shown to a new
// one, even when the deletion could not remove them.
func TestStaleSettingsNotInherited(t *testing.T) {
	e := newEnv(t)
	e.addMailbox()
	e.accounts["alex"] = Account{Name: "alex", Created: time.Now().Add(time.Hour)}
	fresh := e.newManager()
	if got := fresh.Mailboxes("alex"); len(got) != 0 {
		t.Fatalf("inherited: %+v", got)
	}
}

func TestProviders(t *testing.T) {
	for _, c := range []struct {
		email, id   string
		supported   bool
		appPassword bool
	}{{"max@gmx.de", "gmx", true, false}, {"max@web.de", "webde", true, false}, {"max@t-online.de", "tonline", true, true},
		{"max@icloud.com", "icloud", true, true}, {"max@gmail.com", "gmail", true, true}, {"max@hotmail.com", "outlook", false, false},
		{"max@posteo.de", "posteo", true, false}, {"max@mailbox.org", "mailboxorg", true, false}, {"max@ionos.de", "ionos", true, false}} {
		p, ok := ProviderFor(c.email)
		if !ok || p.ID != c.id || p.Supported != c.supported || p.AppPassword != c.appPassword {
			t.Errorf("%s: %+v %v", c.email, p, ok)
		}
	}
	if _, ok := ProviderFor("me@my-own-domain.example"); ok {
		t.Error("unknown domain suggested")
	}
}

func TestSettingsValidation(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	for _, s := range []Settings{
		{Rules: []Rule{{Account: "nope", Enabled: true, When: RuleWhen{From: "x"}, Then: RuleThen{Flag: true}}}},
		{Rules: []Rule{{Account: id, Enabled: true, Then: RuleThen{Flag: true}}}},
		{Rules: []Rule{{Account: id, Enabled: true, When: RuleWhen{From: "x"}}}},
		{Signatures: []SignatureView{{Name: ""}}},
	} {
		if _, err := e.m.SetSettings("alex", s); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", s, err)
		}
	}
	got, err := e.m.SetSettings("alex", Settings{Rules: []Rule{{Account: id, Enabled: true, When: RuleWhen{Subject: "x"}, Then: RuleThen{MoveTo: "A"}}}})
	if err != nil || len(got.Rules) != 1 || got.Rules[0].ID == "" || got.PushContent {
		t.Fatalf("%+v %v", got, err)
	}
	// Removing the mailbox takes its rules along.
	if err := e.m.RemoveMailbox("alex", id); err != nil {
		t.Fatal(err)
	}
	if got := e.m.Settings("alex"); len(got.Rules) != 0 {
		t.Fatalf("rules left: %+v", got.Rules)
	}
}
