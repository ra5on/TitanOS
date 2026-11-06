package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

// annexB joins NAL units with 4-byte start codes.
func annexB(nals ...[]byte) []byte {
	var b []byte
	for _, n := range nals {
		b = append(b, 0, 0, 0, 1)
		b = append(b, n...)
	}
	return b
}

var (
	nAUD   = []byte{0x09, 0xf0}
	nSPS   = []byte{0x67, 0x42, 0xc0, 0x1f, 0xda, 0x01}
	nPPS   = []byte{0x68, 0xce, 0x3c, 0x80}
	nIDR   = []byte{0x65, 0x88, 0x84, 0x00, 0x00, 0x03, 0x00, 0x01} // first_mb 0, contains emulation prevention
	nIDR2  = []byte{0x65, 0x40, 0x11, 0x22}                         // second slice (first_mb != 0)
	nSlice = []byte{0x41, 0x9a, 0x02, 0x03}
)

func TestAUSplitterWithAUD(t *testing.T) {
	stream := annexB(nAUD, nSPS, nPPS, nIDR, nIDR2, nAUD, nSlice, nAUD, nSlice, nAUD)
	// feed byte by byte and in one piece: same result
	for _, step := range []int{1, 3, 7, len(stream)} {
		sp := newAUSplitter()
		var aus []accessUnit
		for i := 0; i < len(stream); i += step {
			aus = append(aus, sp.Write(stream[i:min(i+step, len(stream))])...)
		}
		if len(aus) != 3 {
			t.Fatalf("step %d: %d AUs, want 3", step, len(aus))
		}
		if !aus[0].Key || aus[1].Key || aus[2].Key {
			t.Fatalf("step %d: key flags %v %v %v", step, aus[0].Key, aus[1].Key, aus[2].Key)
		}
		if aus[0].Codec != "avc1.42C01F" {
			t.Fatalf("codec %q", aus[0].Codec)
		}
		if got := nalTypes(aus[0].Data); !slices.Equal(got, []int{9, 7, 8, 5, 5}) {
			t.Fatalf("step %d: AU0 NALs %v", step, got)
		}
		if !bytes.Equal(aus[0].Data, annexB(nAUD, nSPS, nPPS, nIDR, nIDR2)) {
			t.Fatalf("step %d: AU0 bytes differ", step)
		}
		if got := nalTypes(aus[2].Data); !slices.Equal(got, []int{9, 1}) {
			t.Fatalf("AU2 NALs %v", got)
		}
	}
}

func TestAUSplitterWithoutAUD(t *testing.T) {
	// 3-byte start codes, no delimiters: boundaries from SPS and first slices
	var stream []byte
	for _, n := range [][]byte{nSPS, nPPS, nIDR, nIDR2, nSlice, nSlice, nSPS, nPPS, nIDR, nSlice} {
		stream = append(stream, 0, 0, 1)
		stream = append(stream, n...)
	}
	aus := newAUSplitter().Write(stream)
	// the trailing slice starts an AU that is not complete yet
	want := [][]int{{7, 8, 5, 5}, {1}, {1}, {7, 8, 5}}
	if len(aus) != len(want) {
		t.Fatalf("%d AUs, want %d", len(aus), len(want))
	}
	for i, au := range aus {
		if got := nalTypes(au.Data); !slices.Equal(got, want[i]) {
			t.Fatalf("AU%d NALs %v want %v", i, got, want[i])
		}
	}
}

func TestAUSplitterGarbagePrefix(t *testing.T) {
	stream := append([]byte{1, 2, 3, 4, 5}, annexB(nAUD, nSlice, nAUD, nSlice, nAUD)...)
	aus := newAUSplitter().Write(stream)
	if len(aus) != 2 || !slices.Equal(nalTypes(aus[0].Data), []int{9, 1}) {
		t.Fatalf("got %d AUs", len(aus))
	}
}

