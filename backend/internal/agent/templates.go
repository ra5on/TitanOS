package agent

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type PromptTemplate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

var ErrTemplate = errors.New("invalid prompt template")

const templateFile = "prompt-templates.json"

func (s *Store) PromptTemplates(user string) ([]PromptTemplate, error) {
	out := []PromptTemplate{}
	err := s.read(user, templateFile, &out, 8<<20)
	if errors.Is(err, ErrNotFound) {
		return out, nil
	}
	return out, err
}

// A template contains text only. Creating a chat from it follows the same
// checks as typing that text. Mutations preserve edits to unrelated templates.
func (s *Store) ChangePromptTemplate(user string, entry PromptTemplate, remove bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	templates, err := s.PromptTemplates(user)
	if err != nil {
		return err
	}
	if remove {
		for i, t := range templates {
			if entry.ID == t.ID {
				return s.write(user, templateFile, append(templates[:i], templates[i+1:]...))
			}
		}
		return nil
	}
	entry.Name, entry.Text = strings.TrimSpace(entry.Name), strings.TrimSpace(entry.Text)
	if entry.Name == "" || utf8.RuneCountInString(entry.Name) > 80 || entry.Text == "" || len(entry.Text) > 20000 {
		return ErrTemplate
	}
	for _, t := range templates {
		if t.Name == entry.Name && t.Text == entry.Text {
			return nil
		}
	}
	if len(templates) >= 40 {
		return ErrTemplate
	}
	entry.ID = NewID()
	return s.write(user, templateFile, append(templates, entry))
}
