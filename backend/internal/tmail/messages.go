package tmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Address is a person of a mail.
type Address struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// Flags are the flags the web UI shows and changes. Forwarded is the
// keyword $Forwarded (1.12); the web UI only shows it.
type Flags struct {
	Seen      bool `json:"seen"`
	Flagged   bool `json:"flagged"`
	Answered  bool `json:"answered"`
	Forwarded bool `json:"forwarded"`
}

// flagForwarded is the keyword other programs set on a forwarded mail.
const flagForwarded = imap.Flag("$Forwarded")

// Summary is one mail of a list.
type Summary struct {
	UID         uint32    `json:"uid"`
	From        Address   `json:"from"`
	To          []Address `json:"to"`
	Subject     string    `json:"subject"`
	Date        time.Time `json:"date"`
	Flags       Flags     `json:"flags"`
	Size        int64     `json:"size"`
	Attachments bool      `json:"attachments"`
	// TooLarge: the head of this mail was too large to list.
	TooLarge bool `json:"tooLarge,omitempty"`
	// Preview is the beginning of its text (preview.go, 1.12).
	Preview string `json:"preview"`
	// MessageID, InReplyTo and References (at most listReferences ids)
	// let the web UI put a conversation together (1.12).
	MessageID  string `json:"messageId,omitempty"`
	InReplyTo  string `json:"inReplyTo,omitempty"`
	References string `json:"references,omitempty"`
}

// Page is a page of a list, newest first; Next is the UID to ask for the
// next page with (before), 0 at the end.
type Page struct {
	Messages []Summary `json:"messages"`
	Next     uint32    `json:"next"`
	Total    int       `json:"total"`
}

// ListQuery asks for a page of a folder. From, Attachment, Since and
// Until are the filters of 1.12; Since and Until are days (both counted),
// zero for none.
type ListQuery struct {
	Folder     string
	Before     uint32
	Limit      int
	Q          string
	Unread     bool
	Flagged    bool
	From       string
	Attachment bool
	Since      time.Time
	Until      time.Time
}

// validFilter checks the texts of a query.
func (q ListQuery) validFilter() bool {
	for _, text := range []string{q.Q, q.From} {
		if len(text) > 256 || strings.ContainsAny(text, "\r\n\x00") {
			return false
		}
	}
	return q.Since.IsZero() || q.Until.IsZero() || !q.Until.Before(q.Since)
}

// Message is one mail to read.
type Message struct {
	UID    uint32    `json:"uid"`
	Folder string    `json:"folder"`
	From   Address   `json:"from"`
	To     []Address `json:"to"`
	Cc     []Address `json:"cc"`
	// Bcc is only known of own mail (a draft keeps it).
	Bcc            []Address    `json:"bcc"`
	ReplyTo        []Address    `json:"replyTo"`
	Subject        string       `json:"subject"`
	Date           time.Time    `json:"date"`
	MessageID      string       `json:"messageId"`
	InReplyTo      string       `json:"inReplyTo"`
	References     string       `json:"references"`
	Flags          Flags        `json:"flags"`
	Text           string       `json:"text"`
	HTML           string       `json:"html"`
	ExternalImages bool         `json:"externalImages"`
	Attachments    []Attachment `json:"attachments"`
	Truncated      bool         `json:"truncated"`
	// RawText: the structure of the mail was too large to read (a digest
	// of hundreds of mails); Text is the beginning of its body as it
	// came, without decoding, HTML or attachments.
	RawText bool `json:"rawText,omitempty"`
}

func addresses(list []imap.Address) []Address {
	out := []Address{}
	for _, a := range list {
		if a.IsGroupStart() || a.IsGroupEnd() {
			continue
		}
		out = append(out, Address{Name: clean(a.Name, 256), Addr: clean(a.Addr(), 320)})
	}
	return out
}

// clean keeps a header text to one valid line of at most limit runes.
func clean(s string, limit int) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		if r < 0x20 || r == 0x7f || invisible(r) {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > limit {
		s = string([]rune(s)[:limit])
	}
	return strings.TrimSpace(s)
}

// invisible tells format characters (Unicode Cf): zero widths and the
// controls of the writing direction (U+202A–202E, U+2066–2069) that make
// "rechnung\u202efdp.exe" look like a PDF (security review of 1.05).
func invisible(r rune) bool { return unicode.Is(unicode.Cf, r) }

func flagsOf(list []imap.Flag) Flags {
	return Flags{Seen: slices.Contains(list, imap.FlagSeen), Flagged: slices.Contains(list, imap.FlagFlagged), Answered: slices.Contains(list, imap.FlagAnswered),
		Forwarded: slices.ContainsFunc(list, func(f imap.Flag) bool { return strings.EqualFold(string(f), string(flagForwarded)) })}
}

