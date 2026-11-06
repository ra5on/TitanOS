// Package docindex is the local document memory of the agent (PLAN 54-D):
// a small inverted index with BM25 over the pages of documents, the
// deadline watcher's rules (German and English dates) and the recognition
// of letters and invoices for the paperwork autopilot. Pure Go, no
// network, no model: the caller extracts the text (pdftotext, office
// readers, the picture model) and hands it in.
package docindex

import (
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Page is the text of one page of a document (1 for files without pages).
type Page struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

// Doc is one indexed file.
type Doc struct {
	Root  string    `json:"root"`
	Path  string    `json:"path"`
	Size  int64     `json:"size"`
	MTime time.Time `json:"mtime"`
	// Source tells how the text came: "text" (text layer, plain text,
	// office) or "vision" (the picture model read a scan).
	Source string `json:"source,omitempty"`
	Pages  []Page `json:"pages"`
}

// Key is how a document is found again: root and path.
func (d Doc) Key() string { return d.Root + "\x00" + d.Path }

// MaxDocText bounds the text kept per document (the snippets come from it).
const MaxDocText = 24 << 10

// Index holds the documents of one account. Search builds the postings
// lazily and keeps them until the documents change.
type Index struct {
	Docs []Doc `json:"docs"`

	built    bool
	postings map[string][]posting
	lengths  []int // tokens per unit
	units    []unit
	avg      float64
}

type unit struct{ doc, page int }
type posting struct {
	unit int
	tf   int
}

// Put adds or replaces a document (by root and path), cutting its text.
func (x *Index) Put(d Doc) {
	left := MaxDocText
	pages := d.Pages[:0:0]
	for _, p := range d.Pages {
		if left <= 0 {
			break
		}
		t := strings.TrimSpace(p.Text)
		if len(t) > left {
			t = cutUTF8(t, left)
		}
		left -= len(t)
		pages = append(pages, Page{N: p.N, Text: t})
	}
	d.Pages = pages
	if i := slices.IndexFunc(x.Docs, func(o Doc) bool { return o.Key() == d.Key() }); i >= 0 {
		x.Docs[i] = d
	} else {
		x.Docs = append(x.Docs, d)
	}
	x.built = false
}

// Get returns the document of a root and path.
func (x *Index) Get(root, p string) (Doc, bool) {
	for _, d := range x.Docs {
		if d.Root == root && d.Path == p {
			return d, true
		}
	}
	return Doc{}, false
}

// Remove drops the documents for which drop says so.
func (x *Index) Remove(drop func(Doc) bool) int {
	n := len(x.Docs)
	x.Docs = slices.DeleteFunc(x.Docs, drop)
	if len(x.Docs) != n {
		x.built = false
	}
	return n - len(x.Docs)
}

func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && !utf8Start(s[n]) {
		n--
	}
	return s[:n]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// Tokens splits text into lower-case words with a light stemming for German
// and English, so that "Kündigung", "Kündigungen" and "kündigen" meet.
func Tokens(text string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		w := b.String()
		b.Reset()
		if len([]rune(w)) < 2 {
			return
		}
		out = append(out, stem(w))
	}
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(fold(r))
		default:
			flush()
		}
	}
	flush()
	return out
}

// fold makes ä a, ö o, ü u and ß s, so that "Kundigung" finds "Kündigung".
func fold(r rune) rune {
	switch r {
	case 'ä', 'à', 'á', 'â':
		return 'a'
	case 'ö', 'ò', 'ó', 'ô':
		return 'o'
	case 'ü', 'ù', 'ú', 'û':
		return 'u'
	case 'é', 'è', 'ê':
		return 'e'
	case 'ß':
		return 's'
	}
	return r
}

var suffixes = []string{"ungen", "ung", "en", "er", "es", "e", "s", "n"}

func stem(w string) string {
	if len(w) < 6 || strings.IndexFunc(w, unicode.IsDigit) >= 0 {
		return w
	}
	for _, s := range suffixes {
		if strings.HasSuffix(w, s) && len(w)-len(s) >= 4 {
			return w[:len(w)-len(s)]
		}
	}
	return w
}

