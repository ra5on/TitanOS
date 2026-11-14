package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Everything an account has with the agent lives on a data volume, below
// .titanos/agent/<account>/, never on the stick and never in the RAM in
// place of a missing volume (docs/rules/STORAGE.md). The directory name is
// "agent", not "assistant": api/retired.go removes .titanos/assistant.

// ErrNoVolume means no data volume is mounted: the agent is unavailable.
var ErrNoVolume = errors.New("agent: no data volume mounted")

// ErrNotFound is a chat, project or entry that does not exist.
var ErrNotFound = errors.New("agent: not found")

const (
	maxChatFile  = 64 << 20 // PLAN 59: older chats grew past 16 MB
	maxIndexFile = 4 << 20
	// MaxChats an account keeps; the oldest unpinned chats go first.
	MaxChats = 2000
)

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9]{8,32}$`)
	accountPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
)

// NewID returns a random identifier.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ValidID reports whether an identifier could be one of ours.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Project groups chats with its own instructions and a default place.
type Project struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Icon         string    `json:"icon,omitempty"`
	Color        string    `json:"color,omitempty"`
	Instructions string    `json:"instructions,omitempty"`
	Location     string    `json:"location,omitempty"`
	Created      time.Time `json:"created"`
	// Members are other accounts that share the project (PLAN 51): they
	// see it and its chats and start chats in it. Owner is set only in
	// answers, for a project another account shares.
	Members []string `json:"members,omitempty"`
	Owner   string   `json:"owner,omitempty"`
}

// Source is a web page an answer used.
type Source struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
}

// FileRef is a file the agent made or changed.
type FileRef struct {
	Root string `json:"root"`
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
}

// Entry is one line of a chat as the person sees it; the messages for the
// model are built from it (messages in loop.go).
type Entry struct {
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
	Role string    `json:"role"` // user, assistant, tool, note
	// Content is the text; Thinking what the model thought before.
	Content  string     `json:"content,omitempty"`
	Thinking string     `json:"thinking,omitempty"`
	Calls    []ToolCall `json:"calls,omitempty"`
	// A tool step.
	Tool     string          `json:"tool,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
	Result   string          `json:"result,omitempty"`
	OK       bool            `json:"ok,omitempty"`
	Level    string          `json:"level,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	Duration int64           `json:"durationMs,omitempty"`
	// Kind of a tool step: read, create, change or agent.
	Kind string `json:"kind,omitempty"`
	// Choices the agent offers as buttons with its question.
	Choices []string  `json:"choices,omitempty"`
	Sources []Source  `json:"sources,omitempty"`
	Files   []FileRef `json:"files,omitempty"`
	// Note entries: "memory" (something it remembered), "approved",
	// "declined", "stopped", "interrupted", "error", "limit", "timelimit",
	// "summary" (older lines summarised), "verify", "badcall", "loop".
	Note string `json:"note,omitempty"`
	// Stats of the round that wrote an assistant entry.
	Stats *Stats `json:"stats,omitempty"`
}

// Pending is a step that waits for the person: the call as it was planned,
// so that "Ausführen" runs exactly that and the model does not plan again.
type Pending struct {
	ID      string          `json:"id"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Plan    json.RawMessage `json:"plan,omitempty"`
	Level   string          `json:"level"`
	Summary string          `json:"summary"`
	Area    string          `json:"area,omitempty"`
	Kind    string          `json:"kind,omitempty"`
	// Untrusted: the step reads outside content; approved, it still marks
	// the chat (PLAN 59, it was lost on the way through the approval).
	Untrusted bool      `json:"untrusted,omitempty"`
	At        time.Time `json:"at"`
}

// States of a chat.
const (
	StateIdle        = "idle"
	StateRunning     = "running"
	StateWaiting     = "waiting"
	StateInterrupted = "interrupted"
	StateError       = "error"
)