// listReferences bounds the References of a line of the list: the first
// id (the start of the conversation) and the last ones (the nearest).
const listReferences = 20

func summaryOf(b *imapclient.FetchMessageBuffer) Summary {
	s := Summary{UID: uint32(b.UID), Flags: flagsOf(b.Flags), Size: b.RFC822Size, To: []Address{}}
	if b.BodyStructure != nil {
		s.Attachments = hasAttachments(b.BodyStructure)
	}
	head := headFields(findSection(b, listTypeSection), false)
	if ct, ok := head["content-type"]; ok {
		s.Attachments = s.Attachments || attachmentsByHead([]byte("Content-Type: "+ct))
	}
	if text := findSection(b, listTextSection); text != nil {
		s.Preview = previewOf(head["content-type"], head["content-transfer-encoding"], text)
	}
	if raw := findSection(b, listReferencesSection); raw != nil {
		ids := strings.Fields(references(string(raw)))
		if len(ids) > listReferences {
			ids = append(ids[:1], ids[len(ids)-listReferences+1:]...)
		}
		s.References = strings.Join(ids, " ")
	}
	if e := b.Envelope; e != nil {
		s.Subject, s.Date, s.To = clean(e.Subject, 1000), e.Date, addresses(e.To)
		if from := addresses(e.From); len(from) > 0 {
			s.From = from[0]
		}
		s.MessageID = clean(angle(e.MessageID), 998)
		if len(e.InReplyTo) > 0 {
			s.InReplyTo = clean(angle(e.InReplyTo[0]), 998)
		}
	}
	if s.Date.IsZero() {
		s.Date = b.InternalDate
	}
	return s
}

func uidSet(uids ...uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, u := range uids {
		set.AddNum(imap.UID(u))
	}
	return set
}

// searchCriteria makes the IMAP SEARCH of a query: the words of q in
// subject, sender, recipient or text.
func searchCriteria(q ListQuery) *imap.SearchCriteria {
	c := &imap.SearchCriteria{}
	if q.Unread {
		c.NotFlag = append(c.NotFlag, imap.FlagSeen)
	}
	if q.Flagged {
		c.Flag = append(c.Flag, imap.FlagFlagged)
	}
	c.NotFlag = append(c.NotFlag, imap.FlagDeleted)
	if from := strings.TrimSpace(q.From); from != "" {
		c.Header = append(c.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: from})
	}
	if q.Attachment {
		// IMAP has no search for attachments: the type of the head is the
		// approximation (docs/TMAIL.md "1.12").
		c.Header = append(c.Header, imap.SearchCriteriaHeaderField{Key: "Content-Type", Value: "multipart/mixed"})
	}
	if !q.Since.IsZero() {
		c.Since = q.Since
	}
	if !q.Until.IsZero() {
		c.Before = q.Until.AddDate(0, 0, 1)
	}
	if text := strings.TrimSpace(q.Q); text != "" {
		subject := imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: text}}}
		from := imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "From", Value: text}}}
		to := imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "To", Value: text}}}
		body := imap.SearchCriteria{Body: []string{text}}
		c.Or = append(c.Or, [2]imap.SearchCriteria{
			{Or: [][2]imap.SearchCriteria{{subject, from}}},
			{Or: [][2]imap.SearchCriteria{{to, body}}},
		})
	}
	return c
}

// The sections a line of the list fetches besides the envelope: the
// type of the mail (attachment mark and preview), its References (the
// conversation) and the beginning of its body (the preview, 1.12). All
// with PEEK: listing marks nothing read.
var (
	listTypeSection = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader,
		HeaderFields: []string{"Content-Type", "Content-Transfer-Encoding"}, Peek: true, Partial: &imap.SectionPartial{Size: 8 << 10}}
	listReferencesSection = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader,
		HeaderFields: []string{"References"}, Peek: true, Partial: &imap.SectionPartial{Size: 4 << 10}}
	listTextSection = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierText, Peek: true,
		Partial: &imap.SectionPartial{Size: previewFetch}}
)

// leanFetch is listFetch without the preview and References: the line of
// a page that was too large with them (fallback).
func leanFetch() *imap.FetchOptions {
	return &imap.FetchOptions{UID: true, Flags: true, Envelope: true, RFC822Size: true, InternalDate: true,
		BodySection: []*imap.FetchItemBodySection{fresh(listTypeSection)}}
}

