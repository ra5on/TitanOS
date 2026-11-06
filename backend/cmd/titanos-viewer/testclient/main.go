// Command testclient drives a running titanos-viewer container for the
// integration test (scripts/build-viewer-image.sh does not run it; see
// viewer/README.md). It prints "PASS name" / "FAIL name: why" lines and
// exits non-zero on any failure.
//
//	testclient -addr <viewer-ip>:7300 -key <key> [-audio]
//	testclient pngpixel <file.png> <x> <y>
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

var failed bool

func pass(name string, format string, a ...any) {
	fmt.Printf("PASS %s: %s\n", name, fmt.Sprintf(format, a...))
}

func fail(name string, format string, a ...any) {
	failed = true
	fmt.Printf("FAIL %s: %s\n", name, fmt.Sprintf(format, a...))
}

type stream struct {
	conn *websocket.Conn
	mu   sync.Mutex
	// counters
	aus, keys, audio int
	auBytes          int64
	keyNALOK         bool
	keyNALs          []int
	firstAU          time.Time
	text             chan map[string]any
	closed           chan struct{}
	lastVideoTS      uint64
	cursors          []string
}

func dial(ctx context.Context, addr, key string) (*stream, error) {
	c, _, err := websocket.Dial(ctx, "ws://"+addr+"/stream?key="+key, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(64 << 20)
	s := &stream{conn: c, text: make(chan map[string]any, 1000), closed: make(chan struct{}), keyNALOK: true}
	go s.read()
	return s, nil
}

func nalTypes(b []byte) []int {
	var t []int
	for i := 0; i+3 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			t = append(t, int(b[i+3]&0x1f))
			i += 3
		}
	}
	return t
}

func (s *stream) read() {
	defer close(s.closed)
	for {
		typ, data, err := s.conn.Read(context.Background())
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				if m["t"] == "cursor" {
					s.mu.Lock()
					s.cursors = append(s.cursors, fmt.Sprint(m["shape"]))
					s.mu.Unlock()
				}
				select {
				case s.text <- m:
				default:
				}
			}
			continue
		}
		s.mu.Lock()
		switch data[0] {
		case proto.TypeVideo:
			s.aus++
			s.auBytes += int64(len(data))
			if s.firstAU.IsZero() {
				s.firstAU = time.Now()
			}
			s.lastVideoTS = binary.BigEndian.Uint64(data[2:10])
			t := nalTypes(data[10:])
			if len(t) == 0 || t[0] != 9 {
				s.keyNALOK = false
			}
			if data[1]&proto.VideoFlagKey != 0 {
				s.keys++
				s.keyNALs = t
				if !slices.Contains(t, 7) || !slices.Contains(t, 8) || !slices.Contains(t, 5) {
					s.keyNALOK = false
				}
			}
		case proto.TypeAudio:
			if len(data) > 9 {
				s.audio++
			}
		}
		s.mu.Unlock()
	}
}

func (s *stream) counts() (aus, keys, audio int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aus, s.keys, s.audio
}

// waitText waits for a text message of type t (other types are skipped
// but stats are remembered).
func (s *stream) waitText(t string, d time.Duration, match func(map[string]any) bool) map[string]any {
	deadline := time.After(d)
	for {
		select {
		case m := <-s.text:
			if m["t"] == "stats" {
				lastStats = m
			}
			if m["t"] == t && (match == nil || match(m)) {
				return m
			}
		case <-deadline:
			return nil
		case <-s.closed:
			if t == "" {
				return nil
			}
		}
	}
}

var lastStats map[string]any

func (s *stream) send(v any) {
	b, _ := json.Marshal(v)
	_ = s.conn.Write(context.Background(), websocket.MessageText, b)
}

func (s *stream) key(code, key string, down bool, mods proto.Mods) {
	s.send(map[string]any{"t": "key", "code": code, "key": key, "down": down, "mods": mods})
}

func (s *stream) tap(code, key string, mods proto.Mods) {
	s.key(code, key, true, mods)
	time.Sleep(30 * time.Millisecond)
	s.key(code, key, false, mods)
	time.Sleep(30 * time.Millisecond)
}

func (s *stream) combo(mod, modKey, code, key string) {
	m := proto.Mods{Ctrl: mod == "ControlLeft", Alt: mod == "AltLeft"}
	s.key(mod, modKey, true, m)
	time.Sleep(30 * time.Millisecond)
	s.tap(code, key, m)
	s.key(mod, modKey, false, proto.Mods{})
	time.Sleep(50 * time.Millisecond)
}