// Chat is one conversation.
type Chat struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Project string `json:"project,omitempty"`
	Pinned  bool   `json:"pinned,omitempty"`
	// Archived chats leave the list for the Archiv (PLAN 72); they are
	// kept like pinned ones. Older versions show them in the list again.
	Archived bool      `json:"archived,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	// Schedule is the planned task that started the chat (its wake choice).
	Schedule string `json:"schedule,omitempty"`
	// Model and Think as chosen in the chat; empty uses the settings.
	Model string `json:"model,omitempty"`
	Think string `json:"think,omitempty"`
	// Origin: "web", "schedule", "event", "homeassistant".
	Origin  string   `json:"origin,omitempty"`
	State   string   `json:"state"`
	Error   string   `json:"error,omitempty"`
	Pending *Pending `json:"pending,omitempty"`
	// Tainted: the chat has read content from outside TitanOS (files,
	// logs, web pages). Web tools then need the person's approval.
	Tainted bool `json:"tainted,omitempty"`
	// OutsideSkill: a skill from outside (catalog, or saved after outside
	// content) is in the prompt (PLAN 54-G); web tools ask first, like after
	// outside content, but what the person says is not marked for it.
	OutsideSkill bool `json:"outsideSkill,omitempty"`
	// Plan is the plan the agent keeps for the current task (plan_update).
	Plan []PlanStep `json:"plan,omitempty"`
	// Areas of tools loaded in this chat (tools_load, PLAN 55).
	Areas   []string `json:"areas,omitempty"`
	Entries []Entry  `json:"entries"`
}

// ChatInfo is a chat in the list, without its entries.
type ChatInfo struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Project  string    `json:"project,omitempty"`
	Pinned   bool      `json:"pinned,omitempty"`
	Archived bool      `json:"archived,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	State    string    `json:"state"`
	Origin   string    `json:"origin,omitempty"`
	// Preview is the start of the last answer.
	Preview string `json:"preview,omitempty"`
	// Unread: changed since the person last looked (only in the list
	// answer, never stored).
	Unread bool `json:"unread,omitempty"`
	// Author: the account a chat of a shared project belongs to, when it
	// is not the reader's (only in answers; such chats are read-only).
	Author string `json:"author,omitempty"`
}

// Info returns the list entry of a chat.
func (c *Chat) Info() ChatInfo {
	info := ChatInfo{ID: c.ID, Title: c.Title, Project: c.Project, Pinned: c.Pinned, Archived: c.Archived, Created: c.Created, Updated: c.Updated, State: c.State, Origin: c.Origin}
	for i := len(c.Entries) - 1; i >= 0; i-- {
		e := c.Entries[i]
		if e.Role == "assistant" && strings.TrimSpace(e.Content) != "" {
			info.Preview = preview(e.Content, 140)
			break
		}
	}
	return info
}

func preview(s string, n int) string {
	s = strings.Join(strings.Fields(strings.NewReplacer("**", "", "`", "", "#", "").Replace(s)), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Memory is something the agent remembered, with where it came from.
type Memory struct {
	ID      string    `json:"id"`
	Text    string    `json:"text"`
	Project string    `json:"project,omitempty"`
	Chat    string    `json:"chat,omitempty"`
	At      time.Time `json:"at"`
	// Untrusted: the chat had read outside content when it was saved.
	Untrusted bool `json:"untrusted,omitempty"`
	// PLAN 90-4: Topic groups the memory on its page (the project names it
	// otherwise), a Pinned one never fades and survives "Gedächtnis leeren",
	// Used is the last day a message of the person touched it. Older
	// versions read the file without these fields.
	Topic  string     `json:"topic,omitempty"`
	Pinned bool       `json:"pinned,omitempty"`
	Used   *time.Time `json:"used,omitempty"`
}

// Skill is a procedure the agent follows for a kind of task.
type Skill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	When        string    `json:"when"`
	Steps       string    `json:"steps"`
	BuiltIn     bool      `json:"builtIn,omitempty"`
	Chat        string    `json:"chat,omitempty"`
	Untrusted   bool      `json:"untrusted,omitempty"`
	Updated     time.Time `json:"updated"`
	Disabled    bool      `json:"disabled,omitempty"`
	Description string    `json:"description,omitempty"`
}

// Schedule is a planned task of the agent: once, again and again, or on a
// state in Home Assistant. A reminder only sends a message (PLAN 49).
type Schedule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Task    string `json:"task"`
	Project string `json:"project,omitempty"`
	// Kind: "" a task the agent works on, "reminder" only a message,
	// "morning" the morning report (Report says what goes in).
	Kind string `json:"kind,omitempty"`
	// Every: "once" (Date and At), "minutes"/"hours"/"days" (every
	// Interval, days at At), "hourly", "daily", "weekdays" (Weekdays at
	// At), "weekly", "monthly", "cron" (Cron, five fields), "trigger".
	// Weekday 0–6 (Sunday 0) for weekly, Day 1–28 for monthly.
	Every    string `json:"every"`
	At       string `json:"at,omitempty"`
	Date     string `json:"date,omitempty"`
	Interval int    `json:"interval,omitempty"`
	Weekdays []int  `json:"weekdays,omitempty"`
	Cron     string `json:"cron,omitempty"`
	Weekday  int    `json:"weekday,omitempty"`
	Day      int    `json:"day,omitempty"`
	// Report: what the morning report contains.
	Report  *Report   `json:"report,omitempty"`
	Enabled bool      `json:"enabled"`
	LastRun time.Time `json:"lastRun,omitzero"`
	LastOK  bool      `json:"lastOk,omitempty"`
	Chat    string    `json:"chat,omitempty"`
	// Trigger "homeassistant": runs when an entity reaches a state.
	Trigger *Trigger `json:"trigger,omitempty"`
	// Button offers the task as a button in Home Assistant (MQTT).
	Button bool `json:"button,omitempty"`
	// Wake: whether the task may wake a sleeping disk (PLAN 53, 72): ""
	// as the account chose (Eigeninitiative), "wake", "ask" or "never".
	Wake    string    `json:"wake,omitempty"`
	Created time.Time `json:"created"`
}

