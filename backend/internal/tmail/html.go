package tmail

import (
	"bytes"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/text/unicode/norm"
)

// HTML mail is cleaned on the server (docs/TMAIL.md "Sicherheit der
// Anzeige"): an allow list of text markup, tables, pictures, links and a
// few inline styles. No scripts, forms, <style> blocks, <base>, <meta>,
// frames, event attributes, javascript: or data: links. Afterwards every
// link opens in a new window without opener or referrer, pictures of the
// mail itself (cid:) become data: URLs, and pictures from the internet
// are replaced by a placeholder unless the person asked for them.

var (
	policyOnce sync.Once
	policy     *bluemonday.Policy
)

func mailPolicy() *bluemonday.Policy {
	policyOnce.Do(func() {
		p := bluemonday.NewPolicy()
		p.AllowElements("a", "abbr", "address", "b", "bdi", "bdo", "big", "blockquote", "br", "caption", "center", "cite",
			"code", "col", "colgroup", "dd", "del", "dfn", "div", "dl", "dt", "em", "font", "h1", "h2", "h3", "h4", "h5", "h6",
			"hr", "i", "img", "ins", "kbd", "li", "mark", "ol", "p", "pre", "q", "s", "samp", "small", "span", "strike",
			"strong", "sub", "sup", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "tt", "u", "ul", "var", "wbr")
		p.AllowAttrs("href").OnElements("a")
		p.AllowAttrs("src", "alt", "width", "height").OnElements("img")
		p.AllowAttrs("title", "dir", "lang").Globally()
		p.AllowAttrs("align", "valign").OnElements("p", "div", "td", "th", "tr", "table", "img", "h1", "h2", "h3", "h4", "h5", "h6", "col", "colgroup", "tbody", "thead", "tfoot", "center", "hr")
		p.AllowAttrs("width", "height").Matching(regexp.MustCompile(`^[0-9]{1,4}(%|px)?$`)).OnElements("table", "td", "th", "col", "colgroup", "hr")
		p.AllowAttrs("bgcolor").Matching(regexp.MustCompile(`^#?[0-9A-Za-z]{1,20}$`)).OnElements("table", "td", "th", "tr", "body")
		p.AllowAttrs("border", "cellpadding", "cellspacing").Matching(regexp.MustCompile(`^[0-9]{1,3}$`)).OnElements("table", "img")
		p.AllowAttrs("colspan", "rowspan").Matching(regexp.MustCompile(`^[0-9]{1,3}$`)).OnElements("td", "th")
		p.AllowAttrs("color").Matching(regexp.MustCompile(`^#?[0-9A-Za-z]{1,20}$`)).OnElements("font")
		p.AllowAttrs("face").Matching(regexp.MustCompile(`^[0-9A-Za-z ,'"-]{1,100}$`)).OnElements("font")
		p.AllowAttrs("size").Matching(regexp.MustCompile(`^[+-]?[0-9]$`)).OnElements("font")
		p.AllowAttrs("start").Matching(regexp.MustCompile(`^[0-9]{1,6}$`)).OnElements("ol")
		p.AllowStyles("color", "background-color", "font-size", "font-weight", "font-style", "font-family", "text-align",
			"text-decoration", "text-transform", "line-height", "letter-spacing", "vertical-align", "white-space",
			"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
			"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
			"border", "border-top", "border-right", "border-bottom", "border-left", "border-color", "border-style", "border-width",
			"border-collapse", "border-spacing", "border-radius", "width", "max-width", "min-width", "height", "max-height",
			"display", "float", "clear").Globally()
		p.AllowURLSchemes("http", "https", "mailto", "cid")
		p.RequireParseableURLs(true)
		p.AllowRelativeURLs(false)
		policy = p
	})
	return policy
}

// placeholder stands in for a picture from the internet that was not
// loaded: a grey box that says nothing.
const placeholder = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIzMiIgaGVpZ2h0PSIzMiI+PHJlY3Qgd2lkdGg9IjMyIiBoZWlnaHQ9IjMyIiBmaWxsPSIjZGRkIi8+PC9zdmc+"

// CSP is the policy the document of a mail carries; images=true lets
// pictures from the internet load (https only).
func CSP(images bool) string {
	img := "data:"
	if images {
		img += " https:"
	}
	return "default-src 'none'; style-src 'unsafe-inline'; img-src " + img + "; font-src 'none'; form-action 'none'; base-uri 'none'"
}