func TestRTPPayload(t *testing.T) {
	hdr := []byte{0x80, 0x60, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3}
	opus := []byte{0xfc, 0xff, 0xfe}
	if p, err := rtpPayload(append(hdr, opus...)); err != nil || !bytes.Equal(p, opus) {
		t.Fatalf("plain: %v %x", err, p)
	}
	// CSRC count 1, extension with 1 word, padding 2 bytes
	h := []byte{0xb1, 0x60, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 9, 9, 9, 9, 0xbe, 0xde, 0, 1, 1, 2, 3, 4}
	pkt := append(append(h, opus...), 0, 2)
	if p, err := rtpPayload(pkt); err != nil || !bytes.Equal(p, opus) {
		t.Fatalf("ext/pad: %v %x", err, p)
	}
	for _, bad := range [][]byte{
		hdr[:8],
		append([]byte{0x40}, hdr[1:]...), // version 1
		{0x81, 0x60, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3},        // csrc missing
		{0xa0, 0x60, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 1, 50}, // padding too long
	} {
		if _, err := rtpPayload(bad); err == nil {
			t.Fatalf("accepted %x", bad)
		}
	}
}

func TestKeyMapping(t *testing.T) {
	if xKeycode("KeyA") != 38 || xKeycode("Enter") != 36 || xKeycode("Space") != 65 || xKeycode("ArrowLeft") != 113 {
		t.Fatal("evdev+8 table broken")
	}
	if xKeycode("Nope") != 0 {
		t.Fatal("unknown code")
	}
	plain := proto.Mods{}
	cases := []struct {
		m     proto.ClientMsg
		shift bool
		act   keyAction
		code  string
		r     rune
	}{
		{proto.ClientMsg{Code: "KeyA", Key: "a", Down: true}, false, keyPhysical, "KeyA", 'a'},
		{proto.ClientMsg{Code: "KeyA", Key: "A", Down: true, Mods: proto.Mods{Shift: true}}, true, keyPhysical, "KeyA", 'A'},
		{proto.ClientMsg{Code: "KeyA", Key: "A", Down: true}, false, keyType, "KeyA", 'A'}, // caps lock on client
		{proto.ClientMsg{Code: "KeyY", Key: "z", Down: true}, false, keyType, "KeyY", 'z'}, // German layout
		{proto.ClientMsg{Code: "Digit7", Key: "/", Down: true, Mods: proto.Mods{Shift: true}}, true, keyType, "Digit7", '/'},
		{proto.ClientMsg{Code: "Quote", Key: "ä", Down: true}, false, keyType, "Quote", 'ä'},
		{proto.ClientMsg{Code: "KeyE", Key: "€", Down: true, Mods: proto.Mods{Ctrl: true, Alt: true}}, false, keyType, "KeyE", '€'}, // AltGr on Windows
		{proto.ClientMsg{Code: "KeyC", Key: "c", Down: true, Mods: proto.Mods{Ctrl: true}}, false, keyPhysical, "KeyC", 0},
		{proto.ClientMsg{Code: "Enter", Key: "Enter", Down: true}, false, keyPhysical, "Enter", 0},
		{proto.ClientMsg{Code: "", Key: "Backspace", Down: true}, false, keyPhysical, "Backspace", 0},
		{proto.ClientMsg{Code: "ShiftLeft", Key: "Shift", Down: true}, false, keyPhysical, "ShiftLeft", 0},
		{proto.ClientMsg{Code: "", Key: "Unidentified", Down: true}, false, keyIgnore, "", 0},
		{proto.ClientMsg{Code: "Space", Key: " ", Down: true}, false, keyPhysical, "Space", ' '},
		{proto.ClientMsg{Code: "Space", Key: " ", Down: true, Mods: plain}, true, keyPhysical, "Space", ' '},
	}
	for i, c := range cases {
		act, code, r := decideKey(c.m, c.shift)
		if act != c.act || code != c.code || r != c.r {
			t.Errorf("case %d: got %v %q %q, want %v %q %q", i, act, code, r, c.act, c.code, c.r)
		}
	}
	if keysymFor('a') != 'a' || keysymFor('ä') != 0xe4 || keysymFor('€') != 0x010020ac || keysymFor('😀') != 0x0101f600 {
		t.Fatal("keysyms")
	}
	if _, ok := singleRune("ab"); ok {
		t.Fatal("two runes")
	}
	if _, ok := singleRune("\x7f"); ok {
		t.Fatal("control")
	}
}

func TestWheelSteps(t *testing.T) {
	acc := 0.0
	if n := wheelSteps(&acc, 40); n != 0 {
		t.Fatal(n)
	}
	if n := wheelSteps(&acc, 70); n != 1 || acc != 10 {
		t.Fatal(n, acc)
	}
	if n := wheelSteps(&acc, -310); n != -3 {
		t.Fatal(n)
	}
	if n := wheelSteps(&acc, 1e6); n != 20 {
		t.Fatal(n)
	}
}

