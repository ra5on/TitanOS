package davsync

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
)

// Namespaces (TECHNIK 1.4).
const (
	nsDAV     = "DAV:"
	nsCalDAV  = "urn:ietf:params:xml:ns:caldav"
	nsCardDAV = "urn:ietf:params:xml:ns:carddav"
	nsCS      = "http://calendarserver.org/ns/"
	nsApple   = "http://apple.com/ns/ical/"
)

// prefixes are declared once on the root of every answer.
var prefixes = []struct{ space, prefix string }{
	{nsDAV, "d"}, {nsCalDAV, "cal"}, {nsCardDAV, "card"}, {nsCS, "cs"}, {nsApple, "ical"},
}

func prefixOf(space string) (string, bool) {
	for _, p := range prefixes {
		if p.space == space {
			return p.prefix, true
		}
	}
	return "", false
}

func davName(local string) xml.Name  { return xml.Name{Space: nsDAV, Local: local} }
func calName(local string) xml.Name  { return xml.Name{Space: nsCalDAV, Local: local} }
func cardName(local string) xml.Name { return xml.Name{Space: nsCardDAV, Local: local} }

// Limits of a request body besides its size.
const (
	maxXMLDepth = 32
	maxXMLNodes = 20000
)

// errXML is a body that is no acceptable XML (400).
var errXML = errors.New("davsync: bad XML body")

// node is an element of a request body.
type node struct {
	Name     xml.Name
	Attr     []xml.Attr
	Children []*node
	// Text is the character data directly inside the element.
	Text string
}