// fresh is a copy of a section of the list for one request: the shared
// ones are only patterns (and what findSection compares with).
func fresh(s *imap.FetchItemBodySection) *imap.FetchItemBodySection {
	c := *s
	c.HeaderFields = slices.Clone(s.HeaderFields)
	if s.Partial != nil {
		p := *s.Partial
		c.Partial = &p
	}
	return &c
}

// findSection is the data of a fetched section like want, whether or not
// the server echoed its partial; nil if it is missing.
func findSection(b *imapclient.FetchMessageBuffer, want *imap.FetchItemBodySection) []byte {
	for _, got := range b.BodySection {
		if got.Section == nil || got.Section.Specifier != want.Specifier || len(got.Section.Part) > 0 ||
			!slices.EqualFunc(got.Section.HeaderFields, want.HeaderFields, strings.EqualFold) {
			continue
		}
		return got.Bytes
	}
	return nil
}

// listFetch asks for what a line of the list shows; the attachment mark
// comes from the Content-Type of the head, not from the structure, so that
// a mail with thousands of parts cannot make a page too large.
func listFetch() *imap.FetchOptions {
	return &imap.FetchOptions{UID: true, Flags: true, Envelope: true, RFC822Size: true, InternalDate: true,
		BodySection: []*imap.FetchItemBodySection{fresh(listTypeSection), fresh(listReferencesSection), fresh(listTextSection)}}
}

// attachmentsByHead tells from the Content-Type of a mail whether it has
// attachments: multipart/mixed, or a single part that is no text.
func attachmentsByHead(header []byte) bool {
	_, value, ok := strings.Cut(string(header), ":")
	if !ok {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", ""), "\n", "")))
	if err != nil {
		return false
	}
	return mediaType == "multipart/mixed" || !strings.HasPrefix(mediaType, "multipart/") && !strings.HasPrefix(mediaType, "text/")
}

// List returns a page of a folder, newest first. A mail whose head is too
// large to list is shown with its UID and tooLarge, not left out.
func (m *Manager) List(ctx context.Context, user, id string, q ListQuery) (Page, error) {
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 50
	}
	if !q.validFilter() {
		return Page{}, ErrInvalid
	}
	page := Page{Messages: []Summary{}}
	var pick []uint32
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, q.Folder); err != nil {
			return err
		}
		data, err := uidSearch(c, searchCriteria(q), true)
		if err != nil {
			return err
		}
		page.Total = uidCount(data.All)
		pick = uidsDesc(data.All, q.Before, q.Limit+1)
		if len(pick) > q.Limit {
			pick = pick[:q.Limit]
			page.Next = pick[len(pick)-1]
		}
		if len(pick) == 0 {
			return nil
		}
		msgs, err := collect(c.Fetch(uidSet(pick...), listFetch()))
		if err != nil {
			return err
		}
		for _, b := range msgs {
			page.Messages = append(page.Messages, summaryOf(b))
		}
		return nil
	})
	if errors.Is(err, ErrTooLarge) && len(pick) > 0 {
		page.Messages, err = m.fallback(ctx, user, id, q.Folder, pick)
	}
	slices.SortFunc(page.Messages, func(a, b Summary) int { return int(int64(b.UID) - int64(a.UID)) })
	return page, err
}

// summaries fetches the lines of some mails of a folder (search over all
// folders); a page too large for the budget goes the way of fallback.
func (m *Manager) summaries(ctx context.Context, user, id, folder string, uids []uint32) ([]Summary, error) {
	out, err := m.fetchLines(ctx, user, id, folder, uids, listFetch)
	if errors.Is(err, ErrTooLarge) {
		return m.fallback(ctx, user, id, folder, uids)
	}
	return out, err
}

// fetchLines fetches the lines of some mails in one FETCH.
func (m *Manager) fetchLines(ctx context.Context, user, id, folder string, uids []uint32, opts func() *imap.FetchOptions) ([]Summary, error) {
	var out []Summary
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		msgs, err := collect(c.Fetch(uidSet(uids...), opts()))
		if err != nil {
			return err
		}
		for _, b := range msgs {
			out = append(out, summaryOf(b))
		}
		return nil
	})
	return out, err
}

// fallback is the way of a page too large for the budget of a command:
// the page again without preview and References (a server that ignores
// the cut of BODY[TEXT] sends whole bodies – the preview is optional,
// sender and subject are not), then mail by mail.
func (m *Manager) fallback(ctx context.Context, user, id, folder string, uids []uint32) ([]Summary, error) {
	out, err := m.fetchLines(ctx, user, id, folder, uids, leanFetch)
	if errors.Is(err, ErrTooLarge) {
		return m.oneByOne(ctx, user, id, folder, uids)
	}
	return out, err
}

