package viewer

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
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
	m.Engine, m.Dev, m.Version = engine, true, "dev"
	m.Prefix, m.Network = "fern63-test-b-", "fern63-test-bnet"
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
	for time.Now().Before(deadline) && (hello == nil || keyframes == 0 || frames < 5) {
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
