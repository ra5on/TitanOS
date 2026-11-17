package davsync

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// TestIOS26 replays the iOS-like request sequences of testdata/ios26/*.txt
// against the real handler over TLS (TECHNIK 8.2 step 6). The format is
// described in testdata/ios26/README.txt.
func TestIOS26(t *testing.T) {
	files, err := filepath.Glob("testdata/ios26/*.txt")
	if err != nil || len(files) == 0 {
		t.Fatal("no sequences", err)
	}
	for _, file := range files {
		if filepath.Base(file) == "README.txt" {
			continue
		}
		t.Run(strings.TrimSuffix(filepath.Base(file), ".txt"), func(t *testing.T) {
			steps, err := readSequence(file)
			if err != nil {
				t.Fatal(err)
			}
			runSequence(t, newEnv(t), file, steps)
		})
	}
}

// step is a directive (@…) or a request with its expectations.
type step struct {
	line      int
	directive []string
	method    string
	path      string
	headers   [][2]string
	body      []string
	inBody    bool
	expects   []expectation
}

type expectation struct {
	line  int
	words []string
}

func readSequence(file string) ([]*step, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var steps []*step
	var cur *step
	for i, line := range strings.Split(string(data), "\n") {
		n := i + 1
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "@"):
			cur = nil
			steps = append(steps, &step{line: n, directive: strings.Fields(line[1:])})
		case line == ">" || strings.HasPrefix(line, "> "):
			text := strings.TrimPrefix(strings.TrimPrefix(line, ">"), " ")
			switch {
			case cur == nil || len(cur.expects) > 0:
				f := strings.Fields(text)
				if len(f) != 2 {
					return nil, fmt.Errorf("%s:%d: want \"> METHOD PATH\"", file, n)
				}
				cur = &step{line: n, method: f[0], path: f[1]}
				steps = append(steps, cur)
			case cur.inBody:
				cur.body = append(cur.body, text)
			case text == "":
				cur.inBody = true
			default:
				k, v, ok := strings.Cut(text, ":")
				if !ok {
					return nil, fmt.Errorf("%s:%d: want a header", file, n)
				}
				cur.headers = append(cur.headers, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
			}
		case strings.HasPrefix(line, "< "):
			if cur == nil {
				return nil, fmt.Errorf("%s:%d: expectation without request", file, n)
			}
			cur.expects = append(cur.expects, expectation{line: n, words: strings.Fields(line[2:])})
		default:
			return nil, fmt.Errorf("%s:%d: unknown line", file, n)
		}
	}
	return steps, nil
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata/ios26/fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func runSequence(t *testing.T, e *env, file string, steps []*step) {
	for _, s := range steps {
		where := fmt.Sprintf("%s:%d", filepath.Base(file), s.line)
		if s.directive != nil {
			e.directive(t, where, s.directive)
			continue
		}
		res, body := e.replay(t, s)
		for _, x := range s.expects {
			if err := e.check(t, s, res, body, x.words); err != nil {
				t.Errorf("%s:%d (%s %s): %v", filepath.Base(file), x.line, s.method, s.path, err)
			}
		}
		if t.Failed() {
			t.Logf("%s: answer %d\n%s", where, res.StatusCode, body)
			return
		}
	}
}

func (e *env) directive(t *testing.T, where string, d []string) {
	switch {
	case d[0] == "seed" && len(d) == 5:
		// @seed calendar|contacts <collection> <name> <fixture>; the name
		// percent-encoded where it has spaces.
		e.put(store.Kind(d[1]), "alex", d[2], unescapeName(t, d[3]), fixture(t, d[4]))
	case d[0] == "seed-bea" && len(d) == 5:
		e.put(store.Kind(d[1]), "bea", d[2], unescapeName(t, d[3]), fixture(t, d[4]))
	case d[0] == "collection" && len(d) == 4:
		// @collection calendar <id> <name>: another collection of alex.
		if _, err := e.store.CreateCollection(store.Kind(d[1]), "alex", d[2], store.Meta{Name: d[3], Color: store.DefaultColor}); err != nil {
			t.Fatalf("%s: %v", where, err)
		}
	case d[0] == "volume" && len(d) == 2:
		// @volume off | disk1 | disk2 (another, empty volume)
		v := d[1]
		if v == "off" {
			v = ""
		}
		e.setVolume(v)
	case d[0] == "redirect" && len(d) == 2:
		code, err := strconv.Atoi(d[1])
		if err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		e.h.Redirect = code
	default:
		t.Fatalf("%s: unknown directive %v", where, d)
	}
}