// oneByOne fetches lines one mail at a time, each with its preview and
// else without: only the mails whose heads alone are too large to list
// are marked (tooLarge), the others are whole.
func (m *Manager) oneByOne(ctx context.Context, user, id, folder string, uids []uint32) ([]Summary, error) {
	out := []Summary{}
	for _, uid := range uids {
		var one Summary
		var err error
		for _, opts := range []func() *imap.FetchOptions{listFetch, leanFetch} {
			err = m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
				if _, err := selectFolder(c, s, folder); err != nil {
					return err
				}
				s.budget.set(budgetCommand / 4)
				b, err := fetchOne(c, uid, opts())
				if err == nil {
					one = summaryOf(b)
				}
				return err
			})
			if !errors.Is(err, ErrTooLarge) {
				break
			}
		}
		switch {
		case errors.Is(err, ErrTooLarge):
			one = Summary{UID: uid, To: []Address{}, TooLarge: true}
		case errors.Is(err, ErrNotFound):
			continue
		case err != nil:
			return out, err
		}
		out = append(out, one)
	}
	return out, nil
}

// fetchStructure is fetchOne for an answer with BODYSTRUCTURE: decoding a
// structure costs many times its size, so its answer has a budget of its
// own (budgetStructure; a mail of 200 parts needs a few KiB).
func fetchStructure(c *imapclient.Client, b *budgetConn, uid uint32, opts *imap.FetchOptions) (*imapclient.FetchMessageBuffer, error) {
	b.set(budgetStructure)
	defer b.set(budgetCommand)
	return fetchOne(c, uid, opts)
}

// fetchOne fetches the head data of one mail; ErrNotFound without it.
func fetchOne(c *imapclient.Client, uid uint32, opts *imap.FetchOptions) (*imapclient.FetchMessageBuffer, error) {
	opts.UID = true
	msgs, err := collect(c.Fetch(uidSet(uid), opts))
	if err != nil {
		return nil, err
	}
	for _, b := range msgs {
		if uint32(b.UID) == uid {
			return b, nil
		}
	}
	return nil, fmt.Errorf("%w: mail", ErrNotFound)
}

// fetchSections fetches parts of a mail, each at most its limit, and
// reports which were cut.
func fetchSections(c *imapclient.Client, b *budgetConn, uid uint32, parts []partInfo, limits []int64) (map[string][]byte, map[string]bool, error) {
	if len(parts) == 0 {
		return map[string][]byte{}, map[string]bool{}, nil
	}
	opts := &imap.FetchOptions{UID: true}
	for i, p := range parts {
		opts.BodySection = append(opts.BodySection, &imap.FetchItemBodySection{Part: p.path, Peek: true,
			Partial: &imap.SectionPartial{Offset: 0, Size: limits[i]}})
	}
	data := map[string][]byte{}
	cut := map[string]bool{}
	cmd := c.Fetch(uidSet(uid), opts)
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		for item := msg.Next(); item != nil; item = msg.Next() {
			section, ok := item.(imapclient.FetchItemDataBodySection)
			if !ok || section.Literal == nil {
				continue
			}
			k := sectionKey(section.Section.Part)
			at := slices.IndexFunc(parts, func(p partInfo) bool { return sectionKey(p.path) == k })
			if at < 0 || data[k] != nil {
				// Not asked for: read away within the budget of a command
				// (go-imap waits until a literal is read).
				if _, err := io.Copy(io.Discard, section.Literal); err != nil {
					abandon(c, drainFetch(cmd, msg))
					return nil, nil, err
				}
				continue
			}
			raise(b, section.Literal, limits[at])
			got, err := io.ReadAll(io.LimitReader(section.Literal, limits[at]+1))
			b.set(budgetCommand)
			if err != nil {
				// The rest of the answer is not read.
				abandon(c, drainFetch(cmd, msg))
				return nil, nil, err
			}
			if int64(len(got)) > limits[at] {
				// More than asked for: the rest is not read.
				abandon(c, drainFetch(cmd, msg, section.Literal))
				got = got[:limits[at]]
				data[k], cut[k] = got, true
				return data, cut, nil
			}
			data[k] = got
			cut[k] = parts[at].size > limits[at]
		}
	}
	return data, cut, cmd.Close()
}

