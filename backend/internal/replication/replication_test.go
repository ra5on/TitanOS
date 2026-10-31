package replication

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/retention"
)

// fakeBtrfs makes and deletes snapshot folders like btrfs would.
type fakeBtrfs struct {
	mu       sync.Mutex
	commands []string
}

func (f *fakeBtrfs) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	f.mu.Lock()
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	f.mu.Unlock()
	switch {
	case len(args) >= 4 && args[1] == "snapshot":
		return "", os.MkdirAll(args[len(args)-1], 0o700)
	case len(args) >= 3 && args[1] == "delete":
		return "", os.RemoveAll(args[2])
	}
	return "", nil
}

func (f *fakeBtrfs) has(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.commands {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// The fake send stream names its snapshot and its parent; the fake receive
// makes the snapshot folder from it.
func fakeSend(_ context.Context, args ...string) *exec.Cmd {
	parent := ""
	for i, a := range args {
		if a == "-p" {
			parent = filepath.Base(args[i+1])
		}
	}
	return exec.Command("printf", "NAME:%s PARENT:%s\n", filepath.Base(args[len(args)-1]), parent)
}

func fakeReceive(_ context.Context, args ...string) *exec.Cmd {
	return exec.Command("sh", "-c", `read l; n=${l#NAME:}; n=${n%% *}; mkdir "$1/$n" && echo "$l" > "$1/$n/stream"`, "_", args[len(args)-1])
}

func code(url, user, token string) string {
	raw, _ := json.Marshal(map[string]any{"v": 1, "url": url, "user": user, "password": token})
	return codePrefix + base64.RawURLEncoding.EncodeToString(raw)
}

type pair struct {
	sender   *Manager
	receiver *Receiver
	btrfs    *fakeBtrfs
	server   *httptest.Server
	inbox    string
	clock    time.Time
}

func newPair(t *testing.T) *pair {
	t.Helper()
	p := &pair{btrfs: &fakeBtrfs{}, clock: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	receiveRoot := t.TempDir()
	os.Mkdir(filepath.Join(receiveRoot, "backup"), 0o755)
	p.receiver = &Receiver{MountRoot: receiveRoot, Mounted: func() []string { return []string{"backup"} }, runner: p.btrfs, command: fakeReceive, busy: map[string]bool{},
		Filesystem: func(string) string { return "btrfs" }}
	p.inbox = filepath.Join(receiveRoot, "backup", receivedDir, "home")
	// Stands in for backuptarget: one source "home" with token "secret".
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, token, _ := r.BasicAuth()
		rest, ok := strings.CutPrefix(r.URL.Path, "/home/_replica")
		if user != "home" || token != "secret" || !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		p.receiver.Serve(w, r, "home", "backup", strings.TrimPrefix(rest, "/"))
	}))
	t.Cleanup(p.server.Close)

	sendRoot := t.TempDir()
	os.Mkdir(filepath.Join(sendRoot, "tank"), 0o755)
	p.sender = NewManager(config.Open(t.TempDir()))
	p.sender.mountRoot, p.sender.runner, p.sender.command = sendRoot, p.btrfs, fakeSend
	p.sender.Mounted = func() []string { return []string{"tank"} }
	p.sender.Filesystem = func(string) string { return "btrfs" }
	p.sender.now = func() time.Time { return p.clock }
	p.sender.Client = p.server.Client()
	if err := p.sender.Load(); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *pair) runOnce(t *testing.T, id string) State {
	t.Helper()
	done := make(chan error, 1)
	p.sender.OnResult = func(_ Job, err error) { done <- err }
	if err := p.sender.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run never ended")
	}
	return p.sender.Jobs()[0].State
}

