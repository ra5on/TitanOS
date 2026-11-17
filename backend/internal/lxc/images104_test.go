package lxc

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A failed reload of an old list is tried again after a minute and keeps
// saying so until a load works; it is not taken for a fresh list for six
// hours. A load cut off by the page that asked is no failure to remember.
func TestFailedReloadIsTriedAgainAndShown(t *testing.T) {
	env := newTestManager(t)
	calls := 0
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	env.m.now = func() time.Time { return clock }
	offline := false
	env.m.fetchIndex = func(ctx context.Context) ([]byte, error) {
		calls++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if offline {
			return nil, errors.New("offline")
		}
		return []byte(testIndex), nil
	}
	if _, err := env.m.Images(context.Background()); err != nil {
		t.Fatal(err)
	}
	offline = true
	clock = clock.Add(7 * time.Hour)
	if _, err := env.m.Images(context.Background()); err == nil {
		t.Fatal("a failed reload is not reported")
	}
	clock = clock.Add(2 * time.Minute)
	images, err := env.m.Images(context.Background())
	if err == nil || len(images) != 3 || calls != 3 {
		t.Fatalf("not tried again or the failure is hidden: %v %v calls %d", images, err, calls)
	}
	offline = false
	clock = clock.Add(2 * time.Minute)
	if _, err := env.m.Images(context.Background()); err != nil || calls != 4 {
		t.Fatalf("back online: %v calls %d", err, calls)
	}

	// A first load cut off by its caller is not remembered as a failure.
	fresh := newTestManager(t)
	fresh.m.now = env.m.now
	fresh.m.fetchIndex = env.m.fetchIndex
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fresh.m.Images(gone); err == nil {
		t.Fatal("a cut off load reported a list")
	}
	if images, err := fresh.m.Images(context.Background()); err != nil || len(images) != 3 {
		t.Fatalf("a cut off load is remembered as a failure: %v %v", images, err)
	}
}