// Read returns a mail to show and marks it as read. images lets pictures
// from the internet load in its HTML. A mail whose structure is too large
// to read is shown as the beginning of its raw body (readRaw).
func (m *Manager) Read(ctx context.Context, user, id, folder string, uid uint32, images bool) (*Message, error) {
	msg, err := m.read(ctx, user, id, folder, uid, images)
	if errors.Is(err, ErrTooLarge) {
		return m.readRaw(ctx, user, id, folder, uid)
	}
	return msg, err
}

// rawTextLimit is how much of the body readRaw shows.
const rawTextLimit = 1 << 20

// readRaw shows a mail without its structure: a few header fields and the
// beginning of its body as it came (third review of 1.05: digests with
// hundreds of attached mails could not be opened at all).
func (m *Manager) readRaw(ctx context.Context, user, id, folder string, uid uint32) (*Message, error) {
	var out *Message
	err := m.withIMAP(ctx, user, id, fetchTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		sections := headSections(headSender, headRecipients, headReply, headReferences)
		sections = append(sections, &imap.FetchItemBodySection{Specifier: imap.PartSpecifierText, Peek: true,
			Partial: &imap.SectionPartial{Size: rawTextLimit}})
		b, err := fetchOne(c, uid, &imap.FetchOptions{Flags: true, InternalDate: true, BodySection: sections})
		if err != nil {
			return err
		}
		e := headEnvelope(b)
		msg := &Message{UID: uid, Folder: folder, Flags: flagsOf(b.Flags), Subject: clean(e.Subject, 1000), Date: e.Date,
			To: addresses(e.To), Cc: addresses(e.Cc), Bcc: []Address{}, ReplyTo: addresses(e.ReplyTo),
			Attachments: []Attachment{}, Truncated: true, RawText: true}
		if from := addresses(e.From); len(from) > 0 {
			msg.From = from[0]
		}
		if e.MessageID != "" {
			msg.MessageID = clean(angle(e.MessageID), 998)
		}
		if len(e.InReplyTo) > 0 {
			msg.InReplyTo = clean(angle(e.InReplyTo[0]), 998)
		}
		if msg.Date.IsZero() {
			msg.Date = b.InternalDate
		}
		fields := headFieldsOf(b)
		msg.References = references("References: " + fields["references"])
		for _, section := range b.BodySection {
			if section.Section != nil && section.Section.Specifier == imap.PartSpecifierText {
				msg.Text = rawText(section.Bytes)
			}
		}
		if !msg.Flags.Seen {
			if err := c.Store(uidSet(uid), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err == nil {
				msg.Flags.Seen = true
			}
		}
		out = msg
		return nil
	})
	return out, err
}

// rawText makes a body as it came showable: valid UTF-8, lines ending in
// "\n", no control or invisible characters but tabs.
func rawText(raw []byte) string {
	text := strings.ToValidUTF8(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\uFFFD")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f || invisible(r) {
			return -1
		}
		return r
	}, text)
}