func (x *Index) build() {
	if x.built {
		return
	}
	x.postings = map[string][]posting{}
	x.units, x.lengths = nil, nil
	total := 0
	for di, d := range x.Docs {
		name := strings.TrimSuffix(lastPart(d.Path), extOf(d.Path))
		for pi, p := range d.Pages {
			u := len(x.units)
			x.units = append(x.units, unit{di, pi})
			counts := map[string]int{}
			toks := Tokens(p.Text)
			if pi == 0 {
				// The file name counts on the first page.
				toks = append(toks, Tokens(name)...)
			}
			for _, t := range toks {
				counts[t]++
			}
			for t, c := range counts {
				x.postings[t] = append(x.postings[t], posting{u, c})
			}
			x.lengths = append(x.lengths, len(toks))
			total += len(toks)
		}
	}
	if len(x.units) > 0 {
		x.avg = float64(total) / float64(len(x.units))
	}
	x.built = true
}

func lastPart(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func extOf(p string) string {
	name := lastPart(p)
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[i:]
	}
	return ""
}

// Hit is a page that matches a question, with a snippet as source.
type Hit struct {
	Root    string  `json:"root"`
	Path    string  `json:"path"`
	Page    int     `json:"page"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
	Source  string  `json:"source,omitempty"`
}

// Search ranks pages with BM25 (k1 1.2, b 0.75); keep decides which
// documents may be shown (rights, folder rules), limit how many hits.
func (x *Index) Search(query string, limit int, keep func(Doc) bool) []Hit {
	x.build()
	terms := Tokens(query)
	slices.Sort(terms)
	terms = slices.Compact(terms)
	if len(terms) == 0 || len(x.units) == 0 {
		return nil
	}
	const k1, b = 1.2, 0.75
	n := float64(len(x.units))
	scores := map[int]float64{}
	for _, t := range terms {
		list := x.postings[t]
		if len(list) == 0 {
			continue
		}
		idf := math.Log(1 + (n-float64(len(list))+0.5)/(float64(len(list))+0.5))
		for _, p := range list {
			l := float64(x.lengths[p.unit])
			tf := float64(p.tf)
			scores[p.unit] += idf * tf * (k1 + 1) / (tf + k1*(1-b+b*l/max(x.avg, 1)))
		}
	}
	type scored struct {
		u int
		s float64
	}
	var ranked []scored
	for u, s := range scores {
		ranked = append(ranked, scored{u, s})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].s != ranked[j].s {
			return ranked[i].s > ranked[j].s
		}
		return ranked[i].u < ranked[j].u
	})
	var hits []Hit
	seen := map[int]int{} // pages per document
	for _, r := range ranked {
		if len(hits) >= limit {
			break
		}
		u := x.units[r.u]
		d := x.Docs[u.doc]
		if keep != nil && !keep(d) {
			continue
		}
		if seen[u.doc] >= 2 {
			continue
		}
		seen[u.doc]++
		p := d.Pages[u.page]
		hits = append(hits, Hit{Root: d.Root, Path: d.Path, Page: p.N, Snippet: Snippet(p.Text, terms, 240), Score: math.Round(r.s*100) / 100, Source: d.Source})
	}
	return hits
}

// Snippet cuts about width bytes of text around the first place where a
// term occurs, on whole words, with the whitespace folded.
func Snippet(text string, terms []string, width int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	want := map[string]bool{}
	for _, t := range terms {
		want[t] = true
	}
	at := 0
	for i, w := range words {
		if toks := Tokens(w); len(toks) > 0 && want[toks[0]] {
			at = i
			break
		}
	}
	start := max(0, at-8)
	var b strings.Builder
	if start > 0 {
		b.WriteString("… ")
	}
	i := start
	for ; i < len(words) && b.Len() < width; i++ {
		if i > start {
			b.WriteByte(' ')
		}
		b.WriteString(words[i])
	}
	if i < len(words) {
		b.WriteString(" …")
	}
	return b.String()
}
