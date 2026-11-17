package tmail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// File is an attachment of a mail to send.
type File struct {
	Name string
	Type string
	Data []byte
}

// Outgoing is a mail to send or to keep as a draft. From is always the
// address of the mailbox.
type Outgoing struct {
	To, Cc, Bcc []Address
	Subject     string
	Text        string
	InReplyTo   string
	References  string
	// Signature is the ID of a signature put below the text ("" none).
	Signature string
	// DraftUID is the draft this mail replaces (removed after sending or
	// after the new draft is kept).
	DraftUID uint32
	Files    []File
}

// SendResult says what happened besides sending.
type SendResult struct {
	// SentCopy: a copy lies in the sent folder (by titand or, as with
	// Gmail, by the provider).
	SentCopy bool `json:"sentCopy"`
}

// checkAddresses cleans the recipients: real addresses, one line each.
func checkAddresses(list []Address) ([]*gomail.Address, error) {
	out := []*gomail.Address{}
	for _, a := range list {
		addr := strings.TrimSpace(a.Addr)
		parsed, err := mail.ParseAddress(addr)
		if err != nil || parsed.Address != addr || strings.ContainsAny(a.Name, "\r\n\x00") || len(a.Name) > 256 {
			return nil, invalid("address %q", clean(addr, 100))
		}
		out = append(out, &gomail.Address{Name: strings.TrimSpace(a.Name), Address: addr})
	}
	return out, nil
}

// compose builds the mail as it goes out (without Bcc) and the envelope
// recipients.
func (m *Manager) compose(user string, mb Mailbox, out Outgoing, draft bool) ([]byte, []string, error) {
	to, err := checkAddresses(out.To)
	if err != nil {
		return nil, nil, err
	}
	cc, err := checkAddresses(out.Cc)
	if err != nil {
		return nil, nil, err
	}
	bcc, err := checkAddresses(out.Bcc)
	if err != nil {
		return nil, nil, err
	}
	all := len(to) + len(cc) + len(bcc)
	if all > MaxRecipients {
		return nil, nil, invalid("at most %d recipients", MaxRecipients)
	}
	if all == 0 && !draft {
		return nil, nil, invalid("no recipient")
	}
	if !shortText(out.Subject, 998) || len(out.Text) > MaxText*2 || strings.ContainsRune(out.Text, 0) {
		return nil, nil, invalid("subject or text")
	}
	var size int
	for _, f := range out.Files {
		size += len(f.Data)
	}
	if size > MaxAttachments {
		return nil, nil, ErrTooLarge
	}
	text := strings.ReplaceAll(out.Text, "\r\n", "\n")
	if out.Signature != "" {
		if sig, ok := m.signatureText(user, out.Signature); ok && sig != "" {
			text = strings.TrimRight(text, "\n") + "\n\n-- \n" + sig
		}
	}
	text = strings.ReplaceAll(text, "\n", "\r\n")

	var h gomail.Header
	h.SetDate(m.clock())
	h.SetAddressList("From", []*gomail.Address{{Name: mb.SenderName, Address: mb.Email}})
	if len(to) > 0 {
		h.SetAddressList("To", to)
	}
	if len(cc) > 0 {
		h.SetAddressList("Cc", cc)
	}
	if draft && len(bcc) > 0 {
		// A draft keeps its Bcc so that it can be opened again; it is
		// never sent like this.
		h.SetAddressList("Bcc", bcc)
	}
	h.SetSubject(out.Subject)
	_, domain, _ := strings.Cut(mb.Email, "@")
	if err := h.GenerateMessageIDWithHostname(domain); err != nil {
		return nil, nil, err
	}
	if id := msgIDs(out.InReplyTo, 1); len(id) == 1 {
		h.SetMsgIDList("In-Reply-To", id)
	}
	if refs := msgIDs(out.References, 50); len(refs) > 0 {
		h.SetMsgIDList("References", refs)
	}
	h.Set("User-Agent", "T-Mail (TitanOS)")
	var buf bytes.Buffer
	textHeader := gomail.InlineHeader{}
	textHeader.Set("Content-Type", "text/plain; charset=utf-8")
	textHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	if len(out.Files) == 0 {
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		w, err := gomail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, nil, err
		}
		if _, err := io.WriteString(w, text); err != nil {
			return nil, nil, err
		}
		if err := w.Close(); err != nil {
			return nil, nil, err
		}
	} else {
		mw, err := gomail.CreateWriter(&buf, h)
		if err != nil {
			return nil, nil, err
		}
		w, err := mw.CreateSingleInline(textHeader)
		if err != nil {
			return nil, nil, err
		}
		if _, err := io.WriteString(w, text); err != nil {
			return nil, nil, err
		}
		if err := w.Close(); err != nil {
			return nil, nil, err
		}
		for _, f := range out.Files {
			var ah gomail.AttachmentHeader
			mediaType := f.Type
			if _, _, err := mime.ParseMediaType(mediaType); err != nil || mediaType == "" {
				mediaType = "application/octet-stream"
			}
			ah.Set("Content-Type", mediaType)
			ah.SetFilename(safeFileName(f.Name))
			aw, err := mw.CreateAttachment(ah)
			if err != nil {
				return nil, nil, err
			}
			if _, err := aw.Write(f.Data); err != nil {
				return nil, nil, err
			}
			if err := aw.Close(); err != nil {
				return nil, nil, err
			}
		}
		if err := mw.Close(); err != nil {
			return nil, nil, err
		}
	}
	var rcpt []string
	for _, list := range [][]*gomail.Address{to, cc, bcc} {
		for _, a := range list {
			rcpt = append(rcpt, a.Address)
		}
	}
	return buf.Bytes(), rcpt, nil
}

