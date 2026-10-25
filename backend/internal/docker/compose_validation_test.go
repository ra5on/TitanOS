package docker

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

type validationRunner func(string, ...string) (string, error)

func (r validationRunner) Run(_ context.Context, stdin, name string, args ...string) (string, error) {
	return r(stdin, args...)
}

func TestComposeValidationUsesPrivateEnvBeforePersisting(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.applied = m.dataRoot("data")
	var temporary string
	const secret = "private-value"
	m.runner = validationRunner(func(stdin string, args ...string) (string, error) {
		if !strings.Contains(stdin, "${TAG}") || strings.Contains(strings.Join(args, " "), secret) {
			t.Fatal("interpolation lost or secret in arguments")
		}
		at := slices.Index(args, "--env-file")
		if at < 0 {
			t.Fatal("no env supplied for validation")
		}
		temporary = args[at+1]
		info, err := os.Stat(temporary)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("env not private", err)
		}
		data, err := os.ReadFile(temporary)
		if err != nil || !strings.Contains(string(data), "TAG='v2'") || !strings.Contains(string(data), secret) {
			t.Fatal("env values missing", err)
		}
		if _, err := m.store.Read(envFile("fixture")); !os.IsNotExist(err) {
			t.Fatal("persisted before validation")
		}
		return "invalid fixture", errors.New("validation failed")
	})
	err := m.CreateApp(context.Background(), "fixture", "services:\n  web:\n    image: app:${TAG}\n", map[string]string{"TAG": "v2", "PASSWORD": secret})
	var invalid *InvalidComposeError
	if !errors.As(err, &invalid) {
		t.Fatal(err)
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatal("temporary secret retained", err)
	}
}

func TestComposeEditValidationReadsExistingEnv(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.store.Write(envFile("fixture"), []byte("TAG='stable'\n"))
	m.runner = validationRunner(func(_ string, args ...string) (string, error) {
		at := slices.Index(args, "--env-file")
		if at < 0 {
			t.Fatal("existing env missing")
		}
		data, _ := os.ReadFile(args[at+1])
		if string(data) != "TAG='stable'\n" {
			t.Fatal("existing env not used")
		}
		return "rejected", errors.New("rejected")
	})
	if err := m.SetCompose(context.Background(), "fixture", "services:\n  web:\n    image: app:${TAG}\n"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