// child returns the first child of the name, or nil.
func (n *node) child(name xml.Name) *node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// all returns the children of the name.
func (n *node) all(name xml.Name) []*node {
	var out []*node
	for _, c := range n.Children {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// attr returns an attribute without namespace.
func (n *node) attr(local string) string {
	for _, a := range n.Attr {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// parseXML reads a body into a tree; an empty body (only white space) is
// nil. A DOCTYPE or any other directive is refused (no entities, internal
// or external), as are more than maxXMLDepth levels or maxXMLNodes
// elements.
func parseXML(data []byte) (*node, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	var root *node
	var stack []*node
	count := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errXML
		}
		switch tok := tok.(type) {
		case xml.Directive:
			return nil, errXML
		case xml.StartElement:
			count++
			if len(stack) >= maxXMLDepth || count > maxXMLNodes || (root != nil && len(stack) == 0) {
				return nil, errXML
			}
			n := &node{Name: tok.Name}
			for _, a := range tok.Attr {
				if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
					n.Attr = append(n.Attr, a)
				}
			}
			if len(stack) == 0 {
				root = n
			} else {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(tok)
			} else if len(bytes.TrimSpace(tok)) > 0 {
				return nil, errXML
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, errXML
	}
	return root, nil
}

// readBody reads a request body of at most MaxBody within the body
// timeout (as readJSONBody); on failure it answered (413, 408 or 400).
func (rq *request) readBody() ([]byte, bool) {
	tooLarge := func() { http.Error(rq.w, "request body too large", http.StatusRequestEntityTooLarge) }
	if rq.r.ContentLength > MaxBody {
		tooLarge()
		return nil, false
	}
	return rq.readLimited(MaxBody, tooLarge)
}

// readLimited reads a body of at most limit bytes within the body timeout;
// tooLarge answers a longer one (PUT: 413 with max-resource-size). It
// reads limit+1 bytes before it gives up, so a client that announced the
// length is not cut off mid-send (which could reset the connection before
// it reads the answer).
func (rq *request) readLimited(limit int64, tooLarge func()) ([]byte, bool) {
	control := http.NewResponseController(rq.w)
	timed := control.SetReadDeadline(time.Now().Add(rq.h.bodyTimeout)) == nil
	body, err := io.ReadAll(http.MaxBytesReader(rq.w, rq.r.Body, limit))
	if timed && err == nil {
		// A long answer must not end on the deadline of the body.
		_ = control.SetReadDeadline(time.Time{})
	}
	if err != nil {
		var maxErr *http.MaxBytesError
		var netErr net.Error
		switch {
		case errors.As(err, &maxErr):
			tooLarge()
		case errors.As(err, &netErr) && netErr.Timeout():
			http.Error(rq.w, "the request body did not arrive in time", http.StatusRequestTimeout)
		default:
			http.Error(rq.w, "could not read the request body", http.StatusBadRequest)
		}
		return nil, false
	}
	return body, true
}

// readXML reads and parses the body; nil for an empty one.
func (rq *request) readXML() (*node, bool) {
	body, ok := rq.readBody()
	if !ok {
		return nil, false
	}
	n, err := parseXML(body)
	if err != nil {
		http.Error(rq.w, "the body is no acceptable XML", http.StatusBadRequest)
		return nil, false
	}
	return n, true
}

// Elem is an element of an answer.
type Elem struct {
	Name xml.Name
	// Attr are attributes without namespace.
	Attr     []xml.Attr
	Text     string
	Children []Elem
}

func elem(name xml.Name, children ...Elem) Elem { return Elem{Name: name, Children: children} }

func textElem(name xml.Name, text string) Elem { return Elem{Name: name, Text: text} }

func hrefElem(href string) Elem { return textElem(davName("href"), href) }

// xmlWriter writes elements with the prefixes of the root; an element of
// another namespace declares its own. Errors of the bufio.Writer stick and
// show at Flush (the writes are not checked one by one).
type xmlWriter struct {
	b     *bufio.Writer
	extra int
}

func (x *xmlWriter) open(name xml.Name, attrs []xml.Attr, empty bool) string {
	_ = x.b.WriteByte('<')
	tag := name.Local
	declare := ""
	if name.Space != "" {
		if p, ok := prefixOf(name.Space); ok {
			tag = p + ":" + name.Local
		} else {
			x.extra++
			p := "x" + strconv.Itoa(x.extra)
			tag = p + ":" + name.Local
			declare = p
		}
	}
	_, _ = x.b.WriteString(tag)
	if declare != "" {
		_, _ = x.b.WriteString(` xmlns:` + declare + `="`)
		escapeText(x.b, name.Space, true)
		_ = x.b.WriteByte('"')
	}
	for _, a := range attrs {
		_, _ = x.b.WriteString(" " + a.Name.Local + `="`)
		escapeText(x.b, a.Value, true)
		_ = x.b.WriteByte('"')
	}
	if empty {
		_, _ = x.b.WriteString("/>")
	} else {
		_ = x.b.WriteByte('>')
	}
	return tag
}

func (x *xmlWriter) elem(e Elem) {
	if e.Text == "" && len(e.Children) == 0 {
		x.open(e.Name, e.Attr, true)
		return
	}
	tag := x.open(e.Name, e.Attr, false)
	escapeText(x.b, e.Text, false)
	for _, c := range e.Children {
		x.elem(c)
	}
	_, _ = x.b.WriteString("</" + tag + ">")
}

// root opens the root element with every prefix declared.
func (x *xmlWriter) root(name xml.Name) {
	_, _ = x.b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	p, _ := prefixOf(name.Space)
	_, _ = x.b.WriteString("<" + p + ":" + name.Local)
	for _, pr := range prefixes {
		_, _ = x.b.WriteString(` xmlns:` + pr.prefix + `="` + pr.space + `"`)
	}
	_ = x.b.WriteByte('>')
}

func (x *xmlWriter) closeRoot(name xml.Name) {
	p, _ := prefixOf(name.Space)
	_, _ = x.b.WriteString("</" + p + ":" + name.Local + ">\n")
}

// escapeText escapes for XML. CR is kept as a character reference (an XML
// parser would turn a raw CR LF into LF, and calendar data must arrive
// byte for byte); characters XML cannot hold become U+FFFD.
func escapeText(b *bufio.Writer, s string, attr bool) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '&':
			_, _ = b.WriteString("&amp;")
		case r == '<':
			_, _ = b.WriteString("&lt;")
		case r == '>':
			_, _ = b.WriteString("&gt;")
		case r == '"' && attr:
			_, _ = b.WriteString("&quot;")
		case r == '\r':
			_, _ = b.WriteString("&#13;")
		case (r == '\n' || r == '\t') && attr:
			_, _ = b.WriteString("&#" + strconv.Itoa(int(r)) + ";")
		case r == utf8.RuneError && size == 1, !xmlChar(r):
			_, _ = b.WriteString("�")
		default:
			_, _ = b.WriteString(s[i : i+size])
		}
		i += size
	}
}

