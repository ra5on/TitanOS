package assistant

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// MaxChats is how many chats an account keeps; the oldest go first.
	MaxChats = 200
	// MaxChatBytes is how large one chat may grow.
	MaxChatBytes = 12 << 20
)

var (
	ErrChatNotFound = errors.New("assistant: no such chat")
	ErrChatFull     = errors.New("assistant: the chat is full")
	ErrBusy         = errors.New("assistant: the chat is answering")
)

// Chat is one conversation of an account.
type Run struct {
	Stage   string    `json:"stage,omitempty"`
	Tool    string    `json:"tool,omitempty"`
	Since   time.Time `json:"since,omitempty"`
	State   string    `json:"state"`
	Detail  string    `json:"detail,omitempty"`
	Started time.Time `json:"started"`
	Wake    time.Time `json:"wake,omitempty"`
}

// Stats add up what the work of a chat cost, for the person to see. Older
// versions ignore them.
type Stats struct {
	Rounds    int   `json:"rounds"`
	Prompt    int   `json:"prompt"`    // tokens read by the model
	Generated int   `json:"generated"` // tokens written by the model
	ModelMS   int64 `json:"modelMs"`
	ToolMS    int64 `json:"toolMs"`
	Tools     int   `json:"tools"`
}

type Chat struct {
	Priority   int             `json:"priority,omitempty"`
	DependsOn  []string        `json:"dependsOn,omitempty"`
	Stats      *Stats          `json:"stats,omitempty"`
	Automation string          `json:"automation,omitempty"`
	Execution  string          `json:"execution,omitempty"`
	Notify     string          `json:"notify,omitempty"`
	Attempts   map[string]bool `json:"attempts,omitempty"`
	Run        *Run            `json:"run,omitempty"`
	DryRun     bool            `json:"dryRun,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	Archived   int             `json:"archived,omitempty"`

	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Model    string    `json:"model,omitempty"`
	Messages []Entry   `json:"messages"`
	// Task is the approved job the model works on, if any.
	Task *Task `json:"task,omitempty"`
}

// Summary is a chat in the list.
type Summary struct {
	Priority  int       `json:"priority,omitempty"`
	DependsOn []string  `json:"dependsOn,omitempty"`
	Run       *Run      `json:"run,omitempty"`
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Messages  int       `json:"messages"`
}

func (c *Chat) summary() Summary {
	return Summary{Priority: c.Priority, DependsOn: slices.Clone(c.DependsOn), Run: c.Run, ID: c.ID, Title: c.Title, Created: c.Created, Updated: c.Updated, Messages: len(c.Messages)}
}

// Entry is one message of a chat: of the person (user), of the model
// (assistant) or the result of a tool the model called (tool).
type Entry struct {
	DurationMS int64     `json:"durationMs,omitempty"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	Time       time.Time `json:"time"`
	Thinking   string    `json:"thinking,omitempty"`

	// user
	Attachments []Attachment `json:"attachments,omitempty"`
	Page        string       `json:"page,omitempty"`

	// assistant
	Calls   []Call   `json:"calls,omitempty"`
	Stopped bool     `json:"stopped,omitempty"`
	Problem *Problem `json:"problem,omitempty"`
	Sources []Source `json:"sources,omitempty"`

	// tool
	Tool      string          `json:"tool,omitempty"`
	CallID    string          `json:"callId,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
	OK        bool            `json:"ok,omitempty"`
	Action    *Action         `json:"action,omitempty"`
}

// Call is a tool call of the model.
type Call struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// Problem is why an answer did not come.
type Problem struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// Attachment is a file the person gave the assistant: text or an image.
type Attachment struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // text, image; nas only from the browser
	// Root and Path name a file on the NAS (kind nas); titand reads it with
	// the rights of the account and turns it into text or an image.
	Root  string `json:"root,omitempty"`
	Path  string `json:"path,omitempty"`
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"` // base64
	Size  int    `json:"size"`
}

var (
	chatIDRE  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	accountRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
)

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Chats keeps the chats of all accounts in a folder on a volume, or in
// memory until the next start of titand when there is none. Never on the
// stick.
type Chats struct {
	// Dir returns the folder of all chats, or false without a volume.
	Dir func() (string, bool)
	now func() time.Time

	mu   sync.Mutex
	mem  map[string]map[string]*Chat
	busy map[string]bool
}

