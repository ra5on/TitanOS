// Package personalagent stores the account-scoped Agent's work independently of legacy conversations.
package personalagent

import (
	"encoding/json"
	"errors"
	"time"
)

// Feature names the private data directory used by the volume manager.
// Its identifiers follow the 12-byte volume naming contract.
const Feature = "agent-v2"

// ErrNoEffect means the adapter verified that the attempted external action had no effect.
var ErrNoEffect = errors.New("native action was rejected before any change")

type Level string

const (
	Never Level = "never"
	Ask   Level = "ask"
	Auto  Level = "auto"
)

type Source struct {
	Kind  string            `json:"kind"`
	ID    string            `json:"id"`
	Label string            `json:"label,omitempty"`
	Ref   map[string]string `json:"ref,omitempty"`
}
type Request struct {
	// Origin is assigned by the local authenticated HTTP handler for links back to selected originals.
	Origin string `json:"-"`
	// PreviousText is inert data from a preceding step and cannot be posted by a client.
	PreviousText string          `json:"-"`
	Tool         string          `json:"tool"`
	Prompt       string          `json:"prompt,omitempty"`
	Sources      []Source        `json:"sources,omitempty"`
	Args         json.RawMessage `json:"args,omitempty"`
	ChatID       string          `json:"chatId,omitempty"`
	RequestID    string          `json:"requestId"`
	RunAt        time.Time       `json:"runAt,omitzero"`
}
type Preview struct {
	Silent  bool            `json:"-"`
	Title   string          `json:"title"`
	Text    string          `json:"text,omitempty"`
	Sources []Source        `json:"sources,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"` // renderable fields bound into the approval
}
type Result struct {
	Silent  bool            `json:"-"`
	Title   string          `json:"title"`
	Text    string          `json:"text,omitempty"`
	Sources []Source        `json:"sources,omitempty"`
	Link    string          `json:"link,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}
type Capability struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	App       string `json:"app"`
	Sensitive bool   `json:"sensitive"`
	Effective Level  `json:"effective"`
	Ceiling   Level  `json:"ceiling"`
	Reason    string `json:"reason,omitempty"`
}
type Audit struct {
	Time  time.Time `json:"time"`
	State string    `json:"state"`
	Tool  string    `json:"tool"`
}
type Receipt struct {
	Step  int       `json:"step"`
	Tool  string    `json:"tool"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}
type Job struct {
	CompletedSteps int       `json:"completedSteps"`
	TotalSteps     int       `json:"totalSteps"`
	Audit          []Audit   `json:"audit,omitempty"`
	Receipts       []Receipt `json:"receipts,omitempty"`
	ID             string    `json:"id"`
	Request        Request   `json:"request"`
	Status         string    `json:"status"` // planned, running, waiting, paused, done, failed
	Phase          string    `json:"phase"`  // queued, permission, preparing, approval, executing, complete, uncertain
	Preview        *Preview  `json:"preview,omitempty"`
	Token          string    `json:"token,omitempty"`
	Result         *Result   `json:"result,omitempty"`
	Error          string    `json:"error,omitempty"`
	Created        time.Time `json:"created"`
	Updated        time.Time `json:"updated"`
	Seen           bool      `json:"seen"`
}
type Chat struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Created time.Time `json:"created"`
}
type NoticePrefs struct {
	Result   bool `json:"result"`
	Blocker  bool `json:"blocker"`
	Approval bool `json:"approval"`
}
type Settings struct {
	Notifications NoticePrefs       `json:"notifications"`
	Rights        map[string]Level  `json:"rights"`
	Paused        bool              `json:"paused"`
	Theme         string            `json:"theme"`  // system, light, dark
	Motion        string            `json:"motion"` // full, subtle, off
	DotSize       int               `json:"dotSize"`
	Colors        map[string]string `json:"colors"`
}
type State struct {
	Chats        []Chat       `json:"chats"`
	Jobs         []Job        `json:"jobs"`
	Settings     Settings     `json:"settings"`
	Capabilities []Capability `json:"capabilities"`
}

type Skill struct {
	WatchState string                     `json:"watchState,omitempty"` // active, retry, revoked, paused
	PDFName    string                     `json:"pdfName,omitempty"`
	Prompt     string                     `json:"prompt,omitempty"`
	Args       json.RawMessage            `json:"args,omitempty"`
	StepArgs   map[string]json.RawMessage `json:"stepArgs,omitempty"`
	Sender     string                     `json:"sender,omitempty"`
	Expected   string                     `json:"expected,omitempty"`
	ID         string                     `json:"id"`
	Title      string                     `json:"title"`
	Steps      []string                   `json:"steps"`
	Trigger    string                     `json:"trigger"`
	RunAt      time.Time                  `json:"runAt,omitzero"`
	Sources    []Source                   `json:"sources,omitempty"`
	Approval   string                     `json:"approval"`
	BuiltIn    bool                       `json:"builtIn"`
}