func (m *Manager) read(ctx context.Context, user, id, folder string, uid uint32, images bool) (*Message, error) {
	var out *Message
	err := m.withIMAP(ctx, user, id, fetchTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		b, err := fetchStructure(c, s.budget, uid, &imap.FetchOptions{Flags: true, Envelope: true, RFC822Size: true, InternalDate: true,
			BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
			BodySection: []*imap.FetchItemBodySection{{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"References"}, Peek: true,
				Partial: &imap.SectionPartial{Size: MaxHeader}}}})
		if err != nil {
			return err
		}
		msg := &Message{UID: uid, Folder: folder, Flags: flagsOf(b.Flags), To: []Address{}, Cc: []Address{}, Bcc: []Address{}, ReplyTo: []Address{}}
		if e := b.Envelope; e != nil {
			msg.Subject, msg.Date = clean(e.Subject, 1000), e.Date
			msg.To, msg.Cc, msg.Bcc, msg.ReplyTo = addresses(e.To), addresses(e.Cc), addresses(e.Bcc), addresses(e.ReplyTo)
			if from := addresses(e.From); len(from) > 0 {
				msg.From = from[0]
			}
			msg.MessageID = clean(angle(e.MessageID), 998)
			if len(e.InReplyTo) > 0 {
				msg.InReplyTo = clean(angle(e.InReplyTo[0]), 998)
			}
		}
		if msg.Date.IsZero() {
			msg.Date = b.InternalDate
		}
		for _, section := range b.BodySection {
			msg.References = references(string(section.Bytes))
		}
		p := walkStructure(b.BodyStructure)
		msg.Attachments = p.attachmentList()
		msg.Truncated = p.truncated
		var parts []partInfo
		var limits []int64
		for _, t := range []*partInfo{p.text, p.html} {
			if t != nil {
				parts = append(parts, *t)
				limits = append(limits, encodedLimit(*t, MaxText))
			}
		}
		// The mail's own pictures, within their limits; none of a mail
		// above MaxDisplay.
		var total int64
		if p.html != nil && b.RFC822Size <= MaxDisplay {
			for _, a := range p.attachments {
				view := a.attachment()
				if !view.Inline || a.size > encodedLimit(a, maxInlineImage) || total+a.size > maxInlineTotal*4/3 {
					continue
				}
				total += a.size
				parts = append(parts, a)
				limits = append(limits, encodedLimit(a, maxInlineImage))
			}
		}
		data, cut, err := fetchSections(c, s.budget, uid, parts, limits)
		if err != nil {
			return err
		}
		if p.text != nil {
			k := sectionKey(p.text.path)
			text, truncated := decodeText(*p.text, data[k], cut[k], MaxText)
			msg.Text, msg.Truncated = text, msg.Truncated || truncated
		}
		if p.html != nil {
			k := sectionKey(p.html.path)
			raw, truncated := decodeText(*p.html, data[k], cut[k], MaxText)
			msg.Truncated = msg.Truncated || truncated
			cids := map[string]string{}
			for _, a := range parts {
				k := sectionKey(a.path)
				if a.id == "" || !slices.Contains(inlineTypes, a.mediaType) || cut[k] || data[k] == nil {
					continue
				}
				raw, err := io.ReadAll(io.LimitReader(decoder(a.encoding, bytes.NewReader(data[k])), maxInlineImage+1))
				if err != nil || len(raw) > maxInlineImage || !sniffImage(raw, a.mediaType) {
					continue
				}
				cids[a.id] = "data:" + a.mediaType + ";base64," + base64.StdEncoding.EncodeToString(raw)
			}
			msg.HTML, msg.ExternalImages = SanitizeHTML(raw, cids, images)
		}
		if !msg.Flags.Seen {
			if err := c.Store(uidSet(uid), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err == nil {
				msg.Flags.Seen = true
			}
		}
		out = msg
		return nil
	})
	return out, err
}

// sniffImage checks that data is the picture its type claims.
func sniffImage(data []byte, mediaType string) bool {
	switch mediaType {
	case "image/png":
		return len(data) > 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n"
	case "image/jpeg":
		return len(data) > 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
	case "image/gif":
		return len(data) > 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a")
	case "image/webp":
		return len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	}
	return false
}

func angle(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.HasPrefix(id, "<") {
		return id
	}
	return "<" + id + ">"
}

// references takes the message IDs out of a References header.
func references(header string) string {
	_, value, ok := strings.Cut(header, ":")
	if !ok {
		return ""
	}
	var ids []string
	for _, field := range strings.Fields(value) {
		if strings.HasPrefix(field, "<") && strings.HasSuffix(field, ">") && len(field) <= 998 && len(ids) < 100 {
			ids = append(ids, clean(field, 998))
		}
	}
	return strings.Join(ids, " ")
}

// PartInfo describes a part to hand out.
type PartInfo struct {
	Name string
	Type string
}

// Part streams one part of a mail, decoded, to w after head was told its
// name and type. It is the same for attachments and embedded pictures.
//
// It takes a turn of the downloads (DownloadsPerAccount) and a command's
// turn only until the part streams (third review of 1.05).
func (m *Manager) Part(ctx context.Context, user, id, folder string, uid uint32, part string, head func(PartInfo) io.Writer) error {
	path, ok := parsePart(part)
	if !ok {
		return ErrInvalid
	}
	done, err := m.download(ctx, user)
	if err != nil {
		return err
	}
	defer done()
	return m.withIMAP(ctx, user, id, PartTimeout, func(c *imapclient.Client, s *session) error {
		// Until the literal begins, a download is an ordinary command: it
		// gets commandTimeout, not the browser's PartTimeout, so that a
		// server that stalls before the literal does not hold a turn and
		// its reservation for ten minutes (review of 1.06, B).
		var early atomic.Bool
		before := time.AfterFunc(partHeadTimeout, func() {
			early.Store(true)
			s.kill()
		})
		defer before.Stop()
		started := func() {
			before.Stop()
			s.streaming()
		}
		info, err := partOf(c, s, folder, uid, part)
		if err == nil {
			w := head(PartInfo{Name: info.attachment().Name, Type: info.mediaType})
			err = streamSection(c, s.budget, uid, path, info.encoding, w, -1, started)
		}
		if err != nil && early.Load() {
			return ErrTimeout
		}
		return err
	})
}

