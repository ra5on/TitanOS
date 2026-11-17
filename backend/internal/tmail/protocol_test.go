package tmail

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func plainMail(from, subject, body string) string {
	return fmt.Sprintf("From: %s\nTo: Alice <alice@example.org>\nSubject: %s\nDate: Tue, 17 Nov 2026 10:00:00 +0100\nMessage-ID: <%d@example.net>\nMIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\n%s\n",
		from, subject, time.Now().UnixNano(), body)
}

func TestLoginFoldersAndRoles(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	e.deliver("INBOX", plainMail("bob@example.net", "Hallo", "Text"))
	folders, err := e.m.Folders(t.Context(), "alex", id)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, f := range folders {
		roles[f.Path] = f.Role
	}
	want := map[string]string{"INBOX": RoleInbox, "Sent": RoleSent, "Drafts": RoleDrafts, "Trash": RoleTrash, "Archiv": RoleArchive}
	for path, role := range want {
		if roles[path] != role {
			t.Errorf("folder %s: role %q, want %q (%v)", path, roles[path], role, folders)
		}
	}
	if folders[0].Path != "INBOX" || folders[0].Total != 1 || folders[0].Unread != 1 {
		t.Errorf("INBOX first with counts: %+v", folders[0])
	}
	if got := e.m.Mailboxes("alex")[0].Unread; got != 1 {
		t.Errorf("unread of the mailbox: %d", got)
	}
}

// A refused login is not tried again until the settings change.
func TestAuthFailedStopsUntilChange(t *testing.T) {
	e := newEnv(t)
	in := e.input("wrong-password")
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Folders(t.Context(), "alex", view.ID); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("wrong password: %v", err)
	}
	before := e.srv.IMAPConns.Load()
	for range 3 {
		if _, err := e.m.Folders(t.Context(), "alex", view.ID); !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("again: %v", err)
		}
	}
	if _, err := e.m.Send(t.Context(), "alex", view.ID, Outgoing{To: []Address{{Addr: "bob@example.net"}}, Text: "x"}); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("send while refused: %v", err)
	}
	if e.srv.IMAPConns.Load() != before {
		t.Fatalf("tried again after a refused login: %d connections, %d before", e.srv.IMAPConns.Load(), before)
	}
	if st := e.m.Mailboxes("alex")[0].Status; st.State != StateAuthFailed || st.Code != "mail_auth_failed" {
		t.Fatalf("status: %+v", st)
	}
	// Saving without a password changes nothing that the login depends on.
	in.Password = ""
	if _, err := e.m.UpdateMailbox("alex", view.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Folders(t.Context(), "alex", view.ID); !errors.Is(err, ErrAuthFailed) || e.srv.IMAPConns.Load() != before {
		t.Fatalf("an unchanged save started trying: %v", err)
	}
	in.Password = testPassword
	if _, err := e.m.UpdateMailbox("alex", view.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Folders(t.Context(), "alex", view.ID); err != nil {
		t.Fatalf("after the new password: %v", err)
	}
}

func TestListPagesAndSearch(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	for i := 1; i <= 7; i++ {
		subject := fmt.Sprintf("Mail %d", i)
		if i == 4 {
			subject = "Rechnung November"
		}
		e.deliver("INBOX", plainMail("bob@example.net", subject, "Inhalt "+subject))
	}
	page, err := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "INBOX", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 7 || len(page.Messages) != 3 || page.Messages[0].UID != 7 || page.Next != 5 {
		t.Fatalf("first page: total %d, %d mails, first %d, next %d", page.Total, len(page.Messages), page.Messages[0].UID, page.Next)
	}
	if page.Messages[0].From.Addr != "bob@example.net" || page.Messages[0].Subject != "Mail 7" || page.Messages[0].Flags.Seen {
		t.Fatalf("summary: %+v", page.Messages[0])
	}
	var uids []uint32
	for next := uint32(0); ; {
		p, err := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "INBOX", Limit: 3, Before: next})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range p.Messages {
			uids = append(uids, s.UID)
		}
		if p.Next == 0 {
			break
		}
		next = p.Next
	}
	if fmt.Sprint(uids) != "[7 6 5 4 3 2 1]" {
		t.Fatalf("pages: %v", uids)
	}
	found, err := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "INBOX", Q: "rechnung"})
	if err != nil {
		t.Fatal(err)
	}
	if found.Total != 1 || found.Messages[0].Subject != "Rechnung November" {
		t.Fatalf("search: %+v", found)
	}
	if _, err := e.m.List(t.Context(), "alex", id, ListQuery{Folder: "Nirgends"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing folder: %v", err)
	}
}

const pixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func htmlMail() string {
	return `From: "Bob" <bob@example.net>
To: alice@example.org
Subject: Newsletter
Date: Tue, 17 Nov 2026 10:00:00 +0100
Message-ID: <html-1@example.net>
References: <a@example.net> <b@example.net>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="outer"

--outer
Content-Type: multipart/related; boundary="rel"

--rel
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

Gr=FC=DFe aus dem Text

--alt
Content-Type: text/html; charset=utf-8

<html><head><style>@import url(https://evil.example/x.css);</style><script>alert(1)</script></head>
<body onload="alert(2)"><p onclick="steal()">Hallo <b>Alice</b></p>
<a href="javascript:alert(3)">click</a> <a href="https://example.net/real">Link</a>
<a href="data:text/html,<script>alert(4)</script>">data</a>
<img src="https://tracker.example/pixel.gif" width="1"><img src="cid:logo@x">
<form action="https://evil.example"><input name="pw"></form><iframe src="https://evil.example"></iframe>
<base href="https://evil.example/"><meta http-equiv="refresh" content="0;url=https://evil.example">
<svg onload="alert(5)"></svg><p style="color:red;background-image:url(https://t.example/x)">rot</p>
</body></html>
--alt--

--rel
Content-Type: image/png
Content-Transfer-Encoding: base64
Content-ID: <logo@x>
Content-Disposition: inline

` + pixelPNG + `
--rel--

--outer
Content-Type: application/pdf; name="rechnung.pdf"
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="rechnung.pdf"

JVBERi0xLjQKJcOkw7zDtsOfCg==
--outer--
`
}

