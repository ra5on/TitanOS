package imapmini

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Mail is what the autopilot needs of a message.
type Mail struct {
	MessageID string
	From      string // address
	FromName  string
	Subject   string
	Date      time.Time
	// Text is the plain text (or the text of the HTML part).
	Text        string
	Attachments []Attachment
}

// Attachment is a file of a mail.
type Attachment struct {
	Name string
	Type string
	Data []byte
}

const (
	maxParts = 50
	maxDepth = 5
	maxText  = 256 << 10
)

var decoder = &mime.WordDecoder{CharsetReader: charsetReader}

// charsetReader knows UTF-8, US-ASCII and Latin-1 / Windows-1252 (the
// usual ones of German mail); others are passed through.
func charsetReader(charset string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "utf-8", "us-ascii", "ascii", "":
		return r, nil
	case "iso-8859-1", "iso-8859-15", "latin1", "windows-1252", "cp1252":
		data, err := io.ReadAll(io.LimitReader(r, maxText))
		if err != nil {
			return nil, err
		}
		return strings.NewReader(latin1(data)), nil
	}
	return r, nil
}

func latin1(b []byte) string {
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
		if c == 0x80 {
			rs[i] = '€'
		}
	}
	return string(rs)
}

// Parse reads a mail: sender, subject, date, the text and the attachments.
// A mail that makes the parser panic is an error, never a crash.
func Parse(raw []byte) (m Mail, err error) {
	defer func() {
		if r := recover(); r != nil {
			m, err = Mail{}, fmt.Errorf("mail cannot be read: %v", r)
		}
	}()
	return parse(raw)
}

func parse(raw []byte) (Mail, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Mail{}, err
	}
	var m Mail
	m.MessageID = strings.Trim(strings.TrimSpace(msg.Header.Get("Message-Id")), "<>")
	if addr, err := (&mail.AddressParser{WordDecoder: decoder}).Parse(msg.Header.Get("From")); err == nil {
		m.From, m.FromName = strings.ToLower(addr.Address), addr.Name
	}
	if s, err := decoder.DecodeHeader(msg.Header.Get("Subject")); err == nil {
		m.Subject = strings.TrimSpace(s)
	}
	if d, err := msg.Header.Date(); err == nil {
		m.Date = d
	}
	var plain, htmlText strings.Builder
	parts := 0
	var walk func(h map[string][]string, body io.Reader, depth int) error
	walk = func(h map[string][]string, body io.Reader, depth int) error {
		if depth > maxDepth {
			return errors.New("mail nested too deeply")
		}
		header := mail.Header(h)
		ctype, params, err := mime.ParseMediaType(header.Get("Content-Type"))
		if err != nil {
			ctype, params = "text/plain", map[string]string{"charset": "utf-8"}
		}
		if strings.HasPrefix(ctype, "multipart/") {
			mr := multipart.NewReader(body, params["boundary"])
			for {
				p, err := mr.NextRawPart()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if parts++; parts > maxParts {
					return nil
				}
				if err := walk(p.Header, p, depth+1); err != nil {
					return err
				}
			}
		}
		data, err := io.ReadAll(io.LimitReader(decode(header.Get("Content-Transfer-Encoding"), body), int64(MaxMessage)))
		if err != nil {
			return err
		}
		disp, dparams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
		name := dparams["filename"]
		if name == "" {
			name = params["name"]
		}
		if n, err := decoder.DecodeHeader(name); err == nil {
			name = n
		}
		if disp == "attachment" || (name != "" && !strings.HasPrefix(ctype, "text/")) {
			m.Attachments = append(m.Attachments, Attachment{Name: SafeFileName(name, ctype), Type: ctype, Data: data})
			return nil
		}
		text := string(data)
		if cs := params["charset"]; cs != "" {
			if r, err := charsetReader(cs, bytes.NewReader(data)); err == nil {
				b, _ := io.ReadAll(r)
				text = string(b)
			}
		}
		switch ctype {
		case "text/plain":
			if plain.Len() < maxText {
				plain.WriteString(text)
				plain.WriteString("\n")
			}
		case "text/html":
			if htmlText.Len() < maxText {
				htmlText.WriteString(HTMLText(text))
				htmlText.WriteString("\n")
			}
		}
		return nil
	}
	if err := walk(msg.Header, msg.Body, 0); err != nil {
		return m, err
	}
	m.Text = strings.TrimSpace(plain.String())
	if m.Text == "" {
		m.Text = strings.TrimSpace(htmlText.String())
	}
	if len(m.Text) > maxText {
		m.Text = m.Text[:maxText]
	}
	return m, nil
}

func decode(enc string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &stripSpace{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

// stripSpace drops line breaks of base64 bodies.
type stripSpace struct{ r io.Reader }

func (s *stripSpace) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	j := 0
	for i := 0; i < n; i++ {
		if c := p[i]; c != '\r' && c != '\n' && c != ' ' && c != '\t' {
			p[j] = c
			j++
		}
	}
	if j == 0 && n > 0 && err == nil {
		return s.Read(p)
	}
	return j, err
}

var (
	tagRe      = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>|<[^>]+>`)
	blockRe    = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/li|/h[1-6])[^>]*>`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// HTMLText turns the HTML of a mail into plain text.
func HTMLText(s string) string {
	s = blockRe.ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	var lines []string
	for l := range strings.SplitSeq(s, "\n") {
		lines = append(lines, strings.Join(strings.Fields(l), " "))
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// SafeFileName makes the name of an attachment safe to write: no folders,
// no "..", no control characters, no leading dot; a name is made up from
// the type if none is left.
func SafeFileName(name, ctype string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base("/" + name)
	if name == "/" || name == "." {
		name = ""
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune(`/:*?"<>|`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimLeft(strings.TrimSpace(b.String()), ".")
	if r := []rune(name); len(r) > 120 {
		ext := path.Ext(name)
		if len([]rune(ext)) > 16 {
			ext = "" // no real extension; never cut below zero
		}
		name = string(r[:120-len([]rune(ext))]) + ext
	}
	if name == "" {
		ext := ".bin"
		switch ctype {
		case "application/pdf":
			ext = ".pdf"
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		}
		name = "Anhang" + ext
	}
	return name
}