// Report is what the morning report of an account contains (PLAN 49);
// each person sets it for themselves.
type Report struct {
	NAS     bool   `json:"nas,omitempty"`
	Backups bool   `json:"backups,omitempty"`
	Today   bool   `json:"today,omitempty"`
	Home    bool   `json:"home,omitempty"`
	Extra   string `json:"extra,omitempty"`
}

// Trigger starts a schedule from Home Assistant.
type Trigger struct {
	// Connection is the Home Assistant connection by its ID; empty is the
	// first one (files of older versions).
	Connection string `json:"connection,omitempty"`
	Entity     string `json:"entity"`
	// State is the state the entity has to reach ("on"); empty with Above
	// or Below, which then decide alone.
	State string `json:"state"`
	// Above and Below are thresholds for entities with numbers
	// (temperature, power, humidity): the trigger fires when the value
	// crosses the threshold, not for every value beyond it.
	Above *float64 `json:"above,omitempty"`
	Below *float64 `json:"below,omitempty"`
	// For keeps the state that long (minutes) before it counts.
	For int `json:"for,omitempty"`
}

// Store keeps the files of all accounts.
type Store struct {
	// Open returns .titanos/agent on a data volume.
	Open func() (*os.Root, error)
	mu   sync.Mutex
	// schedules are kept in RAM once read: the scheduler looks every
	// minute, and reading the volume that often would keep its disks
	// awake (docs/rules/STORAGE.md, Ruhemodus).
	schedules map[string][]Schedule
	scanned   bool
	// seen holds the read states once read (seen.go); updated the last
	// change of each chat as the index says, so a "seen" from the page
	// needs no read of the volume.
	seen    map[string]seenState
	updated map[string]map[string]time.Time
}

func (s *Store) userRoot(user string, create bool) (*os.Root, func(), error) {
	if !accountPattern.MatchString(user) {
		return nil, nil, ErrNotFound
	}
	if s.Open == nil {
		return nil, nil, ErrNoVolume
	}
	base, err := s.Open()
	if err != nil {
		if errors.Is(err, volumedata.ErrUnavailable) {
			return nil, nil, ErrNoVolume
		}
		return nil, nil, err
	}
	for _, dir := range []string{user, user + "/chats"} {
		if !create {
			break
		}
		if err := base.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			base.Close()
			return nil, nil, err
		}
		st, err := base.Lstat(dir)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			base.Close()
			return nil, nil, errors.New("agent: unsafe account directory")
		}
	}
	if st, err := base.Lstat(user); err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		base.Close()
		return nil, nil, ErrNotFound
	}
	root, err := base.OpenRoot(user)
	base.Close()
	if err != nil {
		return nil, nil, err
	}
	return root, func() { root.Close() }, nil
}

