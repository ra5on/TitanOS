package viewer

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// TestLiveDevView runs one real view against the local Docker in
// development mode, only with TITANOS_VIEWER_LIVE=<app> set: the image
// titanos-viewer:dev must exist and <app> must be a running Compose
// project with a WebUI label. Everything it creates is named fern63-test-*;
// the start-up cleanup (which would remove every viewer on the host) is
// skipped.
func TestLiveDevView(t *testing.T) {
	app := os.Getenv("TITANOS_VIEWER_LIVE")
	if app == "" {
		t.Skip("TITANOS_VIEWER_LIVE not set")
	}
	dir := t.TempDir()
	engine := docker.NewManager(config.Open(dir), dir)
	m := New(config.Open(t.TempDir()))
	m.Engine, m.Dev, m.Version = logEngine{engine, t}, true, "dev"
	// Own names: the containers and networks of other tests are never touched.
	engine.ViewerNamePrefix, m.Prefix = "fern63-test-b-", "fern63-test-b-"
	m.Policy = proto.ChromiumPolicy
	m.cleaned = true
	t.Cleanup(func() {
		if m.devDir != "" {
			os.RemoveAll(m.devDir)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var lease *Lease
	var err error
	for range 50 {
		if lease, err = m.Open(ctx, "anna", app); err != ErrImageNotReady {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("open: %v (image %+v)", err, m.ImageStatus())
	}
	defer lease.Close(false)
	conn, err := lease.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.Write(ctx, websocket.MessageText, []byte(`{"t":"size","width":800,"height":600,"scale":1}`))
	var hello map[string]any
	frames, keyframes := 0, 0
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) && (hello == nil || keyframes == 0 || frames < 90) {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (hello %v, frames %d)", err, hello, frames)
		}
		if kind == websocket.MessageText {
			var msg map[string]any
			json.Unmarshal(data, &msg)
			if msg["t"] == "hello" {
				hello = msg
			}
			continue
		}
		if len(data) > 1 && data[0] == 0x01 {
			frames++
			if data[1]&1 == 1 {
				keyframes++
			}
		}
	}
	t.Logf("hello %v, %d frames, %d key frames, sessions %+v", hello, frames, keyframes, m.Sessions(ctx, ""))
	if hello == nil || keyframes == 0 {
		t.Fatal("no picture")
	}
}

// logEngine shows the output of a viewer before it is removed.
type logEngine struct {
	*docker.Manager
	t *testing.T
}

func (e logEngine) ViewerRemove(ctx context.Context, id string) error {
	out, _ := exec.Command("docker", "logs", "--tail", "30", id).CombinedOutput()
	e.t.Logf("viewer %.12s output:\n%s", id, out)
	return e.Manager.ViewerRemove(ctx, id)
}

// ViewerCreate hands the policy file to root first, as titand (root) on a
// NAS writes it; the agent refuses a policy it could own.
func (e logEngine) ViewerCreate(ctx context.Context, spec docker.ViewerSpec) (string, error) {
	if spec.Policy != "" {
		dir := filepath.Dir(spec.Policy)
		if out, err := exec.Command("docker", "run", "--rm", "--network", "none", "-v", dir+":/p", "debian:trixie-slim", "chown", "0:0", "/p/titanos.json").CombinedOutput(); err != nil {
			e.t.Fatalf("chown policy: %v %s", err, out)
		}
	}
	return e.Manager.ViewerCreate(ctx, spec)
}
