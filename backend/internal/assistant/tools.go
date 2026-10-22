package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// Tool is something the model may call: either it reads (Read) or it
// proposes an action that a person has to confirm (Plan). An action tool
// never changes anything itself.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	// Untrusted tools return text that comes from outside TitanOS (logs,
	// files, web pages). An action proposed after reading it says so.
	Untrusted bool
	// Area is the part of the NAS the tool belongs to, for the settings of
	// what the assistant may do; empty for tools that are always there.
	Area string
	Read func(ctx context.Context, args json.RawMessage) (Result, error)
	Plan func(ctx context.Context, args json.RawMessage) (*Action, error)
}

// Result is what a reading tool returns: the text for the model and, for
// the web, where it came from.
type Result struct {
	Text    string
	Sources []Source
}

// Source is a web page an answer is based on.
type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ToolError is a mistake of the model, such as a wrong argument. Its
// message is shown to the model as is, so that it can try again.
type ToolError struct{ Message string }

func (e *ToolError) Error() string { return e.Message }

// Errorf returns a ToolError.
func Errorf(format string, args ...any) error {
	return &ToolError{Message: fmt.Sprintf(format, args...)}
}

// DecodeArgs reads the arguments of a call strictly.
func DecodeArgs(args json.RawMessage, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return Errorf("invalid arguments: %v", err)
	}
	return nil
}

// JSON returns v as the text of a Result.
func JSON(v any) (Result, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Result{}, err
	}
	return Result{Text: string(data)}, nil
}

// Confirmation levels of an action, decided by TitanOS, never by the model.
const (
	// LevelNormal needs a click.
	LevelNormal = "normal"
	// LevelDanger needs a click and a tick that names the consequences.
	LevelDanger = "danger"
	// LevelDestroy needs the name of the target typed in, as in the UI.
	LevelDestroy = "destroy"
)

// States of an action.
const (
	ActionPending   = "pending"
	ActionRunning   = "running"
	ActionUncertain = "uncertain"
	ActionDone      = "done"
	ActionFailed    = "failed"
	ActionRejected  = "rejected"
	ActionExpired   = "expired"
)

// Action is a stored proposal. The server revalidates and executes it with
// current account rights and records the actual API outcome.
type Action struct {
	ID   string `json:"id"`
	Tool string `json:"tool"`
	// Key and Params name the text of the card in the translation files:
	// assistant.action.<Key>.
	Key    string            `json:"key"`
	Params map[string]string `json:"params,omitempty"`
	Reason string            `json:"reason"`
	Level  string            `json:"level"`
	// Confirm is what has to be typed for LevelDestroy. ConfirmField says
	// where the server puts it: "body:<field>" or "query:<parameter>";
	// empty if the route itself does not ask for it.
	Confirm      string `json:"confirm,omitempty"`
	ConfirmField string `json:"confirmField,omitempty"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	// Body is sent as JSON; nil sends none.
	Body any `json:"body,omitempty"`
	// After names the tools the model read from outside TitanOS before it
	// proposed this, or "attachment".
	After  []string `json:"after,omitempty"`
	State  string   `json:"state"`
	Result string   `json:"result,omitempty"`
	// Steps are the plan of a task (Key "task"), which the person approves
	// once. Auto reflects the current execution policy; Approved records an
	// explicit approval for this particular action.
	Steps    []string `json:"steps,omitempty"`
	Auto     bool     `json:"auto,omitempty"`
	Approved bool     `json:"approved,omitempty"`
}

// Task is a job the person approved as a whole: until the model answers,
// its actions run without asking for each one.
type Task struct {
	Title    string   `json:"title"`
	Steps    []string `json:"steps"`
	Approved bool     `json:"approved"`
}