// partHeadTimeout bounds a download until its literal begins (a variable
// for the tests).
var partHeadTimeout = commandTimeout

// PartTimeout bounds a download of an attachment: it goes at the pace of
// the browser. The API bounds its writes to the browser by it as well, so
// that a browser that stops reading lets go of the connection.
const PartTimeout = 10 * time.Minute

// PictureTimeout bounds a download of an embedded picture (CID).
const PictureTimeout = fetchTimeout

// partOf selects the folder and finds a part in the structure of a mail.
func partOf(c *imapclient.Client, s *session, folder string, uid uint32, part string) (partInfo, error) {
	if _, err := selectFolder(c, s, folder); err != nil {
		return partInfo{}, err
	}
	b, err := fetchStructure(c, s.budget, uid, &imap.FetchOptions{BodyStructure: &imap.FetchItemBodyStructure{Extended: true}})
	if err != nil {
		return partInfo{}, err
	}
	info, ok := findPart(b.BodyStructure, part)
	if !ok {
		return partInfo{}, fmt.Errorf("%w: part", ErrNotFound)
	}
	return info, nil
}

// raise lets the literal of a part through the budget once it arrives:
// its announced size (at most cap encoded bytes for a bounded part) plus
// the margin. The literal is streamed, never kept whole; the caller sets
// the budget back to budgetCommand right after it (security review of
// 1.05: a size the server claims in the structure is never a budget).
func raise(b *budgetConn, lit imap.LiteralReader, decoded int64) {
	size := lit.Size()
	if decoded >= 0 {
		size = min(size, 3*decoded+4096)
	}
	b.set(size + budgetMargin)
}

// streamSection copies a decoded part to w; limit >= 0 refuses more.
// started, if set, is called once the part's literal begins.
func streamSection(c *imapclient.Client, b *budgetConn, uid uint32, path []int, encoding string, w io.Writer, limit int64, started func()) error {
	cmd := c.Fetch(uidSet(uid), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{{Part: path, Peek: true}}})
	found := false
	var copyErr error
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		for item := msg.Next(); item != nil; item = msg.Next() {
			section, ok := item.(imapclient.FetchItemDataBodySection)
			if !ok || section.Literal == nil {
				continue
			}
			if found {
				// A second literal nobody asked for: read away within the
				// budget of a command (go-imap waits until it is read).
				if _, err := io.Copy(io.Discard, section.Literal); err != nil {
					abandon(c, drainFetch(cmd, msg))
					return err
				}
				continue
			}
			found = true
			raise(b, section.Literal, limit)
			if started != nil {
				started()
			}
			r := decoder(encoding, section.Literal)
			if limit >= 0 {
				var n int64
				n, copyErr = io.Copy(w, io.LimitReader(r, limit+1))
				if copyErr == nil && n > limit {
					copyErr = ErrTooLarge
				}
			} else {
				_, copyErr = io.Copy(w, r)
			}
			if started != nil {
				// A download has given its turn and reservation back: what
				// may still follow is the end of the response, so only a
				// small budget (review of 1.06, A).
				b.set(budgetTail)
			} else {
				b.set(budgetCommand)
			}
			if copyErr != nil {
				// The rest of the literal is not read (the browser went
				// away, or the part is larger than allowed): the
				// connection goes.
				abandon(c, drainFetch(cmd, msg, section.Literal))
				return copyErr
			}
		}
	}
	if err := cmd.Close(); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: part", ErrNotFound)
	}
	return nil
}

// CID streams an embedded picture by its Content-ID: only the allowed
// picture types, everything else is not found here.
func (m *Manager) CID(ctx context.Context, user, id, folder string, uid uint32, cid string, head func(PartInfo) io.Writer) error {
	cid = strings.Trim(strings.TrimSpace(cid), "<>")
	if cid == "" || len(cid) > 998 {
		return ErrInvalid
	}
	done, err := m.download(ctx, user)
	if err != nil {
		return err
	}
	defer done()
	return m.withIMAP(ctx, user, id, PictureTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		b, err := fetchStructure(c, s.budget, uid, &imap.FetchOptions{BodyStructure: &imap.FetchItemBodyStructure{Extended: true}})
		if err != nil {
			return err
		}
		for _, a := range walkStructure(b.BodyStructure).attachments {
			if a.id == cid && slices.Contains(inlineTypes, a.mediaType) {
				w := head(PartInfo{Name: a.attachment().Name, Type: a.mediaType})
				return streamSection(c, s.budget, uid, a.path, a.encoding, w, maxInlineImage, s.streaming)
			}
		}
		return fmt.Errorf("%w: picture", ErrNotFound)
	})
}

