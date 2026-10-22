package assistant

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// The web search of the assistant: DuckDuckGo through its HTML page, which
// needs neither a key nor an account, and reading single pages. Nothing on
// the home network can be reached this way: every address a name resolves
// to is checked before titand connects, after redirects as well.

const (
	// MaxSearchResults is how many results the model gets.
	MaxSearchResults = 8
	// MaxPageBytes is how much of a page is loaded.
	MaxPageBytes = 2 << 20
	// MaxPageText is how much of a page's text the model reads.
	MaxPageText = 20000
)

// SearchURL is the HTML search page of DuckDuckGo; tests point it elsewhere.
var SearchURL = "https://html.duckduckgo.com/html/"

var ErrBlockedAddress = errors.New("assistant: address not allowed")

// blocked are networks the web tools never connect to.
var blocked = func() []netip.Prefix {
	var list []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
		"224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "64:ff9b::/96", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		list = append(list, netip.MustParsePrefix(p))
	}
	return list
}()

// Allowed reports whether the web tools may connect to an address.
func Allowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(addr) {
			return false
		}
	}
	// An address of this NAS itself, even a public one.
	if ifaces, err := net.InterfaceAddrs(); err == nil {
		for _, a := range ifaces {
			if prefix, err := netip.ParsePrefix(a.String()); err == nil && prefix.Addr().Unmap() == addr {
				return false
			}
		}
	}
	return true
}

// Web loads search results and pages.
type Web struct {
	client *http.Client
	// Resolve looks up a name; tests replace it.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	// Check decides about an address; tests replace it.
	Check func(netip.Addr) bool
	// Ports are the ports it connects to; tests add theirs.
	Ports []string
}

// NewWeb returns the web tools with the checks in place.
func NewWeb() *Web {
	w := &Web{
		Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		Check: Allowed,
		Ports: []string{"80", "443"},
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(w.Ports, port) {
				return nil, fmt.Errorf("%w: port %s", ErrBlockedAddress, port)
			}
			var addrs []netip.Addr
			if addr, err := netip.ParseAddr(host); err == nil {
				addrs = []netip.Addr{addr}
			} else if addrs, err = w.Resolve(ctx, host); err != nil {
				return nil, err
			}
			// Every address must be allowed: otherwise a name with one
			// public and one private address could pick the private one.
			for _, addr := range addrs {
				if !w.Check(addr) {
					return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, host)
				}
			}
			if len(addrs) == 0 {
				return nil, fmt.Errorf("no address for %s", host)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0].String(), port))
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	w.client = &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("%w: %s", ErrBlockedAddress, req.URL.Scheme)
			}
			return nil
		},
	}
	return w
}

// SearchResult is one hit of the web search.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

var (
	resultRE  = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	snippetRE = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
	tagRE     = regexp.MustCompile(`(?s)<[^>]*>`)
)

func plain(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRE.ReplaceAllString(fragment, " "))), " ")
}

// resultURL takes the target out of DuckDuckGo's redirect link.
func resultURL(href string) string {
	href = html.UnescapeString(href)
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if target := u.Query().Get("uddg"); target != "" {
		href = target
		if u, err = url.Parse(href); err != nil {
			return ""
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func (w *Web) userAgent(version string) string {
	return "Mozilla/5.0 (compatible; TitanOS/" + version + ")"
}

// Search asks DuckDuckGo.
func (w *Web) Search(ctx context.Context, version, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 300 {
		return nil, Errorf("the search text must have 1 to 300 characters")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, SearchURL, strings.NewReader(url.Values{"q": {query}, "kl": {"wt-wt"}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", w.userAgent(version))
	res, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search not available: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search not available: DuckDuckGo answered %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxPageBytes))
	if err != nil {
		return nil, fmt.Errorf("search not available: %w", err)
	}
	page := string(body)
	links := resultRE.FindAllStringSubmatchIndex(page, -1)
	results := []SearchResult{}
	for i, link := range links {
		target := resultURL(page[link[2]:link[3]])
		title := plain(page[link[4]:link[5]])
		if target == "" || title == "" || strings.Contains(target, "duckduckgo.com/y.js") {
			continue
		}
		end := len(page)
		if i+1 < len(links) {
			end = links[i+1][0]
		}
		snippet := ""
		if m := snippetRE.FindStringSubmatch(page[link[1]:end]); m != nil {
			snippet = plain(m[1])
		}
		results = append(results, SearchResult{Title: title, URL: target, Snippet: snippet})
		if len(results) == MaxSearchResults {
			break
		}
	}
	if len(results) == 0 && !strings.Contains(page, "result") {
		return nil, errors.New("search not available: DuckDuckGo sent an unexpected page")
	}
	return results, nil
}

var (
	dropRE  = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head|nav|footer|form|iframe)\b.*?</(script|style|noscript|svg|head|nav|footer|form|iframe)>`)
	breakRE = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/h[1-6]|/tr|/section|/article|/pre)[^>]*>`)
	titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	spaceRE = regexp.MustCompile(`[ \t\f\v\r]+`)
	blankRE = regexp.MustCompile(`\n\s*\n+`)
)

// Page is the text of a web page.
type Page struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Cut   bool   `json:"cut,omitempty"`
}

// Read loads one page and returns its text.
func (w *Web) Read(ctx context.Context, version, address string) (Page, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return Page{}, Errorf("only http and https addresses without a login can be read")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", w.userAgent(version))
	req.Header.Set("Accept", "text/html,text/plain;q=0.9,*/*;q=0.1")
	res, err := w.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			return Page{}, Errorf("this address is on the home network or otherwise not allowed")
		}
		return Page{}, Errorf("the page could not be loaded: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Page{}, Errorf("the page answered %d", res.StatusCode)
	}
	kind := res.Header.Get("Content-Type")
	if kind != "" && !strings.Contains(kind, "text/") && !strings.Contains(kind, "json") && !strings.Contains(kind, "xml") {
		return Page{}, Errorf("the page is %s, not text", kind)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxPageBytes))
	if err != nil {
		return Page{}, Errorf("the page could not be loaded: %v", err)
	}
	if !utf8.Valid(body) {
		body = []byte(strings.ToValidUTF8(string(body), "�"))
	}
	page := Page{URL: res.Request.URL.String()}
	text := string(body)
	if strings.Contains(kind, "html") || strings.Contains(strings.ToLower(text[:min(len(text), 512)]), "<html") {
		if m := titleRE.FindStringSubmatch(text); m != nil {
			page.Title = plain(m[1])
		}
		text = dropRE.ReplaceAllString(text, " ")
		text = breakRE.ReplaceAllString(text, "\n")
		text = html.UnescapeString(tagRE.ReplaceAllString(text, " "))
	}
	text = spaceRE.ReplaceAllString(text, " ")
	text = strings.TrimSpace(blankRE.ReplaceAllString(text, "\n\n"))
	if r := []rune(text); len(r) > MaxPageText {
		text, page.Cut = string(r[:MaxPageText]), true
	}
	page.Text = text
	return page, nil
}
