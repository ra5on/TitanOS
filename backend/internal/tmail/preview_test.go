package tmail

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestPreviewOfBodies(t *testing.T) {
	long := strings.Repeat("Wort ", 60)
	cases := []struct {
		name, ct, cte, body, want string
	}{
		{"plain", "text/plain; charset=utf-8", "", "Hallo Alex,\r\n\r\nanbei die Rechnung.\r\n", "Hallo Alex, anbei die Rechnung."},
		{"no type", "", "", "Nur Text", "Nur Text"},
		{"quoted-printable latin1", "text/plain; charset=iso-8859-1", "quoted-printable", "Gr=FC=DFe aus K=F6ln=\r\n und mehr", "Grüße aus Köln und mehr"},
		{"base64 cut", "text/plain; charset=utf-8", "base64", "SGFsbG8gV2VsdCwgd2llIGdlaHQg\r\nZXM/IEhhbGxvIFdlbH", "Hallo Welt, wie geht es? Hallo We"},
		{"quotes and signature", "text/plain", "", "Ja, passt.\n\n> Passt dir Freitag?\n> Gruß\n-- \nAlex Muster\nTel 123", "Ja, passt."},
		{"addresses and long words", "text/plain", "", "Siehe https://example.org/a?b=c und www.example.org oder " + strings.Repeat("x", 41) + " Ende", "Siehe und oder Ende"},
		{"invisible characters", "text/plain", "", "Rech\u200bnung\u202e fdp.exe \x07da", "Rechnung fdp.exe da"},
		{"html", "text/html; charset=utf-8", "", "<html><head><title>T</title><style>p{color:red}</style></head><body><script>alert(1)</script><p>Guten&nbsp;Tag &amp; willkommen</p><blockquote>alt</blockquote><p>Neu</p></body></html>", "Guten Tag & willkommen Neu"},
		{"html cut in a tag", "text/html", "", "<p>Anfang</p><a href=\"https://exa", "Anfang"},
		{"encrypted", "multipart/encrypted; boundary=x", "", "--x\r\nContent-Type: text/plain\r\n\r\ngeheim\r\n--x--", ""},
		{"pkcs7", "application/pkcs7-mime", "base64", "AAAA", ""},
		{"pdf alone", "application/pdf", "base64", "JVBERi0=", ""},
		{"alternative", "multipart/alternative; boundary=\"b1\"", "", "This is a multi-part message.\r\n--b1\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nSch=C3=B6ne Gr=C3=BC=C3=9Fe\r\n--b1\r\nContent-Type: text/html\r\n\r\n<p>HTML</p>\r\n--b1--\r\n", "Schöne Grüße"},
		{"nested mixed", "multipart/mixed; boundary=outer", "", "--outer\r\nContent-Type: multipart/alternative; boundary=inner\r\n\r\n--inner\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<div>Nur <b>HTML</b> hier</div>\r\n--inner--\r\n--outer\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment\r\n\r\nJVBERi0=\r\n--outer--\r\n", "Nur HTML hier"},
		{"attachment first", "multipart/mixed; boundary=o", "", "--o\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=a.txt\r\n\r\nanhang\r\n--o\r\nContent-Type: text/plain\r\n\r\nder Text\r\n--o--", "der Text"},
		{"cut part", "multipart/alternative; boundary=b", "", "--b\r\nContent-Type: text/plain\r\n\r\nDer Anfang ohne Ende", "Der Anfang ohne Ende"},
		{"cut header", "multipart/alternative; boundary=b", "", "--b\r\nContent-Type: text/pl", ""},
		{"bad type", "text/plain; charset=\"", "", "x", ""},
	}
	for _, c := range cases {
		if got := previewOf(c.ct, c.cte, []byte(c.body)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	got := previewOf("text/plain", "", []byte(long))
	if utf8.RuneCountInString(got) > previewRunes || strings.HasSuffix(got, " ") || strings.Contains(got, "…") || !strings.HasSuffix(got, "Wort") {
		t.Fatalf("long: %d runes %q", utf8.RuneCountInString(got), got)
	}
	// A text without spaces never comes longer than the limit either.
	if got := previewOf("text/plain", "", []byte(strings.Repeat("ab ", 100))); utf8.RuneCountInString(got) > previewRunes {
		t.Fatalf("short words: %d", utf8.RuneCountInString(got))
	}
}

func TestPreviewDeepNestingStops(t *testing.T) {
	body := ""
	for i := range 10 {
		body += fmt.Sprintf("--b%d\r\nContent-Type: multipart/mixed; boundary=b%d\r\n\r\n", i, i+1)
	}
	body += "--b10\r\nContent-Type: text/plain\r\n\r\ntief\r\n"
	if got := previewOf("multipart/mixed; boundary=b0", "", []byte(body)); got != "" {
		t.Fatalf("depth: %q", got)
	}
}

// The list brings preview, thread heads, $Forwarded and the folder
// delimiter, and listing marks nothing read (1.12).
func TestListPreviewThreadAndForwarded(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	e.deliver("INBOX", "From: bob@example.net\nSubject: Frage\nDate: Tue, 17 Nov 2026 10:00:00 +0100\nMessage-ID: <root@example.net>\nMIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\nKommst du am Freitag?\n")
	refs := "<root@example.net>"
	for i := range 30 {
		refs += fmt.Sprintf(" <r%d@example.net>", i)
	}
	e.deliver("INBOX", "From: alice@example.org\nSubject: Re: Frage\nDate: Tue, 17 Nov 2026 11:00:00 +0100\nMessage-ID: <answer@example.org>\nIn-Reply-To: <r29@example.net>\nReferences: "+refs+"\nMIME-Version: 1.0\nContent-Type: multipart/alternative; boundary=zz\n\n--zz\nContent-Type: text/plain; charset=utf-8\nContent-Transfer-Encoding: base64\n\nSmEsIGljaCBrb21tZS4K\n--zz--\n")
	ctx := t.Context()
	err := e.m.withIMAP(ctx, "alex", id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, "INBOX"); err != nil {
			return err
		}
		return c.Store(uidSet(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{flagForwarded}}, nil).Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.m.List(ctx, "alex", id, ListQuery{Folder: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	answer, first := page.Messages[0], page.Messages[1]
	if first.Preview != "Kommst du am Freitag?" || !first.Flags.Forwarded || first.Flags.Seen || first.MessageID != "<root@example.net>" {
		t.Fatalf("first: %+v", first)
	}
	if answer.Preview != "Ja, ich komme." || answer.Flags.Forwarded || answer.InReplyTo != "<r29@example.net>" {
		t.Fatalf("answer: %+v", answer)
	}
	ids := strings.Fields(answer.References)
	if len(ids) != listReferences || ids[0] != "<root@example.net>" || ids[len(ids)-1] != "<r29@example.net>" || ids[1] != "<r11@example.net>" {
		t.Fatalf("references: %v", ids)
	}
	// Listing peeked: both still unread.
	unread, _ := e.m.List(ctx, "alex", id, ListQuery{Folder: "INBOX", Unread: true})
	if unread.Total != 2 {
		t.Fatalf("listing marked mails read: %d unread", unread.Total)
	}
	folders, err := e.m.Folders(ctx, "alex", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range folders {
		if f.Delim != "/" && f.Delim != "." {
			t.Fatalf("folder %q without delimiter %q", f.Path, f.Delim)
		}
	}
}

// A mail whose head is too large for the budget still lands in the
// one-by-one fallback (tooLarge), and the others keep their previews.
func TestListTooLargeKeepsPreviews(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	e.deliver("INBOX", plainMail("bob@example.net", "Eins", "Erster Text"))
	e.deliver("INBOX", plainMail("bob@example.net", strings.Repeat("Riesig ", 800<<10), "nie gezeigt"))
	e.deliver("INBOX", plainMail("bob@example.net", "Drei", "Dritter Text"))
	page, err := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 3 || !page.Messages[1].TooLarge || page.Messages[1].Preview != "" ||
		page.Messages[0].Preview != "Dritter Text" || page.Messages[2].Preview != "Erster Text" {
		t.Fatalf("page: %+v", page.Messages)
	}
}