func xmlChar(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF)
}

// writeXMLError answers status with a DAV:error naming the precondition
// (RFC 4918 16).
func writeXMLError(w http.ResponseWriter, status int, condition xml.Name) {
	writeXMLErrorElem(w, status, elem(condition))
}

// writeXMLErrorElem is writeXMLError with content in the precondition
// (no-uid-conflict names the entry that holds the UID).
func writeXMLErrorElem(w http.ResponseWriter, status int, condition Elem) {
	writeXMLBody(w, status, davName("error"), condition)
}

// writeXMLBody answers status with one XML document of root and children.
func writeXMLBody(w http.ResponseWriter, status int, root xml.Name, children ...Elem) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)
	x := &xmlWriter{b: bufio.NewWriter(w)}
	x.root(root)
	for _, c := range children {
		x.elem(c)
	}
	x.closeRoot(root)
	_ = x.b.Flush()
}

// propstatElems are propstats as elements (the body of a failed MKCALENDAR
// or MKCOL, RFC 4791 5.3.1.2, RFC 5689 3.1).
func propstatElems(ps []Propstat) []Elem {
	var out []Elem
	for _, p := range ps {
		if len(p.Props) > 0 {
			out = append(out, elem(davName("propstat"), elem(davName("prop"), p.Props...), textElem(davName("status"), statusLine(p.Status))))
		}
	}
	return out
}

// Response is one response of a multistatus: either a Status (404 for a
// missing href) or the propstats of the resource.
type Response struct {
	Href      string
	Status    int
	Propstats []Propstat
	// Error is the content of a DAV:error after the status (RFC 4918
	// 14.24: the 507 of a cut REPORT names number-of-matches-within-limits).
	Error []Elem
}

// Propstat is the properties of one status.
type Propstat struct {
	Status int
	Props  []Elem
}

func statusLine(code int) string {
	return "HTTP/1.1 " + strconv.Itoa(code) + " " + http.StatusText(code)
}

// multistatus streams a 207 answer: the status goes out with the first
// response, so a handler may still answer otherwise before (503 without
// the volume: never 207 with an empty list). Large answers (a multiget of
// 1000 entries) never sit in RAM as a whole.
type multistatus struct {
	w       http.ResponseWriter
	x       *xmlWriter
	started bool
}

func newMultistatus(w http.ResponseWriter) *multistatus { return &multistatus{w: w} }

func (m *multistatus) start() {
	if m.started {
		return
	}
	m.started = true
	m.w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	m.w.WriteHeader(http.StatusMultiStatus)
	m.x = &xmlWriter{b: bufio.NewWriterSize(m.w, 32<<10)}
	m.x.root(davName("multistatus"))
}

// add writes one response.
func (m *multistatus) add(r Response) {
	m.start()
	children := []Elem{hrefElem(r.Href)}
	if r.Status != 0 {
		children = append(children, textElem(davName("status"), statusLine(r.Status)))
	}
	children = append(children, propstatElems(r.Propstats)...)
	if len(r.Error) > 0 {
		children = append(children, elem(davName("error"), r.Error...))
	}
	m.x.elem(elem(davName("response"), children...))
}

// finish ends the answer; tail follows the responses (the sync-token of
// sync-collection).
func (m *multistatus) finish(tail ...Elem) {
	m.start()
	for _, e := range tail {
		m.x.elem(e)
	}
	m.x.closeRoot(davName("multistatus"))
	_ = m.x.b.Flush()
}
