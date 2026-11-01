package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunSecretPassesStdinAndDescriptors(t *testing.T) {
	out, err := Exec{}.RunSecret(context.Background(), []byte("old secret"), [][]byte{[]byte("new secret"), []byte("third")},
		"sh", "-c", `cat; printf '|'; cat /dev/fd/3; printf '|'; cat /dev/fd/4`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "old secret|new secret|third" {
		t.Fatalf("%q", out)
	}
}

func TestRunSecretErrorNeverCarriesTheSecret(t *testing.T) {
	_, err := Exec{}.RunSecret(context.Background(), []byte("hunter2-secret"), [][]byte{[]byte("other-secret")}, "sh", "-c", "exit 2")
	var command *CommandError
	if !errors.As(err, &command) {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret in error: %v", err)
	}
}

func TestRunReturnsStdoutOnly(t *testing.T) {
	out, err := Exec{}.Run(context.Background(), "", "sh", "-c", `echo warning >&2; echo 1234-ABCD`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "1234-ABCD\n" {
		t.Fatalf("stderr leaked into the result: %q", out)
	}
}

func TestRunErrorCarriesStderrAndStdout(t *testing.T) {
	out, err := Exec{}.Run(context.Background(), "", "sh", "-c", `echo partial; echo "device busy" >&2; exit 3`)
	var command *CommandError
	if !errors.As(err, &command) {
		t.Fatal(err)
	}
	if out != "partial\n" {
		t.Fatalf("result: %q", out)
	}
	if command.Output != "device busy\npartial" {
		t.Fatalf("output: %q", command.Output)
	}
}

func TestRunSecretReturnsStdoutOnly(t *testing.T) {
	out, err := Exec{}.RunSecret(context.Background(), nil, nil, "sh", "-c", `echo noise >&2; printf uuid`)
	if err != nil || out != "uuid" {
		t.Fatalf("%q %v", out, err)
	}
}

type onlyRun struct{}

func (onlyRun) Run(context.Context, string, string, ...string) (string, error) { return "plain", nil }

func TestCombined(t *testing.T) {
	out, err := Combined(context.Background(), Exec{}, "", "sh", "-c", `echo a; echo b >&2`)
	if err != nil || out != "a\nb\n" {
		t.Fatalf("%q %v", out, err)
	}
	if out, _ := Combined(context.Background(), onlyRun{}, "", "x"); out != "plain" {
		t.Fatalf("fallback: %q", out)
	}
}

func TestCombinedLines(t *testing.T) {
	var lines []string
	out, err := CombinedLines(context.Background(), Exec{}, func(line string) { lines = append(lines, line) },
		"sh", "-c", `printf ' a Pulling \n'; printf 'b 1\rb 2\r\n' >&2; printf 'end'`)
	if err != nil || !strings.Contains(out, "a Pulling") || !strings.HasSuffix(out, "end") {
		t.Fatalf("%q %v", out, err)
	}
	if strings.Join(lines, "|") != "a Pulling|b 1|b 2|end" {
		t.Fatalf("lines %q", lines)
	}
	lines = nil
	if out, _ := CombinedLines(context.Background(), onlyRun{}, func(line string) { lines = append(lines, line) }, "x"); out != "plain" || strings.Join(lines, "|") != "plain" {
		t.Fatalf("fallback: %q %q", out, lines)
	}
	if _, err := CombinedLines(context.Background(), Exec{}, func(string) {}, "sh", "-c", "echo broken; exit 3"); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("failure: %v", err)
	}
}

func TestRunGivesUpOnAProcessThatDoesNotEnd(t *testing.T) {
	old := abandonAfter
	abandonAfter = 100 * time.Millisecond
	defer func() { abandonAfter = old }()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// The child ignores nothing, but a grandchild keeps stdout open: Wait
	// alone would block on the pipe for 30 seconds.
	start := time.Now()
	_, err := Exec{}.Run(ctx, "", "sh", "-c", "sleep 30 & sleep 30")
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("%v after %v", err, time.Since(start))
	}
}