func TestReadMessageCleanHTML(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	e.deliver("INBOX", htmlMail())
	msg, err := e.m.Read(t.Context(), "alex", id, "INBOX", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "Grüße aus dem Text\r\n" && strings.TrimSpace(msg.Text) != "Grüße aus dem Text" {
		t.Errorf("text (charset, quoted-printable): %q", msg.Text)
	}
	for _, bad := range []string{"<script", "alert(", "onload", "onclick", "javascript:", "data:text", "<form", "<input", "<iframe", "@import", "<base", "refresh", "<svg", "tracker.example", "url(", "<style"} {
		if strings.Contains(msg.HTML, bad) {
			t.Errorf("HTML still has %q:\n%s", bad, msg.HTML)
		}
	}
	for _, good := range []string{"<b>Alice</b>", `href="https://example.net/real"`, `target="_blank"`, `rel="noopener noreferrer"`, "data:image/png;base64,", "default-src &#39;none&#39;", "color: red"} {
		if !strings.Contains(msg.HTML, good) {
			t.Errorf("HTML lacks %q:\n%s", good, msg.HTML)
		}
	}
	if !msg.ExternalImages {
		t.Error("external picture not reported")
	}
	if msg.From.Name != "Bob" || msg.MessageID != "<html-1@example.net>" || msg.References != "<a@example.net> <b@example.net>" || msg.Truncated {
		t.Errorf("head: %+v", msg)
	}
	var pdf, logo *Attachment
	for i, a := range msg.Attachments {
		switch a.Type {
		case "application/pdf":
			pdf = &msg.Attachments[i]
		case "image/png":
			logo = &msg.Attachments[i]
		}
	}
	if pdf == nil || pdf.Name != "rechnung.pdf" || pdf.Inline || pdf.Part != "2" || logo == nil || !logo.Inline || logo.CID != "logo@x" || logo.Part != "1.2" {
		t.Fatalf("attachments: %+v", msg.Attachments)
	}
	if !msg.Flags.Seen {
		t.Error("not marked as read")
	}
	// With images=1 pictures from the internet stay (https only).
	msg, err = e.m.Read(t.Context(), "alex", id, "INBOX", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.HTML, "https://tracker.example/pixel.gif") || !strings.Contains(msg.HTML, "img-src data: https:") {
		t.Errorf("images=1:\n%s", msg.HTML)
	}
	// The parts by number, decoded.
	var got strings.Builder
	var info PartInfo
	if err := e.m.Part(t.Context(), "alex", id, "INBOX", 1, "2", func(p PartInfo) io.Writer { info = p; return &got }); err != nil {
		t.Fatal(err)
	}
	if info.Name != "rechnung.pdf" || info.Type != "application/pdf" || !strings.HasPrefix(got.String(), "%PDF-1.4") {
		t.Fatalf("part: %+v %q", info, got.String())
	}
	got.Reset()
	if err := e.m.CID(t.Context(), "alex", id, "INBOX", 1, "<logo@x>", func(p PartInfo) io.Writer { info = p; return &got }); err != nil {
		t.Fatal(err)
	}
	if info.Type != "image/png" || !strings.HasPrefix(got.String(), "\x89PNG") {
		t.Fatalf("cid: %+v", info)
	}
	if err := e.m.CID(t.Context(), "alex", id, "INBOX", 1, "nothing@x", func(PartInfo) io.Writer { return io.Discard }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown cid: %v", err)
	}
	if err := e.m.Part(t.Context(), "alex", id, "INBOX", 1, "../1", func(PartInfo) io.Writer { return io.Discard }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad part: %v", err)
	}
}

func TestFlagsMoveAndDelete(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	for i := range 3 {
		e.deliver("INBOX", plainMail("bob@example.net", fmt.Sprint("M", i), "x"))
	}
	ctx := t.Context()
	if err := e.m.SetFlags(ctx, "alex", id, "INBOX", []uint32{1, 2}, []string{"seen", "flagged"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.m.SetFlags(ctx, "alex", id, "INBOX", []uint32{2}, nil, []string{"flagged"}); err != nil {
		t.Fatal(err)
	}
	if err := e.m.SetFlags(ctx, "alex", id, "INBOX", []uint32{2}, []string{"deleted"}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown flag: %v", err)
	}
	page, _ := e.m.List(ctx, "alex", id, ListQuery{Folder: "INBOX", Flagged: true})
	if page.Total != 1 || page.Messages[0].UID != 1 {
		t.Fatalf("flagged: %+v", page)
	}
	page, _ = e.m.List(ctx, "alex", id, ListQuery{Folder: "INBOX", Unread: true})
	if page.Total != 1 || page.Messages[0].UID != 3 {
		t.Fatalf("unread: %+v", page)
	}
	if err := e.m.Move(ctx, "alex", id, "INBOX", []uint32{3}, "Archiv"); err != nil {
		t.Fatal(err)
	}
	if e.count("INBOX") != 2 || e.count("Archiv") != 1 {
		t.Fatalf("move: INBOX %d, Archiv %d", e.count("INBOX"), e.count("Archiv"))
	}
	// Delete: to the trash first, from there for good.
	if err := e.m.Delete(ctx, "alex", id, "INBOX", []uint32{1}); err != nil {
		t.Fatal(err)
	}
	if e.count("INBOX") != 1 || e.count("Trash") != 1 {
		t.Fatalf("delete to trash: INBOX %d, Trash %d", e.count("INBOX"), e.count("Trash"))
	}
	trash, _ := e.m.List(ctx, "alex", id, ListQuery{Folder: "Trash"})
	if err := e.m.Delete(ctx, "alex", id, "Trash", []uint32{trash.Messages[0].UID}); err != nil {
		t.Fatal(err)
	}
	if e.count("Trash") != 0 {
		t.Fatalf("delete in trash: %d left", e.count("Trash"))
	}
}

func TestFolderChanges(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	ctx := t.Context()
	if err := e.m.CreateFolder(ctx, "alex", id, "Projekte"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.CreateFolder(ctx, "alex", id, "Projekte"); !errors.Is(err, ErrFolderExists) {
		t.Fatalf("twice: %v", err)
	}
	if err := e.m.RenameFolder(ctx, "alex", id, "Projekte", "Kunden"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.DeleteFolder(ctx, "alex", id, "Kunden"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.DeleteFolder(ctx, "alex", id, "INBOX"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("INBOX deleted: %v", err)
	}
	folders, _ := e.m.Folders(ctx, "alex", id)
	for _, f := range folders {
		if f.Path == "Projekte" || f.Path == "Kunden" {
			t.Fatalf("folder left: %v", folders)
		}
	}
}

func TestSendWithAttachmentCopyAndDraft(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	ctx := t.Context()
	if _, err := e.m.SetSettings("alex", Settings{Signatures: []SignatureView{{Name: "Kurz", Text: "Viele Grüße\nAlice", Default: []string{id}}}, PushContent: true}); err != nil {
		t.Fatal(err)
	}
	sig := e.m.Settings("alex").Signatures[0]
	if len(sig.Default) != 1 || e.m.Mailboxes("alex")[0].Signature != sig.ID {
		t.Fatalf("default signature: %+v", sig)
	}
	out := Outgoing{To: []Address{{Name: "Bob", Addr: "bob@example.net"}}, Cc: []Address{{Addr: "carol@example.net"}},
		Bcc: []Address{{Addr: "dave@example.net"}}, Subject: "Angebot", Text: "Hallo Bob,\nanbei das Angebot.", Signature: sig.ID,
		InReplyTo: "<x1@example.net>", References: "<x0@example.net> <x1@example.net>",
		Files: []File{{Name: "angebot.pdf", Type: "application/pdf", Data: []byte("%PDF-1.4 test")}}}
	// A draft first, replaced by a second one, removed by sending.
	uid, err := e.m.SaveDraft(ctx, "alex", id, out)
	if err != nil || uid == 0 {
		t.Fatalf("draft: %d %v", uid, err)
	}
	out.DraftUID = uid
	uid2, err := e.m.SaveDraft(ctx, "alex", id, out)
	if err != nil || uid2 == uid || e.count("Drafts") != 1 {
		t.Fatalf("second draft: %d %v, %d drafts", uid2, err, e.count("Drafts"))
	}
	out.DraftUID = uid2
	res, err := e.m.Send(ctx, "alex", id, out)
	if err != nil {
		t.Fatal(err)
	}
	if !res.SentCopy || e.count("Sent") != 1 || e.count("Drafts") != 0 {
		t.Fatalf("copy %v, sent %d, drafts %d", res.SentCopy, e.count("Sent"), e.count("Drafts"))
	}
	sent := e.sentMails()
	if len(sent) != 1 || sent[0].From != testUser || strings.Join(sent[0].To, ",") != "bob@example.net,carol@example.net,dave@example.net" {
		t.Fatalf("SMTP envelope: %+v", sent)
	}
	data := string(sent[0].Data)
	for _, want := range []string{"From: \"Alice\" <alice@example.org>", "Subject: Angebot", "In-Reply-To: <x1@example.net>", "angebot.pdf", "Viele Gr=C3=BC=C3=9Fe", "--=20"} {
		if !strings.Contains(data, want) {
			t.Errorf("sent mail lacks %q:\n%s", want, data)
		}
	}
	if strings.Contains(data, "dave@example.net") {
		t.Error("Bcc in the sent mail")
	}
	// The copy reads back with its attachment.
	sentList, _ := e.m.List(ctx, "alex", id, ListQuery{Folder: "Sent"})
	msg, err := e.m.Read(ctx, "alex", id, "Sent", sentList.Messages[0].UID, false)
	if err != nil || len(msg.Attachments) != 1 || msg.Attachments[0].Name != "angebot.pdf" || !strings.Contains(msg.Text, "anbei das Angebot") {
		t.Fatalf("copy: %+v %v", msg, err)
	}
	// The recipients are in the address book, on the volume.
	contacts := e.m.Contacts("alex", "bob")
	if len(contacts) != 1 || contacts[0].Name != "Bob" || contacts[0].Count != 1 {
		t.Fatalf("contacts: %+v", contacts)
	}
	fresh := e.newManager()
	if got := fresh.Contacts("alex", ""); len(got) != 3 {
		t.Fatalf("address book after a restart: %+v", got)
	}
	fresh.ForgetContact("alex", "carol@example.net")
	if got := e.newManager().Contacts("alex", ""); len(got) != 2 {
		t.Fatalf("forgotten: %+v", got)
	}
	// Limits of sending.
	many := make([]Address, MaxRecipients+1)
	for i := range many {
		many[i] = Address{Addr: fmt.Sprintf("r%d@example.net", i)}
	}
	if _, err := e.m.Send(ctx, "alex", id, Outgoing{To: many, Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("101 recipients: %v", err)
	}
	if _, err := e.m.Send(ctx, "alex", id, Outgoing{To: []Address{{Addr: "x@example.net\r\nBcc: y@example.net"}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("header injection: %v", err)
	}
	big := Outgoing{To: []Address{{Addr: "bob@example.net"}}, Files: []File{{Name: "a", Data: make([]byte, MaxAttachments+1)}}}
	if _, err := e.m.Send(ctx, "alex", id, big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
}

func TestSMTPLoginRefused(t *testing.T) {
	e := newEnv(t)
	in := e.input(testPassword)
	in.User = "someone-else"
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Send(t.Context(), "alex", view.ID, Outgoing{To: []Address{{Addr: "bob@example.net"}}, Text: "x"}); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("send: %v", err)
	}
	if st := e.m.Mailboxes("alex")[0].Status; st.State != StateAuthFailed {
		t.Fatalf("status: %+v", st)
	}
}

// IDLE: new mail goes through the rules, the rest becomes one message,
// and a new start of titand reports nothing that was there before.
func TestIdleRulesAndNotices(t *testing.T) {
	e := newEnv(t)
	e.deliver("INBOX", plainMail("old@example.net", "Vorher", "schon da"))
	in := e.input(testPassword)
	in.Notify = true
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	id := view.ID
	if _, err := e.m.SetSettings("alex", Settings{PushContent: true, Rules: []Rule{{Account: id, Enabled: true,
		When: RuleWhen{From: "Newsletter"}, Then: RuleThen{MoveTo: "Archiv", MarkRead: true}}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { e.m.Run(ctx); close(done) }()
	events, stopEvents := e.m.Subscribe("alex")
	defer stopEvents()
	waitFor(t, "IDLE", func() bool { return e.m.Mailboxes("alex")[0].Status.State == StateOK })
	time.Sleep(100 * time.Millisecond)

	e.deliver("INBOX", plainMail("\"Shop Newsletter\" <news@shop.example>", "Angebote", "kaufen"))
	waitFor(t, "rule", func() bool { return e.count("Archiv") == 1 })
	e.deliver("INBOX", plainMail("\"Bob\" <bob@example.net>", "Treffen", "morgen?"))
	waitFor(t, "notice", func() bool { return len(e.noticeList()) == 1 })
	n := e.noticeList()[0]
	if n.Count != 1 || n.From != "Bob" || n.Subject != "Treffen" || n.MailboxName != "Privat" || !n.Content {
		t.Fatalf("notice: %+v", n)
	}
	// Several mails at once: one message.
	e.deliver("INBOX", plainMail("carol@example.net", "Eins", "1"))
	e.deliver("INBOX", plainMail("dave@example.net", "Zwei", "2"))
	waitFor(t, "collected notice", func() bool { return len(e.noticeList()) == 2 })
	if n := e.noticeList()[1]; n.Count != 2 || n.Subject != "Zwei" {
		t.Fatalf("collected: %+v", n)
	}
	time.Sleep(400 * time.Millisecond)
	if len(e.noticeList()) != 2 {
		t.Fatalf("notices: %+v", e.noticeList())
	}
	seen := map[string]int{}
	for len(events) > 0 {
		ev := <-events
		if ev.Type == "mail" {
			seen[fmt.Sprint(ev.Data.(map[string]any)["folder"])]++
		}
	}
	if seen["INBOX"] != 3 || seen["Archiv"] != 1 {
		t.Fatalf("events: %v", seen)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop at once")
	}

	// A new start: the mails of before stay quiet, a new one is told.
	again := e.newManager()
	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()
	go again.Run(ctx2)
	waitFor(t, "IDLE after the start", func() bool { return again.Mailboxes("alex")[0].Status.State == StateOK })
	time.Sleep(500 * time.Millisecond)
	if len(e.noticeList()) != 2 {
		t.Fatalf("a start told old mail again: %+v", e.noticeList())
	}
	e.deliver("INBOX", plainMail("erin@example.net", "Neu", "neu"))
	waitFor(t, "notice after the start", func() bool { return len(e.noticeList()) == 3 })
}

// Without messages and rules there is no permanent connection.
func TestNoWatcherWhenOff(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go e.m.Run(ctx)
	time.Sleep(300 * time.Millisecond)
	if n := e.srv.IMAPConns.Load(); n != 0 {
		t.Fatalf("%d connections without messages or rules", n)
	}
	if st := e.m.Mailboxes("alex")[0].Status; st.State != StateOff {
		t.Fatalf("status %+v", st)
	}
	_ = id
}

func TestStartTLSRequired(t *testing.T) {
	e := newEnv(t)
	in := e.input(testPassword)
	// The implicit TLS port spoken to as STARTTLS: no plain text goes out.
	in.IMAP.Security = SecurityStartTLS
	view, err := e.m.AddMailbox("alex", in)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	if _, err := e.m.Folders(ctx, "alex", view.ID); err == nil {
		t.Fatal("STARTTLS against a TLS port went through")
	}
}

func TestCertificateChecked(t *testing.T) {
	e := newEnv(t)
	id := e.addMailbox()
	e.m.RootCAs = nil // the system's roots do not know the test's certificate
	if _, err := e.m.Folders(t.Context(), "alex", id); !errors.Is(err, ErrTLS) {
		t.Fatalf("unknown certificate: %v", err)
	}
	if _, err := e.m.Send(t.Context(), "alex", id, Outgoing{To: []Address{{Addr: "bob@example.net"}}, Text: "x"}); !errors.Is(err, ErrTLS) {
		t.Fatalf("SMTP with an unknown certificate: %v", err)
	}
}
