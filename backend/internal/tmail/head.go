package tmail

import (
	"mime"
	"net/mail"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

// Some heads are fetched as a few header fields, each group cut at
// headPartial, instead of ENVELOPE: a sender whose mails carry heads of
// hundreds of KiB cannot make a round of the watcher too large (third
// review of 1.05). The groups are fetched apart because a server returns
// fields in the order of the mail: a huge "To" in front would push the
// "Subject" past the cut.

// headPartial is how much of one group of header fields is fetched.
const headPartial = 16 << 10

// Groups of header fields.
var (
	// headSender is what a message and the rules need besides the
	// recipients.
	headSender = []string{"From", "Sender", "Subject", "Date"}
	// headRecipients are the recipients the rules look at.
	headRecipients = []string{"To", "Cc"}
	// headReply is what an answer needs (Read when the structure is too
	// large).
	headReply = []string{"Reply-To", "Message-ID", "In-Reply-To"}
	// headReferences is the thread of an answer, a group of its own.
	headReferences = []string{"References"}
)

// headSections asks for groups of header fields, each cut at headPartial.
func headSections(groups ...[]string) []*imap.FetchItemBodySection {
	out := make([]*imap.FetchItemBodySection, 0, len(groups))
	for _, g := range groups {
		out = append(out, &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: g, Peek: true,
			Partial: &imap.SectionPartial{Size: headPartial}})
	}
	return out
}

// headFields takes header fields out of raw (cut: raw ends inside a
// field): unfolded values by lower-case name, the first of each. A cut
// list of addresses keeps the addresses before its last comma, another
// cut field what came of it.
func headFields(raw []byte, cut bool) map[string]string {
	type field struct{ name, value string }
	var fields []field
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		switch {
		case line == "":
			continue
		case line[0] == ' ' || line[0] == '\t':
			if len(fields) > 0 {
				fields[len(fields)-1].value += " " + strings.TrimSpace(line)
			}
		default:
			name, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			fields = append(fields, field{strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(value)})
		}
	}
	if cut && len(fields) > 0 {
		last := &fields[len(fields)-1]
		switch last.name {
		case "from", "sender", "to", "cc", "reply-to":
			if i := strings.LastIndexByte(last.value, ','); i >= 0 {
				last.value = last.value[:i]
			} else {
				last.value = ""
			}
		}
	}
	out := map[string]string{}
	for _, f := range fields {
		if _, ok := out[f.name]; !ok {
			out[f.name] = f.value
		}
	}
	return out
}

var headDecoder = &mime.WordDecoder{CharsetReader: charset.Reader}

// headText decodes the encoded words of a header text (RFC 2047); one it
// cannot decode stays as it came.
func headText(s string) string {
	if decoded, err := headDecoder.DecodeHeader(s); err == nil {
		return decoded
	}
	return s
}

// headAddresses reads a list of addresses; when the whole list does not
// parse, each part between commas that does is kept.
func headAddresses(s string) []imap.Address {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parser := &mail.AddressParser{WordDecoder: headDecoder}
	list, err := parser.ParseList(s)
	if err != nil {
		list = nil
		for _, part := range strings.Split(s, ",") {
			if a, err := parser.Parse(part); err == nil {
				list = append(list, a)
			}
		}
	}
	out := make([]imap.Address, 0, len(list))
	for _, a := range list {
		local, host := a.Address, ""
		if i := strings.LastIndexByte(a.Address, '@'); i >= 0 {
			local, host = a.Address[:i], a.Address[i+1:]
		}
		out = append(out, imap.Address{Name: a.Name, Mailbox: local, Host: host})
	}
	return out
}

// headEnvelope makes the envelope of a mail from the header fields of
// headSections.
func headEnvelope(b *imapclient.FetchMessageBuffer) *imap.Envelope {
	fields := headFieldsOf(b)
	e := &imap.Envelope{Subject: headText(fields["subject"]), From: headAddresses(fields["from"]),
		Sender: headAddresses(fields["sender"]), To: headAddresses(fields["to"]), Cc: headAddresses(fields["cc"]),
		ReplyTo: headAddresses(fields["reply-to"]), MessageID: strings.Trim(fields["message-id"], "<> ")}
	if d, err := mail.ParseDate(fields["date"]); err == nil {
		e.Date = d
	}
	if ids := strings.Fields(fields["in-reply-to"]); len(ids) > 0 {
		e.InReplyTo = []string{strings.Trim(ids[0], "<>")}
	}
	return e
}

// headFieldsOf are the header fields of all header sections of a mail.
func headFieldsOf(b *imapclient.FetchMessageBuffer) map[string]string {
	fields := map[string]string{}
	for _, section := range b.BodySection {
		if section.Section == nil || section.Section.Specifier != imap.PartSpecifierHeader {
			continue
		}
		for k, v := range headFields(section.Bytes, len(section.Bytes) >= headPartial) {
			if _, ok := fields[k]; !ok {
				fields[k] = v
			}
		}
	}
	return fields
}
