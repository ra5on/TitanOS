package toolcatalog

import (
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Manual is the documentation of TitanOS (README and docs/*.md), built
// into titand with the web UI, so that MCP clients can look up
// how to set something up instead of guessing.
type Manual struct {
	files fs.FS
}

// NewManual reads the Markdown files of fsys; nil means there is none.
func NewManual(fsys fs.FS) *Manual { return &Manual{files: fsys} }

// Section is a part of a document under one heading.
type Section struct {
	File    string `json:"file"`
	Heading string `json:"heading"`
	Text    string `json:"text"`
}

// Files lists the documents.
func (m *Manual) Files() []string {
	if m == nil || m.files == nil {
		return nil
	}
	var names []string
	_ = fs.WalkDir(m.files, ".", func(name string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(name, ".md") {
			names = append(names, name)
		}
		return nil
	})
	sort.Strings(names)
	return names
}

func (m *Manual) sections(name string) []Section {
	data, err := fs.ReadFile(m.files, name)
	if err != nil {
		return nil
	}
	var list []Section
	current := Section{File: name, Heading: strings.TrimSuffix(path.Base(name), ".md")}
	var text strings.Builder
	flush := func() {
		current.Text = strings.TrimSpace(text.String())
		if current.Text != "" {
			list = append(list, current)
		}
		text.Reset()
	}
	fence := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "```") {
			fence = !fence
		}
		if !fence && strings.HasPrefix(line, "#") {
			flush()
			current = Section{File: name, Heading: strings.TrimSpace(strings.TrimLeft(line, "#"))}
			continue
		}
		text.WriteString(line)
		text.WriteByte('\n')
	}
	flush()
	return list
}

func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Search returns the sections that fit a question best.
func (m *Manual) Search(query string, limit int) []Section {
	terms := slices.DeleteFunc(words(query), func(w string) bool { return len([]rune(w)) < 3 })
	if len(terms) == 0 {
		return nil
	}
	type hit struct {
		section Section
		score   int
	}
	var hits []hit
	for _, name := range m.Files() {
		for _, section := range m.sections(name) {
			heading, body := strings.ToLower(section.Heading), strings.ToLower(section.Text)
			score := 0
			for _, term := range terms {
				if strings.Contains(heading, term) {
					score += 5
				}
				score += min(strings.Count(body, term), 5)
			}
			if score > 0 {
				hits = append(hits, hit{section, score})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	var result []Section
	for _, h := range hits[:min(len(hits), limit)] {
		if r := []rune(h.section.Text); len(r) > 1200 {
			h.section.Text = string(r[:1200]) + " …"
		}
		result = append(result, h.section)
	}
	return result
}

// Read returns a document, or only its sections whose heading contains
// heading.
func (m *Manual) Read(name, heading string) (string, bool) {
	if !slices.Contains(m.Files(), name) {
		return "", false
	}
	if heading == "" {
		data, err := fs.ReadFile(m.files, name)
		return string(data), err == nil
	}
	var text strings.Builder
	for _, section := range m.sections(name) {
		if strings.Contains(strings.ToLower(section.Heading), strings.ToLower(heading)) {
			text.WriteString("## " + section.Heading + "\n\n" + section.Text + "\n\n")
		}
	}
	return text.String(), text.Len() > 0
}