func TestReplicationSendsIncrementallyAndKeeps(t *testing.T) {
	p := newPair(t)
	id, err := p.sender.Save("", Request{Name: "offsite", Enabled: true, Volume: "tank", Code: code(p.server.URL+"/home", "home", "secret"), EveryHours: 24, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	if view := p.sender.Jobs()[0]; view.Token != "" || view.URL == "" {
		t.Fatalf("token leaked or address missing: %+v", view)
	}
	var names []string
	for i := range 3 {
		state := p.runOnce(t, id)
		names = append(names, state.Snapshot)
		stream, err := os.ReadFile(filepath.Join(p.inbox, state.Snapshot, "stream"))
		if err != nil {
			t.Fatal(err)
		}
		// The first run sends everything, later ones the difference to the last.
		wantParent := "PARENT:"
		if i > 0 {
			wantParent += names[i-1]
		}
		if !strings.Contains(string(stream), wantParent+"\n") {
			t.Fatalf("run %d: %q, want %s", i, stream, wantParent)
		}
		p.clock = p.clock.Add(25 * time.Hour)
	}
	// The other NAS keeps two, this one its last two (the newest is the next parent).
	if got := received(p.inbox); len(got) != 2 || got[1] != names[2] {
		t.Fatalf("received: %v", got)
	}
	if got := localSnapshots(filepath.Join(p.sender.mountRoot, "tank", localDir, id)); len(got) != 2 || got[1] != names[2] {
		t.Fatalf("local: %v", got)
	}
	if !p.btrfs.has("btrfs subvolume snapshot -r " + filepath.Join(p.sender.mountRoot, "tank")) {
		t.Fatal("no read-only snapshot of the volume")
	}
}

func TestReplicationRefusesBadInput(t *testing.T) {
	p := newPair(t)
	good := Request{Name: "offsite", Enabled: true, Volume: "tank", Code: code(p.server.URL+"/home", "home", "secret"), EveryHours: 24, Keep: 7}
	for _, change := range []func(*Request){
		func(r *Request) { r.Code = "titanos-backup:###" },
		func(r *Request) { r.Code = code("https://x/home", "home", "s") }, // only the plain Tailscale server
		func(r *Request) { r.EveryHours = 0 },
		func(r *Request) { r.Keep = 1 },
		func(r *Request) { r.Name = "../x" },
	} {
		req := good
		change(&req)
		if _, err := p.sender.Save("", req); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", req, err)
		}
	}
	p.sender.Filesystem = func(string) string { return "ext4" }
	if _, err := p.sender.Save("", good); !errors.Is(err, ErrNotBtrfs) {
		t.Errorf("ext4 volume: %v", err)
	}
}

func TestReplicationWrongTokenAndBrokenStream(t *testing.T) {
	p := newPair(t)
	id, _ := p.sender.Save("", Request{Name: "offsite", Enabled: true, Volume: "tank", Code: code(p.server.URL+"/home", "home", "wrong"), EveryHours: 24, Keep: 7})
	done := make(chan error, 1)
	p.sender.OnResult = func(_ Job, err error) { done <- err }
	p.sender.Start(context.Background(), id)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("wrong token: %v", err)
	}
	// A failed run leaves no snapshot behind on either side.
	if got := localSnapshots(filepath.Join(p.sender.mountRoot, "tank", localDir, id)); len(got) != 0 {
		t.Fatalf("local snapshot kept after failure: %v", got)
	}
	if state := p.sender.Jobs()[0].State; state.LastError == "" || state.Next == nil || state.Next.After(p.clock.Add(31*time.Minute)) {
		t.Fatalf("failed run not retried soon: %+v", state)
	}

	// The receiver drops what a broken transfer left.
	p.receiver.command = func(_ context.Context, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", `mkdir "$1/20261027T120000Z"; exit 1`, "_", args[len(args)-1])
	}
	p.sender.Save(id, Request{Name: "offsite", Enabled: true, Volume: "tank", Code: code(p.server.URL+"/home", "home", "secret"), EveryHours: 24, Keep: 7})
	p.sender.Start(context.Background(), id)
	if err := <-done; err == nil {
		t.Fatal("broken receive reported as success")
	}
	if got := received(p.inbox); len(got) != 0 {
		t.Fatalf("incomplete snapshot listed: %v", got)
	}
}

