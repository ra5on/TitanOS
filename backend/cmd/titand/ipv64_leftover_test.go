package main

import (
	"context"
	"strings"
	"testing"
)

type recordingRunner struct{ calls []string }

func (r *recordingRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return "", nil
}

// The tunnel of an older version is removed if it is there, and nothing
// runs if it is not.
func TestRemoveLeftoverIPv64Tunnel(t *testing.T) {
	r := &recordingRunner{}
	removeLeftoverIPv64Tunnel(context.Background(), r, func(string) bool { return false })
	if len(r.calls) != 0 {
		t.Fatalf("ran without the interface: %v", r.calls)
	}
	removeLeftoverIPv64Tunnel(context.Background(), r, func(iface string) bool { return iface == "ipv64wg" })
	if len(r.calls) != 1 || r.calls[0] != "ip link del dev ipv64wg" {
		t.Fatalf("calls: %v", r.calls)
	}
}
