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

// Flags are the flags the web UI shows and changes.
type Flags struct {
	Seen     bool `json:"seen"`
	Flagged  bool `json:"flagged"`
	Answered bool `json:"answered"`
}

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
}

// Page is a page of a list, newest first; Next is the UID to ask for the
// next page with (before), 0 at the end.
type Page struct {
	Messages []Summary `json:"messages"`
	Next     uint32    `json:"next"`
	Total    int       `json:"total"`
}

// ListQuery asks for a page of a folder.
type ListQuery struct {
	Folder  string
	Before  uint32
	Limit   int
	Q       string
	Unread  bool
	Flagged bool
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
	return Flags{Seen: slices.Contains(list, imap.FlagSeen), Flagged: slices.Contains(list, imap.FlagFlagged), Answered: slices.Contains(list, imap.FlagAnswered)}
}

func summaryOf(b *imapclient.FetchMessageBuffer) Summary {
	s := Summary{UID: uint32(b.UID), Flags: flagsOf(b.Flags), Size: b.RFC822Size, To: []Address{}}
	if b.BodyStructure != nil {
		s.Attachments = hasAttachments(b.BodyStructure)
	}
	for _, section := range b.BodySection {
		s.Attachments = s.Attachments || attachmentsByHead(section.Bytes)
	}
	if e := b.Envelope; e != nil {
		s.Subject, s.Date, s.To = clean(e.Subject, 1000), e.Date, addresses(e.To)
		if from := addresses(e.From); len(from) > 0 {
			s.From = from[0]
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

// listFetch asks for what a line of the list shows; the attachment mark
// comes from the Content-Type of the head, not from the structure, so that
// a mail with thousands of parts cannot make a page too large.
func listFetch() *imap.FetchOptions {
	return &imap.FetchOptions{UID: true, Flags: true, Envelope: true, RFC822Size: true, InternalDate: true,
		BodySection: []*imap.FetchItemBodySection{{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"Content-Type"}, Peek: true,
			Partial: &imap.SectionPartial{Size: 8 << 10}}}}
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
	if len(q.Q) > 256 || strings.ContainsAny(q.Q, "\r\n\x00") {
		return Page{}, ErrInvalid
	}
	page := Page{Messages: []Summary{}}
	var pick []uint32
	err := m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, q.Folder); err != nil {
			return err
		}
		data, err := c.UIDSearch(searchCriteria(q), nil).Wait()
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
		// One by one: only the mails too large to list are marked.
		page.Messages = []Summary{}
		for _, uid := range pick {
			var one Summary
			err = m.withIMAP(ctx, user, id, commandTimeout, func(c *imapclient.Client, s *session) error {
				if _, err := selectFolder(c, s, q.Folder); err != nil {
					return err
				}
				s.budget.set(budgetCommand / 4)
				b, err := fetchOne(c, uid, listFetch())
				if err == nil {
					one = summaryOf(b)
				}
				return err
			})
			switch {
			case errors.Is(err, ErrTooLarge):
				one = Summary{UID: uid, To: []Address{}, TooLarge: true}
			case errors.Is(err, ErrNotFound):
				continue
			case err != nil:
				return page, err
			}
			page.Messages = append(page.Messages, one)
		}
		err = nil
	}
	slices.SortFunc(page.Messages, func(a, b Summary) int { return int(int64(b.UID) - int64(a.UID)) })
	return page, err
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
			for i, p := range parts {
				if sectionKey(p.path) != k {
					continue
				}
				raise(b, section.Literal, limits[i])
				got, err := io.ReadAll(io.LimitReader(section.Literal, limits[i]+1))
				b.set(budgetCommand)
				if err != nil {
					_ = c.Close() // the rest of the answer is not read
					return nil, nil, err
				}
				if int64(len(got)) > limits[i] {
					// More than asked for: the rest is not read.
					_ = c.Close()
					got = got[:limits[i]]
					data[k], cut[k] = got, true
					return data, cut, nil
				}
				data[k] = got
				cut[k] = p.size > limits[i]
			}
		}
	}
	return data, cut, cmd.Close()
}

// Read returns a mail to show and marks it as read. images lets pictures
// from the internet load in its HTML.
func (m *Manager) Read(ctx context.Context, user, id, folder string, uid uint32, images bool) (*Message, error) {
	var out *Message
	err := m.withIMAP(ctx, user, id, fetchTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		b, err := fetchOne(c, uid, &imap.FetchOptions{Flags: true, Envelope: true, RFC822Size: true, InternalDate: true,
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
func (m *Manager) Part(ctx context.Context, user, id, folder string, uid uint32, part string, head func(PartInfo) io.Writer) error {
	path, ok := parsePart(part)
	if !ok {
		return ErrInvalid
	}
	return m.withIMAP(ctx, user, id, partTimeout, func(c *imapclient.Client, s *session) error {
		info, err := partOf(c, s, folder, uid, part)
		if err != nil {
			return err
		}
		w := head(PartInfo{Name: info.attachment().Name, Type: info.mediaType})
		return streamSection(c, s.budget, uid, path, info.encoding, w, -1)
	})
}

// partTimeout bounds a download of an attachment: it goes at the pace of
// the browser.
const partTimeout = 10 * time.Minute

// partOf selects the folder and finds a part in the structure of a mail.
func partOf(c *imapclient.Client, s *session, folder string, uid uint32, part string) (partInfo, error) {
	if _, err := selectFolder(c, s, folder); err != nil {
		return partInfo{}, err
	}
	b, err := fetchOne(c, uid, &imap.FetchOptions{BodyStructure: &imap.FetchItemBodyStructure{Extended: true}})
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
func streamSection(c *imapclient.Client, b *budgetConn, uid uint32, path []int, encoding string, w io.Writer, limit int64) error {
	cmd := c.Fetch(uidSet(uid), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{{Part: path, Peek: true}}})
	found := false
	var copyErr error
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		for item := msg.Next(); item != nil; item = msg.Next() {
			section, ok := item.(imapclient.FetchItemDataBodySection)
			if !ok || section.Literal == nil || found {
				continue
			}
			found = true
			raise(b, section.Literal, limit)
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
			b.set(budgetCommand)
			if copyErr != nil {
				// The rest of the literal is not read: the connection goes.
				_ = c.Close()
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
	return m.withIMAP(ctx, user, id, fetchTimeout, func(c *imapclient.Client, s *session) error {
		if _, err := selectFolder(c, s, folder); err != nil {
			return err
		}
		b, err := fetchOne(c, uid, &imap.FetchOptions{BodyStructure: &imap.FetchItemBodyStructure{Extended: true}})
		if err != nil {
			return err
		}
		for _, a := range walkStructure(b.BodyStructure).attachments {
			if a.id == cid && slices.Contains(inlineTypes, a.mediaType) {
				w := head(PartInfo{Name: a.attachment().Name, Type: a.mediaType})
				return streamSection(c, s.budget, uid, a.path, a.encoding, w, maxInlineImage)
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
		if expected := info.attachment().Size; expected > limit {
			return ErrTooLarge
		} else if expected > 0 {
			buf.Grow(int(expected) + 4096)
		}
		return streamSection(c, s.budget, uid, path, info.encoding, &buf, limit)
	})
	if err != nil {
		return nil, Attachment{}, err
	}
	return buf.Bytes(), info.attachment(), nil
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
