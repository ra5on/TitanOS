package tmail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/quotedprintable"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message/charset"
)

// Reading mail with limits against hostile structures (docs/TMAIL.md
// "Grenzen gegen bösartige Mails"). titand never fetches a whole mail to
// show it: it asks the server for the structure (BODYSTRUCTURE) and then
// for the text parts and the embedded pictures it shows, each bounded.
// Part numbers are the IMAP section numbers of that structure, so that a
// part is fetched again by the same number (download, saving, forwarding).
const (
	MaxDepth       = 10
	MaxParts       = 200
	MaxHeader      = 64 << 10
	MaxText        = 2 << 20
	MaxDisplay     = 25 << 20
	maxInlineImage = 2 << 20
	maxInlineTotal = 8 << 20
)

// inlineTypes are the pictures shown inside a mail (cid:); every other
// type is only an attachment.
var inlineTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// Attachment is a part of a mail that is not its text.
type Attachment struct {
	Part   string `json:"part"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	Inline bool   `json:"inline"`
	CID    string `json:"cid"`
}

// partInfo is one leaf of the structure.
type partInfo struct {
	path        []int
	mediaType   string
	params      map[string]string
	encoding    string
	size        int64
	id          string
	disposition string
	filename    string
}

func (p partInfo) section() string {
	parts := make([]string, len(p.path))
	for i, n := range p.path {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// plan is what a mail consists of, as far as the limits let it be seen.
type plan struct {
	text, html  *partInfo
	attachments []partInfo
	parts       int
	truncated   bool
}

// walkStructure goes through a structure: depth at most MaxDepth, at most
// MaxParts parts; what lies beyond is left out and marks the plan as
// truncated. An attached mail (message/rfc822) is one attachment.
func walkStructure(bs imap.BodyStructure) *plan {
	p := &plan{}
	if bs != nil {
		p.visit(bs, nil, 1)
	}
	return p
}

func (p *plan) visit(bs imap.BodyStructure, path []int, depth int) {
	if depth > MaxDepth || p.parts >= MaxParts {
		p.truncated = true
		return
	}
	p.parts++
	switch b := bs.(type) {
	case *imap.BodyStructureMultiPart:
		for i, child := range b.Children {
			p.visit(child, append(slices.Clone(path), i+1), depth+1)
			if p.truncated && p.parts >= MaxParts {
				return
			}
		}
	case *imap.BodyStructureSinglePart:
		if len(path) == 0 {
			path = []int{1}
		}
		info := partInfo{path: path, mediaType: b.MediaType(), params: b.Params, encoding: strings.ToLower(b.Encoding),
			size: int64(b.Size), id: strings.Trim(strings.TrimSpace(b.ID), "<>"), filename: b.Filename()}
		if d := b.Disposition(); d != nil {
			info.disposition = strings.ToLower(d.Value)
		}
		switch {
		case info.mediaType == "text/plain" && info.disposition != "attachment" && info.filename == "" && p.text == nil:
			p.text = &info
		case info.mediaType == "text/html" && info.disposition != "attachment" && info.filename == "" && p.html == nil:
			p.html = &info
		default:
			p.attachments = append(p.attachments, info)
		}
	}
}

// attachmentList is the list of attachments for the web UI.
func (p *plan) attachmentList() []Attachment {
	out := []Attachment{}
	for _, a := range p.attachments {
		out = append(out, a.attachment())
	}
	return out
}

func (p partInfo) attachment() Attachment {
	name := p.filename
	if name == "" {
		name = "part-" + p.section() + extension(p.mediaType)
	}
	size := p.size
	if p.encoding == "base64" {
		size = size * 3 / 4
	}
	inline := p.id != "" && slices.Contains(inlineTypes, p.mediaType) && p.disposition != "attachment"
	return Attachment{Part: p.section(), Name: safeFileName(name), Type: p.mediaType, Size: size, Inline: inline, CID: p.id}
}

func extension(mediaType string) string {
	switch mediaType {
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	case "message/rfc822":
		return ".eml"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	}
	return ""
}

// safeFileName keeps a name a browser and the file manager accept: no
// paths, no control characters, at most 200 characters.
func safeFileName(name string) string {
	name = strings.ToValidUTF8(name, "_")
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	out := []rune{}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"|?*`, r) {
			r = '_'
		}
		out = append(out, r)
		if len(out) >= 200 {
			break
		}
	}
	name = strings.Trim(strings.TrimSpace(string(out)), ".")
	if name == "" {
		name = "attachment"
	}
	return name
}