// msgIDs takes up to max message IDs out of a header value, without the
// angle brackets (go-message adds them).
func msgIDs(value string, max int) []string {
	var out []string
	for _, field := range strings.Fields(value) {
		id := strings.Trim(field, "<>")
		if id == "" || len(id) > 900 || strings.ContainsAny(id, "<>\r\n\x00 ") || !strings.Contains(id, "@") {
			continue
		}
		out = append(out, id)
		if len(out) == max {
			break
		}
	}
	return out
}

// smtpSend hands a mail to the SMTP server of a mailbox.
func (m *Manager) smtpSend(ctx context.Context, user string, mb Mailbox, from string, rcpt []string, data []byte) error {
	password, err := m.password(user, mb)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	conn, err := m.dial(ctx, mb.SMTP)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(sendTimeout)) // a failure shows in the next read or write
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	var c *smtp.Client
	cfg := m.tlsConfig(mb.SMTP.Host)
	if mb.SMTP.Security == SecurityTLS {
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close() // given up
			return tlsError(err)
		}
		c = smtp.NewClient(tc)
	} else {
		c, err = smtp.NewClientStartTLS(conn, cfg)
		if err != nil {
			_ = conn.Close() // given up
			if ctx.Err() != nil {
				return ErrTimeout
			}
			return tlsError(err)
		}
	}
	defer c.Close()
	var auth sasl.Client
	switch {
	case c.SupportsAuth(sasl.Plain):
		auth = sasl.NewPlainClient("", mb.User, password)
	case c.SupportsAuth(sasl.Login):
		auth = sasl.NewLoginClient(mb.User, password)
	default:
		return fmt.Errorf("%w: the server offers no login with password", ErrSendFailed)
	}
	if err := c.Auth(auth); err != nil {
		var smtpErr *smtp.SMTPError
		if errors.As(err, &smtpErr) && (smtpErr.Code == 535 || smtpErr.Code == 534 || smtpErr.Code == 530) {
			return ErrAuthFailed
		}
		return smtpError(ctx, err)
	}
	if err := c.SendMail(from, rcpt, bytes.NewReader(data)); err != nil {
		return smtpError(ctx, err)
	}
	_ = c.Quit() // the mail is accepted; a failing goodbye changes nothing
	return nil
}