func TestCursorCSS(t *testing.T) {
	for in, want := range map[string]string{"xterm": "text", "hand2": "pointer", "left_ptr": "default", "": "default", "ew-resize": "ew-resize", "weird<x>": "default"} {
		if got := cursorCSS(in); got != want {
			t.Errorf("%q → %q want %q", in, got, want)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	for in, want := range map[string]string{
		"a.pdf":                              "a.pdf",
		"../../etc/passwd":                   "passwd",
		`C:\Users\x\bild.jpg`:                "bild.jpg",
		"..":                                 "",
		"...":                                "",
		".bashrc":                            "bashrc",
		"a\x00b\nc.txt":                      "abc.txt",
		"ä\u202e.txt":                        "ä.txt",
		"what?.txt":                          "what_.txt",
		"  spaced  ":                         "spaced",
		"name. ":                             "name",
		"":                                   "",
		"/":                                  "",
		"x/" + "\xff\xfe.bin":                "_.bin",
		strings.Repeat("ü", 300) + ".tar.gz": strings.Repeat("ü", 98) + ".gz",
	} {
		if got := sanitizeName(in); got != want {
			t.Errorf("%q → %q want %q", in, got, want)
		}
		if got := sanitizeName(in); len(got) > 200 || strings.ContainsAny(got, "/\\") {
			t.Errorf("%q → unsafe %q", in, got)
		}
	}
}

func TestUploads(t *testing.T) {
	withProfile(t, 100<<30)
	u := newUploads()
	if _, code := u.begin(1, "a.txt", proto.MaxUploadSize+1); code != proto.UploadTooLarge {
		t.Fatal(code)
	}
	if _, code := u.begin(1, "..", 3); code != proto.UploadBadName {
		t.Fatal(code)
	}
	for round := 0; round < 2; round++ {
		if _, code := u.begin(1, "../a.txt", 6); code != "" {
			t.Fatal(code)
		}
		if done, _, code := u.chunk(1, []byte("abc")); done || code != "" {
			t.Fatal(done, code)
		}
		done, name, code := u.chunk(1, []byte("def"))
		want := "a.txt"
		if round == 1 {
			want = "a (2).txt"
		}
		if !done || code != "" || name != want {
			t.Fatalf("round %d: %v %q %q", round, done, name, code)
		}
		b, _ := os.ReadFile(filepath.Join(uploadDir, name))
		if string(b) != "abcdef" {
			t.Fatalf("content %q", b)
		}
	}
	// overflow drops the upload and its temp file
	u.begin(2, "b.txt", 2)
	if _, _, code := u.chunk(2, []byte("abc")); code != proto.UploadTooLarge {
		t.Fatal(code)
	}
	if _, _, code := u.chunk(2, []byte("a")); code != proto.UploadFailed {
		t.Fatal(code)
	}
	ents, _ := os.ReadDir(uploadDir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
}

func TestDownloadsPoll(t *testing.T) {
	dir := t.TempDir()
	old := downloadDir
	downloadDir = dir
	defer func() { downloadDir = old }()
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("x"), 0o644)
	d := newDownloads()
	os.WriteFile(filepath.Join(dir, "new.bin.crdownload"), []byte("partial"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello"), 0o644)
	if got := d.poll(); len(got) != 0 { // first sighting: not yet stable
		t.Fatal(got)
	}
	got := d.poll()
	if len(got) != 1 || got[0].Name != "new.txt" || got[0].Size != 5 || got[0].ID != "d1" {
		t.Fatalf("%+v", got)
	}
	if len(d.poll()) != 0 {
		t.Fatal("announced twice")
	}
	if dl, ok := d.get("d1"); !ok || dl.Path != filepath.Join(dir, "new.txt") {
		t.Fatal("get")
	}
	if _, ok := d.get("../x"); ok {
		t.Fatal("bad id")
	}
}

func TestKeyCompare(t *testing.T) {
	if !keyOK("0123456789abcdef", "0123456789abcdef") {
		t.Fatal("equal")
	}
	for _, got := range []string{"", "0123456789abcdeF", "0123456789abcde", "0123456789abcdef0"} {
		if keyOK("0123456789abcdef", got) {
			t.Fatalf("accepted %q", got)
		}
	}
	if keyOK("", "") {
		t.Fatal("empty")
	}
}

func TestAppOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"http://172.18.0.2:8080":    "http://172.18.0.2:8080",
		"https://app:8443/path?x=1": "https://app:8443",
		"http://[fd00::1]:80/":      "http://[fd00::1]:80",
	} {
		if got, err := appOrigin(in); err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "file:///etc/passwd", "javascript:alert(1)", "http://user:pw@host/", "http://*/"} {
		if _, err := appOrigin(in); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
}

func TestCheckPolicyFile(t *testing.T) {
	origin := "http://10.0.0.5:8080"
	uid := uint32(os.Getuid())
	if uid == 0 {
		t.Skip("root can write anything")
	}
	mk := func(fileMode, dirMode os.FileMode, data []byte) string {
		dir := filepath.Join(t.TempDir(), "managed")
		os.Mkdir(dir, 0o755)
		p := filepath.Join(dir, "titanos.json")
		os.WriteFile(p, data, 0o644)
		os.Chmod(p, fileMode)
		os.Chmod(dir, dirMode)
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
		return p
	}
	good, _ := proto.ChromiumPolicy(origin)
	if err := checkPolicyFile(mk(0o444, 0o555, good), origin, uid); err != nil {
		t.Fatalf("read-only policy rejected: %v", err)
	}
	for name, c := range map[string]struct {
		file, dir os.FileMode
		data      []byte
		origin    string
		uid       uint32
	}{
		"writable file":  {0o644, 0o555, good, origin, uid},
		"writable dir":   {0o444, 0o755, good, origin, uid},
		"group writable": {0o464, 0o555, good, origin, uid},
		"other origin":   {0o444, 0o555, good, "http://10.0.0.6:8080", uid},
		"wrong owner":    {0o444, 0o555, good, origin, uid + 1},
		"no blocklist":   {0o444, 0o555, []byte(`{"URLAllowlist":["http://10.0.0.5:8080","about:blank"]}`), origin, uid},
	} {
		if err := checkPolicyFile(mk(c.file, c.dir, c.data), c.origin, c.uid); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if checkPolicyFile(filepath.Join(t.TempDir(), "missing.json"), origin, uid) == nil {
		t.Error("missing file accepted")
	}
	target := mk(0o444, 0o555, good)
	dir := filepath.Join(t.TempDir(), "m2")
	os.Mkdir(dir, 0o755)
	link := filepath.Join(dir, "titanos.json")
	os.Symlink(target, link)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if checkPolicyFile(link, origin, uid) == nil {
		t.Error("symlink accepted")
	}
}

func TestPipelineArgs(t *testing.T) {
	args, err := pipelineArgs(encoderParams{GPU: "cpu", W: 1280, H: 800, FPS: 30, Kbps: 2000})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"endx=1279", "endy=799", "show-pointer=false", "use-damage=false", "x264enc", "aud=true",
		"profile=constrained-baseline", "config-interval=-1", "alignment=au", "fdsink fd=3", "key-int-max=90", "bitrate=2000"} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in %s", want, line)
		}
	}
	if _, err := pipelineArgs(encoderParams{GPU: "cpu", W: 1281, H: 800, FPS: 30}); err == nil {
		t.Fatal("odd width accepted")
	}
	if vaElement("", "h264enc") != "vah264enc" || vaElement("/dev/dri/renderD129", "h264enc") != "varenderD129h264enc" {
		t.Fatal("va element names")
	}
	if k := bitrate(1280, 800, 30, "balanced", 1); k < 1000 || k > 3000 {
		t.Fatal(k)
	}
	if bitrate(3840, 2160, 60, "high", 1) != 25000 || bitrate(320, 240, 5, "low", 0.1) != 300 {
		t.Fatal("bitrate clamp")
	}
}

func TestContentDisposition(t *testing.T) {
	got := contentDisposition(`bäd "x".txt`)
	if got != `attachment; filename="b_d _x_.txt"; filename*=UTF-8''b%C3%A4d%20%22x%22.txt` {
		t.Fatal(got)
	}
}

func TestUSKey(t *testing.T) {
	for r, want := range map[rune]struct {
		code  string
		shift bool
	}{'a': {"KeyA", false}, 'A': {"KeyA", true}, '!': {"Digit1", true}, ' ': {"Space", false}, '/': {"Slash", false}, '?': {"Slash", true}} {
		kc, shift, ok := usKey(r)
		if !ok || kc != xKeycode(want.code) || shift != want.shift {
			t.Errorf("%q: %d %v %v", r, kc, shift, ok)
		}
	}
	for _, r := range "äß€ " {
		if _, _, ok := usKey(r); ok {
			t.Errorf("%q has no US key", r)
		}
	}
}

func withProfile(t *testing.T, free int64) string {
	t.Helper()
	dir := t.TempDir()
	oldU, oldD, oldF := uploadDir, downloadDir, freeSpace
	uploadDir, downloadDir = filepath.Join(dir, "Hochgeladen"), filepath.Join(dir, "Downloads")
	os.MkdirAll(downloadDir, 0o755)
	freeSpace = func(string) (int64, error) { return free, nil }
	t.Cleanup(func() { uploadDir, downloadDir, freeSpace = oldU, oldD, oldF })
	return dir
}

func TestUploadSpace(t *testing.T) {
	withProfile(t, proto.ProfileReserve+100)
	u := newUploads()
	if _, code := u.begin(1, "a", 101); code != proto.UploadNoSpace {
		t.Fatalf("reserve: %q", code)
	}
	if _, code := u.begin(1, "a", 60); code != "" {
		t.Fatal(code)
	}
	// the second upload would eat into the reserve together with the first
	if _, code := u.begin(2, "b", 50); code != proto.UploadNoSpace {
		t.Fatalf("pending: %q", code)
	}
	// space shrinks while writing: the chunk is refused, the upload dropped
	freeSpace = func(string) (int64, error) { return proto.ProfileReserve + 10, nil }
	if _, _, code := u.chunk(1, make([]byte, 20)); code != proto.UploadNoSpace {
		t.Fatalf("chunk: %q", code)
	}
	if ents, _ := os.ReadDir(uploadDir); len(ents) != 0 {
		t.Fatal("temp file left")
	}
}

func TestUploadCap(t *testing.T) {
	withProfile(t, 100<<30)
	f, _ := os.Create(filepath.Join(downloadDir, "big"))
	f.Truncate(proto.ProfileCap - 10) // sparse
	f.Close()
	u := newUploads()
	if _, code := u.begin(1, "a", 11); code != proto.UploadNoSpace {
		t.Fatalf("cap: %q", code)
	}
	if _, code := u.begin(1, "a", 10); code != "" {
		t.Fatalf("within cap: %q", code)
	}
}

func TestProfileCleanup(t *testing.T) {
	withProfile(t, 100<<30)
	os.MkdirAll(uploadDir, 0o755)
	os.WriteFile(filepath.Join(uploadDir, "x"), []byte("x"), 0o644)
	clearUploads()
	if _, err := os.Stat(uploadDir); !os.IsNotExist(err) {
		t.Fatal("uploads kept")
	}
	old, young := filepath.Join(downloadDir, "old"), filepath.Join(downloadDir, "young")
	os.WriteFile(old, []byte("o"), 0o644)
	os.WriteFile(young, []byte("y"), 0o644)
	os.Chtimes(old, time.Now().Add(-25*time.Hour), time.Now().Add(-25*time.Hour))
	pruneDownloads(24 * time.Hour)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old download kept")
	}
	if _, err := os.Stat(young); err != nil {
		t.Fatal("young download removed")
	}
}

func TestEnforceDownloadLimits(t *testing.T) {
	withProfile(t, 100<<30)
	mk := func(name string, size int64, age time.Duration) string {
		p := filepath.Join(downloadDir, name)
		f, _ := os.Create(p)
		f.Truncate(size)
		f.Close()
		os.Chtimes(p, time.Now().Add(-age), time.Now().Add(-age))
		return p
	}
	a := mk("a.bin", 2<<30, 3*time.Hour)
	b := mk("b.bin", 1<<30, time.Hour)
	part := mk("c.bin.crdownload", 1<<30+1, 0)
	enforceDownloadLimits() // 4 GiB + 1: the running download goes first
	for p, want := range map[string]bool{a: true, b: true, part: false} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s exists=%v, want %v", filepath.Base(p), err == nil, want)
		}
	}
	freeSpace = func(string) (int64, error) { return proto.ProfileReserve - 1, nil }
	enforceDownloadLimits() // low space: remove oldest until ok (here: all)
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Error("oldest download kept with low space")
	}
}