// hasAttachments tells whether a mail has parts besides its text, for
// the list (embedded pictures do not count).
func hasAttachments(bs imap.BodyStructure) bool {
	p := walkStructure(bs)
	for _, a := range p.attachments {
		if !a.attachment().Inline {
			return true
		}
	}
	return false
}

var partRE = regexp.MustCompile(`^[1-9][0-9]{0,3}(\.[1-9][0-9]{0,3}){0,9}$`)

// parsePart turns "1.2" into a section path.
func parsePart(part string) ([]int, bool) {
	if !partRE.MatchString(part) {
		return nil, false
	}
	var out []int
	for _, s := range strings.Split(part, ".") {
		n, _ := strconv.Atoi(s)
		out = append(out, n)
	}
	return out, true
}

// findPart returns a leaf of the structure by its section number.
func findPart(bs imap.BodyStructure, part string) (partInfo, bool) {
	p := walkStructure(bs)
	all := slices.Clone(p.attachments)
	if p.text != nil {
		all = append(all, *p.text)
	}
	if p.html != nil {
		all = append(all, *p.html)
	}
	for _, a := range all {
		if a.section() == part {
			return a, true
		}
	}
	return partInfo{}, false
}

// decoder undoes the transfer encoding of a part.
func decoder(encoding string, r io.Reader) io.Reader {
	switch encoding {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &base64Filter{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

// base64Filter drops what is not base64 (line ends, spaces, junk) so that
// a sloppy mail still decodes.
type base64Filter struct{ r io.Reader }

func (f *base64Filter) Read(p []byte) (int, error) {
	for {
		n, err := f.r.Read(p)
		j := 0
		for _, c := range p[:n] {
			if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' {
				p[j] = c
				j++
			}
		}
		if j > 0 || err != nil {
			return j, err
		}
	}
}

// decodeText decodes a text part to UTF-8 within limit; cut tells that
// the part was longer (or its encoded data was cut).
func decodeText(info partInfo, raw []byte, rawCut bool, limit int) (string, bool) {
	r := decoder(info.encoding, bytes.NewReader(raw))
	if label := info.params["charset"]; label != "" && !strings.EqualFold(label, "utf-8") && !strings.EqualFold(label, "us-ascii") {
		if converted, err := charset.Reader(label, r); err == nil {
			r = converted
		}
	}
	data, _ := io.ReadAll(io.LimitReader(r, int64(limit)+1)) // a cut part ends with a broken quantum; what came is shown
	cut := rawCut
	if len(data) > limit {
		data, cut = data[:limit], true
	}
	// A cut can split a character.
	for len(data) > 0 && !utf8.Valid(data) && cut {
		_, size := utf8.DecodeLastRune(data)
		if size >= len(data) {
			break
		}
		data = data[:len(data)-size]
	}
	return strings.ToValidUTF8(string(data), "�"), cut
}

// encodedLimit is how much of a part is fetched for limit decoded bytes.
func encodedLimit(info partInfo, limit int) int64 {
	switch info.encoding {
	case "base64":
		return int64(limit)*4/3 + int64(limit)/38 + 64
	case "quoted-printable":
		return int64(limit) * 3
	}
	return int64(limit)
}

func sectionKey(part []int) string { return fmt.Sprint(part) }