func unescapeName(t *testing.T, s string) string {
	name, err := url.PathUnescape(s)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

// request builds the request of a step; Auth and Follow are pseudo
// headers.
func (e *env) request(t *testing.T, s *step, target string) *http.Request {
	body := strings.Join(s.body, "\n")
	req, err := http.NewRequest(s.method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "iOS/26.1 (23B85) dataaccessd/1.0")
	for _, h := range s.headers {
		switch h[0] {
		case "Auth":
			if h[1] != "none" {
				pw, ok := e.passwords[h[1]]
				if !ok {
					t.Fatalf("unknown Auth %q", h[1])
				}
				user := h[1]
				if user == "account" {
					user = "alex"
				}
				req.SetBasicAuth(user, pw)
			}
		case "Follow":
		default:
			req.Header.Set(h[0], subst(t, h[1]))
		}
	}
	if len(s.body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	}
	return req
}

func (s *step) header(name string) string {
	for _, h := range s.headers {
		if h[0] == name {
			return h[1]
		}
	}
	return ""
}

// replay sends a step, following redirects with the same method, body
// and sign-in when it says Follow: yes (as iOS does).
func (e *env) replay(t *testing.T, s *step) (*http.Response, []byte) {
	target := e.srv.URL + s.path
	for hops := 0; ; hops++ {
		res, err := e.client().Do(e.request(t, s, target))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if s.header("Follow") != "yes" || res.StatusCode < 300 || res.StatusCode > 399 || hops > 3 {
			return res, body
		}
		loc, err := res.Request.URL.Parse(res.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		target = loc.String()
	}
}

// tnode is an element of an answer, namespace-aware.
type tnode struct {
	name     xml.Name
	attr     []xml.Attr
	children []*tnode
	text     string
}

func parseAnswer(b []byte) (*tnode, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	var stack []*tnode
	var root *tnode
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			n := &tnode{name: tok.Name}
			for _, a := range tok.Attr {
				if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
					n.attr = append(n.attr, a)
				}
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(tok)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("no XML")
	}
	return root, nil
}

func (n *tnode) child(name xml.Name) *tnode {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

// canonicalOf writes an element with the fixed test prefixes.
func canonicalOf(n *tnode) string {
	p, ok := prefixOf(n.name.Space)
	tag := "{" + n.name.Space + "}" + n.name.Local
	if ok {
		tag = p + ":" + n.name.Local
	}
	var b strings.Builder
	b.WriteString("<" + tag)
	attrs := slices.Clone(n.attr)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name.Local < attrs[j].Name.Local })
	for _, a := range attrs {
		b.WriteString(" " + a.Name.Local + `="` + a.Value + `"`)
	}
	inner := innerOf(n)
	if inner == "" {
		b.WriteString("/>")
		return b.String()
	}
	b.WriteString(">" + inner + "</" + tag + ">")
	return b.String()
}

func innerOf(n *tnode) string {
	var b strings.Builder
	if len(n.children) == 0 {
		b.WriteString(strings.TrimSpace(n.text))
	}
	for _, c := range n.children {
		b.WriteString(canonicalOf(c))
	}
	return b.String()
}

// davResponse is one response of a multistatus.
type davResponse struct {
	status string
	props  map[xml.Name]davProp
}

type davProp struct {
	status int
	node   *tnode
}

