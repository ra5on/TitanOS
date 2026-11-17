package tmail

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/publicsuffix"
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
	setAttr(n, "target", "_blank")
	setAttr(n, "rel", "noopener noreferrer")
	// The real target shows on hover, whatever the text says.
	setAttr(n, "title", u.String())
	text := strings.TrimSpace(textOf(n))
	var shown, real string
	if u.Scheme == "mailto" {
		// An address as text is compared with the address of the link.
		shown, real = mailDomain(text), mailDomain(u.Opaque)
	} else {
		shown, real = shownHost(text), strings.ToLower(u.Hostname())
	}
	if shown == "" || real == "" || sameSite(shown, real) {
		return
	}
	// The visible text names another domain: say where the link goes,
	// right behind it.
	setAttr(n, "data-tmail-mismatch", "1")
	mark := &html.Node{Type: html.ElementNode, DataAtom: atom.Span, Data: "span",
		Attr: []html.Attribute{{Key: "style", Val: "color:#b00020;font-weight:bold"}, {Key: "data-tmail-target", Val: real}}}
	mark.AppendChild(&html.Node{Type: html.TextNode, Data: " \u26a0 " + real})
	if n.Parent != nil {
		n.Parent.InsertBefore(mark, n.NextSibling)
	}
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

// webInText is a link text that may be a web address: an optional scheme
// and a host name, nothing else but a path after it.
var webInText = regexp.MustCompile(`(?i)^(https?://)?((?:[a-z0-9-]+\.)+[a-z0-9-]+)\.?(?::[0-9]+)?(?:[/?#]\S*)?$`)

// shownHost is the host a link text names, "" if the text does not look
// like a web address: a scheme, "www." or a name whose ending is a real
// public suffix (ICANN). File names (rechnung.pdf, bild.jpg), version
// numbers (v1.2) and mail addresses are no targets to compare.
func shownHost(text string) string {
	if strings.Contains(text, "@") && !strings.Contains(strings.ToLower(text), "://") {
		return ""
	}
	m := webInText.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	host := strings.ToLower(m[2])
	if m[1] != "" || strings.HasPrefix(host, "www.") {
		return host
	}
	suffix, icann := publicsuffix.PublicSuffix(host)
	if !icann || suffix == host {
		return ""
	}
	return host
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