// NewChats returns a store; dir says where chats are kept.
func NewChats(dir func() (string, bool)) *Chats {
	return &Chats{Dir: dir, now: time.Now, mem: map[string]map[string]*Chat{}, busy: map[string]bool{}}
}

// Persistent reports whether chats outlive a restart of titand.
func (c *Chats) Persistent() bool {
	_, ok := c.dir()
	return ok
}

func (c *Chats) dir() (string, bool) {
	if c.Dir == nil {
		return "", false
	}
	return c.Dir()
}

func (c *Chats) userDir(user string) (string, bool, error) {
	if !accountRE.MatchString(user) {
		return "", false, ErrInvalid
	}
	base, ok := c.dir()
	if !ok {
		return "", false, nil
	}
	return filepath.Join(base, user), true, nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (c *Chats) readIndex(dir string) map[string]Summary {
	index := map[string]Summary{}
	if data, err := os.ReadFile(filepath.Join(dir, "index.json")); err == nil && json.Unmarshal(data, &index) == nil {
		return index
	}
	// No or a broken index: read the chats themselves.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !chatIDRE.MatchString(id) {
			continue
		}
		if chat, err := readChat(filepath.Join(dir, entry.Name())); err == nil {
			index[id] = chat.summary()
		}
	}
	return index
}

func (c *Chats) writeIndex(dir string, index map[string]Summary) error {
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "index.json"), data)
}

func readChat(path string) (*Chat, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrChatNotFound
		}
		return nil, err
	}
	var chat Chat
	if err := decodeChat(data, &chat); err != nil {
		return nil, err
	}
	return &chat, nil
}

func sortSummaries(list []Summary) []Summary {
	slices.SortFunc(list, func(a, b Summary) int { return b.Updated.Compare(a.Updated) })
	return list
}

// List returns the chats of an account, newest first. A search text keeps
// those whose title or messages contain it.
func (c *Chats) List(user, search string) ([]Summary, error) {
	search = strings.ToLower(strings.TrimSpace(search))
	c.mu.Lock()
	defer c.mu.Unlock()
	dir, persistent, err := c.userDir(user)
	if err != nil {
		return nil, err
	}
	list := []Summary{}
	if !persistent {
		for _, chat := range c.mem[user] {
			if search == "" || chatContains(chat, search) {
				list = append(list, chat.summary())
			}
		}
		return sortSummaries(list), nil
	}
	for id, summary := range c.readIndex(dir) {
		if search != "" && !strings.Contains(strings.ToLower(summary.Title), search) {
			chat, err := readChat(filepath.Join(dir, id+".json"))
			if err != nil || !chatContains(chat, search) {
				continue
			}
		}
		list = append(list, summary)
	}
	return sortSummaries(list), nil
}

func chatContains(chat *Chat, search string) bool {
	if strings.Contains(strings.ToLower(chat.Title), search) {
		return true
	}
	for _, m := range chat.Messages {
		if m.Role != "tool" && strings.Contains(strings.ToLower(m.Content), search) {
			return true
		}
	}
	return false
}