func (s *Store) read(user, name string, target any, limit int64) error {
	root, done, err := s.userRoot(user, false)
	if err != nil {
		return err
	}
	defer done()
	err = volumedata.ReadJSON(root, name, target, limit)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

func (s *Store) write(user, name string, value any) error {
	root, done, err := s.userRoot(user, true)
	if err != nil {
		return err
	}
	defer done()
	return volumedata.WriteJSON(root, name, value)
}

// Available reports whether the agent has a place for its files.
func (s *Store) Available() error {
	if s.Open == nil {
		return ErrNoVolume
	}
	root, err := s.Open()
	if err != nil {
		return ErrNoVolume
	}
	root.Close()
	return nil
}

// Chat reads one chat.
func (s *Store) Chat(user, id string) (*Chat, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	var c Chat
	if err := s.read(user, "chats/"+id+".json", &c, maxChatFile); err != nil {
		return nil, err
	}
	if c.Entries == nil {
		c.Entries = []Entry{}
	}
	return &c, nil
}

// SaveChat writes a chat and its line in the index.
func (s *Store) SaveChat(user string, c *Chat) error {
	if !ValidID(c.ID) {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(user, "chats/"+c.ID+".json", c); err != nil {
		return err
	}
	index, err := s.indexLocked(user)
	if err != nil {
		// The list of chats cannot be read (damaged, a read error). It is
		// not written anew with this one chat alone, which would take
		// every other chat off the list: it is put together again from
		// the chats themselves, or the save fails.
		if index, err = s.rebuildIndexLocked(user); err != nil {
			return err
		}
	}
	info := c.Info()
	replaced := false
	for i := range index {
		if index[i].ID == c.ID {
			index[i], replaced = info, true
		}
	}
	if !replaced {
		index = append(index, info)
	}
	removed := s.trimLocked(&index)
	if err := s.write(user, "chats.json", index); err != nil {
		return err
	}
	s.updatedLocked(user, index)
	for _, id := range removed {
		s.removeFile(user, "chats/"+id+".json")
	}
	return nil
}

// trimLocked keeps at most MaxChats, dropping the oldest unpinned ones;
// archived chats are kept like pinned ones.
func (s *Store) trimLocked(index *[]ChatInfo) []string {
	if len(*index) <= MaxChats {
		return nil
	}
	sort.SliceStable(*index, func(i, j int) bool { return (*index)[i].Updated.After((*index)[j].Updated) })
	var removed []string
	kept := (*index)[:0]
	for i, c := range *index {
		if i >= MaxChats && !c.Pinned && !c.Archived {
			removed = append(removed, c.ID)
			continue
		}
		kept = append(kept, c)
	}
	*index = kept
	return removed
}

func (s *Store) removeFile(user, name string) {
	root, done, err := s.userRoot(user, false)
	if err != nil {
		return
	}
	defer done()
	_ = root.Remove(name)
}

func (s *Store) indexLocked(user string) ([]ChatInfo, error) {
	var index []ChatInfo
	if err := s.read(user, "chats.json", &index, maxIndexFile); err != nil {
		if errors.Is(err, ErrNotFound) {
			s.updatedLocked(user, nil)
			return []ChatInfo{}, nil
		}
		return nil, err
	}
	s.updatedLocked(user, index)
	return index, nil
}

// rebuildIndexLocked makes the list of chats from the chat files, oldest
// change first, for a list that cannot be read.
func (s *Store) rebuildIndexLocked(user string) ([]ChatInfo, error) {
	root, done, err := s.userRoot(user, false)
	if err != nil {
		return nil, err
	}
	defer done()
	entries, err := fs.ReadDir(root.FS(), "chats")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	index := []ChatInfo{}
	for _, entry := range entries {
		id, isChat := strings.CutSuffix(entry.Name(), ".json")
		if entry.IsDir() || !isChat || !ValidID(id) {
			continue
		}
		var c Chat
		if volumedata.ReadJSON(root, "chats/"+entry.Name(), &c, maxChatFile) != nil || c.ID != id {
			continue // a chat that cannot be read stays as a file, off the list
		}
		index = append(index, c.Info())
	}
	slices.SortFunc(index, func(a, b ChatInfo) int { return a.Updated.Compare(b.Updated) })
	slog.Warn("agent: list of chats rebuilt from the chat files", "account", user, "chats", len(index))
	return index, nil
}

// updatedLocked remembers the last change of every chat of the index.
func (s *Store) updatedLocked(user string, index []ChatInfo) {
	if s.updated == nil {
		s.updated = map[string]map[string]time.Time{}
	}
	m := make(map[string]time.Time, len(index))
	for _, c := range index {
		m[c.ID] = c.Updated
	}
	s.updated[user] = m
}

// Chats lists the chats of an account, newest first, pinned ones on top.
func (s *Store) Chats(user string) ([]ChatInfo, error) {
	s.mu.Lock()
	index, err := s.indexLocked(user)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(index, func(i, j int) bool {
		if index[i].Pinned != index[j].Pinned {
			return index[i].Pinned
		}
		return index[i].Updated.After(index[j].Updated)
	})
	return index, nil
}

// DeleteChat removes a chat.
func (s *Store) DeleteChat(user, id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.indexLocked(user)
	if err != nil {
		return err
	}
	n := len(index)
	index = slices.DeleteFunc(index, func(c ChatInfo) bool { return c.ID == id })
	if len(index) == n {
		return ErrNotFound
	}
	if err := s.write(user, "chats.json", index); err != nil {
		return err
	}
	s.updatedLocked(user, index)
	s.removeFile(user, "chats/"+id+".json")
	if root, done, err := s.userRoot(user, false); err == nil {
		_ = root.RemoveAll("files/" + id)
		done()
	}
	return nil
}

// Projects of an account.
func (s *Store) Projects(user string) ([]Project, error) {
	var list []Project
	if err := s.read(user, "projects.json", &list, maxIndexFile); err != nil {
		if errors.Is(err, ErrNotFound) {
			return []Project{}, nil
		}
		return nil, err
	}
	return list, nil
}

// SaveProjects writes the projects of an account.
func (s *Store) SaveProjects(user string, list []Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(user, "projects.json", list)
}

// Memories of an account.
func (s *Store) Memories(user string) ([]Memory, error) {
	var list []Memory
	if err := s.read(user, "memory.json", &list, maxIndexFile); err != nil {
		if errors.Is(err, ErrNotFound) {
			return []Memory{}, nil
		}
		return nil, err
	}
	return list, nil
}

// SaveMemories writes the memory of an account.
func (s *Store) SaveMemories(user string, list []Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(user, "memory.json", list)
}

// Skills the account added or the agent wrote (built-in ones are not
// stored, see skills.go).
func (s *Store) Skills(user string) ([]Skill, error) {
	var list []Skill
	if err := s.read(user, "skills.json", &list, maxIndexFile); err != nil {
		if errors.Is(err, ErrNotFound) {
			return []Skill{}, nil
		}
		return nil, err
	}
	return list, nil
}

// SaveSkills writes the skills of an account.
func (s *Store) SaveSkills(user string, list []Skill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(user, "skills.json", list)
}

// Schedules of an account, from RAM once read.
func (s *Store) Schedules(user string) ([]Schedule, error) {
	s.mu.Lock()
	if list, ok := s.schedules[user]; ok {
		s.mu.Unlock()
		return slices.Clone(list), nil
	}
	s.mu.Unlock()
	var list []Schedule
	if err := s.read(user, "schedules.json", &list, maxIndexFile); err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		list = []Schedule{}
	}
	s.mu.Lock()
	if s.schedules == nil {
		s.schedules = map[string][]Schedule{}
	}
	s.schedules[user] = list
	s.mu.Unlock()
	return slices.Clone(list), nil
}

// SaveSchedules writes the schedules of an account.
func (s *Store) SaveSchedules(user string, list []Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(user, "schedules.json", list); err != nil {
		return err
	}
	if s.schedules == nil {
		s.schedules = map[string][]Schedule{}
	}
	s.schedules[user] = slices.Clone(list)
	return nil
}

// UpdateSchedule changes one schedule of an account in one step: the list
// as it is now is read, change is applied to the schedule with that ID and
// the list is written. Reading the list, changing a copy and saving it in
// separate steps loses what another change wrote in between (a task that
// ends while the minute tick stamps another: a "once" task would run again).
// It reports ErrNotFound when the schedule is gone.
func (s *Store) UpdateSchedule(user, id string, change func(*Schedule)) error {
	if _, err := s.Schedules(user); err != nil { // fills the RAM copy
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := slices.Clone(s.schedules[user])
	i := slices.IndexFunc(list, func(sc Schedule) bool { return sc.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	change(&list[i])
	if err := s.write(user, "schedules.json", list); err != nil {
		return err
	}
	s.schedules[user] = list
	return nil
}

// ScheduleAccounts are the accounts with recurring tasks. The volume is
// read once; after that the list comes from RAM.
func (s *Store) ScheduleAccounts() []string {
	s.mu.Lock()
	scanned := s.scanned
	s.mu.Unlock()
	if !scanned {
		accounts := s.Accounts()
		if len(accounts) > 0 || s.Available() == nil {
			for _, a := range accounts {
				_, _ = s.Schedules(a)
			}
			s.mu.Lock()
			s.scanned = true
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for user, list := range s.schedules {
		if len(list) > 0 {
			out = append(out, user)
		}
	}
	sort.Strings(out)
	return out
}

// Accounts lists the accounts that have files, for the start-up check of
// interrupted runs and for schedules.
func (s *Store) Accounts() []string {
	if s.Open == nil {
		return nil
	}
	root, err := s.Open()
	if err != nil {
		return nil
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil
	}
	defer dir.Close()
	names, _ := dir.Readdirnames(-1)
	var out []string
	for _, n := range names {
		if accountPattern.MatchString(n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// ReadShared reads a file all accounts share (the NAS map, descriptions)
// from .titanos/agent itself.
func (s *Store) ReadShared(name string, target any) error {
	if s.Open == nil {
		return ErrNoVolume
	}
	root, err := s.Open()
	if err != nil {
		return ErrNoVolume
	}
	defer root.Close()
	err = volumedata.ReadJSON(root, name, target, maxIndexFile)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// WriteShared writes a shared file.
func (s *Store) WriteShared(name string, value any) error {
	if s.Open == nil {
		return ErrNoVolume
	}
	root, err := s.Open()
	if err != nil {
		return ErrNoVolume
	}
	defer root.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	return volumedata.WriteJSON(root, name, value)
}

// Profile is what an account tells the agent about itself.
type Profile struct {
	Instruction string `json:"instruction,omitempty"`
}

// Profile reads the profile of an account.
func (s *Store) Profile(user string) (Profile, error) {
	var p Profile
	err := s.read(user, "profile.json", &p, maxIndexFile)
	if errors.Is(err, ErrNotFound) {
		return Profile{}, nil
	}
	return p, err
}

// SaveProfile writes the profile of an account.
func (s *Store) SaveProfile(user string, p Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(user, "profile.json", p)
}

var attachmentName = regexp.MustCompile(`^[^/\\\x00-\x1f.][^/\\\x00-\x1f]{0,199}$`)

// SaveAttachment keeps a file uploaded into a chat.
func (s *Store) SaveAttachment(user, chat, name string, data []byte) error {
	if !ValidID(chat) || !attachmentName.MatchString(name) {
		return ErrNotFound
	}
	root, done, err := s.userRoot(user, true)
	if err != nil {
		return err
	}
	defer done()
	dir := "files/" + chat
	for _, d := range []string{"files", dir} {
		if err := root.Mkdir(d, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	sub, err := root.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer sub.Close()
	return volumedata.Write(sub, name, data)
}

// Attachment reads a file of a chat.
func (s *Store) Attachment(user, chat, name string) ([]byte, error) {
	if !ValidID(chat) || !attachmentName.MatchString(name) {
		return nil, ErrNotFound
	}
	root, done, err := s.userRoot(user, false)
	if err != nil {
		return nil, err
	}
	defer done()
	f, err := root.Open("files/" + chat + "/" + name)
	if err != nil {
		return nil, ErrNotFound
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 32<<20 {
		return nil, ErrNotFound
	}
	return io.ReadAll(f)
}

// NotesMeta is what the notes of an account need beside their files
// (PLAN 57): the pinned ones, by their path below Notizen/.
type NotesMeta struct {
	Pinned []string `json:"pinned,omitempty"`
}

// NotesMeta reads notes.json; none is an empty one.
func (s *Store) NotesMeta(user string) (NotesMeta, error) {
	var m NotesMeta
	if err := s.read(user, "notes.json", &m, 1<<20); err != nil && !errors.Is(err, ErrNotFound) {
		return m, err
	}
	return m, nil
}

// SaveNotesMeta writes notes.json.
func (s *Store) SaveNotesMeta(user string, m NotesMeta) error {
	return s.write(user, "notes.json", m)
}