func (s *stream) click(x, y int) {
	s.send(map[string]any{"t": "mouse", "x": x, "y": y, "buttons": 0})
	time.Sleep(50 * time.Millisecond)
	s.send(map[string]any{"t": "mouse", "x": x, "y": y, "buttons": 1})
	time.Sleep(50 * time.Millisecond)
	s.send(map[string]any{"t": "mouse", "x": x, "y": y, "buttons": 0})
	time.Sleep(100 * time.Millisecond)
}

func pngPixel(args []string) {
	f, err := os.Open(args[0])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	img, err := png.Decode(f)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	x, _ := strconv.Atoi(args[1])
	y, _ := strconv.Atoi(args[2])
	r, g, b, _ := img.At(x, y).RGBA()
	fmt.Printf("%d %d %d %dx%d\n", r>>8, g>>8, b>>8, img.Bounds().Dx(), img.Bounds().Dy())
}

func main() {
	if len(os.Args) == 5 && os.Args[1] == "pngpixel" {
		pngPixel(os.Args[2:])
		return
	}
	addr := flag.String("addr", "", "viewer host:port")
	key := flag.String("key", "", "session key")
	wantAudio := flag.Bool("audio", false, "expect audio packets")
	phase := flag.String("phase", "all", "all | stream (video, size, scale) | big (3840x2160 for 10 s) | hold (wait for bye)")
	wantGPU := flag.String("wantgpu", "cpu", "expected hello.gpu")
	flag.Parse()
	ctx := context.Background()
	httpc := &http.Client{Timeout: 10 * time.Second}

	// wrong key is rejected
	if resp, err := httpc.Get("http://" + *addr + "/stream?key=wrong-key-0123456789"); err != nil || resp.StatusCode != 403 {
		fail("auth", "wrong key not rejected (%v)", err)
	} else {
		resp.Body.Close()
		pass("auth", "wrong key → 403")
	}

	t0 := time.Now()
	s, err := dial(ctx, *addr, *key)
	if err != nil {
		fail("connect", "%v", err)
		os.Exit(1)
	}
	hello := s.waitText("hello", 10*time.Second, nil)
	if hello == nil {
		fail("hello", "no hello")
		os.Exit(1)
	}
	pass("hello", "%v after %v", hello, time.Since(t0).Round(time.Millisecond))
	if hello["gpu"] != *wantGPU {
		fail("gpu", "hello.gpu %v, want %s", hello["gpu"], *wantGPU)
	}
	switch *phase {
	case "hold":
		if m := s.waitText("bye", 60*time.Second, nil); m != nil && m["reason"] == proto.ByeStopped {
			pass("bye", "%v", m)
		} else {
			fail("bye", "got %v", m)
		}
		exit()
	case "fileclick": // debugging: open the file dialog and leave it open
		s.send(map[string]any{"t": "size", "width": 1000, "height": 600, "scale": 1})
		time.Sleep(3 * time.Second)
		s.click(150, 230)
		time.Sleep(3 * time.Second)
		exit()
	case "big":
		s.send(map[string]any{"t": "size", "width": 1920, "height": 1080, "scale": 2})
		if h := s.waitText("hello", 15*time.Second, func(m map[string]any) bool { return m["width"] == 3840.0 }); h != nil {
			pass("big", "%v", h)
		} else {
			fail("big", "no 3840 hello")
		}
		a0, _, _ := s.counts()
		time.Sleep(8 * time.Second)
		a1, _, _ := s.counts()
		pass("big-fps", "%.1f fps at 3840x2160 (cpu)", float64(a1-a0)/8)
		finish(s)
		exit()
	}
	time.Sleep(5 * time.Second)
	aus, keys, _ := s.counts()
	s.mu.Lock()
	first := s.firstAU.Sub(t0)
	nalOK, kn, kb := s.keyNALOK, s.keyNALs, s.auBytes
	s.mu.Unlock()
	if aus >= 30 && keys >= 1 && nalOK {
		pass("video", "%d AUs in 5 s (%.1f fps), %d key frames, key NALs %v, first AU after %v, %d kbit/s",
			aus, float64(aus)/5, keys, kn, first.Round(time.Millisecond), kb*8/5/1000)
	} else {
		fail("video", "%d AUs, %d keys, nal ok %v (%v)", aus, keys, nalOK, kn)
	}

	// resize
	t1 := time.Now()
	s.send(map[string]any{"t": "size", "width": 1000, "height": 600, "scale": 1})
	h2 := s.waitText("hello", 10*time.Second, func(m map[string]any) bool { return m["width"] == 1000.0 && m["height"] == 600.0 })
	if h2 != nil {
		pass("resize", "new hello %v after %v", h2, time.Since(t1).Round(time.Millisecond))
	} else {
		fail("resize", "no hello with 1000x600")
	}
	time.Sleep(1500 * time.Millisecond)
	if *phase == "stream" {
		// HiDPI: a new scale factor restarts the browser
		t2 := time.Now()
		s.send(map[string]any{"t": "size", "width": 640, "height": 400, "scale": 2})
		if h := s.waitText("hello", 15*time.Second, func(m map[string]any) bool { return m["width"] == 1280.0 && m["scale"] == 2.0 }); h != nil {
			pass("scale", "hello %v after %v", h, time.Since(t2).Round(time.Millisecond))
		} else {
			fail("scale", "no hello with 1280x800 scale 2")
		}
		a0, _, _ := s.counts()
		time.Sleep(3 * time.Second)
		if a1, _, _ := s.counts(); a1-a0 >= 30 {
			pass("scale-video", "%d AUs in 3 s", a1-a0)
		} else {
			fail("scale-video", "%d AUs in 3 s", a1-a0)
		}
		finish(s)
		exit()
	}

	// policy: Tab from the autofocused field to the link to the other server, Enter
	s.click(300, 20)
	s.tap("Tab", "Tab", proto.Mods{})
	s.tap("Enter", "Enter", proto.Mods{})
	time.Sleep(2500 * time.Millisecond)
	fmt.Println("MARK policy-clicked")
	s.combo("AltLeft", "Alt", "ArrowLeft", "ArrowLeft") // back
	time.Sleep(2 * time.Second)

	// typing: physical keys, remapped characters, text message
	s.click(300, 20)
	time.Sleep(300 * time.Millisecond)
	s.mu.Lock()
	cursors := slices.Clone(s.cursors)
	s.mu.Unlock()
	if slices.Contains(cursors, "text") {
		pass("cursor", "shapes seen %v", cursors)
	} else {
		fail("cursor", "no text cursor over the input (seen %v)", cursors)
	}
	for _, c := range "abc" {
		s.tap("Key"+strings.ToUpper(string(c)), string(c), proto.Mods{})
	}
	// German layout: KeyY produces "z", Shift+Digit7 produces "/"
	s.tap("KeyY", "z", proto.Mods{})
	s.key("ShiftLeft", "Shift", true, proto.Mods{Shift: true})
	s.tap("Digit7", "/", proto.Mods{Shift: true})
	s.tap("KeyA", "A", proto.Mods{Shift: true})
	s.key("ShiftLeft", "Shift", false, proto.Mods{})
	s.send(map[string]any{"t": "text", "text": "äö€ Z!"})
	time.Sleep(500 * time.Millisecond)
	want := "abcz/Aäö€ Z!"
	// copy via Ctrl+A, Ctrl+C → clipboard message
	s.combo("ControlLeft", "Control", "KeyA", "a")
	s.combo("ControlLeft", "Control", "KeyC", "c")
	if m := s.waitText("clipboard", 4*time.Second, nil); m != nil && m["text"] == want {
		pass("clipboard-out", "%q", m["text"])
	} else {
		fail("clipboard-out", "got %v want %q", m, want)
	}
	s.tap("Enter", "Enter", proto.Mods{})
	time.Sleep(2 * time.Second)
	fmt.Println("MARK typed " + want)

	// clipboard in: no echo back
	s.send(map[string]any{"t": "clipboard", "text": "from-client"})
	if m := s.waitText("clipboard", 2500*time.Millisecond, nil); m == nil {
		pass("clipboard-in", "set, no echo")
	} else {
		fail("clipboard-in", "echo %v", m)
	}

	// download: back to the form, Tab Tab to the download link, Enter
	s.combo("AltLeft", "Alt", "ArrowLeft", "ArrowLeft")
	time.Sleep(2 * time.Second)
	s.click(300, 20)
	s.tap("Tab", "Tab", proto.Mods{})
	s.tap("Tab", "Tab", proto.Mods{})
	s.tap("Enter", "Enter", proto.Mods{})
	if m := s.waitText("download", 8*time.Second, nil); m != nil {
		id, _ := m["id"].(string)
		resp, err := httpc.Get("http://" + *addr + "/downloads/" + id + "?key=" + *key)
		if err != nil {
			fail("download", "%v", err)
		} else {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if strings.TrimSpace(string(b)) == "hello download" {
				pass("download", "%v, fetched %d bytes, %s", m, len(b), resp.Header.Get("Content-Disposition"))
			} else {
				fail("download", "status %d body %q", resp.StatusCode, b)
			}
		}
		if resp, err := httpc.Get("http://" + *addr + "/downloads/" + id + "?key=bad-key-0123456789"); err == nil && resp.StatusCode == 403 {
			pass("download-auth", "bad key → 403")
		} else {
			fail("download-auth", "bad key not rejected")
		}
	} else {
		fail("download", "no download message")
	}

	// upload with a hostile name
	payload := []byte(strings.Repeat("x", 300<<10))
	s.send(map[string]any{"t": "upload", "id": 7, "name": "../../etc/ä‮.txt", "size": len(payload)})
	for off := 0; off < len(payload); off += proto.MaxUploadChunk {
		end := min(off+proto.MaxUploadChunk, len(payload))
		msg := append([]byte{proto.TypeUpload, 0, 0, 0, 7}, payload[off:end]...)
		_ = s.conn.Write(ctx, websocket.MessageBinary, msg)
	}
	if m := s.waitText("uploaded", 5*time.Second, nil); m != nil && m["name"] == "ä.txt" && m["error"] == nil {
		pass("upload", "%v", m)
	} else {
		fail("upload", "%v", m)
	}
	s.send(map[string]any{"t": "upload", "id": 8, "name": "big.bin", "size": int64(3) << 30})
	if m := s.waitText("uploaded", 3*time.Second, nil); m != nil && m["error"] == proto.UploadTooLarge {
		pass("upload-limit", "%v", m)
	} else {
		fail("upload-limit", "%v", m)
	}

	// file dialog: click the file input, type the path of the upload
	s.click(150, 230)
	time.Sleep(2 * time.Second)
	s.combo("ControlLeft", "Control", "KeyL", "l")
	time.Sleep(300 * time.Millisecond)
	s.send(map[string]any{"t": "text", "text": "/profile/Hochgeladen/ä.txt"})
	time.Sleep(500 * time.Millisecond)
	s.tap("Enter", "Enter", proto.Mods{})
	time.Sleep(2 * time.Second)
	s.tap("Escape", "Escape", proto.Mods{}) // close a dialog left open
	fmt.Println("MARK file-dialog")

	// key frame request
	_, k0, _ := s.counts()
	s.send(map[string]any{"t": "keyframe"})
	time.Sleep(2 * time.Second)
	if _, k1, _ := s.counts(); k1 > k0 {
		pass("keyframe", "%d → %d key frames", k0, k1)
	} else {
		fail("keyframe", "no new key frame")
	}

	if *wantAudio {
		if _, _, a := s.counts(); a > 50 {
			pass("audio", "%d Opus packets", a)
		} else {
			fail("audio", "%d Opus packets", a)
		}
	}
	finish(s)

	// a second client replaces the first
	s2, err := dial(ctx, *addr, *key)
	if err != nil {
		fail("replace", "%v", err)
	} else {
		if m := s.waitText("bye", 5*time.Second, nil); m != nil && m["reason"] == proto.ByeReplaced {
			pass("replace", "old client got %v", m)
		} else {
			fail("replace", "old client got %v", m)
		}
		if s2.waitText("hello", 10*time.Second, nil) != nil {
			pass("replace-hello", "new client streaming")
		} else {
			fail("replace-hello", "no hello for new client")
		}
		s2.conn.Close(websocket.StatusNormalClosure, "")
	}
	exit()
}

func exit() {
	if failed {
		os.Exit(1)
	}
	os.Exit(0)
}

func finish(s *stream) {
	s.waitText("stats", 2*time.Second, nil)
	if lastStats != nil {
		pass("stats", "%v", lastStats)
	} else {
		fail("stats", "no stats")
	}
}
