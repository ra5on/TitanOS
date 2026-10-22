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
	Read      func(ctx context.Context, args json.RawMessage) (Result, error)
	Plan      func(ctx context.Context, args json.RawMessage) (*Action, error)
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
	ActionPending  = "pending"
	ActionDone     = "done"
	ActionFailed   = "failed"
	ActionRejected = "rejected"
	ActionExpired  = "expired"
)

// Action is a change the model proposes. The browser shows it as a card
// and, once the person confirms it, calls the API route itself with the
// session of the account: rights, checks and the log are those of the
// button in the UI.
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
	// where the browser puts it: "body:<field>" or "query:<parameter>";
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
}