// PartBytes returns a decoded part of at most limit bytes (saving to the
// NAS, forwarding).
func (m *Manager) PartBytes(ctx context.Context, user, id, folder string, uid uint32, part string, limit int64) ([]byte, Attachment, error) {
	path, ok := parsePart(part)
	if !ok {
		return nil, Attachment{}, ErrInvalid
	}
	var buf bytes.Buffer
	var info partInfo
	err := m.withIMAP(ctx, user, id, fetchTimeout, func(c *imapclient.Client, s *session) error {
		var err error
		if info, err = partOf(c, s, folder, uid, part); err != nil {
			return err
		}
		// The size of the structure is the server's word: no more than
		// a MiB is made room for in advance (security review of 1.05).
		if expected := info.attachment().Size; expected > 0 {
			buf.Grow(int(min(expected, 1<<20)))
		}
		return streamSection(c, s.budget, uid, path, info.encoding, &buf, limit, nil)
	})
	if err != nil {
		return nil, Attachment{}, err
	}
	data := buf.Bytes()
	if cap(data) > len(data)+len(data)/4+4096 {
		data = bytes.Clone(data) // what is kept holds its real length only
	}
	return data, info.attachment(), nil
}

var flagNames = map[string]imap.Flag{"seen": imap.FlagSeen, "flagged": imap.FlagFlagged, "answered": imap.FlagAnswered}

func checkUIDs(uids []uint32) error {
	if len(uids) == 0 || len(uids) > 1000 || slices.Contains(uids, 0) {
		return ErrInvalid
	}
	return nil
}

// SetFlags adds and removes flags of mails.
func (m *Manager) SetFlags(ctx context.Context, user, id, folder string, uids []uint32, add, remove []string) error {
	if err := checkUIDs(uids); err != nil {
		return err
	}
	toFlags := func(names []string) ([]imap.Flag, error) {
		var out []imap.Flag
		for _, n := range names {
			f, ok := flagNames[n]
			if !ok {
				return nil, ErrInvalid
			}
			out = append(out, f)
		}
		return out, nil
	}
	addFlags, err := toFlags(add)
	if err != nil {
		return err
	}
	removeFlags, err := toFlags(remove)
	if err != nil {
		return err
	}
	err = m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		if len(addFlags) > 0 {
			if err := c.Store(uidSet(uids...), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: addFlags}, nil).Close(); err != nil {
				return err
			}
		}
		if len(removeFlags) > 0 {
			if err := c.Store(uidSet(uids...), &imap.StoreFlags{Op: imap.StoreFlagsDel, Silent: true, Flags: removeFlags}, nil).Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		m.events.publish(user, Event{Type: "flags", Data: map[string]any{"account": id, "folder": folder, "uids": uids}})
	}
	return err
}

// Move moves mails to another folder.
func (m *Manager) Move(ctx context.Context, user, id, folder string, uids []uint32, to string) error {
	if err := checkUIDs(uids); err != nil {
		return err
	}
	if to == "" || to == folder || len(to) > 1024 || strings.ContainsAny(to, "\r\n\x00") {
		return ErrInvalid
	}
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		_, err := c.Move(uidSet(uids...), to).Wait()
		return err
	})
	if err == nil {
		m.events.publish(user, Event{Type: "flags", Data: map[string]any{"account": id, "folder": folder, "uids": uids}})
	}
	return err
}

// Delete moves mails to the trash; in the trash it deletes them for good.
// A mailbox without a trash gets one ("Trash").
func (m *Manager) Delete(ctx context.Context, user, id, folder string, uids []uint32) error {
	if err := checkUIDs(uids); err != nil {
		return err
	}
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		trash, err := folderOf(c, RoleTrash, "Trash", true)
		if err != nil {
			return err
		}
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		if folder != trash {
			_, err := c.Move(uidSet(uids...), trash).Wait()
			return err
		}
		return expunge(c, uids)
	})
	if err == nil {
		m.events.publish(user, Event{Type: "flags", Data: map[string]any{"account": id, "folder": folder, "uids": uids}})
	}
	return err
}

// expunge deletes mails of the selected folder for good: only these with
// UIDPLUS, else every mail marked as deleted there (as other programs do).
func expunge(c *imapclient.Client, uids []uint32) error {
	if err := c.Store(uidSet(uids...), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return err
	}
	if c.Caps().Has(imap.CapUIDPlus) || c.Caps().Has(imap.CapIMAP4rev2) {
		return c.UIDExpunge(uidSet(uids...)).Close()
	}
	return c.Expunge().Close()
}
