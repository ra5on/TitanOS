// Package toolcatalog holds the fixed catalog of tools that the MCP server
// offers: reading tools that return data about the NAS and action tools
// that describe one call of the web API, which the server executes with the
// rights of the account. The tools themselves live in package api; this
// package has their types, the argument checks and the manual.
package toolcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrInvalid is a request that does not fit the catalog.
var ErrInvalid = errors.New("toolcatalog: invalid request")

// Tool is something a client may call: either it reads (Read) or it
// describes an action (Plan). An action tool never changes anything itself.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	// Untrusted tools return text that comes from outside TitanOS (logs,
	// files, container output).
	Untrusted bool
	// Area is the part of the NAS the tool belongs to, for grouping on the
	// MCP page; empty for tools that are always there.
	Area string
	Read func(ctx context.Context, args json.RawMessage) (Result, error)
	Plan func(ctx context.Context, args json.RawMessage) (*Action, error)
}

// Result is what a reading tool returns.
type Result struct {
	Text string
}

// ToolError is a mistake of the client, such as a wrong argument. Its
// message is shown to the client as is, so that it can try again.
type ToolError struct {
	Message string
	Code    string
}

func (e *ToolError) Error() string { return e.Message }

// Errorf returns a ToolError.
func Errorf(format string, args ...any) error {
	return &ToolError{Message: fmt.Sprintf(format, args...)}
}

// CodedErrorf exposes a content-free diagnostic category separately from details.
func CodedErrorf(code, format string, args ...any) error {
	return &ToolError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ToolErrorCode is the category of an error for the activity log.
func ToolErrorCode(err error) string {
	var problem *ToolError
	if errors.As(err, &problem) && problem.Code != "" {
		return problem.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "tool_timeout"
	}
	return "tool_failed"
}

// DecodeArgs reads the arguments of a call strictly.
func DecodeArgs(args json.RawMessage, v any) error {
	if len(bytes.TrimSpace(args)) == 0 || bytes.TrimSpace(args)[0] != '{' {
		return Errorf("invalid arguments: expected a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return Errorf("invalid arguments: %v", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Errorf("invalid arguments: expected exactly one JSON object")
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

// Levels of an action, decided by TitanOS, never by the client.
const (
	// LevelNormal runs when the tool is switched on.
	LevelNormal = "normal"
	// LevelDanger also needs dangerous actions allowed for the tool and
	// confirm=true from the client.
	LevelDanger = "danger"
	// LevelDestroy is like LevelDanger; the server adds the confirmation
	// value the route expects (Confirm, ConfirmField).
	LevelDestroy = "destroy"
)

// Dangerous reports whether a level needs the extra permission.
func Dangerous(level string) bool { return level == LevelDanger || level == LevelDestroy }

// Action is one call of the web API that an action tool describes. The
// server checks it again and executes it with the current rights of the
// account.
type Action struct {
	Tool string `json:"tool"`
	// Key and Params name what is done, for the activity log.
	Key    string            `json:"key"`
	Params map[string]string `json:"params,omitempty"`
	Reason string            `json:"reason,omitempty"`
	Level  string            `json:"level"`
	// Confirm is the confirmation value a route expects for LevelDestroy.
	// ConfirmField says where the server puts it: "body:<field>" or
	// "query:<parameter>"; empty if the route itself does not ask for it.
	Confirm      string `json:"confirm,omitempty"`
	ConfirmField string `json:"confirmField,omitempty"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	// Body is sent as JSON; nil sends none.
	Body any `json:"body,omitempty"`
}

// Areas of the NAS, for grouping the tools on the MCP page.
const (
	AreaFiles    = "files"
	AreaDocker   = "docker"
	AreaVMs      = "vms"
	AreaStorage  = "storage"
	AreaShares   = "shares"
	AreaBackups  = "backups"
	AreaAccounts = "accounts"
	AreaSystem   = "system"
	AreaUpdates  = "updates"
	AreaHome     = "home"
	AreaPlugins  = "plugins"
	AreaNetwork  = "network"
)

// Areas in the order the MCP page shows them.
var Areas = []string{AreaSystem, AreaStorage, AreaShares, AreaFiles, AreaDocker, AreaVMs, AreaBackups, AreaNetwork, AreaAccounts, AreaPlugins, AreaUpdates, AreaHome}
