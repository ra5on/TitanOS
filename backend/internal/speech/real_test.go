package speech

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// realEngine is the Docker of this machine, which no TitanOS manages.
type realEngine struct{ *docker.Manager }

func (realEngine) EngineReady() bool { return true }

// TestRealSpeech runs the manager against a real Docker engine, the real
// images and Hugging Face: download of the smallest model and a German
// voice, both containers in their network without a way out, speaking a
// sentence and writing it down again, the Wyoming ports, a changed
// setting, and removal. Not part of the normal run (it downloads about
// 0.6 GB of images and 140 MB of models, opens the ports 10300 and 10200
// and starts containers named titanos-speech-… on this machine):
//
//	TITANOS_SPEECH_REAL=<empty folder> go test ./internal/speech -run TestRealSpeech -v -timeout 30m
//
// Only the processor: no GPU is asked for.
func TestRealSpeech(t *testing.T) {
	root := os.Getenv("TITANOS_SPEECH_REAL")
	if root == "" {
		t.Skip("set TITANOS_SPEECH_REAL to a folder to run the speech services for real")
	}
	if err := os.MkdirAll(filepath.Join(root, "tank"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := config.Open(t.TempDir())
	m := New(store)
	engine := realEngine{docker.NewManager(store, store.Dir())}
	m.Engine, m.MountRoot = engine, root
	m.Mounted = func() []string { return []string{"tank"} }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	wait := func(what string, limit time.Duration, ok func(Status) bool) Status {
		t.Helper()
		deadline := time.Now().Add(limit)
		for {
			st := m.Status()
			if ok(st) {
				return st
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %+v jobs %+v", what, st, m.Jobs())
			}
			time.Sleep(time.Second)
		}
	}
	defer func() {
		// Whatever happened: no container stays behind.
		s := m.Settings()
		s.Enabled = false
		_ = m.SetSettings(s)
		wait("off", time.Minute, func(st Status) bool { return st.Whisper.Phase == "off" && st.Piper.Phase == "off" })
		cancel()
		<-done
		for _, name := range []string{whisperService, piperService} {
			if st, err := engine.SpeechInspect(context.Background(), name); err != nil || st.Exists {
				t.Errorf("%s stays: %+v %v", name, st, err)
			}
		}
	}()

	if err := m.SetSettings(Settings{Volume: "tank"}); err != nil {
		t.Fatal(err)
	}
	voices, err := m.Voices(ctx)
	if err != nil || len(voices) < 100 {
		t.Fatalf("catalog of voices: %v %d", err, len(voices))
	}
	for _, d := range [][2]string{{"model", "tiny"}, {"voice", "de_DE-thorsten-low"}} {
		if err := m.Download(ctx, d[0], d[1]); err != nil {
			t.Fatal(err)
		}
	}
	for deadline := time.Now().Add(15 * time.Minute); len(m.Jobs()) > 0; time.Sleep(time.Second) {
		for k, j := range m.Jobs() {
			if j.Error != "" {
				t.Fatalf("download %s: %s", k, j.Error)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("downloads: %+v", m.Jobs())
		}
	}
	models, got := m.Installed()
	t.Logf("installed: %v %v", models, got)

	s := Settings{Enabled: true, Volume: "tank", WhisperModel: "tiny", Language: "de", Voice: "de_DE-thorsten-low", Speed: 1, Reach: ReachHA}
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	st := wait("running", 25*time.Minute, func(st Status) bool {
		if st.Whisper.Phase == "error" || st.Piper.Phase == "error" {
			t.Fatalf("a service gave up: %+v", st)
		}
		return st.Whisper.Phase == "running" && st.Piper.Phase == "running"
	})
	if !st.Whisper.Listening || !st.Piper.Listening {
		t.Fatalf("ports: %+v", st)
	}
	// The services need a moment after their container is up.
	var audio Audio
	for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(2 * time.Second) {
		sctx, c := context.WithTimeout(ctx, 30*time.Second)
		audio, err = m.Say(sctx, "Hallo, hier ist das NAS. Das Licht im Flur ist aus.", "")
		c()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("say: %v", err)
		}
	}
	if audio.Rate < 8000 || len(audio.PCM) < 20000 {
		t.Fatalf("audio: %d Hz, %d bytes", audio.Rate, len(audio.PCM))
	}
	var text string
	for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(2 * time.Second) {
		hctx, c := context.WithTimeout(ctx, 60*time.Second)
		text, err = m.Hear(hctx, audio, "de")
		c()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hear: %v", err)
		}
	}
	t.Logf("heard: %q", text)
	if !strings.Contains(strings.ToLower(text), "hallo") || !strings.Contains(strings.ToLower(text), "licht") {
		t.Fatalf("heard something else: %q", text)
	}

	// The Wyoming port on this machine: a loopback sender is let through
	// and gets the service's description.
	conn, err := net.DialTimeout("tcp", "127.0.0.1:10200", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	writeEvent(conn, "describe", nil, nil)
	ev, err := readEvent(bufio.NewReaderSize(conn, maxEventHead))
	conn.Close()
	if err != nil || ev.Type != "info" || !strings.Contains(string(ev.Data), "piper") {
		t.Fatalf("describe through the port: %v %s", err, ev.Type)
	}
	if seen := m.Status().Piper.Seen; seen == nil || !seen.OK {
		t.Fatalf("last connection: %+v", seen)
	}

	// A changed speed replaces the container of the voice only.
	before, _ := engine.SpeechInspect(ctx, piperService)
	whisperBefore, _ := engine.SpeechInspect(ctx, whisperService)
	s.Speed = 1.5
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	wait("replaced", 3*time.Minute, func(Status) bool {
		now, err := engine.SpeechInspect(ctx, piperService)
		return err == nil && now.Exists && now.Running && now.ID != before.ID
	})
	if now, _ := engine.SpeechInspect(ctx, whisperService); now.ID != whisperBefore.ID {
		t.Fatal("speech to text was replaced for a change of the voice")
	}
	// The containers have no way out of the NAS and publish nothing.
	logs, _ := engine.SpeechLogs(ctx, whisperService, 30)
	t.Logf("whisper log: %s", lastLines(logs, 400))
}
