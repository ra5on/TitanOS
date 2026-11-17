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
	href, _ := attr(n, "href")
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
		dropAttr(n, "href")
		return
	}
	setAttr(n, "target", "_blank")
	setAttr(n, "rel", "noopener noreferrer")
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
