package main

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func fakeSystemctl(t *testing.T, fail string) *[]string {
	t.Helper()
	var calls []string
	before := systemctl
	systemctl = func(ctx context.Context, args ...string) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("systemctl without a deadline")
		}
		calls = append(calls, args[0])
		if args[0] == fail {
			return []byte("failed"), errors.New("exit status 1")
		}
		return nil, nil
	}
	t.Cleanup(func() { systemctl = before })
	return &calls
}

func TestWithTitandStoppedStartsAgain(t *testing.T) {
	calls := fakeSystemctl(t, "")
	ran := false
	if err := withTitandStopped(func(ctx context.Context) error {
		if !slices.Equal(*calls, []string{"stop"}) {
			t.Errorf("action ran after %v", *calls)
		}
		ran = true
		return nil
	}); err != nil || !ran {
		t.Fatalf("err %v, ran %t", err, ran)
	}
	if !slices.Equal(*calls, []string{"stop", "start"}) {
		t.Fatalf("calls %v", *calls)
	}
}

func TestWithTitandStoppedStartsAgainAfterFailure(t *testing.T) {
	calls := fakeSystemctl(t, "")
	want := errors.New("broken")
	if err := withTitandStopped(func(context.Context) error { return want }); !errors.Is(err, want) {
		t.Fatalf("err %v", err)
	}
	if !slices.Equal(*calls, []string{"stop", "start"}) {
		t.Fatalf("calls %v", *calls)
	}
}

func TestWithTitandStoppedNoActionWhenStopFails(t *testing.T) {
	calls := fakeSystemctl(t, "stop")
	if err := withTitandStopped(func(context.Context) error {
		t.Error("action ran although titand still runs")
		return nil
	}); err == nil {
		t.Fatal("no error")
	}
	if !slices.Equal(*calls, []string{"stop"}) {
		t.Fatalf("calls %v", *calls)
	}
}
