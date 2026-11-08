package main

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/webp"

	"github.com/ra5on/TitanOS/backend/internal/viewer/proto"
)

func frameOf(w, h int, b, g, r byte) []byte {
	f := make([]byte, w*h*4)
	for i := 0; i < len(f); i += 4 {
		f[i], f[i+1], f[i+2] = b, g, r
	}
	return f
}

func paint(f []byte, w int, x0, y0, x1, y1 int, b, g, r byte) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			o := (y*w + x) * 4
			f[o], f[o+1], f[o+2] = b, g, r
		}
	}
}

func TestTileGridDiffCommitAndRuns(t *testing.T) {
	// 200×100: 4×2 tiles, the right column and bottom row are smaller.
	g := newTileGrid(200, 100)
	if g.cols != 4 || g.rows != 2 || g.tileRect(7) != (rect{192, 64, 8, 36}) {
		t.Fatalf("grid %d×%d %+v", g.cols, g.rows, g.tileRect(7))
	}
	now := time.Unix(1000, 0)
	f := frameOf(200, 100, 10, 20, 30)
	changed, share := g.diff(f, now)
	if len(changed) != 8 || share != 1 {
		t.Fatalf("first frame: %v %v", changed, share)
	}
	g.commit(f, changed, true)
	if changed, share = g.diff(f, now); len(changed) != 0 || share != 0 {
		t.Fatalf("same frame: %v %v", changed, share)
	}
	// Change two neighbouring tiles of the first row and one of the second.
	paint(f, 200, 70, 10, 140, 20, 0, 0, 255)
	paint(f, 200, 0, 70, 5, 75, 0, 255, 0)
	changed, share = g.diff(f, now)
	if len(changed) != 3 || changed[0] != 1 || changed[1] != 2 || changed[2] != 4 {
		t.Fatalf("changed %v", changed)
	}
	if want := float64(64*64*2+64*36) / 20000; share != want {
		t.Fatalf("share %v want %v", share, want)
	}
	runs := g.runs(changed)
	if len(runs) != 2 || runs[0].r != (rect{64, 0, 128, 64}) || len(runs[0].tiles) != 2 || runs[1].r != (rect{0, 64, 64, 36}) {
		t.Fatalf("runs %+v", runs)
	}
	// Tiles at the end of one row and the start of the next stay apart.
	if r := g.runs([]int{3, 4}); len(r) != 2 {
		t.Fatalf("across rows: %+v", r)
	}
	// Not committed: still changed next time.
	if changed, _ = g.diff(f, now); len(changed) != 3 {
		t.Fatalf("uncommitted %v", changed)
	}
}

func TestTileSharpenAndMoving(t *testing.T) {
	g := newTileGrid(128, 64)
	now := time.Unix(1000, 0)
	f := frameOf(128, 64, 1, 2, 3)
	changed, _ := g.diff(f, now)
	g.commit(f, changed, true)
	// Right after, nothing is calm long enough.
	if s := g.sharpen(now, 10); len(s) != 0 {
		t.Fatalf("too early %v", s)
	}
	later := now.Add(sharpenAfter)
	g.diff(f, later) // a calm frame
	if s := g.sharpen(later, 10); len(s) != 2 {
		t.Fatalf("sharpen %v", s)
	}
	if s := g.sharpen(later, 1); len(s) != 1 {
		t.Fatalf("budget %v", s)
	}
	g.commit(f, []int{0, 1}, false)
	if s := g.sharpen(later, 10); len(s) != 0 {
		t.Fatalf("lossless already %v", s)
	}
	// A tile changing in three of five frames counts as moving.
	for i := 0; i < 5; i++ {
		if i%2 == 0 {
			paint(f, 128, 0, 0, 1, 1, byte(i), 0, 0)
		}
		ch, _ := g.diff(f, later)
		g.commit(f, ch, true)
	}
	if !g.moving(0) || g.moving(1) {
		t.Fatalf("moving %v %v", g.moving(0), g.moving(1))
	}
}