func parseMultistatus(b []byte) (map[string]*davResponse, int, error) {
	root, err := parseAnswer(b)
	if err != nil {
		return nil, 0, err
	}
	if root.name != davName("multistatus") {
		return nil, 0, fmt.Errorf("root is %v", root.name)
	}
	out := map[string]*davResponse{}
	count := 0
	for _, r := range root.children {
		if r.name != davName("response") {
			continue
		}
		count++
		href := r.child(davName("href"))
		if href == nil {
			return nil, 0, fmt.Errorf("response without href")
		}
		dr := &davResponse{props: map[xml.Name]davProp{}}
		if s := r.child(davName("status")); s != nil {
			dr.status = strings.TrimSpace(s.text)
		}
		for _, ps := range r.children {
			if ps.name != davName("propstat") {
				continue
			}
			st := ps.child(davName("status"))
			prop := ps.child(davName("prop"))
			if st == nil || prop == nil {
				return nil, 0, fmt.Errorf("propstat without status or prop")
			}
			f := strings.Fields(st.text)
			if len(f) < 2 {
				return nil, 0, fmt.Errorf("bad status %q", st.text)
			}
			code, _ := strconv.Atoi(f[1])
			for _, p := range prop.children {
				if _, dup := dr.props[p.name]; dup {
					return nil, 0, fmt.Errorf("%s: property %v twice", href.text, p.name)
				}
				dr.props[p.name] = davProp{status: code, node: p}
			}
		}
		out[strings.TrimSpace(href.text)] = dr
	}
	return out, count, nil
}

// testName reads "d:getctag" with the fixed prefixes.
func testName(s string) (xml.Name, error) {
	p, local, ok := strings.Cut(s, ":")
	if !ok {
		return xml.Name{}, fmt.Errorf("name %q without prefix", s)
	}
	for _, pr := range prefixes {
		if pr.prefix == p {
			return xml.Name{Space: pr.space, Local: local}, nil
		}
	}
	if p == "x" {
		return xml.Name{Space: "http://example.com/ns/", Local: local}, nil
	}
	return xml.Name{}, fmt.Errorf("unknown prefix %q", p)
}

// subst replaces {etag:<fixture>} by the quoted ETag of the fixture.
func subst(t *testing.T, s string) string {
	for {
		i := strings.Index(s, "{etag:")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "}")
		name := s[i+6 : i+j]
		s = s[:i] + quote(store.ETag(fixture(t, name))) + s[i+j+1:]
	}
}

