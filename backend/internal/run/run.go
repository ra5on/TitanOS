// Package run executes system commands in a way tests can replace.
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner runs a system command and returns what it printed on stdout.
//
// What the command printed on stderr never ends up in the returned string,
// so warnings cannot sneak into parsed values (a UUID, JSON). On failure
// the returned string is still stdout only; the error is a *CommandError
// whose Output carries stderr and stdout for messages and logs.
type Runner interface {
	Run(ctx context.Context, stdin string, name string, args ...string) (string, error)
}

// CombinedRunner also runs a command with stdout and stderr in one string,
// for output that is shown to people as it is (job logs, diagnostics) and
// never parsed.
type CombinedRunner interface {
	RunCombined(ctx context.Context, stdin string, name string, args ...string) (string, error)
}

// Combined runs a command through r and returns stdout and stderr together
// when r can do that, otherwise what r.Run returns. Test fakes usually only
// implement Run and take the second path.
func Combined(ctx context.Context, r Runner, stdin string, name string, args ...string) (string, error) {
	if c, ok := r.(CombinedRunner); ok {
		return c.RunCombined(ctx, stdin, name, args...)
	}
	return r.Run(ctx, stdin, name, args...)
}

// LineRunner is a CombinedRunner that also hands over every line of the
// output as it is printed, for the progress of commands that take minutes.
type LineRunner interface {
	RunCombinedLines(ctx context.Context, onLine func(string), name string, args ...string) (string, error)
}

// CombinedLines runs a command like Combined and calls onLine with every
// line of its output, without surrounding blanks: as the line is printed
// when r can do that, otherwise all at the end. Lines end at "\n" or "\r".
func CombinedLines(ctx context.Context, r Runner, onLine func(string), name string, args ...string) (string, error) {
	if l, ok := r.(LineRunner); ok {
		return l.RunCombinedLines(ctx, onLine, name, args...)
	}
	out, err := Combined(ctx, r, "", name, args...)
	w := &lineWriter{onLine: onLine}
	_, _ = w.Write([]byte(out)) // writes into memory, cannot fail
	w.flush()
	return out, err
}

// lineWriter keeps everything written and calls onLine for every line.
type lineWriter struct {
	all    bytes.Buffer
	part   []byte
	onLine func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.all.Write(p)
	for _, b := range p {
		if b == '\n' || b == '\r' {
			w.flush()
			continue
		}
		w.part = append(w.part, b)
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if line := strings.TrimSpace(string(w.part)); line != "" {
		w.onLine(line)
	}
	w.part = w.part[:0]
}

// Exec runs commands on the real system.
type Exec struct{}

func (Exec) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return separate(ctx, cmd, name, args)
}

// RunCombined runs a command and returns stdout and stderr interleaved, as
// the terminal would show them. Only for output that is not parsed.
func (Exec) RunCombined(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := wait(ctx, cmd); err != nil {
		if errors.Is(err, ErrAbandoned) {
			return "", &CommandError{Command: commandLine(name, args), Err: err}
		}
		return output.String(), &CommandError{Command: commandLine(name, args), Output: strings.TrimSpace(output.String()), Err: err}
	}
	return output.String(), nil
}

// RunCombinedLines is RunCombined that also calls onLine for every line as
// the command prints it. stdout and stderr share one writer, so onLine is
// never called twice at the same time.
func (Exec) RunCombinedLines(ctx context.Context, onLine func(string), name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output := &lineWriter{onLine: onLine}
	cmd.Stdout, cmd.Stderr = output, output
	if err := wait(ctx, cmd); err != nil {
		if errors.Is(err, ErrAbandoned) {
			return "", &CommandError{Command: commandLine(name, args), Err: err}
		}
		output.flush()
		return output.all.String(), &CommandError{Command: commandLine(name, args), Output: strings.TrimSpace(output.all.String()), Err: err}
	}
	output.flush()
	return output.all.String(), nil
}

// RunSecret runs a command that needs secrets, such as the passphrase of an
// encrypted volume. stdin is fed as given; each entry of extra is readable
// on its own descriptor, the first as /dev/fd/3. Secrets never appear on
// the command line, and a CommandError carries only the arguments and what
// the command printed, never its input. Like Run, it returns stdout only.
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
			_, _ = w.Write(secret) // a short write shows as a failing command
			w.Close()
		}(w, secret)
	}
	return separate(ctx, cmd, name, args)
}

// abandonAfter is how long a command may take to end once its context is
// done before Run gives up waiting for it (replaced in tests).
var abandonAfter = 15 * time.Second

// ErrAbandoned says that a command did not end after its context was done
// and was left behind: typically a process stuck in uninterruptible sleep
// (D state) on a hanging disk, which not even SIGKILL ends. The caller can
// go on; the process stays until the kernel lets it go.
var ErrAbandoned = errors.New("run: command did not end after it was cancelled and was left behind")

// separate runs cmd with stdout and stderr in their own buffers.
func separate(ctx context.Context, cmd *exec.Cmd, name string, args []string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := wait(ctx, cmd); err != nil {
		if errors.Is(err, ErrAbandoned) {
			return "", &CommandError{Command: commandLine(name, args), Err: err}
		}
		return stdout.String(), &CommandError{Command: commandLine(name, args), Output: joinOutput(stderr.String(), stdout.String()), Err: err}
	}
	return stdout.String(), nil
}

// wait runs cmd to its end. When its context is done, CommandContext kills
// the process; if it still has not ended abandonAfter later, wait returns
// ErrAbandoned without it. The buffers of cmd must not be read then: the
// process may still write.
//
// No WaitDelay: a command that ends normally while a child it left behind
// keeps stdout open is still waited for, as before.
func wait(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	select {
	case err := <-done:
		return err
	case <-time.After(abandonAfter):
		return fmt.Errorf("%w: %v", ErrAbandoned, ctx.Err())
	}
}

// joinOutput is what a failed command printed, stderr first: that is where
// the reason usually is, while some tools (smartctl, sfdisk) explain
// themselves on stdout.
func joinOutput(stderr, stdout string) string {
	stderr, stdout = strings.TrimSpace(stderr), strings.TrimSpace(stdout)
	switch {
	case stderr == "":
		return stdout
	case stdout == "":
		return stderr
	}
	return stderr + "\n" + stdout
}

func commandLine(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
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
