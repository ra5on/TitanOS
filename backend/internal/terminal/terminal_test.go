package terminal

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestSettings(t *testing.T) {
	store := config.Open(t.TempDir())
	m := NewManager(store)
	if err := m.Load(); err != nil || !m.Enabled() {
		t.Fatalf("terminals are on unless switched off: %v, %v", m.Enabled(), err)
	}
	off := m.Off()
	if err := m.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-off:
	default:
		t.Error("running sessions are not told that terminals were switched off")
	}
	if _, err := m.Shell(context.Background()); !errors.Is(err, ErrOff) {
		t.Errorf("shell while switched off: %v", err)
	}
	again := NewManager(store)
	if err := again.Load(); err != nil || again.Enabled() {
		t.Errorf("the setting did not survive a restart: %v, %v", again.Enabled(), err)
	}
	again.Disabled = true
	again.SetEnabled(true)
	if _, err := again.Shell(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("shell in development mode: %v", err)
	}
}

func TestShellRunsInATerminal(t *testing.T) {
	m := NewManager(config.Open(t.TempDir()))
	m.command = []string{"/bin/sh"}
	session, err := m.Shell(context.Background())
	if err != nil {
		t.Skipf("no pseudo terminal here: %v", err)
	}
	defer session.Close()
	session.Resize(132, 43)
	// A terminal: the shell sees its size and that its input is one.
	session.Write([]byte("test -t 0 && stty size && echo done-$((6*7))\n"))

	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(session)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	var seen []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("the shell ended early: %q", seen)
			}
			seen = append(seen, line)
			if strings.Contains(line, "done-42") && !strings.Contains(line, "echo") {
				if !strings.Contains(strings.Join(seen, "\n"), "43 132") {
					t.Errorf("the size did not arrive: %q", seen)
				}
				// Closing ends the shell; reading then ends too.
				session.Close()
				for range lines {
				}
				return
			}
		case <-deadline:
			t.Fatalf("no answer from the shell: %q", seen)
		}
	}
}