func TestRestoreReplicaIntoNewFolder(t *testing.T) {
	p := newPair(t)
	os.MkdirAll(filepath.Join(p.inbox, "20261027T120000Z"), 0o700)
	if list := p.receiver.List("backup"); len(list) != 1 || list[0].Source != "home" {
		t.Fatalf("list: %+v", list)
	}
	path, err := p.receiver.Restore(context.Background(), "backup", "home", "20261027T120000Z")
	if err != nil || !strings.HasSuffix(path, "/restored/home-20261027T120000Z") {
		t.Fatal(path, err)
	}
	if _, err := p.receiver.Restore(context.Background(), "backup", "home", "20261027T120000Z"); err == nil {
		t.Fatal("restored over an existing folder")
	}
	if _, err := p.receiver.Restore(context.Background(), "backup", "../x", "20261027T120000Z"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

// endlessSend stands in for a large btrfs send.
func endlessSend(ctx context.Context, _ ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "cat", "/dev/zero")
}

// otherNAS lists no snapshots and handles PUT with put.
func otherNAS(t *testing.T, put http.HandlerFunc) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"snapshots":[]}`))
			return
		}
		put(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func runAgainst(t *testing.T, p *pair, server *httptest.Server) error {
	t.Helper()
	p.sender.Client = server.Client()
	p.sender.command = endlessSend
	id, err := p.sender.Save("", Request{Name: "offsite", Enabled: true, Volume: "tank", Code: code(server.URL+"/home", "home", "secret"), EveryHours: 24, Keep: 7})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	p.sender.OnResult = func(_ Job, err error) { done <- err }
	if err := p.sender.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if p.sender.Busy() {
			t.Fatal("still running after the end")
		}
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("the run hangs: btrfs send blocks on a pipe nobody reads")
	}
	return nil
}

func TestReplicationEndsWhenTheOtherSideAnswersEarly(t *testing.T) {
	p := newPair(t)
	server := otherNAS(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusConflict) // without reading the stream
	})
	err := runAgainst(t, p, server)
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatal(err)
	}
	// The snapshot without a copy on the other side is gone again.
	if got := localSnapshots(filepath.Join(p.sender.mountRoot, "tank", localDir, p.sender.Jobs()[0].ID)); len(got) != 0 {
		t.Fatalf("left: %v", got)
	}
}

func TestReplicationGivesUpAStreamThatStandsStill(t *testing.T) {
	p := newPair(t)
	p.sender.stall = 300 * time.Millisecond
	server := otherNAS(t, func(w http.ResponseWriter, r *http.Request) {
		// Takes nothing and does not answer (for longer than the sender
		// waits; the server notices no hang-up while the body is unread).
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	err := runAgainst(t, p, server)
	if err == nil || !strings.Contains(err.Error(), "stood still") {
		t.Fatal(err)
	}
}

func TestRollingReplicaMakesRoomOnTheOtherNAS(t *testing.T) {
	p := newPair(t)
	free := uint64(50)
	p.receiver.Space = func(string) (uint64, uint64, error) { return free, 1000, nil }
	id, err := p.sender.Save("", Request{Name: "offsite", Enabled: true, Volume: "tank", Code: code(p.server.URL+"/home", "home", "secret"), EveryHours: 24, Keep: 2, Versions: &retention.Keep{Mode: "rolling"}})
	if err != nil {
		t.Fatal(err)
	}
	if job := p.sender.Jobs()[0]; job.Keep != maxKeep || job.Versions == nil || job.Versions.Mode != "rolling" {
		t.Fatalf("rolling job: %+v", job)
	}
	// Enough space: every snapshot stays.
	free = 500
	var names []string
	for range 4 {
		names = append(names, p.runOnce(t, id).Snapshot)
		p.clock = p.clock.Add(25 * time.Hour)
	}
	if got := received(p.inbox); len(got) != 4 {
		t.Fatalf("received: %v", got)
	}
	// Short of space: the oldest go before the next transfer, the newest
	// (the parent) stays.
	free = 50
	names = append(names, p.runOnce(t, id).Snapshot)
	got := received(p.inbox)
	if len(got) != 2 || got[0] != names[3] || got[1] != names[4] {
		t.Fatalf("after making room: %v", got)
	}
	if !p.btrfs.has("btrfs subvolume sync") {
		t.Fatal("space not waited for")
	}
	if _, err := p.sender.Save(id, Request{Name: "offsite", Enabled: true, Volume: "tank", EveryHours: 24, Keep: 2, Versions: &retention.Keep{Mode: "rolling", WhenFull: "oldest"}}); err == nil {
		t.Fatal("whenFull accepted")
	}
}
