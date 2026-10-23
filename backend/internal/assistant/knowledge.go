package assistant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Knowledge is explicit account-owned material, never inferred from chats.
// Source definitions authorize searches, not access beyond current file rights.
type KnowledgeEntry struct {
	ID       string    `json:"id"`
	Revision int       `json:"revision"`
	Kind     string    `json:"kind"` // fact, preference, rule, source, template, project
	Title    string    `json:"title"`
	Content  string    `json:"content,omitempty"`
	Origin   string    `json:"origin,omitempty"`
	Root     string    `json:"root,omitempty"`
	Path     string    `json:"path,omitempty"`
	Updated  time.Time `json:"updated"`
}

func (e KnowledgeEntry) Validate() error {
	if !slices.Contains([]string{"fact", "preference", "rule", "source", "template", "project", "model_test"}, e.Kind) || strings.TrimSpace(e.Title) == "" || len(e.Title) > 200 || len(e.Content) > 8000 || len(e.Origin) > 500 || len(e.Root) > 200 || len(e.Path) > 1000 {
		return ErrInvalid
	}
	if e.Kind == "source" && (e.Root == "" || strings.HasPrefix(e.Path, "/") || slices.Contains(strings.Split(e.Path, "/"), "..")) {
		return ErrInvalid
	}
	return nil
}

type Knowledge struct {
	mu  sync.Mutex
	Dir func() (string, bool)
}

func NewKnowledge(dir func() (string, bool)) *Knowledge { return &Knowledge{Dir: dir} }

func (k *Knowledge) update(user string, fn func(*[]KnowledgeEntry) error) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !accountRE.MatchString(user) {
		return ErrInvalid
	}
	base, ok := k.Dir()
	if !ok {
		return fmt.Errorf("%w: knowledge requires a persistent volume", ErrInvalid)
	}
	dir := filepath.Join(base, user, "knowledge")
	file := filepath.Join(dir, "entries.json")
	list := []KnowledgeEntry{}
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err = json.Unmarshal(data, &list); err != nil {
			return err
		}
	}
	before, _ := json.Marshal(list)
	if err = fn(&list); err != nil {
		return err
	}
	after, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if string(before) == string(after) {
		return nil
	}
	current, available := k.Dir()
	if !available || current != base {
		return fmt.Errorf("knowledge volume changed")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return writeFileAtomic(file, after)
}
func (k *Knowledge) List(user string) ([]KnowledgeEntry, error) {
	var out []KnowledgeEntry
	err := k.update(user, func(list *[]KnowledgeEntry) error { out = *list; return nil })
	return out, err
}
func (k *Knowledge) Put(user string, e KnowledgeEntry) (KnowledgeEntry, error) {
	if err := e.Validate(); err != nil {
		return e, err
	}
	err := k.update(user, func(list *[]KnowledgeEntry) error {
		e.Updated = time.Now()
		if e.ID == "" {
			if len(*list) >= 200 {
				return ErrChatFull
			}
			e.ID = newID()
			e.Revision = 1
			*list = append(*list, e)
			return nil
		}
		for i, old := range *list {
			if old.ID == e.ID {
				if old.Revision != e.Revision {
					return fmt.Errorf("%w: knowledge changed; reload", ErrInvalid)
				}
				e.Revision++
				(*list)[i] = e
				return nil
			}
		}
		return ErrChatNotFound
	})
	return e, err
}
func (k *Knowledge) Delete(user, id string, revision int) error {
	return k.update(user, func(list *[]KnowledgeEntry) error {
		for i, e := range *list {
			if e.ID == id {
				if e.Revision != revision {
					return ErrInvalid
				}
				*list = append((*list)[:i], (*list)[i+1:]...)
				return nil
			}
		}
		return ErrChatNotFound
	})
}
