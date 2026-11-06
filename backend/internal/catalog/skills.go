package catalog

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Skills of the KI-Agent (PLAN 54-G, decision of the owner 6.11.2026):
// a source may hold a folder skills/ next to its apps, one folder per
// skill with a file skill.yaml:
//
//	skills/rechnungen-steuerjahr/skill.yaml
//
//	name: Rechnungen nach Steuerjahr
//	description: Sortiert Belege in Steuer/<Jahr>
//	author: ra5on
//	when: Wenn Rechnungen eines Jahres zusammengesucht werden sollen
//	tools: [files_search, files_copy, note_write]
//	steps: |
//	  1. Mit files_search nach Rechnungen des Jahres suchen.
//	  2. …
//
// They come with the apps (same sources, same download, kept in RAM,
// fetched again after a start and once a day) and are trusted like them;
// there is no signature of their own. A skill that does not fit the format
// is skipped, the others stay.

// Skill is a skill of a source.
type Skill struct {
	// ID is the name of its folder.
	ID          string   `json:"id"`
	Source      string   `json:"source"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	When        string   `json:"when"`
	Steps       string   `json:"steps"`
	Tools       []string `json:"tools"`
}

const (
	maxSkillsPerSource = 500
	maxSkillName       = 80
	maxSkillText       = 4000
)

var (
	skillFile   = regexp.MustCompile(`(^|/)skills/([^/]+)/skill\.ya?ml$`)
	skillIDRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	skillToolRE = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
)

// isSkill reports whether a file of a source is a skill.
func isSkill(name string) bool { return skillFile.MatchString(name) }

// parseSkills reads the skills of a source; malformed ones are skipped.
func parseSkills(source string, files map[string]string) []Skill {
	names := make([]string, 0, len(files))
	for name := range files {
		if isSkill(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := []Skill{}
	seen := map[string]bool{}
	for _, name := range names {
		id := strings.ToLower(skillFile.FindStringSubmatch(name)[2])
		var raw struct {
			Name        string   `yaml:"name"`
			Description string   `yaml:"description"`
			Author      string   `yaml:"author"`
			When        string   `yaml:"when"`
			Steps       string   `yaml:"steps"`
			Tools       []string `yaml:"tools"`
		}
		if yaml.Unmarshal([]byte(files[name]), &raw) != nil || !skillIDRE.MatchString(id) || seen[id] {
			continue
		}
		s := Skill{ID: id, Source: source, Name: strings.TrimSpace(raw.Name), Description: clip(raw.Description, 300), Author: clip(raw.Author, 80),
			When: strings.TrimSpace(raw.When), Steps: strings.TrimSpace(raw.Steps), Tools: []string{}}
		if s.Name == "" || len(s.Name) > maxSkillName || s.When == "" || s.Steps == "" || len(s.When)+len(s.Steps) > maxSkillText || len(raw.Tools) > 40 {
			continue
		}
		valid := true
		for _, t := range raw.Tools {
			t = strings.TrimSpace(t)
			if !skillToolRE.MatchString(t) {
				valid = false
				break
			}
			if !slices.Contains(s.Tools, t) {
				s.Tools = append(s.Tools, t)
			}
		}
		if !valid {
			continue
		}
		seen[id] = true
		out = append(out, s)
		if len(out) >= maxSkillsPerSource {
			break
		}
	}
	return out
}

// SkillOf is a skill with the name of its source, as the agent shows it.
type SkillOf struct {
	Skill
	SourceName string `json:"sourceName"`
}

// Skills returns the skills of all sources, the official one first.
func (m *Manager) Skills() []SkillOf {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []SkillOf{}
	add := func(id, name string) {
		if st := m.states[id]; st != nil {
			for _, s := range st.skills {
				out = append(out, SkillOf{Skill: s, SourceName: name})
			}
		}
	}
	add(BuiltinID, "TitanOS-Apps")
	for _, source := range m.sources {
		add(source.ID, source.Name)
	}
	return out
}