// Get returns one chat.
func (c *Chats) Get(user, id string) (*Chat, error) {
	if !chatIDRE.MatchString(id) {
		return nil, ErrChatNotFound
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(user, id)
}

func (c *Chats) get(user, id string) (*Chat, error) {
	dir, persistent, err := c.userDir(user)
	if err != nil {
		return nil, err
	}
	if !persistent {
		chat, ok := c.mem[user][id]
		if !ok {
			return nil, ErrChatNotFound
		}
		// A copy: the caller changes it before Save.
		data, _ := json.Marshal(chat)
		var copied Chat
		_ = decodeChat(data, &copied)
		return &copied, nil
	}
	return readChat(filepath.Join(dir, id+".json"))
}

// New starts an empty chat.
func (c *Chats) New(user, title string) (*Chat, error) {
	now := c.now()
	chat := &Chat{ID: newID(), Title: title, Created: now, Updated: now, Messages: []Entry{}}
	return chat, c.Save(user, chat)
}

// Save stores a chat, and forgets the oldest when there are too many.
func (c *Chats) Save(user string, chat *Chat) error {
	data, err := json.Marshal(chat)
	if err != nil {
		return err
	}
	if len(data) > MaxChatBytes {
		return ErrChatFull
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	dir, persistent, err := c.userDir(user)
	if err != nil {
		return err
	}
	if !persistent {
		var copied Chat
		_ = decodeChat(data, &copied)
		if c.mem[user] == nil {
			c.mem[user] = map[string]*Chat{}
		}
		c.mem[user][chat.ID] = &copied
		for len(c.mem[user]) > MaxChats {
			oldest := ""
			for id, other := range c.mem[user] {
				if c.busy[user+"/"+id] || resumable(other.Run) {
					continue
				}
				if oldest == "" || other.Updated.Before(c.mem[user][oldest].Updated) {
					oldest = id
				}
			}
			if oldest == "" {
				break
			}
			delete(c.mem[user], oldest)
		}
		return nil
	}
	if err := writeFileAtomic(filepath.Join(dir, chat.ID+".json"), data); err != nil {
		return err
	}
	index := c.readIndex(dir)
	index[chat.ID] = chat.summary()
	for len(index) > MaxChats {
		oldest := ""
		for id, other := range index {
			if c.busy[user+"/"+id] || resumable(other.Run) {
				continue
			}
			if oldest == "" || other.Updated.Before(index[oldest].Updated) {
				oldest = id
			}
		}
		if oldest == "" {
			break
		}
		os.Remove(filepath.Join(dir, oldest+".json"))
		os.RemoveAll(filepath.Join(dir, "receipts", oldest))
		delete(index, oldest)
	}
	return c.writeIndex(dir, index)
}

// Delete removes one chat.
func (c *Chats) Delete(user, id string) error {
	if !chatIDRE.MatchString(id) {
		return ErrChatNotFound
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy[user+"/"+id] {
		return ErrBusy
	}
	dir, persistent, err := c.userDir(user)
	if err != nil {
		return err
	}
	if !persistent {
		if _, ok := c.mem[user][id]; !ok {
			return ErrChatNotFound
		}
		delete(c.mem[user], id)
		return nil
	}
	if err := os.Remove(filepath.Join(dir, id+".json")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrChatNotFound
		}
		return err
	}
	index := c.readIndex(dir)
	delete(index, id)
	_ = os.RemoveAll(filepath.Join(dir, "receipts", id))
	return c.writeIndex(dir, index)
}

// ForgetUser removes all chats of an account, wherever they are.
func (c *Chats) ForgetUser(user string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, busy := range c.busy {
		if busy && strings.HasPrefix(key, user+"/") {
			return ErrBusy
		}
	}
	delete(c.mem, user)
	dir, persistent, err := c.userDir(user)
	if err != nil || !persistent {
		return err
	}
	return os.RemoveAll(dir)
}

// ForgetChats leaves explicitly saved knowledge and notification queues intact.
func (c *Chats) ForgetChats(user string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, busy := range c.busy {
		if busy && strings.HasPrefix(key, user+"/") {
			return ErrBusy
		}
	}
	dir, persistent, err := c.userDir(user)
	if err != nil {
		return err
	}
	delete(c.mem, user)
	if !persistent {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && (entry.Name() == "index.json" || chatIDRE.MatchString(strings.TrimSuffix(entry.Name(), ".json")) && strings.HasSuffix(entry.Name(), ".json")) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return os.RemoveAll(filepath.Join(dir, "receipts"))
}

// Reserve marks a chat as answering, so that two answers never run at
// once. The function returned frees it.
func (c *Chats) Reserve(user, id string) (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := user + "/" + id
	if c.busy[key] {
		return nil, ErrBusy
	}
	c.busy[key] = true
	return func() {
		c.mu.Lock()
		delete(c.busy, key)
		c.mu.Unlock()
	}, nil
}

// Busy reports whether a chat is answering right now.
func (c *Chats) Busy(user, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.busy[user+"/"+id]
}

// titleOf makes a title from the first question.
func titleOf(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > 60 {
		return strings.TrimSpace(string(r[:60])) + " …"
	}
	return text
}

// Usage is read on demand and includes this account's chat and receipt files.
func (c *Chats) Usage(user string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	dir, persistent, err := c.userDir(user)
	if err != nil {
		return 0, err
	}
	var size int64
	if !persistent {
		for _, chat := range c.mem[user] {
			b, _ := json.Marshal(chat)
			size += int64(len(b))
		}
		return size, nil
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, e := d.Info()
			if e != nil {
				return e
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func decodeChat(data []byte, c *Chat) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(c)
}