func TestModeSwitchFollowsKasmThresholds(t *testing.T) {
	var m modeSwitch
	t0 := time.Unix(0, 0)
	for s := 0; s < 5; s++ {
		if m.update(0.6, t0.Add(time.Duration(s)*time.Second)) {
			t.Fatalf("switched after %d s", s)
		}
	}
	if !m.update(0.6, t0.Add(5*time.Second)) || !m.video {
		t.Fatal("no video after 5 s of motion")
	}
	// A short calm moment does not go back, three seconds do.
	m.update(0.1, t0.Add(6*time.Second))
	m.update(0.5, t0.Add(7*time.Second))
	if m.update(0.1, t0.Add(8*time.Second)) || !m.video {
		t.Fatal("back too early")
	}
	if !m.update(0.1, t0.Add(11*time.Second)) || m.video {
		t.Fatal("no tiles after 3 s of calm")
	}
	// Motion interrupted before 5 s starts over.
	m.update(0.9, t0.Add(20*time.Second))
	m.update(0.2, t0.Add(23*time.Second))
	if m.update(0.9, t0.Add(25*time.Second)) || m.video {
		t.Fatal("interrupted motion counted")
	}
}

func TestEncodeTileKeepsColoursAndHeader(t *testing.T) {
	f := frameOf(100, 80, 0, 0, 0)
	paint(f, 100, 10, 20, 74, 84-20, 30, 60, 200) // BGR: red-ish
	r := rect{10, 20, 64, 40}
	msg, err := encodeTile(f, 100, tileJob{r: r, lossless: true})
	if err != nil {
		t.Fatal(err)
	}
	if msg[0] != proto.TypeTile || msg[1] != proto.TilePNG ||
		binary.BigEndian.Uint16(msg[2:]) != 10 || binary.BigEndian.Uint16(msg[4:]) != 20 ||
		binary.BigEndian.Uint16(msg[6:]) != 64 || binary.BigEndian.Uint16(msg[8:]) != 40 {
		t.Fatalf("header % x", msg[:proto.TileHeaderSize])
	}
	img, err := png.Decode(bytes.NewReader(msg[proto.TileHeaderSize:]))
	if err != nil {
		t.Fatal(err)
	}
	cr, cg, cb, _ := img.At(0, 0).RGBA()
	if cr>>8 != 200 || cg>>8 != 60 || cb>>8 != 30 || img.Bounds().Dx() != 64 || img.Bounds().Dy() != 40 {
		t.Fatalf("pixel %d %d %d, %v", cr>>8, cg>>8, cb>>8, img.Bounds())
	}
	msg, err = encodeTile(f, 100, tileJob{r: r, quality: 80})
	if err != nil || msg[1] != proto.TileJPEG {
		t.Fatal(err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(msg[proto.TileHeaderSize:])); err != nil {
		t.Fatal(err)
	}
}

func TestJPEGQualityAndRawPipeline(t *testing.T) {
	if jpegQuality(proto.QualityAuto, false) <= jpegQuality(proto.QualityAuto, true) ||
		jpegQuality(proto.QualityHigh, false) <= jpegQuality(proto.QualityLow, false) {
		t.Fatal("quality order")
	}
	args, err := rawPipelineArgs(1280, 800, 30)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"endx=1279", "endy=799", "framerate=30/1", "format=BGRx,width=1280,height=800", "fdsink fd=3"} {
		if !strings.Contains(line, want) {
			t.Fatalf("%q missing in %s", want, line)
		}
	}
	if _, err := rawPipelineArgs(0, 800, 30); err == nil {
		t.Fatal("zero width accepted")
	}
}

// Regression (integration test 8.11.2026): tiles sent in several runs must
// count as known, otherwise a calm screen is sent again every frame.
func TestTileGridKnownPerTile(t *testing.T) {
	g := newTileGrid(256, 128)
	now := time.Unix(1, 0)
	f := frameOf(256, 128, 5, 5, 5)
	changed, _ := g.diff(f, now)
	for _, run := range g.runs(changed) {
		g.commit(f, run.tiles, true)
	}
	if changed, share := g.diff(f, now); len(changed) != 0 || share != 0 {
		t.Fatalf("calm frame after run-wise commit: %v %v", changed, share)
	}
	// A tile never sent stays changed.
	g2 := newTileGrid(256, 128)
	changed, _ = g2.diff(f, now)
	g2.commit(f, changed[1:], true)
	if changed, _ = g2.diff(f, now); len(changed) != 1 || changed[0] != 0 {
		t.Fatalf("unsent tile: %v", changed)
	}
}