func (e *env) check(t *testing.T, s *step, res *http.Response, body []byte, w []string) error {
	arg := func(i int) string {
		if i < len(w) {
			return subst(t, strings.Join(w[i:], " "))
		}
		return ""
	}
	multistatus := func() (map[string]*davResponse, int, error) { return parseMultistatus(body) }
	response := func(href string) (*davResponse, error) {
		all, _, err := multistatus()
		if err != nil {
			return nil, err
		}
		r := all[href]
		if r == nil {
			keys := make([]string, 0, len(all))
			for k := range all {
				keys = append(keys, k)
			}
			return nil, fmt.Errorf("no response for %s (have %v)", href, keys)
		}
		return r, nil
	}
	prop := func(href, name string) (davProp, error) {
		r, err := response(href)
		if err != nil {
			return davProp{}, err
		}
		n, err := testName(name)
		if err != nil {
			return davProp{}, err
		}
		p, ok := r.props[n]
		if !ok {
			return davProp{}, fmt.Errorf("%s: no %s", href, name)
		}
		return p, nil
	}
	switch w[0] {
	case "status":
		if strconv.Itoa(res.StatusCode) != w[1] {
			return fmt.Errorf("status %d, want %s", res.StatusCode, w[1])
		}
	case "header":
		if got := res.Header.Get(w[1]); got != arg(2) {
			return fmt.Errorf("%s: %q, want %q", w[1], got, arg(2))
		}
	case "header-has":
		if got := res.Header.Get(w[1]); !strings.Contains(got, arg(2)) {
			return fmt.Errorf("%s: %q lacks %q", w[1], got, arg(2))
		}
	case "no-header":
		if got := res.Header.Get(w[1]); got != "" {
			return fmt.Errorf("%s: %q, want none", w[1], got)
		}
	case "body":
		if want := fixture(t, w[1]); !bytes.Equal(body, want) {
			return fmt.Errorf("body differs from %s", w[1])
		}
	case "body-has":
		if !strings.Contains(string(body), arg(1)) {
			return fmt.Errorf("body lacks %q", arg(1))
		}
	case "body-lacks":
		if strings.Contains(string(body), arg(1)) {
			return fmt.Errorf("body has %q", arg(1))
		}
	case "responses":
		_, n, err := multistatus()
		if err != nil {
			return err
		}
		if strconv.Itoa(n) != w[1] {
			return fmt.Errorf("%d responses, want %s", n, w[1])
		}
	case "response":
		// response HREF STATUS: a response with a status of its own.
		r, err := response(w[1])
		if err != nil {
			return err
		}
		if f := strings.Fields(r.status); len(f) < 2 || f[1] != w[2] {
			return fmt.Errorf("%s: status %q, want %s", w[1], r.status, w[2])
		}
	case "prop":
		// prop HREF STATUS NAME…
		for _, name := range w[3:] {
			p, err := prop(w[1], name)
			if err != nil {
				return err
			}
			if strconv.Itoa(p.status) != w[2] {
				return fmt.Errorf("%s %s: %d, want %s", w[1], name, p.status, w[2])
			}
		}
	case "no-prop":
		// no-prop HREF NAME…: not in the answer at all.
		r, err := response(w[1])
		if err != nil {
			return err
		}
		for _, name := range w[2:] {
			n, err := testName(name)
			if err != nil {
				return err
			}
			if _, ok := r.props[n]; ok {
				return fmt.Errorf("%s: %s present", w[1], name)
			}
		}
	case "value":
		// value HREF NAME CANONICAL: the content of a 200 property
		// (canonical form) contains the text.
		p, err := prop(w[1], w[2])
		if err != nil {
			return err
		}
		if p.status != 200 {
			return fmt.Errorf("%s %s: %d", w[1], w[2], p.status)
		}
		if got := innerOf(p.node); !strings.Contains(got, arg(3)) {
			return fmt.Errorf("%s %s: %q lacks %q", w[1], w[2], got, arg(3))
		}
	case "privileges":
		// privileges HREF RIGHT…: exactly these, one privilege element
		// each.
		p, err := prop(w[1], "d:current-user-privilege-set")
		if err != nil {
			return err
		}
		var got []string
		for _, c := range p.node.children {
			if c.name != davName("privilege") || len(c.children) != 1 || c.children[0].name.Space != nsDAV {
				return fmt.Errorf("%s: privilege element %s", w[1], canonicalOf(c))
			}
			got = append(got, c.children[0].name.Local)
		}
		if !slices.Equal(got, w[2:]) {
			return fmt.Errorf("%s: privileges %v, want %v", w[1], got, w[2:])
		}
	case "data":
		// data HREF FIXTURE: calendar-data/address-data is the fixture
		// byte for byte.
		name := "cal:calendar-data"
		if strings.HasPrefix(w[1], CardDAVPrefix) {
			name = "card:address-data"
		}
		p, err := prop(w[1], name)
		if err != nil {
			return err
		}
		if want := string(fixture(t, w[2])); p.node.text != want {
			return fmt.Errorf("%s: data differs from %s:\n%q\n%q", w[1], w[2], p.node.text, want)
		}
	case "error":
		// error NAME: a DAV:error with this precondition.
		root, err := parseAnswer(body)
		if err != nil {
			return err
		}
		n, err := testName(w[1])
		if err != nil {
			return err
		}
		if root.name != davName("error") || root.child(n) == nil {
			return fmt.Errorf("no DAV:error with %s", w[1])
		}
	case "not-counted":
		// The request (without a password) does not count: ten more of
		// it never lock, and the device signs in at once after.
		for range 10 {
			r, _ := e.replay(t, s)
			if r.StatusCode != http.StatusUnauthorized {
				return fmt.Errorf("again: %d", r.StatusCode)
			}
		}
		again := &step{method: "PROPFIND", path: CalDAVPrefix + "/", headers: [][2]string{{"Auth", "alex"}, {"Depth", "0"}}}
		if r, _ := e.replay(t, again); r.StatusCode != http.StatusMultiStatus {
			return fmt.Errorf("device after unsigned requests: %d", r.StatusCode)
		}
		if e.limiter.RetryAfter("cal-user:alex") > 0 {
			return fmt.Errorf("account locked")
		}
	default:
		return fmt.Errorf("unknown expectation %q", w[0])
	}
	return nil
}

// The harness reads what it is given; a broken sequence file fails
// loudly rather than passing empty.
func TestIOS26FormatErrors(t *testing.T) {
	dir := t.TempDir()
	for _, text := range []string{"< status 200\n", "> PROPFIND\n", "bogus\n", "> GET /x\n> no header\n"} {
		f := filepath.Join(dir, "x.txt")
		if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readSequence(f); err == nil {
			t.Errorf("%q read without error", text)
		}
	}
}