// smtpError keeps the answer of the server (never a password: none is in
// it) for the person.
func smtpError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ErrTimeout
	}
	var smtpErr *smtp.SMTPError
	if errors.As(err, &smtpErr) {
		return fmt.Errorf("%w: %d %s", ErrSendFailed, smtpErr.Code, safeText(smtpErr.Message))
	}
	if c := classify(err); errors.Is(c, ErrTimeout) || errors.Is(c, ErrTLS) {
		return c
	}
	return fmt.Errorf("%w: %s", ErrSendFailed, safeText(err.Error()))
}

// Send sends a mail from a mailbox, keeps a copy in its sent folder,
// removes the draft it came from and learns the recipients.
func (m *Manager) Send(ctx context.Context, user, id string, out Outgoing) (SendResult, error) {
	if !m.Allowed(user) {
		return SendResult{}, ErrDisabled
	}
	mb, err := m.mailbox(user, id)
	if err != nil {
		return SendResult{}, err
	}
	if m.authFailed(user, mb) {
		return SendResult{}, ErrAuthFailed
	}
	data, rcpt, err := m.compose(user, mb, out, false)
	if err != nil {
		return SendResult{}, err
	}
	if err := m.smtpSend(ctx, user, mb, mb.Email, rcpt, data); err != nil {
		if errors.Is(err, ErrAuthFailed) {
			m.setAuthFailed(user, mb)
		}
		return SendResult{}, err
	}
	m.learn(user, append(append(append([]Address{}, out.To...), out.Cc...), out.Bcc...))
	result := SendResult{}
	// The copy and the draft after the mail went out: their failure does
	// not make the mail unsent.
	err = m.withIMAP(context.WithoutCancel(ctx), user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if c.Caps().Has("X-GM-EXT-1") {
			result.SentCopy = true // Gmail keeps the copy itself
		} else if sent, err := folderOf(c, RoleSent, "Sent", true); err == nil {
			if _, err := appendMail(c, sent, data, []imap.Flag{imap.FlagSeen}); err == nil {
				result.SentCopy = true
			} else {
				slog.Warn("T-Mail: copy of a sent mail not kept", "account", user, "err", safeText(err.Error()))
			}
		}
		if out.DraftUID != 0 {
			removeDraft(c, s, out.DraftUID)
		}
		return nil
	})
	if err != nil {
		slog.Warn("T-Mail: sent mail not filed", "account", user, "err", safeText(err.Error()))
	}
	return result, nil
}

// SaveDraft keeps a mail in the drafts folder and removes the draft it
// replaces; it returns the UID of the new draft (0 if the server does not
// tell it).
func (m *Manager) SaveDraft(ctx context.Context, user, id string, out Outgoing) (uint32, error) {
	mb, err := m.mailbox(user, id)
	if err != nil {
		return 0, err
	}
	data, _, err := m.compose(user, mb, out, true)
	if err != nil {
		return 0, err
	}
	var uid uint32
	err = m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		drafts, err := folderOf(c, RoleDrafts, "Drafts", true)
		if err != nil {
			return err
		}
		if uid, err = appendMail(c, drafts, data, []imap.Flag{imap.FlagSeen, imap.FlagDraft}); err != nil {
			return err
		}
		if out.DraftUID != 0 && out.DraftUID != uid {
			removeDraft(c, s, out.DraftUID)
		}
		return nil
	})
	return uid, err
}

func appendMail(c *imapclient.Client, folder string, data []byte, flags []imap.Flag) (uint32, error) {
	cmd := c.Append(folder, int64(len(data)), &imap.AppendOptions{Flags: flags})
	if _, err := cmd.Write(data); err != nil {
		_ = cmd.Close() // the write error is what counts
		return 0, err
	}
	if err := cmd.Close(); err != nil {
		return 0, err
	}
	res, err := cmd.Wait()
	if err != nil {
		return 0, err
	}
	return uint32(res.UID), nil
}

// removeDraft deletes an old draft for good (it was replaced).
func removeDraft(c *imapclient.Client, s *session, uid uint32) {
	drafts, err := folderOf(c, RoleDrafts, "Drafts", false)
	if err != nil {
		return
	}
	if _, err := selectFolder(c, s, drafts); err != nil {
		return
	}
	if err := expunge(c, []uint32{uid}); err != nil {
		slog.Warn("T-Mail: old draft not removed", "err", safeText(err.Error()))
	}
}