// PLAN 72, Kasm-Reste: lossless tiles as WebP for a browser that reads it,
// with the same pixels as the PNG.
func TestEncodeTileWebP(t *testing.T) {
	f := frameOf(100, 80, 0, 0, 0)
	paint(f, 100, 10, 20, 74, 60, 30, 60, 200)
	paint(f, 100, 40, 30, 50, 40, 255, 255, 255)
	r := rect{10, 20, 64, 40}
	msg, err := encodeTile(f, 100, tileJob{r: r, lossless: true, webp: true})
	if err != nil || msg[1] != proto.TileWebP {
		t.Fatalf("% x %v", msg[:2], err)
	}
	img, err := webp.Decode(bytes.NewReader(msg[proto.TileHeaderSize:]))
	if err != nil {
		t.Fatal(err)
	}
	ref, _ := encodeTile(f, 100, tileJob{r: r, lossless: true})
	want, _ := png.Decode(bytes.NewReader(ref[proto.TileHeaderSize:]))
	for y := 0; y < r.H; y++ {
		for x := 0; x < r.W; x++ {
			a1, b1, c1, _ := img.At(x, y).RGBA()
			a2, b2, c2, _ := want.At(x, y).RGBA()
			if a1 != a2 || b1 != b2 || c1 != c2 {
				t.Fatalf("pixel %d,%d differs", x, y)
			}
		}
	}
}

// Encoding on all cores keeps the order of the areas.
func TestEncodeAllKeepsOrder(t *testing.T) {
	f := frameOf(640, 64, 9, 9, 9)
	var jobs []tileJob
	for i := 0; i < 10; i++ {
		jobs = append(jobs, tileJob{r: rect{i * 64, 0, 64, 64}, tiles: []int{i}, quality: 70, lossless: i%3 == 0, webp: i%2 == 0})
	}
	old := encodeWorkers
	defer func() { encodeWorkers = old }()
	encodeWorkers = 4
	msgs, errs := encodeAll(f, 640, jobs)
	for i, m := range msgs {
		if errs[i] != nil || binary.BigEndian.Uint16(m[2:]) != uint16(i*64) {
			t.Fatalf("job %d: x=%d err=%v", i, binary.BigEndian.Uint16(m[2:]), errs[i])
		}
	}
	if !slices.Contains([]byte{proto.TileWebP}, msgs[0][1]) || msgs[1][1] != proto.TileJPEG || msgs[3][1] != proto.TilePNG {
		t.Fatalf("codecs %d %d %d", msgs[0][1], msgs[1][1], msgs[3][1])
	}
}

// The video mode sends at most 1920×1080, aspect kept, even sizes.
func TestFitVideoAndScaledPipeline(t *testing.T) {
	for _, c := range []struct{ w, h, ow, oh int }{
		{1280, 800, 1280, 800}, {1920, 1080, 1920, 1080}, {3840, 2160, 1920, 1080},
		{2560, 1600, 1728, 1080}, {3440, 1440, 1920, 802}, {1080, 2400, 486, 1080},
	} {
		if ow, oh := fitVideo(c.w, c.h); ow != c.ow || oh != c.oh {
			t.Errorf("%dx%d → %dx%d, want %dx%d", c.w, c.h, ow, oh, c.ow, c.oh)
		}
	}
	args, err := pipelineArgs(encoderParams{GPU: "cpu", W: 3840, H: 2160, OutW: 1920, OutH: 1080, FPS: 30, Kbps: 4000})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"endx=3839", "videoscale", "format=I420,width=1920,height=1080"} {
		if !strings.Contains(line, want) {
			t.Fatalf("%q missing in %s", want, line)
		}
	}
	// Same size: no scaling in the pipeline.
	args, _ = pipelineArgs(encoderParams{GPU: "cpu", W: 1280, H: 800, OutW: 1280, OutH: 800, FPS: 30, Kbps: 4000})
	if strings.Contains(strings.Join(args, " "), "videoscale") {
		t.Fatal("scaled without need")
	}
}
