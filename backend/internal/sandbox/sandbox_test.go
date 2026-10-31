package sandbox

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestPropertiesCloseTheUsualDoors(t *testing.T) {
	p := properties(Limits{MemoryMax: 64 << 20})
	for _, want := range []string{"DynamicUser=yes", "PrivateNetwork=yes", "ProtectSystem=strict", "NoNewPrivileges=yes", "CapabilityBoundingSet=", "SystemCallFilter=@system-service", "MemoryMax=67108864", "RuntimeMaxSec=120"} {
		if !slices.Contains(p, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// Tests do not run as root: the helper runs directly, with a clean
// environment, and stdin still reaches it.
func TestCommandFallsBackWithoutRoot(t *testing.T) {
	cmd := Command(context.Background(), Limits{}, "/bin/cat")
	if cmd.Path != "/bin/cat" || !slices.Contains(cmd.Env, "LC_ALL=C") {
		t.Fatalf("%s %v", cmd.Path, cmd.Env)
	}
	cmd.Stdin = strings.NewReader("hello")
	out, err := cmd.Output()
	if err != nil || string(out) != "hello" {
		t.Fatalf("%q %v", out, err)
	}
}
