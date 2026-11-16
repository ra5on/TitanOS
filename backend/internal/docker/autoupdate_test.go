package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeUpdateEngine struct {
	images  map[string]string // service -> image id running
	pulled  map[string]string // what a pull would bring
	healthy func() (bool, string)
	steps   []string
	pullErr error
}

func (f *fakeUpdateEngine) updater() updater {
	return updater{
		watch: 30 * time.Millisecond, step: 5 * time.Millisecond,
		snapshot: func(ctx context.Context, app string) ([]imageOf, error) {
			var out []imageOf
			for service, id := range f.images {
				out = append(out, imageOf{service: service, ref: "example/" + service + ":latest", id: id})
			}
			return out, nil
		},
		pullUp: func(ctx context.Context, app string, pull bool) error {
			if pull {
				f.steps = append(f.steps, "pull+up")
				if f.pullErr != nil {
					return f.pullErr
				}
				for service, id := range f.pulled {
					f.images[service] = id
				}
				return nil
			}
			f.steps = append(f.steps, "up")
			return nil
		},
		healthy: func(ctx context.Context, app string) (bool, string) { return f.healthy() },
		tag: func(ctx context.Context, id, ref string) error {
			f.steps = append(f.steps, "tag "+id+" "+ref)
			service := strings.TrimSuffix(strings.TrimPrefix(ref, "example/"), ":latest")
			f.images[service] = id
			return nil
		},
	}
}

func TestAutoUpdateKeepsAHealthyUpdate(t *testing.T) {
	f := &fakeUpdateEngine{images: map[string]string{"web": "old", "db": "db1"}, pulled: map[string]string{"web": "new"}, healthy: func() (bool, string) { return true, "" }}
	r := f.updater().run(context.Background(), "photos")
	if r.Outcome != "updated" || r.Detail != "web" || f.images["web"] != "new" {
		t.Fatalf("%+v %v", r, f.images)
	}
}

func TestAutoUpdateRollsBackAnUnhealthyOne(t *testing.T) {
	calls := 0
	f := &fakeUpdateEngine{images: map[string]string{"web": "old", "db": "db1"}, pulled: map[string]string{"web": "new"}, healthy: func() (bool, string) {
		calls++
		if calls < 3 {
			return false, "starting"
		}
		return false, "web reports itself unhealthy"
	}}
	r := f.updater().run(context.Background(), "photos")
	if r.Outcome != "rolledBack" || !strings.Contains(r.Detail, "unhealthy") || f.images["web"] != "old" {
		t.Fatalf("%+v %v", r, f.images)
	}
	last := f.steps[len(f.steps)-1]
	if last != "up" || !strings.Contains(strings.Join(f.steps, "|"), "tag old example/web:latest") {
		t.Fatalf("steps %v", f.steps)
	}
}

func TestAutoUpdateStartingTooLongRollsBack(t *testing.T) {
	f := &fakeUpdateEngine{images: map[string]string{"web": "old"}, pulled: map[string]string{"web": "new"}, healthy: func() (bool, string) { return false, "starting" }}
	if r := f.updater().run(context.Background(), "photos"); r.Outcome != "rolledBack" || f.images["web"] != "old" {
		t.Fatalf("%+v", r)
	}
}

func TestAutoUpdateNothingNewAndFailedPull(t *testing.T) {
	f := &fakeUpdateEngine{images: map[string]string{"web": "same"}, pulled: map[string]string{}, healthy: func() (bool, string) { return true, "" }}
	if r := f.updater().run(context.Background(), "photos"); r.Outcome != "current" {
		t.Fatalf("%+v", r)
	}
	f = &fakeUpdateEngine{images: map[string]string{"web": "old"}, pullErr: errors.New("registry down"), healthy: func() (bool, string) { return true, "" }}
	if r := f.updater().run(context.Background(), "photos"); r.Outcome != "rolledBack" || !strings.Contains(r.Detail, "registry down") {
		t.Fatalf("%+v", r)
	}
}

func TestUpdateWindow(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	s := AutoUpdateSettings{Weekdays: []int{0, 3}, Time: "04:00"} // Sunday, Wednesday
	wed := time.Date(2026, 10, 28, 4, 10, 0, 0, berlin)
	if !dueWindow(s, wed, time.Time{}) {
		t.Fatal("window not open on Wednesday 04:10")
	}
	if dueWindow(s, wed, wed.Add(-5*time.Minute)) {
		t.Fatal("window used twice")
	}
	if dueWindow(s, time.Date(2026, 10, 29, 4, 10, 0, 0, berlin), time.Time{}) {
		t.Fatal("open on a Thursday")
	}
	if dueWindow(s, time.Date(2026, 10, 28, 5, 30, 0, 0, berlin), time.Time{}) {
		t.Fatal("open after the hour")
	}
	if next := nextWindow(s, wed); !next.Equal(time.Date(2026, 11, 1, 4, 0, 0, 0, berlin)) {
		t.Fatalf("next %v", next)
	}
}

// A container in a restart loop (the engine says Running and Restarting
// between two crashes) is no healthy update; one that runs is.
func TestAutoUpdateWatchSeesARestartLoop(t *testing.T) {
	m, _, _ := newTestManager(t)
	state := `{"Running":true,"Restarting":true,"ExitCode":1,"Status":"restarting"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			fmt.Fprint(w, `[{"Id":"c1","Labels":{"com.docker.compose.project":"app","com.docker.compose.service":"web"}}]`)
			return
		}
		fmt.Fprintf(w, `{"State":%s,"HostConfig":{"RestartPolicy":{"Name":"unless-stopped"}}}`, state)
	}))
	t.Cleanup(server.Close)
	m.api = &client{http: &http.Client{Transport: rewriteTransport{server.URL}}}
	healthy := m.realUpdater().healthy
	if ok, why := healthy(context.Background(), "app"); ok || !strings.Contains(why, "restarting") {
		t.Fatalf("restart loop: %v %q", ok, why)
	}
	state = `{"Running":true,"ExitCode":0,"Status":"running"}`
	if ok, why := healthy(context.Background(), "app"); !ok {
		t.Fatalf("running: %q", why)
	}
}

// A service pinned by digest (redis:7@sha256:…) does not stop the way back
// of the others: the engine refuses to tag such a reference.
func TestAutoUpdateRollbackSkipsDigestPinnedImages(t *testing.T) {
	var steps []string
	u := updater{
		pullUp: func(ctx context.Context, app string, pull bool) error {
			steps = append(steps, "up")
			return nil
		},
		tag: func(ctx context.Context, id, ref string) error {
			if strings.Contains(ref, "@") {
				return errors.New("refusing to create a tag with a digest reference")
			}
			steps = append(steps, "tag "+id+" "+ref)
			return nil
		},
	}
	before := []imageOf{
		{service: "redis", ref: "redis:7-alpine@sha256:" + strings.Repeat("a", 64), id: "r1"},
		{service: "web", ref: "example/web:latest", id: "old"},
	}
	if back := u.rollback(context.Background(), "photos", before); back != "" {
		t.Fatalf("rollback: %s", back)
	}
	if strings.Join(steps, "|") != "tag old example/web:latest|up" {
		t.Fatalf("steps: %v", steps)
	}
}
