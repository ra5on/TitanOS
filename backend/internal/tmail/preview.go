package tmail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/emersion/go-message/charset"
	"golang.org/x/net/html"
)

// The preview of a mail in the list (1.12, docs/TMAIL.md "1.12"): the
// beginning of its text, taken from the first bytes of its body that the
// list fetches with the heads (BODY.PEEK[TEXT]<0.previewFetch>, never
// marking it read). Only what lies in these bytes is used: a text part
// further back gives no preview.

const (
	// previewFetch is how much of the body the list fetches per mail.
	previewFetch = 3 << 10
	// previewRunes is the longest preview; it is cut at a word, without
	// an ellipsis (the web UI wraps it, nothing is hidden).
	previewRunes = 140
	// previewWord is the longest word kept: longer ones (tracking codes,
	// base64 left over) would only wrap over several lines.
	previewWord = 40
	// previewDepth bounds nested multiparts.
	previewDepth = 4
)

// previewOf makes the preview of a body prefix with the Content-Type and
// Content-Transfer-Encoding of the mail's head.
func previewOf(contentType, encoding string, body []byte) string {
	text, isHTML := previewPart(contentType, encoding, body, 0)
	if isHTML {
		text = htmlText(text)
	}
	return previewText(text)
}

// previewPart finds the first text of a part: its decoded text and
// whether it is HTML.
func previewPart(contentType, encoding string, body []byte, depth int) (string, bool) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		if strings.TrimSpace(contentType) != "" {
			return "", false
		}
		mediaType, params = "text/plain", map[string]string{}
	}
	switch {
	case mediaType == "text/plain":
		return decodePreview(encoding, params["charset"], body), false
	case mediaType == "text/html":
		return decodePreview(encoding, params["charset"], body), true
	case mediaType == "multipart/encrypted", !strings.HasPrefix(mediaType, "multipart/"):
		return "", false
	}
	boundary := params["boundary"]
	if boundary == "" || depth >= previewDepth {
		return "", false
	}
	r := multipart.NewReader(bytes.NewReader(body), boundary)
	for range 20 {
		part, err := r.NextRawPart()
		if err != nil {
			return "", false
		}
		// A part cut off by the end of the prefix gives what it has.
		data, _ := io.ReadAll(io.LimitReader(part, previewFetch))
		h := textproto.MIMEHeader(part.Header)
		if d, _, err := mime.ParseMediaType(h.Get("Content-Disposition")); err == nil && d == "attachment" {
			continue
		}
		ct := h.Get("Content-Type")
		if ct == "" {
			ct = "text/plain"
			if mediaType == "multipart/digest" {
				continue
			}
		}
		if text, isHTML := previewPart(ct, h.Get("Content-Transfer-Encoding"), data, depth+1); strings.TrimSpace(text) != "" {
			return text, isHTML
		}
	}
	return "", false
}

// decodePreview decodes a cut transfer encoding and a charset; what does
// not decode is left out.
func decodePreview(encoding, label string, data []byte) string {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, data)
		clean = clean[:len(clean)/4*4]
		out := make([]byte, base64.StdEncoding.DecodedLen(len(clean)))
		n, _ := base64.StdEncoding.Decode(out, clean)
		data = out[:n]
	case "quoted-printable":
		// A soft break or "=" cut by the end of the prefix ends the text.
		out, _ := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(data)))
		data = out
	}
	if label != "" && !strings.EqualFold(label, "utf-8") && !strings.EqualFold(label, "us-ascii") {
		if r, err := charset.Reader(label, bytes.NewReader(data)); err == nil {
			if out, err := io.ReadAll(r); err == nil || len(out) > 0 {
				data = out
			}
		}
	}
	// A character cut at the end of the prefix (or not decodable) is dropped.
	return strings.ToValidUTF8(string(data), "")
}

// htmlText is the visible text of (the beginning of) an HTML document:
// no head, styles, scripts or quoted blocks; blocks end lines.
func htmlText(doc string) string {
	z := html.NewTokenizer(strings.NewReader(doc))
	var b strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return b.String()
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "head", "style", "script", "title", "noscript", "template", "blockquote", "svg":
				if tt := z.Token().Type; tt == html.StartTagToken {
					skip++
				}
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "table":
				b.WriteByte('\n')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "head", "style", "script", "title", "noscript", "template", "blockquote", "svg":
				if skip > 0 {
					skip--
				}
			case "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "table":
				b.WriteByte('\n')
			}
		}
	}
}

// previewText makes one line of a text: no quoted lines, nothing after
// the signature, no web addresses or overlong words, no control or
// invisible characters; at most previewRunes, cut at a word.
func previewText(text string) string {
	var words []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line == "-- " || line == "--" {
			break
		}
		trimmed := strings.TrimLeft(line, " \t ")
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		for _, w := range strings.FieldsFunc(line, func(r rune) bool { return unicode.IsSpace(r) || r == ' ' }) {
			w = strings.Map(func(r rune) rune {
				if r < 0x20 || r == 0x7f || invisible(r) || r == '�' {
					return -1
				}
				return r
			}, w)
			lower := strings.ToLower(w)
			if w == "" || utf8.RuneCountInString(w) > previewWord || strings.Contains(lower, "://") ||
				strings.HasPrefix(lower, "www.") || strings.HasPrefix(lower, "mailto:") {
				continue
			}
			words = append(words, w)
		}
	}
	var b strings.Builder
	n := 0
	for _, w := range words {
		size := utf8.RuneCountInString(w)
		if n > 0 {
			size++
		}
		if n+size > previewRunes {
			break
		}
		if n > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(w)
		n += size
	}
	return b.String()
}
