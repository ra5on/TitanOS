// Package run executes system commands in a way tests can replace.
package run

import (
	"bytes"
	"context"
	"os"
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

// RunSecret runs a command that needs secrets, such as the passphrase of an
// encrypted volume. stdin is fed as given; each entry of extra is readable
// on its own descriptor, the first as /dev/fd/3. Secrets never appear on
// the command line, and a CommandError carries only the arguments and what
// the command printed, never its input.
func (Exec) RunSecret(ctx context.Context, stdin []byte, extra [][]byte, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var writers []*os.File
	defer func() {
		for _, w := range writers {
			w.Close()
		}
	}()
	for _, secret := range extra {
		r, w, err := os.Pipe()
		if err != nil {
			return "", err
		}
		defer r.Close()
		writers = append(writers, w)
		cmd.ExtraFiles = append(cmd.ExtraFiles, r)
		// A pipe holds 64 KiB; passphrases are far smaller, but write in
		// the background anyway so that a large one cannot block.
		go func(w *os.File, secret []byte) {
			w.Write(secret)
			w.Close()
		}(w, secret)
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
