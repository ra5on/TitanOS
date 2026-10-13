// Package run executes system commands in a way tests can replace.
package run

import (
	"context"
	"os/exec"
	"strings"
)

// Runner runs a system command and returns what it printed.
type Runner interface {
	Run(ctx context.Context, stdin string, name string, args ...string) (string, error)
}

// Exec runs commands on the real system.
type Exec struct{}

func (Exec) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), &CommandError{Command: name + " " + strings.Join(args, " "), Output: strings.TrimSpace(string(output)), Err: err}
	}
	return string(output), nil
}

// CommandError is a failed system command, with what it printed.
type CommandError struct {
	Command string
	Output  string
	Err     error
}

func (e *CommandError) Error() string {
	return e.Command + ": " + e.Err.Error() + ": " + e.Output
}

func (e *CommandError) Unwrap() error { return e.Err }