// SanitizeHTML cleans the HTML of a mail and returns a whole document for
// an iframe's srcdoc. cids maps Content-IDs to data: URLs of the mail's
// own pictures. external tells whether the mail has pictures from the
// internet (replaced unless images).
func SanitizeHTML(raw string, cids map[string]string, images bool) (doc string, external bool) {
	clean := mailPolicy().Sanitize(raw)
	body := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(clean), body)
	if err != nil {
		nodes = nil
	}
	for _, n := range nodes {
		body.AppendChild(n)
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.A:
				fixLink(n)
			case atom.Img:
				if fixImage(n, cids, images) {
					external = true
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(body)
	var buf bytes.Buffer
	buf.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="`)
	buf.WriteString(html.EscapeString(CSP(images)))
	buf.WriteString(`"><meta name="referrer" content="no-referrer"></head>`)
	if err := html.Render(&buf, body); err != nil {
		return "", external
	}
	buf.WriteString(`</html>`)
	return buf.String(), external
}

func attr(n *html.Node, name string) (string, int) {
	for i, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			return a.Val, i
		}
	}
	return "", -1
}

func setAttr(n *html.Node, name, value string) {
	if _, i := attr(n, name); i >= 0 {
		n.Attr[i].Val = value
		return
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: value})
}

func dropAttr(n *html.Node, name string) {
	if _, i := attr(n, name); i >= 0 {
		n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
	}
}

// fixLink lets a link open only http(s) and mailto in a new window.
func fixLink(n *html.Node) {
	dropAttr(n, "title") // the sender's title may name another target
	href, _ := attr(n, "href")
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
		dropAttr(n, "href")
		return
	}
	var real string
	var mixed bool
	if u.Scheme == "mailto" {
		real = mailDomain(u.Opaque)
	} else {
		// The host as the browser will look it up: Punycode. A host with
		// invisible characters or that IDNA refuses leads nowhere sure:
		// the link goes and the place is marked.
		host := u.Hostname()
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil || ascii == "" || hasInvisible(host) {
			dropAttr(n, "href")
			markLink(n, "?")
			return
		}
		ascii = strings.ToLower(ascii)
		if port := u.Port(); port != "" {
			u.Host = net.JoinHostPort(ascii, port)
		} else {
			u.Host = ascii
		}
		setAttr(n, "href", u.String())
		real = ascii
		unicodeHost, _ := idna.Lookup.ToUnicode(ascii)
		mixed = mixedScripts(unicodeHost)
	}
	setAttr(n, "target", "_blank")
	setAttr(n, "rel", "noopener noreferrer")
	// The real target shows on hover, whatever the text says.
	setAttr(n, "title", stripInvisible(u.String()))
	text := normalizeText(textOf(n))
	var shown string
	var shownMixed bool
	if u.Scheme == "mailto" {
		// An address as text is compared with the address of the link.
		shown = mailDomain(text)
	} else {
		shown, shownMixed = shownHost(text)
	}
	if !mixed && !shownMixed && (shown == "" || real == "" || sameSite(shown, real)) {
		return
	}
	// The visible text names another domain, or a name mixes scripts:
	// say where the link goes, right behind it.
	markLink(n, real)
}

// markLink puts a warning with the real target behind a link. Its own
// styles win over what the sender set on the parent (font-size:0, white
// on white), so that it cannot be hidden.
func markLink(n *html.Node, real string) {
	setAttr(n, "data-tmail-mismatch", "1")
	real = stripInvisible(real)
	mark := &html.Node{Type: html.ElementNode, DataAtom: atom.Span, Data: "span",
		Attr: []html.Attribute{{Key: "style", Val: "font-size:14px !important;line-height:normal !important;color:#b00020 !important;background:#ffffff !important;" +
			"font-weight:bold !important;display:inline !important;visibility:visible !important;opacity:1 !important;position:static !important;" +
			// The sender's letter spacing, font (a symbol font of the
			// device), indent, case and spacing are not inherited either
			// (third review of 1.05).
			"letter-spacing:normal !important;word-spacing:normal !important;font-family:sans-serif !important;text-indent:0 !important;" +
			"text-transform:none !important;font-style:normal !important;text-decoration:none !important;white-space:normal !important;" +
			"vertical-align:baseline !important"},
			{Key: "data-tmail-target", Val: real}}}
	mark.AppendChild(&html.Node{Type: html.TextNode, Data: " \u26a0 " + real})
	if n.Parent != nil {
		n.Parent.InsertBefore(mark, n.NextSibling)
	}
}

// hasInvisible tells whether a text holds format characters (Cf).
func hasInvisible(s string) bool {
	return strings.IndexFunc(s, invisible) >= 0
}

// normalizeText makes a link text comparable: NFKC (look-alike forms to
// their plain letters), no invisible characters.
func normalizeText(s string) string {
	return strings.TrimSpace(stripInvisible(norm.NFKC.String(s)))
}

// mixedScripts tells whether the letters of a name come from more than
// one script (a Cyrillic "а" in a Latin name).
func mixedScripts(s string) bool {
	scripts := []*unicode.RangeTable{unicode.Latin, unicode.Cyrillic, unicode.Greek, unicode.Armenian, unicode.Hebrew,
		unicode.Arabic, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Thai, unicode.Devanagari, unicode.Georgian, unicode.Cherokee}
	seen := map[int]bool{}
	other := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		found := false
		for i, table := range scripts {
			if unicode.Is(table, r) {
				seen[i], found = true, true
				break
			}
		}
		if !found {
			other = true
		}
	}
	n := len(seen)
	if other {
		n++
	}
	// Chinese and Japanese mix scripts by nature.
	if n == 2 && seen[6] && (seen[7] || seen[8]) {
		return false
	}
	return n > 1
}

// textOf is the visible text of a node.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// hostInText finds names in a link text that may be web addresses: an
// optional scheme and a host name of letters (any script), digits and
// hyphens with at least one dot.
var hostInText = regexp.MustCompile(`(?i)(https?://)?([\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)+)`)

// shownHost is the host a link text names (in Punycode), "" if it names
// none that looks like a web address: a scheme, "www." or a name whose
// ending is a real public suffix (ICANN), anywhere in the text ("Login:
// www.sparkasse.de"). File names (rechnung.pdf), version numbers (v1.2)
// and mail addresses are no targets to compare. mixed: the name mixes
// scripts.
func shownHost(text string) (string, bool) {
	for _, loc := range hostInText.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] > 0 && text[loc[0]-1] == '@' || loc[1] < len(text) && text[loc[1]] == '@' {
			continue // part of a mail address
		}
		scheme := loc[2] >= 0
		host := strings.ToLower(text[loc[4]:loc[5]])
		mixed := mixedScripts(host)
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil {
			if mixed {
				return host, true
			}
			continue
		}
		if scheme || strings.HasPrefix(ascii, "www.") {
			return ascii, mixed
		}
		// The last label must be a top-level domain of the ICANN list
		// (".com", ".de"), not a file ending (".pdf").
		if dot := strings.LastIndexByte(ascii, '.'); dot > 0 {
			if suffix, icann := publicsuffix.PublicSuffix(ascii[dot+1:]); icann && suffix == ascii[dot+1:] {
				return ascii, mixed
			}
		}
	}
	return "", false
}

// mailDomain is the domain of a mail address ("" if the text is none).
func mailDomain(text string) string {
	addr := strings.TrimSpace(text)
	if i := strings.IndexAny(addr, "?"); i >= 0 {
		addr = addr[:i]
	}
	local, domain, ok := strings.Cut(addr, "@")
	if !ok || local == "" || strings.ContainsAny(domain, " /@") || !strings.Contains(domain, ".") {
		return ""
	}
	return strings.ToLower(domain)
}

// sameSite tells whether two hosts belong to the same registrable domain
// (eTLD+1): www.sparkasse.de and banking.sparkasse.de are one site.
func sameSite(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := publicsuffix.EffectiveTLDPlusOne(a)
	rb, errB := publicsuffix.EffectiveTLDPlusOne(b)
	if errA != nil || errB != nil {
		return strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
	}
	return ra == rb
}

// fixImage resolves cid: pictures and holds back the others; it reports
// a picture from the internet.
func fixImage(n *html.Node, cids map[string]string, images bool) bool {
	src, _ := attr(n, "src")
	src = strings.TrimSpace(src)
	lower := strings.ToLower(src)
	switch {
	case strings.HasPrefix(lower, "cid:"):
		id, _ := url.PathUnescape(src[4:])
		if data, ok := cids[strings.Trim(id, "<>")]; ok {
			setAttr(n, "src", data)
		} else {
			setAttr(n, "src", placeholder)
		}
		return false
	case strings.HasPrefix(lower, "https://") && images:
		setAttr(n, "referrerpolicy", "no-referrer")
		return true
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		setAttr(n, "src", placeholder)
		return true
	}
	dropAttr(n, "src")
	return false
}
